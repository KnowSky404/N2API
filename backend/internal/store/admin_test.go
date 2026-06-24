package store

import (
	"context"
	"errors"
	"os"
	"slices"
	"strings"
	"testing"

	"github.com/KnowSky404/N2API/backend/internal/admin"
	"github.com/jackc/pgx/v5/pgxpool"
)

func TestAdminRepositoryImplementsInterface(t *testing.T) {
	var _ admin.Repository = (*AdminRepository)(nil)
}

func TestUsageSummaryGroupSQLAllowsOnlyKnownGroups(t *testing.T) {
	for _, groupBy := range []string{"client_key", "provider_account", "model", "session"} {
		t.Run(groupBy, func(t *testing.T) {
			groupExpr, labelExpr, _, ok := usageSummaryGroupSQL(groupBy)
			if !ok {
				t.Fatalf("usageSummaryGroupSQL(%q) ok = false, want true", groupBy)
			}
			if groupExpr == "" || labelExpr == "" {
				t.Fatalf("usageSummaryGroupSQL(%q) returned empty expressions", groupBy)
			}
		})
	}

	if _, _, _, ok := usageSummaryGroupSQL("status; DROP TABLE request_logs"); ok {
		t.Fatal("usageSummaryGroupSQL accepted an unknown group")
	}
}

func TestUsageSummarySessionGroupUsesLoggedSessionID(t *testing.T) {
	groupExpr, labelExpr, joinSQL, ok := usageSummaryGroupSQL("session")
	if !ok {
		t.Fatal("usageSummaryGroupSQL(session) ok = false, want true")
	}
	if !strings.Contains(groupExpr, "l.session_id") || !strings.Contains(labelExpr, "l.session_id") {
		t.Fatalf("session group expressions = %q / %q, want request log session_id", groupExpr, labelExpr)
	}
	if joinSQL != "" {
		t.Fatalf("session group join SQL = %q, want no join", joinSQL)
	}
}

func TestListRequestLogsPrefersProviderAccountNameSnapshot(t *testing.T) {
	source, err := os.ReadFile("admin.go")
	if err != nil {
		t.Fatalf("ReadFile admin.go returned error: %v", err)
	}
	sql := string(source)
	if !strings.Contains(sql, "COALESCE(NULLIF(l.provider_account_name, ''), NULLIF(a.display_name, ''), a.name, '')") {
		t.Fatal("ListRequestLogs must prefer the logged provider account name snapshot before joining the current account row")
	}
}

func TestListRequestLogsSelectsGatewayFallbackDiagnostics(t *testing.T) {
	source, err := os.ReadFile("admin.go")
	if err != nil {
		t.Fatalf("ReadFile admin.go returned error: %v", err)
	}
	sql := string(source)
	for _, want := range []string{
		"COALESCE(l.gateway_attempt_count, 0)",
		"COALESCE(l.gateway_fallback_count, 0)",
		"&log.GatewayAttemptCount",
		"&log.GatewayFallbackCount",
	} {
		if !strings.Contains(sql, want) {
			t.Fatalf("ListRequestLogs source missing %q", want)
		}
	}
}

func TestListRequestLogsSupportsParameterizedFilters(t *testing.T) {
	whereSQL, args := requestLogFilterSQL(admin.RequestLogFilter{
		Query:             "codex",
		StatusClass:       admin.RequestLogStatusServerError,
		ProviderAccountID: 7,
		ClientKeyID:       12,
		Model:             "gpt-5",
		SessionID:         "workspace-123",
	})
	if len(args) != 5 || args[0] != int64(7) || args[1] != int64(12) || args[2] != "gpt-5" || args[3] != "workspace-123" || args[4] != "codex" {
		t.Fatalf("args = %+v, want provider account 7, client key 12, model gpt-5, session workspace-123, and codex args", args)
	}
	for _, want := range []string{
		"ILIKE '%' || $",
		"l.status_code >= 500",
		"l.provider_account_id = $",
		"l.client_key_id = $",
		"l.model = $",
		"l.session_id = $",
		"l.request_id",
		"l.error",
		"l.status_code::text",
	} {
		if !strings.Contains(whereSQL, want) {
			t.Fatalf("requestLogFilterSQL missing %q in %s", want, whereSQL)
		}
	}

	whereSQL, _ = requestLogFilterSQL(admin.RequestLogFilter{StatusClass: admin.RequestLogStatusClientError})
	if !strings.Contains(whereSQL, "l.status_code >= 400 AND l.status_code < 500") {
		t.Fatalf("client error filter SQL = %s", whereSQL)
	}

	whereSQL, _ = requestLogFilterSQL(admin.RequestLogFilter{StatusClass: admin.RequestLogStatusSuccess})
	if !strings.Contains(whereSQL, "l.status_code >= 200 AND l.status_code < 400") {
		t.Fatalf("success filter SQL = %s", whereSQL)
	}
}

func TestUsageSummaryProviderAccountGroupPrefersLoggedNameSnapshot(t *testing.T) {
	groupExpr, labelExpr, joinSQL, ok := usageSummaryGroupSQL("provider_account")
	if !ok {
		t.Fatal("usageSummaryGroupSQL(provider_account) ok = false, want true")
	}
	if !strings.Contains(groupExpr, "l.provider") || !strings.Contains(groupExpr, "l.provider_account_id") {
		t.Fatalf("provider account group expression = %q, want provider plus account id", groupExpr)
	}
	if !strings.Contains(labelExpr, "NULLIF(l.provider_account_name, '')") {
		t.Fatalf("provider account label expression = %q, want logged name snapshot first", labelExpr)
	}
	if !strings.Contains(labelExpr, "l.provider") || !strings.Contains(labelExpr, " / ") {
		t.Fatalf("provider account label expression = %q, want provider-prefixed label", labelExpr)
	}
	if !strings.Contains(joinSQL, "LEFT JOIN provider_accounts") {
		t.Fatalf("provider account join SQL = %q, want current account fallback join", joinSQL)
	}
}

func TestAdminRepositoryUsagePricingSettings(t *testing.T) {
	repo := newTestAdminRepository(t)
	ctx := context.Background()

	if _, err := repo.GetUsagePricing(ctx); !errors.Is(err, admin.ErrNotFound) {
		t.Fatalf("GetUsagePricing empty error = %v, want ErrNotFound", err)
	}

	saved, err := repo.SaveUsagePricing(ctx, admin.UsagePricing{
		Version:  1,
		Currency: "USD",
		Unit:     "1M_tokens",
		Models: map[string]admin.UsagePrice{
			"gpt-5": {
				InputMicrousdPerMillion:       1_000_000,
				CachedInputMicrousdPerMillion: 100_000,
				OutputMicrousdPerMillion:      4_000_000,
			},
		},
	})
	if err != nil {
		t.Fatalf("SaveUsagePricing returned error: %v", err)
	}

	found, err := repo.GetUsagePricing(ctx)
	if err != nil {
		t.Fatalf("GetUsagePricing returned error: %v", err)
	}
	if found.Currency != saved.Currency || found.Unit != saved.Unit || found.Models["gpt-5"].OutputMicrousdPerMillion != 4_000_000 {
		t.Fatalf("pricing = %+v, want saved pricing", found)
	}
}

func TestAdminRepositoryAPIKeyModelPolicyBehavior(t *testing.T) {
	repo := newTestAdminRepository(t)
	ctx := context.Background()

	created, err := repo.CreateAPIKey(ctx, "codex laptop", "hash-model-policy", "n2api_")
	if err != nil {
		t.Fatalf("CreateAPIKey returned error: %v", err)
	}
	if created.ModelPolicy != admin.APIKeyModelPolicyAll {
		t.Fatalf("ModelPolicy = %q, want all", created.ModelPolicy)
	}
	if len(created.AllowedModels) != 0 {
		t.Fatalf("AllowedModels = %+v, want empty default", created.AllowedModels)
	}

	updated, err := repo.UpdateAPIKeyModelPolicy(ctx, created.ID, admin.APIKeyModelPolicySelected, []string{" gpt-5 ", "gpt-5-mini", "gpt-5"})
	if err != nil {
		t.Fatalf("UpdateAPIKeyModelPolicy selected returned error: %v", err)
	}
	if updated.ModelPolicy != admin.APIKeyModelPolicySelected || !slices.Equal(updated.AllowedModels, []string{"gpt-5", "gpt-5-mini"}) {
		t.Fatalf("updated key = %+v, want selected models", updated)
	}

	models, err := repo.ListAPIKeyModels(ctx, created.ID)
	if err != nil {
		t.Fatalf("ListAPIKeyModels returned error: %v", err)
	}
	if !slices.Equal(models, []string{"gpt-5", "gpt-5-mini"}) {
		t.Fatalf("models = %+v, want persisted selected models", models)
	}

	keys, err := repo.ListAPIKeys(ctx)
	if err != nil {
		t.Fatalf("ListAPIKeys returned error: %v", err)
	}
	if len(keys) != 1 || keys[0].ModelPolicy != admin.APIKeyModelPolicySelected || !slices.Equal(keys[0].AllowedModels, []string{"gpt-5", "gpt-5-mini"}) {
		t.Fatalf("keys = %+v, want selected policy with models", keys)
	}

	renamed, err := repo.UpdateAPIKeyName(ctx, created.ID, "renamed codex laptop")
	if err != nil {
		t.Fatalf("UpdateAPIKeyName returned error: %v", err)
	}
	if renamed.Name != "renamed codex laptop" || renamed.ModelPolicy != admin.APIKeyModelPolicySelected || !slices.Equal(renamed.AllowedModels, []string{"gpt-5", "gpt-5-mini"}) {
		t.Fatalf("renamed key = %+v, want renamed selected-policy key", renamed)
	}

	found, err := repo.FindAPIKeyByHash(ctx, "hash-model-policy", created.CreatedAt)
	if err != nil {
		t.Fatalf("FindAPIKeyByHash returned error: %v", err)
	}
	if found.ModelPolicy != admin.APIKeyModelPolicySelected || !slices.Equal(found.AllowedModels, []string{"gpt-5", "gpt-5-mini"}) {
		t.Fatalf("found key = %+v, want selected policy with models", found)
	}

	disabled, err := repo.SetAPIKeyDisabled(ctx, created.ID, true)
	if err != nil {
		t.Fatalf("SetAPIKeyDisabled true returned error: %v", err)
	}
	if disabled.DisabledAt == nil || disabled.ModelPolicy != admin.APIKeyModelPolicySelected || !slices.Equal(disabled.AllowedModels, []string{"gpt-5", "gpt-5-mini"}) {
		t.Fatalf("disabled key = %+v, want disabled selected-policy key", disabled)
	}
	if _, err := repo.FindAPIKeyByHash(ctx, "hash-model-policy", created.CreatedAt); !errors.Is(err, admin.ErrNotFound) {
		t.Fatalf("FindAPIKeyByHash disabled error = %v, want ErrNotFound", err)
	}
	enabled, err := repo.SetAPIKeyDisabled(ctx, created.ID, false)
	if err != nil {
		t.Fatalf("SetAPIKeyDisabled false returned error: %v", err)
	}
	if enabled.DisabledAt != nil {
		t.Fatalf("DisabledAt = %v, want nil after enable", enabled.DisabledAt)
	}
	if _, err := repo.FindAPIKeyByHash(ctx, "hash-model-policy", created.CreatedAt); err != nil {
		t.Fatalf("FindAPIKeyByHash after enable returned error: %v", err)
	}

	cleared, err := repo.UpdateAPIKeyModelPolicy(ctx, created.ID, admin.APIKeyModelPolicyAll, nil)
	if err != nil {
		t.Fatalf("UpdateAPIKeyModelPolicy all returned error: %v", err)
	}
	if cleared.ModelPolicy != admin.APIKeyModelPolicyAll || len(cleared.AllowedModels) != 0 {
		t.Fatalf("cleared key = %+v, want all policy with no models", cleared)
	}
	models, err = repo.ListAPIKeyModels(ctx, created.ID)
	if err != nil {
		t.Fatalf("ListAPIKeyModels after clear returned error: %v", err)
	}
	if len(models) != 0 {
		t.Fatalf("models after clear = %+v, want empty", models)
	}

	revoked, err := repo.RevokeAPIKey(ctx, created.ID)
	if err != nil {
		t.Fatalf("RevokeAPIKey returned error: %v", err)
	}
	if revoked.ModelPolicy != admin.APIKeyModelPolicyAll {
		t.Fatalf("revoked ModelPolicy = %q, want all", revoked.ModelPolicy)
	}
	if _, err := repo.UpdateAPIKeyModelPolicy(ctx, created.ID, admin.APIKeyModelPolicySelected, []string{"gpt-5"}); !errors.Is(err, admin.ErrNotFound) {
		t.Fatalf("UpdateAPIKeyModelPolicy revoked error = %v, want ErrNotFound", err)
	}
	if _, err := repo.UpdateAPIKeyName(ctx, created.ID, "revoked rename"); !errors.Is(err, admin.ErrNotFound) {
		t.Fatalf("UpdateAPIKeyName revoked error = %v, want ErrNotFound", err)
	}
	if _, err := repo.SetAPIKeyDisabled(ctx, created.ID, true); !errors.Is(err, admin.ErrNotFound) {
		t.Fatalf("SetAPIKeyDisabled revoked error = %v, want ErrNotFound", err)
	}
}

func newTestAdminRepository(t *testing.T) *AdminRepository {
	t.Helper()

	dsn := os.Getenv("N2API_STORE_TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("N2API_STORE_TEST_DATABASE_URL is not set")
	}
	pool, err := pgxpool.New(context.Background(), dsn)
	if err != nil {
		t.Fatalf("pgxpool.New returned error: %v", err)
	}
	t.Cleanup(pool.Close)

	if _, err := pool.Exec(context.Background(), "TRUNCATE client_api_key_models, request_logs, client_api_keys, admin_sessions, admins, settings RESTART IDENTITY CASCADE"); err != nil {
		t.Fatalf("test database cleanup failed: %v", err)
	}
	return NewAdminRepository(pool)
}

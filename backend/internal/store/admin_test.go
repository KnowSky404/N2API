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
	for _, groupBy := range []string{"client_key", "provider_account", "model"} {
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

	found, err := repo.FindAPIKeyByHash(ctx, "hash-model-policy", created.CreatedAt)
	if err != nil {
		t.Fatalf("FindAPIKeyByHash returned error: %v", err)
	}
	if found.ModelPolicy != admin.APIKeyModelPolicySelected || !slices.Equal(found.AllowedModels, []string{"gpt-5", "gpt-5-mini"}) {
		t.Fatalf("found key = %+v, want selected policy with models", found)
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

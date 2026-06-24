package admin

import (
	"context"
	"encoding/json"
	"errors"
	"slices"
	"strconv"
	"strings"
	"testing"
	"time"
)

func TestBootstrapCreatesAdminOnceAndPreservesExistingHash(t *testing.T) {
	repo := newMemoryRepo()
	service := NewService(repo, Config{SessionTTL: 7 * 24 * time.Hour})

	if err := service.BootstrapAdmin(context.Background(), "admin", "first-password"); err != nil {
		t.Fatalf("BootstrapAdmin returned error: %v", err)
	}
	firstHash := repo.admin.PasswordHash
	if err := service.BootstrapAdmin(context.Background(), "admin", "second-password"); err != nil {
		t.Fatalf("BootstrapAdmin returned error: %v", err)
	}
	if repo.admin.PasswordHash != firstHash {
		t.Fatal("BootstrapAdmin changed existing password hash")
	}
}

func TestBootstrapRenamesExistingAdminAndPreservesPasswordHash(t *testing.T) {
	repo := newMemoryRepo()
	service := NewService(repo, Config{SessionTTL: 7 * 24 * time.Hour})

	if err := service.BootstrapAdmin(context.Background(), "admin", "first-password"); err != nil {
		t.Fatalf("BootstrapAdmin returned error: %v", err)
	}
	firstID := repo.admin.ID
	firstHash := repo.admin.PasswordHash

	if err := service.BootstrapAdmin(context.Background(), "owner", "second-password"); err != nil {
		t.Fatalf("BootstrapAdmin returned error: %v", err)
	}

	if repo.admin.ID != firstID {
		t.Fatalf("admin ID = %d, want existing ID %d", repo.admin.ID, firstID)
	}
	if repo.admin.Username != "owner" {
		t.Fatalf("admin username = %q, want owner", repo.admin.Username)
	}
	if repo.admin.PasswordHash != firstHash {
		t.Fatal("BootstrapAdmin changed existing password hash")
	}
	if _, err := service.Login(context.Background(), "admin", "first-password"); !errors.Is(err, ErrUnauthorized) {
		t.Fatalf("old username login error = %v, want ErrUnauthorized", err)
	}
	if _, err := service.Login(context.Background(), "owner", "first-password"); err != nil {
		t.Fatalf("new username login returned error: %v", err)
	}
}

func TestLoginCreatesSessionAndValidateSessionReturnsAdmin(t *testing.T) {
	repo := newMemoryRepo()
	service := NewService(repo, Config{SessionTTL: time.Hour})
	requireBootstrap(t, service, "admin", "secret")

	session, err := service.Login(context.Background(), "admin", "secret")
	if err != nil {
		t.Fatalf("Login returned error: %v", err)
	}
	if session.Token == "" || session.ExpiresAt.IsZero() {
		t.Fatalf("invalid session: %+v", session)
	}
	admin, err := service.ValidateSession(context.Background(), session.Token)
	if err != nil {
		t.Fatalf("ValidateSession returned error: %v", err)
	}
	if admin.Username != "admin" {
		t.Fatalf("Username = %q, want admin", admin.Username)
	}
}

func TestAdminJSONOmitsPasswordHash(t *testing.T) {
	payload, err := json.Marshal(Admin{ID: 1, Username: "admin", PasswordHash: "secret-hash"})
	if err != nil {
		t.Fatalf("json.Marshal returned error: %v", err)
	}
	if strings.Contains(string(payload), "secret-hash") {
		t.Fatalf("json payload contains password hash: %s", payload)
	}
}

func TestLoginRejectsInvalidCredentials(t *testing.T) {
	repo := newMemoryRepo()
	service := NewService(repo, Config{SessionTTL: time.Hour})
	requireBootstrap(t, service, "admin", "secret")

	if _, err := service.Login(context.Background(), "admin", "wrong"); !errors.Is(err, ErrUnauthorized) {
		t.Fatalf("Login wrong password error = %v, want ErrUnauthorized", err)
	}
	if _, err := service.Login(context.Background(), "missing", "secret"); !errors.Is(err, ErrUnauthorized) {
		t.Fatalf("Login missing username error = %v, want ErrUnauthorized", err)
	}
}

func TestLogoutRevokesSession(t *testing.T) {
	repo := newMemoryRepo()
	service := NewService(repo, Config{SessionTTL: time.Hour})
	requireBootstrap(t, service, "admin", "secret")
	session, err := service.Login(context.Background(), "admin", "secret")
	if err != nil {
		t.Fatalf("Login returned error: %v", err)
	}

	if err := service.Logout(context.Background(), session.Token); err != nil {
		t.Fatalf("Logout returned error: %v", err)
	}
	if _, err := service.ValidateSession(context.Background(), session.Token); !errors.Is(err, ErrUnauthorized) {
		t.Fatalf("ValidateSession after logout error = %v, want ErrUnauthorized", err)
	}
	if err := service.Logout(context.Background(), ""); err != nil {
		t.Fatalf("Logout empty token returned error: %v", err)
	}
	if err := service.Logout(context.Background(), "unknown-token"); err != nil {
		t.Fatalf("Logout unknown token returned error: %v", err)
	}
}

func TestCreateAPIKeyReturnsSecretOnceAndAuthenticateRejectsRevoked(t *testing.T) {
	repo := newMemoryRepo()
	service := NewService(repo, Config{SessionTTL: time.Hour})
	result, err := service.CreateAPIKey(context.Background(), "codex laptop")
	if err != nil {
		t.Fatalf("CreateAPIKey returned error: %v", err)
	}
	if result.Secret == "" || result.Key.Prefix == "" {
		t.Fatalf("missing secret or prefix: %+v", result)
	}
	if strings.Contains(repo.keys[result.Key.ID].Hash, result.Secret) {
		t.Fatal("repository stored cleartext key")
	}
	if _, err := service.AuthenticateAPIKey(context.Background(), result.Secret); err != nil {
		t.Fatalf("AuthenticateAPIKey returned error: %v", err)
	}
	if _, err := service.RevokeAPIKey(context.Background(), result.Key.ID); err != nil {
		t.Fatalf("RevokeAPIKey returned error: %v", err)
	}
	if _, err := service.AuthenticateAPIKey(context.Background(), result.Secret); !errors.Is(err, ErrUnauthorized) {
		t.Fatalf("AuthenticateAPIKey error = %v, want ErrUnauthorized", err)
	}
}

func TestAuthenticateAPIKeyMapsTouchNotFoundToUnauthorized(t *testing.T) {
	repo := newMemoryRepo()
	service := NewService(repo, Config{SessionTTL: time.Hour})
	result, err := service.CreateAPIKey(context.Background(), "codex laptop")
	if err != nil {
		t.Fatalf("CreateAPIKey returned error: %v", err)
	}
	repo.touchErr = ErrNotFound

	if _, err := service.AuthenticateAPIKey(context.Background(), result.Secret); !errors.Is(err, ErrUnauthorized) {
		t.Fatalf("AuthenticateAPIKey error = %v, want ErrUnauthorized", err)
	}
}

func TestCreateAPIKeyRejectsInvalidName(t *testing.T) {
	repo := newMemoryRepo()
	service := NewService(repo, Config{SessionTTL: time.Hour})

	if _, err := service.CreateAPIKey(context.Background(), " \t "); !errors.Is(err, ErrInvalidInput) {
		t.Fatalf("CreateAPIKey error = %v, want ErrInvalidInput", err)
	}
}

func TestListAPIKeysReturnsRepositoryKeys(t *testing.T) {
	repo := newMemoryRepo()
	service := NewService(repo, Config{SessionTTL: time.Hour})
	first, err := service.CreateAPIKey(context.Background(), "first")
	if err != nil {
		t.Fatalf("CreateAPIKey first returned error: %v", err)
	}
	second, err := service.CreateAPIKey(context.Background(), "second")
	if err != nil {
		t.Fatalf("CreateAPIKey second returned error: %v", err)
	}

	keys, err := service.ListAPIKeys(context.Background())
	if err != nil {
		t.Fatalf("ListAPIKeys returned error: %v", err)
	}
	if len(keys) != 2 {
		t.Fatalf("ListAPIKeys returned %d keys, want 2", len(keys))
	}
	if keys[0].ID != first.Key.ID || keys[1].ID != second.Key.ID {
		t.Fatalf("ListAPIKeys IDs = [%d %d], want [%d %d]", keys[0].ID, keys[1].ID, first.Key.ID, second.Key.ID)
	}
}

func TestAPIKeyModelPolicyDefaultsToAll(t *testing.T) {
	repo := newMemoryRepo()
	service := NewService(repo, Config{SessionTTL: time.Hour})
	result, err := service.CreateAPIKey(context.Background(), "codex laptop")
	if err != nil {
		t.Fatalf("CreateAPIKey returned error: %v", err)
	}

	if result.Key.ModelPolicy != APIKeyModelPolicyAll {
		t.Fatalf("ModelPolicy = %q, want all", result.Key.ModelPolicy)
	}
	if len(result.Key.AllowedModels) != 0 {
		t.Fatalf("AllowedModels = %+v, want empty for all policy", result.Key.AllowedModels)
	}
	for _, policy := range []string{"", APIKeyModelPolicyAll} {
		if !service.APIKeyAllowsModel(APIKey{ModelPolicy: policy}, "gpt-5") {
			t.Fatalf("APIKeyAllowsModel policy %q returned false, want true", policy)
		}
	}
}

func TestAPIKeySelectedModelPolicy(t *testing.T) {
	repo := newMemoryRepo()
	service := NewService(repo, Config{SessionTTL: time.Hour})
	result, err := service.CreateAPIKey(context.Background(), "codex laptop")
	if err != nil {
		t.Fatalf("CreateAPIKey returned error: %v", err)
	}

	updated, err := service.UpdateAPIKeyModelPolicy(context.Background(), result.Key.ID, APIKeyModelPolicySelected, []string{
		" gpt-5 ",
		"gpt-5-mini",
		"gpt-5",
		"",
	})
	if err != nil {
		t.Fatalf("UpdateAPIKeyModelPolicy returned error: %v", err)
	}
	if updated.ModelPolicy != APIKeyModelPolicySelected {
		t.Fatalf("ModelPolicy = %q, want selected", updated.ModelPolicy)
	}
	if !slices.Equal(updated.AllowedModels, []string{"gpt-5", "gpt-5-mini"}) {
		t.Fatalf("AllowedModels = %+v, want normalized selected models", updated.AllowedModels)
	}
	if !service.APIKeyAllowsModel(updated, "gpt-5-mini") {
		t.Fatal("APIKeyAllowsModel returned false for selected model")
	}
	if service.APIKeyAllowsModel(updated, "gpt-4o") {
		t.Fatal("APIKeyAllowsModel returned true for unselected model")
	}
	if service.APIKeyAllowsModel(APIKey{ModelPolicy: "unknown", AllowedModels: []string{"gpt-5"}}, "gpt-5") {
		t.Fatal("APIKeyAllowsModel returned true for unknown policy")
	}

	models, err := repo.ListAPIKeyModels(context.Background(), result.Key.ID)
	if err != nil {
		t.Fatalf("ListAPIKeyModels returned error: %v", err)
	}
	if !slices.Equal(models, []string{"gpt-5", "gpt-5-mini"}) {
		t.Fatalf("stored models = %+v, want normalized selected models", models)
	}

	updated, err = service.UpdateAPIKeyModelPolicy(context.Background(), result.Key.ID, APIKeyModelPolicyAll, []string{"gpt-5"})
	if err != nil {
		t.Fatalf("UpdateAPIKeyModelPolicy all returned error: %v", err)
	}
	if updated.ModelPolicy != APIKeyModelPolicyAll || len(updated.AllowedModels) != 0 {
		t.Fatalf("updated key = %+v, want all policy with no models", updated)
	}
}

func TestUpdateAPIKeyModelPolicyRejectsInvalidInput(t *testing.T) {
	repo := newMemoryRepo()
	service := NewService(repo, Config{SessionTTL: time.Hour})
	result, err := service.CreateAPIKey(context.Background(), "codex laptop")
	if err != nil {
		t.Fatalf("CreateAPIKey returned error: %v", err)
	}

	for _, tc := range []struct {
		name   string
		policy string
		models []string
	}{
		{name: "invalid policy", policy: "limited", models: []string{"gpt-5"}},
		{name: "empty selected", policy: APIKeyModelPolicySelected, models: []string{" ", ""}},
		{name: "too many models", policy: APIKeyModelPolicySelected, models: buildModelNames(101)},
		{name: "model name too long", policy: APIKeyModelPolicySelected, models: []string{strings.Repeat("a", 129)}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if _, err := service.UpdateAPIKeyModelPolicy(context.Background(), result.Key.ID, tc.policy, tc.models); !errors.Is(err, ErrInvalidInput) {
				t.Fatalf("UpdateAPIKeyModelPolicy error = %v, want ErrInvalidInput", err)
			}
		})
	}
}

func TestUpdateAPIKeyLimitsPersistsNonNegativeLimits(t *testing.T) {
	repo := newMemoryRepo()
	service := NewService(repo, Config{})
	result, err := service.CreateAPIKey(context.Background(), "codex laptop")
	if err != nil {
		t.Fatalf("CreateAPIKey returned error: %v", err)
	}

	updated, err := service.UpdateAPIKeyLimits(context.Background(), result.Key.ID, 12, 40000)
	if err != nil {
		t.Fatalf("UpdateAPIKeyLimits returned error: %v", err)
	}
	if updated.RequestsPerMinute != 12 || updated.TokensPerMinute != 40000 {
		t.Fatalf("updated limits = %d/%d, want 12/40000", updated.RequestsPerMinute, updated.TokensPerMinute)
	}

	found, err := service.AuthenticateAPIKey(context.Background(), result.Secret)
	if err != nil {
		t.Fatalf("AuthenticateAPIKey returned error: %v", err)
	}
	if found.RequestsPerMinute != 12 || found.TokensPerMinute != 40000 {
		t.Fatalf("authenticated limits = %d/%d, want 12/40000", found.RequestsPerMinute, found.TokensPerMinute)
	}
}

func TestUpdateAPIKeyLimitsRejectsNegativeLimits(t *testing.T) {
	service := NewService(newMemoryRepo(), Config{})

	if _, err := service.UpdateAPIKeyLimits(context.Background(), 7, -1, 0); !errors.Is(err, ErrInvalidInput) {
		t.Fatalf("UpdateAPIKeyLimits negative requests error = %v, want ErrInvalidInput", err)
	}
	if _, err := service.UpdateAPIKeyLimits(context.Background(), 7, 0, -1); !errors.Is(err, ErrInvalidInput) {
		t.Fatalf("UpdateAPIKeyLimits negative tokens error = %v, want ErrInvalidInput", err)
	}
}

func TestListRequestLogsClampsLimitAndReturnsRepositoryLogs(t *testing.T) {
	repo := newMemoryRepo()
	service := NewService(repo, Config{SessionTTL: time.Hour})
	repo.logs = []RequestLog{
		{ID: 2, RequestID: "req_2", Route: "/v1/chat/completions", StatusCode: 200, ProviderAccountID: 7, ProviderAccountType: "api_upstream", ProviderAccountName: "Upstream A", Model: "gpt-5", SessionID: "workspace-123", InputTokens: 12, OutputTokens: 3, TotalTokens: 15, EstimatedCostMicrousd: 1234, PricingMatched: true, UsageSource: "chat_completions"},
		{ID: 1, RequestID: "req_1", Route: "/v1/models", StatusCode: 503},
	}

	logs, err := service.ListRequestLogs(context.Background(), RequestLogFilter{
		Query:       "  gpt-5  ",
		StatusClass: "server_error",
	})
	if err != nil {
		t.Fatalf("ListRequestLogs returned error: %v", err)
	}
	if repo.lastLogFilter.Limit != 50 {
		t.Fatalf("repository limit = %d, want default 50", repo.lastLogFilter.Limit)
	}
	if repo.lastLogFilter.Query != "gpt-5" {
		t.Fatalf("repository query = %q, want gpt-5", repo.lastLogFilter.Query)
	}
	if repo.lastLogFilter.StatusClass != RequestLogStatusServerError {
		t.Fatalf("repository status class = %q, want %q", repo.lastLogFilter.StatusClass, RequestLogStatusServerError)
	}
	if len(logs) != 2 || logs[0].RequestID != "req_2" {
		t.Fatalf("logs = %+v", logs)
	}
	if logs[0].ProviderAccountID != 7 || logs[0].ProviderAccountType != "api_upstream" || logs[0].ProviderAccountName != "Upstream A" {
		t.Fatalf("log account attribution = %+v", logs[0])
	}
	if logs[0].Model != "gpt-5" {
		t.Fatalf("log model = %q, want gpt-5", logs[0].Model)
	}
	if logs[0].SessionID != "workspace-123" {
		t.Fatalf("log session ID = %q, want workspace-123", logs[0].SessionID)
	}
	if logs[0].InputTokens != 12 || logs[0].OutputTokens != 3 || logs[0].TotalTokens != 15 || logs[0].UsageSource != "chat_completions" {
		t.Fatalf("log usage = %+v, want token usage fields", logs[0])
	}
	if logs[0].EstimatedCostMicrousd != 1234 {
		t.Fatalf("log cost = %d, want 1234", logs[0].EstimatedCostMicrousd)
	}
	if !logs[0].PricingMatched {
		t.Fatal("PricingMatched = false, want true")
	}

	if _, err := service.ListRequestLogs(context.Background(), RequestLogFilter{Limit: 500}); err != nil {
		t.Fatalf("ListRequestLogs returned error: %v", err)
	}
	if repo.lastLogFilter.Limit != 200 {
		t.Fatalf("repository limit = %d, want max 200", repo.lastLogFilter.Limit)
	}
	if repo.lastLogFilter.StatusClass != RequestLogStatusAll {
		t.Fatalf("repository status class = %q, want all", repo.lastLogFilter.StatusClass)
	}

	if _, err := service.ListRequestLogs(context.Background(), RequestLogFilter{StatusClass: "bad"}); !errors.Is(err, ErrInvalidInput) {
		t.Fatalf("ListRequestLogs invalid status error = %v, want ErrInvalidInput", err)
	}
	if _, err := service.ListRequestLogs(context.Background(), RequestLogFilter{Query: strings.Repeat("x", 201)}); !errors.Is(err, ErrInvalidInput) {
		t.Fatalf("ListRequestLogs long query error = %v, want ErrInvalidInput", err)
	}
}

func TestGetUsageSummaryValidatesRangeAndGroup(t *testing.T) {
	repo := newMemoryRepo()
	service := NewService(repo, Config{SessionTTL: time.Hour})
	repo.usageSummary = UsageSummary{
		Rows: []UsageSummaryRow{{ID: "gpt-5", Label: "gpt-5", Requests: 2, InputTokens: 30, OutputTokens: 10, TotalTokens: 40}},
	}

	summary, err := service.GetUsageSummary(context.Background(), "7d", "model")
	if err != nil {
		t.Fatalf("GetUsageSummary returned error: %v", err)
	}
	if summary.Range != "7d" || summary.GroupBy != "model" || repo.lastUsageGroupBy != "model" {
		t.Fatalf("summary metadata = %+v repo group=%q", summary, repo.lastUsageGroupBy)
	}
	if summary.TotalRequests != 2 || summary.TotalInputTokens != 30 || summary.TotalOutputTokens != 10 || summary.TotalTokens != 40 {
		t.Fatalf("summary totals = %+v, want row totals", summary)
	}
	if _, err := service.GetUsageSummary(context.Background(), "7d", "session"); err != nil {
		t.Fatalf("GetUsageSummary session returned error: %v", err)
	}
	if repo.lastUsageGroupBy != "session" {
		t.Fatalf("repository group = %q, want session", repo.lastUsageGroupBy)
	}

	for _, tc := range []struct {
		name    string
		rangeIn string
		groupBy string
	}{
		{name: "bad range", rangeIn: "bad", groupBy: "model"},
		{name: "bad group", rangeIn: "7d", groupBy: "bad"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if _, err := service.GetUsageSummary(context.Background(), tc.rangeIn, tc.groupBy); !errors.Is(err, ErrInvalidInput) {
				t.Fatalf("GetUsageSummary error = %v, want ErrInvalidInput", err)
			}
		})
	}
}

func TestModelSettingsDefaultAndUpdate(t *testing.T) {
	repo := newMemoryRepo()
	service := NewService(repo, Config{SessionTTL: time.Hour})

	settings, err := service.GetModelSettings(context.Background())
	if err != nil {
		t.Fatalf("GetModelSettings returned error: %v", err)
	}
	if settings.DefaultModel != "gpt-4.1" {
		t.Fatalf("DefaultModel = %q, want gpt-4.1", settings.DefaultModel)
	}
	if len(settings.AllowedModels) == 0 {
		t.Fatal("AllowedModels is empty")
	}

	updated, err := service.UpdateModelSettings(context.Background(), ModelSettings{
		DefaultModel:  " gpt-5 ",
		AllowedModels: []string{" gpt-5 ", "", "gpt-5-mini", "gpt-5"},
	})
	if err != nil {
		t.Fatalf("UpdateModelSettings returned error: %v", err)
	}
	if updated.DefaultModel != "gpt-5" {
		t.Fatalf("DefaultModel = %q, want gpt-5", updated.DefaultModel)
	}
	if !slices.Equal(updated.AllowedModels, []string{"gpt-5", "gpt-5-mini"}) {
		t.Fatalf("AllowedModels = %+v, want normalized unique list", updated.AllowedModels)
	}
}

func TestUpdateModelSettingsRejectsInvalidInput(t *testing.T) {
	repo := newMemoryRepo()
	service := NewService(repo, Config{SessionTTL: time.Hour})

	for _, tc := range []struct {
		name     string
		settings ModelSettings
	}{
		{name: "empty default", settings: ModelSettings{DefaultModel: " ", AllowedModels: []string{"gpt-5"}}},
		{name: "default not allowed", settings: ModelSettings{DefaultModel: "gpt-5", AllowedModels: []string{"gpt-5-mini"}}},
		{name: "empty allowed", settings: ModelSettings{DefaultModel: "gpt-5"}},
		{name: "model name too long", settings: ModelSettings{DefaultModel: strings.Repeat("a", 129), AllowedModels: []string{strings.Repeat("a", 129)}}},
		{name: "too many models", settings: ModelSettings{DefaultModel: "model-0", AllowedModels: buildModelNames(101)}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if _, err := service.UpdateModelSettings(context.Background(), tc.settings); !errors.Is(err, ErrInvalidInput) {
				t.Fatalf("UpdateModelSettings error = %v, want ErrInvalidInput", err)
			}
		})
	}
}

func TestGatewaySettingsDefaultsToDisabledAndSavesLimits(t *testing.T) {
	repo := newMemoryRepo()
	service := NewService(repo, Config{
		SessionTTL: time.Hour,
		DefaultGatewaySettings: GatewaySettings{
			ProviderAccountAutoTestEnabled:         true,
			ProviderAccountAutoTestIntervalSeconds: 120,
		},
	})

	settings, err := service.GetGatewaySettings(context.Background())
	if err != nil {
		t.Fatalf("GetGatewaySettings returned error: %v", err)
	}
	wantDefault := GatewaySettings{
		ProviderAccountAutoTestEnabled:         true,
		ProviderAccountAutoTestIntervalSeconds: 120,
	}
	if settings != wantDefault {
		t.Fatalf("default gateway settings = %+v, want %+v", settings, wantDefault)
	}

	saved, err := service.UpdateGatewaySettings(context.Background(), GatewaySettings{
		MaxConcurrentGatewayRequests:           10,
		MaxConcurrentRequestsPerAccount:        2,
		MaxConcurrentRequestsPerKey:            3,
		RequestsPerMinutePerKey:                60,
		TokensPerMinutePerKey:                  60000,
		ProviderAccountAutoTestEnabled:         true,
		ProviderAccountAutoTestIntervalSeconds: 120,
	})
	if err != nil {
		t.Fatalf("UpdateGatewaySettings returned error: %v", err)
	}
	if saved.MaxConcurrentGatewayRequests != 10 ||
		saved.MaxConcurrentRequestsPerAccount != 2 ||
		saved.MaxConcurrentRequestsPerKey != 3 ||
		saved.RequestsPerMinutePerKey != 60 ||
		saved.TokensPerMinutePerKey != 60000 ||
		!saved.ProviderAccountAutoTestEnabled ||
		saved.ProviderAccountAutoTestIntervalSeconds != 120 {
		t.Fatalf("saved gateway settings = %+v", saved)
	}

	found, err := service.GetGatewaySettings(context.Background())
	if err != nil {
		t.Fatalf("GetGatewaySettings after save returned error: %v", err)
	}
	if found != saved {
		t.Fatalf("found gateway settings = %+v, want %+v", found, saved)
	}
}

func TestGatewaySettingsRejectsNegativeLimits(t *testing.T) {
	service := NewService(newMemoryRepo(), Config{SessionTTL: time.Hour})

	if _, err := service.UpdateGatewaySettings(context.Background(), GatewaySettings{MaxConcurrentGatewayRequests: -1}); !errors.Is(err, ErrInvalidInput) {
		t.Fatalf("UpdateGatewaySettings error = %v, want ErrInvalidInput", err)
	}
}

func TestGatewaySettingsNormalizesMissingAutoTestInterval(t *testing.T) {
	service := NewService(newMemoryRepo(), Config{SessionTTL: time.Hour})

	saved, err := service.UpdateGatewaySettings(context.Background(), GatewaySettings{})
	if err != nil {
		t.Fatalf("UpdateGatewaySettings returned error: %v", err)
	}
	if saved.ProviderAccountAutoTestEnabled {
		t.Fatal("ProviderAccountAutoTestEnabled = true, want false")
	}
	if saved.ProviderAccountAutoTestIntervalSeconds != 300 {
		t.Fatalf("ProviderAccountAutoTestIntervalSeconds = %d, want 300", saved.ProviderAccountAutoTestIntervalSeconds)
	}
}

func TestGatewaySettingsRejectsInvalidAutoTestSchedule(t *testing.T) {
	service := NewService(newMemoryRepo(), Config{SessionTTL: time.Hour})

	for _, settings := range []GatewaySettings{
		{ProviderAccountAutoTestIntervalSeconds: -1},
		{ProviderAccountAutoTestEnabled: true, ProviderAccountAutoTestIntervalSeconds: 30},
	} {
		if _, err := service.UpdateGatewaySettings(context.Background(), settings); !errors.Is(err, ErrInvalidInput) {
			t.Fatalf("UpdateGatewaySettings(%+v) error = %v, want ErrInvalidInput", settings, err)
		}
	}
}

func TestUsagePricingDefaultAndUpdate(t *testing.T) {
	repo := newMemoryRepo()
	service := NewService(repo, Config{SessionTTL: time.Hour})

	pricing, err := service.GetUsagePricing(context.Background())
	if err != nil {
		t.Fatalf("GetUsagePricing returned error: %v", err)
	}
	if pricing.Version != 1 || pricing.Currency != "USD" || pricing.Unit != "1M_tokens" {
		t.Fatalf("default pricing = %+v, want version 1 USD per 1M_tokens", pricing)
	}
	if len(pricing.Models) == 0 {
		t.Fatal("default pricing models is empty")
	}

	updated, err := service.UpdateUsagePricing(context.Background(), UsagePricing{
		Version:  1,
		Currency: " usd ",
		Unit:     "1M_tokens",
		Models: map[string]UsagePrice{
			" gpt-5 ": {
				InputMicrousdPerMillion:       1_000_000,
				CachedInputMicrousdPerMillion: 100_000,
				OutputMicrousdPerMillion:      4_000_000,
			},
		},
	})
	if err != nil {
		t.Fatalf("UpdateUsagePricing returned error: %v", err)
	}
	if updated.Currency != "USD" || updated.Unit != "1M_tokens" || updated.Version != 1 {
		t.Fatalf("updated pricing metadata = %+v", updated)
	}
	if updated.UpdatedAt.IsZero() {
		t.Fatal("UpdatedAt is zero")
	}
	if _, ok := updated.Models["gpt-5"]; !ok {
		t.Fatalf("Models = %+v, want trimmed gpt-5 key", updated.Models)
	}
}

func TestUpdateUsagePricingRejectsInvalidInput(t *testing.T) {
	repo := newMemoryRepo()
	service := NewService(repo, Config{SessionTTL: time.Hour})

	for _, tc := range []struct {
		name    string
		pricing UsagePricing
	}{
		{name: "bad currency", pricing: UsagePricing{Version: 1, Currency: "EUR", Unit: "1M_tokens", Models: map[string]UsagePrice{"gpt-5": {}}}},
		{name: "bad unit", pricing: UsagePricing{Version: 1, Currency: "USD", Unit: "tokens", Models: map[string]UsagePrice{"gpt-5": {}}}},
		{name: "empty models", pricing: UsagePricing{Version: 1, Currency: "USD", Unit: "1M_tokens"}},
		{name: "empty model name", pricing: UsagePricing{Version: 1, Currency: "USD", Unit: "1M_tokens", Models: map[string]UsagePrice{" ": {}}}},
		{name: "negative rate", pricing: UsagePricing{Version: 1, Currency: "USD", Unit: "1M_tokens", Models: map[string]UsagePrice{"gpt-5": {InputMicrousdPerMillion: -1}}}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if _, err := service.UpdateUsagePricing(context.Background(), tc.pricing); !errors.Is(err, ErrInvalidInput) {
				t.Fatalf("UpdateUsagePricing error = %v, want ErrInvalidInput", err)
			}
		})
	}
}

func TestEstimateUsageCostUsesConfiguredModelPricing(t *testing.T) {
	repo := newMemoryRepo()
	repo.usagePricing = UsagePricing{
		Version:  1,
		Currency: "USD",
		Unit:     "1M_tokens",
		Models: map[string]UsagePrice{
			"gpt-5": {
				InputMicrousdPerMillion:       1_000_000,
				CachedInputMicrousdPerMillion: 100_000,
				OutputMicrousdPerMillion:      4_000_000,
			},
		},
	}
	service := NewService(repo, Config{SessionTTL: time.Hour})

	estimate, err := service.EstimateUsageCost(context.Background(), UsageCostInput{
		Model:             "gpt-5",
		InputTokens:       1_000,
		CachedInputTokens: 200,
		OutputTokens:      500,
	})
	if err != nil {
		t.Fatalf("EstimateUsageCost returned error: %v", err)
	}
	if !estimate.Matched {
		t.Fatal("Matched = false, want true")
	}
	if estimate.CostMicrousd != 2820 {
		t.Fatalf("CostMicrousd = %d, want 2820", estimate.CostMicrousd)
	}
	if estimate.Snapshot["matched"] != true || estimate.Snapshot["model"] != "gpt-5" {
		t.Fatalf("Snapshot = %+v, want matched gpt-5", estimate.Snapshot)
	}
}

func TestEstimateUsageCostReturnsUnmatchedSnapshot(t *testing.T) {
	repo := newMemoryRepo()
	repo.usagePricing = UsagePricing{
		Version:  1,
		Currency: "USD",
		Unit:     "1M_tokens",
		Models: map[string]UsagePrice{
			"gpt-5": {},
		},
	}
	service := NewService(repo, Config{SessionTTL: time.Hour})

	estimate, err := service.EstimateUsageCost(context.Background(), UsageCostInput{Model: "unknown-model", InputTokens: 100})
	if err != nil {
		t.Fatalf("EstimateUsageCost returned error: %v", err)
	}
	if estimate.Matched || estimate.CostMicrousd != 0 {
		t.Fatalf("estimate = %+v, want unmatched zero cost", estimate)
	}
	if estimate.Snapshot["matched"] != false || estimate.Snapshot["model"] != "unknown-model" {
		t.Fatalf("Snapshot = %+v, want unmatched unknown-model", estimate.Snapshot)
	}
}

func TestModelPolicyHelpersReturnDefaultAndAllowedStatus(t *testing.T) {
	repo := newMemoryRepo()
	service := NewService(repo, Config{SessionTTL: time.Hour})

	defaultModel, err := service.DefaultModel(context.Background())
	if err != nil {
		t.Fatalf("DefaultModel returned error: %v", err)
	}
	if defaultModel != "gpt-4.1" {
		t.Fatalf("DefaultModel = %q, want gpt-4.1", defaultModel)
	}

	allowed, err := service.IsModelAllowed(context.Background(), " gpt-4.1-mini ")
	if err != nil {
		t.Fatalf("IsModelAllowed returned error: %v", err)
	}
	if !allowed {
		t.Fatal("IsModelAllowed returned false for configured model")
	}

	allowed, err = service.IsModelAllowed(context.Background(), "gpt-5")
	if err != nil {
		t.Fatalf("IsModelAllowed returned error: %v", err)
	}
	if allowed {
		t.Fatal("IsModelAllowed returned true for unconfigured model")
	}
}

func TestDefaultModelRejectsInvalidStoredSettings(t *testing.T) {
	repo := newMemoryRepo()
	repo.modelSettings = ModelSettings{
		DefaultModel:  "gpt-5",
		AllowedModels: []string{"gpt-5-mini"},
	}
	service := NewService(repo, Config{SessionTTL: time.Hour})

	if _, err := service.DefaultModel(context.Background()); !errors.Is(err, ErrInvalidInput) {
		t.Fatalf("DefaultModel error = %v, want ErrInvalidInput", err)
	}
}

func buildModelNames(count int) []string {
	names := make([]string, 0, count)
	for i := range count {
		names = append(names, "model-"+strconv.Itoa(i))
	}
	return names
}

func requireBootstrap(t *testing.T, service *Service, username, password string) {
	t.Helper()

	if err := service.BootstrapAdmin(context.Background(), username, password); err != nil {
		t.Fatalf("BootstrapAdmin returned error: %v", err)
	}
}

type memoryRepo struct {
	admin            Admin
	nextAdminID      int64
	sessions         map[string]memorySession
	keys             map[int64]memoryAPIKey
	nextAPIKeyID     int64
	touchErr         error
	logs             []RequestLog
	lastLogFilter    RequestLogFilter
	usageSummary     UsageSummary
	lastUsageSince   time.Time
	lastUsageGroupBy string
	modelSettings    ModelSettings
	gatewaySettings  GatewaySettings
	usagePricing     UsagePricing
}

type memorySession struct {
	adminID   int64
	expiresAt time.Time
	revokedAt *time.Time
}

type memoryAPIKey struct {
	APIKey
	Hash string
}

func newMemoryRepo() *memoryRepo {
	return &memoryRepo{
		nextAdminID:  1,
		sessions:     map[string]memorySession{},
		keys:         map[int64]memoryAPIKey{},
		nextAPIKeyID: 1,
	}
}

func (r *memoryRepo) FindAdminByUsername(_ context.Context, username string) (Admin, error) {
	if r.admin.ID == 0 || r.admin.Username != username {
		return Admin{}, ErrNotFound
	}
	return r.admin, nil
}

func (r *memoryRepo) FindBootstrapAdmin(_ context.Context) (Admin, error) {
	if r.admin.ID == 0 {
		return Admin{}, ErrNotFound
	}
	return r.admin, nil
}

func (r *memoryRepo) CreateAdmin(_ context.Context, username, passwordHash string) (Admin, error) {
	r.admin = Admin{ID: r.nextAdminID, Username: username, PasswordHash: passwordHash}
	r.nextAdminID++
	return r.admin, nil
}

func (r *memoryRepo) UpdateAdminUsername(_ context.Context, id int64, username string) (Admin, error) {
	if r.admin.ID != id {
		return Admin{}, ErrNotFound
	}
	r.admin.Username = username
	return r.admin, nil
}

func (r *memoryRepo) CreateSession(_ context.Context, adminID int64, tokenHash string, expiresAt time.Time) error {
	r.sessions[tokenHash] = memorySession{adminID: adminID, expiresAt: expiresAt}
	return nil
}

func (r *memoryRepo) FindAdminBySessionHash(_ context.Context, tokenHash string, now time.Time) (Admin, error) {
	session, ok := r.sessions[tokenHash]
	if !ok || session.revokedAt != nil || !session.expiresAt.After(now) || r.admin.ID != session.adminID {
		return Admin{}, ErrNotFound
	}
	return r.admin, nil
}

func (r *memoryRepo) RevokeSession(_ context.Context, tokenHash string) error {
	session, ok := r.sessions[tokenHash]
	if !ok {
		return ErrNotFound
	}
	now := time.Now()
	session.revokedAt = &now
	r.sessions[tokenHash] = session
	return nil
}

func (r *memoryRepo) CreateAPIKey(_ context.Context, name, hash, prefix string) (APIKey, error) {
	key := APIKey{
		ID:          r.nextAPIKeyID,
		Name:        name,
		Prefix:      prefix,
		CreatedAt:   time.Now(),
		ModelPolicy: APIKeyModelPolicyAll,
	}
	r.nextAPIKeyID++
	r.keys[key.ID] = memoryAPIKey{APIKey: key, Hash: hash}
	return key, nil
}

func (r *memoryRepo) ListAPIKeys(_ context.Context) ([]APIKey, error) {
	keys := make([]APIKey, 0, len(r.keys))
	for _, key := range r.keys {
		keys = append(keys, key.APIKey)
	}
	slices.SortFunc(keys, func(a, b APIKey) int {
		return int(a.ID - b.ID)
	})
	return keys, nil
}

func (r *memoryRepo) RevokeAPIKey(_ context.Context, id int64) (APIKey, error) {
	key, ok := r.keys[id]
	if !ok {
		return APIKey{}, ErrNotFound
	}
	now := time.Now()
	key.RevokedAt = &now
	r.keys[id] = key
	return key.APIKey, nil
}

func (r *memoryRepo) UpdateAPIKeyModelPolicy(_ context.Context, id int64, policy string, models []string) (APIKey, error) {
	key, ok := r.keys[id]
	if !ok || key.RevokedAt != nil {
		return APIKey{}, ErrNotFound
	}
	key.ModelPolicy = policy
	key.AllowedModels = append([]string(nil), models...)
	r.keys[id] = key
	return key.APIKey, nil
}

func (r *memoryRepo) UpdateAPIKeyLimits(_ context.Context, id int64, requestsPerMinute, tokensPerMinute int) (APIKey, error) {
	key, ok := r.keys[id]
	if !ok || key.RevokedAt != nil {
		return APIKey{}, ErrNotFound
	}
	key.RequestsPerMinute = requestsPerMinute
	key.TokensPerMinute = tokensPerMinute
	r.keys[id] = key
	return key.APIKey, nil
}

func (r *memoryRepo) ListAPIKeyModels(_ context.Context, id int64) ([]string, error) {
	key, ok := r.keys[id]
	if !ok {
		return nil, ErrNotFound
	}
	return append([]string(nil), key.AllowedModels...), nil
}

func (r *memoryRepo) FindAPIKeyByHash(_ context.Context, hash string, _ time.Time) (APIKey, error) {
	for _, key := range r.keys {
		if key.Hash == hash && key.RevokedAt == nil {
			return key.APIKey, nil
		}
	}
	return APIKey{}, ErrNotFound
}

func (r *memoryRepo) TouchAPIKey(_ context.Context, id int64, usedAt time.Time) error {
	if r.touchErr != nil {
		return r.touchErr
	}
	key, ok := r.keys[id]
	if !ok {
		return ErrNotFound
	}
	key.LastUsedAt = &usedAt
	r.keys[id] = key
	return nil
}

func (r *memoryRepo) ListRequestLogs(_ context.Context, filter RequestLogFilter) ([]RequestLog, error) {
	r.lastLogFilter = filter
	limit := filter.Limit
	if limit > len(r.logs) {
		limit = len(r.logs)
	}
	return append([]RequestLog(nil), r.logs[:limit]...), nil
}

func (r *memoryRepo) GetUsageSummary(_ context.Context, since time.Time, groupBy string) (UsageSummary, error) {
	r.lastUsageSince = since
	r.lastUsageGroupBy = groupBy
	return r.usageSummary, nil
}

func (r *memoryRepo) GetModelSettings(_ context.Context) (ModelSettings, error) {
	if r.modelSettings.DefaultModel == "" {
		return ModelSettings{}, ErrNotFound
	}
	return r.modelSettings, nil
}

func (r *memoryRepo) SaveModelSettings(_ context.Context, settings ModelSettings) (ModelSettings, error) {
	r.modelSettings = settings
	return settings, nil
}

func (r *memoryRepo) GetGatewaySettings(_ context.Context) (GatewaySettings, error) {
	if r.gatewaySettings == (GatewaySettings{}) {
		return GatewaySettings{}, ErrNotFound
	}
	return r.gatewaySettings, nil
}

func (r *memoryRepo) SaveGatewaySettings(_ context.Context, settings GatewaySettings) (GatewaySettings, error) {
	r.gatewaySettings = settings
	return settings, nil
}

func (r *memoryRepo) GetUsagePricing(_ context.Context) (UsagePricing, error) {
	if r.usagePricing.Version == 0 {
		return UsagePricing{}, ErrNotFound
	}
	return r.usagePricing, nil
}

func (r *memoryRepo) SaveUsagePricing(_ context.Context, pricing UsagePricing) (UsagePricing, error) {
	r.usagePricing = pricing
	return pricing, nil
}

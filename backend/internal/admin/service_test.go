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

func TestSetAPIKeyDisabledBlocksAndRestoresAuthentication(t *testing.T) {
	repo := newMemoryRepo()
	service := NewService(repo, Config{SessionTTL: time.Hour})
	result, err := service.CreateAPIKey(context.Background(), "codex laptop")
	if err != nil {
		t.Fatalf("CreateAPIKey returned error: %v", err)
	}

	disabled, err := service.SetAPIKeyDisabled(context.Background(), result.Key.ID, true)
	if err != nil {
		t.Fatalf("SetAPIKeyDisabled true returned error: %v", err)
	}
	if disabled.DisabledAt == nil {
		t.Fatalf("DisabledAt = nil, want timestamp")
	}
	if _, err := service.AuthenticateAPIKey(context.Background(), result.Secret); !errors.Is(err, ErrUnauthorized) {
		t.Fatalf("AuthenticateAPIKey disabled error = %v, want ErrUnauthorized", err)
	}

	enabled, err := service.SetAPIKeyDisabled(context.Background(), result.Key.ID, false)
	if err != nil {
		t.Fatalf("SetAPIKeyDisabled false returned error: %v", err)
	}
	if enabled.DisabledAt != nil {
		t.Fatalf("DisabledAt = %v, want nil", enabled.DisabledAt)
	}
	if _, err := service.AuthenticateAPIKey(context.Background(), result.Secret); err != nil {
		t.Fatalf("AuthenticateAPIKey after enable returned error: %v", err)
	}
}

func TestUpdateAPIKeyBudgetsValidatesNonNegativeValues(t *testing.T) {
	repo := newMemoryRepo()
	service := NewService(repo, Config{})
	result, err := service.CreateAPIKey(context.Background(), "codex")
	if err != nil {
		t.Fatalf("CreateAPIKey returned error: %v", err)
	}

	updated, err := service.UpdateAPIKeyBudgets(context.Background(), result.Key.ID, 10, 1000, 300, 30000)
	if err != nil {
		t.Fatalf("UpdateAPIKeyBudgets returned error: %v", err)
	}
	if updated.RequestBudget24h != 10 || updated.TokenBudget24h != 1000 || updated.RequestBudget30d != 300 || updated.TokenBudget30d != 30000 {
		t.Fatalf("budgets = %+v, want configured values", updated)
	}

	if _, err := service.UpdateAPIKeyBudgets(context.Background(), result.Key.ID, -1, 0, 0, 0); !errors.Is(err, ErrInvalidInput) {
		t.Fatalf("negative requestBudget24h error = %v, want ErrInvalidInput", err)
	}
	if _, err := service.UpdateAPIKeyBudgets(context.Background(), result.Key.ID, 0, -1, 0, 0); !errors.Is(err, ErrInvalidInput) {
		t.Fatalf("negative tokenBudget24h error = %v, want ErrInvalidInput", err)
	}
	if _, err := service.UpdateAPIKeyBudgets(context.Background(), result.Key.ID, 0, 0, -1, 0); !errors.Is(err, ErrInvalidInput) {
		t.Fatalf("negative requestBudget30d error = %v, want ErrInvalidInput", err)
	}
	if _, err := service.UpdateAPIKeyBudgets(context.Background(), result.Key.ID, 0, 0, 0, -1); !errors.Is(err, ErrInvalidInput) {
		t.Fatalf("negative tokenBudget30d error = %v, want ErrInvalidInput", err)
	}
}

func TestRoutingPoolServiceValidatesNameAndMembership(t *testing.T) {
	repo := newMemoryRepo()
	service := NewService(repo, Config{})

	if _, err := service.CreateRoutingPool(context.Background(), " ", "", true, nil); !errors.Is(err, ErrInvalidInput) {
		t.Fatalf("CreateRoutingPool blank error = %v, want ErrInvalidInput", err)
	}

	pool, err := service.CreateRoutingPool(context.Background(), " codex primary ", " daily pool ", true, nil)
	if err != nil {
		t.Fatalf("CreateRoutingPool returned error: %v", err)
	}
	if pool.Name != "codex primary" || pool.Description != "daily pool" || !pool.Enabled {
		t.Fatalf("pool = %+v, want trimmed enabled pool", pool)
	}

	if _, err := service.ReplaceRoutingPoolAccounts(context.Background(), pool.ID, []RoutingPoolAccount{{AccountID: -1, Priority: 0}}); !errors.Is(err, ErrInvalidInput) {
		t.Fatalf("negative account id error = %v, want ErrInvalidInput", err)
	}
	if _, err := service.ReplaceRoutingPoolAccounts(context.Background(), pool.ID, []RoutingPoolAccount{{AccountID: 7, Priority: -1}}); !errors.Is(err, ErrInvalidInput) {
		t.Fatalf("negative priority error = %v, want ErrInvalidInput", err)
	}

	updated, err := service.ReplaceRoutingPoolAccounts(context.Background(), pool.ID, []RoutingPoolAccount{{AccountID: 7, Priority: 10}})
	if err != nil {
		t.Fatalf("ReplaceRoutingPoolAccounts returned error: %v", err)
	}
	if len(updated.Accounts) != 1 || updated.Accounts[0].AccountID != 7 || updated.Accounts[0].Priority != 10 {
		t.Fatalf("pool accounts = %+v, want account 7 priority 10", updated.Accounts)
	}
}

func TestRoutingPoolFallbackValidationRejectsSelfAndCycles(t *testing.T) {
	repo := newMemoryRepo()
	service := NewService(repo, Config{})

	primary, err := service.CreateRoutingPool(context.Background(), "primary", "", true, nil)
	if err != nil {
		t.Fatalf("CreateRoutingPool primary returned error: %v", err)
	}
	secondary, err := service.CreateRoutingPool(context.Background(), "secondary", "", true, nil)
	if err != nil {
		t.Fatalf("CreateRoutingPool secondary returned error: %v", err)
	}

	if _, err := service.UpdateRoutingPool(context.Background(), primary.ID, "primary", "", true, &primary.ID); !errors.Is(err, ErrInvalidInput) {
		t.Fatalf("self fallback error = %v, want ErrInvalidInput", err)
	}

	if _, err := service.UpdateRoutingPool(context.Background(), primary.ID, "primary", "", true, &secondary.ID); err != nil {
		t.Fatalf("primary fallback update returned error: %v", err)
	}
	if _, err := service.UpdateRoutingPool(context.Background(), secondary.ID, "secondary", "", true, &primary.ID); !errors.Is(err, ErrInvalidInput) {
		t.Fatalf("cycle fallback error = %v, want ErrInvalidInput", err)
	}
}

func TestRoutingPoolFallbackValidationRejectsMissingTarget(t *testing.T) {
	repo := newMemoryRepo()
	service := NewService(repo, Config{})

	pool, err := service.CreateRoutingPool(context.Background(), "primary", "", true, nil)
	if err != nil {
		t.Fatalf("CreateRoutingPool returned error: %v", err)
	}
	missing := int64(999)
	if _, err := service.UpdateRoutingPool(context.Background(), pool.ID, "primary", "", true, &missing); !errors.Is(err, ErrInvalidInput) {
		t.Fatalf("missing fallback error = %v, want ErrInvalidInput", err)
	}
}

func TestAPIKeyBudgetUsageComputesRemainingAndExceeded(t *testing.T) {
	repo := newMemoryRepo()
	service := NewService(repo, Config{})
	key := APIKey{
		ID:               42,
		RequestBudget24h: 3,
		TokenBudget24h:   80,
		RequestBudget30d: 10,
		TokenBudget30d:   100,
	}
	repo.budgetUsage[key.ID] = APIKeyBudgetUsage{
		KeyID:           key.ID,
		RequestsUsed24h: 3,
		TokensUsed24h:   70,
		RequestsUsed30d: 8,
		TokensUsed30d:   120,
	}

	usage, err := service.GetAPIKeyBudgetUsage(context.Background(), key, time.Unix(5000, 0).UTC())
	if err != nil {
		t.Fatalf("GetAPIKeyBudgetUsage returned error: %v", err)
	}
	if usage.RequestsRemaining24h == nil || *usage.RequestsRemaining24h != 0 {
		t.Fatalf("RequestsRemaining24h = %v, want 0", usage.RequestsRemaining24h)
	}
	if usage.TokensRemaining24h == nil || *usage.TokensRemaining24h != 10 {
		t.Fatalf("TokensRemaining24h = %v, want 10", usage.TokensRemaining24h)
	}
	if usage.RequestsRemaining30d == nil || *usage.RequestsRemaining30d != 2 {
		t.Fatalf("RequestsRemaining30d = %v, want 2", usage.RequestsRemaining30d)
	}
	if usage.TokensRemaining30d == nil || *usage.TokensRemaining30d != 0 {
		t.Fatalf("TokensRemaining30d = %v, want 0", usage.TokensRemaining30d)
	}
	if !usage.RequestBudgetExceeded || !usage.TokenBudgetExceeded {
		t.Fatalf("budget exceeded flags = request:%v token:%v, want both true", usage.RequestBudgetExceeded, usage.TokenBudgetExceeded)
	}

	uncapped, err := service.GetAPIKeyBudgetUsage(context.Background(), APIKey{ID: 43}, time.Unix(5000, 0).UTC())
	if err != nil {
		t.Fatalf("GetAPIKeyBudgetUsage uncapped returned error: %v", err)
	}
	if uncapped.RequestsRemaining24h != nil || uncapped.TokensRemaining30d != nil || uncapped.RequestBudgetExceeded || uncapped.TokenBudgetExceeded {
		t.Fatalf("uncapped usage = %+v, want nil remaining and no exceeded flags", uncapped)
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

func TestUpdateAPIKeyNameTrimsAndPersistsName(t *testing.T) {
	repo := newMemoryRepo()
	service := NewService(repo, Config{SessionTTL: time.Hour})
	result, err := service.CreateAPIKey(context.Background(), "codex laptop")
	if err != nil {
		t.Fatalf("CreateAPIKey returned error: %v", err)
	}

	updated, err := service.UpdateAPIKeyName(context.Background(), result.Key.ID, " renamed workstation ")
	if err != nil {
		t.Fatalf("UpdateAPIKeyName returned error: %v", err)
	}
	if updated.Name != "renamed workstation" {
		t.Fatalf("Name = %q, want trimmed rename", updated.Name)
	}

	keys, err := service.ListAPIKeys(context.Background())
	if err != nil {
		t.Fatalf("ListAPIKeys returned error: %v", err)
	}
	if len(keys) != 1 || keys[0].Name != "renamed workstation" {
		t.Fatalf("keys = %+v, want renamed key", keys)
	}
}

func TestUpdateAPIKeyNameRejectsInvalidName(t *testing.T) {
	repo := newMemoryRepo()
	service := NewService(repo, Config{SessionTTL: time.Hour})

	if _, err := service.UpdateAPIKeyName(context.Background(), 7, " \t "); !errors.Is(err, ErrInvalidInput) {
		t.Fatalf("UpdateAPIKeyName error = %v, want ErrInvalidInput", err)
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
	since := time.Unix(2000, 0).UTC()

	logs, err := service.ListRequestLogs(context.Background(), RequestLogFilter{
		RequestID:         " req_3 ",
		Query:             "  gpt-5  ",
		StatusClass:       "server_error",
		StatusCode:        503,
		ProviderAccountID: 7,
		ClientKeyID:       12,
		Model:             " gpt-5 ",
		SessionID:         " workspace-123 ",
		Error:             " api_key_token_rate_limited ",
		RoutingPoolError:  " routing_pool_unavailable ",
		RoutingPoolChain:  " primary -> secondary ",
		GatewayFallbacks:  true,
		Since:             since,
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
	if repo.lastLogFilter.RequestID != "req_3" {
		t.Fatalf("repository request ID = %q, want req_3", repo.lastLogFilter.RequestID)
	}
	if repo.lastLogFilter.StatusClass != RequestLogStatusServerError {
		t.Fatalf("repository status class = %q, want %q", repo.lastLogFilter.StatusClass, RequestLogStatusServerError)
	}
	if repo.lastLogFilter.StatusCode != 503 {
		t.Fatalf("repository status code = %d, want 503", repo.lastLogFilter.StatusCode)
	}
	if repo.lastLogFilter.ProviderAccountID != 7 {
		t.Fatalf("repository provider account ID = %d, want 7", repo.lastLogFilter.ProviderAccountID)
	}
	if repo.lastLogFilter.ClientKeyID != 12 {
		t.Fatalf("repository client key ID = %d, want 12", repo.lastLogFilter.ClientKeyID)
	}
	if repo.lastLogFilter.Model != "gpt-5" || repo.lastLogFilter.SessionID != "workspace-123" {
		t.Fatalf("repository model/session = %q/%q, want gpt-5/workspace-123", repo.lastLogFilter.Model, repo.lastLogFilter.SessionID)
	}
	if repo.lastLogFilter.Error != "api_key_token_rate_limited" {
		t.Fatalf("repository error = %q, want api_key_token_rate_limited", repo.lastLogFilter.Error)
	}
	if repo.lastLogFilter.RoutingPoolError != "routing_pool_unavailable" {
		t.Fatalf("repository routing pool error = %q, want routing_pool_unavailable", repo.lastLogFilter.RoutingPoolError)
	}
	if repo.lastLogFilter.RoutingPoolChain != "primary -> secondary" {
		t.Fatalf("repository routing pool chain = %q, want primary -> secondary", repo.lastLogFilter.RoutingPoolChain)
	}
	if !repo.lastLogFilter.GatewayFallbacks {
		t.Fatal("repository gateway fallback filter = false, want true")
	}
	if !repo.lastLogFilter.Since.Equal(since) {
		t.Fatalf("repository since = %s, want %s", repo.lastLogFilter.Since, since)
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
	if _, err := service.ListRequestLogs(context.Background(), RequestLogFilter{RequestID: strings.Repeat("x", 101)}); !errors.Is(err, ErrInvalidInput) {
		t.Fatalf("ListRequestLogs long request ID error = %v, want ErrInvalidInput", err)
	}
	if _, err := service.ListRequestLogs(context.Background(), RequestLogFilter{ProviderAccountID: -1}); !errors.Is(err, ErrInvalidInput) {
		t.Fatalf("ListRequestLogs invalid provider account ID error = %v, want ErrInvalidInput", err)
	}
	if _, err := service.ListRequestLogs(context.Background(), RequestLogFilter{RoutingPoolID: -1}); !errors.Is(err, ErrInvalidInput) {
		t.Fatalf("ListRequestLogs invalid routing pool ID error = %v, want ErrInvalidInput", err)
	}
	if _, err := service.ListRequestLogs(context.Background(), RequestLogFilter{ClientKeyID: -1}); !errors.Is(err, ErrInvalidInput) {
		t.Fatalf("ListRequestLogs invalid client key ID error = %v, want ErrInvalidInput", err)
	}
	if _, err := service.ListRequestLogs(context.Background(), RequestLogFilter{StatusCode: 99}); !errors.Is(err, ErrInvalidInput) {
		t.Fatalf("ListRequestLogs invalid low status code error = %v, want ErrInvalidInput", err)
	}
	if _, err := service.ListRequestLogs(context.Background(), RequestLogFilter{StatusCode: 600}); !errors.Is(err, ErrInvalidInput) {
		t.Fatalf("ListRequestLogs invalid high status code error = %v, want ErrInvalidInput", err)
	}
	if _, err := service.ListRequestLogs(context.Background(), RequestLogFilter{Model: strings.Repeat("x", 101)}); !errors.Is(err, ErrInvalidInput) {
		t.Fatalf("ListRequestLogs long model error = %v, want ErrInvalidInput", err)
	}
	if _, err := service.ListRequestLogs(context.Background(), RequestLogFilter{SessionID: strings.Repeat("x", 101)}); !errors.Is(err, ErrInvalidInput) {
		t.Fatalf("ListRequestLogs long session error = %v, want ErrInvalidInput", err)
	}
	if _, err := service.ListRequestLogs(context.Background(), RequestLogFilter{Error: strings.Repeat("x", 101)}); !errors.Is(err, ErrInvalidInput) {
		t.Fatalf("ListRequestLogs long error code error = %v, want ErrInvalidInput", err)
	}
	if _, err := service.ListRequestLogs(context.Background(), RequestLogFilter{RoutingPoolError: strings.Repeat("x", 101)}); !errors.Is(err, ErrInvalidInput) {
		t.Fatalf("ListRequestLogs long routing pool error = %v, want ErrInvalidInput", err)
	}
	if _, err := service.ListRequestLogs(context.Background(), RequestLogFilter{RoutingPoolChain: strings.Repeat("x", 201)}); !errors.Is(err, ErrInvalidInput) {
		t.Fatalf("ListRequestLogs long routing pool chain error = %v, want ErrInvalidInput", err)
	}
	if _, err := service.ListRequestLogs(context.Background(), RequestLogFilter{UsageSource: strings.Repeat("x", 101)}); !errors.Is(err, ErrInvalidInput) {
		t.Fatalf("ListRequestLogs long usage source error = %v, want ErrInvalidInput", err)
	}

	if _, err := service.ListRequestLogs(context.Background(), RequestLogFilter{UsageSource: " stream "}); err != nil {
		t.Fatalf("ListRequestLogs usage source filter returned error: %v", err)
	}
	if repo.lastLogFilter.UsageSource != "stream" {
		t.Fatalf("repository usage source filter = %q, want stream", repo.lastLogFilter.UsageSource)
	}
}

func TestGetUsageSummaryValidatesRangeAndGroup(t *testing.T) {
	repo := newMemoryRepo()
	service := NewService(repo, Config{SessionTTL: time.Hour})
	repo.usageSummary = UsageSummary{
		Rows: []UsageSummaryRow{{ID: "gpt-5", Label: "gpt-5", Requests: 2, InputTokens: 30, OutputTokens: 10, TotalTokens: 40, CachedInputTokens: 12, ReasoningTokens: 4}},
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
	if summary.TotalCachedInputTokens != 12 || summary.TotalReasoningTokens != 4 {
		t.Fatalf("summary cached/reasoning totals = %d/%d, want 12/4", summary.TotalCachedInputTokens, summary.TotalReasoningTokens)
	}
	if _, err := service.GetUsageSummary(context.Background(), "7d", "session"); err != nil {
		t.Fatalf("GetUsageSummary session returned error: %v", err)
	}
	if repo.lastUsageGroupBy != "session" {
		t.Fatalf("repository group = %q, want session", repo.lastUsageGroupBy)
	}
	if _, err := service.GetUsageSummary(context.Background(), "7d", "routing_pool"); err != nil {
		t.Fatalf("GetUsageSummary routing_pool returned error: %v", err)
	}
	if repo.lastUsageGroupBy != "routing_pool" {
		t.Fatalf("repository group = %q, want routing_pool", repo.lastUsageGroupBy)
	}
	if _, err := service.GetUsageSummary(context.Background(), "7d", "routing_pool_chain"); err != nil {
		t.Fatalf("GetUsageSummary routing_pool_chain returned error: %v", err)
	}
	if repo.lastUsageGroupBy != "routing_pool_chain" {
		t.Fatalf("repository group = %q, want routing_pool_chain", repo.lastUsageGroupBy)
	}
	if _, err := service.GetUsageSummary(context.Background(), "7d", "usage_source"); err != nil {
		t.Fatalf("GetUsageSummary usage_source returned error: %v", err)
	}
	if repo.lastUsageGroupBy != "usage_source" {
		t.Fatalf("repository group = %q, want usage_source", repo.lastUsageGroupBy)
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

func TestGetOpsAccountHealthReturnsRepositorySummary(t *testing.T) {
	repo := newMemoryRepo()
	service := NewService(repo, Config{SessionTTL: time.Hour})
	now := time.Unix(5000, 0).UTC()
	since := now.Add(-24 * time.Hour)
	repo.opsAccountHealth = OpsAccountHealth{
		WindowStart:       since,
		WindowEnd:         now,
		TotalAccounts:     5,
		EnabledAccounts:   4,
		Schedulable:       3,
		Disabled:          1,
		RateLimited:       1,
		CircuitOpen:       1,
		Expired:           1,
		TestedAccounts:    4,
		TestPassed:        3,
		TestFailed:        1,
		TestMissing:       1,
		RecentTestFailure: 1,
	}

	health, err := service.GetOpsAccountHealth(context.Background(), since)
	if err != nil {
		t.Fatalf("GetOpsAccountHealth returned error: %v", err)
	}
	if health != repo.opsAccountHealth {
		t.Fatalf("GetOpsAccountHealth = %+v, want %+v", health, repo.opsAccountHealth)
	}
	if !repo.lastOpsAccountHealthSince.Equal(since) {
		t.Fatalf("repository since = %v, want %v", repo.lastOpsAccountHealthSince, since)
	}
}

func TestListOpsAccountTestsReturnsRepositoryRows(t *testing.T) {
	repo := newMemoryRepo()
	service := NewService(repo, Config{SessionTTL: time.Hour})
	since := time.Unix(5000, 0).UTC().Add(-24 * time.Hour)
	checkedAt := time.Unix(5000, 0).UTC()
	repo.opsAccountTests = []OpsAccountTest{{
		ID:          91,
		AccountID:   7,
		Provider:    "openai",
		AccountName: "Work Codex",
		AccountType: "codex_oauth",
		Status:      "failed",
		Message:     "quota exceeded",
		CheckedAt:   checkedAt,
		CreatedAt:   checkedAt,
	}}

	tests, err := service.ListOpsAccountTests(context.Background(), since, 20)
	if err != nil {
		t.Fatalf("ListOpsAccountTests returned error: %v", err)
	}
	if len(tests) != 1 || tests[0] != repo.opsAccountTests[0] {
		t.Fatalf("ops account tests = %+v, want %+v", tests, repo.opsAccountTests)
	}
	if !repo.lastOpsAccountTestsSince.Equal(since) || repo.lastOpsAccountTestsLimit != 20 {
		t.Fatalf("repository args = since:%v limit:%d", repo.lastOpsAccountTestsSince, repo.lastOpsAccountTestsLimit)
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

func TestFingerprintProfileInputNormalizesValidProfile(t *testing.T) {
	repo := newMemoryRepo()
	service := NewService(repo, Config{SessionTTL: time.Hour})

	created, err := service.CreateFingerprintProfile(context.Background(), FingerprintProfileInput{
		Name:           "  Chrome desktop  ",
		Description:    " browser profile ",
		UserAgent:      "  Mozilla/5.0  ",
		TLSFingerprint: "HelloChrome",
		Headers: map[string]string{
			" x-client-version ": "  desktop  ",
		},
		Enabled: true,
	})
	if err != nil {
		t.Fatalf("CreateFingerprintProfile returned error: %v", err)
	}
	if created.Name != "Chrome desktop" || created.Description != "browser profile" || created.UserAgent != "Mozilla/5.0" || created.TLSFingerprint != "chrome" || created.Headers["X-Client-Version"] != "desktop" {
		t.Fatalf("created profile = %+v, want normalized fields", created)
	}
	if repo.lastFingerprintInput.Name != created.Name || repo.lastFingerprintInput.TLSFingerprint != "chrome" || repo.lastFingerprintInput.Headers["X-Client-Version"] != "desktop" {
		t.Fatalf("repo input = %+v, want normalized profile input", repo.lastFingerprintInput)
	}
}

func TestFingerprintProfileInputRejectsInvalidTLSFingerprint(t *testing.T) {
	service := NewService(newMemoryRepo(), Config{SessionTTL: time.Hour})

	if _, err := service.CreateFingerprintProfile(context.Background(), FingerprintProfileInput{Name: "bad", TLSFingerprint: "netscape"}); !errors.Is(err, ErrInvalidInput) {
		t.Fatalf("CreateFingerprintProfile error = %v, want ErrInvalidInput", err)
	}
}

func TestFingerprintProfileInputRejectsInvalidHeaders(t *testing.T) {
	service := NewService(newMemoryRepo(), Config{SessionTTL: time.Hour})

	for _, input := range []FingerprintProfileInput{
		{Name: "bad header name", Headers: map[string]string{"Bad Header": "value"}},
		{Name: "bad header value", Headers: map[string]string{"X-Test": "line\r\nbreak"}},
	} {
		if _, err := service.UpdateFingerprintProfile(context.Background(), 7, input); !errors.Is(err, ErrInvalidInput) {
			t.Fatalf("UpdateFingerprintProfile(%q) error = %v, want ErrInvalidInput", input.Name, err)
		}
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
	admin                     Admin
	nextAdminID               int64
	sessions                  map[string]memorySession
	keys                      map[int64]memoryAPIKey
	nextAPIKeyID              int64
	touchErr                  error
	logs                      []RequestLog
	lastLogFilter             RequestLogFilter
	budgetUsage               map[int64]APIKeyBudgetUsage
	routingPools              map[int64]RoutingPool
	usageSummary              UsageSummary
	lastUsageSince            time.Time
	lastUsageGroupBy          string
	modelSettings             ModelSettings
	gatewaySettings           GatewaySettings
	usagePricing              UsagePricing
	opsAccountHealth          OpsAccountHealth
	lastOpsAccountHealthSince time.Time
	opsAccountTests           []OpsAccountTest
	lastOpsAccountTestsSince  time.Time
	lastOpsAccountTestsLimit  int
	lastFingerprintInput      FingerprintProfileInput
	lastFingerprintUpdateID   int64
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
		budgetUsage:  map[int64]APIKeyBudgetUsage{},
		routingPools: map[int64]RoutingPool{},
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

func (r *memoryRepo) UpdateAPIKeyName(_ context.Context, id int64, name string) (APIKey, error) {
	key, ok := r.keys[id]
	if !ok || key.RevokedAt != nil {
		return APIKey{}, ErrNotFound
	}
	key.Name = name
	r.keys[id] = key
	return key.APIKey, nil
}

func (r *memoryRepo) SetAPIKeyDisabled(_ context.Context, id int64, disabled bool) (APIKey, error) {
	key, ok := r.keys[id]
	if !ok || key.RevokedAt != nil {
		return APIKey{}, ErrNotFound
	}
	if disabled {
		now := time.Now()
		key.DisabledAt = &now
	} else {
		key.DisabledAt = nil
	}
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

func (r *memoryRepo) UpdateAPIKeyBudgets(_ context.Context, id int64, requestBudget24h, tokenBudget24h, requestBudget30d, tokenBudget30d int) (APIKey, error) {
	key, ok := r.keys[id]
	if !ok || key.RevokedAt != nil {
		return APIKey{}, ErrNotFound
	}
	key.RequestBudget24h = requestBudget24h
	key.TokenBudget24h = tokenBudget24h
	key.RequestBudget30d = requestBudget30d
	key.TokenBudget30d = tokenBudget30d
	r.keys[id] = key
	return key.APIKey, nil
}

func (r *memoryRepo) ListRoutingPools(_ context.Context) ([]RoutingPool, error) {
	pools := make([]RoutingPool, 0, len(r.routingPools))
	for _, pool := range r.routingPools {
		pools = append(pools, pool)
	}
	return pools, nil
}

func (r *memoryRepo) CreateRoutingPool(_ context.Context, name, description string, enabled bool, fallbackPoolID *int64) (RoutingPool, error) {
	if err := r.validateRoutingPoolFallback(0, fallbackPoolID); err != nil {
		return RoutingPool{}, err
	}
	r.nextAPIKeyID++
	pool := RoutingPool{ID: r.nextAPIKeyID, Name: name, Description: description, Enabled: enabled, FallbackPoolID: fallbackPoolID}
	if fallbackPoolID != nil {
		pool.FallbackPoolName = r.routingPools[*fallbackPoolID].Name
	}
	r.routingPools[pool.ID] = pool
	return pool, nil
}

func (r *memoryRepo) UpdateRoutingPool(_ context.Context, id int64, name, description string, enabled bool, fallbackPoolID *int64) (RoutingPool, error) {
	pool, ok := r.routingPools[id]
	if !ok {
		return RoutingPool{}, ErrNotFound
	}
	if err := r.validateRoutingPoolFallback(id, fallbackPoolID); err != nil {
		return RoutingPool{}, err
	}
	pool.Name = name
	pool.Description = description
	pool.Enabled = enabled
	pool.FallbackPoolID = fallbackPoolID
	pool.FallbackPoolName = ""
	if fallbackPoolID != nil {
		pool.FallbackPoolName = r.routingPools[*fallbackPoolID].Name
	}
	r.routingPools[id] = pool
	return pool, nil
}

func (r *memoryRepo) validateRoutingPoolFallback(poolID int64, fallbackPoolID *int64) error {
	if fallbackPoolID == nil {
		return nil
	}
	if *fallbackPoolID == poolID {
		return ErrInvalidInput
	}
	if _, ok := r.routingPools[*fallbackPoolID]; !ok {
		return ErrInvalidInput
	}
	seen := map[int64]struct{}{}
	if poolID > 0 {
		seen[poolID] = struct{}{}
	}
	for currentID := *fallbackPoolID; currentID > 0; {
		if _, ok := seen[currentID]; ok {
			return ErrInvalidInput
		}
		seen[currentID] = struct{}{}
		current := r.routingPools[currentID]
		if current.FallbackPoolID == nil {
			return nil
		}
		if _, ok := r.routingPools[*current.FallbackPoolID]; !ok {
			return ErrInvalidInput
		}
		currentID = *current.FallbackPoolID
	}
	return nil
}

func (r *memoryRepo) DeleteRoutingPool(_ context.Context, id int64) error {
	if _, ok := r.routingPools[id]; !ok {
		return ErrNotFound
	}
	delete(r.routingPools, id)
	return nil
}

func (r *memoryRepo) ReplaceRoutingPoolAccounts(_ context.Context, id int64, accounts []RoutingPoolAccount) (RoutingPool, error) {
	pool, ok := r.routingPools[id]
	if !ok {
		return RoutingPool{}, ErrNotFound
	}
	pool.Accounts = append([]RoutingPoolAccount(nil), accounts...)
	pool.AccountIDs = make([]int64, 0, len(accounts))
	for _, account := range accounts {
		pool.AccountIDs = append(pool.AccountIDs, account.AccountID)
	}
	r.routingPools[id] = pool
	return pool, nil
}

func (r *memoryRepo) UpdateAPIKeyRoutingPool(_ context.Context, id int64, routingPoolID *int64) (APIKey, error) {
	key, ok := r.keys[id]
	if !ok || key.RevokedAt != nil {
		return APIKey{}, ErrNotFound
	}
	key.RoutingPoolID = routingPoolID
	key.RoutingPoolName = ""
	if routingPoolID != nil {
		pool, ok := r.routingPools[*routingPoolID]
		if !ok {
			return APIKey{}, ErrNotFound
		}
		key.RoutingPoolName = pool.Name
	}
	r.keys[id] = key
	return key.APIKey, nil
}

func (r *memoryRepo) GetAPIKeyBudgetUsage(_ context.Context, keyID int64, _ time.Time) (APIKeyBudgetUsage, error) {
	usage := r.budgetUsage[keyID]
	usage.KeyID = keyID
	return usage, nil
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
		if key.Hash == hash && key.RevokedAt == nil && key.DisabledAt == nil {
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

func (r *memoryRepo) GetOpsErrorStats(_ context.Context, _ time.Time) (OpsErrorStats, error) {
	return OpsErrorStats{}, nil
}

func (r *memoryRepo) GetOpsThroughputTrend(_ context.Context, _ time.Time, _ string) (OpsThroughputTrend, error) {
	return OpsThroughputTrend{}, nil
}

func (r *memoryRepo) GetOpsErrorTrend(_ context.Context, _ time.Time, _ string) (OpsErrorTrend, error) {
	return OpsErrorTrend{}, nil
}

func (r *memoryRepo) GetOpsLatencyDistribution(_ context.Context, _ time.Time) (OpsLatencyDistribution, error) {
	return OpsLatencyDistribution{}, nil
}

func (r *memoryRepo) GetOpsAccountHealth(_ context.Context, since time.Time) (OpsAccountHealth, error) {
	r.lastOpsAccountHealthSince = since
	return r.opsAccountHealth, nil
}

func (r *memoryRepo) ListOpsAccountTests(_ context.Context, since time.Time, limit int) ([]OpsAccountTest, error) {
	r.lastOpsAccountTestsSince = since
	r.lastOpsAccountTestsLimit = limit
	return r.opsAccountTests, nil
}

func (r *memoryRepo) ListFingerprintProfiles(_ context.Context) ([]FingerprintProfile, error) {
	return nil, nil
}

func (r *memoryRepo) CreateFingerprintProfile(_ context.Context, input FingerprintProfileInput) (FingerprintProfile, error) {
	r.lastFingerprintInput = input
	return FingerprintProfile{ID: 1, Name: input.Name, Description: input.Description, UserAgent: input.UserAgent, TLSFingerprint: input.TLSFingerprint, Headers: input.Headers, Enabled: input.Enabled}, nil
}

func (r *memoryRepo) UpdateFingerprintProfile(_ context.Context, id int64, input FingerprintProfileInput) (FingerprintProfile, error) {
	r.lastFingerprintUpdateID = id
	r.lastFingerprintInput = input
	return FingerprintProfile{ID: id, Name: input.Name, Description: input.Description, UserAgent: input.UserAgent, TLSFingerprint: input.TLSFingerprint, Headers: input.Headers, Enabled: input.Enabled}, nil
}

func (r *memoryRepo) DeleteFingerprintProfile(_ context.Context, _ int64) error {
	return nil
}

func (r *memoryRepo) ListErrorPassthroughRules(_ context.Context) ([]ErrorPassthroughRule, error) {
	return nil, nil
}

func (r *memoryRepo) CreateErrorPassthroughRule(_ context.Context, _ ErrorPassthroughRuleInput) (ErrorPassthroughRule, error) {
	return ErrorPassthroughRule{}, nil
}

func (r *memoryRepo) UpdateErrorPassthroughRule(_ context.Context, _ int64, _ ErrorPassthroughRuleInput) (ErrorPassthroughRule, error) {
	return ErrorPassthroughRule{}, nil
}

func (r *memoryRepo) DeleteErrorPassthroughRule(_ context.Context, _ int64) error {
	return nil
}

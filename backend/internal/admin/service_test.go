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

func TestCreateAPIKeyStoresRetrievableEncryptedSecretAndAuthenticateRejectsRevoked(t *testing.T) {
	repo := newMemoryRepo()
	service := NewService(repo, Config{SessionTTL: time.Hour, EncryptionSecret: "test-encryption-secret"})
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
	if repo.keys[result.Key.ID].EncryptedSecret == "" || strings.Contains(repo.keys[result.Key.ID].EncryptedSecret, result.Secret) {
		t.Fatalf("encrypted secret = %q, want encrypted non-plaintext value", repo.keys[result.Key.ID].EncryptedSecret)
	}
	revealed, err := service.GetAPIKeySecret(context.Background(), result.Key.ID)
	if err != nil {
		t.Fatalf("GetAPIKeySecret returned error: %v", err)
	}
	if revealed != result.Secret {
		t.Fatalf("GetAPIKeySecret = %q, want created secret", revealed)
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
	if _, err := service.GetAPIKeySecret(context.Background(), result.Key.ID); !errors.Is(err, ErrNotFound) {
		t.Fatalf("GetAPIKeySecret revoked error = %v, want ErrNotFound", err)
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

	updated, err := service.UpdateAPIKeyBudgets(context.Background(), result.Key.ID, 10, 1000, 1_500_000, 300, 30000, 9_000_000)
	if err != nil {
		t.Fatalf("UpdateAPIKeyBudgets returned error: %v", err)
	}
	if updated.RequestBudget24h != 10 || updated.TokenBudget24h != 1000 || updated.CostBudgetMicrousd24h != 1_500_000 || updated.RequestBudget30d != 300 || updated.TokenBudget30d != 30000 || updated.CostBudgetMicrousd30d != 9_000_000 {
		t.Fatalf("budgets = %+v, want configured values", updated)
	}

	if _, err := service.UpdateAPIKeyBudgets(context.Background(), result.Key.ID, -1, 0, 0, 0, 0, 0); !errors.Is(err, ErrInvalidInput) {
		t.Fatalf("negative requestBudget24h error = %v, want ErrInvalidInput", err)
	}
	if _, err := service.UpdateAPIKeyBudgets(context.Background(), result.Key.ID, 0, -1, 0, 0, 0, 0); !errors.Is(err, ErrInvalidInput) {
		t.Fatalf("negative tokenBudget24h error = %v, want ErrInvalidInput", err)
	}
	if _, err := service.UpdateAPIKeyBudgets(context.Background(), result.Key.ID, 0, 0, -1, 0, 0, 0); !errors.Is(err, ErrInvalidInput) {
		t.Fatalf("negative costBudgetMicrousd24h error = %v, want ErrInvalidInput", err)
	}
	if _, err := service.UpdateAPIKeyBudgets(context.Background(), result.Key.ID, 0, 0, 0, -1, 0, 0); !errors.Is(err, ErrInvalidInput) {
		t.Fatalf("negative requestBudget30d error = %v, want ErrInvalidInput", err)
	}
	if _, err := service.UpdateAPIKeyBudgets(context.Background(), result.Key.ID, 0, 0, 0, 0, -1, 0); !errors.Is(err, ErrInvalidInput) {
		t.Fatalf("negative tokenBudget30d error = %v, want ErrInvalidInput", err)
	}
	if _, err := service.UpdateAPIKeyBudgets(context.Background(), result.Key.ID, 0, 0, 0, 0, 0, -1); !errors.Is(err, ErrInvalidInput) {
		t.Fatalf("negative costBudgetMicrousd30d error = %v, want ErrInvalidInput", err)
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
		ID:                    42,
		RequestBudget24h:      3,
		TokenBudget24h:        80,
		CostBudgetMicrousd24h: 2000,
		RequestBudget30d:      10,
		TokenBudget30d:        100,
		CostBudgetMicrousd30d: 3000,
	}
	repo.budgetUsage[key.ID] = APIKeyBudgetUsage{
		KeyID:           key.ID,
		RequestsUsed24h: 3,
		TokensUsed24h:   70,
		CostMicrousd24h: 1500,
		RequestsUsed30d: 8,
		TokensUsed30d:   120,
		CostMicrousd30d: 3500,
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
	if usage.CostRemainingMicrousd24h == nil || *usage.CostRemainingMicrousd24h != 500 {
		t.Fatalf("CostRemainingMicrousd24h = %v, want 500", usage.CostRemainingMicrousd24h)
	}
	if usage.RequestsRemaining30d == nil || *usage.RequestsRemaining30d != 2 {
		t.Fatalf("RequestsRemaining30d = %v, want 2", usage.RequestsRemaining30d)
	}
	if usage.TokensRemaining30d == nil || *usage.TokensRemaining30d != 0 {
		t.Fatalf("TokensRemaining30d = %v, want 0", usage.TokensRemaining30d)
	}
	if usage.CostRemainingMicrousd30d == nil || *usage.CostRemainingMicrousd30d != 0 {
		t.Fatalf("CostRemainingMicrousd30d = %v, want 0", usage.CostRemainingMicrousd30d)
	}
	if !usage.RequestBudgetExceeded || !usage.TokenBudgetExceeded || !usage.CostBudgetExceeded {
		t.Fatalf("budget exceeded flags = request:%v token:%v cost:%v, want all true", usage.RequestBudgetExceeded, usage.TokenBudgetExceeded, usage.CostBudgetExceeded)
	}

	uncapped, err := service.GetAPIKeyBudgetUsage(context.Background(), APIKey{ID: 43}, time.Unix(5000, 0).UTC())
	if err != nil {
		t.Fatalf("GetAPIKeyBudgetUsage uncapped returned error: %v", err)
	}
	if uncapped.RequestsRemaining24h != nil || uncapped.TokensRemaining30d != nil || uncapped.CostRemainingMicrousd30d != nil || uncapped.RequestBudgetExceeded || uncapped.TokenBudgetExceeded || uncapped.CostBudgetExceeded {
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

func TestListAPIKeysPurgesRevokedKeysPastRetention(t *testing.T) {
	repo := newMemoryRepo()
	service := NewService(repo, Config{SessionTTL: time.Hour})
	now := time.Now().UTC()
	oldRevokedAt := now.Add(-31 * 24 * time.Hour)
	recentRevokedAt := now.Add(-2 * 24 * time.Hour)
	repo.keys[1] = memoryAPIKey{APIKey: APIKey{
		ID:        1,
		Name:      "old deleted",
		Prefix:    "n2_old",
		CreatedAt: now.Add(-60 * 24 * time.Hour),
		RevokedAt: &oldRevokedAt,
	}}
	repo.keys[2] = memoryAPIKey{APIKey: APIKey{
		ID:        2,
		Name:      "recent deleted",
		Prefix:    "n2_recent",
		CreatedAt: now.Add(-2 * 24 * time.Hour),
		RevokedAt: &recentRevokedAt,
	}}
	repo.keys[3] = memoryAPIKey{APIKey: APIKey{
		ID:        3,
		Name:      "active",
		Prefix:    "n2_active",
		CreatedAt: now.Add(-time.Hour),
	}}

	keys, err := service.ListAPIKeys(context.Background())
	if err != nil {
		t.Fatalf("ListAPIKeys returned error: %v", err)
	}

	for _, key := range keys {
		if key.ID == 1 {
			t.Fatalf("old deleted key remained in ListAPIKeys result: %+v", keys)
		}
	}
	if _, ok := repo.keys[1]; ok {
		t.Fatalf("old deleted key remained in repository after purge")
	}
	if _, ok := repo.keys[2]; !ok {
		t.Fatalf("recent deleted key was purged")
	}
	if _, ok := repo.keys[3]; !ok {
		t.Fatalf("active key was purged")
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
			RequestLogRetentionDays:                14,
		},
	})

	settings, err := service.GetGatewaySettings(context.Background())
	if err != nil {
		t.Fatalf("GetGatewaySettings returned error: %v", err)
	}
	wantDefault := GatewaySettings{
		ProviderAccountAutoTestEnabled:         true,
		ProviderAccountAutoTestIntervalSeconds: 120,
		RequestLogRetentionDays:                14,
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
		RequestLogRetentionDays:                30,
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
		saved.ProviderAccountAutoTestIntervalSeconds != 120 ||
		saved.RequestLogRetentionDays != 30 {
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
	if _, err := service.UpdateGatewaySettings(context.Background(), GatewaySettings{RequestLogRetentionDays: -1}); !errors.Is(err, ErrInvalidInput) {
		t.Fatalf("UpdateGatewaySettings negative request log retention error = %v, want ErrInvalidInput", err)
	}
}

func TestCleanupRequestLogsUsesRetentionDays(t *testing.T) {
	repo := newMemoryRepo()
	repo.gatewaySettings = GatewaySettings{RequestLogRetentionDays: 14}
	repo.deletedRequestLogCount = 3
	service := NewService(repo, Config{SessionTTL: time.Hour})
	now := time.Date(2026, 6, 28, 12, 0, 0, 0, time.UTC)

	result, err := service.CleanupRequestLogs(context.Background(), now)
	if err != nil {
		t.Fatalf("CleanupRequestLogs returned error: %v", err)
	}

	wantBefore := now.Add(-14 * 24 * time.Hour)
	if !repo.deletedRequestLogsBefore.Equal(wantBefore) {
		t.Fatalf("delete cutoff = %s, want %s", repo.deletedRequestLogsBefore, wantBefore)
	}
	if result.RetentionDays != 14 || result.Deleted != 3 || !result.Before.Equal(wantBefore) {
		t.Fatalf("cleanup result = %+v, want retention 14 deleted 3 before %s", result, wantBefore)
	}
}

func TestCleanupRequestLogsRejectsDisabledRetention(t *testing.T) {
	repo := newMemoryRepo()
	service := NewService(repo, Config{SessionTTL: time.Hour})

	_, err := service.CleanupRequestLogs(context.Background(), time.Date(2026, 6, 28, 12, 0, 0, 0, time.UTC))
	if !errors.Is(err, ErrInvalidInput) {
		t.Fatalf("CleanupRequestLogs error = %v, want ErrInvalidInput", err)
	}
	if !repo.deletedRequestLogsBefore.IsZero() {
		t.Fatalf("deleted request logs before = %s, want zero", repo.deletedRequestLogsBefore)
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

func TestGetOpsCostBreakdownReturnsRepositoryBreakdown(t *testing.T) {
	repo := newMemoryRepo()
	service := NewService(repo, Config{SessionTTL: time.Hour})
	since := time.Unix(5000, 0).UTC().Add(-24 * time.Hour)
	repo.opsCostBreakdown = OpsCostBreakdown{
		WindowStart:           since,
		WindowEnd:             time.Unix(5000, 0).UTC(),
		EstimatedCostMicrousd: 7500,
		TopModels: []OpsCostBucket{{
			Key:                   "gpt-5",
			Label:                 "gpt-5",
			Requests:              3,
			EstimatedCostMicrousd: 7500,
		}},
	}

	breakdown, err := service.GetOpsCostBreakdown(context.Background(), since)
	if err != nil {
		t.Fatalf("GetOpsCostBreakdown returned error: %v", err)
	}
	if breakdown.EstimatedCostMicrousd != 7500 || len(breakdown.TopModels) != 1 {
		t.Fatalf("ops cost breakdown = %+v, want repo breakdown", breakdown)
	}
	if !repo.lastOpsCostSince.Equal(since) {
		t.Fatalf("repository since = %v, want %v", repo.lastOpsCostSince, since)
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
	deletedRequestLogsBefore  time.Time
	deletedRequestLogCount    int64
	usagePricing              UsagePricing
	usagePricingSaveCount     int
	opsAccountHealth          OpsAccountHealth
	lastOpsAccountHealthSince time.Time
	opsAccountTests           []OpsAccountTest
	lastOpsAccountTestsSince  time.Time
	lastOpsAccountTestsLimit  int
	opsCostBreakdown          OpsCostBreakdown
	lastOpsCostSince          time.Time
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
	Hash            string
	EncryptedSecret string
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

func (r *memoryRepo) UpdateAdminPassword(_ context.Context, id int64, passwordHash string) error {
	if r.admin.ID != id || r.admin.ID == 0 {
		return ErrNotFound
	}
	r.admin.PasswordHash = passwordHash
	return nil
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

func (r *memoryRepo) CreateAPIKey(_ context.Context, name, hash, prefix, encryptedSecret string) (APIKey, error) {
	key := APIKey{
		ID:              r.nextAPIKeyID,
		Name:            name,
		Prefix:          prefix,
		SecretAvailable: encryptedSecret != "",
		CreatedAt:       time.Now(),
		ModelPolicy:     APIKeyModelPolicyAll,
	}
	r.nextAPIKeyID++
	r.keys[key.ID] = memoryAPIKey{APIKey: key, Hash: hash, EncryptedSecret: encryptedSecret}
	return key, nil
}

func (r *memoryRepo) GetAPIKeyEncryptedSecret(_ context.Context, id int64) (string, error) {
	key, ok := r.keys[id]
	if !ok || key.RevokedAt != nil {
		return "", ErrNotFound
	}
	return key.EncryptedSecret, nil
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

func (r *memoryRepo) PurgeRevokedAPIKeys(_ context.Context, cutoff time.Time) (int64, error) {
	var deleted int64
	for id, key := range r.keys {
		if key.RevokedAt != nil && !key.RevokedAt.After(cutoff) {
			delete(r.keys, id)
			deleted++
		}
	}
	return deleted, nil
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

func (r *memoryRepo) UpdateAPIKeyBudgets(_ context.Context, id int64, requestBudget24h, tokenBudget24h int, costBudgetMicrousd24h int64, requestBudget30d, tokenBudget30d int, costBudgetMicrousd30d int64) (APIKey, error) {
	key, ok := r.keys[id]
	if !ok || key.RevokedAt != nil {
		return APIKey{}, ErrNotFound
	}
	key.RequestBudget24h = requestBudget24h
	key.TokenBudget24h = tokenBudget24h
	key.CostBudgetMicrousd24h = costBudgetMicrousd24h
	key.RequestBudget30d = requestBudget30d
	key.TokenBudget30d = tokenBudget30d
	key.CostBudgetMicrousd30d = costBudgetMicrousd30d
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

func (r *memoryRepo) DeleteRequestLogsBefore(_ context.Context, before time.Time) (int64, error) {
	r.deletedRequestLogsBefore = before
	return r.deletedRequestLogCount, nil
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
	r.usagePricingSaveCount++
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

func (r *memoryRepo) GetOpsCostBreakdown(_ context.Context, since time.Time) (OpsCostBreakdown, error) {
	r.lastOpsCostSince = since
	return r.opsCostBreakdown, nil
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
func TestDefaultUsagePricingHasNonZeroOfficialPrices(t *testing.T) {
	pricing := defaultUsagePricing()

	if pricing.Version != 1 {
		t.Fatalf("version = %d, want 1", pricing.Version)
	}
	if pricing.Currency != "USD" {
		t.Fatalf("currency = %s, want USD", pricing.Currency)
	}
	if pricing.Unit != "1M_tokens" {
		t.Fatalf("unit = %s, want 1M_tokens", pricing.Unit)
	}

	requiredModels := map[string]UsagePrice{
		"gpt-5.5":             {InputMicrousdPerMillion: 5_000_000, CachedInputMicrousdPerMillion: 500_000, OutputMicrousdPerMillion: 30_000_000},
		"gpt-5.4":             {InputMicrousdPerMillion: 2_500_000, CachedInputMicrousdPerMillion: 250_000, OutputMicrousdPerMillion: 15_000_000},
		"gpt-5.4-mini":        {InputMicrousdPerMillion: 750_000, CachedInputMicrousdPerMillion: 75_000, OutputMicrousdPerMillion: 4_500_000},
		"gpt-5.4-nano":        {InputMicrousdPerMillion: 200_000, CachedInputMicrousdPerMillion: 20_000, OutputMicrousdPerMillion: 1_250_000},
		"gpt-5.2":             {InputMicrousdPerMillion: 1_750_000, CachedInputMicrousdPerMillion: 175_000, OutputMicrousdPerMillion: 14_000_000},
		"gpt-5.1":             {InputMicrousdPerMillion: 1_250_000, CachedInputMicrousdPerMillion: 125_000, OutputMicrousdPerMillion: 10_000_000},
		"gpt-5":               {InputMicrousdPerMillion: 1_250_000, CachedInputMicrousdPerMillion: 125_000, OutputMicrousdPerMillion: 10_000_000},
		"gpt-5-mini":          {InputMicrousdPerMillion: 250_000, CachedInputMicrousdPerMillion: 25_000, OutputMicrousdPerMillion: 2_000_000},
		"gpt-5-nano":          {InputMicrousdPerMillion: 50_000, CachedInputMicrousdPerMillion: 5_000, OutputMicrousdPerMillion: 400_000},
		"gpt-5-pro":           {InputMicrousdPerMillion: 15_000_000, CachedInputMicrousdPerMillion: 0, OutputMicrousdPerMillion: 120_000_000},
		"gpt-4.1":             {InputMicrousdPerMillion: 2_000_000, CachedInputMicrousdPerMillion: 500_000, OutputMicrousdPerMillion: 8_000_000},
		"gpt-4.1-mini":        {InputMicrousdPerMillion: 400_000, CachedInputMicrousdPerMillion: 100_000, OutputMicrousdPerMillion: 1_600_000},
		"gpt-4.1-nano":        {InputMicrousdPerMillion: 100_000, CachedInputMicrousdPerMillion: 25_000, OutputMicrousdPerMillion: 400_000},
		"gpt-4o":              {InputMicrousdPerMillion: 2_500_000, CachedInputMicrousdPerMillion: 1_250_000, OutputMicrousdPerMillion: 10_000_000},
		"gpt-4o-mini":         {InputMicrousdPerMillion: 150_000, CachedInputMicrousdPerMillion: 75_000, OutputMicrousdPerMillion: 600_000},
		"gpt-5.3-chat-latest": {InputMicrousdPerMillion: 1_750_000, CachedInputMicrousdPerMillion: 175_000, OutputMicrousdPerMillion: 14_000_000},
		"chat-latest":         {InputMicrousdPerMillion: 5_000_000, CachedInputMicrousdPerMillion: 500_000, OutputMicrousdPerMillion: 30_000_000},
		"gpt-5.3-codex":       {InputMicrousdPerMillion: 1_750_000, CachedInputMicrousdPerMillion: 175_000, OutputMicrousdPerMillion: 14_000_000},
	}

	for model, want := range requiredModels {
		got, ok := pricing.Models[model]
		if !ok {
			t.Fatalf("default pricing missing model %q", model)
		}
		if got.InputMicrousdPerMillion != want.InputMicrousdPerMillion {
			t.Errorf("model %q input = %d, want %d", model, got.InputMicrousdPerMillion, want.InputMicrousdPerMillion)
		}
		if got.CachedInputMicrousdPerMillion != want.CachedInputMicrousdPerMillion {
			t.Errorf("model %q cached input = %d, want %d", model, got.CachedInputMicrousdPerMillion, want.CachedInputMicrousdPerMillion)
		}
		if got.OutputMicrousdPerMillion != want.OutputMicrousdPerMillion {
			t.Errorf("model %q output = %d, want %d", model, got.OutputMicrousdPerMillion, want.OutputMicrousdPerMillion)
		}
	}
}

func TestNormalizeUsagePricingRejectsNegativeLongFields(t *testing.T) {
	pricing := UsagePricing{
		Version:  1,
		Currency: "USD",
		Unit:     "1M_tokens",
		Models: map[string]UsagePrice{
			"gpt-5.5": {
				InputMicrousdPerMillion:           5_000_000,
				CachedInputMicrousdPerMillion:     500_000,
				OutputMicrousdPerMillion:          30_000_000,
				LongInputMicrousdPerMillion:       -1,
				LongCachedInputMicrousdPerMillion: 500_000,
				LongOutputMicrousdPerMillion:      45_000_000,
			},
		},
	}
	_, err := normalizeUsagePricing(pricing)
	if err == nil {
		t.Fatal("expected error for negative long input field")
	}
}

func TestNormalizeUsagePricingAcceptsZeroLongFields(t *testing.T) {
	pricing := UsagePricing{
		Version:  1,
		Currency: "USD",
		Unit:     "1M_tokens",
		Models: map[string]UsagePrice{
			"gpt-4.1": {
				InputMicrousdPerMillion:  2_000_000,
				OutputMicrousdPerMillion: 8_000_000,
			},
		},
	}
	normalized, err := normalizeUsagePricing(pricing)
	if err != nil {
		t.Fatalf("normalize with zero long fields: %v", err)
	}
	if normalized.Models["gpt-4.1"].LongInputMicrousdPerMillion != 0 {
		t.Error("long input should be zero for model without long pricing")
	}
}

func TestParseOfficialStandardPricingExtractsShortAndLongContextRows(t *testing.T) {
	body := `[1,[[0,"gpt-5.5"],[0,5],[0,0.5],[0,30]]]
<div data-content-switcher-pane="true" data-value="standard"><div class="hidden">Standard</div>
<table><thead><tr><th>Model</th><th colSpan="3">Short context</th><th colSpan="3">Long context</th></tr>
<tr><th>Model</th><th>Input</th><th>Cached input</th><th>Output</th><th>Input</th><th>Cached input</th><th>Output</th></tr></thead>
<tbody><tr>
<td><span>gpt-5.5</span></td><td><span>$5.00</span></td><td><span>$0.50</span></td><td><span>$30.00</span></td>
<td><span>$10.00</span></td><td><span>$1.00</span></td><td><span>$45.00</span></td>
</tr><tr>
<td><span>gpt-5.5-pro</span></td><td><span>$30.00</span></td><td><span>$0.00</span></td><td><span>$180.00</span></td>
<td><span>$60.00</span></td><td><span>$0.00</span></td><td><span>$270.00</span></td>
</tr></tbody></table></div>
<div data-content-switcher-pane="true" data-value="batch" hidden></div>`

	models, err := parseOfficialStandardPricing(body)
	if err != nil {
		t.Fatalf("parseOfficialStandardPricing: %v", err)
	}

	gpt55, ok := models["gpt-5.5"]
	if !ok {
		t.Fatal("missing gpt-5.5 from Short/Long context table")
	}
	if gpt55.InputMicrousdPerMillion != 5_000_000 {
		t.Errorf("gpt-5.5 short input = %d, want 5000000", gpt55.InputMicrousdPerMillion)
	}
	if gpt55.CachedInputMicrousdPerMillion != 500_000 {
		t.Errorf("gpt-5.5 short cached = %d, want 500000", gpt55.CachedInputMicrousdPerMillion)
	}
	if gpt55.OutputMicrousdPerMillion != 30_000_000 {
		t.Errorf("gpt-5.5 short output = %d, want 30000000", gpt55.OutputMicrousdPerMillion)
	}
	if gpt55.LongInputMicrousdPerMillion != 10_000_000 {
		t.Errorf("gpt-5.5 long input = %d, want 10000000", gpt55.LongInputMicrousdPerMillion)
	}
	if gpt55.LongCachedInputMicrousdPerMillion != 1_000_000 {
		t.Errorf("gpt-5.5 long cached = %d, want 1000000", gpt55.LongCachedInputMicrousdPerMillion)
	}
	if gpt55.LongOutputMicrousdPerMillion != 45_000_000 {
		t.Errorf("gpt-5.5 long output = %d, want 45000000", gpt55.LongOutputMicrousdPerMillion)
	}

	gpt55pro, ok := models["gpt-5.5-pro"]
	if !ok {
		t.Fatal("missing gpt-5.5-pro from Short/Long context table")
	}
	if gpt55pro.InputMicrousdPerMillion != 30_000_000 {
		t.Errorf("gpt-5.5-pro short input = %d, want 30000000", gpt55pro.InputMicrousdPerMillion)
	}
	if gpt55pro.CachedInputMicrousdPerMillion != 0 {
		t.Errorf("gpt-5.5-pro short cached = %d, want 0", gpt55pro.CachedInputMicrousdPerMillion)
	}
	if gpt55pro.OutputMicrousdPerMillion != 180_000_000 {
		t.Errorf("gpt-5.5-pro short output = %d, want 180000000", gpt55pro.OutputMicrousdPerMillion)
	}
	if gpt55pro.LongInputMicrousdPerMillion != 60_000_000 {
		t.Errorf("gpt-5.5-pro long input = %d, want 60000000", gpt55pro.LongInputMicrousdPerMillion)
	}
	if gpt55pro.LongCachedInputMicrousdPerMillion != 0 {
		t.Errorf("gpt-5.5-pro long cached = %d, want 0", gpt55pro.LongCachedInputMicrousdPerMillion)
	}
	if gpt55pro.LongOutputMicrousdPerMillion != 270_000_000 {
		t.Errorf("gpt-5.5-pro long output = %d, want 270000000", gpt55pro.LongOutputMicrousdPerMillion)
	}
}

func TestEstimateUsageCostSnapshotsLongFieldsWhenPresent(t *testing.T) {
	repo := newMemoryRepo()
	repo.usagePricing = UsagePricing{
		Version:  1,
		Currency: "USD",
		Unit:     "1M_tokens",
		Models: map[string]UsagePrice{
			"gpt-5.5": {
				InputMicrousdPerMillion:           5_000_000,
				CachedInputMicrousdPerMillion:     500_000,
				OutputMicrousdPerMillion:          30_000_000,
				LongInputMicrousdPerMillion:       10_000_000,
				LongCachedInputMicrousdPerMillion: 1_000_000,
				LongOutputMicrousdPerMillion:      45_000_000,
			},
			"gpt-4.1": {
				InputMicrousdPerMillion:       2_000_000,
				CachedInputMicrousdPerMillion: 500_000,
				OutputMicrousdPerMillion:      8_000_000,
			},
		},
	}
	service := NewService(repo, Config{SessionTTL: time.Hour})

	// Model with long fields: snapshot should include them.
	estimate, err := service.EstimateUsageCost(context.Background(), UsageCostInput{
		Model:        "gpt-5.5",
		InputTokens:  1000,
		OutputTokens: 500,
	})
	if err != nil {
		t.Fatalf("EstimateUsageCost: %v", err)
	}
	if !estimate.Matched {
		t.Fatal("Matched = false, want true")
	}
	if estimate.Snapshot["longInputMicrousdPerMillion"] != int64(10_000_000) {
		t.Errorf("long input snapshot = %v, want 10000000", estimate.Snapshot["longInputMicrousdPerMillion"])
	}
	if estimate.Snapshot["longCachedInputMicrousdPerMillion"] != int64(1_000_000) {
		t.Errorf("long cached snapshot = %v, want 1000000", estimate.Snapshot["longCachedInputMicrousdPerMillion"])
	}
	if estimate.Snapshot["longOutputMicrousdPerMillion"] != int64(45_000_000) {
		t.Errorf("long output snapshot = %v, want 45000000", estimate.Snapshot["longOutputMicrousdPerMillion"])
	}

	// Model without long fields: snapshot should NOT include them.
	estimate2, err := service.EstimateUsageCost(context.Background(), UsageCostInput{
		Model:        "gpt-4.1",
		InputTokens:  1000,
		OutputTokens: 500,
	})
	if err != nil {
		t.Fatalf("EstimateUsageCost: %v", err)
	}
	if _, ok := estimate2.Snapshot["longInputMicrousdPerMillion"]; ok {
		t.Error("long input snapshot should be absent for model without long pricing")
	}
}

// TestParseOfficialStandardPricingBlocksSSRContamination proves that hidden
// batch/priority panes do not contaminate Standard pricing parsed from SSR HTML.
func TestParseOfficialStandardPricingBlocksSSRContamination(t *testing.T) {
	body := `[1,[[0,"gpt-5.5"],[0,5],[0,0.5],[0,30]]]
<div data-content-switcher-pane="true" data-value="standard">
<table><tbody><tr>
<td><span>gpt-5.5</span></td><td><span>$5.00</span></td><td><span>$0.50</span></td><td><span>$30.00</span></td>
<td><span>$10.00</span></td><td><span>$1.00</span></td><td><span>$45.00</span></td>
</tr></tbody></table></div>
<div data-content-switcher-pane="true" data-value="batch" hidden>
<table><tbody><tr>
<td><span>gpt-5.5</span></td><td><span>$1.00</span></td><td><span>$0.10</span></td><td><span>$2.00</span></td>
<td><span>$3.00</span></td><td><span>$0.30</span></td><td><span>$6.00</span></td>
</tr></tbody></table></div>
<div data-content-switcher-pane="true" data-value="priority" hidden>
<table><tbody><tr>
<td><span>gpt-5.5</span></td><td><span>$7.50</span></td><td><span>$0.75</span></td><td><span>$45.00</span></td>
<td><span>$15.00</span></td><td><span>$1.50</span></td><td><span>$67.50</span></td>
</tr></tbody></table></div>
<div data-content-switcher-pane="true" data-value="flex" hidden></div>`

	models, err := parseOfficialStandardPricing(body)
	if err != nil {
		t.Fatalf("parseOfficialStandardPricing: %v", err)
	}

	gpt55, ok := models["gpt-5.5"]
	if !ok {
		t.Fatal("missing gpt-5.5")
	}
	// Must be Standard values, NOT batch/priority contamination.
	if gpt55.InputMicrousdPerMillion != 5_000_000 {
		t.Errorf("short input = %d, want 5000000 (contamination detected)", gpt55.InputMicrousdPerMillion)
	}
	if gpt55.CachedInputMicrousdPerMillion != 500_000 {
		t.Errorf("short cached = %d, want 500000 (contamination detected)", gpt55.CachedInputMicrousdPerMillion)
	}
	if gpt55.OutputMicrousdPerMillion != 30_000_000 {
		t.Errorf("short output = %d, want 30000000 (contamination detected)", gpt55.OutputMicrousdPerMillion)
	}
	if gpt55.LongInputMicrousdPerMillion != 10_000_000 {
		t.Errorf("long input = %d, want 10000000 (contamination detected)", gpt55.LongInputMicrousdPerMillion)
	}
	if gpt55.LongCachedInputMicrousdPerMillion != 1_000_000 {
		t.Errorf("long cached = %d, want 1000000 (contamination detected)", gpt55.LongCachedInputMicrousdPerMillion)
	}
	if gpt55.LongOutputMicrousdPerMillion != 45_000_000 {
		t.Errorf("long output = %d, want 45000000 (contamination detected)", gpt55.LongOutputMicrousdPerMillion)
	}
}

// TestParseOfficialStandardPricingParsesSSRPaneAsLastPane verifies that
// Short/Long context rows are still parsed when the Standard pane is the last
// content-switcher pane (no following pane div).
func TestParseOfficialStandardPricingParsesSSRPaneAsLastPane(t *testing.T) {
	// Standard pane at the end — no batch/priority div after it.
	body := `[1,[[0,"gpt-5.5"],[0,5],[0,0.5],[0,30]]]
<div data-content-switcher-pane="true" data-value="standard">
<table><tbody><tr>
<td><span>gpt-5.5</span></td><td><span>$5.00</span></td><td><span>$0.50</span></td><td><span>$30.00</span></td>
<td><span>$10.00</span></td><td><span>$1.00</span></td><td><span>$45.00</span></td>
</tr></tbody></table></div>
</body></html>`

	models, err := parseOfficialStandardPricing(body)
	if err != nil {
		t.Fatalf("parseOfficialStandardPricing: %v", err)
	}
	gpt55, ok := models["gpt-5.5"]
	if !ok {
		t.Fatal("missing gpt-5.5 when Standard is last pane")
	}
	if gpt55.InputMicrousdPerMillion != 5_000_000 {
		t.Errorf("short input = %d, want 5000000", gpt55.InputMicrousdPerMillion)
	}
	if gpt55.LongInputMicrousdPerMillion != 10_000_000 {
		t.Errorf("long input = %d, want 10000000", gpt55.LongInputMicrousdPerMillion)
	}
	if gpt55.LongOutputMicrousdPerMillion != 45_000_000 {
		t.Errorf("long output = %d, want 45000000", gpt55.LongOutputMicrousdPerMillion)
	}
}

// TestParseOfficialStandardPricingSSROnlyModel parses a model that only appears
// in the SSR table (not in 4-value Astro props).
func TestParseOfficialStandardPricingSSROnlyModel(t *testing.T) {
	body := `[1,[[0,"gpt-5.5"],[0,5],[0,0.5],[0,30]]]
<div data-content-switcher-pane="true" data-value="standard">
<table><tbody><tr>
<td><span>gpt-5.5</span></td><td><span>$5.00</span></td><td><span>$0.50</span></td><td><span>$30.00</span></td>
<td><span>$10.00</span></td><td><span>$1.00</span></td><td><span>$45.00</span></td>
</tr><tr>
<td><span>ssr-only-model</span></td><td><span>$2.00</span></td><td><span>$0.20</span></td><td><span>$12.00</span></td>
<td><span>$4.00</span></td><td><span>$0.40</span></td><td><span>$18.00</span></td>
</tr></tbody></table></div>
<div data-content-switcher-pane="true" data-value="batch" hidden></div>`

	models, err := parseOfficialStandardPricing(body)
	if err != nil {
		t.Fatalf("parseOfficialStandardPricing: %v", err)
	}
	ssrOnly, ok := models["ssr-only-model"]
	if !ok {
		t.Fatal("missing ssr-only-model from SSR table")
	}
	if ssrOnly.InputMicrousdPerMillion != 2_000_000 {
		t.Errorf("short input = %d, want 2000000", ssrOnly.InputMicrousdPerMillion)
	}
	if ssrOnly.CachedInputMicrousdPerMillion != 200_000 {
		t.Errorf("short cached = %d, want 200000", ssrOnly.CachedInputMicrousdPerMillion)
	}
	if ssrOnly.OutputMicrousdPerMillion != 12_000_000 {
		t.Errorf("short output = %d, want 12000000", ssrOnly.OutputMicrousdPerMillion)
	}
	if ssrOnly.LongInputMicrousdPerMillion != 4_000_000 {
		t.Errorf("long input = %d, want 4000000", ssrOnly.LongInputMicrousdPerMillion)
	}
	if ssrOnly.LongCachedInputMicrousdPerMillion != 400_000 {
		t.Errorf("long cached = %d, want 400000", ssrOnly.LongCachedInputMicrousdPerMillion)
	}
	if ssrOnly.LongOutputMicrousdPerMillion != 18_000_000 {
		t.Errorf("long output = %d, want 18000000", ssrOnly.LongOutputMicrousdPerMillion)
	}
}

func TestParseOfficialStandardPricingExtractsStandardRows(t *testing.T) {
	body := `[1,[[0,"gpt-5.5 (<272K context length)"],[0,5],[0,0.5],[0,30]]],[1,[[0,"gpt-5.4-mini"],[0,0.75],[0,0.075],[0,4.5]]],[1,[[0,"gpt-5-nano"],[0,0.05],[0,0.005],[0,0.4]]],[1,[[0,"gpt-5-pro"],[0,15],[0,null],[0,120]]],[1,[[0,"gpt-4.1"],[0,2],[0,0.5],[0,8]]],[1,[[0,"chatgpt-4o-latest"],[0,5],[0,"-"],[0,15]]]`

	models, err := parseOfficialStandardPricing(body)
	if err != nil {
		t.Fatalf("parseOfficialStandardPricing: %v", err)
	}

	if got, want := len(models), 6; got != want {
		t.Fatalf("model count = %d, want %d", got, want)
	}

	// Context annotation stripped.
	gpt55, ok := models["gpt-5.5"]
	if !ok {
		t.Fatal("missing gpt-5.5 (context annotation stripping failed)")
	}
	if gpt55.InputMicrousdPerMillion != 5_000_000 {
		t.Errorf("gpt-5.5 input = %d, want 5000000", gpt55.InputMicrousdPerMillion)
	}
	if gpt55.CachedInputMicrousdPerMillion != 500_000 {
		t.Errorf("gpt-5.5 cached = %d, want 500000", gpt55.CachedInputMicrousdPerMillion)
	}
	if gpt55.OutputMicrousdPerMillion != 30_000_000 {
		t.Errorf("gpt-5.5 output = %d, want 30000000", gpt55.OutputMicrousdPerMillion)
	}

	// gpt-5-nano: tiny prices.
	nano, ok := models["gpt-5-nano"]
	if !ok {
		t.Fatal("missing gpt-5-nano")
	}
	if nano.InputMicrousdPerMillion != 50_000 {
		t.Errorf("gpt-5-nano input = %d, want 50000", nano.InputMicrousdPerMillion)
	}
	if nano.CachedInputMicrousdPerMillion != 5_000 {
		t.Errorf("gpt-5-nano cached = %d, want 5000", nano.CachedInputMicrousdPerMillion)
	}

	// gpt-5-pro: null cached input → 0.
	pro, ok := models["gpt-5-pro"]
	if !ok {
		t.Fatal("missing gpt-5-pro")
	}
	if pro.InputMicrousdPerMillion != 15_000_000 {
		t.Errorf("gpt-5-pro input = %d, want 15000000", pro.InputMicrousdPerMillion)
	}
	if pro.CachedInputMicrousdPerMillion != 0 {
		t.Errorf("gpt-5-pro cached = %d, want 0 (null cached)", pro.CachedInputMicrousdPerMillion)
	}

	// chatgpt-4o-latest: "-" cached → 0, but input is 5 and output is 15, so it should be parsed.
	chat, ok := models["chatgpt-4o-latest"]
	if !ok {
		t.Fatal("missing chatgpt-4o-latest")
	}
	if chat.CachedInputMicrousdPerMillion != 0 {
		t.Errorf("chatgpt-4o-latest cached = %d, want 0", chat.CachedInputMicrousdPerMillion)
	}
}

func TestParseOfficialStandardPricingSkipsNonNumericRows(t *testing.T) {
	// Row with __pricingHtml output (not a plain number) should be skipped.
	body := `[1,[[0,"gpt-5.5"],[0,5],[0,0.5],[0,30]]]`
	models, err := parseOfficialStandardPricing(body)
	if err != nil {
		t.Fatalf("parseOfficialStandardPricing: %v", err)
	}
	if _, ok := models["gpt-5.5"]; !ok {
		t.Fatal("valid row should be parsed")
	}
}

func TestParseOfficialStandardPricingReturnsErrInvalidInputOnEmptyResult(t *testing.T) {
	_, err := parseOfficialStandardPricing("no model data here")
	if !errors.Is(err, ErrInvalidInput) {
		t.Fatalf("error = %v, want ErrInvalidInput", err)
	}
}

func TestParseOfficialStandardPricingDeduplicatesByFirstRow(t *testing.T) {
	// Two rows for same model: keep first.
	body := `[1,[[0,"gpt-5.5"],[0,5],[0,0.5],[0,30]]],[1,[[0,"gpt-5.5"],[0,1],[0,0.1],[0,5]]]`
	models, err := parseOfficialStandardPricing(body)
	if err != nil {
		t.Fatalf("parseOfficialStandardPricing: %v", err)
	}
	if len(models) != 1 {
		t.Fatalf("model count = %d, want 1 (deduplicated)", len(models))
	}
	gpt55 := models["gpt-5.5"]
	if gpt55.InputMicrousdPerMillion != 5_000_000 {
		t.Errorf("dedup kept wrong row: input = %d, want 5000000", gpt55.InputMicrousdPerMillion)
	}
}

func TestParseOfficialStandardPricingExtractsCurrentPageShape(t *testing.T) {
	body := `<astro-island component-export="TextTokenPricingTables" props="{&quot;tier&quot;:[0,&quot;standard&quot;],&quot;rows&quot;:[1,[[1,[[0,&quot;gpt-5.5 (&lt;272K context length)&quot;],[0,5],[0,0.5],[0,30]]],[1,[[0,&quot;gpt-5.4-mini&quot;],[0,0.75],[0,0.075],[0,4.5]]]]]}"></astro-island>
<astro-island component-export="TextTokenPricingTables" props="{&quot;tier&quot;:[0,&quot;batch&quot;],&quot;rows&quot;:[1,[[1,[[0,&quot;batch-only&quot;],[0,1],[0,0.1],[0,2]]]]]}"></astro-island>`

	models, err := parseOfficialStandardPricing(body)
	if err != nil {
		t.Fatalf("parseOfficialStandardPricing: %v", err)
	}
	if got := len(models); got != 2 {
		t.Fatalf("model count = %d, want 2", got)
	}
	for _, model := range []string{"gpt-5.5", "gpt-5.4-mini"} {
		if _, ok := models[model]; !ok {
			t.Fatalf("missing parsed model %q from current page shape", model)
		}
	}
	if _, ok := models["batch-only"]; ok {
		t.Fatal("batch-only model was parsed from non-standard tier")
	}
}

func TestParseOfficialModelCatalogIncludesDeprecatedMarker(t *testing.T) {
	body := `<a href="/api/docs/models/gpt-5.6-sol"><div>GPT-5.6 Sol</div></a>
<a href="/api/docs/models/gpt-5.3-chat-latest"><div>GPT-5.3 Chat</div><div>Deprecated</div></a>`

	models, err := parseOfficialModelCatalog(body)
	if err != nil {
		t.Fatalf("parseOfficialModelCatalog: %v", err)
	}
	if models["gpt-5.6-sol"].Deprecated {
		t.Fatal("gpt-5.6-sol unexpectedly deprecated")
	}
	if !models["gpt-5.3-chat-latest"].Deprecated {
		t.Fatal("missing deprecated marker")
	}
}

func TestParseOfficialStandardPricingSupportsCacheWritesColumns(t *testing.T) {
	body := `<astro-island component-export="TextTokenPricingTables" props="{&quot;tier&quot;:[0,&quot;standard&quot;],&quot;rows&quot;:[1,[[1,[[0,&quot;gpt-5.6-sol&quot;],[0,5],[0,0.5],[0,6.25],[0,30]]]]]}"></astro-island>`

	models, err := parseOfficialStandardPricing(body)
	if err != nil {
		t.Fatalf("parseOfficialStandardPricing: %v", err)
	}
	price, ok := models["gpt-5.6-sol"]
	if !ok {
		t.Fatal("missing gpt-5.6-sol")
	}
	if price.InputMicrousdPerMillion != 5_000_000 {
		t.Fatalf("input = %d, want 5000000", price.InputMicrousdPerMillion)
	}
	if price.CachedInputMicrousdPerMillion != 500_000 {
		t.Fatalf("cached input = %d, want 500000", price.CachedInputMicrousdPerMillion)
	}
	if price.OutputMicrousdPerMillion != 30_000_000 {
		t.Fatalf("output = %d, want 30000000", price.OutputMicrousdPerMillion)
	}
}

func TestParseOfficialStandardPricingSupportsCacheWritesSSRColumns(t *testing.T) {
	body := `<div data-content-switcher-pane="true" data-value="standard">
<table><tbody><tr>
<td>gpt-5.6-sol</td><td>$5.00</td><td>$0.50</td><td>$6.25</td><td>$30.00</td>
<td>$10.00</td><td>$1.00</td><td>$12.50</td><td>$60.00</td>
</tr></tbody></table></div>
<div data-content-switcher-pane="true" data-value="batch" hidden></div>`

	models, err := parseOfficialStandardPricing(body)
	if err != nil {
		t.Fatalf("parseOfficialStandardPricing: %v", err)
	}
	price, ok := models["gpt-5.6-sol"]
	if !ok {
		t.Fatal("missing gpt-5.6-sol")
	}
	if price.OutputMicrousdPerMillion != 30_000_000 {
		t.Fatalf("short output = %d, want 30000000", price.OutputMicrousdPerMillion)
	}
	if price.LongInputMicrousdPerMillion != 10_000_000 {
		t.Fatalf("long input = %d, want 10000000", price.LongInputMicrousdPerMillion)
	}
	if price.LongOutputMicrousdPerMillion != 60_000_000 {
		t.Fatalf("long output = %d, want 60000000", price.LongOutputMicrousdPerMillion)
	}
}

func TestParseOfficialDeprecationsNormalizesDates(t *testing.T) {
	body := `<table><thead><tr><th>Shutdown date</th><th>Model / system</th><th>Recommended replacement</th></tr></thead><tbody>
<tr><td>Aug 10, 2026</td><td><code>gpt-5.3-chat-latest</code></td><td><code>gpt-5.5</code></td></tr>
<tr><td>2026‑03‑26</td><td><code>gpt-4-0314</code></td><td><code>gpt-5</code></td></tr>
</tbody></table>`

	items, err := parseOfficialDeprecations(body)
	if err != nil {
		t.Fatalf("parseOfficialDeprecations: %v", err)
	}
	if got := items["gpt-5.3-chat-latest"].ShutdownDate; got != "2026-08-10" {
		t.Fatalf("English date = %q, want 2026-08-10", got)
	}
	if got := items["gpt-4-0314"].ShutdownDate; got != "2026-03-26" {
		t.Fatalf("Unicode date = %q, want 2026-03-26", got)
	}
	if got := items["gpt-5.3-chat-latest"].Replacement; got != "gpt-5.5" {
		t.Fatalf("replacement = %q, want gpt-5.5", got)
	}
}

type fakeOfficialDocumentFetcher struct {
	bodies map[string][]byte
	errs   map[string]error
}

func (f *fakeOfficialDocumentFetcher) Fetch(_ context.Context, url string) ([]byte, error) {
	if err := f.errs[url]; err != nil {
		return nil, err
	}
	return f.bodies[url], nil
}

func officialSyncFixtures() map[string][]byte {
	return map[string][]byte{
		officialModelsURL: []byte(`<a href="/api/docs/models/gpt-5.5"><div>GPT-5.5</div></a>
<a href="/api/docs/models/gpt-5.6-sol"><div>GPT-5.6 Sol</div></a>
<a href="/api/docs/models/gpt-5.3-chat-latest"><div>GPT-5.3 Chat</div><div>Deprecated</div></a>
<a href="/api/docs/models/gpt-4-0314"><div>GPT-4 0314</div><div>Deprecated</div></a>`),
		officialPricingURL: []byte(`<astro-island component-export="TextTokenPricingTables" props="{&quot;tier&quot;:[0,&quot;standard&quot;],&quot;rows&quot;:[1,[[1,[[0,&quot;gpt-5.5&quot;],[0,5],[0,0.5],[0,6.25],[0,30]]],[1,[[0,&quot;gpt-5.6-sol&quot;],[0,5],[0,0.5],[0,6.25],[0,30]]]]]}"></astro-island>`),
		officialDeprecationsURL: []byte(`<table><tbody>
<tr><td>Aug 10, 2026</td><td><code>gpt-5.3-chat-latest</code></td><td><code>gpt-5.5</code></td></tr>
<tr><td>2026-03-26</td><td><code>gpt-4-0314</code></td><td><code>gpt-5</code></td></tr>
</tbody></table>`),
	}
}

func TestSyncOfficialUsagePricingAdditiveMergeAndLifecycle(t *testing.T) {
	repo := newMemoryRepo()
	repo.usagePricing = UsagePricing{
		Version:  1,
		Currency: "USD",
		Unit:     "1M_tokens",
		Models: map[string]UsagePrice{
			"gpt-5.5":             {InputMicrousdPerMillion: 1},
			"local-model":         {InputMicrousdPerMillion: 99},
			"gpt-4-0314":          {InputMicrousdPerMillion: 30_000_000},
			"gpt-5.3-chat-latest": {InputMicrousdPerMillion: 1_750_000},
		},
	}
	service := NewService(repo, Config{SessionTTL: time.Hour})
	service.SetOfficialDocumentFetcher(&fakeOfficialDocumentFetcher{bodies: officialSyncFixtures()})
	service.SetNow(func() time.Time { return time.Date(2026, 7, 10, 12, 0, 0, 0, time.UTC) })

	pricing, summary, err := service.SyncOfficialUsagePricing(context.Background())
	if err != nil {
		t.Fatalf("SyncOfficialUsagePricing: %v", err)
	}
	if got := pricing.Models["gpt-5.5"].InputMicrousdPerMillion; got != 5_000_000 {
		t.Fatalf("updated gpt-5.5 input = %d, want 5000000", got)
	}
	if _, ok := pricing.Models["gpt-5.6-sol"]; !ok {
		t.Fatal("missing newly added gpt-5.6-sol")
	}
	if got := pricing.Models["local-model"].InputMicrousdPerMillion; got != 99 {
		t.Fatalf("local model input = %d, want 99", got)
	}
	if _, ok := pricing.Models["gpt-4-0314"]; !ok {
		t.Fatal("sync deleted shut-down model before confirmation")
	}
	if got, want := summary.Added, []string{"gpt-5.6-sol"}; !slices.Equal(got, want) {
		t.Fatalf("added = %v, want %v", got, want)
	}
	if got, want := summary.Updated, []string{"gpt-5.5"}; !slices.Equal(got, want) {
		t.Fatalf("updated = %v, want %v", got, want)
	}
	if summary.Unchanged != 0 {
		t.Fatalf("unchanged = %d, want 0", summary.Unchanged)
	}
	if len(summary.UpcomingShutdowns) != 1 || summary.UpcomingShutdowns[0].Model != "gpt-5.3-chat-latest" {
		t.Fatalf("upcoming shutdowns = %+v", summary.UpcomingShutdowns)
	}
	if len(summary.DeletionCandidates) != 1 || summary.DeletionCandidates[0].Model != "gpt-4-0314" {
		t.Fatalf("deletion candidates = %+v", summary.DeletionCandidates)
	}
	if summary.Sources.Models != officialModelsURL || summary.Sources.Pricing != officialPricingURL || summary.Sources.Deprecations != officialDeprecationsURL {
		t.Fatalf("sources = %+v", summary.Sources)
	}
	if repo.usagePricingSaveCount != 1 {
		t.Fatalf("save count = %d, want 1", repo.usagePricingSaveCount)
	}
}

func TestSyncOfficialUsagePricingSourceFailureIsAtomic(t *testing.T) {
	tests := []struct {
		name string
		url  string
	}{
		{name: "models", url: officialModelsURL},
		{name: "pricing", url: officialPricingURL},
		{name: "deprecations", url: officialDeprecationsURL},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			repo := newMemoryRepo()
			repo.usagePricing = UsagePricing{
				Version:  1,
				Currency: "USD",
				Unit:     "1M_tokens",
				Models:   map[string]UsagePrice{"local-model": {InputMicrousdPerMillion: 99}},
			}
			service := NewService(repo, Config{SessionTTL: time.Hour})
			fixtures := officialSyncFixtures()
			fixtures[tt.url] = []byte("invalid source")
			service.SetOfficialDocumentFetcher(&fakeOfficialDocumentFetcher{bodies: fixtures})

			_, _, err := service.SyncOfficialUsagePricing(context.Background())
			if !errors.Is(err, ErrInvalidInput) {
				t.Fatalf("error = %v, want ErrInvalidInput", err)
			}
			if repo.usagePricingSaveCount != 0 {
				t.Fatalf("save count = %d, want 0", repo.usagePricingSaveCount)
			}
			if got := repo.usagePricing.Models["local-model"].InputMicrousdPerMillion; got != 99 {
				t.Fatalf("local model changed to %d", got)
			}
		})
	}
}

func TestSyncOfficialUsagePricingSavesAndReturnsSummary(t *testing.T) {
	repo := newMemoryRepo()
	service := NewService(repo, Config{SessionTTL: time.Hour})
	service.SetOfficialDocumentFetcher(&fakeOfficialDocumentFetcher{bodies: officialSyncFixtures()})

	pricing, summary, err := service.SyncOfficialUsagePricing(context.Background())
	if err != nil {
		t.Fatalf("SyncOfficialUsagePricing: %v", err)
	}

	if summary.Total == 0 {
		t.Error("summary.Total = 0")
	}
	if summary.Sources.Pricing == "" {
		t.Error("summary.Sources.Pricing is empty")
	}
	if len(pricing.Models) == 0 {
		t.Error("len(pricing.Models) = 0")
	}

	// Verify the values were saved through the repo.
	saved, _ := repo.GetUsagePricing(context.Background())
	if saved.Models["gpt-5.5"].InputMicrousdPerMillion != 5_000_000 {
		t.Errorf("saved gpt-5.5 input = %d, want 5000000", saved.Models["gpt-5.5"].InputMicrousdPerMillion)
	}
}

func TestSyncOfficialUsagePricingInvalidSourceReturnsErrInvalidInput(t *testing.T) {
	repo := newMemoryRepo()
	service := NewService(repo, Config{SessionTTL: time.Hour})
	fixtures := officialSyncFixtures()
	fixtures[officialPricingURL] = []byte("no model data here")
	service.SetOfficialDocumentFetcher(&fakeOfficialDocumentFetcher{bodies: fixtures})

	_, _, err := service.SyncOfficialUsagePricing(context.Background())
	if !errors.Is(err, ErrInvalidInput) {
		t.Fatalf("error = %v, want ErrInvalidInput", err)
	}
}

func TestSyncOfficialUsagePricingWithoutFetcherUsesDefaultFetcher(t *testing.T) {
	repo := newMemoryRepo()
	service := NewService(repo, Config{SessionTTL: time.Hour})

	if service.officialDocumentFetcher == nil {
		t.Fatal("officialDocumentFetcher is nil, want default HTTP fetcher")
	}
}

func TestRemoveShutdownUsagePricingRemovesValidatedModels(t *testing.T) {
	repo := newMemoryRepo()
	repo.usagePricing = UsagePricing{
		Version: 1, Currency: "USD", Unit: "1M_tokens",
		Models: map[string]UsagePrice{
			"gpt-4-0314":  {InputMicrousdPerMillion: 30_000_000},
			"o1-mini":     {InputMicrousdPerMillion: 3_000_000},
			"local-model": {InputMicrousdPerMillion: 99},
		},
	}
	fixtures := officialSyncFixtures()
	fixtures[officialDeprecationsURL] = []byte(`<table><tbody>
<tr><td>2026-03-26</td><td><code>gpt-4-0314</code></td><td><code>gpt-5</code></td></tr>
<tr><td>2025-10-27</td><td><code>o1-mini</code></td><td><code>o4-mini</code></td></tr>
</tbody></table>`)
	service := NewService(repo, Config{SessionTTL: time.Hour})
	service.SetOfficialDocumentFetcher(&fakeOfficialDocumentFetcher{bodies: fixtures})
	service.SetNow(func() time.Time { return time.Date(2026, 7, 10, 12, 0, 0, 0, time.UTC) })

	pricing, removed, err := service.RemoveShutdownUsagePricing(context.Background(), []string{"o1-mini", "gpt-4-0314"})
	if err != nil {
		t.Fatalf("RemoveShutdownUsagePricing: %v", err)
	}
	if got, want := removed, []string{"gpt-4-0314", "o1-mini"}; !slices.Equal(got, want) {
		t.Fatalf("removed = %v, want %v", got, want)
	}
	if _, ok := pricing.Models["gpt-4-0314"]; ok {
		t.Fatal("gpt-4-0314 was not removed")
	}
	if _, ok := pricing.Models["o1-mini"]; ok {
		t.Fatal("o1-mini was not removed")
	}
	if _, ok := pricing.Models["local-model"]; !ok {
		t.Fatal("local-model was removed")
	}
	if repo.usagePricingSaveCount != 1 {
		t.Fatalf("save count = %d, want 1", repo.usagePricingSaveCount)
	}
}

func TestRemoveShutdownUsagePricingRejectsInvalidBatchAtomically(t *testing.T) {
	tests := []struct {
		name   string
		models []string
	}{
		{name: "blank", models: []string{"gpt-4-0314", " "}},
		{name: "duplicate", models: []string{"gpt-4-0314", "gpt-4-0314"}},
		{name: "unknown", models: []string{"unknown-model"}},
		{name: "not local", models: []string{"o1-mini"}},
		{name: "future shutdown", models: []string{"gpt-5.3-chat-latest"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			repo := newMemoryRepo()
			repo.usagePricing = UsagePricing{
				Version: 1, Currency: "USD", Unit: "1M_tokens",
				Models: map[string]UsagePrice{
					"gpt-4-0314":          {InputMicrousdPerMillion: 30_000_000},
					"gpt-5.3-chat-latest": {InputMicrousdPerMillion: 1_750_000},
				},
			}
			service := NewService(repo, Config{SessionTTL: time.Hour})
			service.SetOfficialDocumentFetcher(&fakeOfficialDocumentFetcher{bodies: officialSyncFixtures()})
			service.SetNow(func() time.Time { return time.Date(2026, 7, 10, 12, 0, 0, 0, time.UTC) })

			_, _, err := service.RemoveShutdownUsagePricing(context.Background(), tt.models)
			if !errors.Is(err, ErrInvalidInput) {
				t.Fatalf("error = %v, want ErrInvalidInput", err)
			}
			if repo.usagePricingSaveCount != 0 {
				t.Fatalf("save count = %d, want 0", repo.usagePricingSaveCount)
			}
			if len(repo.usagePricing.Models) != 2 {
				t.Fatalf("models changed: %v", repo.usagePricing.Models)
			}
		})
	}
}

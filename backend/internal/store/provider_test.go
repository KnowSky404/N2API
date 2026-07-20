package store

import (
	"context"
	"errors"
	"os"
	"reflect"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/KnowSky404/N2API/backend/internal/provider"
	"github.com/KnowSky404/N2API/backend/internal/systemevent"
	"github.com/jackc/pgx/v5/pgxpool"
)

func TestProviderRepositoryImplementsInterface(t *testing.T) {
	var _ provider.Repository = (*ProviderRepository)(nil)
}

func TestProviderRepositoryCommitsProviderMutationWithIntent(t *testing.T) {
	repo, cleanup := newProviderRepositoryForTest(t)
	defer cleanup()
	account := saveProviderTestAccount(t, repo, provider.Account{
		Provider: "openai", AccountType: provider.AccountTypeCodexOAuth, Subject: "audit-account",
		Name: "Audited account", DisplayName: "Audited account", Enabled: true, Status: provider.AccountStatusActive,
		EncryptedAccessToken: "encrypted-access", EncryptedRefreshToken: "encrypted-refresh",
	})
	ctx := systemevent.WithRequestContext(context.Background(), systemevent.RequestContext{
		CorrelationID: "provider-test-request", Actor: systemevent.Actor{Type: systemevent.ActorAdmin, ID: 3, Name: "admin"},
	})
	ctx = systemevent.WithIntent(ctx, systemevent.EventIntent{
		Category: systemevent.CategoryAudit, Severity: systemevent.SeverityInfo,
		Action: systemevent.ActionProviderAccountUpdated, Outcome: systemevent.OutcomeSuccess,
		Metadata: map[string]any{"batch_id": "batch-7"},
	})
	enabled := false
	if _, err := repo.UpdateAccount(ctx, "openai", account.ID, provider.AccountUpdate{Enabled: &enabled}); err != nil {
		t.Fatalf("UpdateAccount returned error: %v", err)
	}
	var action, targetID, targetName, correlationID, batchID string
	if err := repo.pool.QueryRow(context.Background(), `
		SELECT action, target_id, target_name, correlation_id, metadata->>'batch_id'
		FROM system_events WHERE action = $1
	`, systemevent.ActionProviderAccountUpdated).Scan(&action, &targetID, &targetName, &correlationID, &batchID); err != nil {
		t.Fatalf("query system event returned error: %v", err)
	}
	if action != string(systemevent.ActionProviderAccountUpdated) || targetID != strconv.FormatInt(account.ID, 10) || targetName != "Audited account" || correlationID != "provider-test-request" || batchID != "batch-7" {
		t.Fatalf("event = action %q target %q/%q correlation %q batch %q", action, targetID, targetName, correlationID, batchID)
	}
}

func TestProviderRepositoryRollsBackMutationWhenIntentIsInvalid(t *testing.T) {
	repo, cleanup := newProviderRepositoryForTest(t)
	defer cleanup()
	account := saveProviderTestAccount(t, repo, provider.Account{
		Provider: "openai", AccountType: provider.AccountTypeCodexOAuth, Subject: "rollback-account",
		DisplayName: "Rollback account", Enabled: true, Status: provider.AccountStatusActive,
		EncryptedAccessToken: "encrypted-access", EncryptedRefreshToken: "encrypted-refresh",
	})
	ctx := systemevent.WithIntent(context.Background(), systemevent.EventIntent{
		Category: systemevent.CategoryAudit, Severity: systemevent.SeverityInfo,
		Action: systemevent.Action("provider_account.unknown"), Outcome: systemevent.OutcomeSuccess,
	})
	enabled := false
	if _, err := repo.UpdateAccount(ctx, "openai", account.ID, provider.AccountUpdate{Enabled: &enabled}); !errors.Is(err, systemevent.ErrInvalidEvent) {
		t.Fatalf("UpdateAccount error = %v, want invalid system event", err)
	}
	found, err := repo.FindAccountByID(context.Background(), "openai", account.ID)
	if err != nil {
		t.Fatal(err)
	}
	if !found.Enabled {
		t.Fatal("provider mutation committed despite invalid audit event")
	}
}

func TestProviderRuntimeEventOnlyRecordsStateTransitions(t *testing.T) {
	repo, cleanup := newProviderRepositoryForTest(t)
	defer cleanup()
	account := saveProviderTestAccount(t, repo, provider.Account{
		Provider: "openai", AccountType: provider.AccountTypeCodexOAuth, Subject: "runtime-account",
		DisplayName: "Runtime account", Enabled: true, Status: provider.AccountStatusActive,
		EncryptedAccessToken: "encrypted-access", EncryptedRefreshToken: "encrypted-refresh",
	})
	ctx := systemevent.WithIntent(context.Background(), systemevent.EventIntent{
		Category: systemevent.CategoryRuntime, Severity: systemevent.SeverityWarning,
		Action: systemevent.ActionProviderAccountRateLimited, Outcome: systemevent.OutcomeSuccess,
	})
	now := time.Now().UTC()
	if err := repo.RecordAccountStatus(ctx, "openai", account.ID, provider.AccountStatusActive, "same", now, nil, nil); err != nil {
		t.Fatal(err)
	}
	until := now.Add(time.Minute)
	if err := repo.RecordAccountStatus(ctx, "openai", account.ID, provider.AccountStatusRateLimited, "limited", now, &until, nil); err != nil {
		t.Fatal(err)
	}
	if err := repo.RecordAccountStatus(ctx, "openai", account.ID, provider.AccountStatusRateLimited, "limited again", now, &until, nil); err != nil {
		t.Fatal(err)
	}
	var count int
	if err := repo.pool.QueryRow(context.Background(), `SELECT count(*) FROM system_events WHERE action = $1`, systemevent.ActionProviderAccountRateLimited).Scan(&count); err != nil {
		t.Fatal(err)
	}
	if count != 1 {
		t.Fatalf("runtime event count = %d, want 1 actual transition", count)
	}
}

func TestSaveAccountSubjectConflictPreservesSchedulingFields(t *testing.T) {
	source, err := os.ReadFile("provider.go")
	if err != nil {
		t.Fatalf("ReadFile provider.go returned error: %v", err)
	}
	sql := string(source)
	for _, forbidden := range []string{
		"enabled = EXCLUDED.enabled",
		"priority = EXCLUDED.priority",
	} {
		if strings.Contains(sql, forbidden) {
			t.Fatalf("SaveAccount subject conflict must preserve scheduling field, found %q", forbidden)
		}
	}
}

func TestRoutingPoolHasAccountsUsesMembershipRows(t *testing.T) {
	source, err := os.ReadFile("provider.go")
	if err != nil {
		t.Fatalf("ReadFile provider.go returned error: %v", err)
	}
	sql := strings.ToUpper(string(source))
	for _, want := range []string{
		"FUNC (R *PROVIDERREPOSITORY) ROUTINGPOOLHASACCOUNTS",
		"SELECT EXISTS",
		"FROM ROUTING_POOL_ACCOUNTS",
		"WHERE POOL_ID = $1",
	} {
		if !strings.Contains(sql, want) {
			t.Fatalf("RoutingPoolHasAccounts query must include %q", want)
		}
	}
}

func TestRoutingPoolQueriesDoNotFilterRefreshableOAuthAccountsByAccessTokenExpiry(t *testing.T) {
	source, err := os.ReadFile("provider.go")
	if err != nil {
		t.Fatalf("ReadFile provider.go returned error: %v", err)
	}
	text := string(source)
	for _, testCase := range []struct {
		name  string
		start string
		end   string
	}{
		{
			name:  "account selection",
			start: "func (r *ProviderRepository) ListAccountsForRoutingPool",
			end:   "func (r *ProviderRepository) FindSessionBindingInRoutingPool",
		},
		{
			name:  "model exposure",
			start: "func (r *ProviderRepository) ListExposedModelsForRoutingPools",
			end:   "func (r *ProviderRepository) ListEligibleAccountsForModel",
		},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			start := strings.Index(text, testCase.start)
			end := strings.Index(text, testCase.end)
			if start < 0 || end <= start {
				t.Fatalf("could not isolate %s query", testCase.name)
			}
			if strings.Contains(text[start:end], "c.access_token_expires_at") {
				t.Fatal("routing pool query filters access-token expiry before provider refresh can run")
			}
		})
	}
}

func TestProviderRepositorySubjectConflictPreservesSchedulingFields(t *testing.T) {
	repo, cleanup := newProviderRepositoryForTest(t)
	defer cleanup()

	first := saveProviderTestAccount(t, repo, provider.Account{
		Provider:              "openai",
		AccountType:           provider.AccountTypeCodexOAuth,
		Subject:               "same-subject",
		DisplayName:           "first",
		EncryptedAccessToken:  "first-access-token",
		EncryptedRefreshToken: "first-refresh-token",
		Enabled:               false,
		Priority:              7,
		Status:                provider.AccountStatusActive,
	})

	reconnected := saveProviderTestAccount(t, repo, provider.Account{
		Provider:              "openai",
		AccountType:           provider.AccountTypeCodexOAuth,
		Subject:               "same-subject",
		DisplayName:           "second",
		EncryptedAccessToken:  "second-access-token",
		EncryptedRefreshToken: "second-refresh-token",
		Enabled:               true,
		Priority:              100,
		Status:                provider.AccountStatusActive,
	})

	if reconnected.ID != first.ID {
		t.Fatalf("reconnected ID = %d, want existing account ID %d", reconnected.ID, first.ID)
	}
	if reconnected.Enabled || reconnected.Priority != 7 {
		t.Fatalf("scheduling = enabled %v priority %d, want false/7", reconnected.Enabled, reconnected.Priority)
	}
	if reconnected.EncryptedAccessToken != "second-access-token" || reconnected.EncryptedRefreshToken != "second-refresh-token" {
		t.Fatalf("tokens = (%q, %q), want updated reconnect tokens", reconnected.EncryptedAccessToken, reconnected.EncryptedRefreshToken)
	}
}

func TestReplaceAccountModelsLocksParentAccountRow(t *testing.T) {
	source, err := os.ReadFile("provider.go")
	if err != nil {
		t.Fatalf("ReadFile provider.go returned error: %v", err)
	}
	sql := strings.ToUpper(string(source))
	if !strings.Contains(sql, "SELECT ID\n\t\tFROM PROVIDER_ACCOUNTS\n\t\tWHERE PROVIDER = $1\n\t\t\tAND ID = $2\n\t\tFOR UPDATE") {
		t.Fatal("ReplaceAccountModels must lock the parent provider_accounts row before deleting and inserting model rows")
	}
}

func TestListAccountModelsChecksParentAccountExists(t *testing.T) {
	source, err := os.ReadFile("provider.go")
	if err != nil {
		t.Fatalf("ReadFile provider.go returned error: %v", err)
	}
	sql := strings.ToUpper(string(source))
	if !strings.Contains(sql, "SELECT 1\n\t\tFROM PROVIDER_ACCOUNTS\n\t\tWHERE PROVIDER = $1\n\t\t\tAND ID = $2") {
		t.Fatal("ListAccountModels must check provider_accounts before returning model rows")
	}
}

func TestHasEnabledAccountsChecksEnabledProviderRows(t *testing.T) {
	source, err := os.ReadFile("provider.go")
	if err != nil {
		t.Fatalf("ReadFile provider.go returned error: %v", err)
	}
	sql := strings.ToUpper(string(source))
	if !strings.Contains(sql, "SELECT EXISTS") ||
		!strings.Contains(sql, "FROM PROVIDER_ACCOUNTS") ||
		!strings.Contains(sql, "WHERE PROVIDER = $1") ||
		!strings.Contains(sql, "AND ENABLED = TRUE") {
		t.Fatal("HasEnabledAccounts must check for enabled provider account rows")
	}
}

func TestFindFingerprintProfileByIDOnlyReturnsEnabledProfiles(t *testing.T) {
	source, err := os.ReadFile("provider.go")
	if err != nil {
		t.Fatalf("ReadFile provider.go returned error: %v", err)
	}
	sql := strings.ToUpper(string(source))
	if !strings.Contains(sql, "FUNC (R *PROVIDERREPOSITORY) FINDFINGERPRINTPROFILEBYID") ||
		!strings.Contains(sql, "FROM FINGERPRINT_PROFILES") ||
		!strings.Contains(sql, "WHERE ID = $1 AND ENABLED = TRUE") {
		t.Fatal("FindFingerprintProfileByID must only return enabled fingerprint profiles")
	}
}

func TestEnsureDefaultCodexFingerprintProfileUsesSystemKey(t *testing.T) {
	source, err := os.ReadFile("provider.go")
	if err != nil {
		t.Fatalf("ReadFile provider.go returned error: %v", err)
	}
	sql := string(source)
	for _, want := range []string{
		"func (r *ProviderRepository) EnsureDefaultCodexFingerprintProfile",
		"provider.DefaultCodexFingerprintSystemKey",
		"ON CONFLICT (system_key) WHERE system_key <> ''",
		"name = EXCLUDED.name",
		"user_agent = EXCLUDED.user_agent",
		"headers_json = EXCLUDED.headers_json",
		"enabled = true",
		"RETURNING id",
	} {
		if !strings.Contains(sql, want) {
			t.Fatalf("provider store source missing %q", want)
		}
	}
}

func TestSystemFingerprintProfilesAreReadOnly(t *testing.T) {
	source, err := os.ReadFile("fingerprint.go")
	if err != nil {
		t.Fatalf("ReadFile fingerprint.go returned error: %v", err)
	}
	sql := string(source)
	for _, want := range []string{
		"WHERE id = $1 AND system_key = ''",
		"DELETE FROM fingerprint_profiles WHERE id = $1 AND system_key = ''",
		"ORDER BY CASE WHEN system_key <> '' THEN 0 ELSE 1 END",
	} {
		if !strings.Contains(sql, want) {
			t.Fatalf("fingerprint store source missing %q", want)
		}
	}
}

func TestProviderRepositorySavesAPIUpstreamAccount(t *testing.T) {
	repo, cleanup := newProviderRepositoryForTest(t)
	defer cleanup()

	ctx := context.Background()
	saved := saveProviderTestAccount(t, repo, provider.Account{
		Provider:    "openai",
		AccountType: provider.AccountTypeAPIUpstream,
		Subject:     "https://upstream.example.test",
		DisplayName: "API Upstream",
		Enabled:     true,
		Priority:    5,
		Status:      provider.AccountStatusActive,
		Credential: provider.AccountCredential{
			CredentialType:  provider.CredentialTypeAPIKey,
			EncryptedAPIKey: "encrypted-api-key",
			BaseURL:         "https://upstream.example.test/v1",
		},
	})

	if saved.AccountType != provider.AccountTypeAPIUpstream {
		t.Fatalf("saved AccountType = %q, want api upstream", saved.AccountType)
	}
	if saved.Credential.CredentialType != provider.CredentialTypeAPIKey || saved.Credential.EncryptedAPIKey != "encrypted-api-key" {
		t.Fatalf("saved credential = %+v, want api key credential", saved.Credential)
	}

	found, err := repo.FindAccountByID(ctx, "openai", saved.ID)
	if err != nil {
		t.Fatalf("FindAccountByID returned error: %v", err)
	}
	if found.AccountType != provider.AccountTypeAPIUpstream {
		t.Fatalf("found AccountType = %q, want api upstream", found.AccountType)
	}
	if found.Credential.CredentialType != provider.CredentialTypeAPIKey || found.Credential.BaseURL != "https://upstream.example.test/v1" {
		t.Fatalf("found credential = %+v, want api key base URL", found.Credential)
	}
}

func TestProviderRepositoryUpsertsAndFindsSessionBinding(t *testing.T) {
	repo, cleanup := newProviderRepositoryForTest(t)
	defer cleanup()

	ctx := context.Background()
	saved := saveProviderTestAccount(t, repo, provider.Account{
		Provider:              "openai",
		AccountType:           provider.AccountTypeCodexOAuth,
		Subject:               "session-binding-account",
		DisplayName:           "Session Binding Account",
		EncryptedAccessToken:  "access-token",
		EncryptedRefreshToken: "refresh-token",
		Enabled:               true,
		Priority:              5,
		Status:                provider.AccountStatusActive,
	})

	if err := repo.UpsertSessionBinding(ctx, "openai", "gpt-5", "workspace-123", saved.ID); err != nil {
		t.Fatalf("UpsertSessionBinding returned error: %v", err)
	}
	binding, err := repo.FindSessionBinding(ctx, "openai", "gpt-5", "workspace-123")
	if err != nil {
		t.Fatalf("FindSessionBinding returned error: %v", err)
	}
	if binding.AccountID != saved.ID || binding.Model != "gpt-5" || binding.SessionID != "workspace-123" {
		t.Fatalf("binding = %+v, want saved account binding", binding)
	}
}

func TestProviderRepositoryRoutingPoolSelectionAndBinding(t *testing.T) {
	repo, cleanup := newProviderRepositoryForTest(t)
	defer cleanup()

	ctx := context.Background()
	global := saveProviderTestAccount(t, repo, provider.Account{
		Provider:              "openai",
		AccountType:           provider.AccountTypeCodexOAuth,
		Subject:               "global-account",
		DisplayName:           "Global Account",
		EncryptedAccessToken:  "global-token",
		EncryptedRefreshToken: "refresh-token",
		Enabled:               true,
		Priority:              1,
		Status:                provider.AccountStatusActive,
	})
	expiredAt := time.Now().Add(-time.Minute)
	pooled := saveProviderTestAccount(t, repo, provider.Account{
		Provider:              "openai",
		AccountType:           provider.AccountTypeCodexOAuth,
		Subject:               "pooled-account",
		DisplayName:           "Pooled Account",
		EncryptedAccessToken:  "pool-token",
		EncryptedRefreshToken: "refresh-token",
		AccessTokenExpiresAt:  &expiredAt,
		Enabled:               true,
		Priority:              50,
		Status:                provider.AccountStatusActive,
	})
	if _, err := repo.ReplaceAccountModels(ctx, "openai", global.ID, []provider.AccountModelInput{{Model: "gpt-5", Enabled: true}}); err != nil {
		t.Fatalf("ReplaceAccountModels global returned error: %v", err)
	}
	if _, err := repo.ReplaceAccountModels(ctx, "openai", pooled.ID, []provider.AccountModelInput{{Model: "gpt-5", Enabled: true}}); err != nil {
		t.Fatalf("ReplaceAccountModels pooled returned error: %v", err)
	}

	fallbackPoolID := insertProviderRoutingPool(t, repo.pool, "secondary", pooled.ID)
	poolID := insertProviderRoutingPoolWithFallback(t, repo.pool, "primary", pooled.ID, &fallbackPoolID)
	pool, err := repo.FindRoutingPool(ctx, poolID)
	if err != nil {
		t.Fatalf("FindRoutingPool returned error: %v", err)
	}
	if pool.ID != poolID || pool.Name != "primary" || !pool.Enabled {
		t.Fatalf("pool = %+v, want primary", pool)
	}
	if pool.FallbackPoolID == nil || *pool.FallbackPoolID != fallbackPoolID {
		t.Fatalf("fallback pool ID = %v, want %d", pool.FallbackPoolID, fallbackPoolID)
	}

	accounts, err := repo.ListAccountsForRoutingPool(ctx, "openai", poolID, "gpt-5", nil, time.Now())
	if err != nil {
		t.Fatalf("ListAccountsForRoutingPool returned error: %v", err)
	}
	if got := accountIDs(accounts); !reflect.DeepEqual(got, []int64{pooled.ID}) {
		t.Fatalf("routing pool accounts = %+v, want only pooled account %d", got, pooled.ID)
	}

	if err := repo.UpsertSessionBindingInRoutingPool(ctx, "openai", poolID, "gpt-5", "workspace-123", pooled.ID); err != nil {
		t.Fatalf("UpsertSessionBindingInRoutingPool returned error: %v", err)
	}
	binding, err := repo.FindSessionBindingInRoutingPool(ctx, "openai", poolID, "gpt-5", "workspace-123")
	if err != nil {
		t.Fatalf("FindSessionBindingInRoutingPool returned error: %v", err)
	}
	if binding.AccountID != pooled.ID {
		t.Fatalf("pool binding account = %d, want %d", binding.AccountID, pooled.ID)
	}
	if _, err := repo.FindSessionBinding(ctx, "openai", "gpt-5", "workspace-123"); !errors.Is(err, provider.ErrSessionBindingNotFound) {
		t.Fatalf("global binding error = %v, want ErrSessionBindingNotFound", err)
	}
}

func TestProviderRepositoryRoutingPoolHasAccounts(t *testing.T) {
	repo, cleanup := newProviderRepositoryForTest(t)
	defer cleanup()

	ctx := context.Background()
	emptyPoolID := insertProviderRoutingPool(t, repo.pool, "empty", 0)
	hasAccounts, err := repo.RoutingPoolHasAccounts(ctx, emptyPoolID)
	if err != nil {
		t.Fatalf("RoutingPoolHasAccounts empty returned error: %v", err)
	}
	if hasAccounts {
		t.Fatalf("RoutingPoolHasAccounts empty = true, want false")
	}

	account := saveProviderTestAccount(t, repo, provider.Account{
		Provider:              "openai",
		AccountType:           provider.AccountTypeCodexOAuth,
		Subject:               "routing-pool-member",
		DisplayName:           "Routing Pool Member",
		EncryptedAccessToken:  "member-token",
		EncryptedRefreshToken: "refresh-token",
		Enabled:               true,
		Priority:              1,
		Status:                provider.AccountStatusActive,
	})
	nonEmptyPoolID := insertProviderRoutingPool(t, repo.pool, "non-empty", account.ID)
	hasAccounts, err = repo.RoutingPoolHasAccounts(ctx, nonEmptyPoolID)
	if err != nil {
		t.Fatalf("RoutingPoolHasAccounts non-empty returned error: %v", err)
	}
	if !hasAccounts {
		t.Fatalf("RoutingPoolHasAccounts non-empty = false, want true")
	}
}

func TestProviderRepositoryListExposedModelsForRoutingPools(t *testing.T) {
	repo, cleanup := newProviderRepositoryForTest(t)
	defer cleanup()

	ctx := context.Background()
	global := saveProviderTestAccount(t, repo, provider.Account{
		Provider:              "openai",
		AccountType:           provider.AccountTypeCodexOAuth,
		Subject:               "global-account",
		DisplayName:           "Global Account",
		EncryptedAccessToken:  "global-token",
		EncryptedRefreshToken: "refresh-token",
		Enabled:               true,
		Priority:              1,
		Status:                provider.AccountStatusActive,
	})
	pooled := saveProviderTestAccount(t, repo, provider.Account{
		Provider:              "openai",
		AccountType:           provider.AccountTypeCodexOAuth,
		Subject:               "pooled-account",
		DisplayName:           "Pooled Account",
		EncryptedAccessToken:  "pool-token",
		EncryptedRefreshToken: "refresh-token",
		Enabled:               true,
		Priority:              1,
		Status:                provider.AccountStatusActive,
	})
	if _, err := repo.ReplaceAccountModels(ctx, "openai", global.ID, []provider.AccountModelInput{{Model: "global-only", Enabled: true}}); err != nil {
		t.Fatalf("ReplaceAccountModels global returned error: %v", err)
	}
	if _, err := repo.ReplaceAccountModels(ctx, "openai", pooled.ID, []provider.AccountModelInput{{Model: "gpt-5", Enabled: true}}); err != nil {
		t.Fatalf("ReplaceAccountModels pooled returned error: %v", err)
	}
	poolID := insertProviderRoutingPool(t, repo.pool, "primary", pooled.ID)

	models, err := repo.ListExposedModelsForRoutingPools(ctx, "openai", []int64{poolID})
	if err != nil {
		t.Fatalf("ListExposedModelsForRoutingPools returned error: %v", err)
	}
	if got := providerExposedModelIDs(models); !reflect.DeepEqual(got, []string{"gpt-5"}) {
		t.Fatalf("models = %+v, want only pooled gpt-5", got)
	}
}

func TestProviderRepositoryUpdatesAPIUpstreamCredential(t *testing.T) {
	repo, cleanup := newProviderRepositoryForTest(t)
	defer cleanup()

	ctx := context.Background()
	saved := saveProviderTestAccount(t, repo, provider.Account{
		Provider:    "openai",
		AccountType: provider.AccountTypeAPIUpstream,
		Subject:     "https://upstream.example.test",
		DisplayName: "API Upstream",
		Enabled:     true,
		Priority:    5,
		Status:      provider.AccountStatusActive,
		Credential: provider.AccountCredential{
			CredentialType:  provider.CredentialTypeAPIKey,
			EncryptedAPIKey: "old-encrypted-api-key",
			BaseURL:         "https://old.example.test",
		},
	})
	baseURL := "https://new.example.test"
	encryptedAPIKey := "new-encrypted-api-key"

	updated, err := repo.UpdateAccount(ctx, "openai", saved.ID, provider.AccountUpdate{
		APIUpstreamBaseURL:         &baseURL,
		EncryptedAPIUpstreamAPIKey: &encryptedAPIKey,
	})
	if err != nil {
		t.Fatalf("UpdateAccount returned error: %v", err)
	}
	if updated.Credential.BaseURL != "https://new.example.test" || updated.Credential.EncryptedAPIKey != encryptedAPIKey {
		t.Fatalf("updated credential = %+v, want new base URL and encrypted key", updated.Credential)
	}

	found, err := repo.FindAccountByID(ctx, "openai", saved.ID)
	if err != nil {
		t.Fatalf("FindAccountByID returned error: %v", err)
	}
	if found.Credential.BaseURL != "https://new.example.test" || found.Credential.EncryptedAPIKey != encryptedAPIKey {
		t.Fatalf("found credential = %+v, want persisted update", found.Credential)
	}
}

func TestProviderRepositoryUpdatesAccountLoadFactor(t *testing.T) {
	repo, cleanup := newProviderRepositoryForTest(t)
	defer cleanup()

	ctx := context.Background()
	saved := saveProviderTestAccount(t, repo, provider.Account{
		Provider:              "openai",
		Subject:               "load-factor-account",
		DisplayName:           "Load Factor Account",
		EncryptedAccessToken:  "access",
		EncryptedRefreshToken: "refresh",
		Enabled:               true,
		Priority:              5,
		LoadFactor:            1,
		Status:                provider.AccountStatusActive,
	})
	loadFactor := 10

	updated, err := repo.UpdateAccount(ctx, "openai", saved.ID, provider.AccountUpdate{LoadFactor: &loadFactor})
	if err != nil {
		t.Fatalf("UpdateAccount returned error: %v", err)
	}
	if updated.LoadFactor != 10 {
		t.Fatalf("updated load factor = %d, want 10", updated.LoadFactor)
	}

	found, err := repo.FindAccountByID(ctx, "openai", saved.ID)
	if err != nil {
		t.Fatalf("FindAccountByID returned error: %v", err)
	}
	if found.LoadFactor != 10 {
		t.Fatalf("found load factor = %d, want persisted 10", found.LoadFactor)
	}
}

func TestProviderRepositoryRecordAccountTestResult(t *testing.T) {
	repo, cleanup := newProviderRepositoryForTest(t)
	defer cleanup()

	ctx := context.Background()
	saved := saveProviderTestAccount(t, repo, provider.Account{
		Provider:              "openai",
		Subject:               "probe-result",
		DisplayName:           "Probe Result",
		EncryptedAccessToken:  "access",
		EncryptedRefreshToken: "refresh",
		Enabled:               true,
		Status:                provider.AccountStatusActive,
	})
	firstCheckedAt := time.Now().UTC().Truncate(time.Microsecond)
	secondCheckedAt := firstCheckedAt.Add(time.Minute)

	if err := repo.RecordAccountTestResult(ctx, "openai", saved.ID, provider.AccountTestStatusFailed, "quota window", firstCheckedAt); err != nil {
		t.Fatalf("RecordAccountTestResult returned error: %v", err)
	}
	if err := repo.RecordAccountTestResult(ctx, "openai", saved.ID, provider.AccountTestStatusPassed, "", secondCheckedAt); err != nil {
		t.Fatalf("RecordAccountTestResult second result returned error: %v", err)
	}
	found, err := repo.FindAccountByID(ctx, "openai", saved.ID)
	if err != nil {
		t.Fatalf("FindAccountByID returned error: %v", err)
	}

	if found.LastTestAt == nil || !found.LastTestAt.Equal(secondCheckedAt) {
		t.Fatalf("LastTestAt = %v, want %v", found.LastTestAt, secondCheckedAt)
	}
	if found.LastTestStatus != provider.AccountTestStatusPassed || found.LastTestError != "" {
		t.Fatalf("test result = status:%q error:%q, want passed/empty", found.LastTestStatus, found.LastTestError)
	}

	results, err := repo.ListAccountTestResults(ctx, "openai", saved.ID, 10)
	if err != nil {
		t.Fatalf("ListAccountTestResults returned error: %v", err)
	}
	if len(results) != 2 {
		t.Fatalf("history result count = %d, want 2", len(results))
	}
	if results[0].AccountID != saved.ID || results[0].Provider != "openai" || results[0].Status != provider.AccountTestStatusPassed || results[0].Message != "" || !results[0].CheckedAt.Equal(secondCheckedAt) {
		t.Fatalf("newest result = %+v, want passed result at %v", results[0], secondCheckedAt)
	}
	if results[1].AccountID != saved.ID || results[1].Provider != "openai" || results[1].Status != provider.AccountTestStatusFailed || results[1].Message != "quota window" || !results[1].CheckedAt.Equal(firstCheckedAt) {
		t.Fatalf("oldest result = %+v, want failed result at %v", results[1], firstCheckedAt)
	}
}

func TestMarkAccountUsedClearsTemporaryFailureStateColumns(t *testing.T) {
	source, err := os.ReadFile("provider.go")
	if err != nil {
		t.Fatalf("ReadFile provider.go returned error: %v", err)
	}
	sql := strings.ToUpper(string(source))
	for _, required := range []string{
		"LAST_ERROR = ''",
		"LAST_ERROR_AT = NULL",
		"STATUS = 'ACTIVE'",
		"STATUS_REASON = ''",
		"FAILURE_COUNT = 0",
		"CIRCUIT_OPEN_UNTIL = NULL",
		"RATE_LIMITED_UNTIL = NULL",
		"LAST_REFRESH_ERROR = ''",
		"LAST_REFRESH_ERROR_AT = NULL",
	} {
		if !strings.Contains(sql, required) {
			t.Fatalf("MarkAccountUsed must clear temporary failure state, missing %q", required)
		}
	}
}

func TestUpdateAccountClearStatusClearsLocalFailureStateColumns(t *testing.T) {
	source, err := os.ReadFile("provider.go")
	if err != nil {
		t.Fatalf("ReadFile provider.go returned error: %v", err)
	}
	sql := strings.ToUpper(string(source))
	for _, required := range []string{
		"LAST_ERROR = CASE WHEN $5 THEN '' ELSE LAST_ERROR END",
		"LAST_ERROR_AT = CASE WHEN $5 THEN NULL ELSE LAST_ERROR_AT END",
		"STATUS = CASE WHEN $5 THEN 'ACTIVE' ELSE STATUS END",
		"STATUS_REASON = CASE WHEN $5 THEN '' ELSE STATUS_REASON END",
		"FAILURE_COUNT = CASE WHEN $5 THEN 0 ELSE FAILURE_COUNT END",
		"CIRCUIT_OPEN_UNTIL = CASE WHEN $5 THEN NULL ELSE CIRCUIT_OPEN_UNTIL END",
		"RATE_LIMITED_UNTIL = CASE WHEN $5 THEN NULL ELSE RATE_LIMITED_UNTIL END",
	} {
		if !strings.Contains(sql, required) {
			t.Fatalf("UpdateAccount ClearStatus must clear local failure state, missing %q", required)
		}
	}
	for _, preserved := range []string{
		"LAST_REFRESH_ERROR = CASE WHEN $5",
		"LAST_REFRESH_ERROR_AT = CASE WHEN $5",
	} {
		if strings.Contains(sql, preserved) {
			t.Fatalf("UpdateAccount ClearStatus must preserve refresh diagnostics, found %q", preserved)
		}
	}
}

func TestUpdateAccountCanClearFingerprintProfileColumn(t *testing.T) {
	source, err := os.ReadFile("provider.go")
	if err != nil {
		t.Fatalf("ReadFile provider.go returned error: %v", err)
	}
	sql := strings.ToUpper(string(source))
	if strings.Contains(sql, "FINGERPRINT_PROFILE_ID = COALESCE($10, FINGERPRINT_PROFILE_ID)") {
		t.Fatal("UpdateAccount must allow clearing fingerprint_profile_id instead of preserving it with COALESCE")
	}
	for _, want := range []string{
		"FINGERPRINT_PROFILE_ID = CASE",
		"WHEN $10 THEN $11",
		"ELSE FINGERPRINT_PROFILE_ID",
	} {
		if !strings.Contains(sql, want) {
			t.Fatalf("UpdateAccount must distinguish unset and null fingerprint updates, missing %q", want)
		}
	}
}

func TestProviderAccountSaveAndScanIncludeFingerprintProfileID(t *testing.T) {
	source, err := os.ReadFile("provider.go")
	if err != nil {
		t.Fatalf("ReadFile provider.go returned error: %v", err)
	}
	sql := string(source)
	for _, want := range []string{
		"a.fingerprint_profile_id",
		"&account.FingerprintProfileID",
		"fingerprint_profile_id = $19",
		"fingerprint_profile_id, updated_at",
		"account.FingerprintProfileID",
	} {
		if !strings.Contains(sql, want) {
			t.Fatalf("provider store source missing %q", want)
		}
	}
}

func TestUpdateAccountCanSetLocalAccountNameColumn(t *testing.T) {
	source, err := os.ReadFile("provider.go")
	if err != nil {
		t.Fatalf("ReadFile provider.go returned error: %v", err)
	}
	sql := strings.ToUpper(string(source))
	for _, required := range []string{
		"NAME = CASE WHEN $7 THEN $6 ELSE NAME END",
		"UPDATE.NAME",
	} {
		if !strings.Contains(sql, required) {
			t.Fatalf("UpdateAccount must support updating local account name, missing %q", required)
		}
	}
}

func TestReplaceAccountModelsNormalizesAndListsRows(t *testing.T) {
	repo, cleanup := newProviderRepositoryForTest(t)
	defer cleanup()

	ctx := context.Background()
	account := saveProviderTestAccount(t, repo, provider.Account{
		Provider:              "openai",
		Subject:               "acct-models",
		DisplayName:           "Models",
		EncryptedAccessToken:  "access",
		EncryptedRefreshToken: "refresh",
		Enabled:               true,
		Priority:              10,
		Status:                "active",
	})

	models, err := repo.ReplaceAccountModels(ctx, "openai", account.ID, []provider.AccountModelInput{
		{Model: " gpt-5 ", Enabled: true},
		{Model: "", Enabled: true},
		{Model: "gpt-5", Enabled: false},
		{Model: " codex-mini ", Enabled: false},
	})
	if err != nil {
		t.Fatalf("ReplaceAccountModels returned error: %v", err)
	}
	assertAccountModelRows(t, models, []accountModelWant{
		{Model: "codex-mini", Enabled: false},
		{Model: "gpt-5", Enabled: true},
	})
	for _, model := range models {
		if model.AccountID != account.ID || model.Provider != "openai" {
			t.Fatalf("model account/provider = (%d, %q), want (%d, openai)", model.AccountID, model.Provider, account.ID)
		}
		if model.Source != provider.AccountModelSourceManual {
			t.Fatalf("model source = %q, want manual", model.Source)
		}
	}

	listed, err := repo.ListAccountModels(ctx, "openai", account.ID)
	if err != nil {
		t.Fatalf("ListAccountModels returned error: %v", err)
	}
	assertAccountModelRows(t, listed, []accountModelWant{
		{Model: "codex-mini", Enabled: false},
		{Model: "gpt-5", Enabled: true},
	})

	_, err = repo.ReplaceAccountModels(ctx, "openai", 999999, []provider.AccountModelInput{{Model: "gpt-5", Enabled: true}})
	if !errors.Is(err, provider.ErrNotConnected) {
		t.Fatalf("ReplaceAccountModels missing account error = %v, want ErrNotConnected", err)
	}

	_, err = repo.ReplaceAccountModels(ctx, "openai", account.ID, []provider.AccountModelInput{{Model: strings.Repeat("x", 129), Enabled: true}})
	if !errors.Is(err, provider.ErrInvalidInput) {
		t.Fatalf("ReplaceAccountModels long model error = %v, want ErrInvalidInput", err)
	}

	tooMany := make([]provider.AccountModelInput, 101)
	for i := range tooMany {
		tooMany[i] = provider.AccountModelInput{Model: "model-" + strings.Repeat("x", i+1), Enabled: true}
	}
	_, err = repo.ReplaceAccountModels(ctx, "openai", account.ID, tooMany)
	if !errors.Is(err, provider.ErrInvalidInput) {
		t.Fatalf("ReplaceAccountModels too many models error = %v, want ErrInvalidInput", err)
	}
}

func TestListAccountModelsDistinguishesMissingAccountFromNoModels(t *testing.T) {
	repo, cleanup := newProviderRepositoryForTest(t)
	defer cleanup()

	ctx := context.Background()
	account := saveProviderTestAccount(t, repo, provider.Account{
		Provider:              "openai",
		Subject:               "acct-no-models",
		DisplayName:           "No Models",
		EncryptedAccessToken:  "access",
		EncryptedRefreshToken: "refresh",
		Enabled:               true,
		Priority:              10,
		Status:                "active",
	})

	models, err := repo.ListAccountModels(ctx, "openai", account.ID)
	if err != nil {
		t.Fatalf("ListAccountModels existing account returned error: %v", err)
	}
	if len(models) != 0 {
		t.Fatalf("models = %+v, want empty list", models)
	}

	_, err = repo.ListAccountModels(ctx, "openai", 9_223_372_036_854_775_000)
	if !errors.Is(err, provider.ErrNotConnected) {
		t.Fatalf("ListAccountModels missing account error = %v, want ErrNotConnected", err)
	}
}

func TestRecordAccountModelTestResultPersistsLatestFields(t *testing.T) {
	repo, cleanup := newProviderRepositoryForTest(t)
	defer cleanup()
	ctx := context.Background()
	account := saveProviderTestAccount(t, repo, provider.Account{
		Provider:              "openai",
		AccountType:           provider.AccountTypeCodexOAuth,
		Subject:               "model-test-result",
		DisplayName:           "Model Test Result",
		EncryptedAccessToken:  "access",
		EncryptedRefreshToken: "refresh",
		Enabled:               true,
		Status:                provider.AccountStatusActive,
	})
	if _, err := repo.ReplaceAccountModels(ctx, "openai", account.ID, []provider.AccountModelInput{{Model: "gpt-test", Enabled: false}}); err != nil {
		t.Fatalf("ReplaceAccountModels returned error: %v", err)
	}
	checkedAt := time.Date(2026, 7, 16, 10, 0, 0, 0, time.UTC)
	result := provider.AccountModelTestResult{
		AccountID:  account.ID,
		Model:      "gpt-test",
		Status:     provider.AccountTestStatusFailed,
		ErrorCode:  "rate_limited",
		HTTPStatus: 429,
		LatencyMS:  842,
		Message:    "quota window",
		CheckedAt:  checkedAt,
	}
	if err := repo.RecordAccountModelTestResult(ctx, "openai", result); err != nil {
		t.Fatalf("RecordAccountModelTestResult returned error: %v", err)
	}
	models, err := repo.ListAccountModels(ctx, "openai", account.ID)
	if err != nil {
		t.Fatalf("ListAccountModels returned error: %v", err)
	}
	if len(models) != 1 || models[0].LastTestAt == nil || !models[0].LastTestAt.Equal(checkedAt) || models[0].LastTestStatus != provider.AccountTestStatusFailed || models[0].LastTestHTTPStatus != 429 || models[0].LastTestLatencyMS != 842 || models[0].LastError != "quota window" {
		t.Fatalf("persisted model result = %+v", models)
	}
	result.Status = provider.AccountTestStatusPassed
	result.HTTPStatus = 200
	result.LatencyMS = 110
	result.Message = ""
	result.CheckedAt = checkedAt.Add(time.Minute)
	if err := repo.RecordAccountModelTestResult(ctx, "openai", result); err != nil {
		t.Fatalf("RecordAccountModelTestResult pass returned error: %v", err)
	}
	models, err = repo.ListAccountModels(ctx, "openai", account.ID)
	if err != nil {
		t.Fatalf("ListAccountModels after pass returned error: %v", err)
	}
	if models[0].LastTestStatus != provider.AccountTestStatusPassed || models[0].LastTestHTTPStatus != 200 || models[0].LastTestLatencyMS != 110 || models[0].LastError != "" {
		t.Fatalf("latest passed model result = %+v", models[0])
	}
	if err := repo.RecordAccountModelTestResult(ctx, "openai", provider.AccountModelTestResult{AccountID: account.ID, Model: "missing", CheckedAt: checkedAt}); !errors.Is(err, provider.ErrNotConnected) {
		t.Fatalf("missing model error = %v, want ErrNotConnected", err)
	}
}

func TestSyncAccountModelsPreservesManualRowsAndDisablesNewUpstreamRows(t *testing.T) {
	repo, cleanup := newProviderRepositoryForTest(t)
	defer cleanup()

	ctx := context.Background()
	account := saveProviderTestAccount(t, repo, provider.Account{
		Provider:              "openai",
		AccountType:           provider.AccountTypeAPIUpstream,
		Subject:               "sync-account",
		DisplayName:           "Sync Account",
		EncryptedAccessToken:  "access",
		EncryptedRefreshToken: "",
		Enabled:               true,
		Priority:              10,
		Status:                "active",
	})

	if _, err := repo.ReplaceAccountModels(ctx, "openai", account.ID, []provider.AccountModelInput{
		{Model: "manual-only", Enabled: true},
		{Model: "shared-model", Enabled: true},
	}); err != nil {
		t.Fatalf("ReplaceAccountModels returned error: %v", err)
	}

	models, summary, err := repo.SyncAccountModels(ctx, "openai", account.ID, []provider.AccountModelInput{
		{Model: " upstream-new ", Enabled: true},
		{Model: "shared-model", Enabled: true},
	}, time.Date(2026, 7, 1, 8, 0, 0, 0, time.UTC))
	if err != nil {
		t.Fatalf("SyncAccountModels returned error: %v", err)
	}

	if summary.Total != 2 || summary.New != 1 || summary.Preserved != 0 || summary.SkippedManual != 1 {
		t.Fatalf("summary = %+v, want total=2 new=1 preserved=0 skippedManual=1", summary)
	}
	assertAccountModelRows(t, models, []accountModelWant{
		{Model: "manual-only", Enabled: true},
		{Model: "shared-model", Enabled: true},
		{Model: "upstream-new", Enabled: false},
	})
	for _, model := range models {
		if model.Model == "upstream-new" {
			if model.Source != provider.AccountModelSourceUpstream || model.LastSeenAt == nil {
				t.Fatalf("upstream model = %+v, want upstream source with last seen", model)
			}
		}
		if model.Model == "shared-model" && model.Source != provider.AccountModelSourceManual {
			t.Fatalf("shared model source = %q, want manual", model.Source)
		}
	}
}

func TestSyncAccountModelsPreservesExistingUpstreamEnabledAndRemovesStaleRows(t *testing.T) {
	repo, cleanup := newProviderRepositoryForTest(t)
	defer cleanup()

	ctx := context.Background()
	account := saveProviderTestAccount(t, repo, provider.Account{
		Provider:              "openai",
		AccountType:           provider.AccountTypeAPIUpstream,
		Subject:               "sync-account-2",
		DisplayName:           "Sync Account 2",
		EncryptedAccessToken:  "access",
		EncryptedRefreshToken: "",
		Enabled:               true,
		Priority:              10,
		Status:                "active",
	})

	// Add upstream rows via direct ReplaceAccountModels (manual source), then sync first batch.
	if _, err := repo.ReplaceAccountModels(ctx, "openai", account.ID, []provider.AccountModelInput{
		{Model: "manual-stable", Enabled: true},
	}); err != nil {
		t.Fatalf("ReplaceAccountModels returned error: %v", err)
	}

	// First sync creates upstream rows: "alpha", "beta", "gamma"
	_, firstSummary, err := repo.SyncAccountModels(ctx, "openai", account.ID, []provider.AccountModelInput{
		{Model: "alpha", Enabled: false},
		{Model: "beta", Enabled: true},
		{Model: "gamma", Enabled: true},
	}, time.Date(2026, 7, 1, 8, 0, 0, 0, time.UTC))
	if err != nil {
		t.Fatalf("first SyncAccountModels returned error: %v", err)
	}
	if firstSummary.New != 3 || firstSummary.Total != 3 {
		t.Fatalf("first sync summary = %+v, want new=3 total=3", firstSummary)
	}

	// Enable beta in the database between syncs, simulating user toggle.
	// The first sync stores all new upstream rows as disabled; the second sync
	// must preserve the user's enabled=true choice from the database.
	if _, err := repo.pool.Exec(ctx, `
		UPDATE provider_account_models
		SET enabled = true
		WHERE provider = $1 AND account_id = $2 AND model = $3
	`, "openai", account.ID, "beta"); err != nil {
		t.Fatalf("enabling beta between syncs returned error: %v", err)
	}
	// Second sync removes "gamma" (stale), preserves "alpha" and "beta" enabled state.
	models, summary, err := repo.SyncAccountModels(ctx, "openai", account.ID, []provider.AccountModelInput{
		{Model: "alpha", Enabled: false},
		{Model: "beta", Enabled: true},
	}, time.Date(2026, 7, 2, 8, 0, 0, 0, time.UTC))
	if err != nil {
		t.Fatalf("second SyncAccountModels returned error: %v", err)
	}

	if summary.Total != 2 || summary.Preserved != 2 || summary.New != 0 || summary.SkippedManual != 0 {
		t.Fatalf("second sync summary = %+v, want preserved=2", summary)
	}
	assertAccountModelRows(t, models, []accountModelWant{
		{Model: "alpha", Enabled: false},
		{Model: "beta", Enabled: true},
		{Model: "manual-stable", Enabled: true},
	})
	for _, model := range models {
		if model.Model == "alpha" || model.Model == "beta" {
			if model.Source != provider.AccountModelSourceUpstream {
				t.Fatalf("%s source = %q, want upstream", model.Model, model.Source)
			}
		}
	}
	// "gamma" must be gone
	for _, model := range models {
		if model.Model == "gamma" {
			t.Fatal("gamma should have been deleted as stale")
		}
	}
}

func TestSyncAccountModelsRejectsMissingAccount(t *testing.T) {
	repo, cleanup := newProviderRepositoryForTest(t)
	defer cleanup()

	ctx := context.Background()
	_, _, err := repo.SyncAccountModels(ctx, "openai", 999999, []provider.AccountModelInput{
		{Model: "gpt-5", Enabled: true},
	}, time.Now())
	if !errors.Is(err, provider.ErrNotConnected) {
		t.Fatalf("SyncAccountModels missing account error = %v, want ErrNotConnected", err)
	}
}

func TestListEligibleAccountsForModelFiltersAndOrders(t *testing.T) {
	repo, cleanup := newProviderRepositoryForTest(t)
	defer cleanup()

	ctx := context.Background()
	now := time.Date(2026, 6, 22, 12, 0, 0, 0, time.UTC)
	older := now.Add(-2 * time.Hour)
	past := now.Add(-time.Minute)
	later := now.Add(time.Hour)

	first := saveProviderTestAccount(t, repo, provider.Account{
		Provider:              "openai",
		Subject:               "eligible-first",
		DisplayName:           "Eligible First",
		EncryptedAccessToken:  "access",
		EncryptedRefreshToken: "refresh",
		Enabled:               true,
		Priority:              10,
		LastUsedAt:            &older,
		Status:                "active",
	})
	if err := repo.MarkAccountUsed(ctx, "openai", first.ID, older); err != nil {
		t.Fatalf("MarkAccountUsed returned error: %v", err)
	}
	nullLastUsed := saveProviderTestAccount(t, repo, provider.Account{
		Provider:              "openai",
		Subject:               "eligible-null",
		DisplayName:           "Eligible Null",
		EncryptedAccessToken:  "access",
		EncryptedRefreshToken: "refresh",
		Enabled:               true,
		Priority:              10,
		Status:                "active",
	})
	higherPriority := saveProviderTestAccount(t, repo, provider.Account{
		Provider:              "openai",
		Subject:               "eligible-priority",
		DisplayName:           "Eligible Priority",
		EncryptedAccessToken:  "access",
		EncryptedRefreshToken: "refresh",
		Enabled:               true,
		Priority:              5,
		Status:                "active",
	})
	errorSameUse := saveProviderTestAccount(t, repo, provider.Account{
		Provider:              "openai",
		Subject:               "error-same-use",
		DisplayName:           "Error Same Use",
		EncryptedAccessToken:  "access",
		EncryptedRefreshToken: "refresh",
		Enabled:               true,
		Priority:              8,
		Status:                "active",
	})
	if err := repo.MarkAccountUsed(ctx, "openai", errorSameUse.ID, older); err != nil {
		t.Fatalf("MarkAccountUsed error account returned error: %v", err)
	}
	if err := repo.MarkAccountError(ctx, "openai", errorSameUse.ID, "temporary failure", now.Add(-time.Minute)); err != nil {
		t.Fatalf("MarkAccountError returned error: %v", err)
	}
	cleanSameUse := saveProviderTestAccount(t, repo, provider.Account{
		Provider:              "openai",
		Subject:               "clean-same-use",
		DisplayName:           "Clean Same Use",
		EncryptedAccessToken:  "access",
		EncryptedRefreshToken: "refresh",
		Enabled:               true,
		Priority:              8,
		Status:                "active",
	})
	if err := repo.MarkAccountUsed(ctx, "openai", cleanSameUse.ID, older); err != nil {
		t.Fatalf("MarkAccountUsed clean returned error: %v", err)
	}
	disabledAccount := saveProviderTestAccount(t, repo, provider.Account{
		Provider:              "openai",
		Subject:               "disabled-account",
		DisplayName:           "Disabled",
		EncryptedAccessToken:  "access",
		EncryptedRefreshToken: "refresh",
		Enabled:               false,
		Priority:              1,
		Status:                "active",
	})
	disabledModel := saveProviderTestAccount(t, repo, provider.Account{
		Provider:              "openai",
		Subject:               "disabled-model",
		DisplayName:           "Disabled Model",
		EncryptedAccessToken:  "access",
		EncryptedRefreshToken: "refresh",
		Enabled:               true,
		Priority:              1,
		Status:                "active",
	})
	rateLimitedFuture := saveProviderTestAccount(t, repo, provider.Account{
		Provider:              "openai",
		Subject:               "rate-limited-future",
		DisplayName:           "Rate Limited Future",
		EncryptedAccessToken:  "access",
		EncryptedRefreshToken: "refresh",
		Enabled:               true,
		Priority:              1,
		Status:                "rate_limited",
		RateLimitedUntil:      &later,
	})
	rateLimitedNull := saveProviderTestAccount(t, repo, provider.Account{
		Provider:              "openai",
		Subject:               "rate-limited-null",
		DisplayName:           "Rate Limited Null",
		EncryptedAccessToken:  "access",
		EncryptedRefreshToken: "refresh",
		Enabled:               true,
		Priority:              1,
		Status:                "rate_limited",
	})
	rateLimitedPast := saveProviderTestAccount(t, repo, provider.Account{
		Provider:              "openai",
		Subject:               "rate-limited-past",
		DisplayName:           "Rate Limited Past",
		EncryptedAccessToken:  "access",
		EncryptedRefreshToken: "refresh",
		Enabled:               true,
		Priority:              6,
		Status:                "rate_limited",
		RateLimitedUntil:      &past,
	})
	highLoadSamePriority := saveProviderTestAccount(t, repo, provider.Account{
		Provider:              "openai",
		Subject:               "high-load-same-priority",
		DisplayName:           "High Load Same Priority",
		EncryptedAccessToken:  "access",
		EncryptedRefreshToken: "refresh",
		Enabled:               true,
		Priority:              6,
		LoadFactor:            20,
		Status:                "active",
	})
	circuitOpenFuture := saveProviderTestAccount(t, repo, provider.Account{
		Provider:              "openai",
		Subject:               "circuit-open-future",
		DisplayName:           "Circuit Open Future",
		EncryptedAccessToken:  "access",
		EncryptedRefreshToken: "refresh",
		Enabled:               true,
		Priority:              1,
		Status:                "circuit_open",
		CircuitOpenUntil:      &later,
	})
	circuitOpenNull := saveProviderTestAccount(t, repo, provider.Account{
		Provider:              "openai",
		Subject:               "circuit-open-null",
		DisplayName:           "Circuit Open Null",
		EncryptedAccessToken:  "access",
		EncryptedRefreshToken: "refresh",
		Enabled:               true,
		Priority:              1,
		Status:                "circuit_open",
	})
	circuitOpenPast := saveProviderTestAccount(t, repo, provider.Account{
		Provider:              "openai",
		Subject:               "circuit-open-past",
		DisplayName:           "Circuit Open Past",
		EncryptedAccessToken:  "access",
		EncryptedRefreshToken: "refresh",
		Enabled:               true,
		Priority:              7,
		Status:                "circuit_open",
		CircuitOpenUntil:      &past,
	})
	expired := saveProviderTestAccount(t, repo, provider.Account{
		Provider:              "openai",
		Subject:               "expired",
		DisplayName:           "Expired",
		EncryptedAccessToken:  "access",
		EncryptedRefreshToken: "refresh",
		AccessTokenExpiresAt:  &older,
		Enabled:               true,
		Priority:              1,
		Status:                "active",
	})
	expiredStatus := saveProviderTestAccount(t, repo, provider.Account{
		Provider:              "openai",
		Subject:               "expired-status",
		DisplayName:           "Expired Status",
		EncryptedAccessToken:  "access",
		EncryptedRefreshToken: "refresh",
		Enabled:               true,
		Priority:              1,
		Status:                "expired",
	})
	unknownStatus := saveProviderTestAccount(t, repo, provider.Account{
		Provider:              "openai",
		Subject:               "unknown-status",
		DisplayName:           "Unknown Status",
		EncryptedAccessToken:  "access",
		EncryptedRefreshToken: "refresh",
		Enabled:               true,
		Priority:              1,
		Status:                "needs_review",
	})

	for _, account := range []provider.Account{
		first,
		nullLastUsed,
		higherPriority,
		cleanSameUse,
		errorSameUse,
		disabledAccount,
		rateLimitedFuture,
		rateLimitedNull,
		rateLimitedPast,
		highLoadSamePriority,
		circuitOpenFuture,
		circuitOpenNull,
		circuitOpenPast,
		expired,
		expiredStatus,
		unknownStatus,
	} {
		if _, err := repo.ReplaceAccountModels(ctx, "openai", account.ID, []provider.AccountModelInput{{Model: "gpt-5", Enabled: true}}); err != nil {
			t.Fatalf("ReplaceAccountModels(%d) returned error: %v", account.ID, err)
		}
	}
	if _, err := repo.ReplaceAccountModels(ctx, "openai", disabledModel.ID, []provider.AccountModelInput{{Model: "gpt-5", Enabled: false}}); err != nil {
		t.Fatalf("ReplaceAccountModels(disabled model) returned error: %v", err)
	}
	if _, err := repo.ReplaceAccountModels(ctx, "openai", first.ID, []provider.AccountModelInput{{Model: "gpt-5", Enabled: true}, {Model: "codex-mini", Enabled: true}}); err != nil {
		t.Fatalf("ReplaceAccountModels(first extra model) returned error: %v", err)
	}

	eligible, err := repo.ListEligibleAccountsForModel(ctx, "openai", "gpt-5", []int64{higherPriority.ID}, now)
	if err != nil {
		t.Fatalf("ListEligibleAccountsForModel returned error: %v", err)
	}
	gotIDs := accountIDs(eligible)
	wantIDs := []int64{expired.ID, highLoadSamePriority.ID, rateLimitedPast.ID, circuitOpenPast.ID, cleanSameUse.ID, errorSameUse.ID, nullLastUsed.ID, first.ID}
	if !reflect.DeepEqual(gotIDs, wantIDs) {
		t.Fatalf("eligible account IDs = %v, want %v", gotIDs, wantIDs)
	}

	codexEligible, err := repo.ListEligibleAccountsForModel(ctx, "openai", "codex-mini", nil, now)
	if err != nil {
		t.Fatalf("ListEligibleAccountsForModel codex returned error: %v", err)
	}
	if gotIDs := accountIDs(codexEligible); !reflect.DeepEqual(gotIDs, []int64{first.ID}) {
		t.Fatalf("codex eligible account IDs = %v, want [%d]", gotIDs, first.ID)
	}
}

func TestProviderRepositoryListEligibleAccountsForModelUsesUnifiedTables(t *testing.T) {
	repo, cleanup := newProviderRepositoryForTest(t)
	defer cleanup()

	ctx := context.Background()
	now := time.Date(2026, 6, 22, 12, 0, 0, 0, time.UTC)
	oauthAccount := saveProviderTestAccount(t, repo, provider.Account{
		Provider:              "openai",
		AccountType:           provider.AccountTypeCodexOAuth,
		Subject:               "oauth-account",
		DisplayName:           "OAuth Account",
		EncryptedAccessToken:  "encrypted-access-token",
		EncryptedRefreshToken: "encrypted-refresh-token",
		Enabled:               true,
		Priority:              10,
		Status:                provider.AccountStatusActive,
	})
	apiUpstreamAccount := saveProviderTestAccount(t, repo, provider.Account{
		Provider:    "openai",
		AccountType: provider.AccountTypeAPIUpstream,
		Subject:     "https://upstream.example.test",
		DisplayName: "API Upstream",
		Enabled:     true,
		Priority:    1,
		Status:      provider.AccountStatusActive,
		Credential: provider.AccountCredential{
			CredentialType:  provider.CredentialTypeAPIKey,
			EncryptedAPIKey: "encrypted-api-key",
			BaseURL:         "https://upstream.example.test/v1",
		},
	})

	for _, account := range []provider.Account{oauthAccount, apiUpstreamAccount} {
		if _, err := repo.ReplaceAccountModels(ctx, "openai", account.ID, []provider.AccountModelInput{{Model: "gpt-5", Enabled: true}}); err != nil {
			t.Fatalf("ReplaceAccountModels(%d) returned error: %v", account.ID, err)
		}
	}

	eligible, err := repo.ListEligibleAccountsForModel(ctx, "openai", "gpt-5", nil, now)
	if err != nil {
		t.Fatalf("ListEligibleAccountsForModel returned error: %v", err)
	}
	gotIDs := accountIDs(eligible)
	wantIDs := []int64{apiUpstreamAccount.ID, oauthAccount.ID}
	if !reflect.DeepEqual(gotIDs, wantIDs) {
		t.Fatalf("eligible account IDs = %v, want %v", gotIDs, wantIDs)
	}
	if eligible[0].AccountType != provider.AccountTypeAPIUpstream || eligible[1].AccountType != provider.AccountTypeCodexOAuth {
		t.Fatalf("eligible account types = %q/%q, want api upstream then oauth", eligible[0].AccountType, eligible[1].AccountType)
	}
	if eligible[0].Credential.BaseURL != "https://upstream.example.test/v1" {
		t.Fatalf("eligible API upstream base URL = %q, want upstream URL", eligible[0].Credential.BaseURL)
	}
}

func TestListExposedModelsForRoutingPoolFiltersUnschedulableAccounts(t *testing.T) {
	repo, cleanup := newProviderRepositoryForTest(t)
	defer cleanup()

	ctx := context.Background()
	now := time.Now().UTC()
	past := now.Add(-time.Minute)
	later := now.Add(time.Hour)
	account := saveProviderTestAccount(t, repo, provider.Account{
		Provider:              "openai",
		Subject:               "exposed-enabled",
		DisplayName:           "Exposed",
		EncryptedAccessToken:  "access",
		EncryptedRefreshToken: "refresh",
		Enabled:               true,
		Priority:              10,
		Status:                "active",
	})
	disabledAccount := saveProviderTestAccount(t, repo, provider.Account{
		Provider:              "openai",
		Subject:               "exposed-disabled-account",
		DisplayName:           "Disabled",
		EncryptedAccessToken:  "access",
		EncryptedRefreshToken: "refresh",
		Enabled:               false,
		Priority:              10,
		Status:                "active",
	})
	expiredAccount := saveProviderTestAccount(t, repo, provider.Account{
		Provider:              "openai",
		Subject:               "exposed-expired",
		DisplayName:           "Expired",
		EncryptedAccessToken:  "access",
		EncryptedRefreshToken: "refresh",
		AccessTokenExpiresAt:  &past,
		Enabled:               true,
		Priority:              10,
		Status:                "active",
	})
	unknownStatus := saveProviderTestAccount(t, repo, provider.Account{
		Provider:              "openai",
		Subject:               "exposed-unknown",
		DisplayName:           "Unknown",
		EncryptedAccessToken:  "access",
		EncryptedRefreshToken: "refresh",
		Enabled:               true,
		Priority:              10,
		Status:                "needs_review",
	})
	rateLimitedNull := saveProviderTestAccount(t, repo, provider.Account{
		Provider:              "openai",
		Subject:               "exposed-rate-null",
		DisplayName:           "Rate Null",
		EncryptedAccessToken:  "access",
		EncryptedRefreshToken: "refresh",
		Enabled:               true,
		Priority:              10,
		Status:                "rate_limited",
	})
	rateLimitedFuture := saveProviderTestAccount(t, repo, provider.Account{
		Provider:              "openai",
		Subject:               "exposed-rate-future",
		DisplayName:           "Rate Future",
		EncryptedAccessToken:  "access",
		EncryptedRefreshToken: "refresh",
		Enabled:               true,
		Priority:              10,
		Status:                "rate_limited",
		RateLimitedUntil:      &later,
	})
	rateLimitedPast := saveProviderTestAccount(t, repo, provider.Account{
		Provider:              "openai",
		Subject:               "exposed-rate-past",
		DisplayName:           "Rate Past",
		EncryptedAccessToken:  "access",
		EncryptedRefreshToken: "refresh",
		Enabled:               true,
		Priority:              10,
		Status:                "rate_limited",
		RateLimitedUntil:      &past,
	})
	circuitOpenNull := saveProviderTestAccount(t, repo, provider.Account{
		Provider:              "openai",
		Subject:               "exposed-circuit-null",
		DisplayName:           "Circuit Null",
		EncryptedAccessToken:  "access",
		EncryptedRefreshToken: "refresh",
		Enabled:               true,
		Priority:              10,
		Status:                "circuit_open",
	})
	circuitOpenFuture := saveProviderTestAccount(t, repo, provider.Account{
		Provider:              "openai",
		Subject:               "exposed-circuit-future",
		DisplayName:           "Circuit Future",
		EncryptedAccessToken:  "access",
		EncryptedRefreshToken: "refresh",
		Enabled:               true,
		Priority:              10,
		Status:                "circuit_open",
		CircuitOpenUntil:      &later,
	})
	circuitOpenPast := saveProviderTestAccount(t, repo, provider.Account{
		Provider:              "openai",
		Subject:               "exposed-circuit-past",
		DisplayName:           "Circuit Past",
		EncryptedAccessToken:  "access",
		EncryptedRefreshToken: "refresh",
		Enabled:               true,
		Priority:              10,
		Status:                "circuit_open",
		CircuitOpenUntil:      &past,
	})

	if _, err := repo.ReplaceAccountModels(ctx, "openai", account.ID, []provider.AccountModelInput{
		{Model: "gpt-5", Enabled: true},
		{Model: "codex-mini", Enabled: true},
		{Model: "disabled-model", Enabled: false},
	}); err != nil {
		t.Fatalf("ReplaceAccountModels enabled account returned error: %v", err)
	}
	if _, err := repo.ReplaceAccountModels(ctx, "openai", disabledAccount.ID, []provider.AccountModelInput{
		{Model: "disabled-account-only", Enabled: true},
	}); err != nil {
		t.Fatalf("ReplaceAccountModels disabled account returned error: %v", err)
	}
	for _, testCase := range []struct {
		account provider.Account
		model   string
	}{
		{expiredAccount, "expired-only"},
		{unknownStatus, "unknown-status-only"},
		{rateLimitedNull, "rate-limited-null-only"},
		{rateLimitedFuture, "rate-limited-future-only"},
		{rateLimitedPast, "rate-limited-past"},
		{circuitOpenNull, "circuit-open-null-only"},
		{circuitOpenFuture, "circuit-open-future-only"},
		{circuitOpenPast, "circuit-open-past"},
	} {
		if _, err := repo.ReplaceAccountModels(ctx, "openai", testCase.account.ID, []provider.AccountModelInput{{Model: testCase.model, Enabled: true}}); err != nil {
			t.Fatalf("ReplaceAccountModels %s returned error: %v", testCase.model, err)
		}
	}
	poolID := insertProviderRoutingPool(t, repo.pool, "all accounts", account.ID)
	for _, pooledAccount := range []provider.Account{
		disabledAccount,
		expiredAccount,
		unknownStatus,
		rateLimitedNull,
		rateLimitedFuture,
		rateLimitedPast,
		circuitOpenNull,
		circuitOpenFuture,
		circuitOpenPast,
	} {
		if _, err := repo.pool.Exec(ctx, `INSERT INTO routing_pool_accounts (pool_id, account_id) VALUES ($1, $2)`, poolID, pooledAccount.ID); err != nil {
			t.Fatalf("insert routing pool account %d: %v", pooledAccount.ID, err)
		}
	}

	models, err := repo.ListExposedModelsForRoutingPools(ctx, "openai", []int64{poolID})
	if err != nil {
		t.Fatalf("ListExposedModels returned error: %v", err)
	}
	want := []provider.ExposedModel{
		{ID: "circuit-open-past", OwnedBy: "openai"},
		{ID: "codex-mini", OwnedBy: "openai"},
		{ID: "expired-only", OwnedBy: "openai"},
		{ID: "gpt-5", OwnedBy: "openai"},
		{ID: "rate-limited-past", OwnedBy: "openai"},
	}
	if !reflect.DeepEqual(models, want) {
		t.Fatalf("exposed models = %+v, want %+v", models, want)
	}
}

type accountModelWant struct {
	Model   string
	Enabled bool
}

func assertAccountModelRows(t *testing.T, got []provider.AccountModel, want []accountModelWant) {
	t.Helper()
	if len(got) != len(want) {
		t.Fatalf("model count = %d, want %d: %+v", len(got), len(want), got)
	}
	for i := range want {
		if got[i].Model != want[i].Model || got[i].Enabled != want[i].Enabled {
			t.Fatalf("model[%d] = (%q, %v), want (%q, %v)", i, got[i].Model, got[i].Enabled, want[i].Model, want[i].Enabled)
		}
	}
}

func accountIDs(accounts []provider.Account) []int64 {
	ids := make([]int64, 0, len(accounts))
	for _, account := range accounts {
		ids = append(ids, account.ID)
	}
	return ids
}

func providerExposedModelIDs(models []provider.ExposedModel) []string {
	ids := make([]string, 0, len(models))
	for _, model := range models {
		ids = append(ids, model.ID)
	}
	return ids
}

func saveProviderTestAccount(t *testing.T, repo *ProviderRepository, account provider.Account) provider.Account {
	t.Helper()
	saved, err := repo.SaveAccount(context.Background(), account)
	if err != nil {
		t.Fatalf("SaveAccount(%q) returned error: %v", account.Subject, err)
	}
	return saved
}

func insertProviderRoutingPool(t *testing.T, pool *pgxpool.Pool, name string, accountID int64) int64 {
	t.Helper()
	return insertProviderRoutingPoolWithFallback(t, pool, name, accountID, nil)
}

func insertProviderRoutingPoolWithFallback(t *testing.T, pool *pgxpool.Pool, name string, accountID int64, fallbackPoolID *int64) int64 {
	t.Helper()

	ctx := context.Background()
	var poolID int64
	if err := pool.QueryRow(ctx, `
		INSERT INTO routing_pools (name, description, enabled, fallback_pool_id)
		VALUES ($1, '', true, $2)
		RETURNING id
	`, name, fallbackPoolID).Scan(&poolID); err != nil {
		t.Fatalf("insert routing pool failed: %v", err)
	}
	if accountID <= 0 {
		return poolID
	}
	if _, err := pool.Exec(ctx, `
		INSERT INTO routing_pool_accounts (pool_id, account_id, priority)
		VALUES ($1, $2, 0)
	`, poolID, accountID); err != nil {
		t.Fatalf("insert routing pool account failed: %v", err)
	}
	return poolID
}

func newProviderRepositoryForTest(t *testing.T) (*ProviderRepository, func()) {
	t.Helper()
	dsn := os.Getenv("N2API_STORE_TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("set N2API_STORE_TEST_DATABASE_URL to run PostgreSQL store integration tests")
	}

	ctx := context.Background()
	pool, err := pgxpool.New(ctx, dsn)
	if err != nil {
		t.Fatalf("pgxpool.New returned error: %v", err)
	}
	if err := RunMigrations(ctx, pool); err != nil {
		pool.Close()
		t.Fatalf("RunMigrations returned error: %v", err)
	}
	if _, err := pool.Exec(ctx, truncateStoreTestDataSQL); err != nil {
		pool.Close()
		t.Fatalf("TRUNCATE returned error: %v", err)
	}
	return NewProviderRepository(pool), pool.Close
}

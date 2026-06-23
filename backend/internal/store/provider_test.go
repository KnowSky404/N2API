package store

import (
	"context"
	"errors"
	"os"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/KnowSky404/N2API/backend/internal/provider"
	"github.com/jackc/pgx/v5/pgxpool"
)

func TestProviderRepositoryImplementsInterface(t *testing.T) {
	var _ provider.Repository = (*ProviderRepository)(nil)
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
	wantIDs := []int64{expired.ID, rateLimitedPast.ID, circuitOpenPast.ID, cleanSameUse.ID, errorSameUse.ID, nullLastUsed.ID, first.ID}
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

func TestListExposedModelsFiltersByAllowedOrder(t *testing.T) {
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

	models, err := repo.ListExposedModels(ctx, "openai", []string{
		"codex-mini",
		"disabled-model",
		"expired-only",
		"unknown-status-only",
		"rate-limited-null-only",
		"rate-limited-future-only",
		"rate-limited-past",
		"circuit-open-null-only",
		"circuit-open-future-only",
		"circuit-open-past",
		"gpt-5",
		"missing",
		"codex-mini",
	})
	if err != nil {
		t.Fatalf("ListExposedModels returned error: %v", err)
	}
	want := []provider.ExposedModel{
		{ID: "codex-mini", OwnedBy: "openai"},
		{ID: "expired-only", OwnedBy: "openai"},
		{ID: "rate-limited-past", OwnedBy: "openai"},
		{ID: "circuit-open-past", OwnedBy: "openai"},
		{ID: "gpt-5", OwnedBy: "openai"},
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

func saveProviderTestAccount(t *testing.T, repo *ProviderRepository, account provider.Account) provider.Account {
	t.Helper()
	saved, err := repo.SaveAccount(context.Background(), account)
	if err != nil {
		t.Fatalf("SaveAccount(%q) returned error: %v", account.Subject, err)
	}
	return saved
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
	if _, err := pool.Exec(ctx, "TRUNCATE provider_account_models, provider_account_credentials, provider_accounts, oauth_account_models, oauth_accounts, oauth_states RESTART IDENTITY CASCADE"); err != nil {
		pool.Close()
		t.Fatalf("TRUNCATE returned error: %v", err)
	}
	return NewProviderRepository(pool), pool.Close
}

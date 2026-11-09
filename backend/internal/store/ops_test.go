package store

import (
	"context"
	"testing"
	"time"

	"github.com/KnowSky404/N2API/backend/internal/provider"
)

func TestOpsAccountHealthExcludesRetiredOAuthAccounts(t *testing.T) {
	repo, cleanup := newProviderRepositoryForTest(t)
	defer cleanup()
	ctx := context.Background()
	now := time.Now().UTC()
	until := now.Add(time.Hour)
	apiAccount := saveProviderTestAccount(t, repo, provider.Account{
		Provider: "openai", AccountType: provider.AccountTypeAPIUpstream,
		Subject: "api-upstream", DisplayName: "API upstream", Enabled: true,
		Status: provider.AccountStatusCircuitOpen, CircuitOpenUntil: &until,
		Credential: provider.AccountCredential{CredentialType: provider.CredentialTypeAPIKey, EncryptedAPIKey: "encrypted-api-key"},
	})
	legacyAccount := saveProviderTestAccount(t, repo, provider.Account{
		Provider: "openai", AccountType: provider.AccountTypeCodexOAuth,
		Subject: "legacy-oauth", DisplayName: "Legacy OAuth", Enabled: true,
		Status: provider.AccountStatusActive, EncryptedAccessToken: "encrypted-access-token",
		EncryptedRefreshToken: "encrypted-refresh-token",
	})
	for _, test := range []struct {
		accountID int64
		status    string
	}{{apiAccount.ID, provider.AccountTestStatusFailed}, {legacyAccount.ID, provider.AccountTestStatusPassed}} {
		if err := repo.RecordAccountTestResult(ctx, "openai", test.accountID, test.status, "", now); err != nil {
			t.Fatal(err)
		}
	}
	adminRepo := NewAdminRepository(repo.pool, "ops-health-test")
	health, err := adminRepo.GetOpsAccountHealth(ctx, now.Add(-time.Hour))
	if err != nil {
		t.Fatal(err)
	}
	if health.TotalAccounts != 1 || health.EnabledAccounts != 1 || health.Schedulable != 0 || health.CircuitOpen != 1 ||
		health.TestedAccounts != 1 || health.TestPassed != 0 || health.TestFailed != 1 || health.RecentTestFailure != 1 {
		t.Fatalf("API-only account health = %+v, want one unavailable API upstream", health)
	}
	history, err := adminRepo.ListOpsAccountTests(ctx, now.Add(-time.Hour), 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(history) != 2 {
		t.Fatalf("historical account tests = %d, want both API and retired OAuth results", len(history))
	}
}

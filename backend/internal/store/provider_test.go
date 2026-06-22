package store

import (
	"context"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/KnowSky404/N2API/backend/internal/provider"
)

func TestProviderRepositoryImplementsInterface(t *testing.T) {
	type repositoryPersistence interface {
		ListAccounts(ctx context.Context, provider string) ([]provider.Account, error)
		FindAccount(ctx context.Context, provider string) (provider.Account, error)
		FindAccountByID(ctx context.Context, provider string, id int64) (provider.Account, error)
		FindAccountByIdentity(ctx context.Context, provider string, identities provider.AccountIdentities) (provider.Account, error)
		SaveAccount(ctx context.Context, account provider.Account) (provider.Account, error)
		UpdateAccount(ctx context.Context, provider string, id int64, update provider.AccountUpdate) (provider.Account, error)
		DeleteAccount(ctx context.Context, provider string, id int64) error
		DeleteAccounts(ctx context.Context, provider string) error
		MarkAccountUsed(ctx context.Context, provider string, id int64, usedAt time.Time) error
		MarkAccountError(ctx context.Context, provider string, id int64, message string, at time.Time) error
		RecordRefreshFailure(ctx context.Context, provider string, id int64, message string, at time.Time, openUntil *time.Time) error
		RecordAccountStatus(ctx context.Context, provider string, id int64, status, reason string, at time.Time, rateLimitedUntil, circuitOpenUntil *time.Time) error
		CreateState(ctx context.Context, state provider.OAuthState) error
		ClaimState(ctx context.Context, provider, stateHash string, now time.Time) (provider.OAuthState, error)
	}

	var _ repositoryPersistence = (*ProviderRepository)(nil)
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

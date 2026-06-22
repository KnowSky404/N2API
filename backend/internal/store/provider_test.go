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
		ListAccountModels(ctx context.Context, provider string, accountID int64) ([]provider.AccountModel, error)
		ReplaceAccountModels(ctx context.Context, provider string, accountID int64, models []provider.AccountModelInput) ([]provider.AccountModel, error)
		ListExposedModels(ctx context.Context, provider string, allowedModels []string) ([]provider.ExposedModel, error)
		ListEligibleAccountsForModel(ctx context.Context, provider string, model string, excludedAccountIDs []int64, now time.Time) ([]provider.Account, error)
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

func TestListEligibleAccountsForModelFiltersAndOrders(t *testing.T) {
	repo, cleanup := newProviderRepositoryForTest(t)
	defer cleanup()

	ctx := context.Background()
	now := time.Date(2026, 6, 22, 12, 0, 0, 0, time.UTC)
	older := now.Add(-2 * time.Hour)
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
	rateLimited := saveProviderTestAccount(t, repo, provider.Account{
		Provider:              "openai",
		Subject:               "rate-limited",
		DisplayName:           "Rate Limited",
		EncryptedAccessToken:  "access",
		EncryptedRefreshToken: "refresh",
		Enabled:               true,
		Priority:              1,
		Status:                "rate_limited",
		RateLimitedUntil:      &later,
	})
	circuitOpen := saveProviderTestAccount(t, repo, provider.Account{
		Provider:              "openai",
		Subject:               "circuit-open",
		DisplayName:           "Circuit Open",
		EncryptedAccessToken:  "access",
		EncryptedRefreshToken: "refresh",
		Enabled:               true,
		Priority:              1,
		Status:                "circuit_open",
		CircuitOpenUntil:      &later,
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

	for _, account := range []provider.Account{first, nullLastUsed, higherPriority, disabledAccount, rateLimited, circuitOpen, expired} {
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
	wantIDs := []int64{nullLastUsed.ID, first.ID}
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

func TestListExposedModelsFiltersByAllowedOrder(t *testing.T) {
	repo, cleanup := newProviderRepositoryForTest(t)
	defer cleanup()

	ctx := context.Background()
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

	models, err := repo.ListExposedModels(ctx, "openai", []string{"codex-mini", "disabled-model", "gpt-5", "missing", "codex-mini"})
	if err != nil {
		t.Fatalf("ListExposedModels returned error: %v", err)
	}
	want := []provider.ExposedModel{
		{ID: "codex-mini", OwnedBy: "openai"},
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
	if _, err := pool.Exec(ctx, "TRUNCATE oauth_account_models, oauth_accounts, oauth_states RESTART IDENTITY CASCADE"); err != nil {
		pool.Close()
		t.Fatalf("TRUNCATE returned error: %v", err)
	}
	return NewProviderRepository(pool), pool.Close
}

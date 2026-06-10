package provider

import (
	"context"
	"errors"
	"net/url"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/KnowSky404/N2API/backend/internal/secret"
)

func TestStatusReportsConfigurationAndConnection(t *testing.T) {
	repo := newMemoryRepo()
	service := NewService(repo, fakeOAuthClient{}, Config{
		Provider:     "openai",
		ClientID:     "client-id",
		ClientSecret: "client-secret",
		RedirectURL:  "http://localhost/oauth/openai/callback",
		AuthURL:      "https://auth.example.test/authorize",
		TokenURL:     "https://auth.example.test/token",
		Secret:       "encryption-secret",
	})

	status, err := service.Status(context.Background())
	if err != nil {
		t.Fatalf("Status returned error: %v", err)
	}
	if !status.Configured || status.Connected {
		t.Fatalf("status = %+v, want configured disconnected provider", status)
	}

	expiresAt := time.Now().Add(time.Hour).UTC()
	if _, err := repo.SaveAccount(context.Background(), Account{
		Provider:              "openai",
		Subject:               "acct_1",
		DisplayName:           "Codex Account",
		EncryptedAccessToken:  mustEncrypt(t, "encryption-secret", "access-token"),
		EncryptedRefreshToken: mustEncrypt(t, "encryption-secret", "refresh-token"),
		AccessTokenExpiresAt:  &expiresAt,
		LastRefreshAt:         nil,
	}); err != nil {
		t.Fatalf("SaveAccount returned error: %v", err)
	}

	status, err = service.Status(context.Background())
	if err != nil {
		t.Fatalf("Status returned error: %v", err)
	}
	if !status.Connected || status.DisplayName != "Codex Account" {
		t.Fatalf("status = %+v, want connected Codex Account", status)
	}
}

func TestStartConnectStoresHashedStateAndBuildsAuthorizationURL(t *testing.T) {
	repo := newMemoryRepo()
	service := newConfiguredService(repo, fakeOAuthClient{})

	result, err := service.StartConnect(context.Background(), "/")
	if err != nil {
		t.Fatalf("StartConnect returned error: %v", err)
	}
	parsed, err := url.Parse(result.AuthorizationURL)
	if err != nil {
		t.Fatalf("authorization URL did not parse: %v", err)
	}
	if parsed.Query().Get("client_id") != "client-id" {
		t.Fatalf("client_id = %q", parsed.Query().Get("client_id"))
	}
	if parsed.Query().Get("redirect_uri") != "http://localhost/oauth/openai/callback" {
		t.Fatalf("redirect_uri = %q", parsed.Query().Get("redirect_uri"))
	}
	state := parsed.Query().Get("state")
	if state == "" {
		t.Fatal("state was empty")
	}
	if strings.Contains(repo.states[0].StateHash, state) {
		t.Fatal("state repository stored cleartext state")
	}
}

func TestCompleteCallbackRejectsInvalidState(t *testing.T) {
	service := newConfiguredService(newMemoryRepo(), fakeOAuthClient{})
	if _, err := service.CompleteCallback(context.Background(), "code", "missing-state"); !errors.Is(err, ErrInvalidState) {
		t.Fatalf("CompleteCallback error = %v, want ErrInvalidState", err)
	}
}

func TestCompleteCallbackStoresEncryptedTokensAndConsumesState(t *testing.T) {
	repo := newMemoryRepo()
	client := fakeOAuthClient{
		exchange: TokenResponse{
			AccessToken:  "access-token",
			RefreshToken: "refresh-token",
			ExpiresIn:    3600,
			Subject:      "acct_1",
			DisplayName:  "Codex Account",
		},
	}
	service := newConfiguredService(repo, client)

	started, err := service.StartConnect(context.Background(), "/")
	if err != nil {
		t.Fatalf("StartConnect returned error: %v", err)
	}
	state := mustQuery(t, started.AuthorizationURL, "state")
	account, err := service.CompleteCallback(context.Background(), "auth-code", state)
	if err != nil {
		t.Fatalf("CompleteCallback returned error: %v", err)
	}
	if account.DisplayName != "Codex Account" {
		t.Fatalf("DisplayName = %q", account.DisplayName)
	}
	if repo.accounts[0].EncryptedAccessToken == "access-token" || repo.accounts[0].EncryptedRefreshToken == "refresh-token" {
		t.Fatal("repository stored cleartext tokens")
	}
	if repo.states[0].ConsumedAt == nil {
		t.Fatal("state was not consumed")
	}
}

func TestCompleteCallbackConsumesStateWhenTokenExchangeFails(t *testing.T) {
	repo := newMemoryRepo()
	service := newConfiguredService(repo, fakeOAuthClient{exchangeErr: errors.New("token endpoint unavailable")})

	started, err := service.StartConnect(context.Background(), "/")
	if err != nil {
		t.Fatalf("StartConnect returned error: %v", err)
	}
	state := mustQuery(t, started.AuthorizationURL, "state")
	if _, err := service.CompleteCallback(context.Background(), "auth-code", state); err == nil {
		t.Fatal("CompleteCallback returned nil error, want token exchange error")
	}
	if repo.states[0].ConsumedAt == nil {
		t.Fatal("state was not consumed")
	}
}

func TestCompleteCallbackClaimsStateBeforeSavingTokens(t *testing.T) {
	repo := newMemoryRepo()
	client := fakeOAuthClient{
		exchange: TokenResponse{
			AccessToken:  "access-token",
			RefreshToken: "refresh-token",
			ExpiresIn:    3600,
			Subject:      "acct_1",
		},
	}
	service := newConfiguredService(repo, client)

	started, err := service.StartConnect(context.Background(), "/")
	if err != nil {
		t.Fatalf("StartConnect returned error: %v", err)
	}
	state := mustQuery(t, started.AuthorizationURL, "state")
	if _, err := service.CompleteCallback(context.Background(), "auth-code", state); err != nil {
		t.Fatalf("CompleteCallback returned error: %v", err)
	}
	if _, err := service.CompleteCallback(context.Background(), "auth-code", state); !errors.Is(err, ErrInvalidState) {
		t.Fatalf("replay error = %v, want ErrInvalidState", err)
	}
	if repo.saveCount != 1 {
		t.Fatalf("saveCount = %d, want 1", repo.saveCount)
	}
}

func TestCompleteCallbackGeneratesLocalSubjectsForBlankTokenSubject(t *testing.T) {
	repo := newMemoryRepo()
	service := newConfiguredService(repo, fakeOAuthClient{
		exchanges: []TokenResponse{
			{AccessToken: "access-token-1", RefreshToken: "refresh-token-1", ExpiresIn: 3600},
			{AccessToken: "access-token-2", RefreshToken: "refresh-token-2", ExpiresIn: 3600},
		},
	})

	for i := 0; i < 2; i++ {
		started, err := service.StartConnect(context.Background(), "/")
		if err != nil {
			t.Fatalf("StartConnect %d returned error: %v", i, err)
		}
		state := mustQuery(t, started.AuthorizationURL, "state")
		if _, err := service.CompleteCallback(context.Background(), "auth-code", state); err != nil {
			t.Fatalf("CompleteCallback %d returned error: %v", i, err)
		}
	}

	if len(repo.accounts) != 2 {
		t.Fatalf("account count = %d, want 2", len(repo.accounts))
	}
	if strings.TrimSpace(repo.accounts[0].Subject) == "" || strings.TrimSpace(repo.accounts[1].Subject) == "" {
		t.Fatalf("subjects = %q/%q, want non-empty local subjects", repo.accounts[0].Subject, repo.accounts[1].Subject)
	}
	if repo.accounts[0].Subject == repo.accounts[1].Subject {
		t.Fatalf("subjects matched: %q", repo.accounts[0].Subject)
	}
}

func TestAccessTokenReturnsUnexpiredToken(t *testing.T) {
	repo := newMemoryRepo()
	expiresAt := time.Now().Add(time.Hour)
	if _, err := repo.SaveAccount(context.Background(), Account{
		Provider:              "openai",
		Subject:               "acct_1",
		EncryptedAccessToken:  mustEncrypt(t, "encryption-secret", "access-token"),
		EncryptedRefreshToken: mustEncrypt(t, "encryption-secret", "refresh-token"),
		AccessTokenExpiresAt:  &expiresAt,
	}); err != nil {
		t.Fatalf("SaveAccount returned error: %v", err)
	}
	service := newConfiguredService(repo, fakeOAuthClient{})

	token, err := service.AccessToken(context.Background())
	if err != nil {
		t.Fatalf("AccessToken returned error: %v", err)
	}
	if token != "access-token" {
		t.Fatalf("token = %q, want access-token", token)
	}
}

func TestAccessTokenRefreshesExpiredToken(t *testing.T) {
	repo := newMemoryRepo()
	expired := time.Now().Add(-time.Minute)
	if _, err := repo.SaveAccount(context.Background(), Account{
		Provider:              "openai",
		Subject:               "acct_1",
		EncryptedAccessToken:  mustEncrypt(t, "encryption-secret", "old-access"),
		EncryptedRefreshToken: mustEncrypt(t, "encryption-secret", "refresh-token"),
		AccessTokenExpiresAt:  &expired,
	}); err != nil {
		t.Fatalf("SaveAccount returned error: %v", err)
	}
	service := newConfiguredService(repo, fakeOAuthClient{
		refresh: TokenResponse{
			AccessToken:  "new-access",
			RefreshToken: "new-refresh",
			ExpiresIn:    3600,
		},
	})

	token, err := service.AccessToken(context.Background())
	if err != nil {
		t.Fatalf("AccessToken returned error: %v", err)
	}
	if token != "new-access" {
		t.Fatalf("token = %q, want new-access", token)
	}
	if repo.accounts[0].LastRefreshAt == nil {
		t.Fatal("LastRefreshAt was not updated")
	}
}

func TestAccessTokenForAccountRefreshPreservesExistingIdentityAndRefreshToken(t *testing.T) {
	repo := newMemoryRepo()
	expired := time.Now().Add(-time.Minute)
	repo.accounts = []Account{
		testExpiredAccount(t, 7, false, 3, "old-access", "old-refresh", expired),
	}
	repo.accounts[0].Subject = "acct_original"
	service := newConfiguredService(repo, fakeOAuthClient{
		refresh: TokenResponse{
			AccessToken: "new-access",
			ExpiresIn:   3600,
			Subject:     "acct_changed",
			DisplayName: "Updated Name",
		},
	})

	token, err := service.AccessTokenForAccount(context.Background(), repo.accounts[0])
	if err != nil {
		t.Fatalf("AccessTokenForAccount returned error: %v", err)
	}
	if token != "new-access" {
		t.Fatalf("token = %q, want new-access", token)
	}
	if len(repo.accounts) != 1 {
		t.Fatalf("account count = %d, want 1", len(repo.accounts))
	}
	account := repo.accounts[0]
	if account.ID != 7 || account.Subject != "acct_original" || account.Enabled || account.Priority != 3 {
		t.Fatalf("account = %+v, want original id/subject/enabled/priority preserved", account)
	}
	refreshToken, err := secret.DecryptString("encryption-secret", account.EncryptedRefreshToken)
	if err != nil {
		t.Fatalf("DecryptString returned error: %v", err)
	}
	if refreshToken != "old-refresh" {
		t.Fatalf("refreshToken = %q, want old-refresh", refreshToken)
	}
}

func TestAccessTokenForAccountSerializesConcurrentRefresh(t *testing.T) {
	repo := newMemoryRepo()
	expired := time.Now().Add(-time.Minute)
	repo.accounts = []Account{
		testExpiredAccount(t, 7, true, 3, "old-access", "old-refresh", expired),
	}
	client := &blockingOAuthClient{
		refresh: TokenResponse{
			AccessToken:  "new-access",
			RefreshToken: "new-refresh",
			ExpiresIn:    3600,
		},
		entered: make(chan struct{}),
		release: make(chan struct{}),
	}
	service := newConfiguredService(repo, client)

	errs := make(chan error, 2)
	tokens := make(chan string, 2)
	for i := 0; i < 2; i++ {
		go func() {
			token, err := service.AccessTokenForAccount(context.Background(), repo.accounts[0])
			if err != nil {
				errs <- err
				return
			}
			tokens <- token
			errs <- nil
		}()
	}

	<-client.entered
	client.release <- struct{}{}
	for i := 0; i < 2; i++ {
		if err := <-errs; err != nil {
			t.Fatalf("AccessTokenForAccount returned error: %v", err)
		}
	}
	for i := 0; i < 2; i++ {
		if token := <-tokens; token != "new-access" {
			t.Fatalf("token = %q, want new-access", token)
		}
	}
	if client.calls != 1 {
		t.Fatalf("refresh calls = %d, want 1", client.calls)
	}
}

func TestSelectAccessTokenSkipsDisabledAndUsesPriority(t *testing.T) {
	repo := newMemoryRepo()
	repo.accounts = []Account{
		testAccount(t, 1, false, 1, "disabled-token"),
		testAccount(t, 2, true, 10, "enabled-token"),
	}
	service := newConfiguredService(repo, fakeOAuthClient{})

	selected, err := service.SelectAccessToken(context.Background())
	if err != nil {
		t.Fatalf("SelectAccessToken returned error: %v", err)
	}
	if selected.AccountID != 2 || selected.Token != "enabled-token" {
		t.Fatalf("selected = %+v", selected)
	}
}

func TestSelectAccessTokenUsesPriorityOrder(t *testing.T) {
	repo := newMemoryRepo()
	repo.accounts = []Account{
		testAccount(t, 1, true, 10, "low-priority-token"),
		testAccount(t, 2, true, 1, "high-priority-token"),
	}
	service := newConfiguredService(repo, fakeOAuthClient{})

	selected, err := service.SelectAccessToken(context.Background())
	if err != nil {
		t.Fatalf("SelectAccessToken returned error: %v", err)
	}
	if selected.AccountID != 2 || selected.Token != "high-priority-token" {
		t.Fatalf("selected = %+v", selected)
	}
}

func TestSelectAccessTokenSkipsExcludedAccount(t *testing.T) {
	repo := newMemoryRepo()
	repo.accounts = []Account{
		testAccount(t, 1, true, 1, "first-token"),
		testAccount(t, 2, true, 2, "fallback-token"),
	}
	service := newConfiguredService(repo, fakeOAuthClient{})

	selected, err := service.SelectAccessToken(context.Background(), 1)
	if err != nil {
		t.Fatalf("SelectAccessToken returned error: %v", err)
	}
	if selected.AccountID != 2 || selected.Token != "fallback-token" {
		t.Fatalf("selected = %+v, want account 2 fallback-token", selected)
	}
}

func TestSelectAccessTokenReturnsUnavailableWhenAllEnabledAccountsExcluded(t *testing.T) {
	repo := newMemoryRepo()
	repo.accounts = []Account{
		testAccount(t, 1, true, 1, "only-token"),
	}
	service := newConfiguredService(repo, fakeOAuthClient{})

	if _, err := service.SelectAccessToken(context.Background(), 1); !errors.Is(err, ErrAccountsUnavailable) {
		t.Fatalf("SelectAccessToken error = %v, want accounts unavailable", err)
	}
}

func TestSelectAccessTokenFallsBackWhenRefreshFails(t *testing.T) {
	repo := newMemoryRepo()
	expired := time.Now().Add(-time.Minute)
	repo.accounts = []Account{
		testExpiredAccount(t, 1, true, 1, "old-token", "bad-refresh", expired),
		testAccount(t, 2, true, 2, "fallback-token"),
	}
	service := newConfiguredService(repo, fakeOAuthClient{refreshErr: errors.New("refresh failed")})

	selected, err := service.SelectAccessToken(context.Background())
	if err != nil {
		t.Fatalf("SelectAccessToken returned error: %v", err)
	}
	if selected.AccountID != 2 || selected.Token != "fallback-token" {
		t.Fatalf("selected = %+v", selected)
	}
	if repo.accounts[0].LastError == "" {
		t.Fatal("first account error was not marked")
	}
}

func TestSelectAccessTokenReturnsMarkAccountErrorFailure(t *testing.T) {
	repo := newMemoryRepo()
	expired := time.Now().Add(-time.Minute)
	repo.accounts = []Account{
		testExpiredAccount(t, 1, true, 1, "old-token", "bad-refresh", expired),
		testAccount(t, 2, true, 2, "fallback-token"),
	}
	repo.markAccountErrorErr = errors.New("mark account error failed")
	service := newConfiguredService(repo, fakeOAuthClient{refreshErr: errors.New("refresh failed")})

	if _, err := service.SelectAccessToken(context.Background()); !errors.Is(err, repo.markAccountErrorErr) {
		t.Fatalf("SelectAccessToken error = %v, want mark account error failure", err)
	}
}

func TestSelectAccessTokenReturnsMarkAccountUsedFailure(t *testing.T) {
	repo := newMemoryRepo()
	repo.accounts = []Account{
		testAccount(t, 1, true, 1, "access-token"),
	}
	repo.markAccountUsedErr = errors.New("mark account used failed")
	service := newConfiguredService(repo, fakeOAuthClient{})

	if _, err := service.SelectAccessToken(context.Background()); !errors.Is(err, repo.markAccountUsedErr) {
		t.Fatalf("SelectAccessToken error = %v, want mark account used failure", err)
	}
}

type memoryRepo struct {
	accounts []Account
	states   []OAuthState

	saveCount           int
	nextID              int64
	markAccountErrorErr error
	markAccountUsedErr  error
}

func newMemoryRepo() *memoryRepo {
	return &memoryRepo{nextID: 1}
}

func (r *memoryRepo) ListAccounts(ctx context.Context, providerName string) ([]Account, error) {
	var accounts []Account
	for _, account := range r.accounts {
		if account.Provider == providerName {
			accounts = append(accounts, account)
		}
	}
	sort.SliceStable(accounts, func(i, j int) bool {
		if accounts[i].Priority != accounts[j].Priority {
			return accounts[i].Priority < accounts[j].Priority
		}
		iHasError := accounts[i].LastErrorAt != nil
		jHasError := accounts[j].LastErrorAt != nil
		if iHasError != jHasError {
			return !iHasError
		}
		if accounts[i].LastUsedAt == nil && accounts[j].LastUsedAt != nil {
			return true
		}
		if accounts[i].LastUsedAt != nil && accounts[j].LastUsedAt == nil {
			return false
		}
		if accounts[i].LastUsedAt != nil && accounts[j].LastUsedAt != nil && !accounts[i].LastUsedAt.Equal(*accounts[j].LastUsedAt) {
			return accounts[i].LastUsedAt.Before(*accounts[j].LastUsedAt)
		}
		return accounts[i].ID < accounts[j].ID
	})
	return accounts, nil
}

func (r *memoryRepo) FindAccount(ctx context.Context, providerName string) (Account, error) {
	accounts, err := r.ListAccounts(ctx, providerName)
	if err != nil {
		return Account{}, err
	}
	if len(accounts) == 0 {
		return Account{}, ErrNotConnected
	}
	return accounts[0], nil
}

func (r *memoryRepo) FindAccountByID(ctx context.Context, providerName string, id int64) (Account, error) {
	for _, account := range r.accounts {
		if account.Provider == providerName && account.ID == id {
			return account, nil
		}
	}
	return Account{}, ErrNotConnected
}

func (r *memoryRepo) SaveAccount(ctx context.Context, account Account) (Account, error) {
	r.saveCount++
	now := time.Now()
	for i := range r.accounts {
		if r.accounts[i].Provider == account.Provider && r.accounts[i].Subject == account.Subject {
			account.ID = valueOrDefaultInt64(account.ID, r.accounts[i].ID)
			if !account.Enabled {
				account.Enabled = r.accounts[i].Enabled
			}
			if account.Priority == 0 {
				account.Priority = r.accounts[i].Priority
			}
			account.CreatedAt = r.accounts[i].CreatedAt
			account.UpdatedAt = now
			r.accounts[i] = account
			return account, nil
		}
	}
	if account.ID == 0 {
		account.ID = r.nextID
		r.nextID++
	}
	if !account.Enabled {
		account.Enabled = true
	}
	if account.Priority == 0 {
		account.Priority = 100
	}
	account.CreatedAt = now
	account.UpdatedAt = now
	r.accounts = append(r.accounts, account)
	return account, nil
}

func (r *memoryRepo) UpdateAccount(ctx context.Context, providerName string, id int64, update AccountUpdate) (Account, error) {
	for i := range r.accounts {
		if r.accounts[i].Provider != providerName || r.accounts[i].ID != id {
			continue
		}
		if update.Enabled != nil {
			r.accounts[i].Enabled = *update.Enabled
		}
		if update.Priority != nil {
			r.accounts[i].Priority = *update.Priority
		}
		r.accounts[i].UpdatedAt = time.Now()
		return r.accounts[i], nil
	}
	return Account{}, ErrNotConnected
}

func (r *memoryRepo) DeleteAccount(ctx context.Context, providerName string, id int64) error {
	for i := range r.accounts {
		if r.accounts[i].Provider == providerName && r.accounts[i].ID == id {
			r.accounts = append(r.accounts[:i], r.accounts[i+1:]...)
			return nil
		}
	}
	return ErrNotConnected
}

func (r *memoryRepo) DeleteAccounts(ctx context.Context, providerName string) error {
	kept := r.accounts[:0]
	for _, account := range r.accounts {
		if account.Provider != providerName {
			kept = append(kept, account)
		}
	}
	r.accounts = kept
	return nil
}

func (r *memoryRepo) MarkAccountUsed(ctx context.Context, providerName string, id int64, usedAt time.Time) error {
	if r.markAccountUsedErr != nil {
		return r.markAccountUsedErr
	}
	for i := range r.accounts {
		if r.accounts[i].Provider == providerName && r.accounts[i].ID == id {
			r.accounts[i].LastUsedAt = &usedAt
			r.accounts[i].LastError = ""
			r.accounts[i].LastErrorAt = nil
			return nil
		}
	}
	return ErrNotConnected
}

func (r *memoryRepo) MarkAccountError(ctx context.Context, providerName string, id int64, message string, at time.Time) error {
	if r.markAccountErrorErr != nil {
		return r.markAccountErrorErr
	}
	for i := range r.accounts {
		if r.accounts[i].Provider == providerName && r.accounts[i].ID == id {
			r.accounts[i].LastError = message
			r.accounts[i].LastErrorAt = &at
			return nil
		}
	}
	return ErrNotConnected
}

func (r *memoryRepo) CreateState(ctx context.Context, state OAuthState) error {
	r.states = append(r.states, state)
	return nil
}

func (r *memoryRepo) ClaimState(ctx context.Context, providerName, stateHash string, now time.Time) (OAuthState, error) {
	for i := range r.states {
		state := &r.states[i]
		if state.Provider != providerName || state.StateHash != stateHash || state.ConsumedAt != nil || !state.ExpiresAt.After(now) {
			continue
		}
		consumedAt := now
		state.ConsumedAt = &consumedAt
		return *state, nil
	}
	return OAuthState{}, ErrInvalidState
}

type fakeOAuthClient struct {
	exchange    TokenResponse
	exchanges   []TokenResponse
	exchangeErr error
	refresh     TokenResponse
	refreshErr  error
}

func (c fakeOAuthClient) ExchangeCode(ctx context.Context, cfg Config, code string) (TokenResponse, error) {
	if c.exchangeErr != nil {
		return TokenResponse{}, c.exchangeErr
	}
	if len(c.exchanges) > 0 {
		return c.exchanges[0], nil
	}
	return c.exchange, nil
}

func (c fakeOAuthClient) RefreshToken(ctx context.Context, cfg Config, refreshToken string) (TokenResponse, error) {
	if c.refreshErr != nil {
		return TokenResponse{}, c.refreshErr
	}
	return c.refresh, nil
}

type blockingOAuthClient struct {
	refresh TokenResponse
	entered chan struct{}
	release chan struct{}
	calls   int
}

func (c *blockingOAuthClient) ExchangeCode(ctx context.Context, cfg Config, code string) (TokenResponse, error) {
	return TokenResponse{}, errors.New("unexpected exchange")
}

func (c *blockingOAuthClient) RefreshToken(ctx context.Context, cfg Config, refreshToken string) (TokenResponse, error) {
	c.calls++
	if c.calls == 1 {
		c.entered <- struct{}{}
		<-c.release
	}
	return c.refresh, nil
}

func newConfiguredService(repo Repository, client OAuthClient) *Service {
	return NewService(repo, client, Config{
		Provider:     "openai",
		ClientID:     "client-id",
		ClientSecret: "client-secret",
		RedirectURL:  "http://localhost/oauth/openai/callback",
		AuthURL:      "https://auth.example.test/authorize",
		TokenURL:     "https://auth.example.test/token",
		Secret:       "encryption-secret",
	})
}

func mustEncrypt(t *testing.T, encryptionSecret, value string) string {
	t.Helper()
	encrypted, err := secret.EncryptString(encryptionSecret, value)
	if err != nil {
		t.Fatalf("EncryptString returned error: %v", err)
	}
	return encrypted
}

func mustQuery(t *testing.T, rawURL, key string) string {
	t.Helper()
	parsed, err := url.Parse(rawURL)
	if err != nil {
		t.Fatalf("url.Parse returned error: %v", err)
	}
	return parsed.Query().Get(key)
}

func testAccount(t *testing.T, id int64, enabled bool, priority int, accessToken string) Account {
	t.Helper()
	expiresAt := time.Now().Add(time.Hour)
	return testExpiredAccount(t, id, enabled, priority, accessToken, "refresh-token", expiresAt)
}

func testExpiredAccount(t *testing.T, id int64, enabled bool, priority int, accessToken, refreshToken string, expiresAt time.Time) Account {
	t.Helper()
	now := time.Now()
	return Account{
		ID:                    id,
		Provider:              "openai",
		Subject:               "acct_" + string(rune('0'+id)),
		DisplayName:           "Account",
		EncryptedAccessToken:  mustEncrypt(t, "encryption-secret", accessToken),
		EncryptedRefreshToken: mustEncrypt(t, "encryption-secret", refreshToken),
		AccessTokenExpiresAt:  &expiresAt,
		Enabled:               enabled,
		Priority:              priority,
		CreatedAt:             now,
		UpdatedAt:             now,
	}
}

func valueOrDefaultInt64(value, fallback int64) int64 {
	if value == 0 {
		return fallback
	}
	return value
}

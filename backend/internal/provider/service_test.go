package provider

import (
	"context"
	"errors"
	"net/url"
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
	if err := repo.SaveAccount(context.Background(), Account{
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
	if repo.account.EncryptedAccessToken == "access-token" || repo.account.EncryptedRefreshToken == "refresh-token" {
		t.Fatal("repository stored cleartext tokens")
	}
	if repo.states[0].ConsumedAt == nil {
		t.Fatal("state was not consumed")
	}
}

func TestAccessTokenReturnsUnexpiredToken(t *testing.T) {
	repo := newMemoryRepo()
	expiresAt := time.Now().Add(time.Hour)
	if err := repo.SaveAccount(context.Background(), Account{
		Provider:              "openai",
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
	if err := repo.SaveAccount(context.Background(), Account{
		Provider:              "openai",
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
	if repo.account.LastRefreshAt == nil {
		t.Fatal("LastRefreshAt was not updated")
	}
}

type memoryRepo struct {
	account    Account
	hasAccount bool
	states     []OAuthState
}

func newMemoryRepo() *memoryRepo {
	return &memoryRepo{}
}

func (r *memoryRepo) FindAccount(ctx context.Context, providerName string) (Account, error) {
	if !r.hasAccount || r.account.Provider != providerName {
		return Account{}, ErrNotConnected
	}
	return r.account, nil
}

func (r *memoryRepo) SaveAccount(ctx context.Context, account Account) error {
	r.account = account
	r.hasAccount = true
	return nil
}

func (r *memoryRepo) DeleteAccount(ctx context.Context, providerName string) error {
	if r.hasAccount && r.account.Provider == providerName {
		r.account = Account{}
		r.hasAccount = false
	}
	return nil
}

func (r *memoryRepo) CreateState(ctx context.Context, state OAuthState) error {
	r.states = append(r.states, state)
	return nil
}

func (r *memoryRepo) ConsumeState(ctx context.Context, providerName, stateHash string, now time.Time) (OAuthState, error) {
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
	exchange TokenResponse
	refresh  TokenResponse
}

func (c fakeOAuthClient) ExchangeCode(ctx context.Context, cfg Config, code string) (TokenResponse, error) {
	return c.exchange, nil
}

func (c fakeOAuthClient) RefreshToken(ctx context.Context, cfg Config, refreshToken string) (TokenResponse, error) {
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

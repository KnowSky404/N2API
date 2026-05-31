# OpenAI OAuth Account Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add an admin-managed OpenAI/Codex OAuth account connection primitive that stores encrypted tokens and exposes provider status for later gateway routes.

**Architecture:** Keep provider account business rules in `backend/internal/provider`, PostgreSQL access in `backend/internal/store`, and routing in `backend/internal/httpapi`. OAuth endpoint values are configuration, not compile-time assumptions. The frontend adds one operational provider panel above the existing API key table.

**Tech Stack:** Go standard library HTTP/crypto, pgx v5, Goose v3, PostgreSQL, Bun, SvelteKit 5, Tailwind CSS.

---

## File Structure

- Modify: `backend/internal/config/config.go` and `backend/internal/config/config_test.go`
  - Add OAuth authorization and token endpoint configuration.
- Create: `backend/internal/store/migrations/00003_oauth_states.sql`
  - Add single-use OAuth callback state persistence.
- Modify: `backend/internal/store/migrations_test.go`
  - Verify the OAuth state migration is embedded.
- Create: `backend/internal/provider/service.go`, `backend/internal/provider/service_test.go`, and `backend/internal/provider/http_client_test.go`
  - Own provider status, connect URL creation, callback completion, disconnect, token lookup, and refresh rules behind repository and OAuth client interfaces.
- Create: `backend/internal/store/provider.go` and `backend/internal/store/provider_test.go`
  - Implement provider repository methods using pgx and existing tables.
- Modify: `backend/internal/httpapi/server.go` and `backend/internal/httpapi/server_test.go`
  - Add provider status/connect/disconnect admin endpoints and public OAuth callback route.
- Modify: `backend/cmd/n2api/main.go`
  - Wire provider service and repository into the HTTP server.
- Modify: `frontend/src/routes/+page.svelte`
  - Add provider panel and status/connect/disconnect behavior.
- Modify: `.env.example`
  - Document required OAuth endpoint settings.

---

### Task 1: OAuth Configuration

**Files:**
- Modify: `backend/internal/config/config.go`
- Modify: `backend/internal/config/config_test.go`
- Modify: `.env.example`

- [x] **Step 1: Write failing config tests**

Add this test in `backend/internal/config/config_test.go`:

```go
func TestLoadOpenAIOAuthEndpointConfig(t *testing.T) {
	env := map[string]string{
		"DATABASE_URL":                "postgres://example",
		"N2API_ENCRYPTION_SECRET":     "encryption-secret",
		"N2API_ADMIN_PASSWORD":        "admin-password",
		"OPENAI_OAUTH_CLIENT_ID":      "client-id",
		"OPENAI_OAUTH_CLIENT_SECRET":  "client-secret",
		"OPENAI_OAUTH_REDIRECT_URL":   "http://localhost:3000/oauth/openai/callback",
		"OPENAI_OAUTH_AUTH_URL":       "https://auth.example.test/authorize",
		"OPENAI_OAUTH_TOKEN_URL":      "https://auth.example.test/token",
	}
	cfg, err := Load(func(key string) string { return env[key] })
	if err != nil {
		t.Fatalf("Load returned error: %v", err)
	}
	if cfg.OpenAIOAuthAuthURL != "https://auth.example.test/authorize" {
		t.Fatalf("OpenAIOAuthAuthURL = %q", cfg.OpenAIOAuthAuthURL)
	}
	if cfg.OpenAIOAuthTokenURL != "https://auth.example.test/token" {
		t.Fatalf("OpenAIOAuthTokenURL = %q", cfg.OpenAIOAuthTokenURL)
	}
}
```

- [x] **Step 2: Run test and verify failure**

Run from `backend`:

```bash
GOCACHE=/root/Clouds/N2API/.cache/go-build go test ./internal/config
```

Expected: compile failure because `OpenAIOAuthAuthURL` and `OpenAIOAuthTokenURL` do not exist.

- [x] **Step 3: Implement config fields**

Update `backend/internal/config/config.go`:

```go
type Config struct {
	Host                   string
	Port                   int
	PublicURL              string
	DatabaseURL            string
	AdminUsername          string
	AdminPassword          string
	EncryptionSecret       string
	OpenAIOAuthClientID    string
	OpenAIOAuthSecret      string
	OpenAIOAuthRedirectURL string
	OpenAIOAuthAuthURL     string
	OpenAIOAuthTokenURL    string
}
```

Add these assignments inside `Load`:

```go
OpenAIOAuthAuthURL:  lookup("OPENAI_OAUTH_AUTH_URL"),
OpenAIOAuthTokenURL: lookup("OPENAI_OAUTH_TOKEN_URL"),
```

- [x] **Step 4: Update `.env.example`**

Append endpoint placeholders:

```dotenv
OPENAI_OAUTH_AUTH_URL=
OPENAI_OAUTH_TOKEN_URL=
```

- [x] **Step 5: Verify and commit**

Run:

```bash
cd backend
GOCACHE=/root/Clouds/N2API/.cache/go-build go test ./...
cd ..
git add .env.example backend/internal/config/config.go backend/internal/config/config_test.go
git commit -m "feat: add oauth endpoint configuration"
```

Expected: backend tests pass and one commit is created.

### Task 2: OAuth State Migration

**Files:**
- Create: `backend/internal/store/migrations/00003_oauth_states.sql`
- Modify: `backend/internal/store/migrations_test.go`

- [x] **Step 1: Write failing migration test**

Add to `backend/internal/store/migrations_test.go`:

```go
func TestOAuthStatesMigrationIsEmbedded(t *testing.T) {
	sql, err := MigrationSQL("00003_oauth_states.sql")
	if err != nil {
		t.Fatalf("MigrationSQL returned error: %v", err)
	}
	for _, want := range []string{
		"CREATE TABLE IF NOT EXISTS oauth_states",
		"provider TEXT NOT NULL",
		"state_hash TEXT NOT NULL UNIQUE",
		"redirect_after TEXT NOT NULL DEFAULT '/'",
		"oauth_states_state_hash_idx",
		"oauth_states_expires_at_idx",
	} {
		if !strings.Contains(sql, want) {
			t.Fatalf("migration missing %q", want)
		}
	}
}
```

- [x] **Step 2: Run test and verify failure**

Run from `backend`:

```bash
GOCACHE=/root/Clouds/N2API/.cache/go-build go test ./internal/store
```

Expected: failure because `00003_oauth_states.sql` is missing.

- [x] **Step 3: Add migration**

Create `backend/internal/store/migrations/00003_oauth_states.sql`:

```sql
-- +goose Up
-- +goose StatementBegin
CREATE TABLE IF NOT EXISTS oauth_states (
    id BIGSERIAL PRIMARY KEY,
    provider TEXT NOT NULL,
    state_hash TEXT NOT NULL UNIQUE,
    redirect_after TEXT NOT NULL DEFAULT '/',
    expires_at TIMESTAMPTZ NOT NULL,
    consumed_at TIMESTAMPTZ,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX IF NOT EXISTS oauth_states_state_hash_idx ON oauth_states (state_hash);
CREATE INDEX IF NOT EXISTS oauth_states_expires_at_idx ON oauth_states (expires_at);
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
DROP TABLE IF EXISTS oauth_states;
-- +goose StatementEnd
```

- [x] **Step 4: Verify and commit**

Run:

```bash
cd backend
GOCACHE=/root/Clouds/N2API/.cache/go-build go test ./...
cd ..
git add backend/internal/store/migrations/00003_oauth_states.sql backend/internal/store/migrations_test.go
git commit -m "feat: add oauth state migration"
```

Expected: backend tests pass and one commit is created.

### Task 3: Provider Service

**Files:**
- Create: `backend/internal/provider/service.go`
- Create: `backend/internal/provider/service_test.go`

- [x] **Step 1: Write provider service tests**

Create `backend/internal/provider/service_test.go` with tests for configuration, connect URL creation, callback, disconnect, token lookup, and refresh. Use in-memory fakes for the repository and OAuth client. The full file starts with:

```go
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
		Provider:                "openai",
		Subject:                 "acct_1",
		DisplayName:             "Codex Account",
		EncryptedAccessToken:    mustEncrypt(t, "encryption-secret", "access-token"),
		EncryptedRefreshToken:   mustEncrypt(t, "encryption-secret", "refresh-token"),
		AccessTokenExpiresAt:    &expiresAt,
		LastRefreshAt:           nil,
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
```

Append these helpers to the same file:

```go
type memoryRepo struct {
	account Account
	hasAccount bool
	states []OAuthState
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
```

- [x] **Step 2: Run tests and verify failure**

Run from `backend`:

```bash
GOCACHE=/root/Clouds/N2API/.cache/go-build go test ./internal/provider
```

Expected: compile failure because the provider package does not exist.

- [x] **Step 3: Implement provider service**

Create `backend/internal/provider/service.go` with:

```go
package provider

import (
	"context"
	"errors"
	"fmt"
	"net/url"
	"strings"
	"time"

	"github.com/KnowSky404/N2API/backend/internal/secret"
)

const (
	defaultStateTTL      = 10 * time.Minute
	defaultRefreshWindow = 2 * time.Minute
)

var (
	ErrNotConfigured = errors.New("provider not configured")
	ErrNotConnected  = errors.New("provider not connected")
	ErrInvalidState  = errors.New("invalid oauth state")
)

type Config struct {
	Provider      string
	ClientID      string
	ClientSecret  string
	RedirectURL   string
	AuthURL       string
	TokenURL      string
	Secret        string
	StateTTL      time.Duration
	RefreshWindow time.Duration
}

type Status struct {
	Provider             string     `json:"provider"`
	Configured           bool       `json:"configured"`
	Connected            bool       `json:"connected"`
	DisplayName          string     `json:"displayName"`
	AccessTokenExpiresAt *time.Time `json:"accessTokenExpiresAt"`
	LastRefreshAt        *time.Time `json:"lastRefreshAt"`
}

type ConnectResult struct {
	AuthorizationURL string
}

type OAuthState struct {
	Provider      string
	StateHash     string
	RedirectAfter string
	ExpiresAt     time.Time
	ConsumedAt    *time.Time
}

type Account struct {
	Provider              string
	Subject               string
	DisplayName           string
	EncryptedAccessToken  string
	EncryptedRefreshToken string
	AccessTokenExpiresAt  *time.Time
	LastRefreshAt         *time.Time
}

type TokenResponse struct {
	AccessToken  string
	RefreshToken string
	ExpiresIn    int
	Subject      string
	DisplayName  string
}

type Repository interface {
	FindAccount(ctx context.Context, provider string) (Account, error)
	SaveAccount(ctx context.Context, account Account) error
	DeleteAccount(ctx context.Context, provider string) error
	CreateState(ctx context.Context, state OAuthState) error
	ConsumeState(ctx context.Context, provider, stateHash string, now time.Time) (OAuthState, error)
}

type OAuthClient interface {
	ExchangeCode(ctx context.Context, cfg Config, code string) (TokenResponse, error)
	RefreshToken(ctx context.Context, cfg Config, refreshToken string) (TokenResponse, error)
}

type Service struct {
	repo   Repository
	client OAuthClient
	cfg    Config
}

func NewService(repo Repository, client OAuthClient, cfg Config) *Service {
	if cfg.Provider == "" {
		cfg.Provider = "openai"
	}
	if cfg.StateTTL <= 0 {
		cfg.StateTTL = defaultStateTTL
	}
	if cfg.RefreshWindow <= 0 {
		cfg.RefreshWindow = defaultRefreshWindow
	}
	return &Service{repo: repo, client: client, cfg: cfg}
}
```

Then implement `Configured`, `Status`, `StartConnect`, `CompleteCallback`, `Disconnect`, `AccessToken`, private `storeTokenResponse`, and private token encryption/decryption helpers with these rules:

```go
func (s *Service) Configured() bool {
	return strings.TrimSpace(s.cfg.ClientID) != "" &&
		strings.TrimSpace(s.cfg.ClientSecret) != "" &&
		strings.TrimSpace(s.cfg.RedirectURL) != "" &&
		strings.TrimSpace(s.cfg.AuthURL) != "" &&
		strings.TrimSpace(s.cfg.TokenURL) != "" &&
		strings.TrimSpace(s.cfg.Secret) != ""
}
```

`StartConnect` returns `ErrNotConfigured` when `Configured()` is false. Otherwise it generates a random state, stores `secret.HashAPIKey(state)` with expiry `time.Now().Add(s.cfg.StateTTL)`, and builds `cfg.AuthURL` with `response_type=code`, `client_id`, `redirect_uri`, and `state`.

`CompleteCallback` returns `ErrInvalidState` for blank code or state. Otherwise it consumes `secret.HashAPIKey(state)`, exchanges the code through `OAuthClient.ExchangeCode`, encrypts the returned tokens, saves the account, and returns the saved account metadata.

`AccessToken` loads the account, decrypts the access token, and returns it when `AccessTokenExpiresAt` is nil or after `time.Now().Add(s.cfg.RefreshWindow)`. If the token is expired or inside the refresh window, decrypt the refresh token, call `OAuthClient.RefreshToken`, save the refreshed encrypted tokens, and return the new access token.

Use `secret.GenerateToken("oauth_state")`, `secret.HashAPIKey`, `secret.EncryptString`, and `secret.DecryptString`.

- [x] **Step 4: Verify and commit**

Run:

```bash
cd backend
GOCACHE=/root/Clouds/N2API/.cache/go-build go test ./...
cd ..
git add backend/internal/provider/service.go backend/internal/provider/service_test.go
git commit -m "feat: add provider oauth service"
```

Expected: backend tests pass and one commit is created.

### Task 4: Provider Store

**Files:**
- Create: `backend/internal/store/provider.go`
- Create or modify: `backend/internal/store/provider_test.go`

- [x] **Step 1: Add repository compile test**

Create `backend/internal/store/provider_test.go`:

```go
package store

import (
	"testing"

	"github.com/KnowSky404/N2API/backend/internal/provider"
)

func TestProviderRepositoryImplementsInterface(t *testing.T) {
	var _ provider.Repository = (*ProviderRepository)(nil)
}
```

- [x] **Step 2: Run test and verify failure**

Run from `backend`:

```bash
GOCACHE=/root/Clouds/N2API/.cache/go-build go test ./internal/store
```

Expected: compile failure because `ProviderRepository` does not exist.

- [x] **Step 3: Implement provider repository**

Create `backend/internal/store/provider.go` with methods:

```go
type ProviderRepository struct {
	pool *pgxpool.Pool
}

func NewProviderRepository(pool *pgxpool.Pool) *ProviderRepository {
	return &ProviderRepository{pool: pool}
}
```

Implement:
- `FindAccount(ctx, providerName string) (provider.Account, error)`
- `SaveAccount(ctx context.Context, account provider.Account) error`
- `DeleteAccount(ctx context.Context, providerName string) error`
- `CreateState(ctx context.Context, state provider.OAuthState) error`
- `ConsumeState(ctx context.Context, providerName, stateHash string, now time.Time) (provider.OAuthState, error)`

Use `provider.ErrNotConnected` for missing accounts and `provider.ErrInvalidState` for missing, expired, consumed, or already-consumed states.

`FindAccount` should return the most recent account for the provider:

```sql
SELECT provider, subject, display_name, encrypted_access_token, encrypted_refresh_token, access_token_expires_at, last_refresh_at
FROM oauth_accounts
WHERE provider = $1
ORDER BY updated_at DESC, id DESC
LIMIT 1
```

`SaveAccount` should normalize an empty subject to `''` and upsert by `(provider, subject)`:

```sql
INSERT INTO oauth_accounts (provider, subject, display_name, encrypted_access_token, encrypted_refresh_token, access_token_expires_at, last_refresh_at, updated_at)
VALUES ($1, $2, $3, $4, $5, $6, $7, now())
ON CONFLICT (provider, subject)
DO UPDATE SET
    display_name = EXCLUDED.display_name,
    encrypted_access_token = EXCLUDED.encrypted_access_token,
    encrypted_refresh_token = EXCLUDED.encrypted_refresh_token,
    access_token_expires_at = EXCLUDED.access_token_expires_at,
    last_refresh_at = EXCLUDED.last_refresh_at,
    updated_at = now()
```

`ConsumeState` should use one atomic update:

```sql
UPDATE oauth_states
SET consumed_at = $4
WHERE provider = $1
  AND state_hash = $2
  AND expires_at > $3
  AND consumed_at IS NULL
RETURNING provider, state_hash, redirect_after, expires_at, consumed_at
```

- [x] **Step 4: Verify and commit**

Run:

```bash
cd backend
GOCACHE=/root/Clouds/N2API/.cache/go-build go test ./...
cd ..
git add backend/internal/store/provider.go backend/internal/store/provider_test.go
git commit -m "feat: add provider postgres repository"
```

Expected: backend tests pass and one commit is created.

### Task 5: OAuth HTTP Client

**Files:**
- Modify: `backend/internal/provider/service.go`
- Create: `backend/internal/provider/http_client_test.go`

- [x] **Step 1: Add OAuth client tests**

Create `backend/internal/provider/http_client_test.go` using `httptest.Server`:

```go
package provider

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestHTTPClientExchangeCodePostsAuthorizationCodeGrant(t *testing.T) {
	var gotGrantType string
	var gotCode string
	var gotClientID string
	var gotClientSecret string
	var gotRedirectURI string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if err := r.ParseForm(); err != nil {
			t.Fatalf("ParseForm returned error: %v", err)
		}
		gotGrantType = r.Form.Get("grant_type")
		gotCode = r.Form.Get("code")
		gotClientID = r.Form.Get("client_id")
		gotClientSecret = r.Form.Get("client_secret")
		gotRedirectURI = r.Form.Get("redirect_uri")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"access_token":  "access-token",
			"refresh_token": "refresh-token",
			"expires_in":    3600,
			"subject":       "acct_1",
			"display_name":  "Codex Account",
		})
	}))
	defer server.Close()

	client := NewHTTPClient(server.Client())
	token, err := client.ExchangeCode(context.Background(), Config{
		ClientID:     "client-id",
		ClientSecret: "client-secret",
		RedirectURL:  "http://localhost/oauth/openai/callback",
		TokenURL:     server.URL,
	}, "auth-code")
	if err != nil {
		t.Fatalf("ExchangeCode returned error: %v", err)
	}
	if token.AccessToken != "access-token" || token.RefreshToken != "refresh-token" {
		t.Fatalf("token = %+v", token)
	}
	if gotGrantType != "authorization_code" || gotCode != "auth-code" || gotClientID != "client-id" || gotClientSecret != "client-secret" || gotRedirectURI != "http://localhost/oauth/openai/callback" {
		t.Fatalf("posted form = grant_type:%q code:%q client_id:%q client_secret:%q redirect_uri:%q", gotGrantType, gotCode, gotClientID, gotClientSecret, gotRedirectURI)
	}
}

func TestHTTPClientRefreshTokenPostsRefreshGrant(t *testing.T) {
	var gotGrantType string
	var gotRefreshToken string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if err := r.ParseForm(); err != nil {
			t.Fatalf("ParseForm returned error: %v", err)
		}
		gotGrantType = r.Form.Get("grant_type")
		gotRefreshToken = r.Form.Get("refresh_token")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"access_token":  "new-access",
			"refresh_token": "new-refresh",
			"expires_in":    3600,
		})
	}))
	defer server.Close()

	client := NewHTTPClient(server.Client())
	token, err := client.RefreshToken(context.Background(), Config{
		ClientID:     "client-id",
		ClientSecret: "client-secret",
		TokenURL:     server.URL,
	}, "old-refresh")
	if err != nil {
		t.Fatalf("RefreshToken returned error: %v", err)
	}
	if token.AccessToken != "new-access" || token.RefreshToken != "new-refresh" {
		t.Fatalf("token = %+v", token)
	}
	if gotGrantType != "refresh_token" || gotRefreshToken != "old-refresh" {
		t.Fatalf("posted form = grant_type:%q refresh_token:%q", gotGrantType, gotRefreshToken)
	}
}
```

- [x] **Step 2: Run tests and verify failure**

Run from `backend`:

```bash
GOCACHE=/root/Clouds/N2API/.cache/go-build go test ./internal/provider
```

Expected: compile failure because `NewHTTPClient` does not exist.

- [x] **Step 3: Implement HTTP OAuth client**

In `backend/internal/provider/service.go`, add `net/http`, `encoding/json`, and `io` imports, then add:

```go
type HTTPClient struct {
	client *http.Client
}

func NewHTTPClient(client *http.Client) *HTTPClient {
	if client == nil {
		client = http.DefaultClient
	}
	return &HTTPClient{client: client}
}
```

Implement `ExchangeCode` and `RefreshToken` with `application/x-www-form-urlencoded` POST requests to `cfg.TokenURL`, bounded response body reads, and JSON decoding for `access_token`, `refresh_token`, `expires_in`, `subject`, and `display_name`.

- [x] **Step 4: Verify and commit**

Run:

```bash
cd backend
GOCACHE=/root/Clouds/N2API/.cache/go-build go test ./...
cd ..
git add backend/internal/provider/service.go backend/internal/provider/http_client_test.go
git commit -m "feat: add oauth token http client"
```

Expected: backend tests pass and one commit is created.

### Task 6: Provider HTTP API

**Files:**
- Modify: `backend/internal/httpapi/server.go`
- Modify: `backend/internal/httpapi/server_test.go`
- Modify: `backend/cmd/n2api/main.go`

- [x] **Step 1: Add HTTP tests**

Add tests in `backend/internal/httpapi/server_test.go` for:
- `GET /api/admin/providers/openai` returns `401` without session.
- authenticated `GET /api/admin/providers/openai` returns provider status.
- authenticated `POST /api/admin/providers/openai/connect` returns an authorization URL.
- authenticated `POST /api/admin/providers/openai/disconnect` returns `204`.
- `GET /oauth/openai/callback` redirects to connected or error status.

Extend the existing fake admin service setup with a fake provider service.

- [x] **Step 2: Run tests and verify failure**

Run from `backend`:

```bash
GOCACHE=/root/Clouds/N2API/.cache/go-build go test ./internal/httpapi
```

Expected: compile failure because `NewServer` has no provider dependency and routes are missing.

- [x] **Step 3: Add provider HTTP interface and routes**

In `backend/internal/httpapi/server.go`, add:

```go
type ProviderService interface {
	Status(ctx context.Context) (provider.Status, error)
	StartConnect(ctx context.Context, redirectAfter string) (provider.ConnectResult, error)
	CompleteCallback(ctx context.Context, code, state string) (provider.Account, error)
	Disconnect(ctx context.Context) error
}
```

Change `NewServer` signature to:

```go
func NewServer(cfg config.Config, health HealthChecker, admins AdminService, providers ProviderService) http.Handler
```

Add the provider admin routes inside the existing authenticated middleware. Add the public callback route before the catch-all handler. Map `provider.ErrNotConfigured` to `409 provider_not_configured`; callback errors redirect to `/?provider=openai&status=error`.

The connect route should return:

```go
writeJSON(w, http.StatusOK, map[string]string{"authorizationUrl": result.AuthorizationURL})
```

The callback route should use:

```go
code := r.URL.Query().Get("code")
state := r.URL.Query().Get("state")
if _, err := providers.CompleteCallback(r.Context(), code, state); err != nil {
	http.Redirect(w, r, "/?provider=openai&status=error", http.StatusFound)
	return
}
http.Redirect(w, r, "/?provider=openai&status=connected", http.StatusFound)
```

- [x] **Step 4: Wire main**

In `backend/cmd/n2api/main.go`, create:

```go
providerRepo := store.NewProviderRepository(pool)
providerService := provider.NewService(providerRepo, provider.NewHTTPClient(http.DefaultClient), provider.Config{
	Provider:     "openai",
	ClientID:     cfg.OpenAIOAuthClientID,
	ClientSecret: cfg.OpenAIOAuthSecret,
	RedirectURL:  cfg.OpenAIOAuthRedirectURL,
	AuthURL:      cfg.OpenAIOAuthAuthURL,
	TokenURL:     cfg.OpenAIOAuthTokenURL,
	Secret:       cfg.EncryptionSecret,
})
```

Then pass `providerService` to `httpapi.NewServer`.

- [x] **Step 5: Verify and commit**

Run:

```bash
cd backend
GOCACHE=/root/Clouds/N2API/.cache/go-build go test ./...
cd ..
git add backend/cmd/n2api/main.go backend/internal/httpapi/server.go backend/internal/httpapi/server_test.go
git commit -m "feat: add provider admin api"
```

Expected: backend tests pass and one commit is created.

### Task 7: Admin Provider Panel

**Files:**
- Modify: `frontend/src/routes/+page.svelte`

- [x] **Step 1: Add provider state and request functions**

In `frontend/src/routes/+page.svelte`, add provider state:

```js
let provider = $state({
  loading: false,
  connecting: false,
  disconnecting: false,
  error: '',
  data: null
});
```

Add `loadProvider`, `connectProvider`, and `disconnectProvider` functions using the existing `requestJSON` helper.

- [x] **Step 2: Add provider panel markup**

Render the provider panel only when `session.authenticated` is true. Place it above the API keys section. The panel should show OpenAI/Codex, configured/connected status, display name, token expiry, last refresh, and connect/disconnect buttons.

- [x] **Step 3: Ensure session changes clear provider state**

When `clearAPIKeys()` runs on logout or unauthenticated state, also clear provider state. After successful `loadSession`, call `loadProvider()` before or alongside `loadKeys()`.

- [x] **Step 4: Verify and commit**

Run:

```bash
cd frontend
bun run check
bun run build
cd ..
git add frontend/src/routes/+page.svelte
git commit -m "feat: show provider connection in admin ui"
```

Expected: frontend checks pass and one commit is created.

### Task 8: Final Verification

**Files:**
- Review repository state and documentation.

- [x] **Step 1: Run backend tests**

Run:

```bash
cd backend
GOCACHE=/root/Clouds/N2API/.cache/go-build go test ./...
```

Expected: all backend packages pass.

- [x] **Step 2: Run frontend checks**

Run:

```bash
cd frontend
bun run check
bun run build
```

Expected: Svelte validation and production build pass.

- [x] **Step 3: Check repository status**

Run:

```bash
git status --short
```

Expected: no uncommitted implementation changes remain unless the final verification modifies ignored build/cache output only.

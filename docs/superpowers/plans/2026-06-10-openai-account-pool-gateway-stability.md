# OpenAI Account Pool Gateway Stability Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Build a stable personal OpenAI/Codex account pool for N2API so Codex and chat/completions traffic can use enabled accounts with deterministic selection and pre-stream fallback.

**Architecture:** Extend the existing provider boundary from a single-account service into an account-pool service. Keep OAuth, token encryption, and token refresh in `internal/provider`, PostgreSQL persistence in `internal/store`, and upstream retry/fallback in `internal/gateway`.

**Tech Stack:** Go, PostgreSQL/goose migrations, pgx, SvelteKit, Bun, Docker Compose.

---

## File Structure

- Modify `backend/internal/provider/service.go`: add account-pool types, repository methods, account-scoped token access, state claiming, and pool selection.
- Modify `backend/internal/provider/service_test.go`: replace the single-account memory repo with a pool-aware memory repo and add selector/callback replay/refresh tests.
- Modify `backend/internal/store/migrations/00004_oauth_account_pool.sql`: add account-pool metadata columns and indexes.
- Modify `backend/internal/store/migrations_test.go`: assert the new migration is embedded and provider sees four migrations.
- Modify `backend/internal/store/provider.go`: implement account-pool repository methods using PostgreSQL.
- Modify `backend/internal/store/provider_test.go`: update interface assertions and migration expectations.
- Modify `backend/internal/gateway/proxy.go`: change the token provider interface to return account-aware tokens and support retry before streaming begins.
- Modify `backend/internal/gateway/proxy_test.go`: add account fallback and no-retry-after-streaming tests.
- Modify `backend/internal/httpapi/server.go`: add admin account-list/update/disconnect endpoints while preserving existing provider status/connect endpoints.
- Modify `backend/internal/httpapi/server_test.go`: cover account endpoints and authentication requirements.
- Modify `backend/cmd/n2api/main.go`: wire the upgraded provider service into the gateway.
- Modify `frontend/src/routes/+page.svelte`: replace the single provider panel with account-pool controls.
- Modify `deploy/README.md` and `README.md`: document multi-account operation and verification commands.

## Task 1: Database and Provider Types

**Files:**
- Create: `backend/internal/store/migrations/00004_oauth_account_pool.sql`
- Modify: `backend/internal/store/migrations_test.go`
- Modify: `backend/internal/provider/service.go`

- [ ] **Step 1: Add the migration test first**

Add this test to `backend/internal/store/migrations_test.go`:

```go
func TestOAuthAccountPoolMigrationIsEmbedded(t *testing.T) {
	sql, err := MigrationSQL("00004_oauth_account_pool.sql")
	if err != nil {
		t.Fatalf("MigrationSQL returned error: %v", err)
	}
	for _, want := range []string{
		"ALTER TABLE oauth_accounts ADD COLUMN IF NOT EXISTS enabled BOOLEAN NOT NULL DEFAULT true",
		"ALTER TABLE oauth_accounts ADD COLUMN IF NOT EXISTS priority INTEGER NOT NULL DEFAULT 100",
		"ALTER TABLE oauth_accounts ADD COLUMN IF NOT EXISTS last_used_at TIMESTAMPTZ",
		"ALTER TABLE oauth_accounts ADD COLUMN IF NOT EXISTS last_error TEXT NOT NULL DEFAULT ''",
		"ALTER TABLE oauth_accounts ADD COLUMN IF NOT EXISTS last_error_at TIMESTAMPTZ",
		"oauth_accounts_pool_order_idx",
	} {
		if !strings.Contains(sql, want) {
			t.Fatalf("migration missing %q", want)
		}
	}
}
```

Update `TestMigrationProviderSeesEmbeddedMigrations`:

```go
if len(sources) != 4 {
	t.Fatalf("migration sources = %d, want 4", len(sources))
}
if sources[0].Path != "00001_init.sql" || sources[3].Path != "00004_oauth_account_pool.sql" {
	t.Fatalf("migration source paths = %+v", sources)
}
```

- [ ] **Step 2: Run the failing migration tests**

Run:

```bash
cd backend
GOMODCACHE=/root/Clouds/N2API/.cache/go-mod GOCACHE=/root/Clouds/N2API/.cache/go-build go test ./internal/store -run 'TestOAuthAccountPoolMigrationIsEmbedded|TestMigrationProviderSeesEmbeddedMigrations'
```

Expected: failure because `00004_oauth_account_pool.sql` does not exist.

- [ ] **Step 3: Add the migration**

Create `backend/internal/store/migrations/00004_oauth_account_pool.sql`:

```sql
-- +goose Up
-- +goose StatementBegin
ALTER TABLE oauth_accounts ADD COLUMN IF NOT EXISTS enabled BOOLEAN NOT NULL DEFAULT true;
ALTER TABLE oauth_accounts ADD COLUMN IF NOT EXISTS priority INTEGER NOT NULL DEFAULT 100;
ALTER TABLE oauth_accounts ADD COLUMN IF NOT EXISTS last_used_at TIMESTAMPTZ;
ALTER TABLE oauth_accounts ADD COLUMN IF NOT EXISTS last_error TEXT NOT NULL DEFAULT '';
ALTER TABLE oauth_accounts ADD COLUMN IF NOT EXISTS last_error_at TIMESTAMPTZ;

CREATE INDEX IF NOT EXISTS oauth_accounts_pool_order_idx
	ON oauth_accounts (provider, enabled, priority, last_error_at, last_used_at, id);
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
DROP INDEX IF EXISTS oauth_accounts_pool_order_idx;
ALTER TABLE oauth_accounts DROP COLUMN IF EXISTS last_error_at;
ALTER TABLE oauth_accounts DROP COLUMN IF EXISTS last_error;
ALTER TABLE oauth_accounts DROP COLUMN IF EXISTS last_used_at;
ALTER TABLE oauth_accounts DROP COLUMN IF EXISTS priority;
ALTER TABLE oauth_accounts DROP COLUMN IF EXISTS enabled;
-- +goose StatementEnd
```

- [ ] **Step 4: Extend provider types and repository interface**

In `backend/internal/provider/service.go`, extend `Account`:

```go
type Account struct {
	ID                     int64      `json:"id"`
	Provider               string     `json:"provider"`
	Subject                string     `json:"subject"`
	DisplayName            string     `json:"displayName"`
	EncryptedAccessToken   string     `json:"-"`
	EncryptedRefreshToken  string     `json:"-"`
	AccessTokenExpiresAt   *time.Time `json:"accessTokenExpiresAt"`
	LastRefreshAt          *time.Time `json:"lastRefreshAt"`
	Enabled                bool       `json:"enabled"`
	Priority               int        `json:"priority"`
	LastUsedAt             *time.Time `json:"lastUsedAt"`
	LastError              string     `json:"lastError"`
	LastErrorAt            *time.Time `json:"lastErrorAt"`
	CreatedAt              time.Time  `json:"createdAt"`
	UpdatedAt              time.Time  `json:"updatedAt"`
}
```

Add:

```go
type AccountUpdate struct {
	Enabled  *bool
	Priority *int
}

type SelectedToken struct {
	AccountID int64
	Token     string
}
```

Replace `Repository` with:

```go
type Repository interface {
	ListAccounts(ctx context.Context, provider string) ([]Account, error)
	FindAccount(ctx context.Context, provider string) (Account, error)
	FindAccountByID(ctx context.Context, provider string, id int64) (Account, error)
	SaveAccount(ctx context.Context, account Account) (Account, error)
	UpdateAccount(ctx context.Context, provider string, id int64, update AccountUpdate) (Account, error)
	DeleteAccount(ctx context.Context, provider string, id int64) error
	DeleteAccounts(ctx context.Context, provider string) error
	MarkAccountUsed(ctx context.Context, provider string, id int64, usedAt time.Time) error
	MarkAccountError(ctx context.Context, provider string, id int64, message string, at time.Time) error
	CreateState(ctx context.Context, state OAuthState) error
	ClaimState(ctx context.Context, provider, stateHash string, now time.Time) (OAuthState, error)
}
```

Keep `FindAccount` and `DeleteAccounts` temporarily for backward-compatible status/disconnect behavior while later tasks migrate call sites.

- [ ] **Step 5: Run migration/type tests**

Run:

```bash
cd backend
GOMODCACHE=/root/Clouds/N2API/.cache/go-mod GOCACHE=/root/Clouds/N2API/.cache/go-build go test ./internal/store ./internal/provider
```

Expected: provider/store compile failures until Task 2 updates implementations.

- [ ] **Step 6: Commit**

```bash
git add backend/internal/store/migrations/00004_oauth_account_pool.sql backend/internal/store/migrations_test.go backend/internal/provider/service.go
git commit -m "feat: add oauth account pool schema"
```

## Task 2: Store Repository Account Pool Methods

**Files:**
- Modify: `backend/internal/store/provider.go`
- Modify: `backend/internal/store/provider_test.go`

- [ ] **Step 1: Update interface assertion test**

Keep `backend/internal/store/provider_test.go` simple:

```go
func TestProviderRepositoryImplementsInterface(t *testing.T) {
	var _ provider.Repository = (*ProviderRepository)(nil)
}
```

This should fail until `ProviderRepository` implements the new interface.

- [ ] **Step 2: Implement account listing and lookup**

In `backend/internal/store/provider.go`, add a scanner helper:

```go
func scanProviderAccount(row pgx.Row) (provider.Account, error) {
	var account provider.Account
	err := row.Scan(
		&account.ID,
		&account.Provider,
		&account.Subject,
		&account.DisplayName,
		&account.EncryptedAccessToken,
		&account.EncryptedRefreshToken,
		&account.AccessTokenExpiresAt,
		&account.LastRefreshAt,
		&account.Enabled,
		&account.Priority,
		&account.LastUsedAt,
		&account.LastError,
		&account.LastErrorAt,
		&account.CreatedAt,
		&account.UpdatedAt,
	)
	return account, err
}
```

Add:

```go
const providerAccountColumns = `
	id, provider, subject, display_name, encrypted_access_token, encrypted_refresh_token,
	access_token_expires_at, last_refresh_at, enabled, priority, last_used_at,
	last_error, last_error_at, created_at, updated_at
`
```

Implement `ListAccounts` ordered by pool priority:

```go
func (r *ProviderRepository) ListAccounts(ctx context.Context, providerName string) ([]provider.Account, error) {
	rows, err := r.pool.Query(ctx, `
		SELECT `+providerAccountColumns+`
		FROM oauth_accounts
		WHERE provider = $1
		ORDER BY
			priority ASC,
			(last_error_at IS NOT NULL) ASC,
			last_used_at ASC NULLS FIRST,
			id ASC
	`, providerName)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var accounts []provider.Account
	for rows.Next() {
		account, err := scanProviderAccount(rows)
		if err != nil {
			return nil, err
		}
		accounts = append(accounts, account)
	}
	return accounts, rows.Err()
}
```

Implement `FindAccountByID` and update `FindAccount` to use the same columns.

- [ ] **Step 3: Implement save/update/delete markers**

Change `SaveAccount` to return the persisted row:

```go
func (r *ProviderRepository) SaveAccount(ctx context.Context, account provider.Account) (provider.Account, error) {
	row := r.pool.QueryRow(ctx, `
		INSERT INTO oauth_accounts (
			provider, subject, display_name, encrypted_access_token, encrypted_refresh_token,
			access_token_expires_at, last_refresh_at, enabled, priority, last_error, updated_at
		)
		VALUES ($1, $2, $3, $4, $5, $6, $7, true, 100, '', now())
		ON CONFLICT (provider, subject)
		DO UPDATE SET
			display_name = EXCLUDED.display_name,
			encrypted_access_token = EXCLUDED.encrypted_access_token,
			encrypted_refresh_token = EXCLUDED.encrypted_refresh_token,
			access_token_expires_at = EXCLUDED.access_token_expires_at,
			last_refresh_at = EXCLUDED.last_refresh_at,
			last_error = '',
			last_error_at = NULL,
			updated_at = now()
		RETURNING `+providerAccountColumns+`
	`, account.Provider, account.Subject, account.DisplayName, account.EncryptedAccessToken, account.EncryptedRefreshToken, account.AccessTokenExpiresAt, account.LastRefreshAt)
	saved, err := scanProviderAccount(row)
	if err != nil {
		return provider.Account{}, err
	}
	return saved, nil
}
```

Implement `UpdateAccount`, `DeleteAccount`, `DeleteAccounts`, `MarkAccountUsed`, and `MarkAccountError` with `RETURNING` where needed. Map `pgx.ErrNoRows` to `provider.ErrNotConnected` for account lookup/update/delete by id.

- [ ] **Step 4: Replace state find/consume with claim**

Remove `FindState` and `ConsumeState` implementation after call sites move. Add:

```go
func (r *ProviderRepository) ClaimState(ctx context.Context, providerName, stateHash string, now time.Time) (provider.OAuthState, error) {
	var state provider.OAuthState
	err := r.pool.QueryRow(ctx, `
		UPDATE oauth_states
		SET consumed_at = $4
		WHERE provider = $1
			AND state_hash = $2
			AND expires_at > $3
			AND consumed_at IS NULL
		RETURNING provider, state_hash, redirect_after, expires_at, consumed_at
	`, providerName, stateHash, now, now).Scan(
		&state.Provider,
		&state.StateHash,
		&state.RedirectAfter,
		&state.ExpiresAt,
		&state.ConsumedAt,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return provider.OAuthState{}, provider.ErrInvalidState
	}
	if err != nil {
		return provider.OAuthState{}, err
	}
	return state, nil
}
```

- [ ] **Step 5: Run store tests**

Run:

```bash
cd backend
GOMODCACHE=/root/Clouds/N2API/.cache/go-mod GOCACHE=/root/Clouds/N2API/.cache/go-build go test ./internal/store
```

Expected: pass.

- [ ] **Step 6: Commit**

```bash
git add backend/internal/store/provider.go backend/internal/store/provider_test.go
git commit -m "feat: add provider account pool store"
```

## Task 3: Provider Service Pool Selection and OAuth Claiming

**Files:**
- Modify: `backend/internal/provider/service.go`
- Modify: `backend/internal/provider/service_test.go`

- [ ] **Step 1: Write provider service tests**

Add tests to `backend/internal/provider/service_test.go`:

```go
func TestCompleteCallbackClaimsStateBeforeSavingTokens(t *testing.T) {
	repo := newMemoryRepo()
	client := fakeOAuthClient{exchange: TokenResponse{AccessToken: "access-token", RefreshToken: "refresh-token", ExpiresIn: 3600, Subject: "acct_1"}}
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

func TestSelectAccessTokenSkipsDisabledAndUsesPriority(t *testing.T) {
	repo := newMemoryRepo()
	repo.accounts = []Account{
		testAccount(t, 1, false, 1, "disabled-token"),
		testAccount(t, 2, true, 10, "low-priority-token"),
		testAccount(t, 3, true, 1, "high-priority-token"),
	}
	service := newConfiguredService(repo, fakeOAuthClient{})

	selected, err := service.SelectAccessToken(context.Background())
	if err != nil {
		t.Fatalf("SelectAccessToken returned error: %v", err)
	}
	if selected.AccountID != 3 || selected.Token != "high-priority-token" {
		t.Fatalf("selected = %+v", selected)
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
```

Extend `fakeOAuthClient` with `refreshErr`.

- [ ] **Step 2: Run failing provider tests**

Run:

```bash
cd backend
GOMODCACHE=/root/Clouds/N2API/.cache/go-mod GOCACHE=/root/Clouds/N2API/.cache/go-build go test ./internal/provider
```

Expected: compile/test failures for missing `SelectAccessToken`, repository methods, and memory repo fields.

- [ ] **Step 3: Implement callback state claiming**

In `CompleteCallback`, replace find-then-save-then-consume with claim-then-exchange-then-save:

```go
stateHash := secret.HashAPIKey(state)
claimed, err := s.repo.ClaimState(ctx, s.cfg.Provider, stateHash, time.Now())
if err != nil {
	if errors.Is(err, ErrInvalidState) {
		return Account{}, ErrInvalidState
	}
	return Account{}, err
}
_ = claimed
```

Then exchange the code and save tokens. If token exchange fails, the state remains consumed. This is intentional for one-time callback integrity.

- [ ] **Step 4: Implement account-scoped access token methods**

Add:

```go
func (s *Service) AccessTokenForAccount(ctx context.Context, account Account) (string, error) {
	if account.AccessTokenExpiresAt == nil || account.AccessTokenExpiresAt.After(time.Now().Add(s.cfg.RefreshWindow)) {
		return secret.DecryptString(s.cfg.Secret, account.EncryptedAccessToken)
	}
	refreshToken, err := secret.DecryptString(s.cfg.Secret, account.EncryptedRefreshToken)
	if err != nil {
		return "", err
	}
	tokens, err := s.client.RefreshToken(ctx, s.cfg, refreshToken)
	if err != nil {
		return "", err
	}
	refreshed, err := s.storeTokenResponse(ctx, tokens, &account)
	if err != nil {
		return "", err
	}
	return secret.DecryptString(s.cfg.Secret, refreshed.EncryptedAccessToken)
}
```

Update `storeTokenResponse` to call the new `SaveAccount` return value and preserve account id/priority/enabled when refreshing.

- [ ] **Step 5: Implement pool selection**

Add:

```go
func (s *Service) SelectAccessToken(ctx context.Context) (SelectedToken, error) {
	if !s.Configured() {
		return SelectedToken{}, ErrNotConfigured
	}
	accounts, err := s.repo.ListAccounts(ctx, s.cfg.Provider)
	if err != nil {
		return SelectedToken{}, err
	}
	if len(accounts) == 0 {
		return SelectedToken{}, ErrNotConnected
	}
	hasEnabled := false
	for _, account := range accounts {
		if !account.Enabled {
			continue
		}
		hasEnabled = true
		token, err := s.AccessTokenForAccount(ctx, account)
		if err != nil {
			_ = s.repo.MarkAccountError(ctx, s.cfg.Provider, account.ID, err.Error(), time.Now())
			continue
		}
		_ = s.repo.MarkAccountUsed(ctx, s.cfg.Provider, account.ID, time.Now())
		return SelectedToken{AccountID: account.ID, Token: token}, nil
	}
	if !hasEnabled {
		return SelectedToken{}, ErrAccountsDisabled
	}
	return SelectedToken{}, ErrAccountsUnavailable
}
```

Define:

```go
var (
	ErrAccountsDisabled    = errors.New("provider accounts disabled")
	ErrAccountsUnavailable = errors.New("provider accounts unavailable")
)
```

Keep `AccessToken(ctx)` as a compatibility wrapper that calls `SelectAccessToken` and returns only `Token`.

- [ ] **Step 6: Update memory repo**

Refactor `memoryRepo` to hold `accounts []Account`, `states []OAuthState`, and `saveCount int`. Implement all repository interface methods with deterministic in-memory behavior. Add helper functions `testAccount` and `testExpiredAccount`.

- [ ] **Step 7: Run provider tests**

Run:

```bash
cd backend
GOMODCACHE=/root/Clouds/N2API/.cache/go-mod GOCACHE=/root/Clouds/N2API/.cache/go-build go test ./internal/provider
```

Expected: pass.

- [ ] **Step 8: Commit**

```bash
git add backend/internal/provider/service.go backend/internal/provider/service_test.go
git commit -m "feat: select OpenAI accounts from pool"
```

## Task 4: Gateway Account-Aware Fallback

**Files:**
- Modify: `backend/internal/gateway/proxy.go`
- Modify: `backend/internal/gateway/proxy_test.go`
- Modify: `backend/cmd/n2api/main.go`

- [ ] **Step 1: Add gateway fallback tests**

In `backend/internal/gateway/proxy_test.go`, replace `fakeAccessTokenProvider` with account-aware behavior:

```go
type fakeSelectedTokenProvider struct {
	tokens []SelectedToken
	errs   []error
	calls  int
}

func (p *fakeSelectedTokenProvider) SelectAccessToken(ctx context.Context) (SelectedToken, error) {
	i := p.calls
	p.calls++
	if i < len(p.errs) && p.errs[i] != nil {
		return SelectedToken{}, p.errs[i]
	}
	if i < len(p.tokens) {
		return p.tokens[i], nil
	}
	return SelectedToken{}, provider.ErrAccountsUnavailable
}
```

Add test:

```go
func TestProxyRetriesAnotherAccountBeforeStreaming(t *testing.T) {
	attempt := 0
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		attempt++
		if attempt == 1 {
			panic(http.ErrAbortHandler)
		}
		if r.Header.Get("Authorization") != "Bearer second-token" {
			t.Fatalf("Authorization = %q", r.Header.Get("Authorization"))
		}
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"ok":true}`))
	}))
	defer upstream.Close()
	tokens := &fakeSelectedTokenProvider{tokens: []SelectedToken{{AccountID: 1, Token: "first-token"}, {AccountID: 2, Token: "second-token"}}}
	proxy := NewProxy(&fakeAPIKeyAuthenticator{}, tokens, Config{UpstreamBaseURL: upstream.URL})
	req := httptest.NewRequest(http.MethodGet, "/v1/models", nil)
	req.Header.Set("Authorization", "Bearer n2api_client_secret")
	recorder := httptest.NewRecorder()

	proxy.ServeHTTP(recorder, req)

	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d body=%s", recorder.Code, recorder.Body.String())
	}
	if tokens.calls != 2 {
		t.Fatalf("token calls = %d, want 2", tokens.calls)
	}
}
```

Add a no-retry-after-streaming test with a custom `RoundTripper`:

```go
type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(r *http.Request) (*http.Response, error) {
	return f(r)
}

type brokenReader struct {
	sent bool
}

func (r *brokenReader) Read(p []byte) (int, error) {
	if !r.sent {
		r.sent = true
		return copy(p, "data: partial\n\n"), nil
	}
	return 0, errors.New("stream broke")
}

func (r *brokenReader) Close() error {
	return nil
}

func TestProxyDoesNotRetryAfterStreamingBegins(t *testing.T) {
	tokens := &fakeSelectedTokenProvider{tokens: []SelectedToken{{AccountID: 1, Token: "first-token"}, {AccountID: 2, Token: "second-token"}}}
	client := &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
		return &http.Response{
			StatusCode: http.StatusOK,
			Header:     http.Header{"Content-Type": []string{"text/event-stream"}},
			Body:       &brokenReader{},
			Request:    r,
		}, nil
	})}
	proxy := NewProxyWithClient(&fakeAPIKeyAuthenticator{}, tokens, Config{UpstreamBaseURL: "https://upstream.example.test"}, client)
	req := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", strings.NewReader(`{"stream":true}`))
	req.Header.Set("Authorization", "Bearer n2api_client_secret")
	recorder := httptest.NewRecorder()

	proxy.ServeHTTP(recorder, req)

	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", recorder.Code)
	}
	if tokens.calls != 1 {
		t.Fatalf("token calls = %d, want 1", tokens.calls)
	}
	if !strings.Contains(recorder.Body.String(), "data: partial") {
		t.Fatalf("body = %q", recorder.Body.String())
	}
}
```

- [ ] **Step 2: Run failing gateway tests**

Run:

```bash
cd backend
GOMODCACHE=/root/Clouds/N2API/.cache/go-mod GOCACHE=/root/Clouds/N2API/.cache/go-build go test ./internal/gateway
```

Expected: compile failures until gateway interfaces change.

- [ ] **Step 3: Update gateway token interface**

In `backend/internal/gateway/proxy.go`, define:

```go
type SelectedToken struct {
	AccountID int64
	Token     string
}

type AccessTokenProvider interface {
	SelectAccessToken(ctx context.Context) (SelectedToken, error)
}
```

Update call sites to use `selected.Token` for upstream authorization and include `selected.AccountID` internally for debugging/future logs.

- [ ] **Step 4: Implement retry loop**

In `ServeHTTP`, after API key authentication, wrap upstream attempt in a small loop:

```go
for attempt := 0; attempt < 2; attempt++ {
	selected, err := p.tokens.SelectAccessToken(r.Context())
	if err != nil {
		errorCode = providerErrorCode(err)
		writeOpenAIError(recorder, http.StatusServiceUnavailable, errorCode, providerErrorMessage(errorCode))
		return
	}
	upstreamReq, err := p.newUpstreamRequest(r, selected.Token)
	if err != nil {
		errorCode = "upstream_request_error"
		writeOpenAIError(recorder, http.StatusBadGateway, errorCode, "could not create upstream request")
		return
	}
	upstreamResp, err := p.client.Do(upstreamReq)
	if err != nil {
		if attempt == 0 {
			continue
		}
		errorCode = "upstream_unavailable"
		writeOpenAIError(recorder, http.StatusBadGateway, errorCode, "upstream request failed")
		return
	}
	defer upstreamResp.Body.Close()
	copyResponseHeaders(recorder.Header(), upstreamResp.Header)
	recorder.WriteHeader(upstreamResp.StatusCode)
	_, _ = io.Copy(flushWriter{ResponseWriter: recorder}, upstreamResp.Body)
	return
}
```

For POST routes, buffer request bodies up to 1 MiB before the first attempt and recreate readers for each attempt. If the body exceeds 1 MiB, execute one upstream attempt only and skip fallback retry for that request.

- [ ] **Step 5: Map provider pool errors**

Add helper:

```go
func providerErrorCode(err error) string {
	switch {
	case errors.Is(err, provider.ErrNotConnected):
		return "provider_not_connected"
	case errors.Is(err, provider.ErrNotConfigured):
		return "provider_not_configured"
	case errors.Is(err, provider.ErrAccountsDisabled):
		return "provider_accounts_disabled"
	case errors.Is(err, provider.ErrAccountsUnavailable):
		return "provider_accounts_unavailable"
	default:
		return "upstream_token_error"
	}
}
```

- [ ] **Step 6: Wire main**

`provider.Service` implements `SelectAccessToken`, so `backend/cmd/n2api/main.go` should still pass `providerService` into `gateway.NewProxy`.

- [ ] **Step 7: Run gateway tests**

Run:

```bash
cd backend
GOMODCACHE=/root/Clouds/N2API/.cache/go-mod GOCACHE=/root/Clouds/N2API/.cache/go-build go test ./internal/gateway
```

Expected: pass.

- [ ] **Step 8: Commit**

```bash
git add backend/internal/gateway/proxy.go backend/internal/gateway/proxy_test.go backend/cmd/n2api/main.go
git commit -m "feat: retry gateway requests across account pool"
```

## Task 5: Admin API for Accounts

**Files:**
- Modify: `backend/internal/httpapi/server.go`
- Modify: `backend/internal/httpapi/server_test.go`
- Modify: `backend/internal/provider/service.go`

- [ ] **Step 1: Extend HTTP provider interface**

In `backend/internal/httpapi/server.go`, update `ProviderService`:

```go
type ProviderService interface {
	Status(ctx context.Context) (provider.Status, error)
	ListAccounts(ctx context.Context) ([]provider.Account, error)
	StartConnect(ctx context.Context, redirectAfter string) (provider.ConnectResult, error)
	CompleteCallback(ctx context.Context, code, state string) (provider.Account, error)
	UpdateAccount(ctx context.Context, id int64, update provider.AccountUpdate) (provider.Account, error)
	DisconnectAccount(ctx context.Context, id int64) error
	Disconnect(ctx context.Context) error
}
```

- [ ] **Step 2: Write HTTP tests first**

Add tests covering:

```go
func TestAdminProviderAccountsRequireSession(t *testing.T) {
	server := NewServer(testConfig(), nil, fakeAdminService{validateErr: admin.ErrUnauthorized}, fakeProviderService{})
	req := httptest.NewRequest(http.MethodGet, "/api/admin/providers/openai/accounts", nil)
	recorder := httptest.NewRecorder()

	server.ServeHTTP(recorder, req)

	if recorder.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401", recorder.Code)
	}
}

func TestAdminCanListProviderAccounts(t *testing.T) {
	providers := fakeProviderService{accounts: []provider.Account{{ID: 7, Provider: "openai", DisplayName: "Account A", Enabled: true, Priority: 10}}}
	server := NewServer(testConfig(), nil, authenticatedAdminService(), providers)
	req := authenticatedRequest(http.MethodGet, "/api/admin/providers/openai/accounts", nil)
	recorder := httptest.NewRecorder()

	server.ServeHTTP(recorder, req)

	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d body=%s", recorder.Code, recorder.Body.String())
	}
	if !strings.Contains(recorder.Body.String(), `"id":7`) {
		t.Fatalf("body = %s", recorder.Body.String())
	}
}
```

Add update and disconnect tests with `PATCH /api/admin/providers/openai/accounts/7` and `POST /api/admin/providers/openai/accounts/7/disconnect`.

- [ ] **Step 3: Implement provider service admin methods**

In `backend/internal/provider/service.go`:

```go
func (s *Service) ListAccounts(ctx context.Context) ([]Account, error) {
	return s.repo.ListAccounts(ctx, s.cfg.Provider)
}

func (s *Service) UpdateAccount(ctx context.Context, id int64, update AccountUpdate) (Account, error) {
	if id <= 0 {
		return Account{}, ErrInvalidInput
	}
	if update.Priority != nil && *update.Priority < 0 {
		return Account{}, ErrInvalidInput
	}
	return s.repo.UpdateAccount(ctx, s.cfg.Provider, id, update)
}

func (s *Service) DisconnectAccount(ctx context.Context, id int64) error {
	if id <= 0 {
		return ErrInvalidInput
	}
	return s.repo.DeleteAccount(ctx, s.cfg.Provider, id)
}
```

Define `ErrInvalidInput` in provider or map invalid account updates through existing admin errors in HTTP. Prefer provider-local `ErrInvalidInput`.

- [ ] **Step 4: Add HTTP handlers**

Add protected handlers:

```go
mux.HandleFunc("GET /api/admin/providers/openai/accounts", requireAdmin(func(w http.ResponseWriter, r *http.Request, _ admin.Admin) {
	accounts, err := providers.ListAccounts(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal_error")
		return
	}
	writeJSON(w, http.StatusOK, map[string][]provider.Account{"accounts": accounts})
}))
```

For patch:

```go
var req struct {
	Enabled  *bool `json:"enabled"`
	Priority *int  `json:"priority"`
}
```

Reject requests with neither field.

- [ ] **Step 5: Run HTTP tests**

Run:

```bash
cd backend
GOMODCACHE=/root/Clouds/N2API/.cache/go-mod GOCACHE=/root/Clouds/N2API/.cache/go-build go test ./internal/httpapi ./internal/provider
```

Expected: pass.

- [ ] **Step 6: Commit**

```bash
git add backend/internal/httpapi/server.go backend/internal/httpapi/server_test.go backend/internal/provider/service.go
git commit -m "feat: add admin provider account endpoints"
```

## Task 6: Frontend Account Pool UI

**Files:**
- Modify: `frontend/src/routes/+page.svelte`

- [ ] **Step 1: Add account state and API calls**

In `frontend/src/routes/+page.svelte`, add typedef:

```js
/**
 * @typedef {object} ProviderAccount
 * @property {number} id
 * @property {string} provider
 * @property {string} subject
 * @property {string} displayName
 * @property {boolean} enabled
 * @property {number} priority
 * @property {string | null} accessTokenExpiresAt
 * @property {string | null} lastRefreshAt
 * @property {string | null} lastUsedAt
 * @property {string} lastError
 * @property {string | null} lastErrorAt
 */
```

Add state:

```js
let providerAccounts = $state({ loading: false, saving: 0, error: '', items: [] });
```

Add functions:

```js
async function loadProviderAccounts() {
	const version = sessionVersion;
	if (!isCurrentAuthenticated(version)) return;
	providerAccounts.loading = true;
	providerAccounts.error = '';
	try {
		const payload = await requestJSON('/api/admin/providers/openai/accounts');
		if (!isCurrentAuthenticated(version)) return;
		providerAccounts.items = payload.accounts ?? [];
	} catch (error) {
		if (!isCurrentAuthenticated(version)) return;
		providerAccounts.error = error instanceof Error ? error.message : 'Account load failed';
	} finally {
		if (isCurrentAuthenticated(version)) providerAccounts.loading = false;
	}
}
```

Call `loadProviderAccounts()` after `loadProvider()`.

- [ ] **Step 2: Add update/disconnect actions**

Add:

```js
async function updateProviderAccount(account, patch) {
	const version = sessionVersion;
	providerAccounts.saving = account.id;
	providerAccounts.error = '';
	try {
		await requestJSON(`/api/admin/providers/openai/accounts/${account.id}`, {
			method: 'PATCH',
			body: JSON.stringify(patch)
		});
		if (!isCurrentAuthenticated(version)) return;
		await loadProviderAccounts();
	} catch (error) {
		if (!isCurrentAuthenticated(version)) return;
		providerAccounts.error = error instanceof Error ? error.message : 'Account update failed';
	} finally {
		if (isCurrentAuthenticated(version)) providerAccounts.saving = 0;
	}
}
```

Add `disconnectProviderAccount(account)` using `POST /api/admin/providers/openai/accounts/{id}/disconnect`.

- [ ] **Step 3: Replace provider panel markup**

Render an account-pool section with:
- provider config status
- connect button
- account rows
- checkbox for enabled
- numeric input for priority
- disconnect button
- last error text when present

Keep styling consistent with the existing dashboard; do not add a new design system.

- [ ] **Step 4: Run frontend checks**

Run:

```bash
cd frontend
bun run check
bun run build
```

Expected: pass.

- [ ] **Step 5: Commit**

```bash
git add frontend/src/routes/+page.svelte
git commit -m "feat: show OpenAI account pool in admin UI"
```

## Task 7: Documentation and Full Verification

**Files:**
- Modify: `README.md`
- Modify: `deploy/README.md`

- [ ] **Step 1: Update README status**

In `README.md`, update `Current Status` to mention:

```markdown
The backend includes admin API key management, OpenAI/Codex OAuth account pool management, request logs, static admin UI serving, and an OpenAI-compatible gateway for `/v1/models`, `/v1/chat/completions`, and core `/v1/responses` routes. The gateway selects enabled OpenAI/Codex accounts by priority and recent use, and can fall back before response streaming begins.
```

- [ ] **Step 2: Update deployment runbook**

In `deploy/README.md`, add sections:

```markdown
## OpenAI/Codex Account Pool

Set the OAuth variables in `.env`, start the stack, log in as admin, and use the provider section to connect one or more accounts.

- Disabled accounts are kept in PostgreSQL but are not selected for gateway traffic.
- Lower priority numbers are selected before higher priority numbers.
- If one enabled account cannot refresh a token or fails before streaming starts, N2API tries another eligible account.
- Once upstream streaming has started, N2API preserves that stream and does not retry against another account.
```

Add backup warning:

```markdown
Before upgrading an existing deployment, back up PostgreSQL because the upgrade adds account-pool metadata columns to `oauth_accounts`.
```

- [ ] **Step 3: Run full verification**

Run:

```bash
cd backend
GOMODCACHE=/root/Clouds/N2API/.cache/go-mod GOCACHE=/root/Clouds/N2API/.cache/go-build go test ./...
cd ../frontend
bun run check
bun run build
```

Expected:
- Go tests pass for all packages.
- Svelte check reports 0 errors and 0 warnings.
- Frontend build exits 0.

- [ ] **Step 4: Commit**

```bash
git add README.md deploy/README.md
git commit -m "docs: document OpenAI account pool operations"
```

## Final Review Checklist

- [ ] `git status --short` shows a clean worktree.
- [ ] `git log --oneline -7` shows atomic commits for schema, store, provider, gateway, HTTP API, UI, and docs.
- [ ] The OAuth callback claims state before durable credential persistence.
- [ ] Account disconnect by id does not delete other provider accounts.
- [ ] Gateway fallback happens only before response streaming begins.
- [ ] Frontend has no competing design system and remains an operational dashboard.

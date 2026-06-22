# Unified Provider Accounts And API Key Model Policy Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Replace OAuth-specific gateway account storage with unified provider accounts, add API-upstream accounts, and move client model access policy onto API keys.

**Architecture:** Add unified provider account tables and migrate existing OAuth account data into them. Refactor the provider service and gateway to select account-type-neutral exits, then add API key model policy enforcement before provider account selection. Finish by updating admin endpoints, the Svelte admin UI, and docs so Accounts owns upstream capability and API Keys owns client access policy.

**Tech Stack:** Go, PostgreSQL/goose migrations, pgx, SvelteKit, Tailwind CSS, Bun, Docker Compose.

---

## File Structure

- Create `backend/internal/store/migrations/00008_unified_provider_accounts.sql`: unified account, credential, account-model, and API-key-model schema plus data migration.
- Modify `backend/internal/store/migrations_test.go`: migration embedding and SQL contract tests.
- Modify `backend/internal/provider/service.go`: add account type, credential type, API-upstream input, selected account output, and account-neutral selection methods.
- Modify `backend/internal/provider/service_test.go`: provider service selection, OAuth refresh, API-upstream selection, model filtering, and health behavior.
- Modify `backend/internal/store/provider.go`: move repository reads/writes from `oauth_accounts` and `oauth_account_models` to unified provider tables.
- Modify `backend/internal/store/provider_test.go`: unified schema migration behavior, OAuth data copy, API-upstream persistence, and eligible account queries.
- Modify `backend/internal/admin/service.go`: add API key model policy types, validation, and access checks.
- Modify `backend/internal/admin/service_test.go`: API key policy defaults, selected model validation, and model visibility filtering.
- Modify `backend/internal/store/admin.go`: persist API key policies and selected model rows.
- Modify `backend/internal/store/admin_test.go`: admin repository policy behavior and authentication defaults.
- Modify `backend/internal/gateway/proxy.go`: replace token-only selection with selected-account routing and API-key model policy enforcement.
- Modify `backend/internal/gateway/proxy_test.go`: account-type-specific upstream request construction, policy rejection, and `/v1/models` filtering.
- Modify `backend/internal/httpapi/server.go`: add unified provider-account endpoints and API-key policy payloads.
- Modify `backend/internal/httpapi/server_test.go`: endpoint auth, validation, response shape, and legacy route wrapper behavior.
- Modify `backend/cmd/n2api/main.go`: wire unified provider account service into the gateway.
- Modify `frontend/src/lib/admin-state.svelte.js`: add provider account and API key policy state/actions.
- Modify `frontend/src/routes/providers/+page.svelte`: evolve Providers into account-oriented controls for both account types.
- Modify `frontend/src/routes/api-keys/+page.svelte`: add gateway default model and per-key model policy controls.
- Modify `frontend/src/routes/models/+page.svelte`: replace primary model settings UI with a compatibility message linking to API Keys.
- Modify `frontend/src/routes/+layout.svelte` and `frontend/src/routes/navigation.test.mjs`: remove Models from primary navigation.
- Modify `frontend/src/routes/providers/provider-page.test.mjs`: update UI text and helper coverage for provider accounts and API key policies.
- Modify `README.md`, `deploy/README.md`: document unified accounts and API key model policy.

## Task 1: Unified Schema And Migration

**Files:**
- Create: `backend/internal/store/migrations/00008_unified_provider_accounts.sql`
- Modify: `backend/internal/store/migrations_test.go`
- Modify: `backend/internal/store/provider_test.go`

- [ ] **Step 1: Write failing migration contract tests**

Add tests to `backend/internal/store/migrations_test.go`:

```go
func TestUnifiedProviderAccountsMigrationIsEmbedded(t *testing.T) {
	sql, err := MigrationSQL("00008_unified_provider_accounts.sql")
	if err != nil {
		t.Fatalf("MigrationSQL returned error: %v", err)
	}
	for _, want := range []string{
		"CREATE TABLE IF NOT EXISTS provider_accounts",
		"CREATE TABLE IF NOT EXISTS provider_account_credentials",
		"CREATE TABLE IF NOT EXISTS provider_account_models",
		"CREATE TABLE IF NOT EXISTS client_api_key_models",
		"ALTER TABLE client_api_keys ADD COLUMN IF NOT EXISTS model_policy",
		"INSERT INTO provider_accounts",
		"FROM oauth_accounts",
		"INSERT INTO provider_account_models",
		"FROM oauth_account_models",
		"provider_accounts_schedulable_idx",
		"provider_account_models_provider_model_enabled_idx",
	} {
		if !strings.Contains(sql, want) {
			t.Fatalf("migration missing %q", want)
		}
	}
}
```

Update `TestMigrationProviderSeesEmbeddedMigrations` to expect the new final migration path `00008_unified_provider_accounts.sql`.

- [ ] **Step 2: Add failing migration data-copy contract test**

In `backend/internal/store/migrations_test.go`, add a SQL contract test for data-copy behavior. This repo's store test helpers initialize the fully migrated schema before each repository test, so the migration copy itself is verified by source contract here and repository behavior is verified after Task 2 starts reading unified tables.

```go
func TestUnifiedProviderAccountMigrationCopiesOAuthData(t *testing.T) {
	sql, err := MigrationSQL("00008_unified_provider_accounts.sql")
	if err != nil {
		t.Fatalf("MigrationSQL returned error: %v", err)
	}
	for _, want := range []string{
		"SELECT\n        id, provider, 'codex_oauth'",
		"encrypted_access_token, encrypted_refresh_token, encrypted_id_token",
		"FROM oauth_accounts",
		"SELECT id, account_id, provider, model, enabled",
		"FROM oauth_account_models",
		"client_api_keys ADD COLUMN IF NOT EXISTS model_policy TEXT NOT NULL DEFAULT 'all'",
	} {
		if !strings.Contains(sql, want) {
			t.Fatalf("migration copy SQL missing %q", want)
		}
	}
}
```

- [ ] **Step 3: Run failing tests**

```bash
cd backend
GOMODCACHE=/root/Clouds/N2API/.cache/go-mod GOCACHE=/root/Clouds/N2API/.cache/go-build go test ./internal/store -run 'UnifiedProviderAccounts|MigrationProviderSeesEmbeddedMigrations'
```

Expected: FAIL because `00008_unified_provider_accounts.sql` and provider constants do not exist yet.

- [ ] **Step 4: Add provider constants required by tests**

In `backend/internal/provider/service.go`, add:

```go
const (
	AccountTypeCodexOAuth = "codex_oauth"
	AccountTypeAPIUpstream = "api_upstream"
)

const (
	CredentialTypeOAuthToken = "oauth_token"
	CredentialTypeAPIKey = "api_key"
)
```

- [ ] **Step 5: Create migration**

Create `backend/internal/store/migrations/00008_unified_provider_accounts.sql` with:

```sql
-- +goose Up
-- +goose StatementBegin
CREATE TABLE IF NOT EXISTS provider_accounts (
    id BIGSERIAL PRIMARY KEY,
    provider TEXT NOT NULL,
    account_type TEXT NOT NULL,
    name TEXT NOT NULL DEFAULT '',
    subject TEXT NOT NULL DEFAULT '',
    display_name TEXT NOT NULL DEFAULT '',
    enabled BOOLEAN NOT NULL DEFAULT true,
    priority INTEGER NOT NULL DEFAULT 100,
    status TEXT NOT NULL DEFAULT 'active',
    status_reason TEXT NOT NULL DEFAULT '',
    last_used_at TIMESTAMPTZ,
    last_error TEXT NOT NULL DEFAULT '',
    last_error_at TIMESTAMPTZ,
    failure_count INTEGER NOT NULL DEFAULT 0,
    circuit_open_until TIMESTAMPTZ,
    rate_limited_until TIMESTAMPTZ,
    fingerprint_hash TEXT NOT NULL DEFAULT '',
    user_agent_hash TEXT NOT NULL DEFAULT '',
    ip_hash TEXT NOT NULL DEFAULT '',
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE UNIQUE INDEX IF NOT EXISTS provider_accounts_provider_type_subject_idx
    ON provider_accounts (provider, account_type, subject)
    WHERE subject <> '';

CREATE INDEX IF NOT EXISTS provider_accounts_schedulable_idx
    ON provider_accounts (provider, enabled, status, priority, last_used_at, id);

CREATE TABLE IF NOT EXISTS provider_account_credentials (
    account_id BIGINT PRIMARY KEY REFERENCES provider_accounts(id) ON DELETE CASCADE,
    credential_type TEXT NOT NULL,
    encrypted_access_token TEXT NOT NULL DEFAULT '',
    encrypted_refresh_token TEXT NOT NULL DEFAULT '',
    encrypted_id_token TEXT NOT NULL DEFAULT '',
    access_token_expires_at TIMESTAMPTZ,
    last_refresh_at TIMESTAMPTZ,
    last_refresh_error TEXT NOT NULL DEFAULT '',
    last_refresh_error_at TIMESTAMPTZ,
    encrypted_api_key TEXT NOT NULL DEFAULT '',
    base_url TEXT NOT NULL DEFAULT '',
    metadata JSONB NOT NULL DEFAULT '{}'::jsonb,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE TABLE IF NOT EXISTS provider_account_models (
    id BIGSERIAL PRIMARY KEY,
    account_id BIGINT NOT NULL REFERENCES provider_accounts(id) ON DELETE CASCADE,
    provider TEXT NOT NULL,
    model TEXT NOT NULL,
    enabled BOOLEAN NOT NULL DEFAULT true,
    source TEXT NOT NULL DEFAULT 'manual',
    last_seen_at TIMESTAMPTZ,
    last_error TEXT NOT NULL DEFAULT '',
    metadata JSONB NOT NULL DEFAULT '{}'::jsonb,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE (account_id, model)
);

CREATE INDEX IF NOT EXISTS provider_account_models_provider_model_enabled_idx
    ON provider_account_models (provider, model, enabled, account_id);

CREATE INDEX IF NOT EXISTS provider_account_models_account_enabled_idx
    ON provider_account_models (account_id, enabled, model);

ALTER TABLE client_api_keys ADD COLUMN IF NOT EXISTS model_policy TEXT NOT NULL DEFAULT 'all';

CREATE TABLE IF NOT EXISTS client_api_key_models (
    id BIGSERIAL PRIMARY KEY,
    client_key_id BIGINT NOT NULL REFERENCES client_api_keys(id) ON DELETE CASCADE,
    model TEXT NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE (client_key_id, model)
);

WITH copied_accounts AS (
    INSERT INTO provider_accounts (
        id, provider, account_type, name, subject, display_name, enabled, priority,
        status, status_reason, last_used_at, last_error, last_error_at, failure_count,
        circuit_open_until, rate_limited_until, fingerprint_hash, user_agent_hash, ip_hash,
        created_at, updated_at
    )
    SELECT
        id, provider, 'codex_oauth', name, subject, display_name, enabled, priority,
        status, status_reason, last_used_at, last_error, last_error_at, failure_count,
        circuit_open_until, rate_limited_until, fingerprint_hash, user_agent_hash, ip_hash,
        created_at, updated_at
    FROM oauth_accounts
    ON CONFLICT DO NOTHING
    RETURNING id
)
INSERT INTO provider_account_credentials (
    account_id, credential_type, encrypted_access_token, encrypted_refresh_token, encrypted_id_token,
    access_token_expires_at, last_refresh_at, last_refresh_error, last_refresh_error_at,
    metadata, created_at, updated_at
)
SELECT
    id, 'oauth_token', encrypted_access_token, encrypted_refresh_token, encrypted_id_token,
    access_token_expires_at, last_refresh_at, last_refresh_error, last_refresh_error_at,
    metadata, created_at, updated_at
FROM oauth_accounts
ON CONFLICT (account_id) DO NOTHING;

INSERT INTO provider_account_models (
    id, account_id, provider, model, enabled, source, last_seen_at, last_error, metadata, created_at, updated_at
)
SELECT id, account_id, provider, model, enabled, source, last_seen_at, last_error, metadata, created_at, updated_at
FROM oauth_account_models
ON CONFLICT DO NOTHING;

SELECT setval(pg_get_serial_sequence('provider_accounts', 'id'), COALESCE((SELECT MAX(id) FROM provider_accounts), 1), true);
SELECT setval(pg_get_serial_sequence('provider_account_models', 'id'), COALESCE((SELECT MAX(id) FROM provider_account_models), 1), true);
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
DROP TABLE IF EXISTS client_api_key_models;
ALTER TABLE client_api_keys DROP COLUMN IF EXISTS model_policy;
DROP INDEX IF EXISTS provider_account_models_account_enabled_idx;
DROP INDEX IF EXISTS provider_account_models_provider_model_enabled_idx;
DROP TABLE IF EXISTS provider_account_models;
DROP TABLE IF EXISTS provider_account_credentials;
DROP INDEX IF EXISTS provider_accounts_schedulable_idx;
DROP INDEX IF EXISTS provider_accounts_provider_type_subject_idx;
DROP TABLE IF EXISTS provider_accounts;
-- +goose StatementEnd
```

- [ ] **Step 6: Run focused tests**

```bash
cd backend
GOMODCACHE=/root/Clouds/N2API/.cache/go-mod GOCACHE=/root/Clouds/N2API/.cache/go-build go test ./internal/store -run 'UnifiedProviderAccounts|MigrationProviderSeesEmbeddedMigrations'
```

Expected: PASS.

- [ ] **Step 7: Commit**

```bash
git add backend/internal/store/migrations/00008_unified_provider_accounts.sql backend/internal/store/migrations_test.go backend/internal/store/provider_test.go backend/internal/provider/service.go
git commit -m "feat: add unified provider account schema"
```

## Task 2: Provider Domain Types And Repository

**Files:**
- Modify: `backend/internal/provider/service.go`
- Modify: `backend/internal/provider/service_test.go`
- Modify: `backend/internal/store/provider.go`
- Modify: `backend/internal/store/provider_test.go`

- [ ] **Step 1: Write failing provider type and repository tests**

In `backend/internal/store/provider_test.go`, add tests:

```go
func TestProviderRepositorySavesAPIUpstreamAccount(t *testing.T) {
	repo, ctx := newTestProviderRepository(t)
	account := provider.Account{
		Provider: "openai",
		AccountType: provider.AccountTypeAPIUpstream,
		Name: "OpenRouter",
		Enabled: true,
		Priority: 20,
		Status: provider.AccountStatusActive,
		Credential: provider.AccountCredential{
			CredentialType: provider.CredentialTypeAPIKey,
			EncryptedAPIKey: "encrypted-upstream-key",
			BaseURL: "https://openrouter.ai/api",
		},
	}

	saved, err := repo.SaveAccount(ctx, account)
	if err != nil {
		t.Fatalf("SaveAccount returned error: %v", err)
	}
	if saved.AccountType != provider.AccountTypeAPIUpstream || saved.Credential.BaseURL != "https://openrouter.ai/api" {
		t.Fatalf("saved account = %+v", saved)
	}
}

func TestProviderRepositoryListEligibleAccountsForModelUsesUnifiedTables(t *testing.T) {
	repo, ctx := newTestProviderRepository(t)
	oauth := saveTestProviderAccount(t, repo, ctx, "openai", "oauth-model")
	api := provider.Account{
		Provider: "openai",
		AccountType: provider.AccountTypeAPIUpstream,
		Name: "api upstream",
		Enabled: true,
		Priority: 5,
		Status: provider.AccountStatusActive,
		Credential: provider.AccountCredential{CredentialType: provider.CredentialTypeAPIKey, EncryptedAPIKey: "enc", BaseURL: "https://api.example.com"},
	}
	api, err := repo.SaveAccount(ctx, api)
	if err != nil {
		t.Fatalf("SaveAccount API upstream returned error: %v", err)
	}
	_, _ = repo.ReplaceAccountModels(ctx, "openai", oauth.ID, []provider.AccountModelInput{{Model: "gpt-5", Enabled: true}})
	_, _ = repo.ReplaceAccountModels(ctx, "openai", api.ID, []provider.AccountModelInput{{Model: "gpt-5", Enabled: true}})

	accounts, err := repo.ListEligibleAccountsForModel(ctx, "openai", "gpt-5", nil, time.Now())
	if err != nil {
		t.Fatalf("ListEligibleAccountsForModel returned error: %v", err)
	}
	if len(accounts) != 2 || accounts[0].ID != api.ID || accounts[1].ID != oauth.ID {
		t.Fatalf("accounts = %+v, want priority-ordered api then oauth", accounts)
	}
}
```

- [ ] **Step 2: Add account credential fields to provider types**

In `backend/internal/provider/service.go`, update `Account` and add credential types:

```go
type Account struct {
	ID                    int64             `json:"id"`
	Provider              string            `json:"provider"`
	AccountType           string            `json:"accountType"`
	Subject               string            `json:"subject"`
	Name                  string            `json:"name"`
	DisplayName           string            `json:"displayName"`
	Credential            AccountCredential `json:"-"`
	EncryptedAccessToken  string            `json:"-"`
	EncryptedRefreshToken string            `json:"-"`
	EncryptedIDToken      string            `json:"-"`
	AccessTokenExpiresAt  *time.Time        `json:"accessTokenExpiresAt"`
	LastRefreshAt         *time.Time        `json:"lastRefreshAt"`
	Enabled               bool              `json:"enabled"`
	Priority              int               `json:"priority"`
	LastUsedAt            *time.Time        `json:"lastUsedAt"`
	LastError             string            `json:"lastError"`
	LastErrorAt           *time.Time        `json:"lastErrorAt"`
	Metadata              map[string]string `json:"metadata"`
	Status                string            `json:"status"`
	StatusReason          string            `json:"statusReason"`
	FingerprintHash       string            `json:"fingerprintHash"`
	UserAgentHash         string            `json:"userAgentHash"`
	IPHash                string            `json:"ipHash"`
	FailureCount          int               `json:"failureCount"`
	CircuitOpenUntil      *time.Time        `json:"circuitOpenUntil"`
	RateLimitedUntil      *time.Time        `json:"rateLimitedUntil"`
	LastRefreshError      string            `json:"lastRefreshError"`
	LastRefreshErrorAt    *time.Time        `json:"lastRefreshErrorAt"`
	CreatedAt             time.Time         `json:"createdAt"`
	UpdatedAt             time.Time         `json:"updatedAt"`
}

type AccountCredential struct {
	CredentialType        string            `json:"credentialType"`
	EncryptedAccessToken string            `json:"-"`
	EncryptedRefreshToken string           `json:"-"`
	EncryptedIDToken     string            `json:"-"`
	AccessTokenExpiresAt *time.Time        `json:"accessTokenExpiresAt"`
	LastRefreshAt        *time.Time        `json:"lastRefreshAt"`
	LastRefreshError     string            `json:"lastRefreshError"`
	LastRefreshErrorAt   *time.Time        `json:"lastRefreshErrorAt"`
	EncryptedAPIKey      string            `json:"-"`
	BaseURL              string            `json:"baseUrl"`
	Metadata             map[string]string `json:"metadata"`
}
```

Keep legacy encrypted token fields on `Account` during this task so existing OAuth code compiles. Task 3 switches gateway selection to read authorization data through `SelectedAccount`.

- [ ] **Step 3: Move repository SQL to unified tables**

In `backend/internal/store/provider.go`, replace account column constants with unified-table joins:

```go
const providerAccountColumnsFromAlias = `
	a.id, a.provider, a.account_type, a.subject, a.name, a.display_name,
	c.encrypted_access_token, c.encrypted_refresh_token, c.encrypted_id_token,
	c.access_token_expires_at, c.last_refresh_at, a.enabled, a.priority, a.last_used_at,
	a.last_error, a.last_error_at, c.metadata, a.status, a.status_reason,
	a.fingerprint_hash, a.user_agent_hash, a.ip_hash, a.failure_count, a.circuit_open_until,
	a.rate_limited_until, c.last_refresh_error, c.last_refresh_error_at, a.created_at, a.updated_at,
	c.credential_type, c.encrypted_api_key, c.base_url
`
```

Update `scanProviderAccount` to scan `AccountType`, populate both legacy OAuth fields and `Credential`, and set `Credential.Metadata = account.Metadata`.

Update every query to read from:

```sql
FROM provider_accounts a
JOIN provider_account_credentials c ON c.account_id = a.id
```

Update `SaveAccount` to upsert `provider_accounts` and `provider_account_credentials` in one transaction.

Update account-model methods to use `provider_account_models`.

- [ ] **Step 4: Run repository tests**

```bash
cd backend
GOMODCACHE=/root/Clouds/N2API/.cache/go-mod GOCACHE=/root/Clouds/N2API/.cache/go-build go test ./internal/store ./internal/provider
```

Expected: PASS after repository and memory fakes are updated.

- [ ] **Step 5: Commit**

```bash
git add backend/internal/provider/service.go backend/internal/provider/service_test.go backend/internal/store/provider.go backend/internal/store/provider_test.go
git commit -m "refactor: use unified provider account repository"
```

## Task 3: Account-Neutral Gateway Selection

**Files:**
- Modify: `backend/internal/provider/service.go`
- Modify: `backend/internal/provider/service_test.go`
- Modify: `backend/internal/gateway/proxy.go`
- Modify: `backend/internal/gateway/proxy_test.go`
- Modify: `backend/cmd/n2api/main.go`

- [ ] **Step 1: Write failing gateway tests for account types**

In `backend/internal/gateway/proxy_test.go`, replace token-only fake selection with selected accounts and add:

```go
func TestProxyRoutesAPIUpstreamAccountToConfiguredBaseURL(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer upstream-secret" {
			t.Fatalf("Authorization = %q", r.Header.Get("Authorization"))
		}
		if r.URL.Path != "/v1/chat/completions" {
			t.Fatalf("path = %q", r.URL.Path)
		}
		_, _ = w.Write([]byte(`{"ok":true}`))
	}))
	defer upstream.Close()

	accounts := &fakeSelectedAccountProvider{selected: []SelectedAccount{{
		AccountID: 9,
		AccountType: provider.AccountTypeAPIUpstream,
		AuthorizationToken: "upstream-secret",
		BaseURL: upstream.URL,
	}}}
	proxy := NewProxy(fakeAPIKeyAuthenticator{key: admin.APIKey{ID: 1}}, accounts, Config{
		ModelProvider: fakeModelProvider{defaultModel: "gpt-5", allowedModels: []string{"gpt-5"}},
	})

	req := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", strings.NewReader(`{"model":"gpt-5","messages":[]}`))
	req.Header.Set("Authorization", "Bearer n2api_test")
	recorder := httptest.NewRecorder()
	proxy.ServeHTTP(recorder, req)

	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, body=%s", recorder.Code, recorder.Body.String())
	}
}
```

Add a second test for `codex_oauth` responses requests preserving the Codex endpoint behavior.

- [ ] **Step 2: Add selected account contract**

In `backend/internal/provider/service.go`, add:

```go
type SelectedAccount struct {
	AccountID          int64
	Provider           string
	AccountType        string
	AuthorizationToken string
	BaseURL            string
	ChatGPTAccountID   string
}
```

Change selection method:

```go
func (s *Service) SelectAccountForModel(ctx context.Context, model string, excludedAccountIDs ...int64) (SelectedAccount, error)
```

Keep `SelectAccessToken` temporarily as a wrapper for compile compatibility while `cmd/n2api` and tests are being converted in this task:

```go
func (s *Service) SelectAccessToken(ctx context.Context, model string, excludedAccountIDs ...int64) (SelectedToken, error) {
	selected, err := s.SelectAccountForModel(ctx, model, excludedAccountIDs...)
	if err != nil {
		return SelectedToken{}, err
	}
	return SelectedToken{AccountID: selected.AccountID, Token: selected.AuthorizationToken, ChatGPTAccountID: selected.ChatGPTAccountID}, nil
}
```

- [ ] **Step 3: Update gateway interfaces and upstream request builder**

In `backend/internal/gateway/proxy.go`, replace `SelectedToken` and `AccessTokenProvider` with:

```go
type SelectedAccount struct {
	AccountID          int64
	Provider           string
	AccountType        string
	AuthorizationToken string
	BaseURL            string
	ChatGPTAccountID   string
}

type AccountProvider interface {
	SelectAccountForModel(ctx context.Context, model string, excludedAccountIDs ...int64) (SelectedAccount, error)
}
```

Update `newUpstreamRequest` to choose base URL from selected account:

```go
func (p *Proxy) newUpstreamRequest(r *http.Request, selected SelectedAccount, body io.ReadCloser) (*http.Request, error) {
	useCodexEndpoint := selected.AccountType == provider.AccountTypeCodexOAuth &&
		r.Method == http.MethodPost &&
		r.URL.Path == "/v1/responses" &&
		strings.TrimSpace(selected.ChatGPTAccountID) != ""

	upstreamPath := r.URL.Path
	upstreamBaseURL := strings.TrimRight(strings.TrimSpace(selected.BaseURL), "/")
	if upstreamBaseURL == "" {
		upstreamBaseURL = p.upstreamBaseURL
	}
	if useCodexEndpoint {
		upstreamBaseURL = p.codexBaseURL
		upstreamPath = "/responses"
		var err error
		body, err = normalizeCodexResponsesBody(body)
		if err != nil {
			return nil, err
		}
	}
	upstreamURL, err := url.Parse(upstreamBaseURL + upstreamPath)
	if err != nil {
		return nil, fmt.Errorf("parse upstream url: %w", err)
	}
	upstreamURL.RawQuery = r.URL.RawQuery
	req, err := http.NewRequestWithContext(r.Context(), r.Method, upstreamURL.String(), body)
	if err != nil {
		return nil, err
	}
	copyRequestHeaders(req.Header, r.Header)
	req.Header.Set("Authorization", "Bearer "+selected.AuthorizationToken)
	if useCodexEndpoint {
		req.Header.Set("chatgpt-account-id", strings.TrimSpace(selected.ChatGPTAccountID))
		req.Header.Set("Accept", "text/event-stream")
		req.Header.Set("OpenAI-Beta", "responses=experimental")
		req.Header.Set("originator", "codex_cli_rs")
		req.Header.Set("User-Agent", defaultCodexUserAgent)
		req.Header.Set("Content-Type", "application/json")
	}
	return req, nil
}
```

- [ ] **Step 4: Update production wrapper**

In `backend/cmd/n2api/main.go`, rename the gateway wrapper method to `SelectAccountForModel` and map `provider.SelectedAccount` to `gateway.SelectedAccount`.

- [ ] **Step 5: Run gateway and cmd tests**

```bash
cd backend
GOMODCACHE=/root/Clouds/N2API/.cache/go-mod GOCACHE=/root/Clouds/N2API/.cache/go-build go test ./internal/gateway ./internal/provider ./cmd/n2api
```

Expected: PASS.

- [ ] **Step 6: Commit**

```bash
git add backend/internal/provider/service.go backend/internal/provider/service_test.go backend/internal/gateway/proxy.go backend/internal/gateway/proxy_test.go backend/cmd/n2api/main.go
git commit -m "refactor: route through selected provider accounts"
```

## Task 4: API Key Model Policy Backend

**Files:**
- Modify: `backend/internal/admin/service.go`
- Modify: `backend/internal/admin/service_test.go`
- Modify: `backend/internal/store/admin.go`
- Modify: `backend/internal/store/admin_test.go`
- Modify: `backend/internal/gateway/proxy.go`
- Modify: `backend/internal/gateway/proxy_test.go`

- [ ] **Step 1: Write failing admin policy tests**

In `backend/internal/admin/service_test.go`, add:

```go
func TestAPIKeyModelPolicyDefaultsToAll(t *testing.T) {
	repo := newMemoryRepository()
	service := NewService(repo, Config{})
	created, err := service.CreateAPIKey(context.Background(), "codex")
	if err != nil {
		t.Fatalf("CreateAPIKey returned error: %v", err)
	}
	key, err := service.AuthenticateAPIKey(context.Background(), created.Secret)
	if err != nil {
		t.Fatalf("AuthenticateAPIKey returned error: %v", err)
	}
	if key.ModelPolicy != APIKeyModelPolicyAll {
		t.Fatalf("ModelPolicy = %q, want %q", key.ModelPolicy, APIKeyModelPolicyAll)
	}
}

func TestAPIKeySelectedModelPolicy(t *testing.T) {
	repo := newMemoryRepository()
	service := NewService(repo, Config{})
	created, _ := service.CreateAPIKey(context.Background(), "limited")

	updated, err := service.UpdateAPIKeyModelPolicy(context.Background(), created.Key.ID, APIKeyModelPolicySelected, []string{" gpt-5 ", "gpt-5"})
	if err != nil {
		t.Fatalf("UpdateAPIKeyModelPolicy returned error: %v", err)
	}
	if updated.ModelPolicy != APIKeyModelPolicySelected || strings.Join(updated.AllowedModels, ",") != "gpt-5" {
		t.Fatalf("updated key = %+v", updated)
	}
	if !service.APIKeyAllowsModel(updated, "gpt-5") {
		t.Fatalf("selected key should allow gpt-5")
	}
	if service.APIKeyAllowsModel(updated, "gpt-4.1") {
		t.Fatalf("selected key should reject gpt-4.1")
	}
}
```

- [ ] **Step 2: Add API key policy types**

In `backend/internal/admin/service.go`, add:

```go
const (
	APIKeyModelPolicyAll = "all"
	APIKeyModelPolicySelected = "selected"
)

type APIKey struct {
	ID            int64      `json:"id"`
	Name          string     `json:"name"`
	Prefix        string     `json:"prefix"`
	ModelPolicy   string     `json:"modelPolicy"`
	AllowedModels []string   `json:"allowedModels"`
	CreatedAt     time.Time  `json:"createdAt"`
	LastUsedAt    *time.Time `json:"lastUsedAt"`
	RevokedAt     *time.Time `json:"revokedAt"`
}
```

Extend repository interface:

```go
UpdateAPIKeyModelPolicy(ctx context.Context, id int64, policy string, models []string) (APIKey, error)
ListAPIKeyModels(ctx context.Context, id int64) ([]string, error)
```

Add validation helpers:

```go
func (s *Service) UpdateAPIKeyModelPolicy(ctx context.Context, id int64, policy string, models []string) (APIKey, error) {
	policy = strings.TrimSpace(policy)
	if policy != APIKeyModelPolicyAll && policy != APIKeyModelPolicySelected {
		return APIKey{}, ErrInvalidInput
	}
	normalized, err := normalizeModelList(models)
	if err != nil {
		return APIKey{}, err
	}
	if policy == APIKeyModelPolicyAll {
		normalized = nil
	}
	if policy == APIKeyModelPolicySelected && len(normalized) == 0 {
		return APIKey{}, ErrInvalidInput
	}
	return s.repo.UpdateAPIKeyModelPolicy(ctx, id, policy, normalized)
}

func (s *Service) APIKeyAllowsModel(key APIKey, model string) bool {
	model = strings.TrimSpace(model)
	if key.ModelPolicy == "" || key.ModelPolicy == APIKeyModelPolicyAll {
		return true
	}
	if key.ModelPolicy != APIKeyModelPolicySelected {
		return false
	}
	for _, allowed := range key.AllowedModels {
		if allowed == model {
			return true
		}
	}
	return false
}
```

- [ ] **Step 3: Persist policy in admin store**

In `backend/internal/store/admin.go`, include `model_policy` in API key select/insert scans and implement `UpdateAPIKeyModelPolicy` in a transaction:

```go
func (r *AdminRepository) UpdateAPIKeyModelPolicy(ctx context.Context, id int64, policy string, models []string) (admin.APIKey, error) {
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return admin.APIKey{}, err
	}
	defer tx.Rollback(ctx)

	row := tx.QueryRow(ctx, `
		UPDATE client_api_keys
		SET model_policy = $2
		WHERE id = $1 AND revoked_at IS NULL
		RETURNING id, name, prefix, model_policy, created_at, last_used_at, revoked_at
	`, id, policy)
	key, err := scanAPIKey(row)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return admin.APIKey{}, admin.ErrNotFound
		}
		return admin.APIKey{}, err
	}
	if _, err := tx.Exec(ctx, `DELETE FROM client_api_key_models WHERE client_key_id = $1`, id); err != nil {
		return admin.APIKey{}, err
	}
	for _, model := range models {
		if _, err := tx.Exec(ctx, `
			INSERT INTO client_api_key_models (client_key_id, model)
			VALUES ($1, $2)
			ON CONFLICT (client_key_id, model) DO NOTHING
		`, id, model); err != nil {
			return admin.APIKey{}, err
		}
	}
	if err := tx.Commit(ctx); err != nil {
		return admin.APIKey{}, err
	}
	key.AllowedModels = append([]string(nil), models...)
	return key, nil
}
```

- [ ] **Step 4: Enforce policy in gateway**

In `backend/internal/gateway/proxy.go`, after model normalization and before account selection:

```go
if !apiKeyAllowsModel(key, model) {
	errorCode = "model_not_found"
	writeOpenAIError(recorder, http.StatusNotFound, errorCode, "requested model is not available")
	return
}
```

Add helper:

```go
func apiKeyAllowsModel(key admin.APIKey, model string) bool {
	if key.ModelPolicy == "" || key.ModelPolicy == admin.APIKeyModelPolicyAll {
		return true
	}
	if key.ModelPolicy != admin.APIKeyModelPolicySelected {
		return false
	}
	model = strings.TrimSpace(model)
	for _, allowed := range key.AllowedModels {
		if strings.TrimSpace(allowed) == model {
			return true
		}
	}
	return false
}
```

Filter `/v1/models` by key policy by changing `writeLocalModels(ctx, w)` to `writeLocalModels(ctx, w, key)`.

- [ ] **Step 5: Run tests**

```bash
cd backend
GOMODCACHE=/root/Clouds/N2API/.cache/go-mod GOCACHE=/root/Clouds/N2API/.cache/go-build go test ./internal/admin ./internal/store ./internal/gateway
```

Expected: PASS.

- [ ] **Step 6: Commit**

```bash
git add backend/internal/admin/service.go backend/internal/admin/service_test.go backend/internal/store/admin.go backend/internal/store/admin_test.go backend/internal/gateway/proxy.go backend/internal/gateway/proxy_test.go
git commit -m "feat: enforce api key model policy"
```

## Task 5: Unified Admin HTTP API

**Files:**
- Modify: `backend/internal/httpapi/server.go`
- Modify: `backend/internal/httpapi/server_test.go`
- Modify: `backend/internal/provider/service.go`
- Modify: `backend/internal/provider/service_test.go`

- [ ] **Step 1: Write failing HTTP API tests**

In `backend/internal/httpapi/server_test.go`, add:

```go
func TestAdminProviderAccountsEndpointsRequireSession(t *testing.T) {
	server := newTestServer(t)
	for _, tc := range []struct {
		method string
		path string
		body string
	}{
		{http.MethodGet, "/api/admin/provider-accounts", ""},
		{http.MethodPost, "/api/admin/provider-accounts/api-upstream", `{"name":"upstream","baseUrl":"https://api.example.com","apiKey":"sk-test","models":["gpt-5"]}`},
		{http.MethodPatch, "/api/admin/provider-accounts/1", `{"enabled":false}`},
		{http.MethodPut, "/api/admin/provider-accounts/1/models", `{"models":[{"model":"gpt-5","enabled":true}]}`},
	} {
		req := httptest.NewRequest(tc.method, tc.path, strings.NewReader(tc.body))
		recorder := httptest.NewRecorder()
		server.ServeHTTP(recorder, req)
		if recorder.Code != http.StatusUnauthorized {
			t.Fatalf("%s %s status = %d, want 401", tc.method, tc.path, recorder.Code)
		}
	}
}

func TestCreateAPIUpstreamAccount(t *testing.T) {
	server, _, providers := newAuthenticatedTestServer(t)
	req := httptest.NewRequest(http.MethodPost, "/api/admin/provider-accounts/api-upstream", strings.NewReader(`{
		"name":"OpenRouter",
		"baseUrl":"https://openrouter.ai/api",
		"apiKey":"sk-upstream",
		"priority":25,
		"enabled":true,
		"models":["gpt-5"]
	}`))
	req.Header.Set("Content-Type", "application/json")
	attachAdminSession(req)
	recorder := httptest.NewRecorder()
	server.ServeHTTP(recorder, req)
	if recorder.Code != http.StatusCreated {
		t.Fatalf("status = %d, body=%s", recorder.Code, recorder.Body.String())
	}
	if providers.createdAPIUpstream.Name != "OpenRouter" {
		t.Fatalf("created upstream = %+v", providers.createdAPIUpstream)
	}
}
```

- [ ] **Step 2: Add provider service API-upstream method**

In `backend/internal/provider/service.go`, add:

```go
type APIUpstreamInput struct {
	Name     string   `json:"name"`
	BaseURL  string   `json:"baseUrl"`
	APIKey   string   `json:"apiKey"`
	Enabled  bool     `json:"enabled"`
	Priority int      `json:"priority"`
	Models   []string `json:"models"`
}

func (s *Service) CreateAPIUpstreamAccount(ctx context.Context, input APIUpstreamInput) (Account, error) {
	name := strings.TrimSpace(input.Name)
	baseURL := strings.TrimRight(strings.TrimSpace(input.BaseURL), "/")
	apiKey := strings.TrimSpace(input.APIKey)
	if name == "" || baseURL == "" || apiKey == "" {
		return Account{}, ErrInvalidInput
	}
	if _, err := url.ParseRequestURI(baseURL); err != nil {
		return Account{}, ErrInvalidInput
	}
	encrypted, err := s.encrypt(apiKey)
	if err != nil {
		return Account{}, err
	}
	account := Account{
		Provider: s.cfg.Provider,
		AccountType: AccountTypeAPIUpstream,
		Name: name,
		Enabled: input.Enabled,
		Priority: input.Priority,
		Status: AccountStatusActive,
		Credential: AccountCredential{
			CredentialType: CredentialTypeAPIKey,
			EncryptedAPIKey: encrypted,
			BaseURL: baseURL,
		},
	}
	saved, err := s.repo.SaveAccount(ctx, account)
	if err != nil {
		return Account{}, err
	}
	modelInputs := make([]AccountModelInput, 0, len(input.Models))
	for _, model := range input.Models {
		modelInputs = append(modelInputs, AccountModelInput{Model: model, Enabled: true})
	}
	if len(modelInputs) > 0 {
		if _, err := s.ReplaceAccountModels(ctx, saved.ID, modelInputs); err != nil {
			return Account{}, err
		}
	}
	return saved, nil
}
```

- [ ] **Step 3: Add HTTP routes**

In `backend/internal/httpapi/server.go`, add protected routes:

```go
mux.HandleFunc("GET /api/admin/provider-accounts", requireAdmin(func(w http.ResponseWriter, r *http.Request, _ admin.Admin) {
	accounts, err := providers.ListAccounts(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, "list provider accounts")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"accounts": accounts})
}))

mux.HandleFunc("POST /api/admin/provider-accounts/api-upstream", requireAdmin(func(w http.ResponseWriter, r *http.Request, _ admin.Admin) {
	var req provider.APIUpstreamInput
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request")
		return
	}
	account, err := providers.CreateAPIUpstreamAccount(r.Context(), req)
	if err != nil {
		status := http.StatusInternalServerError
		if errors.Is(err, provider.ErrInvalidInput) {
			status = http.StatusBadRequest
		}
		writeError(w, status, err.Error())
		return
	}
	writeJSON(w, http.StatusCreated, map[string]any{"account": account})
}))
```

Add `PATCH /api/admin/provider-accounts/{id}`:

```go
mux.HandleFunc("PATCH /api/admin/provider-accounts/{id}", requireAdmin(func(w http.ResponseWriter, r *http.Request, _ admin.Admin) {
	id, err := pathID(r, "id")
	if err != nil {
		writeError(w, http.StatusNotFound, "not found")
		return
	}
	var req struct {
		Enabled *bool `json:"enabled"`
		Priority *int `json:"priority"`
	}
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request")
		return
	}
	account, err := providers.UpdateAccount(r.Context(), id, provider.AccountUpdate{Enabled: req.Enabled, Priority: req.Priority})
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"account": account})
}))
```

Add `GET /api/admin/provider-accounts/{id}/models` and `PUT /api/admin/provider-accounts/{id}/models` with the same payload shape as the existing `/api/admin/providers/openai/accounts/{id}/models` routes, but route through the unified provider service.

Add `PUT /api/admin/keys/{id}/model-policy`:

```go
mux.HandleFunc("PUT /api/admin/keys/{id}/model-policy", requireAdmin(func(w http.ResponseWriter, r *http.Request, _ admin.Admin) {
	id, err := pathID(r, "id")
	if err != nil {
		writeError(w, http.StatusNotFound, "not found")
		return
	}
	var req struct {
		ModelPolicy string `json:"modelPolicy"`
		Models []string `json:"models"`
	}
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request")
		return
	}
	key, err := admins.UpdateAPIKeyModelPolicy(r.Context(), id, req.ModelPolicy, req.Models)
	if err != nil {
		status := http.StatusInternalServerError
		if errors.Is(err, admin.ErrInvalidInput) {
			status = http.StatusBadRequest
		}
		if errors.Is(err, admin.ErrNotFound) {
			status = http.StatusNotFound
		}
		writeError(w, status, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"key": key})
}))
```

- [ ] **Step 4: Run HTTP API tests**

```bash
cd backend
GOMODCACHE=/root/Clouds/N2API/.cache/go-mod GOCACHE=/root/Clouds/N2API/.cache/go-build go test ./internal/httpapi ./internal/provider
```

Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add backend/internal/httpapi/server.go backend/internal/httpapi/server_test.go backend/internal/provider/service.go backend/internal/provider/service_test.go
git commit -m "feat: add unified provider account admin api"
```

## Task 6: Frontend Admin IA And State

**Files:**
- Modify: `frontend/src/lib/admin-state.svelte.js`
- Modify: `frontend/src/routes/providers/+page.svelte`
- Modify: `frontend/src/routes/api-keys/+page.svelte`
- Modify: `frontend/src/routes/models/+page.svelte`
- Modify: `frontend/src/routes/+layout.svelte`
- Modify: `frontend/src/routes/navigation.test.mjs`
- Modify: `frontend/src/routes/providers/provider-page.test.mjs`

- [ ] **Step 1: Write failing frontend source tests**

In `frontend/src/routes/providers/provider-page.test.mjs`, add assertions:

```js
test('providers page is account-oriented and supports api upstream accounts', () => {
  assert.match(source, /Provider accounts/);
  assert.match(source, /Codex OAuth/);
  assert.match(source, /API upstream/);
  assert.match(source, /Base URL/);
  assert.match(source, /Manual models/);
});

test('api keys page owns model policy and gateway default model', () => {
  const apiKeysSource = readFileSync('src/routes/api-keys/+page.svelte', 'utf8');
  assert.match(apiKeysSource, /Gateway default model/);
  assert.match(apiKeysSource, /Model access/);
  assert.match(apiKeysSource, /All routable models/);
  assert.match(apiKeysSource, /Selected models/);
});
```

In `frontend/src/routes/navigation.test.mjs`, assert Models is not a primary nav item:

```js
test('primary navigation no longer exposes standalone models page', () => {
  assert.doesNotMatch(layoutSource, /href="\/models"/);
  assert.match(layoutSource, /href="\/api-keys"/);
});
```

- [ ] **Step 2: Update admin state actions**

In `frontend/src/lib/admin-state.svelte.js`, add state/actions:

```js
export let providerAccounts = $state({
  loading: false,
  saving: false,
  items: [],
  error: ''
});

export let apiUpstreamForm = $state({
  name: '',
  baseUrl: '',
  apiKey: '',
  priority: 100,
  enabled: true,
  modelsText: '',
  submitting: false,
  error: ''
});

export function parseModelLines(text) {
  const seen = new Set();
  return String(text || '')
    .split('\n')
    .map((line) => line.trim())
    .filter((model) => {
      if (!model || seen.has(model)) return false;
      seen.add(model);
      return true;
    });
}

export async function loadProviderAccounts() {
  providerAccounts.loading = true;
  providerAccounts.error = '';
  try {
    const data = await apiFetch('/api/admin/provider-accounts');
    providerAccounts.items = data.accounts || [];
  } catch (error) {
    providerAccounts.error = error.message;
  } finally {
    providerAccounts.loading = false;
  }
}

export async function createAPIUpstreamAccount() {
  apiUpstreamForm.submitting = true;
  apiUpstreamForm.error = '';
  try {
    await apiFetch('/api/admin/provider-accounts/api-upstream', {
      method: 'POST',
      body: JSON.stringify({
        name: apiUpstreamForm.name,
        baseUrl: apiUpstreamForm.baseUrl,
        apiKey: apiUpstreamForm.apiKey,
        priority: Number(apiUpstreamForm.priority) || 100,
        enabled: apiUpstreamForm.enabled,
        models: parseModelLines(apiUpstreamForm.modelsText)
      })
    });
    apiUpstreamForm.apiKey = '';
    await loadProviderAccounts();
  } catch (error) {
    apiUpstreamForm.error = error.message;
  } finally {
    apiUpstreamForm.submitting = false;
  }
}
```

Add API key policy action:

```js
export async function updateAPIKeyModelPolicy(keyId, modelPolicy, modelsText) {
  const data = await apiFetch(`/api/admin/keys/${keyId}/model-policy`, {
    method: 'PUT',
    body: JSON.stringify({
      modelPolicy,
      models: parseModelLines(modelsText)
    })
  });
  apiKeys.items = apiKeys.items.map((key) => key.id === keyId ? data.key : key);
}
```

- [ ] **Step 3: Update Providers page**

Change the page heading to `Provider accounts`. Add an API-upstream form with fields `Name`, `Base URL`, `API key`, `Priority`, `Enabled`, and `Manual models`. Reuse the existing account table for both `codex_oauth` and `api_upstream`; show account type as a compact text label.

- [ ] **Step 4: Update API Keys page**

Move model settings UI from `models/+page.svelte` into `api-keys/+page.svelte`. For each API key, add a select or segmented control for `All routable models` vs `Selected models`, and a textarea for selected models when the key uses the selected policy.

- [ ] **Step 5: De-emphasize Models route and nav**

Remove `/models` from sidebar navigation in `frontend/src/routes/+layout.svelte`. Replace `frontend/src/routes/models/+page.svelte` content with a compact compatibility view that links to `/api-keys`.

- [ ] **Step 6: Run frontend checks**

```bash
cd frontend
bun run check
bun run build
```

Expected: PASS.

- [ ] **Step 7: Commit**

```bash
git add frontend/src/lib/admin-state.svelte.js frontend/src/routes/providers/+page.svelte frontend/src/routes/api-keys/+page.svelte frontend/src/routes/models/+page.svelte frontend/src/routes/+layout.svelte frontend/src/routes/navigation.test.mjs frontend/src/routes/providers/provider-page.test.mjs
git commit -m "feat: manage accounts and api key model policy"
```

## Task 7: Docs And Compatibility Cleanup

**Files:**
- Modify: `README.md`
- Modify: `deploy/README.md`
- Modify: `backend/internal/httpapi/server.go`
- Modify: `backend/internal/httpapi/server_test.go`
- Modify: `backend/internal/store/provider.go`
- Modify: `backend/internal/provider/service.go`

- [ ] **Step 1: Write legacy wrapper tests**

Keep legacy `/api/admin/providers/openai/...` routes for one compatibility window. Add tests that verify they delegate to unified account behavior:

```go
func TestLegacyProviderAccountModelsRouteDelegatesToUnifiedModels(t *testing.T) {
	server, _, providers := newAuthenticatedTestServer(t)
	providers.accountModels[7] = []provider.AccountModel{{AccountID: 7, Provider: "openai", Model: "gpt-5", Enabled: true}}
	req := httptest.NewRequest(http.MethodGet, "/api/admin/providers/openai/accounts/7/models", nil)
	attachAdminSession(req)
	recorder := httptest.NewRecorder()
	server.ServeHTTP(recorder, req)
	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, body=%s", recorder.Code, recorder.Body.String())
	}
}
```

- [ ] **Step 2: Remove long-term OAuth-specific gateway names**

Rename internal production-facing concepts:
- `gatewayTokenProvider` -> `gatewayAccountProvider`
- `SelectedToken` -> `SelectedAccount`
- `oauth_account_models` references in non-migration code -> `provider_account_models`
- `oauth_accounts` references in non-migration code -> `provider_accounts`

Keep old table names only in migration copy logic and historical docs.

- [ ] **Step 3: Update docs**

In `README.md` and `deploy/README.md`, document:

```markdown
## Provider Accounts

Provider accounts are gateway exits. N2API supports Codex OAuth accounts and API-key upstream accounts. Both account types share enabled state, priority, health status, and per-account model lists.

## API Key Model Access

Client API keys default to all routable models. For narrower access, set a key to selected models on the API Keys page. A selected model must still have at least one enabled healthy provider account before the gateway can route requests to it.
```

- [ ] **Step 4: Run full verification**

```bash
cd backend
GOMODCACHE=/root/Clouds/N2API/.cache/go-mod GOCACHE=/root/Clouds/N2API/.cache/go-build go test ./...
```

```bash
cd frontend
bun run check
bun run build
```

Expected: all commands PASS.

- [ ] **Step 5: Commit**

```bash
git add README.md deploy/README.md backend/internal/httpapi/server.go backend/internal/httpapi/server_test.go backend/internal/store/provider.go backend/internal/provider/service.go
git commit -m "docs: document unified gateway account model"
```

## Task 8: Local Compose Runtime Verification

**Files:**
- No source edits expected.

- [ ] **Step 1: Rebuild local Compose service**

```bash
docker compose -f deploy/compose.yaml up --build --detach
```

Expected: Compose rebuilds and starts `n2api` plus PostgreSQL.

- [ ] **Step 2: Verify health endpoint**

```bash
docker exec deploy-n2api-1 wget -qO- http://127.0.0.1:3000/healthz
```

Expected output contains:

```text
ok
```

- [ ] **Step 3: Verify listener**

```bash
ss -lntp
```

Expected: port `3000` is listening for the local Compose service. If a dev server is started instead, it must bind `0.0.0.0` and/or `::` and be shared as `http://oc-de-fra-1.knowsky.uk:<port>`.

- [ ] **Step 4: Smoke admin static app**

```bash
curl -I -sS http://127.0.0.1:3000/
```

Expected: `HTTP/1.1 200 OK` or equivalent successful static response.

- [ ] **Step 5: Final status**

Run:

```bash
git status --short
```

Expected: no uncommitted source changes except intentionally captured verification artifacts, which should not be committed unless explicitly requested.

## Final Verification Gate

Before claiming implementation complete, run:

```bash
cd backend
GOMODCACHE=/root/Clouds/N2API/.cache/go-mod GOCACHE=/root/Clouds/N2API/.cache/go-build go test ./...
```

```bash
cd frontend
bun run check
bun run build
```

For UI/runtime changes served by Compose, rebuild and recreate the local service, then smoke `/healthz` and the admin app.

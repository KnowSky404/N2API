# Routing Pools Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add personal routing pools so API keys can be bound to named provider-account pools and gateway scheduling stays within that pool.

**Architecture:** Store pools in PostgreSQL, expose them through the admin service/API, and extend provider selection with optional pool scope while preserving the existing global account-pool behavior for unbound API keys. Gateway request logs and sticky session bindings carry pool scope so diagnostics and session affinity remain correct.

**Tech Stack:** Go backend, PostgreSQL migrations, SvelteKit admin UI, Bun frontend tests.

---

## File Structure

- `backend/internal/store/migrations/00024_routing_pools.sql`: creates routing pool tables and adds pool columns to API keys, session bindings, and request logs.
- `backend/internal/store/migrations_test.go`: verifies migration embedding and migration provider ordering.
- `backend/internal/admin/service.go`: defines admin DTOs and service methods for pools and API key pool binding.
- `backend/internal/admin/service_test.go`: validates service inputs and in-memory repo behavior.
- `backend/internal/store/admin.go`: persists pool CRUD, membership replacement, and API key pool binding.
- `backend/internal/store/admin_test.go`: covers SQL repository behavior with a test PostgreSQL database.
- `backend/internal/provider/service.go`: adds pool-scoped selection and sticky session support.
- `backend/internal/store/provider.go`: adds pool-scoped eligible-account queries and pool-scoped session binding queries.
- `backend/internal/provider/service_test.go`: verifies pool-scoped selection, priority, and sticky behavior.
- `backend/internal/gateway/proxy.go`: resolves pool-bound key behavior in the gateway wrapper and logs pool attribution.
- `backend/internal/gateway/proxy_test.go`: verifies gateway rejects disabled/empty pools and logs pool attribution.
- `backend/internal/store/gateway.go`: persists routing pool id/name in request logs.
- `backend/internal/httpapi/server.go`: exposes admin pool endpoints and API key binding endpoint.
- `backend/internal/httpapi/server_test.go`: covers endpoint success and error mapping.
- `frontend/src/lib/admin-state.svelte.js`: adds routing pool state and admin actions.
- `frontend/src/routes/+layout.svelte`: adds Routing Pools navigation entry.
- `frontend/src/routes/routing-pools/+page.svelte`: new pool management page.
- `frontend/src/routes/api-keys/+page.svelte`: adds per-key routing pool selector.
- `frontend/src/routes/navigation.test.mjs` and `frontend/src/routes/providers/provider-page.test.mjs`: frontend source and state tests.
- `README.md`, `deploy/README.md`, `backend/internal/gateway/documentation_test.go`: document routing pools.

## Task 1: Schema And Admin Store Foundation

**Files:**
- Create: `backend/internal/store/migrations/00024_routing_pools.sql`
- Modify: `backend/internal/store/migrations_test.go`
- Modify: `backend/internal/admin/service.go`
- Modify: `backend/internal/admin/service_test.go`
- Modify: `backend/internal/store/admin.go`
- Modify: `backend/internal/store/admin_test.go`

- [ ] **Step 1: Write failing migration tests**

Add `TestRoutingPoolsMigrationIsEmbedded` in `backend/internal/store/migrations_test.go`:

```go
func TestRoutingPoolsMigrationIsEmbedded(t *testing.T) {
	sql, err := MigrationSQL("00024_routing_pools.sql")
	if err != nil {
		t.Fatalf("MigrationSQL returned error: %v", err)
	}
	for _, want := range []string{
		"CREATE TABLE IF NOT EXISTS routing_pools",
		"name TEXT NOT NULL UNIQUE",
		"CREATE TABLE IF NOT EXISTS routing_pool_accounts",
		"PRIMARY KEY (pool_id, account_id)",
		"ALTER TABLE client_api_keys ADD COLUMN IF NOT EXISTS routing_pool_id",
		"ALTER TABLE provider_session_bindings ADD COLUMN IF NOT EXISTS routing_pool_id",
		"provider_session_bindings_pool_scope_idx",
		"ALTER TABLE request_logs ADD COLUMN IF NOT EXISTS routing_pool_id",
		"ALTER TABLE request_logs ADD COLUMN IF NOT EXISTS routing_pool_name",
	} {
		if !strings.Contains(sql, want) {
			t.Fatalf("routing pools migration missing %q", want)
		}
	}
}
```

Update `TestMigrationProviderSeesEmbeddedMigrations` to expect the new migration count and final path `00024_routing_pools.sql`.

- [ ] **Step 2: Run migration test to verify failure**

Run:

```bash
cd backend && GOMODCACHE=/root/Clouds/N2API/.cache/go-mod GOCACHE=/root/Clouds/N2API/.cache/go-build go test ./internal/store -run 'RoutingPoolsMigration|MigrationProviderSeesEmbeddedMigrations'
```

Expected: FAIL because `00024_routing_pools.sql` is missing.

- [ ] **Step 3: Add migration**

Create `backend/internal/store/migrations/00024_routing_pools.sql`:

```sql
-- +goose Up
CREATE TABLE IF NOT EXISTS routing_pools (
    id BIGSERIAL PRIMARY KEY,
    name TEXT NOT NULL UNIQUE,
    description TEXT NOT NULL DEFAULT '',
    enabled BOOLEAN NOT NULL DEFAULT true,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE TABLE IF NOT EXISTS routing_pool_accounts (
    pool_id BIGINT NOT NULL REFERENCES routing_pools(id) ON DELETE CASCADE,
    account_id BIGINT NOT NULL REFERENCES provider_accounts(id) ON DELETE CASCADE,
    priority INTEGER NOT NULL DEFAULT 0,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    PRIMARY KEY (pool_id, account_id)
);

CREATE INDEX IF NOT EXISTS routing_pool_accounts_account_idx
    ON routing_pool_accounts (account_id);

CREATE INDEX IF NOT EXISTS routing_pool_accounts_pool_priority_idx
    ON routing_pool_accounts (pool_id, priority);

ALTER TABLE client_api_keys ADD COLUMN IF NOT EXISTS routing_pool_id BIGINT REFERENCES routing_pools(id) ON DELETE SET NULL;

ALTER TABLE provider_session_bindings ADD COLUMN IF NOT EXISTS routing_pool_id BIGINT REFERENCES routing_pools(id) ON DELETE CASCADE;

DROP INDEX IF EXISTS provider_session_bindings_provider_model_session_idx;

CREATE UNIQUE INDEX IF NOT EXISTS provider_session_bindings_pool_scope_idx
    ON provider_session_bindings (provider, model, session_id, COALESCE(routing_pool_id, 0));

ALTER TABLE request_logs ADD COLUMN IF NOT EXISTS routing_pool_id BIGINT REFERENCES routing_pools(id) ON DELETE SET NULL;
ALTER TABLE request_logs ADD COLUMN IF NOT EXISTS routing_pool_name TEXT NOT NULL DEFAULT '';

CREATE INDEX IF NOT EXISTS request_logs_routing_pool_created_at_idx
    ON request_logs (routing_pool_id, created_at DESC);

-- +goose Down
DROP INDEX IF EXISTS request_logs_routing_pool_created_at_idx;
ALTER TABLE request_logs DROP COLUMN IF EXISTS routing_pool_name;
ALTER TABLE request_logs DROP COLUMN IF EXISTS routing_pool_id;
DROP INDEX IF EXISTS provider_session_bindings_pool_scope_idx;
CREATE UNIQUE INDEX IF NOT EXISTS provider_session_bindings_provider_model_session_idx
    ON provider_session_bindings (provider, model, session_id);
ALTER TABLE provider_session_bindings DROP COLUMN IF EXISTS routing_pool_id;
ALTER TABLE client_api_keys DROP COLUMN IF EXISTS routing_pool_id;
DROP TABLE IF EXISTS routing_pool_accounts;
DROP TABLE IF EXISTS routing_pools;
```

- [ ] **Step 4: Add admin DTOs and repository interface methods**

In `backend/internal/admin/service.go`, add:

```go
type RoutingPool struct {
	ID          int64                `json:"id"`
	Name        string               `json:"name"`
	Description string               `json:"description"`
	Enabled     bool                 `json:"enabled"`
	AccountIDs  []int64              `json:"accountIds"`
	Accounts    []RoutingPoolAccount `json:"accounts,omitempty"`
	CreatedAt   time.Time            `json:"createdAt"`
	UpdatedAt   time.Time            `json:"updatedAt"`
}

type RoutingPoolAccount struct {
	AccountID int64 `json:"accountId"`
	Priority  int   `json:"priority"`
}
```

Extend `APIKey`:

```go
RoutingPoolID   *int64 `json:"routingPoolId"`
RoutingPoolName string `json:"routingPoolName"`
```

Extend `Repository`:

```go
ListRoutingPools(ctx context.Context) ([]RoutingPool, error)
CreateRoutingPool(ctx context.Context, name, description string, enabled bool) (RoutingPool, error)
UpdateRoutingPool(ctx context.Context, id int64, name, description string, enabled bool) (RoutingPool, error)
DeleteRoutingPool(ctx context.Context, id int64) error
ReplaceRoutingPoolAccounts(ctx context.Context, id int64, accounts []RoutingPoolAccount) (RoutingPool, error)
UpdateAPIKeyRoutingPool(ctx context.Context, id int64, routingPoolID *int64) (APIKey, error)
```

- [ ] **Step 5: Write failing admin service tests**

Add tests to `backend/internal/admin/service_test.go`:

```go
func TestRoutingPoolServiceValidatesNameAndMembership(t *testing.T) {
	repo := newMemoryRepo()
	service := NewService(repo, Config{})

	if _, err := service.CreateRoutingPool(context.Background(), " ", "", true); !errors.Is(err, ErrInvalidInput) {
		t.Fatalf("CreateRoutingPool blank error = %v, want ErrInvalidInput", err)
	}

	pool, err := service.CreateRoutingPool(context.Background(), " codex primary ", " daily pool ", true)
	if err != nil {
		t.Fatalf("CreateRoutingPool returned error: %v", err)
	}
	if pool.Name != "codex primary" || pool.Description != "daily pool" || !pool.Enabled {
		t.Fatalf("pool = %+v, want trimmed enabled pool", pool)
	}

	if _, err := service.ReplaceRoutingPoolAccounts(context.Background(), pool.ID, []RoutingPoolAccount{{AccountID: -1, Priority: 0}}); !errors.Is(err, ErrInvalidInput) {
		t.Fatalf("negative account id error = %v, want ErrInvalidInput", err)
	}
	if _, err := service.ReplaceRoutingPoolAccounts(context.Background(), pool.ID, []RoutingPoolAccount{{AccountID: 7, Priority: -1}}); !errors.Is(err, ErrInvalidInput) {
		t.Fatalf("negative priority error = %v, want ErrInvalidInput", err)
	}

	updated, err := service.ReplaceRoutingPoolAccounts(context.Background(), pool.ID, []RoutingPoolAccount{{AccountID: 7, Priority: 10}})
	if err != nil {
		t.Fatalf("ReplaceRoutingPoolAccounts returned error: %v", err)
	}
	if len(updated.Accounts) != 1 || updated.Accounts[0].AccountID != 7 || updated.Accounts[0].Priority != 10 {
		t.Fatalf("pool accounts = %+v, want account 7 priority 10", updated.Accounts)
	}
}
```

Add `memoryRepo` fields and methods for routing pools.

- [ ] **Step 6: Implement admin service validation**

Add methods:

```go
func (s *Service) CreateRoutingPool(ctx context.Context, name, description string, enabled bool) (RoutingPool, error) {
	name = strings.TrimSpace(name)
	description = strings.TrimSpace(description)
	if name == "" {
		return RoutingPool{}, ErrInvalidInput
	}
	return s.repo.CreateRoutingPool(ctx, name, description, enabled)
}

func (s *Service) UpdateRoutingPool(ctx context.Context, id int64, name, description string, enabled bool) (RoutingPool, error) {
	name = strings.TrimSpace(name)
	description = strings.TrimSpace(description)
	if id <= 0 || name == "" {
		return RoutingPool{}, ErrInvalidInput
	}
	return s.repo.UpdateRoutingPool(ctx, id, name, description, enabled)
}

func (s *Service) ReplaceRoutingPoolAccounts(ctx context.Context, id int64, accounts []RoutingPoolAccount) (RoutingPool, error) {
	if id <= 0 {
		return RoutingPool{}, ErrInvalidInput
	}
	normalized := make([]RoutingPoolAccount, 0, len(accounts))
	seen := map[int64]struct{}{}
	for _, account := range accounts {
		if account.AccountID <= 0 || account.Priority < 0 {
			return RoutingPool{}, ErrInvalidInput
		}
		if _, ok := seen[account.AccountID]; ok {
			continue
		}
		seen[account.AccountID] = struct{}{}
		normalized = append(normalized, account)
	}
	return s.repo.ReplaceRoutingPoolAccounts(ctx, id, normalized)
}

func (s *Service) UpdateAPIKeyRoutingPool(ctx context.Context, id int64, routingPoolID *int64) (APIKey, error) {
	if id <= 0 {
		return APIKey{}, ErrInvalidInput
	}
	if routingPoolID != nil && *routingPoolID < 0 {
		return APIKey{}, ErrInvalidInput
	}
	if routingPoolID != nil && *routingPoolID == 0 {
		routingPoolID = nil
	}
	return s.repo.UpdateAPIKeyRoutingPool(ctx, id, routingPoolID)
}
```

- [ ] **Step 7: Add store repository tests**

In `backend/internal/store/admin_test.go`, add:

```go
func TestAdminRepositoryRoutingPools(t *testing.T) {
	repo := newTestAdminRepository(t)
	ctx := context.Background()

	account, err := repo.SaveAccount(ctx, provider.Account{
		Provider: "openai",
		AccountType: provider.AccountTypeAPIUpstream,
		Name: "upstream",
		DisplayName: "Upstream",
		Enabled: true,
		Credential: provider.AccountCredential{
			CredentialType: provider.CredentialTypeAPIKey,
			EncryptedAPIKey: "encrypted",
			BaseURL: "https://upstream.example.test",
		},
	})
	if err != nil {
		t.Fatalf("SaveAccount returned error: %v", err)
	}

	pool, err := repo.CreateRoutingPool(ctx, "codex primary", "daily pool", true)
	if err != nil {
		t.Fatalf("CreateRoutingPool returned error: %v", err)
	}
	if pool.Name != "codex primary" || !pool.Enabled {
		t.Fatalf("pool = %+v, want created pool", pool)
	}

	pool, err = repo.ReplaceRoutingPoolAccounts(ctx, pool.ID, []admin.RoutingPoolAccount{{AccountID: account.ID, Priority: 5}})
	if err != nil {
		t.Fatalf("ReplaceRoutingPoolAccounts returned error: %v", err)
	}
	if len(pool.Accounts) != 1 || pool.Accounts[0].AccountID != account.ID || pool.Accounts[0].Priority != 5 {
		t.Fatalf("pool accounts = %+v, want account membership", pool.Accounts)
	}

	key, err := repo.CreateAPIKey(ctx, "codex laptop", "hash-routing-pool", "n2api_")
	if err != nil {
		t.Fatalf("CreateAPIKey returned error: %v", err)
	}
	key, err = repo.UpdateAPIKeyRoutingPool(ctx, key.ID, &pool.ID)
	if err != nil {
		t.Fatalf("UpdateAPIKeyRoutingPool returned error: %v", err)
	}
	if key.RoutingPoolID == nil || *key.RoutingPoolID != pool.ID || key.RoutingPoolName != "codex primary" {
		t.Fatalf("key routing pool = %+v, want pool binding", key)
	}
}
```

- [ ] **Step 8: Implement store methods**

Add SQL methods in `backend/internal/store/admin.go` for pool CRUD and binding. Use transactions for `ReplaceRoutingPoolAccounts`:

```go
func (r *AdminRepository) ReplaceRoutingPoolAccounts(ctx context.Context, id int64, accounts []admin.RoutingPoolAccount) (admin.RoutingPool, error) {
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return admin.RoutingPool{}, err
	}
	defer tx.Rollback(ctx)

	if _, err := tx.Exec(ctx, `DELETE FROM routing_pool_accounts WHERE pool_id = $1`, id); err != nil {
		return admin.RoutingPool{}, err
	}
	for _, account := range accounts {
		if _, err := tx.Exec(ctx, `
			INSERT INTO routing_pool_accounts (pool_id, account_id, priority)
			VALUES ($1, $2, $3)
		`, id, account.AccountID, account.Priority); err != nil {
			return admin.RoutingPool{}, err
		}
	}
	if err := tx.Commit(ctx); err != nil {
		return admin.RoutingPool{}, err
	}
	return r.GetRoutingPool(ctx, id)
}
```

Add unexported helper `getRoutingPool(ctx, id)` in `backend/internal/store/admin.go`. It must load one row from `routing_pools`, left join `routing_pool_accounts`, return `admin.ErrNotFound` on `pgx.ErrNoRows`, and populate both `RoutingPool.Accounts` and `RoutingPool.AccountIDs` ordered by membership priority then account id.

- [ ] **Step 9: Run targeted tests**

Run:

```bash
cd backend && GOMODCACHE=/root/Clouds/N2API/.cache/go-mod GOCACHE=/root/Clouds/N2API/.cache/go-build go test ./internal/admin ./internal/store -run 'RoutingPool|MigrationProviderSeesEmbeddedMigrations'
```

Expected: PASS.

- [ ] **Step 10: Commit**

```bash
git add backend/internal/store/migrations/00024_routing_pools.sql backend/internal/store/migrations_test.go backend/internal/admin/service.go backend/internal/admin/service_test.go backend/internal/store/admin.go backend/internal/store/admin_test.go
git commit -m "feat: store routing pools"
```

## Task 2: Pool-Scoped Provider Selection

**Files:**
- Modify: `backend/internal/provider/service.go`
- Modify: `backend/internal/provider/service_test.go`
- Modify: `backend/internal/store/provider.go`
- Modify: `backend/internal/store/provider_test.go`

- [ ] **Step 1: Add provider repository interface methods**

In `backend/internal/provider/service.go`, extend repository interface:

```go
FindRoutingPool(ctx context.Context, poolID int64) (RoutingPool, error)
ListAccountsForRoutingPool(ctx context.Context, provider string, poolID int64, model string, excludedAccountIDs []int64, now time.Time) ([]Account, error)
FindSessionBindingInRoutingPool(ctx context.Context, provider string, routingPoolID int64, model, sessionID string) (SessionBinding, error)
UpsertSessionBindingInRoutingPool(ctx context.Context, provider string, routingPoolID int64, model, sessionID string, accountID int64) error
```

Add provider DTO:

```go
type RoutingPool struct {
	ID      int64
	Name    string
	Enabled bool
}
```

Keep the existing unscoped `FindSessionBinding` and `UpsertSessionBinding` signatures unchanged so current callers and tests continue to cover the global provider account pool. Pool-scoped methods are additive.

- [ ] **Step 2: Write failing provider tests**

Add `TestSelectAccountForModelInRoutingPoolScopesCandidates`:

```go
func TestSelectAccountForModelInRoutingPoolScopesCandidates(t *testing.T) {
	repo := newMemoryRepo()
	repo.routingPools[7] = RoutingPool{ID: 7, Name: "primary", Enabled: true}
	repo.accounts = []Account{
		testAccount(t, 1, true, 1, "global-token"),
		testAccount(t, 2, true, 50, "pool-token"),
	}
	repo.routingPoolAccounts[7] = []RoutingPoolAccount{{AccountID: 2, Priority: 0}}
	service := NewService(repo, nil, Config{Provider: "openai", Secret: "secret"})

	selected, err := service.SelectAccountForModelInRoutingPool(context.Background(), 7, "")
	if err != nil {
		t.Fatalf("SelectAccountForModelInRoutingPool returned error: %v", err)
	}
	if selected.AccountID != 2 {
		t.Fatalf("selected account = %d, want pool account 2", selected.AccountID)
	}
}
```

Add `TestSelectAccountForModelAndSessionInRoutingPoolDoesNotCrossScope`:

```go
func TestSelectAccountForModelAndSessionInRoutingPoolDoesNotCrossScope(t *testing.T) {
	repo := newMemoryRepo()
	repo.routingPools[7] = RoutingPool{ID: 7, Name: "primary", Enabled: true}
	repo.routingPools[8] = RoutingPool{ID: 8, Name: "secondary", Enabled: true}
	repo.accounts = []Account{
		testAccount(t, 1, true, 1, "first-token"),
		testAccount(t, 2, true, 1, "second-token"),
	}
	repo.routingPoolAccounts[7] = []RoutingPoolAccount{{AccountID: 1, Priority: 0}}
	repo.routingPoolAccounts[8] = []RoutingPoolAccount{{AccountID: 2, Priority: 0}}
	service := NewService(repo, nil, Config{Provider: "openai", Secret: "secret"})

	first, err := service.SelectAccountForModelAndSessionInRoutingPool(context.Background(), 7, "gpt-5", "workspace-123")
	if err != nil {
		t.Fatalf("pool 7 selection returned error: %v", err)
	}
	second, err := service.SelectAccountForModelAndSessionInRoutingPool(context.Background(), 8, "gpt-5", "workspace-123")
	if err != nil {
		t.Fatalf("pool 8 selection returned error: %v", err)
	}
	if first.AccountID == second.AccountID {
		t.Fatalf("pool scoped sticky selected same account %d across pools", first.AccountID)
	}
}
```

- [ ] **Step 3: Implement provider service methods**

Add public methods:

```go
func (s *Service) SelectAccountForModelInRoutingPool(ctx context.Context, routingPoolID int64, model string, excludedAccountIDs ...int64) (SelectedAccount, error) {
	if routingPoolID <= 0 {
		return s.SelectAccountForModel(ctx, model, excludedAccountIDs...)
	}
	accounts, hasEnabled, notFoundErr, err := s.selectionCandidatesForRoutingPool(ctx, routingPoolID, model, excludedAccountIDs)
	if err != nil {
		return SelectedAccount{}, err
	}
	return s.selectFromCandidates(ctx, accounts, hasEnabled, notFoundErr)
}

func (s *Service) SelectAccountForModelAndSessionInRoutingPool(ctx context.Context, routingPoolID int64, model, sessionID string, excludedAccountIDs ...int64) (SelectedAccount, error) {
	if routingPoolID <= 0 {
		return s.SelectAccountForModelAndSession(ctx, model, sessionID, excludedAccountIDs...)
	}
	sessionID = strings.TrimSpace(sessionID)
	if sessionID == "" {
		return s.SelectAccountForModelInRoutingPool(ctx, routingPoolID, model, excludedAccountIDs...)
	}
	accounts, hasEnabled, notFoundErr, err := s.selectionCandidatesForRoutingPool(ctx, routingPoolID, model, excludedAccountIDs)
	if err != nil {
		return SelectedAccount{}, err
	}
	accounts, _, err = s.stickySessionCandidatesInRoutingPool(ctx, routingPoolID, accounts, model, sessionID)
	if err != nil {
		return SelectedAccount{}, err
	}
	selected, err := s.selectFromCandidates(ctx, accounts, hasEnabled, notFoundErr)
	if err != nil {
		return SelectedAccount{}, err
	}
	if err := s.repo.UpsertSessionBindingInRoutingPool(ctx, s.cfg.Provider, routingPoolID, model, sessionID, selected.AccountID); err != nil {
		return SelectedAccount{}, fmt.Errorf("upsert provider session binding: %w", err)
	}
	return selected, nil
}
```

- [ ] **Step 4: Implement store pool-scoped queries**

In `backend/internal/store/provider.go`, implement `ListAccountsForRoutingPool`:

```sql
SELECT <providerAccountColumns>
FROM routing_pool_accounts rpa
JOIN provider_accounts a ON a.id = rpa.account_id
JOIN provider_account_credentials c ON c.account_id = a.id
LEFT JOIN provider_account_models m ON m.account_id = a.id AND m.model = $3 AND m.enabled = true
WHERE a.provider = $1
  AND rpa.pool_id = $2
  AND a.enabled = true
  AND (a.rate_limited_until IS NULL OR a.rate_limited_until <= $4)
  AND (a.circuit_open_until IS NULL OR a.circuit_open_until <= $4)
  AND (c.access_token_expires_at IS NULL OR c.access_token_expires_at > $4)
  AND NOT (a.id = ANY($5::bigint[]))
  AND ($3 = '' OR m.id IS NOT NULL)
ORDER BY rpa.priority ASC, a.priority ASC, a.load_factor DESC, a.last_used_at ASC NULLS FIRST, a.id ASC
```

- [ ] **Step 5: Run targeted provider tests**

Run:

```bash
cd backend && GOMODCACHE=/root/Clouds/N2API/.cache/go-mod GOCACHE=/root/Clouds/N2API/.cache/go-build go test ./internal/provider ./internal/store -run 'RoutingPool|SessionBinding'
```

Expected: PASS.

- [ ] **Step 6: Commit**

```bash
git add backend/internal/provider/service.go backend/internal/provider/service_test.go backend/internal/store/provider.go backend/internal/store/provider_test.go
git commit -m "feat: scope provider selection by routing pool"
```

## Task 3: Gateway Wiring And Request Logs

**Files:**
- Modify: `backend/internal/gateway/proxy.go`
- Modify: `backend/internal/gateway/proxy_test.go`
- Modify: `backend/internal/store/gateway.go`
- Modify: `backend/internal/store/gateway_test.go`
- Modify: `backend/internal/admin/service.go`
- Modify: `backend/internal/store/admin.go`
- Modify: `backend/cmd/n2api/main.go`

- [ ] **Step 1: Add gateway request log fields**

Extend `gateway.RequestLog`:

```go
RoutingPoolID   int64
RoutingPoolName string
```

Extend store `CreateRequestLog` insert columns and args:

```sql
routing_pool_id, routing_pool_name
```

Use `nil` for zero pool id when inserting.

- [ ] **Step 2: Add gateway account provider interface methods**

In `backend/internal/gateway/proxy.go`, extend account provider abstraction:

```go
type RoutingPoolAccountProvider interface {
	SelectAccountForModelInRoutingPool(ctx context.Context, routingPoolID int64, model string, excludedAccountIDs ...int64) (SelectedAccount, error)
	SelectAccountForModelAndSessionInRoutingPool(ctx context.Context, routingPoolID int64, model, sessionID string, excludedAccountIDs ...int64) (SelectedAccount, error)
}
```

Extend `SelectedAccount`:

```go
RoutingPoolID   int64
RoutingPoolName string
```

- [ ] **Step 3: Write failing gateway tests**

Add `TestProxyRoutesPoolBoundAPIKeyThroughRoutingPool`:

```go
func TestProxyRoutesPoolBoundAPIKeyThroughRoutingPool(t *testing.T) {
	logger := &fakeRequestLogger{}
	accounts := &fakeSelectedAccountProvider{
		accounts: []SelectedAccount{{AccountID: 9, AccountType: provider.AccountTypeAPIUpstream, AuthorizationToken: "pool-token", RoutingPoolID: 7, RoutingPoolName: "primary"}},
	}
	client := &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
		return &http.Response{StatusCode: http.StatusOK, Header: http.Header{"Content-Type": []string{"application/json"}}, Body: io.NopCloser(strings.NewReader(`{"ok":true}`)), Request: r}, nil
	})}
	poolID := int64(7)
	proxy := NewProxyWithClient(&fakeAPIKeyAuthenticator{key: admin.APIKey{ID: 42, Name: "pool key", RoutingPoolID: &poolID, RoutingPoolName: "primary"}}, accounts, Config{UpstreamBaseURL: "https://upstream.example.test", Logger: logger}, client)
	req := httptest.NewRequest(http.MethodGet, "/v1/responses/resp_123", nil)
	req.Header.Set("Authorization", "Bearer n2api_client_secret")
	recorder := httptest.NewRecorder()

	proxy.ServeHTTP(recorder, req)

	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d body=%s, want 200", recorder.Code, recorder.Body.String())
	}
	if accounts.routingPoolCalls != 1 || accounts.routingPoolIDs[0] != 7 {
		t.Fatalf("routing pool calls = %d ids=%+v, want pool 7", accounts.routingPoolCalls, accounts.routingPoolIDs)
	}
	if logger.entries[0].RoutingPoolID != 7 || logger.entries[0].RoutingPoolName != "primary" {
		t.Fatalf("logged pool = %d/%q, want 7/primary", logger.entries[0].RoutingPoolID, logger.entries[0].RoutingPoolName)
	}
}
```

- [ ] **Step 4: Implement gateway pool routing**

Rename the existing helper `selectAccount(ctx, model, sessionID, ...)` to `selectGlobalAccount(ctx, model, sessionID, ...)`. Then add a new key-aware helper that chooses the pool-aware method when `key.RoutingPoolID != nil`:

```go
func (p *Proxy) selectAccountForKey(ctx context.Context, key admin.APIKey, model, sessionID string, excludedAccountIDs ...int64) (SelectedAccount, error) {
	if key.RoutingPoolID != nil && *key.RoutingPoolID > 0 {
		poolProvider, ok := p.accounts.(RoutingPoolAccountProvider)
		if !ok {
			return SelectedAccount{}, provider.ErrAccountsUnavailable
		}
		if strings.TrimSpace(sessionID) != "" {
			return poolProvider.SelectAccountForModelAndSessionInRoutingPool(ctx, *key.RoutingPoolID, model, sessionID, excludedAccountIDs...)
		}
		return poolProvider.SelectAccountForModelInRoutingPool(ctx, *key.RoutingPoolID, model, excludedAccountIDs...)
	}
	return p.selectGlobalAccount(ctx, model, sessionID, excludedAccountIDs...)
}
```

Update all gateway call sites that currently call `p.selectAccount(ctx, model, sessionID, ...)` during authenticated request handling to call `p.selectAccountForKey(ctx, key, model, sessionID, ...)`.

- [ ] **Step 5: Wire production wrapper**

In `backend/cmd/n2api/main.go`, add methods on `gatewayAccountProvider`:

```go
func (p gatewayAccountProvider) SelectAccountForModelInRoutingPool(ctx context.Context, routingPoolID int64, model string, excludedAccountIDs ...int64) (gateway.SelectedAccount, error) {
	selected, err := p.service.SelectAccountForModelInRoutingPool(ctx, routingPoolID, model, excludedAccountIDs...)
	return selectedGatewayAccount(selected, err)
}

func (p gatewayAccountProvider) SelectAccountForModelAndSessionInRoutingPool(ctx context.Context, routingPoolID int64, model, sessionID string, excludedAccountIDs ...int64) (gateway.SelectedAccount, error) {
	selected, err := p.service.SelectAccountForModelAndSessionInRoutingPool(ctx, routingPoolID, model, sessionID, excludedAccountIDs...)
	return selectedGatewayAccount(selected, err)
}
```

Keep compile-time assertions:

```go
var _ gateway.RoutingPoolAccountProvider = gatewayAccountProvider{}
```

- [ ] **Step 6: Run targeted gateway tests**

Run:

```bash
cd backend && GOMODCACHE=/root/Clouds/N2API/.cache/go-mod GOCACHE=/root/Clouds/N2API/.cache/go-build go test ./internal/gateway ./internal/store ./cmd/n2api -run 'RoutingPool|RequestLog'
```

Expected: PASS. If sandbox blocks `httptest` `[::1]`, rerun the same command with escalation.

- [ ] **Step 7: Commit**

```bash
git add backend/internal/gateway/proxy.go backend/internal/gateway/proxy_test.go backend/internal/store/gateway.go backend/internal/store/gateway_test.go backend/cmd/n2api/main.go backend/cmd/n2api/main_test.go
git commit -m "feat: route gateway requests by routing pool"
```

## Task 4: Admin HTTP Endpoints

**Files:**
- Modify: `backend/internal/httpapi/server.go`
- Modify: `backend/internal/httpapi/server_test.go`

- [ ] **Step 1: Extend `AdminService` interface**

Add:

```go
ListRoutingPools(ctx context.Context) ([]admin.RoutingPool, error)
CreateRoutingPool(ctx context.Context, name, description string, enabled bool) (admin.RoutingPool, error)
UpdateRoutingPool(ctx context.Context, id int64, name, description string, enabled bool) (admin.RoutingPool, error)
DeleteRoutingPool(ctx context.Context, id int64) error
ReplaceRoutingPoolAccounts(ctx context.Context, id int64, accounts []admin.RoutingPoolAccount) (admin.RoutingPool, error)
UpdateAPIKeyRoutingPool(ctx context.Context, id int64, routingPoolID *int64) (admin.APIKey, error)
```

- [ ] **Step 2: Write failing HTTP tests**

Add tests:

```go
func TestRoutingPoolsEndpoints(t *testing.T) {
	admins := newFakeAdminService()
	server := NewServer(config.Config{}, staticHealth{}, admins, newFakeProviderService())
	req := httptest.NewRequest(http.MethodPost, "/api/admin/routing-pools", strings.NewReader(`{"name":"primary","description":"daily","enabled":true}`))
	req.AddCookie(&http.Cookie{Name: adminSessionCookieName, Value: "valid-session"})
	recorder := httptest.NewRecorder()

	server.ServeHTTP(recorder, req)

	if recorder.Code != http.StatusCreated {
		t.Fatalf("status = %d body=%s, want 201", recorder.Code, recorder.Body.String())
	}
	var body struct{ Pool admin.RoutingPool `json:"pool"` }
	if err := json.Unmarshal(recorder.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode body: %v", err)
	}
	if body.Pool.Name != "primary" || !body.Pool.Enabled {
		t.Fatalf("pool = %+v, want primary enabled", body.Pool)
	}
}
```

Add `TestUpdateAPIKeyRoutingPoolEndpoint`:

```go
func TestUpdateAPIKeyRoutingPoolEndpoint(t *testing.T) {
	admins := newFakeAdminService()
	server := NewServer(config.Config{}, staticHealth{}, admins, nil)
	req := httptest.NewRequest(http.MethodPut, "/api/admin/keys/7/routing-pool", strings.NewReader(`{"routingPoolId":3}`))
	req.AddCookie(&http.Cookie{Name: adminSessionCookieName, Value: "valid-session"})
	recorder := httptest.NewRecorder()

	server.ServeHTTP(recorder, req)

	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d body=%s, want 200", recorder.Code, recorder.Body.String())
	}
	if admins.routingPoolKeyID != 7 || admins.routingPoolID == nil || *admins.routingPoolID != 3 {
		t.Fatalf("recorded key pool = id:%d pool:%v, want 7/3", admins.routingPoolKeyID, admins.routingPoolID)
	}
}
```

- [ ] **Step 3: Implement handlers**

Add handlers:

- `GET /api/admin/routing-pools`
- `POST /api/admin/routing-pools`
- `PATCH /api/admin/routing-pools/{id}`
- `DELETE /api/admin/routing-pools/{id}`
- `PUT /api/admin/routing-pools/{id}/accounts`
- `PUT /api/admin/keys/{id}/routing-pool`

Use existing `parsePositivePathID`, `decodeJSON`, `writeJSON`, and error mapping style. Return:

- create: `201 {"pool": pool}`
- update: `200 {"pool": pool}`
- delete: `204`
- replace accounts: `200 {"pool": pool}`
- key bind: `200 {"key": key}`

- [ ] **Step 4: Run HTTP tests**

Run:

```bash
cd backend && GOMODCACHE=/root/Clouds/N2API/.cache/go-mod GOCACHE=/root/Clouds/N2API/.cache/go-build go test ./internal/httpapi -run 'RoutingPool'
```

Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add backend/internal/httpapi/server.go backend/internal/httpapi/server_test.go
git commit -m "feat: expose routing pool endpoints"
```

## Task 5: Admin UI Routing Pools

**Files:**
- Modify: `frontend/src/lib/admin-state.svelte.js`
- Modify: `frontend/src/routes/+layout.svelte`
- Create: `frontend/src/routes/routing-pools/+page.svelte`
- Modify: `frontend/src/routes/api-keys/+page.svelte`
- Modify: `frontend/src/routes/navigation.test.mjs`
- Modify: `frontend/src/routes/providers/provider-page.test.mjs`

- [ ] **Step 1: Write failing frontend tests**

In `frontend/src/routes/navigation.test.mjs`, add:

```js
test('routing pools page manages account pools', () => {
  const layout = readFileSync('src/routes/+layout.svelte', 'utf8');
  const poolsPage = readFileSync('src/routes/routing-pools/+page.svelte', 'utf8');
  const adminState = readFileSync('src/lib/admin-state.svelte.js', 'utf8');

  assert.match(layout, /href:\s*'\/routing-pools'/);
  for (const label of ['Routing pools', 'Create pool', 'Pool accounts', 'Save membership', 'Enabled']) {
    assert.match(poolsPage, new RegExp(label.replace(' ', '\\s+')));
  }
  assert.match(adminState, /loadRoutingPools/);
  assert.match(adminState, /createRoutingPool/);
  assert.match(adminState, /replaceRoutingPoolAccounts/);
});
```

In `frontend/src/routes/providers/provider-page.test.mjs`, add:

```js
test('api key state can save routing pool binding', async () => {
  session.authenticated = true;
  apiKeys.error = '';
  apiKeys.items = [{ id: 7, name: 'codex laptop', routingPoolId: null }];
  let request = null;
  globalThis.fetch = async (path, options) => {
    request = { path, options };
    return new Response(JSON.stringify({ key: { id: 7, name: 'codex laptop', routingPoolId: 3, routingPoolName: 'primary' } }), {
      status: 200,
      headers: { 'Content-Type': 'application/json' }
    });
  };

  await updateAPIKeyRoutingPool(7, '3');

  assert.equal(request.path, '/api/admin/keys/7/routing-pool');
  assert.equal(request.options.method, 'PUT');
  assert.deepEqual(JSON.parse(request.options.body), { routingPoolId: 3 });
  assert.equal(apiKeys.items[0].routingPoolId, 3);
});
```

- [ ] **Step 2: Implement admin state**

Add state:

```js
export const routingPools = $state({
  loading: false,
  saving: false,
  error: '',
  items: [],
  newPoolName: '',
  newPoolDescription: ''
});
```

Add functions:

- `loadRoutingPools()`
- `createRoutingPool()`
- `updateRoutingPool(pool)`
- `deleteRoutingPool(poolId)`
- `replaceRoutingPoolAccounts(poolId, accounts)`
- `updateAPIKeyRoutingPool(keyId, routingPoolId)`

Use `requestJSON('/api/admin/routing-pools')` and existing API key item replacement patterns.

- [ ] **Step 3: Implement sidebar route**

In `frontend/src/routes/+layout.svelte`, add a text-only navigation entry, matching the existing sidebar:

```js
{ href: '/routing-pools', label: 'Routing pools' }
```

- [ ] **Step 4: Implement page**

Create `frontend/src/routes/routing-pools/+page.svelte` with:

- login gate consistent with existing pages.
- create form for name/description.
- table/list of pools.
- enabled checkbox.
- account membership checklist using `providerAccounts.items`.
- priority number input per selected account.
- save membership button.

Do not use modal-first UI.

- [ ] **Step 5: Update API Keys page**

Import `routingPools`, `loadRoutingPools`, and `updateAPIKeyRoutingPool`.

Add selector per key:

```svelte
<select
  value={key.routingPoolId ?? 0}
  disabled={Boolean(key.revokedAt)}
  onchange={(event) => updateAPIKeyRoutingPool(key.id, Number(event.currentTarget.value || 0))}
>
  <option value={0}>Global pool</option>
  {#each routingPools.items as pool}
    <option value={pool.id}>{pool.name}</option>
  {/each}
</select>
```

- [ ] **Step 6: Run frontend tests and build**

Run:

```bash
cd frontend && bun test src/routes/navigation.test.mjs src/routes/providers/provider-page.test.mjs
cd frontend && bun run build
```

Expected: PASS.

- [ ] **Step 7: Commit**

```bash
git add frontend/src/lib/admin-state.svelte.js frontend/src/routes/+layout.svelte frontend/src/routes/routing-pools/+page.svelte frontend/src/routes/api-keys/+page.svelte frontend/src/routes/navigation.test.mjs frontend/src/routes/providers/provider-page.test.mjs
git commit -m "feat: manage routing pools in admin ui"
```

## Task 6: Diagnostics, Docs, And Full Verification

**Files:**
- Modify: `README.md`
- Modify: `deploy/README.md`
- Modify: `backend/internal/gateway/documentation_test.go`
- Modify: `frontend/src/routes/request-logs/+page.svelte`
- Modify: `frontend/src/lib/admin-state.svelte.js`
- Modify: `backend/internal/httpapi/server.go`
- Modify: `backend/internal/httpapi/server_test.go`

- [ ] **Step 1: Add docs test**

In `backend/internal/gateway/documentation_test.go`, add:

```go
func TestGatewayDocumentationMentionsRoutingPools(t *testing.T) {
	for _, path := range []string{"../../../README.md", "../../../deploy/README.md"} {
		content, err := os.ReadFile(path)
		if err != nil {
			t.Fatalf("ReadFile(%q) returned error: %v", path, err)
		}
		text := string(content)
		for _, want := range []string{
			"Routing pools",
			"API key can be bound to one routing pool",
			"pool-bound key",
			"global provider account pool",
			"routing_pool_unavailable",
		} {
			if !strings.Contains(text, want) {
				t.Fatalf("%s missing %q in routing pools documentation", path, want)
			}
		}
	}
}
```

- [ ] **Step 2: Document routing pools**

Add paragraph near API key / gateway runtime docs:

```md
Routing pools let a personal admin partition provider accounts into named account pools. An API key can be bound to one routing pool; a pool-bound key schedules only accounts in that pool, while an unbound key keeps using the global provider account pool. Disabled or missing pools fail closed with local request-log reasons such as `routing_pool_disabled` or `routing_pool_unavailable`.
```

- [ ] **Step 3: Add request-log filter for routing pool**

Extend backend request-log filter:

- `admin.RequestLogFilter.RoutingPoolID`
- query param `routingPoolId`
- SQL filter `l.routing_pool_id = $n`
- response fields `routingPoolId`, `routingPoolName`

Extend frontend Request Logs page with `Routing pool` select.

- [ ] **Step 4: Run full verification**

Run:

```bash
cd backend && GOMODCACHE=/root/Clouds/N2API/.cache/go-mod GOCACHE=/root/Clouds/N2API/.cache/go-build go test ./...
cd frontend && bun test src/routes/navigation.test.mjs src/routes/providers/provider-page.test.mjs
cd frontend && bun run check
cd frontend && bun run build
```

If backend `go test ./...` fails in the sandbox with `httptest: failed to listen on a port: listen tcp6 [::1]:0: socket: operation not permitted`, rerun the exact same command with elevated permissions before treating it as a code failure.

- [ ] **Step 5: Commit**

```bash
git add README.md deploy/README.md backend/internal/gateway/documentation_test.go backend/internal/httpapi/server.go backend/internal/httpapi/server_test.go backend/internal/admin/service.go backend/internal/store/admin.go frontend/src/lib/admin-state.svelte.js frontend/src/routes/request-logs/+page.svelte
git commit -m "docs: document routing pools"
```

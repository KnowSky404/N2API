# Routing Pool Fallback Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add explicit routing pool fallback chains so pool-bound API keys can fail over through configured backup pools without ever falling back to the global provider account pool.

**Architecture:** Store a single optional `fallback_pool_id` on each routing pool, validate chains in admin service/store code, and make provider/gateway selection resolve a pool chain before selecting accounts. Request logs record actual pool usage plus fallback diagnostics, while the admin UI exposes fallback configuration and warnings.

**Tech Stack:** Go backend, PostgreSQL migrations, SvelteKit admin UI, Bun frontend tests.

---

## File Structure

- Create `backend/internal/store/migrations/00025_routing_pool_fallback.sql`: adds routing-pool fallback references and request-log fallback diagnostics.
- Modify `backend/internal/store/migrations_test.go`: verifies migration contents and migration ordering.
- Modify `backend/internal/admin/service.go`: extends routing-pool DTOs and validation for fallback ids.
- Modify `backend/internal/admin/service_test.go`: covers self fallback, missing fallback, and cycle validation using memory repo.
- Modify `backend/internal/store/admin.go`: persists fallback fields, returns fallback name, and clears incoming references on delete.
- Modify `backend/internal/store/admin_test.go`: verifies SQL repository create/update/list behavior and cycle validation helper.
- Modify `backend/internal/httpapi/server.go`: accepts `fallbackPoolId` on create/update.
- Modify `backend/internal/httpapi/server_test.go`: covers HTTP create/update/clear fallback payloads.
- Modify `backend/internal/provider/service.go`: adds chain-aware pool selection and selected-account diagnostics.
- Modify `backend/internal/provider/service_test.go`: covers primary-first, fallback-on-model-unavailable, no-global-fallback, cycle drift, and sticky actual-pool binding.
- Modify `backend/internal/store/provider.go`: loads fallback chain data for provider selection.
- Modify `backend/internal/store/provider_test.go`: covers chain lookup and account selection across fallback pools.
- Modify `backend/internal/gateway/proxy.go`: uses chain-aware selection when available and logs routing fallback diagnostics.
- Modify `backend/internal/gateway/proxy_test.go`: covers gateway fallback-pool retry, diagnostics, and fail-closed behavior.
- Modify `backend/internal/store/gateway.go` and `backend/internal/store/gateway_test.go`: persists new request-log fields.
- Modify `frontend/src/lib/admin-state.svelte.js`: adds fallback fields to routing pool state and request-log DTOs.
- Modify `frontend/src/routes/routing-pools/+page.svelte`: adds fallback selector and warnings.
- Modify `frontend/src/routes/api-keys/+page.svelte`: shows fallback target for bound keys.
- Modify `frontend/src/routes/request-logs/+page.svelte`: shows fallback depth/chain diagnostics.
- Modify `frontend/src/routes/navigation.test.mjs` and `frontend/src/routes/providers/provider-page.test.mjs`: source-level UI/state coverage.
- Modify `README.md`, `deploy/README.md`, and `backend/internal/gateway/documentation_test.go`: document fallback behavior and no-global-fallback isolation.

## Task 1: Schema, DTOs, And Admin Validation

**Files:**
- Create: `backend/internal/store/migrations/00025_routing_pool_fallback.sql`
- Modify: `backend/internal/store/migrations_test.go`
- Modify: `backend/internal/admin/service.go`
- Modify: `backend/internal/admin/service_test.go`
- Modify: `backend/internal/store/admin.go`
- Modify: `backend/internal/store/admin_test.go`

- [ ] **Step 1: Write failing migration test**

Add to `backend/internal/store/migrations_test.go`:

```go
func TestRoutingPoolFallbackMigrationIsEmbedded(t *testing.T) {
	sql, err := MigrationSQL("00025_routing_pool_fallback.sql")
	if err != nil {
		t.Fatalf("MigrationSQL returned error: %v", err)
	}
	for _, want := range []string{
		"ALTER TABLE routing_pools ADD COLUMN IF NOT EXISTS fallback_pool_id",
		"REFERENCES routing_pools(id) ON DELETE SET NULL",
		"routing_pools_fallback_pool_idx",
		"ALTER TABLE request_logs ADD COLUMN IF NOT EXISTS routing_pool_fallback_depth",
		"ALTER TABLE request_logs ADD COLUMN IF NOT EXISTS routing_pool_fallback_chain",
		"ALTER TABLE request_logs ADD COLUMN IF NOT EXISTS routing_pool_error",
	} {
		if !strings.Contains(sql, want) {
			t.Fatalf("routing pool fallback migration missing %q", want)
		}
	}
}
```

Update `TestMigrationProviderSeesEmbeddedMigrations` to expect `00025_routing_pool_fallback.sql` as the final migration.

- [ ] **Step 2: Run migration test and verify RED**

Run:

```bash
cd backend && GOMODCACHE=/root/Clouds/N2API/.cache/go-mod GOCACHE=/root/Clouds/N2API/.cache/go-build go test ./internal/store -run 'RoutingPoolFallbackMigration|MigrationProviderSeesEmbeddedMigrations'
```

Expected: FAIL because `00025_routing_pool_fallback.sql` is missing or the provider count is still old.

- [ ] **Step 3: Add migration**

Create `backend/internal/store/migrations/00025_routing_pool_fallback.sql`:

```sql
-- +goose Up
ALTER TABLE routing_pools ADD COLUMN IF NOT EXISTS fallback_pool_id BIGINT REFERENCES routing_pools(id) ON DELETE SET NULL;

CREATE INDEX IF NOT EXISTS routing_pools_fallback_pool_idx
    ON routing_pools (fallback_pool_id)
    WHERE fallback_pool_id IS NOT NULL;

ALTER TABLE request_logs ADD COLUMN IF NOT EXISTS routing_pool_fallback_depth INTEGER NOT NULL DEFAULT 0;
ALTER TABLE request_logs ADD COLUMN IF NOT EXISTS routing_pool_fallback_chain TEXT NOT NULL DEFAULT '';
ALTER TABLE request_logs ADD COLUMN IF NOT EXISTS routing_pool_error TEXT NOT NULL DEFAULT '';

-- +goose Down
ALTER TABLE request_logs DROP COLUMN IF EXISTS routing_pool_error;
ALTER TABLE request_logs DROP COLUMN IF EXISTS routing_pool_fallback_chain;
ALTER TABLE request_logs DROP COLUMN IF EXISTS routing_pool_fallback_depth;
DROP INDEX IF EXISTS routing_pools_fallback_pool_idx;
ALTER TABLE routing_pools DROP COLUMN IF EXISTS fallback_pool_id;
```

- [ ] **Step 4: Extend admin DTOs and repository contract**

In `backend/internal/admin/service.go`, extend `RoutingPool`:

```go
FallbackPoolID   *int64 `json:"fallbackPoolId"`
FallbackPoolName string `json:"fallbackPoolName"`
```

Change service methods to accept fallback ids:

```go
CreateRoutingPool(ctx context.Context, name, description string, enabled bool, fallbackPoolID *int64) (RoutingPool, error)
UpdateRoutingPool(ctx context.Context, id int64, name, description string, enabled bool, fallbackPoolID *int64) (RoutingPool, error)
```

Mirror those signatures in the `Repository` interface.

- [ ] **Step 5: Write failing admin service tests**

Add to `backend/internal/admin/service_test.go`:

```go
func TestRoutingPoolFallbackValidationRejectsSelfAndCycles(t *testing.T) {
	repo := newMemoryRepo()
	service := NewService(repo, Config{})

	primary, err := service.CreateRoutingPool(context.Background(), "primary", "", true, nil)
	if err != nil {
		t.Fatalf("CreateRoutingPool primary returned error: %v", err)
	}
	secondary, err := service.CreateRoutingPool(context.Background(), "secondary", "", true, nil)
	if err != nil {
		t.Fatalf("CreateRoutingPool secondary returned error: %v", err)
	}

	if _, err := service.UpdateRoutingPool(context.Background(), primary.ID, "primary", "", true, &primary.ID); !errors.Is(err, ErrInvalidInput) {
		t.Fatalf("self fallback error = %v, want ErrInvalidInput", err)
	}

	if _, err := service.UpdateRoutingPool(context.Background(), primary.ID, "primary", "", true, &secondary.ID); err != nil {
		t.Fatalf("primary fallback update returned error: %v", err)
	}
	if _, err := service.UpdateRoutingPool(context.Background(), secondary.ID, "secondary", "", true, &primary.ID); !errors.Is(err, ErrInvalidInput) {
		t.Fatalf("cycle fallback error = %v, want ErrInvalidInput", err)
	}
}

func TestRoutingPoolFallbackValidationRejectsMissingTarget(t *testing.T) {
	repo := newMemoryRepo()
	service := NewService(repo, Config{})

	pool, err := service.CreateRoutingPool(context.Background(), "primary", "", true, nil)
	if err != nil {
		t.Fatalf("CreateRoutingPool returned error: %v", err)
	}
	missing := int64(999)
	if _, err := service.UpdateRoutingPool(context.Background(), pool.ID, "primary", "", true, &missing); !errors.Is(err, ErrInvalidInput) {
		t.Fatalf("missing fallback error = %v, want ErrInvalidInput", err)
	}
}
```

Update existing routing-pool service tests and memory repo method signatures.

- [ ] **Step 6: Implement admin validation**

Add helper in `backend/internal/admin/service.go`:

```go
func normalizeRoutingPoolFallbackID(id *int64) (*int64, error) {
	if id == nil || *id == 0 {
		return nil, nil
	}
	if *id < 0 {
		return nil, ErrInvalidInput
	}
	normalized := *id
	return &normalized, nil
}
```

In `CreateRoutingPool` and `UpdateRoutingPool`, trim name/description as today, normalize fallback id, reject self fallback on update, and delegate to repository. The repository/memory repo should reject missing fallback targets and cycles.

- [ ] **Step 7: Implement store persistence**

In `backend/internal/store/admin.go`:

- Add `fallback_pool_id` to `INSERT INTO routing_pools`.
- Add `fallback_pool_id` to `UPDATE routing_pools`.
- In `getRoutingPool`, join fallback pool:

```sql
LEFT JOIN routing_pools fp ON fp.id = p.fallback_pool_id
```

and scan:

```go
&pool.FallbackPoolID,
&pool.FallbackPoolName,
```

- Before create/update with a non-nil fallback id, call a repository helper:

```go
func (r *AdminRepository) validateRoutingPoolFallback(ctx context.Context, poolID int64, fallbackPoolID *int64) error
```

The helper should walk `fallback_pool_id` links and return `admin.ErrInvalidInput` when the target is missing, self-referential, or cyclic.

- [ ] **Step 8: Run admin/store tests and commit**

Run:

```bash
cd backend && GOMODCACHE=/root/Clouds/N2API/.cache/go-mod GOCACHE=/root/Clouds/N2API/.cache/go-build go test ./internal/admin ./internal/store -run 'RoutingPoolFallback|RoutingPoolService|RoutingPoolsMigration|MigrationProviderSeesEmbeddedMigrations'
```

Expected: PASS.

Commit:

```bash
git add backend/internal/store/migrations/00025_routing_pool_fallback.sql backend/internal/store/migrations_test.go backend/internal/admin/service.go backend/internal/admin/service_test.go backend/internal/store/admin.go backend/internal/store/admin_test.go
git commit -m "feat: store routing pool fallback"
```

## Task 2: HTTP API And Admin UI Configuration

**Files:**
- Modify: `backend/internal/httpapi/server.go`
- Modify: `backend/internal/httpapi/server_test.go`
- Modify: `frontend/src/lib/admin-state.svelte.js`
- Modify: `frontend/src/routes/routing-pools/+page.svelte`
- Modify: `frontend/src/routes/api-keys/+page.svelte`
- Modify: `frontend/src/routes/navigation.test.mjs`
- Modify: `frontend/src/routes/providers/provider-page.test.mjs`

- [ ] **Step 1: Write failing HTTP tests**

Update routing-pool create/update tests in `backend/internal/httpapi/server_test.go` so request bodies include:

```json
{"name":"primary","description":"daily","enabled":true,"fallbackPoolId":2}
```

Assert the fake admin service receives `FallbackPoolID == 2`. Add a clear test:

```go
req := httptest.NewRequest(http.MethodPatch, "/api/admin/routing-pools/1", strings.NewReader(`{"name":"primary","enabled":true,"fallbackPoolId":null}`))
```

Expected fake service receives `FallbackPoolID == nil`.

- [ ] **Step 2: Run HTTP tests and verify RED**

Run:

```bash
cd backend && GOMODCACHE=/root/Clouds/N2API/.cache/go-mod GOCACHE=/root/Clouds/N2API/.cache/go-build go test ./internal/httpapi -run 'RoutingPool'
```

Expected: FAIL because the request structs do not parse/pass `fallbackPoolId`.

- [ ] **Step 3: Implement HTTP fallback payloads**

In `backend/internal/httpapi/server.go`, extend create/update request structs:

```go
FallbackPoolID *int64 `json:"fallbackPoolId"`
```

Pass the pointer to `CreateRoutingPool` and `UpdateRoutingPool`.

- [ ] **Step 4: Write failing frontend tests**

In `frontend/src/routes/navigation.test.mjs`, extend `routing pools page manages account pools`:

```js
for (const label of ['Fallback pool', 'No fallback']) {
  assert.match(poolsPage, new RegExp(label.replace(' ', '\\s+')), `routing pools page should include ${label}`);
}
assert.match(poolsPage, /pool\.fallbackPoolId/);
assert.match(poolsPage, /pool\.id === candidate\.id/);
```

In `frontend/src/routes/providers/provider-page.test.mjs`, extend the routing pool state test:

```js
assert.match(adminState, /fallbackPoolId/);
assert.match(adminState, /fallbackPoolName/);
assert.match(adminState, /body: JSON\.stringify\(\{ name, description, enabled, fallbackPoolId/);
```

- [ ] **Step 5: Run frontend tests and verify RED**

Run:

```bash
cd frontend && bun test src/routes/navigation.test.mjs src/routes/providers/provider-page.test.mjs
```

Expected: FAIL because the UI/state does not include fallback fields.

- [ ] **Step 6: Implement frontend state and UI**

In `frontend/src/lib/admin-state.svelte.js`, extend `RoutingPool` typedef:

```js
 * @property {number | null} fallbackPoolId
 * @property {string} fallbackPoolName
```

When creating/updating pools, include:

```js
const fallbackPoolId = Number(pool.fallbackPoolId ?? 0);
body: JSON.stringify({
  name,
  description,
  enabled,
  fallbackPoolId: fallbackPoolId > 0 ? fallbackPoolId : null
})
```

In `frontend/src/routes/routing-pools/+page.svelte`, add helper:

```js
function fallbackWarning(pool) {
  const fallbackID = Number(pool.fallbackPoolId ?? 0);
  if (fallbackID <= 0) return '';
  const target = routingPools.items.find((candidate) => candidate.id === fallbackID);
  if (!target) return 'Fallback pool is missing.';
  if (!target.enabled) return 'Fallback pool is disabled.';
  return '';
}
```

Add selector in each pool article:

```svelte
<label class="text-sm font-medium text-[#3c3c3c]">
  Fallback pool
  <select class="mt-2 w-full rounded-lg border border-[#e5e5e5] bg-white px-3 py-2 text-sm text-[#0d0d0d] outline-none focus:border-[#10a37f] focus:ring-2 focus:ring-[#e8f5f0]" bind:value={pool.fallbackPoolId}>
    <option value={0}>No fallback</option>
    {#each routingPools.items as candidate}
      <option value={candidate.id} disabled={pool.id === candidate.id}>{candidate.name}</option>
    {/each}
  </select>
</label>
```

Show warning:

```svelte
{#if fallbackWarning(pool)}
  <p class="mt-2 rounded-md border border-amber-200 bg-amber-50 p-2 text-xs leading-5 text-amber-800">{fallbackWarning(pool)}</p>
{/if}
```

In `frontend/src/routes/api-keys/+page.svelte`, under routing pool label:

```svelte
{@const keyPool = routingPools.items.find((pool) => pool.id === key.routingPoolId)}
{#if keyPool?.fallbackPoolName}
  <span>Fallback {keyPool.fallbackPoolName}</span>
{/if}
```

- [ ] **Step 7: Run HTTP/frontend tests and commit**

Run:

```bash
cd backend && GOMODCACHE=/root/Clouds/N2API/.cache/go-mod GOCACHE=/root/Clouds/N2API/.cache/go-build go test ./internal/httpapi -run 'RoutingPool'
cd frontend && bun test src/routes/navigation.test.mjs src/routes/providers/provider-page.test.mjs
```

Expected: PASS.

Commit:

```bash
git add backend/internal/httpapi/server.go backend/internal/httpapi/server_test.go frontend/src/lib/admin-state.svelte.js frontend/src/routes/routing-pools/+page.svelte frontend/src/routes/api-keys/+page.svelte frontend/src/routes/navigation.test.mjs frontend/src/routes/providers/provider-page.test.mjs
git commit -m "feat: configure routing pool fallback"
```

## Task 3: Provider Chain Selection

**Files:**
- Modify: `backend/internal/provider/service.go`
- Modify: `backend/internal/provider/service_test.go`
- Modify: `backend/internal/store/provider.go`
- Modify: `backend/internal/store/provider_test.go`

- [ ] **Step 1: Write failing provider tests**

Add to `backend/internal/provider/service_test.go`:

```go
func TestSelectAccountForModelInRoutingPoolChainFallsBackByModel(t *testing.T) {
	repo := newMemoryRepo()
	repo.routingPools[1] = RoutingPool{ID: 1, Name: "primary", Enabled: true, FallbackPoolID: ptrInt64(2)}
	repo.routingPools[2] = RoutingPool{ID: 2, Name: "secondary", Enabled: true}
	repo.accounts = []Account{
		testAccount(t, 10, true, 1, "primary-token"),
		testAccount(t, 20, true, 1, "secondary-token"),
	}
	repo.routingPoolAccounts[1] = []RoutingPoolAccount{{AccountID: 10, Priority: 0}}
	repo.routingPoolAccounts[2] = []RoutingPoolAccount{{AccountID: 20, Priority: 0}}
	repo.accountModels[10] = []AccountModel{{AccountID: 10, Provider: "openai", Model: "gpt-4", Enabled: true}}
	repo.accountModels[20] = []AccountModel{{AccountID: 20, Provider: "openai", Model: "gpt-5", Enabled: true}}
	service := newConfiguredService(repo, fakeOAuthClient{})

	selected, err := service.SelectAccountForModelInRoutingPoolChain(context.Background(), 1, "gpt-5")
	if err != nil {
		t.Fatalf("SelectAccountForModelInRoutingPoolChain returned error: %v", err)
	}
	if selected.AccountID != 20 || selected.RoutingPoolID != 2 || selected.RoutingPoolFallbackDepth != 1 {
		t.Fatalf("selected = %+v, want account 20 in fallback pool 2 depth 1", selected)
	}
	if selected.RoutingPoolFallbackChain != "primary -> secondary" {
		t.Fatalf("chain = %q, want primary -> secondary", selected.RoutingPoolFallbackChain)
	}
}
```

Add cycle drift test:

```go
func TestSelectAccountForModelInRoutingPoolChainRejectsCycle(t *testing.T) {
	repo := newMemoryRepo()
	repo.routingPools[1] = RoutingPool{ID: 1, Name: "primary", Enabled: true, FallbackPoolID: ptrInt64(2)}
	repo.routingPools[2] = RoutingPool{ID: 2, Name: "secondary", Enabled: true, FallbackPoolID: ptrInt64(1)}
	service := newConfiguredService(repo, fakeOAuthClient{})

	if _, err := service.SelectAccountForModelInRoutingPoolChain(context.Background(), 1, "gpt-5"); !errors.Is(err, ErrRoutingPoolCycle) {
		t.Fatalf("cycle error = %v, want ErrRoutingPoolCycle", err)
	}
}
```

- [ ] **Step 2: Run provider tests and verify RED**

Run:

```bash
cd backend && GOMODCACHE=/root/Clouds/N2API/.cache/go-mod GOCACHE=/root/Clouds/N2API/.cache/go-build go test ./internal/provider -run 'RoutingPoolChain'
```

Expected: FAIL because chain methods and `ErrRoutingPoolCycle` do not exist.

- [ ] **Step 3: Implement provider chain types and methods**

In `backend/internal/provider/service.go`:

```go
ErrRoutingPoolCycle = errors.New("routing pool fallback cycle")
```

Extend `RoutingPool`:

```go
FallbackPoolID *int64
```

Extend `SelectedAccount`:

```go
RoutingPoolFallbackDepth int
RoutingPoolFallbackChain string
RoutingPoolError         string
```

Add methods:

```go
func (s *Service) SelectAccountForModelInRoutingPoolChain(ctx context.Context, primaryPoolID int64, model string, excludedAccountIDs ...int64) (SelectedAccount, error)
func (s *Service) SelectAccountForModelAndSessionInRoutingPoolChain(ctx context.Context, primaryPoolID int64, model, sessionID string, excludedAccountIDs ...int64) (SelectedAccount, error)
```

Implementation shape:

```go
func (s *Service) routingPoolChain(ctx context.Context, primaryPoolID int64) ([]RoutingPool, string, error) {
	visited := map[int64]struct{}{}
	var pools []RoutingPool
	for id := primaryPoolID; id > 0; {
		if _, ok := visited[id]; ok {
			return nil, "", ErrRoutingPoolCycle
		}
		visited[id] = struct{}{}
		pool, err := s.repo.FindRoutingPool(ctx, id)
		if err != nil {
			return nil, "", err
		}
		pools = append(pools, pool)
		if pool.FallbackPoolID == nil || *pool.FallbackPoolID <= 0 {
			break
		}
		id = *pool.FallbackPoolID
	}
	return pools, routingPoolChainLabel(pools), nil
}
```

Loop over pools. Reject disabled primary as `ErrAccountsDisabled`; skip disabled fallback pools. For each enabled pool, call `selectionCandidatesForRoutingPool`; on success set selected diagnostics and return. Preserve most-specific error if all pools fail.

- [ ] **Step 4: Update provider repository**

In `backend/internal/store/provider.go`, update `FindRoutingPool` query:

```sql
SELECT id, name, enabled, fallback_pool_id
FROM routing_pools
WHERE id = $1
```

Scan `&pool.FallbackPoolID`.

Update provider memory repo in tests.

- [ ] **Step 5: Run provider/store tests and commit**

Run:

```bash
cd backend && GOMODCACHE=/root/Clouds/N2API/.cache/go-mod GOCACHE=/root/Clouds/N2API/.cache/go-build go test ./internal/provider ./internal/store -run 'RoutingPoolChain|RoutingPool'
```

Expected: PASS.

Commit:

```bash
git add backend/internal/provider/service.go backend/internal/provider/service_test.go backend/internal/store/provider.go backend/internal/store/provider_test.go
git commit -m "feat: select routing pool fallback chains"
```

## Task 4: Gateway Wiring, Logs, And Model List

**Files:**
- Modify: `backend/internal/gateway/proxy.go`
- Modify: `backend/internal/gateway/proxy_test.go`
- Modify: `backend/internal/store/gateway.go`
- Modify: `backend/internal/store/gateway_test.go`
- Modify: `backend/internal/admin/service.go`
- Modify: `backend/internal/store/admin.go`
- Modify: `backend/internal/store/admin_test.go`

- [ ] **Step 1: Write failing gateway tests**

Add to `backend/internal/gateway/proxy_test.go`:

```go
func TestProxyRoutesPoolBoundKeyThroughFallbackPool(t *testing.T) {
	logger := &fakeRequestLogger{}
	poolID := int64(1)
	accounts := &fakeSelectedAccountProvider{
		accounts: []SelectedAccount{{
			AccountID:                20,
			AccountType:              provider.AccountTypeAPIUpstream,
			DisplayName:              "Fallback Account",
			AuthorizationToken:       "fallback-token",
			RoutingPoolID:            2,
			RoutingPoolName:          "secondary",
			RoutingPoolFallbackDepth: 1,
			RoutingPoolFallbackChain: "primary -> secondary",
		}},
	}
	client := &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
		if got := r.Header.Get("Authorization"); got != "Bearer fallback-token" {
			t.Fatalf("Authorization = %q, want fallback token", got)
		}
		return &http.Response{StatusCode: http.StatusOK, Header: http.Header{"Content-Type": []string{"application/json"}}, Body: io.NopCloser(strings.NewReader(`{"ok":true}`)), Request: r}, nil
	})}
	proxy := NewProxyWithClient(&fakeAPIKeyAuthenticator{key: admin.APIKey{ID: 42, RoutingPoolID: &poolID, RoutingPoolName: "primary"}}, accounts, Config{UpstreamBaseURL: "https://upstream.example.test", Logger: logger}, client)
	req := httptest.NewRequest(http.MethodPost, "/v1/responses", strings.NewReader(`{"model":"gpt-5","input":"hi"}`))
	req.Header.Set("Authorization", "Bearer n2api_client_secret")
	recorder := httptest.NewRecorder()

	proxy.ServeHTTP(recorder, req)

	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d body=%s, want 200", recorder.Code, recorder.Body.String())
	}
	if len(logger.entries) != 1 || logger.entries[0].RoutingPoolID != 2 || logger.entries[0].RoutingPoolFallbackDepth != 1 {
		t.Fatalf("log entry = %+v, want fallback pool diagnostics", logger.entries)
	}
}
```

- [ ] **Step 2: Run gateway test and verify RED**

Run:

```bash
cd backend && GOMODCACHE=/root/Clouds/N2API/.cache/go-mod GOCACHE=/root/Clouds/N2API/.cache/go-build go test ./internal/gateway -run 'FallbackPool|RoutingPool'
```

Expected: FAIL because gateway selected/log types do not include the new diagnostics and fake provider does not implement chain selection.

- [ ] **Step 3: Implement gateway chain interface**

In `backend/internal/gateway/proxy.go`, add:

```go
type RoutingPoolChainAccountProvider interface {
	SelectAccountForModelInRoutingPoolChain(ctx context.Context, routingPoolID int64, model string, excludedAccountIDs ...int64) (SelectedAccount, error)
	SelectAccountForModelAndSessionInRoutingPoolChain(ctx context.Context, routingPoolID int64, model, sessionID string, excludedAccountIDs ...int64) (SelectedAccount, error)
}
```

Extend `SelectedAccount` and `RequestLog` with:

```go
RoutingPoolFallbackDepth int
RoutingPoolFallbackChain string
RoutingPoolError         string
```

In `selectAccountForKey`, prefer chain provider for pool-bound keys. Fall back to existing single-pool provider only if chain interface is unavailable.

Map `provider.ErrRoutingPoolCycle` to `routing_pool_cycle`.

- [ ] **Step 4: Persist request-log fields**

In `backend/internal/store/gateway.go`, add the three columns to insert SQL and args.

In `backend/internal/store/admin.go`, select and scan these fields in `ListRequestLogs`.

In `backend/internal/admin/service.go`, add fields to `RequestLog`.

Update store/admin tests to require `routing_pool_fallback_depth`, `routing_pool_fallback_chain`, and `routing_pool_error` in SQL.

- [ ] **Step 5: Chain-aware `/v1/models`**

Update admin/provider model listing logic so pool-bound API keys filter models by the full fallback chain. Add backend tests proving:

```go
// key bound to pool 1, model exists only in fallback pool 2
// GET /v1/models includes that model
```

If current model listing code cannot cleanly reuse provider chain selection, add repository helper:

```go
ListExposedModelsForRoutingPoolChain(ctx context.Context, provider string, primaryPoolID int64, allowedModels []string) ([]provider.ExposedModel, error)
```

- [ ] **Step 6: Run gateway/store/admin tests and commit**

Run:

```bash
cd backend && GOMODCACHE=/root/Clouds/N2API/.cache/go-mod GOCACHE=/root/Clouds/N2API/.cache/go-build go test ./internal/gateway ./internal/store ./internal/admin -run 'RoutingPool|Fallback|Models'
```

Expected: PASS.

Commit:

```bash
git add backend/internal/gateway/proxy.go backend/internal/gateway/proxy_test.go backend/internal/store/gateway.go backend/internal/store/gateway_test.go backend/internal/admin/service.go backend/internal/store/admin.go backend/internal/store/admin_test.go
git commit -m "feat: route gateway through fallback pools"
```

## Task 5: Request Log UI, Documentation, And Full Verification

**Files:**
- Modify: `frontend/src/lib/admin-state.svelte.js`
- Modify: `frontend/src/routes/request-logs/+page.svelte`
- Modify: `frontend/src/routes/navigation.test.mjs`
- Modify: `README.md`
- Modify: `deploy/README.md`
- Modify: `backend/internal/gateway/documentation_test.go`

- [ ] **Step 1: Write failing frontend log tests**

In `frontend/src/routes/navigation.test.mjs`, extend request-log diagnostics test:

```js
assert.match(requestLogsPage, /routingPoolFallbackDepth/);
assert.match(requestLogsPage, /routingPoolFallbackChain/);
assert.match(requestLogsPage, /routingPoolError/);
assert.match(requestLogsPage, /Fallback chain/);
assert.match(adminState, /routingPoolFallbackDepth/);
```

- [ ] **Step 2: Implement request-log frontend diagnostics**

In `frontend/src/lib/admin-state.svelte.js`, extend `RequestLog` typedef:

```js
 * @property {number} routingPoolFallbackDepth
 * @property {string} routingPoolFallbackChain
 * @property {string} routingPoolError
```

In `frontend/src/routes/request-logs/+page.svelte`, enhance Routing pool cell:

```svelte
{#if log.routingPoolFallbackDepth > 0}
  <p class="mt-1 text-xs text-[#6e6e6e]">Fallback depth {log.routingPoolFallbackDepth}</p>
{/if}
{#if log.routingPoolFallbackChain}
  <p class="mt-1 max-w-[180px] truncate text-xs text-[#6e6e6e]" title={log.routingPoolFallbackChain}>Fallback chain {log.routingPoolFallbackChain}</p>
{/if}
{#if log.routingPoolError}
  <p class="mt-1 text-xs font-medium text-amber-700">{errorLabel(log.routingPoolError)}</p>
{/if}
```

- [ ] **Step 3: Write documentation test**

Add to `backend/internal/gateway/documentation_test.go`:

```go
func TestGatewayDocumentationMentionsRoutingPoolFallback(t *testing.T) {
	for _, path := range []string{"../../../README.md", "../../../deploy/README.md"} {
		content, err := os.ReadFile(path)
		if err != nil {
			t.Fatalf("ReadFile(%q) returned error: %v", path, err)
		}
		text := string(content)
		for _, want := range []string{
			"routing pool fallback",
			"pool-bound key never falls back to the global provider account pool",
			"`routing_pool_cycle`",
			"`routing_pool_exhausted`",
		} {
			if !strings.Contains(text, want) {
				t.Fatalf("%s missing %q in routing pool fallback documentation", path, want)
			}
		}
	}
}
```

- [ ] **Step 4: Document fallback behavior**

In `README.md` and `deploy/README.md`, add near the routing pools paragraph:

```markdown
Routing pool fallback is explicit. A routing pool can point to one fallback pool, forming a simple chain such as `primary -> secondary`. A pool-bound key tries only its configured pool and that explicit chain; it never falls back to the global provider account pool. Cycles fail closed with `routing_pool_cycle`, and exhausted chains are logged as `routing_pool_exhausted`.
```

- [ ] **Step 5: Run final verification**

Run:

```bash
cd backend && GOMODCACHE=/root/Clouds/N2API/.cache/go-mod GOCACHE=/root/Clouds/N2API/.cache/go-build go test ./...
cd frontend && bun test src/routes/navigation.test.mjs src/routes/providers/provider-page.test.mjs
cd frontend && bun run check
cd frontend && bun run build
```

If backend `go test ./...` fails in the sandbox with `httptest: failed to listen on a port: listen tcp6 [::1]:0: socket: operation not permitted`, rerun the exact same command with elevated permissions and treat that rerun as the backend verdict.

- [ ] **Step 6: Commit docs/UI diagnostics**

Commit:

```bash
git add frontend/src/lib/admin-state.svelte.js frontend/src/routes/request-logs/+page.svelte frontend/src/routes/navigation.test.mjs README.md deploy/README.md backend/internal/gateway/documentation_test.go
git commit -m "docs: document routing pool fallback"
```

## Self-Review

- Spec coverage: the plan covers schema/admin validation, HTTP/UI configuration, provider chain selection, gateway/log/model behavior, UI diagnostics, documentation, and full verification.
- Placeholder scan: no placeholder or copy-forward instructions remain.
- Type consistency: fallback fields are consistently named `fallbackPoolId`, `fallbackPoolName`, `RoutingPoolFallbackDepth`, `RoutingPoolFallbackChain`, and `RoutingPoolError`.

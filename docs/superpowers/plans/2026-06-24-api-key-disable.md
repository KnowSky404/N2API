# API Key Disable Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add reversible disable/enable management for client API keys while keeping revoke permanent.

**Architecture:** Add nullable `disabled_at` to `client_api_keys`, thread it through `admin.APIKey`, repository scans, authentication filtering, HTTP responses, and Svelte admin UI. Use a focused `SetAPIKeyDisabled` method instead of overloading revoke.

**Tech Stack:** Go, PostgreSQL/goose migrations, SvelteKit, Bun tests.

---

### Task 1: Commit Design And Plan

**Files:**
- Create: `docs/superpowers/specs/2026-06-24-api-key-disable-design.md`
- Create: `docs/superpowers/plans/2026-06-24-api-key-disable.md`

- [ ] **Step 1: Commit docs**

```bash
git add docs/superpowers/specs/2026-06-24-api-key-disable-design.md docs/superpowers/plans/2026-06-24-api-key-disable.md
git commit -m "docs: plan api key disable"
```

Expected: commit succeeds.

### Task 2: Schema And Store

**Files:**
- Create: `backend/internal/store/migrations/00022_client_api_key_disabled_at.sql`
- Modify: `backend/internal/store/migrations_test.go`
- Modify: `backend/internal/admin/service.go`
- Modify: `backend/internal/admin/service_test.go`
- Modify: `backend/internal/store/admin.go`
- Modify: `backend/internal/store/admin_test.go`

- [ ] **Step 1: Add failing migration and service tests**

Add migration test requiring:

```go
"ALTER TABLE client_api_keys ADD COLUMN IF NOT EXISTS disabled_at TIMESTAMPTZ",
"ALTER TABLE client_api_keys DROP COLUMN IF EXISTS disabled_at",
```

Add admin service test that creates a key, disables it, verifies authentication returns `ErrUnauthorized`, re-enables it, and verifies authentication succeeds.

- [ ] **Step 2: Verify red**

```bash
cd backend && GOMODCACHE=/root/Clouds/N2API/.cache/go-mod GOCACHE=/root/Clouds/N2API/.cache/go-build go test ./internal/admin ./internal/store -run 'Disabled|Disable'
```

Expected: FAIL because `disabled_at` and `SetAPIKeyDisabled` do not exist.

- [ ] **Step 3: Implement migration and APIKey field**

Create migration:

```sql
-- +goose Up
ALTER TABLE client_api_keys ADD COLUMN IF NOT EXISTS disabled_at TIMESTAMPTZ;

-- +goose Down
ALTER TABLE client_api_keys DROP COLUMN IF EXISTS disabled_at;
```

Add to `admin.APIKey` and frontend typedef:

```go
DisabledAt *time.Time `json:"disabledAt"`
```

- [ ] **Step 4: Implement store scans and auth filtering**

Update all `client_api_keys` `RETURNING` and `SELECT` scans to include `disabled_at`. Update `FindAPIKeyByHash` with `AND disabled_at IS NULL`.

Add repository method:

```go
SetAPIKeyDisabled(ctx context.Context, id int64, disabled bool) (admin.APIKey, error)
```

Use `disabled_at = CASE WHEN $2 THEN COALESCE(disabled_at, now()) ELSE NULL END` and `revoked_at IS NULL`.

- [ ] **Step 5: Implement service and memory repo**

Add `SetAPIKeyDisabled` to `admin.Repository` and `admin.Service`. In `AuthenticateAPIKey`, reject keys whose `DisabledAt != nil`.

- [ ] **Step 6: Verify store/admin**

```bash
cd backend && GOMODCACHE=/root/Clouds/N2API/.cache/go-mod GOCACHE=/root/Clouds/N2API/.cache/go-build go test ./internal/admin ./internal/store
```

Expected: PASS or store integration tests skip without test database.

- [ ] **Step 7: Commit backend schema/service**

```bash
git add backend/internal/store/migrations/00022_client_api_key_disabled_at.sql backend/internal/store/migrations_test.go backend/internal/admin/service.go backend/internal/admin/service_test.go backend/internal/store/admin.go backend/internal/store/admin_test.go
git commit -m "feat: disable api keys in admin service"
```

### Task 3: HTTP Endpoint

**Files:**
- Modify: `backend/internal/httpapi/server.go`
- Modify: `backend/internal/httpapi/server_test.go`

- [ ] **Step 1: Add failing HTTP tests**

Add tests for:

- `PUT /api/admin/keys/7/disabled` with `{"disabled":true}` returns disabled key.
- `PUT /api/admin/keys/7/disabled` with `{"disabled":false}` returns enabled key.
- invalid id returns `400`.
- not found returns `404`.

- [ ] **Step 2: Verify red**

```bash
cd backend && GOMODCACHE=/root/Clouds/N2API/.cache/go-mod GOCACHE=/root/Clouds/N2API/.cache/go-build go test ./internal/httpapi -run 'APIKeyDisabled|DisableAPIKey'
```

Expected: FAIL because endpoint does not exist.

- [ ] **Step 3: Implement endpoint and response**

Add `SetAPIKeyDisabled` to `httpapi.AdminService`.

Add:

```go
mux.HandleFunc("PUT /api/admin/keys/{id}/disabled", requireAdmin(func(w http.ResponseWriter, r *http.Request, _ admin.Admin) {
  id, err := parsePositivePathID(r, "id")
  if err != nil {
    writeError(w, http.StatusBadRequest, "bad_request")
    return
  }
  var req struct {
    Disabled bool `json:"disabled"`
  }
  if err := decodeJSON(w, r, &req); err != nil {
    writeError(w, http.StatusBadRequest, "bad_request")
    return
  }
  key, err := admins.SetAPIKeyDisabled(r.Context(), id, req.Disabled)
  if err != nil {
    if errors.Is(err, admin.ErrNotFound) {
      writeError(w, http.StatusNotFound, "not_found")
      return
    }
    writeError(w, http.StatusInternalServerError, "internal_error")
    return
  }
  writeJSON(w, http.StatusOK, map[string]admin.APIKey{"key": key})
}))
```

- [ ] **Step 4: Verify HTTP**

```bash
cd backend && GOMODCACHE=/root/Clouds/N2API/.cache/go-mod GOCACHE=/root/Clouds/N2API/.cache/go-build go test ./internal/httpapi -run 'APIKeyDisabled|DisableAPIKey'
```

Expected: PASS.

- [ ] **Step 5: Commit HTTP**

```bash
git add backend/internal/httpapi/server.go backend/internal/httpapi/server_test.go
git commit -m "feat: expose api key disable endpoint"
```

### Task 4: Frontend UI

**Files:**
- Modify: `frontend/src/lib/admin-state.svelte.js`
- Modify: `frontend/src/routes/api-keys/+page.svelte`
- Modify: `frontend/src/routes/navigation.test.mjs`

- [ ] **Step 1: Add failing frontend source test**

Assert:

```js
assert.match(apiKeysPage, /Disabled keys/);
assert.match(apiKeysPage, /key\.disabledAt/);
assert.match(apiKeysPage, /Disabled/);
assert.match(apiKeysPage, /setAPIKeyDisabled/);
assert.match(apiKeysPage, /Enable/);
assert.match(apiKeysPage, /Disable/);
assert.match(adminState, /export async function setAPIKeyDisabled/);
assert.match(adminState, /\/api\/admin\/keys\/\$\{keyId\}\/disabled/);
```

- [ ] **Step 2: Verify red**

```bash
cd frontend && bun test src/routes/navigation.test.mjs
```

Expected: FAIL because disabled support is missing.

- [ ] **Step 3: Implement frontend state and UI**

Add `disabledAt` typedef and `getActiveKeys()` should filter `!key.revokedAt && !key.disabledAt`.

Add:

```js
export async function setAPIKeyDisabled(keyId, disabled) {
  const version = sessionVersion;
  if (!isCurrentAuthenticated(version)) return;
  apiKeys.error = '';
  try {
    const payload = await requestJSON(`/api/admin/keys/${keyId}/disabled`, {
      method: 'PUT',
      body: JSON.stringify({ disabled: Boolean(disabled) })
    });
    if (!isCurrentAuthenticated(version)) return;
    apiKeys.items = apiKeys.items.map((key) => (key.id === keyId ? payload.key : key));
  } catch (error) {
    if (!isCurrentAuthenticated(version)) return;
    apiKeys.error = error instanceof Error ? error.message : 'Failed to update API key status';
  }
}
```

Update search/status filter and status chip:

- status filter includes disabled.
- status text prioritizes revoked, disabled, active.
- action button toggles disabled for non-revoked keys.

- [ ] **Step 4: Verify frontend**

```bash
cd frontend && bun test src/routes/navigation.test.mjs
cd frontend && bun run check
```

Expected: PASS.

- [ ] **Step 5: Commit frontend**

```bash
git add frontend/src/lib/admin-state.svelte.js frontend/src/routes/api-keys/+page.svelte frontend/src/routes/navigation.test.mjs
git commit -m "feat: disable api keys in admin ui"
```

### Task 5: Documentation And Final Verification

**Files:**
- Modify: `README.md`
- Modify: `deploy/README.md`
- Modify: `backend/internal/gateway/documentation_test.go`

- [ ] **Step 1: Add docs and docs test**

Document: `API keys can be temporarily disabled and re-enabled without revoking or rotating the secret; disabled keys cannot authenticate gateway requests but remain visible for configuration and logs.`

- [ ] **Step 2: Commit docs**

```bash
git add README.md deploy/README.md backend/internal/gateway/documentation_test.go
git commit -m "docs: document api key disable"
```

- [ ] **Step 3: Final verification**

```bash
git diff --check
cd backend && GOMODCACHE=/root/Clouds/N2API/.cache/go-mod GOCACHE=/root/Clouds/N2API/.cache/go-build go test ./...
cd frontend && bun test src/routes/navigation.test.mjs
cd frontend && bun run check
cd frontend && bun run build
git status --short
```

Expected: all commands pass and worktree is clean.

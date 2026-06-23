# Provider Account Bulk Enable Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add bulk enable and disable actions for provider accounts.

**Architecture:** Add a small HTTP bulk endpoint that reuses the existing provider account update service. Add frontend selection state and a bulk action bar on the Provider accounts page. Keep persistence and scheduling behavior unchanged: bulk disable uses the same `enabled=false` state already honored by gateway routing.

**Tech Stack:** Go HTTP API, provider service, SvelteKit admin UI, Bun tests.

---

## File Structure

- `backend/internal/httpapi/server.go`: add route and handler for bulk provider account updates.
- `backend/internal/httpapi/server_test.go`: add HTTP tests for the new endpoint.
- `frontend/src/lib/admin-state.svelte.js`: add selected-account state and bulk update action.
- `frontend/src/routes/providers/+page.svelte`: add checkboxes and action bar.
- `frontend/src/routes/providers/provider-page.test.mjs`: add frontend state/source tests.
- `README.md`, `deploy/README.md`, `backend/internal/gateway/documentation_test.go`: document bulk enable/disable.

## Task 1: Backend Bulk Update API

**Files:**
- Modify: `backend/internal/httpapi/server.go`
- Modify: `backend/internal/httpapi/server_test.go`

- [ ] **Step 1: Write failing HTTP tests**

Add tests:

```go
func TestAdminCanBulkDisableUnifiedProviderAccounts(t *testing.T)
func TestAdminBulkProviderAccountUpdateValidatesInput(t *testing.T)
```

The success test posts `{"accountIds":[7,8,7],"enabled":false}` to `/api/admin/provider-accounts/bulk-update` and asserts the fake provider receives ids `[7,8]` with `Enabled=false` and the response contains two accounts.

The validation test covers `[]`, `[0]`, more than `100` ids, and missing `enabled`.

- [ ] **Step 2: Run tests and verify red**

Run:

```bash
cd backend
GOMODCACHE=/root/Clouds/N2API/.cache/go-mod GOCACHE=/root/Clouds/N2API/.cache/go-build go test ./internal/httpapi -run 'BulkProviderAccount'
```

Expected: FAIL because the endpoint does not exist.

- [ ] **Step 3: Implement handler**

Add route:

```go
mux.HandleFunc("POST /api/admin/provider-accounts/bulk-update", requireAdmin(func(w http.ResponseWriter, r *http.Request, _ admin.Admin) {
    handleBulkUpdateProviderAccounts(w, r, providers)
}))
```

Add `handleBulkUpdateProviderAccounts` to decode ids and enabled, validate/dedupe ids, call `providers.UpdateAccount` for each id with `provider.AccountUpdate{Enabled: req.Enabled}`, and write `{ "accounts": accounts }`.

- [ ] **Step 4: Run tests and verify green**

Run:

```bash
cd backend
GOMODCACHE=/root/Clouds/N2API/.cache/go-mod GOCACHE=/root/Clouds/N2API/.cache/go-build go test ./internal/httpapi -run 'BulkProviderAccount'
```

Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add backend/internal/httpapi/server.go backend/internal/httpapi/server_test.go
git commit -m "feat: add provider account bulk update api"
```

## Task 2: Frontend Bulk State

**Files:**
- Modify: `frontend/src/lib/admin-state.svelte.js`
- Modify: `frontend/src/routes/providers/provider-page.test.mjs`

- [ ] **Step 1: Write failing frontend state tests**

Add tests/assertions for:

- `selectedProviderAccountIds`
- `toggleProviderAccountSelection`
- `clearProviderAccountSelection`
- `pruneSelectedProviderAccounts`
- `bulkUpdateSelectedProviderAccounts`
- endpoint `/api/admin/provider-accounts/bulk-update`

- [ ] **Step 2: Run tests and verify red**

Run:

```bash
cd frontend
bun test src/routes/providers/provider-page.test.mjs
```

Expected: FAIL because selection state and bulk action do not exist.

- [ ] **Step 3: Implement state and bulk request**

Add `selectedProviderAccountIds = $state({})`, helpers to toggle/clear/prune, and `bulkUpdateSelectedProviderAccounts(enabled)` that posts selected numeric ids and clears selection on success.

- [ ] **Step 4: Run tests and verify green**

Run:

```bash
cd frontend
bun test src/routes/providers/provider-page.test.mjs
```

Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add frontend/src/lib/admin-state.svelte.js frontend/src/routes/providers/provider-page.test.mjs
git commit -m "feat: add provider account bulk state"
```

## Task 3: Provider Page Bulk UI

**Files:**
- Modify: `frontend/src/routes/providers/+page.svelte`
- Modify: `frontend/src/routes/providers/provider-page.test.mjs`

- [ ] **Step 1: Write failing source tests**

Assert the Provider accounts page includes:

- `Enable selected`
- `Disable selected`
- `Clear selection`
- `toggleProviderAccountSelection`
- `selectedProviderAccountIds`

- [ ] **Step 2: Run tests and verify red**

Run:

```bash
cd frontend
bun test src/routes/providers/provider-page.test.mjs
```

Expected: FAIL because the page does not render bulk controls.

- [ ] **Step 3: Implement UI**

Import selection helpers. Add a checkbox column and a compact bulk action bar above the table. Disable bulk buttons when no accounts are selected or `providerAccounts.saving` is true.

- [ ] **Step 4: Run tests and verify green**

Run:

```bash
cd frontend
bun test src/routes/providers/provider-page.test.mjs
```

Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add frontend/src/routes/providers/+page.svelte frontend/src/routes/providers/provider-page.test.mjs
git commit -m "feat: add provider account bulk controls"
```

## Task 4: Documentation And Verification

**Files:**
- Modify: `README.md`
- Modify: `deploy/README.md`
- Modify: `backend/internal/gateway/documentation_test.go`

- [ ] **Step 1: Write failing documentation test**

Add `bulk enable or disable provider accounts` to provider-account documentation checks.

- [ ] **Step 2: Update docs**

Mention that selected provider accounts can be enabled or disabled together from the Provider accounts page.

- [ ] **Step 3: Run full verification**

Run:

```bash
git diff --check
cd backend
GOMODCACHE=/root/Clouds/N2API/.cache/go-mod GOCACHE=/root/Clouds/N2API/.cache/go-build go test ./...
cd ../frontend
bun test src/routes/providers/provider-page.test.mjs
bun run check
bun run build
```

Expected: all commands exit 0. If sandbox blocks `httptest` sockets, rerun backend tests with escalated permissions.

- [ ] **Step 4: Commit**

```bash
git add README.md deploy/README.md backend/internal/gateway/documentation_test.go
git commit -m "docs: document provider account bulk controls"
```

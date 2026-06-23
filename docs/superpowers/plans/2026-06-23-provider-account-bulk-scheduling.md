# Provider Account Bulk Scheduling Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add selected-account bulk priority and load-factor updates to Provider accounts.

**Architecture:** Extend the existing `/api/admin/provider-accounts/bulk-update` endpoint because enabled, priority, and load factor are all account scheduling fields. Reuse the current selection state and bulk action bar in the Svelte Provider accounts page.

**Tech Stack:** Go HTTP API, provider service interface, SvelteKit admin UI, Bun tests.

---

## File Structure

- `backend/internal/httpapi/server.go`: extend the bulk update request and handler.
- `backend/internal/httpapi/server_test.go`: cover bulk priority/load-factor updates and validation.
- `frontend/src/lib/admin-state.svelte.js`: add bulk scheduling form state and `bulkUpdateSelectedProviderAccountScheduling`.
- `frontend/src/routes/providers/+page.svelte`: add priority/load-factor inputs and **Apply scheduling** button.
- `frontend/src/routes/providers/provider-page.test.mjs`: source-level tests for the state and UI wiring.
- `README.md`, `deploy/README.md`, `backend/internal/gateway/documentation_test.go`: document bulk scheduling parameters.

## Task 1: Backend Bulk Scheduling Fields

**Files:**
- Modify: `backend/internal/httpapi/server.go`
- Modify: `backend/internal/httpapi/server_test.go`

- [ ] **Step 1: Write failing success test**

Add `TestAdminCanBulkUpdateUnifiedProviderAccountScheduling` in `backend/internal/httpapi/server_test.go`. It should POST `{"accountIds":[7,8,7],"priority":2,"loadFactor":5}` to `/api/admin/provider-accounts/bulk-update`, assert HTTP 200, assert fake provider update IDs are `[7, 8]`, and assert every `provider.AccountUpdate` has `Priority=2` and `LoadFactor=5`.

- [ ] **Step 2: Run red test**

Run:

```bash
cd backend
GOMODCACHE=/root/Clouds/N2API/.cache/go-mod GOCACHE=/root/Clouds/N2API/.cache/go-build go test ./internal/httpapi -run 'TestAdminCanBulkUpdateUnifiedProviderAccountScheduling'
```

Expected: FAIL with HTTP 400 because bulk update currently requires `enabled`.

- [ ] **Step 3: Implement handler support**

Change the bulk update request to include `priority` and `loadFactor`. Reject requests where all three fields are absent. Pass the same `provider.AccountUpdate{Enabled, Priority, LoadFactor}` to each selected account.

- [ ] **Step 4: Extend validation tests**

Update `TestAdminBulkProviderAccountUpdateValidatesInput` so `{"accountIds":[7]}` remains invalid because no scheduling fields are present. Add invalid priority/load-factor cases if provider service validation is not already reached.

- [ ] **Step 5: Run targeted backend tests**

Run:

```bash
cd backend
GOMODCACHE=/root/Clouds/N2API/.cache/go-mod GOCACHE=/root/Clouds/N2API/.cache/go-build go test ./internal/httpapi -run 'BulkProviderAccountUpdate'
```

Expected: PASS.

- [ ] **Step 6: Commit**

```bash
git add backend/internal/httpapi/server.go backend/internal/httpapi/server_test.go
git commit -m "feat: add provider account bulk scheduling api"
```

## Task 2: Frontend Bulk Scheduling Controls

**Files:**
- Modify: `frontend/src/lib/admin-state.svelte.js`
- Modify: `frontend/src/routes/providers/+page.svelte`
- Modify: `frontend/src/routes/providers/provider-page.test.mjs`

- [ ] **Step 1: Write failing frontend tests**

Extend `provider-page.test.mjs` to assert `providerAccountBulkSchedulingForm`, `bulkUpdateSelectedProviderAccountScheduling`, **Apply scheduling**, **Bulk priority**, and **Bulk load factor** appear in source.

- [ ] **Step 2: Run red frontend test**

Run:

```bash
cd frontend
bun test src/routes/providers/provider-page.test.mjs
```

Expected: FAIL because the form state and UI do not exist.

- [ ] **Step 3: Implement state**

Add `providerAccountBulkSchedulingForm = $state({ priority: '', loadFactor: '' })` and `bulkUpdateSelectedProviderAccountScheduling()` that validates selected IDs and optional numeric fields, sends `accountIds` plus provided fields to `/bulk-update`, clears selection and form on success, and reloads provider accounts plus model routing.

- [ ] **Step 4: Implement UI**

Add compact inputs and **Apply scheduling** to the existing selected-account action bar. Disable the button when no accounts are selected or saving.

- [ ] **Step 5: Run frontend tests**

Run:

```bash
cd frontend
bun test src/routes/providers/provider-page.test.mjs
```

Expected: PASS.

- [ ] **Step 6: Commit**

```bash
git add frontend/src/lib/admin-state.svelte.js frontend/src/routes/providers/+page.svelte frontend/src/routes/providers/provider-page.test.mjs
git commit -m "feat: add provider account bulk scheduling controls"
```

## Task 3: Documentation And Verification

**Files:**
- Modify: `README.md`
- Modify: `deploy/README.md`
- Modify: `backend/internal/gateway/documentation_test.go`

- [ ] **Step 1: Add documentation test**

Add a documentation test requiring README and deploy notes to mention **Apply scheduling**, bulk priority, and bulk load factor.

- [ ] **Step 2: Update docs**

Document that selected provider accounts can receive shared scheduling parameters from the Provider accounts page.

- [ ] **Step 3: Run documentation test**

Run:

```bash
cd backend
GOMODCACHE=/root/Clouds/N2API/.cache/go-mod GOCACHE=/root/Clouds/N2API/.cache/go-build go test ./internal/gateway -run 'BulkScheduling'
```

Expected: PASS.

- [ ] **Step 4: Run full verification**

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

Expected: all commands exit 0. If sandbox blocks `httptest` sockets, rerun backend tests with escalation and record the reason.

- [ ] **Step 5: Commit**

```bash
git add README.md deploy/README.md backend/internal/gateway/documentation_test.go
git commit -m "docs: document provider account bulk scheduling"
```

# Provider Account Selected Test Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add a selected-account bulk test action for provider accounts.

**Architecture:** Reuse the existing provider `TestAccount` behavior and test-history recording. Add a narrow admin endpoint that validates and deduplicates selected IDs, then expose it through the existing Svelte provider-account selection state.

**Tech Stack:** Go HTTP API, provider service interface, SvelteKit admin UI, Bun frontend tests.

---

## File Structure

- `backend/internal/httpapi/server.go`: add the selected bulk-test route and handler.
- `backend/internal/httpapi/server_test.go`: cover successful selected tests and invalid input.
- `frontend/src/lib/admin-state.svelte.js`: add `testSelectedProviderAccounts`.
- `frontend/src/routes/providers/+page.svelte`: add **Test selected** beside existing selected-account controls.
- `frontend/src/routes/providers/provider-page.test.mjs`: add source-level tests for state and UI wiring.
- `README.md`, `deploy/README.md`, `backend/internal/gateway/documentation_test.go`: document the selected-test workflow.

## Task 1: Backend Selected Test API

**Files:**
- Modify: `backend/internal/httpapi/server.go`
- Modify: `backend/internal/httpapi/server_test.go`

- [ ] **Step 1: Write the failing success test**

Add `TestAdminCanTestSelectedUnifiedProviderAccounts` in `backend/internal/httpapi/server_test.go`. The test should create a fake provider with two accounts, call `POST /api/admin/provider-accounts/bulk-test` with `{"accountIds":[7,8,7]}`, assert HTTP 200, assert the fake saw test IDs `[7, 8]`, and assert the JSON response includes two accounts.

- [ ] **Step 2: Run the targeted red test**

Run:

```bash
cd backend
GOMODCACHE=/root/Clouds/N2API/.cache/go-mod GOCACHE=/root/Clouds/N2API/.cache/go-build go test ./internal/httpapi -run 'TestAdminCanTestSelectedUnifiedProviderAccounts'
```

Expected: FAIL with 404 because the route is not registered.

- [ ] **Step 3: Implement the handler**

Add `POST /api/admin/provider-accounts/bulk-test` behind admin auth. Decode `accountIds`, reject invalid input, deduplicate IDs, sequentially call `providers.TestAccount`, and return `{"accounts": accounts}`.

- [ ] **Step 4: Add validation tests**

Add `TestAdminBulkProviderAccountTestValidatesInput` with empty list, ID `0`, and more than 100 IDs. Expected response is HTTP 400 and no test calls.

- [ ] **Step 5: Run targeted backend tests**

Run:

```bash
cd backend
GOMODCACHE=/root/Clouds/N2API/.cache/go-mod GOCACHE=/root/Clouds/N2API/.cache/go-build go test ./internal/httpapi -run 'BulkProviderAccount(Test|Update)'
```

Expected: PASS.

- [ ] **Step 6: Commit**

```bash
git add backend/internal/httpapi/server.go backend/internal/httpapi/server_test.go
git commit -m "feat: add selected provider account test api"
```

## Task 2: Frontend Selected Test Control

**Files:**
- Modify: `frontend/src/lib/admin-state.svelte.js`
- Modify: `frontend/src/routes/providers/+page.svelte`
- Modify: `frontend/src/routes/providers/provider-page.test.mjs`

- [ ] **Step 1: Write failing frontend tests**

Extend `provider-page.test.mjs` to assert `testSelectedProviderAccounts`, `/api/admin/provider-accounts/bulk-test`, and **Test selected** appear in source.

- [ ] **Step 2: Run red frontend test**

Run:

```bash
cd frontend
bun test src/routes/providers/provider-page.test.mjs
```

Expected: FAIL because the function and button do not exist.

- [ ] **Step 3: Implement state function**

Add `testSelectedProviderAccounts()` that builds numeric IDs from `selectedProviderAccountIds`, validates that at least one ID is selected, posts to `/api/admin/provider-accounts/bulk-test`, clears selection on success, reloads provider accounts and model routing, and calls `refreshExpandedAccountTestResults()`.

- [ ] **Step 4: Implement UI button**

Import `testSelectedProviderAccounts` in the providers page and add **Test selected** beside **Enable selected**. Disable it when no account is selected or a provider-account save/test is in progress.

- [ ] **Step 5: Run frontend test**

Run:

```bash
cd frontend
bun test src/routes/providers/provider-page.test.mjs
```

Expected: PASS.

- [ ] **Step 6: Commit**

```bash
git add frontend/src/lib/admin-state.svelte.js frontend/src/routes/providers/+page.svelte frontend/src/routes/providers/provider-page.test.mjs
git commit -m "feat: add selected provider account test control"
```

## Task 3: Documentation And Verification

**Files:**
- Modify: `README.md`
- Modify: `deploy/README.md`
- Modify: `backend/internal/gateway/documentation_test.go`

- [ ] **Step 1: Write documentation test**

Add a documentation test requiring README and deploy notes to mention `Test selected`.

- [ ] **Step 2: Update docs**

Document that selected rows can be tested without probing the whole account pool.

- [ ] **Step 3: Run documentation test**

Run:

```bash
cd backend
GOMODCACHE=/root/Clouds/N2API/.cache/go-mod GOCACHE=/root/Clouds/N2API/.cache/go-build go test ./internal/gateway -run 'SelectedProviderAccount'
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
git commit -m "docs: document selected provider account testing"
```

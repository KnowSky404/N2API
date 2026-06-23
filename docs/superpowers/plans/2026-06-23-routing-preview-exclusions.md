# Routing Preview Exclusions Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Let admins preview scheduler fallback behavior by excluding provider account IDs in Routing diagnostics.

**Architecture:** Use the existing provider preview path and exclusion-aware scheduler. Extend only the HTTP query parser and Svelte admin state/UI so the existing `PreviewAccountSelection(...excludedAccountIDs)` capability becomes operationally visible.

**Tech Stack:** Go HTTP API, provider service interfaces, SvelteKit admin UI, Bun frontend tests.

---

## File Structure

- `backend/internal/httpapi/server.go`: parse `excludedAccountIds` query and pass IDs to provider preview.
- `backend/internal/httpapi/server_test.go`: prove valid exclusions are passed and invalid exclusions are rejected.
- `frontend/src/lib/admin-state.svelte.js`: store exclusion text and send query parameter.
- `frontend/src/routes/models/+page.svelte`: add the input and response summary text.
- `frontend/src/routes/navigation.test.mjs`: source-level frontend regression coverage.
- `README.md`, `deploy/README.md`, `backend/internal/gateway/documentation_test.go`: document the diagnostic control.

## Task 1: HTTP Preview Exclusions

**Files:**
- Modify: `backend/internal/httpapi/server.go`
- Modify: `backend/internal/httpapi/server_test.go`

- [ ] **Step 1: Write failing HTTP tests**

Add one test to `server_test.go` that calls `/api/admin/model-routing/preview?model=gpt-5&sessionId=workspace-123&excludedAccountIds=7,8` and asserts fake provider received exclusions `[7,8]`. Add one test that calls `excludedAccountIds=abc` and expects `400 bad_request`.

- [ ] **Step 2: Run failing tests**

Run:

```bash
cd backend
GOMODCACHE=/root/Clouds/N2API/.cache/go-mod GOCACHE=/root/Clouds/N2API/.cache/go-build go test ./internal/httpapi -run 'ModelRoutingPreview'
```

Expected: FAIL because HTTP does not parse or pass exclusions yet.

- [ ] **Step 3: Implement parser and wiring**

Add a small parser near `handleModelRoutingPreview`:

- empty string returns nil
- split on comma
- trim whitespace
- skip empty segments
- `strconv.ParseInt(segment, 10, 64)`
- reject `<= 0`

Call `providers.PreviewAccountSelection(r.Context(), model, r.URL.Query().Get("sessionId"), excludedIDs...)`.

- [ ] **Step 4: Run HTTP tests**

Run:

```bash
cd backend
GOMODCACHE=/root/Clouds/N2API/.cache/go-mod GOCACHE=/root/Clouds/N2API/.cache/go-build go test ./internal/httpapi -run 'ModelRoutingPreview'
```

Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add backend/internal/httpapi/server.go backend/internal/httpapi/server_test.go
git commit -m "feat: preview routing with excluded accounts"
```

## Task 2: Frontend Routing Diagnostics Input

**Files:**
- Modify: `frontend/src/lib/admin-state.svelte.js`
- Modify: `frontend/src/routes/models/+page.svelte`
- Modify: `frontend/src/routes/navigation.test.mjs`

- [ ] **Step 1: Write failing frontend source tests**

Add assertions that Routing diagnostics includes `Excluded account IDs`, binds `modelRoutingPreview.excludedAccountIds`, and `loadModelRoutingPreview` sends `excludedAccountIds`.

- [ ] **Step 2: Run failing frontend test**

Run:

```bash
cd frontend
bun test src/routes/navigation.test.mjs
```

Expected: FAIL because the field and query parameter do not exist.

- [ ] **Step 3: Implement admin state and UI**

Add `excludedAccountIds: ''` to `modelRoutingPreview`. In `loadModelRoutingPreview`, trim it and set `params.set('excludedAccountIds', excludedAccountIds)` when non-empty. Add a text input to the Selection preview form with label `Excluded account IDs` and placeholder `7, 8`. Show `excluding {modelRoutingPreview.excludedAccountIds}` in the result summary when non-empty.

- [ ] **Step 4: Run frontend test**

Run:

```bash
cd frontend
bun test src/routes/navigation.test.mjs
```

Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add frontend/src/lib/admin-state.svelte.js frontend/src/routes/models/+page.svelte frontend/src/routes/navigation.test.mjs
git commit -m "feat: add routing preview exclusions UI"
```

## Task 3: Documentation And Verification

**Files:**
- Modify: `README.md`
- Modify: `deploy/README.md`
- Modify: `backend/internal/gateway/documentation_test.go`

- [ ] **Step 1: Add failing documentation test**

Extend gateway documentation test coverage so README and deploy README must mention `Excluded account IDs` and `account excluded`.

- [ ] **Step 2: Update docs**

In README and deploy README, describe that Routing diagnostics can preview fallback by entering excluded provider account IDs, and that excluded accounts appear as blocked candidates with `account excluded`.

- [ ] **Step 3: Run documentation test**

Run:

```bash
cd backend
GOMODCACHE=/root/Clouds/N2API/.cache/go-mod GOCACHE=/root/Clouds/N2API/.cache/go-build go test ./internal/gateway -run 'Documentation'
```

Expected: PASS.

- [ ] **Step 4: Run full verification**

Run:

```bash
git diff --check
cd backend
GOMODCACHE=/root/Clouds/N2API/.cache/go-mod GOCACHE=/root/Clouds/N2API/.cache/go-build go test ./...
cd ../frontend
bun test src/routes/navigation.test.mjs
bun run check
bun run build
```

Expected: all commands exit 0. If sandbox blocks `httptest` sockets, rerun backend tests with the approved escalation path and record it.

- [ ] **Step 5: Commit**

```bash
git add README.md deploy/README.md backend/internal/gateway/documentation_test.go
git commit -m "docs: document routing preview exclusions"
```

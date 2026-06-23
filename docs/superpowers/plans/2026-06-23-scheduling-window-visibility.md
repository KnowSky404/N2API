# Provider Account Scheduling Window Visibility Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Show how long a provider account remains rate-limited or circuit-open from the Provider accounts table.

**Architecture:** Add one pure frontend formatter in `admin-state.svelte.js`, use it in the Provider accounts status column, and document the visible remaining window. No backend changes are required because the API already returns `rateLimitedUntil` and `circuitOpenUntil`.

**Tech Stack:** SvelteKit admin UI, Bun tests, Go documentation tests.

---

## File Structure

- `frontend/src/lib/admin-state.svelte.js`: add `futureTimeRemainingLabel`.
- `frontend/src/routes/providers/+page.svelte`: import and use the helper in the account status cell.
- `frontend/src/routes/providers/provider-page.test.mjs`: test the helper and source usage.
- `README.md`, `deploy/README.md`, `backend/internal/gateway/documentation_test.go`: document and guard the visible remaining scheduling window.

## Task 1: Remaining Window Formatter

**Files:**
- Modify: `frontend/src/lib/admin-state.svelte.js`
- Modify: `frontend/src/routes/providers/provider-page.test.mjs`

- [ ] **Step 1: Write failing formatter tests**

Add assertions:

```js
assert.equal(futureTimeRemainingLabel('2026-06-23T00:05:00Z', new Date('2026-06-23T00:00:00Z')), '5m remaining');
assert.equal(futureTimeRemainingLabel('2026-06-23T02:15:00Z', new Date('2026-06-23T00:00:00Z')), '2h 15m remaining');
assert.equal(futureTimeRemainingLabel('2026-06-24T03:00:00Z', new Date('2026-06-23T00:00:00Z')), '1d 3h remaining');
assert.equal(futureTimeRemainingLabel('2026-06-22T23:59:00Z', new Date('2026-06-23T00:00:00Z')), '');
```

- [ ] **Step 2: Run tests and verify red**

Run:

```bash
cd frontend
bun test src/routes/providers/provider-page.test.mjs
```

Expected: FAIL because `futureTimeRemainingLabel` does not exist.

- [ ] **Step 3: Implement formatter**

Add `futureTimeRemainingLabel(value, now = new Date())`. Parse `value`, return empty string for invalid/past values, and format minutes/hours/days with `remaining`.

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
git commit -m "feat: format provider scheduling windows"
```

## Task 2: Provider Page Status Display

**Files:**
- Modify: `frontend/src/routes/providers/+page.svelte`
- Modify: `frontend/src/routes/providers/provider-page.test.mjs`

- [ ] **Step 1: Write failing source test**

Assert the provider page imports `futureTimeRemainingLabel` and renders it for `account.rateLimitedUntil` and `account.circuitOpenUntil`.

- [ ] **Step 2: Run tests and verify red**

Run:

```bash
cd frontend
bun test src/routes/providers/provider-page.test.mjs
```

Expected: FAIL because the page does not use the helper.

- [ ] **Step 3: Update status cell**

Use `futureTimeRemainingLabel(account.rateLimitedUntil)` and `futureTimeRemainingLabel(account.circuitOpenUntil)` in the status subtext, keeping `formatDate(...)` in the hover detail.

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
git commit -m "feat: show provider scheduling window remaining"
```

## Task 3: Documentation And Verification

**Files:**
- Modify: `README.md`
- Modify: `deploy/README.md`
- Modify: `backend/internal/gateway/documentation_test.go`

- [ ] **Step 1: Write failing documentation test**

Require `remaining scheduling block` in `TestGatewayDocumentationMentionsProviderAccountSchedulingPause`.

- [ ] **Step 2: Update docs**

Mention that rate-limited and paused account rows show the remaining scheduling block.

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

Expected: all commands exit 0. If `httptest` socket creation is blocked by sandboxing, rerun backend tests with escalated permissions.

- [ ] **Step 4: Commit**

```bash
git add README.md deploy/README.md backend/internal/gateway/documentation_test.go
git commit -m "docs: document provider scheduling window visibility"
```

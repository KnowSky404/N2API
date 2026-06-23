# Configurable Provider Account Scheduling Pause Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Make manual provider account scheduling pauses configurable from the Provider accounts page.

**Architecture:** Reuse the existing pause endpoint and backend status model. Add a small frontend state value, validate it before calling `pause`, and document that the pause duration is controlled from the admin UI.

**Tech Stack:** SvelteKit admin UI, Bun tests, Go HTTP API documentation tests.

---

## File Structure

- `frontend/src/lib/admin-state.svelte.js`: add `providerAccountPauseForm`, validate duration, and send it in the existing pause request.
- `frontend/src/routes/providers/+page.svelte`: add the pause duration input near scheduling capacity controls.
- `frontend/src/routes/providers/provider-page.test.mjs`: source and state tests for the control, validation, and payload.
- `README.md`, `deploy/README.md`, `backend/internal/gateway/documentation_test.go`: document configurable pause duration.

## Task 1: Frontend Pause Duration State

**Files:**
- Modify: `frontend/src/lib/admin-state.svelte.js`
- Modify: `frontend/src/routes/providers/provider-page.test.mjs`

- [ ] **Step 1: Write failing tests**

Add tests that assert `providerAccountPauseForm.durationSeconds`, `validateProviderAccountPauseDuration`, `durationSeconds`, and the validation message `Pause duration must be a whole number between 60 and 86400 seconds` exist.

- [ ] **Step 2: Run tests and verify red**

Run:

```bash
cd frontend
bun test src/routes/providers/provider-page.test.mjs
```

Expected: FAIL because pause duration state and validation do not exist.

- [ ] **Step 3: Implement state and validation**

Add:

```js
export const providerAccountPauseForm = $state({ durationSeconds: 300 });

export function validateProviderAccountPauseDuration() {
  const durationSeconds = Number(providerAccountPauseForm.durationSeconds);
  if (!Number.isInteger(durationSeconds) || durationSeconds < 60 || durationSeconds > 86400) {
    providerAccounts.error = 'Pause duration must be a whole number between 60 and 86400 seconds';
    return null;
  }
  return durationSeconds;
}
```

Update `pauseProviderAccount` to call the validator and send `JSON.stringify({ durationSeconds })`.

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
git commit -m "feat: validate provider pause duration"
```

## Task 2: Provider Page Control

**Files:**
- Modify: `frontend/src/routes/providers/+page.svelte`
- Modify: `frontend/src/routes/providers/provider-page.test.mjs`

- [ ] **Step 1: Write failing source test**

Add assertions for `Pause duration seconds`, `providerAccountPauseForm.durationSeconds`, and `min="60"`.

- [ ] **Step 2: Run tests and verify red**

Run:

```bash
cd frontend
bun test src/routes/providers/provider-page.test.mjs
```

Expected: FAIL because the control is not rendered.

- [ ] **Step 3: Add UI control**

Import `providerAccountPauseForm` and add a compact number input in the Provider accounts scheduling controls area:

```svelte
<label>
  Pause duration seconds
  <input type="number" min="60" max="86400" bind:value={providerAccountPauseForm.durationSeconds} />
</label>
```

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
git commit -m "feat: add provider pause duration control"
```

## Task 3: Documentation And Verification

**Files:**
- Modify: `README.md`
- Modify: `deploy/README.md`
- Modify: `backend/internal/gateway/documentation_test.go`

- [ ] **Step 1: Write failing documentation test**

Extend `TestGatewayDocumentationMentionsProviderAccountSchedulingPause` to require `Pause duration seconds`.

- [ ] **Step 2: Run test and verify red**

Run:

```bash
cd backend
GOMODCACHE=/root/Clouds/N2API/.cache/go-mod GOCACHE=/root/Clouds/N2API/.cache/go-build go test ./internal/gateway -run TestGatewayDocumentationMentionsProviderAccountSchedulingPause
```

Expected: FAIL until docs mention the UI control.

- [ ] **Step 3: Update docs**

Update README and deploy README to say the Provider accounts page controls the manual pause duration and that reset clears it early.

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

Expected: all commands exit 0.

- [ ] **Step 5: Commit**

```bash
git add README.md deploy/README.md backend/internal/gateway/documentation_test.go
git commit -m "docs: document provider pause duration"
```

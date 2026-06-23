# Gateway Readiness Self Loading Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Make `/gateway` load provider accounts, model routing, and API keys itself so readiness is accurate when the page is opened directly.

**Architecture:** Reuse existing frontend state loaders from `admin-state.svelte.js` and call them from the Gateway page authenticated-load effect. No backend API or schema changes are needed because the required data already exists.

**Tech Stack:** SvelteKit admin UI, Bun tests, Go documentation tests.

---

## File Structure

- Modify `frontend/src/routes/navigation.test.mjs`: add source assertions for Gateway page readiness data loading.
- Modify `frontend/src/routes/gateway/+page.svelte`: import and call the existing loaders.
- Modify `backend/internal/gateway/documentation_test.go`: document self-loading readiness behavior.
- Modify `README.md` and `deploy/README.md`: describe Gateway management readiness refresh behavior.

### Task 1: Gateway Page Readiness Data Loading

**Files:**
- Modify: `frontend/src/routes/navigation.test.mjs`
- Modify: `frontend/src/routes/gateway/+page.svelte`

- [ ] **Step 1: Write the failing source test**

Add assertions to the existing Gateway page test:

```js
assert.match(gatewayPage, /loadProviderAccounts/);
assert.match(gatewayPage, /loadModelRouting/);
assert.match(gatewayPage, /loadAPIKeys/);
```

- [ ] **Step 2: Run the frontend test to verify red**

Run:

```bash
cd frontend && bun test src/routes/navigation.test.mjs
```

Expected: FAIL because the Gateway page does not import or call all readiness data loaders.

- [ ] **Step 3: Implement the page load**

In `frontend/src/routes/gateway/+page.svelte`, import:

```js
loadAPIKeys,
loadModelRouting,
loadProviderAccounts,
```

Inside the authenticated `gatewayRequested` effect, add:

```js
void loadProviderAccounts();
void loadModelRouting();
void loadAPIKeys();
```

- [ ] **Step 4: Run the frontend test to verify green**

Run:

```bash
cd frontend && bun test src/routes/navigation.test.mjs
```

Expected: PASS.

- [ ] **Step 5: Commit frontend change**

```bash
git add frontend/src/routes/navigation.test.mjs frontend/src/routes/gateway/+page.svelte
git commit -m "fix: load gateway readiness inputs"
```

### Task 2: Documentation

**Files:**
- Modify: `backend/internal/gateway/documentation_test.go`
- Modify: `README.md`
- Modify: `deploy/README.md`

- [ ] **Step 1: Write the failing documentation test**

Add a documentation test requiring README and deploy README to mention:

```go
"Gateway management refreshes provider accounts, model routing, and API keys"
```

- [ ] **Step 2: Run the documentation test to verify red**

Run:

```bash
cd backend && GOMODCACHE=/root/Clouds/N2API/.cache/go-mod GOCACHE=/root/Clouds/N2API/.cache/go-build go test ./internal/gateway -run TestGatewayDocumentationMentionsReadinessRefresh
```

Expected: FAIL because the docs do not mention this readiness behavior yet.

- [ ] **Step 3: Update docs**

Add the sentence to the Gateway runtime limits/readiness sections in `README.md` and `deploy/README.md`:

```md
Gateway management refreshes provider accounts, model routing, and API keys before reporting readiness, so the counts and prerequisite warnings are valid even when `/gateway` is opened directly.
```

- [ ] **Step 4: Run the documentation test to verify green**

Run the same `go test ./internal/gateway -run TestGatewayDocumentationMentionsReadinessRefresh` command.

Expected: PASS.

- [ ] **Step 5: Commit docs**

```bash
git add backend/internal/gateway/documentation_test.go README.md deploy/README.md
git commit -m "docs: document gateway readiness refresh"
```

### Task 3: Verification

**Files:**
- No code changes unless verification reveals a defect.

- [ ] **Step 1: Run frontend checks**

```bash
cd frontend && bun test src/routes/navigation.test.mjs
cd frontend && bun run check
cd frontend && bun run build
```

- [ ] **Step 2: Run backend checks**

```bash
cd backend && GOMODCACHE=/root/Clouds/N2API/.cache/go-mod GOCACHE=/root/Clouds/N2API/.cache/go-build go test ./...
```

- [ ] **Step 3: Check worktree**

```bash
git status --short
```

Expected: clean after commits.

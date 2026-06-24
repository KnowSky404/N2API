# Provider Account Log Deeplink Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add a direct Provider account row action that opens Request Logs filtered for that account, and make Request Logs initialize filters from URL params.

**Architecture:** Keep this frontend-only. Providers page emits a normal anchor to `/request-logs?providerAccountId=${account.id}`. Request Logs page owns one authenticated initialization effect that applies query params to shared `requestLogs` state, loads provider accounts when needed, and loads request logs.

**Tech Stack:** SvelteKit admin UI, shared Svelte state module, Bun source tests and Svelte checks.

---

### Task 1: Plan And Scope

**Files:**
- Create: `docs/superpowers/specs/2026-06-24-provider-account-log-deeplink-design.md`
- Create: `docs/superpowers/plans/2026-06-24-provider-account-log-deeplink.md`

- [ ] **Step 1: Write design and plan**

Document the provider row link, URL initialization rules, and verification commands.

- [ ] **Step 2: Commit docs**

Run:

```bash
git add docs/superpowers/specs/2026-06-24-provider-account-log-deeplink-design.md docs/superpowers/plans/2026-06-24-provider-account-log-deeplink.md
git commit -m "docs: plan provider account log deeplink"
```

Expected: commit contains only the two docs.

### Task 2: Frontend Red Tests

**Files:**
- Modify: `frontend/src/routes/navigation.test.mjs`
- Modify: `frontend/src/routes/providers/provider-page.test.mjs`

- [ ] **Step 1: Add source tests**

In `navigation.test.mjs`, add assertions that `request-logs/+page.svelte` contains:

```js
assert.match(requestLogsPage, /URLSearchParams\(window\.location\.search\)/);
assert.match(requestLogsPage, /requestLogs\.providerAccountId = providerAccountId/);
assert.match(requestLogsPage, /void loadRequestLogs\(\)/);
```

In `providers/provider-page.test.mjs`, add assertions that providers page contains:

```js
assert.match(source, /href=\{`\/request-logs\?providerAccountId=\$\{account\.id\}`\}/);
assert.match(source, /View request logs/);
```

- [ ] **Step 2: Run failing frontend tests**

Run:

```bash
cd frontend && bun test src/routes/navigation.test.mjs src/routes/providers/provider-page.test.mjs
```

Expected: FAIL until URL initialization and provider row link exist.

### Task 3: Implement Deeplink

**Files:**
- Modify: `frontend/src/routes/request-logs/+page.svelte`
- Modify: `frontend/src/routes/providers/+page.svelte`

- [ ] **Step 1: Add Request Logs URL initialization**

Add local state:

```js
let requestLogsInitialized = $state(false);
```

Add helper:

```js
function applyRequestLogURLFilters() {
  const params = new URLSearchParams(window.location.search);
  const providerAccountId = params.get('providerAccountId') ?? '';
  if (/^[1-9]\d*$/.test(providerAccountId)) {
    requestLogs.providerAccountId = providerAccountId;
  }
  const query = params.get('q');
  if (query !== null) requestLogs.query = query;
  const statusClass = params.get('statusClass');
  if (['all', 'success', 'client_error', 'server_error'].includes(statusClass ?? '')) {
    requestLogs.statusClass = statusClass;
  }
}
```

Update the authenticated effect to reset initialization on logout, apply URL filters once, load provider accounts when needed, and call `void loadRequestLogs()`.

- [ ] **Step 2: Add provider row link**

In the Provider account actions cell, add a compact anchor before the test button:

```svelte
<a
  class="inline-flex size-8 items-center justify-center rounded-md border border-[#e5e5e5] bg-white text-sm font-semibold text-[#0d0d0d] hover:bg-[#f5f5f5]"
  href={`/request-logs?providerAccountId=${account.id}`}
  title="View request logs"
  aria-label="View request logs"
>
  <span aria-hidden="true">L</span>
  <span class="sr-only">View request logs</span>
</a>
```

- [ ] **Step 3: Run frontend source tests**

Run:

```bash
cd frontend && bun test src/routes/navigation.test.mjs src/routes/providers/provider-page.test.mjs
```

Expected: PASS.

### Task 4: Final Verification And Commit

**Files:**
- Verify all touched files.

- [ ] **Step 1: Run frontend checks**

```bash
cd frontend && bun test src/routes/navigation.test.mjs src/routes/providers/provider-page.test.mjs
cd frontend && bun run check
cd frontend && bun run build
```

Expected: PASS.

- [ ] **Step 2: Run backend regression**

```bash
cd backend && GOMODCACHE=/root/Clouds/N2API/.cache/go-mod GOCACHE=/root/Clouds/N2API/.cache/go-build go test ./...
```

Expected: PASS.

- [ ] **Step 3: Run diff check**

```bash
git diff --check
```

Expected: no output.

- [ ] **Step 4: Commit implementation**

Run:

```bash
git add frontend/src/routes/request-logs/+page.svelte frontend/src/routes/providers/+page.svelte frontend/src/routes/navigation.test.mjs frontend/src/routes/providers/provider-page.test.mjs
git commit -m "feat: link provider accounts to logs"
```

Expected: commit succeeds and worktree is clean.

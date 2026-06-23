# Provider Account Test History UI Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Show recent provider account test history directly in the Provider accounts admin page.

**Architecture:** Add account-scoped frontend state beside existing account model state, backed by the existing `GET /api/admin/provider-accounts/{id}/test-results?limit=20` API. The Provider accounts table gets an accessible row-level History action that lazily opens an expanded row and renders loading, error, empty, and result states without fetching every account history on page load.

**Tech Stack:** SvelteKit admin UI, Bun test runner, existing admin-state module, Tailwind CSS utility classes.

---

## File Structure

- `frontend/src/lib/admin-state.svelte.js`: add `ProviderAccountTestResult` typedef, `accountTestResults` state map, helpers, and lazy loading functions.
- `frontend/src/routes/providers/+page.svelte`: add History action button and expanded row UI.
- `frontend/src/routes/providers/provider-page.test.mjs`: add source/unit coverage for endpoint usage, state pruning, action markup, and rendered-state branches.

## Task 1: Frontend State For Account Test History

**Files:**
- Modify: `frontend/src/lib/admin-state.svelte.js`
- Modify: `frontend/src/routes/providers/provider-page.test.mjs`

- [ ] **Step 1: Write failing state tests**

Add imports to `frontend/src/routes/providers/provider-page.test.mjs`:

```js
const {
  accountTestResults,
  getAccountTestResultsState,
  pruneAccountTestResultStates,
  shouldApplyAccountTestResultsResponse,
  // existing imports stay in place
} = await import('../../lib/admin-state.svelte.js');
```

Add these tests:

```js
test('provider account state uses unified test-results endpoint', () => {
  const adminStateSource = readFileSync('src/lib/admin-state.svelte.js', 'utf8');

  assert.match(adminStateSource, /\/api\/admin\/provider-accounts\/\$\{accountId\}\/test-results\?limit=20/);
  assert.doesNotMatch(adminStateSource, /\/api\/admin\/providers\/openai\/accounts\/\$\{accountId\}\/test-results/);
});

test('account test result state initializes and rejects stale responses', () => {
  const state = getAccountTestResultsState(7);

  assert.equal(state.expanded, false);
  assert.equal(state.loading, false);
  assert.equal(state.error, '');
  assert.deepEqual(state.items, []);
  assert.equal(shouldApplyAccountTestResultsResponse({ requestSeq: 3 }, 3), true);
  assert.equal(shouldApplyAccountTestResultsResponse({ requestSeq: 4 }, 3), false);
});

test('pruneAccountTestResultStates removes state for missing accounts', () => {
  accountTestResults[7] = { requestSeq: 1 };
  accountTestResults[8] = { requestSeq: 1 };
  accountTestResults[12] = { requestSeq: 1 };

  pruneAccountTestResultStates(accountTestResults, [8, 12]);

  assert.deepEqual(Object.keys(accountTestResults), ['8', '12']);
});
```

- [ ] **Step 2: Run failing state tests**

Run:

```bash
cd frontend
bun test src/routes/providers/provider-page.test.mjs
```

Expected: FAIL because `accountTestResults`, `getAccountTestResultsState`, `pruneAccountTestResultStates`, and `shouldApplyAccountTestResultsResponse` do not exist.

- [ ] **Step 3: Implement state helpers**

In `frontend/src/lib/admin-state.svelte.js`, add:

```js
/**
 * @typedef {object} ProviderAccountTestResult
 * @property {number} id
 * @property {number} accountId
 * @property {string} provider
 * @property {string} status
 * @property {string} message
 * @property {string} checkedAt
 * @property {string} createdAt
 */

/**
 * @typedef {object} AccountTestResultsState
 * @property {boolean} expanded
 * @property {boolean} loading
 * @property {string} error
 * @property {ProviderAccountTestResult[]} items
 * @property {number} requestSeq
 */
```

Add state:

```js
/** @type {Record<string, AccountTestResultsState>} */
export const accountTestResults = $state({});
```

Add helpers near account model helpers:

```js
/** @param {number} accountId */
function ensureAccountTestResultsState(accountId) {
  const key = String(accountId);
  if (!accountTestResults[key]) {
    accountTestResults[key] = {
      expanded: false,
      loading: false,
      error: '',
      items: [],
      requestSeq: 0
    };
  }
  return accountTestResults[key];
}

/** @param {number} accountId */
export function getAccountTestResultsState(accountId) {
  return ensureAccountTestResultsState(accountId);
}

/**
 * @param {Record<string, unknown>} states
 * @param {number[]} accountIDs
 */
export function pruneAccountTestResultStates(states, accountIDs) {
  const keep = new Set(accountIDs.map((id) => String(id)));
  for (const key of Object.keys(states)) {
    if (!keep.has(key)) delete states[key];
  }
}

/**
 * @param {{ requestSeq: number }} state
 * @param {number} requestSeq
 */
export function shouldApplyAccountTestResultsResponse(state, requestSeq) {
  return state.requestSeq === requestSeq;
}
```

Update `loadProviderAccounts()` to prune both maps:

```js
pruneAccountTestResultStates(
  accountTestResults,
  providerAccounts.items.map((account) => account.id)
);
```

- [ ] **Step 4: Implement lazy loader and refresh helpers**

Add:

```js
/**
 * @param {number} accountId
 * @param {{ expand?: boolean }} options
 */
export async function loadAccountTestResults(accountId, options = {}) {
  const version = sessionVersion;
  if (!isCurrentAuthenticated(version)) return;
  const state = ensureAccountTestResultsState(accountId);
  if (options.expand) state.expanded = true;
  state.requestSeq += 1;
  const requestSeq = state.requestSeq;
  state.loading = true;
  state.error = '';
  try {
    const payload = await requestJSON(`/api/admin/provider-accounts/${accountId}/test-results?limit=20`);
    if (!isCurrentAuthenticated(version)) return;
    if (!shouldApplyAccountTestResultsResponse(state, requestSeq)) return;
    state.items = payload.results ?? [];
  } catch (error) {
    if (!isCurrentAuthenticated(version)) return;
    if (!shouldApplyAccountTestResultsResponse(state, requestSeq)) return;
    state.error = error instanceof Error ? error.message : 'Account test history load failed';
  } finally {
    if (isCurrentAuthenticated(version) && shouldApplyAccountTestResultsResponse(state, requestSeq)) {
      state.loading = false;
    }
  }
}

/** @param {number} accountId */
export async function toggleAccountTestHistory(accountId) {
  const state = ensureAccountTestResultsState(accountId);
  if (state.expanded) {
    state.expanded = false;
    return;
  }
  await loadAccountTestResults(accountId, { expand: true });
}

export async function refreshExpandedAccountTestResults() {
  const expandedIDs = Object.entries(accountTestResults)
    .filter(([, state]) => state.expanded)
    .map(([id]) => Number(id))
    .filter((id) => Number.isFinite(id) && id > 0);
  await Promise.all(expandedIDs.map((id) => loadAccountTestResults(id)));
}
```

After `testProviderAccount()` reloads accounts/routing, add:

```js
await loadAccountTestResults(account.id);
```

After `testAllProviderAccounts()` reloads accounts/routing, add:

```js
await refreshExpandedAccountTestResults();
```

- [ ] **Step 5: Run state tests**

Run:

```bash
cd frontend
bun test src/routes/providers/provider-page.test.mjs
```

Expected: PASS.

- [ ] **Step 6: Commit state layer**

```bash
git add frontend/src/lib/admin-state.svelte.js frontend/src/routes/providers/provider-page.test.mjs
git commit -m "feat: add provider account test history state"
```

## Task 2: Provider Account Row History UI

**Files:**
- Modify: `frontend/src/routes/providers/+page.svelte`
- Modify: `frontend/src/routes/providers/provider-page.test.mjs`

- [ ] **Step 1: Write failing provider page tests**

Add to `frontend/src/routes/providers/provider-page.test.mjs`:

```js
test('provider account rows expose expandable test history', () => {
  assert.match(source, /toggleAccountTestHistory\(account\.id\)/);
  assert.match(source, /getAccountTestResultsState\(account\.id\)/);
  assert.match(source, /sr-only">Test history/);
  assert.match(source, /Loading test history/);
  assert.match(source, /No test history recorded yet/);
  assert.match(source, /historyState\.items/);
  assert.match(source, /result\.checkedAt/);
  assert.match(source, /result\.createdAt/);
  assert.match(source, /result\.message/);
});
```

- [ ] **Step 2: Run failing provider page test**

Run:

```bash
cd frontend
bun test src/routes/providers/provider-page.test.mjs
```

Expected: FAIL because the Provider accounts page does not import or render test history state.

- [ ] **Step 3: Import helpers**

In `frontend/src/routes/providers/+page.svelte`, add imports:

```js
getAccountTestResultsState,
toggleAccountTestHistory,
```

- [ ] **Step 4: Add status class helper**

In `frontend/src/routes/providers/+page.svelte`, add:

```js
/** @param {string | null | undefined} status */
function testResultStatusClass(status) {
  if (status === 'passed') return 'bg-[#e8f5f0] text-[#0a7a5e]';
  if (status === 'failed') return 'bg-amber-50 text-amber-700';
  return 'bg-[#f5f5f5] text-[#6e6e6e]';
}
```

- [ ] **Step 5: Add History action**

In the pinned actions button group, add a button after **Test account**:

```svelte
<button
  class="inline-flex size-8 items-center justify-center rounded-md border border-[#e5e5e5] bg-white text-sm font-semibold text-[#0d0d0d] hover:bg-[#f5f5f5] disabled:cursor-not-allowed disabled:text-[#9b9b9b]"
  type="button"
  disabled={providerAccounts.saving}
  onclick={() => toggleAccountTestHistory(account.id)}
  title="Test history"
  aria-label="Test history"
>
  <span aria-hidden="true">H</span>
  <span class="sr-only">Test history</span>
</button>
```

- [ ] **Step 6: Render expanded history row**

Inside the account loop, add:

```svelte
{@const historyState = getAccountTestResultsState(account.id)}
```

After the main `<tr>`, add:

```svelte
{#if historyState.expanded}
  <tr class="bg-[#fafafa]">
    <td class="px-4 py-4" colspan="11">
      <div class="rounded-lg border border-[#ededed] bg-white p-4">
        <div class="flex flex-wrap items-center justify-between gap-2">
          <h3 class="text-sm font-semibold text-[#0d0d0d]">Recent test history</h3>
          {#if historyState.loading}
            <span class="text-xs text-[#6e6e6e]">Loading test history...</span>
          {/if}
        </div>
        {#if historyState.error}
          <p class="mt-3 rounded-md border border-red-200 bg-red-50 p-3 text-sm text-red-700">{historyState.error}</p>
        {:else if !historyState.loading && historyState.items.length === 0}
          <p class="mt-3 text-sm text-[#6e6e6e]">No test history recorded yet.</p>
        {:else if historyState.items.length > 0}
          <div class="mt-3 overflow-x-auto rounded-lg border border-[#ededed]">
            <table class="w-full min-w-[560px] text-left text-sm">
              <thead class="border-b border-[#e5e5e5] bg-[#f5f5f5] text-[#6e6e6e]">
                <tr>
                  <th class="px-3 py-2 font-medium">Checked</th>
                  <th class="px-3 py-2 font-medium">Status</th>
                  <th class="px-3 py-2 font-medium">Message</th>
                  <th class="px-3 py-2 font-medium">Recorded</th>
                </tr>
              </thead>
              <tbody class="divide-y divide-[#ededed]">
                {#each historyState.items as result (result.id)}
                  <tr>
                    <td class="whitespace-nowrap px-3 py-2 text-[#3c3c3c]">{formatDate(result.checkedAt)}</td>
                    <td class="px-3 py-2">
                      <span class={['inline-flex rounded-full px-2 py-0.5 text-xs font-medium', testResultStatusClass(result.status)]}>
                        {result.status || 'unknown'}
                      </span>
                    </td>
                    <td class="max-w-[28rem] px-3 py-2 text-[#3c3c3c]">
                      {result.message || 'No message'}
                    </td>
                    <td class="whitespace-nowrap px-3 py-2 text-[#6e6e6e]">{formatDate(result.createdAt)}</td>
                  </tr>
                {/each}
              </tbody>
            </table>
          </div>
        {/if}
      </div>
    </td>
  </tr>
{/if}
```

- [ ] **Step 7: Run provider page tests**

Run:

```bash
cd frontend
bun test src/routes/providers/provider-page.test.mjs
```

Expected: PASS.

- [ ] **Step 8: Run frontend verification**

Run:

```bash
cd frontend
bun run check
bun run build
```

Expected: both commands exit 0.

- [ ] **Step 9: Commit UI layer**

```bash
git add frontend/src/routes/providers/+page.svelte frontend/src/routes/providers/provider-page.test.mjs
git commit -m "feat: show provider account test history"
```

## Task 3: Rendered Smoke Check

**Files:**
- No committed files expected.

- [ ] **Step 1: Start dev server**

Run:

```bash
cd frontend
bun run dev -- --host 0.0.0.0
```

Expected: Vite serves the admin UI on a reachable port. Use `oc-de-fra-1.knowsky.uk:<port>` for user-facing URLs.

- [ ] **Step 2: Browser smoke**

Use Browser plugin if available. If not available, use regular Playwright and record `Browser plugin not available`.

Flow under test:

`/providers -> sign-in or authenticated shell -> Provider accounts table -> History action -> expanded history row state`

Checks:

- Page title is `N2API Providers`.
- Page is not blank.
- No framework error overlay.
- Console has no relevant app errors.
- The History control exists in the Provider accounts table source/rendered DOM.

- [ ] **Step 3: Final full verification**

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

Expected: all commands exit 0. If backend tests fail in the sandbox due to `httptest` local listener permissions, rerun the same backend test command with escalated permissions and record the sandbox failure.

# API Key Table Bulk Management Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add table filtering, row selection, and frontend-first bulk management to the API Keys page.

**Architecture:** Keep API key bulk behavior in the frontend for this slice. Add selection state and batch helpers in `admin-state.svelte.js`, then update `api-keys/+page.svelte` to render filters, selection, action buttons, and a bulk edit modal that sequentially reuses existing per-key admin APIs.

**Tech Stack:** SvelteKit/Svelte 5 runes, Bun, Tailwind CSS, existing source-level `node:test` assertions.

---

## File Structure

- Modify `frontend/src/routes/navigation.test.mjs`
  - Add source-level regression tests for the API key selection UI, bulk edit modal, and batch helper shape.
- Modify `frontend/src/lib/admin-state.svelte.js`
  - Add `apiKeys.saving`.
  - Add `selectedAPIKeyIds`.
  - Add selection helpers.
  - Add frontend batch helpers that call existing single-key helpers.
- Modify `frontend/src/routes/api-keys/+page.svelte`
  - Import new helpers and icons.
  - Add filter state for routing pool, model policy, and issue state.
  - Add selection derived state.
  - Add select column, select-all, bulk action toolbar, and bulk edit modal.
  - Preserve current create modal, single-key edit modal, logs modal, inline status toggle, copy, edit, logs, and delete behavior.

---

### Task 1: Add Failing Source Tests

**Files:**
- Modify: `frontend/src/routes/navigation.test.mjs`

- [ ] **Step 1: Add the API key bulk management tests**

In `frontend/src/routes/navigation.test.mjs`, after the existing `api keys page initializes key search from client key URL param` test, add:

```js
test('api keys page supports table filters and row selection', () => {
  for (const label of [
    'Routing pool filter',
    'Model policy filter',
    'Issue filter',
    'Global pool',
    'All model policies',
    'Selected models',
    'Only blocked or budget exceeded',
    'Select',
    'Edit selected',
    'Enable',
    'Disable',
    'Delete',
    'Clear'
  ]) {
    assert.match(apiKeysPage, new RegExp(label.replaceAll(' ', '\\s+')), `api keys page should include ${label}`);
  }

  assert.match(apiKeysPage, /selectedAPIKeyIds/);
  assert.match(apiKeysPage, /selectedAPIKeyCount/);
  assert.match(apiKeysPage, /selectedEditableAPIKeys/);
  assert.match(apiKeysPage, /allFilteredAPIKeysSelected/);
  assert.match(apiKeysPage, /toggleAPIKeySelection/);
  assert.match(apiKeysPage, /toggleFilteredAPIKeySelection/);
  assert.match(apiKeysPage, /clearAPIKeySelection/);
  assert.match(apiKeysPage, /bulkSetSelectedAPIKeysDisabled/);
  assert.match(apiKeysPage, /bulkRevokeSelectedAPIKeys/);
  assert.match(apiKeysPage, /openBulkEditModal/);
  assert.match(apiKeysPage, /bind:value=\{keyRoutingPoolFilter\}/);
  assert.match(apiKeysPage, /bind:value=\{keyModelPolicyFilter\}/);
  assert.match(apiKeysPage, /bind:value=\{keyIssueFilter\}/);
});

test('api keys page has a bulk edit modal with opt-in sections', () => {
  for (const label of [
    'Bulk edit API keys',
    'Selected keys',
    'Apply status',
    'Apply model access',
    'Apply routing pool',
    'Apply limits',
    'Apply budgets',
    'Leave unchanged',
    'Apply changes'
  ]) {
    assert.match(apiKeysPage, new RegExp(label.replaceAll(' ', '\\s+')), `bulk edit modal should include ${label}`);
  }

  assert.match(apiKeysPage, /bulkEditModalOpen/);
  assert.match(apiKeysPage, /bulkEditForm\.applyStatus/);
  assert.match(apiKeysPage, /bulkEditForm\.applyModelPolicy/);
  assert.match(apiKeysPage, /bulkEditForm\.applyRoutingPool/);
  assert.match(apiKeysPage, /bulkEditForm\.applyLimits/);
  assert.match(apiKeysPage, /bulkEditForm\.applyBudgets/);
  assert.match(apiKeysPage, /submitBulkEdit/);
  assert.match(apiKeysPage, /bulkUpdateSelectedAPIKeys/);
});

test('api key batch helpers reuse existing per-key endpoints', () => {
  assert.match(adminState, /selectedAPIKeyIds/);
  assert.match(adminState, /export function toggleAPIKeySelection/);
  assert.match(adminState, /export function clearAPIKeySelection/);
  assert.match(adminState, /export async function bulkSetSelectedAPIKeysDisabled/);
  assert.match(adminState, /export async function bulkRevokeSelectedAPIKeys/);
  assert.match(adminState, /export async function bulkUpdateSelectedAPIKeys/);
  assert.match(adminState, /await setAPIKeyDisabled\(id,\s*disabled\)/);
  assert.match(adminState, /await revokeKey\(id\)/);
  assert.match(adminState, /await updateAPIKeyModelPolicy/);
  assert.match(adminState, /await updateAPIKeyLimits/);
  assert.match(adminState, /await updateAPIKeyBudgets/);
  assert.match(adminState, /await updateAPIKeyRoutingPool/);
  assert.doesNotMatch(adminState, /\/api\/admin\/keys\/bulk/);
});
```

- [ ] **Step 2: Run the source test and confirm failure**

Run:

```bash
cd frontend && bun test src/routes/navigation.test.mjs
```

Expected: FAIL. The failure should mention missing `selectedAPIKeyIds`, bulk action labels, or bulk helper names.

---

### Task 2: Add API Key Selection and Batch Helpers

**Files:**
- Modify: `frontend/src/lib/admin-state.svelte.js`

- [ ] **Step 1: Extend API key state**

Change the `apiKeys` typedef and state near the existing API key state from:

```js
/** @type {{ loading: boolean, creating: boolean, error: string, items: APIKey[], newKeyName: string, oneTimeSecret: string }} */
export const apiKeys = $state({
  loading: false,
  creating: false,
  error: '',
  items: [],
  newKeyName: '',
  oneTimeSecret: ''
});
```

to:

```js
/** @type {{ loading: boolean, creating: boolean, saving: boolean, error: string, items: APIKey[], newKeyName: string, oneTimeSecret: string }} */
export const apiKeys = $state({
  loading: false,
  creating: false,
  saving: false,
  error: '',
  items: [],
  newKeyName: '',
  oneTimeSecret: ''
});
```

Add API key selection state beside `selectedProviderAccountIds`:

```js
/** @type {Record<string, boolean>} */
export const selectedAPIKeyIds = $state({});
```

- [ ] **Step 2: Add selection helper functions**

Near the existing provider account selection helpers, add:

```js
/** @param {number} keyId */
function selectedAPIKeyKey(keyId) {
  return String(keyId);
}

/**
 * @param {number} keyId
 * @param {boolean} selected
 */
export function toggleAPIKeySelection(keyId, selected) {
  const key = selectedAPIKeyKey(keyId);
  if (selected) {
    selectedAPIKeyIds[key] = true;
    return;
  }
  delete selectedAPIKeyIds[key];
}

export function clearAPIKeySelection() {
  for (const key of Object.keys(selectedAPIKeyIds)) {
    delete selectedAPIKeyIds[key];
  }
}

/**
 * @param {number[]} ids
 * @param {boolean} selected
 */
export function setAPIKeySelection(ids, selected) {
  for (const id of ids) {
    toggleAPIKeySelection(id, selected);
  }
}

function selectedAPIKeyIDs() {
  return Object.keys(selectedAPIKeyIds)
    .map((id) => Number(id))
    .filter((id) => Number.isFinite(id) && id > 0);
}

function selectedEditableAPIKeyIDs() {
  const selected = new Set(selectedAPIKeyIDs());
  return apiKeys.items
    .filter((key) => selected.has(key.id) && !key.revokedAt)
    .map((key) => key.id);
}
```

- [ ] **Step 3: Add the generic batch runner**

Near the single-key API helper functions, add:

```js
/**
 * @param {number[]} ids
 * @param {(id: number) => Promise<void>} action
 */
async function runAPIKeyBatch(ids, action) {
  const version = sessionVersion;
  if (!isCurrentAuthenticated(version)) return false;
  if (ids.length === 0) {
    apiKeys.error = 'Select at least one API key';
    return false;
  }

  apiKeys.saving = true;
  apiKeys.error = '';
  try {
    for (const id of ids) {
      await action(id);
      if (!isCurrentAuthenticated(version)) return false;
      if (apiKeys.error) return false;
      delete selectedAPIKeyIds[String(id)];
    }
    return true;
  } finally {
    if (isCurrentAuthenticated(version)) apiKeys.saving = false;
  }
}
```

- [ ] **Step 4: Add batch helper functions**

Add:

```js
/** @param {boolean} disabled */
export async function bulkSetSelectedAPIKeysDisabled(disabled) {
  const ids = selectedEditableAPIKeyIDs();
  if (ids.length === 0) {
    apiKeys.error = 'Select at least one active or disabled API key';
    return false;
  }
  const ok = await runAPIKeyBatch(ids, async (id) => {
    await setAPIKeyDisabled(id, disabled);
  });
  if (ok) await loadRequestLogs();
  return ok;
}

export async function bulkRevokeSelectedAPIKeys() {
  const ids = selectedEditableAPIKeyIDs();
  if (ids.length === 0) {
    apiKeys.error = 'Select at least one active or disabled API key';
    return false;
  }
  const ok = await runAPIKeyBatch(ids, async (id) => {
    await revokeKey(id);
  });
  if (ok) await loadRequestLogs();
  return ok;
}

/**
 * @param {{
 *   disabled?: boolean,
 *   modelPolicy?: string,
 *   modelsText?: string,
 *   routingPoolId?: string | number | null,
 *   requestsPerMinute?: string | number,
 *   tokensPerMinute?: string | number,
 *   requestBudget24h?: string | number,
 *   tokenBudget24h?: string | number,
 *   costBudgetMicrousd24h?: string | number,
 *   requestBudget30d?: string | number,
 *   tokenBudget30d?: string | number,
 *   costBudgetMicrousd30d?: string | number
 * }} patch
 */
export async function bulkUpdateSelectedAPIKeys(patch) {
  const ids = selectedEditableAPIKeyIDs();
  if (ids.length === 0) {
    apiKeys.error = 'Select at least one active or disabled API key';
    return false;
  }
  if (Object.keys(patch).length === 0) {
    apiKeys.error = 'Choose at least one bulk edit section';
    return false;
  }

  const ok = await runAPIKeyBatch(ids, async (id) => {
    const current = apiKeys.items.find((key) => key.id === id);
    if (!current) {
      apiKeys.error = 'Selected API key no longer exists';
      return;
    }
    if (patch.disabled !== undefined) {
      await setAPIKeyDisabled(id, patch.disabled);
      if (apiKeys.error) return;
    }
    if (patch.modelPolicy !== undefined) {
      await updateAPIKeyModelPolicy(id, patch.modelPolicy, patch.modelsText ?? '');
      if (apiKeys.error) return;
    }
    if (patch.routingPoolId !== undefined) {
      await updateAPIKeyRoutingPool(id, patch.routingPoolId);
      if (apiKeys.error) return;
    }
    if (patch.requestsPerMinute !== undefined || patch.tokensPerMinute !== undefined) {
      await updateAPIKeyLimits(
        id,
        patch.requestsPerMinute ?? current.requestsPerMinute ?? 0,
        patch.tokensPerMinute ?? current.tokensPerMinute ?? 0
      );
      if (apiKeys.error) return;
    }
    if (
      patch.requestBudget24h !== undefined ||
      patch.tokenBudget24h !== undefined ||
      patch.costBudgetMicrousd24h !== undefined ||
      patch.requestBudget30d !== undefined ||
      patch.tokenBudget30d !== undefined ||
      patch.costBudgetMicrousd30d !== undefined
    ) {
      await updateAPIKeyBudgets(
        id,
        patch.requestBudget24h ?? current.requestBudget24h ?? 0,
        patch.tokenBudget24h ?? current.tokenBudget24h ?? 0,
        patch.costBudgetMicrousd24h ?? current.costBudgetMicrousd24h ?? 0,
        patch.requestBudget30d ?? current.requestBudget30d ?? 0,
        patch.tokenBudget30d ?? current.tokenBudget30d ?? 0,
        patch.costBudgetMicrousd30d ?? current.costBudgetMicrousd30d ?? 0
      );
    }
  });
  if (ok) await loadRequestLogs();
  return ok;
}
```

- [ ] **Step 5: Run the source test and confirm the helper assertions move forward**

Run:

```bash
cd frontend && bun test src/routes/navigation.test.mjs
```

Expected: still FAIL because the page UI has not been implemented yet. The batch helper test should pass or no longer be the first failure.

---

### Task 3: Add API Keys Table Filters, Selection, and Bulk Modal

**Files:**
- Modify: `frontend/src/routes/api-keys/+page.svelte`

- [ ] **Step 1: Update imports**

Change the icon import to include `X`:

```svelte
import { Copy, Pencil, ScrollText, Trash2, X } from 'lucide-svelte';
```

Add these imports from `admin-state.svelte.js`:

```js
bulkRevokeSelectedAPIKeys,
bulkSetSelectedAPIKeysDisabled,
bulkUpdateSelectedAPIKeys,
clearAPIKeySelection,
selectedAPIKeyIds,
setAPIKeySelection,
toggleAPIKeySelection,
```

- [ ] **Step 2: Add filter, sort-free selection, and bulk modal state**

Near existing page state, add:

```js
let keyRoutingPoolFilter = $state('all');
let keyModelPolicyFilter = $state('all');
let keyIssueFilter = $state('all');
let bulkEditModalOpen = $state(false);
const defaultBulkEditForm = () => ({
  applyStatus: false,
  disabled: false,
  applyModelPolicy: false,
  modelPolicy: 'all',
  modelsText: '',
  applyRoutingPool: false,
  routingPoolId: '0',
  applyLimits: false,
  requestsPerMinute: '',
  tokensPerMinute: '',
  applyBudgets: false,
  requestBudget24h: '',
  tokenBudget24h: '',
  costBudgetMicrousd24h: '',
  requestBudget30d: '',
  tokenBudget30d: '',
  costBudgetMicrousd30d: ''
});
let bulkEditForm = $state(defaultBulkEditForm());
```

Add derived state:

```js
const selectedAPIKeyCount = $derived(Object.keys(selectedAPIKeyIds).length);
const selectedEditableAPIKeys = $derived(
  apiKeys.items.filter((key) => Boolean(selectedAPIKeyIds[key.id]) && !key.revokedAt)
);
const filteredAPIKeyIds = $derived(filteredAPIKeys.map((key) => key.id));
const allFilteredAPIKeysSelected = $derived(
  filteredAPIKeyIds.length > 0 && filteredAPIKeyIds.every((id) => Boolean(selectedAPIKeyIds[id]))
);
```

- [ ] **Step 3: Extend filteredAPIKeys**

Replace the current `filteredAPIKeys` filter body with logic that additionally checks:

```js
if (keyRoutingPoolFilter === 'global' && Number(key.routingPoolId ?? 0) > 0) return false;
if (/^[1-9]\d*$/.test(keyRoutingPoolFilter) && String(key.routingPoolId ?? 0) !== keyRoutingPoolFilter) return false;
if (keyModelPolicyFilter === 'all_routable' && key.modelPolicy === 'selected') return false;
if (keyModelPolicyFilter === 'selected' && key.modelPolicy !== 'selected') return false;
if (keyIssueFilter === 'attention' && !apiKeyHasIssue(key)) return false;
```

Add:

```js
/** @param {import('$lib/admin-state.svelte.js').APIKey} key */
function apiKeyHasIssue(key) {
  return Boolean(
    key.concurrencyBlocked ||
      key.requestRateLimited ||
      key.tokenRateLimited ||
      key.requestBudgetExceeded ||
      key.tokenBudgetExceeded ||
      key.costBudgetExceeded
  );
}
```

- [ ] **Step 4: Add selection and modal helper functions**

Add:

```js
/** @param {boolean} selected */
function toggleFilteredAPIKeySelection(selected) {
  setAPIKeySelection(filteredAPIKeyIds, selected);
}

function openBulkEditModal() {
  apiKeys.error = '';
  bulkEditForm = defaultBulkEditForm();
  bulkEditModalOpen = true;
}

function closeBulkEditModal() {
  bulkEditModalOpen = false;
  bulkEditForm = defaultBulkEditForm();
}

async function submitBulkEdit() {
  /** @type {Record<string, string | number | boolean | null>} */
  const patch = {};
  if (bulkEditForm.applyStatus) patch.disabled = Boolean(bulkEditForm.disabled);
  if (bulkEditForm.applyModelPolicy) {
    patch.modelPolicy = bulkEditForm.modelPolicy;
    patch.modelsText = bulkEditForm.modelsText;
  }
  if (bulkEditForm.applyRoutingPool) {
    patch.routingPoolId = Number(bulkEditForm.routingPoolId || 0);
  }
  if (bulkEditForm.applyLimits) {
    if (bulkEditForm.requestsPerMinute !== '') patch.requestsPerMinute = bulkEditForm.requestsPerMinute;
    if (bulkEditForm.tokensPerMinute !== '') patch.tokensPerMinute = bulkEditForm.tokensPerMinute;
  }
  if (bulkEditForm.applyBudgets) {
    if (bulkEditForm.requestBudget24h !== '') patch.requestBudget24h = bulkEditForm.requestBudget24h;
    if (bulkEditForm.tokenBudget24h !== '') patch.tokenBudget24h = bulkEditForm.tokenBudget24h;
    if (bulkEditForm.costBudgetMicrousd24h !== '') patch.costBudgetMicrousd24h = bulkEditForm.costBudgetMicrousd24h;
    if (bulkEditForm.requestBudget30d !== '') patch.requestBudget30d = bulkEditForm.requestBudget30d;
    if (bulkEditForm.tokenBudget30d !== '') patch.tokenBudget30d = bulkEditForm.tokenBudget30d;
    if (bulkEditForm.costBudgetMicrousd30d !== '') patch.costBudgetMicrousd30d = bulkEditForm.costBudgetMicrousd30d;
  }
  const ok = await bulkUpdateSelectedAPIKeys(patch);
  if (ok) closeBulkEditModal();
}
```

- [ ] **Step 5: Render the bulk edit modal**

After the single-key edit modal block and before the logs modal block, render a modal guarded by:

```svelte
{#if bulkEditModalOpen}
```

The modal must include:

```svelte
<h3 class="text-lg font-semibold text-[#0d0d0d]">Bulk edit API keys</h3>
<p class="mt-1 text-sm text-[#6e6e6e]">Selected keys: {selectedAPIKeyCount}. Editable: {selectedEditableAPIKeys.length}.</p>
```

It must include checkbox-enabled sections:

```svelte
<label><input type="checkbox" bind:checked={bulkEditForm.applyStatus} /> Apply status</label>
<label><input type="checkbox" bind:checked={bulkEditForm.applyModelPolicy} /> Apply model access</label>
<label><input type="checkbox" bind:checked={bulkEditForm.applyRoutingPool} /> Apply routing pool</label>
<label><input type="checkbox" bind:checked={bulkEditForm.applyLimits} /> Apply limits</label>
<label><input type="checkbox" bind:checked={bulkEditForm.applyBudgets} /> Apply budgets</label>
```

Use the same option values as the single-key edit modal:

```svelte
<option value="all">All routable models</option>
<option value="selected">Selected models</option>
<option value={0}>Global provider account pool</option>
{#each routingPools.items as pool}
  <option value={pool.id}>{pool.name}</option>
{/each}
```

Add explanatory text:

```svelte
<p class="text-xs text-[#6e6e6e]">Leave unchanged by keeping a section unchecked. Inside checked numeric sections, blank fields leave that value unchanged; enter 0 to apply the existing default or unlimited behavior.</p>
```

The form submit must call `submitBulkEdit`:

```svelte
<form onsubmit={(event) => { event.preventDefault(); submitBulkEdit(); }}>
```

The primary button text must be `Apply changes` and be disabled when `apiKeys.saving || selectedEditableAPIKeys.length === 0`.

- [ ] **Step 6: Replace the filter toolbar**

Extend the existing filter toolbar with labels and controls:

```svelte
<label class="block text-sm font-medium text-[#3c3c3c]">
  Routing pool filter
  <select bind:value={keyRoutingPoolFilter}>
    <option value="all">All routing pools</option>
    <option value="global">Global pool</option>
    {#each routingPools.items as pool}
      <option value={String(pool.id)}>{pool.name}</option>
    {/each}
  </select>
</label>
<label class="block text-sm font-medium text-[#3c3c3c]">
  Model policy filter
  <select bind:value={keyModelPolicyFilter}>
    <option value="all">All model policies</option>
    <option value="all_routable">All routable models</option>
    <option value="selected">Selected models</option>
  </select>
</label>
<label class="block text-sm font-medium text-[#3c3c3c]">
  Issue filter
  <select bind:value={keyIssueFilter}>
    <option value="all">All issue states</option>
    <option value="attention">Only blocked or budget exceeded</option>
  </select>
</label>
```

- [ ] **Step 7: Render the selected bulk action bar**

Below filters and above the table, render only when selected keys exist:

```svelte
{#if selectedAPIKeyCount > 0}
  <div class="mt-4 flex flex-wrap items-center justify-between gap-3 rounded-lg border border-[#e5e5e5] bg-[#fafafa] p-3">
    <p class="text-sm text-[#3c3c3c]">{selectedAPIKeyCount} selected · {selectedEditableAPIKeys.length} editable</p>
    <div class="flex flex-wrap gap-2">
      <button type="button" onclick={openBulkEditModal}>Edit selected</button>
      <button type="button" onclick={() => bulkSetSelectedAPIKeysDisabled(false)}>Enable</button>
      <button type="button" onclick={() => bulkSetSelectedAPIKeysDisabled(true)}>Disable</button>
      <button type="button" onclick={bulkRevokeSelectedAPIKeys}>Delete</button>
      <button type="button" onclick={clearAPIKeySelection}>Clear</button>
    </div>
  </div>
{/if}
```

Use existing quiet dashboard button classes. Disable mutation buttons while `apiKeys.saving` is true.

- [ ] **Step 8: Add the select column to the table**

Change the table minimum width from `min-w-[860px]` to at least `min-w-[980px]`.

Add a first header cell:

```svelte
<th class="w-12 px-4 py-3 font-medium">
  <label class="inline-flex items-center">
    <input
      class="size-4 rounded border-[#d9d9d9] text-[#10a37f] focus:ring-[#10a37f]"
      type="checkbox"
      checked={allFilteredAPIKeysSelected}
      disabled={apiKeys.loading || filteredAPIKeys.length === 0}
      onchange={(event) => toggleFilteredAPIKeySelection(event.currentTarget.checked)}
    />
    <span class="sr-only">Select filtered API keys</span>
  </label>
</th>
```

Add a first body cell in each row:

```svelte
<td class="px-4 py-3 align-middle">
  <label class="inline-flex items-center">
    <input
      class="size-4 rounded border-[#d9d9d9] text-[#10a37f] focus:ring-[#10a37f] disabled:cursor-not-allowed disabled:opacity-60"
      type="checkbox"
      checked={Boolean(selectedAPIKeyIds[key.id])}
      disabled={apiKeys.saving}
      onchange={(event) => toggleAPIKeySelection(key.id, event.currentTarget.checked)}
    />
    <span class="sr-only">Select {key.name}</span>
  </label>
</td>
```

Update empty/loading `colspan` values from `6` to `7`.

- [ ] **Step 9: Run source test**

Run:

```bash
cd frontend && bun test src/routes/navigation.test.mjs
```

Expected: PASS.

---

### Task 4: Svelte Verification and Polish

**Files:**
- Modify if needed: `frontend/src/routes/api-keys/+page.svelte`
- Modify if needed: `frontend/src/lib/admin-state.svelte.js`

- [ ] **Step 1: Run Svelte check**

Run:

```bash
cd frontend && bun run check
```

Expected: 0 errors.

- [ ] **Step 2: Fix any Svelte or JSDoc errors**

If `bun run check` reports a type issue for the dynamic patch object in `submitBulkEdit`, replace the JSDoc with:

```js
/** @type {{
 *   disabled?: boolean,
 *   modelPolicy?: string,
 *   modelsText?: string,
 *   routingPoolId?: number,
 *   requestsPerMinute?: string | number,
 *   tokensPerMinute?: string | number,
 *   requestBudget24h?: string | number,
 *   tokenBudget24h?: string | number,
 *   costBudgetMicrousd24h?: string | number,
 *   requestBudget30d?: string | number,
 *   tokenBudget30d?: string | number,
 *   costBudgetMicrousd30d?: string | number
 * }} */
const patch = {};
```

- [ ] **Step 3: Run frontend production build**

Run:

```bash
cd frontend && bun run build
```

Expected: build succeeds.

- [ ] **Step 4: Review diff for backend scope**

Run:

```bash
git diff --stat
git diff -- frontend/src/routes/api-keys/+page.svelte frontend/src/lib/admin-state.svelte.js frontend/src/routes/navigation.test.mjs
```

Expected: only the three frontend files above changed during implementation. If Go files changed, run backend tests before commit.

- [ ] **Step 5: Commit implementation**

Run:

```bash
git add frontend/src/routes/navigation.test.mjs frontend/src/lib/admin-state.svelte.js frontend/src/routes/api-keys/+page.svelte
git commit -m "feat: add api key bulk management"
```

Expected: commit succeeds.

---

### Task 5: Final Project Verification and Local Docker Refresh

**Files:**
- No source edits expected.

- [ ] **Step 1: Confirm committed state**

Run:

```bash
git status --short
```

Expected: no uncommitted source changes except possible local generated/cache files that should not be committed.

- [ ] **Step 2: Re-run frontend gates**

Run:

```bash
cd frontend && bun test src/routes/navigation.test.mjs
cd frontend && bun run check
cd frontend && bun run build
```

Expected: all pass.

- [ ] **Step 3: Refresh Docker Compose**

Use the `n2api-refresh-docker` skill before running commands. The expected refresh command sequence is:

```bash
docker compose -f deploy/compose.yaml up --build --detach
docker exec deploy-n2api-1 wget -qO- http://127.0.0.1:3000/api/admin/session
```

Expected: Compose rebuilds/recreates the N2API service and the smoke request returns a valid unauthenticated/session JSON response rather than a connection error.

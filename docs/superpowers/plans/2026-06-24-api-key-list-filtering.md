# API Key List Filtering Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add local search and status filtering to the API Keys management table.

**Architecture:** Keep filtering in the Svelte page as derived UI state over `apiKeys.items`. Reuse existing API key records and table actions without changing backend contracts.

**Tech Stack:** SvelteKit, Bun, existing source-level navigation tests.

---

### Task 1: Document Plan

**Files:**
- Create: `docs/superpowers/specs/2026-06-24-api-key-list-filtering-design.md`
- Create: `docs/superpowers/plans/2026-06-24-api-key-list-filtering.md`

- [ ] **Step 1: Commit design and plan**

Run:

```bash
git add docs/superpowers/specs/2026-06-24-api-key-list-filtering-design.md docs/superpowers/plans/2026-06-24-api-key-list-filtering.md
git commit -m "docs: plan api key list filtering"
```

Expected: commit succeeds.

### Task 2: API Keys Page Filtering

**Files:**
- Modify: `frontend/src/routes/navigation.test.mjs`
- Modify: `frontend/src/routes/api-keys/+page.svelte`

- [ ] **Step 1: Write the failing frontend source test**

Add a test that asserts the API Keys page contains:

```js
test('api keys page filters key list locally', () => {
  for (const label of ['Search keys', 'Status filter', 'All keys', 'Active keys', 'Revoked keys']) {
    assert.match(apiKeysPage, new RegExp(label.replace(' ', '\\s+')), `api keys page should include ${label}`);
  }

  assert.match(apiKeysPage, /let keySearch = \$state\(''\)/);
  assert.match(apiKeysPage, /let keyStatusFilter = \$state\('all'\)/);
  assert.match(apiKeysPage, /filteredAPIKeys/);
  assert.match(apiKeysPage, /apiKeySearchText/);
  assert.match(apiKeysPage, /bind:value=\{keySearch\}/);
  assert.match(apiKeysPage, /bind:value=\{keyStatusFilter\}/);
  assert.match(apiKeysPage, /Showing \{filteredAPIKeys\.length\} of \{apiKeys\.items\.length\}/);
  assert.match(apiKeysPage, /No API keys match your filters\./);
});
```

- [ ] **Step 2: Verify the test fails**

Run:

```bash
cd frontend && bun test src/routes/navigation.test.mjs
```

Expected: FAIL because the API Keys page does not yet have `keySearch`, `keyStatusFilter`, or `filteredAPIKeys`.

- [ ] **Step 3: Implement local filtering**

In `frontend/src/routes/api-keys/+page.svelte`, add state, a derived filtered list, and search text helpers:

```js
let keySearch = $state('');
let keyStatusFilter = $state('all');

const filteredAPIKeys = $derived(
  apiKeys.items.filter((key) => {
    if (keyStatusFilter === 'active' && key.revokedAt) return false;
    if (keyStatusFilter === 'revoked' && !key.revokedAt) return false;

    const query = keySearch.trim().toLowerCase();
    if (!query) return true;
    return apiKeySearchText(key).includes(query);
  })
);

function apiKeySearchText(key) {
  return [
    key.name,
    key.prefix,
    key.modelPolicy === 'selected' ? 'selected models' : 'all routable models',
    ...(key.allowedModels ?? []),
    key.revokedAt ? 'revoked' : 'active',
    key.concurrencyBlocked ? 'concurrency full' : '',
    key.requestRateLimited ? 'request limit full' : '',
    key.tokenRateLimited ? 'token limit full' : ''
  ]
    .filter(Boolean)
    .join(' ')
    .toLowerCase();
}
```

Render controls above the table:

```svelte
<div class="mt-6 flex flex-wrap items-end justify-between gap-3">
  <div class="flex flex-wrap items-end gap-3">
    <label class="block text-sm font-medium text-[#3c3c3c]">
      Search keys
      <input
        class="mt-2 w-64 max-w-full rounded-lg border border-[#e5e5e5] bg-white px-3 py-2 text-sm text-[#0d0d0d] outline-none focus:border-[#10a37f] focus:ring-2 focus:ring-[#e8f5f0]"
        type="search"
        bind:value={keySearch}
        placeholder="name, prefix, model, status"
      />
    </label>
    <label class="block text-sm font-medium text-[#3c3c3c]">
      Status filter
      <select
        class="mt-2 rounded-lg border border-[#e5e5e5] bg-white px-3 py-2 text-sm text-[#0d0d0d] outline-none focus:border-[#10a37f] focus:ring-2 focus:ring-[#e8f5f0]"
        bind:value={keyStatusFilter}
      >
        <option value="all">All keys</option>
        <option value="active">Active keys</option>
        <option value="revoked">Revoked keys</option>
      </select>
    </label>
  </div>
  <p class="text-sm text-[#6e6e6e]">
    Showing {filteredAPIKeys.length} of {apiKeys.items.length}
  </p>
</div>
```

Render `filteredAPIKeys` in the table and add the filtered empty state:

```svelte
{:else if filteredAPIKeys.length === 0}
  <tr>
    <td class="px-4 py-5 text-[#6e6e6e]" colspan="8">No API keys match your filters.</td>
  </tr>
{:else}
  {#each filteredAPIKeys as key}
```

- [ ] **Step 4: Verify the frontend test passes**

Run:

```bash
cd frontend && bun test src/routes/navigation.test.mjs
```

Expected: PASS.

- [ ] **Step 5: Run Svelte checks**

Run:

```bash
cd frontend && bun run check
```

Expected: 0 errors and 0 warnings.

- [ ] **Step 6: Commit**

Run:

```bash
git add frontend/src/routes/navigation.test.mjs frontend/src/routes/api-keys/+page.svelte
git commit -m "feat: filter api key list"
```

Expected: commit succeeds.

### Task 3: Documentation

**Files:**
- Modify: `README.md`
- Modify: `deploy/README.md`
- Modify: `backend/internal/gateway/documentation_test.go`

- [ ] **Step 1: Add documentation test**

Add a test requiring both README files to mention:

```go
for _, want := range []string{
  "API Keys page supports local search and status filtering",
  "name, prefix, model policy, selected model, active/revoked status, and limiter state",
} {
  if !strings.Contains(text, want) {
    t.Fatalf("%s missing %q in API key list filtering documentation", path, want)
  }
}
```

- [ ] **Step 2: Verify the documentation test fails**

Run:

```bash
cd backend && GOMODCACHE=/root/Clouds/N2API/.cache/go-mod GOCACHE=/root/Clouds/N2API/.cache/go-build go test ./internal/gateway -run APIKeyListFiltering
```

Expected: FAIL because docs are not updated.

- [ ] **Step 3: Update docs**

Add the same sentence near the API key management documentation in `README.md` and `deploy/README.md`:

```markdown
The API Keys page supports local search and status filtering by name, prefix, model policy, selected model, active/revoked status, and limiter state, so a busy or revoked client key can be found without leaving the page.
```

- [ ] **Step 4: Verify documentation test passes**

Run:

```bash
cd backend && GOMODCACHE=/root/Clouds/N2API/.cache/go-mod GOCACHE=/root/Clouds/N2API/.cache/go-build go test ./internal/gateway -run APIKeyListFiltering
```

Expected: PASS.

- [ ] **Step 5: Commit**

Run:

```bash
git add README.md deploy/README.md backend/internal/gateway/documentation_test.go
git commit -m "docs: document api key list filtering"
```

Expected: commit succeeds.

### Task 4: Final Verification

Run:

```bash
git diff --check
cd backend && GOMODCACHE=/root/Clouds/N2API/.cache/go-mod GOCACHE=/root/Clouds/N2API/.cache/go-build go test ./...
cd frontend && bun test src/routes/navigation.test.mjs
cd frontend && bun run check
cd frontend && bun run build
git status --short
```

Expected: all commands pass and worktree is clean.

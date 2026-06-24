# Gateway Usage Log Drilldown Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Complete Gateway 24h usage drill-down links to exact Request Logs filters for model, session, provider account, and API key dimensions.

**Architecture:** Reuse the existing Request Logs filter state and backend `clientKeyId`/`providerAccountId` support. Add the missing API key dropdown to Request Logs and extend Gateway `usageRowHref` to map provider-account and client-key usage row IDs into existing URL parameters.

**Tech Stack:** SvelteKit, Bun source tests, Go documentation tests.

---

## File Structure

- Modify `frontend/src/routes/navigation.test.mjs`: source assertions for API key filter dropdown and Gateway links.
- Modify `frontend/src/routes/request-logs/+page.svelte`: import/load `apiKeys` and render API key filter.
- Modify `frontend/src/routes/gateway/+page.svelte`: extend `usageRowHref`.
- Modify `backend/internal/gateway/documentation_test.go`: require docs for all-dimension drill-down.
- Modify `README.md` and `deploy/README.md`: document behavior.

### Task 1: Request Logs API Key Filter And Gateway Links

**Files:**
- Modify: `frontend/src/routes/navigation.test.mjs`
- Modify: `frontend/src/routes/request-logs/+page.svelte`
- Modify: `frontend/src/routes/gateway/+page.svelte`

- [ ] **Step 1: Write failing frontend tests**

Extend `test('request logs page filters by provider account'...)` with:

```js
assert.match(requestLogsPage, /apiKeys/);
assert.match(requestLogsPage, /loadKeys\(\)/);
assert.match(requestLogsPage, /bind:value=\{requestLogs\.clientKeyId\}/);
assert.match(requestLogsPage, /All API keys/);
```

Extend `test('gateway usage rows link to filtered request logs'...)` with:

```js
assert.match(gatewayPage, /providerAccountId=\$\{encodeURIComponent/);
assert.match(gatewayPage, /clientKeyId=\$\{encodeURIComponent/);
assert.match(gatewayPage, /providerAccountUsageId/);
```

- [ ] **Step 2: Run red frontend test**

```bash
cd frontend && bun test src/routes/navigation.test.mjs
```

Expected: FAIL because Request Logs has no API key dropdown and Gateway usage rows only link model/session.

- [ ] **Step 3: Implement Request Logs API key dropdown**

In `frontend/src/routes/request-logs/+page.svelte`, import:

```svelte
apiKeys,
loadKeys,
```

Add:

```svelte
let apiKeysRequested = $state(false);
```

In the authenticated effect:

```svelte
if (!apiKeysRequested && apiKeys.items.length === 0) {
  apiKeysRequested = true;
  void loadKeys();
}
```

Reset it on logout beside provider state:

```svelte
apiKeysRequested = false;
```

Render a dropdown in the Request Logs filter bar:

```svelte
<label class="block text-sm font-medium text-[#3c3c3c]">
  API key
  <select
    class="mt-2 max-w-[240px] rounded-lg border border-[#e5e5e5] bg-white px-3 py-2 text-sm text-[#0d0d0d] outline-none focus:border-[#10a37f] focus:ring-2 focus:ring-[#e8f5f0]"
    bind:value={requestLogs.clientKeyId}
  >
    <option value="all">All API keys</option>
    {#each apiKeys.items as key}
      <option value={String(key.id)}>{key.name} ({key.prefix})</option>
    {/each}
  </select>
</label>
```

- [ ] **Step 4: Implement Gateway usage links**

Add helper in `frontend/src/routes/gateway/+page.svelte`:

```svelte
function providerAccountUsageId(id) {
  const value = String(id ?? '');
  const parts = value.split('/');
  const accountId = parts[parts.length - 1] ?? '';
  return /^[1-9]\d*$/.test(accountId) ? accountId : '';
}
```

Extend `usageRowHref`:

```svelte
if (sectionTitle === 'Top provider accounts') {
  const accountId = providerAccountUsageId(id);
  return accountId ? `/request-logs?providerAccountId=${encodeURIComponent(accountId)}` : '';
}
if (sectionTitle === 'Top client keys' && /^[1-9]\d*$/.test(id)) {
  return `/request-logs?clientKeyId=${encodeURIComponent(id)}`;
}
```

- [ ] **Step 5: Run frontend tests and checks**

```bash
cd frontend && bun test src/routes/navigation.test.mjs
cd frontend && bun run check
```

Expected: PASS.

- [ ] **Step 6: Commit frontend changes**

```bash
git add frontend/src/routes/request-logs/+page.svelte frontend/src/routes/gateway/+page.svelte frontend/src/routes/navigation.test.mjs
git commit -m "feat: drill down gateway usage logs"
```

### Task 2: Documentation And Full Verification

**Files:**
- Modify: `backend/internal/gateway/documentation_test.go`
- Modify: `README.md`
- Modify: `deploy/README.md`

- [ ] **Step 1: Write failing documentation test**

Add:

```go
func TestGatewayDocumentationMentionsAllUsageLogDrilldowns(t *testing.T) {
	for _, path := range []string{"../../../README.md", "../../../deploy/README.md"} {
		content, err := os.ReadFile(path)
		if err != nil {
			t.Fatalf("ReadFile(%q) returned error: %v", path, err)
		}
		text := string(content)
		for _, want := range []string{
			"Top provider accounts",
			"Top client keys",
			"provider-account, API-key, model, and sticky-session",
		} {
			if !strings.Contains(text, want) {
				t.Fatalf("%s missing %q in all usage log drill-down documentation", path, want)
			}
		}
	}
}
```

- [ ] **Step 2: Run red docs test**

```bash
cd backend && GOMODCACHE=/root/Clouds/N2API/.cache/go-mod GOCACHE=/root/Clouds/N2API/.cache/go-build go test ./internal/gateway -run AllUsageLogDrilldowns
```

Expected: FAIL because docs do not mention all four drill-down dimensions yet.

- [ ] **Step 3: Update docs**

Add:

```markdown
Gateway management 24h usage rows for **Top provider accounts**, **Top client keys**, **Top models**, and **Top sessions** link to Request Logs with exact provider-account, API-key, model, and sticky-session filters when the row identifies a concrete entity.
```

- [ ] **Step 4: Run docs test**

```bash
cd backend && GOMODCACHE=/root/Clouds/N2API/.cache/go-mod GOCACHE=/root/Clouds/N2API/.cache/go-build go test ./internal/gateway -run AllUsageLogDrilldowns
```

Expected: PASS.

- [ ] **Step 5: Commit docs**

```bash
git add backend/internal/gateway/documentation_test.go README.md deploy/README.md
git commit -m "docs: document gateway usage log drilldowns"
```

- [ ] **Step 6: Full verification**

```bash
git diff --check
cd backend && GOMODCACHE=/root/Clouds/N2API/.cache/go-mod GOCACHE=/root/Clouds/N2API/.cache/go-build go test ./...
cd frontend && bun test src/routes/navigation.test.mjs
cd frontend && bun run check
cd frontend && bun run build
```

Expected: PASS. If sandbox blocks backend `httptest` IPv6 listeners, rerun the same backend command with elevated permissions.

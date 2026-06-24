# Gateway Scheduling Health Summary Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add a Gateway management scheduling-health summary for provider account capacity and blocked-account causes.

**Architecture:** Reuse the existing Svelte admin state helpers instead of adding a backend endpoint. The Gateway page already loads provider accounts; it will derive enabled, schedulable, and blocked reason counts locally. Documentation coverage stays in the existing Go documentation tests.

**Tech Stack:** SvelteKit, Bun source tests, Go documentation tests.

---

## File Structure

- Modify `frontend/src/routes/navigation.test.mjs`: add source assertions for the Gateway scheduling health panel.
- Modify `frontend/src/routes/gateway/+page.svelte`: import `getUnschedulableProviderAccountSummary` and render the panel.
- Modify `backend/internal/gateway/documentation_test.go`: require docs to mention Gateway scheduling health.
- Modify `README.md`: document the Gateway page scheduling-health summary.
- Modify `deploy/README.md`: document the deployment-facing summary.

### Task 1: Gateway Page Scheduling Health Panel

**Files:**
- Modify: `frontend/src/routes/navigation.test.mjs`
- Modify: `frontend/src/routes/gateway/+page.svelte`

- [ ] **Step 1: Write failing source test**

In `test('gateway page manages runtime limits and usage visibility'...)`, add labels:

```js
'Scheduling health',
+'Enabled accounts',
+'Blocked accounts',
+'Blocked reasons',
+'No blocked provider accounts.'
```

Add source assertions:

```js
assert.match(gatewayPage, /getUnschedulableProviderAccountSummary/);
assert.match(gatewayPage, /unschedulableAccountSummary/);
assert.match(gatewayPage, /enabledProviderAccountCount/);
```

- [ ] **Step 2: Run red frontend test**

```bash
cd frontend && bun test src/routes/navigation.test.mjs
```

Expected: FAIL because the Gateway page does not render the scheduling-health panel yet.

- [ ] **Step 3: Implement Gateway page panel**

Update imports in `frontend/src/routes/gateway/+page.svelte`:

```svelte
getSchedulableProviderAccounts,
+getUnschedulableProviderAccountSummary,
```

Add derived values:

```svelte
const enabledProviderAccountCount = $derived(providerAccounts.items.filter((account) => account.enabled).length);
const unschedulableAccountSummary = $derived(getUnschedulableProviderAccountSummary(providerAccounts.items));
const unschedulableAccountCount = $derived(
  unschedulableAccountSummary.reduce((total, item) => total + item.count, 0)
);
```

Render a section after Gateway readiness:

```svelte
<section class="rounded-lg border border-[#ededed] bg-white p-6">
  <div>
    <h3 class="text-base font-semibold text-[#0d0d0d]">Scheduling health</h3>
    <p class="mt-1 text-sm text-[#6e6e6e]">Provider account eligibility and local health state used by the gateway scheduler.</p>
  </div>
  <dl class="mt-4 grid gap-3 sm:grid-cols-3">
    <div class="rounded-md border border-[#ededed] bg-[#fafafa] p-3">
      <dt class="text-sm font-medium text-[#6e6e6e]">Enabled accounts</dt>
      <dd class="mt-2 text-base font-semibold text-[#0d0d0d]">{providerAccounts.loading ? 'Loading' : enabledProviderAccountCount}</dd>
    </div>
    <div class="rounded-md border border-[#ededed] bg-[#fafafa] p-3">
      <dt class="text-sm font-medium text-[#6e6e6e]">Schedulable accounts</dt>
      <dd class="mt-2 text-base font-semibold text-[#0d0d0d]">{providerAccounts.loading ? 'Loading' : schedulableAccounts.length}</dd>
    </div>
    <div class="rounded-md border border-[#ededed] bg-[#fafafa] p-3">
      <dt class="text-sm font-medium text-[#6e6e6e]">Blocked accounts</dt>
      <dd class="mt-2 text-base font-semibold text-[#0d0d0d]">{providerAccounts.loading ? 'Loading' : unschedulableAccountCount}</dd>
    </div>
  </dl>
  <div class="mt-4 rounded-md border border-[#ededed] bg-[#fafafa] p-3">
    <h4 class="text-sm font-semibold text-[#0d0d0d]">Blocked reasons</h4>
    {#if providerAccounts.loading}
      <p class="mt-2 text-sm text-[#6e6e6e]">Loading provider account health...</p>
    {:else if unschedulableAccountSummary.length === 0}
      <p class="mt-2 text-sm text-[#6e6e6e]">No blocked provider accounts.</p>
    {:else}
      <div class="mt-3 flex flex-wrap gap-2">
        {#each unschedulableAccountSummary as item}
          <span class="rounded-md border border-[#e5e5e5] bg-white px-2.5 py-1.5 text-sm text-[#3c3c3c]">
            {item.reasonLabel}: <span class="font-mono text-[#0d0d0d]">{item.count}</span>
          </span>
        {/each}
      </div>
    {/if}
  </div>
</section>
```

- [ ] **Step 4: Run frontend tests**

```bash
cd frontend && bun test src/routes/navigation.test.mjs
cd frontend && bun run check
```

Expected: PASS.

- [ ] **Step 5: Commit frontend panel**

```bash
git add frontend/src/routes/gateway/+page.svelte frontend/src/routes/navigation.test.mjs
git commit -m "feat: summarize gateway scheduling health"
```

### Task 2: Documentation Coverage

**Files:**
- Modify: `backend/internal/gateway/documentation_test.go`
- Modify: `README.md`
- Modify: `deploy/README.md`

- [ ] **Step 1: Write failing docs test**

Add:

```go
func TestGatewayDocumentationMentionsSchedulingHealthSummary(t *testing.T) {
	for _, path := range []string{"../../../README.md", "../../../deploy/README.md"} {
		content, err := os.ReadFile(path)
		if err != nil {
			t.Fatalf("ReadFile(%q) returned error: %v", path, err)
		}
		text := string(content)
		for _, want := range []string{
			"Scheduling health",
			"blocked provider accounts",
			"Blocked reasons",
		} {
			if !strings.Contains(text, want) {
				t.Fatalf("%s missing %q in gateway scheduling health documentation", path, want)
			}
		}
	}
}
```

- [ ] **Step 2: Run red docs test**

```bash
cd backend && GOMODCACHE=/root/Clouds/N2API/.cache/go-mod GOCACHE=/root/Clouds/N2API/.cache/go-build go test ./internal/gateway -run SchedulingHealthSummary
```

Expected: FAIL because the docs do not mention the new summary yet.

- [ ] **Step 3: Update docs**

Add one sentence near Gateway Runtime Limits in both README files:

```markdown
Gateway management also includes **Scheduling health**, which summarizes enabled, schedulable, and blocked provider accounts; **Blocked reasons** groups disabled, expired, rate-limited, and circuit-open exits so account-pool pressure is visible without opening the full Provider accounts table.
```

- [ ] **Step 4: Run docs test**

```bash
cd backend && GOMODCACHE=/root/Clouds/N2API/.cache/go-mod GOCACHE=/root/Clouds/N2API/.cache/go-build go test ./internal/gateway -run SchedulingHealthSummary
```

Expected: PASS.

- [ ] **Step 5: Commit docs**

```bash
git add backend/internal/gateway/documentation_test.go README.md deploy/README.md
git commit -m "docs: document gateway scheduling health"
```

### Task 3: Full Verification

**Files:**
- No code changes.

- [ ] **Step 1: Run whitespace check**

```bash
git diff --check
```

Expected: PASS with no output.

- [ ] **Step 2: Run backend tests**

```bash
cd backend && GOMODCACHE=/root/Clouds/N2API/.cache/go-mod GOCACHE=/root/Clouds/N2API/.cache/go-build go test ./...
```

Expected: PASS.

- [ ] **Step 3: Run frontend tests and checks**

```bash
cd frontend && bun test src/routes/navigation.test.mjs
cd frontend && bun run check
cd frontend && bun run build
```

Expected: PASS.

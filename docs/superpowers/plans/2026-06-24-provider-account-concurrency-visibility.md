# Provider Account Concurrency Visibility Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Show current active provider-account concurrency and the effective concurrency limit in the admin Provider accounts surface.

**Architecture:** The gateway account limiter exposes a mutex-protected read-only snapshot. The admin HTTP layer enriches provider account JSON using that snapshot plus gateway settings. The frontend renders the new fields as an informational readout next to the existing editable Max concurrency input.

**Tech Stack:** Go backend, existing net/http admin API, SvelteKit admin UI, Bun tests.

---

### Task 1: Gateway Account Concurrency Snapshot

**Files:**
- Modify: `backend/internal/gateway/proxy.go`
- Modify: `backend/internal/gateway/proxy_test.go`

- [ ] **Step 1: Write failing limiter test**

Add a test near existing concurrency limiter tests:

```go
func TestAccountConcurrencyLimiterSnapshotIsImmutable(t *testing.T) {
	limiter := newAccountConcurrencyLimiter()
	releaseOne, ok := limiter.Acquire(7, 2)
	if !ok {
		t.Fatal("first acquire returned false")
	}
	defer releaseOne()
	releaseTwo, ok := limiter.Acquire(7, 2)
	if !ok {
		t.Fatal("second acquire returned false")
	}
	defer releaseTwo()

	snapshot := limiter.Snapshot()
	if snapshot[7] != 2 {
		t.Fatalf("snapshot[7] = %d, want 2", snapshot[7])
	}
	snapshot[7] = 99
	if got := limiter.Snapshot()[7]; got != 2 {
		t.Fatalf("mutated snapshot changed limiter count to %d, want 2", got)
	}
}
```

- [ ] **Step 2: Run red test**

Run:

```bash
cd backend && GOMODCACHE=/root/Clouds/N2API/.cache/go-mod GOCACHE=/root/Clouds/N2API/.cache/go-build go test ./internal/gateway -run AccountConcurrencyLimiterSnapshot
```

Expected: FAIL because `Snapshot` is undefined.

- [ ] **Step 3: Implement snapshot**

Add:

```go
type AccountConcurrencySnapshotProvider interface {
	AccountConcurrencySnapshot() map[int64]int
}

func (p *Proxy) AccountConcurrencySnapshot() map[int64]int {
	if p.accountLimiter == nil {
		return map[int64]int{}
	}
	return p.accountLimiter.Snapshot()
}

func (l *accountConcurrencyLimiter) Snapshot() map[int64]int {
	l.mu.Lock()
	defer l.mu.Unlock()
	snapshot := make(map[int64]int, len(l.active))
	for accountID, count := range l.active {
		if count > 0 {
			snapshot[accountID] = count
		}
	}
	return snapshot
}
```

- [ ] **Step 4: Run gateway targeted tests**

Run:

```bash
cd backend && GOMODCACHE=/root/Clouds/N2API/.cache/go-mod GOCACHE=/root/Clouds/N2API/.cache/go-build go test ./internal/gateway -run 'AccountConcurrencyLimiterSnapshot|AccountConcurrency'
```

Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add backend/internal/gateway/proxy.go backend/internal/gateway/proxy_test.go
git commit -m "feat: expose account concurrency snapshot"
```

### Task 2: Enrich Provider Account Admin Responses

**Files:**
- Modify: `backend/internal/httpapi/server.go`
- Modify: `backend/internal/httpapi/server_test.go`
- Modify: `backend/cmd/n2api/main.go`
- Modify: `backend/cmd/n2api/main_test.go`

- [ ] **Step 1: Write failing HTTP test**

Extend `TestAdminCanListUnifiedProviderAccounts` or add a focused test that wires:

```go
gateway := &fakeGatewayHandler{accountConcurrency: map[int64]int{7: 2}}
providers.accounts = []provider.Account{{ID: 7, Provider: "openai", DisplayName: "Busy", MaxConcurrentRequests: 3}}
```

Assert JSON account fields:

```go
if body.Accounts[0].CurrentConcurrentRequests != 2 || body.Accounts[0].EffectiveMaxConcurrentRequests != 3 {
	t.Fatalf("concurrency fields = %+v", body.Accounts[0])
}
```

Also cover inheritance from gateway settings: account override `0`, settings per-account limit `5`, effective `5`.

- [ ] **Step 2: Run red test**

Run:

```bash
cd backend && GOMODCACHE=/root/Clouds/N2API/.cache/go-mod GOCACHE=/root/Clouds/N2API/.cache/go-build go test ./internal/httpapi -run ProviderAccountConcurrency
```

Expected: FAIL because account JSON has no runtime concurrency fields.

- [ ] **Step 3: Implement response enrichment**

Add a small HTTP response DTO for provider accounts that embeds/copies `provider.Account` and adds:

```go
CurrentConcurrentRequests      int `json:"currentConcurrentRequests"`
EffectiveMaxConcurrentRequests int `json:"effectiveMaxConcurrentRequests"`
```

Add an optional interface on the gateway handler:

```go
type accountConcurrencySnapshotProvider interface {
	AccountConcurrencySnapshot() map[int64]int
}
```

In list-account handler, load gateway settings and snapshot, then map accounts through:

```go
effective := account.MaxConcurrentRequests
if effective <= 0 {
	effective = settings.MaxConcurrentRequestsPerAccount
}
```

Update `cmd/n2api/main.go` wiring if needed so the proxy instance is passed to HTTP server and satisfies the snapshot interface.

- [ ] **Step 4: Run HTTP and main tests**

Run:

```bash
cd backend && GOMODCACHE=/root/Clouds/N2API/.cache/go-mod GOCACHE=/root/Clouds/N2API/.cache/go-build go test ./internal/httpapi -run ProviderAccountConcurrency
cd backend && GOMODCACHE=/root/Clouds/N2API/.cache/go-mod GOCACHE=/root/Clouds/N2API/.cache/go-build go test ./cmd/n2api
```

Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add backend/internal/httpapi/server.go backend/internal/httpapi/server_test.go backend/cmd/n2api/main.go backend/cmd/n2api/main_test.go
git commit -m "feat: expose provider account concurrency state"
```

### Task 3: Render Provider Account Active Concurrency

**Files:**
- Modify: `frontend/src/lib/admin-state.svelte.js`
- Modify: `frontend/src/routes/providers/+page.svelte`
- Modify: `frontend/src/routes/providers/provider-page.test.mjs`

- [ ] **Step 1: Write failing frontend source test**

Add assertions:

```js
assert.match(adminStateSource, /currentConcurrentRequests/);
assert.match(adminStateSource, /effectiveMaxConcurrentRequests/);
assert.match(source, /Active/);
assert.match(source, /concurrencyLimitLabel/);
assert.match(source, /account\.currentConcurrentRequests/);
assert.match(source, /account\.effectiveMaxConcurrentRequests/);
```

- [ ] **Step 2: Run red frontend test**

Run:

```bash
cd frontend && bun test src/routes/providers/provider-page.test.mjs
```

Expected: FAIL because fields and UI readout are missing.

- [ ] **Step 3: Implement frontend fields and readout**

Add JSDoc fields to `ProviderAccount`:

```js
 * @property {number} currentConcurrentRequests
 * @property {number} effectiveMaxConcurrentRequests
```

Add helper in `frontend/src/routes/providers/+page.svelte`:

```js
function concurrencyLimitLabel(value) {
  const limit = Number(value || 0);
  return limit > 0 ? String(limit) : 'unlimited';
}
```

In the **Max concurrency** cell, below the input, render:

```svelte
<p class="mt-1 text-xs text-[#6e6e6e]">
  Active {account.currentConcurrentRequests || 0} / {concurrencyLimitLabel(account.effectiveMaxConcurrentRequests)}
</p>
```

- [ ] **Step 4: Run frontend checks**

Run:

```bash
cd frontend && bun test src/routes/providers/provider-page.test.mjs
cd frontend && bun run check
```

Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add frontend/src/lib/admin-state.svelte.js frontend/src/routes/providers/+page.svelte frontend/src/routes/providers/provider-page.test.mjs
git commit -m "feat: show provider account active concurrency"
```

### Task 4: Document Runtime Concurrency Visibility

**Files:**
- Modify: `backend/internal/gateway/documentation_test.go`
- Modify: `README.md`
- Modify: `deploy/README.md`

- [ ] **Step 1: Write failing documentation test**

Add:

```go
func TestGatewayDocumentationMentionsProviderAccountActiveConcurrency(t *testing.T) {
	for _, path := range []string{"../../../README.md", "../../../deploy/README.md"} {
		content, err := os.ReadFile(path)
		if err != nil {
			t.Fatalf("ReadFile(%q) returned error: %v", path, err)
		}
		text := string(content)
		for _, want := range []string{
			"active concurrency",
			"process-local",
			"unlimited",
		} {
			if !strings.Contains(text, want) {
				t.Fatalf("%s missing %q in provider account active concurrency documentation", path, want)
			}
		}
	}
}
```

- [ ] **Step 2: Run red docs test**

Run:

```bash
cd backend && GOMODCACHE=/root/Clouds/N2API/.cache/go-mod GOCACHE=/root/Clouds/N2API/.cache/go-build go test ./internal/gateway -run ProviderAccountActiveConcurrency
```

Expected: FAIL until docs are updated.

- [ ] **Step 3: Update docs**

Add to README and deploy README provider account/concurrency sections:

```markdown
Provider account rows also show active concurrency beside the configured max. The count is process-local runtime state and resets when the service restarts. An effective limit of `0` is shown as unlimited.
```

- [ ] **Step 4: Run docs test green**

Run:

```bash
cd backend && GOMODCACHE=/root/Clouds/N2API/.cache/go-mod GOCACHE=/root/Clouds/N2API/.cache/go-build go test ./internal/gateway -run ProviderAccountActiveConcurrency
```

Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add backend/internal/gateway/documentation_test.go README.md deploy/README.md
git commit -m "docs: document account concurrency visibility"
```

### Final Verification

- [ ] Run `git diff --check`.
- [ ] Run `cd backend && GOMODCACHE=/root/Clouds/N2API/.cache/go-mod GOCACHE=/root/Clouds/N2API/.cache/go-build go test ./internal/gateway ./internal/httpapi ./internal/provider ./internal/store ./cmd/n2api`.
- [ ] Run `cd frontend && bun test src/routes/providers/provider-page.test.mjs`.
- [ ] Run `cd frontend && bun run check`.
- [ ] Run `cd frontend && bun run build`.
- [ ] Run `git status --short`.

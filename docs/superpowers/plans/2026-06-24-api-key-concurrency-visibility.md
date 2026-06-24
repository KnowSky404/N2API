# API Key Concurrency Visibility Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Show current active API-key concurrency and the effective key concurrency cap in the admin API Keys surface.

**Architecture:** The gateway key limiter exposes a mutex-protected read-only snapshot. The admin HTTP layer enriches API key JSON using that snapshot plus Gateway Settings. The frontend renders the new fields as an informational readout next to existing key limit controls.

**Tech Stack:** Go backend, existing net/http admin API, SvelteKit admin UI, Bun tests.

---

### Task 1: Gateway API Key Concurrency Snapshot

**Files:**
- Modify: `backend/internal/gateway/proxy.go`
- Modify: `backend/internal/gateway/proxy_test.go`

- [ ] **Step 1: Write failing key limiter test**

Add near existing API key concurrency tests:

```go
func TestAPIKeyConcurrencyLimiterSnapshotIsImmutable(t *testing.T) {
	limiter := newConcurrencyLimiter()
	releaseOne, ok := limiter.Acquire(42, 2)
	if !ok {
		t.Fatal("first acquire returned false")
	}
	defer releaseOne()
	releaseTwo, ok := limiter.Acquire(42, 2)
	if !ok {
		t.Fatal("second acquire returned false")
	}
	defer releaseTwo()

	snapshot := limiter.Snapshot()
	if snapshot[42] != 2 {
		t.Fatalf("snapshot[42] = %d, want 2", snapshot[42])
	}
	snapshot[42] = 99
	if got := limiter.Snapshot()[42]; got != 2 {
		t.Fatalf("mutated snapshot changed limiter count to %d, want 2", got)
	}
}
```

- [ ] **Step 2: Run red test**

Run:

```bash
cd backend && GOMODCACHE=/root/Clouds/N2API/.cache/go-mod GOCACHE=/root/Clouds/N2API/.cache/go-build go test ./internal/gateway -run APIKeyConcurrencyLimiterSnapshot
```

Expected: FAIL because `Snapshot` is undefined.

- [ ] **Step 3: Implement snapshot**

Add:

```go
type APIKeyConcurrencySnapshotProvider interface {
	APIKeyConcurrencySnapshot() map[int64]int
}

func (p *Proxy) APIKeyConcurrencySnapshot() map[int64]int {
	if p.keyLimiter == nil {
		return map[int64]int{}
	}
	return p.keyLimiter.Snapshot()
}

func (l *concurrencyLimiter) Snapshot() map[int64]int {
	l.mu.Lock()
	defer l.mu.Unlock()
	snapshot := make(map[int64]int, len(l.active))
	for id, count := range l.active {
		if count > 0 {
			snapshot[id] = count
		}
	}
	return snapshot
}
```

- [ ] **Step 4: Run gateway targeted tests**

Run:

```bash
cd backend && GOMODCACHE=/root/Clouds/N2API/.cache/go-mod GOCACHE=/root/Clouds/N2API/.cache/go-build go test ./internal/gateway -run 'APIKeyConcurrencyLimiterSnapshot|APIKeyConcurrency'
```

Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add backend/internal/gateway/proxy.go backend/internal/gateway/proxy_test.go
git commit -m "feat: expose api key concurrency snapshot"
```

### Task 2: Enrich API Key Admin Responses

**Files:**
- Modify: `backend/internal/httpapi/server.go`
- Modify: `backend/internal/httpapi/server_test.go`

- [ ] **Step 1: Write failing HTTP test**

Add:

```go
func TestListAPIKeysIncludesConcurrencyState(t *testing.T) {
	admins := newFakeAdminService()
	admins.gatewaySettings.MaxConcurrentRequestsPerKey = 2
	gateway := &fakeGatewayHandler{apiKeyConcurrency: map[int64]int{7: 2}}
	server := NewServer(config.Config{}, staticHealth{}, admins, newFakeProviderService(), gateway)
	req := httptest.NewRequest(http.MethodGet, "/api/admin/keys", nil)
	req.AddCookie(&http.Cookie{Name: "n2api_admin_session", Value: "valid-session"})
	recorder := httptest.NewRecorder()

	server.ServeHTTP(recorder, req)

	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d body=%s, want 200", recorder.Code, recorder.Body.String())
	}
	var body struct {
		Keys []struct {
			admin.APIKey
			CurrentConcurrentRequests      int  `json:"currentConcurrentRequests"`
			EffectiveMaxConcurrentRequests int  `json:"effectiveMaxConcurrentRequests"`
			ConcurrencyBlocked             bool `json:"concurrencyBlocked"`
		} `json:"keys"`
	}
	if err := json.Unmarshal(recorder.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode body: %v", err)
	}
	if len(body.Keys) != 1 {
		t.Fatalf("keys = %+v, want one key", body.Keys)
	}
	if body.Keys[0].CurrentConcurrentRequests != 2 || body.Keys[0].EffectiveMaxConcurrentRequests != 2 || !body.Keys[0].ConcurrencyBlocked {
		t.Fatalf("key concurrency = %+v, want current 2 effective 2 blocked", body.Keys[0])
	}
}
```

- [ ] **Step 2: Run red HTTP test**

Run:

```bash
cd backend && GOMODCACHE=/root/Clouds/N2API/.cache/go-mod GOCACHE=/root/Clouds/N2API/.cache/go-build go test ./internal/httpapi -run ListAPIKeysIncludesConcurrencyState
```

Expected: FAIL because key JSON has no runtime concurrency fields.

- [ ] **Step 3: Implement response enrichment**

Add HTTP response DTO:

```go
type apiKeyResponse struct {
	admin.APIKey
	CurrentConcurrentRequests      int  `json:"currentConcurrentRequests"`
	EffectiveMaxConcurrentRequests int  `json:"effectiveMaxConcurrentRequests"`
	ConcurrencyBlocked             bool `json:"concurrencyBlocked"`
}
```

Add a local optional interface:

```go
type APIKeyConcurrencySnapshotProvider interface {
	APIKeyConcurrencySnapshot() map[int64]int
}
```

In `NewServer`, derive:

```go
	apiKeyConcurrencySource, _ := gateway.(APIKeyConcurrencySnapshotProvider)
```

Update the key list handler to load Gateway Settings, snapshot, and return enriched keys.

- [ ] **Step 4: Run HTTP tests**

Run:

```bash
cd backend && GOMODCACHE=/root/Clouds/N2API/.cache/go-mod GOCACHE=/root/Clouds/N2API/.cache/go-build go test ./internal/httpapi -run 'ListAPIKeys|APIKey'
```

Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add backend/internal/httpapi/server.go backend/internal/httpapi/server_test.go
git commit -m "feat: expose api key concurrency state"
```

### Task 3: Render API Key Active Concurrency

**Files:**
- Modify: `frontend/src/lib/admin-state.svelte.js`
- Modify: `frontend/src/routes/api-keys/+page.svelte`
- Modify: `frontend/src/routes/navigation.test.mjs`

- [ ] **Step 1: Write failing frontend source test**

In API key runtime-limit test assertions, add:

```js
assert.match(adminState, /currentConcurrentRequests/);
assert.match(adminState, /effectiveMaxConcurrentRequests/);
assert.match(adminState, /concurrencyBlocked/);
assert.match(apiKeysPage, /keyConcurrencyLimitLabel/);
assert.match(apiKeysPage, /Active/);
assert.match(apiKeysPage, /Concurrency full/);
```

- [ ] **Step 2: Run red frontend test**

Run:

```bash
cd frontend && bun test src/routes/navigation.test.mjs
```

Expected: FAIL because fields and UI readout are missing.

- [ ] **Step 3: Implement frontend fields and readout**

Add JSDoc fields to `APIKey`:

```js
 * @property {number} currentConcurrentRequests
 * @property {number} effectiveMaxConcurrentRequests
 * @property {boolean} concurrencyBlocked
```

Add helper in `frontend/src/routes/api-keys/+page.svelte`:

```js
function keyConcurrencyLimitLabel(value) {
  const limit = Number(value ?? 0);
  return limit > 0 ? String(limit) : 'unlimited';
}
```

Render near each key's limit controls:

```svelte
<p class="mt-2 text-xs text-[#6e6e6e]">
  Active {key.currentConcurrentRequests || 0} / {keyConcurrencyLimitLabel(key.effectiveMaxConcurrentRequests)}
</p>
{#if key.concurrencyBlocked}
  <p class="mt-1 text-xs font-medium text-amber-700">Concurrency full</p>
{/if}
```

- [ ] **Step 4: Run frontend tests and checks**

Run:

```bash
cd frontend && bun test src/routes/navigation.test.mjs
cd frontend && bun run check
```

Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add frontend/src/lib/admin-state.svelte.js frontend/src/routes/api-keys/+page.svelte frontend/src/routes/navigation.test.mjs
git commit -m "feat: show api key active concurrency"
```

### Task 4: Document API Key Runtime Concurrency

**Files:**
- Modify: `backend/internal/gateway/documentation_test.go`
- Modify: `README.md`
- Modify: `deploy/README.md`

- [ ] **Step 1: Write failing documentation test**

Add:

```go
func TestGatewayDocumentationMentionsAPIKeyActiveConcurrency(t *testing.T) {
	for _, path := range []string{"../../../README.md", "../../../deploy/README.md"} {
		content, err := os.ReadFile(path)
		if err != nil {
			t.Fatalf("ReadFile(%q) returned error: %v", path, err)
		}
		text := string(content)
		for _, want := range []string{
			"API Keys page shows active concurrency",
			"process-local",
			"Concurrency full",
		} {
			if !strings.Contains(text, want) {
				t.Fatalf("%s missing %q in API key active concurrency documentation", path, want)
			}
		}
	}
}
```

- [ ] **Step 2: Run red documentation test**

Run:

```bash
cd backend && GOMODCACHE=/root/Clouds/N2API/.cache/go-mod GOCACHE=/root/Clouds/N2API/.cache/go-build go test ./internal/gateway -run APIKeyActiveConcurrency
```

Expected: FAIL because docs do not mention API key active concurrency.

- [ ] **Step 3: Update README files**

Add:

```markdown
The API Keys page shows active concurrency for each client key as process-local runtime state. Keys at a positive effective cap are marked **Concurrency full**.
```

- [ ] **Step 4: Run documentation test**

Run:

```bash
cd backend && GOMODCACHE=/root/Clouds/N2API/.cache/go-mod GOCACHE=/root/Clouds/N2API/.cache/go-build go test ./internal/gateway -run APIKeyActiveConcurrency
```

Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add backend/internal/gateway/documentation_test.go README.md deploy/README.md
git commit -m "docs: document api key concurrency visibility"
```

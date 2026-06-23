# Provider Auto Test Status Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Expose provider account auto-test runner status in Gateway management.

**Architecture:** Track the auto-test runner's in-memory status with a mutex-protected snapshot. Pass the runner into the HTTP API server as an optional status source and include the snapshot in `GET /api/admin/gateway-settings`; render it in the existing Svelte Gateway page.

**Tech Stack:** Go `net/http`, `sync.Mutex`, SvelteKit, Bun tests.

---

## File Structure

- Modify `backend/internal/provider/auto_test_runner.go`: add `AutoTestStatus`, mutex-protected status snapshot, and `ProviderAccountAutoTestStatus()`.
- Modify `backend/internal/provider/auto_test_runner_test.go`: add runner status tests.
- Modify `backend/internal/httpapi/server.go`: parse a status source server option and include status in gateway settings response.
- Modify `backend/internal/httpapi/server_test.go`: add gateway settings response test with fake status source.
- Modify `backend/cmd/n2api/main.go`: pass `autoTestRunner` to `httpapi.NewServer`.
- Modify `backend/cmd/n2api/main_test.go`: assert the runner is passed as a server option.
- Modify `frontend/src/lib/admin-state.svelte.js`: normalize `providerAccountAutoTestStatus`.
- Modify `frontend/src/routes/gateway/+page.svelte`: render auto-test status near existing controls.
- Modify `frontend/src/routes/navigation.test.mjs`: add source assertions for the Gateway page and admin state.
- Modify `backend/internal/gateway/documentation_test.go`, `README.md`, and `deploy/README.md`: document the status.

### Task 1: Auto Test Runner Status Snapshot

**Files:**
- Modify: `backend/internal/provider/auto_test_runner_test.go`
- Modify: `backend/internal/provider/auto_test_runner.go`

- [ ] **Step 1: Write failing runner status tests**

Add tests in `backend/internal/provider/auto_test_runner_test.go`:

```go
func TestAutoTestRunnerStatusStartsEmpty(t *testing.T)
func TestAutoTestRunnerStatusTracksSuccessfulCycle(t *testing.T)
func TestAutoTestRunnerStatusTracksFailedCycle(t *testing.T)
```

The success test should call `runner.runCycle(context.Background())` against a fake service returning two accounts and assert:

```go
status := runner.ProviderAccountAutoTestStatus()
if status.Running || status.LastStartedAt == nil || status.LastFinishedAt == nil || status.LastAccountCount != 2 || status.LastError != "" {
	t.Fatalf("status = %+v, want successful completed cycle", status)
}
```

The failure test should use a fake service returning `errors.New("probe failed")` and assert `LastError == "probe failed"`.

- [ ] **Step 2: Run test to verify red**

```bash
cd backend && GOMODCACHE=/root/Clouds/N2API/.cache/go-mod GOCACHE=/root/Clouds/N2API/.cache/go-build go test ./internal/provider -run AutoTestRunnerStatus
```

Expected: FAIL because `ProviderAccountAutoTestStatus` does not exist.

- [ ] **Step 3: Implement status snapshot**

In `backend/internal/provider/auto_test_runner.go`, add:

```go
type AutoTestStatus struct {
	Running          bool       `json:"running"`
	LastStartedAt    *time.Time `json:"lastStartedAt,omitempty"`
	LastFinishedAt   *time.Time `json:"lastFinishedAt,omitempty"`
	LastAccountCount int        `json:"lastAccountCount"`
	LastError        string     `json:"lastError"`
}
```

Add fields to `AutoTestRunner`:

```go
	statusMu sync.Mutex
	status   AutoTestStatus
```

Add:

```go
func (r *AutoTestRunner) ProviderAccountAutoTestStatus() AutoTestStatus {
	if r == nil {
		return AutoTestStatus{}
	}
	r.statusMu.Lock()
	defer r.statusMu.Unlock()
	return r.status
}
```

Update `runCycle` before and after `TestAccounts`.

- [ ] **Step 4: Run test to verify green**

Run the same targeted `go test` command. Expected: PASS.

- [ ] **Step 5: Commit runner status**

```bash
git add backend/internal/provider/auto_test_runner.go backend/internal/provider/auto_test_runner_test.go
git commit -m "feat: track provider auto test status"
```

### Task 2: Gateway Settings API and Production Wiring

**Files:**
- Modify: `backend/internal/httpapi/server_test.go`
- Modify: `backend/internal/httpapi/server.go`
- Modify: `backend/cmd/n2api/main_test.go`
- Modify: `backend/cmd/n2api/main.go`

- [ ] **Step 1: Write failing HTTP API test**

Add a fake status source in `backend/internal/httpapi/server_test.go`:

```go
type fakeAutoTestStatusSource struct {
	status provider.AutoTestStatus
}

func (s fakeAutoTestStatusSource) ProviderAccountAutoTestStatus() provider.AutoTestStatus {
	return s.status
}
```

Add a test that calls:

```go
server := NewServer(config.Config{}, staticHealth{}, admins, newFakeProviderService(), fakeAutoTestStatusSource{status: provider.AutoTestStatus{Running: true, LastAccountCount: 3}})
```

Assert `GET /api/admin/gateway-settings` returns:

```json
"providerAccountAutoTestStatus":{"running":true,"lastAccountCount":3}
```

- [ ] **Step 2: Write failing production wiring test**

Extend `backend/cmd/n2api/main_test.go` to assert source contains:

```go
"autoTestRunner,"
"os.DirFS(\"frontend/build\")"
```

near the `httpapi.NewServer` call, proving the runner is passed as an option.

- [ ] **Step 3: Run tests to verify red**

```bash
cd backend && GOMODCACHE=/root/Clouds/N2API/.cache/go-mod GOCACHE=/root/Clouds/N2API/.cache/go-build go test ./internal/httpapi -run AutoTestStatus
cd backend && GOMODCACHE=/root/Clouds/N2API/.cache/go-mod GOCACHE=/root/Clouds/N2API/.cache/go-build go test ./cmd/n2api -run AutoTestRunner
```

Expected: FAIL because the API response and production wiring do not include the status source.

- [ ] **Step 4: Implement API option and response shape**

In `backend/internal/httpapi/server.go`, add:

```go
type ProviderAccountAutoTestStatusSource interface {
	ProviderAccountAutoTestStatus() provider.AutoTestStatus
}
```

Change `parseServerOptions` to return `(http.Handler, fs.FS, ProviderAccountAutoTestStatusSource)`.

In `GET /api/admin/gateway-settings`, write a response struct embedding `admin.GatewaySettings` plus:

```go
ProviderAccountAutoTestStatus provider.AutoTestStatus `json:"providerAccountAutoTestStatus,omitempty"`
```

Set the field when the status source is non-nil.

- [ ] **Step 5: Wire production**

Change `backend/cmd/n2api/main.go`:

```go
Handler: httpapi.NewServer(cfg, pool, adminService, providerService, gatewayProxy, autoTestRunner, os.DirFS("frontend/build")),
```

- [ ] **Step 6: Run tests to verify green**

Run both targeted commands from Step 3. Expected: PASS.

- [ ] **Step 7: Commit API and wiring**

```bash
git add backend/internal/httpapi/server.go backend/internal/httpapi/server_test.go backend/cmd/n2api/main.go backend/cmd/n2api/main_test.go
git commit -m "feat: expose provider auto test status"
```

### Task 3: Gateway UI Status Display

**Files:**
- Modify: `frontend/src/lib/admin-state.svelte.js`
- Modify: `frontend/src/routes/gateway/+page.svelte`
- Modify: `frontend/src/routes/navigation.test.mjs`

- [ ] **Step 1: Write failing frontend source tests**

Add assertions in `frontend/src/routes/navigation.test.mjs` that the admin state contains:

```js
providerAccountAutoTestStatus
lastStartedAt
lastFinishedAt
lastAccountCount
lastError
```

Add assertions that the Gateway page contains:

```js
Auto-test status
Last finished
Accounts tested
Last error
Not run yet
```

- [ ] **Step 2: Run frontend test to verify red**

```bash
cd frontend && bun test src/routes/navigation.test.mjs
```

Expected: FAIL because the status fields and labels do not exist.

- [ ] **Step 3: Normalize admin state**

In `loadGatewaySettings`, normalize:

```js
providerAccountAutoTestStatus: {
  running: Boolean(payload.providerAccountAutoTestStatus?.running),
  lastStartedAt: payload.providerAccountAutoTestStatus?.lastStartedAt ?? null,
  lastFinishedAt: payload.providerAccountAutoTestStatus?.lastFinishedAt ?? null,
  lastAccountCount: Number(payload.providerAccountAutoTestStatus?.lastAccountCount ?? 0),
  lastError: String(payload.providerAccountAutoTestStatus?.lastError ?? '')
}
```

- [ ] **Step 4: Render Gateway page status**

Under the existing Provider account auto tests controls, render a compact status row:

```svelte
<div class="mt-4 grid gap-2 text-sm text-[#3c3c3c] sm:grid-cols-2 lg:grid-cols-4">
  <div>
    <p class="text-xs font-medium text-[#6e6e6e]">Auto-test status</p>
    <p class="mt-1 font-semibold text-[#0d0d0d]">
      {gatewaySettings.data.providerAccountAutoTestStatus?.running ? 'Running' : 'Idle'}
    </p>
  </div>
  <div>
    <p class="text-xs font-medium text-[#6e6e6e]">Last finished</p>
    <p class="mt-1 font-semibold text-[#0d0d0d]">
      {gatewaySettings.data.providerAccountAutoTestStatus?.lastFinishedAt
        ? formatDate(gatewaySettings.data.providerAccountAutoTestStatus.lastFinishedAt)
        : 'Not run yet'}
    </p>
  </div>
  <div>
    <p class="text-xs font-medium text-[#6e6e6e]">Accounts tested</p>
    <p class="mt-1 font-semibold text-[#0d0d0d]">
      {gatewaySettings.data.providerAccountAutoTestStatus?.lastAccountCount ?? 0}
    </p>
  </div>
  <div>
    <p class="text-xs font-medium text-[#6e6e6e]">Last error</p>
    <p class="mt-1 font-semibold text-[#0d0d0d]">
      {gatewaySettings.data.providerAccountAutoTestStatus?.lastError || 'None'}
    </p>
  </div>
</div>
```

Use `formatDate(...)` for timestamps and `Not run yet` for missing `lastFinishedAt`.

- [ ] **Step 5: Run frontend tests to verify green**

```bash
cd frontend && bun test src/routes/navigation.test.mjs
cd frontend && bun run check
```

Expected: PASS.

- [ ] **Step 6: Commit UI**

```bash
git add frontend/src/lib/admin-state.svelte.js frontend/src/routes/gateway/+page.svelte frontend/src/routes/navigation.test.mjs
git commit -m "feat: show provider auto test status"
```

### Task 4: Docs and Full Verification

**Files:**
- Modify: `backend/internal/gateway/documentation_test.go`
- Modify: `README.md`
- Modify: `deploy/README.md`

- [ ] **Step 1: Add failing docs test**

Require README and deploy README to mention:

```go
"Auto-test status"
"last finished"
"last error"
```

- [ ] **Step 2: Update docs**

Document that Gateway management shows the auto-test runtime status, last finished time, account count, and last error.

- [ ] **Step 3: Run docs test**

```bash
cd backend && GOMODCACHE=/root/Clouds/N2API/.cache/go-mod GOCACHE=/root/Clouds/N2API/.cache/go-build go test ./internal/gateway -run AutoTestStatus
```

Expected: PASS after docs update.

- [ ] **Step 4: Commit docs**

```bash
git add backend/internal/gateway/documentation_test.go README.md deploy/README.md
git commit -m "docs: document provider auto test status"
```

- [ ] **Step 5: Full verification**

```bash
git diff --check
cd backend && GOMODCACHE=/root/Clouds/N2API/.cache/go-mod GOCACHE=/root/Clouds/N2API/.cache/go-build go test ./...
cd frontend && bun test src/routes/navigation.test.mjs
cd frontend && bun run check
cd frontend && bun run build
```

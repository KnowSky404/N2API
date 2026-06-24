# Request Log Filtering Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add server-side Request Logs search and status-class filtering for day-to-day gateway troubleshooting.

**Architecture:** Introduce `admin.RequestLogFilter` as the boundary object from HTTP to service to store. The service normalizes filter values and the store appends parameterized SQL `WHERE` conditions before ordering and limiting logs.

**Tech Stack:** Go admin service, Go HTTP handlers, PostgreSQL via pgx, SvelteKit admin UI, Bun verification.

---

### Task 1: Plan And Scope

**Files:**
- Create: `docs/superpowers/specs/2026-06-24-request-log-filtering-design.md`
- Create: `docs/superpowers/plans/2026-06-24-request-log-filtering.md`

- [ ] **Step 1: Write the design and implementation plan**

Capture scope, validation, files, and verification commands in the two docs.

- [ ] **Step 2: Commit the docs**

Run:

```bash
git add docs/superpowers/specs/2026-06-24-request-log-filtering-design.md docs/superpowers/plans/2026-06-24-request-log-filtering.md
git commit -m "docs: plan request log filtering"
```

Expected: commit succeeds with only the two docs staged.

### Task 2: Backend Filter Contract

**Files:**
- Modify: `backend/internal/admin/service.go`
- Modify: `backend/internal/admin/service_test.go`

- [ ] **Step 1: Add failing service tests**

Update `TestListRequestLogsClampsLimitAndReturnsRepositoryLogs` to call:

```go
logs, err := service.ListRequestLogs(context.Background(), RequestLogFilter{
	Query:       "  gpt-5  ",
	StatusClass: "server_error",
})
```

Assert the repository saw:

```go
if repo.lastLogFilter.Limit != 50 {
	t.Fatalf("repository limit = %d, want default 50", repo.lastLogFilter.Limit)
}
if repo.lastLogFilter.Query != "gpt-5" {
	t.Fatalf("repository query = %q, want gpt-5", repo.lastLogFilter.Query)
}
if repo.lastLogFilter.StatusClass != RequestLogStatusServerError {
	t.Fatalf("repository status class = %q, want %q", repo.lastLogFilter.StatusClass, RequestLogStatusServerError)
}
```

Add invalid-filter assertions for an unknown status class and a query longer than 200 characters.

- [ ] **Step 2: Run the failing service test**

Run:

```bash
cd backend && GOMODCACHE=/root/Clouds/N2API/.cache/go-mod GOCACHE=/root/Clouds/N2API/.cache/go-build go test ./internal/admin -run 'TestListRequestLogs' -count=1
```

Expected: FAIL until `RequestLogFilter` and the new signature exist.

- [ ] **Step 3: Implement `RequestLogFilter` normalization**

Add:

```go
const (
	RequestLogStatusAll         = "all"
	RequestLogStatusSuccess     = "success"
	RequestLogStatusClientError = "client_error"
	RequestLogStatusServerError = "server_error"
	maxRequestLogQueryLen       = 200
)

type RequestLogFilter struct {
	Limit       int
	Query       string
	StatusClass string
}
```

Change repository and service signatures to `ListRequestLogs(ctx context.Context, filter RequestLogFilter)` and normalize limit/query/status in the service.

- [ ] **Step 4: Run the service tests**

Run the same `go test ./internal/admin -run 'TestListRequestLogs' -count=1` command.

Expected: PASS.

- [ ] **Step 5: Commit backend contract**

Commit service, tests, and interface signature changes after compile-focused downstream updates are made in Task 3.

### Task 3: Backend HTTP And Store Filters

**Files:**
- Modify: `backend/internal/httpapi/server.go`
- Modify: `backend/internal/httpapi/server_test.go`
- Modify: `backend/internal/store/admin.go`
- Modify: `backend/internal/store/admin_test.go`

- [ ] **Step 1: Add failing HTTP and store tests**

Extend `TestListRequestLogsRequiresSessionAndReturnsLogs` to request:

```go
req := httptest.NewRequest(http.MethodGet, "/api/admin/request-logs?limit=20&q=codex&statusClass=server_error", nil)
```

Assert the fake service saw `Limit: 20`, `Query: "codex"`, and `StatusClass: admin.RequestLogStatusServerError`.

Add store source assertions that `admin.go` contains:

```go
"ILIKE '%' || $"
"l.status_code >= 500"
"l.status_code >= 400 AND l.status_code < 500"
"l.status_code >= 200 AND l.status_code < 400"
```

- [ ] **Step 2: Run failing targeted tests**

Run:

```bash
cd backend && GOMODCACHE=/root/Clouds/N2API/.cache/go-mod GOCACHE=/root/Clouds/N2API/.cache/go-build go test ./internal/httpapi ./internal/store -run 'TestListRequestLogs' -count=1
```

Expected: FAIL until handler and store filter SQL are implemented.

- [ ] **Step 3: Implement HTTP params and store SQL**

Parse `q` and `statusClass` in the handler and pass an `admin.RequestLogFilter`.

Build store SQL with a helper that adds `WHERE` conditions and always keeps `LIMIT` parameterized. The free-text condition must use one parameter and `ILIKE '%' || $N || '%'` against safe columns and expressions.

- [ ] **Step 4: Run backend targeted tests**

Run:

```bash
cd backend && GOMODCACHE=/root/Clouds/N2API/.cache/go-mod GOCACHE=/root/Clouds/N2API/.cache/go-build go test ./internal/admin ./internal/httpapi ./internal/store -run 'TestListRequestLogs' -count=1
```

Expected: PASS.

- [ ] **Step 5: Commit backend implementation**

Run:

```bash
git add backend/internal/admin/service.go backend/internal/admin/service_test.go backend/internal/httpapi/server.go backend/internal/httpapi/server_test.go backend/internal/store/admin.go backend/internal/store/admin_test.go
git commit -m "feat: filter request logs"
```

Expected: commit succeeds.

### Task 4: Request Logs UI Controls

**Files:**
- Modify: `frontend/src/lib/admin-state.svelte.js`
- Modify: `frontend/src/routes/request-logs/+page.svelte`
- Modify: `frontend/src/routes/navigation.test.mjs`

- [ ] **Step 1: Add failing frontend source tests**

Add a test that asserts:

```js
assert.match(requestLogsPage, /bind:value=\{requestLogs\.query\}/);
assert.match(requestLogsPage, /bind:value=\{requestLogs\.statusClass\}/);
assert.match(adminState, /params\.set\('q'/);
assert.match(adminState, /params\.set\('statusClass'/);
```

- [ ] **Step 2: Run the failing frontend source test**

Run:

```bash
cd frontend && bun test src/routes/navigation.test.mjs
```

Expected: FAIL until controls and URL params exist.

- [ ] **Step 3: Implement UI filter state and controls**

Add `query` and `statusClass` to `requestLogs`, preserve them across refresh, reset them on logout, build the request URL with `URLSearchParams`, and add compact search/status controls above the logs table.

- [ ] **Step 4: Run frontend source test**

Run:

```bash
cd frontend && bun test src/routes/navigation.test.mjs
```

Expected: PASS.

- [ ] **Step 5: Commit frontend implementation**

Run:

```bash
git add frontend/src/lib/admin-state.svelte.js frontend/src/routes/request-logs/+page.svelte frontend/src/routes/navigation.test.mjs
git commit -m "feat: add request log filters"
```

Expected: commit succeeds.

### Task 5: Final Verification

**Files:**
- Verify all touched files.

- [ ] **Step 1: Run backend targeted tests**

```bash
cd backend && GOMODCACHE=/root/Clouds/N2API/.cache/go-mod GOCACHE=/root/Clouds/N2API/.cache/go-build go test ./internal/admin ./internal/httpapi ./internal/store -run 'TestListRequestLogs' -count=1
```

Expected: PASS.

- [ ] **Step 2: Run full backend tests**

```bash
cd backend && GOMODCACHE=/root/Clouds/N2API/.cache/go-mod GOCACHE=/root/Clouds/N2API/.cache/go-build go test ./...
```

Expected: PASS. If sandbox blocks IPv6 httptest listeners, rerun the same command with elevated permissions.

- [ ] **Step 3: Run frontend checks**

```bash
cd frontend && bun test src/routes/navigation.test.mjs
cd frontend && bun run check
cd frontend && bun run build
```

Expected: PASS.

- [ ] **Step 4: Run diff whitespace check**

```bash
git diff --check
```

Expected: no output.

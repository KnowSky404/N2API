# Request Log Account Filter Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Let admins filter Request Logs exactly by provider account ID so account management and gateway diagnostics connect cleanly.

**Architecture:** Extend the existing `admin.RequestLogFilter` object with `ProviderAccountID`. HTTP parses and validates the optional query parameter before the admin service performs final validation and store SQL adds a parameterized provider-account condition. The Svelte Request Logs page reuses global provider account state to populate a compact dropdown and sends the selected account as a query param.

**Tech Stack:** Go admin service, Go HTTP handlers, PostgreSQL via pgx, SvelteKit admin UI, Bun verification.

---

### Task 1: Plan And Scope

**Files:**
- Create: `docs/superpowers/specs/2026-06-24-request-log-account-filter-design.md`
- Create: `docs/superpowers/plans/2026-06-24-request-log-account-filter.md`

- [ ] **Step 1: Write design and implementation plan**

Document the account-scoped filter behavior, validation, UI behavior, and verification commands.

- [ ] **Step 2: Commit docs**

Run:

```bash
git add docs/superpowers/specs/2026-06-24-request-log-account-filter-design.md docs/superpowers/plans/2026-06-24-request-log-account-filter.md
git commit -m "docs: plan request log account filter"
```

Expected: commit contains only the two docs.

### Task 2: Backend Filter Contract

**Files:**
- Modify: `backend/internal/admin/service.go`
- Modify: `backend/internal/admin/service_test.go`

- [ ] **Step 1: Write failing service tests**

In `TestListRequestLogsClampsLimitAndReturnsRepositoryLogs`, call:

```go
logs, err := service.ListRequestLogs(context.Background(), RequestLogFilter{
	Query:             "  gpt-5  ",
	StatusClass:       "server_error",
	ProviderAccountID: 7,
})
```

Assert:

```go
if repo.lastLogFilter.ProviderAccountID != 7 {
	t.Fatalf("repository provider account ID = %d, want 7", repo.lastLogFilter.ProviderAccountID)
}
```

Add:

```go
if _, err := service.ListRequestLogs(context.Background(), RequestLogFilter{ProviderAccountID: -1}); !errors.Is(err, ErrInvalidInput) {
	t.Fatalf("ListRequestLogs invalid provider account ID error = %v, want ErrInvalidInput", err)
}
```

- [ ] **Step 2: Run failing service test**

Run:

```bash
cd backend && GOMODCACHE=/root/Clouds/N2API/.cache/go-mod GOCACHE=/root/Clouds/N2API/.cache/go-build go test ./internal/admin -run 'TestListRequestLogs' -count=1
```

Expected: FAIL until `ProviderAccountID` exists.

- [ ] **Step 3: Implement service validation**

Add `ProviderAccountID int64` to `RequestLogFilter`.

In `ListRequestLogs`, after status validation:

```go
if filter.ProviderAccountID < 0 {
	return nil, ErrInvalidInput
}
```

- [ ] **Step 4: Run service test**

Run the same service test command.

Expected: PASS.

### Task 3: HTTP And Store Filtering

**Files:**
- Modify: `backend/internal/httpapi/server.go`
- Modify: `backend/internal/httpapi/server_test.go`
- Modify: `backend/internal/store/admin.go`
- Modify: `backend/internal/store/admin_test.go`

- [ ] **Step 1: Write failing tests**

Extend `TestListRequestLogsRequiresSessionAndReturnsLogs` request URL to:

```go
"/api/admin/request-logs?limit=20&q=codex&statusClass=server_error&providerAccountId=7"
```

Assert:

```go
if admins.requestLogFilter.ProviderAccountID != 7 {
	t.Fatalf("request log provider account ID = %d, want 7", admins.requestLogFilter.ProviderAccountID)
}
```

Add an HTTP test for `providerAccountId=abc` returning `400`.

Extend `TestListRequestLogsSupportsParameterizedFilters` with `ProviderAccountID: 7` and assert `whereSQL` contains `l.provider_account_id = $`.

- [ ] **Step 2: Run failing backend tests**

Run:

```bash
cd backend && GOMODCACHE=/root/Clouds/N2API/.cache/go-mod GOCACHE=/root/Clouds/N2API/.cache/go-build go test ./internal/httpapi ./internal/store -run 'TestListRequestLogs' -count=1
```

Expected: FAIL until HTTP parsing and SQL condition exist.

- [ ] **Step 3: Implement HTTP parsing and SQL condition**

In the handler, parse `providerAccountId` with `strconv.ParseInt(raw, 10, 64)`. On parse error or value `< 1`, return `400 invalid_input`. Pass the value into `admin.RequestLogFilter`.

In `requestLogFilterSQL`, when `filter.ProviderAccountID > 0`, append one SQL argument and condition:

```go
"l.provider_account_id = $" + strconv.Itoa(len(args))
```

- [ ] **Step 4: Run targeted backend tests**

Run:

```bash
cd backend && GOMODCACHE=/root/Clouds/N2API/.cache/go-mod GOCACHE=/root/Clouds/N2API/.cache/go-build go test ./internal/admin ./internal/httpapi ./internal/store -run 'TestListRequestLogs' -count=1
```

Expected: PASS.

### Task 4: Request Logs UI Account Filter

**Files:**
- Modify: `frontend/src/lib/admin-state.svelte.js`
- Modify: `frontend/src/routes/request-logs/+page.svelte`
- Modify: `frontend/src/routes/navigation.test.mjs`

- [ ] **Step 1: Write failing frontend source tests**

Add assertions:

```js
assert.match(requestLogsPage, /providerAccounts/);
assert.match(requestLogsPage, /bind:value=\{requestLogs\.providerAccountId\}/);
assert.match(requestLogsPage, /All provider accounts/);
assert.match(adminState, /params\.set\('providerAccountId'/);
assert.match(adminState, /loadProviderAccounts\(\)/);
```

- [ ] **Step 2: Run failing frontend source test**

Run:

```bash
cd frontend && bun test src/routes/navigation.test.mjs
```

Expected: FAIL until state, route preload, select, and URL param exist.

- [ ] **Step 3: Implement UI state and controls**

Add `providerAccountId: 'all'` to `requestLogs`, reset it in `clearRequestLogs`, and send it as `providerAccountId` when not `all`.

Import `loadProviderAccounts` and `providerAccounts` in `request-logs/+page.svelte`. In the route effect, call `loadProviderAccounts()` when authenticated and `providerAccounts.items.length === 0`.

Add a Provider account select with an `All provider accounts` option and one option per account.

- [ ] **Step 4: Run frontend source test**

Run:

```bash
cd frontend && bun test src/routes/navigation.test.mjs
```

Expected: PASS.

### Task 5: Final Verification And Commit

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

- [ ] **Step 5: Commit implementation**

Run:

```bash
git add backend/internal/admin/service.go backend/internal/admin/service_test.go backend/internal/httpapi/server.go backend/internal/httpapi/server_test.go backend/internal/store/admin.go backend/internal/store/admin_test.go frontend/src/lib/admin-state.svelte.js frontend/src/routes/request-logs/+page.svelte frontend/src/routes/navigation.test.mjs
git commit -m "feat: filter request logs by account"
```

Expected: commit succeeds and `git status --short` is clean.

# API Key Log Filter Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Let admins jump from an API key row to Request Logs filtered exactly for that client key.

**Architecture:** Extend the existing request-log filter pipeline with `ClientKeyID`: HTTP parses it, admin service validates it, store SQL adds a parameterized `l.client_key_id = $N` condition. The frontend keeps `clientKeyId` in shared Request Logs state and API Keys emits a normal anchor to `/request-logs?clientKeyId=${key.id}`.

**Tech Stack:** Go admin service, Go HTTP handlers, PostgreSQL via pgx, SvelteKit admin UI, Bun verification.

---

### Task 1: Plan And Scope

**Files:**
- Create: `docs/superpowers/specs/2026-06-24-api-key-log-filter-design.md`
- Create: `docs/superpowers/plans/2026-06-24-api-key-log-filter.md`

- [ ] **Step 1: Write design and implementation plan**

Document the API key log filter behavior, validation, UI deep link, and verification commands.

- [ ] **Step 2: Commit docs**

Run:

```bash
git add docs/superpowers/specs/2026-06-24-api-key-log-filter-design.md docs/superpowers/plans/2026-06-24-api-key-log-filter.md
git commit -m "docs: plan api key log filter"
```

Expected: commit contains only the two docs.

### Task 2: Backend Filter Contract

**Files:**
- Modify: `backend/internal/admin/service.go`
- Modify: `backend/internal/admin/service_test.go`

- [ ] **Step 1: Write failing service tests**

In `TestListRequestLogsClampsLimitAndReturnsRepositoryLogs`, add `ClientKeyID: 12` to the filter and assert:

```go
if repo.lastLogFilter.ClientKeyID != 12 {
	t.Fatalf("repository client key ID = %d, want 12", repo.lastLogFilter.ClientKeyID)
}
```

Add:

```go
if _, err := service.ListRequestLogs(context.Background(), RequestLogFilter{ClientKeyID: -1}); !errors.Is(err, ErrInvalidInput) {
	t.Fatalf("ListRequestLogs invalid client key ID error = %v, want ErrInvalidInput", err)
}
```

- [ ] **Step 2: Run failing service test**

Run:

```bash
cd backend && GOMODCACHE=/root/Clouds/N2API/.cache/go-mod GOCACHE=/root/Clouds/N2API/.cache/go-build go test ./internal/admin -run 'TestListRequestLogs' -count=1
```

Expected: FAIL until `ClientKeyID` exists.

- [ ] **Step 3: Implement service validation**

Add `ClientKeyID int64` to `RequestLogFilter`.

In `ListRequestLogs`, after provider-account validation:

```go
if filter.ClientKeyID < 0 {
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

Extend `TestListRequestLogsRequiresSessionAndReturnsLogs` request URL to include:

```text
clientKeyId=12
```

Assert:

```go
if admins.requestLogFilter.ClientKeyID != 12 {
	t.Fatalf("request log client key ID = %d, want 12", admins.requestLogFilter.ClientKeyID)
}
```

Add an HTTP test for `clientKeyId=abc` returning `400`.

Extend `TestListRequestLogsSupportsParameterizedFilters` with `ClientKeyID: 12` and assert `whereSQL` contains `l.client_key_id = $`.

- [ ] **Step 2: Run failing backend tests**

Run:

```bash
cd backend && GOMODCACHE=/root/Clouds/N2API/.cache/go-mod GOCACHE=/root/Clouds/N2API/.cache/go-build go test ./internal/admin ./internal/httpapi ./internal/store -run 'TestListRequestLogs' -count=1
```

Expected: FAIL until HTTP parsing and SQL condition exist.

- [ ] **Step 3: Implement HTTP parsing and SQL condition**

Parse `clientKeyId` with `strconv.ParseInt(raw, 10, 64)`. On parse error or value `< 1`, return `400 invalid_input`. Pass the value into `admin.RequestLogFilter`.

In `requestLogFilterSQL`, when `filter.ClientKeyID > 0`, append one SQL argument and condition:

```go
"l.client_key_id = $" + strconv.Itoa(len(args))
```

- [ ] **Step 4: Run targeted backend tests**

Run:

```bash
cd backend && GOMODCACHE=/root/Clouds/N2API/.cache/go-mod GOCACHE=/root/Clouds/N2API/.cache/go-build go test ./internal/admin ./internal/httpapi ./internal/store -run 'TestListRequestLogs' -count=1
```

Expected: PASS.

### Task 4: Frontend Link And URL State

**Files:**
- Modify: `frontend/src/lib/admin-state.svelte.js`
- Modify: `frontend/src/routes/api-keys/+page.svelte`
- Modify: `frontend/src/routes/request-logs/+page.svelte`
- Modify: `frontend/src/routes/navigation.test.mjs`
- Modify: `frontend/src/routes/providers/provider-page.test.mjs`

- [ ] **Step 1: Write failing frontend source tests**

Add assertions:

```js
assert.match(apiKeysPage, /href=\{`\/request-logs\?clientKeyId=\$\{key\.id\}`\}/);
assert.match(apiKeysPage, /View request logs/);
assert.match(requestLogsPage, /clientKeyId/);
assert.match(requestLogsPage, /requestLogs\.clientKeyId = clientKeyId/);
assert.match(adminState, /params\.set\('clientKeyId'/);
assert.match(adminState, /clientKeyId: 'all'/);
```

- [ ] **Step 2: Run failing frontend tests**

Run:

```bash
cd frontend && bun test src/routes/navigation.test.mjs src/routes/providers/provider-page.test.mjs
```

Expected: FAIL until state, URL initialization, and API key link exist.

- [ ] **Step 3: Implement frontend state and link**

Add `clientKeyId: 'all'` to `requestLogs` state and reset state. In `loadRequestLogs`, set `clientKeyId` when not `all`.

In Request Logs URL initialization, parse positive `clientKeyId` and set `requestLogs.clientKeyId`.

In API Keys row action cell, add:

```svelte
<a
  class="mr-2 inline-flex rounded-lg border border-[#e5e5e5] bg-white px-3 py-1.5 text-sm font-medium text-[#0d0d0d] hover:bg-[#f5f5f5]"
  href={`/request-logs?clientKeyId=${key.id}`}
  title="View request logs"
  aria-label="View request logs"
>
  Logs
</a>
```

- [ ] **Step 4: Run frontend tests**

Run:

```bash
cd frontend && bun test src/routes/navigation.test.mjs src/routes/providers/provider-page.test.mjs
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
cd frontend && bun test src/routes/navigation.test.mjs src/routes/providers/provider-page.test.mjs
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
git add backend/internal/admin/service.go backend/internal/admin/service_test.go backend/internal/httpapi/server.go backend/internal/httpapi/server_test.go backend/internal/store/admin.go backend/internal/store/admin_test.go frontend/src/lib/admin-state.svelte.js frontend/src/routes/api-keys/+page.svelte frontend/src/routes/request-logs/+page.svelte frontend/src/routes/navigation.test.mjs frontend/src/routes/providers/provider-page.test.mjs
git commit -m "feat: filter request logs by api key"
```

Expected: commit succeeds and worktree is clean.

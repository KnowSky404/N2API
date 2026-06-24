# Request Log Model And Session Filter Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add exact Request Logs filters for request model and sticky session, plus Gateway usage drill-down links for model/session rows.

**Architecture:** Extend the existing request-log filter DTO through admin service, HTTP parsing, and store SQL. Reuse frontend Request Logs state and URL parameter patterns already used for provider account and API key filters. Gateway usage rows compute links locally from usage group metadata.

**Tech Stack:** Go admin/http/store tests, SvelteKit source tests, Bun.

---

## File Structure

- Modify `backend/internal/admin/service.go` and `service_test.go`: add filter fields and validation.
- Modify `backend/internal/httpapi/server.go` and `server_test.go`: parse `model` and `sessionId`.
- Modify `backend/internal/store/admin.go` and `admin_test.go`: add exact SQL predicates.
- Modify `frontend/src/lib/admin-state.svelte.js`: add request-log state/query params.
- Modify `frontend/src/routes/request-logs/+page.svelte`: add URL initialization and controls.
- Modify `frontend/src/routes/gateway/+page.svelte`: add model/session usage drill-down links.
- Modify `frontend/src/routes/navigation.test.mjs`: add source coverage.
- Modify `backend/internal/gateway/documentation_test.go`, `README.md`, and `deploy/README.md`: document drill-down.

### Task 1: Backend Request Log Filters

**Files:**
- Modify: `backend/internal/admin/service_test.go`
- Modify: `backend/internal/admin/service.go`
- Modify: `backend/internal/httpapi/server_test.go`
- Modify: `backend/internal/httpapi/server.go`
- Modify: `backend/internal/store/admin_test.go`
- Modify: `backend/internal/store/admin.go`

- [ ] **Step 1: Write failing admin service test**

Update `TestListRequestLogsClampsLimitAndReturnsRepositoryLogs` to pass:

```go
Model: "gpt-5",
SessionID: "workspace-123",
```

Assert:

```go
if repo.lastLogFilter.Model != "gpt-5" || repo.lastLogFilter.SessionID != "workspace-123" {
	t.Fatalf("repository model/session = %q/%q, want gpt-5/workspace-123", repo.lastLogFilter.Model, repo.lastLogFilter.SessionID)
}
```

Add invalid length assertions to `TestListRequestLogsValidatesFilter`:

```go
if _, err := service.ListRequestLogs(context.Background(), RequestLogFilter{Model: strings.Repeat("x", 101)}); !errors.Is(err, ErrInvalidInput) {
	t.Fatalf("ListRequestLogs long model error = %v, want ErrInvalidInput", err)
}
if _, err := service.ListRequestLogs(context.Background(), RequestLogFilter{SessionID: strings.Repeat("x", 101)}); !errors.Is(err, ErrInvalidInput) {
	t.Fatalf("ListRequestLogs long session error = %v, want ErrInvalidInput", err)
}
```

- [ ] **Step 2: Write failing HTTP and store tests**

In `TestListRequestLogsRequiresSessionAndReturnsLogs`, use:

```go
"/api/admin/request-logs?limit=20&q=codex&statusClass=server_error&providerAccountId=7&clientKeyId=12&model=gpt-5&sessionId=workspace-123"
```

Assert:

```go
if admins.requestLogFilter.Model != "gpt-5" || admins.requestLogFilter.SessionID != "workspace-123" {
	t.Fatalf("request log model/session = %q/%q, want gpt-5/workspace-123", admins.requestLogFilter.Model, admins.requestLogFilter.SessionID)
}
```

In `TestListRequestLogsSupportsParameterizedFilters`, add `Model: "gpt-5"` and `SessionID: "workspace-123"` to the filter and assert SQL contains:

```go
"l.model = $"
"l.session_id = $"
```

- [ ] **Step 3: Run red backend tests**

```bash
cd backend && GOMODCACHE=/root/Clouds/N2API/.cache/go-mod GOCACHE=/root/Clouds/N2API/.cache/go-build go test ./internal/admin ./internal/httpapi ./internal/store -run 'TestListRequestLogs' -count=1
```

Expected: FAIL because `RequestLogFilter` has no model/session fields.

- [ ] **Step 4: Implement backend filters**

In `backend/internal/admin/service.go`, add:

```go
Model     string
SessionID string
```

Validate in `ListRequestLogs`:

```go
filter.Model = strings.TrimSpace(filter.Model)
filter.SessionID = strings.TrimSpace(filter.SessionID)
if len(filter.Model) > 100 || len(filter.SessionID) > 100 {
	return nil, ErrInvalidInput
}
```

In `backend/internal/httpapi/server.go`, set:

```go
Model: r.URL.Query().Get("model"),
SessionID: r.URL.Query().Get("sessionId"),
```

In `backend/internal/store/admin.go`, add predicates in `requestLogFilterSQL`:

```go
if filter.Model != "" {
	args = append(args, filter.Model)
	conditions = append(conditions, "l.model = $"+strconv.Itoa(len(args)))
}
if filter.SessionID != "" {
	args = append(args, filter.SessionID)
	conditions = append(conditions, "l.session_id = $"+strconv.Itoa(len(args)))
}
```

- [ ] **Step 5: Run backend tests**

```bash
cd backend && GOMODCACHE=/root/Clouds/N2API/.cache/go-mod GOCACHE=/root/Clouds/N2API/.cache/go-build go test ./internal/admin ./internal/httpapi ./internal/store -run 'TestListRequestLogs' -count=1
```

Expected: PASS.

- [ ] **Step 6: Commit backend filters**

```bash
git add backend/internal/admin/service.go backend/internal/admin/service_test.go backend/internal/httpapi/server.go backend/internal/httpapi/server_test.go backend/internal/store/admin.go backend/internal/store/admin_test.go
git commit -m "feat: filter request logs by model and session"
```

### Task 2: Frontend Filters And Gateway Links

**Files:**
- Modify: `frontend/src/routes/navigation.test.mjs`
- Modify: `frontend/src/lib/admin-state.svelte.js`
- Modify: `frontend/src/routes/request-logs/+page.svelte`
- Modify: `frontend/src/routes/gateway/+page.svelte`

- [ ] **Step 1: Write failing frontend tests**

In request log filter tests, assert:

```js
assert.match(requestLogsPage, /bind:value=\{requestLogs\.model\}/);
assert.match(requestLogsPage, /bind:value=\{requestLogs\.sessionId\}/);
assert.match(adminState, /params\.set\('model'/);
assert.match(adminState, /params\.set\('sessionId'/);
```

In URL initialization test, assert:

```js
assert.match(requestLogsPage, /requestLogs\.model = model/);
assert.match(requestLogsPage, /requestLogs\.sessionId = sessionId/);
assert.match(adminState, /model: ''/);
assert.match(adminState, /sessionId: ''/);
```

In Gateway page test, assert:

```js
assert.match(gatewayPage, /usageRowHref/);
assert.match(gatewayPage, /model=\$\{encodeURIComponent/);
assert.match(gatewayPage, /sessionId=\$\{encodeURIComponent/);
```

- [ ] **Step 2: Run red frontend test**

```bash
cd frontend && bun test src/routes/navigation.test.mjs
```

Expected: FAIL because filters and links are not implemented.

- [ ] **Step 3: Implement admin state**

Add `model` and `sessionId` to request logs state and reset:

```js
model: '',
sessionId: '',
```

Add load params:

```js
if (requestLogs.model) params.set('model', requestLogs.model);
if (requestLogs.sessionId) params.set('sessionId', requestLogs.sessionId);
```

- [ ] **Step 4: Implement Request Logs UI**

Read URL params:

```js
const model = params.get('model') ?? '';
const sessionId = params.get('sessionId') ?? '';
if (model.length > 0 && model.length <= 100) requestLogs.model = model;
if (sessionId.length > 0 && sessionId.length <= 100) requestLogs.sessionId = sessionId;
```

Add two labels beside existing filters:

```svelte
<label class="block text-sm font-medium text-[#3c3c3c]">
  Model filter
  <input class="mt-2 w-full rounded-lg border border-[#e5e5e5] bg-white px-3 py-2 text-sm text-[#0d0d0d] outline-none focus:border-[#10a37f] focus:ring-2 focus:ring-[#e8f5f0]" bind:value={requestLogs.model} placeholder="gpt-5" />
</label>
<label class="block text-sm font-medium text-[#3c3c3c]">
  Session filter
  <input class="mt-2 w-full rounded-lg border border-[#e5e5e5] bg-white px-3 py-2 text-sm text-[#0d0d0d] outline-none focus:border-[#10a37f] focus:ring-2 focus:ring-[#e8f5f0]" bind:value={requestLogs.sessionId} placeholder="workspace-123" />
</label>
```

- [ ] **Step 5: Implement Gateway usage links**

In `frontend/src/routes/gateway/+page.svelte`, add:

```svelte
function usageRowHref(sectionTitle, row) {
  const id = String(row?.id ?? '');
  if (!id || id === 'unknown') return '';
  if (sectionTitle === 'Top models') {
    return `/request-logs?model=${encodeURIComponent(id)}`;
  }
  if (sectionTitle === 'Top sessions' && id !== 'none') {
    return `/request-logs?sessionId=${encodeURIComponent(id)}`;
  }
  return '';
}
```

In usage rows:

```svelte
{@const href = usageRowHref(section.title, row)}
{#if href}
  <a class="min-w-0 truncate font-medium text-[#0d0d0d] underline decoration-[#d9d9d9] underline-offset-4 hover:decoration-[#10a37f]" href={href}>{row.label || row.id}</a>
{:else}
  <span class="min-w-0 truncate font-medium text-[#0d0d0d]">{row.label || row.id}</span>
{/if}
```

- [ ] **Step 6: Run frontend tests and checks**

```bash
cd frontend && bun test src/routes/navigation.test.mjs
cd frontend && bun run check
```

Expected: PASS.

- [ ] **Step 7: Commit frontend filters**

```bash
git add frontend/src/lib/admin-state.svelte.js frontend/src/routes/request-logs/+page.svelte frontend/src/routes/gateway/+page.svelte frontend/src/routes/navigation.test.mjs
git commit -m "feat: link usage rows to filtered request logs"
```

### Task 3: Documentation And Full Verification

**Files:**
- Modify: `backend/internal/gateway/documentation_test.go`
- Modify: `README.md`
- Modify: `deploy/README.md`

- [ ] **Step 1: Write failing docs test**

Add:

```go
func TestGatewayDocumentationMentionsRequestLogModelSessionDrilldown(t *testing.T) {
	for _, path := range []string{"../../../README.md", "../../../deploy/README.md"} {
		content, err := os.ReadFile(path)
		if err != nil {
			t.Fatalf("ReadFile(%q) returned error: %v", path, err)
		}
		text := string(content)
		for _, want := range []string{
			"Model filter",
			"Session filter",
			"Top models",
			"Top sessions",
		} {
			if !strings.Contains(text, want) {
				t.Fatalf("%s missing %q in request log model/session drill-down documentation", path, want)
			}
		}
	}
}
```

- [ ] **Step 2: Update docs**

Add one sentence near request logs / 24h usage:

```markdown
Request Logs support exact **Model filter** and **Session filter** fields. On Gateway management, **Top models** and **Top sessions** usage rows link to Request Logs with those filters applied so model and sticky-session traffic can be inspected directly.
```

- [ ] **Step 3: Run docs test**

```bash
cd backend && GOMODCACHE=/root/Clouds/N2API/.cache/go-mod GOCACHE=/root/Clouds/N2API/.cache/go-build go test ./internal/gateway -run RequestLogModelSessionDrilldown
```

Expected: PASS after docs update.

- [ ] **Step 4: Full verification**

```bash
git diff --check
cd backend && GOMODCACHE=/root/Clouds/N2API/.cache/go-mod GOCACHE=/root/Clouds/N2API/.cache/go-build go test ./...
cd frontend && bun test src/routes/navigation.test.mjs
cd frontend && bun run check
cd frontend && bun run build
```

Expected: PASS. If sandbox blocks backend `httptest` IPv6 listeners, rerun the same backend command with elevated permissions.

- [ ] **Step 5: Commit docs**

```bash
git add backend/internal/gateway/documentation_test.go README.md deploy/README.md
git commit -m "docs: document model session log drilldown"
```

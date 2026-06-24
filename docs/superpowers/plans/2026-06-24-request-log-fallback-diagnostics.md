# Request Log Fallback Diagnostics Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Store and show per-request gateway attempt and fallback counts in Request Logs.

**Architecture:** Add two integer columns to `request_logs`, propagate them through `gateway.RequestLog`, `store.GatewayRepository`, `admin.RequestLog`, and the admin list query. The proxy increments counters only inside provider-account selection/upstream retry flow; local auth/rate/concurrency guards remain zero. The frontend renders compact diagnostics in the existing Request Logs table.

**Tech Stack:** Go backend, PostgreSQL migrations, SvelteKit admin UI, Bun source tests.

---

## File Structure

- Create `backend/internal/store/migrations/00021_request_log_fallback_diagnostics.sql`: add `gateway_attempt_count` and `gateway_fallback_count`.
- Modify `backend/internal/store/migrations_test.go`: assert the migration is embedded and contains both columns.
- Modify `backend/internal/gateway/proxy.go`: add request-log fields and increment attempt/fallback counters.
- Modify `backend/internal/gateway/proxy_test.go`: cover retry fallback and busy-account fallback logging.
- Modify `backend/internal/store/gateway.go` and `backend/internal/store/gateway_test.go`: insert the new fields.
- Modify `backend/internal/admin/service.go`, `backend/internal/store/admin.go`, `backend/internal/store/admin_test.go`, and `backend/internal/httpapi/server_test.go`: expose the fields.
- Modify `frontend/src/lib/admin-state.svelte.js`, `frontend/src/routes/request-logs/+page.svelte`, and `frontend/src/routes/navigation.test.mjs`: type and render the diagnostics.
- Modify `README.md`, `deploy/README.md`, and `backend/internal/gateway/documentation_test.go`: document and guard the behavior.

## Task 1: Schema And Gateway Logging

**Files:**
- Create: `backend/internal/store/migrations/00021_request_log_fallback_diagnostics.sql`
- Modify: `backend/internal/store/migrations_test.go`
- Modify: `backend/internal/gateway/proxy.go`
- Modify: `backend/internal/gateway/proxy_test.go`
- Modify: `backend/internal/store/gateway.go`
- Modify: `backend/internal/store/gateway_test.go`

- [ ] **Step 1: Write failing migration and insert tests**

Add a migration test in `backend/internal/store/migrations_test.go`:

```go
func TestRequestLogFallbackDiagnosticsMigrationIsEmbedded(t *testing.T) {
	sql, err := MigrationSQL("00021_request_log_fallback_diagnostics.sql")
	if err != nil {
		t.Fatalf("MigrationSQL returned error: %v", err)
	}
	for _, want := range []string{
		"ALTER TABLE request_logs ADD COLUMN IF NOT EXISTS gateway_attempt_count INTEGER NOT NULL DEFAULT 0",
		"ALTER TABLE request_logs ADD COLUMN IF NOT EXISTS gateway_fallback_count INTEGER NOT NULL DEFAULT 0",
	} {
		if !strings.Contains(sql, want) {
			t.Fatalf("migration missing %q", want)
		}
	}
}
```

Update `TestCreateRequestLogSQLIncludesProviderAccountAttribution` in `backend/internal/store/gateway_test.go` to also require:

```go
"gateway_attempt_count",
"gateway_fallback_count",
"entry.GatewayAttemptCount",
"entry.GatewayFallbackCount",
```

- [ ] **Step 2: Run tests and verify red**

Run:

```bash
cd backend
GOMODCACHE=/root/Clouds/N2API/.cache/go-mod GOCACHE=/root/Clouds/N2API/.cache/go-build go test ./internal/store -run 'TestRequestLogFallbackDiagnosticsMigrationIsEmbedded|TestCreateRequestLogSQLIncludesProviderAccountAttribution'
```

Expected: FAIL because the migration and insert fields do not exist.

- [ ] **Step 3: Add migration and insert fields**

Create `backend/internal/store/migrations/00021_request_log_fallback_diagnostics.sql`:

```sql
-- +goose Up
-- +goose StatementBegin
ALTER TABLE request_logs ADD COLUMN IF NOT EXISTS gateway_attempt_count INTEGER NOT NULL DEFAULT 0;
ALTER TABLE request_logs ADD COLUMN IF NOT EXISTS gateway_fallback_count INTEGER NOT NULL DEFAULT 0;
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
ALTER TABLE request_logs DROP COLUMN IF EXISTS gateway_fallback_count;
ALTER TABLE request_logs DROP COLUMN IF EXISTS gateway_attempt_count;
-- +goose StatementEnd
```

Add fields to `gateway.RequestLog`:

```go
GatewayAttemptCount  int
GatewayFallbackCount int
```

Update `createRequestLogSQL()` to include `gateway_attempt_count, gateway_fallback_count` and two new placeholders. Pass `entry.GatewayAttemptCount` and `entry.GatewayFallbackCount` to `Exec`.

- [ ] **Step 4: Run store tests and verify green**

Run:

```bash
cd backend
GOMODCACHE=/root/Clouds/N2API/.cache/go-mod GOCACHE=/root/Clouds/N2API/.cache/go-build go test ./internal/store -run 'TestRequestLogFallbackDiagnosticsMigrationIsEmbedded|TestCreateRequestLogSQLIncludesProviderAccountAttribution'
```

Expected: PASS.

- [ ] **Step 5: Write failing gateway fallback counter tests**

Add tests near existing request-log tests in `backend/internal/gateway/proxy_test.go`.

For upstream retry:

```go
func TestProxyLogsGatewayFallbackCountsForRetryableUpstreamFailure(t *testing.T) {
	logger := &fakeRequestLogger{}
	tokens := &fakeSelectedAccountProvider{accounts: []SelectedAccount{
		{AccountID: 1, AccountType: provider.AccountTypeAPIUpstream, AuthorizationToken: "first-token"},
		{AccountID: 2, AccountType: provider.AccountTypeAPIUpstream, AuthorizationToken: "second-token"},
	}}
	transportCalls := 0
	client := &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
		transportCalls++
		if transportCalls == 1 {
			return &http.Response{
				StatusCode: http.StatusTooManyRequests,
				Header:     http.Header{"Retry-After": []string{"30"}},
				Body:       io.NopCloser(strings.NewReader(`{"error":{"message":"rate limited"}}`)),
				Request:    r,
			}, nil
		}
		return &http.Response{
			StatusCode: http.StatusOK,
			Header:     http.Header{"Content-Type": []string{"application/json"}},
			Body:       io.NopCloser(strings.NewReader(`{"ok":true}`)),
			Request:    r,
		}, nil
	})}
	proxy := NewProxyWithClient(&fakeAPIKeyAuthenticator{}, tokens, Config{UpstreamBaseURL: "https://upstream.example.test", Logger: logger}, client)
	req := httptest.NewRequest(http.MethodGet, "/v1/responses/resp_123", nil)
	req.Header.Set("Authorization", "Bearer n2api_client_secret")
	recorder := httptest.NewRecorder()

	proxy.ServeHTTP(recorder, req)

	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d body=%s, want 200", recorder.Code, recorder.Body.String())
	}
	if len(logger.entries) != 1 {
		t.Fatalf("logged entries = %d, want 1", len(logger.entries))
	}
	entry := logger.entries[0]
	if entry.GatewayAttemptCount != 2 || entry.GatewayFallbackCount != 1 {
		t.Fatalf("gateway diagnostics = attempts:%d fallbacks:%d, want 2/1", entry.GatewayAttemptCount, entry.GatewayFallbackCount)
	}
}
```

For account concurrency skip:

```go
func TestProxyLogsGatewayFallbackCountsForBusyAccountFallback(t *testing.T) {
	logger := &fakeRequestLogger{}
	tokens := &fakeSelectedAccountProvider{accounts: []SelectedAccount{
		{AccountID: 1, AccountType: provider.AccountTypeAPIUpstream, AuthorizationToken: "first-token"},
		{AccountID: 2, AccountType: provider.AccountTypeAPIUpstream, AuthorizationToken: "second-token"},
	}}
	client := &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
		return &http.Response{
			StatusCode: http.StatusOK,
			Header:     http.Header{"Content-Type": []string{"application/json"}},
			Body:       io.NopCloser(strings.NewReader(`{"ok":true}`)),
			Request:    r,
		}, nil
	})}
	proxy := NewProxyWithClient(&fakeAPIKeyAuthenticator{}, tokens, Config{
		UpstreamBaseURL:                 "https://upstream.example.test",
		MaxConcurrentRequestsPerAccount: 1,
		Logger:                          logger,
	}, client)
	release, ok := proxy.tryAcquireAccountSlot(1, 1)
	if !ok {
		t.Fatal("failed to acquire setup account slot")
	}
	defer release()
	req := httptest.NewRequest(http.MethodGet, "/v1/responses/resp_123", nil)
	req.Header.Set("Authorization", "Bearer n2api_client_secret")
	recorder := httptest.NewRecorder()

	proxy.ServeHTTP(recorder, req)

	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d body=%s, want 200", recorder.Code, recorder.Body.String())
	}
	entry := logger.entries[0]
	if entry.GatewayAttemptCount != 2 || entry.GatewayFallbackCount != 1 {
		t.Fatalf("gateway diagnostics = attempts:%d fallbacks:%d, want 2/1", entry.GatewayAttemptCount, entry.GatewayFallbackCount)
	}
}
```

- [ ] **Step 6: Run gateway tests and verify red**

Run:

```bash
cd backend
GOMODCACHE=/root/Clouds/N2API/.cache/go-mod GOCACHE=/root/Clouds/N2API/.cache/go-build go test ./internal/gateway -run 'TestProxyLogsGatewayFallbackCounts'
```

Expected: FAIL because the proxy does not populate the counters yet.

- [ ] **Step 7: Implement gateway counters**

In `ServeHTTP`, define:

```go
gatewayAttemptCount := 0
gatewayFallbackCount := 0
```

Include both values in the deferred `RequestLog`.

Increment `gatewayAttemptCount` after `selectAccount` returns a selected account. Increment `gatewayFallbackCount` when:

- an account slot cannot be acquired and the proxy excludes that account;
- a retryable pre-stream upstream response is stored for a later attempt;
- a pre-stream transport error causes a retry attempt.

Do not increment either counter before provider selection.

- [ ] **Step 8: Run gateway tests and verify green**

Run:

```bash
cd backend
GOMODCACHE=/root/Clouds/N2API/.cache/go-mod GOCACHE=/root/Clouds/N2API/.cache/go-build go test ./internal/gateway -run 'TestProxyLogsGatewayFallbackCounts'
```

Expected: PASS.

- [ ] **Step 9: Commit**

```bash
git add backend/internal/store/migrations/00021_request_log_fallback_diagnostics.sql backend/internal/store/migrations_test.go backend/internal/store/gateway.go backend/internal/store/gateway_test.go backend/internal/gateway/proxy.go backend/internal/gateway/proxy_test.go
git commit -m "feat: log gateway fallback diagnostics"
```

## Task 2: Admin API Exposure

**Files:**
- Modify: `backend/internal/admin/service.go`
- Modify: `backend/internal/store/admin.go`
- Modify: `backend/internal/store/admin_test.go`
- Modify: `backend/internal/httpapi/server_test.go`

- [ ] **Step 1: Write failing admin/store/API tests**

Add `GatewayAttemptCount` and `GatewayFallbackCount` fields to expected request-log assertions:

- `backend/internal/store/admin_test.go`: require `ListRequestLogs` query text to select `gateway_attempt_count` and `gateway_fallback_count`.
- `backend/internal/httpapi/server_test.go` in `TestListRequestLogsRequiresSessionAndReturnsLogs`: seed `admin.RequestLog{GatewayAttemptCount: 2, GatewayFallbackCount: 1}` and assert JSON contains those values.

- [ ] **Step 2: Run tests and verify red**

Run:

```bash
cd backend
GOMODCACHE=/root/Clouds/N2API/.cache/go-mod GOCACHE=/root/Clouds/N2API/.cache/go-build go test ./internal/store ./internal/httpapi -run 'TestListRequestLogs'
```

Expected: FAIL because the admin DTO/query does not expose the fields.

- [ ] **Step 3: Expose fields**

Add fields to `admin.RequestLog`:

```go
GatewayAttemptCount  int `json:"gatewayAttemptCount"`
GatewayFallbackCount int `json:"gatewayFallbackCount"`
```

Update `ListRequestLogs` SQL to select:

```sql
COALESCE(l.gateway_attempt_count, 0),
COALESCE(l.gateway_fallback_count, 0),
```

Scan into the new fields before `CreatedAt`.

- [ ] **Step 4: Run tests and verify green**

Run:

```bash
cd backend
GOMODCACHE=/root/Clouds/N2API/.cache/go-mod GOCACHE=/root/Clouds/N2API/.cache/go-build go test ./internal/store ./internal/httpapi -run 'TestListRequestLogs'
```

Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add backend/internal/admin/service.go backend/internal/store/admin.go backend/internal/store/admin_test.go backend/internal/httpapi/server_test.go
git commit -m "feat: expose request log fallback diagnostics"
```

## Task 3: Frontend And Documentation

**Files:**
- Modify: `frontend/src/lib/admin-state.svelte.js`
- Modify: `frontend/src/routes/request-logs/+page.svelte`
- Modify: `frontend/src/routes/navigation.test.mjs`
- Modify: `README.md`
- Modify: `deploy/README.md`
- Modify: `backend/internal/gateway/documentation_test.go`

- [ ] **Step 1: Write failing frontend source test**

In `frontend/src/routes/navigation.test.mjs`, add a test that requires:

```js
assert.match(requestLogsPage, /Gateway diagnostics/);
assert.match(requestLogsPage, /log\.gatewayAttemptCount/);
assert.match(requestLogsPage, /log\.gatewayFallbackCount/);
assert.match(adminState, /gatewayAttemptCount/);
assert.match(adminState, /gatewayFallbackCount/);
```

- [ ] **Step 2: Run frontend test and verify red**

Run:

```bash
cd frontend
bun test src/routes/navigation.test.mjs
```

Expected: FAIL because the page does not render the fields.

- [ ] **Step 3: Render diagnostics**

Update the `RequestLog` typedef in `frontend/src/lib/admin-state.svelte.js` with:

```js
 * @property {number} gatewayAttemptCount
 * @property {number} gatewayFallbackCount
```

Add a `Gateway diagnostics` column on `frontend/src/routes/request-logs/+page.svelte`. Render:

```svelte
<td class="px-4 py-3 text-[#3c3c3c]">
  <span class="text-sm tabular-nums">Attempts {log.gatewayAttemptCount ?? 0}</span>
  <p class="mt-1 text-xs text-[#6e6e6e]">Fallbacks {log.gatewayFallbackCount ?? 0}</p>
</td>
```

Update loading/empty `colspan` values to include the new column.

- [ ] **Step 4: Run frontend test and verify green**

Run:

```bash
cd frontend
bun test src/routes/navigation.test.mjs
```

Expected: PASS.

- [ ] **Step 5: Write failing documentation test**

Add `TestGatewayDocumentationMentionsRequestLogFallbackDiagnostics` in `backend/internal/gateway/documentation_test.go`, requiring README and deploy README to include:

```go
"Request Logs",
"gateway fallback diagnostics",
"attempts",
"fallbacks",
```

- [ ] **Step 6: Update docs**

Add one sentence near the existing Request Logs paragraph in `README.md` and `deploy/README.md`:

```markdown
Request Logs also include gateway fallback diagnostics: attempts count selected provider-account tries, and fallbacks count pre-stream scheduler moves caused by busy accounts or retryable upstream failures.
```

- [ ] **Step 7: Run final verification**

Run:

```bash
git diff --check
cd backend
GOMODCACHE=/root/Clouds/N2API/.cache/go-mod GOCACHE=/root/Clouds/N2API/.cache/go-build go test ./...
cd ../frontend
bun test src/routes/navigation.test.mjs
bun run check
bun run build
```

If backend tests fail only with `httptest: failed to listen on a port: listen tcp6 [::1]:0: socket: operation not permitted`, rerun the same backend `go test ./...` command with escalated permissions.

- [ ] **Step 8: Commit**

```bash
git add frontend/src/lib/admin-state.svelte.js frontend/src/routes/request-logs/+page.svelte frontend/src/routes/navigation.test.mjs README.md deploy/README.md backend/internal/gateway/documentation_test.go
git commit -m "docs: show request log fallback diagnostics"
```

## Self-Review

- Spec coverage: schema, gateway counters, admin API, frontend rendering, docs, and verification are each covered by one task.
- Placeholder scan: no open placeholders remain.
- Type consistency: field names are `GatewayAttemptCount` / `gatewayAttemptCount` and `GatewayFallbackCount` / `gatewayFallbackCount` throughout.

# Provider Account Concurrency Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add provider-account-level gateway concurrency caps that override the global per-account gateway default when set.

**Architecture:** Store the cap on `provider_accounts`, expose it through provider/admin DTOs, propagate it through account selection into the gateway limiter, and add focused admin controls. `0` means inherit the global Gateway Settings limit; positive values override it.

**Tech Stack:** Go backend with PostgreSQL migrations and `go test`; SvelteKit admin UI with Bun; existing static source tests for admin page coverage.

---

## File Map

- Create `backend/internal/store/migrations/00018_provider_account_max_concurrent_requests.sql`: add `max_concurrent_requests` with a non-negative check.
- Modify `backend/internal/store/migrations_test.go`: assert migration SQL includes the column and constraint.
- Modify `backend/internal/provider/service.go`: add `MaxConcurrentRequests` to `Account`, `AccountUpdate`, and `SelectedAccount`; validate update input; copy selected account value.
- Modify `backend/internal/store/provider.go`: include the new column in `providerAccountColumns`, scans, saves, and update SQL.
- Modify `backend/internal/provider/service_test.go`: cover validation and selected DTO propagation.
- Modify `backend/internal/gateway/proxy.go`: compute effective selected-account concurrency before acquiring the account limiter.
- Modify `backend/internal/gateway/proxy_test.go`: prove selected-account override causes fallback even when the global default is higher.
- Modify `backend/internal/httpapi/server.go`: accept `maxConcurrentRequests` in single and bulk update requests.
- Modify `backend/internal/httpapi/server_test.go`: cover HTTP payloads for single and bulk updates.
- Modify `frontend/src/lib/admin-state.svelte.js`: add `maxConcurrentRequests` to account type, update payloads, bulk form, and validation.
- Modify `frontend/src/routes/providers/+page.svelte`: add row input and bulk input.
- Modify `frontend/src/routes/providers/provider-page.test.mjs`: assert source includes the new controls and state actions.
- Modify `backend/internal/gateway/documentation_test.go`, `README.md`, and `deploy/README.md`: document override semantics.

## Task 1: Migration And Store Field

**Files:**
- Create: `backend/internal/store/migrations/00018_provider_account_max_concurrent_requests.sql`
- Modify: `backend/internal/store/migrations_test.go`
- Modify: `backend/internal/store/provider.go`
- Modify: `backend/internal/provider/service.go`

- [ ] **Step 1: Write failing migration test**

Add a test in `backend/internal/store/migrations_test.go`:

```go
func TestProviderAccountMaxConcurrentRequestsMigration(t *testing.T) {
	content, err := os.ReadFile("migrations/00018_provider_account_max_concurrent_requests.sql")
	if err != nil {
		t.Fatalf("ReadFile migration returned error: %v", err)
	}
	sql := string(content)
	for _, want := range []string{
		"max_concurrent_requests INTEGER NOT NULL DEFAULT 0",
		"provider_accounts_max_concurrent_requests_non_negative",
		"CHECK (max_concurrent_requests >= 0)",
	} {
		if !strings.Contains(sql, want) {
			t.Fatalf("migration missing %q", want)
		}
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run:

```bash
cd backend && GOMODCACHE=/root/Clouds/N2API/.cache/go-mod GOCACHE=/root/Clouds/N2API/.cache/go-build go test ./internal/store -run MaxConcurrentRequests
```

Expected: FAIL because migration file does not exist or does not contain the column.

- [ ] **Step 3: Add migration**

Create `backend/internal/store/migrations/00018_provider_account_max_concurrent_requests.sql`:

```sql
-- +goose Up
-- +goose StatementBegin
ALTER TABLE provider_accounts ADD COLUMN IF NOT EXISTS max_concurrent_requests INTEGER NOT NULL DEFAULT 0;

ALTER TABLE provider_accounts DROP CONSTRAINT IF EXISTS provider_accounts_max_concurrent_requests_non_negative;
ALTER TABLE provider_accounts
	ADD CONSTRAINT provider_accounts_max_concurrent_requests_non_negative CHECK (max_concurrent_requests >= 0);
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
ALTER TABLE provider_accounts DROP CONSTRAINT IF EXISTS provider_accounts_max_concurrent_requests_non_negative;
ALTER TABLE provider_accounts DROP COLUMN IF EXISTS max_concurrent_requests;
-- +goose StatementEnd
```

- [ ] **Step 4: Add backend model/store field**

In `backend/internal/provider/service.go`, add:

```go
MaxConcurrentRequests int `json:"maxConcurrentRequests"`
```

to `Account`, and add:

```go
MaxConcurrentRequests *int
```

to `AccountUpdate`.

In `backend/internal/store/provider.go`, add `a.max_concurrent_requests` to `providerAccountColumns`, scan it into `account.MaxConcurrentRequests`, insert/update it where provider account rows are created or updated, and set:

```sql
max_concurrent_requests = COALESCE($N, max_concurrent_requests)
```

in the account update SQL.

- [ ] **Step 5: Run store tests**

Run:

```bash
cd backend && GOMODCACHE=/root/Clouds/N2API/.cache/go-mod GOCACHE=/root/Clouds/N2API/.cache/go-build go test ./internal/store -run MaxConcurrentRequests
```

Expected: PASS.

- [ ] **Step 6: Commit**

```bash
git add backend/internal/store/migrations/00018_provider_account_max_concurrent_requests.sql backend/internal/store/migrations_test.go backend/internal/store/provider.go backend/internal/provider/service.go
git commit -m "feat: store provider account concurrency limits"
```

## Task 2: Provider Validation And Selection Propagation

**Files:**
- Modify: `backend/internal/provider/service_test.go`
- Modify: `backend/internal/provider/service.go`

- [ ] **Step 1: Write failing provider tests**

Add tests in `backend/internal/provider/service_test.go`:

```go
func TestProviderServiceRejectsNegativeMaxConcurrentRequests(t *testing.T) {
	svc, _ := newTestService(t)
	value := -1
	if _, err := svc.UpdateAccount(context.Background(), 7, AccountUpdate{MaxConcurrentRequests: &value}); !errors.Is(err, ErrInvalidInput) {
		t.Fatalf("error = %v, want ErrInvalidInput", err)
	}
}

func TestSelectedAccountIncludesMaxConcurrentRequests(t *testing.T) {
	svc, _ := newTestService(t)
	account := testAccount(t, 7, true, 1, "token")
	account.MaxConcurrentRequests = 2
	selected, err := svc.selectedAccount(context.Background(), account)
	if err != nil {
		t.Fatalf("selectedAccount returned error: %v", err)
	}
	if selected.MaxConcurrentRequests != 2 {
		t.Fatalf("max concurrency = %d, want 2", selected.MaxConcurrentRequests)
	}
}
```

Use existing helpers if their names differ; the assertion must prove validation and selection propagation.

- [ ] **Step 2: Run tests to verify they fail**

Run:

```bash
cd backend && GOMODCACHE=/root/Clouds/N2API/.cache/go-mod GOCACHE=/root/Clouds/N2API/.cache/go-build go test ./internal/provider -run MaxConcurrentRequests
```

Expected: FAIL because validation/selection fields are not wired.

- [ ] **Step 3: Implement minimal provider changes**

Add `MaxConcurrentRequests int` to `SelectedAccount`.

In account update validation, reject:

```go
if update.MaxConcurrentRequests != nil && *update.MaxConcurrentRequests < 0 {
	return Account{}, ErrInvalidInput
}
```

In `selectedAccount`, set:

```go
MaxConcurrentRequests: account.MaxConcurrentRequests,
```

- [ ] **Step 4: Run provider tests**

Run:

```bash
cd backend && GOMODCACHE=/root/Clouds/N2API/.cache/go-mod GOCACHE=/root/Clouds/N2API/.cache/go-build go test ./internal/provider -run MaxConcurrentRequests
```

Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add backend/internal/provider/service.go backend/internal/provider/service_test.go
git commit -m "feat: validate provider account concurrency limits"
```

## Task 3: Gateway Enforcement

**Files:**
- Modify: `backend/internal/gateway/proxy_test.go`
- Modify: `backend/internal/gateway/proxy.go`

- [ ] **Step 1: Write failing gateway test**

Add a test in `backend/internal/gateway/proxy_test.go` near the account concurrency tests:

```go
func TestProxyUsesSelectedAccountConcurrencyOverride(t *testing.T) {
	blocker := make(chan struct{})
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") == "Bearer first-token" {
			<-blocker
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"id":"ok","choices":[{"message":{"content":"ok"}}]}`))
	}))
	defer upstream.Close()

	tokens := &fakeSelectedAccountProvider{accounts: []SelectedAccount{
		{AccountID: 1, AuthorizationToken: "first-token", BaseURL: upstream.URL, MaxConcurrentRequests: 1},
		{AccountID: 2, AuthorizationToken: "second-token", BaseURL: upstream.URL},
	}}
	proxy := NewProxy(&fakeAPIKeyAuthenticator{}, tokens, Config{MaxConcurrentRequestsPerAccount: 5})

	firstDone := make(chan int, 1)
	go func() {
		req := newChatCompletionRequest(t, "gpt-5")
		rec := httptest.NewRecorder()
		proxy.ServeHTTP(rec, req)
		firstDone <- rec.Code
	}()

	waitForAccountSelection(t, tokens, 1)

	req := newChatCompletionRequest(t, "gpt-5")
	rec := httptest.NewRecorder()
	proxy.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("second status = %d, want 200 fallback to second account", rec.Code)
	}
	if len(tokens.usedIDs) < 2 || tokens.usedIDs[1] != 2 {
		t.Fatalf("used IDs = %+v, want fallback to account 2", tokens.usedIDs)
	}

	close(blocker)
	if code := <-firstDone; code != http.StatusOK {
		t.Fatalf("first status = %d, want 200", code)
	}
}
```

If `newChatCompletionRequest` or `waitForAccountSelection` are not present, add small local helpers in `proxy_test.go`: the request helper should build a `POST /v1/chat/completions` request with bearer auth and `{"model":"gpt-5","messages":[{"role":"user","content":"hi"}]}`, and the wait helper should poll the fake provider until the expected account id appears in `usedIDs`.

- [ ] **Step 2: Run test to verify it fails**

Run:

```bash
cd backend && GOMODCACHE=/root/Clouds/N2API/.cache/go-mod GOCACHE=/root/Clouds/N2API/.cache/go-build go test ./internal/gateway -run SelectedAccountConcurrencyOverride
```

Expected: FAIL because `SelectedAccount` has no override or gateway ignores it.

- [ ] **Step 3: Implement effective limit**

Add to `backend/internal/gateway/proxy.go`:

```go
func effectiveAccountConcurrencyLimit(accountLimit, defaultLimit int) int {
	if accountLimit > 0 {
		return accountLimit
	}
	return defaultLimit
}
```

Replace the acquire call with:

```go
limit := effectiveAccountConcurrencyLimit(selected.MaxConcurrentRequests, settings.MaxConcurrentRequestsPerAccount)
releaseAccount, ok := p.tryAcquireAccountSlot(selected.AccountID, limit)
```

- [ ] **Step 4: Run gateway tests**

Run:

```bash
cd backend && GOMODCACHE=/root/Clouds/N2API/.cache/go-mod GOCACHE=/root/Clouds/N2API/.cache/go-build go test ./internal/gateway -run 'SelectedAccountConcurrencyOverride|AccountConcurrency'
```

Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add backend/internal/gateway/proxy.go backend/internal/gateway/proxy_test.go
git commit -m "feat: enforce provider account concurrency overrides"
```

## Task 4: Admin API

**Files:**
- Modify: `backend/internal/httpapi/server_test.go`
- Modify: `backend/internal/httpapi/server.go`

- [ ] **Step 1: Write failing HTTP tests**

Add or extend tests so:

```go
PATCH /api/admin/provider-accounts/7
{"maxConcurrentRequests":2}
```

passes `MaxConcurrentRequests=2` to the fake provider update, and:

```go
POST /api/admin/provider-accounts/bulk-update
{"accountIds":[7,8],"maxConcurrentRequests":3}
```

passes `MaxConcurrentRequests=3` for each selected account.

- [ ] **Step 2: Run tests to verify they fail**

Run:

```bash
cd backend && GOMODCACHE=/root/Clouds/N2API/.cache/go-mod GOCACHE=/root/Clouds/N2API/.cache/go-build go test ./internal/httpapi -run 'ProviderAccount.*Concurrency|Bulk.*Concurrency'
```

Expected: FAIL because request structs do not parse or forward the field.

- [ ] **Step 3: Wire request structs**

In `backend/internal/httpapi/server.go`, add:

```go
MaxConcurrentRequests *int `json:"maxConcurrentRequests"`
```

to single and bulk account update request structs, validate non-negative values, and pass the pointer into `provider.AccountUpdate`.

- [ ] **Step 4: Run HTTP tests**

Run:

```bash
cd backend && GOMODCACHE=/root/Clouds/N2API/.cache/go-mod GOCACHE=/root/Clouds/N2API/.cache/go-build go test ./internal/httpapi -run 'ProviderAccount.*Concurrency|Bulk.*Concurrency'
```

Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add backend/internal/httpapi/server.go backend/internal/httpapi/server_test.go
git commit -m "feat: expose provider account concurrency updates"
```

## Task 5: Frontend Controls

**Files:**
- Modify: `frontend/src/routes/providers/provider-page.test.mjs`
- Modify: `frontend/src/lib/admin-state.svelte.js`
- Modify: `frontend/src/routes/providers/+page.svelte`

- [ ] **Step 1: Write failing frontend source test**

Extend provider page source tests to require:

```js
assert.match(source, /Max concurrency/);
assert.match(source, /provider-account-max-concurrency/);
assert.match(source, /Bulk max concurrency/);
assert.match(adminStateSource, /maxConcurrentRequests/);
assert.match(adminStateSource, /providerAccountBulkSchedulingForm/);
```

- [ ] **Step 2: Run test to verify it fails**

Run:

```bash
cd frontend && bun test src/routes/providers/provider-page.test.mjs
```

Expected: FAIL because controls are missing.

- [ ] **Step 3: Add state and validation**

In `admin-state.svelte.js`, add `maxConcurrentRequests` to `ProviderAccount` JSDoc and update the bulk scheduling form:

```js
export const providerAccountBulkSchedulingForm = $state({ priority: '', loadFactor: '', maxConcurrentRequests: '' });
```

Include `maxConcurrentRequests` in single account update payloads and bulk scheduling payloads. Validate bulk text with non-negative whole number semantics.

- [ ] **Step 4: Add controls**

In `providers/+page.svelte`, add a row input:

```svelte
<label class="sr-only" for={`provider-account-max-concurrency-${account.id}`}>Max concurrency</label>
<input
  id={`provider-account-max-concurrency-${account.id}`}
  class="w-24 rounded-md border border-[#e5e5e5] bg-white px-2 py-1.5 font-mono text-sm text-[#0d0d0d] outline-none focus:border-[#10a37f] focus:ring-2 focus:ring-[#e8f5f0]"
  type="number"
  min="0"
  value={account.maxConcurrentRequests}
  onchange={(event) => updateProviderAccountMaxConcurrentRequests(account, event)}
/>
```

Add a bulk input labeled **Bulk max concurrency** to the scheduling action bar.

- [ ] **Step 5: Run frontend tests**

Run:

```bash
cd frontend && bun test src/routes/providers/provider-page.test.mjs
cd frontend && bun run check
```

Expected: PASS.

- [ ] **Step 6: Commit**

```bash
git add frontend/src/lib/admin-state.svelte.js frontend/src/routes/providers/+page.svelte frontend/src/routes/providers/provider-page.test.mjs
git commit -m "feat: add provider account concurrency controls"
```

## Task 6: Documentation And Full Verification

**Files:**
- Modify: `backend/internal/gateway/documentation_test.go`
- Modify: `README.md`
- Modify: `deploy/README.md`

- [ ] **Step 1: Write failing documentation test**

Add a test requiring:

```go
"Max concurrency",
"inherits the gateway default",
"per-account concurrency"
```

in README and deploy notes.

- [ ] **Step 2: Run test to verify it fails**

Run:

```bash
cd backend && GOMODCACHE=/root/Clouds/N2API/.cache/go-mod GOCACHE=/root/Clouds/N2API/.cache/go-build go test ./internal/gateway -run ProviderAccountConcurrencyDocumentation
```

Expected: FAIL because docs do not describe the new override.

- [ ] **Step 3: Update docs**

Add one short paragraph to README and deploy notes explaining that Gateway Settings defines the default per-account concurrency and provider account **Max concurrency** overrides it; `0` inherits the gateway default.

- [ ] **Step 4: Run final verification**

Run:

```bash
git diff --check
cd backend && GOMODCACHE=/root/Clouds/N2API/.cache/go-mod GOCACHE=/root/Clouds/N2API/.cache/go-build go test ./...
cd frontend && bun test src/routes/providers/provider-page.test.mjs
cd frontend && bun test src/routes/navigation.test.mjs
cd frontend && bun run check
cd frontend && bun run build
git status --short
```

Expected: all commands exit 0 and `git status --short` is empty after commit.

- [ ] **Step 5: Commit**

```bash
git add README.md deploy/README.md backend/internal/gateway/documentation_test.go
git commit -m "docs: document provider account concurrency"
```

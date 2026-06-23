# Provider Account Test History Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Persist and expose recent provider account test results for manual and automatic account probes.

**Architecture:** Add a PostgreSQL history table beside existing `provider_accounts.last_test_*` fields. Keep `RecordAccountTestResult` as the single write path so manual tests, test-all, and auto tests all create history rows. Expose account-scoped recent history through provider service and the existing admin HTTP API.

**Tech Stack:** Go backend, PostgreSQL migrations, provider repository/service, admin HTTP API.

---

## File Structure

- `backend/internal/store/migrations/00018_provider_account_test_result_history.sql`: create/drop history table and indexes.
- `backend/internal/store/migrations_test.go`: assert migration is embedded.
- `backend/internal/store/provider.go` and `provider_test.go`: insert and list test history.
- `backend/internal/provider/service.go` and `service_test.go`: add domain type and service method.
- `backend/internal/httpapi/server.go` and `server_test.go`: add admin endpoint.
- `README.md`, `deploy/README.md`, `backend/internal/gateway/documentation_test.go`: document the backend API/history behavior.

## Task 1: Store Test History

**Files:**
- Create: `backend/internal/store/migrations/00018_provider_account_test_result_history.sql`
- Modify: `backend/internal/store/migrations_test.go`
- Modify: `backend/internal/store/provider.go`
- Modify: `backend/internal/store/provider_test.go`

- [ ] **Step 1: Write failing migration test**

Add `TestProviderAccountTestResultHistoryMigrationIsEmbedded` asserting the migration includes `CREATE TABLE IF NOT EXISTS provider_account_test_results`, `account_id BIGINT NOT NULL REFERENCES provider_accounts(id) ON DELETE CASCADE`, `provider_account_test_results_account_idx`, and `DROP TABLE IF EXISTS provider_account_test_results`.

- [ ] **Step 2: Write failing repository test**

Extend `TestProviderRepositoryRecordAccountTestResult` or add a new test that records two results for one account, then calls `ListAccountTestResults(ctx, "openai", saved.ID, 10)` and expects newest-first rows with status/message/checked_at preserved.

- [ ] **Step 3: Run failing store tests**

Run:

```bash
cd backend
GOMODCACHE=/root/Clouds/N2API/.cache/go-mod GOCACHE=/root/Clouds/N2API/.cache/go-build go test ./internal/store -run 'ProviderAccountTestResult'
```

Expected: FAIL because the migration and repository method do not exist.

- [ ] **Step 4: Implement migration and repository methods**

Create the migration. Add `provider.AccountTestResult` type and repository method:

```go
ListAccountTestResults(ctx context.Context, provider string, accountID int64, limit int) ([]provider.AccountTestResult, error)
```

Update `RecordAccountTestResult` to insert a history row after updating the latest fields.

- [ ] **Step 5: Run store tests**

Run:

```bash
cd backend
GOMODCACHE=/root/Clouds/N2API/.cache/go-mod GOCACHE=/root/Clouds/N2API/.cache/go-build go test ./internal/store -run 'ProviderAccountTestResult'
```

Expected: PASS.

- [ ] **Step 6: Commit**

```bash
git add backend/internal/store/migrations/00018_provider_account_test_result_history.sql backend/internal/store/migrations_test.go backend/internal/store/provider.go backend/internal/store/provider_test.go backend/internal/provider/service.go
git commit -m "feat: record provider account test history"
```

## Task 2: Service And HTTP API

**Files:**
- Modify: `backend/internal/provider/service.go`
- Modify: `backend/internal/provider/service_test.go`
- Modify: `backend/internal/httpapi/server.go`
- Modify: `backend/internal/httpapi/server_test.go`

- [ ] **Step 1: Write failing provider service test**

Add a test for `ListAccountTestResults(ctx, id, limit)` proving invalid IDs return `ErrInvalidInput`, default/too-high limits normalize, and repository rows are returned.

- [ ] **Step 2: Write failing HTTP tests**

Add tests for `GET /api/admin/provider-accounts/{id}/test-results?limit=2` requiring session, returning rows, and mapping unknown account to `404`.

- [ ] **Step 3: Run failing tests**

Run:

```bash
cd backend
GOMODCACHE=/root/Clouds/N2API/.cache/go-mod GOCACHE=/root/Clouds/N2API/.cache/go-build go test ./internal/provider ./internal/httpapi -run 'Test.*TestResults'
```

Expected: FAIL because service and HTTP methods do not exist.

- [ ] **Step 4: Implement service and HTTP endpoint**

Add `ListAccountTestResults` to provider repository/service interfaces and implementation. Add the admin route and handler:

`GET /api/admin/provider-accounts/{id}/test-results`

Use `limit` query parsing with default `20` and max `100`.

- [ ] **Step 5: Run service and HTTP tests**

Run:

```bash
cd backend
GOMODCACHE=/root/Clouds/N2API/.cache/go-mod GOCACHE=/root/Clouds/N2API/.cache/go-build go test ./internal/provider ./internal/httpapi -run 'Test.*TestResults'
```

Expected: PASS.

- [ ] **Step 6: Commit**

```bash
git add backend/internal/provider/service.go backend/internal/provider/service_test.go backend/internal/httpapi/server.go backend/internal/httpapi/server_test.go
git commit -m "feat: expose provider account test history"
```

## Task 3: Documentation And Verification

**Files:**
- Modify: `README.md`
- Modify: `deploy/README.md`
- Modify: `backend/internal/gateway/documentation_test.go`

- [ ] **Step 1: Add failing documentation test**

Require README and deploy README to mention `test-results`, `test history`, and `last test`.

- [ ] **Step 2: Update docs**

Document that manual and automatic provider account tests write recent history rows available through the admin API.

- [ ] **Step 3: Run documentation test**

Run:

```bash
cd backend
GOMODCACHE=/root/Clouds/N2API/.cache/go-mod GOCACHE=/root/Clouds/N2API/.cache/go-build go test ./internal/gateway -run 'Documentation'
```

Expected: PASS.

- [ ] **Step 4: Run full verification**

Run:

```bash
git diff --check
cd backend
GOMODCACHE=/root/Clouds/N2API/.cache/go-mod GOCACHE=/root/Clouds/N2API/.cache/go-build go test ./...
cd ../frontend
bun run check
bun run build
```

Expected: all commands exit 0. If sandbox blocks `httptest` sockets, rerun backend tests with the approved escalation path and record it.

- [ ] **Step 5: Commit**

```bash
git add README.md deploy/README.md backend/internal/gateway/documentation_test.go
git commit -m "docs: document provider account test history"
```

# API Key Budget Cutoff Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add durable per-client API key request/token budget cutoffs over 24h and 30d windows, with admin configuration, gateway enforcement, diagnostics, and UI visibility.

**Architecture:** Store budget settings on `client_api_keys` and derive current usage from durable `request_logs`. Admin service and store expose budget settings and derived usage to HTTP/UI, while the gateway checks budget usage immediately after API key authentication and before account selection. Budget rejections remain OpenAI-compatible for clients and use precise local request-log error codes for admin diagnostics.

**Tech Stack:** Go backend with PostgreSQL migrations, pgx repositories, `net/http` gateway/admin APIs, SvelteKit + Tailwind admin UI, Bun frontend verification.

---

## File Structure

- Create `backend/internal/store/migrations/00023_client_api_key_budgets.sql`
  - Adds four `client_api_keys` budget columns.
- Modify `backend/internal/store/migrations_test.go`
  - Asserts migration embedding and updates embedded migration count/path.
- Modify `backend/internal/admin/service.go`
  - Adds budget fields to `APIKey`, `APIKeyBudgetUsage`, service validation, and repository methods.
- Modify `backend/internal/admin/service_test.go`
  - Covers budget validation and memory repository behavior.
- Modify `backend/internal/store/admin.go`
  - Extends `apiKeyColumns`, scanning, budget update, and budget usage aggregation from `request_logs`.
- Modify `backend/internal/store/admin_test.go`
  - Covers persisted budget fields, revoked-key refusal, and usage aggregation.
- Modify `backend/internal/httpapi/server.go`
  - Adds `PUT /api/admin/keys/{id}/budgets` and includes budget usage in API key list responses.
- Modify `backend/internal/httpapi/server_test.go`
  - Covers endpoint success/error mapping and list response budget fields.
- Modify `backend/internal/gateway/proxy.go`
  - Adds optional `APIKeyBudgetProvider`, checks budgets before rate/concurrency/account selection, logs precise local errors.
- Modify `backend/internal/gateway/proxy_test.go`
  - Covers request and token budget rejections plus below-budget pass-through.
- Modify `backend/internal/store/gateway_test.go`
  - Ensures request log insertion can store new budget error codes through the existing `error` field.
- Modify `frontend/src/lib/admin-state.svelte.js`
  - Adds budget fields to API key typedef, budget save function, and budget usage formatting helpers.
- Modify `frontend/src/routes/api-keys/+page.svelte`
  - Adds budget inputs, save action, usage display, exceeded labels, and search text.
- Modify `frontend/src/routes/navigation.test.mjs`
  - Adds static checks for API key budget UI and state calls.
- Modify `backend/internal/gateway/documentation_test.go`
  - Adds documentation guard for API key budget cutoffs and local log reasons.
- Modify `README.md` and `deploy/README.md`
  - Documents personal API key budgets, `0` disabled semantics, durable rolling-window source, and no billing/payment behavior.

## Task 1: Schema and Admin Types

**Files:**
- Create: `backend/internal/store/migrations/00023_client_api_key_budgets.sql`
- Modify: `backend/internal/store/migrations_test.go`
- Modify: `backend/internal/admin/service.go`
- Modify: `backend/internal/admin/service_test.go`
- Modify: `backend/internal/store/admin.go`
- Modify: `backend/internal/store/admin_test.go`

- [ ] **Step 1: Write failing migration tests**

Add this test to `backend/internal/store/migrations_test.go` near the other client API key migration tests:

```go
func TestClientAPIKeyBudgetsMigrationIsEmbedded(t *testing.T) {
	sql, err := MigrationSQL("00023_client_api_key_budgets.sql")
	if err != nil {
		t.Fatalf("MigrationSQL returned error: %v", err)
	}
	for _, want := range []string{
		"ALTER TABLE client_api_keys ADD COLUMN IF NOT EXISTS request_budget_24h INTEGER NOT NULL DEFAULT 0",
		"ALTER TABLE client_api_keys ADD COLUMN IF NOT EXISTS token_budget_24h INTEGER NOT NULL DEFAULT 0",
		"ALTER TABLE client_api_keys ADD COLUMN IF NOT EXISTS request_budget_30d INTEGER NOT NULL DEFAULT 0",
		"ALTER TABLE client_api_keys ADD COLUMN IF NOT EXISTS token_budget_30d INTEGER NOT NULL DEFAULT 0",
		"ALTER TABLE client_api_keys DROP COLUMN IF EXISTS token_budget_30d",
		"ALTER TABLE client_api_keys DROP COLUMN IF EXISTS request_budget_30d",
		"ALTER TABLE client_api_keys DROP COLUMN IF EXISTS token_budget_24h",
		"ALTER TABLE client_api_keys DROP COLUMN IF EXISTS request_budget_24h",
	} {
		if !strings.Contains(sql, want) {
			t.Fatalf("migration missing %q", want)
		}
	}
}
```

Update `TestMigrationProviderSeesEmbeddedMigrations` expected count from `22` to `23`, and update the final path assertion from `00022_client_api_key_disabled_at.sql` to `00023_client_api_key_budgets.sql`.

- [ ] **Step 2: Run migration tests to verify red**

Run:

```bash
cd backend
GOMODCACHE=/root/Clouds/N2API/.cache/go-mod GOCACHE=/root/Clouds/N2API/.cache/go-build go test ./internal/store -run 'ClientAPIKeyBudgets|MigrationProviderSeesEmbeddedMigrations'
```

Expected: FAIL because `00023_client_api_key_budgets.sql` is missing and the migration count/path no longer matches.

- [ ] **Step 3: Add migration**

Create `backend/internal/store/migrations/00023_client_api_key_budgets.sql`:

```sql
-- +goose Up
ALTER TABLE client_api_keys ADD COLUMN IF NOT EXISTS request_budget_24h INTEGER NOT NULL DEFAULT 0;
ALTER TABLE client_api_keys ADD COLUMN IF NOT EXISTS token_budget_24h INTEGER NOT NULL DEFAULT 0;
ALTER TABLE client_api_keys ADD COLUMN IF NOT EXISTS request_budget_30d INTEGER NOT NULL DEFAULT 0;
ALTER TABLE client_api_keys ADD COLUMN IF NOT EXISTS token_budget_30d INTEGER NOT NULL DEFAULT 0;

-- +goose Down
ALTER TABLE client_api_keys DROP COLUMN IF EXISTS token_budget_30d;
ALTER TABLE client_api_keys DROP COLUMN IF EXISTS request_budget_30d;
ALTER TABLE client_api_keys DROP COLUMN IF EXISTS token_budget_24h;
ALTER TABLE client_api_keys DROP COLUMN IF EXISTS request_budget_24h;
```

- [ ] **Step 4: Write failing service/store budget tests**

In `backend/internal/admin/service_test.go`, add:

```go
func TestUpdateAPIKeyBudgetsValidatesNonNegativeValues(t *testing.T) {
	repo := newMemoryRepo()
	service := NewService(repo, Config{})
	result, err := service.CreateAPIKey(context.Background(), "codex")
	if err != nil {
		t.Fatalf("CreateAPIKey returned error: %v", err)
	}

	updated, err := service.UpdateAPIKeyBudgets(context.Background(), result.Key.ID, 10, 1000, 300, 30000)
	if err != nil {
		t.Fatalf("UpdateAPIKeyBudgets returned error: %v", err)
	}
	if updated.RequestBudget24h != 10 || updated.TokenBudget24h != 1000 || updated.RequestBudget30d != 300 || updated.TokenBudget30d != 30000 {
		t.Fatalf("budgets = %+v, want configured values", updated)
	}

	if _, err := service.UpdateAPIKeyBudgets(context.Background(), result.Key.ID, -1, 0, 0, 0); !errors.Is(err, ErrInvalidInput) {
		t.Fatalf("negative requestBudget24h error = %v, want ErrInvalidInput", err)
	}
	if _, err := service.UpdateAPIKeyBudgets(context.Background(), result.Key.ID, 0, -1, 0, 0); !errors.Is(err, ErrInvalidInput) {
		t.Fatalf("negative tokenBudget24h error = %v, want ErrInvalidInput", err)
	}
	if _, err := service.UpdateAPIKeyBudgets(context.Background(), result.Key.ID, 0, 0, -1, 0); !errors.Is(err, ErrInvalidInput) {
		t.Fatalf("negative requestBudget30d error = %v, want ErrInvalidInput", err)
	}
	if _, err := service.UpdateAPIKeyBudgets(context.Background(), result.Key.ID, 0, 0, 0, -1); !errors.Is(err, ErrInvalidInput) {
		t.Fatalf("negative tokenBudget30d error = %v, want ErrInvalidInput", err)
	}
}
```

In the `memoryRepo` inside `backend/internal/admin/service_test.go`, add the method that the test expects:

```go
func (r *memoryRepo) UpdateAPIKeyBudgets(_ context.Context, id int64, requestBudget24h, tokenBudget24h, requestBudget30d, tokenBudget30d int) (APIKey, error) {
	for i, key := range r.keys {
		if key.ID == id && key.RevokedAt == nil {
			key.RequestBudget24h = requestBudget24h
			key.TokenBudget24h = tokenBudget24h
			key.RequestBudget30d = requestBudget30d
			key.TokenBudget30d = tokenBudget30d
			r.keys[i] = key
			return key, nil
		}
	}
	return APIKey{}, ErrNotFound
}
```

In `backend/internal/store/admin_test.go`, extend the existing API key behavior test with:

```go
	budgeted, err := repo.UpdateAPIKeyBudgets(ctx, created.ID, 12, 1200, 300, 30000)
	if err != nil {
		t.Fatalf("UpdateAPIKeyBudgets returned error: %v", err)
	}
	if budgeted.RequestBudget24h != 12 || budgeted.TokenBudget24h != 1200 || budgeted.RequestBudget30d != 300 || budgeted.TokenBudget30d != 30000 {
		t.Fatalf("budgeted key = %+v", budgeted)
	}
	keys, err := repo.ListAPIKeys(ctx)
	if err != nil {
		t.Fatalf("ListAPIKeys returned error: %v", err)
	}
	if len(keys) != 1 || keys[0].RequestBudget24h != 12 || keys[0].TokenBudget30d != 30000 {
		t.Fatalf("listed budget fields = %+v", keys)
	}
```

After the existing revoke assertion in that test, add:

```go
	if _, err := repo.UpdateAPIKeyBudgets(ctx, created.ID, 1, 1, 1, 1); !errors.Is(err, admin.ErrNotFound) {
		t.Fatalf("UpdateAPIKeyBudgets revoked error = %v, want ErrNotFound", err)
	}
```

- [ ] **Step 5: Run service/store tests to verify red**

Run:

```bash
cd backend
GOMODCACHE=/root/Clouds/N2API/.cache/go-mod GOCACHE=/root/Clouds/N2API/.cache/go-build go test ./internal/admin ./internal/store -run 'Budgets|Budget'
```

Expected: FAIL because `APIKey` lacks budget fields, repository interfaces lack `UpdateAPIKeyBudgets`, and the store does not scan budget columns.

- [ ] **Step 6: Implement admin API key budget fields**

In `backend/internal/admin/service.go`, extend `APIKey`:

```go
	RequestBudget24h  int        `json:"requestBudget24h"`
	TokenBudget24h    int        `json:"tokenBudget24h"`
	RequestBudget30d  int        `json:"requestBudget30d"`
	TokenBudget30d    int        `json:"tokenBudget30d"`
```

Add to `Repository`:

```go
	UpdateAPIKeyBudgets(ctx context.Context, id int64, requestBudget24h, tokenBudget24h, requestBudget30d, tokenBudget30d int) (APIKey, error)
```

Add service method:

```go
func (s *Service) UpdateAPIKeyBudgets(ctx context.Context, id int64, requestBudget24h, tokenBudget24h, requestBudget30d, tokenBudget30d int) (APIKey, error) {
	if requestBudget24h < 0 || tokenBudget24h < 0 || requestBudget30d < 0 || tokenBudget30d < 0 {
		return APIKey{}, ErrInvalidInput
	}
	return s.repo.UpdateAPIKeyBudgets(ctx, id, requestBudget24h, tokenBudget24h, requestBudget30d, tokenBudget30d)
}
```

- [ ] **Step 7: Implement store scan/update fields**

In `backend/internal/store/admin.go`, update `apiKeyColumns`:

```go
const apiKeyColumns = "id, name, prefix, created_at, last_used_at, revoked_at, disabled_at, model_policy, requests_per_minute, tokens_per_minute, request_budget_24h, token_budget_24h, request_budget_30d, token_budget_30d"
```

Extend `scanAPIKey` with:

```go
		&key.RequestBudget24h,
		&key.TokenBudget24h,
		&key.RequestBudget30d,
		&key.TokenBudget30d,
```

Add repository method near `UpdateAPIKeyLimits`:

```go
func (r *AdminRepository) UpdateAPIKeyBudgets(ctx context.Context, id int64, requestBudget24h, tokenBudget24h, requestBudget30d, tokenBudget30d int) (admin.APIKey, error) {
	var updated admin.APIKey
	err := r.pool.QueryRow(ctx, `
		UPDATE client_api_keys
		SET request_budget_24h = $2,
			token_budget_24h = $3,
			request_budget_30d = $4,
			token_budget_30d = $5
		WHERE id = $1
			AND revoked_at IS NULL
		RETURNING `+apiKeyColumns+`
	`, id, requestBudget24h, tokenBudget24h, requestBudget30d, tokenBudget30d).Scan(scanAPIKey(&updated)...)
	if errors.Is(err, pgx.ErrNoRows) {
		return admin.APIKey{}, admin.ErrNotFound
	}
	if err != nil {
		return admin.APIKey{}, err
	}
	if updated.ModelPolicy == admin.APIKeyModelPolicySelected {
		models, err := r.ListAPIKeyModels(ctx, updated.ID)
		if err != nil {
			return admin.APIKey{}, err
		}
		updated.AllowedModels = models
	}
	return updated, nil
}
```

- [ ] **Step 8: Run targeted tests**

Run:

```bash
cd backend
GOMODCACHE=/root/Clouds/N2API/.cache/go-mod GOCACHE=/root/Clouds/N2API/.cache/go-build go test ./internal/admin ./internal/store -run 'Budgets|Budget|ClientAPIKeyBudgets|MigrationProviderSeesEmbeddedMigrations'
```

Expected: PASS.

- [ ] **Step 9: Commit**

```bash
git add backend/internal/store/migrations/00023_client_api_key_budgets.sql backend/internal/store/migrations_test.go backend/internal/admin/service.go backend/internal/admin/service_test.go backend/internal/store/admin.go backend/internal/store/admin_test.go
git commit -m "feat: store api key budgets"
```

## Task 2: Budget Usage Aggregation and Admin List State

**Files:**
- Modify: `backend/internal/admin/service.go`
- Modify: `backend/internal/admin/service_test.go`
- Modify: `backend/internal/store/admin.go`
- Modify: `backend/internal/store/admin_test.go`
- Modify: `backend/internal/httpapi/server.go`
- Modify: `backend/internal/httpapi/server_test.go`

- [ ] **Step 1: Write failing admin/store tests for budget usage**

In `backend/internal/admin/service.go`, the implementation will add these types; write tests against them first.

In `backend/internal/admin/service_test.go`, add:

```go
func TestAPIKeyBudgetUsageComputesRemainingAndExceeded(t *testing.T) {
	repo := newMemoryRepo()
	service := NewService(repo, Config{})
	now := time.Unix(10_000, 0).UTC()
	key := APIKey{
		ID:               7,
		Name:             "codex",
		RequestBudget24h: 3,
		TokenBudget24h:   100,
		RequestBudget30d: 10,
		TokenBudget30d:   1000,
	}
	repo.budgetUsage = map[int64]APIKeyBudgetUsage{
		7: {
			KeyID:          7,
			RequestsUsed24h: 3,
			TokensUsed24h:   80,
			RequestsUsed30d: 4,
			TokensUsed30d:   1000,
		},
	}

	usage, err := service.GetAPIKeyBudgetUsage(context.Background(), key, now)
	if err != nil {
		t.Fatalf("GetAPIKeyBudgetUsage returned error: %v", err)
	}
	if usage.RequestsRemaining24h == nil || *usage.RequestsRemaining24h != 0 || !usage.RequestBudgetExceeded {
		t.Fatalf("request remaining/exceeded = %+v", usage)
	}
	if usage.TokensRemaining24h == nil || *usage.TokensRemaining24h != 20 {
		t.Fatalf("24h token remaining = %+v, want 20", usage.TokensRemaining24h)
	}
	if usage.TokensRemaining30d == nil || *usage.TokensRemaining30d != 0 || !usage.TokenBudgetExceeded {
		t.Fatalf("30d token remaining/exceeded = %+v", usage)
	}
}
```

Add to `memoryRepo`:

```go
budgetUsage map[int64]APIKeyBudgetUsage
```

and:

```go
func (r *memoryRepo) GetAPIKeyBudgetUsage(_ context.Context, keyID int64, _ time.Time) (APIKeyBudgetUsage, error) {
	if usage, ok := r.budgetUsage[keyID]; ok {
		return usage, nil
	}
	return APIKeyBudgetUsage{KeyID: keyID}, nil
}
```

In `backend/internal/store/admin_test.go`, add a new test using real request logs:

```go
func TestAdminRepositoryAPIKeyBudgetUsageAggregatesRequestLogs(t *testing.T) {
	ctx := context.Background()
	repo := newTestAdminRepository(t)
	key, err := repo.CreateAPIKey(ctx, "budgeted", "hash-budgeted", "n2api_b")
	if err != nil {
		t.Fatalf("CreateAPIKey returned error: %v", err)
	}
	now := time.Unix(20_000, 0).UTC()

	insertRequestLog(t, repo.pool, key.ID, now.Add(-time.Hour), 200, 70)
	insertRequestLog(t, repo.pool, key.ID, now.Add(-2*time.Hour), 429, 0)
	insertRequestLog(t, repo.pool, key.ID, now.Add(-48*time.Hour), 200, 30)

	usage, err := repo.GetAPIKeyBudgetUsage(ctx, key.ID, now)
	if err != nil {
		t.Fatalf("GetAPIKeyBudgetUsage returned error: %v", err)
	}
	if usage.RequestsUsed24h != 2 || usage.TokensUsed24h != 70 {
		t.Fatalf("24h usage = %+v, want 2 requests and 70 tokens", usage)
	}
	if usage.RequestsUsed30d != 3 || usage.TokensUsed30d != 100 {
		t.Fatalf("30d usage = %+v, want 3 requests and 100 tokens", usage)
	}
}
```

If no helper exists, add this test helper in `backend/internal/store/admin_test.go`:

```go
func insertRequestLog(t *testing.T, pool *pgxpool.Pool, keyID int64, createdAt time.Time, statusCode, totalTokens int) {
	t.Helper()
	_, err := pool.Exec(context.Background(), `
		INSERT INTO request_logs (
			request_id, client_key_id, provider, route, method, status_code, latency_ms,
			total_tokens, usage_source, created_at
		)
		VALUES ($1, $2, 'openai', '/v1/responses', 'POST', $3, 12, $4, 'test', $5)
	`, "req-budget-"+strconv.FormatInt(createdAt.Unix(), 10), keyID, statusCode, totalTokens, createdAt)
	if err != nil {
		t.Fatalf("insert request log: %v", err)
	}
}
```

- [ ] **Step 2: Run tests to verify red**

Run:

```bash
cd backend
GOMODCACHE=/root/Clouds/N2API/.cache/go-mod GOCACHE=/root/Clouds/N2API/.cache/go-build go test ./internal/admin ./internal/store -run 'BudgetUsage|Budget'
```

Expected: FAIL because `APIKeyBudgetUsage` and `GetAPIKeyBudgetUsage` do not exist.

- [ ] **Step 3: Implement admin budget usage types and service method**

In `backend/internal/admin/service.go`, add:

```go
type APIKeyBudgetUsage struct {
	KeyID                   int64  `json:"-"`
	RequestsUsed24h         int64  `json:"requestsUsed24h"`
	TokensUsed24h           int64  `json:"tokensUsed24h"`
	RequestsUsed30d         int64  `json:"requestsUsed30d"`
	TokensUsed30d           int64  `json:"tokensUsed30d"`
	RequestsRemaining24h    *int64 `json:"requestsRemaining24h"`
	TokensRemaining24h      *int64 `json:"tokensRemaining24h"`
	RequestsRemaining30d    *int64 `json:"requestsRemaining30d"`
	TokensRemaining30d      *int64 `json:"tokensRemaining30d"`
	RequestBudgetExceeded   bool   `json:"requestBudgetExceeded"`
	TokenBudgetExceeded     bool   `json:"tokenBudgetExceeded"`
}
```

Add to `Repository`:

```go
	GetAPIKeyBudgetUsage(ctx context.Context, keyID int64, now time.Time) (APIKeyBudgetUsage, error)
```

Add service method:

```go
func (s *Service) GetAPIKeyBudgetUsage(ctx context.Context, key APIKey, now time.Time) (APIKeyBudgetUsage, error) {
	usage, err := s.repo.GetAPIKeyBudgetUsage(ctx, key.ID, now)
	if err != nil {
		return APIKeyBudgetUsage{}, err
	}
	usage.KeyID = key.ID
	applyBudgetRemaining(&usage, key)
	return usage, nil
}
```

Add helper:

```go
func applyBudgetRemaining(usage *APIKeyBudgetUsage, key APIKey) {
	if key.RequestBudget24h > 0 {
		remaining := int64(key.RequestBudget24h) - usage.RequestsUsed24h
		if remaining < 0 {
			remaining = 0
		}
		usage.RequestsRemaining24h = &remaining
		if usage.RequestsUsed24h >= int64(key.RequestBudget24h) {
			usage.RequestBudgetExceeded = true
		}
	}
	if key.TokenBudget24h > 0 {
		remaining := int64(key.TokenBudget24h) - usage.TokensUsed24h
		if remaining < 0 {
			remaining = 0
		}
		usage.TokensRemaining24h = &remaining
		if usage.TokensUsed24h >= int64(key.TokenBudget24h) {
			usage.TokenBudgetExceeded = true
		}
	}
	if key.RequestBudget30d > 0 {
		remaining := int64(key.RequestBudget30d) - usage.RequestsUsed30d
		if remaining < 0 {
			remaining = 0
		}
		usage.RequestsRemaining30d = &remaining
		if usage.RequestsUsed30d >= int64(key.RequestBudget30d) {
			usage.RequestBudgetExceeded = true
		}
	}
	if key.TokenBudget30d > 0 {
		remaining := int64(key.TokenBudget30d) - usage.TokensUsed30d
		if remaining < 0 {
			remaining = 0
		}
		usage.TokensRemaining30d = &remaining
		if usage.TokensUsed30d >= int64(key.TokenBudget30d) {
			usage.TokenBudgetExceeded = true
		}
	}
}
```

- [ ] **Step 4: Implement store budget usage aggregation**

In `backend/internal/store/admin.go`, add:

```go
func (r *AdminRepository) GetAPIKeyBudgetUsage(ctx context.Context, keyID int64, now time.Time) (admin.APIKeyBudgetUsage, error) {
	var usage admin.APIKeyBudgetUsage
	err := r.pool.QueryRow(ctx, `
		SELECT
			COALESCE(COUNT(*) FILTER (WHERE created_at >= $2), 0),
			COALESCE(SUM(total_tokens) FILTER (WHERE created_at >= $2), 0),
			COALESCE(COUNT(*) FILTER (WHERE created_at >= $3), 0),
			COALESCE(SUM(total_tokens) FILTER (WHERE created_at >= $3), 0)
		FROM request_logs
		WHERE client_key_id = $1
	`, keyID, now.Add(-24*time.Hour), now.Add(-30*24*time.Hour)).Scan(
		&usage.RequestsUsed24h,
		&usage.TokensUsed24h,
		&usage.RequestsUsed30d,
		&usage.TokensUsed30d,
	)
	if err != nil {
		return admin.APIKeyBudgetUsage{}, err
	}
	usage.KeyID = keyID
	return usage, nil
}
```

- [ ] **Step 5: Extend HTTP API key list response**

In `backend/internal/httpapi/server.go`, update `AdminService`:

```go
	GetAPIKeyBudgetUsage(ctx context.Context, key admin.APIKey, now time.Time) (admin.APIKeyBudgetUsage, error)
```

Extend `apiKeyResponse`:

```go
	admin.APIKeyBudgetUsage
```

Change `apiKeyResponses` signature to accept budget usage:

```go
func apiKeyResponses(keys []admin.APIKey, budgetUsage map[int64]admin.APIKeyBudgetUsage, settings admin.GatewaySettings, concurrency, requestRate, tokenRate map[int64]int) []apiKeyResponse
```

In `GET /api/admin/keys`, build `budgetUsage := map[int64]admin.APIKeyBudgetUsage{}` and for each key call:

```go
usage, err := admins.GetAPIKeyBudgetUsage(r.Context(), key, time.Now())
if err != nil {
	writeError(w, http.StatusInternalServerError, "internal_error")
	return
}
budgetUsage[key.ID] = usage
```

Pass the map to `apiKeyResponses`. In the response loop, set `APIKeyBudgetUsage: budgetUsage[key.ID]`.

Update existing tests and fakes so `fakeAdminService` implements `GetAPIKeyBudgetUsage`.

- [ ] **Step 6: Run targeted tests**

Run:

```bash
cd backend
GOMODCACHE=/root/Clouds/N2API/.cache/go-mod GOCACHE=/root/Clouds/N2API/.cache/go-build go test ./internal/admin ./internal/store ./internal/httpapi -run 'BudgetUsage|ListAPIKeys'
```

Expected: PASS.

- [ ] **Step 7: Commit**

```bash
git add backend/internal/admin/service.go backend/internal/admin/service_test.go backend/internal/store/admin.go backend/internal/store/admin_test.go backend/internal/httpapi/server.go backend/internal/httpapi/server_test.go
git commit -m "feat: show api key budget usage"
```

## Task 3: Admin Budget Update Endpoint

**Files:**
- Modify: `backend/internal/httpapi/server.go`
- Modify: `backend/internal/httpapi/server_test.go`

- [ ] **Step 1: Write failing endpoint tests**

In `backend/internal/httpapi/server_test.go`, add fields to `fakeAdminService`:

```go
budgetKeyID       int64
requestBudget24h int
tokenBudget24h   int
requestBudget30d int
tokenBudget30d   int
budgetErr         error
```

Add method:

```go
func (s *fakeAdminService) UpdateAPIKeyBudgets(_ context.Context, id int64, requestBudget24h, tokenBudget24h, requestBudget30d, tokenBudget30d int) (admin.APIKey, error) {
	s.budgetKeyID = id
	s.requestBudget24h = requestBudget24h
	s.tokenBudget24h = tokenBudget24h
	s.requestBudget30d = requestBudget30d
	s.tokenBudget30d = tokenBudget30d
	if s.budgetErr != nil {
		return admin.APIKey{}, s.budgetErr
	}
	for i, key := range s.keys {
		if key.ID == id {
			key.RequestBudget24h = requestBudget24h
			key.TokenBudget24h = tokenBudget24h
			key.RequestBudget30d = requestBudget30d
			key.TokenBudget30d = tokenBudget30d
			s.keys[i] = key
			return key, nil
		}
	}
	return admin.APIKey{}, admin.ErrNotFound
}
```

Add test:

```go
func TestUpdateAPIKeyBudgetsEndpoint(t *testing.T) {
	admins := newFakeAdminService()
	server := NewServer(config.Config{}, staticHealth{}, admins, nil)
	req := httptest.NewRequest(http.MethodPut, "/api/admin/keys/7/budgets", strings.NewReader(`{"requestBudget24h":10,"tokenBudget24h":1000,"requestBudget30d":300,"tokenBudget30d":30000}`))
	req.AddCookie(&http.Cookie{Name: adminSessionCookieName, Value: "valid-session"})
	recorder := httptest.NewRecorder()

	server.ServeHTTP(recorder, req)

	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", recorder.Code, recorder.Body.String())
	}
	var body struct {
		Key admin.APIKey `json:"key"`
	}
	if err := json.NewDecoder(recorder.Body).Decode(&body); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if body.Key.RequestBudget24h != 10 || body.Key.TokenBudget24h != 1000 || body.Key.RequestBudget30d != 300 || body.Key.TokenBudget30d != 30000 {
		t.Fatalf("key budgets = %+v", body.Key)
	}
	if admins.budgetKeyID != 7 || admins.requestBudget24h != 10 || admins.tokenBudget30d != 30000 {
		t.Fatalf("budget call = %+v", admins)
	}
}
```

Add error mapping test:

```go
func TestUpdateAPIKeyBudgetsEndpointMapsErrors(t *testing.T) {
	tests := []struct {
		name       string
		path       string
		body       string
		serviceErr error
		wantStatus int
	}{
		{
			name:       "invalid id",
			path:       "/api/admin/keys/not-a-number/budgets",
			body:       `{"requestBudget24h":10}`,
			wantStatus: http.StatusBadRequest,
		},
		{
			name:       "bad json",
			path:       "/api/admin/keys/7/budgets",
			body:       `{`,
			wantStatus: http.StatusBadRequest,
		},
		{
			name:       "invalid input",
			path:       "/api/admin/keys/7/budgets",
			body:       `{"requestBudget24h":-1}`,
			serviceErr: admin.ErrInvalidInput,
			wantStatus: http.StatusBadRequest,
		},
		{
			name:       "not found",
			path:       "/api/admin/keys/99/budgets",
			body:       `{"requestBudget24h":10}`,
			serviceErr: admin.ErrNotFound,
			wantStatus: http.StatusNotFound,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			admins := newFakeAdminService()
			admins.budgetErr = tt.serviceErr
			server := NewServer(config.Config{}, staticHealth{}, admins, nil)
			req := httptest.NewRequest(http.MethodPut, tt.path, strings.NewReader(tt.body))
			req.AddCookie(&http.Cookie{Name: adminSessionCookieName, Value: "valid-session"})
			recorder := httptest.NewRecorder()

			server.ServeHTTP(recorder, req)

			if recorder.Code != tt.wantStatus {
				t.Fatalf("status = %d, body = %s, want %d", recorder.Code, recorder.Body.String(), tt.wantStatus)
			}
		})
	}
}
```

- [ ] **Step 2: Run endpoint tests to verify red**

Run:

```bash
cd backend
GOMODCACHE=/root/Clouds/N2API/.cache/go-mod GOCACHE=/root/Clouds/N2API/.cache/go-build go test ./internal/httpapi -run APIKeyBudgets
```

Expected: FAIL because the route does not exist.

- [ ] **Step 3: Implement endpoint**

In `backend/internal/httpapi/server.go`, add to `AdminService`:

```go
	UpdateAPIKeyBudgets(ctx context.Context, id int64, requestBudget24h, tokenBudget24h, requestBudget30d, tokenBudget30d int) (admin.APIKey, error)
```

Add route near `/api/admin/keys/{id}/limits`:

```go
mux.HandleFunc("PUT /api/admin/keys/{id}/budgets", requireAdmin(func(w http.ResponseWriter, r *http.Request, _ admin.Admin) {
	id, err := parsePositivePathID(r, "id")
	if err != nil {
		writeError(w, http.StatusBadRequest, "bad_request")
		return
	}

	var req struct {
		RequestBudget24h int `json:"requestBudget24h"`
		TokenBudget24h   int `json:"tokenBudget24h"`
		RequestBudget30d int `json:"requestBudget30d"`
		TokenBudget30d   int `json:"tokenBudget30d"`
	}
	if err := decodeJSON(w, r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "bad_request")
		return
	}
	key, err := admins.UpdateAPIKeyBudgets(r.Context(), id, req.RequestBudget24h, req.TokenBudget24h, req.RequestBudget30d, req.TokenBudget30d)
	if err != nil {
		if errors.Is(err, admin.ErrInvalidInput) {
			writeError(w, http.StatusBadRequest, "invalid_input")
			return
		}
		if errors.Is(err, admin.ErrNotFound) {
			writeError(w, http.StatusNotFound, "not_found")
			return
		}
		writeError(w, http.StatusInternalServerError, "internal_error")
		return
	}
	writeJSON(w, http.StatusOK, map[string]admin.APIKey{"key": key})
}))
```

- [ ] **Step 4: Run endpoint tests**

Run:

```bash
cd backend
GOMODCACHE=/root/Clouds/N2API/.cache/go-mod GOCACHE=/root/Clouds/N2API/.cache/go-build go test ./internal/httpapi -run APIKeyBudgets
```

Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add backend/internal/httpapi/server.go backend/internal/httpapi/server_test.go
git commit -m "feat: expose api key budget settings"
```

## Task 4: Gateway Budget Enforcement

**Files:**
- Modify: `backend/internal/gateway/proxy.go`
- Modify: `backend/internal/gateway/proxy_test.go`

- [ ] **Step 1: Write failing gateway tests**

In `backend/internal/gateway/proxy_test.go`, extend `fakeAPIKeyAuthenticator` or add a budget-aware fake implementing:

```go
type fakeBudgetProvider struct {
	usage admin.APIKeyBudgetUsage
	err   error
}

func (p *fakeBudgetProvider) GetAPIKeyBudgetUsage(_ context.Context, key admin.APIKey, _ time.Time) (admin.APIKeyBudgetUsage, error) {
	if p.err != nil {
		return admin.APIKeyBudgetUsage{}, p.err
	}
	return p.usage, nil
}
```

Add test:

```go
func TestProxyRejectsWhenAPIKeyRequestBudgetExceeded(t *testing.T) {
	auth := &fakeAPIKeyAuthenticator{key: admin.APIKey{ID: 7, RequestBudget24h: 1}}
	budgets := &fakeBudgetProvider{usage: admin.APIKeyBudgetUsage{RequestsUsed24h: 1}}
	logger := &fakeRequestLogger{}
	accounts := &fakeSelectedAccountProvider{accounts: []SelectedAccount{{AccountID: 1, AuthorizationToken: "upstream-token"}}}
	client := &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
		return &http.Response{
			StatusCode: http.StatusOK,
			Header:     make(http.Header),
			Body:       io.NopCloser(strings.NewReader(`{"id":"ok"}`)),
			Request:    r,
		}, nil
	})}
	proxy := NewProxyWithClient(auth, accounts, Config{
		UpstreamBaseURL: "https://upstream.example.test",
		Logger:          logger,
		BudgetProvider:  budgets,
	}, client)

	recorder := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/v1/responses", strings.NewReader(`{"model":"gpt-5","input":"hi"}`))
	req.Header.Set("Authorization", "Bearer n2api_test")
	req.Header.Set("Content-Type", "application/json")

	proxy.ServeHTTP(recorder, req)

	if recorder.Code != http.StatusTooManyRequests {
		t.Fatalf("status = %d, body = %s", recorder.Code, recorder.Body.String())
	}
	if accounts.calls != 0 {
		t.Fatalf("account calls = %d, want 0 before account selection", accounts.calls)
	}
	if len(logger.entries) != 1 || logger.entries[0].Error != "api_key_request_budget_exceeded" {
		t.Fatalf("logs = %+v", logger.entries)
	}
}
```

Add analogous token budget test with `TokenBudget24h: 100` and `TokensUsed24h: 100`, expecting `api_key_token_budget_exceeded`.

Add a below-budget test that configures `RequestBudget24h: 2`, `RequestsUsed24h: 1` and verifies the upstream/account path is called.

- [ ] **Step 2: Run gateway tests to verify red**

Run:

```bash
cd backend
GOMODCACHE=/root/Clouds/N2API/.cache/go-mod GOCACHE=/root/Clouds/N2API/.cache/go-build go test ./internal/gateway -run APIKey.*Budget
```

Expected: FAIL because `Config.BudgetProvider` and gateway budget checks do not exist.

- [ ] **Step 3: Implement gateway budget provider**

In `backend/internal/gateway/proxy.go`, add:

```go
type APIKeyBudgetProvider interface {
	GetAPIKeyBudgetUsage(ctx context.Context, key admin.APIKey, now time.Time) (admin.APIKeyBudgetUsage, error)
}
```

Add to `Config`:

```go
	BudgetProvider APIKeyBudgetProvider
```

Add to `Proxy`:

```go
	budgets APIKeyBudgetProvider
```

Set it in `NewProxyWithClient`:

```go
budgets: cfg.BudgetProvider,
```

After gateway settings load and before `allowAPIKeyRequest`, add:

```go
	if code, blocked := p.apiKeyBudgetExceeded(r.Context(), key, startedAt); blocked {
		errorCode = code
		writeOpenAIError(recorder, http.StatusTooManyRequests, "rate_limit_exceeded", "api key budget exceeded")
		return
	}
```

Add method:

```go
func (p *Proxy) apiKeyBudgetExceeded(ctx context.Context, key admin.APIKey, now time.Time) (string, bool) {
	if p.budgets == nil {
		return "", false
	}
	usage, err := p.budgets.GetAPIKeyBudgetUsage(ctx, key, now)
	if err != nil {
		return "internal_error", true
	}
	if key.RequestBudget24h > 0 && usage.RequestsUsed24h >= int64(key.RequestBudget24h) {
		return "api_key_request_budget_exceeded", true
	}
	if key.RequestBudget30d > 0 && usage.RequestsUsed30d >= int64(key.RequestBudget30d) {
		return "api_key_request_budget_exceeded", true
	}
	if key.TokenBudget24h > 0 && usage.TokensUsed24h >= int64(key.TokenBudget24h) {
		return "api_key_token_budget_exceeded", true
	}
	if key.TokenBudget30d > 0 && usage.TokensUsed30d >= int64(key.TokenBudget30d) {
		return "api_key_token_budget_exceeded", true
	}
	return "", false
}
```

Adjust the call site to return `500 internal_error` when the returned code is `internal_error`:

```go
	if code, blocked := p.apiKeyBudgetExceeded(r.Context(), key, startedAt); blocked {
		errorCode = code
		if code == "internal_error" {
			writeOpenAIError(recorder, http.StatusInternalServerError, code, "api key budget check failed")
			return
		}
		writeOpenAIError(recorder, http.StatusTooManyRequests, "rate_limit_exceeded", "api key budget exceeded")
		return
	}
```

- [ ] **Step 4: Wire production gateway**

In `backend/cmd/n2api/main.go`, where `gateway.Config` is built, set:

```go
BudgetProvider: adminService,
```

Use the existing `adminService` value; after Task 2 it implements `GetAPIKeyBudgetUsage`.

- [ ] **Step 5: Run gateway tests**

Run:

```bash
cd backend
GOMODCACHE=/root/Clouds/N2API/.cache/go-mod GOCACHE=/root/Clouds/N2API/.cache/go-build go test ./internal/gateway ./cmd/n2api -run 'APIKey.*Budget|Test'
```

Expected: PASS for the new budget tests and cmd compile test. If sandbox blocks `httptest` listeners, rerun the same command with elevated permissions.

- [ ] **Step 6: Commit**

```bash
git add backend/internal/gateway/proxy.go backend/internal/gateway/proxy_test.go backend/cmd/n2api/main.go
git commit -m "feat: enforce api key budgets"
```

## Task 5: Frontend Budget Controls

**Files:**
- Modify: `frontend/src/lib/admin-state.svelte.js`
- Modify: `frontend/src/routes/api-keys/+page.svelte`
- Modify: `frontend/src/routes/navigation.test.mjs`

- [ ] **Step 1: Write failing frontend static test**

In `frontend/src/routes/navigation.test.mjs`, add:

```js
test('api keys page manages request and token budgets', () => {
  for (const label of [
    'Key budgets',
    '24h requests',
    '24h tokens',
    '30d requests',
    '30d tokens',
    'Save budgets',
    'Request budget exceeded',
    'Token budget exceeded'
  ]) {
    assert.match(apiKeysPage, new RegExp(label.replace(' ', '\\s+')), `api keys page should include ${label}`);
  }

  assert.match(apiKeysPage, /updateAPIKeyBudgets/);
  assert.match(apiKeysPage, /key\.requestBudget24h/);
  assert.match(apiKeysPage, /key\.tokenBudget24h/);
  assert.match(apiKeysPage, /key\.requestBudget30d/);
  assert.match(apiKeysPage, /key\.tokenBudget30d/);
  assert.match(apiKeysPage, /key\.requestBudgetExceeded/);
  assert.match(apiKeysPage, /key\.tokenBudgetExceeded/);
  assert.match(adminState, /export async function updateAPIKeyBudgets/);
  assert.match(adminState, /\/api\/admin\/keys\/\$\{keyId\}\/budgets/);
  assert.match(adminState, /requestBudget24h/);
  assert.match(adminState, /tokensRemaining30d/);
});
```

- [ ] **Step 2: Run frontend test to verify red**

Run:

```bash
cd frontend
bun test src/routes/navigation.test.mjs
```

Expected: FAIL because budget UI/state does not exist.

- [ ] **Step 3: Implement admin-state budget fields and API call**

In `frontend/src/lib/admin-state.svelte.js`, extend the `APIKey` typedef with:

```js
 * @property {number} requestBudget24h
 * @property {number} tokenBudget24h
 * @property {number} requestBudget30d
 * @property {number} tokenBudget30d
 * @property {number} requestsUsed24h
 * @property {number} tokensUsed24h
 * @property {number | null} requestsRemaining24h
 * @property {number | null} tokensRemaining24h
 * @property {number} requestsUsed30d
 * @property {number} tokensUsed30d
 * @property {number | null} requestsRemaining30d
 * @property {number | null} tokensRemaining30d
 * @property {boolean} requestBudgetExceeded
 * @property {boolean} tokenBudgetExceeded
```

Add:

```js
export async function updateAPIKeyBudgets(keyId, requestBudget24h, tokenBudget24h, requestBudget30d, tokenBudget30d) {
  const version = sessionVersion;
  if (!isCurrentAuthenticated(version)) return;

  const budgets = [requestBudget24h, tokenBudget24h, requestBudget30d, tokenBudget30d].map(Number);
  if (budgets.some((value) => !Number.isInteger(value) || value < 0)) {
    apiKeys.error = 'API key budgets must be non-negative whole numbers';
    return;
  }

  apiKeys.error = '';
  try {
    const payload = await requestJSON(`/api/admin/keys/${keyId}/budgets`, {
      method: 'PUT',
      body: JSON.stringify({
        requestBudget24h: budgets[0],
        tokenBudget24h: budgets[1],
        requestBudget30d: budgets[2],
        tokenBudget30d: budgets[3]
      })
    });
    if (!isCurrentAuthenticated(version)) return;
    apiKeys.items = apiKeys.items.map((key) => (key.id === keyId ? payload.key : key));
    await loadKeys();
  } catch (error) {
    if (!isCurrentAuthenticated(version)) return;
    apiKeys.error = error instanceof Error ? error.message : 'Failed to update key budgets';
  }
}
```

Add helper:

```js
export function budgetUsageLabel(used, limit) {
  const cap = Number(limit ?? 0);
  if (cap <= 0) return 'uncapped';
  return `${formatTokens(Number(used ?? 0))} / ${formatTokens(cap)}`;
}
```

- [ ] **Step 4: Implement API Keys page budget UI**

In `frontend/src/routes/api-keys/+page.svelte`, import `budgetUsageLabel` and `updateAPIKeyBudgets`.

Add budget state to `apiKeySearchText`:

```js
      key.requestBudgetExceeded ? 'request budget exceeded' : '',
      key.tokenBudgetExceeded ? 'token budget exceeded' : '',
```

Add a compact budget form in the table, after the Key limits column content or by widening that column into a combined limits/budgets group:

```svelte
<form
  class="mt-3 grid gap-2 border-t border-[#ededed] pt-3"
  onsubmit={(event) => {
    event.preventDefault();
    updateAPIKeyBudgets(
      key.id,
      key.requestBudget24h ?? 0,
      key.tokenBudget24h ?? 0,
      key.requestBudget30d ?? 0,
      key.tokenBudget30d ?? 0
    );
  }}
>
  <h4 class="text-xs font-semibold uppercase tracking-wide text-[#6e6e6e]">Key budgets</h4>
  <div class="grid gap-2 sm:grid-cols-2">
    <label class="block text-xs font-medium text-[#6e6e6e]" for={`api-key-request-budget-24h-${key.id}`}>
      24h requests
      <input id={`api-key-request-budget-24h-${key.id}`} class="mt-1 w-full rounded-md border border-[#e5e5e5] bg-white px-2 py-1.5 font-mono text-[13px] text-[#0d0d0d] outline-none focus:border-[#10a37f] focus:ring-2 focus:ring-[#e8f5f0] disabled:cursor-not-allowed disabled:bg-[#f5f5f5] disabled:text-[#9b9b9b]" type="number" min="0" step="1" value={key.requestBudget24h ?? 0} disabled={Boolean(key.revokedAt)} oninput={(event) => { key.requestBudget24h = Number(event.currentTarget.value || 0); }} />
      <span class="mt-1 block text-[11px] font-normal text-[#6e6e6e]">{budgetUsageLabel(key.requestsUsed24h, key.requestBudget24h)}</span>
    </label>
    <label class="block text-xs font-medium text-[#6e6e6e]" for={`api-key-token-budget-24h-${key.id}`}>
      24h tokens
      <input id={`api-key-token-budget-24h-${key.id}`} class="mt-1 w-full rounded-md border border-[#e5e5e5] bg-white px-2 py-1.5 font-mono text-[13px] text-[#0d0d0d] outline-none focus:border-[#10a37f] focus:ring-2 focus:ring-[#e8f5f0] disabled:cursor-not-allowed disabled:bg-[#f5f5f5] disabled:text-[#9b9b9b]" type="number" min="0" step="1" value={key.tokenBudget24h ?? 0} disabled={Boolean(key.revokedAt)} oninput={(event) => { key.tokenBudget24h = Number(event.currentTarget.value || 0); }} />
      <span class="mt-1 block text-[11px] font-normal text-[#6e6e6e]">{budgetUsageLabel(key.tokensUsed24h, key.tokenBudget24h)}</span>
    </label>
    <label class="block text-xs font-medium text-[#6e6e6e]" for={`api-key-request-budget-30d-${key.id}`}>
      30d requests
      <input id={`api-key-request-budget-30d-${key.id}`} class="mt-1 w-full rounded-md border border-[#e5e5e5] bg-white px-2 py-1.5 font-mono text-[13px] text-[#0d0d0d] outline-none focus:border-[#10a37f] focus:ring-2 focus:ring-[#e8f5f0] disabled:cursor-not-allowed disabled:bg-[#f5f5f5] disabled:text-[#9b9b9b]" type="number" min="0" step="1" value={key.requestBudget30d ?? 0} disabled={Boolean(key.revokedAt)} oninput={(event) => { key.requestBudget30d = Number(event.currentTarget.value || 0); }} />
      <span class="mt-1 block text-[11px] font-normal text-[#6e6e6e]">{budgetUsageLabel(key.requestsUsed30d, key.requestBudget30d)}</span>
    </label>
    <label class="block text-xs font-medium text-[#6e6e6e]" for={`api-key-token-budget-30d-${key.id}`}>
      30d tokens
      <input id={`api-key-token-budget-30d-${key.id}`} class="mt-1 w-full rounded-md border border-[#e5e5e5] bg-white px-2 py-1.5 font-mono text-[13px] text-[#0d0d0d] outline-none focus:border-[#10a37f] focus:ring-2 focus:ring-[#e8f5f0] disabled:cursor-not-allowed disabled:bg-[#f5f5f5] disabled:text-[#9b9b9b]" type="number" min="0" step="1" value={key.tokenBudget30d ?? 0} disabled={Boolean(key.revokedAt)} oninput={(event) => { key.tokenBudget30d = Number(event.currentTarget.value || 0); }} />
      <span class="mt-1 block text-[11px] font-normal text-[#6e6e6e]">{budgetUsageLabel(key.tokensUsed30d, key.tokenBudget30d)}</span>
    </label>
  </div>
  {#if key.requestBudgetExceeded}
    <p class="text-xs font-medium text-red-700">Request budget exceeded</p>
  {/if}
  {#if key.tokenBudgetExceeded}
    <p class="text-xs font-medium text-red-700">Token budget exceeded</p>
  {/if}
  <button class="justify-self-start rounded-md border border-[#e5e5e5] bg-white px-2.5 py-1.5 text-xs font-medium text-[#0d0d0d] hover:bg-[#f5f5f5] disabled:cursor-not-allowed disabled:text-[#9b9b9b]" type="submit" disabled={Boolean(key.revokedAt)}>
    Save budgets
  </button>
</form>
```

- [ ] **Step 5: Run frontend checks**

Run:

```bash
cd frontend
bun test src/routes/navigation.test.mjs
bun run check
```

Expected: PASS.

- [ ] **Step 6: Commit**

```bash
git add frontend/src/lib/admin-state.svelte.js frontend/src/routes/api-keys/+page.svelte frontend/src/routes/navigation.test.mjs
git commit -m "feat: manage api key budgets in admin ui"
```

## Task 6: Documentation Guards

**Files:**
- Modify: `backend/internal/gateway/documentation_test.go`
- Modify: `README.md`
- Modify: `deploy/README.md`

- [ ] **Step 1: Write failing documentation test**

In `backend/internal/gateway/documentation_test.go`, add:

```go
func TestGatewayDocumentationMentionsAPIKeyBudgets(t *testing.T) {
	for _, path := range []string{"../../../README.md", "../../../deploy/README.md"} {
		content, err := os.ReadFile(path)
		if err != nil {
			t.Fatalf("ReadFile(%q) returned error: %v", path, err)
		}
		text := string(content)
		for _, want := range []string{
			"API key budgets",
			"request and token budgets over rolling 24h and 30d windows",
			"`0` disables a budget field",
			"`api_key_request_budget_exceeded`",
			"`api_key_token_budget_exceeded`",
			"not billing balances",
		} {
			if !strings.Contains(text, want) {
				t.Fatalf("%s missing %q in API key budget documentation", path, want)
			}
		}
	}
}
```

- [ ] **Step 2: Run documentation test to verify red**

Run:

```bash
cd backend
GOMODCACHE=/root/Clouds/N2API/.cache/go-mod GOCACHE=/root/Clouds/N2API/.cache/go-build go test ./internal/gateway -run APIKeyBudgets
```

Expected: FAIL because docs do not mention budget cutoffs yet.

- [ ] **Step 3: Update README docs**

Add this paragraph near the API Key Model Access / Gateway Runtime Limits sections in both `README.md` and `deploy/README.md`:

```markdown
API key budgets are personal operational safeguards, not billing balances. Each key can have request and token budgets over rolling 24h and 30d windows; `0` disables a budget field. When a key is over budget, clients receive OpenAI-compatible `rate_limit_exceeded` responses while Request Logs store the precise local reason as `api_key_request_budget_exceeded` or `api_key_token_budget_exceeded`.
```

- [ ] **Step 4: Run documentation test**

Run:

```bash
cd backend
GOMODCACHE=/root/Clouds/N2API/.cache/go-mod GOCACHE=/root/Clouds/N2API/.cache/go-build go test ./internal/gateway -run APIKeyBudgets
```

Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add backend/internal/gateway/documentation_test.go README.md deploy/README.md
git commit -m "docs: document api key budgets"
```

## Task 7: Full Verification

**Files:** No code changes expected.

- [ ] **Step 1: Run formatting/diff check**

Run:

```bash
git diff --check
```

Expected: exit 0.

- [ ] **Step 2: Run backend full test**

Run:

```bash
cd backend
GOMODCACHE=/root/Clouds/N2API/.cache/go-mod GOCACHE=/root/Clouds/N2API/.cache/go-build go test ./...
```

Expected: PASS. If this fails in the sandbox with `httptest: failed to listen on a port: listen tcp6 [::1]:0: socket: operation not permitted`, rerun the same command with elevated permissions and use that result as the backend verdict.

- [ ] **Step 3: Run frontend tests and checks**

Run:

```bash
cd frontend
bun test src/routes/navigation.test.mjs
bun run check
bun run build
```

Expected: all commands exit 0.

- [ ] **Step 4: Confirm clean worktree**

Run:

```bash
git status --short
git log --oneline -8
```

Expected: clean status and recent commits showing the budget cutoff feature slices.

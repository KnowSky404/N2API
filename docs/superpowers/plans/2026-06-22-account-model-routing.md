# Account Model Routing Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Route N2API gateway requests through accounts that are manually configured to support the requested model.

**Architecture:** Add account-scoped model capability rows beside the existing OAuth account pool. Keep persistence in `backend/internal/store`, OAuth/account behavior in `backend/internal/provider`, gateway routing in `backend/internal/gateway`, admin policy in `backend/internal/admin`, and the operational UI in Svelte admin routes.

**Tech Stack:** Go, PostgreSQL/goose migrations, pgx, SvelteKit, Bun, Tailwind CSS, Docker Compose.

---

## File Structure

- Create `backend/internal/store/migrations/00007_oauth_account_models.sql`: account model capability schema and indexes.
- Modify `backend/internal/store/migrations_test.go`: assert migration embedding and SQL contents.
- Modify `backend/internal/provider/service.go`: add account model types, repository interface methods, model-aware selector, and model list normalization.
- Modify `backend/internal/provider/service_test.go`: cover normalization, model filtering, disabled rows, fallback filtering, and reconnect preservation.
- Modify `backend/internal/store/provider.go`: implement account model CRUD, aggregate model listing, and model-filtered eligible account queries.
- Modify `backend/internal/store/provider_test.go`: keep interface regression coverage and SQL source checks.
- Modify `backend/internal/admin/service.go`: add model routing DTOs and global policy helpers.
- Modify `backend/internal/admin/service_test.go`: cover model policy availability and default model validation.
- Modify `backend/internal/gateway/proxy.go`: parse/inject request model, call model-aware token selection, and serve `/v1/models` locally.
- Modify `backend/internal/gateway/proxy_test.go`: cover model-scoped routing, default injection, unsupported model rejection, and `/v1/models`.
- Modify `backend/internal/httpapi/server.go`: expose account model and model routing admin endpoints.
- Modify `backend/internal/httpapi/server_test.go`: cover auth, validation, and endpoint response shapes.
- Modify `backend/cmd/n2api/main.go`: update gateway wrapper interfaces for model-aware selection and exposed model listing.
- Modify `frontend/src/lib/admin-state.svelte.js`: add account model and model routing state/actions.
- Modify `frontend/src/routes/providers/+page.svelte`: add manual model editor per account.
- Modify `frontend/src/routes/models/+page.svelte`: present model routing policy and aggregate availability.
- Modify `frontend/src/routes/providers/provider-page.test.mjs`: cover account model editor state helpers.
- Modify `README.md` and `deploy/README.md`: document manual account model configuration and model-scoped fallback.

## Task 1: Schema And Type Foundation

**Files:**
- Create: `backend/internal/store/migrations/00007_oauth_account_models.sql`
- Modify: `backend/internal/store/migrations_test.go`
- Modify: `backend/internal/provider/service.go`
- Modify: `backend/internal/store/provider_test.go`

- [ ] **Step 1: Write failing migration tests**

Add a test in `backend/internal/store/migrations_test.go`:

```go
func TestOAuthAccountModelsMigrationIsEmbedded(t *testing.T) {
	sql, err := MigrationSQL("00007_oauth_account_models.sql")
	if err != nil {
		t.Fatalf("MigrationSQL returned error: %v", err)
	}
	for _, want := range []string{
		"CREATE TABLE IF NOT EXISTS oauth_account_models",
		"account_id BIGINT NOT NULL REFERENCES oauth_accounts(id) ON DELETE CASCADE",
		"source TEXT NOT NULL DEFAULT 'manual'",
		"UNIQUE (account_id, model)",
		"oauth_account_models_provider_model_enabled_idx",
		"oauth_account_models_account_enabled_idx",
	} {
		if !strings.Contains(sql, want) {
			t.Fatalf("migration missing %q", want)
		}
	}
}
```

Update the embedded source count in `TestMigrationProviderSeesEmbeddedMigrations` so it expects the new migration and the final path is `00007_oauth_account_models.sql`.

- [ ] **Step 2: Run the failing migration tests**

Run:

```bash
cd backend
GOMODCACHE=/root/Clouds/N2API/.cache/go-mod GOCACHE=/root/Clouds/N2API/.cache/go-build go test ./internal/store -run 'TestOAuthAccountModelsMigrationIsEmbedded|TestMigrationProviderSeesEmbeddedMigrations'
```

Expected: FAIL because `00007_oauth_account_models.sql` is not present yet.

- [ ] **Step 3: Add the migration**

Create `backend/internal/store/migrations/00007_oauth_account_models.sql`:

```sql
-- +goose Up
-- +goose StatementBegin
CREATE TABLE IF NOT EXISTS oauth_account_models (
    id BIGSERIAL PRIMARY KEY,
    account_id BIGINT NOT NULL REFERENCES oauth_accounts(id) ON DELETE CASCADE,
    provider TEXT NOT NULL,
    model TEXT NOT NULL,
    enabled BOOLEAN NOT NULL DEFAULT true,
    source TEXT NOT NULL DEFAULT 'manual',
    last_seen_at TIMESTAMPTZ,
    last_error TEXT NOT NULL DEFAULT '',
    metadata JSONB NOT NULL DEFAULT '{}'::jsonb,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE (account_id, model)
);

CREATE INDEX IF NOT EXISTS oauth_account_models_provider_model_enabled_idx
    ON oauth_account_models (provider, model, enabled, account_id);

CREATE INDEX IF NOT EXISTS oauth_account_models_account_enabled_idx
    ON oauth_account_models (account_id, enabled, model);
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
DROP INDEX IF EXISTS oauth_account_models_account_enabled_idx;
DROP INDEX IF EXISTS oauth_account_models_provider_model_enabled_idx;
DROP TABLE IF EXISTS oauth_account_models;
-- +goose StatementEnd
```

- [ ] **Step 4: Add provider model types**

In `backend/internal/provider/service.go`, add:

```go
const (
	AccountModelSourceManual = "manual"
	maxAccountModels         = 100
	maxModelNameLen          = 128
)

var ErrModelUnavailable = errors.New("model unavailable")

type AccountModel struct {
	ID         int64             `json:"id"`
	AccountID  int64             `json:"accountId"`
	Provider   string            `json:"provider"`
	Model      string            `json:"model"`
	Enabled    bool              `json:"enabled"`
	Source     string            `json:"source"`
	LastSeenAt *time.Time        `json:"lastSeenAt"`
	LastError  string            `json:"lastError"`
	Metadata   map[string]string `json:"metadata"`
	CreatedAt  time.Time         `json:"createdAt"`
	UpdatedAt  time.Time         `json:"updatedAt"`
}

type AccountModelInput struct {
	Model   string `json:"model"`
	Enabled bool   `json:"enabled"`
}

type ExposedModel struct {
	ID      string `json:"id"`
	OwnedBy string `json:"ownedBy"`
}
```

Extend `Repository` with:

```go
ListAccountModels(ctx context.Context, provider string, accountID int64) ([]AccountModel, error)
ReplaceAccountModels(ctx context.Context, provider string, accountID int64, models []AccountModelInput) ([]AccountModel, error)
ListExposedModels(ctx context.Context, provider string, allowedModels []string) ([]ExposedModel, error)
ListEligibleAccountsForModel(ctx context.Context, provider string, model string, excludedAccountIDs []int64, now time.Time) ([]Account, error)
```

- [ ] **Step 5: Run focused compile check**

Run:

```bash
cd backend
GOMODCACHE=/root/Clouds/N2API/.cache/go-mod GOCACHE=/root/Clouds/N2API/.cache/go-build go test ./internal/provider ./internal/store
```

Expected: FAIL with missing repository methods until Task 2 implements them.

- [ ] **Step 6: Commit**

Run:

```bash
git add backend/internal/store/migrations/00007_oauth_account_models.sql backend/internal/store/migrations_test.go backend/internal/provider/service.go backend/internal/store/provider_test.go
git commit -m "feat: add account model schema"
```

## Task 2: Store Account Model Repository

**Files:**
- Modify: `backend/internal/store/provider.go`
- Modify: `backend/internal/store/provider_test.go`
- Modify: `backend/internal/store/migrations_test.go`

- [ ] **Step 1: Write repository behavior tests**

Add tests around `ProviderRepository` using the existing store test patterns:

```go
func TestProviderRepositoryAccountModelsCascadeAndReplace(t *testing.T) {
	repo, ctx := newTestProviderRepository(t)
	account := saveTestProviderAccount(t, repo, ctx, "openai", "acct-models")

	models, err := repo.ReplaceAccountModels(ctx, "openai", account.ID, []provider.AccountModelInput{
		{Model: " gpt-5 ", Enabled: true},
		{Model: "gpt-5-mini", Enabled: false},
		{Model: "gpt-5", Enabled: true},
	})
	if err != nil {
		t.Fatalf("ReplaceAccountModels returned error: %v", err)
	}
	if len(models) != 2 || models[0].Model != "gpt-5" || models[1].Model != "gpt-5-mini" {
		t.Fatalf("models = %+v, want normalized unique order", models)
	}

	listed, err := repo.ListAccountModels(ctx, "openai", account.ID)
	if err != nil {
		t.Fatalf("ListAccountModels returned error: %v", err)
	}
	if len(listed) != 2 {
		t.Fatalf("listed models = %d, want 2", len(listed))
	}
}
```

Add a selector query test:

```go
func TestProviderRepositoryListEligibleAccountsForModelFiltersByModel(t *testing.T) {
	repo, ctx := newTestProviderRepository(t)
	first := saveTestProviderAccountWithPriority(t, repo, ctx, "openai", "acct-a", 10)
	second := saveTestProviderAccountWithPriority(t, repo, ctx, "openai", "acct-b", 20)
	_, _ = repo.ReplaceAccountModels(ctx, "openai", first.ID, []provider.AccountModelInput{{Model: "gpt-5", Enabled: true}})
	_, _ = repo.ReplaceAccountModels(ctx, "openai", second.ID, []provider.AccountModelInput{{Model: "gpt-4.1", Enabled: true}})

	accounts, err := repo.ListEligibleAccountsForModel(ctx, "openai", "gpt-5", nil, time.Now())
	if err != nil {
		t.Fatalf("ListEligibleAccountsForModel returned error: %v", err)
	}
	if len(accounts) != 1 || accounts[0].ID != first.ID {
		t.Fatalf("accounts = %+v, want only first account", accounts)
	}
}
```

- [ ] **Step 2: Run failing store tests**

Run:

```bash
cd backend
GOMODCACHE=/root/Clouds/N2API/.cache/go-mod GOCACHE=/root/Clouds/N2API/.cache/go-build go test ./internal/store -run 'AccountModels|EligibleAccountsForModel'
```

Expected: FAIL because repository methods are missing.

- [ ] **Step 3: Implement scanners and replacement**

In `backend/internal/store/provider.go`, add `providerAccountModelColumns`, `scanProviderAccountModel`, `normalizeAccountModelInputs`, `ListAccountModels`, and `ReplaceAccountModels`.

Use one transaction in `ReplaceAccountModels`:

```go
tx, err := r.pool.Begin(ctx)
if err != nil {
	return nil, err
}
defer tx.Rollback(ctx)
```

Within the transaction:
- verify account exists for `provider` and `id`
- delete rows for account where source is `manual`
- insert normalized rows with `source = 'manual'`
- query rows ordered by `model ASC`
- commit

- [ ] **Step 4: Implement aggregate and eligible queries**

Implement `ListExposedModels` using enabled account rows joined to enabled account model rows, filtered by the passed `allowedModels`. Preserve allowed-list order by sorting in Go after fetching the distinct model set.

Implement `ListEligibleAccountsForModel`:

```sql
SELECT <providerAccountColumns>
FROM oauth_accounts a
JOIN oauth_account_models m ON m.account_id = a.id
WHERE a.provider = $1
  AND m.provider = $1
  AND m.model = $2
  AND m.enabled = true
  AND a.enabled = true
  AND a.status NOT IN ('disabled', 'expired')
  AND (a.rate_limited_until IS NULL OR a.rate_limited_until <= $3)
  AND (a.circuit_open_until IS NULL OR a.circuit_open_until <= $3)
  AND NOT (a.id = ANY($4))
ORDER BY a.priority ASC, a.last_used_at ASC NULLS FIRST, a.id ASC
```

- [ ] **Step 5: Run store tests**

Run:

```bash
cd backend
GOMODCACHE=/root/Clouds/N2API/.cache/go-mod GOCACHE=/root/Clouds/N2API/.cache/go-build go test ./internal/store
```

Expected: PASS.

- [ ] **Step 6: Commit**

Run:

```bash
git add backend/internal/store/provider.go backend/internal/store/provider_test.go backend/internal/store/migrations_test.go
git commit -m "feat: persist account model capabilities"
```

## Task 3: Provider Model-Aware Selection

**Files:**
- Modify: `backend/internal/provider/service.go`
- Modify: `backend/internal/provider/service_test.go`

- [ ] **Step 1: Write provider tests**

Add tests:

```go
func TestSelectAccessTokenFiltersByRequestedModel(t *testing.T) {
	repo := newMemoryRepo()
	service := NewService(repo, newFakeOAuthClient(), Config{Provider: "openai", Secret: testSecret})
	first := repo.addAccount(provider.Account{Provider: "openai", Subject: "a", Enabled: true, Priority: 10, EncryptedAccessToken: encryptTestToken(t, "token-a")})
	second := repo.addAccount(provider.Account{Provider: "openai", Subject: "b", Enabled: true, Priority: 1, EncryptedAccessToken: encryptTestToken(t, "token-b")})
	repo.replaceModels(first.ID, []provider.AccountModelInput{{Model: "gpt-5", Enabled: true}})
	repo.replaceModels(second.ID, []provider.AccountModelInput{{Model: "gpt-4.1", Enabled: true}})

	selected, err := service.SelectAccessToken(context.Background(), "gpt-5")
	if err != nil {
		t.Fatalf("SelectAccessToken returned error: %v", err)
	}
	if selected.AccountID != first.ID {
		t.Fatalf("selected account = %d, want %d", selected.AccountID, first.ID)
	}
}
```

Add disabled-model and unavailable tests:

```go
func TestSelectAccessTokenReturnsModelUnavailableWhenNoAccountSupportsModel(t *testing.T) {
	repo := newMemoryRepo()
	service := NewService(repo, newFakeOAuthClient(), Config{Provider: "openai", Secret: testSecret})
	_, err := service.SelectAccessToken(context.Background(), "gpt-5")
	if !errors.Is(err, provider.ErrModelUnavailable) {
		t.Fatalf("error = %v, want ErrModelUnavailable", err)
	}
}
```

- [ ] **Step 2: Run failing provider tests**

Run:

```bash
cd backend
GOMODCACHE=/root/Clouds/N2API/.cache/go-mod GOCACHE=/root/Clouds/N2API/.cache/go-build go test ./internal/provider -run 'SelectAccessToken.*Model|AccountModel'
```

Expected: FAIL until selection signature and memory repo are updated.

- [ ] **Step 3: Add model normalization service methods**

In `service.go`, add:

```go
func normalizeAccountModelInputs(inputs []AccountModelInput) ([]AccountModelInput, error) {
	if len(inputs) == 0 {
		return nil, nil
	}
	seen := map[string]struct{}{}
	normalized := make([]AccountModelInput, 0, len(inputs))
	for _, input := range inputs {
		model := strings.TrimSpace(input.Model)
		if model == "" {
			continue
		}
		if len(model) > maxModelNameLen {
			return nil, ErrInvalidInput
		}
		if _, ok := seen[model]; ok {
			continue
		}
		seen[model] = struct{}{}
		normalized = append(normalized, AccountModelInput{Model: model, Enabled: input.Enabled})
		if len(normalized) > maxAccountModels {
			return nil, ErrInvalidInput
		}
	}
	return normalized, nil
}
```

Add public methods:

```go
func (s *Service) ListAccountModels(ctx context.Context, accountID int64) ([]AccountModel, error)
func (s *Service) ReplaceAccountModels(ctx context.Context, accountID int64, models []AccountModelInput) ([]AccountModel, error)
func (s *Service) ListExposedModels(ctx context.Context, allowedModels []string) ([]ExposedModel, error)
```

- [ ] **Step 4: Change selector signature and implementation**

Change:

```go
func (s *Service) SelectAccessToken(ctx context.Context, excludedAccountIDs ...int64) (SelectedToken, error)
```

to:

```go
func (s *Service) SelectAccessToken(ctx context.Context, model string, excludedAccountIDs ...int64) (SelectedToken, error)
```

When `model` is blank, keep the existing account-pool selection path for response follow-up GET routes. When non-blank, call `repo.ListEligibleAccountsForModel` and only try those accounts. If the eligible list is empty, return `ErrModelUnavailable`.

- [ ] **Step 5: Update provider tests and memory repo**

Update the memory repository in `service_test.go` to store `map[int64][]AccountModel` and implement:

```go
func (r *memoryRepo) ListAccountModels(ctx context.Context, provider string, accountID int64) ([]AccountModel, error)
func (r *memoryRepo) ReplaceAccountModels(ctx context.Context, provider string, accountID int64, models []AccountModelInput) ([]AccountModel, error)
func (r *memoryRepo) ListExposedModels(ctx context.Context, provider string, allowedModels []string) ([]ExposedModel, error)
func (r *memoryRepo) ListEligibleAccountsForModel(ctx context.Context, provider string, model string, excluded []int64, now time.Time) ([]Account, error)
```

- [ ] **Step 6: Run provider tests**

Run:

```bash
cd backend
GOMODCACHE=/root/Clouds/N2API/.cache/go-mod GOCACHE=/root/Clouds/N2API/.cache/go-build go test ./internal/provider
```

Expected: PASS.

- [ ] **Step 7: Commit**

Run:

```bash
git add backend/internal/provider/service.go backend/internal/provider/service_test.go
git commit -m "feat: select provider accounts by model"
```

## Task 4: Gateway Model Routing

**Files:**
- Modify: `backend/internal/gateway/proxy.go`
- Modify: `backend/internal/gateway/proxy_test.go`
- Modify: `backend/cmd/n2api/main.go`

- [ ] **Step 1: Write gateway tests**

Add tests covering:

```go
func TestProxyRoutesChatCompletionByRequestedModel(t *testing.T)
func TestProxyInjectsDefaultModelWhenMissing(t *testing.T)
func TestProxyReturnsModelUnavailableBeforeUpstream(t *testing.T)
func TestProxyModelsReturnsLocalAggregateList(t *testing.T)
```

Use a fake token provider with this signature:

```go
func (p *fakeSelectedTokenProvider) SelectAccessToken(ctx context.Context, model string, excludedAccountIDs ...int64) (SelectedToken, error) {
	p.models = append(p.models, model)
	return p.tokens[p.calls], p.errs[p.calls]
}
```

- [ ] **Step 2: Run failing gateway tests**

Run:

```bash
cd backend
GOMODCACHE=/root/Clouds/N2API/.cache/go-mod GOCACHE=/root/Clouds/N2API/.cache/go-build go test ./internal/gateway -run 'Model|Default'
```

Expected: FAIL until gateway interfaces and parsing are updated.

- [ ] **Step 3: Extend gateway interfaces and config**

Change `AccessTokenProvider`:

```go
SelectAccessToken(ctx context.Context, model string, excludedAccountIDs ...int64) (SelectedToken, error)
```

Add:

```go
type ModelProvider interface {
	DefaultModel(ctx context.Context) (string, error)
	IsModelAllowed(ctx context.Context, model string) (bool, error)
	ListExposedModels(ctx context.Context) ([]Model, error)
}

type Model struct {
	ID      string `json:"id"`
	OwnedBy string `json:"owned_by"`
}
```

Add `Models ModelProvider` to `Config` and store it on `Proxy`.

- [ ] **Step 4: Implement body model parsing and default injection**

Add helper:

```go
func modelRoutedPost(r *http.Request) bool {
	return r.Method == http.MethodPost && (r.URL.Path == "/v1/chat/completions" || r.URL.Path == "/v1/responses")
}
```

Add helper:

```go
func parseAndNormalizeRequestModel(raw []byte, defaultModel string) (string, []byte, error) {
	var payload map[string]any
	if err := json.Unmarshal(raw, &payload); err != nil {
		return "", raw, nil
	}
	model, _ := payload["model"].(string)
	model = strings.TrimSpace(model)
	if model == "" {
		model = strings.TrimSpace(defaultModel)
		if model != "" {
			payload["model"] = model
			normalized, err := json.Marshal(payload)
			if err != nil {
				return "", nil, err
			}
			return model, normalized, nil
		}
	}
	return model, raw, nil
}
```

For model-routed POSTs, reject bodies larger than `maxReplayableRequestBody` with `invalid_request`.

- [ ] **Step 5: Serve `/v1/models` locally**

Before token selection, if route is `GET /v1/models`, call `p.models.ListExposedModels(ctx)` and write:

```go
writeJSON(w, http.StatusOK, map[string]any{
	"object": "list",
	"data": models,
})
```

Keep OpenAI-compatible object fields in tests.

- [ ] **Step 6: Map model errors**

Update `providerErrorCode`:

```go
case errors.Is(err, provider.ErrModelUnavailable):
	return "model_unavailable"
```

Return `404 model_not_found` or `400 model_not_allowed` for disallowed models. Pick `model_not_found` and assert it in tests.

- [ ] **Step 7: Update production wrapper**

In `backend/cmd/n2api/main.go`, update `gatewayTokenProvider.SelectAccessToken` to accept `model string` and pass it through to provider service. Add a gateway model provider wrapper that delegates default/allowed/exposed behavior to admin/provider services.

- [ ] **Step 8: Run gateway tests**

Run:

```bash
cd backend
GOMODCACHE=/root/Clouds/N2API/.cache/go-mod GOCACHE=/root/Clouds/N2API/.cache/go-build go test ./internal/gateway ./cmd/n2api
```

Expected: PASS.

- [ ] **Step 9: Commit**

Run:

```bash
git add backend/internal/gateway/proxy.go backend/internal/gateway/proxy_test.go backend/cmd/n2api/main.go
git commit -m "feat: route gateway requests by model"
```

## Task 5: Admin API And Policy Surface

**Files:**
- Modify: `backend/internal/admin/service.go`
- Modify: `backend/internal/admin/service_test.go`
- Modify: `backend/internal/httpapi/server.go`
- Modify: `backend/internal/httpapi/server_test.go`

- [ ] **Step 1: Add admin service tests**

Add tests for:

```go
func TestModelPolicyAllowsConfiguredModels(t *testing.T)
func TestModelRoutingStatusReportsUnavailableDefault(t *testing.T)
```

Expected DTOs:

```go
type ModelRoutingStatus struct {
	DefaultModel  string                 `json:"defaultModel"`
	AllowedModels []string               `json:"allowedModels"`
	Models        []ModelRoutingModel    `json:"models"`
	Warnings      []string               `json:"warnings"`
}

type ModelRoutingModel struct {
	Model           string `json:"model"`
	Allowed         bool   `json:"allowed"`
	ConfiguredCount int    `json:"configuredCount"`
	EnabledCount    int    `json:"enabledCount"`
}
```

- [ ] **Step 2: Add HTTP API tests**

Add tests:

```go
func TestAccountModelsRequireSession(t *testing.T)
func TestListAccountModelsReturnsModels(t *testing.T)
func TestReplaceAccountModelsReturnsSavedModels(t *testing.T)
func TestModelRoutingReturnsStatus(t *testing.T)
```

Use paths:

```text
GET /api/admin/providers/openai/accounts/{id}/models
PUT /api/admin/providers/openai/accounts/{id}/models
GET /api/admin/model-routing
```

- [ ] **Step 3: Run failing admin/API tests**

Run:

```bash
cd backend
GOMODCACHE=/root/Clouds/N2API/.cache/go-mod GOCACHE=/root/Clouds/N2API/.cache/go-build go test ./internal/admin ./internal/httpapi -run 'Model|AccountModels|Routing'
```

Expected: FAIL until interfaces and handlers are implemented.

- [ ] **Step 4: Extend interfaces and services**

In `httpapi.ProviderService`, add:

```go
ListAccountModels(ctx context.Context, accountID int64) ([]provider.AccountModel, error)
ReplaceAccountModels(ctx context.Context, accountID int64, models []provider.AccountModelInput) ([]provider.AccountModel, error)
ListExposedModels(ctx context.Context, allowedModels []string) ([]provider.ExposedModel, error)
```

In `admin.Service`, add helpers:

```go
func (s *Service) DefaultModel(ctx context.Context) (string, error)
func (s *Service) IsModelAllowed(ctx context.Context, model string) (bool, error)
```

- [ ] **Step 5: Add handlers**

Add handlers inside `NewServer`:

```go
mux.HandleFunc("GET /api/admin/providers/openai/accounts/{id}/models", requireAdmin(...))
mux.HandleFunc("PUT /api/admin/providers/openai/accounts/{id}/models", requireAdmin(...))
mux.HandleFunc("GET /api/admin/model-routing", requireAdmin(...))
```

For `PUT`, decode:

```go
var req struct {
	Models []provider.AccountModelInput `json:"models"`
}
```

Map `provider.ErrNotConnected` to `404 not_found` and `provider.ErrInvalidInput` to `400 invalid_input`.

- [ ] **Step 6: Run admin/API tests**

Run:

```bash
cd backend
GOMODCACHE=/root/Clouds/N2API/.cache/go-mod GOCACHE=/root/Clouds/N2API/.cache/go-build go test ./internal/admin ./internal/httpapi
```

Expected: PASS.

- [ ] **Step 7: Commit**

Run:

```bash
git add backend/internal/admin/service.go backend/internal/admin/service_test.go backend/internal/httpapi/server.go backend/internal/httpapi/server_test.go
git commit -m "feat: expose account model admin api"
```

## Task 6: Frontend Manual Model Configuration

**Files:**
- Modify: `frontend/src/lib/admin-state.svelte.js`
- Modify: `frontend/src/routes/providers/+page.svelte`
- Modify: `frontend/src/routes/models/+page.svelte`
- Modify: `frontend/src/routes/providers/provider-page.test.mjs`

- [ ] **Step 1: Add state helper tests**

In `frontend/src/routes/providers/provider-page.test.mjs`, add tests for parsing model textarea input:

```js
import { parseAccountModelsText } from '$lib/admin-state.svelte.js';

test('parseAccountModelsText trims blanks and deduplicates', () => {
  assert.deepEqual(parseAccountModelsText(' gpt-5\\n\\ngpt-5-mini\\ngpt-5 '), [
    { model: 'gpt-5', enabled: true },
    { model: 'gpt-5-mini', enabled: true }
  ]);
});
```

- [ ] **Step 2: Run failing frontend helper test**

Run:

```bash
cd frontend
bun test src/routes/providers/provider-page.test.mjs
```

Expected: FAIL because `parseAccountModelsText` is missing.

- [ ] **Step 3: Add account model state/actions**

In `frontend/src/lib/admin-state.svelte.js`, export:

```js
export function parseAccountModelsText(value) {
  const seen = new Set();
  return String(value ?? '')
    .split('\n')
    .map((line) => line.trim())
    .filter(Boolean)
    .filter((model) => {
      if (seen.has(model)) return false;
      seen.add(model);
      return true;
    })
    .map((model) => ({ model, enabled: true }));
}
```

Add per-account model state keyed by account id:

```js
export const accountModels = $state({
  byAccountId: {},
  loading: {},
  saving: {},
  error: {}
});
```

Add `loadAccountModels(accountId)` and `saveAccountModels(accountId, text)` using the new admin endpoints.

- [ ] **Step 4: Update Providers page**

In `frontend/src/routes/providers/+page.svelte`, add an account models editor in each account row/details area:

```svelte
<textarea
  class="mt-2 min-h-24 w-full resize-y rounded-lg border border-[#e5e5e5] bg-white px-3 py-2 font-mono text-[13px] leading-6 text-[#0d0d0d] outline-none focus:border-[#10a37f] focus:ring-2 focus:ring-[#e8f5f0]"
  bind:value={accountModels.byAccountId[account.id].text}
  placeholder={'gpt-5\ngpt-5-mini'}
></textarea>
```

Add a compact save button and inline error/saved state. Load models when provider accounts are loaded.

- [ ] **Step 5: Update Models page**

In `frontend/src/routes/models/+page.svelte`, keep default/allowed editing and add aggregate routing status from `GET /api/admin/model-routing`. Show rows for model, allowed, configured accounts, enabled accounts, and warnings.

- [ ] **Step 6: Run frontend checks**

Run:

```bash
cd frontend
bun test src/routes/providers/provider-page.test.mjs
bun run check
bun run build
```

Expected: PASS.

- [ ] **Step 7: Commit**

Run:

```bash
git add frontend/src/lib/admin-state.svelte.js frontend/src/routes/providers/+page.svelte frontend/src/routes/models/+page.svelte frontend/src/routes/providers/provider-page.test.mjs
git commit -m "feat: add manual account model controls"
```

## Task 7: Docs And Full Verification

**Files:**
- Modify: `README.md`
- Modify: `deploy/README.md`

- [ ] **Step 1: Update docs**

Document:

- models are configured per connected account
- global model settings control exposure and default model
- `/v1/models` returns aggregate exposed models
- fallback only occurs between accounts that support the requested model
- connected accounts with no configured models do not receive model-routed POST traffic

- [ ] **Step 2: Run backend verification**

Run:

```bash
cd backend
GOMODCACHE=/root/Clouds/N2API/.cache/go-mod GOCACHE=/root/Clouds/N2API/.cache/go-build go test ./...
```

Expected: PASS.

- [ ] **Step 3: Run frontend verification**

Run:

```bash
cd frontend
bun test src/routes/providers/provider-page.test.mjs
bun run check
bun run build
```

Expected: PASS.

- [ ] **Step 4: Run optional local Compose smoke**

If the user wants runtime verification against the local stack, run:

```bash
docker compose -f deploy/compose.yaml up -d --build n2api
curl -sS http://localhost:3000/api/admin/health
```

Expected: health JSON with `"status":"ok"`.

- [ ] **Step 5: Commit docs**

Run:

```bash
git add README.md deploy/README.md
git commit -m "docs: document account model routing"
```

## Self-Review Checklist

- Spec coverage: every requirement in `docs/superpowers/specs/2026-06-22-account-model-routing-design.md` maps to a task above.
- Placeholder scan: this plan contains no TBD/TODO/fill-in placeholders.
- Type consistency: `AccountModel`, `AccountModelInput`, `ExposedModel`, `SelectAccessToken(ctx, model, excluded...)`, and endpoint paths are used consistently.
- Verification: backend, frontend, and optional Compose verification commands are explicit.

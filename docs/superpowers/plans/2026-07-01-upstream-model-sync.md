# Upstream Model Sync Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add a manual admin action that syncs API-upstream account models from `GET /v1/models`, stores synced rows separately from manual rows, and keeps newly discovered models disabled until the admin enables them.

**Architecture:** Add an `upstream` account-model source beside the existing `manual` source. The store layer replaces only upstream rows during sync, preserving manual rows and previous upstream enabled state. The provider service owns credential decryption and OpenAI-compatible model-list HTTP calls, the admin HTTP API exposes `POST /api/admin/provider-accounts/{id}/models/sync`, and the provider account edit modal shows a combined model manager.

**Tech Stack:** Go backend, PostgreSQL store, SvelteKit/Svelte 5 frontend, Bun tests, Docker Compose local stack.

---

## File Structure

- Modify `backend/internal/provider/service.go`: add `AccountModelSourceUpstream`, `AccountModelSyncSummary`, `SyncUpstreamAccountModels`, repository interface method, and model-list HTTP parsing.
- Modify `backend/internal/provider/service_test.go`: add service and memory-repo coverage for sync parsing, credential use, error behavior, and non-API-upstream rejection.
- Modify `backend/internal/store/provider.go`: add transactional `SyncAccountModels` implementation.
- Modify `backend/internal/store/provider_test.go`: add PostgreSQL repository tests for upstream rows, preservation, stale removal, and manual conflict behavior.
- Modify `backend/internal/httpapi/server.go`: add provider service interface method, route, handler, and JSON response shape.
- Modify `backend/internal/httpapi/server_test.go`: add endpoint success, auth, and error-mapping coverage.
- Modify `frontend/src/lib/admin-state.svelte.js`: add account-model sync state, summary helpers, `syncAccountModels`, source-aware manual text helpers, and source-aware remove behavior.
- Modify `frontend/src/routes/providers/+page.svelte`: rename the edit modal model section to `Models`, add sync button/status, summary counts, source badges, and manual-only remove.
- Modify `frontend/src/routes/providers/provider-page.test.mjs`: update source-level and state-helper tests.

## Task 1: Store Upstream Model Rows

**Files:**
- Modify: `backend/internal/provider/service.go`
- Modify: `backend/internal/store/provider.go`
- Modify: `backend/internal/store/provider_test.go`
- Modify: `backend/internal/provider/service_test.go`

- [ ] **Step 1: Write failing store tests**

Add tests near `TestReplaceAccountModelsNormalizesAndListsRows` in `backend/internal/store/provider_test.go`:

```go
func TestSyncAccountModelsPreservesManualRowsAndDisablesNewUpstreamRows(t *testing.T) {
	repo, cleanup := newProviderRepositoryForTest(t)
	defer cleanup()

	ctx := context.Background()
	account := saveProviderTestAccount(t, repo, provider.Account{
		Provider:             "openai",
		AccountType:          provider.AccountTypeAPIUpstream,
		Subject:              "sync-account",
		DisplayName:          "Sync Account",
		EncryptedAccessToken: "access",
		Enabled:              true,
		Priority:             10,
		Status:               "active",
	})

	if _, err := repo.ReplaceAccountModels(ctx, "openai", account.ID, []provider.AccountModelInput{
		{Model: "manual-only", Enabled: true},
		{Model: "shared-model", Enabled: true},
	}); err != nil {
		t.Fatalf("ReplaceAccountModels returned error: %v", err)
	}

	models, summary, err := repo.SyncAccountModels(ctx, "openai", account.ID, []provider.AccountModelInput{
		{Model: " upstream-new ", Enabled: true},
		{Model: "shared-model", Enabled: true},
	}, time.Date(2026, 7, 1, 8, 0, 0, 0, time.UTC))
	if err != nil {
		t.Fatalf("SyncAccountModels returned error: %v", err)
	}

	if summary.Total != 2 || summary.New != 1 || summary.Preserved != 0 || summary.SkippedManual != 1 {
		t.Fatalf("summary = %+v, want total/new/skipped manual counts", summary)
	}
	assertAccountModelRows(t, models, []accountModelWant{
		{Model: "manual-only", Enabled: true},
		{Model: "shared-model", Enabled: true},
		{Model: "upstream-new", Enabled: false},
	})
	for _, model := range models {
		if model.Model == "upstream-new" {
			if model.Source != provider.AccountModelSourceUpstream || model.LastSeenAt == nil {
				t.Fatalf("upstream model = %+v, want upstream source with last seen", model)
			}
		}
		if model.Model == "shared-model" && model.Source != provider.AccountModelSourceManual {
			t.Fatalf("shared model source = %q, want manual", model.Source)
		}
	}
}

func TestSyncAccountModelsPreservesExistingUpstreamEnabledAndRemovesStaleRows(t *testing.T) {
	repo, cleanup := newProviderRepositoryForTest(t)
	defer cleanup()

	ctx := context.Background()
	account := saveProviderTestAccount(t, repo, provider.Account{
		Provider:             "openai",
		AccountType:          provider.AccountTypeAPIUpstream,
		Subject:              "sync-stale",
		DisplayName:          "Sync Stale",
		EncryptedAccessToken: "access",
		Enabled:              true,
		Priority:             10,
		Status:               "active",
	})

	_, _, err := repo.SyncAccountModels(ctx, "openai", account.ID, []provider.AccountModelInput{
		{Model: "kept-model", Enabled: true},
		{Model: "stale-model", Enabled: true},
	}, time.Date(2026, 7, 1, 8, 0, 0, 0, time.UTC))
	if err != nil {
		t.Fatalf("first SyncAccountModels returned error: %v", err)
	}
	if _, err := repo.pool.Exec(ctx, `UPDATE provider_account_models SET enabled = true WHERE account_id = $1 AND model = 'kept-model'`, account.ID); err != nil {
		t.Fatalf("enable kept model: %v", err)
	}

	models, summary, err := repo.SyncAccountModels(ctx, "openai", account.ID, []provider.AccountModelInput{
		{Model: "kept-model", Enabled: false},
		{Model: "new-model", Enabled: true},
	}, time.Date(2026, 7, 1, 9, 0, 0, 0, time.UTC))
	if err != nil {
		t.Fatalf("second SyncAccountModels returned error: %v", err)
	}

	if summary.Total != 2 || summary.New != 1 || summary.Preserved != 1 {
		t.Fatalf("summary = %+v, want one preserved and one new", summary)
	}
	assertAccountModelRows(t, models, []accountModelWant{
		{Model: "kept-model", Enabled: true},
		{Model: "new-model", Enabled: false},
	})
}
```

Add `time` to imports if needed.

- [ ] **Step 2: Run store tests and confirm they fail**

Run:

```bash
cd backend
GOMODCACHE=/root/Clouds/N2API/.cache/go-mod GOCACHE=/root/Clouds/N2API/.cache/go-build go test ./internal/store -run 'TestSyncAccountModels' -count=1
```

Expected: fail because `SyncAccountModels`, `AccountModelSourceUpstream`, and `AccountModelSyncSummary` do not exist.

- [ ] **Step 3: Add provider types and repository interface method**

In `backend/internal/provider/service.go`, update the model constants and repository interface:

```go
const (
	AccountModelSourceManual   = "manual"
	AccountModelSourceUpstream = "upstream"

	maxAccountModels = 100
	maxModelNameLen  = 128
)

type AccountModelSyncSummary struct {
	Total         int `json:"total"`
	New           int `json:"new"`
	Preserved     int `json:"preserved"`
	SkippedManual int `json:"skippedManual"`
}
```

Add to `Repository`:

```go
SyncAccountModels(ctx context.Context, provider string, accountID int64, models []AccountModelInput, seenAt time.Time) ([]AccountModel, AccountModelSyncSummary, error)
```

- [ ] **Step 4: Implement store sync transaction**

In `backend/internal/store/provider.go`, add `SyncAccountModels` near `ReplaceAccountModels`. It must:

- call `provider.Normalize` through existing `normalizeAccountModelInputs` path indirectly is not available in store package; use the existing store-local `normalizeAccountModelInputs` already used by `ReplaceAccountModels`
- lock `provider_accounts` with `FOR UPDATE`
- read existing upstream rows and manual rows before deleting upstream rows
- delete only `source = provider.AccountModelSourceUpstream`
- insert synced rows with `enabled=false` when new, previous enabled state when preserved
- skip rows where a manual row already exists
- return all account models ordered by model

Use this structure:

```go
func (r *ProviderRepository) SyncAccountModels(ctx context.Context, providerName string, accountID int64, inputs []provider.AccountModelInput, seenAt time.Time) ([]provider.AccountModel, provider.AccountModelSyncSummary, error) {
	models, err := normalizeAccountModelInputs(inputs)
	if err != nil {
		return nil, provider.AccountModelSyncSummary{}, err
	}
	if seenAt.IsZero() {
		seenAt = time.Now().UTC()
	}

	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return nil, provider.AccountModelSyncSummary{}, err
	}
	defer tx.Rollback(ctx)

	var existingID int64
	err = tx.QueryRow(ctx, `
		SELECT id
		FROM provider_accounts
		WHERE provider = $1
			AND id = $2
		FOR UPDATE
	`, providerName, accountID).Scan(&existingID)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, provider.AccountModelSyncSummary{}, provider.ErrNotConnected
	}
	if err != nil {
		return nil, provider.AccountModelSyncSummary{}, err
	}

	upstreamEnabled := map[string]bool{}
	manualModels := map[string]bool{}
	rows, err := tx.Query(ctx, `
		SELECT model, enabled, source
		FROM provider_account_models
		WHERE provider = $1
			AND account_id = $2
	`, providerName, accountID)
	if err != nil {
		return nil, provider.AccountModelSyncSummary{}, err
	}
	for rows.Next() {
		var model string
		var enabled bool
		var source string
		if err := rows.Scan(&model, &enabled, &source); err != nil {
			rows.Close()
			return nil, provider.AccountModelSyncSummary{}, err
		}
		if source == provider.AccountModelSourceUpstream {
			upstreamEnabled[model] = enabled
		}
		if source == provider.AccountModelSourceManual || source == "" {
			manualModels[model] = true
		}
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return nil, provider.AccountModelSyncSummary{}, err
	}
	rows.Close()

	_, err = tx.Exec(ctx, `
		DELETE FROM provider_account_models
		WHERE provider = $1
			AND account_id = $2
			AND source = $3
	`, providerName, accountID, provider.AccountModelSourceUpstream)
	if err != nil {
		return nil, provider.AccountModelSyncSummary{}, err
	}

	summary := provider.AccountModelSyncSummary{Total: len(models)}
	for _, model := range models {
		if manualModels[model.Model] {
			summary.SkippedManual++
			continue
		}
		enabled, ok := upstreamEnabled[model.Model]
		if ok {
			summary.Preserved++
		} else {
			summary.New++
			enabled = false
		}
		_, err = tx.Exec(ctx, `
			INSERT INTO provider_account_models (
				account_id, provider, model, enabled, source, last_seen_at, last_error, metadata, updated_at
			)
			VALUES ($1, $2, $3, $4, $5, $6, '', '{}'::jsonb, now())
		`, accountID, providerName, model.Model, enabled, provider.AccountModelSourceUpstream, seenAt)
		if err != nil {
			return nil, provider.AccountModelSyncSummary{}, err
		}
	}

	rows, err = tx.Query(ctx, `
		SELECT `+providerAccountModelColumns+`
		FROM provider_account_models
		WHERE provider = $1
			AND account_id = $2
		ORDER BY model ASC
	`, providerName, accountID)
	if err != nil {
		return nil, provider.AccountModelSyncSummary{}, err
	}
	defer rows.Close()

	saved := []provider.AccountModel{}
	for rows.Next() {
		model, err := scanProviderAccountModel(rows)
		if err != nil {
			return nil, provider.AccountModelSyncSummary{}, err
		}
		saved = append(saved, model)
	}
	if err := rows.Err(); err != nil {
		return nil, provider.AccountModelSyncSummary{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, provider.AccountModelSyncSummary{}, err
	}
	return saved, summary, nil
}
```

Keep helper code local and boring: `map[string]bool` for existing upstream enabled state and `map[string]bool` for manual models.

- [ ] **Step 5: Update memory repo fake**

In `backend/internal/provider/service_test.go`, implement `SyncAccountModels` on `memoryRepo`:

```go
func (r *memoryRepo) SyncAccountModels(ctx context.Context, providerName string, accountID int64, inputs []AccountModelInput, seenAt time.Time) ([]AccountModel, AccountModelSyncSummary, error) {
	normalized, err := normalizeAccountModelInputs(inputs)
	if err != nil {
		return nil, AccountModelSyncSummary{}, err
	}
	if _, err := r.FindAccountByID(ctx, providerName, accountID); err != nil {
		return nil, AccountModelSyncSummary{}, err
	}
	existing := r.accountModels[accountID]
	upstreamEnabled := map[string]bool{}
	manual := map[string]bool{}
	for _, row := range existing {
		if row.Source == AccountModelSourceUpstream {
			upstreamEnabled[row.Model] = row.Enabled
		}
		if row.Source == AccountModelSourceManual || row.Source == "" {
			manual[row.Model] = true
		}
	}
	kept := make([]AccountModel, 0, len(existing)+len(normalized))
	for _, row := range existing {
		if row.Source != AccountModelSourceUpstream {
			kept = append(kept, row)
		}
	}
	summary := AccountModelSyncSummary{Total: len(normalized)}
	for _, input := range normalized {
		if manual[input.Model] {
			summary.SkippedManual++
			continue
		}
		enabled, ok := upstreamEnabled[input.Model]
		if ok {
			summary.Preserved++
		} else {
			summary.New++
			enabled = false
		}
		kept = append(kept, AccountModel{
			ID:         int64(len(kept) + 1),
			AccountID:  accountID,
			Provider:   providerName,
			Model:      input.Model,
			Enabled:    enabled,
			Source:     AccountModelSourceUpstream,
			LastSeenAt: &seenAt,
			Metadata:   map[string]string{},
			CreatedAt:  seenAt,
			UpdatedAt:  seenAt,
		})
	}
	sort.Slice(kept, func(i, j int) bool { return kept[i].Model < kept[j].Model })
	r.accountModels[accountID] = kept
	return r.ListAccountModels(ctx, providerName, accountID)
}
```

Adjust return to include `summary`:

```go
models, err := r.ListAccountModels(ctx, providerName, accountID)
return models, summary, err
```

- [ ] **Step 6: Run store tests**

Run:

```bash
cd backend
GOMODCACHE=/root/Clouds/N2API/.cache/go-mod GOCACHE=/root/Clouds/N2API/.cache/go-build go test ./internal/store -run 'TestReplaceAccountModels|TestSyncAccountModels' -count=1
```

Expected: PASS.

- [ ] **Step 7: Commit store layer**

Run:

```bash
git add backend/internal/provider/service.go backend/internal/provider/service_test.go backend/internal/store/provider.go backend/internal/store/provider_test.go
git commit -m "feat: store synced account models"
```

## Task 2: Service Sync From OpenAI-Compatible Upstream

**Files:**
- Modify: `backend/internal/provider/service.go`
- Modify: `backend/internal/provider/service_test.go`

- [ ] **Step 1: Write failing service tests**

Add tests near `TestCreateAPIUpstreamAccountSavesEncryptedKeyAndEnabledModels` in `backend/internal/provider/service_test.go`:

```go
func TestSyncUpstreamAccountModelsFetchesOpenAICompatibleModelList(t *testing.T) {
	var gotAuthorization string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/models" {
			t.Fatalf("path = %s, want /v1/models", r.URL.Path)
		}
		gotAuthorization = r.Header.Get("Authorization")
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"object":"list","data":[{"id":"gpt-5","object":"model","owned_by":"openai"},{"id":"gpt-4.1","object":"model","owned_by":"openai"},{"id":"gpt-5"}]}`))
	}))
	defer server.Close()

	repo := newMemoryRepo()
	service := newConfiguredService(repo, fakeOAuthClient{})
	service.cfg.AllowHTTPAPIUpstreams = true
	account, err := service.CreateAPIUpstreamAccount(context.Background(), APIUpstreamInput{
		Name:    "Upstream",
		BaseURL: server.URL + "/v1",
		APIKey:  "sk-upstream",
	})
	if err != nil {
		t.Fatalf("CreateAPIUpstreamAccount returned error: %v", err)
	}

	models, summary, err := service.SyncUpstreamAccountModels(context.Background(), account.ID)
	if err != nil {
		t.Fatalf("SyncUpstreamAccountModels returned error: %v", err)
	}
	if gotAuthorization != "Bearer sk-upstream" {
		t.Fatalf("authorization = %q, want bearer upstream key", gotAuthorization)
	}
	if summary.Total != 2 || summary.New != 2 {
		t.Fatalf("summary = %+v, want two new models", summary)
	}
	if got := modelNamesAndEnabled(models); strings.Join(got, ",") != "gpt-4.1:false,gpt-5:false" {
		t.Fatalf("models = %v, want synced disabled models", got)
	}
}

func TestSyncUpstreamAccountModelsRejectsNonAPIUpstreamAccount(t *testing.T) {
	repo := newMemoryRepo()
	repo.accounts = []Account{testAccount(t, 7, true, 1, "access-token")}
	service := newConfiguredService(repo, fakeOAuthClient{})

	if _, _, err := service.SyncUpstreamAccountModels(context.Background(), 7); !errors.Is(err, ErrInvalidInput) {
		t.Fatalf("SyncUpstreamAccountModels error = %v, want ErrInvalidInput", err)
	}
}

func TestSyncUpstreamAccountModelsDoesNotUpdateOnUpstreamError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
	}))
	defer server.Close()

	repo := newMemoryRepo()
	service := newConfiguredService(repo, fakeOAuthClient{})
	service.cfg.AllowHTTPAPIUpstreams = true
	account, err := service.CreateAPIUpstreamAccount(context.Background(), APIUpstreamInput{
		Name:    "Upstream",
		BaseURL: server.URL + "/v1",
		APIKey:  "sk-upstream",
	})
	if err != nil {
		t.Fatalf("CreateAPIUpstreamAccount returned error: %v", err)
	}

	if _, _, err := service.SyncUpstreamAccountModels(context.Background(), account.ID); err == nil {
		t.Fatal("SyncUpstreamAccountModels error = nil, want upstream error")
	}
	models, err := service.ListAccountModels(context.Background(), account.ID)
	if err != nil {
		t.Fatalf("ListAccountModels returned error: %v", err)
	}
	if len(models) != 0 {
		t.Fatalf("models = %+v, want no partial update", models)
	}
}
```

Add `net/http/httptest` import if needed.

- [ ] **Step 2: Run service tests and confirm they fail**

Run:

```bash
cd backend
GOMODCACHE=/root/Clouds/N2API/.cache/go-mod GOCACHE=/root/Clouds/N2API/.cache/go-build go test ./internal/provider -run 'TestSyncUpstreamAccountModels' -count=1
```

Expected: fail because `SyncUpstreamAccountModels` does not exist.

- [ ] **Step 3: Add service method and response parser**

In `backend/internal/provider/service.go`, add:

```go
func (s *Service) SyncUpstreamAccountModels(ctx context.Context, accountID int64) ([]AccountModel, AccountModelSyncSummary, error) {
	if accountID <= 0 {
		return nil, AccountModelSyncSummary{}, ErrInvalidInput
	}
	account, err := s.repo.FindAccountByID(ctx, s.cfg.Provider, accountID)
	if err != nil {
		return nil, AccountModelSyncSummary{}, err
	}
	if account.AccountType != AccountTypeAPIUpstream {
		return nil, AccountModelSyncSummary{}, ErrInvalidInput
	}
	selected, err := s.selectedAccount(ctx, account)
	if err != nil {
		return nil, AccountModelSyncSummary{}, err
	}
	models, err := s.fetchUpstreamModelIDs(ctx, selected)
	if err != nil {
		return nil, AccountModelSyncSummary{}, err
	}
	return s.repo.SyncAccountModels(ctx, s.cfg.Provider, accountID, models, time.Now().UTC())
}
```

Add helper methods:

```go
func (s *Service) fetchUpstreamModelIDs(ctx context.Context, account SelectedAccount) ([]AccountModelInput, error) {
	baseURL := strings.TrimRight(strings.TrimSpace(account.BaseURL), "/")
	if baseURL == "" || strings.TrimSpace(account.AuthorizationToken) == "" {
		return nil, ErrInvalidInput
	}
	endpoint := baseURL + "/models"
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return nil, ErrInvalidInput
	}
	req.Header.Set("Authorization", "Bearer "+account.AuthorizationToken)
	req.Header.Set("Accept", "application/json")

	client := s.client.clientForProxy(account.ProxyURL)
	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("upstream model sync failed: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("upstream returned %d while listing models", resp.StatusCode)
	}
	var body struct {
		Data []struct {
			ID string `json:"id"`
		} `json:"data"`
	}
	if err := json.NewDecoder(io.LimitReader(resp.Body, 2<<20)).Decode(&body); err != nil {
		return nil, fmt.Errorf("invalid upstream model list response: %w", err)
	}
	if body.Data == nil {
		return nil, fmt.Errorf("invalid upstream model list response")
	}
	inputs := make([]AccountModelInput, 0, len(body.Data))
	for _, item := range body.Data {
		inputs = append(inputs, AccountModelInput{Model: item.ID, Enabled: true})
	}
	normalized, err := normalizeAccountModelInputs(inputs)
	if err != nil {
		return nil, err
	}
	return normalized, nil
}
```

Add imports `encoding/json` and `io` if needed. Preserve existing credential secrecy: never include token values in errors.

- [ ] **Step 4: Run service tests**

Run:

```bash
cd backend
GOMODCACHE=/root/Clouds/N2API/.cache/go-mod GOCACHE=/root/Clouds/N2API/.cache/go-build go test ./internal/provider -run 'TestSyncUpstreamAccountModels|TestCreateAPIUpstreamAccountSavesEncryptedKeyAndEnabledModels' -count=1
```

Expected: PASS.

- [ ] **Step 5: Commit service layer**

Run:

```bash
git add backend/internal/provider/service.go backend/internal/provider/service_test.go
git commit -m "feat: sync models from api upstream"
```

## Task 3: Admin HTTP Sync Endpoint

**Files:**
- Modify: `backend/internal/httpapi/server.go`
- Modify: `backend/internal/httpapi/server_test.go`

- [ ] **Step 1: Write failing HTTP API tests**

In `backend/internal/httpapi/server_test.go`, add near `TestUnifiedAccountModelsEndpoints`:

```go
func TestSyncProviderAccountModelsReturnsModelsAndSummary(t *testing.T) {
	providers := newFakeProviderService()
	providers.accountModels[7] = []provider.AccountModel{}
	providers.syncModelsResult = []provider.AccountModel{
		{ID: 11, AccountID: 7, Provider: "openai", Model: "gpt-5", Enabled: false, Source: provider.AccountModelSourceUpstream},
	}
	providers.syncModelsSummary = provider.AccountModelSyncSummary{Total: 1, New: 1}
	server := NewServer(config.Config{}, staticHealth{}, newFakeAdminService(), providers)
	req := httptest.NewRequest(http.MethodPost, "/api/admin/provider-accounts/7/models/sync", nil)
	req.AddCookie(&http.Cookie{Name: "n2api_admin_session", Value: "valid-session"})
	recorder := httptest.NewRecorder()

	server.ServeHTTP(recorder, req)

	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d body=%s, want 200", recorder.Code, recorder.Body.String())
	}
	var body struct {
		Models []provider.AccountModel          `json:"models"`
		Synced provider.AccountModelSyncSummary `json:"synced"`
	}
	if err := json.Unmarshal(recorder.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode body: %v", err)
	}
	if len(body.Models) != 1 || body.Models[0].Source != provider.AccountModelSourceUpstream || body.Synced.New != 1 {
		t.Fatalf("body = %+v", body)
	}
}
```

Extend `TestAccountModelsMapProviderErrors` or add a sync-specific table for `ErrInvalidInput` and `ErrNotConnected`:

```go
func TestSyncProviderAccountModelsMapsProviderErrors(t *testing.T) {
	for _, tc := range []struct {
		name string
		err  error
		want int
		code string
	}{
		{name: "invalid input", err: provider.ErrInvalidInput, want: http.StatusBadRequest, code: "invalid_input"},
		{name: "not found", err: provider.ErrNotConnected, want: http.StatusNotFound, code: "not_found"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			providers := newFakeProviderService()
			providers.syncModelsErr = tc.err
			server := NewServer(config.Config{}, staticHealth{}, newFakeAdminService(), providers)
			req := httptest.NewRequest(http.MethodPost, "/api/admin/provider-accounts/7/models/sync", nil)
			req.AddCookie(&http.Cookie{Name: "n2api_admin_session", Value: "valid-session"})
			recorder := httptest.NewRecorder()

			server.ServeHTTP(recorder, req)

			if recorder.Code != tc.want {
				t.Fatalf("status = %d, want %d", recorder.Code, tc.want)
			}
			var body map[string]string
			if err := json.Unmarshal(recorder.Body.Bytes(), &body); err != nil {
				t.Fatalf("decode body: %v", err)
			}
			if body["error"] != tc.code {
				t.Fatalf("error = %q, want %s", body["error"], tc.code)
			}
		})
	}
}
```

- [ ] **Step 2: Run HTTP tests and confirm they fail**

Run:

```bash
cd backend
GOMODCACHE=/root/Clouds/N2API/.cache/go-mod GOCACHE=/root/Clouds/N2API/.cache/go-build go test ./internal/httpapi -run 'TestSyncProviderAccountModels' -count=1
```

Expected: fail because route/service fake method does not exist.

- [ ] **Step 3: Add HTTP interface and handler**

In `backend/internal/httpapi/server.go`, add to `ProviderService`:

```go
SyncUpstreamAccountModels(ctx context.Context, accountID int64) ([]provider.AccountModel, provider.AccountModelSyncSummary, error)
```

Register the route after the existing model endpoints:

```go
mux.HandleFunc("POST /api/admin/provider-accounts/{id}/models/sync", requireAdmin(func(w http.ResponseWriter, r *http.Request, _ admin.Admin) {
	handleSyncProviderAccountModels(w, r, providers)
}))
```

Add handler:

```go
func handleSyncProviderAccountModels(w http.ResponseWriter, r *http.Request, providers ProviderService) {
	if providers == nil {
		writeError(w, http.StatusServiceUnavailable, "service_unavailable")
		return
	}
	id, err := parsePositivePathID(r, "id")
	if err != nil {
		writeError(w, http.StatusBadRequest, "bad_request")
		return
	}
	models, summary, err := providers.SyncUpstreamAccountModels(r.Context(), id)
	if err != nil {
		writeProviderAccountError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"models": models, "synced": summary})
}
```

- [ ] **Step 4: Update fake provider service**

In `backend/internal/httpapi/server_test.go`, add fake fields:

```go
syncModelsResult  []provider.AccountModel
syncModelsSummary provider.AccountModelSyncSummary
syncModelsErr     error
```

Add fake method:

```go
func (s *fakeProviderService) SyncUpstreamAccountModels(_ context.Context, accountID int64) ([]provider.AccountModel, provider.AccountModelSyncSummary, error) {
	if s.syncModelsErr != nil {
		return nil, provider.AccountModelSyncSummary{}, s.syncModelsErr
	}
	if s.syncModelsResult != nil {
		s.accountModels[accountID] = s.syncModelsResult
		return s.syncModelsResult, s.syncModelsSummary, nil
	}
	models := s.accountModels[accountID]
	return models, s.syncModelsSummary, nil
}
```

- [ ] **Step 5: Run HTTP tests**

Run:

```bash
cd backend
GOMODCACHE=/root/Clouds/N2API/.cache/go-mod GOCACHE=/root/Clouds/N2API/.cache/go-build go test ./internal/httpapi -run 'TestSyncProviderAccountModels|TestUnifiedAccountModelsEndpoints|TestAccountModelsMapProviderErrors' -count=1
```

Expected: PASS.

- [ ] **Step 6: Commit HTTP endpoint**

Run:

```bash
git add backend/internal/httpapi/server.go backend/internal/httpapi/server_test.go
git commit -m "feat: expose account model sync api"
```

## Task 4: Frontend State And Helpers

**Files:**
- Modify: `frontend/src/lib/admin-state.svelte.js`
- Modify: `frontend/src/routes/providers/provider-page.test.mjs`

- [ ] **Step 1: Write failing frontend state tests**

Update imports in `frontend/src/routes/providers/provider-page.test.mjs` to include new helpers:

```js
  accountModelSummary,
  syncAccountModels,
  sourceBadgeLabel,
```

Add tests near existing account-model helper tests:

```js
test('accountModelSummary counts synced manual and enabled rows', () => {
  assert.deepEqual(
    accountModelSummary([
      { model: 'gpt-5', enabled: true, source: 'manual' },
      { model: 'gpt-4.1', enabled: false, source: 'upstream' },
      { model: 'codex-mini', enabled: true, source: 'upstream' }
    ]),
    { total: 3, manual: 1, synced: 2, enabled: 2 }
  );
});

test('sourceBadgeLabel maps account model sources', () => {
  assert.equal(sourceBadgeLabel({ source: 'manual' }), 'Manual');
  assert.equal(sourceBadgeLabel({ source: 'upstream' }), 'Synced');
  assert.equal(sourceBadgeLabel({ source: '' }), 'Manual');
});

test('syncAccountModels calls sync endpoint and refreshes routing state', async () => {
  session.authenticated = true;
  let requestedPath = '';
  globalThis.fetch = async (path, options = {}) => {
    requestedPath = String(path);
    assert.equal(options.method, 'POST');
    return new Response(JSON.stringify({
      models: [{ model: 'gpt-5', enabled: false, source: 'upstream' }],
      synced: { total: 1, new: 1, preserved: 0, skippedManual: 0 }
    }), {
      status: 200,
      headers: { 'content-type': 'application/json' }
    });
  };

  await syncAccountModels(7);
  assert.equal(requestedPath, '/api/admin/provider-accounts/7/models/sync');
});
```

- [ ] **Step 2: Run frontend tests and confirm they fail**

Run:

```bash
cd frontend
bun test src/routes/providers/provider-page.test.mjs
```

Expected: fail because helpers and `syncAccountModels` do not exist.

- [ ] **Step 3: Extend account model state**

In `frontend/src/lib/admin-state.svelte.js`, update the state created by `ensureAccountModelsState`:

```js
accountModels[key] = {
  loading: false,
  saving: false,
  syncing: false,
  error: '',
  syncError: '',
  syncMessage: '',
  saved: false,
  text: '',
  items: [],
  syncSummary: null,
  requestSeq: 0
};
```

Ensure `loadAccountModels` clears `syncError` only when appropriate and does not overwrite a manual save error unexpectedly.

- [ ] **Step 4: Add source-aware helpers**

Add exports:

```js
/** @param {AccountModel[]} models */
export function accountModelSummary(models) {
  return models.reduce(
    (summary, model) => {
      summary.total += 1;
      if (model.enabled) summary.enabled += 1;
      if (model.source === 'upstream') summary.synced += 1;
      else summary.manual += 1;
      return summary;
    },
    { total: 0, synced: 0, manual: 0, enabled: 0 }
  );
}

/** @param {AccountModel} model */
export function sourceBadgeLabel(model) {
  if (model.source === 'upstream') return 'Synced';
  return 'Manual';
}
```

Keep `accountModelsText(models)` manual-oriented by returning only non-upstream rows:

```js
export function accountModelsText(models) {
  return modelListText(models.filter((item) => item.source !== 'upstream').map((item) => item.model));
}
```

This prevents synced rows from being pushed into the manual textarea after loading.

- [ ] **Step 5: Add sync action**

Add:

```js
/** @param {number} accountId */
export async function syncAccountModels(accountId) {
  const version = sessionVersion;
  if (!isCurrentAuthenticated(version)) return;
  const state = ensureAccountModelsState(accountId);
  state.requestSeq += 1;
  const requestSeq = state.requestSeq;
  state.syncing = true;
  state.error = '';
  state.syncError = '';
  state.syncMessage = '';
  state.saved = false;
  try {
    const payload = await requestJSON(`/api/admin/provider-accounts/${accountId}/models/sync`, {
      method: 'POST'
    });
    if (!isCurrentAuthenticated(version)) return;
    if (!shouldApplyAccountModelsResponse(state, requestSeq)) return;
    const models = payload.models ?? [];
    state.items = models;
    state.text = accountModelsText(models);
    state.syncSummary = payload.synced ?? null;
    const added = Number(payload.synced?.new ?? 0);
    const total = Number(payload.synced?.total ?? models.length);
    state.syncMessage = added > 0
      ? `Synced ${total} models. ${added} new model${added === 1 ? '' : 's'} were added disabled.`
      : `Synced ${total} models.`;
    await loadModelRouting();
  } catch (error) {
    if (!isCurrentAuthenticated(version)) return;
    if (!shouldApplyAccountModelsResponse(state, requestSeq)) return;
    state.syncError = error instanceof Error ? error.message : 'Account model sync failed';
  } finally {
    if (isCurrentAuthenticated(version) && shouldApplyAccountModelsResponse(state, requestSeq)) {
      state.syncing = false;
    }
  }
}
```

- [ ] **Step 6: Make removal manual-only**

Update `removeAccountModel` so it only removes manual rows and leaves synced rows untouched:

```js
export function removeAccountModel(models, modelName) {
  return models.filter((item) => item.model !== modelName || item.source === 'upstream');
}
```

Confirm existing tests still pass for source-less rows because they are treated as manual.

- [ ] **Step 7: Run frontend state tests**

Run:

```bash
cd frontend
bun test src/routes/providers/provider-page.test.mjs
```

Expected: PASS.

- [ ] **Step 8: Commit frontend state**

Run:

```bash
git add frontend/src/lib/admin-state.svelte.js frontend/src/routes/providers/provider-page.test.mjs
git commit -m "feat: add account model sync state"
```

## Task 5: Provider Edit Modal UI

**Files:**
- Modify: `frontend/src/routes/providers/+page.svelte`
- Modify: `frontend/src/routes/providers/provider-page.test.mjs`

- [ ] **Step 1: Write failing source-level UI tests**

Add/update tests in `frontend/src/routes/providers/provider-page.test.mjs` near the existing manual model controls tests:

```js
test('provider account edit modal exposes account model sync controls', () => {
  assert.match(source, /Sync from upstream/);
  assert.match(source, /Save manual/);
  assert.match(source, /accountModelSummary/);
  assert.match(source, /syncAccountModels\(account\.id\)/);
  assert.match(source, /sourceBadgeLabel\(configuredModel\)/);
});

test('provider account model list only offers remove for manual models', () => {
  assert.match(source, /configuredModel\.source !== 'upstream'/);
  assert.match(source, /Manual models/);
});
```

Update existing tests that assert `Manual models` if they need the broader `Models` heading too:

```js
assert.match(source, /<h3[^>]*>Models<\/h3>/);
assert.match(source, /Manual models/);
```

- [ ] **Step 2: Run provider page tests and confirm they fail**

Run:

```bash
cd frontend
bun test src/routes/providers/provider-page.test.mjs
```

Expected: fail because UI strings/imports are missing.

- [ ] **Step 3: Import new helpers**

In `frontend/src/routes/providers/+page.svelte`, extend imports:

```js
    accountModelSummary,
    sourceBadgeLabel,
    syncAccountModels,
```

- [ ] **Step 4: Update derived values in edit modal**

Inside `{#if editingProviderAccount}` after `enabledModels`, add:

```svelte
  {@const modelSummary = accountModelSummary(modelState.items)}
```

- [ ] **Step 5: Replace model section header and buttons**

Change the current `Manual models` form header to a `Models` header with summary and actions:

```svelte
<div class="flex flex-wrap items-start justify-between gap-3">
  <div class="min-w-0">
    <h3 class="text-sm font-semibold text-[#0d0d0d]">Models</h3>
    <p class="mt-1 text-xs text-[#6e6e6e]">
      {modelSummary.total} total · {modelSummary.synced} synced · {modelSummary.manual} manual · {modelSummary.enabled} enabled
    </p>
  </div>
  <div class="flex flex-wrap gap-2">
    {#if account.accountType === 'api_upstream'}
      <button
        class="rounded-md border border-[#e5e5e5] bg-white px-2.5 py-1.5 text-xs font-medium text-[#0d0d0d] hover:bg-[#f5f5f5] disabled:cursor-not-allowed disabled:text-[#9b9b9b]"
        type="button"
        disabled={modelState.loading || modelState.saving || modelState.syncing}
        onclick={() => syncAccountModels(account.id)}
      >
        {modelState.syncing ? 'Syncing' : 'Sync from upstream'}
      </button>
    {/if}
    <button
      class="rounded-md border border-[#e5e5e5] bg-white px-2.5 py-1.5 text-xs font-medium text-[#0d0d0d] hover:bg-[#f5f5f5] disabled:cursor-not-allowed disabled:text-[#9b9b9b]"
      type="submit"
      disabled={modelState.loading || modelState.saving || modelState.syncing}
    >
      {modelState.saving ? 'Saving' : 'Save manual'}
    </button>
  </div>
</div>
```

- [ ] **Step 6: Keep manual textarea and source-aware model list**

Add visible manual label before the textarea:

```svelte
<label class="grid gap-1 text-xs font-medium text-[#3c3c3c]" for={`provider-account-models-${account.id}`}>
  Manual models
  <textarea
    id={`provider-account-models-${account.id}`}
    class="min-h-16 w-full resize-y rounded-lg border border-[#e5e5e5] bg-white px-3 py-2 font-mono text-[13px] leading-5 text-[#0d0d0d] outline-none focus:border-[#10a37f] focus:ring-2 focus:ring-[#e8f5f0] disabled:cursor-not-allowed disabled:bg-[#f5f5f5] disabled:text-[#9b9b9b]"
    placeholder={'gpt-4.1\ngpt-4.1-mini'}
    bind:value={modelState.text}
    disabled={modelState.loading || modelState.saving || modelState.syncing}
  ></textarea>
</label>
```

In model rows, add source badge and hide remove for synced rows:

```svelte
<span class="rounded-full bg-[#f5f5f5] px-2 py-0.5 text-xs font-medium text-[#6e6e6e]">{sourceBadgeLabel(configuredModel)}</span>
{#if configuredModel.source !== 'upstream'}
  <button
    class="rounded-md border border-[#e5e5e5] bg-white px-2 py-1 text-xs font-medium text-[#0d0d0d] hover:bg-[#f5f5f5] disabled:cursor-not-allowed disabled:text-[#9b9b9b]"
    type="button"
    disabled={modelState.loading || modelState.saving || modelState.syncing}
    onclick={() => {
      modelState.items = removeAccountModel(modelState.items, configuredModel.model);
      modelState.saved = false;
    }}
  >Remove</button>
{:else}
  <span class="w-[4.5rem]" aria-hidden="true"></span>
{/if}
```

Adjust grid columns so rows remain stable:

```svelte
class="grid grid-cols-[minmax(0,1fr)_auto_auto_auto] items-center gap-2"
```

Use the `aria-hidden` empty span shown above when no remove button is rendered so row columns stay aligned.

- [ ] **Step 7: Add sync inline status**

Below saved/error messages add:

```svelte
{#if modelState.syncMessage}<p class="text-xs text-[#0a7a5e]">{modelState.syncMessage}</p>{/if}
{#if modelState.syncError}<p class="text-xs text-red-700">{modelState.syncError}</p>{/if}
```

- [ ] **Step 8: Run frontend tests and checks**

Run:

```bash
cd frontend
bun test src/routes/providers/provider-page.test.mjs
bun run check
```

Expected: PASS.

- [ ] **Step 9: Commit UI**

Run:

```bash
git add frontend/src/routes/providers/+page.svelte frontend/src/routes/providers/provider-page.test.mjs
git commit -m "feat: add upstream model sync controls"
```

## Task 6: Full Verification And Local Refresh

**Files:**
- No source edits expected unless verification exposes failures.

- [ ] **Step 1: Run backend tests**

Run:

```bash
cd backend
GOMODCACHE=/root/Clouds/N2API/.cache/go-mod GOCACHE=/root/Clouds/N2API/.cache/go-build go test ./...
```

Expected: PASS.

- [ ] **Step 2: Run frontend checks**

Run:

```bash
cd frontend
bun run check
bun run build
bun test
```

Expected: PASS.

- [ ] **Step 3: Inspect git status**

Run:

```bash
git status --short
```

Expected: clean after all task commits, or only expected uncommitted verification artifacts that must not be committed.

- [ ] **Step 4: Refresh local Docker Compose stack**

Use the `n2api-refresh-docker` skill before running these commands. Expected refresh path:

```bash
docker compose -f deploy/compose.yaml build --no-cache
docker compose -f deploy/compose.yaml up -d --force-recreate
docker compose -f deploy/compose.yaml ps
docker exec deploy-n2api-1 wget -qO- http://127.0.0.1:3000/healthz
```

Expected: app container healthy and `/healthz` returns a healthy response.

- [ ] **Step 5: Smoke check admin static route**

Run:

```bash
docker exec deploy-n2api-1 wget -qO- http://127.0.0.1:3000/ | head
```

Expected: HTML for the admin app, not the plain fallback.

- [ ] **Step 6: Report verification**

Final report must include:

- commit hashes created
- exact test commands that passed
- Docker refresh result
- any skipped verification with reason

## Plan Self-Review

- Spec coverage: the plan covers source separation, disabled-by-default synced models, preserving manual rows, sync endpoint, UI summary/source badges, inline errors, tests, and Docker refresh.
- Placeholder scan: no TBD/TODO/fill-in placeholders are intentionally left.
- Type consistency: `AccountModelSourceUpstream`, `AccountModelSyncSummary`, `SyncAccountModels`, and `SyncUpstreamAccountModels` names are consistent across backend, HTTP, and frontend tasks.

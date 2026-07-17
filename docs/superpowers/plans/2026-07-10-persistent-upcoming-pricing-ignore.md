# Persistent Upcoming Pricing Ignore Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Replace the persistent upcoming-shutdown pricing notice with a warning-icon modal that can remove and persistently ignore all affected models until an exact model is manually restored.

**Architecture:** Store `ignoredModels` beside `models` in the existing PostgreSQL-backed `usage_pricing` JSON document. Add a lifecycle-validating admin operation for atomic upcoming-model removal, make official sync skip ignored identifiers, and make the normal pricing update path treat an explicitly submitted model as a restore. Keep the frontend lifecycle list ephemeral while the backend owns durable ignore state.

**Tech Stack:** Go, PostgreSQL JSON settings repository, `net/http`, Bun, SvelteKit 2, Svelte 5 runes, Tailwind CSS 4, Lucide Svelte, Bun source-contract tests, Docker Compose.

---

## File Structure

- Modify `backend/internal/admin/service.go`: add `IgnoredModels`, normalize it, preserve it across ordinary pricing saves, and restore exact manually submitted models.
- Modify `backend/internal/admin/service_test.go`: cover normalization, preservation, restore, sync filtering, atomic upcoming ignore, and lifecycle reconstruction.
- Modify `backend/internal/admin/official_pricing.go`: skip ignored models during sync, preserve ignore state, and implement validated upcoming-model ignore.
- Modify `backend/internal/httpapi/server.go`: extend the admin service contract and register `POST /api/admin/usage-pricing/ignore-upcoming`.
- Modify `backend/internal/httpapi/server_test.go`: extend the fake service and cover the new authenticated endpoint.
- Modify `backend/internal/store/admin_test.go`: prove the JSON repository round-trips `IgnoredModels`.
- Modify `frontend/src/lib/admin-state.svelte.js`: add async state and the ignore-upcoming request helper.
- Modify `frontend/src/routes/request-logs/+page.svelte`: add the Scheme A warning icon and modal, remove the persistent warning band, and wire one-action removal.
- Modify `frontend/src/routes/navigation.test.mjs`: lock the frontend state request, action order, modal, accessibility, loading, and refresh contracts.

No new source file, database migration, dependency, or general ignored-model management UI is required.

### Task 1: Persist Ignore State And Restore Explicit Models

**Files:**
- Modify: `backend/internal/admin/service.go` (`UsagePricing`, `UpdateUsagePricing`, `normalizeUsagePricing`)
- Modify: `backend/internal/admin/service_test.go` (usage-pricing normalization and update tests)
- Modify: `backend/internal/store/admin_test.go` (`TestAdminRepositoryUsagePricingSettings`)

- [ ] **Step 1: Write failing service and repository tests**

Add focused tests that construct pricing with ignored models, verify deterministic normalization, preserve ignored identifiers on unrelated edits, and restore an explicitly submitted exact model:

```go
func TestNormalizeUsagePricingSortsIgnoredModels(t *testing.T) {
	pricing, err := normalizeUsagePricing(UsagePricing{
		Version: 1, Currency: "USD", Unit: "1M_tokens",
		Models: map[string]UsagePrice{"gpt-5.5": {}},
		IgnoredModels: []string{" gpt-5.3-chat-latest ", "gpt-5.2-codex"},
	})
	if err != nil {
		t.Fatalf("normalizeUsagePricing: %v", err)
	}
	if got, want := pricing.IgnoredModels, []string{"gpt-5.2-codex", "gpt-5.3-chat-latest"}; !slices.Equal(got, want) {
		t.Fatalf("ignored models = %v, want %v", got, want)
	}
}

func TestUpdateUsagePricingPreservesIgnoredModelsAndRestoresExplicitModel(t *testing.T) {
	repo := newMemoryRepo()
	repo.usagePricing = UsagePricing{
		Version: 1, Currency: "USD", Unit: "1M_tokens",
		Models: map[string]UsagePrice{"local-model": {InputMicrousdPerMillion: 1}},
		IgnoredModels: []string{"gpt-5.2-codex", "gpt-5.3-chat-latest"},
	}
	service := NewService(repo, Config{SessionTTL: time.Hour})

	saved, err := service.UpdateUsagePricing(context.Background(), UsagePricing{
		Version: 1, Currency: "USD", Unit: "1M_tokens",
		Models: map[string]UsagePrice{
			"local-model": {InputMicrousdPerMillion: 2},
			"gpt-5.3-chat-latest": {InputMicrousdPerMillion: 1_750_000},
		},
	})
	if err != nil {
		t.Fatalf("UpdateUsagePricing: %v", err)
	}
	if got, want := saved.IgnoredModels, []string{"gpt-5.2-codex"}; !slices.Equal(got, want) {
		t.Fatalf("ignored models = %v, want %v", got, want)
	}
}
```

Extend `TestAdminRepositoryUsagePricingSettings` to save and reload `IgnoredModels: []string{"gpt-5.3-chat-latest"}` and assert the exact round trip.

Add table cases proving blank, duplicate, and overlong ignored identifiers return `ErrInvalidInput`. Add one overlap case proving a model present in `Models` is omitted from normalized `IgnoredModels`, which is the explicit-restore invariant.

- [ ] **Step 2: Run the focused tests and verify RED**

Run from `backend/`:

```bash
GOMODCACHE=/root/Clouds/N2API/.cache/go-mod \
GOCACHE=/root/Clouds/N2API/.cache/go-build \
go test ./internal/admin ./internal/store -run 'TestNormalizeUsagePricing|TestUpdateUsagePricingPreservesIgnoredModels|TestAdminRepositoryUsagePricingSettings' -count=1
```

Expected: FAIL because `UsagePricing.IgnoredModels` does not exist and the update path cannot preserve or restore ignore state.

- [ ] **Step 3: Implement the durable field and normalization**

Extend the type:

```go
type UsagePricing struct {
	Version       int                   `json:"version"`
	Currency      string                `json:"currency"`
	Unit          string                `json:"unit"`
	UpdatedAt     time.Time             `json:"updatedAt"`
	Models        map[string]UsagePrice `json:"models"`
	IgnoredModels []string              `json:"ignoredModels,omitempty"`
}
```

In `normalizeUsagePricing`, validate ignored identifiers with the same model-name bounds, reject duplicates, omit identifiers already present in the normalized model map, and sort the result before returning it:

```go
	ignoredModels := make([]string, 0, len(pricing.IgnoredModels))
	seenIgnored := make(map[string]struct{}, len(pricing.IgnoredModels))
	for _, rawModel := range pricing.IgnoredModels {
		model := strings.TrimSpace(rawModel)
		if model == "" || len(model) > maxModelNameLen {
			return UsagePricing{}, ErrInvalidInput
		}
		if _, duplicate := seenIgnored[model]; duplicate {
			return UsagePricing{}, ErrInvalidInput
		}
		seenIgnored[model] = struct{}{}
		if _, restored := models[model]; !restored {
			ignoredModels = append(ignoredModels, model)
		}
	}
	sort.Strings(ignoredModels)
```

Return `IgnoredModels: ignoredModels` from normalization.

- [ ] **Step 4: Preserve server-owned ignores in ordinary updates**

Change `UpdateUsagePricing` to normalize the submitted models, load the current stored pricing, copy its ignored identifiers, and remove any exact identifier explicitly present in the submitted model map:

```go
func (s *Service) UpdateUsagePricing(ctx context.Context, pricing UsagePricing) (UsagePricing, error) {
	normalized, err := normalizeUsagePricing(pricing)
	if err != nil {
		return UsagePricing{}, err
	}
	current, err := s.GetUsagePricing(ctx)
	if err != nil {
		return UsagePricing{}, err
	}
	normalized.IgnoredModels = append([]string(nil), current.IgnoredModels...)
	for model := range normalized.Models {
		normalized.IgnoredModels = slices.DeleteFunc(normalized.IgnoredModels, func(ignored string) bool {
			return ignored == model
		})
	}
	normalized, err = normalizeUsagePricing(normalized)
	if err != nil {
		return UsagePricing{}, err
	}
	normalized.UpdatedAt = time.Now().UTC()
	return s.repo.SaveUsagePricing(ctx, normalized)
}
```

Add the standard-library `slices` import to `backend/internal/admin/service.go`.

- [ ] **Step 5: Run focused and package tests and verify GREEN**

```bash
GOMODCACHE=/root/Clouds/N2API/.cache/go-mod \
GOCACHE=/root/Clouds/N2API/.cache/go-build \
go test ./internal/admin ./internal/store -count=1
```

Expected: PASS.

- [ ] **Step 6: Commit the persistence unit**

```bash
git add backend/internal/admin/service.go backend/internal/admin/service_test.go backend/internal/store/admin_test.go
git commit -m "feat: persist ignored pricing models"
```

### Task 2: Make Lifecycle Sync Preserve And Skip Ignores

**Files:**
- Modify: `backend/internal/admin/service_test.go` (official sync and shutdown removal tests)
- Modify: `backend/internal/admin/official_pricing.go` (`SyncOfficialUsagePricing`, `RemoveShutdownUsagePricing`)

- [ ] **Step 1: Write failing lifecycle preservation tests**

Add a sync test with an ignored upcoming model present in the official fixtures:

```go
func TestSyncOfficialUsagePricingSkipsIgnoredModels(t *testing.T) {
	repo := newMemoryRepo()
	repo.usagePricing = UsagePricing{
		Version: 1, Currency: "USD", Unit: "1M_tokens",
		Models: map[string]UsagePrice{"local-model": {InputMicrousdPerMillion: 99}},
		IgnoredModels: []string{"gpt-5.3-chat-latest"},
	}
	fixtures := officialSyncFixtures()
	fixtures[officialPricingURL] = []byte(`<astro-island component-export="TextTokenPricingTables" props="{&quot;tier&quot;:[0,&quot;standard&quot;],&quot;rows&quot;:[1,[[1,[[0,&quot;gpt-5.3-chat-latest&quot;],[0,1.75],[0,0.175],[0,14]]]]]}"></astro-island>`)
	service := NewService(repo, Config{SessionTTL: time.Hour})
	service.SetOfficialDocumentFetcher(&fakeOfficialDocumentFetcher{bodies: fixtures})
	service.SetNow(func() time.Time { return time.Date(2026, 7, 10, 12, 0, 0, 0, time.UTC) })

	pricing, summary, err := service.SyncOfficialUsagePricing(context.Background())
	if err != nil {
		t.Fatalf("SyncOfficialUsagePricing: %v", err)
	}
	if _, exists := pricing.Models["gpt-5.3-chat-latest"]; exists {
		t.Fatal("ignored upcoming model was re-added")
	}
	if !slices.Equal(pricing.IgnoredModels, []string{"gpt-5.3-chat-latest"}) {
		t.Fatalf("ignored models = %v", pricing.IgnoredModels)
	}
	if len(summary.UpcomingShutdowns) != 0 || slices.Contains(summary.Added, "gpt-5.3-chat-latest") {
		t.Fatalf("summary contains ignored model: %+v", summary)
	}
}
```

Extend the successful shutdown-removal test with an unrelated ignored identifier and assert it survives the save unchanged.

- [ ] **Step 2: Run the lifecycle tests and verify RED**

```bash
GOMODCACHE=/root/Clouds/N2API/.cache/go-mod \
GOCACHE=/root/Clouds/N2API/.cache/go-build \
go test ./internal/admin -run 'TestSyncOfficialUsagePricingSkipsIgnoredModels|TestRemoveShutdownUsagePricingRemovesValidatedModels' -count=1
```

Expected: FAIL because official sync re-adds the ignored model and reconstruction drops `IgnoredModels`.

- [ ] **Step 3: Filter ignored identifiers during official merge**

Build an ignored set from current pricing and skip it before catalog, pricing, and summary accounting:

```go
	ignored := make(map[string]struct{}, len(current.IgnoredModels))
	for _, model := range current.IgnoredModels {
		ignored[model] = struct{}{}
	}
	for model, price := range officialPrices {
		if _, skip := ignored[model]; skip {
			continue
		}
		// existing catalog, lifecycle, added/updated, and merge logic
	}
```

Set `IgnoredModels: append([]string(nil), current.IgnoredModels...)` on the `UsagePricing` value constructed by official sync.

- [ ] **Step 4: Preserve ignores during confirmed shutdown removal**

Set the same copied ignore list on the `UsagePricing` value constructed in `RemoveShutdownUsagePricing` before normalization and saving:

```go
	normalized, err := normalizeUsagePricing(UsagePricing{
		Version: current.Version, Currency: current.Currency, Unit: current.Unit,
		UpdatedAt: now().UTC(), Models: mergedModels,
		IgnoredModels: append([]string(nil), current.IgnoredModels...),
	})
```

- [ ] **Step 5: Run package tests and verify GREEN**

```bash
GOMODCACHE=/root/Clouds/N2API/.cache/go-mod \
GOCACHE=/root/Clouds/N2API/.cache/go-build \
go test ./internal/admin -count=1
```

Expected: PASS.

- [ ] **Step 6: Commit lifecycle filtering**

```bash
git add backend/internal/admin/official_pricing.go backend/internal/admin/service_test.go
git commit -m "feat: skip ignored models during pricing sync"
```

### Task 3: Add Atomic Upcoming-Model Ignore Service

**Files:**
- Modify: `backend/internal/admin/official_pricing.go` (new service method and shared request validation helper)
- Modify: `backend/internal/admin/service_test.go` (ignore-upcoming service tests)

- [ ] **Step 1: Write failing service tests**

Add a success test that requests two future-shutdown local models, expects one save, sorted ignored identifiers, preserved unrelated models, and both requested identifiers removed.

```go
func TestIgnoreUpcomingUsagePricingRemovesAndPersistsModels(t *testing.T) {
	repo := newMemoryRepo()
	repo.usagePricing = UsagePricing{
		Version: 1, Currency: "USD", Unit: "1M_tokens",
		Models: map[string]UsagePrice{
			"gpt-5.3-chat-latest": {InputMicrousdPerMillion: 1_750_000},
			"gpt-5.2-codex": {InputMicrousdPerMillion: 1_500_000},
			"local-model": {InputMicrousdPerMillion: 99},
		},
	}
	fixtures := officialSyncFixtures()
	fixtures[officialDeprecationsURL] = []byte(`<table><tbody>
<tr><td>Aug 10, 2026</td><td><code>gpt-5.3-chat-latest</code></td><td><code>gpt-5.5</code></td></tr>
<tr><td>Sep 15, 2026</td><td><code>gpt-5.2-codex</code></td><td><code>gpt-5.3-codex</code></td></tr>
</tbody></table>`)
	service := NewService(repo, Config{SessionTTL: time.Hour})
	service.SetOfficialDocumentFetcher(&fakeOfficialDocumentFetcher{bodies: fixtures})
	service.SetNow(func() time.Time { return time.Date(2026, 7, 10, 12, 0, 0, 0, time.UTC) })

	pricing, ignored, err := service.IgnoreUpcomingUsagePricing(context.Background(), []string{"gpt-5.3-chat-latest", "gpt-5.2-codex"})
	if err != nil {
		t.Fatalf("IgnoreUpcomingUsagePricing: %v", err)
	}
	if got, want := ignored, []string{"gpt-5.2-codex", "gpt-5.3-chat-latest"}; !slices.Equal(got, want) {
		t.Fatalf("ignored = %v, want %v", got, want)
	}
	if _, exists := pricing.Models["gpt-5.3-chat-latest"]; exists {
		t.Fatal("upcoming model still present")
	}
	if _, exists := pricing.Models["local-model"]; !exists {
		t.Fatal("unrelated model was removed")
	}
	if repo.usagePricingSaveCount != 1 {
		t.Fatalf("save count = %d, want 1", repo.usagePricingSaveCount)
	}
}
```

Add table-driven invalid cases for blank, duplicate, unknown, not-local, and already-shut-down identifiers. Add an official-deprecations fetch failure case. Every failure must return an error and leave `usagePricingSaveCount == 0`.

- [ ] **Step 2: Run the focused test and verify RED**

```bash
GOMODCACHE=/root/Clouds/N2API/.cache/go-mod \
GOCACHE=/root/Clouds/N2API/.cache/go-build \
go test ./internal/admin -run 'TestIgnoreUpcomingUsagePricing' -count=1
```

Expected: compilation FAIL because `IgnoreUpcomingUsagePricing` does not exist.

- [ ] **Step 3: Implement full-batch validation before mutation**

Implement `IgnoreUpcomingUsagePricing` beside `RemoveShutdownUsagePricing`. It must normalize requested identifiers, fetch only the deprecations source, load current pricing, compare against the injected UTC date, and validate every model before copying or saving:

```go
func (s *Service) IgnoreUpcomingUsagePricing(ctx context.Context, models []string) (UsagePricing, []string, error) {
	requested, seen, err := normalizeRequestedPricingModels(models)
	if err != nil {
		return UsagePricing{}, nil, err
	}
	if s.officialDocumentFetcher == nil {
		s.officialDocumentFetcher = NewHTTPOfficialDocumentFetcher(30 * time.Second)
	}
	body, err := s.officialDocumentFetcher.Fetch(ctx, officialDeprecationsURL)
	if err != nil {
		return UsagePricing{}, nil, fmt.Errorf("fetch official deprecations: %w", err)
	}
	deprecations, err := parseOfficialDeprecations(string(body))
	if err != nil {
		return UsagePricing{}, nil, err
	}
	current, err := s.GetUsagePricing(ctx)
	if err != nil {
		return UsagePricing{}, nil, err
	}
	now := time.Now
	if s.now != nil {
		now = s.now
	}
	today := now().UTC().Format("2006-01-02")
	for _, model := range requested {
		if _, exists := current.Models[model]; !exists {
			return UsagePricing{}, nil, ErrInvalidInput
		}
		item, exists := deprecations[model]
		if !exists || item.ShutdownDate <= today {
			return UsagePricing{}, nil, ErrInvalidInput
		}
	}

	remaining := make(map[string]UsagePrice, len(current.Models)-len(requested))
	for model, price := range current.Models {
		if _, remove := seen[model]; !remove {
			remaining[model] = price
		}
	}
	ignored := append(append([]string(nil), current.IgnoredModels...), requested...)
	normalized, err := normalizeUsagePricing(UsagePricing{
		Version: current.Version, Currency: current.Currency, Unit: current.Unit,
		UpdatedAt: now().UTC(), Models: remaining, IgnoredModels: ignored,
	})
	if err != nil {
		return UsagePricing{}, nil, err
	}
	saved, err := s.repo.SaveUsagePricing(ctx, normalized)
	if err != nil {
		return UsagePricing{}, nil, fmt.Errorf("save usage pricing after upcoming ignore: %w", err)
	}
	sort.Strings(requested)
	return saved, requested, nil
}
```

Extract the identical request-list normalization from shutdown removal into `normalizeRequestedPricingModels(models) ([]string, map[string]struct{}, error)` and use it from both methods. The helper must reject an empty list, blanks, overlong identifiers, and duplicates.

- [ ] **Step 4: Run focused and package tests and verify GREEN**

```bash
GOMODCACHE=/root/Clouds/N2API/.cache/go-mod \
GOCACHE=/root/Clouds/N2API/.cache/go-build \
go test ./internal/admin -count=1
```

Expected: PASS.

- [ ] **Step 5: Commit the service operation**

```bash
git add backend/internal/admin/official_pricing.go backend/internal/admin/service_test.go
git commit -m "feat: ignore upcoming pricing models"
```

### Task 4: Expose The Authenticated Ignore Endpoint

**Files:**
- Modify: `backend/internal/httpapi/server.go` (`AdminService`, usage-pricing routes)
- Modify: `backend/internal/httpapi/server_test.go` (`fakeAdminService`, endpoint tests)

- [ ] **Step 1: Write failing HTTP tests**

Add authentication and success tests following the existing remove-shutdown tests:

```go
func TestAdminIgnoreUpcomingUsagePricingRequiresAuth(t *testing.T) {
	admins := newFakeAdminService()
	srv := NewServer(config.Config{}, staticHealth{}, admins, nil)
	req := httptest.NewRequest(http.MethodPost, "/api/admin/usage-pricing/ignore-upcoming", strings.NewReader(`{"models":["gpt-5.3-chat-latest"]}`))
	rec := httptest.NewRecorder()
	srv.ServeHTTP(rec, req)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusUnauthorized)
	}
}

func TestAdminIgnoreUpcomingUsagePricingReturnsIgnoredModels(t *testing.T) {
	admins := newFakeAdminService()
	admins.ignoreUpcomingPricing = admin.UsagePricing{
		Version: 1, Currency: "USD", Unit: "1M_tokens",
		Models: map[string]admin.UsagePrice{"local-model": {}},
		IgnoredModels: []string{"gpt-5.3-chat-latest"},
	}
	admins.ignoreUpcomingIgnored = []string{"gpt-5.3-chat-latest"}
	srv := NewServer(config.Config{}, staticHealth{}, admins, nil)
	req := httptest.NewRequest(http.MethodPost, "/api/admin/usage-pricing/ignore-upcoming", strings.NewReader(`{"models":["gpt-5.3-chat-latest"]}`))
	req.AddCookie(&http.Cookie{Name: adminSessionCookieName, Value: "valid-session"})
	rec := httptest.NewRecorder()
	srv.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d body=%s", rec.Code, rec.Body.String())
	}
	if got, want := admins.ignoreUpcomingModels, []string{"gpt-5.3-chat-latest"}; !slices.Equal(got, want) {
		t.Fatalf("models = %v, want %v", got, want)
	}
	var body struct {
		Pricing admin.UsagePricing `json:"pricing"`
		Ignored []string           `json:"ignored"`
	}
	if err := json.NewDecoder(rec.Body).Decode(&body); err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(body.Ignored, []string{"gpt-5.3-chat-latest"}) {
		t.Fatalf("ignored = %v", body.Ignored)
	}
}
```

Add these fields and method to the existing fake service:

```go
	ignoreUpcomingModels  []string
	ignoreUpcomingPricing admin.UsagePricing
	ignoreUpcomingIgnored []string
	ignoreUpcomingErr     error

func (s *fakeAdminService) IgnoreUpcomingUsagePricing(_ context.Context, models []string) (admin.UsagePricing, []string, error) {
	s.ignoreUpcomingModels = append([]string(nil), models...)
	return s.ignoreUpcomingPricing, s.ignoreUpcomingIgnored, s.ignoreUpcomingErr
}
```

Add `TestAdminIgnoreUpcomingUsagePricingMapsInvalidInputTo400` by setting `admins.ignoreUpcomingErr = admin.ErrInvalidInput`, authenticating with `adminSessionCookieName`, and expecting `http.StatusBadRequest`. Add `TestAdminIgnoreUpcomingUsagePricingRejectsMalformedJSON` with body `{"models":` and assert status `400` plus response error `bad_request`, matching the existing shutdown-removal test structure.

- [ ] **Step 2: Run the HTTP tests and verify RED**

```bash
GOMODCACHE=/root/Clouds/N2API/.cache/go-mod \
GOCACHE=/root/Clouds/N2API/.cache/go-build \
go test ./internal/httpapi -run 'TestAdminIgnoreUpcomingUsagePricing' -count=1
```

Expected: FAIL because the route and admin-service method do not exist.

- [ ] **Step 3: Extend the service interface and fake**

Add this method to `AdminService` and implement the matching fake method used by tests:

```go
IgnoreUpcomingUsagePricing(ctx context.Context, models []string) (admin.UsagePricing, []string, error)
```

- [ ] **Step 4: Register the endpoint**

Add the route next to `remove-shutdown`, reusing `decodeJSON`, `requireAdmin`, and existing error mappings:

```go
	mux.HandleFunc("POST /api/admin/usage-pricing/ignore-upcoming", requireAdmin(func(w http.ResponseWriter, r *http.Request, _ admin.Admin) {
		var req struct {
			Models []string `json:"models"`
		}
		if err := decodeJSON(w, r, &req); err != nil {
			writeError(w, http.StatusBadRequest, "bad_request")
			return
		}
		pricing, ignored, err := admins.IgnoreUpcomingUsagePricing(r.Context(), req.Models)
		if err != nil {
			if errors.Is(err, admin.ErrInvalidInput) {
				writeError(w, http.StatusBadRequest, "invalid_input")
				return
			}
			writeError(w, http.StatusInternalServerError, "internal_error")
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"pricing": pricing, "ignored": ignored})
	}))
```

- [ ] **Step 5: Run HTTP and full backend tests**

```bash
GOMODCACHE=/root/Clouds/N2API/.cache/go-mod \
GOCACHE=/root/Clouds/N2API/.cache/go-build \
go test ./internal/httpapi -count=1

GOMODCACHE=/root/Clouds/N2API/.cache/go-mod \
GOCACHE=/root/Clouds/N2API/.cache/go-build \
go test ./... -count=1
```

Expected: PASS.

- [ ] **Step 6: Commit the HTTP contract**

```bash
git add backend/internal/httpapi/server.go backend/internal/httpapi/server_test.go
git commit -m "feat: expose upcoming pricing ignore API"
```

### Task 5: Add Frontend Ignore State And Request Flow

**Files:**
- Modify: `frontend/src/lib/admin-state.svelte.js` (`usagePricing`, new request helper)
- Modify: `frontend/src/routes/navigation.test.mjs` (state-level source contracts)

- [ ] **Step 1: Write failing source-contract assertions**

Add assertions beside the existing pricing lifecycle tests:

```js
assert.match(adminState, /ignoringUpcoming:\s*false/);
assert.match(adminState, /export async function ignoreUpcomingUsagePricing\(models\)/);
assert.match(adminState, /\/api\/admin\/usage-pricing\/ignore-upcoming/);
assert.match(adminState, /ignoreUpcomingUsagePricing[\s\S]*?POST[\s\S]*?JSON\.stringify\(\{ models \}\)/);
assert.match(adminState, /ignoreUpcomingUsagePricing[\s\S]*?usagePricing\.upcomingShutdowns\s*=\s*usagePricing\.upcomingShutdowns\.filter/);
assert.match(adminState, /ignoreUpcomingUsagePricing[\s\S]*?await loadUsagePricing\(\)[\s\S]*?await loadUsageSummary/);
assert.match(adminState, /Ignored .* upcoming-shutdown model/);
```

- [ ] **Step 2: Run the focused frontend test and verify RED**

Run from `frontend/`:

```bash
bun test src/routes/navigation.test.mjs
```

Expected: FAIL because frontend ignore state and the request helper do not exist.

- [ ] **Step 3: Add state and the async helper**

Add `ignoringUpcoming: false` to `usagePricing` and its JSDoc type. Export:

```js
/** @param {string[]} models */
export async function ignoreUpcomingUsagePricing(models) {
  const version = sessionVersion;
  if (!isCurrentAuthenticated(version) || !Array.isArray(models) || models.length === 0) return false;

  usagePricing.ignoringUpcoming = true;
  usagePricing.error = '';
  usagePricing.saved = false;
  usagePricing.removalMessage = '';

  try {
    const payload = await requestJSON('/api/admin/usage-pricing/ignore-upcoming', {
      method: 'POST',
      body: JSON.stringify({ models })
    });
    if (!isCurrentAuthenticated(version)) return false;
    const ignored = Array.isArray(payload.ignored) ? payload.ignored : [];
    usagePricing.upcomingShutdowns = usagePricing.upcomingShutdowns.filter((item) => !ignored.includes(item.model));
    usagePricing.removalMessage = `Ignored ${ignored.length} upcoming-shutdown model${ignored.length === 1 ? '' : 's'}.`;
    await loadUsagePricing();
    await loadUsageSummary(usage.range, usage.groupBy);
    return true;
  } catch (error) {
    if (!isCurrentAuthenticated(version)) return false;
    usagePricing.error = error instanceof Error ? error.message : 'Failed to ignore upcoming-shutdown models';
    return false;
  } finally {
    if (!isCurrentAuthenticated(version)) return false;
    usagePricing.ignoringUpcoming = false;
  }
}
```

Reset `ignoringUpcoming` with the rest of pricing state during logout/session reset.

- [ ] **Step 4: Run the focused test and frontend check**

```bash
bun test src/routes/navigation.test.mjs
bun run check
```

Expected: PASS.

- [ ] **Step 5: Commit the frontend request unit**

```bash
git add frontend/src/lib/admin-state.svelte.js frontend/src/routes/navigation.test.mjs
git commit -m "feat: add upcoming pricing ignore state"
```

### Task 6: Replace The Persistent Notice With Scheme A Modal

**Files:**
- Modify: `frontend/src/routes/request-logs/+page.svelte` (header action, modal, handlers, loading state)
- Modify: `frontend/src/routes/navigation.test.mjs` (page-level source contracts)

- [ ] **Step 1: Write failing UI source-contract assertions**

Add assertions that lock the approved design without depending on generated HTML:

```js
assert.match(requestLogsPage, /import \{[^}]*TriangleAlert[^}]*\} from 'lucide-svelte'/);
assert.doesNotMatch(requestLogsPage, /<p class="font-medium">Upcoming shutdowns<\/p>/);
assert.match(requestLogsPage, /aria-label="Review upcoming model shutdowns"/);
assert.match(requestLogsPage, /title="Review upcoming model shutdowns"/);
assert.match(requestLogsPage, /usagePricing\.upcomingShutdowns\.length/);
assert.match(requestLogsPage, /Review upcoming model shutdowns[\s\S]*?Sync official/);
assert.match(requestLogsPage, /role="dialog"[\s\S]*?aria-modal="true"[\s\S]*?Upcoming model shutdowns/);
assert.match(requestLogsPage, /Remove \{usagePricing\.upcomingShutdowns\.length\} models/);
assert.match(requestLogsPage, /confirmUpcomingIgnore[\s\S]*?ignoreUpcomingUsagePricing/);
assert.doesNotMatch(requestLogsPage, /showUpcomingIgnoreModal[\s\S]*?type="checkbox"/);
assert.match(requestLogsPage, /pricingBusy[\s\S]*?usagePricing\.ignoringUpcoming/);
```

- [ ] **Step 2: Run the focused test and verify RED**

```bash
bun test src/routes/navigation.test.mjs
```

Expected: FAIL because the warning band still exists and the Scheme A icon/modal do not.

- [ ] **Step 3: Add state and handlers using Svelte 5 runes**

Import `ignoreUpcomingUsagePricing` and `TriangleAlert`. Add:

```svelte
let showUpcomingIgnoreModal = $state(false);

function openUpcomingIgnoreModal() {
  showUpcomingIgnoreModal = true;
}

function closeUpcomingIgnoreModal() {
  if (!pricingBusy) showUpcomingIgnoreModal = false;
}

async function confirmUpcomingIgnore() {
  const models = (usagePricing.upcomingShutdowns || []).map((item) => item.model);
  if (await ignoreUpcomingUsagePricing(models)) {
    showUpcomingIgnoreModal = false;
  }
}

function handlePricingModalKeydown(event) {
  if (event.key === 'Escape' && showUpcomingIgnoreModal && !pricingBusy) {
    showUpcomingIgnoreModal = false;
  }
}
```

Extend the existing derived busy state:

```js
const pricingBusy = $derived(
  usagePricing.loading || usagePricing.saving || usagePricing.syncing ||
  usagePricing.removingShutdown || usagePricing.ignoringUpcoming
);
```

Attach `onkeydown={handlePricingModalKeydown}` to the existing `<svelte:window>` declaration alongside scroll and resize handlers.

- [ ] **Step 4: Render the Scheme A warning icon before Sync official**

Inside the right-aligned action group and before the `Sync official` button, render only when upcoming entries exist:

```svelte
{#if usagePricing.upcomingShutdowns?.length}
  <button
    class="relative grid h-9 w-9 shrink-0 place-items-center rounded-lg border border-amber-200 bg-amber-50 text-amber-800 hover:bg-amber-100 disabled:cursor-not-allowed disabled:opacity-60"
    type="button"
    aria-label="Review upcoming model shutdowns"
    title="Review upcoming model shutdowns"
    disabled={pricingBusy}
    onclick={openUpcomingIgnoreModal}
  >
    <TriangleAlert class="h-4 w-4" aria-hidden="true" />
    <span class="absolute -right-1.5 -top-1.5 grid min-h-4 min-w-4 place-items-center rounded-full border-2 border-white bg-amber-700 px-1 text-[9px] font-semibold leading-none text-white">
      {usagePricing.upcomingShutdowns.length}
    </span>
  </button>
{/if}
```

Delete the existing full-width `{#if usagePricing.upcomingShutdowns?.length}` amber notice below the saved/error feedback.

- [ ] **Step 5: Render the accessible one-action modal**

Place the modal at page level with the existing overlay conventions:

```svelte
{#if showUpcomingIgnoreModal}
  <div
    class="fixed inset-0 z-50 flex items-center justify-center bg-black/40 p-4"
    role="dialog"
    aria-modal="true"
    aria-labelledby="upcoming-ignore-title"
    tabindex="-1"
    onclick={(event) => event.target === event.currentTarget && closeUpcomingIgnoreModal()}
    onkeydown={handlePricingModalKeydown}
  >
    <div class="grid w-full max-w-lg gap-4 rounded-lg bg-white p-5 shadow-xl">
      <div class="flex items-start gap-3">
        <div class="grid h-9 w-9 shrink-0 place-items-center rounded-lg bg-amber-50 text-amber-800">
          <TriangleAlert class="h-5 w-5" aria-hidden="true" />
        </div>
        <div>
          <h3 id="upcoming-ignore-title" class="text-lg font-semibold text-[#0d0d0d]">Upcoming model shutdowns</h3>
          <p class="mt-1 text-sm text-[#6e6e6e]">Remove and keep these models out of future official pricing syncs.</p>
        </div>
      </div>
      <div class="max-h-72 overflow-y-auto rounded-lg border border-[#ededed]">
        {#each usagePricing.upcomingShutdowns as item (item.model)}
          <div class="border-t border-[#ededed] px-3 py-3 first:border-t-0">
            <p class="font-mono text-[13px] font-medium text-[#0d0d0d]">{item.model}</p>
            <p class="mt-1 text-xs text-[#6e6e6e]">
              Shutdown {item.shutdownDate}{item.replacement ? ` · Use ${item.replacement}` : ''}
            </p>
          </div>
        {/each}
      </div>
      <div class="flex justify-end gap-2">
        <button class="rounded-lg border border-[#e5e5e5] bg-white px-3 py-2 text-sm font-medium text-[#0d0d0d] hover:bg-[#f5f5f5] disabled:opacity-60" type="button" disabled={pricingBusy} onclick={closeUpcomingIgnoreModal}>Cancel</button>
        <button class="rounded-lg bg-[#ef4146] px-3 py-2 text-sm font-medium text-white hover:bg-[#d7373c] disabled:opacity-60" type="button" disabled={pricingBusy || !usagePricing.upcomingShutdowns.length} onclick={confirmUpcomingIgnore}>
          Remove {usagePricing.upcomingShutdowns.length} models
        </button>
      </div>
    </div>
  </div>
{/if}
```

Keep the approved fixed `Remove {count} models` wording and all other visible copy in English to match the existing admin UI.

- [ ] **Step 6: Run focused tests, all frontend tests, check, and build**

```bash
bun test src/routes/navigation.test.mjs
bun test
bun run check
bun run build
```

Expected: all commands PASS. `svelte-check` reports zero errors and zero warnings.

- [ ] **Step 7: Commit the approved UI**

```bash
git add frontend/src/routes/request-logs/+page.svelte frontend/src/routes/navigation.test.mjs
git commit -m "feat: move pricing lifecycle warning into modal"
```

### Task 7: Integration Review, Full Verification, And Local Refresh

**Files:**
- Review only: all files changed by Tasks 1-6
- Runtime refresh: `deploy/compose.yaml`

- [ ] **Step 1: Inspect commit and worktree scope**

```bash
git status --short
git log --oneline -8
git diff 3c55c9a..HEAD -- backend frontend
```

Expected: only the planned backend/frontend files changed, each coherent unit is committed, and no generated build output is tracked.

- [ ] **Step 2: Run final backend verification**

From `backend/`:

```bash
GOMODCACHE=/root/Clouds/N2API/.cache/go-mod \
GOCACHE=/root/Clouds/N2API/.cache/go-build \
go test ./... -count=1
```

Expected: PASS.

- [ ] **Step 3: Run final frontend verification**

From `frontend/`:

```bash
bun test
bun run check
bun run build
```

Expected: PASS; `svelte-check` reports zero errors and zero warnings.

- [ ] **Step 4: Rebuild and recreate the local Compose stack**

Follow the `n2api-refresh-docker` skill exactly. The expected project commands are:

```bash
docker compose -f deploy/compose.yaml build --no-cache
docker compose -f deploy/compose.yaml up -d --force-recreate
docker compose -f deploy/compose.yaml ps
```

Expected: `deploy-n2api-1` is running and port `3000` is published on IPv4 and IPv6.

- [ ] **Step 5: Run container-local smoke checks**

```bash
docker exec deploy-n2api-1 wget -qO- http://127.0.0.1:3000/healthz
docker exec deploy-n2api-1 wget -qO- http://127.0.0.1:3000/request-logs >/dev/null
docker exec deploy-n2api-1 wget -qO- http://127.0.0.1:3000/api/public/status
```

Expected: health/status responses succeed and `/request-logs` returns HTML.

- [ ] **Step 6: Confirm final repository state**

```bash
git status --short
git log --oneline -8
```

Expected: clean worktree and the plan's atomic Conventional Commits present. If remote publication is authorized for the execution session, push `main` only after all verification and smoke checks pass.

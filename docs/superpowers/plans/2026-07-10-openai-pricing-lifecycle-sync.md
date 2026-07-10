# OpenAI Pricing Lifecycle Sync Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Synchronize compatible OpenAI prices additively from three official sources, warn about upcoming shutdowns, and remove already shut-down local models only after a user-selected bulk confirmation.

**Architecture:** Keep `UsagePricing` as the only durable pricing record. Expand the admin official-pricing module with source-specific fetchers and parsers, merge official compatible prices into existing rows, and return ephemeral lifecycle notices. Add a separate removal operation that refetches deprecations and validates every requested model before one atomic pricing save.

**Tech Stack:** Go 1.24 backend, PostgreSQL settings repository, SvelteKit/Svelte 5 frontend, Bun tests, Tailwind CSS, Docker Compose.

---

## File Structure

- Modify `backend/internal/admin/official_pricing.go`: source URLs, fetch abstraction, catalog/pricing/deprecation parsers, merge summary, clock injection, and shutdown removal service method.
- Modify `backend/internal/admin/service.go`: replace the single pricing fetcher dependency with the official-document fetcher and current-time dependency.
- Modify `backend/internal/admin/service_test.go`: parser, merge, atomic failure, date-boundary, and deletion service tests.
- Modify `backend/internal/httpapi/server.go`: extend `AdminService` and register the shutdown-removal endpoint.
- Modify `backend/internal/httpapi/server_test.go`: response shape, request validation, auth, and error-mapping tests.
- Modify `frontend/src/lib/admin-state.svelte.js`: lifecycle state, additive sync result handling, and confirmed shutdown removal request.
- Modify `frontend/src/routes/request-logs/+page.svelte`: additive sync copy, upcoming notice, and selectable bulk removal modal.
- Modify `frontend/src/routes/navigation.test.mjs`: source contracts for lifecycle state and UI behavior.

## Task 1: Parse Current Official Document Shapes

**Files:**
- Modify: `backend/internal/admin/official_pricing.go`
- Test: `backend/internal/admin/service_test.go`

- [ ] **Step 1: Write failing parser tests**

Add tests with compact fixtures that prove:

```go
func TestParseOfficialModelCatalogIncludesDeprecatedMarker(t *testing.T) {
	body := `<a href="/api/docs/models/gpt-5.6-sol"><div>GPT-5.6 Sol</div></a>
<a href="/api/docs/models/gpt-5.3-chat-latest"><div>GPT-5.3 Chat</div><div>Deprecated</div></a>`

	models, err := parseOfficialModelCatalog(body)
	if err != nil { t.Fatal(err) }
	if models["gpt-5.6-sol"].Deprecated { t.Fatal("gpt-5.6-sol unexpectedly deprecated") }
	if !models["gpt-5.3-chat-latest"].Deprecated { t.Fatal("missing deprecated marker") }
}

func TestParseOfficialStandardPricingSupportsCacheWritesColumns(t *testing.T) {
	body := `<astro-island component-export="TextTokenPricingTables" props="{&quot;tier&quot;:[0,&quot;standard&quot;],&quot;rows&quot;:[1,[[1,[[0,&quot;gpt-5.6-sol&quot;],[0,5],[0,0.5],[0,6.25],[0,30]]]]]}"></astro-island>`

	models, err := parseOfficialStandardPricing(body)
	if err != nil { t.Fatal(err) }
	if got := models["gpt-5.6-sol"].OutputMicrousdPerMillion; got != 30_000_000 {
		t.Fatalf("output = %d", got)
	}
}

func TestParseOfficialDeprecationsNormalizesDates(t *testing.T) {
	body := `<table><thead><tr><th>Shutdown date</th><th>Model / system</th><th>Recommended replacement</th></tr></thead><tbody>
<tr><td>Aug 10, 2026</td><td><code>gpt-5.3-chat-latest</code></td><td><code>gpt-5.5</code></td></tr>
<tr><td>2026‑03‑26</td><td><code>gpt-4-0314</code></td><td><code>gpt-5</code></td></tr>
</tbody></table>`

	items, err := parseOfficialDeprecations(body)
	if err != nil { t.Fatal(err) }
	if items["gpt-5.3-chat-latest"].ShutdownDate != "2026-08-10" { t.Fatal("wrong date") }
	if items["gpt-4-0314"].ShutdownDate != "2026-03-26" { t.Fatal("wrong unicode date") }
}
```

Also add a nine-`td` SSR fixture and assert cache-write cells are skipped while short/long output values are preserved.

- [ ] **Step 2: Run focused tests and verify RED**

Run from `backend/`:

```bash
GOMODCACHE=/root/Clouds/N2API/.cache/go-mod \
GOCACHE=/root/Clouds/N2API/.cache/go-build \
go test ./internal/admin -run 'TestParseOfficial(ModelCatalog|StandardPricingSupportsCacheWrites|Deprecations)' -count=1 -v
```

Expected: compilation fails because the catalog and deprecation types/functions do not exist, and the five-value pricing fixture is not supported.

- [ ] **Step 3: Implement source-specific parsers**

In `official_pricing.go`:

```go
const (
	officialModelsURL       = "https://developers.openai.com/api/docs/models/all"
	officialPricingURL      = "https://developers.openai.com/api/docs/pricing"
	officialDeprecationsURL = "https://developers.openai.com/api/docs/deprecations"
)

type OfficialModel struct {
	Deprecated bool
}

type ModelDeprecation struct {
	Model        string `json:"model"`
	ShutdownDate string `json:"shutdownDate"`
	Replacement  string `json:"replacement"`
}
```

Implement:

- `parseOfficialModelCatalog(string) (map[string]OfficialModel, error)` using exact `/api/docs/models/<id>` hrefs and the enclosing card's `Deprecated` marker.
- pricing row matching for four- and five-value Astro rows, choosing output index `3` or `4` respectively.
- SSR short/long matching for seven or nine cells, choosing `[1,2,3]` / `[4,5,6]` or `[1,2,4]` / `[5,6,8]` respectively.
- `parseOfficialDeprecations(string) (map[string]ModelDeprecation, error)` by walking table rows whose first header is `Shutdown date`, extracting exact `<code>` model ids, parsing `2006-01-02`, `Jan 2, 2006`, and `January 2, 2006` after normalizing Unicode hyphens.
- validation errors when any parser produces no compatible records.

- [ ] **Step 4: Run focused and existing parser tests and verify GREEN**

```bash
GOMODCACHE=/root/Clouds/N2API/.cache/go-mod \
GOCACHE=/root/Clouds/N2API/.cache/go-build \
go test ./internal/admin -run 'TestParseOfficial' -count=1 -v
```

Expected: all parser tests pass, including existing Standard-tier contamination tests.

- [ ] **Step 5: Commit the parser change**

```bash
git add backend/internal/admin/official_pricing.go backend/internal/admin/service_test.go
git commit -m "fix: parse current openai pricing documents"
```

## Task 2: Add Three-Source Additive Pricing Sync

**Files:**
- Modify: `backend/internal/admin/official_pricing.go`
- Modify: `backend/internal/admin/service.go`
- Test: `backend/internal/admin/service_test.go`

- [ ] **Step 1: Write failing additive-sync tests**

Replace the single fake pricing fetcher with a keyed fake document fetcher and add tests that start with:

```go
repo.usagePricing = UsagePricing{
	Version: 1, Currency: "USD", Unit: "1M_tokens",
	Models: map[string]UsagePrice{
		"gpt-5.5":    {InputMicrousdPerMillion: 1},
		"local-model": {InputMicrousdPerMillion: 99},
		"gpt-4-0314": {InputMicrousdPerMillion: 30_000_000},
	},
}
```

Prove that a sync at `2026-07-10T12:00:00Z`:

- updates `gpt-5.5`
- adds `gpt-5.6-sol`
- preserves `local-model`
- preserves shut-down `gpt-4-0314` until explicit removal
- classifies a future `2026-08-10` row as upcoming
- classifies a `2026-03-26` row as a deletion candidate
- returns sorted added/updated/notices for deterministic JSON

Add three failure-atomicity table cases where models, pricing, or deprecations content is invalid. Assert the repository's pricing remains byte-for-byte equivalent and its save counter remains zero.

- [ ] **Step 2: Run sync tests and verify RED**

```bash
GOMODCACHE=/root/Clouds/N2API/.cache/go-mod \
GOCACHE=/root/Clouds/N2API/.cache/go-build \
go test ./internal/admin -run 'TestSyncOfficialUsagePricing(Additive|SourceFailure)' -count=1 -v
```

Expected: tests fail because sync still fetches one document and replaces the entire table.

- [ ] **Step 3: Implement document fetching and merge summary**

Define:

```go
type OfficialDocumentFetcher interface {
	Fetch(ctx context.Context, url string) ([]byte, error)
}

type UsagePricingSyncSources struct {
	Models       string `json:"models"`
	Pricing      string `json:"pricing"`
	Deprecations string `json:"deprecations"`
}

type UsagePricingSyncSummary struct {
	Total                int                `json:"total"`
	Added                []string           `json:"added"`
	Updated              []string           `json:"updated"`
	Unchanged            int                `json:"unchanged"`
	UpcomingShutdowns    []ModelDeprecation `json:"upcomingShutdowns"`
	DeletionCandidates   []ModelDeprecation `json:"deletionCandidates"`
	Sources              UsagePricingSyncSources `json:"sources"`
}
```

Update `Service` to hold `officialDocumentFetcher` and `now func() time.Time`. Preserve test injection through setters. `SyncOfficialUsagePricing` must fetch and parse all sources before calling `GetUsagePricing`, copy the local model map, merge parsed prices, normalize once, save once, and classify only exact local model ids from the deprecations map using `now().UTC()` truncated to a calendar date.

- [ ] **Step 4: Run focused and full admin tests**

```bash
GOMODCACHE=/root/Clouds/N2API/.cache/go-mod \
GOCACHE=/root/Clouds/N2API/.cache/go-build \
go test ./internal/admin -count=1
```

Expected: PASS.

- [ ] **Step 5: Commit additive sync**

```bash
git add backend/internal/admin/official_pricing.go backend/internal/admin/service.go backend/internal/admin/service_test.go
git commit -m "feat: merge official usage pricing"
```

## Task 3: Add Revalidated Shutdown Removal API

**Files:**
- Modify: `backend/internal/admin/official_pricing.go`
- Modify: `backend/internal/httpapi/server.go`
- Test: `backend/internal/admin/service_test.go`
- Test: `backend/internal/httpapi/server_test.go`

- [ ] **Step 1: Write failing service and HTTP tests**

Add service tests for `RemoveShutdownUsagePricing(ctx, []string)`:

- all requested models exist locally and have shutdown dates on/before today: remove them and save once
- one requested model has a future date: return `ErrInvalidInput`, remove nothing, save zero times
- duplicate, blank, unknown, or absent-local model: return `ErrInvalidInput`, save zero times
- deprecations fetch/parse failure: return an error, save zero times

Add HTTP tests proving:

```http
POST /api/admin/usage-pricing/remove-shutdown
{"models":["gpt-4-0314"]}
```

requires admin auth, returns `{ "pricing": ..., "removed": ["gpt-4-0314"] }`, maps validation to `400 invalid_input`, and malformed JSON to `400 bad_request`.

- [ ] **Step 2: Run service and HTTP tests and verify RED**

```bash
GOMODCACHE=/root/Clouds/N2API/.cache/go-mod \
GOCACHE=/root/Clouds/N2API/.cache/go-build \
go test ./internal/admin ./internal/httpapi -run 'Test(RemoveShutdown|AdminRemoveShutdown)' -count=1 -v
```

Expected: compilation fails because the service and interface methods do not exist.

- [ ] **Step 3: Implement all-or-nothing removal**

Add:

```go
func (s *Service) RemoveShutdownUsagePricing(ctx context.Context, models []string) (UsagePricing, []string, error)
```

Validate request shape first, refetch only the deprecations document, parse it, validate every selected exact model against the current UTC date and current local table, then copy the model map, delete all selected ids, normalize, and save exactly once. Sort the removed list.

Extend `httpapi.AdminService` and register `POST /api/admin/usage-pricing/remove-shutdown` using the existing `decodeJSON`, `requireAdmin`, `writeError`, and `writeJSON` patterns.

- [ ] **Step 4: Run focused and full backend tests**

```bash
GOMODCACHE=/root/Clouds/N2API/.cache/go-mod \
GOCACHE=/root/Clouds/N2API/.cache/go-build \
go test ./... -count=1
```

Expected: PASS.

- [ ] **Step 5: Commit shutdown removal API**

```bash
git add backend/internal/admin/official_pricing.go backend/internal/admin/service_test.go backend/internal/httpapi/server.go backend/internal/httpapi/server_test.go
git commit -m "feat: confirm shutdown pricing removal"
```

## Task 4: Add Lifecycle State And Bulk Confirmation UI

**Files:**
- Modify: `frontend/src/lib/admin-state.svelte.js`
- Modify: `frontend/src/routes/request-logs/+page.svelte`
- Test: `frontend/src/routes/navigation.test.mjs`

- [ ] **Step 1: Write failing frontend source-contract tests**

Extend `usage pricing supports official OpenAI sync` or add a focused test asserting:

- admin state contains `upcomingShutdowns`, `deletionCandidates`, `removingShutdown`, and `removeShutdownUsagePricing`
- removal posts to `/api/admin/usage-pricing/remove-shutdown` with selected model ids and reloads pricing on success
- sync notification includes added and updated counts
- sync confirmation says local-only rows remain and no longer says `replaces all current pricing rows`
- the pricing page renders upcoming shutdowns without removal buttons in that notice
- the bulk modal uses checkboxes, defaults selected ids from candidates, allows selection changes, and displays `Remove {selectedShutdownModels.length} models`
- `pricingBusy` includes `usagePricing.removingShutdown`

- [ ] **Step 2: Run frontend test and verify RED**

Run from `frontend/`:

```bash
bun test src/routes/navigation.test.mjs
```

Expected: FAIL on missing lifecycle state, removal endpoint, additive copy, and modal contracts.

- [ ] **Step 3: Implement admin lifecycle state**

Add to `usagePricing`:

```js
upcomingShutdowns: [],
deletionCandidates: [],
removingShutdown: false,
removalMessage: ''
```

On successful sync, assign normalized notice arrays and set:

```js
usagePricing.syncMessage = `Official pricing synced: ${added} added, ${updated} updated.`;
```

Implement:

```js
export async function removeShutdownUsagePricing(models) {
	// POST selected ids, retain lifecycle state on failure,
	// clear removed candidates and reload pricing on success.
}
```

- [ ] **Step 4: Implement compact warning and selectable modal**

In `+page.svelte`:

- import the removal helper
- include removal in `pricingBusy`
- update confirmation copy and show links for all three official sources
- render one amber upcoming-shutdown band below errors/saved feedback
- open the removal modal automatically after a successful sync with candidates
- initialize a `Set`-equivalent plain object or string array with every candidate id selected
- render checkbox rows with model, shutdown date, and replacement
- disable the destructive button when zero candidates are selected
- retain selection and modal on failure; close and notify on success

Use the existing restrained operational styling: `rounded-lg` or smaller, no nested cards, stable max height with scroll for long candidate lists, and no full-page instructional prose.

- [ ] **Step 5: Run frontend verification**

```bash
bun test
bun run check
bun run build
```

Expected: all commands pass with zero Svelte errors and warnings.

- [ ] **Step 6: Commit frontend lifecycle UI**

```bash
git add frontend/src/lib/admin-state.svelte.js frontend/src/routes/request-logs/+page.svelte frontend/src/routes/navigation.test.mjs
git commit -m "feat: confirm shutdown models after pricing sync"
```

## Task 5: Live Parser Validation And Operational Closeout

**Files:**
- Temporary test only: `backend/internal/admin/live_pricing_lifecycle_diagnostic_test.go` (remove before commit)

- [ ] **Step 1: Download all three current official pages**

```bash
curl -fsSL https://developers.openai.com/api/docs/models/all -o /tmp/n2api-openai-models-all.html
curl -fsSL https://developers.openai.com/api/docs/pricing -o /tmp/n2api-openai-pricing.html
curl -fsSL https://developers.openai.com/api/docs/deprecations -o /tmp/n2api-openai-deprecations.html
```

Expected: all commands exit `0` and each file is non-empty.

- [ ] **Step 2: Validate live documents without persistence**

Add a temporary package-local test that reads the three `/tmp` files, calls each production parser, and asserts representative current records:

```go
if _, ok := catalog["gpt-5.6-sol"]; !ok { t.Fatal("missing gpt-5.6-sol") }
if got := prices["gpt-5.6-sol"].OutputMicrousdPerMillion; got != 30_000_000 { t.Fatal(got) }
if _, ok := deprecations["gpt-5.3-chat-latest"]; !ok { t.Fatal("missing deprecation") }
```

Run it once with `go test ./internal/admin -run TestLivePricingLifecycleDiagnostic -count=1 -v`, record parsed counts, then delete the temporary file with `apply_patch` and confirm `git status` has no diagnostic artifact.

- [ ] **Step 3: Run final verification from fresh command output**

Backend:

```bash
cd /root/Clouds/N2API/backend
GOMODCACHE=/root/Clouds/N2API/.cache/go-mod \
GOCACHE=/root/Clouds/N2API/.cache/go-build \
go test ./... -count=1
```

Frontend:

```bash
cd /root/Clouds/N2API/frontend
bun test
bun run check
bun run build
```

Expected: every command passes.

- [ ] **Step 4: Review repository state and push commits**

```bash
git diff --check
git status --short
git log -6 --oneline
git push origin main
```

Expected: no uncommitted source changes, four coherent implementation commits after the design/plan commits, and push succeeds.

- [ ] **Step 5: Refresh Docker Compose using the project skill**

```bash
docker compose -f deploy/compose.yaml build --no-cache
docker compose -f deploy/compose.yaml up -d --force-recreate
docker compose -f deploy/compose.yaml ps
```

Expected: `deploy-n2api-1` and `deploy-postgres-1` are up; the app is bound to `0.0.0.0:3000` and `[::]:3000`.

- [ ] **Step 6: Smoke test the rebuilt container**

```bash
docker exec deploy-n2api-1 wget -qO- http://127.0.0.1:3000/api/public/status
docker exec deploy-n2api-1 wget -qO- http://127.0.0.1:3000/healthz
```

Expected: public status returns the N2API status payload and health returns success. Report the remote test URL as `http://oc-de-fra-1.knowsky.uk:3000`.

# Token Usage Accounting Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Record OpenAI-style token usage and estimated USD cost for N2API gateway requests, then expose admin summaries by local API key, upstream OAuth account, and model.

**Architecture:** Extend the existing request-log path instead of adding a separate ledger. The gateway observes upstream response bodies while preserving passthrough and streaming, maps OpenAI usage fields into a focused `gateway.Usage` value, computes cost from editable pricing settings, and writes all accounting fields into `request_logs`. Admin API and UI aggregate those durable request-log fields.

**Tech Stack:** Go backend with `net/http`, PostgreSQL migrations through embedded Goose, pgx repositories, SvelteKit admin UI, Tailwind CSS, Bun scripts.

---

## File Structure

- Create `backend/internal/gateway/usage.go`
  - Owns usage data types, OpenAI Chat Completions/Responses usage parsing, SSE event observation, and request-model extraction.
- Create `backend/internal/gateway/usage_test.go`
  - Unit tests for usage parsing and streaming observation without involving the HTTP proxy.
- Create `backend/internal/admin/usage.go`
  - Owns pricing config types, validation, cost calculation, usage summaries, and formatting-neutral admin DTOs.
- Create `backend/internal/admin/usage_test.go`
  - Unit tests for pricing validation and integer cost calculation.
- Create `backend/internal/store/migrations/00007_request_usage_accounting.sql`
  - Adds request-log accounting columns and indexes.
- Modify `backend/internal/store/migrations_test.go`
  - Asserts migration 00007 is embedded and updates migration source count.
- Modify `backend/internal/gateway/proxy.go`
  - Adds selected OAuth account id and observed usage to `RequestLog`.
  - Wraps upstream body copy with non-stream and SSE observers.
- Modify `backend/internal/gateway/proxy_test.go`
  - Adds proxy-level tests proving usage is logged and streaming stays intact.
- Modify `backend/internal/store/gateway.go`
  - Inserts new accounting fields into `request_logs`.
- Modify `backend/internal/store/gateway_test.go`
  - Extends compile-time interface assertions for usage logging.
- Modify `backend/internal/store/admin.go`
  - Reads enriched request logs, pricing settings, and usage summary aggregates.
- Modify `backend/internal/admin/service.go`
  - Adds service methods for usage summary and pricing settings.
- Modify `backend/internal/admin/service_test.go`
  - Extends memory repo and service tests for usage summary, pricing defaults, and validation.
- Modify `backend/internal/httpapi/server.go`
  - Adds protected usage summary and pricing endpoints.
- Modify `backend/internal/httpapi/server_test.go`
  - Adds endpoint tests for usage summary, invalid query values, and pricing update validation.
- Modify `frontend/src/lib/admin-state.svelte.js`
  - Adds usage/pricing state, loaders, save action, and token/cost formatters.
- Create `frontend/src/lib/usage-format.test.mjs`
  - Tests token and USD formatting helpers.
- Modify `frontend/src/routes/request-logs/+page.svelte`
  - Adds summary cards, group selector, grouped table, usage columns, and pricing editor.
- Modify `frontend/src/routes/navigation.test.mjs`
  - Keeps route expectations current if labels change.
- Optionally modify `deploy/README.md`
  - Add an operational note only if implementation changes deployment or migration guidance beyond normal DB migration.

## Task 1: Database Migration and Backend Types

**Files:**
- Create: `backend/internal/store/migrations/00007_request_usage_accounting.sql`
- Modify: `backend/internal/store/migrations_test.go`
- Modify: `backend/internal/gateway/proxy.go`
- Modify: `backend/internal/admin/service.go`

- [ ] **Step 1: Write failing migration tests**

Add `TestRequestUsageAccountingMigrationIsEmbedded` to `backend/internal/store/migrations_test.go`:

```go
func TestRequestUsageAccountingMigrationIsEmbedded(t *testing.T) {
	sql, err := MigrationSQL("00007_request_usage_accounting.sql")
	if err != nil {
		t.Fatalf("MigrationSQL returned error: %v", err)
	}
	for _, want := range []string{
		"ALTER TABLE request_logs ADD COLUMN IF NOT EXISTS oauth_account_id BIGINT REFERENCES oauth_accounts(id) ON DELETE SET NULL",
		"ALTER TABLE request_logs ADD COLUMN IF NOT EXISTS model TEXT NOT NULL DEFAULT ''",
		"ALTER TABLE request_logs ADD COLUMN IF NOT EXISTS input_tokens INTEGER NOT NULL DEFAULT 0",
		"ALTER TABLE request_logs ADD COLUMN IF NOT EXISTS output_tokens INTEGER NOT NULL DEFAULT 0",
		"ALTER TABLE request_logs ADD COLUMN IF NOT EXISTS total_tokens INTEGER NOT NULL DEFAULT 0",
		"ALTER TABLE request_logs ADD COLUMN IF NOT EXISTS cached_input_tokens INTEGER NOT NULL DEFAULT 0",
		"ALTER TABLE request_logs ADD COLUMN IF NOT EXISTS reasoning_tokens INTEGER NOT NULL DEFAULT 0",
		"ALTER TABLE request_logs ADD COLUMN IF NOT EXISTS estimated_cost_microusd BIGINT NOT NULL DEFAULT 0",
		"ALTER TABLE request_logs ADD COLUMN IF NOT EXISTS pricing_snapshot JSONB NOT NULL DEFAULT '{}'::jsonb",
		"ALTER TABLE request_logs ADD COLUMN IF NOT EXISTS usage_source TEXT NOT NULL DEFAULT 'missing'",
		"request_logs_client_key_usage_idx",
		"request_logs_oauth_account_usage_idx",
		"request_logs_model_usage_idx",
	} {
		if !strings.Contains(sql, want) {
			t.Fatalf("migration missing %q", want)
		}
	}
}
```

Update `TestMigrationProviderSeesEmbeddedMigrations` expected source count from `6` to `7` and last path from `00006_codex_account_pool_state.sql` to `00007_request_usage_accounting.sql`.

- [ ] **Step 2: Run migration test to verify it fails**

Run:

```bash
cd backend
GOMODCACHE=/root/Clouds/N2API/.cache/go-mod GOCACHE=/root/Clouds/N2API/.cache/go-build go test ./internal/store -run 'TestRequestUsageAccountingMigrationIsEmbedded|TestMigrationProviderSeesEmbeddedMigrations'
```

Expected: FAIL because `00007_request_usage_accounting.sql` does not exist and source count is still six.

- [ ] **Step 3: Add migration**

Create `backend/internal/store/migrations/00007_request_usage_accounting.sql`:

```sql
-- +goose Up
-- +goose StatementBegin
ALTER TABLE request_logs ADD COLUMN IF NOT EXISTS oauth_account_id BIGINT REFERENCES oauth_accounts(id) ON DELETE SET NULL;
ALTER TABLE request_logs ADD COLUMN IF NOT EXISTS model TEXT NOT NULL DEFAULT '';
ALTER TABLE request_logs ADD COLUMN IF NOT EXISTS input_tokens INTEGER NOT NULL DEFAULT 0;
ALTER TABLE request_logs ADD COLUMN IF NOT EXISTS output_tokens INTEGER NOT NULL DEFAULT 0;
ALTER TABLE request_logs ADD COLUMN IF NOT EXISTS total_tokens INTEGER NOT NULL DEFAULT 0;
ALTER TABLE request_logs ADD COLUMN IF NOT EXISTS cached_input_tokens INTEGER NOT NULL DEFAULT 0;
ALTER TABLE request_logs ADD COLUMN IF NOT EXISTS reasoning_tokens INTEGER NOT NULL DEFAULT 0;
ALTER TABLE request_logs ADD COLUMN IF NOT EXISTS estimated_cost_microusd BIGINT NOT NULL DEFAULT 0;
ALTER TABLE request_logs ADD COLUMN IF NOT EXISTS pricing_snapshot JSONB NOT NULL DEFAULT '{}'::jsonb;
ALTER TABLE request_logs ADD COLUMN IF NOT EXISTS usage_source TEXT NOT NULL DEFAULT 'missing';

CREATE INDEX IF NOT EXISTS request_logs_client_key_usage_idx ON request_logs (client_key_id, created_at DESC);
CREATE INDEX IF NOT EXISTS request_logs_oauth_account_usage_idx ON request_logs (oauth_account_id, created_at DESC);
CREATE INDEX IF NOT EXISTS request_logs_model_usage_idx ON request_logs (model, created_at DESC);
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
DROP INDEX IF EXISTS request_logs_model_usage_idx;
DROP INDEX IF EXISTS request_logs_oauth_account_usage_idx;
DROP INDEX IF EXISTS request_logs_client_key_usage_idx;

ALTER TABLE request_logs DROP COLUMN IF EXISTS usage_source;
ALTER TABLE request_logs DROP COLUMN IF EXISTS pricing_snapshot;
ALTER TABLE request_logs DROP COLUMN IF EXISTS estimated_cost_microusd;
ALTER TABLE request_logs DROP COLUMN IF EXISTS reasoning_tokens;
ALTER TABLE request_logs DROP COLUMN IF EXISTS cached_input_tokens;
ALTER TABLE request_logs DROP COLUMN IF EXISTS total_tokens;
ALTER TABLE request_logs DROP COLUMN IF EXISTS output_tokens;
ALTER TABLE request_logs DROP COLUMN IF EXISTS input_tokens;
ALTER TABLE request_logs DROP COLUMN IF EXISTS model;
ALTER TABLE request_logs DROP COLUMN IF EXISTS oauth_account_id;
-- +goose StatementEnd
```

- [ ] **Step 4: Add request-log DTO fields**

In `backend/internal/gateway/proxy.go`, extend `RequestLog`:

```go
type RequestLog struct {
	RequestID             string
	ClientKeyID           int64
	OAuthAccountID        int64
	Provider              string
	Route                 string
	Method                string
	StatusCode            int
	Latency               time.Duration
	Error                 string
	Model                 string
	InputTokens           int
	OutputTokens          int
	TotalTokens           int
	CachedInputTokens     int
	ReasoningTokens       int
	EstimatedCostMicrousd int64
	PricingSnapshot       map[string]any
	UsageSource           string
	CreatedAt             time.Time
}
```

In `backend/internal/admin/service.go`, extend `RequestLog` with matching JSON fields:

```go
type RequestLog struct {
	ID                    int64     `json:"id"`
	RequestID             string    `json:"requestId"`
	ClientKey             string    `json:"clientKey"`
	OAuthAccount          string    `json:"oauthAccount"`
	Provider              string    `json:"provider"`
	Route                 string    `json:"route"`
	Method                string    `json:"method"`
	StatusCode            int       `json:"statusCode"`
	LatencyMS             int       `json:"latencyMs"`
	Error                 string    `json:"error"`
	Model                 string    `json:"model"`
	InputTokens           int       `json:"inputTokens"`
	OutputTokens          int       `json:"outputTokens"`
	TotalTokens           int       `json:"totalTokens"`
	CachedInputTokens     int       `json:"cachedInputTokens"`
	ReasoningTokens       int       `json:"reasoningTokens"`
	EstimatedCostMicrousd int64     `json:"estimatedCostMicrousd"`
	UsageSource           string    `json:"usageSource"`
	PricingMatched        bool      `json:"pricingMatched"`
	CreatedAt             time.Time `json:"createdAt"`
}
```

- [ ] **Step 5: Run migration tests**

Run:

```bash
cd backend
GOMODCACHE=/root/Clouds/N2API/.cache/go-mod GOCACHE=/root/Clouds/N2API/.cache/go-build go test ./internal/store -run 'TestRequestUsageAccountingMigrationIsEmbedded|TestMigrationProviderSeesEmbeddedMigrations'
```

Expected: PASS.

- [ ] **Step 6: Commit**

```bash
git add backend/internal/store/migrations/00007_request_usage_accounting.sql backend/internal/store/migrations_test.go backend/internal/gateway/proxy.go backend/internal/admin/service.go
git commit -m "feat: add request usage accounting schema"
```

## Task 2: Pricing Settings and Cost Calculation

**Files:**
- Create: `backend/internal/admin/usage.go`
- Create: `backend/internal/admin/usage_test.go`
- Modify: `backend/internal/admin/service.go`
- Modify: `backend/internal/admin/service_test.go`

- [ ] **Step 1: Write failing pricing tests**

Create `backend/internal/admin/usage_test.go`:

```go
package admin

import (
	"errors"
	"testing"
	"time"
)

func TestUsagePricingValidateRejectsInvalidInput(t *testing.T) {
	invalid := UsagePricing{
		Version:   1,
		Currency: "EUR",
		Unit:     "1M_tokens",
		Models: map[string]UsagePrice{
			"gpt-5": {InputMicrousdPerMillion: 1},
		},
	}
	if err := invalid.Validate(); !errors.Is(err, ErrInvalidInput) {
		t.Fatalf("Validate error = %v, want ErrInvalidInput", err)
	}

	invalid.Currency = "USD"
	invalid.Models["gpt-5"] = UsagePrice{InputMicrousdPerMillion: -1}
	if err := invalid.Validate(); !errors.Is(err, ErrInvalidInput) {
		t.Fatalf("Validate negative rate error = %v, want ErrInvalidInput", err)
	}
}

func TestEstimateUsageCostSeparatesCachedInput(t *testing.T) {
	pricing := UsagePricing{
		Version:   1,
		Currency: "USD",
		Unit:     UsagePricingUnitPerMillion,
		UpdatedAt: time.Date(2026, 6, 12, 0, 0, 0, 0, time.UTC),
		Models: map[string]UsagePrice{
			"gpt-5": {
				InputMicrousdPerMillion:       2_000_000,
				CachedInputMicrousdPerMillion: 500_000,
				OutputMicrousdPerMillion:      8_000_000,
			},
		},
	}

	estimate := EstimateUsageCost(Usage{
		Model:             "gpt-5",
		InputTokens:       1_000_000,
		CachedInputTokens: 250_000,
		OutputTokens:      100_000,
		ReasoningTokens:   50_000,
	}, pricing)

	if !estimate.Matched {
		t.Fatal("estimate should match pricing")
	}
	if estimate.CostMicrousd != 2_425_000 {
		t.Fatalf("CostMicrousd = %d, want 2425000", estimate.CostMicrousd)
	}
	if estimate.Snapshot["matched"] != true {
		t.Fatalf("snapshot = %+v, want matched true", estimate.Snapshot)
	}
}

func TestEstimateUsageCostUnknownModelIsUnpriced(t *testing.T) {
	estimate := EstimateUsageCost(Usage{Model: "unknown", InputTokens: 10}, DefaultUsagePricing())
	if estimate.Matched {
		t.Fatal("unknown model should not match pricing")
	}
	if estimate.CostMicrousd != 0 {
		t.Fatalf("CostMicrousd = %d, want 0", estimate.CostMicrousd)
	}
}
```

- [ ] **Step 2: Run pricing tests to verify they fail**

Run:

```bash
cd backend
GOMODCACHE=/root/Clouds/N2API/.cache/go-mod GOCACHE=/root/Clouds/N2API/.cache/go-build go test ./internal/admin -run 'TestUsagePricing|TestEstimateUsageCost'
```

Expected: FAIL because `UsagePricing`, `Usage`, and `EstimateUsageCost` are not defined.

- [ ] **Step 3: Implement pricing types and calculator**

Create `backend/internal/admin/usage.go` with:

```go
package admin

import (
	"strings"
	"time"
)

const (
	UsagePricingUnitPerMillion = "1M_tokens"
	usagePricingCurrencyUSD    = "USD"
	maxUsagePricingModels      = 200
)

type Usage struct {
	Model             string
	InputTokens       int
	OutputTokens      int
	TotalTokens       int
	CachedInputTokens int
	ReasoningTokens   int
	Source            string
}

type UsagePrice struct {
	InputMicrousdPerMillion       int64 `json:"inputMicrousdPerMillion"`
	CachedInputMicrousdPerMillion int64 `json:"cachedInputMicrousdPerMillion"`
	OutputMicrousdPerMillion      int64 `json:"outputMicrousdPerMillion"`
}

type UsagePricing struct {
	Version   int                   `json:"version"`
	Currency  string                `json:"currency"`
	Unit      string                `json:"unit"`
	UpdatedAt time.Time             `json:"updatedAt"`
	Models    map[string]UsagePrice `json:"models"`
}

type UsageCostEstimate struct {
	Matched      bool
	CostMicrousd int64
	Snapshot     map[string]any
}

func DefaultUsagePricing() UsagePricing {
	return UsagePricing{
		Version:   1,
		Currency: usagePricingCurrencyUSD,
		Unit:     UsagePricingUnitPerMillion,
		UpdatedAt: time.Date(2026, 6, 12, 0, 0, 0, 0, time.UTC),
		Models:   map[string]UsagePrice{},
	}
}

func (p UsagePricing) Validate() error {
	if p.Currency != usagePricingCurrencyUSD || p.Unit != UsagePricingUnitPerMillion || len(p.Models) > maxUsagePricingModels {
		return ErrInvalidInput
	}
	for model, price := range p.Models {
		if strings.TrimSpace(model) == "" || len(model) > maxModelNameLen {
			return ErrInvalidInput
		}
		if price.InputMicrousdPerMillion < 0 || price.CachedInputMicrousdPerMillion < 0 || price.OutputMicrousdPerMillion < 0 {
			return ErrInvalidInput
		}
	}
	return nil
}

func NormalizeUsagePricing(p UsagePricing) (UsagePricing, error) {
	if p.Version <= 0 {
		p.Version = 1
	}
	if p.Currency == "" {
		p.Currency = usagePricingCurrencyUSD
	}
	if p.Unit == "" {
		p.Unit = UsagePricingUnitPerMillion
	}
	if p.UpdatedAt.IsZero() {
		p.UpdatedAt = time.Now().UTC()
	}
	normalized := map[string]UsagePrice{}
	for model, price := range p.Models {
		normalized[strings.TrimSpace(model)] = price
	}
	p.Models = normalized
	if p.Models == nil {
		p.Models = map[string]UsagePrice{}
	}
	if err := p.Validate(); err != nil {
		return UsagePricing{}, err
	}
	return p, nil
}

func EstimateUsageCost(usage Usage, pricing UsagePricing) UsageCostEstimate {
	model := strings.TrimSpace(usage.Model)
	price, matched := pricing.Models[model]
	snapshot := map[string]any{
		"matched":   matched,
		"model":     model,
		"currency":  pricing.Currency,
		"unit":      pricing.Unit,
		"version":   pricing.Version,
		"updatedAt": pricing.UpdatedAt,
	}
	if !matched {
		return UsageCostEstimate{Matched: false, Snapshot: snapshot}
	}
	snapshot["inputMicrousdPerMillion"] = price.InputMicrousdPerMillion
	snapshot["cachedInputMicrousdPerMillion"] = price.CachedInputMicrousdPerMillion
	snapshot["outputMicrousdPerMillion"] = price.OutputMicrousdPerMillion

	cached := min(max(usage.CachedInputTokens, 0), max(usage.InputTokens, 0))
	billableInput := max(usage.InputTokens-cached, 0)
	cost := roundedPerMillionCost(billableInput, price.InputMicrousdPerMillion) +
		roundedPerMillionCost(cached, price.CachedInputMicrousdPerMillion) +
		roundedPerMillionCost(max(usage.OutputTokens, 0), price.OutputMicrousdPerMillion)
	return UsageCostEstimate{Matched: true, CostMicrousd: cost, Snapshot: snapshot}
}

func roundedPerMillionCost(tokens int, microusdPerMillion int64) int64 {
	if tokens <= 0 || microusdPerMillion <= 0 {
		return 0
	}
	return (int64(tokens)*microusdPerMillion + 500_000) / 1_000_000
}
```

- [ ] **Step 4: Add service/repository contracts for pricing and summary**

In `backend/internal/admin/service.go`, extend `Repository`:

```go
	GetUsagePricing(ctx context.Context) (UsagePricing, error)
	SaveUsagePricing(ctx context.Context, pricing UsagePricing) (UsagePricing, error)
	GetUsageSummary(ctx context.Context, query UsageSummaryQuery) (UsageSummary, error)
```

Add:

```go
type UsageSummaryQuery struct {
	Range   string `json:"range"`
	GroupBy string `json:"groupBy"`
}

type UsageSummaryRow struct {
	Key                   string `json:"key"`
	Label                 string `json:"label"`
	Requests              int    `json:"requests"`
	InputTokens           int64  `json:"inputTokens"`
	OutputTokens          int64  `json:"outputTokens"`
	TotalTokens           int64  `json:"totalTokens"`
	CachedInputTokens     int64  `json:"cachedInputTokens"`
	ReasoningTokens       int64  `json:"reasoningTokens"`
	EstimatedCostMicrousd int64  `json:"estimatedCostMicrousd"`
}

type UsageSummary struct {
	Range string            `json:"range"`
	Since time.Time         `json:"since"`
	Total UsageSummaryRow   `json:"total"`
	Rows  []UsageSummaryRow `json:"rows"`
}
```

Add service methods:

```go
func (s *Service) GetUsagePricing(ctx context.Context) (UsagePricing, error) {
	pricing, err := s.repo.GetUsagePricing(ctx)
	if errors.Is(err, ErrNotFound) {
		return DefaultUsagePricing(), nil
	}
	return pricing, err
}

func (s *Service) UpdateUsagePricing(ctx context.Context, pricing UsagePricing) (UsagePricing, error) {
	normalized, err := NormalizeUsagePricing(pricing)
	if err != nil {
		return UsagePricing{}, err
	}
	return s.repo.SaveUsagePricing(ctx, normalized)
}

func (s *Service) GetUsageSummary(ctx context.Context, query UsageSummaryQuery) (UsageSummary, error) {
	if query.Range == "" {
		query.Range = "7d"
	}
	if query.GroupBy == "" {
		query.GroupBy = "client_key"
	}
	switch query.Range {
	case "24h", "7d", "30d":
	default:
		return UsageSummary{}, ErrInvalidInput
	}
	switch query.GroupBy {
	case "client_key", "oauth_account", "model":
	default:
		return UsageSummary{}, ErrInvalidInput
	}
	return s.repo.GetUsageSummary(ctx, query)
}
```

- [ ] **Step 5: Extend memory repo and service tests**

In `backend/internal/admin/service_test.go`, add fields to `memoryRepo`:

```go
	usagePricing UsagePricing
	usageSummary UsageSummary
	lastUsageSummaryQuery UsageSummaryQuery
```

Add methods:

```go
func (r *memoryRepo) GetUsagePricing(_ context.Context) (UsagePricing, error) {
	if r.usagePricing.Currency == "" {
		return UsagePricing{}, ErrNotFound
	}
	return r.usagePricing, nil
}

func (r *memoryRepo) SaveUsagePricing(_ context.Context, pricing UsagePricing) (UsagePricing, error) {
	r.usagePricing = pricing
	return pricing, nil
}

func (r *memoryRepo) GetUsageSummary(_ context.Context, query UsageSummaryQuery) (UsageSummary, error) {
	r.lastUsageSummaryQuery = query
	return r.usageSummary, nil
}
```

Add tests:

```go
func TestUsagePricingDefaultAndUpdate(t *testing.T) {
	repo := newMemoryRepo()
	service := NewService(repo, Config{SessionTTL: time.Hour})
	pricing, err := service.GetUsagePricing(context.Background())
	if err != nil {
		t.Fatalf("GetUsagePricing returned error: %v", err)
	}
	if pricing.Currency != "USD" || pricing.Unit != UsagePricingUnitPerMillion {
		t.Fatalf("default pricing = %+v", pricing)
	}

	updated, err := service.UpdateUsagePricing(context.Background(), UsagePricing{
		Currency: "USD",
		Unit:     UsagePricingUnitPerMillion,
		Models: map[string]UsagePrice{
			" gpt-5 ": {InputMicrousdPerMillion: 1},
		},
	})
	if err != nil {
		t.Fatalf("UpdateUsagePricing returned error: %v", err)
	}
	if _, ok := updated.Models["gpt-5"]; !ok {
		t.Fatalf("models = %+v, want trimmed model key", updated.Models)
	}
}

func TestGetUsageSummaryValidatesRangeAndGroupBy(t *testing.T) {
	repo := newMemoryRepo()
	service := NewService(repo, Config{SessionTTL: time.Hour})
	if _, err := service.GetUsageSummary(context.Background(), UsageSummaryQuery{Range: "bad", GroupBy: "client_key"}); !errors.Is(err, ErrInvalidInput) {
		t.Fatalf("bad range error = %v, want ErrInvalidInput", err)
	}
	if _, err := service.GetUsageSummary(context.Background(), UsageSummaryQuery{Range: "7d", GroupBy: "bad"}); !errors.Is(err, ErrInvalidInput) {
		t.Fatalf("bad group error = %v, want ErrInvalidInput", err)
	}
	if _, err := service.GetUsageSummary(context.Background(), UsageSummaryQuery{}); err != nil {
		t.Fatalf("default summary query returned error: %v", err)
	}
	if repo.lastUsageSummaryQuery.Range != "7d" || repo.lastUsageSummaryQuery.GroupBy != "client_key" {
		t.Fatalf("query = %+v, want defaults", repo.lastUsageSummaryQuery)
	}
}
```

- [ ] **Step 6: Run admin tests**

Run:

```bash
cd backend
GOMODCACHE=/root/Clouds/N2API/.cache/go-mod GOCACHE=/root/Clouds/N2API/.cache/go-build go test ./internal/admin
```

Expected: PASS.

- [ ] **Step 7: Commit**

```bash
git add backend/internal/admin/usage.go backend/internal/admin/usage_test.go backend/internal/admin/service.go backend/internal/admin/service_test.go
git commit -m "feat: add usage pricing service"
```

## Task 3: Usage Parser and Proxy Observation

**Files:**
- Create: `backend/internal/gateway/usage.go`
- Create: `backend/internal/gateway/usage_test.go`
- Modify: `backend/internal/gateway/proxy.go`
- Modify: `backend/internal/gateway/proxy_test.go`

- [ ] **Step 1: Write failing parser tests**

Create `backend/internal/gateway/usage_test.go`:

```go
package gateway

import "testing"

func TestParseChatCompletionsUsage(t *testing.T) {
	usage := ParseUsageFromJSON("/v1/chat/completions", []byte(`{
		"model":"gpt-5",
		"usage":{
			"prompt_tokens":100,
			"completion_tokens":25,
			"total_tokens":125,
			"prompt_tokens_details":{"cached_tokens":40},
			"completion_tokens_details":{"reasoning_tokens":10}
		}
	}`))
	if usage.Source != "chat_completions" || usage.Model != "gpt-5" {
		t.Fatalf("usage = %+v", usage)
	}
	if usage.InputTokens != 100 || usage.OutputTokens != 25 || usage.TotalTokens != 125 || usage.CachedInputTokens != 40 || usage.ReasoningTokens != 10 {
		t.Fatalf("usage tokens = %+v", usage)
	}
}

func TestParseResponsesUsage(t *testing.T) {
	usage := ParseUsageFromJSON("/v1/responses", []byte(`{
		"model":"gpt-5-mini",
		"usage":{
			"input_tokens":50,
			"output_tokens":20,
			"total_tokens":70,
			"input_token_details":{"cached_tokens":15},
			"output_token_details":{"reasoning_tokens":8}
		}
	}`))
	if usage.Source != "responses" || usage.Model != "gpt-5-mini" {
		t.Fatalf("usage = %+v", usage)
	}
	if usage.InputTokens != 50 || usage.OutputTokens != 20 || usage.TotalTokens != 70 || usage.CachedInputTokens != 15 || usage.ReasoningTokens != 8 {
		t.Fatalf("usage tokens = %+v", usage)
	}
}

func TestSSEUsageObserverParsesFinalEvent(t *testing.T) {
	observer := NewSSEUsageObserver("/v1/responses")
	observer.Observe([]byte("data: {\"type\":\"response.output_text.delta\",\"delta\":\"hi\"}\n\n"))
	observer.Observe([]byte("data: {\"type\":\"response.completed\",\"response\":{\"model\":\"gpt-5\",\"usage\":{\"input_tokens\":5,\"output_tokens\":7,\"total_tokens\":12}}}\n\n"))
	observer.Observe([]byte("data: [DONE]\n\n"))

	usage := observer.Usage()
	if usage.Source != "stream" || usage.Model != "gpt-5" || usage.InputTokens != 5 || usage.OutputTokens != 7 || usage.TotalTokens != 12 {
		t.Fatalf("usage = %+v", usage)
	}
}

func TestRequestModelFromBodyFallback(t *testing.T) {
	model := RequestModelFromBody([]byte(`{"model":"gpt-5","messages":[]}`))
	if model != "gpt-5" {
		t.Fatalf("model = %q, want gpt-5", model)
	}
}
```

- [ ] **Step 2: Run parser tests to verify they fail**

Run:

```bash
cd backend
GOMODCACHE=/root/Clouds/N2API/.cache/go-mod GOCACHE=/root/Clouds/N2API/.cache/go-build go test ./internal/gateway -run 'TestParse|TestSSEUsageObserver|TestRequestModel'
```

Expected: FAIL because parser functions are not defined.

- [ ] **Step 3: Implement parser and observer**

Create `backend/internal/gateway/usage.go` with:

```go
package gateway

import (
	"bytes"
	"encoding/json"
	"strings"
)

type Usage struct {
	Model             string
	InputTokens       int
	OutputTokens      int
	TotalTokens       int
	CachedInputTokens int
	ReasoningTokens   int
	Source            string
}

func ParseUsageFromJSON(route string, body []byte) Usage {
	var payload map[string]any
	if err := json.Unmarshal(body, &payload); err != nil {
		return Usage{Source: "parse_error"}
	}
	return parseUsagePayload(route, payload, "")
}

func RequestModelFromBody(body []byte) string {
	var payload struct {
		Model string `json:"model"`
	}
	if err := json.Unmarshal(body, &payload); err != nil {
		return ""
	}
	return strings.TrimSpace(payload.Model)
}

type SSEUsageObserver struct {
	route   string
	pending []byte
	usage   Usage
}

func NewSSEUsageObserver(route string) *SSEUsageObserver {
	return &SSEUsageObserver{route: route, usage: Usage{Source: "missing"}}
}

func (o *SSEUsageObserver) Observe(data []byte) {
	o.pending = append(o.pending, data...)
	for {
		idx := bytes.IndexByte(o.pending, '\n')
		if idx < 0 {
			return
		}
		line := strings.TrimSpace(string(o.pending[:idx]))
		o.pending = o.pending[idx+1:]
		if !strings.HasPrefix(line, "data:") {
			continue
		}
		raw := strings.TrimSpace(strings.TrimPrefix(line, "data:"))
		if raw == "" || raw == "[DONE]" {
			continue
		}
		var payload map[string]any
		if err := json.Unmarshal([]byte(raw), &payload); err != nil {
			continue
		}
		if response, ok := payload["response"].(map[string]any); ok {
			parsed := parseUsagePayload(o.route, response, "stream")
			if parsed.Source != "missing" && parsed.Source != "parse_error" {
				o.usage = parsed
			}
			continue
		}
		parsed := parseUsagePayload(o.route, payload, "stream")
		if parsed.Source != "missing" && parsed.Source != "parse_error" {
			o.usage = parsed
		}
	}
}

func (o *SSEUsageObserver) Usage() Usage {
	return o.usage
}
```

Also implement helpers in the same file:

```go
func parseUsagePayload(route string, payload map[string]any, sourceOverride string) Usage {
	model, _ := payload["model"].(string)
	usagePayload, ok := payload["usage"].(map[string]any)
	if !ok {
		return Usage{Model: strings.TrimSpace(model), Source: "missing"}
	}
	if strings.TrimSpace(sourceOverride) != "" {
		source := sourceOverride
		usage := parseResponsesUsage(usagePayload)
		if usage.InputTokens == 0 && usage.OutputTokens == 0 && usage.TotalTokens == 0 {
			usage = parseChatUsage(usagePayload)
		}
		usage.Model = strings.TrimSpace(model)
		usage.Source = source
		return usage
	}
	if route == "/v1/chat/completions" {
		usage := parseChatUsage(usagePayload)
		usage.Model = strings.TrimSpace(model)
		usage.Source = "chat_completions"
		return usage
	}
	usage := parseResponsesUsage(usagePayload)
	usage.Model = strings.TrimSpace(model)
	usage.Source = "responses"
	return usage
}

func parseChatUsage(payload map[string]any) Usage {
	usage := Usage{
		InputTokens:  intFromAny(payload["prompt_tokens"]),
		OutputTokens: intFromAny(payload["completion_tokens"]),
		TotalTokens:  intFromAny(payload["total_tokens"]),
	}
	if details, ok := payload["prompt_tokens_details"].(map[string]any); ok {
		usage.CachedInputTokens = intFromAny(details["cached_tokens"])
	}
	if details, ok := payload["completion_tokens_details"].(map[string]any); ok {
		usage.ReasoningTokens = intFromAny(details["reasoning_tokens"])
	}
	return usage
}

func parseResponsesUsage(payload map[string]any) Usage {
	usage := Usage{
		InputTokens:  intFromAny(payload["input_tokens"]),
		OutputTokens: intFromAny(payload["output_tokens"]),
		TotalTokens:  intFromAny(payload["total_tokens"]),
	}
	if details, ok := payload["input_token_details"].(map[string]any); ok {
		usage.CachedInputTokens = intFromAny(details["cached_tokens"])
	}
	if details, ok := payload["output_token_details"].(map[string]any); ok {
		usage.ReasoningTokens = intFromAny(details["reasoning_tokens"])
	}
	return usage
}

func intFromAny(value any) int {
	switch typed := value.(type) {
	case float64:
		if typed < 0 {
			return 0
		}
		return int(typed)
	case int:
		if typed < 0 {
			return 0
		}
		return typed
	default:
		return 0
	}
}
```

- [ ] **Step 4: Run parser tests**

Run:

```bash
cd backend
GOMODCACHE=/root/Clouds/N2API/.cache/go-mod GOCACHE=/root/Clouds/N2API/.cache/go-build go test ./internal/gateway -run 'TestParse|TestSSEUsageObserver|TestRequestModel'
```

Expected: PASS.

- [ ] **Step 5: Write failing proxy logging tests**

In `backend/internal/gateway/proxy_test.go`, add:

```go
func TestProxyLogsNonStreamingChatUsage(t *testing.T) {
	logger := &fakeRequestLogger{}
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"model":"gpt-5","usage":{"prompt_tokens":20,"completion_tokens":5,"total_tokens":25,"prompt_tokens_details":{"cached_tokens":4},"completion_tokens_details":{"reasoning_tokens":2}}}`))
	}))
	defer upstream.Close()
	proxy := NewProxy(&fakeAPIKeyAuthenticator{}, &fakeSelectedTokenProvider{tokens: []SelectedToken{{AccountID: 7, Token: "upstream-token"}}}, Config{
		UpstreamBaseURL: upstream.URL,
		Logger:          logger,
	})
	req := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", strings.NewReader(`{"model":"gpt-5","messages":[]}`))
	req.Header.Set("Authorization", "Bearer n2api_client_secret")
	recorder := httptest.NewRecorder()

	proxy.ServeHTTP(recorder, req)

	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", recorder.Code)
	}
	if len(logger.entries) != 1 {
		t.Fatalf("logged entries = %d, want 1", len(logger.entries))
	}
	entry := logger.entries[0]
	if entry.OAuthAccountID != 7 || entry.Model != "gpt-5" || entry.UsageSource != "chat_completions" {
		t.Fatalf("entry = %+v", entry)
	}
	if entry.InputTokens != 20 || entry.OutputTokens != 5 || entry.TotalTokens != 25 || entry.CachedInputTokens != 4 || entry.ReasoningTokens != 2 {
		t.Fatalf("entry tokens = %+v", entry)
	}
}
```

Add a streaming test:

```go
func TestProxyLogsStreamingResponsesUsageAndPreservesBody(t *testing.T) {
	logger := &fakeRequestLogger{}
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = w.Write([]byte("data: {\"type\":\"response.output_text.delta\",\"delta\":\"hi\"}\n\n"))
		_, _ = w.Write([]byte("data: {\"type\":\"response.completed\",\"response\":{\"model\":\"gpt-5\",\"usage\":{\"input_tokens\":3,\"output_tokens\":4,\"total_tokens\":7}}}\n\n"))
		_, _ = w.Write([]byte("data: [DONE]\n\n"))
	}))
	defer upstream.Close()
	proxy := NewProxy(&fakeAPIKeyAuthenticator{}, &fakeSelectedTokenProvider{tokens: []SelectedToken{{AccountID: 9, Token: "upstream-token"}}}, Config{
		UpstreamBaseURL: upstream.URL,
		Logger:          logger,
	})
	req := httptest.NewRequest(http.MethodPost, "/v1/responses", strings.NewReader(`{"model":"gpt-5","input":"hi","stream":true}`))
	req.Header.Set("Authorization", "Bearer n2api_client_secret")
	recorder := httptest.NewRecorder()

	proxy.ServeHTTP(recorder, req)

	if !strings.Contains(recorder.Body.String(), "response.output_text.delta") || !strings.Contains(recorder.Body.String(), "[DONE]") {
		t.Fatalf("body = %q, want original stream", recorder.Body.String())
	}
	entry := logger.entries[0]
	if entry.UsageSource != "stream" || entry.InputTokens != 3 || entry.OutputTokens != 4 || entry.TotalTokens != 7 {
		t.Fatalf("entry = %+v", entry)
	}
}
```

- [ ] **Step 6: Run proxy tests to verify they fail**

Run:

```bash
cd backend
GOMODCACHE=/root/Clouds/N2API/.cache/go-mod GOCACHE=/root/Clouds/N2API/.cache/go-build go test ./internal/gateway -run 'TestProxyLogs'
```

Expected: FAIL because proxy copy path does not populate usage.

- [ ] **Step 7: Integrate observer into proxy**

In `backend/internal/gateway/proxy.go`:

- Capture request model from replayable request bodies in `requestBodyFactory` by returning a fourth `requestModel string`, or add a small helper that reads it from the already-buffered body.
- Initialize `observedUsage := Usage{Model: requestModel, Source: "missing"}` before upstream attempts.
- When selected token succeeds, set the deferred log entry `OAuthAccountID` to `selected.AccountID`.
- Replace direct `io.Copy(flushWriter{ResponseWriter: recorder}, upstreamResp.Body)` with a helper:

```go
observedUsage = p.copyUpstreamResponse(recorder, upstreamResp, r.URL.Path, requestModel)
```

Add helper:

```go
func (p *Proxy) copyUpstreamResponse(w http.ResponseWriter, resp *http.Response, route, requestModel string) Usage {
	if strings.Contains(strings.ToLower(resp.Header.Get("Content-Type")), "text/event-stream") {
		observer := NewSSEUsageObserver(route)
		_, _ = io.Copy(flushWriter{ResponseWriter: w, observer: observer}, resp.Body)
		usage := observer.Usage()
		if usage.Model == "" {
			usage.Model = requestModel
		}
		return usage
	}
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return Usage{Model: requestModel, Source: "parse_error"}
	}
	_, _ = w.Write(body)
	usage := ParseUsageFromJSON(route, body)
	if usage.Model == "" {
		usage.Model = requestModel
	}
	return usage
}
```

Update `flushWriter`:

```go
type flushWriter struct {
	http.ResponseWriter
	observer interface{ Observe([]byte) }
}

func (w flushWriter) Write(data []byte) (int, error) {
	if w.observer != nil {
		w.observer.Observe(data)
	}
	n, err := w.ResponseWriter.Write(data)
	if flusher, ok := w.ResponseWriter.(http.Flusher); ok {
		flusher.Flush()
	}
	return n, err
}
```

Before logging, copy `observedUsage` into `RequestLog` fields. Do not compute cost in this task.

- [ ] **Step 8: Run gateway tests**

Run:

```bash
cd backend
GOMODCACHE=/root/Clouds/N2API/.cache/go-mod GOCACHE=/root/Clouds/N2API/.cache/go-build go test ./internal/gateway
```

Expected: PASS.

- [ ] **Step 9: Commit**

```bash
git add backend/internal/gateway/usage.go backend/internal/gateway/usage_test.go backend/internal/gateway/proxy.go backend/internal/gateway/proxy_test.go
git commit -m "feat: observe gateway token usage"
```

## Task 4: Persist Usage and Provide Admin Aggregates

**Files:**
- Modify: `backend/internal/store/gateway.go`
- Modify: `backend/internal/store/gateway_test.go`
- Modify: `backend/internal/store/admin.go`
- Modify: `backend/internal/admin/service.go`
- Modify: `backend/internal/admin/service_test.go`

- [ ] **Step 1: Write failing repository-oriented tests**

In `backend/internal/store/gateway_test.go`, add compile-time assertion after the existing one remains:

```go
func TestGatewayRepositoryRequestLogUsesUsageColumns(t *testing.T) {
	sql := createRequestLogSQL()
	for _, want := range []string{
		"oauth_account_id",
		"model",
		"input_tokens",
		"estimated_cost_microusd",
		"pricing_snapshot",
		"usage_source",
	} {
		if !strings.Contains(sql, want) {
			t.Fatalf("CreateRequestLog SQL missing %q", want)
		}
	}
}
```

This requires extracting the SQL string in `gateway.go` into:

```go
func createRequestLogSQL() string { return `...` }
```

In `backend/internal/admin/service_test.go`, extend `TestListRequestLogsClampsLimitAndReturnsRepositoryLogs` expected log fixture with usage fields and assert they are returned unchanged.

- [ ] **Step 2: Run targeted tests to verify they fail**

Run:

```bash
cd backend
GOMODCACHE=/root/Clouds/N2API/.cache/go-mod GOCACHE=/root/Clouds/N2API/.cache/go-build go test ./internal/store ./internal/admin -run 'TestGatewayRepositoryRequestLogUsesUsageColumns|TestListRequestLogs'
```

Expected: FAIL because SQL helper does not exist and store scan does not include new fields.

- [ ] **Step 3: Persist accounting fields**

In `backend/internal/store/gateway.go`, add imports:

```go
import "encoding/json"
```

Add:

```go
func createRequestLogSQL() string {
	return `
		INSERT INTO request_logs (
			request_id, client_key_id, oauth_account_id, provider, route, method, status_code, latency_ms, error,
			model, input_tokens, output_tokens, total_tokens, cached_input_tokens, reasoning_tokens,
			estimated_cost_microusd, pricing_snapshot, usage_source, created_at
		)
		VALUES ($1, $2, NULLIF($3, 0), $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14, $15, $16, $17, $18, $19)
	`
}
```

Update `CreateRequestLog` to marshal `entry.PricingSnapshot`, default `UsageSource` to `missing`, and pass every new field.

- [ ] **Step 4: Read enriched logs**

In `backend/internal/store/admin.go`, update `ListRequestLogs` query to select:

```sql
COALESCE(NULLIF(a.display_name, ''), NULLIF(a.subject, ''), ''),
l.model,
l.input_tokens,
l.output_tokens,
l.total_tokens,
l.cached_input_tokens,
l.reasoning_tokens,
l.estimated_cost_microusd,
l.usage_source,
COALESCE((l.pricing_snapshot->>'matched')::boolean, false)
```

Join:

```sql
LEFT JOIN oauth_accounts a ON a.id = l.oauth_account_id
```

Scan into the new `admin.RequestLog` fields.

- [ ] **Step 5: Implement pricing settings repository methods**

In `backend/internal/store/admin.go`, add:

```go
const usagePricingKey = "usage_pricing"
```

Add `GetUsagePricing` and `SaveUsagePricing` following the existing model settings JSONB pattern.

- [ ] **Step 6: Implement usage summary repository method**

In `backend/internal/store/admin.go`, add `GetUsageSummary(ctx context.Context, query admin.UsageSummaryQuery)`.

Use safe switch-generated SQL fragments, not raw user input:

```go
var groupExpr, labelExpr string
switch query.GroupBy {
case "client_key":
	groupExpr = "COALESCE(l.client_key_id::text, 'unknown')"
	labelExpr = "COALESCE(k.name || ' (' || k.prefix || ')', 'Unknown')"
case "oauth_account":
	groupExpr = "COALESCE(l.oauth_account_id::text, 'unknown')"
	labelExpr = "COALESCE(NULLIF(a.display_name, ''), NULLIF(a.subject, ''), 'Unknown')"
case "model":
	groupExpr = "COALESCE(NULLIF(l.model, ''), 'unknown')"
	labelExpr = "COALESCE(NULLIF(l.model, ''), 'Unknown')"
}
```

Compute `since` in Go from `query.Range`, pass it as `$1`, and aggregate:

```sql
COUNT(*),
COALESCE(SUM(input_tokens), 0),
COALESCE(SUM(output_tokens), 0),
COALESCE(SUM(total_tokens), 0),
COALESCE(SUM(cached_input_tokens), 0),
COALESCE(SUM(reasoning_tokens), 0),
COALESCE(SUM(estimated_cost_microusd), 0)
```

Return total plus ordered rows by `estimated_cost_microusd DESC, requests DESC, label ASC`.

- [ ] **Step 7: Run store/admin tests**

Run:

```bash
cd backend
GOMODCACHE=/root/Clouds/N2API/.cache/go-mod GOCACHE=/root/Clouds/N2API/.cache/go-build go test ./internal/store ./internal/admin
```

Expected: PASS.

- [ ] **Step 8: Commit**

```bash
git add backend/internal/store/gateway.go backend/internal/store/gateway_test.go backend/internal/store/admin.go backend/internal/admin/service.go backend/internal/admin/service_test.go
git commit -m "feat: persist usage accounting"
```

## Task 5: Compute Cost During Gateway Logging

**Files:**
- Modify: `backend/internal/gateway/proxy.go`
- Modify: `backend/internal/gateway/proxy_test.go`
- Modify: `backend/internal/admin/usage.go`
- Modify: `backend/cmd/n2api/main.go`

- [ ] **Step 1: Write failing gateway cost test**

In `backend/internal/gateway/proxy_test.go`, extend `fakeRequestLogger` or add fake pricing dependency. The cleaner boundary is a new gateway interface:

```go
type fakeUsagePricer struct{}

func (fakeUsagePricer) EstimateUsageCost(_ context.Context, usage Usage) (UsageCostEstimate, error) {
	return UsageCostEstimate{
		Matched:      true,
		CostMicrousd: 1234,
		Snapshot:     map[string]any{"matched": true, "model": usage.Model},
	}, nil
}
```

Add test:

```go
func TestProxyLogsEstimatedUsageCost(t *testing.T) {
	logger := &fakeRequestLogger{}
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"model":"gpt-5","usage":{"input_tokens":10,"output_tokens":5,"total_tokens":15}}`))
	}))
	defer upstream.Close()
	proxy := NewProxy(&fakeAPIKeyAuthenticator{}, &fakeSelectedTokenProvider{tokens: []SelectedToken{{AccountID: 1, Token: "upstream-token"}}}, Config{
		UpstreamBaseURL: upstream.URL,
		Logger:          logger,
		UsagePricer:     fakeUsagePricer{},
	})
	req := httptest.NewRequest(http.MethodPost, "/v1/responses", strings.NewReader(`{"model":"gpt-5","input":"hi"}`))
	req.Header.Set("Authorization", "Bearer n2api_client_secret")
	proxy.ServeHTTP(httptest.NewRecorder(), req)

	entry := logger.entries[0]
	if entry.EstimatedCostMicrousd != 1234 || entry.PricingSnapshot["matched"] != true {
		t.Fatalf("entry cost/snapshot = %d/%+v", entry.EstimatedCostMicrousd, entry.PricingSnapshot)
	}
}
```

- [ ] **Step 2: Run gateway cost test to verify it fails**

Run:

```bash
cd backend
GOMODCACHE=/root/Clouds/N2API/.cache/go-mod GOCACHE=/root/Clouds/N2API/.cache/go-build go test ./internal/gateway -run TestProxyLogsEstimatedUsageCost
```

Expected: FAIL because `UsagePricer` is not wired into gateway config.

- [ ] **Step 3: Add gateway pricing interface**

In `backend/internal/gateway/proxy.go`, add:

```go
type UsagePricer interface {
	EstimateUsageCost(ctx context.Context, usage Usage) (UsageCostEstimate, error)
}

type UsageCostEstimate struct {
	Matched      bool
	CostMicrousd int64
	Snapshot     map[string]any
}
```

Extend `Config` and `Proxy` with `UsagePricer`.

Before request log creation, if `p.usagePricer != nil`, call it with observed usage. Logging remains best-effort: pricing errors produce `EstimatedCostMicrousd=0`, `PricingSnapshot={"matched": false, "error": "pricing_error"}`.

- [ ] **Step 4: Bridge admin service to gateway pricer**

In `backend/internal/admin/usage.go`, add method:

```go
func (s *Service) EstimateUsageCost(ctx context.Context, usage gateway.Usage) (gateway.UsageCostEstimate, error) {
	pricing, err := s.GetUsagePricing(ctx)
	if err != nil {
		return gateway.UsageCostEstimate{}, err
	}
	estimate := EstimateUsageCost(Usage{
		Model:             usage.Model,
		InputTokens:       usage.InputTokens,
		OutputTokens:      usage.OutputTokens,
		TotalTokens:       usage.TotalTokens,
		CachedInputTokens: usage.CachedInputTokens,
		ReasoningTokens:   usage.ReasoningTokens,
		Source:            usage.Source,
	}, pricing)
	return gateway.UsageCostEstimate{
		Matched:      estimate.Matched,
		CostMicrousd: estimate.CostMicrousd,
		Snapshot:     estimate.Snapshot,
	}, nil
}
```

Add the import for `github.com/KnowSky404/N2API/backend/internal/gateway`.

- [ ] **Step 5: Wire pricer in main**

In `backend/cmd/n2api/main.go`, where `gateway.Config` is built, set:

```go
UsagePricer: adminService,
```

Use the actual local variable name for the existing admin service.

- [ ] **Step 6: Run gateway and command tests**

Run:

```bash
cd backend
GOMODCACHE=/root/Clouds/N2API/.cache/go-mod GOCACHE=/root/Clouds/N2API/.cache/go-build go test ./internal/gateway ./internal/admin ./cmd/n2api
```

Expected: PASS.

- [ ] **Step 7: Commit**

```bash
git add backend/internal/gateway/proxy.go backend/internal/gateway/proxy_test.go backend/internal/admin/usage.go backend/cmd/n2api/main.go
git commit -m "feat: price gateway token usage"
```

## Task 6: Admin HTTP API

**Files:**
- Modify: `backend/internal/httpapi/server.go`
- Modify: `backend/internal/httpapi/server_test.go`
- Modify: `backend/internal/admin/service.go`

- [ ] **Step 1: Write failing HTTP API tests**

In `backend/internal/httpapi/server_test.go`, extend fake admin service with usage methods, then add tests:

```go
func TestUsageSummaryRequiresValidQuery(t *testing.T) {
	admins := newFakeAdminService()
	admins.usageSummary = admin.UsageSummary{Range: "7d"}
	server := NewServer(testConfig(), nil, admins, nil)
	req := httptest.NewRequest(http.MethodGet, "/api/admin/usage-summary?range=bad&groupBy=client_key", nil)
	req.AddCookie(validAdminCookie())
	rec := httptest.NewRecorder()

	server.ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", rec.Code)
	}
}

func TestUsagePricingUpdateReturnsValidationError(t *testing.T) {
	admins := newFakeAdminService()
	server := NewServer(testConfig(), nil, admins, nil)
	req := httptest.NewRequest(http.MethodPut, "/api/admin/usage-pricing", strings.NewReader(`{"currency":"EUR","unit":"1M_tokens","models":{}}`))
	req.AddCookie(validAdminCookie())
	rec := httptest.NewRecorder()

	server.ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", rec.Code)
	}
}
```

Adapt helper names to the existing test file.

- [ ] **Step 2: Run HTTP API tests to verify they fail**

Run:

```bash
cd backend
GOMODCACHE=/root/Clouds/N2API/.cache/go-mod GOCACHE=/root/Clouds/N2API/.cache/go-build go test ./internal/httpapi -run 'TestUsage'
```

Expected: FAIL because routes and interface methods are missing.

- [ ] **Step 3: Extend AdminService interface**

In `backend/internal/httpapi/server.go`, add to `AdminService`:

```go
	GetUsageSummary(ctx context.Context, query admin.UsageSummaryQuery) (admin.UsageSummary, error)
	GetUsagePricing(ctx context.Context) (admin.UsagePricing, error)
	UpdateUsagePricing(ctx context.Context, pricing admin.UsagePricing) (admin.UsagePricing, error)
```

- [ ] **Step 4: Add protected routes**

In `NewServer`, after request logs route:

```go
mux.HandleFunc("GET /api/admin/usage-summary", requireAdmin(func(w http.ResponseWriter, r *http.Request, _ admin.Admin) {
	summary, err := admins.GetUsageSummary(r.Context(), admin.UsageSummaryQuery{
		Range:   r.URL.Query().Get("range"),
		GroupBy: r.URL.Query().Get("groupBy"),
	})
	if err != nil {
		if errors.Is(err, admin.ErrInvalidInput) {
			writeError(w, http.StatusBadRequest, "bad_request")
			return
		}
		writeError(w, http.StatusInternalServerError, "internal_error")
		return
	}
	writeJSON(w, http.StatusOK, summary)
}))

mux.HandleFunc("GET /api/admin/usage-pricing", requireAdmin(func(w http.ResponseWriter, r *http.Request, _ admin.Admin) {
	pricing, err := admins.GetUsagePricing(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal_error")
		return
	}
	writeJSON(w, http.StatusOK, pricing)
}))

mux.HandleFunc("PUT /api/admin/usage-pricing", requireAdmin(func(w http.ResponseWriter, r *http.Request, _ admin.Admin) {
	var req admin.UsagePricing
	if err := decodeJSON(w, r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "bad_request")
		return
	}
	pricing, err := admins.UpdateUsagePricing(r.Context(), req)
	if err != nil {
		if errors.Is(err, admin.ErrInvalidInput) {
			writeError(w, http.StatusBadRequest, "invalid_input")
			return
		}
		writeError(w, http.StatusInternalServerError, "internal_error")
		return
	}
	writeJSON(w, http.StatusOK, pricing)
}))
```

- [ ] **Step 5: Update test fakes**

Update `backend/internal/httpapi/server_test.go` fake admin service to implement the three new methods and delegate to real service validation if useful. Ensure invalid usage-pricing payload returns `admin.ErrInvalidInput`.

- [ ] **Step 6: Run HTTP API tests**

Run:

```bash
cd backend
GOMODCACHE=/root/Clouds/N2API/.cache/go-mod GOCACHE=/root/Clouds/N2API/.cache/go-build go test ./internal/httpapi
```

Expected: PASS.

- [ ] **Step 7: Commit**

```bash
git add backend/internal/httpapi/server.go backend/internal/httpapi/server_test.go backend/internal/admin/service.go
git commit -m "feat: expose usage admin api"
```

## Task 7: Frontend State and Formatting

**Files:**
- Modify: `frontend/src/lib/admin-state.svelte.js`
- Create: `frontend/src/lib/usage-format.test.mjs`

- [ ] **Step 1: Write failing formatter tests**

Create `frontend/src/lib/usage-format.test.mjs`:

```js
import { test } from 'node:test';
import assert from 'node:assert/strict';
import { formatCostMicrousd, formatTokens } from './admin-state.svelte.js';

test('formatTokens uses compact tabular-friendly counts', () => {
  assert.equal(formatTokens(0), '0');
  assert.equal(formatTokens(1234), '1,234');
});

test('formatCostMicrousd renders approximate USD', () => {
  assert.equal(formatCostMicrousd(0), '$0.0000');
  assert.equal(formatCostMicrousd(1234), '$0.0012');
  assert.equal(formatCostMicrousd(1234567), '$1.2346');
});
```

- [ ] **Step 2: Run formatter tests to verify they fail**

Run:

```bash
cd frontend
bun test src/lib/usage-format.test.mjs
```

Expected: FAIL because formatters are not exported.

- [ ] **Step 3: Add usage state and loaders**

In `frontend/src/lib/admin-state.svelte.js`, add typedefs for usage summary, pricing model rows, and pricing. Add state:

```js
export const usage = $state({
  loading: false,
  error: '',
  range: '7d',
  groupBy: 'client_key',
  summaries: {},
  current: null
});

export const usagePricing = $state({
  loading: false,
  saving: false,
  error: '',
  saved: false,
  version: 1,
  currency: 'USD',
  unit: '1M_tokens',
  rows: []
});
```

Add clear/reset calls wherever `clearRequestLogs()` is called, or add a new `clearUsage()` and call it on logout/unauthenticated session.

Add functions:

```js
export function formatTokens(value) {
  return Number(value ?? 0).toLocaleString();
}

export function formatCostMicrousd(value) {
  return `$${(Number(value ?? 0) / 1_000_000).toFixed(4)}`;
}
```

Add `loadUsageSummary(range = usage.range, groupBy = usage.groupBy)`, `loadUsagePricing()`, and `saveUsagePricing(event)` using `requestJSON`.

When authenticated session loads, call:

```js
await loadUsageSummary('24h', usage.groupBy);
await loadUsageSummary('7d', usage.groupBy);
await loadUsageSummary('30d', usage.groupBy);
await loadUsagePricing();
```

- [ ] **Step 4: Run formatter tests**

Run:

```bash
cd frontend
bun test src/lib/usage-format.test.mjs
```

Expected: PASS.

- [ ] **Step 5: Run frontend check**

Run:

```bash
cd frontend
bun run check
```

Expected: PASS.

- [ ] **Step 6: Commit**

```bash
git add frontend/src/lib/admin-state.svelte.js frontend/src/lib/usage-format.test.mjs
git commit -m "feat: add usage admin state"
```

## Task 8: Frontend Request Logs Usage UI

**Files:**
- Modify: `frontend/src/routes/request-logs/+page.svelte`
- Modify: `frontend/src/routes/navigation.test.mjs`

- [ ] **Step 1: Write failing static UI test**

In `frontend/src/routes/navigation.test.mjs`, add assertions that request logs page contains usage UI labels:

```js
test('request logs page includes usage accounting UI', () => {
  const page = readFileSync('src/routes/request-logs/+page.svelte', 'utf8');
  for (const label of ['Usage summary', 'Estimated cost', 'Input tokens', 'Output tokens', 'Pricing']) {
    assert.match(page, new RegExp(label.replace(' ', '\\s+')), `request logs page should include ${label}`);
  }
});
```

- [ ] **Step 2: Run static UI test to verify it fails**

Run:

```bash
cd frontend
bun test src/routes/navigation.test.mjs
```

Expected: FAIL because labels are not present.

- [ ] **Step 3: Add usage summary and pricing UI**

In `frontend/src/routes/request-logs/+page.svelte`, import new state/functions:

```js
usage,
usagePricing,
formatTokens,
formatCostMicrousd,
loadUsageSummary,
loadUsagePricing,
saveUsagePricing
```

Add a top section above the request logs table:

- Three summary cards for `24h`, `7d`, and `30d`.
- Group selector bound to `usage.groupBy`.
- Grouped table from `usage.current.rows`.
- Pricing editor table from `usagePricing.rows`.

Keep all sections as flat panels, not nested cards. Use existing neutral palette classes from the page.

- [ ] **Step 4: Add request-log usage columns**

In the request log table:

- Increase min width from `900px` to around `1180px`.
- Add headers `Model`, `Tokens`, `Estimated cost`, and `Usage`.
- Render token summary as:

```svelte
{formatTokens(log.inputTokens)} in / {formatTokens(log.outputTokens)} out
```

- Render cost as `Unpriced` when `!log.pricingMatched && (log.inputTokens || log.outputTokens || log.totalTokens)`.
- Render missing usage as `log.usageSource || 'missing'`.

- [ ] **Step 5: Run frontend tests/check/build**

Run:

```bash
cd frontend
bun test src/routes/navigation.test.mjs src/lib/usage-format.test.mjs
bun run check
bun run build
```

Expected: PASS.

- [ ] **Step 6: Commit**

```bash
git add frontend/src/routes/request-logs/+page.svelte frontend/src/routes/navigation.test.mjs
git commit -m "feat: show usage accounting in admin"
```

## Task 9: Full Verification and Docs Touch-Up

**Files:**
- Optionally modify: `deploy/README.md`
- Optionally modify: `README.md`

- [ ] **Step 1: Run full backend tests**

Run:

```bash
cd backend
GOMODCACHE=/root/Clouds/N2API/.cache/go-mod GOCACHE=/root/Clouds/N2API/.cache/go-build go test ./...
```

Expected: PASS.

- [ ] **Step 2: Run full frontend verification**

Run:

```bash
cd frontend
bun test src/routes/navigation.test.mjs src/lib/usage-format.test.mjs
bun run check
bun run build
```

Expected: PASS.

- [ ] **Step 3: Review docs need**

If implementation added an operator-visible migration or pricing-setting behavior not already clear from UI, add a short section to `deploy/README.md`:

```md
## Usage Accounting

N2API records token usage from upstream OpenAI-compatible response metadata when available. Estimated cost is calculated from the editable admin pricing table and stored with each request log as a historical estimate. Existing request logs created before this feature keep zero usage fields.
```

If the UI already explains pricing clearly and no deployment command changed, skip docs change and note that in final status.

- [ ] **Step 4: Run git diff review**

Run:

```bash
git status --short
git diff --check
```

Expected: no unintended files and no whitespace errors.

- [ ] **Step 5: Commit docs if changed**

If docs changed:

```bash
git add deploy/README.md README.md
git commit -m "docs: document usage accounting"
```

If no docs changed, do not create an empty commit.

## Self-Review Checklist

- Spec goal coverage:
  - Per-request token fields: Tasks 1, 3, 4.
  - API key and OAuth account attribution: Tasks 1, 3, 4.
  - Editable pricing and historical snapshots: Tasks 2, 4, 5, 6, 7, 8.
  - Admin summary by API key, account, model: Tasks 4, 6, 7, 8.
  - Streaming preservation: Task 3.
  - Unknown model and missing usage states: Tasks 2, 3, 8.
- Placeholder scan: no deferred-work placeholders.
- Type consistency:
  - `estimated_cost_microusd` maps to `EstimatedCostMicrousd` and `estimatedCostMicrousd`.
  - `cached_input_tokens` maps to `CachedInputTokens` and `cachedInputTokens`.
  - `usage_source` maps to `UsageSource` and `usageSource`.
  - Pricing rates use `MicrousdPerMillion` consistently.

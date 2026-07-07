package admin

import (
	"context"
	"fmt"
	"html"
	"io"
	"math"
	"net/http"
	"regexp"
	"strconv"
	"strings"
	"time"
)

const officialPricingURL = "https://developers.openai.com/api/docs/pricing"

// PricingFetcher abstracts fetching the OpenAI official pricing page.
type PricingFetcher interface {
	Fetch(ctx context.Context) ([]byte, error)
}

// HTTPPricingFetcher fetches the official pricing page over HTTP.
type HTTPPricingFetcher struct {
	client *http.Client
	url    string
}

// NewHTTPPricingFetcher returns a PricingFetcher that fetches the official
// pricing page with a bounded timeout.
func NewHTTPPricingFetcher(timeout time.Duration) PricingFetcher {
	if timeout <= 0 {
		timeout = 30 * time.Second
	}
	return &HTTPPricingFetcher{
		client: &http.Client{Timeout: timeout},
		url:    officialPricingURL,
	}
}

// Fetch retrieves the pricing page body.
func (f *HTTPPricingFetcher) Fetch(ctx context.Context) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, f.url, nil)
	if err != nil {
		return nil, fmt.Errorf("pricing request: %w", err)
	}
	resp, err := f.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("pricing fetch: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("pricing page status %d", resp.StatusCode)
	}
	return io.ReadAll(io.LimitReader(resp.Body, 2<<20))
}

// UsagePricingSyncSummary reports the result of an official pricing sync.
type UsagePricingSyncSummary struct {
	Total  int    `json:"total"`
	Source string `json:"source"`
}

// defaultUsagePricing returns a UsagePricing pre-seeded with OpenAI official
// Standard token prices (per 1M tokens, micro-USD).
func defaultUsagePricing() UsagePricing {
	return UsagePricing{
		Version:   1,
		Currency:  "USD",
		Unit:      "1M_tokens",
		UpdatedAt: time.Now().UTC(),
		Models: map[string]UsagePrice{
			// Flagship standard models.
			"gpt-5.5":      {InputMicrousdPerMillion: 5_000_000, CachedInputMicrousdPerMillion: 500_000, OutputMicrousdPerMillion: 30_000_000, LongInputMicrousdPerMillion: 10_000_000, LongCachedInputMicrousdPerMillion: 1_000_000, LongOutputMicrousdPerMillion: 45_000_000},
			"gpt-5.4":      {InputMicrousdPerMillion: 2_500_000, CachedInputMicrousdPerMillion: 250_000, OutputMicrousdPerMillion: 15_000_000, LongInputMicrousdPerMillion: 5_000_000, LongCachedInputMicrousdPerMillion: 500_000, LongOutputMicrousdPerMillion: 22_500_000},
			"gpt-5.4-mini": {InputMicrousdPerMillion: 750_000, CachedInputMicrousdPerMillion: 75_000, OutputMicrousdPerMillion: 4_500_000},
			"gpt-5.4-nano": {InputMicrousdPerMillion: 200_000, CachedInputMicrousdPerMillion: 20_000, OutputMicrousdPerMillion: 1_250_000},
			"gpt-5.2":      {InputMicrousdPerMillion: 1_750_000, CachedInputMicrousdPerMillion: 175_000, OutputMicrousdPerMillion: 14_000_000},
			"gpt-5.1":      {InputMicrousdPerMillion: 1_250_000, CachedInputMicrousdPerMillion: 125_000, OutputMicrousdPerMillion: 10_000_000},
			"gpt-5":        {InputMicrousdPerMillion: 1_250_000, CachedInputMicrousdPerMillion: 125_000, OutputMicrousdPerMillion: 10_000_000},
			"gpt-5-mini":   {InputMicrousdPerMillion: 250_000, CachedInputMicrousdPerMillion: 25_000, OutputMicrousdPerMillion: 2_000_000},
			"gpt-5-nano":   {InputMicrousdPerMillion: 50_000, CachedInputMicrousdPerMillion: 5_000, OutputMicrousdPerMillion: 400_000},
			"gpt-5-pro":    {InputMicrousdPerMillion: 15_000_000, CachedInputMicrousdPerMillion: 0, OutputMicrousdPerMillion: 120_000_000, LongInputMicrousdPerMillion: 30_000_000, LongCachedInputMicrousdPerMillion: 0, LongOutputMicrousdPerMillion: 240_000_000},
			"gpt-4.1":      {InputMicrousdPerMillion: 2_000_000, CachedInputMicrousdPerMillion: 500_000, OutputMicrousdPerMillion: 8_000_000},
			"gpt-4.1-mini": {InputMicrousdPerMillion: 400_000, CachedInputMicrousdPerMillion: 100_000, OutputMicrousdPerMillion: 1_600_000},
			"gpt-4.1-nano": {InputMicrousdPerMillion: 100_000, CachedInputMicrousdPerMillion: 25_000, OutputMicrousdPerMillion: 400_000},
			"gpt-4o":       {InputMicrousdPerMillion: 2_500_000, CachedInputMicrousdPerMillion: 1_250_000, OutputMicrousdPerMillion: 10_000_000},
			"gpt-4o-mini":  {InputMicrousdPerMillion: 150_000, CachedInputMicrousdPerMillion: 75_000, OutputMicrousdPerMillion: 600_000},

			"gpt-5.5-pro": {InputMicrousdPerMillion: 30_000_000, CachedInputMicrousdPerMillion: 0, OutputMicrousdPerMillion: 180_000_000, LongInputMicrousdPerMillion: 60_000_000, LongCachedInputMicrousdPerMillion: 0, LongOutputMicrousdPerMillion: 270_000_000},
			"gpt-5.4-pro": {InputMicrousdPerMillion: 30_000_000, CachedInputMicrousdPerMillion: 0, OutputMicrousdPerMillion: 180_000_000, LongInputMicrousdPerMillion: 60_000_000, LongCachedInputMicrousdPerMillion: 0, LongOutputMicrousdPerMillion: 270_000_000},

			// Specialized – ChatGPT.
			"gpt-5.3-chat-latest": {InputMicrousdPerMillion: 1_750_000, CachedInputMicrousdPerMillion: 175_000, OutputMicrousdPerMillion: 14_000_000},
			"chat-latest":         {InputMicrousdPerMillion: 5_000_000, CachedInputMicrousdPerMillion: 500_000, OutputMicrousdPerMillion: 30_000_000},

			// Specialized – Codex.
			"gpt-5.3-codex": {InputMicrousdPerMillion: 1_750_000, CachedInputMicrousdPerMillion: 175_000, OutputMicrousdPerMillion: 14_000_000},
		},
	}
}

// contextAnnotationRe matches trailing context annotations in model names such
// as " (<272K context length)".
var contextAnnotationRe = regexp.MustCompile(`\s*\(<\d+[KM] context length\)\s*$`)

// pricingRowRe matches Astro verbose JSON pricing rows of the form:
//
//	[1,[[0,"model-name"],[0,<input>],[0,<cached>],[0,<output>]]
//
// Each price field may be a number, null, "", or "-".
var pricingRowRe = regexp.MustCompile(`\[1,\[\[0,"([^"]+)"\],\[0,([^\]]+)\],\[0,([^\]]+)\],\[0,([^\]]+)\]\]`)
var standardTextTokenPropsRe = regexp.MustCompile(`TextTokenPricingTables"[^>]*props="([^"]*&quot;tier&quot;:\[0,&quot;standard&quot;][^"]*)"`)

// ssrStandardPaneRe isolates the rendered Standard content-switcher pane.
// The captured content stops at the next pane div or at end-of-string,
// so it works whether Standard is the last pane or not.
var ssrStandardPaneRe = regexp.MustCompile(`(?s)<div[^>]*\bdata-content-switcher-pane="true"[^>]*\bdata-value="standard"[^>]*>(.*?)(?:<div[^>]*\bdata-content-switcher-pane="true"|\z)`)

// ssrTrRe matches <tr>...</tr> elements.
var ssrTrRe = regexp.MustCompile(`(?s)<tr[^>]*>(.*?)</tr>`)

// ssrTdRe matches <td>...</td> cells.
var ssrTdRe = regexp.MustCompile(`(?s)<td[^>]*>(.*?)</td>`)

// htmlTagRe strips HTML tags from extracted cell text.
var htmlTagRe = regexp.MustCompile(`<[^>]*>`)

// parseOfficialStandardPricing extracts compatible Standard token-pricing rows
// from the official pricing page HTML body.
func parseOfficialStandardPricing(body string) (map[string]UsagePrice, error) {
	sections := officialStandardPricingSections(body)
	if len(sections) == 0 {
		sections = []string{body}
	}

	models := map[string]UsagePrice{}
	for _, section := range sections {
		parsePricingRowsInto(models, section)
	}

	// Parse SSR-rendered Short/Long context rows from the Standard pane.
	parseSSRShortLongInto(models, body)

	if len(models) == 0 {
		return nil, ErrInvalidInput
	}

	return models, nil
}

func officialStandardPricingSections(body string) []string {
	sections := []string{}
	for _, match := range standardTextTokenPropsRe.FindAllStringSubmatch(body, -1) {
		if len(match) > 1 {
			sections = append(sections, html.UnescapeString(match[1]))
		}
	}
	return sections
}

// parseSSRShortLongInto isolates the standard content-switcher pane and
// parses SSR-rendered Short/Long context pricing rows (7 <td> cells each).
// Long-context fields are merged into models already populated by 4-value
// props parsing; new entries are created for first-time models.
func parseSSRShortLongInto(models map[string]UsagePrice, body string) {
	for _, match := range ssrStandardPaneRe.FindAllStringSubmatch(body, -1) {
		if len(match) <= 1 {
			continue
		}
		pane := match[1]
		trs := ssrTrRe.FindAllStringSubmatch(pane, -1)
		for _, tr := range trs {
			if len(tr) <= 1 {
				continue
			}
			cells := ssrTdRe.FindAllStringSubmatch(tr[1], -1)
			if len(cells) != 7 {
				continue
			}
			rawModel := htmlTagRe.ReplaceAllString(cells[0][1], "")
			rawModel = strings.TrimSpace(rawModel)

			model := contextAnnotationRe.ReplaceAllString(rawModel, "")
			model = strings.TrimSpace(model)
			if model == "" || len(model) > maxModelNameLen {
				continue
			}

			cleanCell := func(i int) string {
				s := htmlTagRe.ReplaceAllString(cells[i][1], "")
				return strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(s), "$"))
			}

			sInput, ok := parseDollarPrice(cleanCell(1))
			if !ok {
				continue
			}
			sOutput, ok := parseDollarPrice(cleanCell(3))
			if !ok {
				continue
			}
			sCached, _ := parseDollarPrice(cleanCell(2))

			lInput, _ := parseDollarPrice(cleanCell(4))
			lCached, _ := parseDollarPrice(cleanCell(5))
			lOutput, _ := parseDollarPrice(cleanCell(6))

			if existing, exists := models[model]; exists {
				// Merge: keep existing short fields, update long fields.
				existing.LongInputMicrousdPerMillion = lInput
				existing.LongCachedInputMicrousdPerMillion = lCached
				existing.LongOutputMicrousdPerMillion = lOutput
				models[model] = existing
			} else {
				models[model] = UsagePrice{
					InputMicrousdPerMillion:           sInput,
					CachedInputMicrousdPerMillion:     sCached,
					OutputMicrousdPerMillion:          sOutput,
					LongInputMicrousdPerMillion:       lInput,
					LongCachedInputMicrousdPerMillion: lCached,
					LongOutputMicrousdPerMillion:      lOutput,
				}
			}
		}
	}
}

func parsePricingRowsInto(models map[string]UsagePrice, body string) {
	body = html.UnescapeString(body)
	matches := pricingRowRe.FindAllStringSubmatch(body, -1)

	for _, m := range matches {
		rawModel := strings.TrimSpace(m[1])
		rawInput := strings.TrimSpace(m[2])
		rawCached := strings.TrimSpace(m[3])
		rawOutput := strings.TrimSpace(m[4])

		// Strip context annotations like " (<272K context length)".
		model := contextAnnotationRe.ReplaceAllString(rawModel, "")
		model = strings.TrimSpace(model)
		if model == "" || len(model) > maxModelNameLen {
			continue
		}

		input, ok := parseDollarPrice(rawInput)
		if !ok {
			continue
		}
		output, ok := parseDollarPrice(rawOutput)
		if !ok {
			continue
		}

		// Missing cached input is treated as 0.
		cached, _ := parseDollarPrice(rawCached)

		// Deduplicate – first parsed row wins.
		if _, exists := models[model]; exists {
			continue
		}

		models[model] = UsagePrice{
			InputMicrousdPerMillion:       input,
			CachedInputMicrousdPerMillion: cached,
			OutputMicrousdPerMillion:      output,
		}
	}
}

// parseDollarPrice converts a dollar string/number from the pricing page into
// micro-USD per 1M tokens. Recognised formats: "5", "5.00", "0.5", "0.075".
// Returns false for null, "", "-", or unparseable values.
func parseDollarPrice(raw string) (int64, bool) {
	raw = strings.TrimSpace(raw)
	if raw == "" || raw == "null" || raw == "-" || raw == `""` || raw == `"-"` {
		return 0, false
	}
	// Strip any wrapping JSON string quotes.
	raw = strings.Trim(raw, `"`)
	if raw == "" || raw == "null" || raw == "-" {
		return 0, false
	}
	f, err := strconv.ParseFloat(raw, 64)
	if err != nil {
		return 0, false
	}
	if f < 0 {
		return 0, false
	}
	// Dollars per 1M tokens → micro-USD per 1M tokens.
	microusd := int64(math.Round(f * 1_000_000))
	return microusd, true
}

// SyncOfficialUsagePricing fetches the official OpenAI pricing page, parses
// compatible Standard token-pricing rows, saves them, and returns the stored
// pricing plus a summary.
func (s *Service) SyncOfficialUsagePricing(ctx context.Context) (UsagePricing, UsagePricingSyncSummary, error) {
	if s.pricingFetcher == nil {
		s.pricingFetcher = NewHTTPPricingFetcher(30 * time.Second)
	}

	body, err := s.pricingFetcher.Fetch(ctx)
	if err != nil {
		return UsagePricing{}, UsagePricingSyncSummary{}, fmt.Errorf("fetch official pricing: %w", err)
	}

	models, err := parseOfficialStandardPricing(string(body))
	if err != nil {
		return UsagePricing{}, UsagePricingSyncSummary{}, err
	}

	pricing := UsagePricing{
		Version:   1,
		Currency:  "USD",
		Unit:      "1M_tokens",
		UpdatedAt: time.Now().UTC(),
		Models:    models,
	}

	normalized, err := normalizeUsagePricing(pricing)
	if err != nil {
		return UsagePricing{}, UsagePricingSyncSummary{}, err
	}

	saved, err := s.repo.SaveUsagePricing(ctx, normalized)
	if err != nil {
		return UsagePricing{}, UsagePricingSyncSummary{}, fmt.Errorf("save official pricing: %w", err)
	}

	return saved, UsagePricingSyncSummary{
		Total:  len(saved.Models),
		Source: officialPricingURL,
	}, nil
}

// SetPricingFetcher replaces the pricing fetcher used by SyncOfficialUsagePricing.
// Pass nil to use the default HTTP fetcher with a 30 s timeout.
func (s *Service) SetPricingFetcher(f PricingFetcher) {
	if f == nil {
		f = NewHTTPPricingFetcher(30 * time.Second)
	}
	s.pricingFetcher = f
}

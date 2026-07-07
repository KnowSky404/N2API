# OpenAI Official Pricing Sync Design

## Summary

N2API already records request-log token usage and estimates historical cost from an editable admin pricing table. The current default table is incomplete: it only contains `gpt-5` with zero prices. This feature makes the default pricing table useful out of the box by seeding it with OpenAI official Standard token prices, and adds an explicit admin action to refresh that table from the current OpenAI pricing page.

This is separate from provider-account model sync. Provider-account sync discovers which models a configured upstream account can route. Official pricing sync updates the global usage-pricing table used by request logs and API-key cost budgets.

## Goals

- Replace the zero-price default with OpenAI official Standard token prices for models that fit N2API's token-cost schema: input, cached input, and output per 1M tokens.
- Add an admin-only one-click action on the Request Logs pricing panel to fetch the latest official OpenAI pricing page, parse supported text-token rows, save them as `UsagePricing`, and refresh the visible table.
- Include current flagship and relevant specialized token models from the official pricing page, including ChatGPT and Codex rows when they have compatible token prices.
- Keep the existing manual pricing editor and `PUT /api/admin/usage-pricing` behavior unchanged.
- Fail safely: a failed official sync must not replace the stored pricing table.

## Non-Goals

- No background scheduler.
- No automatic provider-account capability changes.
- No API-key or secret requirement for the official pricing-page sync.
- No billing, balance, recharge, or SaaS behavior.
- No modality-specific image/audio/video cost model. Rows that do not map cleanly to text-token input/cached-input/output prices are skipped.

## Official Source

Use `https://developers.openai.com/api/docs/pricing` as the source. The page states prices are per 1M tokens and exposes the Standard token pricing rows in server-rendered markup and Astro component props. N2API stores rates as micro-USD per 1M tokens, so a displayed `$5.00` price maps to `5_000_000`.

The parser should prefer Standard pricing and ignore Batch, Flex, and Priority panes because request logs do not record service tier.

## Backend Design

Add an admin service method:

```go
SyncOfficialUsagePricing(ctx context.Context) (UsagePricing, UsagePricingSyncSummary, error)
```

The method fetches the official pricing page with a short timeout, extracts compatible Standard token rows, normalizes them through the existing pricing validation, sets `UpdatedAt`, saves them through the existing repository, and returns the saved pricing plus a summary count.

Expose it through:

```http
POST /api/admin/usage-pricing/sync-official
```

Response:

```json
{
  "pricing": { "version": 1, "currency": "USD", "unit": "1M_tokens", "models": {} },
  "synced": { "total": 42, "source": "https://developers.openai.com/api/docs/pricing" }
}
```

Implementation notes:

- Add a small internal fetcher interface so tests can provide fixture HTML without network access.
- Parse only rows with a model id and numeric input/output prices. Treat missing cached input (`-`, empty, or null) as `0`.
- Strip context annotations such as `(<272K context length)` from model ids.
- Deduplicate by model id. If the same model appears in both flagship and specialized Standard rows, keep the first parsed row.
- Return `ErrInvalidInput` if the source page yields no compatible rows.

## Frontend Design

On `frontend/src/routes/request-logs/+page.svelte`, add a compact `Sync official` button beside the existing pricing actions.

States:

- Idle: `Sync official`.
- Loading: `Syncing`.
- Success: inline green message like `Synced official pricing for 42 models.`
- Failure: existing pricing error surface, without clearing edited rows.

The action should call a new `syncOfficialUsagePricing()` helper in `frontend/src/lib/admin-state.svelte.js`. On success it replaces `usagePricing.current` and `usagePricing.rows` with the returned pricing, similar to loading/saving.

## Testing

Backend:

- Default usage pricing includes official non-zero prices for representative models such as `gpt-5.5`, `gpt-5.4-mini`, `gpt-5`, `gpt-4.1`, `chat-latest`, and `gpt-5.3-codex`.
- Parser extracts Standard rows from an official-page-like fixture and converts dollars to micro-USD.
- Parser skips non-token or unsupported rows and treats absent cached input as zero.
- Sync endpoint requires admin, saves parsed pricing, returns summary, and maps invalid source data to `400 invalid_input`.

Frontend:

- Request Logs pricing panel exposes `Sync official`.
- `syncOfficialUsagePricing()` posts to `/api/admin/usage-pricing/sync-official`, updates rows/current pricing, and reports the synced count.
- A failed sync reports an inline error without deleting existing rows.

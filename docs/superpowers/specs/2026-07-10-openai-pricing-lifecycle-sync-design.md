# OpenAI Pricing Lifecycle Sync Design

## Summary

Replace the current destructive official-pricing import with an additive lifecycle-aware sync. N2API will combine three OpenAI documentation sources: the models catalog for public model membership and deprecated labels, the pricing page for compatible Standard token prices, and the deprecations page for shutdown dates and replacement models.

The sync updates existing official model prices and adds newly priced models without removing local rows. Models whose shutdown date has arrived are returned as deletion candidates, but deletion only happens through a separate, authenticated, user-confirmed bulk action.

## Goals

- Update prices for local models that have current compatible OpenAI Standard token prices.
- Add newly published models that have compatible token prices.
- Preserve local and manually added rows that are absent from the official pricing page.
- Show upcoming shutdowns without allowing deletion before the official shutdown date.
- Offer bulk deletion, with per-model selection, only on or after the shutdown date.
- Treat the three official documents as one consistency boundary: fetch and parse all three successfully before saving any pricing changes.
- Support the current pricing-page shape, including the added `Cache writes` column, without adding cache-write billing to N2API.

## Non-Goals

- Do not add image, audio, video, fine-tuning, per-call, or other non-compatible pricing dimensions.
- Do not infer shutdown from absence on the models or pricing pages.
- Do not change provider-account routing or account-specific `/v1/models` discovery.
- Do not add background synchronization or automatic deletion.
- Do not persist a separate lifecycle table in PostgreSQL. Lifecycle notices are refreshed from official sources during each explicit sync or deletion validation.

## Official Sources

### Models Catalog

`https://developers.openai.com/api/docs/models/all`

This is the public model catalog. It provides model identifiers and a `Deprecated` label. It is not an account-specific availability list and does not provide normalized token prices or shutdown dates.

### Pricing

`https://developers.openai.com/api/docs/pricing`

This provides Standard prices. N2API imports only rows that map to its current short- and long-context fields:

- input
- cached input
- output

The official `Cache writes` price is ignored because request logs do not record cache-write tokens. Batch, Flex, Priority, fine-tuning, image, audio, video, and per-call prices are excluded.

### Deprecations

`https://developers.openai.com/api/docs/deprecations`

This is authoritative for shutdown dates and recommended replacements. An absent pricing row or catalog entry is never treated as proof of shutdown.

Definitions:

- `deprecated`: officially announced for retirement, with a future or current shutdown date.
- `upcoming`: shutdown date is after the current UTC calendar date; display a warning only.
- `shutdown`: shutdown date is equal to or before the current UTC calendar date; eligible for user-confirmed deletion when the exact model exists in local pricing.

Entries about APIs, systems, fine-tuned model families, or non-model products are ignored unless an exact model identifier matches a local pricing row.

## Sync Data Flow

1. The authenticated admin confirms `Sync official`.
2. The backend fetches all three official pages with bounded HTTP requests.
3. It parses the catalog, Standard compatible pricing, and model deprecations independently.
4. It validates that every source produced a structurally valid, non-empty result. Any fetch or parse failure aborts without calling `SaveUsagePricing`.
5. It loads the current `UsagePricing` table.
6. It copies all current rows, overwrites prices for matching official-priced model identifiers, and adds missing official-priced model identifiers.
7. It saves the merged table once.
8. It returns counts and lifecycle notices:
   - added models
   - updated models
   - unchanged official-priced models
   - upcoming shutdowns
   - deletion candidates whose shutdown date has arrived and whose identifier exists locally
9. The frontend refreshes pricing. It shows upcoming warnings and opens the bulk deletion dialog when deletion candidates exist.

Models with compatible prices may be imported even if they are not present in the catalog only when they belong to an explicitly supported specialized Standard pricing group such as Codex or ChatGPT. This accommodates official specialized rows while keeping the pricing page authoritative for whether a row can be billed by N2API.

## Backend Contract

Retain the existing authenticated endpoint:

```http
POST /api/admin/usage-pricing/sync-official
```

The response keeps `pricing` and expands `synced`:

```json
{
  "pricing": {
    "version": 1,
    "currency": "USD",
    "unit": "1M_tokens",
    "models": {}
  },
  "synced": {
    "total": 64,
    "added": ["gpt-5.6-sol"],
    "updated": ["gpt-5.5"],
    "unchanged": 62,
    "upcomingShutdowns": [
      {
        "model": "gpt-5.3-chat-latest",
        "shutdownDate": "2026-08-10",
        "replacement": "gpt-5.5"
      }
    ],
    "deletionCandidates": [
      {
        "model": "chatgpt-4o-latest",
        "shutdownDate": "2026-02-17",
        "replacement": "gpt-5.1-chat-latest"
      }
    ],
    "sources": {
      "models": "https://developers.openai.com/api/docs/models/all",
      "pricing": "https://developers.openai.com/api/docs/pricing",
      "deprecations": "https://developers.openai.com/api/docs/deprecations"
    }
  }
}
```

Add a separate authenticated endpoint:

```http
POST /api/admin/usage-pricing/remove-shutdown
Content-Type: application/json

{
  "models": ["chatgpt-4o-latest"]
}
```

The deletion endpoint does not trust lifecycle data from the browser. It refetches and parses the deprecations page, compares shutdown dates using the current UTC date, and deletes only requested model identifiers that are confirmed shut down. If the official page cannot be fetched or parsed, no rows are removed. Unknown, duplicate, not-yet-shut-down, or no-longer-local identifiers are rejected as invalid input; the operation is all-or-nothing.

After validation, it loads the current pricing, removes the selected rows, saves once, and returns the saved pricing plus the removed model identifiers.

## Parser Design

Use small parser functions with explicit result types:

- model catalog parser: model identifier plus deprecated marker
- Standard pricing parser: model identifier plus compatible short/long token prices
- deprecations parser: exact model identifier, parsed date, replacement text

Pricing parsing supports both page shapes:

- four values: model, input, cached input, output
- five values: model, input, cached input, cache writes, output

SSR tables support both seven and nine data cells for short/long context. Cache-write cells are skipped. Missing cached input remains zero. Rows without numeric input and output are excluded.

Deprecation dates are normalized to `YYYY-MM-DD`. The parser accepts the official date formats currently present on the page, including ISO dates and English month-name dates, plus Unicode non-breaking hyphens. Entries without an exact parseable date are not deletion candidates. A structurally valid deprecations page must still contain at least one recognized model deprecation row.

## Frontend Design

The existing sync confirmation copy changes from replacement language to additive language: official compatible prices will be added or updated; local-only rows remain.

After a successful sync:

- Show the existing top-right notification with added and updated counts.
- If upcoming shutdowns exist, show a compact amber notice in the pricing section listing model, shutdown date, and replacement. It has no delete action.
- If deletion candidates exist, open one modal containing a checkbox list. All candidates are selected initially, and the user may deselect any row.
- The destructive button states the selected count, for example `Remove 3 models`.
- Submitting calls `remove-shutdown`; cancellation or deselection does not mutate pricing.
- On success, reload pricing and show a top-right removal notification.
- On failure, keep the modal selection and show the existing pricing error surface.

The existing per-row manual `Remove` action remains unchanged and retains its local popconfirm behavior.

## Error Handling

- Any source fetch failure, non-200 response, oversized response, or parser validation failure aborts sync before persistence.
- A failed merge normalization or database save leaves the prior table intact.
- A failed shutdown revalidation removes nothing.
- The API returns `400 invalid_input` for invalid source content or invalid deletion requests, and `500 internal_error` for fetch or persistence failures, matching existing conventions.
- Frontend controls remain disabled under the pricing-local `thinking` overlay while sync or bulk removal is active.

## Testing

Backend tests cover:

- current four- and five-value pricing props
- current seven- and nine-cell SSR pricing rows
- model catalog identifiers and deprecated markers
- deprecation dates in ISO and English month formats
- additive merge behavior: update, add, preserve local-only, and never delete during sync
- failure atomicity when any one of the three sources is invalid
- upcoming versus shutdown classification at an injected UTC date boundary
- deletion revalidation, all-or-nothing validation, and save behavior
- authenticated HTTP response and request contracts

Frontend tests cover:

- additive confirmation copy
- added/updated success notification
- upcoming warning without delete controls
- bulk candidate modal, default selection, deselection, cancel, and confirmed removal request
- reload after successful removal and retained selection on failure

Final verification includes full Go tests, Bun tests, Svelte check, frontend build, Docker Compose rebuild/recreate, and container-local health smoke checks.

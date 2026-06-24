# API Key List Filtering Design

## Goal

Make API key management easier when a personal gateway has many client keys by adding local search and status filtering to the API Keys page.

## Context

Provider accounts already support search, sorting, selection, and bulk operations. API Keys now owns client access policy, per-key limits, runtime window visibility, and request-log deep links, but the table always renders every key. That makes it harder to find a specific client key by name, prefix, model policy, status, or current limiter state.

## Scope

- Add local search on the API Keys page.
- Add a status filter with `All keys`, `Active keys`, and `Revoked keys`.
- Filter by key name, prefix, model policy label, selected models, active/revoked status, and limiter-state labels such as concurrency/rate/token limit full.
- Show a compact `Showing X of Y` count above the table.
- Keep create, model-policy, limit, logs, and revoke behavior unchanged.

## Non-goals

- Backend query parameters for API key listing.
- Bulk API key operations.
- Restoring revoked keys.
- Public multi-tenant user management or billing features.

## UI Behavior

The controls live above the API key table, after any error or one-time-secret callout. Search is a plain local input. Status filtering is a select. If no rows match, the table shows `No API keys match your filters.` while preserving the existing empty-state message when no keys exist at all.

## Data Flow

Filtering is derived from `apiKeys.items` in the Svelte page. It does not mutate key records or change network requests. The filtered list is used only by the rendered table.

## Verification

- Frontend source test proves the controls, derived filtered list, status filter values, count, and filtered empty state exist.
- `bun test src/routes/navigation.test.mjs`
- `bun run check`
- `bun run build`

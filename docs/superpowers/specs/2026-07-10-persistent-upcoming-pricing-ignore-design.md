# Persistent Upcoming Pricing Ignore Design

## Summary

The Pricing panel currently renders upcoming model shutdowns as a persistent amber notice after an official pricing sync. Replace that notice with a compact warning-icon action immediately to the left of `Sync official`. The action opens a modal that shows the same lifecycle details and can remove every listed upcoming-shutdown model from the current pricing table in one operation.

Removal is persistent. N2API records the model identifiers in the existing `usage_pricing` settings document and excludes them from later official pricing syncs. A later explicit `Add model` save for the same identifier restores that model by removing it from the ignore list.

This design supersedes the upcoming-shutdown UI and the "no deletion before the official shutdown date" behavior in `2026-07-10-openai-pricing-lifecycle-sync-design.md`. The existing confirmed-removal workflow for models whose shutdown date has already arrived remains unchanged.

## Goals

- Remove the persistent upcoming-shutdown notice from the Pricing panel.
- Show a compact `TriangleAlert` icon with a count badge immediately before `Sync official` when upcoming shutdowns are present.
- Open an accessible modal containing the affected model, shutdown date, and replacement details.
- Remove all listed upcoming-shutdown models from current pricing with one confirmation.
- Persist ignored model identifiers so later official syncs do not re-add them.
- Treat a later manual add of an exact ignored model identifier as an explicit restore.
- Keep pricing mutation, usage-summary refresh, loading state, and top-right success feedback consistent with the existing panel.

## Non-Goals

- No general ignored-model management page.
- No per-model selection inside the upcoming-shutdown modal.
- No database migration or new table.
- No change to provider-account model availability or routing.
- No change to the existing `Review shutdowns` flow for models already past their shutdown date.
- No automatic removal before the user confirms the modal action.

## Data Model

Extend `UsagePricing` with a normalized, sorted set of exact model identifiers:

```go
IgnoredModels []string `json:"ignoredModels,omitempty"`
```

The field is stored in the existing PostgreSQL `settings.value` JSON document under `usage_pricing`, so existing rows remain backward compatible and no schema migration is required.

Normalization trims identifiers, rejects blanks, overlong identifiers, and duplicates, sorts the result, and maintains the invariant that an identifier cannot be present in both `Models` and `IgnoredModels`. Internal pricing reconstruction paths, including official sync and confirmed shutdown removal, must preserve the ignore list.

## Backend Behavior

Add an admin service operation and authenticated endpoint:

```go
IgnoreUpcomingUsagePricing(ctx context.Context, models []string) (UsagePricing, []string, error)
```

```http
POST /api/admin/usage-pricing/ignore-upcoming
Content-Type: application/json

{"models":["gpt-5.3-chat-latest"]}
```

The operation validates all requested identifiers before saving:

1. The request is non-empty, unique, and contains valid exact model identifiers.
2. The official deprecations document can be fetched and parsed.
3. Every requested model exists in the current pricing table.
4. Every requested model has a shutdown date after the current UTC calendar date.

After validation, it copies the current model map and ignore list, removes all requested models from `Models`, adds them to `IgnoredModels`, normalizes once, and saves once. The response returns the saved pricing and a sorted `ignored` identifier list. Any validation, fetch, parse, or persistence failure leaves pricing unchanged.

`SyncOfficialUsagePricing` must skip identifiers in `IgnoredModels` before calculating `added`, `updated`, or `upcomingShutdowns`. It preserves the ignore list in the saved pricing document, so ignored identifiers do not reappear in the table or warning modal.

`UpdateUsagePricing` preserves the current server-side ignore list because the frontend pricing editor does not own that field. If the submitted `Models` map explicitly contains an ignored identifier, the service removes that identifier from `IgnoredModels` before saving. This makes `Add model` the explicit restore path without adding another UI.

## Frontend Behavior

The Pricing header action order is:

1. Upcoming-shutdown warning icon, only when `usagePricing.upcomingShutdowns` is non-empty.
2. `Sync official`.
3. `Add model`.

The warning action uses Lucide `TriangleAlert`, an amber semantic treatment, an accessible label, a tooltip, and a stable square icon-button size. A small count badge reports the number of affected models. The existing full-width `Upcoming shutdowns` notice is removed.

Clicking the warning icon opens a centered modal that:

- identifies the operation as `Upcoming model shutdowns`;
- lists each model, shutdown date, and replacement when available;
- provides `Cancel` and `Remove N models` actions;
- closes on backdrop click or Escape when idle;
- disables dismissal and actions while the request is running.

The removal action posts all currently listed upcoming model identifiers to `ignore-upcoming`. On success it closes the modal, clears stale upcoming entries, reloads pricing and the current usage summary, and shows the existing top-right notification style with the removed count. The Pricing section keeps the existing `LoaderCircle` plus exact `thinking` loading treatment. Failure uses the existing pricing error surface and keeps the modal open for review or retry.

The existing `Review shutdowns` button and modal remain separate because already-shut-down deletion has different validation and does not need persistent ignore state.

## Error Handling And Concurrency

- The backend never trusts lifecycle dates supplied by the browser; it refetches and validates official deprecation data.
- Batch validation is atomic. One invalid, missing, duplicate, no-longer-local, or already-shut-down identifier rejects the full request.
- The frontend submits the identifiers from the currently displayed modal. If state changed concurrently, the backend rejection prevents partial removal and the existing error surface explains that the operation failed.
- Official sync failures do not alter the stored pricing or ignore list.
- Manual pricing saves preserve ignored identifiers unless the submitted model map explicitly restores an exact identifier.

## Testing

Backend service tests cover:

- upcoming models are removed and added to `IgnoredModels` in one save;
- invalid mixed batches and official-source failures save nothing;
- official sync skips ignored models and excludes them from summaries;
- ignored identifiers survive unrelated pricing edits and shutdown removals;
- manually adding an ignored identifier removes it from `IgnoredModels`;
- normalization rejects invalid ignore identifiers and removes model/ignore overlap through the explicit restore path.

HTTP tests cover authentication, request decoding, successful response shape, and invalid-input mapping for `POST /api/admin/usage-pricing/ignore-upcoming`.

Frontend source-contract tests cover the `TriangleAlert` import, icon placement before `Sync official`, count badge, accessible modal, one-action request, refresh behavior, top-right success feedback, and removal of the persistent upcoming-shutdown notice.

Final verification uses focused backend and frontend tests, `go test ./...`, `bun test`, `bun run check`, `bun run build`, Docker Compose rebuild/recreate, and container-local health and page smoke checks.

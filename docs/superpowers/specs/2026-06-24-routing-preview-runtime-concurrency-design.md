# Routing Preview Runtime Concurrency Design

## Goal

Show process-local provider account concurrency in the Routing diagnostics preview so the operator can see when a scheduler candidate is currently busy or at its effective account concurrency cap.

## Context

N2API already exposes routing preview candidates with scheduler order, sticky-session markers, excluded-account diagnostics, account health, and model capability reasons. It also exposes active provider account concurrency on the Provider accounts page.

The remaining diagnostic gap is that Routing diagnostics cannot explain runtime account pressure. sub2api's scheduling workflow emphasizes seeing why accounts are or are not usable. N2API should add the personal-gateway equivalent without changing the actual scheduler or introducing distributed state.

## Scope

In scope:

- Enrich `GET /api/admin/model-routing/preview` candidate JSON with:
  - `currentConcurrentRequests`
  - `effectiveMaxConcurrentRequests`
  - `concurrencyBlocked`
- Compute effective max the same way as Provider accounts:
  - account override when greater than `0`;
  - otherwise Gateway Settings `maxConcurrentRequestsPerAccount`;
  - `0` means unlimited.
- Mark `concurrencyBlocked` when the effective limit is greater than `0` and current active concurrency is greater than or equal to the limit.
- Render the preview chip with `Active current / limit`.
- Preserve existing provider-service preview behavior and scheduler selection. This is admin diagnostic enrichment only.

Out of scope:

- Changing account selection or sticky binding behavior.
- Adding distributed concurrency state.
- Adding historical concurrency charts.
- Showing per-client or per-key runtime concurrency in this slice.

## Backend Behavior

The provider service keeps returning the static `SelectionPreview`. The HTTP admin layer already has optional access to the gateway concurrency snapshot. The preview route should load gateway settings, read the snapshot, and map candidate rows into an enriched response type.

When no gateway snapshot provider is present, current counts default to `0`.

## Frontend Behavior

The models page Routing preview candidate chips should show a compact runtime line:

- `Active 0 / unlimited`
- `Active 1 / 3`

If a candidate is at its effective cap, add a `Concurrency full` marker to the chip. This marker is informational. It does not remove the candidate from the preview because the preview still reports the provider service scheduler order for the submitted model/session/exclusion inputs.

## Verification

Required gates:

- HTTP test proves preview candidates include current/effective concurrency and blocked state.
- Frontend source test proves the models page renders active concurrency and full markers.
- Documentation test proves README/deploy notes mention Routing preview active concurrency.
- `go test ./internal/httpapi -run ModelRoutingPreview`
- `bun test src/routes/navigation.test.mjs`
- `bun run check`
- `bun run build`

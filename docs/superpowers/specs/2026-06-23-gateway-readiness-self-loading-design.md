# Gateway Readiness Self Loading Design

## Goal

Make the Gateway management page load the data it needs to report readiness when opened directly.

## Context

The Gateway page shows readiness counts for provider accounts, schedulable accounts, routable models, and active API keys. Those values are derived from shared frontend state:

- `providerAccounts.items`
- `modelRouting.models`
- `apiKeys.items`
- `gatewaySettings.data`

The page currently loads gateway settings and usage summaries, but it does not request provider accounts, model routing, or API keys itself. If an admin opens `/gateway` before visiting the other pages, the readiness section can show incomplete counts and misleading missing-prerequisite warnings.

sub2api treats gateway management as an operational dashboard. For N2API V1, the equivalent page should be self-contained enough to answer "can this gateway route traffic right now?" without relying on navigation order.

## Scope

In scope:

- Update the Gateway page to request provider accounts, model routing, and API keys on first authenticated load.
- Keep using the existing shared state loaders and readiness helpers.
- Add source-level frontend tests proving the page owns these loads.
- Document that Gateway management refreshes readiness inputs directly.

Out of scope:

- New backend readiness endpoint.
- New database schema.
- New scheduling algorithm.
- Live polling or background refresh.
- Runtime Compose deployment.

## Frontend Behavior

On first authenticated render of `/gateway`, the page should call:

- `loadGatewaySettings()`
- `loadProviderAccounts()`
- `loadModelRouting()`
- `loadAPIKeys()`
- `loadUsageSummary('24h', ...)` for the existing usage panels

The existing `gatewayRequested` guard remains, so these requests run once per authenticated page lifecycle and reset after logout.

Readiness calculations stay in `admin-state.svelte.js`:

- `getSchedulableProviderAccounts()`
- `getRoutableModelCount()`
- `getActiveKeys()`
- `getGatewayReadinessIssues(...)`

## Testing

Frontend source tests should assert:

- `frontend/src/routes/gateway/+page.svelte` imports `loadProviderAccounts`.
- The page imports `loadModelRouting`.
- The page imports `loadAPIKeys`.
- The authenticated load effect calls all three functions.

Documentation tests should assert README and deploy README mention Gateway management refreshes provider accounts, model routing, and API keys for readiness.

## Validation

Run:

- `cd frontend && bun test src/routes/navigation.test.mjs`
- `cd frontend && bun run check`
- `cd frontend && bun run build`
- `cd backend && GOMODCACHE=/root/Clouds/N2API/.cache/go-mod GOCACHE=/root/Clouds/N2API/.cache/go-build go test ./internal/gateway -run GatewayDocumentation`
- Full backend tests before final completion.

# Provider Account Concurrency Visibility Design

## Goal

Show current per-provider-account gateway concurrency in the admin account management surface so the operator can see which account is actively occupied and how that compares with its effective concurrency limit.

## Context

N2API now has:

- global gateway concurrency limits;
- global per-account concurrency defaults in Gateway Settings;
- provider-account **Max concurrency** overrides;
- gateway fallback when a selected account is at its local concurrency cap.

The missing operational signal is the runtime count. sub2api-style account management shows both configured scheduling controls and current load. N2API should expose the useful personal-gateway equivalent without adding Redis, multi-node aggregation, users, billing, or platform quota accounting.

## Scope

In scope:

- Add an in-process gateway concurrency snapshot for active provider account slots.
- Expose current active count and effective limit per provider account through the admin provider-account list response.
- Show a compact **Active / limit** readout beside **Max concurrency** on the Provider accounts page.
- Treat account-level override `0` as inheriting `GatewaySettings.MaxConcurrentRequestsPerAccount`.
- Document that the value is process-local runtime state.

Out of scope:

- Distributed concurrency state.
- Per-client/user current concurrency displays.
- Historical concurrency charts.
- Prometheus or metrics exporter.
- Changing limiter behavior or scheduler selection rules.

## Backend Behavior

`accountConcurrencyLimiter` should expose a read-only snapshot:

- key: provider account id
- value: active request count

The snapshot must be protected by the existing mutex and must not allow callers to mutate limiter state.

The admin HTTP API should enrich provider account responses with:

- `currentConcurrentRequests`
- `effectiveMaxConcurrentRequests`

The effective max is:

- account `maxConcurrentRequests` when greater than `0`;
- otherwise current Gateway Settings `maxConcurrentRequestsPerAccount`;
- `0` means unlimited.

This enrichment should be best-effort only for the live gateway process. If no gateway metrics provider is wired, counts default to `0` and effective max is still computed from settings/account values.

## Frontend Behavior

Provider accounts table keeps the existing editable **Max concurrency** input and adds a nearby readout:

- `Active 0 / inherited`
- `Active 1 / 3`
- `Active 2 / unlimited`

The readout is informational. Editing **Max concurrency** remains the only control in that cell.

## Documentation

README and deploy notes should state:

- Provider accounts show active concurrency next to the configured max.
- The count is process-local and resets when the service restarts.
- `0` inherits the gateway default, and an effective limit of `0` is displayed as unlimited.

## Verification

Required gates:

- gateway limiter unit test proves snapshot reports counts and is immutable;
- HTTP admin test proves account list includes current and effective concurrency fields;
- frontend source test proves Provider accounts renders the active/limit readout;
- documentation test proves README/deploy notes mention process-local active concurrency;
- `go test ./internal/gateway ./internal/httpapi`;
- `bun test src/routes/providers/provider-page.test.mjs`;
- `bun run check`;
- `bun run build`.

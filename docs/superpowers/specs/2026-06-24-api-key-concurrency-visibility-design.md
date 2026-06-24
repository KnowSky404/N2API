# API Key Concurrency Visibility Design

## Goal

Show current per-API-key gateway concurrency in the API Keys management surface so the operator can see whether a client key is actively using slots or has reached its effective key concurrency cap.

## Context

N2API already supports:

- global per-key concurrency through Gateway Settings;
- per-key request and token minute limits;
- per-provider-account active concurrency visibility;
- routing preview active account concurrency diagnostics.

The missing runtime signal is the API key side of the same gateway guardrail. sub2api's useful personal-gateway behavior includes understanding which client or user is constrained by runtime limits. In N2API V1, the equivalent entity is the client API key.

## Scope

In scope:

- Add an in-process API key concurrency snapshot for active key slots.
- Expose current active count and effective per-key concurrency limit through `GET /api/admin/keys`.
- Show a compact **Active / limit** readout in the API Keys page.
- Treat `GatewaySettings.maxConcurrentRequestsPerKey` value `0` as unlimited.
- Mark a key as full when the effective limit is greater than `0` and current active requests are greater than or equal to the limit.
- Document that the value is process-local runtime state.

Out of scope:

- Per-key request/token minute usage window introspection.
- Historical concurrency charts.
- Distributed concurrency state.
- New per-key concurrency override fields. N2API currently has per-key request/token overrides, but per-key concurrency remains a gateway default.
- Changing gateway limiter behavior.

## Backend Behavior

The gateway key concurrency limiter should expose a read-only snapshot:

- key: API key id
- value: active request count

The snapshot must be protected by the existing mutex and callers must not be able to mutate limiter state.

The admin HTTP API should enrich key responses with:

- `currentConcurrentRequests`
- `effectiveMaxConcurrentRequests`
- `concurrencyBlocked`

The effective max is `GatewaySettings.maxConcurrentRequestsPerKey`. `0` means unlimited.

If no gateway metrics provider is wired, counts default to `0`.

## Frontend Behavior

The API Keys page should keep the existing request/token limit controls and show an informational readout near each key's limits:

- `Active 0 / unlimited`
- `Active 1 / 3`

When `concurrencyBlocked` is true, the row shows a compact **Concurrency full** marker.

## Verification

Required gates:

- gateway limiter unit test proves key snapshot reports counts and is immutable;
- HTTP admin test proves key list includes current/effective concurrency fields and blocked state;
- frontend source test proves API Keys renders the active/limit readout and full marker;
- documentation test proves README/deploy notes mention process-local API key active concurrency;
- `go test ./internal/gateway -run 'APIKeyConcurrency|KeyConcurrency'`;
- `go test ./internal/httpapi -run APIKey`;
- `bun test src/routes/navigation.test.mjs`;
- `bun run check`;
- `bun run build`.

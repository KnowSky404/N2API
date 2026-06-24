# API Key Rate Window Visibility Design

## Goal

Show current per-API-key request and token minute-window usage in the API Keys management surface so the operator can see which client key is close to, or already at, its effective gateway rate guardrail.

## Context

N2API already enforces:

- per-key requests per minute through the gateway request limiter;
- per-key tokens per minute through the gateway token limiter;
- per-key concurrency through a process-local limiter;
- API-key concurrency visibility in the admin API and API Keys page.

The missing runtime signal is the current fixed-minute request/token window usage. This mirrors the useful personal-gateway diagnostic direction from sub2api without copying its implementation or adding SaaS-style accounting.

## Scope

In scope:

- Add read-only process-local snapshots for API-key request and token minute-window counts.
- Expose current window count, effective limit, remaining capacity, and blocked state through `GET /api/admin/keys`.
- Show compact request-window and token-window readouts in the API Keys page.
- Treat effective limit `0` as unlimited.
- Mark a window as full only when the effective limit is greater than `0` and current usage is greater than or equal to that limit.
- Document that these counters are process-local, reset on the next fixed minute, and reset on restart.

Out of scope:

- Persistent rate-window history.
- Distributed or multi-replica counters.
- Retry-after countdown display.
- Changing limiter enforcement semantics.
- Adding new per-key limit fields.

## Backend Behavior

The gateway request limiter exposes a snapshot map:

- key: API key id
- value: current request count in the active fixed-minute window

The gateway token limiter exposes a snapshot map:

- key: API key id
- value: current token count in the active fixed-minute window

Both snapshots must:

- be protected by the limiter mutex;
- omit stale windows from prior minutes;
- return copies so callers cannot mutate limiter state.

The admin HTTP API enriches each key response with:

- `currentRequestsThisMinute`
- `effectiveRequestsPerMinute`
- `requestRateRemaining`
- `requestRateLimited`
- `currentTokensThisMinute`
- `effectiveTokensPerMinute`
- `tokenRateRemaining`
- `tokenRateLimited`

Effective limits prefer per-key overrides. When the per-key value is `0`, the gateway default applies. When the effective value is `0`, the window is unlimited and remaining capacity is reported as `0`.

## Frontend Behavior

The API Keys page keeps the existing limit inputs and active-concurrency readout. It adds two compact runtime lines near each key's limit controls:

- `Requests window 3 / 60`
- `Tokens window 1024 / unlimited`

When a limited window is full, the row shows a compact marker:

- `Request limit full`
- `Token limit full`

## Verification

Required gates:

- gateway limiter unit tests prove request and token snapshots report active-window counts and ignore stale windows;
- HTTP admin test proves key list includes current/effective/remaining/blocked request and token window fields;
- frontend source test proves API Keys renders the new readouts and blocked markers;
- documentation test proves rate-window visibility is documented as process-local fixed-minute state;
- `go test ./internal/gateway -run 'APIKey.*Rate.*Snapshot|APIKey.*Token.*Snapshot|APIKey.*Rate|APIKey.*Token'`;
- `go test ./internal/httpapi -run 'ListAPIKeys|APIKey'`;
- `bun test src/routes/navigation.test.mjs src/routes/providers/provider-page.test.mjs`;
- `bun run check`;
- `bun run build`.

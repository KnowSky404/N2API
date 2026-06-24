# Routing Pool Fallback Design

## Summary

N2API should add explicit fallback chains for routing pools. A pool-bound API key should first schedule inside its configured pool, then move through configured fallback pools only when the current pool cannot provide a usable account before streaming starts.

This is the next personal-use slice of sub2api's group scheduling model. The reference project has group-level fallback fields such as `fallback_group_id` and `fallback_group_id_on_invalid_request`, plus scheduler/proxy fallback code with loop detection. N2API should keep only the scheduling reliability part and continue to exclude public users, subscriptions, balances, payments, reseller accounting, and platform billing semantics.

## Current N2API Baseline

N2API already has:

- unified provider accounts for Codex OAuth and API-upstream exits.
- account health, rate-limit, circuit-open, pause, priority, load factor, concurrency, test status, and manual model capability.
- API keys with model policy, per-key limits, budgets, disabled/revoked state, and optional `routing_pool_id`.
- routing pools with account memberships and membership priority.
- pool-scoped provider selection and pool-scoped sticky session bindings.
- request logs with routing pool id/name snapshots and fallback attempt counters.

The current routing pool contract intentionally treats pool membership as a hard boundary: a pool-bound key never falls back to global accounts. That must remain true. The new feature adds only admin-configured pool-to-pool fallback.

## Goals

- Let a routing pool optionally point to one fallback routing pool.
- Let fallback form a chain, for example `primary -> secondary -> emergency`.
- Preserve hard isolation: pool-bound keys can use only their bound pool and its explicit fallback chain.
- Keep unbound API keys unchanged: they use the global provider account pool and never use routing pool fallback.
- Fail closed when a fallback chain is missing, disabled, cyclic, or exhausted.
- Reuse the existing account filters in every pool: provider, enabled state, health, token expiry, model capability, excluded-account ids, and concurrency.
- Preserve pre-stream gateway fallback. A failed or busy account should be excluded when trying the same pool or the next fallback pool.
- Keep sticky sessions scoped to the actual pool used for a selected account.
- Add request-log diagnostics that distinguish configured fallback pool movement from ordinary same-pool account retry.
- Show fallback configuration and warnings in the Routing Pools admin page.
- Keep the feature PostgreSQL-only; do not require Redis.

## Non-Goals

- No fallback to the global provider account pool for pool-bound API keys.
- No fallback based on user balance, subscription plan, billing quota, USD limits, or rate multipliers.
- No invalid-request prompt/body rewriting fallback in this slice.
- No multi-branch fallback graph. Each pool has at most one fallback target.
- No automatic account migration between pools.
- No provider-platform expansion beyond the current provider abstraction.
- No copying source code from sub2api.

## Terminology

- **Primary pool**: the routing pool directly bound to an API key.
- **Fallback pool**: a routing pool referenced by another pool as its next scheduling target.
- **Fallback chain**: the ordered list starting at the primary pool and following `fallback_pool_id`.
- **Actual pool**: the pool that selected the provider account for a gateway request.
- **Fallback depth**: zero for the primary pool, one for the first fallback pool, and so on.

## Data Model

Add migration `00025_routing_pool_fallback.sql`.

Extend `routing_pools`:

- `fallback_pool_id BIGINT REFERENCES routing_pools(id) ON DELETE SET NULL`

Add an index:

- `routing_pools_fallback_pool_idx ON routing_pools (fallback_pool_id) WHERE fallback_pool_id IS NOT NULL`

Extend `request_logs`:

- `routing_pool_fallback_depth INTEGER NOT NULL DEFAULT 0`
- `routing_pool_fallback_chain TEXT NOT NULL DEFAULT ''`
- `routing_pool_error TEXT NOT NULL DEFAULT ''`

`routing_pool_id` and `routing_pool_name` should continue to mean the actual pool used for a selected account when an account is selected. If no account is selected, logs should keep the API key's primary pool id/name when available. The fallback chain field stores a compact admin-only snapshot such as `primary -> secondary -> emergency`. It is not returned to gateway clients.

## Admin DTOs

Extend `admin.RoutingPool`:

```go
FallbackPoolID   *int64 `json:"fallbackPoolId"`
FallbackPoolName string `json:"fallbackPoolName"`
```

The existing `RoutingPool` list response should include these fields. A missing fallback name should render as an empty string.

Extend gateway/admin request-log DTOs:

```go
RoutingPoolFallbackDepth int    `json:"routingPoolFallbackDepth"`
RoutingPoolFallbackChain string `json:"routingPoolFallbackChain"`
RoutingPoolError         string `json:"routingPoolError"`
```

`RoutingPoolError` is a local diagnostic reason such as `routing_pool_disabled`, `routing_pool_cycle`, or `routing_pool_exhausted`. It is separate from the existing public-ish `error` field so logs can preserve both the OpenAI-compatible response reason and routing diagnostics.

## Admin API

Keep the current routing pool create/update endpoints and add the fallback field to their request bodies:

```json
{
  "name": "primary",
  "description": "daily accounts",
  "enabled": true,
  "fallbackPoolId": 12
}
```

Use `null` or `0` to clear fallback.

Validation:

- fallback pool id must be positive when present.
- a pool cannot fall back to itself.
- a fallback target must exist.
- saving a fallback must reject cycles, including indirect cycles such as `A -> B -> C -> A`.
- disabled fallback pools may be selected intentionally, but runtime skips or fails through them according to the gateway rules below.
- deleting a pool should clear incoming fallback references through `ON DELETE SET NULL`.

Cycle detection belongs in the admin service/store path so invalid chains cannot be persisted through normal admin APIs. Provider selection should still protect itself from cycles in case of database drift.

## Gateway Selection Flow

For unbound API keys, keep the existing global flow.

For pool-bound API keys:

1. Build the fallback chain from the key's primary pool.
2. If the primary pool is missing, reject locally with `routing_pool_unavailable`.
3. If chain resolution detects a cycle, reject locally with `routing_pool_cycle`.
4. Try pools in order.
5. A disabled pool is skipped only if it is a fallback pool. A disabled primary pool rejects with `routing_pool_disabled` because an admin explicitly bound the key to a disabled pool.
6. In each enabled pool, select an account using the same filters as current pool selection.
7. If a pool has no candidate for the requested model, continue to the next fallback pool.
8. If a pool has eligible candidates but the selected account is busy or fails before streaming, exclude that account and keep trying within the same chain.
9. If the chain is exhausted, return the most specific local reason:
   - `provider_account_concurrency_limited` when every otherwise eligible candidate is blocked by per-account concurrency.
   - `model_unavailable` when no pool in the chain has a schedulable account for the requested model.
   - `provider_accounts_unavailable` when accounts exist but are all unavailable.
   - `routing_pool_exhausted` only for chain-level drift or disabled/missing fallback exhaustion that does not map cleanly to account/model availability.

Fallback is pre-stream only. Once upstream streaming starts, preserve the stream and do not switch pools.

## Provider Selection Boundary

The provider service should expose a chain-aware selection method rather than making the gateway manually loop over pools and duplicate provider semantics.

Suggested provider interface:

```go
type RoutingPoolSelection struct {
    PrimaryPoolID int64
    Pools         []RoutingPool
}

type SelectedAccount struct {
    // existing fields
    RoutingPoolID              int64
    RoutingPoolName            string
    RoutingPoolFallbackDepth   int
    RoutingPoolFallbackChain   string
    RoutingPoolError           string
}

SelectAccountForModelInRoutingPoolChain(ctx context.Context, primaryPoolID int64, model string, excludedAccountIDs ...int64) (SelectedAccount, error)
SelectAccountForModelAndSessionInRoutingPoolChain(ctx context.Context, primaryPoolID int64, model, sessionID string, excludedAccountIDs ...int64) (SelectedAccount, error)
```

The implementation can internally reuse `selectionCandidatesForRoutingPool`. It should keep existing single-pool methods for tests and simple callers, but the gateway should use the chain-aware methods when available.

## Sticky Sessions

Sticky sessions must bind to the actual pool used:

- If the primary pool selects an account, bind under the primary pool id.
- If fallback pool 12 selects an account, bind under pool 12.
- Later requests with the same API key and session should try the primary pool first. If the primary pool cannot serve and fallback pool 12 is reached, the pool-12 binding can be reused.
- A sticky binding from a fallback pool must never pull that account into the primary pool.

This preserves the existing invariant that sticky session scope is `(provider, model, session_id, routing_pool_id)`.

## `/v1/models`

For pool-bound keys, `/v1/models` should consider the full fallback chain:

- Include a model when at least one enabled healthy account in the primary or fallback chain can serve it.
- Apply the API key model policy after computing chain-routable models.
- Return an empty model list if the chain has no routable models.
- Do not expose pool names in the client response.

The admin UI can show pool/fallback model diagnostics separately.

## Request Logs

Request logs should answer these questions:

- Which pool did the API key start from?
- Which pool actually served the request?
- Did routing move to a fallback pool?
- Why did routing fail when no account was selected?

Use:

- `routing_pool_id` / `routing_pool_name`: actual selected pool when selected; otherwise primary pool snapshot.
- `routing_pool_fallback_depth`: selected pool depth, or the deepest attempted depth on failure.
- `routing_pool_fallback_chain`: admin-readable chain snapshot.
- `routing_pool_error`: pool-specific local reason.
- existing `gateway_attempt_count` / `gateway_fallback_count`: keep counting provider-account attempts and pre-stream account retries.

The Request Logs page should show fallback depth/chain in the routing pool column and keep filtering by `routingPoolId` against actual/recorded pool id.

## Admin UI

Routing Pools page:

- Add a fallback selector to each pool row.
- The selector includes `No fallback` and all other pools.
- Disable selecting the pool itself.
- Show `Fallback: <name>` in the pool summary.
- Show a warning when the selected fallback is disabled.
- Show save errors inline via existing `routingPools.error`.

API Keys page:

- Keep the existing routing pool selector.
- When a key is pool-bound, show the configured fallback target next to the selected pool when available.

Routing diagnostics:

- Add pool scope and fallback-chain preview after backend support exists.
- The first implementation can ship without a new diagnostics panel if request logs and pool UI carry the operational signal.

## Error Handling

Gateway client responses remain OpenAI-compatible:

- Disabled or missing primary pool: `503 service_unavailable`.
- Cycle or exhausted chain: `503 service_unavailable`.
- Local rate/concurrency limits: keep existing `rate_limit_exceeded` mapping.
- Model unavailable: keep existing model unavailable mapping.

Request logs keep precise local diagnostics:

- `routing_pool_disabled`
- `routing_pool_unavailable`
- `routing_pool_cycle`
- `routing_pool_exhausted`

Admin API validation should return:

- `400 invalid_input` for self fallback or cycle.
- `404 not_found` when updating a missing pool.
- `400 invalid_input` or `404 not_found` for a missing fallback target; use `400 invalid_input` if the service can validate before store persistence.

## Security and Privacy

- Fallback configuration contains only pool ids and names.
- Gateway responses must not expose pool names or chain details.
- All pool configuration remains behind the existing admin session.
- No provider credentials are exposed through fallback endpoints or logs.

## Testing Strategy

Backend tests should cover:

- migration adds `fallback_pool_id` and request-log fallback diagnostics.
- admin service rejects self fallback and indirect cycles.
- store list/get returns fallback id/name.
- HTTP create/update accepts and clears `fallbackPoolId`.
- provider chain selection uses primary first, then fallback when primary cannot serve the model.
- provider chain selection rejects cycles from database drift.
- sticky sessions bind and reuse under the actual fallback pool.
- gateway logs fallback depth/chain and keeps no-global-fallback isolation.
- `/v1/models` for pool-bound keys includes models from the fallback chain only.

Frontend tests should cover:

- routing pools page renders fallback selector and `No fallback`.
- routing pools page disables self fallback option.
- admin state sends `fallbackPoolId` on create/update.
- API Keys page surfaces fallback target for a pool-bound key.
- Request Logs page renders fallback depth/chain diagnostics.

Verification commands:

- `GOMODCACHE=/root/Clouds/N2API/.cache/go-mod GOCACHE=/root/Clouds/N2API/.cache/go-build go test ./...` from `backend/`
- `bun test src/routes/navigation.test.mjs src/routes/providers/provider-page.test.mjs` from `frontend/`
- `bun run check` from `frontend/`
- `bun run build` from `frontend/`

## Rollout Plan

Implement in small commits:

1. Schema, admin DTOs, store read/write, and cycle validation.
2. HTTP API and frontend fallback configuration.
3. Provider chain selection and sticky-session actual-pool binding.
4. Gateway chain-aware selection, request-log diagnostics, and `/v1/models` chain filtering.
5. Request Logs/Admin UI diagnostics, docs, and full verification.

Each slice should preserve existing global and single-pool behavior before adding fallback behavior.

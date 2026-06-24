# Routing Pools Design

## Summary

N2API should add personal routing pools so the admin can partition provider accounts into named scheduling groups and bind API keys to those pools. This brings the useful part of sub2api's `groups`, `account_groups`, and `api_keys.group_id` model into N2API while preserving the project's personal self-hosted scope.

The first version should be an account-pool and scheduling feature only. It must not add public users, subscriptions, balances, payment providers, rate multipliers, merchant accounting, or broad SaaS group semantics.

## Reference Behavior

The sub2api reference has these relevant concepts:

- `groups`: named routing/billing containers with platform, status, model routing, fallback group, RPM limits, quota fields, and display order.
- `account_groups`: many-to-many account membership with per-membership priority.
- `api_keys.group_id`: an API key can be bound to one group, which scopes the gateway's account selection.
- scheduling uses group membership so different API keys can use different upstream account pools.

N2API already implements the parts that should remain global or account-local:

- unified provider accounts for Codex OAuth and API-upstream exits.
- account enabled state, priority, load factor, max concurrency, health status, test state, and manual model capability.
- API key model policy, per-minute limits, usage budgets, disabled/revoked state, and request logs.
- sticky session bindings by provider, model, and session id.

The missing personal-use capability is pool-scoped account selection.

## Goals

- Let the admin create named routing pools.
- Let the admin enable or disable a routing pool.
- Let the admin attach provider accounts to one or more routing pools.
- Let the admin set a pool-membership priority override for an account.
- Let an API key optionally bind to one routing pool.
- Keep existing behavior for API keys with no routing pool: they use the global provider account pool exactly as today.
- Scope gateway account selection to the authenticated API key's routing pool when one is configured.
- Keep model capability, account health, account enabled state, excluded-account fallback, per-account concurrency, and provider credential handling unchanged.
- Include pool identity in request logs so routing decisions can be audited.
- Include pool scope in sticky session bindings so the same `session_id` can safely route through different pools.
- Show routing pool membership and schedulability diagnostics in the admin UI.
- Keep the feature useful without Redis or any new service.

## Non-Goals

- No public user system.
- No subscription plans, payments, recharge balances, invoices, merchant accounting, sponsor flows, or reseller behavior.
- No group-level USD rate multipliers or billing quotas.
- No provider-platform abstraction beyond N2API's current OpenAI/Codex provider scope.
- No automatic model discovery.
- No multi-pool load splitting for one API key in the first version.
- No fallback pool in the first version.
- No hard delete of pools that are referenced by request logs.
- No copying sub2api source code.

## Terminology

- **Routing pool**: a named, admin-managed provider account set used for gateway scheduling.
- **Pool membership**: a provider account's membership in one routing pool, with optional scheduling metadata.
- **Unscoped key**: an API key with no pool binding; it uses the existing global provider account pool.
- **Pool-bound key**: an API key with a pool id; it can only schedule provider accounts that are members of that pool.

## Data Model

Add `routing_pools`:

- `id BIGSERIAL PRIMARY KEY`
- `name TEXT NOT NULL`
- `description TEXT NOT NULL DEFAULT ''`
- `enabled BOOLEAN NOT NULL DEFAULT true`
- `created_at TIMESTAMPTZ NOT NULL DEFAULT now()`
- `updated_at TIMESTAMPTZ NOT NULL DEFAULT now()`
- `UNIQUE (name)`

Add `routing_pool_accounts`:

- `pool_id BIGINT NOT NULL REFERENCES routing_pools(id) ON DELETE CASCADE`
- `account_id BIGINT NOT NULL REFERENCES provider_accounts(id) ON DELETE CASCADE`
- `priority INTEGER NOT NULL DEFAULT 0`
- `created_at TIMESTAMPTZ NOT NULL DEFAULT now()`
- primary key: `(pool_id, account_id)`
- index by `account_id`
- index by `(pool_id, priority)`

Extend `client_api_keys`:

- `routing_pool_id BIGINT REFERENCES routing_pools(id) ON DELETE SET NULL`

Extend `provider_session_bindings`:

- `routing_pool_id BIGINT REFERENCES routing_pools(id) ON DELETE CASCADE`

The sticky binding uniqueness should include `routing_pool_id` semantics. Because PostgreSQL unique indexes treat `NULL` values as distinct, use a generated or expression index with `COALESCE(routing_pool_id, 0)` if N2API needs one binding per provider/model/session for unscoped keys. The design target is:

- unscoped key: binding key is `(provider, model, session_id, pool_scope=0)`.
- pool-bound key: binding key is `(provider, model, session_id, pool_scope=routing_pool_id)`.

Extend `request_logs`:

- `routing_pool_id BIGINT REFERENCES routing_pools(id) ON DELETE SET NULL`
- `routing_pool_name TEXT NOT NULL DEFAULT ''`

The name snapshot follows the existing provider account name snapshot pattern so old logs remain readable if a pool is renamed or deleted.

## Admin DTOs

Add:

```go
type RoutingPool struct {
    ID          int64     `json:"id"`
    Name        string    `json:"name"`
    Description string    `json:"description"`
    Enabled     bool      `json:"enabled"`
    AccountIDs  []int64   `json:"accountIds"`
    Accounts    []RoutingPoolAccount `json:"accounts,omitempty"`
    CreatedAt   time.Time `json:"createdAt"`
    UpdatedAt   time.Time `json:"updatedAt"`
}

type RoutingPoolAccount struct {
    AccountID int64 `json:"accountId"`
    Priority  int   `json:"priority"`
}
```

Extend API key DTOs:

- `routingPoolId`
- `routingPoolName`

`routingPoolName` is a convenience field for the API Keys page; the canonical binding is `routingPoolId`.

Extend provider selection DTOs:

- selected account should carry `RoutingPoolID` and `RoutingPoolName` only where gateway logging needs it. The provider account itself should remain reusable across pools.

## Admin API

Add protected endpoints:

- `GET /api/admin/routing-pools`
- `POST /api/admin/routing-pools`
- `PATCH /api/admin/routing-pools/{id}`
- `DELETE /api/admin/routing-pools/{id}`
- `PUT /api/admin/routing-pools/{id}/accounts`
- `PUT /api/admin/keys/{id}/routing-pool`

`POST /api/admin/routing-pools` body:

```json
{
  "name": "codex primary",
  "description": "daily Codex accounts",
  "enabled": true
}
```

`PATCH /api/admin/routing-pools/{id}` body:

```json
{
  "name": "codex primary",
  "description": "daily Codex accounts",
  "enabled": true
}
```

`PUT /api/admin/routing-pools/{id}/accounts` body:

```json
{
  "accounts": [
    { "accountId": 7, "priority": 0 },
    { "accountId": 8, "priority": 10 }
  ]
}
```

`PUT /api/admin/keys/{id}/routing-pool` body:

```json
{
  "routingPoolId": 12
}
```

Use `0` or `null` to clear an API key pool binding.

Validation:

- pool name must be non-empty after trimming.
- membership account ids must be positive.
- membership accounts must exist under the current provider.
- priority must be non-negative.
- disabled pools can still be edited, but bound keys cannot schedule through them.
- binding a key to a disabled pool is allowed so the admin can stage configuration; runtime rejects until enabled or the key is rebound.
- deleting a pool clears API key bindings through `ON DELETE SET NULL`, deletes memberships through cascade, and leaves request logs with the name snapshot.

## Gateway Selection Flow

Gateway request flow should become:

1. Authenticate API key.
2. Load gateway settings and API key budget state as today.
3. Resolve the API key's routing pool, if any.
4. If the key is bound to a disabled or missing pool, reject locally with OpenAI-compatible `503 service_unavailable`.
5. Extract model and session id as today.
6. Select a provider account:
   - unscoped key: existing global selection behavior.
   - pool-bound key: select only accounts in that routing pool.
7. Preserve pre-stream fallback by excluding failed or busy account ids within the same pool.
8. Log request with routing pool id/name snapshot.

Pool membership is a hard scheduling boundary. A pool-bound key must not fall back to global accounts because that would defeat the reason to isolate the key.

## Provider Selection Rules

Pool-scoped selection should reuse current account filters:

- account provider matches N2API's configured provider.
- account is enabled.
- account is healthy and not expired, rate-limited, circuit-open, or manually paused.
- account supports the requested model through manual account-model rows.
- account is not in the excluded-account list.
- account can produce a usable credential.

Ordering should be:

1. pool membership priority ascending.
2. account priority ascending.
3. account load factor descending.
4. account error/last-used/id ordering as today.

This makes the pool membership priority an override layer without deleting the existing account-level priority. If two pools share the same account, the account can have different pool priorities in each pool.

If no account in the pool supports the model, return the same model-unavailable semantics used by global selection. If accounts exist but all are disabled or unhealthy, return the matching unavailable/disabled error semantics where possible.

## Sticky Sessions

Sticky bindings must include pool scope:

- A pool-bound key stores and reads bindings under that pool id.
- An unscoped key stores and reads bindings under the global scope.
- If a bound account leaves the pool, becomes unschedulable, or is excluded during fallback, selection may rebind to another eligible account in the same pool.
- A sticky binding should never cause a pool-bound key to select an account outside the pool.

The existing sticky session preview should show pool-scoped sticky behavior when a pool id is supplied.

## Model List Behavior

`/v1/models` should be filtered by both API key model policy and routing pool scope:

- unscoped key: current routable model list.
- pool-bound key: models routable by at least one enabled healthy account in that pool.
- selected-model API key: intersection of selected models and pool-routable models.

If a pool has no routable models, `/v1/models` returns an empty OpenAI-compatible list rather than failing.

## Admin UI

Add a Routing Pools page in the existing sidebar shell.

The page should support:

- list pools with enabled state, account count, active account count, and routable model count.
- create and rename pools.
- enable or disable pools.
- edit pool membership by selecting provider accounts.
- set per-membership priority.
- link to provider account logs or provider account detail actions where existing pages already support that.

Update API Keys page:

- add a routing pool selector per key.
- show `Global pool` for unscoped keys.
- show disabled/missing pool warnings.
- keep pool selector disabled after key revocation.

Update Routing diagnostics / Models page:

- allow previewing selection under a pool.
- show pool scope in candidate diagnostics when supplied.

The UI should remain operational and dense, matching `DESIGN.md`; no marketing cards or landing-style layout.

## Request Logs and Usage

Request logs should include routing pool id/name in admin responses and filters.

Add a Request Logs filter:

- `routingPoolId`

Usage summaries should add a `routing_pool` group-by mode after the basic routing feature is stable. This can be a second implementation task if the initial pool scheduling slice becomes too large. The first gateway slice must at least log pool attribution so historical data is available.

## Error Handling

- Missing pool id on an API key should clear the binding only through admin endpoints. At runtime, if a key references a missing pool due to database drift, fail closed with local `503 service_unavailable` and log `routing_pool_unavailable`.
- Disabled pool should return local `503 service_unavailable` and log `routing_pool_disabled`.
- Empty enabled pool should return local `503 service_unavailable` and log `routing_pool_empty`.
- Pool with accounts that cannot serve the model should preserve the existing model unavailable response and log model unavailable.
- Pool membership update should be transactional: replace the full set or leave the old set intact.

## Security and Privacy

- Routing pools contain only account ids and scheduling metadata.
- Do not expose provider credentials through pool endpoints.
- Keep all endpoints behind the existing admin session.
- API clients should not learn pool names from gateway responses. Pool names are for admin UI and request logs.

## Testing Strategy

Backend tests should cover:

- migrations for `routing_pools`, `routing_pool_accounts`, API key `routing_pool_id`, session binding pool scope, and request-log pool attribution.
- repository create/update/delete/list routing pools.
- repository replace pool memberships transactionally.
- API key pool binding update and list response fields.
- provider selection scoped to a pool.
- pool membership priority ordering ahead of account priority.
- global unscoped keys preserve current selection behavior.
- sticky session bindings do not cross pool scope.
- `/v1/models` returns pool-routable models for pool-bound keys.
- gateway local rejection for disabled, missing, or empty pools.
- request logs include pool id/name snapshots.
- HTTP endpoints map invalid input, not found, and success correctly.

Frontend tests should cover:

- sidebar includes Routing Pools.
- routing pools page can create, edit, enable/disable, and replace memberships through admin state calls.
- API Keys page renders and saves routing pool binding.
- Request Logs page can filter by routing pool after log attribution endpoint support lands.
- routing preview can include pool scope after preview endpoint support lands.

Verification commands:

- `GOMODCACHE=/root/Clouds/N2API/.cache/go-mod GOCACHE=/root/Clouds/N2API/.cache/go-build go test ./...` from `backend/`
- `bun test src/routes/navigation.test.mjs src/routes/providers/provider-page.test.mjs` from `frontend/`
- `bun run check` from `frontend/`
- `bun run build` from `frontend/`

## Rollout Plan

Implement in small slices:

1. Store and admin service routing pool CRUD plus API key binding.
2. Provider selection scoped by pool with global fallback only for unscoped keys.
3. Gateway wiring, sticky session pool scope, and request-log attribution.
4. Admin HTTP endpoints.
5. Routing Pools page and API Keys pool selector.
6. Routing diagnostics and Request Logs filters.
7. Documentation and full verification.

Each slice should be separately committed and should keep existing global behavior passing before adding pool-bound behavior.

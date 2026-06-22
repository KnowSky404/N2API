# Account Model Routing Design

## Summary
This phase makes model availability part of the OpenAI/Codex account pool instead of treating models as a detached global setting. N2API remains a personal self-hosted gateway: one admin, OpenAI/Codex OAuth accounts first, no billing, no public registration, and no broad SaaS behavior.

The first implementation is manual-first. The admin explicitly configures which models each connected upstream account can serve. The gateway then routes each request to an enabled, healthy account that is configured for the requested model, and `/v1/models` reports the model set that the gateway can actually route.

## Goals
- Bind model availability to individual provider accounts.
- Keep global model settings only as routing policy: default model and external exposure controls.
- Let the admin manually add, enable, disable, and remove model names for each OpenAI/Codex account.
- Select gateway accounts by requested model before applying priority, health, and last-use ordering.
- Keep fallback constrained to accounts that support the same requested model.
- Make `/v1/models` return the currently exposed aggregate model list instead of blindly proxying one upstream account.
- Preserve existing account-pool health behavior, token refresh behavior, request logging, and streaming behavior.
- Add backend tests for model-scoped account selection, `/v1/models`, and admin model management.

## Non-Goals
- No automatic upstream model discovery in the first implementation.
- No background model sync job.
- No per-client model policy or quota policy.
- No billing, recharge, balance, merchant, or public SaaS behavior.
- No non-OpenAI provider implementation in this phase.
- No Redis or new infrastructure dependency.
- No attempt to guarantee that manually configured models are accepted by upstream forever; runtime upstream failures still update account health as they do today.

## Current Baseline
The account pool already stores connected OAuth accounts in `oauth_accounts` and selects accounts by enabled state, priority, health, last use, and id. Upstream failures update account health, and gateway fallback only happens before response streaming begins.

Model settings currently live in the generic `settings` table under the `model_settings` key. They contain a global `defaultModel` and `allowedModels`, but they are not connected to provider accounts and do not influence account selection. The gateway currently selects an account before considering the requested model, so it can route a request to an account that does not support that model.

This phase evolves the existing account-pool design instead of replacing it.

## Data Model
Add a new table for account-scoped model capability:

```sql
CREATE TABLE IF NOT EXISTS oauth_account_models (
    id BIGSERIAL PRIMARY KEY,
    account_id BIGINT NOT NULL REFERENCES oauth_accounts(id) ON DELETE CASCADE,
    provider TEXT NOT NULL,
    model TEXT NOT NULL,
    enabled BOOLEAN NOT NULL DEFAULT true,
    source TEXT NOT NULL DEFAULT 'manual',
    last_seen_at TIMESTAMPTZ,
    last_error TEXT NOT NULL DEFAULT '',
    metadata JSONB NOT NULL DEFAULT '{}'::jsonb,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE (account_id, model)
);
```

Indexes:
- `(provider, model, enabled, account_id)` for model-scoped account selection.
- `(account_id, enabled, model)` for account detail views.

Rules:
- `source` is `manual` in the first implementation. The column exists so future automatic discovery can be added without changing the core model.
- `model` is trimmed, non-empty, and at most the same maximum length used by existing global model settings.
- Deleting an OAuth account deletes its configured models through `ON DELETE CASCADE`.
- Reconnecting the same OAuth subject preserves account scheduling fields and should preserve manually configured models because the account row is updated rather than deleted.

## Global Model Policy
Keep the existing global model settings, but narrow their meaning:

- `defaultModel`: model injected when a supported request body omits `model`.
- `allowedModels`: external exposure allowlist.

The gateway should treat a model as externally exposed only when both are true:
- the model appears in `allowedModels`
- at least one enabled account has an enabled `oauth_account_models` row for that model

If this proves too strict during implementation, the fallback should be explicit: an empty `allowedModels` may mean expose all enabled account models, but the first implementation should keep the current validation behavior where the default model must be present in the allowed list.

## Backend Architecture

### Store Layer
The store layer owns persistence for account model rows. It should expose methods that can be implemented cleanly in PostgreSQL and memory fakes:

- list models for one account
- replace the full manual model list for one account
- add one model to an account
- update one account model enabled state
- delete one model from an account
- list aggregate exposed models from enabled accounts
- list eligible accounts for a provider and model using existing account health columns

The first implementation can use "replace full manual model list" for the admin UI because it keeps the API simple and matches textarea-style editing. Fine-grained add/update/delete methods can still exist if they make tests or UI actions cleaner.

### Provider Service
The provider service remains the boundary for account-pool behavior. It should accept a requested model when selecting a token:

```go
SelectAccessToken(ctx context.Context, model string, excludedAccountIDs ...int64) (SelectedToken, error)
```

Selection order after model filtering remains:
1. enabled account
2. account status is usable at the current time
3. enabled account-model row exists for the requested model
4. lower priority first
5. older `last_used_at` first, with never-used accounts first
6. lower id as deterministic tie-breaker

If the model is allowed globally but no account supports it, return a model-scoped unavailable error. The HTTP layer can map that to an OpenAI-compatible response.

### Gateway
The gateway must extract the requested model before selecting an account.

Supported model extraction:
- `POST /v1/chat/completions`: JSON body field `model`
- `POST /v1/responses`: JSON body field `model`
- `GET /v1/models`: no account selection; return aggregate model list
- `GET /v1/responses/{id}` and `/input_items`: no body model is available, so keep the existing token selection behavior unless later response-session affinity is introduced

For POST routes:
1. Read the replayable body as the gateway already does.
2. Parse `model` from JSON.
3. If the model is blank, inject the global default model into the forwarded JSON body.
4. Validate the model against global policy.
5. Select an account that has the requested model enabled.
6. Retry only against other accounts that support the same model.

If the body is too large to replay, the gateway cannot safely inject or parse JSON. For this first implementation, it should require a replayable body for model-routed POST routes and return a clear OpenAI-compatible error if the body exceeds the existing replay limit.

### `/v1/models`
`GET /v1/models` should be served locally from aggregate model capability and global policy. It should not call upstream.

Response shape should stay OpenAI-compatible:

```json
{
  "object": "list",
  "data": [
    {
      "id": "gpt-5",
      "object": "model",
      "created": 0,
      "owned_by": "openai"
    }
  ]
}
```

The list should be deterministic, sorted by global allowed-list order first and lexicographic order for any future unlisted exposed models.

## Admin API
Keep existing provider/account endpoints and add account-model operations under the account resource.

Protected endpoints:
- `GET /api/admin/providers/openai/accounts/{id}/models`
  - Returns configured models for one account.
- `PUT /api/admin/providers/openai/accounts/{id}/models`
  - Replaces the account's manual model list.
  - Accepts `{ "models": [{"model": "gpt-5", "enabled": true}, {"model": "gpt-5-mini", "enabled": true}] }`.
- `GET /api/admin/model-routing`
  - Returns aggregate model routing status: default model, allowed models, configured accounts per model, enabled accounts per model, and availability warnings.

The first implementation should use replace semantics for account models. Fine-grained `PATCH` and `DELETE` endpoints are intentionally deferred until the UI needs separate row-level actions.

## Frontend Design
The admin UI remains operational and dense.

Providers page:
- Keep provider status and OAuth account table.
- Add per-account model capability summary, such as "3 enabled models".
- Add an account details area or inline editor where the admin can paste one model per line.
- Show save state and validation errors inline.
- If an account has no enabled models, show that it cannot receive model-routed POST traffic even if the account itself is enabled.

Models page:
- Rename the visible page concept to model routing policy.
- Keep default model and allowed model editing.
- Show aggregate availability by model:
  - model name
  - allowed or hidden
  - total configured accounts
  - enabled healthy accounts
  - warning when the default model has no eligible account
- Avoid passive success popups; use inline saved state for routine model edits.

## Error Handling
- Request model missing and no valid default model: `400 invalid_request`.
- Request model not in global allowlist: `404 model_not_found` or `400 model_not_allowed`; use one code consistently in tests.
- Request model allowed but no enabled account supports it: `503 model_unavailable`.
- Account exists but its model row is disabled for the requested model: treat as not eligible.
- All eligible model-capable accounts fail token lookup or pre-stream upstream attempt: `503 provider_accounts_unavailable`.
- Invalid account model input returns `400 invalid_input`.
- Missing account id returns `404 not_found`.
- `/v1/models` with no exposed models returns an empty OpenAI-compatible list, not a gateway failure.

## Security Rules
- Account model names are not secrets, but account endpoints still require an admin session.
- Do not expose account ids in public `/v1/models`.
- Do not log request bodies while parsing models.
- Continue encrypting OAuth credentials at rest.
- Continue preserving account `enabled` and `priority` on reconnect.

## Migration and Compatibility
Existing deployments have global model settings but no account model rows. After migration:
- Existing OAuth accounts remain connected.
- No account is model-routable until models are manually configured, unless the implementation chooses a one-time backfill from global `allowedModels`.
- To reduce surprise, the implementation should backfill existing accounts with current global `allowedModels` only if there is exactly one connected account. With multiple accounts, the admin should configure models manually to avoid false capability assumptions.
- The admin UI should surface this state clearly: connected accounts without model configuration cannot serve model-routed POST requests.

## Testing Strategy
Backend tests should cover:
- migration creates `oauth_account_models` with cascade delete and uniqueness.
- account model list normalization trims blanks and deduplicates.
- invalid model names are rejected.
- reconnecting an existing account preserves its configured models.
- selector only chooses accounts that support the requested model.
- selector fallback excludes failed accounts but keeps the same model filter.
- disabled account-model rows are not eligible.
- disabled accounts are not eligible even if the model row is enabled.
- `/v1/models` returns aggregate enabled model rows filtered by global policy.
- POST requests without `model` receive the default model before upstream forwarding.
- POST requests for unsupported models return the selected error code without contacting upstream.
- oversized unreplayable POST bodies return a clear error instead of being routed without model validation.
- admin account-model endpoints require a valid admin session.

Frontend verification should continue to run:
- `bun run check`
- `bun run build`

Backend verification should continue to run with repo-local caches:
- `GOMODCACHE=/root/Clouds/N2API/.cache/go-mod GOCACHE=/root/Clouds/N2API/.cache/go-build go test ./...`

## Deployment and Operations
Docker Compose remains the default deployment path. No new service is required.

Operational docs should explain:
- models are configured per upstream account
- global model settings control exposure, not account capability
- connected accounts without configured models will not serve model-routed POST traffic
- `/v1/models` is the externally visible aggregate model list
- fallback only happens between accounts that support the requested model

## Acceptance Criteria
- Admin can manually configure model names for each connected OpenAI/Codex account.
- Gateway selects only accounts that are enabled, healthy, and configured for the requested model.
- Gateway fallback stays model-scoped.
- Requests without `model` use the configured default model.
- Requests for globally hidden or unsupported models fail before contacting upstream.
- `/v1/models` returns the exposed aggregate model list.
- Existing accounts and OAuth credentials survive migration.
- Backend tests pass.
- Frontend check and build pass.

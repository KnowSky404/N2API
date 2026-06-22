# Unified Provider Accounts And API Key Model Policy Design

## Summary
N2API should treat every upstream gateway exit as one unified provider account. Codex OAuth accounts and API-key upstream accounts share the same scheduling, health, priority, and model-capability behavior. They differ only in credential type and upstream request construction.

This design replaces the current OAuth-specific account-pool model with a unified provider account model, and moves client-facing model access control into API keys. The standalone Models page stops being a primary admin surface.

## Goals
- Use one provider account resource for Codex OAuth and API-key upstream exits.
- Keep account routing based on enabled state, model support, health state, priority, last use, and deterministic id ordering.
- Store model capability on each provider account, not in a detached global model page.
- Let each client API key default to all routable models while optionally restricting it to selected models.
- Keep one global default model for requests that omit `model`.
- Move model policy controls into the API Keys admin surface.
- Retire the standalone Models navigation concept.
- Keep V1 scoped to personal self-hosted gateway use with no billing, recharge, public registration, or SaaS behavior.

## Non-Goals
- No platform billing, balances, quotas, merchant accounting, or payment providers.
- No public registration or broad multi-tenant management.
- No Redis or new infrastructure dependency.
- No automatic model discovery in the first unified-account implementation.
- No simultaneous long-term business logic support for both old OAuth-specific and new unified account paths.
- No source-code copying from sub2api.

## Current Baseline
The current backend stores Codex/OpenAI OAuth accounts in `oauth_accounts`. Several later migrations added scheduling and health fields to that table. Account model capability currently lives in `oauth_account_models`, and gateway model routing already selects OAuth accounts by requested model.

The missing architectural piece is that API-key upstream exits are not first-class accounts yet. Keeping them separate from OAuth accounts would force duplicated scheduling, model, and health logic. This design intentionally chooses a unified-table refactor so future provider work does not depend on an OAuth-specific data model.

## Data Model

### `provider_accounts`
Create a unified account table for all upstream exits:

```sql
CREATE TABLE provider_accounts (
    id BIGSERIAL PRIMARY KEY,
    provider TEXT NOT NULL,
    account_type TEXT NOT NULL,
    name TEXT NOT NULL DEFAULT '',
    subject TEXT NOT NULL DEFAULT '',
    display_name TEXT NOT NULL DEFAULT '',
    enabled BOOLEAN NOT NULL DEFAULT true,
    priority INTEGER NOT NULL DEFAULT 100,
    status TEXT NOT NULL DEFAULT 'active',
    status_reason TEXT NOT NULL DEFAULT '',
    last_used_at TIMESTAMPTZ,
    last_error TEXT NOT NULL DEFAULT '',
    last_error_at TIMESTAMPTZ,
    failure_count INTEGER NOT NULL DEFAULT 0,
    circuit_open_until TIMESTAMPTZ,
    rate_limited_until TIMESTAMPTZ,
    fingerprint_hash TEXT NOT NULL DEFAULT '',
    user_agent_hash TEXT NOT NULL DEFAULT '',
    ip_hash TEXT NOT NULL DEFAULT '',
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);
```

Account types:
- `codex_oauth`: an OAuth-backed Codex/OpenAI account.
- `api_upstream`: an API-key-backed OpenAI-compatible upstream account.

Indexes:
- `(provider, account_type, subject)` unique where `subject <> ''` for OAuth identity preservation.
- `(provider, enabled, status, priority, last_used_at, id)` for schedulable account selection.
- `(provider, account_type, enabled, priority, id)` for admin and routing lists.

### `provider_account_credentials`
Store credentials separately from scheduling fields:

```sql
CREATE TABLE provider_account_credentials (
    account_id BIGINT PRIMARY KEY REFERENCES provider_accounts(id) ON DELETE CASCADE,
    credential_type TEXT NOT NULL,
    encrypted_access_token TEXT NOT NULL DEFAULT '',
    encrypted_refresh_token TEXT NOT NULL DEFAULT '',
    access_token_expires_at TIMESTAMPTZ,
    last_refresh_at TIMESTAMPTZ,
    last_refresh_error TEXT NOT NULL DEFAULT '',
    last_refresh_error_at TIMESTAMPTZ,
    encrypted_api_key TEXT NOT NULL DEFAULT '',
    base_url TEXT NOT NULL DEFAULT '',
    metadata JSONB NOT NULL DEFAULT '{}'::jsonb,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);
```

Credential types:
- `oauth_token` for `codex_oauth`.
- `api_key` for `api_upstream`.

All reversible tokens and API keys remain encrypted at rest with the existing encryption secret.

### `provider_account_models`
Replace `oauth_account_models` with an account-type-neutral table:

```sql
CREATE TABLE provider_account_models (
    id BIGSERIAL PRIMARY KEY,
    account_id BIGINT NOT NULL REFERENCES provider_accounts(id) ON DELETE CASCADE,
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

### `client_api_keys`
Add model policy to client API keys:

```sql
ALTER TABLE client_api_keys
    ADD COLUMN model_policy TEXT NOT NULL DEFAULT 'all';
```

Supported values:
- `all`: default. The key may access all models currently routable through enabled provider accounts.
- `selected`: the key may access only models listed in `client_api_key_models`.

### `client_api_key_models`
Store selected model allowlists:

```sql
CREATE TABLE client_api_key_models (
    id BIGSERIAL PRIMARY KEY,
    client_key_id BIGINT NOT NULL REFERENCES client_api_keys(id) ON DELETE CASCADE,
    model TEXT NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE (client_key_id, model)
);
```

## Migration Strategy
The migration should move existing OAuth data into the unified model without losing connected accounts.

1. Create `provider_accounts`, `provider_account_credentials`, `provider_account_models`, and `client_api_key_models`.
2. Copy each `oauth_accounts` row into `provider_accounts` with `account_type = 'codex_oauth'`.
3. Copy OAuth token fields into `provider_account_credentials` with `credential_type = 'oauth_token'`.
4. Copy `oauth_account_models` rows into `provider_account_models` using the new account ids.
5. Add `client_api_keys.model_policy` with default `all` so existing keys keep current access.
6. Update `oauth_states.target_account_id` semantics to reference `provider_accounts.id` for future OAuth connect flows.
7. Keep old OAuth tables for one compatibility window, but production business logic should read and write only the unified tables after the refactor lands.

The code should not keep a long-term dual-write path. Dual writing increases the chance that the new account model silently drifts from the old OAuth-specific model.

## Backend Architecture

### Store Layer
The store package should expose provider-account operations instead of OAuth-account operations for gateway routing:

- list provider accounts
- create or update a Codex OAuth account
- create or update an API upstream account
- update account enabled, priority, status, and health fields
- list and replace account models
- list exposed aggregate models
- list eligible accounts for a provider and model
- read credentials for a selected account

Existing OAuth-specific repository methods should either be removed or become narrow helpers used only by the OAuth callback path during the transition.

### Provider Service
The provider service becomes a unified account service. The gateway selection contract should be account-type-neutral:

```go
SelectAccountForModel(ctx context.Context, model string, excludedAccountIDs ...int64) (SelectedAccount, error)
```

`SelectedAccount` should include:
- account id
- provider
- account type
- upstream base URL
- authorization material needed by the gateway
- account display metadata for logging

The service is responsible for:
- OAuth refresh when a selected OAuth account token is expired.
- API upstream credential loading and base URL validation.
- health and failure recording for either account type.
- preserving model-scoped fallback.

### Gateway
The request path should be:

1. Authenticate the client API key.
2. Parse the request model for model-routed POST routes.
3. If the model is missing, inject the global default model.
4. Check the authenticated client key model policy.
5. Select an enabled, healthy provider account that supports the requested model.
6. Construct the upstream request according to account type:
   - `codex_oauth`: use bearer OAuth access token and Codex/OpenAI upstream settings.
   - `api_upstream`: use encrypted API key, configured base URL, and optional metadata headers.
7. Retry only before streaming begins, and only against other accounts that support the same model.

`GET /v1/models` should return the aggregate model list that is both routable and visible to the authenticated API key. For `all`, this is the gateway aggregate list. For `selected`, this is the intersection between selected models and routable models.

### Admin API
Replace OAuth-specific admin naming where practical:

- `GET /api/admin/provider-accounts`
- `POST /api/admin/provider-accounts/api-upstream`
- `PATCH /api/admin/provider-accounts/{id}`
- `DELETE /api/admin/provider-accounts/{id}`
- `GET /api/admin/provider-accounts/{id}/models`
- `PUT /api/admin/provider-accounts/{id}/models`
- `POST /api/admin/provider-accounts/codex-oauth/connect`
- `POST /api/admin/provider-accounts/{id}/disconnect`

Existing `/api/admin/providers/openai/...` routes can remain as compatibility wrappers for one release if needed, but new UI code should use the unified provider-account endpoints.

API key endpoints should include:
- model policy in list/detail responses
- selected model list when `model_policy = 'selected'`
- update endpoint for policy and selected models
- gateway default model update on the API Keys page

## Frontend Information Architecture

### Accounts Page
Rename or evolve the current Providers page into an account-oriented page. It should manage every gateway exit:

- Codex OAuth accounts
- API-key upstream accounts
- enabled state
- priority
- health/status
- per-account model list
- last error and scheduling state

The UI should make account type visible but not split common account behavior into separate pages.

### API Keys Page
The API Keys page becomes the client access policy page:

- create and revoke client API keys
- show last use
- configure model policy: `all` or `selected`
- edit selected models when policy is `selected`
- show Gateway default model control
- surface warnings when a selected model has no enabled provider account

The default model belongs here because it affects client request handling, not upstream account capability.

### Models Page
The standalone Models page should stop being a primary navigation item.

Implementation options:
- route `/models` redirects to `/api-keys`
- or route `/models` shows a short compatibility message and link to API Keys

The sidebar should not present Models as a separate top-level configuration concept after this refactor.

## Error Handling
- Missing request model and no configured default: `400 invalid_request`.
- API key policy rejects model: `404 model_not_found` to avoid exposing hidden model availability.
- Model allowed by API key but no enabled healthy account supports it: `503 model_unavailable`.
- API upstream account with invalid base URL: account is not eligible and records a health error.
- OAuth refresh failure: account health is updated and fallback remains model-scoped before streaming starts.
- Invalid model policy payload: `400 invalid_input`.
- Missing provider account: `404 not_found`.

## Security
- Continue storing API key hashes only for client API keys.
- Encrypt upstream API keys, OAuth access tokens, and OAuth refresh tokens at rest.
- Do not expose credential values through admin API responses.
- Do not expose provider account ids in public `/v1/models`.
- Do not log request bodies while extracting model fields.
- Keep admin endpoints behind the existing admin session controls.

## Testing Strategy
Backend tests should cover:
- migration copies OAuth accounts, credentials, health fields, and model rows into unified tables.
- existing client API keys default to `model_policy = 'all'`.
- selected API key policy rejects non-selected models before account selection.
- `GET /v1/models` is filtered by authenticated API key policy.
- OAuth account selection and API upstream account selection share the same model and health filters.
- fallback excludes failed accounts while preserving the requested model.
- OAuth refresh still serializes per account.
- API upstream base URL and credential validation prevents unsafe routing.
- admin provider-account endpoints require a valid admin session.
- old compatibility endpoints, if kept, delegate to unified account service.

Frontend tests should cover:
- account page renders both Codex OAuth and API upstream account controls.
- account model editor works for either account type.
- API Keys page exposes `all` and `selected` model policy controls.
- selected model warnings appear when no provider account can serve the model.
- sidebar no longer treats Models as a primary configuration page.

Verification commands remain:

```bash
cd backend
GOMODCACHE=/root/Clouds/N2API/.cache/go-mod GOCACHE=/root/Clouds/N2API/.cache/go-build go test ./...
```

```bash
cd frontend
bun run check
bun run build
```

## Deployment And Compatibility
Docker Compose remains the deployment target and no new service is required.

Existing deployments should keep their OAuth accounts and client API keys after migration. Existing client API keys should continue to work because they default to `all`. After the refactor, admins should configure API upstream accounts on the Accounts page and can optionally restrict client API keys on the API Keys page.

The local admin UI should clearly explain through labels and inline warnings that:
- accounts define what the gateway can route to
- API keys define what each client can access
- the default model is injected only when a request omits `model`

## Acceptance Criteria
- Codex OAuth and API upstream exits are represented by one provider account resource.
- Gateway account selection no longer depends on OAuth-specific table or service names.
- Each provider account has its own model capability list.
- API keys default to all routable models.
- API keys can be restricted to selected models.
- Gateway default model remains available and is managed from API Keys.
- `/models` is removed from primary navigation or redirects to API Keys.
- Existing OAuth accounts and model rows survive migration.
- Existing client API keys keep access after migration.
- Backend and frontend verification commands pass.

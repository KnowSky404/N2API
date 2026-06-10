# OpenAI Account Pool Gateway Stability Design

## Summary
This phase moves N2API from a single connected OpenAI/Codex account toward a stable personal gateway with a small upstream account pool. The target is still personal self-hosted use: one administrator, no public users, no billing, no balances, and no merchant behavior.

The account pool should make the daily gateway resilient for Codex and OpenAI-compatible chat clients. Admins can connect multiple OpenAI/Codex OAuth accounts, enable or disable them, assign a simple priority, inspect their token and error state, and let the gateway choose a healthy account for each supported `/v1/*` request.

## Goals
- Support multiple OpenAI/Codex OAuth accounts under the existing `openai` provider.
- Let the admin list, connect, enable, disable, prioritize, and disconnect individual upstream accounts.
- Route supported gateway requests through an enabled account selected from the pool.
- Retry one request against another eligible account when the first selected account fails before response streaming begins.
- Keep token refresh account-scoped and safe under concurrent gateway traffic.
- Preserve end-to-end streaming behavior for supported upstream responses.
- Return OpenAI-compatible errors when no account is connected, no account is enabled, token refresh fails for every eligible account, or the upstream request cannot be completed.
- Add focused backend tests and admin UI checks for account pool behavior.

## Non-Goals
- No public registration or end-user account system.
- No platform billing, recharge, quota sale, invoice, sponsor, merchant, or balance features.
- No Redis requirement.
- No Claude, Gemini, or non-OpenAI provider implementation in this phase.
- No complex rate-limit strategy, usage accounting, or per-client routing policies.
- No automatic background health checker in the first implementation; health state is updated by connect, refresh, and gateway attempts.
- No retry after response headers or streaming bytes have been sent to the client.

## Current Baseline
The current code supports one effective provider account. `ProviderRepository.FindAccount` returns the latest row for `provider = 'openai'`, `SaveAccount` upserts by `(provider, subject)`, and `DeleteAccount` removes all rows for a provider. The provider service exposes single-account methods such as `Status`, `StartConnect`, `CompleteCallback`, `Disconnect`, and `AccessToken`.

The gateway already authenticates client API keys, fetches one provider access token, proxies a small supported route set, preserves response streaming, and writes request logs. The supported route set is:
- `GET /v1/models`
- `POST /v1/chat/completions`
- `POST /v1/responses`
- `GET /v1/responses/{id}`
- `GET /v1/responses/{id}/input_items`

This phase should evolve these boundaries instead of replacing them wholesale.

## Data Model
Extend `oauth_accounts` with account-pool metadata:
- `id BIGSERIAL PRIMARY KEY` remains the stable account identifier.
- `provider TEXT NOT NULL` remains `openai` for this phase.
- `subject TEXT NOT NULL DEFAULT ''` remains the provider account identity when available.
- `display_name TEXT NOT NULL DEFAULT ''` remains the admin-facing account label.
- `encrypted_access_token TEXT NOT NULL`
- `encrypted_refresh_token TEXT NOT NULL`
- `access_token_expires_at TIMESTAMPTZ`
- `last_refresh_at TIMESTAMPTZ`
- `enabled BOOLEAN NOT NULL DEFAULT true`
- `priority INTEGER NOT NULL DEFAULT 100`
- `last_used_at TIMESTAMPTZ`
- `last_error TEXT NOT NULL DEFAULT ''`
- `last_error_at TIMESTAMPTZ`
- `created_at TIMESTAMPTZ NOT NULL DEFAULT now()`
- `updated_at TIMESTAMPTZ NOT NULL DEFAULT now()`

Keep the unique key on `(provider, subject)` for accounts with a real subject. If the OAuth token response does not include a subject, use a deterministic fallback subject derived from provider response metadata when possible. If no stable upstream identity is available, generate a local subject during callback and show the display name clearly as provider supplied or locally labeled.

Keep `oauth_states` as the callback state table, but the callback implementation must claim the state before storing credentials or use one database transaction that atomically validates state consumption and account persistence. A replayed or concurrent callback must not be able to persist credentials after the state has already been claimed.

## Backend Architecture
Split provider account behavior into three clear responsibilities:

### Account Repository
The store layer owns PostgreSQL persistence for account rows and callback states. It should expose account-pool methods instead of only single-account methods:
- list accounts for a provider ordered by priority, recent error state, and id
- find account by id
- save or update an account from OAuth callback
- update account enabled state
- update account priority
- delete one account by id
- mark account used
- mark account error
- claim OAuth state once

### Provider Service
The provider service owns OAuth, encryption, token refresh, and account status. It should expose:
- provider configuration status
- account list for admin UI
- connect URL creation
- callback completion into one account row
- account enable, disable, priority update, and disconnect operations
- account-scoped access-token lookup and refresh
- pool access-token selection for the gateway

Token refresh must be account-scoped. If two requests concurrently need to refresh the same account, the implementation must avoid overwriting newer credentials with older refresh results. For the first implementation, this can be handled with an optimistic `updated_at`/token-expiry guard in SQL or a repository method that updates the row only if the row has not changed since it was read.

### Gateway Selection
The gateway should ask a pool selector for an access token and account id. The selector considers only enabled accounts. The simple selection order for this phase is:
1. lower `priority` first
2. accounts without recent errors before accounts with recent errors
3. older `last_used_at` first, with never-used accounts first
4. lower id as deterministic tie-breaker

If token lookup or refresh fails for one account, the selector marks that account error and tries the next eligible account. If every eligible account fails, the gateway returns a `503` OpenAI-compatible error with code `provider_accounts_unavailable`.

If the upstream HTTP request fails before response headers are written, the gateway may retry with another selected account. Once response headers or stream bytes have been sent, it must not retry; it should preserve the upstream response as-is.

## Admin API
Existing provider endpoints should remain stable where practical, but the account pool needs account-specific endpoints.

Protected endpoints:
- `GET /api/admin/providers/openai`
  - Returns provider configuration and account summary.
- `GET /api/admin/providers/openai/accounts`
  - Returns all OpenAI/Codex accounts with id, display name, subject, enabled, priority, expiry, last refresh, last use, last error, and timestamps.
- `POST /api/admin/providers/openai/connect`
  - Creates a callback state and returns an authorization URL.
- `PATCH /api/admin/providers/openai/accounts/{id}`
  - Accepts `{ "enabled": true|false, "priority": 100 }`.
  - Allows updating either field or both fields.
- `POST /api/admin/providers/openai/accounts/{id}/disconnect`
  - Deletes one account row and its stored encrypted credentials.

Public callback endpoint:
- `GET /oauth/openai/callback?code=...&state=...`
  - Claims state, exchanges the code, stores or updates one account, and redirects to the admin UI with connected or error status.

All admin endpoints must keep current admin-session protection. Account IDs are internal numeric IDs and must be validated before use.

## Frontend Design
The admin UI remains an operational dashboard. Replace the single provider card with an account-pool section after login:
- provider configuration status
- connect account button
- account table or compact account list
- account display name and subject
- enabled toggle
- priority input
- token expiry and last refresh time
- last used time
- last error and last error time
- disconnect action for one account

The UI should avoid hiding operational problems. If there are no enabled accounts, show that the gateway cannot serve requests until at least one account is enabled. If provider OAuth configuration is incomplete, keep connect disabled and show a short configuration state.

## Error Handling
- Missing OAuth config: admin connect returns `provider_not_configured`; gateway returns `provider_not_configured`.
- No connected accounts: gateway returns `provider_not_connected`.
- Connected accounts exist but all are disabled: gateway returns `provider_accounts_disabled`.
- All enabled accounts fail token lookup, refresh, or pre-stream upstream request: gateway returns `provider_accounts_unavailable`.
- OAuth callback replay or race: callback fails without storing credentials.
- Account update for a missing id returns `404 not_found`.
- Invalid priority or malformed JSON returns `400 invalid_input` or `bad_request` consistently with current handlers.

## Security Rules
- Never store cleartext access tokens, refresh tokens, authorization codes, OAuth states, session tokens, or client API keys.
- Never log cleartext OAuth tokens or callback codes.
- Store OAuth states only as hashes.
- Claim OAuth state once before credentials become durable, or persist state consumption and credentials in one transaction.
- Deleting or disconnecting one account must not affect other connected accounts.
- Admin account-pool endpoints must require a valid admin session.
- Gateway client authentication must continue to use existing API key verification.

## Testing Strategy
Backend tests should cover:
- migration defaults for new account metadata
- listing accounts in deterministic selection order
- saving multiple accounts under the same provider
- enabling, disabling, prioritizing, and deleting one account
- OAuth callback replay does not persist credentials twice
- concurrent callback attempts cannot both claim one state
- account-scoped access token decrypts unexpired tokens
- token refresh updates only the intended account
- selector skips disabled accounts
- selector falls back when one account token refresh fails
- gateway retries another account only before response headers are written
- gateway does not retry after streaming begins
- admin account endpoints reject unauthenticated requests

Frontend checks should continue to run:
- `bun run check`
- `bun run build`

Project verification should continue to run backend tests with writable caches:
- `GOMODCACHE=/root/Clouds/N2API/.cache/go-mod GOCACHE=/root/Clouds/N2API/.cache/go-build go test ./...`

## Deployment and Operations
The existing Docker Compose deployment remains the default. No new infrastructure service is required.

Deployment documentation should be updated with:
- required OAuth configuration
- how to connect multiple accounts
- what disabled accounts mean
- how gateway fallback works
- how to read last error fields
- how to back up PostgreSQL data before migration

## Acceptance Criteria
- Admin can connect more than one OpenAI/Codex account.
- Admin can see each account's enabled state, priority, expiry, refresh time, last use, and last error.
- Admin can enable, disable, reprioritize, and disconnect one account without affecting other accounts.
- Gateway requests use enabled accounts from the pool.
- Gateway falls back to another eligible account when token refresh or pre-stream upstream connection fails.
- Gateway preserves streaming once upstream response streaming begins.
- OAuth callback state is one-time and cannot persist credentials after a replay or concurrent claim.
- Backend tests pass with repository-local `GOMODCACHE` and `GOCACHE`.
- Frontend check and build pass.

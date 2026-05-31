# OpenAI OAuth Account Design

## Summary
This phase adds the first upstream account connection workflow for N2API. The admin can connect one Codex/OpenAI-oriented OAuth account, inspect its connection status, and disconnect it. The backend stores encrypted access and refresh tokens in PostgreSQL and exposes a small provider interface that future gateway routes can use to obtain a valid bearer token.

The implementation intentionally keeps this phase narrower than gateway forwarding. It does not add `/v1/*` proxy routes, request logging, model mapping, or Codex-specific adapter behavior. Those depend on having an upstream account primitive first.

## Documentation Basis
Current OpenAI API documentation describes API-key authentication for the public OpenAI REST API. It does not document a stable public OAuth authorization-code flow for using a personal OpenAI account as a generic `/v1/*` upstream. OpenAI documentation does show OAuth token shapes in connected-service contexts, including `access_token`, `refresh_token`, and `expires_in`.

Because the Codex/OpenAI account OAuth details may rely on endpoints that are not guaranteed by the public OpenAI API docs, N2API should implement a configurable OAuth2 account-connection boundary instead of hard-coding undocumented assumptions deep into gateway code. Default environment variable names remain OpenAI/Codex-oriented.

## Goals
- Add an admin-only OAuth connect flow for one OpenAI provider account.
- Persist provider account metadata in the existing `oauth_accounts` table.
- Encrypt access and refresh tokens at rest with the existing `secret` package.
- Store token expiry and last-refresh metadata.
- Provide a backend provider service that can return a valid access token and refresh it when expired.
- Add admin JSON endpoints for provider status, connect start, callback completion, and disconnect.
- Update the admin UI to show provider connection state and connect/disconnect controls.

## Non-Goals
- No `/v1/*` gateway forwarding in this phase.
- No Codex adapter routes in this phase.
- No model list, model mapping, or model configuration UI.
- No request logging UI.
- No multiple upstream providers.
- No multiple OpenAI accounts.
- No public user OAuth flow.
- No billing, quota, recharge, or SaaS account behavior.

## Backend Architecture
Add a new `internal/provider` package for provider-agnostic account logic and a focused `internal/provider/openai` package only where OpenAI/Codex naming or endpoint configuration is needed.

Core responsibilities:
- Build OAuth authorization URLs with `state`.
- Validate callback `state`.
- Exchange authorization codes for token responses.
- Encrypt token payloads before storing them.
- Decrypt stored tokens when a valid upstream access token is needed.
- Refresh tokens before or after expiry, depending on a small safety window.
- Disconnect the provider account by deleting or clearing stored OAuth credentials.

The admin service remains responsible for admin authentication and API keys only. HTTP handlers can depend on both `admin.Service` and the new provider account service.

## Configuration
Use the existing OpenAI/Codex environment variables:
- `OPENAI_OAUTH_CLIENT_ID`
- `OPENAI_OAUTH_CLIENT_SECRET`
- `OPENAI_OAUTH_REDIRECT_URL`

Add configurable endpoint values so undocumented or self-hosted-compatible OAuth endpoints do not become compile-time constants:
- `OPENAI_OAUTH_AUTH_URL`
- `OPENAI_OAUTH_TOKEN_URL`

If OAuth client credentials or endpoint URLs are missing, the provider status endpoint returns `configured: false`, and the UI disables the connect action with an operational message.

## Data Model
The existing `oauth_accounts` table is sufficient for this phase:
- `provider`: `openai`
- `subject`: account subject when available, otherwise an empty string for the single-account V1 record
- `display_name`: email, username, or provider label when available
- `encrypted_access_token`
- `encrypted_refresh_token`
- `access_token_expires_at`
- `last_refresh_at`

OAuth callback state should be stored separately from access credentials. Add an `oauth_states` table with:
- `id BIGSERIAL PRIMARY KEY`
- `provider TEXT NOT NULL`
- `state_hash TEXT NOT NULL UNIQUE`
- `redirect_after TEXT NOT NULL DEFAULT '/';`
- `expires_at TIMESTAMPTZ NOT NULL`
- `consumed_at TIMESTAMPTZ`
- `created_at TIMESTAMPTZ NOT NULL DEFAULT now()`

State tokens are random bearer secrets. Store only their hashes, expire them quickly, and mark them consumed after a successful callback.

## Admin API
All endpoints return JSON except the OAuth callback, which redirects back to the admin UI after completing or failing the flow.

Protected endpoints:
- `GET /api/admin/providers/openai`
  - Success: `200 { "provider": "openai", "configured": true, "connected": false, "displayName": "", "accessTokenExpiresAt": null, "lastRefreshAt": null }`
- `POST /api/admin/providers/openai/connect`
  - Success: `200 { "authorizationUrl": "https://..." }`
  - Failure when not configured: `409 { "error": "provider_not_configured" }`
- `POST /api/admin/providers/openai/disconnect`
  - Success: `204`
  - Idempotent if no account is connected.

Public callback endpoint:
- `GET /oauth/openai/callback?code=...&state=...`
  - Valid callback: exchanges the code, stores encrypted tokens, and redirects to `/?provider=openai&status=connected`
  - Invalid callback: redirects to `/?provider=openai&status=error`

The callback is public because the OAuth provider calls it, but it must only complete when the `state` matches an unexpired, unconsumed state created by an authenticated admin session.

## Frontend Design
The admin UI adds a provider panel above API key management after login. The panel shows:
- provider name: OpenAI/Codex
- configuration state
- connection state
- display name when available
- access token expiry when available
- last refresh time when available
- connect and disconnect controls

Clicking connect calls the protected connect endpoint and navigates the browser to the returned authorization URL. After the callback redirects back to the admin UI, the UI reloads provider status.

The unauthenticated login screen remains focused on admin access. Provider controls are not visible until the admin session is valid.

## Error Handling
- Missing OAuth config returns `provider_not_configured`.
- Invalid or expired state redirects to the UI with an error status.
- Token exchange failures do not store partial credentials.
- Token refresh failures keep the existing encrypted refresh token unless the provider explicitly indicates it is invalid.
- Provider service methods return typed errors so future gateway routes can distinguish missing account, expired refresh credentials, and transient upstream errors.

## Security Rules
- Never store cleartext access tokens, refresh tokens, authorization codes, or OAuth state values.
- Never log access tokens, refresh tokens, authorization codes, or OAuth state values.
- Store only hashes of OAuth state tokens.
- Require an authenticated admin session to create an OAuth connect URL.
- Keep callback state TTL short, 10 minutes for V1.
- Consume callback state once so replayed callbacks fail.
- Use the configured `OPENAI_OAUTH_REDIRECT_URL` exactly when building the authorization URL and exchanging the code.

## Testing Strategy
Backend tests should cover:
- provider status when unconfigured, configured but disconnected, and connected
- connect URL creation stores only hashed state
- callback rejects missing, expired, consumed, or unknown state
- callback exchanges a code and stores encrypted tokens
- disconnect is idempotent
- access-token lookup decrypts a valid unexpired token
- refresh path exchanges a refresh token and updates encrypted stored credentials
- admin provider endpoints reject unauthenticated requests

Frontend checks should cover Svelte validation and production build. Browser-level testing can stay optional for this phase unless the provider panel introduces layout regressions.

## Acceptance Criteria
- Admin can see whether the OpenAI/Codex provider is configured and connected.
- Admin can start the OAuth connect flow only when provider configuration is complete.
- OAuth callback stores encrypted provider tokens in PostgreSQL.
- OAuth state values are single-use, short-lived, and stored only as hashes.
- Admin can disconnect the provider account.
- Backend exposes a provider service method that future gateway routes can call to obtain a valid access token.
- Backend tests pass with `GOCACHE=/root/Clouds/N2API/.cache/go-build go test ./...`.
- Frontend checks pass with `bun run check` and `bun run build`.

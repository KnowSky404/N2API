# Admin Auth and API Keys Design

## Summary
This phase makes the N2API admin surface usable and gives future gateway routes a secure client authentication primitive. It adds single-admin login, PostgreSQL-backed admin sessions, protected admin JSON APIs, and client API key lifecycle management.

The scope stays personal and self-hosted. There is one administrator account, no public registration, no user management, no teams, no billing, and no multi-tenant account model.

## Goals
- Bootstrap the configured administrator into PostgreSQL at service startup.
- Authenticate the admin UI with an `HttpOnly` cookie-backed session.
- Protect admin JSON APIs behind the admin session.
- Let the admin create, list, and revoke client API keys.
- Store only API key hashes in PostgreSQL and reveal plaintext API keys only once.
- Provide a backend API key authenticator that later `/v1/*` gateway routes can reuse.
- Update the admin UI with a login view and an API key management view.

## Non-Goals
- No public user registration.
- No multiple admin accounts in V1.
- No password reset emails, invitations, or account recovery flows.
- No JWT access or refresh token system for admin auth.
- No platform billing, balances, quota packages, recharge, invoices, or payment providers.
- No implementation of OpenAI OAuth or `/v1/*` gateway forwarding in this phase.
- No Redis-backed sessions.

## Backend Architecture

### Bootstrap
On startup, the backend ensures the configured administrator exists in the `admins` table. The source values are:
- `N2API_ADMIN_USERNAME`
- `N2API_ADMIN_PASSWORD`

The password is hashed before storage. If the admin row does not exist, the service inserts it. If the row exists, this phase keeps the existing password hash unchanged to avoid silently changing credentials on every restart. Explicit password rotation can be added later as an admin action.

### Sessions
Admin login creates an opaque random session token. The browser receives only the token in a `Set-Cookie` header:
- Name: `n2api_admin_session`
- Flags: `HttpOnly`, `SameSite=Lax`, `Path=/`
- `Secure` is enabled when `N2API_PUBLIC_URL` uses `https://`

The database stores only a SHA-256 hash of the session token, plus admin id, creation time, expiration time, and optional revocation time. Session TTL is fixed for V1 at 7 days.

Logout revokes the current session and clears the cookie. A request with a missing, expired, revoked, or unknown session is unauthenticated.

### API Keys
Client API keys are generated server-side with enough entropy for bearer-token use. The cleartext key is returned only by the create endpoint response. PostgreSQL stores:
- key hash
- short prefix for display and troubleshooting
- operator-provided name
- `created_at`
- `last_used_at`
- `revoked_at`

Listing keys never returns the cleartext secret. Revocation sets `revoked_at`; keys are not physically deleted in V1 so logs can retain a stable relationship to the old key id.

### Store Layer
The store package should gain focused repository methods instead of embedding SQL in HTTP handlers. The required responsibilities are:
- bootstrap admin
- verify admin credentials
- create, lookup, and revoke admin sessions
- create, list, revoke, and authenticate client API keys

The existing `secret` package remains the boundary for password hashing, API key hashing, and token generation helpers.

## Admin API

All admin API responses are JSON.

Public endpoints:
- `POST /api/admin/login`
  - Request: `{ "username": "...", "password": "..." }`
  - Success: `200 { "username": "admin" }` and session cookie
  - Failure: `401 { "error": "invalid_credentials" }`
- `POST /api/admin/logout`
  - Always clears the cookie.
  - Returns `204`.

Protected endpoints:
- `GET /api/admin/me`
  - Success: `200 { "username": "admin" }`
  - Failure: `401 { "error": "unauthorized" }`
- `GET /api/admin/keys`
  - Success: `200 { "keys": [...] }`
  - Each key includes `id`, `name`, `prefix`, `createdAt`, `lastUsedAt`, and `revokedAt`.
- `POST /api/admin/keys`
  - Request: `{ "name": "codex laptop" }`
  - Success: `201 { "key": { ... }, "secret": "n2api_..." }`
  - The `secret` field is returned only once.
- `POST /api/admin/keys/{id}/revoke`
  - Success: `200 { "key": { ... } }`
  - Revoking an already revoked key is idempotent.

Existing health endpoints keep their current behavior:
- `GET /healthz` remains public.
- `GET /api/admin/health` remains public for the dashboard connection indicator.
- `GET /api/admin/bootstrap` remains public and returns only non-secret UI bootstrap values.

## Frontend Design
The admin UI remains an operational dashboard, not a landing page.

When unauthenticated, the first screen is a compact login view using the existing visual language from `DESIGN.md` and the current dashboard shell. It posts to `/api/admin/login`, handles invalid credentials inline, and then reloads admin state from `/api/admin/me`.

When authenticated, the dashboard shows:
- current backend and database health
- signed-in admin username
- logout control
- API key table with name, prefix, creation time, last used time, and revoked state
- create-key form with a name input
- one-time secret reveal after key creation
- revoke action for active keys

The one-time secret should be visually distinct and easy to copy. After it is dismissed or the page reloads, it cannot be recovered from the backend.

## Data Model Changes
Add an `admin_sessions` table:
- `id BIGSERIAL PRIMARY KEY`
- `admin_id BIGINT NOT NULL REFERENCES admins(id) ON DELETE CASCADE`
- `token_hash TEXT NOT NULL UNIQUE`
- `expires_at TIMESTAMPTZ NOT NULL`
- `revoked_at TIMESTAMPTZ`
- `created_at TIMESTAMPTZ NOT NULL DEFAULT now()`

Add indexes for active session lookup and expiration cleanup:
- `admin_sessions_token_hash_idx`
- `admin_sessions_expires_at_idx`

The existing `client_api_keys` table is sufficient for V1 API key storage.

## Security Rules
- Never log admin passwords, session tokens, or cleartext client API keys.
- Never store cleartext session tokens or client API keys.
- Login failures return a generic `invalid_credentials` response.
- Session cookies use `HttpOnly` and `SameSite=Lax`.
- Admin endpoints must reject unauthenticated requests before running business logic.
- Revoked API keys must fail authentication.
- Request handlers should use bounded JSON body parsing to avoid unbounded memory reads.

## Testing Strategy
Backend tests should cover:
- admin bootstrap creates the admin once and preserves an existing hash
- password verification accepts valid credentials and rejects invalid credentials
- login sets the session cookie and stores a hashed session
- logout revokes the session and clears the cookie
- protected endpoints reject missing or invalid sessions
- API key creation returns a one-time secret and stores only a hash
- API key list omits cleartext secrets
- API key revoke is idempotent
- API key authentication accepts active keys and rejects revoked or unknown keys

Frontend checks should cover compile-time Svelte validation and production build. Browser-level tests are optional in this phase unless the UI grows beyond the described login and key management workflow.

## Acceptance Criteria
- A fresh database starts with one configured admin account.
- The admin can log in, refresh the page, remain authenticated until session expiry or logout, and log out.
- Admin-only endpoints return `401` without a valid session.
- The admin can create, list, and revoke client API keys.
- Cleartext client API keys are shown only in the create response.
- Backend tests pass with `GOCACHE=/root/Clouds/N2API/.cache/go-build go test ./...`.
- Frontend checks pass with `bun run check` and `bun run build`.

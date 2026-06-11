# Codex OAuth Account Pool Design

## Goal

N2API should manage multiple independent Codex/OpenAI OAuth upstream accounts from the admin UI. Each account has isolated OAuth state, encrypted tokens, operator-visible identity metadata, scheduling state, refresh state, and gateway health state.

## Scope

This design covers Codex/OpenAI OAuth accounts only. It does not add public registration, billing, user balances, payment flows, account groups, CRS sync, Redis, or broad multi-tenant SaaS behavior.

## Account Model

`oauth_accounts` remains the storage table but becomes the upstream account pool. Each row is one upstream Codex OAuth account.

Existing fields continue to store encrypted access/refresh/id tokens, token expiry, enabled flag, priority, last use, errors, and metadata. New fields add account-pool behavior:

- `name`: operator label shown in the UI.
- `status`: `active`, `disabled`, `rate_limited`, `circuit_open`, or `expired`.
- `status_reason`: short operator-visible reason for the status.
- `fingerprint_hash`: SHA-256 hash derived from the initiating browser request fingerprint.
- `user_agent_hash`: SHA-256 hash of the browser user agent used to start OAuth.
- `ip_hash`: SHA-256 hash of the client IP observed by N2API when OAuth starts.
- `failure_count`: consecutive gateway or refresh failures.
- `circuit_open_until`: account is skipped until this time.
- `rate_limited_until`: account is skipped until this time.
- `last_refresh_error` and `last_refresh_error_at`: refresh-specific diagnostic state.

Identity metadata lives in `metadata`: `email`, `chatgpt_account_id`, `chatgpt_user_id`, `organization_id`, `plan_type`, `client_id`, and `access_token_sha256`.

## OAuth State Isolation

Starting OAuth creates an `oauth_states` row that contains all pending-account context:

- Hashed state.
- Encrypted PKCE verifier and verifier hash.
- OAuth client id.
- Redirect target.
- Pending account name, enabled flag, priority.
- Optional target account id for re-authorizing an existing account.
- Fingerprint hash, user agent hash, and IP hash.

Callback handling claims exactly one unconsumed state row. The callback exchanges the code with that state's PKCE verifier and client id, then creates or updates one account.

If `target_account_id` is present, callback updates only that account after validating it belongs to the same provider. If no target id is present, callback deduplicates by identity keys: `chatgpt_account_id`, `chatgpt_user_id`, lowercase email, then access-token fingerprint. If a match exists, token fields and metadata update that account; otherwise a new account row is inserted.

## Gateway Selection

The gateway selects from accounts where:

- `enabled = true`
- `status` is `active` or blank
- `circuit_open_until` is null or in the past
- `rate_limited_until` is null or in the past
- `access_token_expires_at` is usable or refresh succeeds

Ordering is priority ascending, never-used first, then oldest last-used, then id ascending.

If token refresh fails, N2API records refresh error state and increments `failure_count`. After three consecutive failures it opens the circuit for five minutes. If token retrieval succeeds, refresh and gateway error state is cleared.

If an upstream attempt fails before streaming starts, gateway retries another eligible account. A rate-limit error marks `rate_limited_until`; repeated non-rate-limit failures can open the circuit. Once streaming starts, gateway preserves the stream and does not retry.

## Admin API

The existing provider routes stay for compatibility but move toward account-pool semantics:

- `GET /api/admin/providers/openai/accounts`: list account rows.
- `POST /api/admin/providers/openai/connect`: start OAuth for a new account; accepts `name`, `priority`, `enabled`, `fingerprint`.
- `POST /api/admin/providers/openai/accounts/{id}/reauthorize`: start OAuth bound to an existing account.
- `POST /api/admin/providers/openai/accounts/{id}/refresh`: manually refresh one account's OAuth token.
- `PATCH /api/admin/providers/openai/accounts/{id}`: update `name`, `enabled`, and `priority`.
- `POST /api/admin/providers/openai/accounts/{id}/disconnect`: delete one account.

## Admin UI

The provider section becomes "Codex OAuth accounts". It supports:

- Add account: label, priority, enabled flag, then OAuth login.
- Per-account enable/disable, priority edit, rename, refresh token, re-authorize, and disconnect.
- Visible status, email, ChatGPT account id, plan type, token expiry, last refresh, last used, current error, and circuit/rate-limit timers.

The UI records a lightweight browser fingerprint payload for the OAuth start request. The server stores only hashes.

## Verification

Backend tests must prove:

- OAuth state stores pending account fields and fingerprint hashes.
- New OAuth callback creates a distinct account.
- Re-authorize callback updates the target account, not another matching account.
- Duplicate OAuth identity updates an existing account when no target account is supplied.
- Selection skips disabled, rate-limited, circuit-open, and expired-unrefreshable accounts.
- Refresh failure opens the circuit after the configured threshold.
- Rate-limit marking skips the account until reset.

Frontend checks must prove the admin UI still type-checks and builds. Docker Compose must build and serve the app plus API status endpoint.

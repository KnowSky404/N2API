# Admin Security Hardening Plan

Status: planned
Public API changes: additive admin endpoints; stricter mutation and proxy behavior
Data migration: additive session metadata in Task 4

## Current Baseline And Threat Decision

Sessions are random, hashed in PostgreSQL, HttpOnly, SameSite=Lax, and scoped to
`/`. Login errors are uniform. However, `clientIP` and OAuth absolute URL logic
trust forwarding headers from any peer, while System Event source IP uses the
direct address. Login has no throttle, session TTL is hard-coded, and admin
responses lack a common security policy.

The first CSRF control is same-origin validation for cookie-authenticated unsafe
methods, not a token. CORS stays disabled and JSON-only handlers remain. This
matches the current same-origin admin architecture and avoids token complexity
without evidence it is needed.

## Task 1: Enforce Trusted Proxy Boundaries

Status: completed locally on 2026-07-21; real reverse-proxy acceptance remains
manual.

### Goal

Make audit, OAuth fingerprint, scheme, and host derivation use one trustworthy
request source.

### Dependencies

None; login throttling depends on this task.

### Files

- Modify: `backend/internal/config/config.go`, `config_test.go`
- Create: `backend/internal/httpapi/request_info.go`, `request_info_test.go`
- Modify: `backend/internal/httpapi/server.go`, `system_event.go`, related tests
- Modify: `.env.example`, `docs/manual.md`
- Test: config and HTTP API packages
- Migrate: none
- Document: trusted proxy examples and multi-hop order

### Implementation

1. Parse `N2API_TRUSTED_PROXY_CIDRS`; empty trusts none and invalid CIDR fails
   startup.
2. Normalize IPv4, IPv4-mapped IPv6, bracketed IPv6, and port forms.
3. Use forwarded values only when the direct peer is trusted. Walk XFF from
   right to left, skipping trusted hops, and choose the first untrusted address.
4. Apply the same resolved information to audit and OAuth code. Support RFC
   `Forwarded` only after parser tests cover quoted/escaped values.

### Tests And Verification

Table-test direct/untrusted, one proxy, multiple proxies, malformed headers,
IPv4/IPv6, and invalid configuration; run `go test ./internal/config
./internal/httpapi`.

### Compatibility And Security

Deployments currently relying on forwarding headers must configure CIDRs.
Default behavior becomes fail-closed.

### Risks And Rollback

Incorrect proxy chains can change observed IP or callback URL. Roll back the
resolver commit and unset the new variable.

### Manual Acceptance

Verify one real reverse-proxy deployment.

### Completion Criteria

Untrusted peers cannot spoof client address, scheme, or host.

Local resolver, configuration, OAuth callback, fingerprint, and System Event
tests pass. Final deployment acceptance requires confirming the direct peer and
multi-hop chain against the owner's real reverse proxy configuration.

### Commit

`feat(security): enforce trusted proxy boundaries`

## Task 2: Throttle Administrator Login

### Goal

Bound password guessing by normalized IP and username without enumeration.

### Dependencies

Task 1.

### Files

- Create: `backend/internal/admin/login_throttle.go`, tests
- Modify: `backend/internal/httpapi/server.go`, `server_test.go`
- Modify: `backend/internal/systemevent/event.go` if a new aggregate action is needed
- Modify: `.env.example`, `docs/manual.md`
- Test: admin and HTTP API packages
- Migrate: none for the recommended single-node implementation

### Implementation

Use a bounded expiring in-memory map for IP and normalized username. Apply
exponential delay/temporary denial after configured failures, return integer
`Retry-After`, use a dummy password hash for unknown users, reset the successful
identity, and aggregate repeated audit events by window.

### Tests And Verification

Use an injected clock to test thresholds, expiry, maximum entries, IP and user
independence, successful reset, uniform bodies, and `Retry-After`.

### Compatibility And Security

Restarts reset throttle state, an accepted single-node tradeoff documented to
operators. No username existence signal is added.

### Risks And Rollback

Aggressive defaults can lock out the owner. Conservative defaults and expiry
are mandatory; disable through configuration to roll back.

### Manual Acceptance

Required for proxy deployments to confirm the correct client IP is throttled.

### Completion Criteria

Both dimensions enforce a bounded retry schedule and emit one aggregated event.

### Commit

`feat(security): throttle admin login attempts`

## Task 3: Add Browser Request Policy And Security Headers

### Goal

Protect cookie-authenticated mutations and consistently harden browser responses.

### Dependencies

Task 1 for trusted external origin derivation.

### Files

- Create: `backend/internal/httpapi/security_middleware.go`, tests
- Modify: `backend/internal/httpapi/server.go`
- Test: static UI, OAuth callback, admin mutation, export, and SSE handlers
- Migrate: none
- Document: `docs/manual.md`

### Implementation

Reject cross-origin cookie-authenticated unsafe methods using `Origin` and
Fetch Metadata while allowing same-origin and intentional non-browser requests
without `Origin`. Add `nosniff`, Referrer Policy, Permissions Policy, frame
ancestors/CSP, HTTPS-only HSTS, and `no-store` for sensitive admin/auth output.

### Tests And Verification

Run HTTP tests plus `bun run check`, `bun test`, and a Playwright login/export
smoke against the built app.

### Compatibility And Security

CSP must allow SvelteKit assets and the existing OAuth callback page but no
unreviewed remote script. SSE and downloads retain correct content types.

### Risks And Rollback

A strict CSP can blank the UI. Keep the middleware isolated and revert its
registration if browser verification fails.

### Manual Acceptance

Required on HTTP local and HTTPS reverse-proxy deployments.

### Completion Criteria

Cross-origin mutations fail before handlers; required pages and streams work.

### Commit

`feat(security): protect admin browser requests`

## Task 4: Add Session Controls

### Goal

Make TTL configurable and allow the owner to inspect and revoke sessions.

### Dependencies

Tasks 1 and 3.

### Files

- Create: `backend/internal/store/migrations/00038_admin_session_metadata.sql`
- Modify: admin service/repository, HTTP server, config, `.env.example`
- Modify: `frontend/src/lib/admin-state.svelte.js`, account/settings UI and tests
- Test: migrations, store, admin, HTTP, frontend, Playwright
- Document: `docs/manual.md`

### Implementation

Store last-used time, created IP summary, and user-agent summary without raw
secrets. Add list, revoke one, and revoke all other endpoints. Rotate session on
login by always issuing a new token. Present the owner decision for whether
password change revokes other sessions; recommended default is yes.

### Tests And Verification

Test expiry, ownership, current-session protection, revoke-other behavior,
cookie clearing, and UI confirmation.

### Compatibility And Security

TTL defaults to the current seven days. Metadata is bounded and redacted.

### Risks And Rollback

Old rows have empty metadata. The additive migration may remain on rollback.

### Manual Acceptance

Required for revoke-current and revoke-other flows.

### Completion Criteria

The owner can identify and revoke every active session.

### Commit

`feat(auth): add administrator session controls`

## Task 5: Reject Unsafe Startup Configuration

### Goal

Catch placeholder and internally inconsistent production settings before listen.

### Dependencies

Task 1 config structure.

### Files

- Modify: `backend/internal/config/config.go`, `config_test.go`
- Modify: `.env.example`, `docs/manual.md`, deployment Compose files if overrides are needed
- Test: config package and Compose config validation
- Migrate: none

### Implementation

Parse `N2API_PUBLIC_URL`; reject known placeholders, short admin/encryption
secrets, equal password/key values, unsupported origin components, and unsafe
HTTP upstreams. For public HTTP, public bind, or disabled database TLS, require
an explicit development/risk override rather than guessing environment.

### Tests And Verification

Table-test every invalid combination and assert errors contain variable names
but no values. Validate documented local and release Compose configurations.

### Compatibility And Security

This is intentionally stricter and needs release notes. `.env.example` remains
a template, not a safe production secret source.

### Risks And Rollback

Operators with weak legacy configuration may fail startup; provide exact
remediation and roll back only the validation commit if emergency access is
required.

### Completion Criteria

Known unsafe production combinations cannot start silently.

### Commit

`feat(config): reject unsafe deployment settings`

## Task 6: Evaluate Optional TOTP

### Goal

Add TOTP only after the owner confirms it is required.

### Dependencies

Tasks 2-5 and explicit owner approval.

### Files

- Planned migration and admin/auth/frontend files, chosen after threat review
- Test: secret encryption, recovery codes, replay window, login flow
- Document: recovery procedure

### Completion Criteria

No implementation begins without a recovery design and owner decision.

### Commit

`feat(auth): add optional administrator TOTP`

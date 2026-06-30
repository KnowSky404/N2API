# OAuth Default Codex Fingerprint Design

## Summary

Codex OAuth provider accounts should always have a stable outbound identity profile. When an admin adds or reauthorizes a Codex OAuth account without choosing a custom fingerprint profile, N2API should attach a built-in default Codex CLI profile. If the admin chooses a custom profile, that explicit choice wins.

This makes OAuth accounts usable out of the box while keeping fingerprint behavior visible, editable, and scoped to personal self-hosted gateway use.

## Goals

- Automatically bind a default fingerprint profile to new Codex OAuth accounts when no custom profile is selected.
- Keep custom profile selection available during OAuth connect and through provider account editing.
- Make the default profile represent a Codex CLI-style client, not a browser device.
- Preserve account-level stability: the same account should keep the same profile until the admin changes it.
- Fix the current persistence gap where OAuth callback account creation can carry `FingerprintProfileID` in memory but not insert it into `provider_accounts`.
- Keep the feature compatible with API-upstream accounts and the existing fingerprint profile CRUD.

## Non-Goals

- No browser fingerprint simulation such as canvas, WebGL, fonts, cookies, or browser storage.
- No claim that N2API can reproduce OpenAI's private Codex CLI network fingerprint exactly.
- No per-request randomization of user agents or TLS fingerprints.
- No new Redis requirement or external fingerprint service.
- No source-code copying from sub2api, CLIProxyAPI, OpenClaw, Hermes, or other reference projects.
- No automatic proxy or residential-IP assignment. Proxy selection remains account/network configuration, not fingerprint profile data.

## Reference Findings

### sub2api

sub2api separates identity-like request metadata from TLS fingerprinting.

- Its identity service caches per-account application-layer values such as `User-Agent`, `X-Stainless-*` headers, and a generated client id.
- Missing client headers fall back to default CLI-oriented values.
- Existing cached identity is preserved and only merged forward when a newer client user agent appears.
- TLS fingerprint profiles are separate and are applied through uTLS-aware HTTP upstream clients.

For N2API, the useful pattern is account-level stable identity with conservative defaults. The Claude-specific `X-Stainless-*` fields do not directly apply to Codex.

### CLIProxyAPI

CLIProxyAPI treats Codex OAuth as a CLI login/runtime path.

- Codex OAuth uses PKCE and localhost callback behavior.
- Codex request defaults include a `codex_cli_rs/...` user agent and `Originator: codex_cli_rs`.
- Codex websocket requests can also use Codex-specific beta feature headers.
- Downstream client user agents are not blindly forwarded for OAuth Codex paths; configured/default Codex headers are preferred.

For N2API, this supports a default Codex CLI profile rather than a browser-like Chrome profile.

### OpenClaw

OpenClaw's OpenAI/Codex OAuth implementation is plugin/runtime oriented.

- It uses the public Codex OAuth flow with PKCE, localhost callback, `codex_cli_simplified_flow=true`, and an `originator` value.
- It performs TLS/network preflight diagnostics for OAuth login, but it does not model a gateway account fingerprint profile in the same way N2API does.

For N2API, OpenClaw is mainly evidence that OAuth login/runtime identity should stay explicit and provider-scoped.

### Hermes

The exact Hermes reference repository was not identified during design. If the user provides the URL later, compare it before changing the final implementation plan.

## Fingerprint Scope

N2API should define fingerprint profiles as outbound request identity overlays, not full device emulation.

In scope:

- `User-Agent`
- custom HTTP headers such as `originator`
- route-owned Codex headers already applied by the gateway, such as `OpenAI-Beta` and `chatgpt-account-id`
- optional TLS ClientHello family via the existing uTLS support

Out of scope:

- browser-only attributes
- cookies and browser sessions
- IP reputation or region
- request timing, retry pacing, and concurrency behavior
- account proxy assignment

The gateway should continue to own route-specific headers. The fingerprint profile should own reusable identity fields that are safe to apply across supported OAuth requests.

## Default Codex CLI Profile

Create or ensure one built-in fingerprint profile:

- Name: `Default Codex CLI`
- Description: `Built-in Codex CLI-style outbound identity for OAuth accounts.`
- Enabled: `true`
- User agent: the current gateway Codex default user agent, initially `codex_cli_rs/0.125.0 (Ubuntu 22.4.0; x86_64) xterm-256color`
- TLS fingerprint: empty or `golang`
- Headers:
  - `originator: codex_cli_rs`

The implementation should prefer a single durable profile row rather than creating a new profile per account. A durable shared row makes the default visible and editable while keeping accounts stable by reference.

The first implementation should not use a browser TLS preset such as `chrome` as the Codex default. Codex is a CLI client, and N2API already emits Codex CLI headers on Codex OAuth request paths.

## Data Model

The existing schema is mostly sufficient:

- `fingerprint_profiles`
- `provider_accounts.fingerprint_profile_id`
- `oauth_states.pending_fingerprint_profile_id`

Required correction:

- `ProviderRepository.SaveAccount` must persist `FingerprintProfileID` when inserting a new provider account.
- Reconnect/update-by-id paths should keep preserving the current account profile unless a new explicit profile selection is present.

No new table is required for this slice.

## Backend Behavior

### Default Profile Resolution

Add a provider service/store operation that resolves the default Codex fingerprint profile id:

1. Look up a built-in/default Codex CLI profile by stable marker.
2. If missing, create it.
3. Return its id.

Because `fingerprint_profiles` currently has no stable system key column, the implementation can either:

- add a small `system_key TEXT NOT NULL DEFAULT ''` column with a unique partial index, or
- identify the built-in row by exact name.

Recommendation: add `system_key`. It avoids fragile name matching and lets the admin rename the display name later without breaking default resolution.

### OAuth Connect

When parsing `POST /api/admin/provider-accounts/codex-oauth/connect`:

- `fingerprintProfileId > 0`: validate and store that id in the pending OAuth state.
- `fingerprintProfileId == 0` or omitted: resolve the default Codex CLI profile id and store that id in the pending OAuth state.

The OAuth state remains the handoff point, because the account row is only known after callback completion.

### OAuth Callback

When completing the OAuth callback:

- If `PendingFingerprintProfileID` exists and this is a new account, set `account.FingerprintProfileID`.
- If reauthorizing a target account, allow the pending profile to update the target account.
- If reconnecting an existing identity without target account id, preserve the existing profile unless the design later introduces an explicit reconnect override.

This matches the existing scheduling-field preservation policy: user-controlled account configuration should not be overwritten accidentally.

### Gateway Request Construction

The gateway already applies selected account fingerprint data:

- non-empty profile user agent overrides `User-Agent`
- custom headers are set on the upstream request
- non-empty TLS fingerprint is applied through request context

Keep route-specific Codex OAuth behavior:

- Codex `/v1/responses` continues setting `chatgpt-account-id`, `Accept`, `OpenAI-Beta`, `originator`, default Codex user agent, and JSON content type.
- The selected fingerprint profile then overlays reusable fields such as `User-Agent` and `originator`.

## Admin UI Behavior

OAuth add modal:

- The fingerprint profile selector keeps its current behavior.
- The empty/default option should communicate that N2API will use the built-in Codex CLI default.
- The request can still send `0` for default; backend resolves the actual profile id.

Provider account edit:

- Existing account profile selector keeps `None` available for advanced users who intentionally want no profile.
- Clearing a profile remains explicit and should not be confused with the OAuth add default.

Fingerprint Profiles page:

- The built-in profile appears as a normal profile.
- If `system_key` is added, deletion behavior should be decided in implementation planning. Recommendation: either block deletion of built-in profiles or recreate them on next OAuth connect.

## Error Handling

- If default profile creation fails, OAuth connect should fail with a clear admin error instead of creating an account with ambiguous fingerprint state.
- If a custom profile id is disabled or missing, keep the existing invalid-input behavior.
- If a default profile is later disabled, backend default resolution should either re-enable it or reject OAuth connect with a clear message. Recommendation: re-enable only during explicit default resolution and document that built-in defaults are managed by N2API.

## Testing

Backend tests should cover:

- default profile is created or reused when OAuth connect omits `fingerprintProfileId`
- custom profile id still wins
- OAuth callback persists `fingerprint_profile_id` on new account insert
- target-account reauthorization can update the profile
- existing-identity reconnect without target id preserves the current profile
- selected account applies default profile user agent and headers
- migration for `fingerprint_profiles.system_key`, if added

Frontend tests should cover:

- OAuth add flow sends default sentinel when no custom profile is selected
- the add-account UI labels the default behavior
- account edit can still clear an existing profile

Verification should include:

- `go test ./...` from `backend/` with the project cache variables
- `bun run check`
- `bun run build`

## Rollout

This is a small incremental change:

1. Add system-key support for built-in fingerprint profiles if chosen in the implementation plan.
2. Ensure the default Codex CLI profile from the provider service.
3. Resolve default profile id during OAuth connect.
4. Persist `fingerprint_profile_id` in provider account inserts.
5. Adjust frontend copy and focused tests.
6. Rebuild and refresh the local Docker Compose stack after code changes.

Existing OAuth accounts without a profile do not need an automatic migration in the first slice. Admins can reconnect or edit them. A later optional maintenance action can bulk-assign the default profile to existing Codex OAuth accounts with `NULL` profile ids.

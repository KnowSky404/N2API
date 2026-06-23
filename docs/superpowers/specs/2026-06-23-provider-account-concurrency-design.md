# Provider Account Concurrency Design

## Context

sub2api exposes account-level scheduling controls that include an account `concurrency` value and runtime `current_concurrency` signal. N2API already has global gateway limits, per-key limits, and a global per-account concurrency limit in Gateway Settings. It also has provider account priority, load factor, health status, model capability, sticky-session routing, and account fallback before streaming starts.

The current gap is that one slow or fragile upstream account cannot be given a lower concurrency cap than the rest of the pool. This makes scheduling less precise than sub2api's account management model and forces the operator to use one global per-account value.

## Goal

Add an optional provider-account-level gateway concurrency cap. A value of `0` means "inherit the Gateway Settings per-account limit"; a positive value overrides that default for the selected account.

## Non-Goals

- Do not add user-facing billing, subscription, tenant groups, recharge balances, or public SaaS account concepts.
- Do not add Redis or a distributed limiter for V1.
- Do not add long-running channel monitor templates or cron-plan CRUD in this slice.
- Do not expose current live concurrency counters in the UI yet; this slice only configures the cap and enforces it during gateway routing.

## Data Model

Add `provider_accounts.max_concurrent_requests INTEGER NOT NULL DEFAULT 0`.

Rules:

- `0` inherits `GatewaySettings.MaxConcurrentRequestsPerAccount`.
- Positive values cap that account independently.
- Negative values are invalid.
- Existing accounts migrate to `0`, preserving current behavior.

## Backend Behavior

`provider.Account` and admin-facing account responses include `maxConcurrentRequests`.

Provider account update paths accept `MaxConcurrentRequests`:

- single account update can set it to `0` or a positive integer;
- bulk scheduling update can set it across selected accounts;
- invalid negative values return `ErrInvalidInput`.

Account selection returns `SelectedAccount.MaxConcurrentRequests`. The gateway computes an effective account limit for each selected account:

- if selected account override is greater than `0`, use it;
- otherwise use the current gateway setting;
- if the effective limit is `0`, do not limit that account.

Fallback behavior stays the same: when one selected account is at its concurrency cap, the proxy excludes it and tries the next schedulable account before streaming begins. If all candidates are capped, the response remains `429 rate_limit_exceeded`.

## Frontend Behavior

Provider account rows show an editable **Max concurrency** numeric input beside priority/load factor controls. `0` is displayed and saved as "inherit global".

The bulk scheduling controls gain an optional **Bulk max concurrency** input. Applying it with selected accounts updates the same account field across the selected rows.

Gateway Settings remains the place for the default global per-account value.

## Documentation

README and deploy notes should explain:

- Gateway Settings contains the default per-account concurrency limit.
- Provider account **Max concurrency** can override that default.
- `0` inherits the gateway default and does not disable routing by itself.

## Testing

Use TDD for each layer:

- migration and SQL scan tests for the new column and non-negative constraint;
- provider service tests for validation and selection DTO propagation;
- gateway proxy test proving account-specific caps override the global value and trigger fallback;
- HTTP admin tests for single and bulk update payloads;
- frontend source tests for the new input, state field, and bulk action;
- documentation test requiring README and deploy notes to describe override semantics.

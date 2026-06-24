# Gateway Scheduling Health Summary Design

## Goal

Show a compact provider-account scheduling health summary on the Gateway management page so the operator can see account-pool capacity and blocked-account causes without opening the Provider accounts table.

## Context

N2API already has provider-account health state, schedulability helpers, Gateway readiness, and detailed Provider accounts rows. The Gateway page loads provider accounts for readiness but only shows total and schedulable counts. A sub2api-style gateway management workflow benefits from a quick operational view of which exits are usable, disabled, rate-limited, circuit-open, or expired.

This slice is diagnostic only. It does not change scheduler order, account selection, rate-limit behavior, token refresh, or persistence.

## Scope

In scope:

- Reuse existing frontend provider-account state loaded by the Gateway page.
- Reuse existing schedulable and unschedulable summary helpers.
- Add a Gateway page section named **Scheduling health**.
- Show enabled account count, schedulable account count, and blocked-account reason counts.
- Document that Gateway management summarizes schedulable and blocked provider accounts.

Out of scope:

- New backend APIs.
- New scheduling policies or account groups.
- Charts or historical health tracking.
- Automatic remediation actions from the Gateway page.

## Frontend Behavior

The Gateway page should render a compact scheduling health panel after readiness:

- **Enabled accounts**: count of provider accounts with `enabled === true`.
- **Schedulable accounts**: count from `getSchedulableProviderAccounts()`.
- **Blocked accounts**: count of accounts with an unschedulable reason.
- **Blocked reasons**: one row per reason from `getUnschedulableProviderAccountSummary()`, using the existing status labels.

When the provider-account list is loading, the panel should show `Loading` for the main counts. When no blocked reasons exist, it should show `No blocked provider accounts.`

## Verification

Required gates:

- Frontend source test proves the Gateway page imports and renders `getUnschedulableProviderAccountSummary`.
- Frontend check passes.
- Documentation test proves README and deploy notes mention Gateway management scheduling health.
- Full frontend build passes.

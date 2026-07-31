# OAuth Model Catalog Cache Design

## Goal

Avoid repeated Codex model-catalog requests for the same OAuth account while
preserving the account-specific authentication boundary of the catalog endpoint.

## Design

The provider service keeps a process-local catalog snapshot keyed by:

- N2API provider account ID;
- Codex base URL;
- Codex client version;
- normalized ChatGPT plan type.

A successful fetch remains fresh for six hours. Repeated or concurrent calls for
the same account and catalog configuration reuse one snapshot or in-flight
refresh. Different accounts, plans, or client versions never share a snapshot.
The cache is capped at 32 snapshots and evicts the snapshot nearest expiry when
full. It stores only normalized model names and the internal N2API account ID;
OAuth credentials and external ChatGPT account identifiers are never cached.

Each account still persists its own `oauth_catalog` rows, so enabled state and
manual model overrides remain account-scoped. A service restart simply starts
with an empty cache and causes the first matching account connection to refresh
the snapshot.

## Acceptance Criteria

- Different OAuth accounts with the same plan and client version fetch and
  persist their own authenticated catalogs.
- Repeated and concurrent connections for the same cache key share one remote
  request while the snapshot is fresh.
- Expired snapshots refresh before being applied.
- Different accounts, plans, and client versions use separate snapshots.
- Existing per-account enabled-state and manual-model behavior is unchanged.

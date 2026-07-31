# OAuth Model Catalog Cache Design

## Goal

Avoid one Codex model-catalog request for every OAuth account connection while
preserving the account capability boundaries that affect the returned catalog.

## Design

The provider service keeps a process-local catalog snapshot keyed by:

- Codex base URL;
- Codex client version;
- normalized ChatGPT plan type.

A successful fetch remains fresh for six hours. Calls for the same key reuse the
fresh snapshot, and concurrent misses share one in-flight refresh. Different
plans or client versions never share a snapshot. The cache is capped at 32
snapshots and evicts the snapshot nearest expiry when full. It stores only
normalized model names, never OAuth credentials or account identifiers.

Each account still persists its own `oauth_catalog` rows, so enabled state and
manual model overrides remain account-scoped. A service restart simply starts
with an empty cache and causes the first matching account connection to refresh
the snapshot.

## Acceptance Criteria

- Two OAuth accounts with the same plan and client version cause one remote
  catalog request while the snapshot is fresh.
- Concurrent connections for the same cache key share one remote request.
- Expired snapshots refresh before being applied.
- Different plans and client versions use separate snapshots.
- Existing per-account enabled-state and manual-model behavior is unchanged.

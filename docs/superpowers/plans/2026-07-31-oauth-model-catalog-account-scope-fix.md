# OAuth Model Catalog Account Scope Fix

- [x] Replace the cross-account reuse test with an entitlement-boundary
  regression test that returns different catalogs for two same-plan accounts.
- [x] Add the internal provider account ID to the cache and in-flight keys.
- [x] Preserve same-account TTL reuse, expiry refresh, and concurrent coalescing.
- [x] Run focused provider tests, the race detector, and the managed full suite.
- [x] Commit the fix and refresh the local Compose stack.

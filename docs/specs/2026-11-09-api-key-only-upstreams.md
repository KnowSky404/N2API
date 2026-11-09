# API Key Only Upstreams

## Scope and acceptance

N2API accepts API-key upstream accounts only. Remove OAuth authorization,
callback, token refresh, and OAuth-specific gateway request/response conversion.
Remove fingerprint management, profile binding, synthesized Codex identity
headers, and TLS ClientHello emulation. Downstream Codex clients continue to
use the OpenAI-compatible API with a client key.

Existing API keys, upstream API credentials, models, routing pools, proxy
configuration, response affinity, request logs, and streaming must continue to
work. Historical OAuth accounts must not be selected, refreshed, or exposed as
active upstreams. Existing database rows and migration history are retained for
backup compatibility; this change performs no credential or database deletion.

## Implementation plan

1. Remove frontend OAuth and fingerprint flows and their management endpoints.
2. Restrict account selection and diagnostics to API-key upstreams; remove
   OAuth startup configuration and refresh behavior.
3. Use standard Go HTTP/TLS transports, retaining proxy pooling and deadlines.
4. Update API and gateway regression tests and current operator documentation.
5. Run unit checks, browser verification, and managed Compose rebuild/probes/
   teardown, then local review and the repository PR/CI workflow.

## Upgrade behavior

Installations using OAuth accounts must add an API-key upstream and update
routing pool membership. Old OAuth credentials and fingerprint profiles remain
in PostgreSQL and backups, but are inactive. No production migration or
real-provider acceptance is implied by local tests.

# OAuth Gateway Closure Implementation Plan

## Goal

Close and verify the full personal gateway path from Codex OAuth accounts to
downstream API keys, including streaming, usage, pricing, limits, routing, and
operational attribution.

## Tasks

- [ ] Add a failing proxy regression test for successful Codex OAuth SSE with a
  missing or plain-text upstream content type.
- [ ] Make Codex OAuth Responses success handling protocol-aware instead of
  relying only on the upstream response header.
- [ ] Prove downstream SSE headers, chunk flushing, byte preservation, and
  `response.completed` usage parsing.
- [ ] Review existing tests against every closed-loop invariant in the design
  and add focused integration coverage for any missing cross-component path.
- [ ] Run backend tests, frontend tests, frontend check, and frontend build.
- [ ] Review the diff, create atomic Conventional Commits, and push `main`.
- [ ] Wait for the `CI Image` Test and Build/smoke jobs and image push.
- [ ] Rebuild and recreate the local Compose stack with the repository skill.
- [ ] Verify `free0` test/refresh, temporary API-key model listing, direct SSE,
  real Codex CLI execution, usage/pricing log attribution, and key cleanup.

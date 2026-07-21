# Container And Deployment Hardening Plan

Status: in progress; Task 1 is the first implementation slice
Public API changes: additive health and version endpoints
Data migration: none

## Current Baseline

The release pipeline already pins GitHub Actions by commit, smoke-tests amd64
and arm64 images, verifies tested image digests, emits attestations, and promotes
the tested manifest into CalVer releases. Runtime defaults are weaker: the
image runs as root, N2API has no container healthcheck, Compose publishes on all
interfaces, and hardening flags are absent. Base images and Bun are not fully
reproducibly pinned.

## Task 1: Separate Liveness And Readiness

### Goal

Expose non-overlapping probes and prevent SPA fallback from returning false
health for unknown probe paths.

### Dependencies

None.

### Files

- Modify: `backend/internal/httpapi/server.go`, `server_test.go`
- Modify: `deploy/compose.yaml`, `deploy/compose.release.yaml`
- Modify: `.github/workflows/ci-image.yml`
- Modify: `docs/manual.md`
- Test: HTTP handler, Compose config, image smoke test
- Migrate: none

### Implementation

1. Add `/livez` as a pure process/HTTP response.
2. Add `/readyz` that checks database and static admin assets with a short
   request-bound timeout. Successful server construction already proves
   migrations, bootstrap, and runner initialization completed before listen.
3. Keep `/healthz` as a liveness compatibility alias.
4. Add application healthchecks using `/readyz` and make CI wait on readiness.
5. Do not include provider availability in readiness.

### Tests And Verification

Test 200/503 JSON bodies, DB failure, missing static assets, nil test fixtures,
and explicit content type. Run Go tests, `docker compose config`, image smoke,
and verify Docker reports the app healthy.

### Compatibility And Security

`/healthz` behavior remains compatible. Probe bodies contain stable component
states only, not connection details or errors.

### Risks And Rollback

Over-strict asset detection can block readiness. Check `index.html` or
`200.html` exactly as `serveWeb` does. Roll back Compose/CI to `/healthz` and
remove the additive handlers.

### Manual Acceptance

Confirm database stop makes `/readyz` fail while `/livez` stays successful.

### Completion Criteria

Compose and CI gate on readiness; unknown probe names no longer masquerade as
HTML health responses.

### Commit

`feat(health): add liveness and readiness probes`

## Task 2: Run The Application As Non-root

### Goal

Use a fixed unprivileged identity with only readable application files.

### Dependencies

Task 1 for container verification.

### Files

- Modify: `deploy/Dockerfile`, CI smoke assertions, `docs/manual.md`
- Test: both platform images and runtime UID/file permissions
- Migrate: none

### Implementation

Create fixed UID/GID `n2api`, copy the binary/assets with correct ownership,
switch `USER`, and retain CA certificates/timezone data only if runtime tests
prove they are needed. The app needs no shell or persistent writable path.

### Tests And Verification

Assert UID is nonzero, migrations work, UI is readable, HTTPS upstream test
works, and SIGTERM exits within ten seconds on amd64 and arm64.

### Compatibility And Security

No host bind mounts currently require root ownership.

### Risks And Rollback

Cross-platform user tooling differs by Alpine version. Revert final-stage user
changes if either architecture fails.

### Manual Acceptance

Not required after both image smoke jobs pass.

### Completion Criteria

The release process never starts N2API as UID 0.

### Commit

`feat(deploy): run application container unprivileged`

## Task 3: Apply Compose Runtime Restrictions

### Goal

Remove unnecessary application container privileges and writes.

### Dependencies

Task 2.

### Files

- Modify: both Compose files and `docs/manual.md`
- Test: Compose config, startup, health, read-only write attempt, shutdown
- Migrate: none

### Implementation

Add `read_only: true`, `security_opt: [no-new-privileges:true]`,
`cap_drop: [ALL]`, bounded `/tmp` tmpfs if required, `stop_grace_period`, and
restart behavior. Do not apply speculative restrictions to PostgreSQL; its
official image owns a persistent write path and separate trust boundary.

### Completion Criteria

N2API runs and migrates with all listed restrictions; root filesystem writes
fail.

### Risks And Rollback

Remove one restriction at a time only with a documented runtime need.

### Commit

`feat(deploy): restrict application container privileges`

## Task 4: Make Host Binding Intentional

### Goal

Avoid accidental public exposure while preserving documented LAN/IPv6 use.

### Dependencies

Explicit owner decision on the release default.

### Files

- Modify: Compose port mappings, `.env.example`, `README.md`, `docs/manual.md`
- Test: rendered Compose configurations and IPv4/IPv6 listeners

### Implementation

Recommended release mapping is
`${N2API_BIND_ADDRESS:-127.0.0.1}:${N2API_PORT:-3000}:3000`. Document explicit
values for reverse proxy, LAN, direct public, Docker-only, and IPv6. Development
Compose may keep a deliberate all-interface default for the current VPS workflow
only if clearly separated from release behavior.

### Completion Criteria

Release startup has a documented, tested binding and no ambiguous public default.

### Commit

`feat(deploy): make host binding explicit`

## Task 5: Expose Build Identity

### Goal

Make the running binary and image traceable to the tested source.

### Dependencies

Task 1.

### Files

- Create: `backend/internal/buildinfo/buildinfo.go`, tests
- Modify: `backend/cmd/n2api/main.go`, HTTP server, Dockerfile, CI/release workflows
- Modify: admin health UI/state and `docs/manual.md`

### Implementation

Inject version, commit SHA, and UTC build time using Go linker flags; keep
development values explicit. Add OCI source/revision/version/created labels and
a public minimal `/version` plus authenticated detailed health fields. Do not
perform external update checks in this task.

### Completion Criteria

API values and OCI labels match the exact tested source/image digest.

### Commit

`feat(version): expose runtime build identity`

## Task 6: Pin Maintainable Dependencies

### Goal

Remove moving `latest` behavior from builds and releases.

### Dependencies

Governance dependency-update automation may land in parallel.

### Files

- Modify: Dockerfile, Compose, workflows, `.env.example`, manual
- Test: both architectures and release preview

### Implementation

Pin Bun to the frontend lock-compatible version, pin exact Go/Alpine/PostgreSQL
versions, then introduce digests where update automation can maintain them.
Require `N2API_IMAGE` or an immutable default in release Compose; never silently
deploy `latest` for production.

### Completion Criteria

The same source and inputs resolve the same base artifacts, and update PRs run
the complete test/image suite.

### Commit

`build: pin container and runtime dependencies`

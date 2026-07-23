# Container And Deployment Hardening Plan

Status: Tasks 1-6 implemented locally; owner loopback acceptance and authorized
CI/release evidence remain
Public API changes: additive health and version endpoints
Data migration: none

## Evidence Status (2026-07-23)

| Dimension | Status | Evidence and remaining gate |
| --- | --- | --- |
| `design` | complete | Probe separation, non-root execution, runtime restrictions, explicit host binding, build identity, and dependency pinning are defined. Loopback is the accepted release default. |
| `implementation` | complete | Local commits `86a72bc`, `d6321c6`, `34eb1d4`, `b843c97`, `bc524f6`, and `e6c55a5` implement Tasks 1-6. |
| `merged` | pending | The cited commits exist only on the local `main` branch, which is ahead of `origin/main`; no remote merge is claimed. |
| `local_tests` | partial | Local Compose and image contracts cover probes, non-root identity, restrictions, host binding, and build metadata. Remote multi-platform execution is not local evidence. |
| `ci` | pending | No GitHub Actions run contains the local commits. AMD64 and ARM64 image jobs remain unverified remotely. |
| `release_artifact` | pending | No published multi-platform digest, SBOM, scan, or attestation is tied to the local commits. |
| `operator_acceptance` | pending | Confirm the chosen loopback/reverse-proxy deployment, writable mounts, graceful shutdown, and externally observed readiness. |
| `owner_decision` | complete | Release Compose defaults to `127.0.0.1`; LAN, public, IPv6, or dual-stack exposure requires explicit operator configuration. |

## Current Baseline

The release pipeline already pins GitHub Actions by commit, smoke-tests amd64
and arm64 images, verifies tested image digests, emits attestations, and promotes
the tested manifest into CalVer releases. Runtime defaults are weaker: the
image runs as root, N2API has no container healthcheck, Compose publishes on all
interfaces, and hardening flags are absent. Base images and Bun are not fully
reproducibly pinned.

## Task 1: Separate Liveness And Readiness

Task status: completed 2026-07-21

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

Task status: completed locally on 2026-07-21; amd64/arm64 CI smoke pending the
next authorized push

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

The native ARM64 image runs healthy as `10001:10001`, keeps the binary and
static assets root-owned and non-writable, reads the CA bundle, completes
migrations, serves the UI, and exits cleanly on SIGTERM. The existing CI image
matrix now enforces the same contract for ARM64 and AMD64 before publication.

### Commit

`feat(deploy): run application container unprivileged`

## Task 3: Apply Compose Runtime Restrictions

Task status: completed locally on 2026-07-21; CI enforcement pending the next
authorized push

### Goal

Remove unnecessary application container privileges and writes.

### Dependencies

Task 2.

### Files

- Modify: development, release, and E2E Compose files, CI, and `docs/manual.md`
- Test: Compose config, startup, health, read-only write attempt, shutdown
- Migrate: none

### Implementation

Add `read_only: true`, `security_opt: [no-new-privileges:true]`,
`cap_drop: [ALL]`, bounded `/tmp` tmpfs if required, `stop_grace_period`, and
restart behavior. Do not apply speculative restrictions to PostgreSQL; its
official image owns a persistent write path and separate trust boundary.

The E2E stack applies the same application restrictions and CI checks the
effective container configuration plus denied root-filesystem and allowed
bounded `/tmp` writes.

### Completion Criteria

N2API runs and migrates with all listed restrictions; root filesystem writes
fail.

The development and isolated E2E stacks run healthy with a read-only root
filesystem, zero effective capabilities, `no-new-privileges`, a 16 MiB
`noexec,nosuid,nodev` `/tmp`, and a ten-second stop timeout. PostgreSQL remains
writable and unrestricted. The PostgreSQL-backed gateway E2E suite passes under
the application restrictions, and CI now enforces the effective runtime
configuration and write boundaries.

### Risks And Rollback

Remove one restriction at a time only with a documented runtime need.

### Commit

`feat(deploy): restrict application container privileges`

## Task 4: Make Host Binding Intentional

Task status: implementation present; owner acceptance of loopback as the
release default remains required

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

The current release Compose uses long port syntax with
`N2API_BIND_ADDRESS=127.0.0.1` by default, while development Compose explicitly
accepts its wildcard bind risk for the remote VPS workflow. The environment
example and operator manual cover loopback, LAN, direct public, IPv6, and
Docker-network-only modes, including explicit Docker-only and dual-stack
overrides. Local Engine verification confirmed that release values
`127.0.0.1`, `0.0.0.0`, `::1`, and `::` bind only their selected address family,
while development Compose publishes on both IPv4 and IPv6. Treat the release
loopback default as provisional until the owner accepts it; no additional
technical change is currently needed.

### Completion Criteria

Release startup has a documented, tested binding and no ambiguous public default.

### Commit

`feat(deploy): make host binding explicit`

## Task 5: Expose Build Identity

Task status: completed locally on 2026-07-21; amd64/arm64 CI smoke and release
promotion verification pending the next authorized push

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

The binary now exposes an explicit short build version publicly and returns
the complete commit/build timestamp only to an authenticated administrator.
The Dashboard renders the authenticated short version and clears build details
on logout. A native ARM64 image built with deterministic test values returned
matching public/authenticated API identities and all four matching OCI labels;
CI now enforces the same identity contract on AMD64 and ARM64, while Release
verifies the labels on every promoted platform before retagging the tested
manifest.

### Commit

`feat(version): expose runtime build identity`

## Task 6: Pin Maintainable Dependencies

Task status: dependency pins completed locally on 2026-07-21; automated update
configuration is tracked by Repository Governance Task 3, and amd64 execution
remains pending the next authorized push

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

All main, E2E, Compose, and CI runtime images now use readable exact version
tags plus multi-platform manifest digests. Bun `1.3.14` is consistent across
the frontend package metadata, Docker builds, and CI; Go `1.26.5` is consistent
between `go.mod` and container builds. Release Compose requires an explicit
`N2API_IMAGE` and no longer falls back to `latest`. A CI preflight rejects
missing digests or toolchain drift. Every pinned manifest includes amd64 and
arm64; native arm64 main/E2E builds and the PostgreSQL-backed gateway plus both
SDK contract suites passed locally. The existing image matrix will execute the
amd64 build after the next authorized push.

### Commit

`build: pin container and runtime dependencies`

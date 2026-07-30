# Agent-Native Operations Acceptance

- Date: 2026-07-30
- Baseline: `origin/main`
- Implementation head tested: `fa7a924`
- Canonical operator entry point: `./ops/n2api`

## Result

The repository-local Agent-Native operations scope is accepted. N2API now has
one discoverable, machine-readable operator interface for host and
configuration checks, image inspection, runtime status, verification, bounded
logs, backup, isolated restore drills, deploy, upgrade, rollback, and operation
records. Every service-changing command uses a protected plan/apply workflow.

This result is local evidence only. No commit was pushed, no pull request or
release was created, no GitHub-hosted workflow ran for this implementation, no
production host was changed, and no real provider, reverse proxy, off-host
backup, or production restore was exercised.

## Delivered Architecture

`ops/n2api` is the only canonical production operator CLI. Its Bash modules
under `ops/lib/` reuse the existing production Compose files, release-image
verification, application configuration loader, PostgreSQL backup semantics,
and `dev/verification/restore-backup.sh`. The Make targets, repository skills,
and documentation call or describe this interface instead of implementing a
second control plane.

The production-host dependency contract is Bash, Docker Engine, Docker Compose
v2, coreutils, curl, jq, openssl, flock, and timeout. The CLI does not install
software, source dotenv files, use `eval`, construct user-controlled `bash -c`
commands, mount the Docker socket inside N2API, or require Go, Bun, or Node.js
on the production host.

The supported command tree is:

```text
describe
doctor
config init|validate
image resolve|inspect
status
verify --level basic|authenticated|gateway
logs n2api|postgres|postgres-backup
backup create|list|verify
restore drill
operations list|show
deploy plan|apply
upgrade plan|apply
rollback plan|apply
```

## Data And Safety Contracts

- Non-streaming JSON commands emit one `n2api.ops/v1` envelope on stdout.
  Progress and bounded diagnostics remain on stderr. Versioned schemas cover
  envelopes, plans, receipts, backup metadata, and restore evidence under
  `ops/schemas/`.
- Operator state defaults to the XDG state directory, outside the repository.
  Directories and files use modes 0700 and 0600, unsafe links and permissions
  fail closed, fingerprints use a local HMAC key, and plans carry an integrity
  HMAC.
- Deploy, upgrade, and rollback apply accept only an existing plan. Apply
  revalidates expiry, plan integrity, environment and Compose fingerprints,
  source and target images, runtime state, schema, backup, restore evidence,
  and the shared mutation lock before changing services.
- Exact `ghcr.io/knowsky404/n2api:YYYYMMDDNN@sha256:<digest>` references are
  required for production apply. Moving tags, tag-only targets, platform
  mismatches, plan edits, stale state, and apply-time target replacement are
  blocked.
- A healthy repeated target returns `noop` with `changed=false` and does not
  restart services, rerun migrations, or create a backup.
- Backup creation uses PostgreSQL custom archives, same-directory temporary
  files, archive-list verification, SHA-256, safe permissions, and atomic
  publication. Missing encrypted off-host custody is `attention`, not success.
- Restore drills directly wrap the existing isolated restore verifier. Fixture
  and real-operator evidence are distinct, live projects and database URLs are
  rejected, and fixture evidence cannot authorize an upgrade.
- Application image rollback is separate from database restore. Rollback is
  blocked unless the exact previous digest and schema compatibility are
  proven. Live database restore, automatic rollback, volume deletion, and
  `compose down -v` are not implemented.
- Credentials for authenticated and gateway verification come from protected
  files or interactive input. Gateway verification additionally requires
  `--allow-upstream-call`. Plans, receipts, JSON, logs metadata, and retained
  test artifacts are covered by secret-canary tests.

## Changed Files

Operator implementation and schemas:

- `ops/n2api`
- `ops/lib/apply.sh`
- `ops/lib/backup.sh`
- `ops/lib/checks.sh`
- `ops/lib/core.sh`
- `ops/lib/docker.sh`
- `ops/lib/env.sh`
- `ops/lib/output.sh`
- `ops/lib/plan.sh`
- `ops/lib/restore.sh`
- `ops/lib/runtime.sh`
- `ops/lib/state.sh`
- `ops/schemas/backup.schema.json`
- `ops/schemas/envelope.schema.json`
- `ops/schemas/plan.schema.json`
- `ops/schemas/receipt.schema.json`
- `ops/schemas/restore.schema.json`

Application and configuration integration:

- `backend/cmd/n2api/admin.go`
- `backend/cmd/n2api/admin_test.go`
- `backend/cmd/n2api/encryption_rotation_command_test.go`
- `backend/cmd/n2api/main.go`
- `backend/internal/config/validation.go`
- `backend/internal/config/validation_test.go`
- `.env.example`

Tests and CI:

- `ops/tests/test-ops.sh`
- `ops/tests/fixtures/fake-bin/docker`
- `ops/tests/fixtures/fake-bin/mv`
- `ops/tests/fixtures/fake-bin/ss`
- `dev/ci/test-agent-operations-docs.sh`
- `dev/ci/test-dev-artifacts.sh`
- `dev/ci/test-security-policy.sh`
- `dev/verification/restore-backup.sh`
- `.github/workflows/ci-image.yml`
- `.github/workflows/security.yml`
- `Makefile`

Agent and documentation integration:

- `.agents/skills/n2api-local-refresh/SKILL.md`
- `.agents/skills/n2api-local-refresh/agents/openai.yaml`
- `.agents/skills/n2api-production-operations/SKILL.md`
- `.agents/skills/n2api-production-operations/agents/openai.yaml`
- `AGENTS.md`
- `README.md`
- `docs/README.md`
- `docs/manual.md`
- `docs/agent-operations.md`
- `docs/specs/2026-07-30-n2api-agent-native-operations.md`
- `docs/plans/2026-07-30-n2api-agent-native-operations-plan.md`

## Atomic Commits

The implementation was delivered as the following coherent Conventional
Commits, in order:

1. `dea54dc docs: specify agent-native operations contract`
2. `97f4286 feat(ops): add discovery and operation state contract`
3. `8cfbbff feat(admin): add read-only configuration validation`
4. `d2535c3 feat(ops): add host configuration and image preflight`
5. `1df6172 feat(ops): add runtime status and verification`
6. `4997b50 feat(ops): add backup and isolated restore operations`
7. `fd6a222 feat(ops): add deploy plan and apply workflow`
8. `4e08a55 feat(ops): add upgrade plan and apply workflow`
9. `2f26d0c feat(ops): add guarded application rollback`
10. `27c81d2 fix(ops): harden apply noop and interruption receipts`
11. `888bce1 feat(agent): add repository operations skills`
12. `2f00eee test(ops): cover operation safety contracts`
13. `cbeacb7 ci: verify agent-native operations tooling`
14. `5c6ff76 docs: publish agent operations runbook`
15. `fa7a924 fix(docs): correct operator backup workflow`

## Verification

The following commands passed locally on 2026-07-30:

- `make test`: all Go packages; Svelte diagnostics with 0 errors and 0
  warnings; 212 Bun tests; static frontend build.
- `make test-ops`: CLI parsing, JSON/schema contracts, stable exits and reason
  codes, state safety, image rules, plan integrity and drift, locks, signals,
  timeouts, noop, backup atomicity, restore classification, rollback blocks,
  receipts, and secret canaries.
- `make test-production-deploy`: development, development metrics, release,
  release secrets, release metrics, release metrics secrets, default and custom
  resources, invalid values, and immutable-image verification.
- `make test-restore-backup`: schema 50 current restore, schema 47-to-50
  upgrade restore, previous key, wrong key, corrupt archive, SIGTERM cleanup,
  and isolated resource cleanup.
- `make test-contracts`: OpenAI JavaScript and Python SDK contracts.
- `make test-e2e`: PostgreSQL-backed gateway and runtime scenarios.
- `make test-critical-race`: critical process, API, gateway, store, and
  background race suites.
- `make test-control-connections`: dedicated control connection and process
  fault scenarios.
- `make test-postgres-faults`: PostgreSQL pause produced readiness 503 while
  livez remained 200; unpause recovered readiness to 200.
- `make test-dev-artifacts`: managed artifact lifecycle and PostgreSQL backup
  script tests.
- `bash dev/ci/verify-pinned-dependencies.sh`: pinned references consistent.
- `GOCACHE=<fresh-temp> GOMODCACHE=<fresh-temp> bash
  dev/ci/test-security-policy.sh`: security policy evaluator passed. Fresh
  temporary Go caches were required because the host default Go build cache was
  corrupt; a clean cache successfully loaded the standard library.
- actionlint `v1.7.12` with ShellCheck `0.10.0`: all GitHub Actions workflows
  passed. The matching CI ShellCheck command with `-x` passed for `ops/n2api`,
  `ops/tests/test-ops.sh`, and the fake tool fixtures.
- `git diff --check`: no whitespace errors.

`make test-production-deploy` renders every supported production Compose
combination with `config --quiet`; it also renders the development and metrics
combination and rejects unsupported resource values. No real production env or
secret was used.

## Refreshed Development Runtime

The repository local-refresh workflow completed after the implementation tests:

1. `docker builder prune --all --force` reclaimed 1.069 GB.
2. `docker compose -f deploy/compose.yaml build --no-cache` succeeded.
3. `docker compose -f deploy/compose.yaml up -d --force-recreate` recreated
   `n2api`, `postgres`, and `postgres-backup` without deleting the database
   volume.
4. `docker compose -f deploy/compose.yaml ps` reported all three services
   healthy. N2API listens on `0.0.0.0:3000` and `[::]:3000`.
5. Container-local `/livez` returned `{"status":"ok"}`; `/readyz` reported
   database, Gateway Settings, runtime, and static assets ready; `/version`
   returned `{"version":"dev"}`; `/` returned success.
6. PostgreSQL reported schema version 50.
7. The final `docker builder prune --all --force` reclaimed 1.069 GB.

Only builder cache was pruned. Images, Compose volumes, and database data were
not pruned or removed. The refreshed dashboard is available at
`http://oc-de-fra-1.knowsky.uk:3000`.

## Definition Of Done

| # | Status | Evidence |
| --- | --- | --- |
| 1 | Complete | `ops/n2api` is the single canonical operator CLI; Make, skills, and docs point to it. |
| 2 | Complete | `describe --format json` is side-effect-free and exposes the complete versioned capability inventory. |
| 3 | Complete | `doctor`, `config validate`, `image inspect`, `status`, and backup commands use stable envelopes, checks, and reason codes. |
| 4 | Complete | Deploy implements HMAC-protected plan/apply with live-state revalidation. |
| 5 | Complete | Upgrade implements plan/apply with source, candidate, backup, restore, and rollback gates. |
| 6 | Complete | Application rollback and database restore are separate contracts and commands. |
| 7 | Complete | Live database restore, blind downgrade, automatic rollback, and volume deletion are absent and blocked. |
| 8 | Complete | Production plan/apply accepts only an exact CalVer tag plus manifest digest. |
| 9 | Complete | `latest`, `main`, tag-only, digest-only, wrong-repository, and malformed targets are rejected. |
| 10 | Complete | Healthy repeated apply is `noop`, `changed=false`, and does not recreate services. |
| 11 | Complete | Backup and every service-changing apply share one exclusive operation lock. |
| 12 | Complete | Plan expiry, HMAC edits, env/Compose changes, source/schema/runtime drift, and target replacement are rejected. |
| 13 | Complete | External work is bounded; SIGINT, SIGTERM, and timeout exits preserve cleanup, lock release, and final receipts. |
| 14 | Complete | Backup tests prove same-directory temporary files, archive verification, checksum, atomic publication, and failure cleanup. |
| 15 | Complete | `restore drill` wraps `dev/verification/restore-backup.sh` rather than reimplementing restore. |
| 16 | Complete | Evidence class is explicit and CI fixtures cannot satisfy real-operator upgrade gates. |
| 17 | Complete | Central redaction and canary scans cover stdout, stderr, JSON, plans, receipts, operation output, and retained artifacts. |
| 18 | Complete | `.agents/skills/n2api-local-refresh/` is repository-local and preserves the non-destructive refresh boundary. |
| 19 | Complete | `.agents/skills/n2api-production-operations/` begins with discovery and enforces plan/apply and owner gates. |
| 20 | Complete | Root `AGENTS.md` names the CLI, read-only workflow, secret boundary, rollback boundary, and both skills. |
| 21 | Complete | `docs/agent-operations.md` is the concise human and agent entry point with five tested scenarios. |
| 22 | Complete | Unit, fake-tool, schema, contract, restore, Compose, E2E, race, control-connection, and database-fault tests passed. |
| 23 | Complete | `make test-ops` is included in the existing CI Image correctness flow; workflow contracts and actionlint passed locally. |
| 24 | Complete | Every applicable existing managed test listed in the source brief passed. |
| 25 | Complete | Development, metrics, release, secrets, metrics-secrets, resource override, and invalid Compose cases passed. |
| 26 | Complete | The required builder-prune, no-cache build, force-recreate, health, endpoint, schema, and final-prune sequence passed. |
| 27 | Complete | Fifteen implementation commits are atomic Conventional Commits; this acceptance record is a separate docs commit. |
| 28 | Complete | No push, PR, release, real credentials, provider traffic, or production mutation occurred. |
| 29 | Complete | This report separates repository-local evidence from all external acceptance items below. |
| 30 | Complete at handoff | This report records files, commits, commands, results, external checks, and the recommended owner sequence; the final response summarizes them. |

## External Acceptance Not Executed

The following require explicit remote authorization, production credentials,
real operator data, or an owner decision and remain open:

- Push the local commits and run the exact-SHA GitHub-hosted `CI Image`
  workflow, including Test, image smoke, main-branch publish, SBOM, provenance,
  and security evidence jobs.
- Deploy a published immutable image to the real production host.
- Validate the intended external reverse proxy, HTTPS/TLS edge, forwarded
  headers, firewall, DNS, and external readiness monitor.
- Complete real OpenAI/Codex OAuth and an explicitly authorized minimal gateway
  request using real credentials.
- Create a real operator backup, place an encrypted copy in verified off-host
  custody, and record its owner-controlled evidence.
- Restore that current real operator backup with the current image and any
  migration-bearing candidate in an isolated drill. The passing fixture restore
  matrix is not this evidence.
- Approve a production upgrade or rollback plan after reviewing its owner
  gates. Database restore remains a separate manual recovery decision.

## Recommended Owner Sequence

Review the 16 local commits including this acceptance record. If publication is
approved, push the exact local head and wait for its `CI Image` workflow. After
the exact digest is published, use `./ops/n2api describe --format json`, then
the production skill's read-only doctor, status, backup, restore-drill, and plan
steps on the target host. Execute apply only after the generated plan and every
external owner gate have been reviewed.

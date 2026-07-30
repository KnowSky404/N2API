# N2API Agent-Native Operations Implementation Plan

Date: 2026-07-30
Specification: `docs/specs/2026-07-30-n2api-agent-native-operations.md`

## Delivery Rules

- Keep `./ops/n2api` as the only production operator entry point.
- Reuse existing Compose, image, backup, and restore implementation instead of
  creating parallel operational logic.
- Preserve the production project, services, volume, endpoints, and release
  model.
- Commit each coherent feature, test, CI, skill, or documentation change with
  a Conventional Commit.
- Do not push, publish, call a real provider, mutate a real production host, or
  use real credentials.
- After code or functionality changes, complete the required no-cache local
  Compose refresh and smoke verification.

## Task 1: Specify The Contract

Files:

- `docs/specs/2026-07-30-n2api-agent-native-operations.md`
- `docs/plans/2026-07-30-n2api-agent-native-operations-plan.md`

Acceptance:

- Current capability inventory and confirmed gaps name the existing authority.
- Threat, risk, CLI, state, JSON, plan/apply, backup, restore, rollback, test,
  compatibility, and acceptance contracts are explicit.
- The plan continues through implementation and verification.

Commit: `docs: specify agent-native operations contract`

## Task 2: Add Output, State, And Discovery Foundations

Files:

- `ops/n2api`
- `ops/lib/core.sh`
- `ops/lib/output.sh`
- `ops/lib/state.sh`
- `ops/schemas/envelope.schema.json`
- `ops/schemas/plan.schema.json`
- `ops/schemas/receipt.schema.json`
- `ops/tests/test-ops.sh`

Implementation:

- Strict shell mode, global parsing, stable exit table, UTC operation IDs,
  JSON/text envelope, diagnostics separation, and signal traps.
- Protected XDG state, local HMAC key, plan/receipt atomic writes, one shared
  flock, safe lock metadata, and operation list/show.
- Side-effect-free `describe` with the complete command and risk inventory.

Acceptance:

- `describe --format json` works with Docker and env absent and writes nothing.
- JSON stdout contains one schema-valid document.
- Unsafe state, lock contention, unknown command, signals, and canary scans are
  covered by the independent ops test entry.

Commit: `feat(ops): add discovery and operation state contract`

## Task 3: Add Application Configuration Validation

Files:

- `backend/cmd/n2api/admin.go`
- `backend/cmd/n2api/admin_test.go`
- relevant focused config tests

Implementation:

- Add `n2api admin validate-config` before database setup.
- Reuse `config.Load` and parsed PostgreSQL configuration.
- Check PostgreSQL password consistency without connecting.
- Return one sanitized versioned document and stable reason code.

Acceptance:

- No listener, migration, pool, or database connection is created.
- Valid configuration succeeds; invalid values and password mismatch fail.
- Config values, DSNs, file paths, and canaries never appear in output.

Commit: `feat(admin): add read-only configuration validation`

## Task 4: Add Host, Env, And Image Preflight

Files:

- `ops/lib/env.sh`
- `ops/lib/docker.sh`
- `ops/lib/checks.sh`
- `ops/n2api`
- `.env.example`
- `ops/tests/fixtures/`
- `ops/tests/test-ops.sh`

Implementation:

- Safe dotenv reading without shell evaluation.
- `doctor`, `config init`, `config validate`, `image resolve`, and `image
  inspect`.
- Docker Compose `config --quiet`, manifest/platform inspection, immutable
  repository/CalVer/digest validation, current port and filesystem checks.
- Application semantic validation through the exact immutable image.

Acceptance:

- Every requested doctor/config/image failure has a stable check/reason.
- Config init is mode 0600, independent-secret, no-overwrite, and leak-free.
- Moving tags, tag-only values, wrong registry, bad digest, missing supported
  platform, and missing host platform are blocked.
- Existing release image verification is called rather than copied.

Commit: `feat(ops): add host configuration and image preflight`

## Task 5: Add Status, Verification, And Logs

Files:

- `ops/lib/runtime.sh`
- `ops/n2api`
- `ops/tests/test-ops.sh`

Implementation:

- Read-only status aggregation for Compose, container/image, probes,
  PostgreSQL/schema, backups, restore evidence, Git, lock, and receipt.
- Basic, authenticated, and explicitly authorized gateway verification.
- Bounded service logs with text/JSONL streaming rules.

Acceptance:

- Status never calls a provider or writes operation state.
- Basic verifies runtime restrictions and exact image identity.
- Credentials enter only through a protected file or prompt and request stdin.
- Gateway verification cannot run without `--allow-upstream-call`.
- Default logs are bounded and follow mode is explicit.

Commit: `feat(ops): add runtime status and verification`

## Task 6: Add Backup And Restore Evidence

Files:

- `ops/lib/backup.sh`
- `ops/lib/restore.sh`
- `ops/schemas/backup.schema.json`
- `ops/schemas/restore.schema.json`
- `ops/n2api`
- `ops/tests/test-ops.sh`

Implementation:

- Shared-lock custom archive creation against the production PostgreSQL
  service, same-directory temporary file, archive-list verification, checksum,
  atomic archive/metadata publication, and off-host attention state.
- Archive list/verify commands without restore claims.
- Immutable-image and live-target rejection before wrapping
  `dev/verification/restore-backup.sh`.
- Allowlisted result capture and `ci_fixture` versus `real_operator` evidence.

Acceptance:

- Failure and signals remove exact temporary files and never write success
  metadata.
- Backup metadata names source digest and schema and contains no secret.
- Restore success/failure cleanup is inherited and retested.
- Fixture evidence cannot satisfy an upgrade gate.

Commit: `feat(ops): add backup and isolated restore operations`

## Task 7: Add Deploy Plan And Apply

Files:

- `ops/lib/plan.sh`
- `ops/lib/apply.sh`
- `ops/n2api`
- `ops/tests/test-ops.sh`

Implementation:

- HMAC-protected plan with expiry and all source/config/runtime invariants.
- First-deploy guards for services, volume, port, platform, and owner risks.
- Apply-only plan consumption, exclusive lock, pull/verify/recreate, bounded
  wait, basic verification, exact digest proof, and receipt.

Acceptance:

- Plan is live-stack read-only.
- Tampering, expiry, env/Compose/current image/schema/state drift, and target
  replacement are blocked.
- Repeating a healthy exact target is noop without restart.
- Compose success without readiness/digest success is failure.

Commit: `feat(ops): add deploy plan and apply workflow`

## Task 8: Add Upgrade Plan And Apply

Files:

- `ops/lib/plan.sh`
- `ops/lib/apply.sh`
- `ops/n2api`
- `ops/tests/test-ops.sh`

Implementation:

- Record source, target, rollback target, schema, backup, current restore, and
  candidate restore evidence.
- Pull and inspect candidate during planning without recreating the stack.
- Revalidate backup checksum, source image, schema, config, candidate, and
  rollback availability before persisting the target and recreating.

Acceptance:

- Missing/future/stale/changed backup and restore evidence block apply.
- Candidate migration possibility always has a real candidate-drill gate.
- Same healthy target is noop and no provider traffic occurs.

Commit: `feat(ops): add upgrade plan and apply workflow`

## Task 9: Add Guarded Application Rollback

Files:

- `ops/lib/plan.sh`
- `ops/lib/apply.sh`
- `ops/n2api`
- `ops/tests/test-ops.sh`

Implementation:

- Derive exact previous image from successful receipts.
- Require digest availability and proven schema compatibility.
- Keep rollback as independent plan/apply and database restore as a blocked,
  documented manual path.

Acceptance:

- Unknown compatibility, unavailable digest, missing prior target, live restore,
  and volume deletion are blocked with stable reasons.
- No failed upgrade automatically starts rollback.

Commit: `feat(ops): add guarded application rollback`

## Task 10: Add Repository Skills And Workflow Instructions

Files:

- `.agents/skills/n2api-local-refresh/SKILL.md`
- `.agents/skills/n2api-production-operations/SKILL.md`
- skill references and trigger fixtures
- `AGENTS.md`

Acceptance:

- Local skill exactly preserves builder-prune and non-destructive refresh
  boundaries.
- Production skill begins with describe, uses only the canonical CLI, separates
  check/plan/apply, protects secrets, restore evidence, and rollback boundaries.
- Root instructions stay concise and point to both skills.

Commit: `feat(agent): add repository operations skills`

## Task 11: Integrate Tests, Make, And CI

Files:

- `Makefile`
- `.github/workflows/ci-image.yml`
- `ops/tests/`
- applicable existing CI contract tests

Implementation:

- Add thin `test-ops`, `ops-describe`, and `ops-doctor` targets.
- Add `test-ops` to the existing correctness matrix.
- Extend ShellCheck/action/workflow contracts where necessary.

Acceptance:

- Fake-tool tests cover the complete matrix without production credentials,
  provider traffic, real backups, or live deployment mutation.
- CI actions remain pinned, permissions minimal, and existing image/release
  flows unchanged.

Commit: `test(ops): cover operator safety contracts`

Commit: `ci: verify agent-native operations tooling`

## Task 12: Publish Operator Documentation

Files:

- `docs/agent-operations.md`
- `README.md`
- `docs/README.md`
- `docs/manual.md`
- documentation contract tests

Acceptance:

- One short operations entry documents discovery, risks, output, plan/apply,
  deploy, status, upgrade, backup, drill, failure, rollback/data distinction,
  logs, receipts, secrets, prohibited actions, owner gates, and lower-level
  fallback.
- Scenarios A-E contain complete canonical CLI sequences.
- Root README and docs index link to it; the manual retains lower-level detail
  without presenting raw Compose mutation as the agent default.

Commit: `docs: publish agent operations runbook`

## Task 13: Complete Acceptance And Runtime Refresh

Files:

- `docs/agent-native-operations-acceptance.md`

Verification sequence:

```text
make test
make test-ops
make test-production-deploy
make test-restore-backup
make test-contracts
make test-e2e
make test-critical-race
make test-control-connections
make test-postgres-faults
```

Also run every supported Compose `config --quiet` combination, actionlint,
ShellCheck, workflow contracts, diff review, and worktree review. Use managed
test lifecycle and disk preflight for heavy commands.

Then use the repository local-refresh skill exactly:

1. `docker builder prune --all --force`
2. `docker compose -f deploy/compose.yaml build --no-cache`
3. `docker compose -f deploy/compose.yaml up -d --force-recreate`
4. Compose status, container-local health/readiness/bootstrap, and database
   schema smoke checks
5. `docker builder prune --all --force`

Acceptance report requirements:

- Map every source acceptance criterion and all 30 Definition of Done items to
  current evidence.
- List files, atomic commits, exact commands, results, and unexecuted external
  checks.
- Do not claim GitHub CI, production, real provider, reverse proxy, off-host
  custody, or current real-backup restore evidence unless actually performed.

Commit: `docs: record agent-native operations acceptance`

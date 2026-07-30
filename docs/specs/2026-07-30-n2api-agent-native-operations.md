# N2API Agent-Native Operations Specification

Status: implementation contract
Date: 2026-07-30
Scope: repository-local operations for a single-node, self-hosted N2API deployment

## 1. Objective

N2API will expose one stable, repository-level operator interface:

```text
./ops/n2api
```

The interface is usable by a human or an automation agent with repository and
local host shell access. It makes supported operations discoverable, separates
planning from service mutation, produces versioned machine output, records
non-sensitive evidence, and fails closed when an invariant cannot be proven.

The interface does not provision hosts, connect over SSH, manage cloud
resources, publish releases, upload backups, call a real provider by default,
or restore a live database.

## 2. Current Capability Inventory

| Area | Existing authority | Reuse decision |
| --- | --- | --- |
| Development stack | `deploy/compose.yaml` | Keep as the local development stack. |
| Production stack | `deploy/compose.release.yaml` and secret/metrics overrides | Canonical production Compose input. Preserve project, service, volume, port, and bind behavior. |
| Immutable image check | `dev/verification/verify-release-image.sh` | Reuse for local image and running-container identity checks; add registry/platform discovery in the operator layer. |
| Backups | `deploy/postgres-backup.sh` | Reuse its custom archive and archive-list validation semantics. Add operator metadata, checksum, and shared locking outside the container. |
| Restore drill | `dev/verification/restore-backup.sh` | Wrap directly. Do not reimplement restore, migration, readiness, integrity, secret, gateway, or cleanup behavior. |
| Disk preflight | `dev/maintenance/disk-check.sh` | Reuse for heavy repository tests. Operator doctor adds host-specific disk and inode checks. |
| Test resource safety | `dev/lib/test-resources.sh` | Keep isolated to tests and restore drills. Never expose its cleanup as a production operation. |
| Application configuration | `backend/internal/config` | Add one read-only application command that calls the existing loader and emits only a sanitized result. |
| Runtime probes | `/livez`, `/readyz`, `/version`, authenticated admin health | Compose and operator verification consume existing semantics unchanged. |
| Release evidence | `docs/release-checklist.md`, CI Image, release workflow | Report as evidence inputs. Do not replace or weaken release approval. |
| Local refresh | root `AGENTS.md` and the user-level refresh skill | Add a portable repository skill that preserves the existing prune/rebuild/recreate/smoke sequence. |

## 3. Gap Analysis

The repository currently has no canonical operator CLI, capability discovery,
stable JSON envelope, JSON schemas, risk table, exit-code contract, XDG state,
operation records, shared operation lock, plan integrity, stale-plan detection,
or deploy/upgrade/rollback plan/apply workflow.

Existing image validation accepts any readable tag with a digest and does not
prove the registry manifest platforms. Production Compose can still be invoked
with a tag-only value when the operator bypasses documented checks. The new CLI
must reject moving or tag-only targets before every production apply.

The production Compose stack does not include the development backup sidecar.
Operator backup therefore needs to execute `pg_dump` against the running
PostgreSQL service, validate the archive with the matching PostgreSQL image,
and write the archive plus metadata on the host without adding a daemon.

The restore script is safe and comprehensive but exposes a shell-oriented
key/value result and accepts an arbitrary image reference. The wrapper must
validate an immutable target, classify evidence, capture only allowlisted
fields, and preserve the existing isolated cleanup boundary.

## 4. Boundaries And Non-Goals

The implementation preserves Go, PostgreSQL, Bun/SvelteKit, Docker Compose,
single-node production, CalVer, `linux/amd64` plus `linux/arm64`, exact image
digests, current public endpoints, current volume names, and current secret
files.

It does not add Kubernetes, Helm, Terraform, Ansible, remote orchestration,
Watchtower, a moving production tag, a second control plane, a hosted service,
mandatory external secret management, an MCP server, a web operations UI,
automatic GitHub publication, automatic off-host transfer, automatic live
restore, or volume deletion.

## 5. Architecture

```text
operator / agent
      |
 ./ops/n2api                 canonical parser and dispatcher
      |
 ops/lib/*.sh                output, env, state, Docker, plan, operation modules
      |
 existing Compose + verification + restore scripts
      |
 Docker Engine / local N2API / PostgreSQL

operator-controlled state (outside git by default)
  plans/        signed immutable plan documents
  operations/   sanitized receipts and evidence
  locks/        one deployment mutation lock
  keys/         local HMAC key, mode 0600
```

The production-host dependency set is Bash, Docker Engine, Docker Compose v2,
coreutils, curl, jq, openssl, flock, and timeout. `doctor` checks every required
tool before an operation needs it and uses stable reason codes. Buildx is
optional when `docker manifest inspect` is available as the manifest inspection
backend. No command installs system software.

The CLI never sources an env file, uses `eval`, enables shell tracing, or
constructs a user-controlled `bash -c` string. External commands are Bash
arrays. Files are created under `umask 077`, inspected without following links,
and published with same-filesystem atomic rename.

## 6. Risk Model

| Risk | Meaning | Commands |
| --- | --- | --- |
| `read_only` | Does not write repository, state, image cache, or service state. | `describe`, `doctor`, `config validate`, `image inspect`, `status`, `verify`, `logs`, `backup list`, `backup verify`, `operations list/show` |
| `local_write` | Writes a protected local file or image cache but does not change the live stack. | `config init`, `image resolve`, every `plan`, `backup create`, `restore drill` |
| `service_change` | Pulls and recreates production services from a validated plan. | `deploy apply`, `upgrade apply` |
| `high_risk` | May move the application to an older image and is allowed only when compatibility is proven. | `rollback apply` |

There is no generic `--force`. An explicit upstream call is a separate consent
gate and does not bypass any other check. Database restore is never an apply
operation in this version.

## 7. CLI Contract

Global options are accepted before or after a command where unambiguous:

```text
--format text|json
--env-file PATH
--compose-file PATH (repeatable)
--project-name NAME
--state-dir PATH
--timeout SECONDS
```

The default production Compose file is `deploy/compose.release.yaml`, the
default project remains `deploy`, and the default state root is
`${XDG_STATE_HOME}/n2api` or `${HOME}/.local/state/n2api`. Explicit override
paths exist for tests and nonstandard host layouts.

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

`describe --format json` requires no env file, Docker daemon, database, state
directory, or network and has no side effects. Its command inventory includes
risk, mutation class, required inputs, plan/apply relationship, Compose
defaults, services, probes, directories, schemas, exit codes, and manual link.

## 8. Output And Exit Contract

Every non-streaming JSON invocation writes exactly one JSON document to stdout.
Docker output, progress, and bounded diagnostics go to stderr or a protected
temporary file. The common envelope schema is `n2api.ops/v1`:

```json
{
  "schema_version": "n2api.ops/v1",
  "operation_id": null,
  "command": "status",
  "risk": "read_only",
  "status": "succeeded",
  "changed": false,
  "started_at": "2026-07-30T00:00:00Z",
  "finished_at": "2026-07-30T00:00:01Z",
  "reason_code": "status_available",
  "summary": "N2API status collected",
  "checks": [],
  "current": {},
  "target": {},
  "artifacts": [],
  "next_actions": []
}
```

Known, unavailable, unknown, and not-applicable values are represented
explicitly; absent evidence is never converted to a passing check. Human text
is descriptive and is never the only automation signal.

| Exit | Meaning |
| --- | --- |
| `0` | `succeeded` or `noop` |
| `3` | `attention` |
| `4` | `blocked` |
| `5` | `contended` |
| `6` | `failed` |
| `64` | invalid invocation |
| `124` | bounded timeout |
| `130` | SIGINT |
| `143` | SIGTERM |

Schemas are versioned under `ops/schemas/` for the envelope, plan, receipt,
backup metadata, and restore evidence. Tests validate emitted documents with
jq plus explicit schema-contract assertions without requiring a production
network.

## 9. State And Operation Records

The state root and its direct parent must not be symlinks or writable by group
or other users. The CLI creates the root at mode 0700 and files at mode 0600.
Existing unsafe ownership or permissions block stateful commands. Read-only
commands report an absent state directory as `unavailable`, not as failure.

Each plan and apply has an operation ID and a sanitized receipt. Receipts are
append-only files named by operation ID. They include timestamps, command,
risk, result, invariant outcomes, non-sensitive artifact IDs, and next actions.
They exclude resolved environment, full DSNs, raw Docker inspection, raw
database errors, request/response bodies, secrets, tokens, cookies, signed
URLs, and archive contents.

The state root contains a random local HMAC key. Environment and Compose
fingerprints use HMAC-SHA256 rather than a bare secret-bearing-file checksum.
Plans also carry an HMAC over canonical plan JSON so edits are detected before
apply.

## 10. Doctor And Configuration

`doctor` is read-only and checks Linux, `amd64`/`arm64`, Docker CLI and daemon,
Compose v2, manifest inspection, curl, jq, openssl, flock, timeout, disk space,
inodes, Docker authorization, env type/link/mode, backup path type and
writability, state safety, Compose `config --quiet`, port occupancy, existing
services, and current-platform image availability. It does not install tools,
create directories, start services, or change firewall rules.

`config init` requires a public URL, exact CalVer or exact image reference,
accepted risks, bind address, and backup directory. A CalVer is resolved to a
manifest digest before writing. It creates independent random PostgreSQL,
administrator, and encryption secrets and writes a complete mode-0600 file
without printing any generated value. Existing targets are never overwritten.

`config validate` performs host file safety, placeholder, required-value,
public URL, bind, accepted-risk, immutable-image, direct/file conflict, secret
file, secret equality, and PostgreSQL credential consistency checks. It then
invokes `n2api admin validate-config` in the exact immutable application image
without dependencies or database access. That application command calls the
existing `config.Load`, emits one redacted JSON document, does not listen,
migrate, or connect, and maps all detailed parse errors to stable safe codes.

## 11. Image Resolution

Only `ghcr.io/knowsky404/n2api:YYYYMMDDNN@sha256:<64 lowercase hex>` is valid
for production plan/apply. `latest`, `main`, tag-only, digest-only, wrong
repository, malformed CalVer, and unsupported platforms are rejected.

`image resolve` accepts a CalVer, pulls that immutable release tag into the
local image cache, resolves its repository digest, and inspects the remote
manifest. `image inspect` validates an exact target, requires both supported
platforms, requires the current host platform, and reuses
`verify-release-image.sh` for local/running identity when applicable. Buildx is
preferred when present; `docker manifest inspect` is the supported equivalent.

## 12. Status, Verification, And Logs

`status` reports Compose identity and files, service state and health, configured
and running image identities, local image ID, manifest digest, all public
probes, PostgreSQL health and schema version, backup location/latest metadata,
latest real restore evidence, Git identity and dirty state, operation lock, and
latest receipt. It performs no provider request.

`verify basic` checks running/healthy services, `/livez`, `/readyz`, `/version`,
PostgreSQL, exact planned/running image identity, read-only root, capability
drop, no-new-privileges, and bounded tmpfs. Deploy and upgrade apply run only
this level automatically.

`verify authenticated` additionally accepts an administrator password from an
interactive prompt or a protected file, sends it through request stdin, uses a
temporary mode-0600 cookie jar, and checks authenticated health/task state.
`verify gateway` requires `--allow-upstream-call`, an API key from a protected
file or prompt, and an explicit model. It checks `/v1/models` and one bounded
streaming `/v1/responses` completion without recording bodies.

`logs` defaults to a bounded tail and supports service, `--since`, `--tail`, and
`--follow`. Follow mode is streaming text or JSONL. The operation receipt holds
only execution metadata; it never embeds raw log content.

## 13. Backup And Restore Drill

`backup create` takes the shared operation lock, creates a custom archive as a
temporary file inside the target backup directory, verifies non-empty content
and `pg_restore --list`, computes SHA-256, applies safe permissions, atomically
renames the archive and metadata, and records UTC time, ID, size, checksum,
source digest, schema, verification status, and off-host status. Failure or
interruption removes only the exact temporary files. No retention deletion or
upload occurs. Missing encrypted off-host evidence yields `attention`.
Metadata also records `ci_fixture` or `real_operator` provenance and is bound to
the protected operator state with HMAC. The CLI does not accept a bare metadata
edit as off-host custody evidence.

`backup verify` proves archive structure and checksum only. It never reports a
successful restore.

`restore drill` validates a real immutable image and archive, rejects live
project names and live database URLs, acquires the shared lock, and directly
wraps `dev/verification/restore-backup.sh`. It captures only allowlisted output,
records cleanup status, and classifies evidence as `ci_fixture` or
`real_operator`. Candidate and current-image drills are distinct. Fixture
evidence can never satisfy a real-operator upgrade gate. Secrets are obtained
through protected files or interactive input and are unset on every exit.
After acquiring the lock, the controller stages a protected archive copy and
binds the evidence to its checksum. The complete isolated wrapper is bounded by
the global timeout, and controller signals are forwarded to its process group.

## 14. Plan And Apply

Plans are live-stack read-only but create a protected plan document and receipt.
They record plan schema, operation ID/type/time/expiry, Git identity, dirty
state, Compose inputs and HMAC, env path and HMAC, current service/image/schema,
target image, host platform, migration possibility, backup evidence, restore
evidence, rollback image, preflight checks, blockers, and invariants.

Apply accepts only `--plan`. It verifies plan HMAC, type, expiry, env and Compose
HMAC, current image, schema, service state, target, backup checksum, and restore
evidence before taking the shared exclusive lock. Any drift is
`stale_plan_detected`. Apply never accepts a replacement target.

External commands have a timeout or bounded poll. SIGINT/SIGTERM cleanup exact
temporary files, release the lock, and preserve a final receipt. The lock
records PID and operation ID; an unknown lock is never deleted automatically.

Repeating apply when the exact target is healthy returns `noop`,
`changed=false`, does not restart services, rerun migrations, or create a
backup, and still records the outcome.

### Deploy

Deploy plan additionally requires no existing Compose service, no existing
production volume, a valid port/bind, and explicit network/TLS risk decisions.
Apply revalidates, pulls the exact target, verifies digest/platform, creates the
stack, waits with a bound, runs basic verification, proves the running identity,
and writes a receipt. Compose exit zero alone is not success.

### Upgrade

Upgrade plan records the running source and exact rollback target, requires a
fresh verified pre-upgrade backup, requires a real-operator restore drill for
the current image, and, because a candidate can contain migrations, requires a
real-operator candidate drill. It may pull the candidate but does not recreate
the live stack. Apply rechecks source, backup/checksum, candidate, rollback
availability, and every invariant before atomically updating the persisted
image setting and recreating only required services.

### Rollback

Application rollback is a separate plan/apply operation. A plan is blocked
unless the previous exact digest exists and schema compatibility is proven by
unchanged schema evidence or a suitable isolated drill. A prior successful
deployment alone is insufficient. Rollback is never automatic after a failed
upgrade.

Database restore is documentation and isolated-drill only. The CLI never
executes a live restore, `compose down -v`, volume removal, dump overwrite, or
automatic transition from application failure to data restore.

## 15. Threat Model

The operator layer defends against malformed env files, unsafe file modes,
symlink swaps, world-writable state, stale or edited plans, concurrent mutation,
moving image tags, platform mismatch, deceptive Docker success, interrupted
file publication, secret leakage, accidental provider billing, fixture evidence
being promoted as production evidence, blind image downgrade, and destructive
database recovery.

Full host/root compromise, a malicious Docker daemon, compromised registry
credentials, external TLS/firewall correctness, and off-host storage custody
remain operator boundaries. The CLI reports these as owner gates rather than
claiming them verified.

## 16. Redaction

All command capture passes through one redaction boundary before persistence.
It removes password, secret, token, API key, Authorization, Cookie, OAuth,
proxy userinfo, PostgreSQL userinfo, and signed-URL values. Raw Compose
environment and Docker container environment are never captured.

Tests use `N2API_TEST_SECRET_CANARY_DO_NOT_LEAK` and scan stdout, stderr, JSON,
plans, receipts, operation listings, and retained test artifacts.

## 17. Skills And Agent Integration

`.agents/skills/n2api-local-refresh/` implements only the existing local
builder-prune, no-cache build, recreate, readiness, and smoke workflow. It does
not delete images, volumes, or data.

`.agents/skills/n2api-production-operations/` begins with `describe --format
json`, uses only the canonical CLI, runs read-only inspection before planning,
never bypasses plan/apply, never handles secrets in prose or arguments, never
promotes fixture restore evidence, and never performs live database restore.
High-risk apply remains explicit.

The root `AGENTS.md` gains only the canonical path, read-only operations,
plan/apply rule, secret rule, rollback boundary, skill names, and evidence
reporting requirement.

## 18. Compatibility And Migration

Existing env files remain valid inputs and are not reinitialized. Existing
secret-file overrides, production project `deploy`, services, PostgreSQL volume,
ports, loopback defaults, probes, and image publishing model remain unchanged.
Manual Compose commands remain a documented lower-level recovery path.

Operator state is outside the business database and repository by default. The
first stateful command creates versioned state without modifying the live stack.
No database migration exists solely for operation records.

## 19. Test Matrix

`make test-ops` uses fake Docker/Compose/curl/manifest tools, temporary protected
directories, env fixtures, health fixtures, archive metadata, and secret
canaries. It covers CLI parsing and help, JSON purity/schema, exit/reason codes,
doctor failures, env/link/mode checks, config placeholders and secret conflicts,
image syntax/platforms, plan integrity/expiry/drift, lock contention, signals,
timeouts, noop, receipt preservation, backup atomicity/checksum/failure cleanup,
restore classification/cleanup, apply failures, rollback blocks, and redaction.

Repository integration also runs `make test`, `make test-production-deploy`,
`make test-restore-backup`, `make test-contracts`, and all applicable heavy
Go/runtime gates. Every supported Compose combination is rendered. CI adds
`make test-ops` to the existing correctness flow without a new workflow, real
secret, real provider call, or backup upload.

## 20. Acceptance Criteria

Completion requires all 30 Definition of Done items from the source brief. The
authoritative mapping is recorded in
`docs/agent-native-operations-acceptance.md`: each CLI capability, plan/apply
invariant, lock, noop, signal/timeout behavior, backup property, restore
classification, redaction boundary, skill, documentation link, test command,
Compose combination, commit, local runtime refresh, and external owner gate has
an explicit evidence source.

Repository-local evidence cannot prove a real reverse proxy, real OAuth/provider
traffic, off-host backup custody, a current production restore, a GitHub-hosted
workflow for an unpushed commit, a release, or a production deployment. Those
remain clearly labeled external acceptance items.

# Backup And Restore Verification Plan

Status: Tasks 1-3 completed locally on 2026-07-21; Task 4 is owner-blocked
Public API changes: optional non-sensitive configuration export/import
Data migration: none initially

## Evidence Status (2026-07-23)

| Dimension | Status | Evidence and remaining gate |
| --- | --- | --- |
| `design` | partial | The isolated restore, drill record, and non-sensitive export contracts are defined. Import conflict and natural-key semantics remain owner-blocked. |
| `implementation` | partial | Local commits `93e6a52`, `e229053`, `5ed5a1b`, `0400c23`, `b9c9bb7`, and `2280d95` cover fixture restore, drill evidence, redacted export, and restore failure scenarios. Import is not implemented. |
| `merged` | partial | Four cited commits are on GitHub `main` at `3664abe`; `b9c9bb7` and `2280d95` remain local. |
| `local_tests` | complete | Generated current-schema and migration fixtures cover successful restore, wrong key, corrupt archive, interruption, cleanup, readiness, and mock gateway checks. Commit `372e049` advances the repeatable fixture to current schema 48 and migration from schema 47. Real-backup evidence remains under `operator_acceptance`. |
| `ci` | pending | No GitHub Actions run contains the local commits or restore driver. |
| `release_artifact` | pending | No release image has been accepted through a restore drill. |
| `operator_acceptance` | pending | Restore an encrypted off-host operator backup in isolation, validate the historical keyring and gateway, record duration, and obtain owner sign-off. |
| `owner_decision` | partial | Real restore evidence is required. Import conflict defaults and natural-key strategy remain undecided. |

## Current Baseline

`docs/manual.md` documents PostgreSQL custom-format backups and checking a dump
with `pg_restore --list`. There is no automatic restore into an isolated
database, migration/health validation, or minimum gateway exercise. PostgreSQL
is the authoritative complete backup; portable configuration export is a
separate convenience feature.

## Task 1: Add An Isolated Restore Verification Script

Status: completed locally on 2026-07-21; real operator-backup acceptance remains
required before claiming recoverability.

### Goal

Prove a dump can restore and serve core queries without touching production.

### Dependencies

Readiness endpoint and E2E mock upstream.

### Files

- Create: `dev/verification/restore-backup.sh`
- Create: `deploy/compose.restore-test.yaml`
- Create: `backend/e2e/restore_verification_test.go`
- Modify: `docs/manual.md`
- Test: shell validation plus a generated non-sensitive fixture database
- Migrate: none

### Implementation

Require explicit dump path, create a unique Compose project and temporary
volume/database, restore with `pg_restore`, start the matching N2API image,
apply migrations, wait for readiness, run counts/integrity queries and one mock
gateway request, emit a redacted report, then remove the temporary project.
Never derive or accept the production database target.

The script requires the exact N2API image, restored administrator credentials,
and encryption secret through environment variables. It uses a fixed internal
database URL and rejects any pre-existing resource carrying its generated
Compose project label before arming cleanup. `EXIT`, `INT`, and `TERM` cleanup
only that exact project. Restore uses `--single-transaction --exit-on-error
--no-owner --no-privileges`. A restored reusable API key is read through the
admin API without logging its value, proving the supplied encryption secret
matches before the mock gateway exercise runs.

### Tests And Verification

Run with a valid dump, corrupt dump, older schema dump, wrong encryption key,
and interrupted cleanup. Confirm production containers and volumes are unchanged.

Repeatable local generated-fixture evidence is available through
`make test-restore-backup`. The managed driver creates only run-labeled test
resources, invokes the production restore verifier for every scenario, and
fails if the existing `deploy` containers, volumes, networks, or running state
change:

- current schema 48 dump restores with secret and gateway checks;
- schema 47 dump migrates to schema 48 and passes the same checks;
- corrupt archive failed at `archive_list`;
- wrong encryption key failed at the restored-secret check;
- `TERM` during archive startup returned failure and left no generated
  container, volume, network, or build image; and
- the existing `deploy` resource identities and running state were unchanged
  and no container was recreated by any drill.

### Compatibility And Security

The report contains schema version, counts, and status codes only. Temporary
files use restrictive permissions.

### Risks And Rollback

Target confusion is destructive. The script must reject non-temporary Compose
project names and existing target volumes. Delete only its exact generated
project resources.

### Manual Acceptance

Required on a real operator backup before claiming recoverability.

### Completion Criteria

A current backup restores into isolation and passes readiness and minimum
gateway validation. Automated generated fixtures satisfy the code acceptance;
a real operator backup remains the manual recovery claim gate.

### Commit

`feat(ops): verify PostgreSQL backup restores`

## Task 2: Schedule And Record Restore Drills

Status: completed locally on 2026-07-21; each real-backup drill and owner
sign-off remains an operator action.

### Goal

Make restore validation repeatable without exposing a production dump to CI.

### Dependencies

Task 1.

### Files

- Modify: `docs/manual.md`, `docs/README.md`
- Create: `docs/release-checklist.md`

### Implementation

Document monthly/upgrade drills, image-version matching, backup retention,
encrypted off-host storage, expected duration, and owner sign-off. CI validates
only a generated fixture dump; operator backups stay local.

The documented baseline requires monthly and pre-upgrade drills, immutable
current/proposed image identifiers, a measured duration, three successful
monthly backups, pre-upgrade retention through the next successful monthly
drill, encrypted off-host storage with separately held key material, and dated
owner sign-off. The standalone operator record captures the non-sensitive
backup identifier, restored schema version, current and candidate image tags
and digests, and separate archive-list, restore, migration, readiness,
restored-secret, mock-gateway, and cleanup outcomes. It also keeps generated CI
fixture evidence independent from real operator-backup acceptance. The
checklist explicitly excludes secrets and real dumps from CI and release
records.

### Completion Criteria

Every release checklist asks for the last successful restore drill and tested
image version.

Local evidence: `docs/release-checklist.md` is linked from both the release
workflow instructions and the documentation index, and contains the required
real-backup recovery gate. No real operator drill is claimed by this task.

### Commit

`docs(ops): define restore drill procedure`

## Task 3: Export Non-sensitive Configuration

Status: completed locally on 2026-07-21, including non-sensitive alert rules
and redacted alert actions.

### Goal

Provide a versioned, portable file without credential material.

### Dependencies

Stable schemas for routing, pricing, fingerprints, and alerts.

### Files

- Create: admin export domain/service tests
- Modify: store/HTTP/frontend and System Event actions
- Document: format version and omitted secrets

### Implementation

Export routing pools/memberships, API key names/policies/limits (never key
hash/prefix/secret), provider account non-sensitive fields/models (never credentials),
pricing, gateway settings, fingerprint profiles, and alert rule structure with
redacted actions. Include format and application version.

The implemented v1 uses a repeatable-read, read-only PostgreSQL transaction and
explicit export DTOs. It emits file-local references for relationships, strips
userinfo/query/fragment from API-upstream base URLs, redacts every fingerprint
custom-header value, excludes revoked API keys, caps the attachment at 5 MiB,
and records the successful security audit event before sending the body.

The alerting extension completes portable format v1 without exporting a usable
delivery destination. Each `alertActions` entry has a file-local
`alert_action:<id>` reference plus `name`, `kind`, `enabled`, and
`destinationConfigured: true`. It does not include a `destination` placeholder
that a future importer could mistake for an endpoint. The snapshot query never
selects `encrypted_destination`, test results, delivery state, timestamps, or
runtime errors. The configured flag makes the missing operator-supplied
destination explicit for a future import while revealing neither ciphertext nor
endpoint shape.

Each `alertRules` entry has a file-local `alert_rule:<id>` reference and an
`actionRef` that must resolve to an exported action. It includes only the
portable rule definition: template key, name, enabled state, category,
severity, trigger and recovery actions, aggregation count/window, cooldown,
deduplication scope, and recovery-notification flag. Persisted matcher state,
cooldown timestamps, delivery attempts, and created/updated timestamps are not
portable configuration.

Both lists are read and reference-validated inside the existing repeatable-read,
read-only transaction and use deterministic name/ID ordering. Every configured
rule and action is exported, including disabled rows, because omitting a
referenced disabled action would break snapshot closure. The completed format
reports `unsupportedSections: []`, while `redactions` continues to name
`alertActionDestinations`, and the successful export event adds bounded action
and rule counts. Format version remains 1 because these sections were explicitly
declared unsupported rather than previously assigned a conflicting shape.

Focused tests prove reference closure, empty/non-empty deterministic
arrays, exact DTO fields, no encrypted destination column in export queries,
the absence of a destination field, no known destination canary in serialized
JSON, updated audit counts, and removal of both unsupported-section markers.
Snapshot failure, attachment-size, authentication, and fail-closed audit
behavior remain unchanged.

### Completion Criteria

Automated tests prove no sensitive column or known secret appears in output.

Local evidence: focused store/service/HTTP tests cover URL sanitation, complete
fingerprint-header value redaction, forbidden JSON field names, alert action
destination exclusion, deterministic action/rule ordering, relationship
closure, authentication, attachment bounds, audit counts, audit failure, and
snapshot failure. The Gateway UI provides a bounded authenticated download
without a new route, modal, or import placeholder.

### Commit

`feat(backup): export non-sensitive configuration`

## Task 4: Dry-run And Atomic Import

Status: blocked on owner approval of conflict defaults and the natural-key
strategy; implementation must not begin by assuming overwrite or rename
semantics.

### Goal

Validate mappings/conflicts before importing portable configuration.

### Dependencies

Task 3 and explicit owner approval of conflict defaults.

### Implementation

Parse with size/version limits, resolve natural-key conflicts, produce an ID
mapping and summary, reject missing relations, then apply in one transaction
with an audit event. Support dry run and idempotent replay. Sensitive full
backup export remains out of scope until a separate password-encrypted format
is approved.

### Completion Criteria

Dry run and apply produce the same summary; failed import leaves no partial data.

### Commit

`feat(backup): import versioned configuration safely`

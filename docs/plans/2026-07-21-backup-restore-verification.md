# Backup And Restore Verification Plan

Status: in progress; Tasks 1-2 completed locally on 2026-07-21
Public API changes: optional non-sensitive configuration export/import
Data migration: none initially

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

Local generated-fixture evidence:

- current schema dump restored at schema 39 with secret and gateway checks;
- schema 38 dump migrated to schema 39 and passed the same checks;
- corrupt archive failed at `archive_list`;
- wrong encryption key failed at the restored-secret check;
- `TERM` during archive startup returned failure and left no generated
  container, volume, or network; and
- the existing `deploy` development stack remained healthy and was not
  recreated by any drill.

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
owner sign-off. The checklist explicitly excludes secrets and real dumps from
CI and release records.

### Completion Criteria

Every release checklist asks for the last successful restore drill and tested
image version.

Local evidence: `docs/release-checklist.md` is linked from both the release
workflow instructions and the documentation index, and contains the required
real-backup recovery gate. No real operator drill is claimed by this task.

### Commit

`docs(ops): define restore drill procedure`

## Task 3: Export Non-sensitive Configuration

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
hash/secret), provider account non-sensitive fields/models (never credentials),
pricing, gateway settings, fingerprint profiles, and alert rule structure with
redacted actions. Include format and application version.

### Completion Criteria

Automated tests prove no sensitive column or known secret appears in output.

### Commit

`feat(backup): export non-sensitive configuration`

## Task 4: Dry-run And Atomic Import

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

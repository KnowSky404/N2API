# Backup And Restore Verification Plan

Status: planned before encryption rotation
Public API changes: optional non-sensitive configuration export/import
Data migration: none initially

## Current Baseline

`docs/manual.md` documents PostgreSQL custom-format backups and checking a dump
with `pg_restore --list`. There is no automatic restore into an isolated
database, migration/health validation, or minimum gateway exercise. PostgreSQL
is the authoritative complete backup; portable configuration export is a
separate convenience feature.

## Task 1: Add An Isolated Restore Verification Script

### Goal

Prove a dump can restore and serve core queries without touching production.

### Dependencies

Readiness endpoint and E2E mock upstream.

### Files

- Create: `dev/verification/restore-backup.sh`
- Create: `deploy/compose.restore-test.yaml`
- Modify: `docs/manual.md`
- Test: shell validation plus a generated non-sensitive fixture database
- Migrate: none

### Implementation

Require explicit dump path, create a unique Compose project and temporary
volume/database, restore with `pg_restore`, start the matching N2API image,
apply migrations, wait for readiness, run counts/integrity queries and one mock
gateway request, emit a redacted report, then remove the temporary project.
Never derive or accept the production database target.

### Tests And Verification

Run with a valid dump, corrupt dump, older schema dump, wrong encryption key,
and interrupted cleanup. Confirm production containers and volumes are unchanged.

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
gateway validation.

### Commit

`feat(ops): verify PostgreSQL backup restores`

## Task 2: Schedule And Record Restore Drills

### Goal

Make restore validation repeatable without exposing a production dump to CI.

### Dependencies

Task 1.

### Files

- Modify: `docs/manual.md`, release checklist/governance docs
- Optional create: manually triggered local workflow template without secrets

### Implementation

Document monthly/upgrade drills, image-version matching, backup retention,
encrypted off-host storage, expected duration, and owner sign-off. CI validates
only a generated fixture dump; operator backups stay local.

### Completion Criteria

Every release checklist asks for the last successful restore drill and tested
image version.

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

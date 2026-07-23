# Encryption Key Rotation Plan

Status: in progress; Tasks 1-2 completed locally on 2026-07-21
Public API changes: CLI first; optional admin status later
Data migration: versioned ciphertext envelope and rotation run state

## Current Baseline

The runtime now derives named AES-256 keys by SHA-256 hashing the configured
secret material. New writes use an authenticated
`n2api:v1:<key-id>:<secret-kind>:<payload>` envelope while reads retain legacy
raw-base64 compatibility. Provider
access/refresh/ID tokens, API-upstream keys, reusable client key secrets, proxy
credentials, and alert action destinations depend on this reversible encryption.

## Task 1: Introduce A Backward-compatible Ciphertext Envelope

Task status: completed locally on 2026-07-21; mixed-key restore acceptance with
a real operator backup remains an operational check

### Goal

Version new ciphertext while preserving reads of every legacy value.

### Dependencies

Verified database backup and restore drill.

### Files

- Modify: `backend/internal/secret/crypto.go`, `crypto_test.go`
- Modify: config for current/previous named keys and tests
- Modify: `.env.example`, `docs/manual.md`
- Test: legacy fixtures, tamper, unknown version/key ID, key order
- Migrate: no bulk rewrite yet

### Implementation

Use a self-describing string envelope with format version and non-secret key ID;
bind envelope metadata as GCM additional authenticated data. Read legacy raw
base64 with the current key, then configured previous keys only during an
explicit rotation window. New writes always use the current key.

### Completion Criteria

Legacy and new values decrypt, unknown/tampered envelopes fail, and errors never
contain key material or plaintext.

The local implementation injects one immutable keyring into admin and provider
credential paths while leaving Request Log and System Event cursor signing on
the current secret only. The current key ID defaults to `default`; an ordered,
strictly parsed JSON array supplies at most eight unique previous keys during a
rotation window. New writes use Go's random-nonce AES-GCM and authenticate the
version, key ID, and fixed credential kind as additional data. Legacy values
try current then previous keys, whereas an envelope never falls back from its
named key or crosses credential kinds. The isolated
restore drill accepts the same current ID and previous-key array so mixed-key
backups can be verified.

Local tests cover an immutable legacy fixture, previous-key order, current-key
writes, metadata tampering, cross-kind substitution, unknown versions and keys,
duplicate/unsafe config, secret-safe errors, admin/provider injection, and
fail-closed proxy decryption. No database rows are rewritten by this task.
Legacy values therefore do not gain kind binding until Task 3 rewrites them,
and version 1 does not claim same-kind row-identity binding.

### Risks And Rollback

Cryptographic format bugs can lock credentials. Keep immutable legacy fixtures
and do not rewrite data in this task. Rollback is asymmetric: an older image
cannot read envelopes written after upgrade, so restore the pre-upgrade backup
or keep the upgraded reader and prior keyring available.

### Commit

`feat(security): version encrypted secret envelopes`

## Task 2: Inventory And Verify Encrypted Values

Task status: lifecycle-aware inventory completed locally on 2026-07-23;
operator backup and isolated restore acceptance remain prerequisites for Task 3

### Goal

Prove which rows can be decrypted before rotation.

### Dependencies

Task 1.

### Files

- Create: admin CLI command wiring and secret inventory service/tests
- Modify: store queries for credential-bearing columns
- Document: backup prerequisite and redacted report format

### Implementation

Add `n2api admin verify-encryption` with dry-run output limited to table,
credential kind, numeric row ID, envelope format, authenticated key ID,
lifecycle status, stable reason code, and aggregate counts. It never prints
ciphertext or plaintext.

### Completion Criteria

The command accounts for every reversible secret class and exits nonzero on a
required or unknown unreadable value.

The local implementation uses one fixed, ordered PostgreSQL query to include
all eight non-empty secret columns, including OAuth `expires_at` and
`consumed_at` lifecycle evidence,
disabled providers, and revoked client keys. The read-only command runs before
server startup and does not migrate, bootstrap, or mutate the database. Its
deterministic JSON always includes all eight types and all six lifecycle states,
even when their counts are zero. Each non-empty value is classified as readable
by the current key, readable by a previous key, readable legacy, unreadable and
required, unreadable and explicitly expired/purgeable, or unreadable with
unknown lifecycle. Only successfully decrypted values expose an authenticated
key ID. Provider credentials, reusable client-key secrets, proxy credentials,
and alert destinations are required. OAuth code verifiers are purgeable only
when `expires_at` has passed or `consumed_at` is set; missing evidence is
unknown and blocking. Raw errors, plaintext, ciphertext, and unauthenticated key
IDs are never emitted. Exit codes distinguish non-blocking reports (`0`),
required/unknown unreadable values (`1`), and usage/infrastructure failure (`2`).

Local tests cover current and previous v1 envelopes, the actual key that opens
a legacy value, unknown keys, corrupt and cross-kind envelopes, active,
expired, consumed, and unknown-lifecycle OAuth states, all eight classes,
zero-count classes and statuses, deterministic ordering, canary redaction,
query coverage, stable numeric row IDs, CLI dispatch, and secret-safe errors.
No migration or data rewrite is part of this task.

### Commit

`feat(admin): verify encrypted credential inventory`

## Task 2A: Clean Expired OAuth State Secrets Safely

Task status: completed locally on 2026-07-23; operator execution against a real
deployment remains explicit and is not performed by tests.

### Goal

Remove only short-lived OAuth state records whose lifecycle proves they can no
longer complete an authorization flow.

### Implementation

Add `n2api admin cleanup-expired-oauth-states` with an explicit non-future UTC
cutoff, dry-run default, explicit `--execute`, and a bounded batch size. A row is
eligible only when `expires_at < cutoff` or a non-null
`consumed_at < cutoff`. The repository holds a dedicated session-level advisory
lock for the complete operation, deletes deterministic ID batches with
`FOR UPDATE SKIP LOCKED`, honors cancellation, and releases or destroys the
dedicated connection safely. Migration 46 adds a partial `consumed_at` index;
rollback drops only that index.

A successful real run records one sanitized
`oauth.state_cleanup.completed` System Event with cutoff, batch size, batch
count, and deleted count. Dry runs do not write an event or mutate data. A
concurrent worker returns a stable `contended` result, and repeated runs are
idempotent. PostgreSQL and event failures are returned through a fixed error and
the CLI writes only a structured stable error code.

### Tests And Verification

Unit tests cover dry-run, batch deletion, zero-row repeat, cancellation,
contention, future-cutoff rejection, stable event fields, event failure,
lock-release failure, and secret-safe errors. Isolated PostgreSQL tests cover
active, expired, consumed, and cutoff-boundary behavior; bounded batches;
repeated deletion; advisory-lock contention/reacquisition; and cancellation.

### Commit

`feat(ops): clean expired OAuth state secrets`

## Task 3: Add Resumable Re-encryption

Task status: blocked locally on the correct historical keyring and successful
operator-backup restore acceptance. The 2026-07-21 pre-lifecycle development
inventory found 14 unreadable values with the then-configured keyring: eight OAuth
code verifiers, one access token, one refresh token, one ID token, one provider
API key, and two reusable client-key secrets. No proxy value was present and no
alert action destination was present; no database value was modified. Do not
infer, replace, or remove key material to bypass this gate.

### Goal

Rewrite secrets in small verified batches with interruption recovery.

### Dependencies

Tasks 1-2 and a successful restore drill.

### Files

- Create: migration for rotation run/checkpoint state
- Create: rotation service/store/CLI and tests
- Modify: encrypted credential repositories only through shared helpers

### Implementation

Require current and previous keys, a recent backup confirmation, dry run, and
an exclusive PostgreSQL advisory lock. For each batch: decrypt, encrypt with
the current envelope, decrypt-verify, update conditionally against the original
value, then checkpoint. Reads accept both keys throughout. Restart resumes
idempotently.

### Tests And Verification

Interrupt after each stage, simulate concurrent updates, retry completed runs,
and verify old/new/failed rows. Restore the pre-rotation backup in isolation.

### Completion Criteria

All rows are current-key envelopes, counts match inventory, and rerun is a no-op.

### Risks And Rollback

Never remove the previous key until verification and backup retention complete.
Rollback uses dual-key reads or database restore.

### Commit

`feat(admin): rotate encrypted credentials safely`

## Task 4: Retire The Previous Key

### Goal

Prove no live value requires the previous key, then remove it operationally.

### Dependencies

Task 3 and an operator-defined observation period.

### Implementation

Run inventory with only the current key in a staging/temporary restore, verify
all credentials, remove previous-key configuration, restart, and test providers.

### Manual Acceptance

Required; this is an operational step, not an automatic database action.

### Completion Criteria

The application starts and all secret consumers work with only the current key.

### Commit

`docs(security): document encryption key retirement`

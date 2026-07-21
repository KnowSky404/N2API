# Encryption Key Rotation Plan

Status: in progress; Task 1 completed locally on 2026-07-21
Public API changes: CLI first; optional admin status later
Data migration: versioned ciphertext envelope and rotation run state

## Current Baseline

The runtime now derives named AES-256 keys by SHA-256 hashing the configured
secret material. New writes use an authenticated
`n2api:v1:<key-id>:<secret-kind>:<payload>` envelope while reads retain legacy
raw-base64 compatibility. Provider
access/refresh/ID tokens, API-upstream keys, reusable client key secrets, proxy
credentials, and future webhook secrets depend on this reversible encryption.

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

### Goal

Prove which rows can be decrypted before rotation.

### Dependencies

Task 1.

### Files

- Create: admin CLI command wiring and secret inventory service/tests
- Modify: store queries for credential-bearing columns
- Document: backup prerequisite and redacted report format

### Implementation

Add `n2api admin verify-encryption` with dry-run output limited to table/type,
counts, key IDs, and failures by stable row ID. It never prints ciphertext or
plaintext.

### Completion Criteria

The command accounts for every reversible secret class and exits nonzero on
any unreadable value.

### Commit

`feat(admin): verify encrypted credential inventory`

## Task 3: Add Resumable Re-encryption

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

# Encryption Key Rotation Plan

Status: design-ready; implementation waits for backup restore verification
Public API changes: CLI first; optional admin status later
Data migration: versioned ciphertext envelope and rotation run state

## Current Baseline

`secret.EncryptString` derives one AES-256 key by SHA-256 hashing
`N2API_ENCRYPTION_SECRET`, seals with AES-GCM, and stores raw base64
`nonce+ciphertext`. The format has no version or key identifier. Provider
access/refresh/ID tokens, API-upstream keys, reusable client key secrets, proxy
credentials, and future webhook secrets depend on reversible encryption.

## Task 1: Introduce A Backward-compatible Ciphertext Envelope

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

### Risks And Rollback

Cryptographic format bugs can lock credentials. Keep immutable legacy fixtures
and do not rewrite data in this task.

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

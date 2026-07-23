# Operator Restore Drill Record

Use one copy of this template for each real-backup restore drill. Store the
record in an operator-controlled location. Do not attach the database archive
or expose a storage URL.

Allowed check states are `passed`, `failed`, `pending`, and `not_applicable`.
Record only stable status codes and non-sensitive identifiers. Never record a
credential, encryption secret, public or signed storage URL, dump contents,
complete archive listing, restored row data, or complete storage object list.

## Drill Identity

- Drill date and time (UTC):
- Backup creation date and time (UTC):
- Backup identifier (non-sensitive object or inventory reference):
- Source deployment version or digest:
- Current image tag:
- Current image digest:
- Candidate image tag (`not_applicable` when no candidate is tested):
- Candidate image digest (`not_applicable` when no candidate is tested):
- Restored schema version:
- Planned drill window:
- Measured duration:

## Independent Evidence Status

- CI generated-fixture restore status:
- CI evidence reference (commit or workflow run, when available):
- Real operator-backup restore status:
- Operator evidence reference (non-sensitive local record identifier):

A passing CI fixture restore does not satisfy the real operator-backup status.
Leave remote CI fields `pending` when no matching workflow has run.

## Real Backup Checks

- Archive list status:
- Restore status:
- Migration status:
- Readiness status:
- Restored-secret check status:
- Mock gateway check status:
- Cleanup status:
- Stable failure code, if any:

Cleanup passes only after the drill's temporary containers, network, and volume
have been removed without changing the live deployment.

## Retention And Approval

- Encrypted off-host copy status:
- Retention expiry or deletion condition:
- Exception owner, reason, and expiry (only when a failed or overdue gate is accepted):
- Owner sign-off name or identifier:
- Owner sign-off date (UTC):


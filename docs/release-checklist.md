# Release Checklist

Use this checklist for every release preview and publish decision. Record exact
immutable identifiers and timestamps, but never copy passwords, tokens, API
keys, encryption secrets, dump contents, or storage credentials into the
record.

## Candidate Identity

- [ ] Source commit SHA:
- [ ] Proposed CalVer:
- [ ] Tested source image tag (`sha-<12 characters>`):
- [ ] Tested manifest digest:
- [ ] Platforms are exactly `linux/amd64` and `linux/arm64`:
- [ ] Release preview workflow run:
- [ ] Required backend, frontend, and image checks passed:

## Supply Chain Evidence

- [ ] `linux/amd64` and `linux/arm64` image evidence matrix jobs passed:
- [ ] Both platform SBOMs were generated from the tested parent manifest digest without rebuilding:
- [ ] The release prepare job verified an SPDX attestation for the exact tested digest, repository workflow, and source commit:
- [ ] Both 14-day evidence artifacts contain a non-empty SPDX JSON SBOM, Trivy JSON, and non-sensitive metadata naming the same parent digest:
- [ ] UNKNOWN, LOW, MEDIUM, HIGH, and CRITICAL report-only counts were reviewed for both platforms:

Vulnerability findings do not currently block a release by severity. The owner
must approve a severity, fix-availability, and time-bounded exception policy
before findings become a release gate. Evidence generation, schema validation,
attestation, and upload failures are already blocking workflow errors.

## Restore Drill Gate

Run a real-backup restore drill at least monthly and again before every upgrade.
The pre-upgrade drill must use a fresh backup. First prove that backup with the
currently deployed immutable image. When the upgrade contains migrations, run
a second isolated drill from the same backup with the proposed immutable image
to prove migration and readiness before changing the live stack. Complete one
[operator restore drill record](restore-drill-record.md) for each drill and
reference its non-sensitive identifier below.

- [ ] Operator restore drill record identifier:
- [ ] Real operator-backup restore status is `passed`:
- [ ] CI generated-fixture restore status was recorded independently:
- [ ] Drill date and time (UTC):
- [ ] Backup creation date and time (UTC):
- [ ] Backup identifier (non-sensitive object or inventory reference):
- [ ] Source deployment version or digest:
- [ ] Current image tested (tag and digest):
- [ ] Proposed image tested when migrations apply (tag and digest, or `N/A`):
- [ ] Restored schema version:
- [ ] Planned drill window:
- [ ] Measured duration:
- [ ] Archive list status is `passed`:
- [ ] Restore status is `passed`:
- [ ] Migration status is `passed` or `not_applicable`:
- [ ] Readiness status is `passed`:
- [ ] Restored-secret check status is `passed`:
- [ ] Mock gateway check status is `passed`:
- [ ] Cleanup status is `passed`:
- [ ] Encrypted off-host copy exists:
- [ ] Retention expiry or deletion condition:
- [ ] Owner sign-off and date:

Allocate at least 60 minutes, or twice the most recent measured duration when
that is longer. This is a scheduling baseline, not a recovery-time guarantee;
record the measured duration so the next drill window can be adjusted.

Keep at least the three most recent successful monthly backups. Keep every
pre-upgrade backup until the upgraded deployment has passed its next monthly
restore drill. Store backups outside the Compose volume in encrypted off-host
storage, with decryption material held separately. A local backup is not a
second copy, and a CI artifact is not approved backup storage.

The drill record may contain a non-sensitive storage object identifier or
inventory reference, but not a public or signed URL, credential, encryption
key, dump contents, complete archive listing, restored data, or complete object
list. A failed or overdue drill blocks the recovery claim and release approval
until the owner records an explicit, time-bounded exception.

## Deployment And Verification

- [ ] PostgreSQL backup for the deployment was created and retained:
- [ ] Release Compose configuration validates without printing secrets:
- [ ] The immutable image was pulled and the stack recreated:
- [ ] `/readyz`, `/livez`, `/version`, and authenticated admin health passed:
- [ ] Provider account test passed:
- [ ] `/v1/models` and one streaming `/v1/responses` request passed:
- [ ] Rollback image and backup identifiers are recorded:
- [ ] Owner approved publish or deployment:

CI restore checks may create and destroy generated, non-sensitive fixture
dumps. Their status must remain separate from real operator-backup acceptance.
Real operator backups must remain in the operator-controlled local and off-host
recovery path and must never be uploaded to GitHub Actions.

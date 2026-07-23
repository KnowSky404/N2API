# Repository Protections

This document defines the intended GitHub repository rules for N2API. It does
not assert that the settings are currently active and does not authorize an
agent or workflow to change them. Apply them only after an owner-authorized push
has created the named checks on GitHub.

## `main` Ruleset

Create an active branch ruleset targeting only the default branch, `main`.
Configure it to:

- Block branch deletion and force pushes.
- Require changes through a pull request, with conversation resolution.
- Require the branch or merge group to be tested against current `main`.
- Require the GitHub Actions checks listed below.
- Allow bypass only for the repository owner or a narrowly scoped emergency
  maintainer role. Treat bypass as an exceptional recovery path and record the
  reason, commit, and follow-up verification in the pull request or private
  security advisory.

For this personal repository, zero approving reviews is acceptable when there
is only one maintainer; the pull request still provides a reviewable diff and
enforces checks. Increase the approval count when another trusted maintainer is
available. Do not weaken status checks merely to accommodate the single-owner
case.

### Required Checks

Select these exact GitHub Actions check names after each has completed at least
once on GitHub:

- `Test`
- `Build and smoke test image (linux/amd64)`
- `Build and smoke test image (linux/arm64)`
- `Security exception policy`
- `CodeQL (go)`
- `CodeQL (javascript-typescript)`
- `Dependency vulnerabilities`

Both required workflows listen for `pull_request` and
`merge_group: checks_requested`, so these checks can be used with GitHub's merge
queue. If merge queue is enabled, verify a real queued pull request before
treating the ruleset as complete.

### Code Scanning Merge Protection

Required `CodeQL (go)` and `CodeQL (javascript-typescript)` status checks prove
that analysis completed, but those checks alone do not block a merge merely
because CodeQL reported an alert. In the same `main` ruleset, also require code
scanning results from CodeQL and configure merge protection so every new alert
with `HIGH` or `CRITICAL` security severity blocks the pull request.

Review the existing CodeQL baseline before enabling the rule. Do not lower the
severity threshold or dismiss an alert only to make the ruleset pass. A
temporary accepted finding must use the exact, expiring exception policy in
`security/exceptions.json`, retain its owner and reason, and be removed before
expiry.

Do not require the following checks on pull requests or merge groups because
their jobs intentionally run only after a push to `main`, on a schedule, or on
manual dispatch:

- `Publish tested platform image (linux/amd64)`
- `Publish tested platform image (linux/arm64)`
- `Publish multi-platform image`
- `Image evidence (linux/amd64)`
- `Image evidence (linux/arm64)`
- `Resolve latest stable image`
- `Stable image evidence (linux/amd64)`
- `Stable image evidence (linux/arm64)`
- `Prepare release`
- `Publish release`

Requiring an event-incompatible check would leave pull requests or merge groups
permanently waiting for a job that cannot start.

## Release Environment

Create an Actions environment named exactly `release`; the publish job already
targets that name. Configure the environment to:

- Allow deployments only from `main`.
- Require an explicit reviewer before the publish job receives environment
  access.
- Disable administrator bypass when repository recovery procedures permit it.
- Keep secrets out of the environment unless a later release integration
  requires them; the current workflow uses only `GITHUB_TOKEN` permissions.

GitHub allows up to six required reviewers and one approval releases the job.
For a single-maintainer repository, leave self-review enabled so the owner can
perform the explicit approval. When a second trusted maintainer is available,
enable prevention of self-review and require that independent reviewer.

The environment protects only `Publish release`. `Prepare release` remains a
read-only preview and verification job so it can produce evidence before an
owner decides whether to approve publication.

## Activation And Verification

Apply protections in this order:

1. Push the workflows only with explicit owner authorization and wait for the
   `CI Image` and `Security` runs to finish.
2. Confirm every required check name exists and has a successful run; do not
   create placeholder or similarly named checks.
3. With explicit owner authorization, create the `main` ruleset, including the
   CodeQL code-scanning merge protection described above.
4. Open a test pull request and verify separately that a failing required check
   and a new HIGH or CRITICAL CodeQL alert each block merge. A successful CodeQL
   status check must not bypass the alert gate.
5. If using merge queue, enqueue the test pull request and confirm both required
   workflows run for the merge group.
6. Create the `release` environment and run Release in `preview` mode first.
   Confirm `publish` waits for approval and that rejection leaves tags, Releases,
   and package tags unchanged.

If a rule prevents all normal recovery, disable only that exact rule, record
the reason, restore the repository, and re-enable it after the same required
checks pass. Do not delete the ruleset as a routine workaround.

## Acceptance State

Local acceptance consists of workflow syntax, check-name, merge-group, and
least-privilege validation. Actual ruleset and environment state, CodeQL
severity enforcement, the first protected pull request, merge-queue behavior,
and release approval remain operator acceptance checks until the owner
explicitly authorizes those remote changes.

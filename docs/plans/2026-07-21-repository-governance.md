# Repository Governance Plan

Status: planned; license choice remains owner-controlled
Public API changes: none
Data migration: none

## Current Baseline

The repository has CI Image and Release workflows with commit-pinned actions,
multi-platform smoke tests, tested-digest publication, attestations, and
Conventional Commit-based release notes. It has no `LICENSE`, `SECURITY.md`,
`CONTRIBUTING.md`, issue/PR templates, dependency update config, CodeQL,
`govulncheck`, frontend audit, container scan, published SBOM, or documented
supported-version/security-release policy.

## Task 1: Establish Security And Contribution Policies

### Goal

Tell operators and contributors how to report, change, test, and release safely.

### Dependencies

Owner chooses contact/private reporting channel and supported-version policy.

### Files

- Create: `SECURITY.md`, `CONTRIBUTING.md`, `.github/pull_request_template.md`
- Create: `.github/ISSUE_TEMPLATE/bug.yml`, `feature.yml`, `config.yml`
- Modify: `docs/README.md`

### Implementation

Document private vulnerability reporting, no-secret diagnostics, supported
versions, response expectations, Bun/Go/Docker verification, atomic Conventional
Commits, personal V1 scope, and release/security checklist. Templates request
reproduction and sanitized logs without requiring public secrets.

### Tests And Verification

Validate links and YAML; review rendered templates on a branch/PR.

### Compatibility And Security

Do not publish a personal email unless the owner approves it.

### Completion Criteria

Security reports and ordinary changes have clear, scoped paths.

### Commit

`docs(governance): add security and contribution policies`

## Task 2: Select A License

### Goal

Present owner-approved license options; do not auto-select.

### Dependencies

Explicit owner decision.

### Options

- MIT: minimal conditions, broad reuse, no explicit patent grant.
- Apache-2.0: permissive with explicit patent terms and NOTICE obligations.
- AGPL-3.0: reciprocal source obligations for network use; materially changes
  adoption and contribution expectations.
- Keep all rights reserved temporarily: clearest ownership but blocks ordinary
  open-source reuse/contribution.

### Files

- Create only after decision: `LICENSE`
- Modify: `README.md`, package/release metadata if needed

### Completion Criteria

The exact license text and repository references match the owner's decision.

### Commit

`docs: add <license-name> license`

## Task 3: Automate Dependency Updates

### Goal

Make pinned Go, Bun, Actions, Docker, and PostgreSQL updates reviewable.

### Dependencies

Container dependency pinning plan.

### Files

- Create: `.github/dependabot.yml` or approved Renovate config, not both
- Modify: contributor/release docs

### Implementation

Group low-risk updates by ecosystem, separate majors, set modest schedules and
open-PR limits, and require the normal backend/frontend/image smoke suite. Base
image digest updates must preserve readable version tags.

### Completion Criteria

A dry/config validation and first update PR demonstrate each ecosystem path.

### Commit

`chore(deps): configure automated dependency updates`

## Task 4: Add Source And Dependency Vulnerability Checks

### Goal

Detect actionable Go, JavaScript, and workflow issues without blocking on noise.

### Dependencies

Task 1 triage policy.

### Files

- Create: CodeQL workflow and/or extend CI with `govulncheck`
- Modify: frontend audit command after selecting a Bun-compatible authoritative path
- Document: severity/exception/expiry policy

### Implementation

Use current official actions pinned by SHA. Run CodeQL for Go/JavaScript,
`govulncheck ./...`, and a Bun-compatible lockfile audit. Initially report-only
for a short baseline period; make high-confidence findings required after
documented exceptions are resolved.

### Completion Criteria

Checks produce actionable findings and an expiring exception format.

### Commit

`ci(security): scan source and dependencies`

## Task 5: Scan Images And Publish SBOMs

### Goal

Attach machine-readable composition and vulnerability evidence to tested images.

### Dependencies

Pinned runtime image and tested-digest release path.

### Files

- Modify: `.github/workflows/ci-image.yml`, `release.yml`
- Modify: release checklist/manual

### Implementation

Generate SPDX or CycloneDX SBOM from the tested manifest, attach/attest it to
the immutable digest, scan the same digest, and gate only on an approved
severity/fix-available policy. Never rebuild for scanning.

### Completion Criteria

Each release digest has provenance, SBOM, and scan results tied to the same
artifact.

### Commit

`ci(security): attest and scan release images`

## Task 6: Configure Repository Protections

### Goal

Apply governance settings after checks and names are stable.

### Dependencies

Tasks 1, 4, and 5; explicit owner authorization for GitHub changes.

### Implementation

Require PRs or documented owner bypass, required `Test` and both platform image
checks, conversation resolution, and protected release environment approval.
Do not modify repository settings automatically without authorization.

### Completion Criteria

The documented branch/release policy matches actual GitHub settings.

### Commit

`docs(governance): document repository protections`

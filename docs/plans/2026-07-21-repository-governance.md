# Repository Governance Plan

Status: in progress; Tasks 1, 3, 4, and 5 completed locally; owner decisions remain
Public API changes: none
Data migration: none

## Current Baseline

The repository has CI Image and Release workflows with commit-pinned actions,
multi-platform smoke tests, tested-digest publication, attestations, and
Conventional Commit-based release notes. The workflows generate and attest two
platform SPDX SBOMs, retain Trivy evidence tied to the tested parent manifest
digest, and block unexcepted fixed HIGH/CRITICAL findings. Security and
contribution policies, templates, CodeQL, `govulncheck`, frontend audit, and an
expiring exception registry are configured locally. No `LICENSE` has been
selected. Dependabot version updates cover every current dependency ecosystem.

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

Task status: configured locally on 2026-07-21; the first generated update PR
remains a remote acceptance check after an owner-authorized push

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

The local configuration covers Go modules, both Bun manifests, the Python uv
contract manifest, GitHub Actions, Dockerfiles, and Compose definitions. Local
schema and repository-path checks pass. The first generated update PR cannot be
observed until this commit reaches GitHub, so that remote evidence remains
pending without expanding this task's push authorization.

### Commit

`chore(deps): configure automated dependency updates`

## Task 4: Add Source And Dependency Vulnerability Checks

Task status: implemented locally on 2026-07-23; first GitHub CodeQL and
scheduled-scan evidence remains pending after an owner-authorized push

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
`govulncheck ./...`, and a Bun-compatible lockfile audit. CodeQL reports the
`security-extended` suite while reachable Go findings and HIGH/CRITICAL Bun
findings block immediately. Apply only exact, expiring exceptions from the
shared registry.

### Completion Criteria

Checks produce actionable findings and an expiring exception format.

### Commit

`ci(security): scan source and dependencies`

## Task 5: Scan Images And Publish SBOMs

Task status: implemented locally on 2026-07-21; remote workflow and registry
evidence remain pending after an owner-authorized push

### Goal

Attach machine-readable composition and vulnerability evidence to tested images.

### Dependencies

Pinned runtime image and tested-digest release path.

### Files

- Modify: `.github/workflows/ci-image.yml`, `release.yml`
- Modify: release checklist/manual

### Implementation

The manifest publication job exports the fully-qualified image name and tested
parent manifest digest. A dependent `linux/amd64` and `linux/arm64` matrix reads
that exact `IMAGE@digest` with platform-specific Syft and Trivy selection; it
does not build or retag the image. Each matrix leg validates its SPDX JSON,
attests it to the parent manifest in GHCR, validates the Trivy JSON schema,
writes only severity counts and the blocking policy to the job summary, and
uploads the SBOM, vulnerability report, and non-sensitive relationship metadata
for 14 days.

The release prepare job verifies at least one SPDX attestation for the exact
parent digest, source commit, repository, and `CI Image` workflow before it
renders a preview or publishes. The two-platform completeness guarantee remains
the CI evidence matrix rather than a fragile count of CLI verification output.
The Release body and operator documentation state that both platform records
are tied to the same digest and that stable release promotion never rebuilds it.

Unexcepted HIGH/CRITICAL findings with a non-empty Trivy `FixedVersion` block
the platform job. Unfixed findings remain report-only. Exact CVE or package
exceptions are platform-scoped and expire within 30 days. Scanner, report,
registry, schema, attestation, and artifact failures fail closed.

A separate weekly/manual read-only workflow resolves the latest stable GitHub
Release CalVer and requires its CalVer and `latest` GHCR tags to match. It scans
the existing parent digest for both platforms, uploads 14-day evidence, and
applies the same gate without building, retagging, publishing, or creating an
issue.

### Completion Criteria

Each release digest has provenance, SBOM, and scan results tied to the same
artifact.

Local completion covers pinned action references, workflow syntax, static
contracts, documentation, and the no-rebuild digest relationship. Because no
push is authorized in this task, the first two-platform workflow artifacts,
registry attestations, and Release prepare verification remain remote acceptance
evidence rather than a local completion claim.

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

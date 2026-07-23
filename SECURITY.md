# Security Policy

## Report A Vulnerability Privately

Use [GitHub private vulnerability reporting](https://github.com/KnowSky404/N2API/security/advisories/new)
to report a suspected vulnerability. This opens a private Security Advisory
with the maintainers.

Do not open a public issue, pull request, or discussion for a vulnerability.
N2API does not publish a personal email address for security reports.

## What To Include

Provide only the minimum sanitized information needed to reproduce and assess
the issue:

- The affected N2API version, image tag, or commit SHA.
- The affected component and deployment mode.
- A concise impact description and expected security boundary.
- Minimal, deterministic reproduction steps using synthetic accounts and data.
- Sanitized error codes, HTTP status codes, and relevant configuration key
  names without their values.
- Any temporary coordination or disclosure constraints.

Replace sensitive values with obvious placeholders before submitting the
report. Verify the final advisory text and every attachment after redaction.

## Never Include Sensitive Data

Do not submit or attach:

- Administrator passwords, client API keys, provider API keys, or proxy
  credentials.
- OAuth access tokens, refresh tokens, ID tokens, authorization callback codes,
  code verifiers, or complete callback URLs.
- Session cookies, browser storage, encryption secrets, previous encryption
  keys, or database connection strings.
- PostgreSQL dumps, backup archives, restored records, or complete database
  listings.
- Complete request or response bodies, complete headers, raw upstream errors,
  packet captures containing credentials, or production logs that have not
  been reviewed and sanitized.

Use synthetic fixtures where possible. If a secret was exposed during testing,
revoke or rotate it outside the report workflow.

## Handling And Disclosure

Maintainers will coordinate triage, remediation, and disclosure inside the
private advisory. Reports for any currently deployed version are welcome;
affected and fixed versions will be recorded in the advisory after assessment.
Do not publish details before maintainers confirm that operators have a safe
upgrade path.

Security fixes must follow the same test, migration, image evidence, and
release gates as other changes. See the [release checklist](docs/release-checklist.md).

## Automated Security Checks

The `Security` workflow runs on changes to `main`, pull requests, merge queue
checks, a weekly schedule, and manual dispatch. It performs these source and
dependency checks:

- CodeQL analyzes Go and JavaScript/TypeScript with the `security-extended`
  query suite and uploads SARIF results. Repository rules determine which
  CodeQL alert severities block a merge.
- `govulncheck` blocks reachable Go vulnerabilities.
- `bun audit --audit-level=high` blocks HIGH and CRITICAL frontend dependency
  vulnerabilities.
- `actionlint` and the pinned-dependency policy reject malformed workflows and
  external Actions that are not pinned to complete commit SHAs.

Temporary scanner exceptions live in `security/exceptions.json`. Every entry
must use one exact scanner-specific CVE, package, or rule identifier, name an
owner and reason, and include RFC 3339 creation and expiry timestamps. Wildcard,
duplicate, future-dated, expired, and longer-than-30-day exceptions are
rejected. Invalid registries, scanner failures, and malformed reports fail
closed. Resolve the finding and remove the exception before its expiry; do not
renew an exception in place merely to keep a check green.

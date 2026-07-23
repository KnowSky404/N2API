# Reliability And Operations Acceptance Report

Date: 2026-07-23

This report closes the repository-local portion of the reliability, security,
recovery, gateway-boundary, and governance iteration. It keeps local evidence,
historical GitHub evidence, current-HEAD CI, release artifacts, operator
acceptance, and owner decisions separate.

The accepted local baseline includes `bf0da37`. The branch has not been pushed,
so no workflow run or published image contains that complete baseline. CI Image
run `29976822364` for `3664abe` is historical evidence for its older container
subset only.

## Overall Conclusion

M1 through M7 are complete for every repository-local requirement selected by
the iteration. The remaining gates require real operator data, a real deployed
topology, GitHub-hosted execution, or an owner decision and must remain pending.
Do not publish a release from this baseline until the current source commit is
on GitHub, its required workflows pass, the exact tested digest exists, and the
operator gates in the release checklist are accepted.

Closed release blockers include bounded request and response handling, timeout
and cancellation behavior, proxy plus TLS fingerprint preservation, persisted
Stateful Responses affinity, enforced single-instance operation, end-to-end
correlation IDs, visible best-effort Request Log failures, fail-closed rotation
preflight, generated restore fixtures, source and image security workflows, and
bounded opt-in metrics.

Still-blocking evidence includes a real encrypted operator-backup restore with
the historical keyring, the intended reverse proxy and OAuth/Codex path, current
HEAD GitHub workflows and image evidence, repository protections, the release
environment, external readiness monitoring, encrypted off-host backup, and the
license decision.

## M1: Recovery And Encryption Lifecycle

- Original problem and root cause: reversible secrets had no complete lifecycle
  inventory, expired OAuth state had no bounded cleanup path, fixture restores
  did not cover failure cleanup, and rotation lacked a fail-closed operator gate.
- Solution: deterministic redacted inventory classes, bounded and locked OAuth
  state cleanup, current and previous-key-aware restore fixtures, a real-drill
  record, and a dry-run rotation preflight. Re-encryption itself remains gated.
- Key files: `backend/internal/encryptioninventory/`,
  `backend/internal/oauthstatecleanup/`, `backend/internal/encryptionrotation/`,
  `dev/verification/restore-backup.sh`, `docs/restore-drill-record.md`, and the
  backup and encryption activity plans.
- Migration and configuration: migration `00046_oauth_state_cleanup.sql` adds
  lifecycle data. Current key ID, previous-key JSON, backup timestamps, and the
  non-sensitive record ID are explicit inputs.
- Compatibility and security: legacy envelopes stay readable. Reports never
  contain plaintext, ciphertext, credentials, raw database errors, or an
  unauthenticated key ID; required unreadable values return non-zero.
- Tests and commits: inventory, canary, cleanup, concurrency, cancellation,
  idempotence, gate rejection, wrong key, corrupt archive, TERM cleanup, schema
  48 restore, and 47-to-48 migration are covered by `4b1c9af`, `0afe82d`,
  `e5e5b58`, `b9c9bb7`, `2280d95`, and `372e049`.
- Pending: real backup restore, historical keyring, encrypted off-host copy,
  approved inventory, re-encryption/retirement drill, and owner sign-off.

## M2: Gateway Resource And Timeout Boundaries

- Original problem and root cause: request acceptance and replay shared one
  limit, admission occurred after buffering, non-stream responses could be
  unbounded, and server/upstream/SSE timeouts were incomplete.
- Solution: separate accepted/replay bounds, pre-body admission, one-attempt
  behavior above replay size, bounded response copy, HTTP idle/header/body
  limits, upstream connect/header limits, SSE idle reset, and shutdown
  cancellation.
- Key files: `backend/internal/config/config.go`,
  `backend/internal/gateway/proxy.go`, `backend/cmd/n2api/main.go`, mock upstream
  scenarios, and `backend/e2e/resource_boundaries_e2e_test.go`.
- Migration and configuration: no migration. The `N2API_GATEWAY_*` body/response
  bounds and `N2API_HTTP_*`/upstream/SSE timeout settings are validated at
  startup; replay cannot exceed acceptance.
- Compatibility and security: `/v1/models`, `/v1/responses`, and
  `/v1/chat/completions` remain compatible. New failures use stable errors and
  do not return addresses, raw network failures, headers, or truncated bodies.
- Tests and commits: known/chunked oversize, slow body, response stall,
  oversized JSON, periodic/stalled SSE, disconnect, fallback attribution,
  socket idle/header behavior, and shutdown are covered by `ff53bb8` through
  `326b788`, plus `b35089f`, `650bf86`, and `6da9ec0`.
- Pending: real reverse-proxy and real-account streaming acceptance.

## M3: Transport Registry And Proxy Fingerprints

- Original problem and root cause: transport construction and the proxy plus
  fingerprint combination needed proof that a configured proxy did not silently
  bypass uTLS behavior or leak credential-bearing registry keys.
- Solution: bounded concurrent transport registry, normalized redacted keys,
  idle-connection cleanup, account invalidation, and HTTP CONNECT followed by
  the selected uTLS handshake.
- Key files: `backend/internal/gateway/transport_registry.go`,
  `fingerprint_transport.go`, `proxy.go`, and their network acceptance tests.
- Migration and configuration: none. Supported proxy behavior is limited to the
  documented HTTP CONNECT matrix; unsupported combinations fail closed.
- Compatibility and security: connection reuse is preserved; proxy credentials
  are represented only by an irreversible identifier and never enter logs,
  errors, metrics, or System Events.
- Tests and commits: direct/proxy by fingerprint/no-fingerprint matrix, real
  ClientHello distinction, Basic auth redaction, reuse, capacity, invalidation,
  CONNECT/TLS failure, cancellation, and SSE tunnel coverage are in `5e57b16`,
  `99b6ba8`, and `3046c49`.
- Pending: acceptance through the operator's actual proxy topology.

## M4: Stateful Responses Affinity

- Original problem and root cause: follow-up Responses operations could select
  a different account because upstream response ownership was not durable.
- Solution: PostgreSQL affinity keyed by a domain-separated HMAC of response ID,
  forced account selection before routing, fail-closed unknown multi-account
  behavior, successful-final-account writes, and bounded retention.
- Key files: `backend/internal/store/response_affinity.go`, gateway affinity and
  retention code, provider selection, and PostgreSQL-backed E2E coverage.
- Migration and configuration: `00047_response_affinities.sql`; TTL, retention
  runner interval, and batch size are conservative and disabled by default.
- Compatibility and security: full response IDs are not stored. A single-account
  compatibility path remains; multi-account unknown affinity returns a stable
  conflict without exposing an account ID.
- Tests and commits: JSON/SSE extraction, GET, input items, previous response,
  fallback, duplicate/concurrent writes, expiry, deletion, write failure, HMAC
  redaction, and process rebuild persistence are covered by `e6e82d3`,
  `73c146f`, and `49363aa`.
- Pending: real upstream Stateful Responses acceptance.

## M5: Single Instance And Failure Observability

- Original problem and root cause: process-local limits were unsafe with two
  instances, request identities diverged across stores and retries, and Request
  Log write failures were silently ignored.
- Solution: a dedicated session advisory lock monitored for connection loss,
  an explicit unsafe override, one normalized correlation ID, an independent
  upstream request ID, and bounded in-process Request Log health state.
- Key files: `backend/internal/store/instance_lock.go`, process lifecycle tests,
  gateway/http request-ID paths, `backend/internal/requestlog/write_monitor.go`,
  authenticated health, and Request Log E2E coverage.
- Migration and configuration: `00048_request_log_upstream_request_id.sql`;
  `N2API_ALLOW_UNSAFE_MULTI_INSTANCE` defaults false.
- Compatibility and security: the override preserves recovery/debug use but
  emits `unsafe_multi_instance_enabled`. Database failures do not change a
  successful gateway response and never return SQL, DSNs, bodies, or raw errors.
- Tests and commits: second-instance refusal, release after shutdown, lock loss,
  two unsafe processes with distinct listeners and authenticated warnings,
  correlation concurrency, retry/fallback identity, write failure, recovery,
  and redaction are covered by `caf6445`, `734f265`, `7e9d5d5`, `5c2a9b4`,
  `05c5083`, `06da041`, `226951f`, and `bf0da37`.
- Pending: deployed graceful-shutdown and external health acceptance.

## M6: Security Governance And CI

- Original problem and root cause: repository policies, source scans, expiring
  exceptions, stable-digest rescans, and documented protections were incomplete
  or not enforced as one reviewable chain.
- Solution: security/contribution templates, strict issue-form validation,
  CodeQL and dependency scanning, fixed-vulnerability policy, 30-day exceptions,
  no-rebuild platform evidence, stable-image rescans, and documented rulesets.
- Key files: `SECURITY.md`, `CONTRIBUTING.md`, `.github/`, `security/`, `dev/ci/`,
  `docs/repository-protections.md`, and `docs/release-checklist.md`.
- Migration and configuration: no database migration. Actions are fully pinned,
  use least privilege, and do not use `pull_request_target` for untrusted code.
- Compatibility and security: scanners, schemas, malformed evidence, expired
  exceptions, and fixed HIGH/CRITICAL findings fail closed. No `LICENSE` was
  selected or created.
- Tests and commits: issue forms, action pins, exception policy, actionlint
  1.7.12 plus ShellCheck 0.10.0, govulncheck policy, Bun high audit, workflow
  contracts, and stable-image no-mutation behavior are covered by the governance
  series from `7e31ad9` through `6863e43`.
- Pending: current-HEAD Security and CI Image runs, CodeQL findings, AMD64/ARM64
  evidence, digest attestations, release preview, ruleset/environment activation,
  and the license decision.

## M7: Bounded Prometheus Metrics

- Original problem and root cause: PostgreSQL views were the only operational
  signal and no low-cardinality process endpoint existed for database-failure or
  external-monitor paths.
- Solution: optional separate metrics listener, loopback default, mandatory
  bearer for non-loopback, closed label sets, in-memory observers, pool stats,
  readiness, alerting, and background-task state. Tracing remains out of scope.
- Key files: `backend/internal/metrics/`, gateway/request-log/system-event
  observers, `deploy/compose.metrics.yaml`, and the metrics contract.
- Migration and configuration: none. `N2API_METRICS_ENABLED=false` by default;
  bind address, port, and bearer are independently validated.
- Compatibility and security: metrics do not use the public listener, query
  large tables, write System Events, or label requests, accounts, keys, pools,
  models, URLs, tokens, bodies, or raw errors.
- Tests and commits: disabled/listener policy, auth, bind failure, traffic,
  label allowlist, canaries, concurrency, SSE lifetime/cancellation, shutdown,
  unavailable PostgreSQL scrape, and the 1,516-of-1,600 series budget are covered
  by `39389e6`, `57029b9`, `ada47b4`, and `bbb1a39`.
- Pending: scrape the real deployment and attach an external readiness monitor.

## Local Verification

The following managed commands passed against the local baseline:

```bash
make test
make test-e2e
make test-contracts
make test-request-log-profile
make test-restore-backup
make test-dev-artifacts
```

Focused race tests passed for `cmd/n2api`, `internal/gateway`, and
`internal/metrics`. The unsafe-override addition separately passed:

```bash
go test -run '^TestInstanceLockProcessLifecycle$' -count=1 -v ./cmd/n2api
go test -race -run '^TestInstanceLockProcessLifecycle$' -count=1 ./cmd/n2api
go test -count=1 ./cmd/n2api
```

The exact actionlint 1.7.12 and ShellCheck 0.10.0 pair, reachable
`govulncheck@v1.6.0` evaluator, pinned dependency validator, security exception
tests, issue-form validator, and Bun high audit also passed. Development,
release, E2E, and restore Compose configurations were rendered successfully.

There are no unresolved local test failures. Initial failures caused by the
read-only global Go cache, read-only Buildx state, and disk preflight were
rerun with managed temporary resources. The restore schema mismatch was a real
fixture defect fixed by `372e049` and retested.

Current-HEAD GitHub CI, CodeQL, platform image evidence, registry attestations,
release preview, real OAuth, real reverse proxy, and real-backup restore were
not executed and must not be treated as passed.

## Manual Acceptance Commands

Run these only in an operator-controlled shell. Do not enable shell tracing,
pipe output to public logs, or attach dumps, tokens, cookies, callbacks, request
bodies, or response bodies to GitHub artifacts.

### Real Restore Drill

Use a fresh real backup and the exact current immutable image first. Repeat with
the candidate image when it contains migrations. The subshell and trap prevent
secret values from surviving interruption:

```bash
(
  set -euo pipefail
  trap 'unset N2API_RESTORE_ADMIN_PASSWORD N2API_RESTORE_ENCRYPTION_SECRET N2API_RESTORE_ENCRYPTION_PREVIOUS_KEYS' EXIT INT TERM
  read -rp 'Local backup archive: ' N2API_RESTORE_ARCHIVE
  read -rp 'Immutable image tag or digest: ' N2API_RESTORE_IMAGE
  read -rp 'Current encryption key ID: ' N2API_RESTORE_ENCRYPTION_KEY_ID
  read -rsp 'Restore admin password: ' N2API_RESTORE_ADMIN_PASSWORD; echo
  read -rsp 'Restore encryption secret: ' N2API_RESTORE_ENCRYPTION_SECRET; echo
  read -rsp 'Previous-key JSON (or []): ' N2API_RESTORE_ENCRYPTION_PREVIOUS_KEYS; echo
  export N2API_RESTORE_IMAGE N2API_RESTORE_ENCRYPTION_KEY_ID
  export N2API_RESTORE_ADMIN_PASSWORD N2API_RESTORE_ENCRYPTION_SECRET
  export N2API_RESTORE_ENCRYPTION_PREVIOUS_KEYS
  export N2API_RESTORE_ADMIN_USERNAME=admin
  dev/verification/restore-backup.sh "$N2API_RESTORE_ARCHIVE"
)
```

Expected: archive, restore, migration, readiness, restored-secret, mock gateway,
and cleanup statuses pass. Record only non-sensitive results in
`docs/restore-drill-record.md` and `docs/release-checklist.md`.

### Reverse Proxy Acceptance

Prerequisites: `N2API_PUBLIC_URL` is the real HTTPS origin, only exact proxy
peers are trusted, and the edge overwrites forwarding headers.

```bash
set -euo pipefail
read -rp 'Public HTTPS origin (for example https://n2api.example): ' N2API_PUBLIC_ORIGIN
read -rp 'Direct private N2API origin (for example http://127.0.0.1:3000): ' N2API_DIRECT_ORIGIN

curl -fsS -D - -o /dev/null "$N2API_PUBLIC_ORIGIN/readyz" \
  | awk 'BEGIN { IGNORECASE=1 } /^strict-transport-security:/ { found=1 } END { exit !found }'

test "$(curl -sS -o /dev/null -w '%{http_code}' -X POST \
  -H "Origin: $N2API_PUBLIC_ORIGIN" -H 'Sec-Fetch-Site: same-origin' \
  "$N2API_PUBLIC_ORIGIN/api/admin/logout")" = 204
test "$(curl -sS -o /dev/null -w '%{http_code}' -X POST \
  -H 'Origin: https://attacker.invalid' -H 'Sec-Fetch-Site: same-origin' \
  "$N2API_PUBLIC_ORIGIN/api/admin/logout")" = 403

curl -fsS -D - -o /dev/null \
  -H 'X-Forwarded-Proto: http' -H 'X-Forwarded-Host: attacker.invalid' \
  "$N2API_DIRECT_ORIGIN/readyz" \
  | awk 'BEGIN { IGNORECASE=1 } /^strict-transport-security:/ { found=1 } END { exit !found }'
```

Expected: public HTTPS has HSTS, the correct same-origin mutation reaches the
handler, the forged origin is rejected, and untrusted direct forwarding headers
cannot replace the configured HTTPS public origin. Confirm one controlled
request's client network in Request Logs/System Events without storing the full
address in this report.

### Real OAuth And Codex Acceptance

Complete OAuth in the admin UI, test the account, enable its model, bind a
temporary client key to a routing pool, and use the current profile format in
the manual. Then run without placing the key in shell history or retaining the
model response:

```bash
(
  set -euo pipefail
  trap 'unset N2API_API_KEY' EXIT INT TERM
  read -rsp 'Temporary N2API client key: ' N2API_API_KEY; echo
  export N2API_API_KEY
  curl -fsS -H "Authorization: Bearer $N2API_API_KEY" \
    http://127.0.0.1:3000/v1/models \
    | jq -e '.object == "list" and (.data | length > 0)' >/dev/null
  codex exec --ephemeral --profile n2api --sandbox read-only \
    'Reply with exactly N2API_OK and do not use tools.' >/dev/null 2>&1
)
```

Expected: both commands exit zero. In the admin UI, verify only redacted Request
Log metadata, the selected final account, usage source, tokens, estimated cost,
and one correlation ID. Delete the temporary client key after acceptance. Do
not retain the callback URL or Codex request/response body.

### Release Preview

This requires an owner-authorized push and a successful current-HEAD CI Image
run. Preview does not publish:

```bash
set -euo pipefail
gh auth status
SOURCE_SHA="$(git rev-parse HEAD)"
test "$(git rev-parse origin/main)" = "$SOURCE_SHA"
BEFORE_TAGS="$(git ls-remote --tags origin | sha256sum | cut -d' ' -f1)"
BEFORE_RELEASES="$(gh release list --limit 100 --json tagName,isDraft,isPrerelease,publishedAt | sha256sum | cut -d' ' -f1)"
BEFORE_LATEST="$(docker buildx imagetools inspect ghcr.io/knowsky404/n2api:latest | sed -n 's/^Digest:[[:space:]]*//p' | head -n 1)"
gh workflow run release.yml --ref main -f mode=preview
RUN_ID=
for _ in {1..30}; do
  RUN_ID="$(gh run list --workflow Release --branch main --event workflow_dispatch \
    --limit 20 --json databaseId,headSha,createdAt \
    | jq -r --arg sha "$SOURCE_SHA" 'map(select(.headSha == $sha)) | max_by(.createdAt) | .databaseId // empty')"
  [[ "$RUN_ID" =~ ^[0-9]+$ ]] && break
  sleep 2
done
[[ "$RUN_ID" =~ ^[0-9]+$ ]]
gh run watch "$RUN_ID" --exit-status
PREVIEW_DIR="$(mktemp -d /tmp/n2api-release-preview.XXXXXX)"
trap 'rm -rf -- "$PREVIEW_DIR"' EXIT INT TERM
chmod 700 "$PREVIEW_DIR"
gh run download "$RUN_ID" --pattern 'release-preview-*' --dir "$PREVIEW_DIR"
find "$PREVIEW_DIR" -maxdepth 3 -type f -print
test "$(git ls-remote --tags origin | sha256sum | cut -d' ' -f1)" = "$BEFORE_TAGS"
test "$(gh release list --limit 100 --json tagName,isDraft,isPrerelease,publishedAt | sha256sum | cut -d' ' -f1)" = "$BEFORE_RELEASES"
test "$(docker buildx imagetools inspect ghcr.io/knowsky404/n2api:latest | sed -n 's/^Digest:[[:space:]]*//p' | head -n 1)" = "$BEFORE_LATEST"
```

Expected: only `Prepare release` runs, the preview artifact is non-empty, and no
new Git tag, GitHub Release, CalVer image tag, or `latest` movement occurs.

### Security Evidence Review

Use the exact current source SHA and keep downloaded evidence in a private
temporary directory:

```bash
set -euo pipefail
SOURCE_SHA="$(git rev-parse HEAD)"
RUN_ID="$(gh run list --workflow 'CI Image' --branch main --limit 30 \
  --json databaseId,headSha,createdAt \
  | jq -r --arg sha "$SOURCE_SHA" 'map(select(.headSha == $sha)) | max_by(.createdAt) | .databaseId // empty')"
[[ "$RUN_ID" =~ ^[0-9]+$ ]]
gh run view "$RUN_ID" --exit-status
EVIDENCE_DIR="$(mktemp -d /tmp/n2api-image-evidence.XXXXXX)"
trap 'rm -rf -- "$EVIDENCE_DIR"' EXIT INT TERM
chmod 700 "$EVIDENCE_DIR"
gh run download "$RUN_ID" --pattern 'image-evidence-*' --dir "$EVIDENCE_DIR"
mapfile -t METADATA < <(find "$EVIDENCE_DIR" -name 'n2api-evidence-*.json' -type f | sort)
test "${#METADATA[@]}" -eq 2
PARENT_DIGEST="$(jq -r '.parentDigest' "${METADATA[0]}")"
test "$(printf '%s\n' "${METADATA[@]}" | xargs -r jq -r '.parentDigest' | sort -u | wc -l)" -eq 1
test "$(printf '%s\n' "${METADATA[@]}" | xargs -r jq -r '.platform' | sort)" = $'linux/amd64\nlinux/arm64'
for platform in amd64 arm64; do
  jq -e 'type == "object" and (.spdxVersion | type == "string") and (.packages | type == "array")' \
    "$(find "$EVIDENCE_DIR" -name "n2api-sbom-$platform.spdx.json" -type f -print -quit)" >/dev/null
  dev/ci/evaluate-trivy-report.sh \
    "$(find "$EVIDENCE_DIR" -name "n2api-trivy-$platform.json" -type f -print -quit)" \
    "linux/$platform" security/exceptions.json
done
gh attestation verify "oci://ghcr.io/knowsky404/n2api@$PARENT_DIGEST" \
  --repo KnowSky404/N2API \
  --predicate-type https://spdx.dev/Document \
  --signer-workflow KnowSky404/N2API/.github/workflows/ci-image.yml \
  --source-digest "$SOURCE_SHA"
```

Expected: Test, both platform smoke jobs, publish, both evidence jobs, Trivy
policy, and the exact digest/source attestation pass. Review report-only unfixed
findings and every active exception before approval.

### Repository Protection Checklist

Do not mutate settings without explicit owner authorization. Read the intended
rules first and compare them with the live state:

```bash
set -euo pipefail
gh auth status
gh ruleset list --repo KnowSky404/N2API
gh api repos/KnowSky404/N2API/rulesets --jq '.[] | {id,name,enforcement,target}'
gh api repos/KnowSky404/N2API/environments/release \
  --jq '{name,protection_rules,deployment_branch_policy}'
```

The owner-authorized configuration must match
`docs/repository-protections.md`: PR-only `main`, conversation resolution,
required Test/AMD64/ARM64/Security/CodeQL/dependency checks, no force push,
HIGH/CRITICAL CodeQL merge protection, recorded emergency bypass, and a
review-protected `release` environment. Verify a failing test PR, a new blocking
CodeQL alert, a merge-group run when enabled, preview non-mutation, and publish
approval before marking operator acceptance complete.

## Decisions And Remaining Blockers

- Owner decisions pending: release loopback/proxy topology, alert policies not
  selected by the iteration, repository settings authorization, and license.
- External dependencies pending: real OpenAI OAuth/Codex, reverse proxy,
  destination delivery, external readiness monitor, and GitHub-hosted jobs.
- Recovery pending: real backup, historical keyring, encrypted off-host copy,
  rotation/retirement drill, and owner sign-off.
- GitHub pending: push, current-HEAD CI Image and Security runs, exact digest,
  SBOM/Trivy/attestation review, release preview, ruleset, and environment.

These gates block a release recommendation but do not invalidate the completed
repository-local implementation and verification evidence above.

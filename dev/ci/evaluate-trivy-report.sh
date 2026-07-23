#!/usr/bin/env bash

set -euo pipefail

if [[ $# -ne 3 ]]; then
  echo "usage: evaluate-trivy-report.sh <report.json> <platform> <exceptions.json>" >&2
  exit 2
fi

repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
report="$1"
platform="$2"
registry="$3"

case "$platform" in
  linux/amd64|linux/arm64) ;;
  *)
    echo "Trivy policy received an unsupported platform" >&2
    exit 2
    ;;
esac

test -s "$report"
identifiers="$(
  cd "$repo_root"
  go run dev/ci/validate-security-exceptions.go \
    --active-identifiers trivy "$platform" "$registry"
)"

while IFS= read -r identifier; do
  if [[ -n "$identifier" && "$identifier" != cve:* && "$identifier" != package:* ]]; then
    echo "Trivy exception has an unsupported identity type" >&2
    exit 1
  fi
done <<< "$identifiers"

jq -e '
  type == "object" and
  (.SchemaVersion == 2) and
  (.ArtifactName | type == "string" and length > 0) and
  (.Results | type == "array") and
  all(
    .Results[];
    type == "object" and
    (.Target | type == "string" and length > 0) and
    ((.Vulnerabilities // []) | type == "array") and
    all(
      (.Vulnerabilities // [])[];
      type == "object" and
      (.VulnerabilityID | type == "string" and length > 0) and
      (.PkgName | type == "string" and length > 0) and
      (.Severity | type == "string" and
        IN("UNKNOWN", "LOW", "MEDIUM", "HIGH", "CRITICAL")) and
      ((.FixedVersion // "") | type == "string")
    )
  )
' "$report" >/dev/null

policy="$(
  jq --arg identifiers "$identifiers" '
    ($identifiers | split("\n") | map(select(length > 0))) as $allowed |
    [
      .Results[]?.Vulnerabilities[]? |
      select(
        (.Severity == "HIGH" or .Severity == "CRITICAL") and
        ((.FixedVersion // "") | length > 0)
      )
    ] as $actionable |
    [
      $actionable[] | . as $finding |
      select(
        ($allowed | index("cve:" + $finding.VulnerabilityID)) == null and
        ($allowed | index("package:" + $finding.PkgName)) == null
      )
    ] as $blocking |
    {
      actionable: ($actionable | length),
      excepted: (($actionable | length) - ($blocking | length)),
      blocking: ($blocking | length)
    }
  ' "$report"
)"

blocking="$(jq -r '.blocking' <<< "$policy")"
if [[ ! "$blocking" =~ ^[0-9]+$ ]]; then
  echo "Trivy report evaluation produced an invalid count" >&2
  exit 1
fi
if (( blocking > 0 )); then
  echo "Trivy found $blocking fixed HIGH/CRITICAL vulnerability finding(s) without an active exception" >&2
  exit 1
fi

echo "Trivy fixed-vulnerability policy passed"

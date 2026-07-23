#!/usr/bin/env bash

set -euo pipefail

if [[ $# -ne 2 ]]; then
  echo "usage: evaluate-govulncheck-report.sh <report.json> <exceptions.json>" >&2
  exit 2
fi

repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
report="$1"
registry="$2"

test -s "$report"
identifiers="$(
  cd "$repo_root"
  go run dev/ci/validate-security-exceptions.go \
    --active-identifiers govulncheck source "$registry"
)"

jq -s -e '
  ([.[] | select(.config != null) | .config] | length) == 1 and
  ([.[] | select(.config != null) | .config][0] |
    .protocol_version == "v1.0.0" and
    .scan_mode == "source" and
    .scan_level == "symbol") and
  all(.[]; type == "object")
' "$report" >/dev/null

unexcepted="$(
  jq -s --arg identifiers "$identifiers" '
    ($identifiers | split("\n") | map(select(length > 0))) as $allowed |
    ([
      .[] | .osv? | select(type == "object") |
      select((.id | type) == "string" and .id != "") |
      {
        key: .id,
        value: [.aliases[]? | select(type == "string" and startswith("CVE-"))]
      }
    ] | from_entries) as $aliases |
    [
      .[] | .finding? | select(. != null) |
      select(any(.trace[]?; (.function // "") != "")) |
      .osv as $id |
      select(($id | type) == "string" and $id != "") |
      select(
        ($allowed | index("rule:" + $id)) == null and
        ($allowed | index("cve:" + $id)) == null and
        all(
          ($aliases[$id] // [])[];
          . as $alias | ($allowed | index("cve:" + $alias)) == null
        )
      ) |
      $id
    ] | unique | length
  ' "$report"
)"

if [[ ! "$unexcepted" =~ ^[0-9]+$ ]]; then
  echo "govulncheck report evaluation produced an invalid count" >&2
  exit 1
fi
if (( unexcepted > 0 )); then
  echo "govulncheck found $unexcepted reachable vulnerability finding(s) without an active exception" >&2
  exit 1
fi

echo "govulncheck reachable-vulnerability policy passed"

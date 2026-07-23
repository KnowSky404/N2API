#!/usr/bin/env bash

set -euo pipefail

repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
work_dir="$(mktemp -d "${TMPDIR:-/tmp}/n2api-security-policy.XXXXXX")"
trap 'rm -rf -- "$work_dir"' EXIT

report="$work_dir/govulncheck.json"
registry="$work_dir/exceptions.json"
codeql_config="$work_dir/codeql.yml"

write_report() {
  local function_name="$1"
  jq -cn '{config:{protocol_version:"v1.0.0",scanner_name:"govulncheck",scan_mode:"source",scan_level:"symbol"}}'
  jq -cn '{osv:{id:"GO-2026-1234",aliases:["CVE-2026-1234"]}}'
  jq -cn --arg function_name "$function_name" '{finding:{osv:"GO-2026-1234",fixed_version:"v1.2.3",trace:[{module:"example.test/module",package:"example.test/module/pkg",function:$function_name}]}}'
}

write_report "Vulnerable" > "$report"
jq -n '{version:1,exceptions:[]}' > "$registry"
if "$repo_root/dev/ci/evaluate-govulncheck-report.sh" "$report" "$registry" >/dev/null 2>&1; then
  echo "unexcepted reachable govulncheck finding unexpectedly passed" >&2
  exit 1
fi

"$repo_root/dev/ci/render-codeql-config.sh" "$registry" "$codeql_config"
grep -Fx '  - uses: security-extended' "$codeql_config" >/dev/null
if grep -Fq 'query-filters:' "$codeql_config"; then
  echo "empty exception registry unexpectedly rendered CodeQL filters" >&2
  exit 1
fi

created_at="$(date -u -d '1 day ago' +'%Y-%m-%dT%H:%M:%SZ')"
expires_at="$(date -u -d '1 day' +'%Y-%m-%dT%H:%M:%SZ')"
jq -n \
  --arg created_at "$created_at" \
  --arg expires_at "$expires_at" \
  '{version:1,exceptions:[{
    scanner:"codeql",
    rule:"go/example-query",
    platform:"source",
    reason:"Temporary test exception.",
    owner:"security-test",
    created_at:$created_at,
    expires_at:$expires_at
  }]}' > "$registry"
"$repo_root/dev/ci/render-codeql-config.sh" "$registry" "$codeql_config"
grep -Fx 'query-filters:' "$codeql_config" >/dev/null
grep -Fx '      id: "go/example-query"' "$codeql_config" >/dev/null

jq -n \
  --arg created_at "$created_at" \
  --arg expires_at "$expires_at" \
  '{version:1,exceptions:[{
    scanner:"govulncheck",
    rule:"GO-2026-1234",
    platform:"source",
    reason:"Temporary test exception.",
    owner:"security-test",
    created_at:$created_at,
    expires_at:$expires_at
  }]}' > "$registry"
"$repo_root/dev/ci/evaluate-govulncheck-report.sh" "$report" "$registry" >/dev/null

jq -n \
  --arg created_at "$created_at" \
  --arg expires_at "$expires_at" \
  '{version:1,exceptions:[{
    scanner:"govulncheck",
    cve:"CVE-2026-1234",
    platform:"source",
    reason:"Temporary test exception.",
    owner:"security-test",
    created_at:$created_at,
    expires_at:$expires_at
  }]}' > "$registry"
"$repo_root/dev/ci/evaluate-govulncheck-report.sh" "$report" "$registry" >/dev/null

write_report "" > "$report"
jq -n '{version:1,exceptions:[]}' > "$registry"
"$repo_root/dev/ci/evaluate-govulncheck-report.sh" "$report" "$registry" >/dev/null

jq -cn '{config:{protocol_version:"v0",scan_mode:"source",scan_level:"symbol"}}' > "$report"
if "$repo_root/dev/ci/evaluate-govulncheck-report.sh" "$report" "$registry" >/dev/null 2>&1; then
  echo "invalid govulncheck protocol unexpectedly passed" >&2
  exit 1
fi

echo "Security policy evaluator tests passed."

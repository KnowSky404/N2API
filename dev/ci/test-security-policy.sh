#!/usr/bin/env bash

set -euo pipefail

repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
work_dir="$(mktemp -d "${TMPDIR:-/tmp}/n2api-security-policy.XXXXXX")"
trap 'rm -rf -- "$work_dir"' EXIT

report="$work_dir/govulncheck.json"
registry="$work_dir/exceptions.json"
codeql_config="$work_dir/codeql.yml"
trivy_report="$work_dir/trivy.json"

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

workflow="$repo_root/.github/workflows/ci-image.yml"
job_block() {
  local job="$1"
  awk -v target="  ${job}:" '
    $0 == target { active = 1 }
    active && $0 ~ /^  [A-Za-z0-9_-]+:$/ && $0 != target { exit }
    active { print }
  ' "$workflow"
}

image_job="$(job_block image)"
publish_job="$(job_block publish-platform)"
manifest_job="$(job_block manifest)"

grep -Fq '  merge_group:' "$workflow"
grep -Fq '      contents: read' <<< "$image_job"
if grep -Fq 'packages: write' <<< "$image_job"; then
  echo "image build job unexpectedly has package write permission" >&2
  exit 1
fi
grep -Fq '    needs: image' <<< "$publish_job"
grep -Fq '      packages: write' <<< "$publish_job"
grep -Fq '      - name: Download tested platform image' <<< "$publish_job"
grep -Fq '      - name: Validate and load tested platform image' <<< "$publish_job"
grep -Fq '    needs: publish-platform' <<< "$manifest_job"

write_trivy_report() {
  local severity="$1"
  local fixed_version="$2"
  jq -n \
    --arg severity "$severity" \
    --arg fixed_version "$fixed_version" \
    '{
      SchemaVersion: 2,
      ArtifactName: "example.test/n2api@sha256:test",
      Results: [{
        Target: "example.test/n2api (alpine 3.23)",
        Vulnerabilities: [{
          VulnerabilityID: "CVE-2026-4321",
          PkgName: "libexample",
          InstalledVersion: "1.0.0",
          FixedVersion: $fixed_version,
          Severity: $severity
        }]
      }]
    }'
}

write_trivy_report CRITICAL 1.0.1 > "$trivy_report"
jq -n '{version:1,exceptions:[]}' > "$registry"
if "$repo_root/dev/ci/evaluate-trivy-report.sh" "$trivy_report" linux/amd64 "$registry" >/dev/null 2>&1; then
  echo "unexcepted fixed Trivy finding unexpectedly passed" >&2
  exit 1
fi

jq -n \
  --arg created_at "$created_at" \
  --arg expires_at "$expires_at" \
  '{version:1,exceptions:[{
    scanner:"trivy",
    cve:"CVE-2026-4321",
    platform:"linux/amd64",
    reason:"Temporary test exception.",
    owner:"security-test",
    created_at:$created_at,
    expires_at:$expires_at
  }]}' > "$registry"
"$repo_root/dev/ci/evaluate-trivy-report.sh" "$trivy_report" linux/amd64 "$registry" >/dev/null
if "$repo_root/dev/ci/evaluate-trivy-report.sh" "$trivy_report" linux/arm64 "$registry" >/dev/null 2>&1; then
  echo "platform-mismatched Trivy exception unexpectedly passed" >&2
  exit 1
fi

jq '.exceptions[0] |= (del(.cve) + {package:"libexample"})' "$registry" > "$work_dir/package-exception.json"
"$repo_root/dev/ci/evaluate-trivy-report.sh" \
  "$trivy_report" linux/amd64 "$work_dir/package-exception.json" >/dev/null

write_trivy_report HIGH "" > "$trivy_report"
jq -n '{version:1,exceptions:[]}' > "$registry"
"$repo_root/dev/ci/evaluate-trivy-report.sh" "$trivy_report" linux/amd64 "$registry" >/dev/null
jq 'del(.Results[0].Vulnerabilities[0].FixedVersion)' \
  "$trivy_report" > "$work_dir/unfixed-trivy.json"
"$repo_root/dev/ci/evaluate-trivy-report.sh" \
  "$work_dir/unfixed-trivy.json" linux/amd64 "$registry" >/dev/null

write_trivy_report MEDIUM 1.0.1 > "$trivy_report"
"$repo_root/dev/ci/evaluate-trivy-report.sh" "$trivy_report" linux/amd64 "$registry" >/dev/null

jq '.SchemaVersion = 1' "$trivy_report" > "$work_dir/invalid-trivy.json"
if "$repo_root/dev/ci/evaluate-trivy-report.sh" "$work_dir/invalid-trivy.json" linux/amd64 "$registry" >/dev/null 2>&1; then
  echo "invalid Trivy schema unexpectedly passed" >&2
  exit 1
fi

echo "Security policy evaluator tests passed."

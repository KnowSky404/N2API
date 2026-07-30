#!/usr/bin/env bash

set -Eeuo pipefail

repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd -P)"
runbook="${repo_root}/docs/agent-operations.md"
manual="${repo_root}/docs/manual.md"

fail() {
  printf 'FAIL: %s\n' "$*" >&2
  exit 1
}

require_text() {
  local file=$1 text=$2
  grep -Fq -- "${text}" "${file}" || fail "missing '${text}' in ${file#"${repo_root}/"}"
}

[[ -f "${runbook}" ]] || fail "agent operations runbook is missing"

for heading in \
  '## Scenario A: First Deployment On A New Host' \
  '## Scenario B: Check Whether An Upgrade Is Safe' \
  '## Scenario C: Apply A Reviewed Upgrade' \
  '## Scenario D: Readiness Fails After Upgrade' \
  '## Scenario E: Monthly Restore Drill Only'; do
  require_text "${runbook}" "${heading}"
done

for command in \
  './ops/n2api describe --format json' \
  'doctor --format json' \
  'config init \' \
  'config validate --format json' \
  'deploy plan --format json' \
  'deploy apply --plan "$DEPLOY_PLAN" --format json' \
  'status --format json' \
  'image resolve --version YYYYMMDDNN --format json' \
  'backup create --evidence-class real_operator --format json' \
  'backup verify --archive "$BACKUP_ARCHIVE" --format json' \
  'restore drill --archive "$BACKUP_ARCHIVE" --image "$CURRENT_IMAGE" \' \
  'upgrade plan --image "$TARGET_IMAGE" --format json' \
  'upgrade apply --plan "$UPGRADE_PLAN" --format json' \
  'verify --level basic --format json' \
  'logs n2api --since 30m --tail 300' \
  'operations show "$UPGRADE_OPERATION_ID" --format json' \
  'rollback plan --format json'; do
  require_text "${runbook}" "${command}"
done

require_text "${runbook}" 'Live database restore is not implemented.'
require_text "${runbook}" 'lower-level fallback, not the default agent workflow.'
require_text "${repo_root}/README.md" '(docs/agent-operations.md)'
require_text "${repo_root}/docs/README.md" '(agent-operations.md)'
require_text "${manual}" 'canonical production operations entry point'
require_text "${manual}" 'Lower-level Compose fallback'

describe="$(${repo_root}/ops/n2api describe --format json)"
jq -e '
  .status == "succeeded" and
  .current.manual == "docs/agent-operations.md" and
  .current.plan_apply.apply_requires_plan == true and
  .current.plan_apply.live_database_restore_supported == false
' <<<"${describe}" >/dev/null || fail "describe contract does not point to the runbook"

printf 'PASS: agent operations documentation contract\n'

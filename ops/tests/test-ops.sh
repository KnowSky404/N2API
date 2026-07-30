#!/usr/bin/env bash

set -Eeuo pipefail

umask 077

repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd -P)"
cli="${repo_root}/ops/n2api"
test_root="$(mktemp -d)"
trap 'rm -rf -- "${test_root}"' EXIT

fail() {
  printf 'ops_test_status=failed reason=%s\n' "$1" >&2
  exit 1
}

assert_jq() {
  local document=$1 expression=$2
  jq -e "${expression}" <<<"${document}" >/dev/null || fail "jq_assertion_failed"
}

[[ -x "${cli}" ]] || fail "cli_not_executable"

state_dir="${test_root}/state/n2api"
mkdir -p -- "${test_root}/state"
chmod 700 -- "${test_root}/state"

describe="$(${cli} --state-dir "${state_dir}" describe --format json)"
assert_jq "${describe}" '
  .schema_version == "n2api.ops/v1" and
  .command == "describe" and
  .status == "succeeded" and
  .changed == false and
  .current.cli_version == "1.0.0" and
  .current.plan_apply.apply_requires_plan == true and
  .current.plan_apply.live_database_restore_supported == false and
  ([.current.commands[].name] | index("upgrade apply")) != null and
  .current.exit_codes.blocked == 4
'
[[ ! -e "${state_dir}" ]] || fail "describe_created_state"

describe_text="$(${cli} describe)"
grep -Fxq 'command=describe' <<<"${describe_text}" || fail "describe_text_command_missing"
grep -Fxq 'status=succeeded' <<<"${describe_text}" || fail "describe_text_status_missing"

help="$(${cli} --help)"
grep -Fq './ops/n2api describe --format json' <<<"${help}" || fail "help_discovery_missing"

set +e
unknown_output="$(${cli} unknown 2>&1)"
unknown_status=$?
set -e
[[ ${unknown_status} -eq 64 ]] || fail "unknown_command_exit"
grep -Fq 'unknown_command' <<<"${unknown_output}" || fail "unknown_command_reason"

set +e
${cli} describe --format yaml >/dev/null 2>"${test_root}/invalid-format.stderr"
format_status=$?
set -e
[[ ${format_status} -eq 64 ]] || fail "invalid_format_exit"

operations="$(${cli} --state-dir "${state_dir}" operations list --format json)"
assert_jq "${operations}" '
  .command == "operations.list" and
  .status == "succeeded" and
  .reason_code == "operation_state_unavailable" and
  .current.availability == "unavailable" and
  .current.operations == []
'
[[ ! -e "${state_dir}" ]] || fail "operations_list_created_state"

mkdir -p -- "${state_dir}/operations"
chmod 700 -- "${state_dir}" "${state_dir}/operations"
operation_id="op-20260730T000000Z-aabbccddeeff"
jq -cn --arg operation_id "${operation_id}" '{
  schema_version:"n2api.ops/v1",
  operation_id:$operation_id,
  command:"deploy.plan",
  risk:"local_write",
  status:"succeeded",
  changed:false,
  started_at:"2026-07-30T00:00:00Z",
  finished_at:"2026-07-30T00:00:01Z",
  reason_code:"plan_created",
  summary:"Plan created",
  checks:[],current:{},target:{},artifacts:[],next_actions:[]
}' >"${state_dir}/operations/${operation_id}.json"
chmod 600 -- "${state_dir}/operations/${operation_id}.json"

listed="$(${cli} --state-dir "${state_dir}" operations list --format json)"
assert_jq "${listed}" ".current.operations | length == 1 and .[0].operation_id == \"${operation_id}\""

shown="$(${cli} --state-dir "${state_dir}" operations show "${operation_id}" --format json)"
assert_jq "${shown}" ".current.receipt.operation_id == \"${operation_id}\" and .reason_code == \"operation_shown\""

chmod 755 -- "${state_dir}"
set +e
unsafe="$(${cli} --state-dir "${state_dir}" operations list --format json)"
unsafe_status=$?
set -e
[[ ${unsafe_status} -eq 6 ]] || fail "unsafe_state_exit"
assert_jq "${unsafe}" '.reason_code == "unsafe_state_directory" and .status == "failed"'

for schema in envelope plan receipt; do
  jq -e '."$schema" == "https://json-schema.org/draft/2020-12/schema"' \
    "${repo_root}/ops/schemas/${schema}.schema.json" >/dev/null || fail "invalid_${schema}_schema"
done

if rg -n 'N2API_TEST_SECRET_CANARY_DO_NOT_LEAK' "${test_root}" >/dev/null 2>&1; then
  fail "secret_canary_leaked"
fi

printf 'ops_test_status=passed scope=discovery_state_operations\n'

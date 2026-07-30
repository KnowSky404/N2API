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

fake_bin="${repo_root}/ops/tests/fixtures/fake-bin"
fake_path="${fake_bin}:${PATH}"
config_dir="${test_root}/config"
mkdir -p -- "${config_dir}"
chmod 700 -- "${config_dir}"
generated_env="${config_dir}/production.env"
backup_dir="${test_root}/backups"
image="ghcr.io/knowsky404/n2api:2026073001@sha256:$(printf 'a%.0s' {1..64})"

init_output="$(PATH="${fake_path}" "${cli}" \
  --env-file "${generated_env}" \
  config init \
  --public-url https://n2api.example.test \
  --image "${image}" \
  --accepted-risks public-bind,database-plaintext \
  --bind-address 127.0.0.1 \
  --backup-dir "${backup_dir}" \
  --format json)"
assert_jq "${init_output}" '
  .command == "config.init" and
  .status == "succeeded" and
  .changed == true and
  .reason_code == "configuration_initialized"
'
[[ "$(stat -c '%a' -- "${generated_env}")" == "600" ]] || fail "config_init_mode"
grep -Fxq "N2API_IMAGE=${image}" "${generated_env}" || fail "config_init_image"
postgres_secret="$(sed -n 's/^POSTGRES_PASSWORD=//p' "${generated_env}")"
admin_secret="$(sed -n 's/^N2API_ADMIN_PASSWORD=//p' "${generated_env}")"
encryption_secret="$(sed -n 's/^N2API_ENCRYPTION_SECRET=//p' "${generated_env}")"
[[ -n "${postgres_secret}" && -n "${admin_secret}" && -n "${encryption_secret}" ]] || fail "config_init_secret_missing"
[[ "${postgres_secret}" != "${admin_secret}" && "${admin_secret}" != "${encryption_secret}" ]] || fail "config_init_secret_reused"
for secret_value in "${postgres_secret}" "${admin_secret}" "${encryption_secret}"; do
  [[ "${init_output}" != *"${secret_value}"* ]] || fail "config_init_secret_stdout"
done

set +e
PATH="${fake_path}" "${cli}" --env-file "${generated_env}" config init \
  --public-url https://n2api.example.test \
  --image "${image}" \
  --accepted-risks public-bind,database-plaintext \
  --bind-address 127.0.0.1 \
  --backup-dir "${backup_dir}" \
  --format json >"${test_root}/init-existing.stdout"
existing_status=$?
set -e
[[ ${existing_status} -eq 6 ]] || fail "config_init_overwrite_exit"
jq -e '.reason_code == "env_file_exists"' "${test_root}/init-existing.stdout" >/dev/null || fail "config_init_overwrite_reason"

validate_output="$(PATH="${fake_path}" "${cli}" --env-file "${generated_env}" config validate --format json)"
assert_jq "${validate_output}" '
  .command == "config.validate" and
  .status == "succeeded" and
  .reason_code == "configuration_valid" and
  ([.checks[].name] | index("application.config")) != null
'

doctor_output="$(PATH="${fake_path}" "${cli}" \
  --env-file "${generated_env}" \
  --state-dir "${test_root}/doctor-state/n2api" \
  doctor --format json || true)"
assert_jq "${doctor_output}" '
  (.status == "attention" or .status == "succeeded") and
  ([.checks[] | select(.name == "docker.manifest" and .status == "passed")] | length) == 1 and
  ([.checks[] | select(.name == "image.platform" and .status == "passed")] | length) == 1
'
[[ ! -e "${test_root}/doctor-state/n2api" ]] || fail "doctor_created_state"

inspect_output="$(PATH="${fake_path}" "${cli}" image inspect --image "${image}" --format json)"
assert_jq "${inspect_output}" '
  .status == "succeeded" and
  .current.digest == "sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa" and
  (.current.platforms | index("linux/amd64")) != null and
  (.current.platforms | index("linux/arm64")) != null
'

resolve_output="$(PATH="${fake_path}" "${cli}" image resolve --version 2026073001 --format json)"
assert_jq "${resolve_output}" ".status == \"succeeded\" and .current.image == \"${image}\""

set +e
moving_output="$(PATH="${fake_path}" "${cli}" image inspect \
  --image "ghcr.io/knowsky404/n2api:latest@sha256:$(printf 'a%.0s' {1..64})" \
  --format json)"
moving_status=$?
set -e
[[ ${moving_status} -eq 4 ]] || fail "moving_tag_exit"
assert_jq "${moving_output}" '.reason_code == "immutable_image_required"'

chmod 644 -- "${generated_env}"
set +e
unsafe_config="$(PATH="${fake_path}" "${cli}" --env-file "${generated_env}" config validate --format json)"
unsafe_config_status=$?
set -e
[[ ${unsafe_config_status} -eq 4 ]] || fail "unsafe_env_config_exit"
assert_jq "${unsafe_config}" '
  .reason_code == "configuration_invalid" and
  ([.checks[] | select(.reason_code == "env_file_permissions_unsafe")] | length) == 1
'

chmod 600 -- "${generated_env}"
runtime_state="${test_root}/runtime-state/n2api"
mkdir -p -- "${runtime_state}/operations" "${runtime_state}/locks"
chmod 700 -- "${test_root}/runtime-state" "${runtime_state}" "${runtime_state}/operations" "${runtime_state}/locks"

status_output="$(PATH="${fake_path}" "${cli}" \
  --env-file "${generated_env}" \
  --state-dir "${runtime_state}" \
  status --format json)"
assert_jq "${status_output}" '
  .status == "succeeded" and
  .reason_code == "status_available" and
  .current.n2api.running == true and
  .current.n2api.health == "healthy" and
  .current.postgres.health == "healthy" and
  .current.postgres.schema.value == 50 and
  .current.probes.livez.status == "passed" and
  .current.probes.readyz.status == "passed" and
  .current.probes.version.status == "passed" and
  .current.backup.availability == "unavailable" and
  .current.restore_drill.availability == "unavailable"
'

basic_output="$(PATH="${fake_path}" "${cli}" \
  --env-file "${generated_env}" \
  --state-dir "${runtime_state}" \
  verify --level basic --format json)"
assert_jq "${basic_output}" '
  .status == "succeeded" and
  .reason_code == "basic_verification_passed" and
  ([.checks[] | select(.status == "failed")] | length) == 0 and
  ([.checks[] | select(.name == "runtime.security" and .status == "passed")] | length) == 1
'

PATH="${fake_path}" "${cli}" --env-file "${generated_env}" \
  logs n2api --tail 10 --since 5m --format json \
  >"${test_root}/logs.stdout" 2>"${test_root}/logs.stderr"
jq -e '.reason_code == "logs_emitted" and .current.tail == 10' "${test_root}/logs.stdout" >/dev/null || fail "logs_json_envelope"
rg -q 'password=\[REDACTED\]' "${test_root}/logs.stderr" || fail "logs_redaction_missing"
if rg -q 'do-not-retain' "${test_root}/logs.stderr"; then
  fail "logs_secret_retained"
fi

set +e
PATH="${fake_path}" "${cli}" --env-file "${generated_env}" verify --level gateway \
  --api-key-file "${generated_env}" --model gpt-test --format json \
  >"${test_root}/gateway-no-consent.stdout" 2>"${test_root}/gateway-no-consent.stderr"
gateway_consent_status=$?
set -e
[[ ${gateway_consent_status} -eq 64 ]] || fail "gateway_consent_exit"
rg -q 'gateway_verify_requires_upstream_consent' "${test_root}/gateway-no-consent.stderr" || fail "gateway_consent_reason"

for retained in "${test_root}"/*.stdout "${test_root}"/*.stderr; do
  [[ -e "${retained}" ]] || continue
  if rg -n --fixed-strings "${postgres_secret}" "${retained}" >/dev/null 2>&1 ||
    rg -n --fixed-strings "${admin_secret}" "${retained}" >/dev/null 2>&1 ||
    rg -n --fixed-strings "${encryption_secret}" "${retained}" >/dev/null 2>&1; then
    fail "generated_secret_retained"
  fi
done

printf 'ops_test_status=passed scope=discovery_state_operations_host_config_image_runtime\n'

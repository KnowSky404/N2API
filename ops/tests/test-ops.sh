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

sign_json_file() {
  local file=$1 key_file=$2 canonical integrity_hmac tmp
  canonical="$(jq -ceS 'del(.integrity_hmac)' "${file}")"
  integrity_hmac="$(printf '%s' "${canonical}" |
    openssl dgst -sha256 -mac HMAC -macopt "hexkey:$(tr -d '\r\n' <"${key_file}")" |
    awk '{print $NF}')"
  tmp="${file}.signed"
  jq --arg integrity_hmac "${integrity_hmac}" '.integrity_hmac = $integrity_hmac' "${file}" >"${tmp}"
  mv -- "${tmp}" "${file}"
  chmod 600 -- "${file}"
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
  (.current.required_tools | index("setsid")) != null and
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

for schema in envelope plan receipt backup restore; do
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

deploy_state="${test_root}/deploy-state/n2api"
deploy_stack_file="${test_root}/deploy-stack.running"
deploy_docker_log="${test_root}/deploy-docker.log"
deploy_plan_output="$(N2API_FAKE_DOCKER_MODE=deploy_stateful \
  N2API_FAKE_DOCKER_STACK_FILE="${deploy_stack_file}" \
  N2API_FAKE_DOCKER_LOG="${deploy_docker_log}" \
  PATH="${fake_path}" "${cli}" --env-file "${generated_env}" --state-dir "${deploy_state}" \
  deploy plan --format json)"
assert_jq "${deploy_plan_output}" '
  .command == "deploy.plan" and
  .status == "succeeded" and
  .reason_code == "deploy_plan_created" and
  .changed == true
'
deploy_plan_path="$(jq -r '.artifacts[] | select(.type == "operation_plan") | .path' <<<"${deploy_plan_output}")"
deploy_plan_id="$(jq -r '.current.plan_operation_id' <<<"${deploy_plan_output}")"
[[ -f "${deploy_plan_path}" && "$(stat -c '%a' -- "${deploy_plan_path}")" == 600 ]] || fail "deploy_plan_file_mode"
jq -e '
  .schema_version == "n2api.ops.plan/v1" and
  .operation == "deploy" and
  .source.runtime.services_count == 0 and
  .source.runtime.postgres_volume_exists == false and
  .blocked_reasons == [] and
  (.integrity_hmac | test("^[0-9a-f]{64}$"))
' "${deploy_plan_path}" >/dev/null || fail "deploy_plan_contract"
if rg -q '(^| )pull( |$)|(^| )up( |$)' "${deploy_docker_log}"; then
  fail "deploy_plan_mutated_docker_state"
fi
deploy_plan_receipt="${deploy_state}/operations/${deploy_plan_id}.json"
[[ -f "${deploy_plan_receipt}" && "$(stat -c '%a' -- "${deploy_plan_receipt}")" == 600 ]] || fail "deploy_plan_receipt_mode"

tampered_plan="${test_root}/tampered-plan.json"
jq '.target.image.tag = "tampered"' "${deploy_plan_path}" >"${tampered_plan}"
chmod 600 -- "${tampered_plan}"
set +e
tampered_plan_output="$(N2API_FAKE_DOCKER_MODE=deploy_stateful N2API_FAKE_DOCKER_STACK_FILE="${deploy_stack_file}" \
  PATH="${fake_path}" "${cli}" --env-file "${generated_env}" --state-dir "${deploy_state}" \
  deploy apply --plan "${tampered_plan}" --format json)"
tampered_plan_status=$?
set -e
[[ ${tampered_plan_status} -eq 4 ]] || fail "deploy_tampered_plan_exit"
assert_jq "${tampered_plan_output}" '.status == "blocked" and .reason_code == "plan_integrity_invalid"'

expired_plan="${test_root}/expired-plan.json"
jq '.expires_at = "2000-01-01T00:00:00Z" | del(.integrity_hmac)' "${deploy_plan_path}" >"${expired_plan}"
expired_canonical="$(jq -ceS 'del(.integrity_hmac)' "${expired_plan}")"
expired_hmac="$(printf '%s' "${expired_canonical}" |
  openssl dgst -sha256 -mac HMAC -macopt "hexkey:$(tr -d '\r\n' <"${deploy_state}/keys/integrity.key")" |
  awk '{print $NF}')"
jq --arg integrity_hmac "${expired_hmac}" '. + {integrity_hmac:$integrity_hmac}' "${expired_plan}" >"${expired_plan}.signed"
mv -- "${expired_plan}.signed" "${expired_plan}"
chmod 600 -- "${expired_plan}"
set +e
expired_plan_output="$(N2API_FAKE_DOCKER_MODE=deploy_stateful N2API_FAKE_DOCKER_STACK_FILE="${deploy_stack_file}" \
  PATH="${fake_path}" "${cli}" --env-file "${generated_env}" --state-dir "${deploy_state}" \
  deploy apply --plan "${expired_plan}" --format json)"
expired_plan_status=$?
set -e
[[ ${expired_plan_status} -eq 4 ]] || fail "deploy_expired_plan_exit"
assert_jq "${expired_plan_output}" '.status == "blocked" and .reason_code == "plan_expired"'

cp -- "${generated_env}" "${test_root}/generated-env.before-drift"
printf '# drift\n' >>"${generated_env}"
set +e
drift_plan_output="$(N2API_FAKE_DOCKER_MODE=deploy_stateful N2API_FAKE_DOCKER_STACK_FILE="${deploy_stack_file}" \
  PATH="${fake_path}" "${cli}" --env-file "${generated_env}" --state-dir "${deploy_state}" \
  deploy apply --plan "${deploy_plan_path}" --format json)"
drift_plan_status=$?
set -e
mv -- "${test_root}/generated-env.before-drift" "${generated_env}"
chmod 600 -- "${generated_env}"
[[ ${drift_plan_status} -eq 4 ]] || fail "deploy_env_drift_exit"
assert_jq "${drift_plan_output}" '.status == "blocked" and .reason_code == "stale_plan_detected"'

deploy_apply_output="$(N2API_FAKE_DOCKER_MODE=deploy_stateful \
  N2API_FAKE_DOCKER_STACK_FILE="${deploy_stack_file}" \
  PATH="${fake_path}" "${cli}" --env-file "${generated_env}" --state-dir "${deploy_state}" \
  deploy apply --plan "${deploy_plan_path}" --format json)"
assert_jq "${deploy_apply_output}" '
  .command == "deploy.apply" and
  .status == "succeeded" and
  .changed == true and
  .reason_code == "deploy_applied" and
  .target.running_identity == "verified"
'
[[ -e "${deploy_stack_file}" ]] || fail "deploy_apply_stack_not_created"

deploy_noop_output="$(N2API_FAKE_DOCKER_MODE=deploy_stateful \
  N2API_FAKE_DOCKER_STACK_FILE="${deploy_stack_file}" \
  PATH="${fake_path}" "${cli}" --env-file "${generated_env}" --state-dir "${deploy_state}" \
  deploy apply --plan "${deploy_plan_path}" --format json)"
assert_jq "${deploy_noop_output}" '
  .status == "noop" and .changed == false and .reason_code == "target_already_healthy"
'

set +e
existing_deploy_plan="$(N2API_FAKE_DOCKER_MODE=deploy_stateful \
  N2API_FAKE_DOCKER_STACK_FILE="${deploy_stack_file}" \
  PATH="${fake_path}" "${cli}" --env-file "${generated_env}" --state-dir "${test_root}/existing-deploy-state/n2api" \
  deploy plan --format json)"
existing_deploy_plan_status=$?
set -e
[[ ${existing_deploy_plan_status} -eq 4 ]] || fail "existing_deploy_plan_exit"
assert_jq "${existing_deploy_plan}" '
  .status == "blocked" and .reason_code == "deploy_plan_blocked"
'
existing_deploy_plan_path="$(jq -r '.artifacts[0].path' <<<"${existing_deploy_plan}")"
jq -e '
  (.blocked_reasons | index("first_deploy_service_exists")) != null and
  (.blocked_reasons | index("first_deploy_volume_exists")) != null
' "${existing_deploy_plan_path}" >/dev/null || fail "existing_deploy_blockers"

failed_deploy_state="${test_root}/failed-deploy-state/n2api"
failed_deploy_stack="${test_root}/failed-deploy-stack.running"
failed_deploy_plan_output="$(N2API_FAKE_DOCKER_MODE=deploy_stateful N2API_FAKE_DOCKER_STACK_FILE="${failed_deploy_stack}" \
  PATH="${fake_path}" "${cli}" --env-file "${generated_env}" --state-dir "${failed_deploy_state}" deploy plan --format json)"
failed_deploy_plan_path="$(jq -r '.artifacts[0].path' <<<"${failed_deploy_plan_output}")"
set +e
failed_deploy_output="$(N2API_FAKE_DOCKER_MODE=deploy_apply_fail N2API_FAKE_DOCKER_STACK_FILE="${failed_deploy_stack}" \
  PATH="${fake_path}" "${cli}" --env-file "${generated_env}" --state-dir "${failed_deploy_state}" \
  deploy apply --plan "${failed_deploy_plan_path}" --format json)"
failed_deploy_status=$?
set -e
[[ ${failed_deploy_status} -eq 6 ]] || fail "failed_deploy_exit"
assert_jq "${failed_deploy_output}" '.status == "failed" and .reason_code == "compose_apply_failed"'
[[ ! -e "${failed_deploy_stack}" ]] || fail "failed_deploy_automatic_recovery"

set +e
timeout_deploy_output="$(N2API_FAKE_DOCKER_MODE=deploy_apply_wait N2API_FAKE_DOCKER_STACK_FILE="${failed_deploy_stack}" \
  PATH="${fake_path}" "${cli}" --env-file "${generated_env}" --state-dir "${failed_deploy_state}" --timeout 1 \
  deploy apply --plan "${failed_deploy_plan_path}" --format json)"
timeout_deploy_status=$?
set -e
[[ ${timeout_deploy_status} -eq 124 ]] || fail "timeout_deploy_exit"
assert_jq "${timeout_deploy_output}" '.status == "failed" and .reason_code == "readiness_timeout"'
(
  exec 8>"${failed_deploy_state}/locks/operator.lock"
  flock --nonblock 8
) || fail "timeout_deploy_lock_retained"

signal_deploy_state="${test_root}/signal-deploy-state/n2api"
signal_deploy_stack="${test_root}/signal-deploy-stack.running"
signal_deploy_ready="${test_root}/signal-deploy.ready"
signal_deploy_plan_output="$(N2API_FAKE_DOCKER_MODE=deploy_stateful N2API_FAKE_DOCKER_STACK_FILE="${signal_deploy_stack}" \
  PATH="${fake_path}" "${cli}" --env-file "${generated_env}" --state-dir "${signal_deploy_state}" deploy plan --format json)"
signal_deploy_plan_path="$(jq -r '.artifacts[0].path' <<<"${signal_deploy_plan_output}")"

deploy_lock_ready="${test_root}/deploy-lock.ready"
(
  exec 8>"${signal_deploy_state}/locks/operator.lock"
  flock 8
  touch "${deploy_lock_ready}"
  sleep 30
) &
deploy_lock_pid=$!
for _ in {1..100}; do
  [[ -e "${deploy_lock_ready}" ]] && break
  sleep 0.05
done
[[ -e "${deploy_lock_ready}" ]] || fail "deploy_lock_not_ready"
set +e
contended_deploy_output="$(N2API_FAKE_DOCKER_MODE=deploy_stateful N2API_FAKE_DOCKER_STACK_FILE="${signal_deploy_stack}" \
  PATH="${fake_path}" "${cli}" --env-file "${generated_env}" --state-dir "${signal_deploy_state}" \
  deploy apply --plan "${signal_deploy_plan_path}" --format json)"
contended_deploy_status=$?
set -e
kill -TERM "${deploy_lock_pid}" 2>/dev/null || true
wait "${deploy_lock_pid}" 2>/dev/null || true
[[ ${contended_deploy_status} -eq 5 ]] || fail "contended_deploy_exit"
assert_jq "${contended_deploy_output}" '.status == "contended" and .reason_code == "operation_lock_contended"'

env \
  PATH="${fake_path}" \
  N2API_FAKE_DOCKER_MODE=deploy_apply_wait \
  N2API_FAKE_DOCKER_STACK_FILE="${signal_deploy_stack}" \
  N2API_FAKE_DOCKER_READY_FILE="${signal_deploy_ready}" \
  "${cli}" --env-file "${generated_env}" --state-dir "${signal_deploy_state}" --timeout 30 \
  deploy apply --plan "${signal_deploy_plan_path}" --format json \
  >"${test_root}/signal-deploy.stdout" 2>"${test_root}/signal-deploy.stderr" &
signal_deploy_pid=$!
for _ in {1..100}; do
  [[ -e "${signal_deploy_ready}" ]] && break
  sleep 0.05
done
if [[ ! -e "${signal_deploy_ready}" ]]; then
  kill -TERM "${signal_deploy_pid}" 2>/dev/null || true
  wait "${signal_deploy_pid}" 2>/dev/null || true
  fail "signal_deploy_not_ready"
fi
kill -TERM "${signal_deploy_pid}"
set +e
wait "${signal_deploy_pid}"
signal_deploy_status=$?
set -e
[[ ${signal_deploy_status} -eq 143 ]] || fail "signal_deploy_exit"
jq -e '.status == "failed" and .reason_code == "operation_interrupted" and .current.signal == "TERM"' \
  "${test_root}/signal-deploy.stdout" >/dev/null || fail "signal_deploy_receipt"
(
  exec 8>"${signal_deploy_state}/locks/operator.lock"
  flock --nonblock 8
) || fail "signal_deploy_lock_retained"

backup_state="${test_root}/backup-state/n2api"
set +e
backup_output="$(PATH="${fake_path}" "${cli}" \
  --env-file "${generated_env}" \
  --state-dir "${backup_state}" \
  backup create --evidence-class ci_fixture --format json)"
backup_status=$?
set -e
[[ ${backup_status} -eq 3 ]] || fail "backup_create_attention_exit"
assert_jq "${backup_output}" '
  .command == "backup.create" and
  .status == "attention" and
  .changed == true and
  .reason_code == "backup_created_off_host_attention" and
  .current.schema_version == "n2api.ops.backup/v1" and
  .current.source_schema_version == 50 and
  .current.source_image == "'"${image}"'" and
  .current.evidence_class == "ci_fixture" and
  .current.verified == true and
  .current.off_host_status == "attention_missing" and
  (.current.size_bytes > 0) and
  (.current.checksum | test("^[0-9a-f]{64}$")) and
  (.current.integrity_hmac | test("^[0-9a-f]{64}$")) and
  (.current.operation_id | test("^op-"))
'
archive="$(jq -r '.artifacts[] | select(.type == "postgres_custom_archive") | .path' <<<"${backup_output}")"
metadata_file="$(jq -r '.artifacts[] | select(.type == "backup_metadata") | .path' <<<"${backup_output}")"
[[ -s "${archive}" && -s "${metadata_file}" ]] || fail "backup_artifacts_missing"
[[ "$(stat -c '%a' -- "${archive}")" == "600" ]] || fail "backup_archive_mode"
[[ "$(stat -c '%a' -- "${metadata_file}")" == "600" ]] || fail "backup_metadata_mode"
[[ "$(sha256sum -- "${archive}" | awk '{print $1}')" == "$(jq -r '.checksum' "${metadata_file}")" ]] || fail "backup_checksum"
jq -e --arg image "${image}" --arg archive_name "$(basename -- "${archive}")" '
  .schema_version == "n2api.ops.backup/v1" and
  .archive == $archive_name and
  .source_image == $image and
  .source_schema_version == 50 and
  .evidence_class == "ci_fixture" and
  .verified == true and
  .off_host_status == "attention_missing" and
  (.integrity_hmac | test("^[0-9a-f]{64}$"))
' "${metadata_file}" >/dev/null || fail "backup_metadata_contract"
jq -e --slurpfile schema "${repo_root}/ops/schemas/backup.schema.json" \
  '(keys | sort) == ($schema[0].required | sort)' "${metadata_file}" >/dev/null || fail "backup_metadata_schema_shape"

backup_list="$(PATH="${fake_path}" "${cli}" --env-file "${generated_env}" backup list --format json)"
assert_jq "${backup_list}" '
  .command == "backup.list" and
  .status == "succeeded" and
  (.current.backups | length) == 1 and
  .current.backups[0].verified == true and
  .current.backups[0].evidence_class == "ci_fixture"
'

backup_verify="$(PATH="${fake_path}" "${cli}" --env-file "${generated_env}" --state-dir "${backup_state}" \
  backup verify --archive "${archive}" --format json)"
assert_jq "${backup_verify}" '
  .command == "backup.verify" and
  .status == "succeeded" and
  .current.archive_list_status == "passed" and
  .current.metadata_checksum_status == "matched" and
  .current.metadata_integrity_status == "matched" and
  .current.restore_proven == false
'

backup_verify_fallback="$(N2API_FAKE_DOCKER_MODE=no_postgres PATH="${fake_path}" "${cli}" \
  --env-file "${generated_env}" --state-dir "${backup_state}" \
  backup verify --archive "${archive}" --format json)"
assert_jq "${backup_verify_fallback}" '.status == "succeeded" and .current.archive_list_status == "passed"'

tampered_archive="${backup_dir}/n2api-20260730T000000Z-ffffffffffff.dump"
tampered_metadata="${backup_dir}/n2api-20260730T000000Z-ffffffffffff.metadata.json"
cp -- "${archive}" "${tampered_archive}"
chmod 600 -- "${tampered_archive}"
jq '.archive = "n2api-20260730T000000Z-ffffffffffff.dump" | .checksum = "ffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffff"' \
  "${metadata_file}" >"${tampered_metadata}"
chmod 600 -- "${tampered_metadata}"
set +e
tampered_output="$(PATH="${fake_path}" "${cli}" --env-file "${generated_env}" --state-dir "${backup_state}" \
  backup verify --archive "${tampered_archive}" --format json)"
tampered_status=$?
set -e
[[ ${tampered_status} -eq 4 ]] || fail "backup_checksum_mismatch_exit"
assert_jq "${tampered_output}" '.reason_code == "backup_checksum_mismatch" and .status == "blocked"'

forged_archive="${backup_dir}/n2api-20260730T000001Z-eeeeeeeeeeee.dump"
forged_metadata="${backup_dir}/n2api-20260730T000001Z-eeeeeeeeeeee.metadata.json"
cp -- "${archive}" "${forged_archive}"
chmod 600 -- "${forged_archive}"
jq '.archive = "n2api-20260730T000001Z-eeeeeeeeeeee.dump" | .off_host_status = "recorded"' \
  "${metadata_file}" >"${forged_metadata}"
chmod 600 -- "${forged_metadata}"
set +e
forged_output="$(PATH="${fake_path}" "${cli}" --env-file "${generated_env}" --state-dir "${backup_state}" \
  backup verify --archive "${forged_archive}" --format json)"
forged_status=$?
set -e
[[ ${forged_status} -eq 4 ]] || fail "backup_off_host_forgery_exit"
assert_jq "${forged_output}" '.reason_code == "backup_metadata_invalid" and .status == "blocked"'

dump_count_before="$(find "${backup_dir}" -maxdepth 1 -type f -name 'n2api-*.dump' | wc -l | tr -d ' ')"
metadata_count_before="$(find "${backup_dir}" -maxdepth 1 -type f -name 'n2api-*.metadata.json' | wc -l | tr -d ' ')"
set +e
backup_failure="$(N2API_FAKE_DOCKER_MODE=pg_dump_fail PATH="${fake_path}" "${cli}" \
  --env-file "${generated_env}" \
  --state-dir "${test_root}/backup-failure-state/n2api" \
  backup create --evidence-class ci_fixture --format json)"
backup_failure_status=$?
set -e
[[ ${backup_failure_status} -eq 6 ]] || fail "backup_failure_exit"
assert_jq "${backup_failure}" '.status == "failed" and .reason_code == "backup_dump_failed" and .current.stage == "dump"'
[[ "$(find "${backup_dir}" -maxdepth 1 -type f -name 'n2api-*.dump' | wc -l | tr -d ' ')" == "${dump_count_before}" ]] || fail "backup_failure_archive_published"
[[ "$(find "${backup_dir}" -maxdepth 1 -type f -name 'n2api-*.metadata.json' | wc -l | tr -d ' ')" == "${metadata_count_before}" ]] || fail "backup_failure_metadata_published"
[[ -z "$(find "${backup_dir}" -maxdepth 1 -type f -name '.n2api-*.tmp' -print -quit)" ]] || fail "backup_failure_temp_retained"

set +e
backup_publish_failure="$(N2API_FAKE_MV_MODE=backup_metadata_publish_fail PATH="${fake_path}" "${cli}" \
  --env-file "${generated_env}" \
  --state-dir "${test_root}/backup-publish-failure-state/n2api" \
  backup create --evidence-class ci_fixture --format json)"
backup_publish_failure_status=$?
set -e
[[ ${backup_publish_failure_status} -eq 6 ]] || fail "backup_publish_failure_exit"
assert_jq "${backup_publish_failure}" '
  .status == "failed" and
  .reason_code == "backup_metadata_publish_failed" and
  .current.stage == "publish"
'
[[ "$(find "${backup_dir}" -maxdepth 1 -type f -name 'n2api-*.dump' | wc -l | tr -d ' ')" == "${dump_count_before}" ]] || fail "backup_publish_failure_archive_retained"
[[ "$(find "${backup_dir}" -maxdepth 1 -type f -name 'n2api-*.metadata.json' | wc -l | tr -d ' ')" == "${metadata_count_before}" ]] || fail "backup_publish_failure_metadata_retained"
[[ -z "$(find "${backup_dir}" -maxdepth 1 -type f -name '.n2api-*.tmp' -print -quit)" ]] || fail "backup_publish_failure_temp_retained"

signal_state="${test_root}/backup-signal-state/n2api"
signal_ready="${test_root}/backup-signal.ready"
signal_release="${test_root}/backup-signal.release"
env \
  PATH="${fake_path}" \
  N2API_FAKE_DOCKER_MODE=pg_dump_wait \
  N2API_FAKE_DOCKER_READY_FILE="${signal_ready}" \
  N2API_FAKE_DOCKER_RELEASE_FILE="${signal_release}" \
  "${cli}" --env-file "${generated_env}" --state-dir "${signal_state}" \
    backup create --evidence-class ci_fixture --format json \
    >"${test_root}/backup-signal.stdout" 2>"${test_root}/backup-signal.stderr" &
signal_pid=$!
for _ in {1..100}; do
  [[ -e "${signal_ready}" ]] && break
  sleep 0.05
done
if [[ ! -e "${signal_ready}" ]]; then
  kill -TERM "${signal_pid}" 2>/dev/null || true
  touch "${signal_release}"
  wait "${signal_pid}" 2>/dev/null || true
  fail "backup_signal_not_ready"
fi
kill -TERM "${signal_pid}"
set +e
wait "${signal_pid}"
signal_status=$?
set -e
[[ ${signal_status} -eq 143 ]] || fail "backup_signal_exit"
jq -e '.status == "failed" and .reason_code == "backup_interrupted" and .current.signal == "TERM"' \
  "${test_root}/backup-signal.stdout" >/dev/null || fail "backup_signal_receipt"
[[ -z "$(find "${backup_dir}" -maxdepth 1 -type f -name '.n2api-*.tmp' -print -quit)" ]] || fail "backup_signal_temp_retained"
[[ "$(find "${backup_dir}" -maxdepth 1 -type f -name 'n2api-*.dump' | wc -l | tr -d ' ')" == "${dump_count_before}" ]] || fail "backup_signal_archive_published"

double_signal_state="${test_root}/backup-double-signal-state/n2api"
double_signal_ready="${test_root}/backup-double-signal.ready"
env \
  PATH="${fake_path}" \
  N2API_FAKE_DOCKER_MODE=pg_dump_ignore_term \
  N2API_FAKE_DOCKER_READY_FILE="${double_signal_ready}" \
  "${cli}" --env-file "${generated_env}" --state-dir "${double_signal_state}" --timeout 1 \
    backup create --evidence-class ci_fixture --format json \
    >"${test_root}/backup-double-signal.stdout" 2>"${test_root}/backup-double-signal.stderr" &
double_signal_pid=$!
for _ in {1..100}; do
  [[ -e "${double_signal_ready}" ]] && break
  sleep 0.05
done
[[ -e "${double_signal_ready}" ]] || fail "backup_double_signal_not_ready"
kill -TERM "${double_signal_pid}"
sleep 0.05
kill -TERM "${double_signal_pid}" 2>/dev/null || true
set +e
wait "${double_signal_pid}"
double_signal_status=$?
set -e
[[ ${double_signal_status} -eq 143 ]] || fail "backup_double_signal_exit"
[[ -z "$(find "${backup_dir}" -maxdepth 1 -type f -name '.n2api-*.tmp' -print -quit)" ]] || fail "backup_double_signal_temp_retained"
(
  exec 8>"${double_signal_state}/locks/operator.lock"
  flock --nonblock 8
) || fail "backup_double_signal_lock_retained"

set +e
backup_timeout="$(N2API_FAKE_DOCKER_MODE=pg_dump_wait N2API_FAKE_DOCKER_RELEASE_FILE="${test_root}/never-release" \
  PATH="${fake_path}" "${cli}" --env-file "${generated_env}" \
  --state-dir "${test_root}/backup-timeout-state/n2api" --timeout 1 \
  backup create --evidence-class ci_fixture --format json)"
backup_timeout_status=$?
set -e
[[ ${backup_timeout_status} -eq 124 ]] || fail "backup_timeout_exit"
assert_jq "${backup_timeout}" '.status == "failed" and .reason_code == "backup_dump_timeout" and .current.stage == "dump"'

set +e
backup_kill_timeout="$(N2API_FAKE_DOCKER_MODE=pg_dump_ignore_term PATH="${fake_path}" "${cli}" \
  --env-file "${generated_env}" --state-dir "${test_root}/backup-kill-timeout-state/n2api" --timeout 1 \
  backup create --evidence-class ci_fixture --format json)"
backup_kill_timeout_status=$?
set -e
[[ ${backup_kill_timeout_status} -eq 124 ]] || fail "backup_kill_timeout_exit"
assert_jq "${backup_kill_timeout}" '.status == "failed" and .reason_code == "backup_dump_timeout"'

set +e
backup_list_timeout="$(N2API_FAKE_DOCKER_MODE=archive_list_wait PATH="${fake_path}" "${cli}" \
  --env-file "${generated_env}" --state-dir "${test_root}/backup-list-timeout-state/n2api" --timeout 1 \
  backup create --evidence-class ci_fixture --format json)"
backup_list_timeout_status=$?
set -e
[[ ${backup_list_timeout_status} -eq 124 ]] || fail "backup_list_timeout_exit"
assert_jq "${backup_list_timeout}" '.status == "failed" and .reason_code == "backup_archive_list_timeout" and .current.stage == "archive_list"'

lock_ready="${test_root}/backup-lock.ready"
(
  exec 8>"${backup_state}/locks/operator.lock"
  flock 8
  touch "${lock_ready}"
  sleep 30
) &
lock_pid=$!
for _ in {1..100}; do
  [[ -e "${lock_ready}" ]] && break
  sleep 0.05
done
[[ -e "${lock_ready}" ]] || fail "backup_lock_not_ready"
set +e
contended_output="$(PATH="${fake_path}" "${cli}" --env-file "${generated_env}" --state-dir "${backup_state}" \
  backup create --evidence-class ci_fixture --format json)"
contended_status=$?
set -e
kill -TERM "${lock_pid}" 2>/dev/null || true
wait "${lock_pid}" 2>/dev/null || true
[[ ${contended_status} -eq 5 ]] || fail "backup_lock_contended_exit"
assert_jq "${contended_output}" '.status == "contended" and .reason_code == "operation_lock_contended"'

admin_secret_file="${test_root}/restore-admin.secret"
encryption_secret_file="${test_root}/restore-encryption.secret"
printf '%s\n' 'N2API_TEST_SECRET_CANARY_DO_NOT_LEAK_ADMIN' >"${admin_secret_file}"
printf '%s\n' 'N2API_TEST_SECRET_CANARY_DO_NOT_LEAK_ENCRYPTION' >"${encryption_secret_file}"
chmod 600 -- "${admin_secret_file}" "${encryption_secret_file}"
fixture_archive="${test_root}/fixture.dump"
cp -- "${archive}" "${fixture_archive}"
chmod 600 -- "${fixture_archive}"

fixture_restore_state="${test_root}/fixture-restore-state/n2api"
fixture_restore="$(PATH="${fake_path}" "${cli}" --env-file "${generated_env}" --state-dir "${fixture_restore_state}" \
  restore drill \
  --archive "${fixture_archive}" \
  --image "${image}" \
  --evidence-class ci_fixture \
  --admin-password-file "${admin_secret_file}" \
  --encryption-secret-file "${encryption_secret_file}" \
  --format json)"
assert_jq "${fixture_restore}" '
  .status == "succeeded" and
  .reason_code == "restore_drill_passed" and
  .current.schema_version == "n2api.ops.restore/v1" and
  .current.evidence_class == "ci_fixture" and
  (.current.backup_id | startswith("fixture-")) and
  .current.cleanup_status == "passed" and
  .current.schema_version_value == 50
'
jq -e --slurpfile schema "${repo_root}/ops/schemas/restore.schema.json" \
  '(.current | keys | sort) == ($schema[0].required | sort)' <<<"${fixture_restore}" >/dev/null || fail "restore_evidence_schema_shape"
[[ -z "$(find "${fixture_restore_state}/restore-runtime/active" -type f -print -quit 2>/dev/null)" ]] || fail "fixture_restore_active_marker_retained"

set +e
real_backup_output="$(PATH="${fake_path}" "${cli}" --env-file "${generated_env}" --state-dir "${backup_state}" \
  backup create --evidence-class real_operator --format json)"
real_backup_status=$?
set -e
[[ ${real_backup_status} -eq 3 ]] || fail "real_backup_attention_exit"
real_archive="$(jq -r '.artifacts[] | select(.type == "postgres_custom_archive") | .path' <<<"${real_backup_output}")"
real_metadata="$(jq -r '.artifacts[] | select(.type == "backup_metadata") | .path' <<<"${real_backup_output}")"
real_backup_id="$(jq -r '.current.backup_id' <<<"${real_backup_output}")"
candidate_image="ghcr.io/knowsky404/n2api:2026073002@sha256:$(printf 'b%.0s' {1..64})"
source_env="${test_root}/upgrade-source.env"
cp -- "${generated_env}" "${source_env}"
chmod 600 -- "${source_env}"

set +e
missing_evidence_plan="$(PATH="${fake_path}" "${cli}" --env-file "${generated_env}" --state-dir "${backup_state}" \
  upgrade plan --image "${candidate_image}" --format json)"
missing_evidence_status=$?
set -e
[[ ${missing_evidence_status} -eq 4 ]] || fail "missing_upgrade_evidence_exit"
missing_evidence_path="$(jq -r '.artifacts[0].path' <<<"${missing_evidence_plan}")"
jq -e '
  (.blocked_reasons | index("current_restore_missing")) != null and
  (.blocked_reasons | index("candidate_restore_missing")) != null
' "${missing_evidence_path}" >/dev/null || fail "missing_upgrade_evidence_blockers"

fixture_current_restore="$(PATH="${fake_path}" "${cli}" --env-file "${generated_env}" --state-dir "${backup_state}" \
  restore drill --archive "${real_archive}" --image "${image}" --evidence-class ci_fixture \
  --admin-password-file "${admin_secret_file}" --encryption-secret-file "${encryption_secret_file}" --format json)"
fixture_candidate_restore="$(PATH="${fake_path}" "${cli}" --env-file "${generated_env}" --state-dir "${backup_state}" \
  restore drill --archive "${real_archive}" --image "${candidate_image}" --evidence-class ci_fixture \
  --admin-password-file "${admin_secret_file}" --encryption-secret-file "${encryption_secret_file}" --format json)"
assert_jq "${fixture_current_restore}" '.status == "succeeded" and .current.evidence_class == "ci_fixture"'
assert_jq "${fixture_candidate_restore}" '.status == "succeeded" and .current.evidence_class == "ci_fixture"'
set +e
fixture_upgrade_plan="$(PATH="${fake_path}" "${cli}" --env-file "${generated_env}" --state-dir "${backup_state}" \
  upgrade plan --image "${candidate_image}" --format json)"
fixture_upgrade_status=$?
set -e
[[ ${fixture_upgrade_status} -eq 4 ]] || fail "fixture_upgrade_evidence_exit"
fixture_upgrade_path="$(jq -r '.artifacts[0].path' <<<"${fixture_upgrade_plan}")"
jq -e '
  (.blocked_reasons | index("current_restore_missing")) != null and
  (.blocked_reasons | index("candidate_restore_missing")) != null
' "${fixture_upgrade_path}" >/dev/null || fail "fixture_upgrade_evidence_promoted"

current_real_restore="$(PATH="${fake_path}" "${cli}" --env-file "${generated_env}" --state-dir "${backup_state}" \
  restore drill --archive "${real_archive}" --image "${image}" --evidence-class real_operator \
  --admin-password-file "${admin_secret_file}" --encryption-secret-file "${encryption_secret_file}" --format json)"
assert_jq "${current_real_restore}" '
  .status == "succeeded" and .current.evidence_class == "real_operator" and .current.backup_id == "'"${real_backup_id}"'"
'
candidate_real_restore="$(PATH="${fake_path}" "${cli}" --env-file "${generated_env}" --state-dir "${backup_state}" \
  restore drill --archive "${real_archive}" --image "${candidate_image}" --evidence-class real_operator \
  --admin-password-file "${admin_secret_file}" --encryption-secret-file "${encryption_secret_file}" --format json)"
assert_jq "${candidate_real_restore}" '
  .status == "succeeded" and .current.evidence_class == "real_operator" and .target.image == "'"${candidate_image}"'"
'
evidence_key="${backup_state}/keys/integrity.key"
current_restore_receipt="${backup_state}/operations/$(jq -r '.operation_id' <<<"${current_real_restore}").json"
candidate_restore_receipt="${backup_state}/operations/$(jq -r '.operation_id' <<<"${candidate_real_restore}").json"

cp -- "${real_metadata}" "${test_root}/real-metadata.valid"
jq --arg created_at "$(date -u -d '+10 minutes' +'%Y-%m-%dT%H:%M:%SZ')" \
  '.created_at = $created_at' "${real_metadata}" >"${real_metadata}.future"
mv -- "${real_metadata}.future" "${real_metadata}"
sign_json_file "${real_metadata}" "${evidence_key}"
set +e
future_backup_plan="$(PATH="${fake_path}" "${cli}" --env-file "${generated_env}" --state-dir "${backup_state}" \
  upgrade plan --image "${candidate_image}" --format json)"
future_backup_status=$?
set -e
[[ ${future_backup_status} -eq 4 ]] || fail "future_backup_upgrade_exit"
future_backup_path="$(jq -r '.artifacts[0].path' <<<"${future_backup_plan}")"
jq -e '(.blocked_reasons | index("backup_missing")) != null' "${future_backup_path}" >/dev/null ||
  fail "future_backup_upgrade_not_blocked"

jq --arg created_at "$(date -u -d '-2 days' +'%Y-%m-%dT%H:%M:%SZ')" \
  '.created_at = $created_at' "${real_metadata}" >"${real_metadata}.stale"
mv -- "${real_metadata}.stale" "${real_metadata}"
sign_json_file "${real_metadata}" "${evidence_key}"
set +e
stale_backup_plan="$(PATH="${fake_path}" "${cli}" --env-file "${generated_env}" --state-dir "${backup_state}" \
  upgrade plan --image "${candidate_image}" --format json)"
stale_backup_status=$?
set -e
[[ ${stale_backup_status} -eq 4 ]] || fail "stale_backup_upgrade_exit"
stale_backup_path="$(jq -r '.artifacts[0].path' <<<"${stale_backup_plan}")"
jq -e '(.blocked_reasons | index("backup_missing")) != null' "${stale_backup_path}" >/dev/null ||
  fail "stale_backup_upgrade_not_blocked"
mv -- "${test_root}/real-metadata.valid" "${real_metadata}"
chmod 600 -- "${real_metadata}"

cp -- "${current_restore_receipt}" "${test_root}/current-restore.valid"
jq --arg finished_at "$(date -u -d '+10 minutes' +'%Y-%m-%dT%H:%M:%SZ')" \
  '.finished_at = $finished_at' "${current_restore_receipt}" >"${current_restore_receipt}.future"
mv -- "${current_restore_receipt}.future" "${current_restore_receipt}"
sign_json_file "${current_restore_receipt}" "${evidence_key}"
set +e
future_restore_plan="$(PATH="${fake_path}" "${cli}" --env-file "${generated_env}" --state-dir "${backup_state}" \
  upgrade plan --image "${candidate_image}" --format json)"
future_restore_status=$?
set -e
[[ ${future_restore_status} -eq 4 ]] || fail "future_restore_upgrade_exit"
future_restore_path="$(jq -r '.artifacts[0].path' <<<"${future_restore_plan}")"
jq -e '(.blocked_reasons | index("current_restore_missing")) != null' "${future_restore_path}" >/dev/null ||
  fail "future_restore_upgrade_not_blocked"
mv -- "${test_root}/current-restore.valid" "${current_restore_receipt}"
chmod 600 -- "${current_restore_receipt}"

cp -- "${candidate_restore_receipt}" "${test_root}/candidate-restore.valid"
jq --arg finished_at "$(date -u -d '-2 days' +'%Y-%m-%dT%H:%M:%SZ')" \
  '.finished_at = $finished_at' "${candidate_restore_receipt}" >"${candidate_restore_receipt}.stale"
mv -- "${candidate_restore_receipt}.stale" "${candidate_restore_receipt}"
sign_json_file "${candidate_restore_receipt}" "${evidence_key}"
set +e
stale_restore_plan="$(PATH="${fake_path}" "${cli}" --env-file "${generated_env}" --state-dir "${backup_state}" \
  upgrade plan --image "${candidate_image}" --format json)"
stale_restore_status=$?
set -e
[[ ${stale_restore_status} -eq 4 ]] || fail "stale_restore_upgrade_exit"
stale_restore_path="$(jq -r '.artifacts[0].path' <<<"${stale_restore_plan}")"
jq -e '(.blocked_reasons | index("candidate_restore_missing")) != null' "${stale_restore_path}" >/dev/null ||
  fail "stale_restore_upgrade_not_blocked"
mv -- "${test_root}/candidate-restore.valid" "${candidate_restore_receipt}"
chmod 600 -- "${candidate_restore_receipt}"

upgrade_docker_log="${test_root}/upgrade-docker.log"
upgrade_plan_output="$(N2API_FAKE_DOCKER_LOG="${upgrade_docker_log}" PATH="${fake_path}" "${cli}" \
  --env-file "${generated_env}" --state-dir "${backup_state}" \
  upgrade plan --image "${candidate_image}" --format json)"
assert_jq "${upgrade_plan_output}" '
  .command == "upgrade.plan" and .status == "succeeded" and .reason_code == "upgrade_plan_created"
'
upgrade_plan_path="$(jq -r '.artifacts[0].path' <<<"${upgrade_plan_output}")"
jq -e --arg backup_id "${real_backup_id}" --arg current_image "${image}" --arg candidate_image "${candidate_image}" '
  .source.runtime.configured_image == $current_image and
  .target.image.reference == $candidate_image and
  .target.rollback_image == $current_image and
  .evidence.backup.backup_id == $backup_id and
  .evidence.backup.evidence_class == "real_operator" and
  .evidence.current_restore.evidence_class == "real_operator" and
  .evidence.candidate_restore.evidence_class == "real_operator" and
  (.invariants.current_restore_hmac | test("^[0-9a-f]{64}$")) and
  (.invariants.candidate_restore_hmac | test("^[0-9a-f]{64}$"))
' "${upgrade_plan_path}" >/dev/null || fail "upgrade_plan_evidence_contract"
rg -q '(^| )pull( |$)' "${upgrade_docker_log}" || fail "upgrade_plan_candidate_not_pulled"
if rg -q '(^| )up( |$)' "${upgrade_docker_log}"; then
  fail "upgrade_plan_recreated_stack"
fi

cp -- "${real_archive}" "${test_root}/real-archive.valid"
tr 'P' 'Q' <"${real_archive}" >"${real_archive}.tampered"
mv -- "${real_archive}.tampered" "${real_archive}"
chmod 600 -- "${real_archive}"
[[ "$(stat -c '%s' -- "${real_archive}")" == "$(jq -r '.size_bytes' "${real_metadata}")" ]] ||
  fail "same_size_archive_fixture_invalid"
set +e
tampered_archive_plan="$(PATH="${fake_path}" "${cli}" --env-file "${generated_env}" --state-dir "${backup_state}" \
  upgrade plan --image "${candidate_image}" --format json)"
tampered_archive_plan_status=$?
tampered_archive_apply_log="${test_root}/tampered-archive-apply.log"
tampered_archive_apply="$(N2API_FAKE_DOCKER_LOG="${tampered_archive_apply_log}" PATH="${fake_path}" \
  "${cli}" --env-file "${generated_env}" --state-dir "${backup_state}" \
  upgrade apply --plan "${upgrade_plan_path}" --format json)"
tampered_archive_apply_status=$?
set -e
mv -- "${test_root}/real-archive.valid" "${real_archive}"
chmod 600 -- "${real_archive}"
[[ ${tampered_archive_plan_status} -eq 4 ]] || fail "tampered_archive_upgrade_plan_exit"
tampered_archive_plan_path="$(jq -r '.artifacts[0].path' <<<"${tampered_archive_plan}")"
jq -e '(.blocked_reasons | index("backup_missing")) != null' "${tampered_archive_plan_path}" >/dev/null ||
  fail "tampered_archive_upgrade_plan_not_blocked"
[[ ${tampered_archive_apply_status} -eq 4 ]] || fail "tampered_archive_upgrade_apply_exit"
assert_jq "${tampered_archive_apply}" '.status == "blocked" and .reason_code == "stale_plan_detected"'
if rg -q '(^| )up( |$)' "${tampered_archive_apply_log}"; then
  fail "tampered_archive_upgrade_mutated_stack"
fi
grep -Fxq "N2API_IMAGE=${image}" "${generated_env}" || fail "tampered_archive_upgrade_mutated_env"

cp -- "${candidate_restore_receipt}" "${test_root}/candidate-restore.before-tamper"
jq '.current.cleanup_status = "failed"' "${candidate_restore_receipt}" >"${candidate_restore_receipt}.tampered"
mv -- "${candidate_restore_receipt}.tampered" "${candidate_restore_receipt}"
chmod 600 -- "${candidate_restore_receipt}"
set +e
tampered_restore_upgrade_output="$(PATH="${fake_path}" "${cli}" --env-file "${generated_env}" --state-dir "${backup_state}" \
  upgrade apply --plan "${upgrade_plan_path}" --format json)"
tampered_restore_upgrade_status=$?
set -e
mv -- "${test_root}/candidate-restore.before-tamper" "${candidate_restore_receipt}"
chmod 600 -- "${candidate_restore_receipt}"
[[ ${tampered_restore_upgrade_status} -eq 4 ]] || fail "tampered_restore_upgrade_exit"
assert_jq "${tampered_restore_upgrade_output}" '.status == "blocked" and .reason_code == "stale_plan_detected"'

cp -- "${real_metadata}" "${test_root}/real-metadata.before-tamper"
jq '.size_bytes += 1' "${real_metadata}" >"${real_metadata}.tampered"
mv -- "${real_metadata}.tampered" "${real_metadata}"
chmod 600 -- "${real_metadata}"
set +e
tampered_upgrade_output="$(PATH="${fake_path}" "${cli}" --env-file "${generated_env}" --state-dir "${backup_state}" \
  upgrade apply --plan "${upgrade_plan_path}" --format json)"
tampered_upgrade_status=$?
set -e
mv -- "${test_root}/real-metadata.before-tamper" "${real_metadata}"
chmod 600 -- "${real_metadata}"
[[ ${tampered_upgrade_status} -eq 4 ]] || fail "tampered_upgrade_evidence_exit"
assert_jq "${tampered_upgrade_output}" '.status == "blocked" and .reason_code == "stale_plan_detected"'

upgrade_lock_ready="${test_root}/upgrade-lock.ready"
(
  exec 8>"${backup_state}/locks/operator.lock"
  flock 8
  touch "${upgrade_lock_ready}"
  sleep 30
) &
upgrade_lock_pid=$!
for _ in {1..100}; do
  [[ -e "${upgrade_lock_ready}" ]] && break
  sleep 0.05
done
[[ -e "${upgrade_lock_ready}" ]] || fail "upgrade_lock_not_ready"
set +e
upgrade_contended_log="${test_root}/upgrade-contended.log"
upgrade_contended_output="$(N2API_FAKE_DOCKER_LOG="${upgrade_contended_log}" PATH="${fake_path}" \
  "${cli}" --env-file "${generated_env}" --state-dir "${backup_state}" \
  upgrade apply --plan "${upgrade_plan_path}" --format json)"
upgrade_contended_status=$?
set -e
kill -TERM "${upgrade_lock_pid}" 2>/dev/null || true
wait "${upgrade_lock_pid}" 2>/dev/null || true
[[ ${upgrade_contended_status} -eq 5 ]] || fail "upgrade_lock_contended_exit"
assert_jq "${upgrade_contended_output}" '.status == "contended" and .reason_code == "operation_lock_contended" and .changed == false'
upgrade_contended_receipt="${backup_state}/operations/$(jq -r '.operation_id' <<<"${upgrade_contended_output}").json"
jq -e '(.integrity_hmac | test("^[0-9a-f]{64}$"))' "${upgrade_contended_receipt}" >/dev/null ||
  fail "upgrade_lock_contended_receipt_unsigned"
if rg -q '(^| )up( |$)' "${upgrade_contended_log}"; then
  fail "upgrade_lock_contended_mutated_stack"
fi

set +e
upgrade_pull_timeout="$(N2API_FAKE_DOCKER_MODE=image_pull_wait PATH="${fake_path}" \
  "${cli}" --env-file "${generated_env}" --state-dir "${backup_state}" --timeout 1 \
  upgrade apply --plan "${upgrade_plan_path}" --format json)"
upgrade_pull_timeout_status=$?
set -e
[[ ${upgrade_pull_timeout_status} -eq 124 ]] || fail "upgrade_pull_timeout_exit"
assert_jq "${upgrade_pull_timeout}" '
  .status == "failed" and .reason_code == "image_pull_timeout" and .changed == false and
  .current.stage == "image_pull" and .current.source_schema == 50
'
grep -Fxq "N2API_IMAGE=${image}" "${generated_env}" || fail "upgrade_pull_timeout_mutated_env"
(
  exec 8>"${backup_state}/locks/operator.lock"
  flock --nonblock 8
) || fail "upgrade_pull_timeout_lock_retained"

upgrade_pull_signal_ready="${test_root}/upgrade-pull-signal.ready"
env \
  PATH="${fake_path}" \
  N2API_FAKE_DOCKER_MODE=image_pull_wait \
  N2API_FAKE_DOCKER_READY_FILE="${upgrade_pull_signal_ready}" \
  "${cli}" --env-file "${generated_env}" --state-dir "${backup_state}" --timeout 30 \
  upgrade apply --plan "${upgrade_plan_path}" --format json \
  >"${test_root}/upgrade-pull-signal.stdout" 2>"${test_root}/upgrade-pull-signal.stderr" &
upgrade_pull_signal_pid=$!
for _ in {1..100}; do
  [[ -e "${upgrade_pull_signal_ready}" ]] && break
  sleep 0.05
done
if [[ ! -e "${upgrade_pull_signal_ready}" ]]; then
  kill -TERM "${upgrade_pull_signal_pid}" 2>/dev/null || true
  wait "${upgrade_pull_signal_pid}" 2>/dev/null || true
  fail "upgrade_pull_signal_not_ready"
fi
kill -TERM "${upgrade_pull_signal_pid}"
set +e
wait "${upgrade_pull_signal_pid}"
upgrade_pull_signal_status=$?
set -e
[[ ${upgrade_pull_signal_status} -eq 143 ]] || fail "upgrade_pull_signal_exit"
jq -e '
  .status == "failed" and .reason_code == "operation_interrupted" and .changed == false and
  .current.stage == "image_pull" and .current.signal == "TERM"
' "${test_root}/upgrade-pull-signal.stdout" >/dev/null || fail "upgrade_pull_signal_receipt"
grep -Fxq "N2API_IMAGE=${image}" "${generated_env}" || fail "upgrade_pull_signal_mutated_env"
(
  exec 8>"${backup_state}/locks/operator.lock"
  flock --nonblock 8
) || fail "upgrade_pull_signal_lock_retained"

set +e
upgrade_env_failure="$(N2API_FAKE_MV_MODE=upgrade_env_publish_fail PATH="${fake_path}" \
  "${cli}" --env-file "${generated_env}" --state-dir "${backup_state}" \
  upgrade apply --plan "${upgrade_plan_path}" --format json)"
upgrade_env_failure_status=$?
set -e
[[ ${upgrade_env_failure_status} -eq 6 ]] || fail "upgrade_env_failure_exit"
assert_jq "${upgrade_env_failure}" '
  .status == "failed" and .reason_code == "environment_update_failed" and .changed == false and
  .current.stage == "persist_target" and .current.source_schema == 50
'
grep -Fxq "N2API_IMAGE=${image}" "${generated_env}" || fail "upgrade_env_failure_mutated_env"

upgrade_stack_file="${test_root}/upgrade-stack.running"
touch "${upgrade_stack_file}"
upgrade_compose_failure_log="${test_root}/upgrade-compose-failure.log"
set +e
upgrade_compose_failure="$(N2API_FAKE_DOCKER_MODE=upgrade_apply_fail \
  N2API_FAKE_DOCKER_STACK_FILE="${upgrade_stack_file}" \
  N2API_FAKE_DOCKER_LOG="${upgrade_compose_failure_log}" \
  PATH="${fake_path}" "${cli}" --env-file "${generated_env}" --state-dir "${backup_state}" \
  upgrade apply --plan "${upgrade_plan_path}" --format json)"
upgrade_compose_failure_status=$?
set -e
[[ ${upgrade_compose_failure_status} -eq 6 ]] || fail "upgrade_compose_failure_exit"
assert_jq "${upgrade_compose_failure}" '
  .status == "failed" and .reason_code == "compose_apply_failed" and .changed == true and
  .current.stage == "compose_apply" and .current.source_schema == 50 and
  .current.observed_schema == 50 and .current.observed_image == "'"${candidate_image}"'"
'
grep -Fxq "N2API_IMAGE=${candidate_image}" "${generated_env}" || fail "upgrade_compose_failure_reverted_env"
if rg -q '(^| )(down|rollback)( |$)|volume rm|down --volumes' "${upgrade_compose_failure_log}"; then
  fail "upgrade_compose_failure_automatic_rollback"
fi
cp -- "${source_env}" "${generated_env}"
chmod 600 -- "${generated_env}"

set +e
upgrade_compose_timeout="$(N2API_FAKE_DOCKER_MODE=upgrade_apply_wait \
  N2API_FAKE_DOCKER_STACK_FILE="${upgrade_stack_file}" \
  PATH="${fake_path}" "${cli}" --env-file "${generated_env}" --state-dir "${backup_state}" --timeout 1 \
  upgrade apply --plan "${upgrade_plan_path}" --format json)"
upgrade_compose_timeout_status=$?
set -e
[[ ${upgrade_compose_timeout_status} -eq 124 ]] || fail "upgrade_compose_timeout_exit"
assert_jq "${upgrade_compose_timeout}" '
  .status == "failed" and .reason_code == "readiness_timeout" and .changed == true and
  .current.stage == "compose_apply" and .current.source_schema == 50 and .current.observed_schema == 50
'
grep -Fxq "N2API_IMAGE=${candidate_image}" "${generated_env}" || fail "upgrade_compose_timeout_reverted_env"
(
  exec 8>"${backup_state}/locks/operator.lock"
  flock --nonblock 8
) || fail "upgrade_compose_timeout_lock_retained"
cp -- "${source_env}" "${generated_env}"
chmod 600 -- "${generated_env}"

upgrade_compose_signal_ready="${test_root}/upgrade-compose-signal.ready"
env \
  PATH="${fake_path}" \
  N2API_FAKE_DOCKER_MODE=upgrade_apply_wait \
  N2API_FAKE_DOCKER_STACK_FILE="${upgrade_stack_file}" \
  N2API_FAKE_DOCKER_READY_FILE="${upgrade_compose_signal_ready}" \
  "${cli}" --env-file "${generated_env}" --state-dir "${backup_state}" --timeout 30 \
  upgrade apply --plan "${upgrade_plan_path}" --format json \
  >"${test_root}/upgrade-compose-signal.stdout" 2>"${test_root}/upgrade-compose-signal.stderr" &
upgrade_compose_signal_pid=$!
for _ in {1..100}; do
  [[ -e "${upgrade_compose_signal_ready}" ]] && break
  sleep 0.05
done
if [[ ! -e "${upgrade_compose_signal_ready}" ]]; then
  kill -TERM "${upgrade_compose_signal_pid}" 2>/dev/null || true
  wait "${upgrade_compose_signal_pid}" 2>/dev/null || true
  fail "upgrade_compose_signal_not_ready"
fi
kill -TERM "${upgrade_compose_signal_pid}"
set +e
wait "${upgrade_compose_signal_pid}"
upgrade_compose_signal_status=$?
set -e
[[ ${upgrade_compose_signal_status} -eq 143 ]] || fail "upgrade_compose_signal_exit"
jq -e '
  .status == "failed" and .reason_code == "operation_interrupted" and .changed == true and
  .current.stage == "compose_apply" and .current.signal == "TERM" and
  .current.source_schema == 50 and .current.observed_schema == 50
' "${test_root}/upgrade-compose-signal.stdout" >/dev/null || fail "upgrade_compose_signal_receipt"
grep -Fxq "N2API_IMAGE=${candidate_image}" "${generated_env}" || fail "upgrade_compose_signal_reverted_env"
cp -- "${source_env}" "${generated_env}"
chmod 600 -- "${generated_env}"

upgrade_mismatch_schema_file="${test_root}/upgrade-mismatch-schema.version"
set +e
upgrade_schema_mismatch="$(N2API_FAKE_DOCKER_SCHEMA_FILE="${upgrade_mismatch_schema_file}" \
  N2API_FAKE_DOCKER_TARGET_SCHEMA_VERSION=52 PATH="${fake_path}" \
  "${cli}" --env-file "${generated_env}" --state-dir "${backup_state}" \
  upgrade apply --plan "${upgrade_plan_path}" --format json)"
upgrade_schema_mismatch_status=$?
set -e
[[ ${upgrade_schema_mismatch_status} -eq 6 ]] || fail "upgrade_schema_mismatch_exit"
assert_jq "${upgrade_schema_mismatch}" '
  .status == "failed" and .reason_code == "candidate_schema_mismatch" and .changed == true and
  .current.source_schema == 50 and .current.observed_schema == 52
'
grep -Fxq "N2API_IMAGE=${candidate_image}" "${generated_env}" || fail "upgrade_schema_mismatch_reverted_env"
cp -- "${source_env}" "${generated_env}"
chmod 600 -- "${generated_env}"

upgrade_schema_file="${test_root}/upgrade-schema.version"
upgrade_apply_output="$(N2API_FAKE_DOCKER_SCHEMA_FILE="${upgrade_schema_file}" \
  N2API_FAKE_DOCKER_TARGET_SCHEMA_VERSION=51 PATH="${fake_path}" \
  "${cli}" --env-file "${generated_env}" --state-dir "${backup_state}" \
  upgrade apply --plan "${upgrade_plan_path}" --format json)"
assert_jq "${upgrade_apply_output}" '
  .command == "upgrade.apply" and .status == "succeeded" and .reason_code == "upgrade_applied" and
  .changed == true and .target.running_identity == "verified" and
  .current.source_schema == 50 and .current.target_schema == 51
'
[[ "$(<"${upgrade_schema_file}")" == 51 ]] || fail "upgrade_schema_not_advanced"
grep -Fxq "N2API_IMAGE=${candidate_image}" "${generated_env}" || fail "upgrade_target_not_persisted"
upgrade_noop_log="${test_root}/upgrade-noop.log"
upgrade_noop_output="$(N2API_FAKE_DOCKER_SCHEMA_FILE="${upgrade_schema_file}" \
  N2API_FAKE_DOCKER_LOG="${upgrade_noop_log}" PATH="${fake_path}" \
  "${cli}" --env-file "${generated_env}" --state-dir "${backup_state}" \
  upgrade apply --plan "${upgrade_plan_path}" --format json)"
assert_jq "${upgrade_noop_output}" '.status == "noop" and .reason_code == "target_already_healthy" and .changed == false'
if rg -q '(^| )(pull|up|pg_dump|rollback)( |$)|/v1/' "${upgrade_noop_log}"; then
  fail "upgrade_noop_performed_mutation_or_provider_call"
fi

race_archive="${test_root}/race-fixture.dump"
race_checksum="$(sha256sum -- "${fixture_archive}" | awk '{print $1}')"
cp -- "${fixture_archive}" "${race_archive}"
chmod 600 -- "${race_archive}"
race_ready="${test_root}/restore-race.ready"
race_release="${test_root}/restore-race.release"
env \
  PATH="${fake_path}" \
  N2API_FAKE_DOCKER_MODE=restore_pause \
  N2API_FAKE_DOCKER_RESTORE_READY_FILE="${race_ready}" \
  N2API_FAKE_DOCKER_RESTORE_RELEASE_FILE="${race_release}" \
  "${cli}" --env-file "${generated_env}" --state-dir "${test_root}/restore-race-state/n2api" \
    restore drill \
    --archive "${race_archive}" \
    --image "${image}" \
    --evidence-class ci_fixture \
    --admin-password-file "${admin_secret_file}" \
    --encryption-secret-file "${encryption_secret_file}" \
    --format json \
    >"${test_root}/restore-race.stdout" 2>"${test_root}/restore-race.stderr" &
race_pid=$!
for _ in {1..100}; do
  [[ -e "${race_ready}" ]] && break
  sleep 0.05
done
if [[ ! -e "${race_ready}" ]]; then
  kill -TERM "${race_pid}" 2>/dev/null || true
  touch "${race_release}"
  wait "${race_pid}" 2>/dev/null || true
  fail "restore_race_not_ready"
fi
printf 'replaced-after-staging\n' >"${race_archive}"
touch "${race_release}"
wait "${race_pid}" || fail "restore_race_exit"
jq -e --arg checksum "${race_checksum}" '
  .status == "succeeded" and .current.archive_checksum == $checksum
' "${test_root}/restore-race.stdout" >/dev/null || fail "restore_race_checksum_binding"

set +e
fixture_as_real="$(PATH="${fake_path}" "${cli}" --env-file "${generated_env}" \
  --state-dir "${backup_state}" \
  restore drill \
  --archive "${archive}" \
  --image "${image}" \
  --evidence-class real_operator \
  --admin-password-file "${admin_secret_file}" \
  --encryption-secret-file "${encryption_secret_file}" \
  --format json)"
fixture_as_real_status=$?
set -e
[[ ${fixture_as_real_status} -eq 6 ]] || fail "fixture_promoted_to_real"
assert_jq "${fixture_as_real}" '.status == "failed" and .reason_code == "fixture_restore_cannot_be_promoted"'

restore_failure_state="${test_root}/restore-failure-state/n2api"
set +e
restore_failure="$(N2API_FAKE_DOCKER_MODE=restore_gateway_fail PATH="${fake_path}" "${cli}" \
  --env-file "${generated_env}" --state-dir "${restore_failure_state}" \
  restore drill \
  --archive "${fixture_archive}" \
  --image "${image}" \
  --evidence-class ci_fixture \
  --admin-password-file "${admin_secret_file}" \
  --encryption-secret-file "${encryption_secret_file}" \
  --format json)"
restore_failure_status=$?
set -e
[[ ${restore_failure_status} -eq 6 ]] || fail "restore_failure_exit"
assert_jq "${restore_failure}" '
  .status == "failed" and
  .reason_code == "restore_drill_failed" and
  .current.stage == "gateway" and
  .current.cleanup_status == "passed" and
  .current.evidence_class == "ci_fixture"
'
[[ -z "$(find "${restore_failure_state}/restore-runtime/active" -type f -print -quit 2>/dev/null)" ]] || fail "restore_failure_active_marker_retained"

restore_cleanup_failure_state="${test_root}/restore-cleanup-failure-state/n2api"
set +e
restore_cleanup_failure="$(N2API_FAKE_DOCKER_MODE=restore_cleanup_fail PATH="${fake_path}" "${cli}" \
  --env-file "${generated_env}" --state-dir "${restore_cleanup_failure_state}" \
  restore drill \
  --archive "${fixture_archive}" \
  --image "${image}" \
  --evidence-class ci_fixture \
  --admin-password-file "${admin_secret_file}" \
  --encryption-secret-file "${encryption_secret_file}" \
  --format json)"
restore_cleanup_failure_status=$?
set -e
[[ ${restore_cleanup_failure_status} -eq 6 ]] || fail "restore_cleanup_failure_exit"
assert_jq "${restore_cleanup_failure}" '
  .status == "failed" and
  .reason_code == "restore_drill_failed" and
  .current.stage == "cleanup" and
  .current.cleanup_status == "failed"
'

restore_timeout_state="${test_root}/restore-timeout-state/n2api"
set +e
restore_timeout="$(N2API_FAKE_DOCKER_MODE=restore_wait PATH="${fake_path}" "${cli}" \
  --env-file "${generated_env}" --state-dir "${restore_timeout_state}" --timeout 1 \
  restore drill \
  --archive "${fixture_archive}" \
  --image "${image}" \
  --evidence-class ci_fixture \
  --admin-password-file "${admin_secret_file}" \
  --encryption-secret-file "${encryption_secret_file}" \
  --format json)"
restore_timeout_status=$?
set -e
[[ ${restore_timeout_status} -eq 124 ]] || fail "restore_timeout_exit"
assert_jq "${restore_timeout}" '.status == "failed" and .reason_code == "restore_drill_timeout"'
[[ -z "$(find "${restore_timeout_state}" -maxdepth 1 -type f -name '.restore-*' -print -quit 2>/dev/null)" ]] || fail "restore_timeout_temp_retained"

set +e
restore_pull_timeout="$(N2API_FAKE_DOCKER_MODE=image_pull_wait PATH="${fake_path}" "${cli}" \
  --env-file "${generated_env}" --state-dir "${test_root}/restore-pull-timeout-state/n2api" --timeout 1 \
  restore drill --archive "${fixture_archive}" --image "${image}" --evidence-class ci_fixture \
  --admin-password-file "${admin_secret_file}" --encryption-secret-file "${encryption_secret_file}" --format json)"
restore_pull_timeout_status=$?
set -e
[[ ${restore_pull_timeout_status} -eq 124 ]] || fail "restore_pull_timeout_exit"
assert_jq "${restore_pull_timeout}" '.status == "failed" and .reason_code == "restore_image_pull_timeout" and .current.stage == "image_pull"'

set +e
restore_pull_kill_timeout="$(N2API_FAKE_DOCKER_MODE=image_pull_ignore_term PATH="${fake_path}" "${cli}" \
  --env-file "${generated_env}" --state-dir "${test_root}/restore-pull-kill-timeout-state/n2api" --timeout 1 \
  restore drill --archive "${fixture_archive}" --image "${image}" --evidence-class ci_fixture \
  --admin-password-file "${admin_secret_file}" --encryption-secret-file "${encryption_secret_file}" --format json)"
restore_pull_kill_timeout_status=$?
set -e
[[ ${restore_pull_kill_timeout_status} -eq 124 ]] || fail "restore_pull_kill_timeout_exit"
assert_jq "${restore_pull_kill_timeout}" '.status == "failed" and .reason_code == "restore_image_pull_timeout"'

set +e
restore_list_timeout="$(N2API_FAKE_DOCKER_MODE=archive_list_wait PATH="${fake_path}" "${cli}" \
  --env-file "${generated_env}" --state-dir "${test_root}/restore-list-timeout-state/n2api" --timeout 1 \
  restore drill --archive "${fixture_archive}" --image "${image}" --evidence-class ci_fixture \
  --admin-password-file "${admin_secret_file}" --encryption-secret-file "${encryption_secret_file}" --format json)"
restore_list_timeout_status=$?
set -e
[[ ${restore_list_timeout_status} -eq 124 ]] || fail "restore_list_timeout_exit"
assert_jq "${restore_list_timeout}" '.status == "failed" and .reason_code == "restore_archive_list_timeout" and .current.stage == "archive_list"'

restore_signal_state="${test_root}/restore-signal-state/n2api"
restore_signal_ready="${test_root}/restore-signal.ready"
env \
  PATH="${fake_path}" \
  N2API_FAKE_DOCKER_MODE=restore_wait \
  N2API_FAKE_DOCKER_RESTORE_READY_FILE="${restore_signal_ready}" \
  "${cli}" --env-file "${generated_env}" --state-dir "${restore_signal_state}" --timeout 30 \
    restore drill \
    --archive "${fixture_archive}" \
    --image "${image}" \
    --evidence-class ci_fixture \
    --admin-password-file "${admin_secret_file}" \
    --encryption-secret-file "${encryption_secret_file}" \
    --format json \
    >"${test_root}/restore-signal.stdout" 2>"${test_root}/restore-signal.stderr" &
restore_signal_pid=$!
for _ in {1..100}; do
  [[ -e "${restore_signal_ready}" ]] && break
  sleep 0.05
done
if [[ ! -e "${restore_signal_ready}" ]]; then
  kill -TERM "${restore_signal_pid}" 2>/dev/null || true
  wait "${restore_signal_pid}" 2>/dev/null || true
  fail "restore_signal_not_ready"
fi
kill -TERM "${restore_signal_pid}"
set +e
wait "${restore_signal_pid}"
restore_signal_status=$?
set -e
[[ ${restore_signal_status} -eq 143 ]] || fail "restore_signal_exit"
jq -e '
  .status == "failed" and
  .reason_code == "restore_drill_interrupted" and
  .current.signal == "TERM" and
  (.current.cleanup_status == "passed" or .current.cleanup_status == "unknown")
' "${test_root}/restore-signal.stdout" >/dev/null || fail "restore_signal_receipt"
[[ -z "$(find "${restore_signal_state}" -maxdepth 1 -type f -name '.restore-*' -print -quit 2>/dev/null)" ]] || fail "restore_signal_temp_retained"

for protected_output in \
  "${backup_state}" \
  "${fixture_restore_state}" \
  "${restore_failure_state}" \
  "${restore_cleanup_failure_state}" \
  "${restore_timeout_state}" \
  "${restore_signal_state}" \
  "${backup_dir}"; do
  if rg -n 'N2API_TEST_SECRET_CANARY_DO_NOT_LEAK' "${protected_output}" >/dev/null 2>&1; then
    fail "restore_secret_canary_retained"
  fi
done

for retained in "${test_root}"/*.stdout "${test_root}"/*.stderr; do
  [[ -e "${retained}" ]] || continue
  if rg -n --fixed-strings "${postgres_secret}" "${retained}" >/dev/null 2>&1 ||
    rg -n --fixed-strings "${admin_secret}" "${retained}" >/dev/null 2>&1 ||
    rg -n --fixed-strings "${encryption_secret}" "${retained}" >/dev/null 2>&1; then
    fail "generated_secret_retained"
  fi
done

printf 'ops_test_status=passed scope=discovery_state_operations_host_config_image_runtime_backup_restore_deploy\n'

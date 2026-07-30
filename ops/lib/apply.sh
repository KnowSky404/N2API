#!/usr/bin/env bash

N2API_APPLY_OPERATION_ID=""
N2API_APPLY_STARTED_AT=""
N2API_APPLY_LOCKED=false
N2API_APPLY_CHILD_PID=""
N2API_APPLY_STAGE="initializing"
N2API_APPLY_COMMAND=""
N2API_APPLY_PLAN_OPERATION_ID=""
N2API_APPLY_TARGET_IMAGE=""
N2API_APPLY_SOURCE_IMAGE=""
N2API_APPLY_SOURCE_SCHEMA=""
N2API_APPLY_LIVE_CHANGED=false

n2api_apply_stop_child() {
  local signal=${1:-TERM} attempts i
  [[ -n "${N2API_APPLY_CHILD_PID}" ]] || return 0
  kill -s "${signal}" -- "-${N2API_APPLY_CHILD_PID}" 2>/dev/null ||
    kill -s "${signal}" "${N2API_APPLY_CHILD_PID}" 2>/dev/null || true
  attempts=$(( $(n2api_timeout_kill_after) * 10 ))
  for ((i = 0; i < attempts; i++)); do
    kill -0 "${N2API_APPLY_CHILD_PID}" 2>/dev/null || break
    sleep 0.1
  done
  if kill -0 "${N2API_APPLY_CHILD_PID}" 2>/dev/null; then
    kill -KILL -- "-${N2API_APPLY_CHILD_PID}" 2>/dev/null ||
      kill -KILL "${N2API_APPLY_CHILD_PID}" 2>/dev/null || true
  fi
  wait "${N2API_APPLY_CHILD_PID}" 2>/dev/null || true
  N2API_APPLY_CHILD_PID=""
}

n2api_apply_cleanup() {
  n2api_apply_stop_child TERM
  if [[ "${N2API_APPLY_LOCKED}" == true ]]; then
    n2api_lock_release
    N2API_APPLY_LOCKED=false
  fi
}

n2api_apply_receipt() {
  local status=$1 changed=$2 reason=$3 summary=$4 current=${5:-'{}'} target=${6:-'{}'} next_actions=${7:-'[]'}
  local document risk="service_change"
  [[ "${N2API_APPLY_COMMAND}" != rollback ]] || risk="high_risk"
  document="$(n2api_envelope_json \
    "${N2API_APPLY_COMMAND}.apply" "${risk}" "${status}" "${changed}" "${reason}" "${summary}" \
    "${current}" "${target}" '[]' "${next_actions}" \
    "${N2API_APPLY_OPERATION_ID}" "${N2API_APPLY_STARTED_AT}")"
  n2api_operation_write "${N2API_APPLY_OPERATION_ID}" "${document}" || return 1
  n2api_emit_document "${document}"
}

n2api_apply_cleanup_signal() {
  local exit_code=$1
  trap - EXIT INT TERM
  n2api_apply_cleanup
  exit "${exit_code}"
}

n2api_apply_finish() {
  local status=$1 changed=$2 reason=$3 summary=$4 current=$5 target=$6 next_actions=$7 exit_code=$8
  local receipt_status=0 risk="service_change"
  [[ "${N2API_APPLY_COMMAND}" != rollback ]] || risk="high_risk"
  trap - EXIT INT TERM
  trap 'n2api_apply_cleanup_signal 130' INT
  trap 'n2api_apply_cleanup_signal 143' TERM
  n2api_apply_receipt "${status}" "${changed}" "${reason}" "${summary}" "${current}" "${target}" "${next_actions}" || receipt_status=1
  n2api_apply_cleanup
  trap - INT TERM
  if [[ ${receipt_status} -ne 0 ]]; then
    n2api_emit "${N2API_APPLY_COMMAND}.apply" "${risk}" "failed" "${changed}" \
      "operation_receipt_failed" "Operation finished but its receipt could not be written" \
      "${current}" "${target}" '[]' '["Preserve external evidence and repair operator state"]'
    exit "${N2API_EXIT_FAILED}"
  fi
  exit "${exit_code}"
}

n2api_apply_signal() {
  local signal=$1 exit_code=$2 current
  trap - EXIT
  trap 'n2api_apply_cleanup_signal 130' INT
  trap 'n2api_apply_cleanup_signal 143' TERM
  n2api_apply_stop_child "${signal}"
  if [[ "${N2API_APPLY_COMMAND}" == upgrade || "${N2API_APPLY_COMMAND}" == rollback ]] &&
    n2api_apply_target_is_persisted; then
    N2API_APPLY_LIVE_CHANGED=true
  fi
  if [[ "${N2API_APPLY_COMMAND}" == upgrade || "${N2API_APPLY_COMMAND}" == rollback ]]; then
    current="$(n2api_apply_signal_observation_json "${N2API_APPLY_STAGE}" "${signal}")"
  else
    current="$(jq -cn \
      --arg stage "${N2API_APPLY_STAGE}" \
      --arg signal "${signal}" \
      --arg plan_operation_id "${N2API_APPLY_PLAN_OPERATION_ID}" \
      '{stage:$stage,signal:$signal,plan_operation_id:$plan_operation_id}')"
  fi
  n2api_apply_receipt "failed" "${N2API_APPLY_LIVE_CHANGED}" "operation_interrupted" "Operation apply was interrupted" \
    "${current}" "$(jq -cn --arg image "${N2API_APPLY_TARGET_IMAGE}" '{image:$image}')" \
    '["Inspect the operation receipt and live stack before retrying"]' || true
  n2api_apply_cleanup
  trap - INT TERM
  exit "${exit_code}"
}

n2api_apply_target_is_persisted() {
  local persisted_image
  [[ -n "${N2API_APPLY_TARGET_IMAGE}" ]] || return 1
  n2api_env_file_is_safe "${N2API_ENV_FILE}" || return 1
  persisted_image="$(sed -n 's/^N2API_IMAGE=//p' "${N2API_ENV_FILE}" | tail -n 1)"
  [[ "${persisted_image}" == "${N2API_APPLY_TARGET_IMAGE}" ]]
}

n2api_apply_signal_observation_json() {
  local stage=$1 signal=$2 observed_image="" observed_schema="" availability="unavailable"
  if n2api_env_file_is_safe "${N2API_ENV_FILE}"; then
    observed_image="$(sed -n 's/^N2API_IMAGE=//p' "${N2API_ENV_FILE}" | tail -n 1)"
    [[ -z "${observed_image}" ]] || availability="partial"
  fi
  if [[ "${N2API_APPLY_LOCKED}" == true ]] &&
    observed_schema="$(N2API_TIMEOUT_SECONDS=2 n2api_schema_version 2>/dev/null)"; then
    availability="available"
  fi
  jq -cn \
    --arg stage "${stage}" \
    --arg signal "${signal}" \
    --arg plan_operation_id "${N2API_APPLY_PLAN_OPERATION_ID}" \
    --arg source_image "${N2API_APPLY_SOURCE_IMAGE}" \
    --arg source_schema "${N2API_APPLY_SOURCE_SCHEMA}" \
    --arg observed_image "${observed_image}" \
    --arg observed_schema "${observed_schema}" \
    --arg observation_availability "${availability}" '
    {
      stage:$stage,
      plan_operation_id:$plan_operation_id,
      source_image:(if $source_image == "" then null else $source_image end),
      source_schema:(if ($source_schema | test("^[0-9]+$")) then ($source_schema | tonumber) else null end),
      observed_image:(if $observed_image == "" then null else $observed_image end),
      observed_schema:(if ($observed_schema | test("^[0-9]+$")) then ($observed_schema | tonumber) else null end),
      observation_availability:$observation_availability,
      signal:$signal
    }'
}

n2api_upgrade_observation_json() {
  local stage=$1 runtime=${2:-} signal=${3:-} observed_image="" observed_schema="" availability="unavailable"
  if [[ -z "${runtime}" ]]; then
    runtime="$(n2api_runtime_snapshot_json 2>/dev/null || printf '{}')"
  fi
  if jq -e 'type == "object" and .n2api != null' <<<"${runtime}" >/dev/null 2>&1; then
    availability="available"
    observed_image="$(jq -r '.n2api.configured_image // empty' <<<"${runtime}")"
    observed_schema="$(jq -r '.postgres.schema.value // empty' <<<"${runtime}")"
  fi
  jq -cn \
    --arg stage "${stage}" \
    --arg signal "${signal}" \
    --arg plan_operation_id "${N2API_APPLY_PLAN_OPERATION_ID}" \
    --arg source_image "${N2API_APPLY_SOURCE_IMAGE}" \
    --arg source_schema "${N2API_APPLY_SOURCE_SCHEMA}" \
    --arg observed_image "${observed_image}" \
    --arg observed_schema "${observed_schema}" \
    --arg observation_availability "${availability}" \
    '{
      stage:$stage,
      plan_operation_id:$plan_operation_id,
      source_image:(if $source_image == "" then null else $source_image end),
      source_schema:(if ($source_schema | test("^[0-9]+$")) then ($source_schema | tonumber) else null end),
      observed_image:(if $observed_image == "" then null else $observed_image end),
      observed_schema:(if ($observed_schema | test("^[0-9]+$")) then ($observed_schema | tonumber) else null end),
      observation_availability:$observation_availability,
      signal:(if $signal == "" then null else $signal end)
    }'
}

n2api_apply_run_tracked() {
  local status
  setsid timeout --signal=TERM --kill-after="$(n2api_timeout_kill_after)" \
    "${N2API_TIMEOUT_SECONDS}" "$@" &
  N2API_APPLY_CHILD_PID=$!
  if wait "${N2API_APPLY_CHILD_PID}" 2>/dev/null; then
    status=0
  else
    status=$?
  fi
  N2API_APPLY_CHILD_PID=""
  return "${status}"
}

n2api_apply_static_invariants_match() {
  local plan=$1 context=$2 target_manifest=$3
  jq -e --argjson context "${context}" --argjson manifest "${target_manifest}" '
    .invariants.environment_hmac == $context.invariants.environment_hmac and
    .invariants.compose_inputs_hmac == $context.invariants.compose_inputs_hmac and
    .invariants.compose_config_hmac == $context.invariants.compose_config_hmac and
    .invariants.git_commit == $context.invariants.git_commit and
    .invariants.git_dirty == $context.invariants.git_dirty and
    .invariants.target_manifest_digest == $manifest.digest
  ' <<<"${plan}" >/dev/null
}

n2api_apply_runtime_invariants_match() {
  local plan=$1 context=$2
  jq -e --argjson context "${context}" '
    .invariants.services_fingerprint == $context.invariants.services_fingerprint and
    .invariants.current_image == $context.invariants.current_image and
    .invariants.current_image_id == $context.invariants.current_image_id and
    .invariants.schema_version == $context.invariants.schema_version and
    .invariants.postgres_volume_name == $context.invariants.postgres_volume_name and
    .invariants.postgres_volume_exists == $context.invariants.postgres_volume_exists
  ' <<<"${plan}" >/dev/null
}

n2api_runtime_exact_target_healthy() {
  local runtime=$1 target_image=$2
  jq -e --arg image "${target_image}" '
    .n2api.availability == "available" and
    .n2api.running == true and
    .n2api.health == "healthy" and
    .n2api.configured_image == $image and
    .postgres.health == "healthy" and
    .probes.livez.status == "passed" and
    .probes.readyz.status == "passed" and
    .probes.version.status == "passed" and
    .n2api.security.read_only_root == true and
    (.n2api.security.cap_drop | index("ALL")) != null and
    (.n2api.security.security_opt | index("no-new-privileges:true")) != null and
    (.n2api.security.tmpfs["/tmp"] | contains("noexec") and contains("nosuid") and contains("nodev"))
  ' <<<"${runtime}" >/dev/null
}

n2api_local_image_digest_matches() {
  local image=$1 repository digest repo_digests
  repository=${image%%:*}
  digest=${image##*@}
  repo_digests="$(docker image inspect --format '{{json .RepoDigests}}' "${image}" 2>/dev/null)" || return 1
  jq -e --arg expected "${repository}@${digest}" 'index($expected) != null' <<<"${repo_digests}" >/dev/null
}

n2api_noop_target_manifest_matches() {
  local plan=$1 target_manifest=$2
  jq -e --argjson manifest "${target_manifest}" '
    .invariants.target_manifest_digest == $manifest.digest and
    .target.image.reference == $manifest.image
  ' <<<"${plan}" >/dev/null
}

n2api_upgrade_noop_is_safe() {
  local plan=$1 runtime=$2 target_manifest=$3 candidate_schema live_schema
  n2api_apply_target_is_persisted || return 1
  n2api_noop_target_manifest_matches "${plan}" "${target_manifest}" || return 1
  n2api_runtime_exact_target_healthy "${runtime}" "${N2API_APPLY_TARGET_IMAGE}" || return 1
  candidate_schema="$(jq -r '.evidence.candidate_restore.schema_version_value // empty' <<<"${plan}")"
  live_schema="$(jq -r '.postgres.schema.value // empty' <<<"${runtime}")"
  [[ "${candidate_schema}" =~ ^[1-9][0-9]*$ && "${live_schema}" == "${candidate_schema}" ]] || return 1
  n2api_upgrade_evidence_matches_plan "${plan}"
}

n2api_rollback_noop_is_safe() {
  local plan=$1 runtime=$2 target_manifest=$3 target_schema
  n2api_apply_target_is_persisted || return 1
  n2api_noop_target_manifest_matches "${plan}" "${target_manifest}" || return 1
  n2api_runtime_exact_target_healthy "${runtime}" "${N2API_APPLY_TARGET_IMAGE}" || return 1
  target_schema="$(jq -r '.evidence.compatibility.current_schema // empty' <<<"${plan}")"
  [[ "${target_schema}" =~ ^[1-9][0-9]*$ ]] || return 1
  [[ "$(jq -r '.postgres.schema.value // empty' <<<"${runtime}")" == "${target_schema}" ]] || return 1
  n2api_previous_receipt_matches_plan "${plan}"
}

n2api_upgrade_evidence_matches_plan() {
  local plan=$1 source_image source_schema target_image backup current_restore candidate_restore rollback_manifest
  source_image="$(jq -r '.source.runtime.configured_image // empty' <<<"${plan}")"
  source_schema="$(jq -r '.source.runtime.schema_version // empty' <<<"${plan}")"
  target_image="$(jq -r '.target.image.reference' <<<"${plan}")"
  backup="$(n2api_real_backup_evidence_json "${source_image}" "${source_schema}")" || return 1
  jq -e --argjson current "${backup}" '
    .evidence.backup.backup_id == $current.backup_id and
    .evidence.backup.checksum == $current.checksum and
    .evidence.backup.size_bytes == $current.size_bytes and
    .invariants.backup_checksum == $current.checksum and
    .invariants.backup_metadata_hmac == $current.metadata_hmac
  ' <<<"${plan}" >/dev/null || return 1
  current_restore="$(n2api_real_restore_evidence_json "${source_image}" "$(jq -r '.backup_id' <<<"${backup}")" "$(jq -r '.checksum' <<<"${backup}")")" || return 1
  candidate_restore="$(n2api_real_restore_evidence_json "${target_image}" "$(jq -r '.backup_id' <<<"${backup}")" "$(jq -r '.checksum' <<<"${backup}")")" || return 1
  jq -e --argjson current_restore "${current_restore}" --argjson candidate_restore "${candidate_restore}" '
    .invariants.current_restore_hmac == $current_restore.receipt_hmac and
    .invariants.candidate_restore_hmac == $candidate_restore.receipt_hmac and
    .evidence.current_restore.operation_id == $current_restore.operation_id and
    .evidence.candidate_restore.operation_id == $candidate_restore.operation_id
  ' <<<"${plan}" >/dev/null || return 1
  rollback_manifest="$(n2api_image_inspect_json "${source_image}")" || return 1
  [[ "$(jq -r '.digest' <<<"${rollback_manifest}")" == "$(jq -r '.invariants.rollback_manifest_digest' <<<"${plan}")" ]]
}

n2api_previous_receipt_matches_plan() {
  local plan=$1 operation_id path document
  operation_id="$(jq -r '.evidence.previous_operation.operation_id // empty' <<<"${plan}")"
  n2api_validate_operation_id "${operation_id}" || return 1
  path="${N2API_STATE_DIR}/operations/${operation_id}.json"
  [[ -f "${path}" && ! -L "${path}" ]] || return 1
  document="$(jq -ce . "${path}" 2>/dev/null)" || return 1
  n2api_operation_integrity_valid "${document}" || return 1
  jq -e --argjson receipt "${document}" '
    .invariants.previous_receipt_hmac == $receipt.integrity_hmac and
    .evidence.previous_operation.receipt_hmac == $receipt.integrity_hmac and
    .evidence.previous_operation.current_image == $receipt.target.image and
    .evidence.previous_operation.previous_image == $receipt.target.rollback_image and
    .evidence.previous_operation.source_schema == $receipt.current.source_schema and
    .evidence.previous_operation.target_schema == $receipt.current.target_schema and
    .source.runtime.configured_image == $receipt.target.image and
    .target.image.reference == $receipt.target.rollback_image and
    .source.runtime.schema_version == $receipt.current.target_schema and
    .evidence.compatibility.current_schema == $receipt.current.source_schema and
    $receipt.current.source_schema == $receipt.current.target_schema and
    ($receipt.command == "upgrade.apply" or $receipt.command == "rollback.apply") and
    $receipt.status == "succeeded" and
    $receipt.changed == true
  ' <<<"${plan}" >/dev/null
}

n2api_env_replace_image() {
  local image=$1 directory tmp
  n2api_env_file_is_safe "${N2API_ENV_FILE}" || return 1
  directory="$(dirname -- "${N2API_ENV_FILE}")"
  tmp="$(mktemp "${directory}/.n2api-env-upgrade.XXXXXX")" || return 1
  if ! awk -v replacement="N2API_IMAGE=${image}" '
    BEGIN { count = 0 }
    /^N2API_IMAGE=/ { print replacement; count++; next }
    { print }
    END { if (count != 1) exit 1 }
  ' "${N2API_ENV_FILE}" >"${tmp}"; then
    rm -f -- "${tmp}"
    return 1
  fi
  chmod 600 -- "${tmp}" || { rm -f -- "${tmp}"; return 1; }
  mv -- "${tmp}" "${N2API_ENV_FILE}" || { rm -f -- "${tmp}"; return 1; }
  N2API_APPLY_LIVE_CHANGED=true
  n2api_env_load "${N2API_ENV_FILE}"
}

n2api_deploy_apply() {
  local plan_path="" plan runtime context target_manifest expiry_epoch now_epoch lock_status status
  local target current after container compose_status pull_status reason exit_code
  while (($# > 0)); do
    case "$1" in
      --plan) (($# >= 2)) || n2api_usage_error "missing_plan_path"; plan_path=$2; shift 2 ;;
      *) n2api_usage_error "deploy_apply_accepts_only_plan" ;;
    esac
  done
  [[ -n "${plan_path}" ]] || n2api_usage_error "deploy_apply_requires_plan"
  N2API_APPLY_OPERATION_ID="$(n2api_operation_id)"
  N2API_APPLY_STARTED_AT="$(n2api_now)"
  N2API_APPLY_COMMAND="deploy"
  N2API_APPLY_LIVE_CHANGED=false
  N2API_APPLY_SOURCE_IMAGE=""
  N2API_APPLY_SOURCE_SCHEMA=""
  if ! n2api_plan_read "${plan_path}" deploy plan; then
    n2api_apply_receipt "blocked" false "${N2API_PLAN_READ_REASON}" "Deploy plan is unsafe or invalid" '{}' '{}' '["Create a new deploy plan"]' || true
    exit "${N2API_EXIT_BLOCKED}"
  fi
  N2API_APPLY_PLAN_OPERATION_ID="$(jq -r '.operation_id' <<<"${plan}")"
  N2API_APPLY_TARGET_IMAGE="$(jq -r '.target.image.reference' <<<"${plan}")"
  if [[ "$(jq -r '.blocked_reasons | length' <<<"${plan}")" -gt 0 ]]; then
    n2api_apply_receipt "blocked" false "plan_contains_blockers" "Blocked deploy plan cannot be applied" \
      "$(jq -cn --arg plan_operation_id "${N2API_APPLY_PLAN_OPERATION_ID}" '{plan_operation_id:$plan_operation_id}')" \
      "$(jq -cn --arg image "${N2API_APPLY_TARGET_IMAGE}" '{image:$image}')" '["Create a new plan after resolving blockers"]' || true
    exit "${N2API_EXIT_BLOCKED}"
  fi
  expiry_epoch="$(date -u -d "$(jq -r '.expires_at' <<<"${plan}")" +%s 2>/dev/null || printf 0)"
  now_epoch="$(date -u +%s)"
  if ((expiry_epoch <= now_epoch)); then
    n2api_apply_receipt "blocked" false "plan_expired" "Deploy plan has expired" '{}' '{}' '["Create a fresh deploy plan"]' || true
    exit "${N2API_EXIT_BLOCKED}"
  fi
  n2api_check_env_file >/dev/null || {
    n2api_apply_receipt "blocked" false "stale_plan_detected" "Deployment environment is unavailable" '{}' '{}' '["Create a fresh deploy plan"]' || true
    exit "${N2API_EXIT_BLOCKED}"
  }
  target_manifest="$(n2api_image_inspect_json "${N2API_APPLY_TARGET_IMAGE}")" || {
    n2api_apply_receipt "blocked" false "target_image_unavailable" "Deploy target image is unavailable" '{}' '{}' '["Verify the immutable target"]' || true
    exit "${N2API_EXIT_BLOCKED}"
  }
  runtime="$(n2api_runtime_snapshot_json)" || {
    n2api_apply_receipt "failed" false "runtime_snapshot_failed" "Runtime snapshot failed" '{}' '{}' '[]' || true
    exit "${N2API_EXIT_FAILED}"
  }
  context="$(n2api_plan_context_json "${runtime}")" || {
    n2api_apply_receipt "failed" false "plan_context_failed" "Apply context could not be verified" '{}' '{}' '[]' || true
    exit "${N2API_EXIT_FAILED}"
  }
  if ! n2api_apply_static_invariants_match "${plan}" "${context}" "${target_manifest}"; then
    n2api_apply_receipt "blocked" false "stale_plan_detected" "Static plan invariants changed" '{}' '{}' '["Create a fresh deploy plan"]' || true
    exit "${N2API_EXIT_BLOCKED}"
  fi
  set +e
  n2api_lock_acquire "${N2API_APPLY_OPERATION_ID}"
  lock_status=$?
  set -e
  if [[ ${lock_status} -eq 2 ]]; then
    n2api_apply_receipt "contended" false "operation_lock_contended" "Another operator action holds the lock" '{}' '{}' '["Wait for the active operation"]' || true
    exit "${N2API_EXIT_CONTENDED}"
  elif [[ ${lock_status} -ne 0 ]]; then
    n2api_apply_receipt "failed" false "operation_state_unsafe" "Operator lock could not be acquired safely" '{}' '{}' '[]' || true
    exit "${N2API_EXIT_FAILED}"
  fi
  N2API_APPLY_LOCKED=true
  trap 'n2api_apply_cleanup' EXIT
  trap 'n2api_apply_signal INT 130' INT
  trap 'n2api_apply_signal TERM 143' TERM
  if n2api_runtime_exact_target_healthy "${runtime}" "${N2API_APPLY_TARGET_IMAGE}"; then
    container="$(jq -r '.n2api.name' <<<"${runtime}")"
    if env N2API_ENV_FILE="${N2API_ENV_FILE}" \
      "${N2API_REPO_ROOT}/dev/verification/verify-release-image.sh" --container "${container}" "${N2API_APPLY_TARGET_IMAGE}" >/dev/null 2>&1; then
      n2api_apply_finish "noop" false "target_already_healthy" "Exact deploy target is already healthy" \
        "$(jq -cn --arg plan_operation_id "${N2API_APPLY_PLAN_OPERATION_ID}" '{plan_operation_id:$plan_operation_id,stage:"noop"}')" \
        "$(jq -cn --arg image "${N2API_APPLY_TARGET_IMAGE}" '{image:$image}')" '[]' 0
    fi
  fi
  if ! n2api_apply_runtime_invariants_match "${plan}" "${context}"; then
    n2api_apply_finish "blocked" false "stale_plan_detected" "Live stack changed after planning" \
      "$(jq -cn --arg plan_operation_id "${N2API_APPLY_PLAN_OPERATION_ID}" '{plan_operation_id:$plan_operation_id,stage:"invariants"}')" \
      "$(jq -cn --arg image "${N2API_APPLY_TARGET_IMAGE}" '{image:$image}')" '["Create a fresh deploy plan"]' "${N2API_EXIT_BLOCKED}"
  fi
  N2API_APPLY_STAGE="image_pull"
  set +e
  n2api_apply_run_tracked docker pull --quiet "${N2API_APPLY_TARGET_IMAGE}" >/dev/null 2>&1
  pull_status=$?
  set -e
  if [[ ${pull_status} -ne 0 ]]; then
    if n2api_status_is_timeout "${pull_status}"; then reason="image_pull_timeout"; exit_code=124; else reason="image_pull_failed"; exit_code=${N2API_EXIT_FAILED}; fi
    n2api_apply_finish "failed" false "${reason}" "Deploy target image pull failed" \
      "$(jq -cn --arg stage "${N2API_APPLY_STAGE}" --arg plan_operation_id "${N2API_APPLY_PLAN_OPERATION_ID}" '{stage:$stage,plan_operation_id:$plan_operation_id}')" \
      "$(jq -cn --arg image "${N2API_APPLY_TARGET_IMAGE}" '{image:$image}')" '["Inspect registry access before retrying"]' "${exit_code}"
  fi
  n2api_local_image_digest_matches "${N2API_APPLY_TARGET_IMAGE}" ||
    n2api_apply_finish "failed" false "image_identity_mismatch" "Pulled image digest does not match the plan" \
      "$(jq -cn --arg stage "image_pull" '{stage:$stage}')" "$(jq -cn --arg image "${N2API_APPLY_TARGET_IMAGE}" '{image:$image}')" '[]' "${N2API_EXIT_FAILED}"
  N2API_APPLY_STAGE="compose_apply"
  N2API_APPLY_LIVE_CHANGED=true
  n2api_compose_command
  set +e
  n2api_apply_run_tracked env N2API_ENV_FILE="${N2API_ENV_FILE}" "${N2API_COMPOSE_CMD[@]}" up --detach --wait
  compose_status=$?
  set -e
  if n2api_status_is_timeout "${compose_status}"; then
    n2api_apply_finish "failed" true "readiness_timeout" "Compose deployment exceeded its readiness timeout" \
      "$(jq -cn --arg stage "compose_apply" '{stage:$stage}')" "$(jq -cn --arg image "${N2API_APPLY_TARGET_IMAGE}" '{image:$image}')" '["Inspect the live stack; automatic rollback is forbidden"]' 124
  elif [[ ${compose_status} -ne 0 ]]; then
    n2api_apply_finish "failed" true "compose_apply_failed" "Compose deployment failed" \
      "$(jq -cn --arg stage "compose_apply" '{stage:$stage}')" "$(jq -cn --arg image "${N2API_APPLY_TARGET_IMAGE}" '{image:$image}')" '["Inspect the live stack; automatic rollback is forbidden"]' "${N2API_EXIT_FAILED}"
  fi
  N2API_APPLY_STAGE="verification"
  after="$(n2api_runtime_snapshot_json)" ||
    n2api_apply_finish "failed" true "basic_verification_failed" "Post-deploy runtime snapshot failed" \
      "$(jq -cn --arg stage "verification" '{stage:$stage}')" "$(jq -cn --arg image "${N2API_APPLY_TARGET_IMAGE}" '{image:$image}')" '[]' "${N2API_EXIT_FAILED}"
  container="$(jq -r '.n2api.name // empty' <<<"${after}")"
  if ! n2api_runtime_exact_target_healthy "${after}" "${N2API_APPLY_TARGET_IMAGE}" ||
    [[ -z "${container}" ]] ||
    ! env N2API_ENV_FILE="${N2API_ENV_FILE}" \
      "${N2API_REPO_ROOT}/dev/verification/verify-release-image.sh" --container "${container}" "${N2API_APPLY_TARGET_IMAGE}" >/dev/null 2>&1; then
    n2api_apply_finish "failed" true "basic_verification_failed" "Post-deploy verification failed" \
      "$(jq -cn --arg stage "verification" --arg plan_operation_id "${N2API_APPLY_PLAN_OPERATION_ID}" '{stage:$stage,plan_operation_id:$plan_operation_id}')" \
      "$(jq -cn --arg image "${N2API_APPLY_TARGET_IMAGE}" '{image:$image}')" '["Preserve evidence and diagnose without automatic rollback"]' "${N2API_EXIT_FAILED}"
  fi
  current="$(jq -cn --arg plan_operation_id "${N2API_APPLY_PLAN_OPERATION_ID}" --arg before_services "$(jq -c '.source.runtime.services_count' <<<"${plan}")" \
    --argjson schema_version "$(jq -r '.postgres.schema.value' <<<"${after}")" \
    '{plan_operation_id:$plan_operation_id,stage:"completed",before_services:($before_services|tonumber),schema_version:$schema_version,basic_verification:"passed"}')"
  target="$(jq -cn --arg image "${N2API_APPLY_TARGET_IMAGE}" '{image:$image,running_identity:"verified"}')"
  n2api_apply_finish "succeeded" true "deploy_applied" "Deployment applied and verified" \
    "${current}" "${target}" '["Complete external reverse proxy, TLS, DNS, and firewall owner gates"]' 0
}

n2api_upgrade_apply() {
  local plan_path="" plan runtime context target_manifest expiry_epoch now_epoch lock_status source_image source_schema
  local target current after container pull_status compose_status reason exit_code candidate_schema
  while (($# > 0)); do
    case "$1" in
      --plan) (($# >= 2)) || n2api_usage_error "missing_plan_path"; plan_path=$2; shift 2 ;;
      *) n2api_usage_error "upgrade_apply_accepts_only_plan" ;;
    esac
  done
  [[ -n "${plan_path}" ]] || n2api_usage_error "upgrade_apply_requires_plan"
  N2API_APPLY_OPERATION_ID="$(n2api_operation_id)"
  N2API_APPLY_STARTED_AT="$(n2api_now)"
  N2API_APPLY_COMMAND="upgrade"
  N2API_APPLY_LIVE_CHANGED=false
  N2API_APPLY_SOURCE_IMAGE=""
  N2API_APPLY_SOURCE_SCHEMA=""
  trap 'n2api_apply_cleanup' EXIT
  trap 'n2api_apply_signal INT 130' INT
  trap 'n2api_apply_signal TERM 143' TERM
  if ! n2api_plan_read "${plan_path}" upgrade plan; then
    n2api_apply_receipt "blocked" false "${N2API_PLAN_READ_REASON}" "Upgrade plan is unsafe or invalid" '{}' '{}' '["Create a new upgrade plan"]' || true
    exit "${N2API_EXIT_BLOCKED}"
  fi
  N2API_APPLY_PLAN_OPERATION_ID="$(jq -r '.operation_id' <<<"${plan}")"
  N2API_APPLY_TARGET_IMAGE="$(jq -r '.target.image.reference' <<<"${plan}")"
  source_image="$(jq -r '.source.runtime.configured_image // empty' <<<"${plan}")"
  source_schema="$(jq -r '.source.runtime.schema_version // empty' <<<"${plan}")"
  N2API_APPLY_SOURCE_IMAGE="${source_image}"
  N2API_APPLY_SOURCE_SCHEMA="${source_schema}"
  if [[ "$(jq -r '.blocked_reasons | length' <<<"${plan}")" -gt 0 ]]; then
    n2api_apply_receipt "blocked" false "plan_contains_blockers" "Blocked upgrade plan cannot be applied" '{}' '{}' '["Resolve evidence gates and create a new plan"]' || true
    exit "${N2API_EXIT_BLOCKED}"
  fi
  expiry_epoch="$(date -u -d "$(jq -r '.expires_at' <<<"${plan}")" +%s 2>/dev/null || printf 0)"
  now_epoch="$(date -u +%s)"
  if ((expiry_epoch <= now_epoch)); then
    n2api_apply_receipt "blocked" false "plan_expired" "Upgrade plan has expired" '{}' '{}' '["Create a fresh upgrade plan"]' || true
    exit "${N2API_EXIT_BLOCKED}"
  fi
  n2api_check_env_file >/dev/null || {
    n2api_apply_receipt "blocked" false "stale_plan_detected" "Upgrade environment is unavailable" '{}' '{}' '["Create a fresh upgrade plan"]' || true
    exit "${N2API_EXIT_BLOCKED}"
  }
  target_manifest="$(n2api_image_inspect_json "${N2API_APPLY_TARGET_IMAGE}")" || {
    n2api_apply_receipt "blocked" false "candidate_image_unavailable" "Upgrade target is unavailable" '{}' '{}' '[]' || true
    exit "${N2API_EXIT_BLOCKED}"
  }
  set +e
  n2api_lock_acquire "${N2API_APPLY_OPERATION_ID}"
  lock_status=$?
  set -e
  if [[ ${lock_status} -eq 2 ]]; then
    n2api_apply_receipt "contended" false "operation_lock_contended" "Another operator action holds the lock" '{}' '{}' '["Wait for the active operation"]' || true
    exit "${N2API_EXIT_CONTENDED}"
  elif [[ ${lock_status} -ne 0 ]]; then
    n2api_apply_receipt "failed" false "operation_state_unsafe" "Operator lock could not be acquired safely" '{}' '{}' '[]' || true
    exit "${N2API_EXIT_FAILED}"
  fi
  N2API_APPLY_LOCKED=true
  runtime="$(n2api_runtime_snapshot_json)" ||
    n2api_apply_finish "failed" false "runtime_snapshot_failed" "Runtime snapshot failed after acquiring the operation lock" \
      "$(n2api_upgrade_observation_json "runtime_snapshot")" \
      "$(jq -cn --arg image "${N2API_APPLY_TARGET_IMAGE}" '{image:$image}')" '[]' "${N2API_EXIT_FAILED}"
  context="$(n2api_plan_context_json "${runtime}")" ||
    n2api_apply_finish "failed" false "plan_context_failed" "Apply context could not be verified after acquiring the operation lock" \
      "$(n2api_upgrade_observation_json "plan_context" "${runtime}")" \
      "$(jq -cn --arg image "${N2API_APPLY_TARGET_IMAGE}" '{image:$image}')" '[]' "${N2API_EXIT_FAILED}"
  if n2api_upgrade_noop_is_safe "${plan}" "${runtime}" "${target_manifest}"; then
    container="$(jq -r '.n2api.name' <<<"${runtime}")"
    if env N2API_ENV_FILE="${N2API_ENV_FILE}" \
      "${N2API_REPO_ROOT}/dev/verification/verify-release-image.sh" --container "${container}" "${N2API_APPLY_TARGET_IMAGE}" >/dev/null 2>&1; then
      n2api_apply_finish "noop" false "target_already_healthy" "Exact upgrade target is already healthy" \
        "$(jq -cn --arg plan_operation_id "${N2API_APPLY_PLAN_OPERATION_ID}" '{plan_operation_id:$plan_operation_id,stage:"noop"}')" \
        "$(jq -cn --arg image "${N2API_APPLY_TARGET_IMAGE}" '{image:$image}')" '[]' 0
    fi
  fi
  if ! n2api_apply_static_invariants_match "${plan}" "${context}" "${target_manifest}" ||
    ! n2api_apply_runtime_invariants_match "${plan}" "${context}"; then
    n2api_apply_finish "blocked" false "stale_plan_detected" "Upgrade plan invariants changed" \
      "$(n2api_upgrade_observation_json "invariants" "${runtime}")" \
      "$(jq -cn --arg image "${N2API_APPLY_TARGET_IMAGE}" '{image:$image}')" '["Create a fresh upgrade plan"]' "${N2API_EXIT_BLOCKED}"
  fi
  if ! n2api_upgrade_evidence_matches_plan "${plan}"; then
    n2api_apply_finish "blocked" false "stale_plan_detected" "Upgrade backup or restore evidence changed" \
      "$(n2api_upgrade_observation_json "evidence" "${runtime}")" "$(jq -cn --arg image "${N2API_APPLY_TARGET_IMAGE}" '{image:$image}')" \
      '["Create fresh backup and restore evidence, then re-plan"]' "${N2API_EXIT_BLOCKED}"
  fi
  N2API_APPLY_STAGE="image_pull"
  set +e
  n2api_apply_run_tracked docker pull --quiet "${N2API_APPLY_TARGET_IMAGE}" >/dev/null 2>&1
  pull_status=$?
  set -e
  if [[ ${pull_status} -ne 0 ]]; then
    if n2api_status_is_timeout "${pull_status}"; then reason="image_pull_timeout"; exit_code=124; else reason="image_pull_failed"; exit_code=${N2API_EXIT_FAILED}; fi
    n2api_apply_finish "failed" false "${reason}" "Upgrade target image pull failed" \
      "$(n2api_upgrade_observation_json "image_pull")" "$(jq -cn --arg image "${N2API_APPLY_TARGET_IMAGE}" '{image:$image}')" '[]' "${exit_code}"
  fi
  n2api_local_image_digest_matches "${N2API_APPLY_TARGET_IMAGE}" ||
    n2api_apply_finish "failed" false "image_identity_mismatch" "Pulled upgrade image does not match the plan" \
      "$(n2api_upgrade_observation_json "image_pull")" "$(jq -cn --arg image "${N2API_APPLY_TARGET_IMAGE}" '{image:$image}')" '[]' "${N2API_EXIT_FAILED}"
  N2API_APPLY_STAGE="persist_target"
  n2api_env_replace_image "${N2API_APPLY_TARGET_IMAGE}" ||
    n2api_apply_finish "failed" false "environment_update_failed" "Upgrade target could not be persisted atomically" \
      "$(n2api_upgrade_observation_json "persist_target")" "$(jq -cn --arg image "${N2API_APPLY_TARGET_IMAGE}" '{image:$image}')" '[]' "${N2API_EXIT_FAILED}"
  N2API_APPLY_STAGE="compose_apply"
  n2api_compose_command
  set +e
  n2api_apply_run_tracked env N2API_ENV_FILE="${N2API_ENV_FILE}" "${N2API_COMPOSE_CMD[@]}" up --detach --wait n2api
  compose_status=$?
  set -e
  if n2api_status_is_timeout "${compose_status}"; then
    n2api_apply_finish "failed" true "readiness_timeout" "Upgrade exceeded its readiness timeout" \
      "$(n2api_upgrade_observation_json "compose_apply")" "$(jq -cn --arg image "${N2API_APPLY_TARGET_IMAGE}" '{image:$image}')" '["Use a separate rollback plan; automatic rollback is forbidden"]' 124
  elif [[ ${compose_status} -ne 0 ]]; then
    n2api_apply_finish "failed" true "compose_apply_failed" "Upgrade Compose apply failed" \
      "$(n2api_upgrade_observation_json "compose_apply")" "$(jq -cn --arg image "${N2API_APPLY_TARGET_IMAGE}" '{image:$image}')" '["Use a separate rollback plan; automatic rollback is forbidden"]' "${N2API_EXIT_FAILED}"
  fi
  N2API_APPLY_STAGE="verification"
  after="$(n2api_runtime_snapshot_json)" ||
    n2api_apply_finish "failed" true "basic_verification_failed" "Post-upgrade runtime snapshot failed" \
      "$(n2api_upgrade_observation_json "verification")" "$(jq -cn --arg image "${N2API_APPLY_TARGET_IMAGE}" '{image:$image}')" '[]' "${N2API_EXIT_FAILED}"
  container="$(jq -r '.n2api.name // empty' <<<"${after}")"
  if ! n2api_runtime_exact_target_healthy "${after}" "${N2API_APPLY_TARGET_IMAGE}" ||
    [[ -z "${container}" ]] ||
    ! env N2API_ENV_FILE="${N2API_ENV_FILE}" \
      "${N2API_REPO_ROOT}/dev/verification/verify-release-image.sh" --container "${container}" "${N2API_APPLY_TARGET_IMAGE}" >/dev/null 2>&1; then
    n2api_apply_finish "failed" true "basic_verification_failed" "Post-upgrade verification failed" \
      "$(n2api_upgrade_observation_json "verification" "${after}")" "$(jq -cn --arg image "${N2API_APPLY_TARGET_IMAGE}" '{image:$image}')" \
      '["Preserve evidence and use a separate rollback plan if compatibility is proven"]' "${N2API_EXIT_FAILED}"
  fi
  candidate_schema="$(jq -r '.evidence.candidate_restore.schema_version_value // empty' <<<"${plan}")"
  if [[ ! "${candidate_schema}" =~ ^[1-9][0-9]*$ ]] ||
    [[ "$(jq -r '.postgres.schema.value // empty' <<<"${after}")" != "${candidate_schema}" ]]; then
    n2api_apply_finish "failed" true "candidate_schema_mismatch" "Live schema does not match the candidate restore drill" \
      "$(n2api_upgrade_observation_json "verification" "${after}")" "$(jq -cn --arg image "${N2API_APPLY_TARGET_IMAGE}" '{image:$image}')" \
      '["Preserve evidence and investigate migration behavior before rollback"]' "${N2API_EXIT_FAILED}"
  fi
  current="$(jq -cn \
    --arg plan_operation_id "${N2API_APPLY_PLAN_OPERATION_ID}" \
    --arg source_image "${source_image}" \
    --argjson source_schema "${source_schema}" \
    --argjson target_schema "$(jq -r '.postgres.schema.value' <<<"${after}")" \
    '{plan_operation_id:$plan_operation_id,stage:"completed",source_image:$source_image,source_schema:$source_schema,target_schema:$target_schema,basic_verification:"passed"}')"
  target="$(jq -cn --arg image "${N2API_APPLY_TARGET_IMAGE}" --arg rollback_image "${source_image}" '{image:$image,rollback_image:$rollback_image,running_identity:"verified"}')"
  n2api_apply_finish "succeeded" true "upgrade_applied" "Upgrade applied and verified" \
    "${current}" "${target}" '["Complete off-host custody and external ingress owner gates"]' 0
}

n2api_rollback_apply() {
  local plan_path="" plan runtime context target_manifest expiry_epoch now_epoch lock_status source_image source_schema
  local target current after container pull_status compose_status reason exit_code target_schema
  while (($# > 0)); do
    case "$1" in
      --plan) (($# >= 2)) || n2api_usage_error "missing_plan_path"; plan_path=$2; shift 2 ;;
      --restore|--restore-database|--live-restore)
        n2api_emit "rollback.apply" "high_risk" "blocked" false "live_database_restore_unsupported" "Live database restore is not supported" '{}' '{}' '[]' '[]'
        exit "${N2API_EXIT_BLOCKED}"
        ;;
      --delete-volumes|--volumes|--down-volumes)
        n2api_emit "rollback.apply" "high_risk" "blocked" false "volume_deletion_forbidden" "Rollback never deletes production volumes" '{}' '{}' '[]' '[]'
        exit "${N2API_EXIT_BLOCKED}"
        ;;
      *) n2api_usage_error "rollback_apply_accepts_only_plan" ;;
    esac
  done
  [[ -n "${plan_path}" ]] || n2api_usage_error "rollback_apply_requires_plan"
  N2API_APPLY_OPERATION_ID="$(n2api_operation_id)"
  N2API_APPLY_STARTED_AT="$(n2api_now)"
  N2API_APPLY_COMMAND="rollback"
  N2API_APPLY_LIVE_CHANGED=false
  N2API_APPLY_SOURCE_IMAGE=""
  N2API_APPLY_SOURCE_SCHEMA=""
  trap 'n2api_apply_cleanup' EXIT
  trap 'n2api_apply_signal INT 130' INT
  trap 'n2api_apply_signal TERM 143' TERM
  if ! n2api_plan_read "${plan_path}" rollback plan; then
    n2api_apply_receipt "blocked" false "${N2API_PLAN_READ_REASON}" "Rollback plan is unsafe or invalid" '{}' '{}' '["Create a new rollback plan"]' || true
    exit "${N2API_EXIT_BLOCKED}"
  fi
  N2API_APPLY_PLAN_OPERATION_ID="$(jq -r '.operation_id' <<<"${plan}")"
  N2API_APPLY_TARGET_IMAGE="$(jq -r '.target.image.reference // empty' <<<"${plan}")"
  source_image="$(jq -r '.source.runtime.configured_image // empty' <<<"${plan}")"
  source_schema="$(jq -r '.source.runtime.schema_version // empty' <<<"${plan}")"
  target_schema="$(jq -r '.evidence.compatibility.current_schema // empty' <<<"${plan}")"
  N2API_APPLY_SOURCE_IMAGE="${source_image}"
  N2API_APPLY_SOURCE_SCHEMA="${source_schema}"
  if [[ "$(jq -r '.blocked_reasons | length' <<<"${plan}")" -gt 0 ]]; then
    n2api_apply_receipt "blocked" false "plan_contains_blockers" "Blocked rollback plan cannot be applied" '{}' '{}' '["Create a safe rollback plan"]' || true
    exit "${N2API_EXIT_BLOCKED}"
  fi
  if [[ "$(jq -r '.evidence.compatibility.status // empty' <<<"${plan}")" != proven ]] ||
    [[ "$(jq -r '.evidence.compatibility.basis // empty' <<<"${plan}")" != schema_unchanged ]]; then
    n2api_apply_receipt "blocked" false "schema_compatibility_unproven" "Rollback schema compatibility is not proven" '{}' '{}' '[]' || true
    exit "${N2API_EXIT_BLOCKED}"
  fi
  expiry_epoch="$(date -u -d "$(jq -r '.expires_at' <<<"${plan}")" +%s 2>/dev/null || printf 0)"
  now_epoch="$(date -u +%s)"
  if ((expiry_epoch <= now_epoch)); then
    n2api_apply_receipt "blocked" false "plan_expired" "Rollback plan has expired" '{}' '{}' '["Create a fresh rollback plan"]' || true
    exit "${N2API_EXIT_BLOCKED}"
  fi
  n2api_check_env_file >/dev/null || {
    n2api_apply_receipt "blocked" false "stale_plan_detected" "Rollback environment is unavailable" '{}' '{}' '["Create a fresh rollback plan"]' || true
    exit "${N2API_EXIT_BLOCKED}"
  }
  target_manifest="$(n2api_image_inspect_json "${N2API_APPLY_TARGET_IMAGE}")" || {
    n2api_apply_receipt "blocked" false "rollback_previous_image_unavailable" "Rollback target image is unavailable" '{}' '{}' '[]' || true
    exit "${N2API_EXIT_BLOCKED}"
  }
  set +e
  n2api_lock_acquire "${N2API_APPLY_OPERATION_ID}"
  lock_status=$?
  set -e
  if [[ ${lock_status} -eq 2 ]]; then
    n2api_apply_receipt "contended" false "operation_lock_contended" "Another operator action holds the lock" '{}' '{}' '["Wait for the active operation"]' || true
    exit "${N2API_EXIT_CONTENDED}"
  elif [[ ${lock_status} -ne 0 ]]; then
    n2api_apply_receipt "failed" false "operation_state_unsafe" "Operator lock could not be acquired safely" '{}' '{}' '[]' || true
    exit "${N2API_EXIT_FAILED}"
  fi
  N2API_APPLY_LOCKED=true
  runtime="$(n2api_runtime_snapshot_json)" ||
    n2api_apply_finish "failed" false "runtime_snapshot_failed" "Runtime snapshot failed after acquiring the operation lock" \
      "$(n2api_upgrade_observation_json "runtime_snapshot")" "$(jq -cn --arg image "${N2API_APPLY_TARGET_IMAGE}" '{image:$image}')" '[]' "${N2API_EXIT_FAILED}"
  context="$(n2api_plan_context_json "${runtime}")" ||
    n2api_apply_finish "failed" false "plan_context_failed" "Apply context could not be verified after acquiring the operation lock" \
      "$(n2api_upgrade_observation_json "plan_context" "${runtime}")" "$(jq -cn --arg image "${N2API_APPLY_TARGET_IMAGE}" '{image:$image}')" '[]' "${N2API_EXIT_FAILED}"
  if n2api_rollback_noop_is_safe "${plan}" "${runtime}" "${target_manifest}"; then
    container="$(jq -r '.n2api.name' <<<"${runtime}")"
    if env N2API_ENV_FILE="${N2API_ENV_FILE}" \
      "${N2API_REPO_ROOT}/dev/verification/verify-release-image.sh" --container "${container}" "${N2API_APPLY_TARGET_IMAGE}" >/dev/null 2>&1; then
      n2api_apply_finish "noop" false "target_already_healthy" "Exact rollback target is already healthy" \
        "$(jq -cn --arg plan_operation_id "${N2API_APPLY_PLAN_OPERATION_ID}" '{plan_operation_id:$plan_operation_id,stage:"noop"}')" \
        "$(jq -cn --arg image "${N2API_APPLY_TARGET_IMAGE}" '{image:$image}')" '[]' 0
    fi
  fi
  if ! n2api_apply_static_invariants_match "${plan}" "${context}" "${target_manifest}" ||
    ! n2api_apply_runtime_invariants_match "${plan}" "${context}" ||
    ! n2api_previous_receipt_matches_plan "${plan}"; then
    n2api_apply_finish "blocked" false "stale_plan_detected" "Rollback plan invariants or receipt evidence changed" \
      "$(n2api_upgrade_observation_json "invariants" "${runtime}")" "$(jq -cn --arg image "${N2API_APPLY_TARGET_IMAGE}" '{image:$image}')" \
      '["Create a fresh rollback plan"]' "${N2API_EXIT_BLOCKED}"
  fi
  if [[ "${source_schema}" != "${target_schema}" ]] ||
    [[ "$(jq -r '.postgres.schema.value // empty' <<<"${runtime}")" != "${target_schema}" ]]; then
    n2api_apply_finish "blocked" false "schema_compatibility_unproven" "Live schema no longer matches proven rollback compatibility" \
      "$(n2api_upgrade_observation_json "compatibility" "${runtime}")" "$(jq -cn --arg image "${N2API_APPLY_TARGET_IMAGE}" '{image:$image}')" '[]' "${N2API_EXIT_BLOCKED}"
  fi
  N2API_APPLY_STAGE="image_pull"
  set +e
  n2api_apply_run_tracked docker pull --quiet "${N2API_APPLY_TARGET_IMAGE}" >/dev/null 2>&1
  pull_status=$?
  set -e
  if [[ ${pull_status} -ne 0 ]]; then
    if n2api_status_is_timeout "${pull_status}"; then reason="image_pull_timeout"; exit_code=124; else reason="image_pull_failed"; exit_code=${N2API_EXIT_FAILED}; fi
    n2api_apply_finish "failed" false "${reason}" "Rollback target image pull failed" \
      "$(n2api_upgrade_observation_json "image_pull")" "$(jq -cn --arg image "${N2API_APPLY_TARGET_IMAGE}" '{image:$image}')" '[]' "${exit_code}"
  fi
  n2api_local_image_digest_matches "${N2API_APPLY_TARGET_IMAGE}" ||
    n2api_apply_finish "failed" false "image_identity_mismatch" "Pulled rollback image does not match the plan" \
      "$(n2api_upgrade_observation_json "image_pull")" "$(jq -cn --arg image "${N2API_APPLY_TARGET_IMAGE}" '{image:$image}')" '[]' "${N2API_EXIT_FAILED}"
  N2API_APPLY_STAGE="persist_target"
  n2api_env_replace_image "${N2API_APPLY_TARGET_IMAGE}" ||
    n2api_apply_finish "failed" false "environment_update_failed" "Rollback target could not be persisted atomically" \
      "$(n2api_upgrade_observation_json "persist_target")" "$(jq -cn --arg image "${N2API_APPLY_TARGET_IMAGE}" '{image:$image}')" '[]' "${N2API_EXIT_FAILED}"
  N2API_APPLY_STAGE="compose_apply"
  n2api_compose_command
  set +e
  n2api_apply_run_tracked env N2API_ENV_FILE="${N2API_ENV_FILE}" "${N2API_COMPOSE_CMD[@]}" up --detach --wait n2api
  compose_status=$?
  set -e
  if n2api_status_is_timeout "${compose_status}"; then
    n2api_apply_finish "failed" true "readiness_timeout" "Rollback exceeded its readiness timeout" \
      "$(n2api_upgrade_observation_json "compose_apply")" "$(jq -cn --arg image "${N2API_APPLY_TARGET_IMAGE}" '{image:$image}')" '[]' 124
  elif [[ ${compose_status} -ne 0 ]]; then
    n2api_apply_finish "failed" true "compose_apply_failed" "Rollback Compose apply failed" \
      "$(n2api_upgrade_observation_json "compose_apply")" "$(jq -cn --arg image "${N2API_APPLY_TARGET_IMAGE}" '{image:$image}')" '[]' "${N2API_EXIT_FAILED}"
  fi
  N2API_APPLY_STAGE="verification"
  after="$(n2api_runtime_snapshot_json)" ||
    n2api_apply_finish "failed" true "basic_verification_failed" "Post-rollback runtime snapshot failed" \
      "$(n2api_upgrade_observation_json "verification")" "$(jq -cn --arg image "${N2API_APPLY_TARGET_IMAGE}" '{image:$image}')" '[]' "${N2API_EXIT_FAILED}"
  container="$(jq -r '.n2api.name // empty' <<<"${after}")"
  if ! n2api_runtime_exact_target_healthy "${after}" "${N2API_APPLY_TARGET_IMAGE}" ||
    [[ "$(jq -r '.postgres.schema.value // empty' <<<"${after}")" != "${target_schema}" ]] ||
    [[ -z "${container}" ]] ||
    ! env N2API_ENV_FILE="${N2API_ENV_FILE}" \
      "${N2API_REPO_ROOT}/dev/verification/verify-release-image.sh" --container "${container}" "${N2API_APPLY_TARGET_IMAGE}" >/dev/null 2>&1; then
    n2api_apply_finish "failed" true "basic_verification_failed" "Post-rollback verification failed" \
      "$(n2api_upgrade_observation_json "verification" "${after}")" "$(jq -cn --arg image "${N2API_APPLY_TARGET_IMAGE}" '{image:$image}')" \
      '["Preserve evidence; live database restore remains a manual owner decision"]' "${N2API_EXIT_FAILED}"
  fi
  current="$(jq -cn --arg plan_operation_id "${N2API_APPLY_PLAN_OPERATION_ID}" --arg source_image "${source_image}" \
    --argjson source_schema "${source_schema}" --argjson target_schema "${target_schema}" \
    '{plan_operation_id:$plan_operation_id,stage:"completed",source_image:$source_image,source_schema:$source_schema,target_schema:$target_schema,basic_verification:"passed"}')"
  target="$(jq -cn --arg image "${N2API_APPLY_TARGET_IMAGE}" --arg rollback_image "${source_image}" \
    '{image:$image,rollback_image:$rollback_image,running_identity:"verified",database_restore:false,volume_deletion:false}')"
  n2api_apply_finish "succeeded" true "rollback_applied" "Application image rollback applied and verified without database restore" \
    "${current}" "${target}" '["Keep database restore as a separate documented manual path"]' 0
}

n2api_apply_operation() {
  local operation=$1
  shift
  case "${operation}" in
    deploy) n2api_deploy_apply "$@" ;;
    upgrade) n2api_upgrade_apply "$@" ;;
    rollback) n2api_rollback_apply "$@" ;;
    *) n2api_usage_error "unsupported_apply_operation" ;;
  esac
}

#!/usr/bin/env bash

N2API_APPLY_OPERATION_ID=""
N2API_APPLY_STARTED_AT=""
N2API_APPLY_LOCKED=false
N2API_APPLY_CHILD_PID=""
N2API_APPLY_STAGE="initializing"
N2API_APPLY_COMMAND=""
N2API_APPLY_PLAN_OPERATION_ID=""
N2API_APPLY_TARGET_IMAGE=""

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
  local document
  document="$(n2api_envelope_json \
    "${N2API_APPLY_COMMAND}.apply" "service_change" "${status}" "${changed}" "${reason}" "${summary}" \
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
  local receipt_status=0
  trap - EXIT INT TERM
  trap 'n2api_apply_cleanup_signal 130' INT
  trap 'n2api_apply_cleanup_signal 143' TERM
  n2api_apply_receipt "${status}" "${changed}" "${reason}" "${summary}" "${current}" "${target}" "${next_actions}" || receipt_status=1
  n2api_apply_cleanup
  trap - INT TERM
  if [[ ${receipt_status} -ne 0 ]]; then
    n2api_emit "${N2API_APPLY_COMMAND}.apply" "service_change" "failed" "${changed}" \
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
  current="$(jq -cn \
    --arg stage "${N2API_APPLY_STAGE}" \
    --arg signal "${signal}" \
    --arg plan_operation_id "${N2API_APPLY_PLAN_OPERATION_ID}" \
    '{stage:$stage,signal:$signal,plan_operation_id:$plan_operation_id}')"
  n2api_apply_receipt "failed" true "operation_interrupted" "Operation apply was interrupted" \
    "${current}" "$(jq -cn --arg image "${N2API_APPLY_TARGET_IMAGE}" '{image:$image}')" \
    '["Inspect the operation receipt and live stack before retrying"]' || true
  n2api_apply_cleanup
  trap - INT TERM
  exit "${exit_code}"
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
    if "${N2API_REPO_ROOT}/dev/verification/verify-release-image.sh" --container "${container}" "${N2API_APPLY_TARGET_IMAGE}" >/dev/null 2>&1; then
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
    ! "${N2API_REPO_ROOT}/dev/verification/verify-release-image.sh" --container "${container}" "${N2API_APPLY_TARGET_IMAGE}" >/dev/null 2>&1; then
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

n2api_apply_operation() {
  local operation=$1
  shift
  case "${operation}" in
    deploy) n2api_deploy_apply "$@" ;;
    *) n2api_usage_error "unsupported_apply_operation" ;;
  esac
}

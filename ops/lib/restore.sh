#!/usr/bin/env bash

N2API_RESTORE_OPERATION_ID=""
N2API_RESTORE_STARTED_AT=""
N2API_RESTORE_LOCKED=false
N2API_RESTORE_STDOUT=""
N2API_RESTORE_STDERR=""
N2API_RESTORE_ARCHIVE_STAGE=""
N2API_RESTORE_CHILD_PID=""
N2API_RESTORE_STAGE="initializing"

n2api_restore_stop_child() {
  local signal=${1:-TERM} attempts i
  [[ -n "${N2API_RESTORE_CHILD_PID}" ]] || return 0
  kill -s "${signal}" -- "-${N2API_RESTORE_CHILD_PID}" 2>/dev/null ||
    kill -s "${signal}" "${N2API_RESTORE_CHILD_PID}" 2>/dev/null || true
  attempts=$(( $(n2api_timeout_kill_after) * 10 ))
  for ((i = 0; i < attempts; i++)); do
    kill -0 "${N2API_RESTORE_CHILD_PID}" 2>/dev/null || break
    sleep 0.1
  done
  if kill -0 "${N2API_RESTORE_CHILD_PID}" 2>/dev/null; then
    kill -KILL -- "-${N2API_RESTORE_CHILD_PID}" 2>/dev/null ||
      kill -KILL "${N2API_RESTORE_CHILD_PID}" 2>/dev/null || true
  fi
  wait "${N2API_RESTORE_CHILD_PID}" 2>/dev/null || true
  N2API_RESTORE_CHILD_PID=""
}

n2api_restore_cleanup() {
  n2api_restore_stop_child TERM
  [[ -z "${N2API_RESTORE_STDOUT}" ]] || rm -f -- "${N2API_RESTORE_STDOUT}"
  [[ -z "${N2API_RESTORE_STDERR}" ]] || rm -f -- "${N2API_RESTORE_STDERR}"
  [[ -z "${N2API_RESTORE_ARCHIVE_STAGE}" ]] || rm -f -- "${N2API_RESTORE_ARCHIVE_STAGE}"
  if [[ "${N2API_RESTORE_LOCKED}" == true ]]; then
    n2api_lock_release
    N2API_RESTORE_LOCKED=false
  fi
}

n2api_restore_cleanup_signal() {
  local exit_code=$1
  trap - EXIT INT TERM
  n2api_restore_cleanup
  unset admin_password encryption_secret previous_keys
  exit "${exit_code}"
}

n2api_restore_finish_failure() {
  local changed=$1 reason=$2 summary=$3 current=$4 target=$5 next_actions=$6 exit_code=${7:-${N2API_EXIT_FAILED}}
  trap - EXIT INT TERM
  trap 'n2api_restore_cleanup_signal 130' INT
  trap 'n2api_restore_cleanup_signal 143' TERM
  unset admin_password encryption_secret previous_keys
  n2api_restore_receipt "failed" "${changed}" "${reason}" "${summary}" \
    "${current}" "${target}" "${next_actions}" || true
  n2api_restore_cleanup
  trap - INT TERM
  exit "${exit_code}"
}

n2api_restore_receipt() {
  local status=$1 changed=$2 reason=$3 summary=$4 current='{}' target='{}' next_actions='[]'
  local document
  (($# < 5)) || current=$5
  (($# < 6)) || target=$6
  (($# < 7)) || next_actions=$7
  document="$(n2api_envelope_json \
    "restore.drill" "local_write" "${status}" "${changed}" "${reason}" "${summary}" \
    "${current}" "${target}" '[]' "${next_actions}" \
    "${N2API_RESTORE_OPERATION_ID}" "${N2API_RESTORE_STARTED_AT}")"
  n2api_operation_write "${N2API_RESTORE_OPERATION_ID}" "${document}" || return 1
  n2api_emit_document "${document}"
}

n2api_restore_signal() {
  local signal=$1 exit_code=$2 current cleanup_status="unknown"
  trap - EXIT
  trap 'n2api_restore_cleanup_signal 130' INT
  trap 'n2api_restore_cleanup_signal 143' TERM
  n2api_restore_stop_child "${signal}"
  if [[ -f "${N2API_RESTORE_STDERR}" ]]; then
    cleanup_status="$(sed -n 's/^restore_cleanup_status=\(passed\|failed\)$/\1/p' "${N2API_RESTORE_STDERR}" | tail -n 1)"
    [[ -n "${cleanup_status}" ]] || cleanup_status="unknown"
  fi
  current="$(jq -cn --arg stage "${N2API_RESTORE_STAGE}" --arg signal "${signal}" --arg cleanup_status "${cleanup_status}" \
    '{stage:$stage,signal:$signal,cleanup_status:$cleanup_status}')"
  n2api_restore_receipt "failed" false "restore_drill_interrupted" "Restore drill was interrupted" \
    "${current}" '{}' '["Inspect the operation receipt before retrying"]' || true
  n2api_restore_cleanup
  trap - INT TERM
  exit "${exit_code}"
}

n2api_restore_locked_fail() {
  local reason=$1 summary=$2 stage=$3 changed=$4 evidence_class=$5 backup_id=$6 image=$7 next_actions=$8
  local exit_code=${9:-${N2API_EXIT_FAILED}} current target
  current="$(jq -cn \
    --arg stage "${stage}" \
    --arg evidence_class "${evidence_class}" \
    --arg backup_id "${backup_id}" \
    '{stage:$stage,evidence_class:$evidence_class,backup_id:$backup_id}')"
  target="$(jq -cn --arg image "${image}" '{image:$image}')"
  n2api_restore_finish_failure "${changed}" "${reason}" "${summary}" \
    "${current}" "${target}" "${next_actions}" "${exit_code}"
}

n2api_restore_drill() {
  local archive="" image="" evidence_class="" admin_password_file="" encryption_secret_file="" previous_keys_file=""
  local encryption_key_id="default" admin_username="admin" admin_password="" encryption_secret="" previous_keys='[]'
  local lock_status checksum metadata_file metadata backup_id="unavailable" stdout_status failure_stage schema_version restored_secret gateway_status cleanup_status stage_status
  local current target document next_actions
  while (($# > 0)); do
    case "$1" in
      --archive) (($# >= 2)) || n2api_usage_error "missing_restore_archive"; archive=$2; shift 2 ;;
      --image) (($# >= 2)) || n2api_usage_error "missing_restore_image"; image=$2; shift 2 ;;
      --evidence-class) (($# >= 2)) || n2api_usage_error "missing_evidence_class"; evidence_class=$2; shift 2 ;;
      --admin-password-file) (($# >= 2)) || n2api_usage_error "missing_restore_admin_password_file"; admin_password_file=$2; shift 2 ;;
      --encryption-secret-file) (($# >= 2)) || n2api_usage_error "missing_restore_encryption_secret_file"; encryption_secret_file=$2; shift 2 ;;
      --previous-keys-file) (($# >= 2)) || n2api_usage_error "missing_restore_previous_keys_file"; previous_keys_file=$2; shift 2 ;;
      --encryption-key-id) (($# >= 2)) || n2api_usage_error "missing_restore_encryption_key_id"; encryption_key_id=$2; shift 2 ;;
      --admin-username) (($# >= 2)) || n2api_usage_error "missing_restore_admin_username"; admin_username=$2; shift 2 ;;
      *) n2api_usage_error "unknown_restore_drill_option" ;;
    esac
  done
  [[ -n "${archive}" && -n "${image}" && -n "${evidence_class}" && -n "${admin_password_file}" && -n "${encryption_secret_file}" ]] || n2api_usage_error "restore_drill_missing_required_option"
  case "${evidence_class}" in ci_fixture|real_operator) ;; *) n2api_usage_error "invalid_restore_evidence_class" ;; esac
  [[ "${encryption_key_id}" =~ ^[A-Za-z0-9._-]{1,128}$ && "${admin_username}" =~ ^[A-Za-z0-9._-]{1,64}$ ]] || n2api_usage_error "invalid_restore_identity"
  n2api_check_env_file >/dev/null || n2api_usage_error "restore_environment_unavailable"
  n2api_image_reference_is_valid "${image}" || n2api_usage_error "invalid_restore_image"
  command -v setsid >/dev/null 2>&1 || n2api_usage_error "restore_requires_setsid"
  n2api_image_inspect_json "${image}" >/dev/null || {
    n2api_emit "restore.drill" "local_write" "blocked" false "restore_image_unavailable" "Restore image manifest or platform is unavailable" '{}' '{}' '[]' '[]'
    exit "${N2API_EXIT_BLOCKED}"
  }
  [[ -f "${archive}" && ! -L "${archive}" && -r "${archive}" && -s "${archive}" ]] || n2api_usage_error "restore_archive_unsafe"
  n2api_secret_file_is_safe "${admin_password_file}" || n2api_usage_error "restore_admin_password_file_unsafe"
  n2api_secret_file_is_safe "${encryption_secret_file}" || n2api_usage_error "restore_encryption_secret_file_unsafe"
  if [[ -n "${previous_keys_file}" ]]; then
    n2api_secret_file_is_safe "${previous_keys_file}" || n2api_usage_error "restore_previous_keys_file_unsafe"
  fi
  metadata_file=""
  [[ "${archive}" == *.dump ]] && metadata_file="${archive%.dump}.metadata.json"
  if [[ "${evidence_class}" == real_operator ]]; then
    [[ -f "${metadata_file}" && ! -L "${metadata_file}" ]] || n2api_usage_error "real_restore_requires_backup_metadata"
  fi

  N2API_RESTORE_OPERATION_ID="$(n2api_operation_id)"
  N2API_RESTORE_STARTED_AT="$(n2api_now)"
  set +e
  n2api_lock_acquire "${N2API_RESTORE_OPERATION_ID}"
  lock_status=$?
  set -e
  if [[ ${lock_status} -eq 2 ]]; then
    n2api_restore_receipt "contended" false "operation_lock_contended" "Another operator action holds the lock" \
      '{"stage":"lock"}' "$(jq -cn --arg image "${image}" '{image:$image}')" '["Wait for the recorded operation to finish"]' || true
    unset admin_password encryption_secret previous_keys
    exit "${N2API_EXIT_CONTENDED}"
  elif [[ ${lock_status} -ne 0 ]]; then
    unset admin_password encryption_secret previous_keys
    n2api_emit "restore.drill" "local_write" "failed" false "operation_state_unsafe" "Operator state could not be initialized safely" '{}' '{}' '[]' '[]'
    exit "${N2API_EXIT_FAILED}"
  fi
  N2API_RESTORE_LOCKED=true
  trap 'n2api_restore_cleanup; unset admin_password encryption_secret previous_keys' EXIT
  trap 'n2api_restore_signal INT 130' INT
  trap 'n2api_restore_signal TERM 143' TERM
  N2API_RESTORE_STDOUT="$(mktemp "${N2API_STATE_DIR}/.restore-stdout.XXXXXX")" ||
    n2api_restore_locked_fail "restore_temp_create_failed" "Restore output capture could not be created" \
      "initializing" false "${evidence_class}" "${backup_id}" "${image}" '["Repair operator state and retry"]'
  N2API_RESTORE_STDERR="$(mktemp "${N2API_STATE_DIR}/.restore-stderr.XXXXXX")" ||
    n2api_restore_locked_fail "restore_temp_create_failed" "Restore diagnostic capture could not be created" \
      "initializing" false "${evidence_class}" "${backup_id}" "${image}" '["Repair operator state and retry"]'
  N2API_RESTORE_ARCHIVE_STAGE="$(mktemp "${N2API_STATE_DIR}/.restore-archive.XXXXXX.dump")" ||
    n2api_restore_locked_fail "restore_temp_create_failed" "Restore archive staging could not be created" \
      "initializing" false "${evidence_class}" "${backup_id}" "${image}" '["Repair operator state and retry"]'
  chmod 600 -- "${N2API_RESTORE_STDOUT}" "${N2API_RESTORE_STDERR}" "${N2API_RESTORE_ARCHIVE_STAGE}" ||
    n2api_restore_locked_fail "restore_temp_permissions_failed" "Restore temporary files could not be protected" \
      "initializing" false "${evidence_class}" "${backup_id}" "${image}" '["Repair operator state and retry"]'

  N2API_RESTORE_STAGE="archive_stage"
  cp --reflink=auto -- "${archive}" "${N2API_RESTORE_ARCHIVE_STAGE}" ||
    n2api_restore_locked_fail "restore_archive_stage_failed" "Restore archive could not be staged safely" \
      "archive_stage" false "${evidence_class}" "${backup_id}" "${image}" '["Check state storage capacity and retry"]'
  checksum="$(sha256sum -- "${N2API_RESTORE_ARCHIVE_STAGE}" | awk '{print $1}')" ||
    n2api_restore_locked_fail "restore_archive_checksum_failed" "Restore archive checksum could not be calculated" \
      "archive_stage" false "${evidence_class}" "${backup_id}" "${image}" '["Verify the archive and retry"]'
  [[ "${checksum}" =~ ^[0-9a-f]{64}$ ]] ||
    n2api_restore_locked_fail "restore_archive_checksum_failed" "Restore archive checksum is invalid" \
      "archive_stage" false "${evidence_class}" "${backup_id}" "${image}" '["Verify the archive and retry"]'
  if [[ "${evidence_class}" == real_operator ]]; then
    metadata="$(n2api_backup_metadata_json "${metadata_file}" "${N2API_RESTORE_ARCHIVE_STAGE}" "$(basename -- "${archive}")")" ||
      n2api_restore_locked_fail "real_restore_backup_metadata_invalid" "Real operator backup metadata is invalid" \
        "archive_stage" false "${evidence_class}" "${backup_id}" "${image}" '["Use a signed real-operator backup"]'
    [[ "$(jq -r '.checksum' <<<"${metadata}")" == "${checksum}" ]] ||
      n2api_restore_locked_fail "real_restore_backup_checksum_mismatch" "Real operator backup checksum does not match" \
        "archive_stage" false "${evidence_class}" "${backup_id}" "${image}" '["Use the archive bound to this metadata"]'
    [[ "$(jq -r '.evidence_class' <<<"${metadata}")" == "real_operator" ]] ||
      n2api_restore_locked_fail "fixture_restore_cannot_be_promoted" "Fixture backup cannot become real operator evidence" \
        "archive_stage" false "${evidence_class}" "${backup_id}" "${image}" '["Create and restore a real operator backup"]'
    n2api_backup_metadata_hmac_valid "${metadata}" ||
      n2api_restore_locked_fail "real_restore_backup_integrity_failed" "Real operator backup metadata integrity validation failed" \
        "archive_stage" false "${evidence_class}" "${backup_id}" "${image}" '["Use metadata signed by this operator state"]'
    backup_id="$(jq -r '.backup_id' <<<"${metadata}")"
  else
    backup_id="fixture-${checksum:0:12}"
  fi

  N2API_RESTORE_STAGE="image_pull"
  set +e
  setsid timeout --signal=TERM --kill-after="$(n2api_timeout_kill_after)" \
    "${N2API_TIMEOUT_SECONDS}" docker pull --quiet "${image}" >/dev/null 2>&1 &
  N2API_RESTORE_CHILD_PID=$!
  wait "${N2API_RESTORE_CHILD_PID}" 2>/dev/null
  stage_status=$?
  N2API_RESTORE_CHILD_PID=""
  set -e
  if n2api_status_is_timeout "${stage_status}"; then
    n2api_restore_locked_fail "restore_image_pull_timeout" "Restore image pull exceeded its timeout" \
      "image_pull" false "${evidence_class}" "${backup_id}" "${image}" '["Retry after verifying registry access"]' 124
  elif [[ ${stage_status} -ne 0 ]]; then
    n2api_restore_locked_fail "restore_image_pull_failed" "Restore image could not be pulled" \
      "image_pull" false "${evidence_class}" "${backup_id}" "${image}" '["Retry after verifying registry access"]'
  fi

  N2API_RESTORE_STAGE="archive_list"
  set +e
  n2api_backup_verify_archive_internal "${N2API_RESTORE_ARCHIVE_STAGE}"
  stage_status=$?
  set -e
  if n2api_status_is_timeout "${stage_status}"; then
    n2api_restore_locked_fail "restore_archive_list_timeout" "Restore archive validation exceeded its timeout" \
      "archive_list" false "${evidence_class}" "${backup_id}" "${image}" '["Verify the archive before retrying"]' 124
  elif [[ ${stage_status} -ne 0 ]]; then
    n2api_restore_locked_fail "restore_archive_invalid" "Restore archive validation failed" \
      "archive_list" false "${evidence_class}" "${backup_id}" "${image}" '["Verify the archive before retrying"]'
  fi

  N2API_RESTORE_STAGE="secrets"
  admin_password="$(n2api_read_protected_secret "${admin_password_file}")" ||
    n2api_restore_locked_fail "restore_secret_file_changed" "Restore admin password file became unsafe" \
      "secrets" false "${evidence_class}" "${backup_id}" "${image}" '["Repair protected secret files before retrying"]'
  encryption_secret="$(n2api_read_protected_secret "${encryption_secret_file}")" ||
    n2api_restore_locked_fail "restore_secret_file_changed" "Restore encryption secret file became unsafe" \
      "secrets" false "${evidence_class}" "${backup_id}" "${image}" '["Repair protected secret files before retrying"]'
  if [[ -n "${previous_keys_file}" ]]; then
    previous_keys="$(n2api_read_protected_secret "${previous_keys_file}")" ||
      n2api_restore_locked_fail "restore_secret_file_changed" "Restore previous-keys file became unsafe" \
        "secrets" false "${evidence_class}" "${backup_id}" "${image}" '["Repair protected secret files before retrying"]'
    jq -e 'type == "array" and all(.[]; (.id | type == "string") and (.secret | type == "string"))' <<<"${previous_keys}" >/dev/null ||
      n2api_restore_locked_fail "restore_previous_keys_invalid" "Restore previous-keys content is invalid" \
        "secrets" false "${evidence_class}" "${backup_id}" "${image}" '["Repair protected secret files before retrying"]'
  fi

  N2API_RESTORE_STAGE="isolated_restore"
  set +e
  env \
    N2API_RESTORE_IMAGE="${image}" \
    N2API_RESTORE_ADMIN_USERNAME="${admin_username}" \
    N2API_RESTORE_ADMIN_PASSWORD="${admin_password}" \
    N2API_RESTORE_ENCRYPTION_SECRET="${encryption_secret}" \
    N2API_RESTORE_ENCRYPTION_KEY_ID="${encryption_key_id}" \
    N2API_RESTORE_ENCRYPTION_PREVIOUS_KEYS="${previous_keys}" \
    N2API_DEV_CACHE_ROOT="${N2API_STATE_DIR}/restore-runtime" \
    N2API_RESOURCE_LOCK_FILE="${N2API_STATE_DIR}/locks/restore-resources.lock" \
    setsid timeout --signal=TERM --kill-after="$(n2api_timeout_kill_after)" "${N2API_TIMEOUT_SECONDS}" \
      "${N2API_REPO_ROOT}/dev/verification/restore-backup.sh" "${N2API_RESTORE_ARCHIVE_STAGE}" \
        >"${N2API_RESTORE_STDOUT}" 2>"${N2API_RESTORE_STDERR}" &
  N2API_RESTORE_CHILD_PID=$!
  wait "${N2API_RESTORE_CHILD_PID}" 2>/dev/null
  stdout_status=$?
  N2API_RESTORE_CHILD_PID=""
  set -e
  unset admin_password encryption_secret previous_keys
  cleanup_status="$(sed -n 's/^restore_cleanup_status=\(passed\|failed\)$/\1/p' "${N2API_RESTORE_STDERR}" | tail -n 1)"
  [[ -n "${cleanup_status}" ]] || cleanup_status="unknown"
  if n2api_status_is_timeout "${stdout_status}"; then
    n2api_restore_finish_failure true "restore_drill_timeout" "Isolated restore drill exceeded its timeout" \
      "$(jq -cn --arg evidence_class "${evidence_class}" --arg backup_id "${backup_id}" --arg cleanup_status "${cleanup_status}" \
        '{stage:"isolated_restore",evidence_class:$evidence_class,backup_id:$backup_id,cleanup_status:$cleanup_status}')" \
      "$(jq -cn --arg image "${image}" '{image:$image}')" '["Inspect cleanup status and retry with an appropriate timeout"]' 124
  fi
  if [[ ${stdout_status} -ne 0 ]]; then
    failure_stage="$(sed -n 's/^restore_status=failed stage=\([a-z_]*\)$/\1/p' "${N2API_RESTORE_STDERR}" | tail -n 1)"
    [[ -n "${failure_stage}" ]] || failure_stage="unknown"
    if [[ "${cleanup_status}" == failed ]]; then
      failure_stage="cleanup"
    fi
    n2api_restore_finish_failure true "restore_drill_failed" "Isolated restore drill failed" \
      "$(jq -cn --arg stage "${failure_stage}" --arg evidence_class "${evidence_class}" --arg backup_id "${backup_id}" --arg cleanup_status "${cleanup_status}" \
        '{stage:$stage,evidence_class:$evidence_class,backup_id:$backup_id,cleanup_status:$cleanup_status}')" \
      "$(jq -cn --arg image "${image}" '{image:$image}')" '["Inspect the sanitized failure stage and preserve the live stack"]'
  fi

  schema_version="$(sed -n 's/^schema_version=\([0-9][0-9]*\)$/\1/p' "${N2API_RESTORE_STDOUT}" | tail -n 1)"
  restored_secret="$(sed -n 's/^restored_secret_check=\([a-z_]*\)$/\1/p' "${N2API_RESTORE_STDOUT}" | tail -n 1)"
  gateway_status="$(sed -n 's/^gateway_status=\([a-z]*\)$/\1/p' "${N2API_RESTORE_STDOUT}" | tail -n 1)"
  if ! grep -Fxq 'restore_status=passed' "${N2API_RESTORE_STDOUT}" ||
    [[ ! "${schema_version}" =~ ^[1-9][0-9]*$ ]] ||
    [[ "${restored_secret}" != passed && "${restored_secret}" != skipped_no_reusable_key ]] ||
    [[ "${gateway_status}" != passed ]] ||
    [[ "${cleanup_status}" != passed ]]; then
    n2api_restore_finish_failure true "restore_evidence_invalid" "Restore drill output did not satisfy the evidence contract" \
      "$(jq -cn --arg evidence_class "${evidence_class}" --arg backup_id "${backup_id}" '{stage:"evidence",evidence_class:$evidence_class,backup_id:$backup_id}')" \
      "$(jq -cn --arg image "${image}" '{image:$image}')" '["Inspect the restore implementation before retrying"]'
  fi

  current="$(jq -cn \
    --arg schema_version "n2api.ops.restore/v1" \
    --arg operation_id "${N2API_RESTORE_OPERATION_ID}" \
    --arg evidence_class "${evidence_class}" \
    --arg backup_id "${backup_id}" \
    --arg archive_checksum "${checksum}" \
    --arg image "${image}" \
    --argjson schema_version_value "${schema_version}" \
    --arg restored_secret_status "${restored_secret}" \
    --arg finished_at "$(n2api_now)" \
    '{
      schema_version:$schema_version,
      operation_id:$operation_id,
      evidence_class:$evidence_class,
      backup_id:$backup_id,
      archive_checksum:$archive_checksum,
      image:$image,
      schema_version_value:$schema_version_value,
      archive_list_status:"passed",
      restore_status:"passed",
      readiness_status:"passed",
      restored_secret_status:$restored_secret_status,
      gateway_status:"passed",
      cleanup_status:"passed",
      finished_at:$finished_at
    }')"
  target="$(jq -cn --arg image "${image}" '{image:$image}')"
  if [[ "${evidence_class}" == real_operator ]]; then
    next_actions='["Record owner sign-off separately for this real operator evidence"]'
  else
    next_actions='["Fixture evidence cannot satisfy a real-operator restore gate"]'
  fi
  document="$(n2api_envelope_json \
    "restore.drill" "local_write" "succeeded" true \
    "restore_drill_passed" "Isolated restore drill passed and cleaned up" \
    "${current}" "${target}" '[]' "${next_actions}" \
    "${N2API_RESTORE_OPERATION_ID}" "${N2API_RESTORE_STARTED_AT}")"
  n2api_operation_write "${N2API_RESTORE_OPERATION_ID}" "${document}" || {
    trap - EXIT INT TERM
    n2api_restore_cleanup
    n2api_emit "restore.drill" "local_write" "failed" true "operation_receipt_failed" "Restore passed but its operation receipt could not be written" \
      "${current}" "${target}" '[]' '["Preserve external evidence and repair operator state"]'
    exit "${N2API_EXIT_FAILED}"
  }
  trap - EXIT INT TERM
  n2api_restore_cleanup
  n2api_emit_document "${document}"
}

n2api_restore() {
  (($# >= 1)) || n2api_usage_error "restore_requires_command"
  local subcommand=$1
  shift
  case "${subcommand}" in
    drill) n2api_restore_drill "$@" ;;
    *) n2api_usage_error "unknown_restore_command" ;;
  esac
}

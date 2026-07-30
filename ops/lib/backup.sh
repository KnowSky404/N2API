#!/usr/bin/env bash

N2API_BACKUP_OPERATION_ID=""
N2API_BACKUP_STARTED_AT=""
N2API_BACKUP_STAGE="initializing"
N2API_BACKUP_TEMP=""
N2API_BACKUP_METADATA_TEMP=""
N2API_BACKUP_FINAL=""
N2API_BACKUP_METADATA_FINAL=""
N2API_BACKUP_LOCKED=false
N2API_BACKUP_PUBLISHED=false
N2API_BACKUP_ARCHIVE_TARGET_OWNED=false
N2API_BACKUP_METADATA_TARGET_OWNED=false
N2API_BACKUP_CHILD_PID=""

n2api_backup_stop_child() {
  local signal=${1:-TERM} attempts i
  [[ -n "${N2API_BACKUP_CHILD_PID}" ]] || return 0
  kill -s "${signal}" -- "-${N2API_BACKUP_CHILD_PID}" 2>/dev/null ||
    kill -s "${signal}" "${N2API_BACKUP_CHILD_PID}" 2>/dev/null || true
  attempts=$(( $(n2api_timeout_kill_after) * 10 ))
  for ((i = 0; i < attempts; i++)); do
    kill -0 "${N2API_BACKUP_CHILD_PID}" 2>/dev/null || break
    sleep 0.1
  done
  if kill -0 "${N2API_BACKUP_CHILD_PID}" 2>/dev/null; then
    kill -KILL -- "-${N2API_BACKUP_CHILD_PID}" 2>/dev/null ||
      kill -KILL "${N2API_BACKUP_CHILD_PID}" 2>/dev/null || true
  fi
  wait "${N2API_BACKUP_CHILD_PID}" 2>/dev/null || true
  N2API_BACKUP_CHILD_PID=""
}

n2api_backup_receipt() {
  local status=$1 changed=$2 reason=$3 summary=$4 current='{}' artifacts='[]' next_actions='[]'
  local document
  (($# < 5)) || current=$5
  (($# < 6)) || artifacts=$6
  (($# < 7)) || next_actions=$7
  document="$(n2api_envelope_json \
    "backup.create" "local_write" "${status}" "${changed}" "${reason}" "${summary}" \
    "${current}" '{}' "${artifacts}" "${next_actions}" \
    "${N2API_BACKUP_OPERATION_ID}" "${N2API_BACKUP_STARTED_AT}")"
  n2api_operation_write "${N2API_BACKUP_OPERATION_ID}" "${document}" || return 1
  n2api_emit_document "${document}"
}

n2api_backup_cleanup() {
  n2api_backup_stop_child TERM
  if [[ "${N2API_BACKUP_PUBLISHED}" != true ]]; then
    [[ -z "${N2API_BACKUP_TEMP}" ]] || rm -f -- "${N2API_BACKUP_TEMP}"
    [[ -z "${N2API_BACKUP_METADATA_TEMP}" ]] || rm -f -- "${N2API_BACKUP_METADATA_TEMP}"
    if [[ "${N2API_BACKUP_ARCHIVE_TARGET_OWNED}" == true && -n "${N2API_BACKUP_FINAL}" ]]; then
      rm -f -- "${N2API_BACKUP_FINAL}"
    fi
    if [[ "${N2API_BACKUP_METADATA_TARGET_OWNED}" == true && -n "${N2API_BACKUP_METADATA_FINAL}" ]]; then
      rm -f -- "${N2API_BACKUP_METADATA_FINAL}"
    fi
  fi
  if [[ "${N2API_BACKUP_LOCKED}" == true ]]; then
    n2api_lock_release
    N2API_BACKUP_LOCKED=false
  fi
}

n2api_backup_cleanup_signal() {
  local exit_code=$1
  trap - EXIT INT TERM
  n2api_backup_cleanup
  exit "${exit_code}"
}

n2api_backup_finish_failure() {
  local changed=$1 reason=$2 summary=$3 current=$4 exit_code=${5:-${N2API_EXIT_FAILED}}
  trap - EXIT INT TERM
  trap 'n2api_backup_cleanup_signal 130' INT
  trap 'n2api_backup_cleanup_signal 143' TERM
  n2api_backup_receipt "failed" "${changed}" "${reason}" "${summary}" \
    "${current}" '[]' '["Inspect the operation receipt before retrying"]' || true
  n2api_backup_cleanup
  trap - INT TERM
  exit "${exit_code}"
}

n2api_backup_signal() {
  local signal=$1 exit_code=$2 current changed=false
  trap - EXIT
  trap 'n2api_backup_cleanup_signal 130' INT
  trap 'n2api_backup_cleanup_signal 143' TERM
  n2api_backup_stop_child "${signal}"
  [[ "${N2API_BACKUP_PUBLISHED}" != true ]] || changed=true
  current="$(jq -cn --arg stage "${N2API_BACKUP_STAGE}" --arg signal "${signal}" '{stage:$stage,signal:$signal}')"
  n2api_backup_receipt "failed" "${changed}" "backup_interrupted" "Backup was interrupted" \
    "${current}" '[]' '["Inspect the operation receipt before retrying"]' || true
  n2api_backup_cleanup
  trap - INT TERM
  exit "${exit_code}"
}

n2api_backup_fail() {
  local reason=$1 summary=$2 exit_code=${3:-${N2API_EXIT_FAILED}} current
  current="$(jq -cn --arg stage "${N2API_BACKUP_STAGE}" '{stage:$stage}')"
  n2api_backup_finish_failure false "${reason}" "${summary}" "${current}" "${exit_code}"
}

n2api_backup_prepare_directory() {
  local directory=$1 parent mode owner
  [[ "${directory}" != /var/lib/docker/volumes/* && "${directory}" != */var/lib/postgresql* ]] || return 1
  if [[ -e "${directory}" ]]; then
    [[ -d "${directory}" && ! -L "${directory}" ]] || return 1
    owner="$(n2api_path_owner "${directory}")" || return 1
    mode="$(n2api_path_mode "${directory}")" || return 1
    [[ "${owner}" == "$(id -u)" ]] || return 1
    ! n2api_mode_has_group_or_other_write "${mode}" || return 1
    [[ -w "${directory}" ]]
    return
  fi
  parent="$(dirname -- "${directory}")"
  [[ -d "${parent}" && ! -L "${parent}" && -w "${parent}" ]] || return 1
  mkdir -- "${directory}"
  chmod 700 -- "${directory}"
}

n2api_backup_verify_archive_internal() {
  local archive=$1 postgres_container postgres_image
  [[ -f "${archive}" && ! -L "${archive}" && -r "${archive}" && -s "${archive}" ]] || return 1
  postgres_container="$(n2api_compose ps --quiet postgres 2>/dev/null || true)"
  if [[ -n "${postgres_container}" ]]; then
    n2api_compose exec --no-TTY postgres pg_restore --list <"${archive}" >/dev/null 2>&1
    return
  fi
  postgres_image="$(n2api_compose config --images 2>/dev/null | awk '/^postgres:/ {print; exit}')"
  [[ -n "${postgres_image}" ]] || return 1
  n2api_run_timeout docker run --rm --pull never -i \
    --entrypoint pg_restore "${postgres_image}" --list <"${archive}" >/dev/null 2>&1
}

n2api_backup_metadata_json() {
  local metadata_file=$1 archive=$2 expected_archive_name=${3:-} metadata archive_name archive_size source_image
  [[ -f "${metadata_file}" && ! -L "${metadata_file}" && -r "${metadata_file}" ]] || return 1
  metadata="$(jq -ce 'select(
    ((keys | sort) == ([
      "archive", "backup_id", "checksum", "created_at", "evidence_class",
      "integrity_hmac", "off_host_status", "operation_id", "schema_version",
      "size_bytes", "source_image", "source_schema_version", "verified"
    ] | sort)) and
    .schema_version == "n2api.ops.backup/v1" and
    (.operation_id | type == "string" and test("^op-[0-9]{8}T[0-9]{6}Z-[0-9a-f]{12}$")) and
    (.backup_id | type == "string" and test("^backup-[0-9]{8}T[0-9]{6}Z-[0-9a-f]{12}$")) and
    (.created_at | type == "string") and
    (.archive | type == "string" and test("^n2api-[0-9]{8}T[0-9]{6}Z-[0-9a-f]{12}\\.dump$")) and
    (.size_bytes | type == "number" and . >= 1 and floor == .) and
    (.checksum | type == "string" and test("^[0-9a-f]{64}$")) and
    (.source_image | type == "string") and
    (.source_schema_version | type == "number" and . >= 1 and floor == .) and
    (.evidence_class == "ci_fixture" or .evidence_class == "real_operator") and
    .verified == true and
    .off_host_status == "attention_missing" and
    (.integrity_hmac | type == "string" and test("^[0-9a-f]{64}$"))
  )' "${metadata_file}" 2>/dev/null)" || return 1
  archive_name="$(jq -r '.archive' <<<"${metadata}")"
  archive_size="$(stat -c '%s' -- "${archive}")" || return 1
  source_image="$(jq -r '.source_image' <<<"${metadata}")"
  [[ -n "${expected_archive_name}" ]] || expected_archive_name="$(basename -- "${archive}")"
  [[ "${archive_name}" == "${expected_archive_name}" ]] || return 1
  [[ "$(jq -r '.size_bytes' <<<"${metadata}")" == "${archive_size}" ]] || return 1
  n2api_image_reference_is_valid "${source_image}" || return 1
  jq -c . <<<"${metadata}"
}

n2api_backup_metadata_hmac_valid() {
  local metadata=$1 expected actual
  expected="$(jq -r '.integrity_hmac // empty' <<<"${metadata}")"
  actual="$(n2api_state_hmac_json "${metadata}")" || return 1
  [[ -n "${expected}" && "${actual}" == "${expected}" ]]
}

n2api_backup_create() {
  local evidence_class="real_operator"
  while (($# > 0)); do
    case "$1" in
      --evidence-class) (($# >= 2)) || n2api_usage_error "missing_backup_evidence_class"; evidence_class=$2; shift 2 ;;
      *) n2api_usage_error "unknown_backup_create_option" ;;
    esac
  done
  case "${evidence_class}" in ci_fixture|real_operator) ;; *) n2api_usage_error "invalid_backup_evidence_class" ;; esac
  local backup_dir runtime source_image source_schema timestamp suffix archive_name metadata_name
  local checksum size metadata integrity_hmac current artifacts document lock_status user database child_status archive_status
  n2api_checks_reset
  n2api_check_env_file || {
    n2api_emit "backup.create" "local_write" "failed" false \
      "backup_configuration_unavailable" "Backup could not load a safe environment file" '{}' '{}' '[]' '[]'
    exit "${N2API_EXIT_FAILED}"
  }
  backup_dir="$(n2api_backup_directory)"
  n2api_backup_prepare_directory "${backup_dir}" || {
    n2api_emit "backup.create" "local_write" "failed" false \
      "backup_directory_unsafe" "Backup directory is unsafe" '{}' '{}' '[]' '[]'
    exit "${N2API_EXIT_FAILED}"
  }
  N2API_BACKUP_OPERATION_ID="$(n2api_operation_id)"
  N2API_BACKUP_STARTED_AT="$(n2api_now)"
  set +e
  n2api_lock_acquire "${N2API_BACKUP_OPERATION_ID}"
  lock_status=$?
  set -e
  if [[ ${lock_status} -eq 2 ]]; then
    N2API_BACKUP_LOCKED=false
    n2api_backup_receipt "contended" false "operation_lock_contended" "Another operator action holds the lock" \
      '{"stage":"lock"}' '[]' '["Wait for the recorded operation to finish"]' || true
    exit "${N2API_EXIT_CONTENDED}"
  elif [[ ${lock_status} -ne 0 ]]; then
    n2api_emit "backup.create" "local_write" "failed" false \
      "operation_state_unsafe" "Operator state could not be initialized safely" '{}' '{}' '[]' '[]'
    exit "${N2API_EXIT_FAILED}"
  fi
  N2API_BACKUP_LOCKED=true
  trap 'n2api_backup_cleanup' EXIT
  trap 'n2api_backup_signal INT 130' INT
  trap 'n2api_backup_signal TERM 143' TERM

  N2API_BACKUP_STAGE="runtime"
  runtime="$(n2api_runtime_snapshot_json)" || n2api_backup_fail "runtime_snapshot_failed" "Runtime snapshot failed"
  source_image="$(jq -r '.n2api.configured_image // empty' <<<"${runtime}")"
  source_schema="$(jq -r '.postgres.schema.value // empty' <<<"${runtime}")"
  n2api_image_reference_is_valid "${source_image}" || n2api_backup_fail "running_image_not_immutable" "Running image is not an exact production release"
  [[ "${source_schema}" =~ ^[1-9][0-9]*$ ]] || n2api_backup_fail "schema_version_unavailable" "Database schema version is unavailable"

  timestamp="$(date -u +'%Y%m%dT%H%M%SZ')"
  suffix=${N2API_BACKUP_OPERATION_ID##*-}
  archive_name="n2api-${timestamp}-${suffix}.dump"
  metadata_name="n2api-${timestamp}-${suffix}.metadata.json"
  N2API_BACKUP_TEMP="$(mktemp "${backup_dir}/.n2api-${timestamp}.XXXXXX.dump.tmp")" ||
    n2api_backup_fail "backup_temp_create_failed" "Backup temporary archive could not be created"
  N2API_BACKUP_FINAL="${backup_dir}/${archive_name}"
  N2API_BACKUP_METADATA_FINAL="${backup_dir}/${metadata_name}"
  [[ ! -e "${N2API_BACKUP_FINAL}" && ! -e "${N2API_BACKUP_METADATA_FINAL}" ]] || n2api_backup_fail "backup_target_exists" "Backup target already exists"

  N2API_BACKUP_STAGE="dump"
  user="$(n2api_env_get POSTGRES_USER n2api)"
  database="$(n2api_env_get POSTGRES_DB n2api)"
  n2api_compose_command
  set +e
  setsid timeout --signal=TERM --kill-after="$(n2api_timeout_kill_after)" "${N2API_TIMEOUT_SECONDS}" \
    env N2API_ENV_FILE="${N2API_ENV_FILE}" "${N2API_COMPOSE_CMD[@]}" \
      exec --no-TTY postgres pg_dump \
      --username "${user}" \
      --dbname "${database}" \
      --format=custom \
      --no-owner \
      --no-privileges >"${N2API_BACKUP_TEMP}" &
  N2API_BACKUP_CHILD_PID=$!
  wait "${N2API_BACKUP_CHILD_PID}" 2>/dev/null
  child_status=$?
  N2API_BACKUP_CHILD_PID=""
  set -e
  if n2api_status_is_timeout "${child_status}"; then
    n2api_backup_fail "backup_dump_timeout" "PostgreSQL backup exceeded its timeout" 124
  elif [[ ${child_status} -ne 0 ]]; then
    n2api_backup_fail "backup_dump_failed" "PostgreSQL backup failed"
  fi
  [[ -s "${N2API_BACKUP_TEMP}" ]] || n2api_backup_fail "backup_archive_empty" "PostgreSQL backup archive is empty"

  N2API_BACKUP_STAGE="archive_list"
  set +e
  n2api_backup_verify_archive_internal "${N2API_BACKUP_TEMP}"
  archive_status=$?
  set -e
  if n2api_status_is_timeout "${archive_status}"; then
    n2api_backup_fail "backup_archive_list_timeout" "PostgreSQL backup archive validation exceeded its timeout" 124
  elif [[ ${archive_status} -ne 0 ]]; then
    n2api_backup_fail "backup_archive_invalid" "PostgreSQL backup archive validation failed"
  fi
  checksum="$(sha256sum -- "${N2API_BACKUP_TEMP}" | awk '{print $1}')" ||
    n2api_backup_fail "backup_metadata_invalid" "Backup checksum could not be calculated"
  size="$(stat -c '%s' -- "${N2API_BACKUP_TEMP}")" ||
    n2api_backup_fail "backup_metadata_invalid" "Backup size could not be calculated"
  [[ "${checksum}" =~ ^[0-9a-f]{64}$ && "${size}" =~ ^[1-9][0-9]*$ ]] || n2api_backup_fail "backup_metadata_invalid" "Backup metadata could not be calculated"
  chmod 600 -- "${N2API_BACKUP_TEMP}" || n2api_backup_fail "backup_archive_permissions_failed" "Backup archive permissions could not be protected"

  N2API_BACKUP_STAGE="metadata"
  metadata="$(jq -cn \
    --arg schema_version "n2api.ops.backup/v1" \
    --arg operation_id "${N2API_BACKUP_OPERATION_ID}" \
    --arg backup_id "backup-${timestamp}-${suffix}" \
    --arg created_at "${N2API_BACKUP_STARTED_AT}" \
    --arg archive "${archive_name}" \
    --argjson size_bytes "${size}" \
    --arg checksum "${checksum}" \
    --arg source_image "${source_image}" \
    --argjson source_schema_version "${source_schema}" \
    --arg evidence_class "${evidence_class}" \
    '{
      schema_version:$schema_version,
      operation_id:$operation_id,
      backup_id:$backup_id,
      created_at:$created_at,
      archive:$archive,
      size_bytes:$size_bytes,
      checksum:$checksum,
      source_image:$source_image,
      source_schema_version:$source_schema_version,
      evidence_class:$evidence_class,
      verified:true,
      off_host_status:"attention_missing"
    }')"
  integrity_hmac="$(n2api_state_hmac_json "${metadata}")" || n2api_backup_fail "backup_metadata_signing_failed" "Backup metadata could not be signed"
  metadata="$(jq -ce --arg integrity_hmac "${integrity_hmac}" '. + {integrity_hmac:$integrity_hmac}' <<<"${metadata}")" ||
    n2api_backup_fail "backup_metadata_invalid" "Backup metadata is invalid"
  N2API_BACKUP_METADATA_TEMP="$(mktemp "${backup_dir}/.n2api-${timestamp}.XXXXXX.metadata.tmp")" ||
    n2api_backup_fail "backup_temp_create_failed" "Backup temporary metadata could not be created"
  jq -ce . <<<"${metadata}" >"${N2API_BACKUP_METADATA_TEMP}" || n2api_backup_fail "backup_metadata_invalid" "Backup metadata is invalid"
  chmod 600 -- "${N2API_BACKUP_METADATA_TEMP}" || n2api_backup_fail "backup_metadata_permissions_failed" "Backup metadata permissions could not be protected"

  N2API_BACKUP_STAGE="publish"
  N2API_BACKUP_ARCHIVE_TARGET_OWNED=true
  mv -- "${N2API_BACKUP_TEMP}" "${N2API_BACKUP_FINAL}" || n2api_backup_fail "backup_archive_publish_failed" "Backup archive could not be published"
  N2API_BACKUP_TEMP=""
  N2API_BACKUP_METADATA_TARGET_OWNED=true
  mv -- "${N2API_BACKUP_METADATA_TEMP}" "${N2API_BACKUP_METADATA_FINAL}" || n2api_backup_fail "backup_metadata_publish_failed" "Backup metadata could not be published"
  N2API_BACKUP_METADATA_TEMP=""
  N2API_BACKUP_PUBLISHED=true

  current="$(jq -c --arg directory "${backup_dir}" '. + {directory:$directory}' <<<"${metadata}")"
  artifacts="$(jq -cn \
    --arg archive "${N2API_BACKUP_FINAL}" \
    --arg metadata "${N2API_BACKUP_METADATA_FINAL}" \
    '[{type:"postgres_custom_archive",path:$archive},{type:"backup_metadata",path:$metadata}]')"
  document="$(n2api_envelope_json \
    "backup.create" "local_write" "attention" true \
    "backup_created_off_host_attention" "Backup created and verified; encrypted off-host copy is not recorded" \
    "${current}" '{}' "${artifacts}" '["Create and record an encrypted off-host copy", "Run restore drill with this archive"]' \
    "${N2API_BACKUP_OPERATION_ID}" "${N2API_BACKUP_STARTED_AT}")"
  n2api_operation_write "${N2API_BACKUP_OPERATION_ID}" "${document}" || {
    trap - EXIT INT TERM
    n2api_backup_cleanup
    n2api_emit "backup.create" "local_write" "failed" true \
      "operation_receipt_failed" "Backup succeeded but its operation receipt could not be written" \
      "${current}" '{}' "${artifacts}" '["Preserve the backup and repair operator state"]'
    exit "${N2API_EXIT_FAILED}"
  }
  trap - EXIT INT TERM
  n2api_lock_release
  N2API_BACKUP_LOCKED=false
  n2api_emit_document "${document}"
  exit "${N2API_EXIT_ATTENTION}"
}

n2api_backup_list() {
  local explicit_directory="" directory backups='[]' metadata_file metadata
  while (($# > 0)); do
    case "$1" in
      --backup-dir) (($# >= 2)) || n2api_usage_error "missing_backup_directory"; explicit_directory=$2; shift 2 ;;
      *) n2api_usage_error "unknown_backup_list_option" ;;
    esac
  done
  if [[ -n "${explicit_directory}" ]]; then
    directory=${explicit_directory}
  else
    if [[ -f "${N2API_ENV_FILE}" && ! -L "${N2API_ENV_FILE}" ]] && n2api_env_file_is_safe "${N2API_ENV_FILE}"; then
      n2api_env_load "${N2API_ENV_FILE}" || true
    fi
    directory="$(n2api_backup_directory)"
  fi
  if [[ ! -d "${directory}" || -L "${directory}" ]]; then
    backups='[]'
  else
    while IFS= read -r -d '' metadata_file; do
      metadata="$(jq -ce '
        select(.schema_version == "n2api.ops.backup/v1") |
        {operation_id,backup_id,created_at,archive,size_bytes,checksum,source_image,source_schema_version,evidence_class,verified,off_host_status}
      ' "${metadata_file}" 2>/dev/null)" || continue
      backups="$(jq -c --argjson metadata "${metadata}" '. + [$metadata]' <<<"${backups}")"
    done < <(find "${directory}" -maxdepth 1 -type f -name 'n2api-*.metadata.json' -print0 2>/dev/null | sort -z)
    backups="$(jq -c 'reverse' <<<"${backups}")"
  fi
  n2api_emit "backup.list" "read_only" "succeeded" false "backups_listed" "Backup metadata listed" \
    "$(jq -cn --arg directory "${directory}" --argjson backups "${backups}" '{directory:$directory,backups:$backups}')" \
    '{}' '[]' '[]'
}

n2api_backup_verify() {
  local archive="" checksum metadata_file metadata expected_checksum integrity_status="unavailable" current state_status archive_status
  while (($# > 0)); do
    case "$1" in
      --archive) (($# >= 2)) || n2api_usage_error "missing_backup_archive"; archive=$2; shift 2 ;;
      *) n2api_usage_error "unknown_backup_verify_option" ;;
    esac
  done
  [[ -n "${archive}" ]] || n2api_usage_error "backup_verify_requires_archive"
  [[ -f "${archive}" && ! -L "${archive}" && -r "${archive}" ]] || {
    n2api_emit "backup.verify" "read_only" "failed" false "backup_archive_unsafe" "Backup archive is unsafe" '{}' '{}' '[]' '[]'
    exit "${N2API_EXIT_FAILED}"
  }
  n2api_check_env_file >/dev/null || {
    n2api_emit "backup.verify" "read_only" "failed" false "backup_configuration_unavailable" "Backup verification needs a safe deployment environment" '{}' '{}' '[]' '[]'
    exit "${N2API_EXIT_FAILED}"
  }
  set +e
  n2api_backup_verify_archive_internal "${archive}"
  archive_status=$?
  set -e
  if n2api_status_is_timeout "${archive_status}"; then
    n2api_emit "backup.verify" "read_only" "failed" false "backup_archive_list_timeout" "Backup archive validation exceeded its timeout" '{}' '{}' '[]' '[]'
    exit 124
  elif [[ ${archive_status} -ne 0 ]]; then
    n2api_emit "backup.verify" "read_only" "blocked" false "backup_archive_invalid" "Backup archive structure validation failed" '{}' '{}' '[]' '[]'
    exit "${N2API_EXIT_BLOCKED}"
  fi
  checksum="$(sha256sum -- "${archive}" | awk '{print $1}')"
  metadata_file=""
  [[ "${archive}" == *.dump ]] && metadata_file="${archive%.dump}.metadata.json"
  if [[ -f "${metadata_file}" && ! -L "${metadata_file}" ]]; then
    metadata="$(n2api_backup_metadata_json "${metadata_file}" "${archive}")" || {
      n2api_emit "backup.verify" "read_only" "blocked" false "backup_metadata_invalid" "Backup metadata is invalid" '{}' '{}' '[]' '[]'
      exit "${N2API_EXIT_BLOCKED}"
    }
    expected_checksum="$(jq -r '.checksum' <<<"${metadata}")"
    if [[ "${expected_checksum}" != "${checksum}" ]]; then
      n2api_emit "backup.verify" "read_only" "blocked" false "backup_checksum_mismatch" "Backup checksum does not match metadata" '{}' '{}' '[]' '[]'
      exit "${N2API_EXIT_BLOCKED}"
    fi
    set +e
    n2api_state_require_existing
    state_status=$?
    set -e
    if [[ ${state_status} -eq 0 ]]; then
      if n2api_backup_metadata_hmac_valid "${metadata}"; then
        integrity_status="matched"
      else
        n2api_emit "backup.verify" "read_only" "blocked" false "backup_metadata_integrity_failed" "Backup metadata integrity validation failed" '{}' '{}' '[]' '[]'
        exit "${N2API_EXIT_BLOCKED}"
      fi
    elif [[ ${state_status} -eq 1 ]]; then
      n2api_emit "backup.verify" "read_only" "failed" false "unsafe_state_directory" "Backup metadata integrity state is unsafe" '{}' '{}' '[]' '[]'
      exit "${N2API_EXIT_FAILED}"
    fi
  else
    expected_checksum=""
  fi
  current="$(jq -cn \
    --arg archive "${archive}" \
    --arg checksum "${checksum}" \
    --arg metadata_status "$([[ -n "${expected_checksum}" ]] && printf 'matched' || printf 'unavailable')" \
    --arg integrity_status "${integrity_status}" \
    '{archive:$archive,archive_list_status:"passed",checksum:$checksum,metadata_checksum_status:$metadata_status,metadata_integrity_status:$integrity_status,restore_proven:false}')"
  n2api_emit "backup.verify" "read_only" "succeeded" false "backup_archive_verified" "Backup archive structure and checksum were verified" \
    "${current}" '{}' '[]' '["Run an isolated restore drill before claiming recoverability"]'
}

n2api_backup() {
  (($# >= 1)) || n2api_usage_error "backup_requires_command"
  local subcommand=$1
  shift
  case "${subcommand}" in
    create) n2api_backup_create "$@" ;;
    list) n2api_backup_list "$@" ;;
    verify) n2api_backup_verify "$@" ;;
    *) n2api_usage_error "unknown_backup_command" ;;
  esac
}

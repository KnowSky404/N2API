#!/usr/bin/env bash

n2api_default_state_dir() {
  if [[ -n "${XDG_STATE_HOME:-}" ]]; then
    printf '%s/n2api\n' "${XDG_STATE_HOME}"
  else
    printf '%s/.local/state/n2api\n' "${HOME}"
  fi
}

n2api_state_parent_is_safe() {
  local parent=$1 mode owner
  [[ -d "${parent}" && ! -L "${parent}" ]] || return 1
  owner="$(n2api_path_owner "${parent}")" || return 1
  mode="$(n2api_path_mode "${parent}")" || return 1
  [[ "${owner}" == "$(id -u)" ]] || return 1
  ! n2api_mode_has_group_or_other_write "${mode}"
}

n2api_state_is_safe() {
  local state=$1 mode owner
  [[ -d "${state}" && ! -L "${state}" ]] || return 1
  owner="$(n2api_path_owner "${state}")" || return 1
  mode="$(n2api_path_mode "${state}")" || return 1
  [[ "${owner}" == "$(id -u)" ]] || return 1
  ! n2api_mode_has_group_or_other_access "${mode}"
}

n2api_state_require_existing() {
  if [[ ! -e "${N2API_STATE_DIR}" ]]; then
    return 2
  fi
  if ! n2api_state_is_safe "${N2API_STATE_DIR}"; then
    return 1
  fi
}

n2api_state_init() {
  local parent key_file key_tmp
  parent="$(dirname -- "${N2API_STATE_DIR}")"
  if [[ ! -e "${parent}" ]]; then
    mkdir -p -- "${parent}"
    chmod 700 -- "${parent}"
  fi
  n2api_state_parent_is_safe "${parent}" || return 1

  if [[ -e "${N2API_STATE_DIR}" ]]; then
    n2api_state_is_safe "${N2API_STATE_DIR}" || return 1
  else
    mkdir -- "${N2API_STATE_DIR}"
    chmod 700 -- "${N2API_STATE_DIR}"
  fi
  mkdir -p -- \
    "${N2API_STATE_DIR}/plans" \
    "${N2API_STATE_DIR}/operations" \
    "${N2API_STATE_DIR}/locks" \
    "${N2API_STATE_DIR}/keys"
  chmod 700 -- \
    "${N2API_STATE_DIR}/plans" \
    "${N2API_STATE_DIR}/operations" \
    "${N2API_STATE_DIR}/locks" \
    "${N2API_STATE_DIR}/keys"

  key_file="${N2API_STATE_DIR}/keys/integrity.key"
  if [[ -e "${key_file}" ]]; then
    [[ -f "${key_file}" && ! -L "${key_file}" ]] || return 1
    chmod 600 -- "${key_file}"
  else
    key_tmp="$(mktemp "${N2API_STATE_DIR}/keys/.integrity.XXXXXX")"
    openssl rand -hex 32 >"${key_tmp}"
    chmod 600 -- "${key_tmp}"
    if ! mv -n -- "${key_tmp}" "${key_file}"; then
      rm -f -- "${key_tmp}"
      [[ -f "${key_file}" && ! -L "${key_file}" ]] || return 1
    fi
    if [[ -e "${key_tmp}" ]]; then
      rm -f -- "${key_tmp}"
      [[ -f "${key_file}" && ! -L "${key_file}" ]] || return 1
    fi
  fi
}

n2api_state_hmac_file() {
  local path=$1 key
  key="$(tr -d '\r\n' <"${N2API_STATE_DIR}/keys/integrity.key")"
  [[ "${key}" =~ ^[0-9a-f]{64}$ ]] || return 1
  openssl dgst -sha256 -mac HMAC -macopt "hexkey:${key}" -- "${path}" | awk '{print $NF}'
}

n2api_operation_write() {
  local operation_id=$1 document=$2 target tmp
  n2api_validate_operation_id "${operation_id}" || return 1
  n2api_state_init || return 1
  target="${N2API_STATE_DIR}/operations/${operation_id}.json"
  [[ ! -e "${target}" ]] || return 1
  tmp="$(mktemp "${N2API_STATE_DIR}/operations/.${operation_id}.XXXXXX")"
  if ! jq -ce . <<<"${document}" >"${tmp}"; then
    rm -f -- "${tmp}"
    return 1
  fi
  chmod 600 -- "${tmp}"
  mv -- "${tmp}" "${target}"
}

n2api_lock_acquire() {
  local operation_id=$1 metadata tmp
  n2api_state_init || return 1
  exec 9>"${N2API_STATE_DIR}/locks/operator.lock"
  chmod 600 -- "${N2API_STATE_DIR}/locks/operator.lock"
  flock -n 9 || return 2
  metadata="${N2API_STATE_DIR}/locks/operator.json"
  tmp="$(mktemp "${N2API_STATE_DIR}/locks/.operator.XXXXXX")"
  jq -cn --arg operation_id "${operation_id}" --argjson pid "$$" --arg acquired_at "$(n2api_now)" \
    '{operation_id:$operation_id,pid:$pid,acquired_at:$acquired_at}' >"${tmp}"
  chmod 600 -- "${tmp}"
  mv -- "${tmp}" "${metadata}"
}

n2api_lock_release() {
  rm -f -- "${N2API_STATE_DIR}/locks/operator.json"
  flock -u 9 2>/dev/null || true
  exec 9>&-
}

n2api_operations_list() {
  (($# == 0)) || n2api_usage_error "operations_list_unexpected_argument"
  local state_status current operations
  set +e
  n2api_state_require_existing
  state_status=$?
  set -e
  case "${state_status}" in
    2)
      current='{"availability":"unavailable","operations":[]}'
      n2api_emit "operations.list" "read_only" "succeeded" false \
        "operation_state_unavailable" "No operation state exists" \
        "${current}" '{}' '[]' '[]'
      return
      ;;
    1)
      n2api_emit "operations.list" "read_only" "failed" false \
        "unsafe_state_directory" "Operation state directory is unsafe" \
        '{"availability":"unavailable","operations":[]}' '{}' '[]' \
        '["Repair state directory ownership and mode"]'
      exit "${N2API_EXIT_FAILED}"
      ;;
  esac

  operations="$(find "${N2API_STATE_DIR}/operations" -maxdepth 1 -type f -name 'op-*.json' -print0 \
    | sort -z \
    | xargs -0 -r jq -c '{operation_id,command,status,changed,started_at,finished_at,reason_code}' \
    | jq -sc 'reverse')"
  current="$(jq -cn --argjson operations "${operations}" '{availability:"available",operations:$operations}')"
  n2api_emit "operations.list" "read_only" "succeeded" false \
    "operations_listed" "Operation records listed" "${current}" '{}' '[]' '[]'
}

n2api_operations_show() {
  (($# == 1)) || n2api_usage_error "operations_show_requires_id"
  local operation_id=$1 path state_status receipt current
  n2api_validate_operation_id "${operation_id}" || n2api_usage_error "invalid_operation_id"
  set +e
  n2api_state_require_existing
  state_status=$?
  set -e
  if [[ ${state_status} -ne 0 ]]; then
    n2api_emit "operations.show" "read_only" "failed" false \
      "operation_state_unavailable" "Operation state is unavailable" '{}' '{}' '[]' '[]'
    exit "${N2API_EXIT_FAILED}"
  fi
  path="${N2API_STATE_DIR}/operations/${operation_id}.json"
  if [[ ! -f "${path}" || -L "${path}" ]]; then
    n2api_emit "operations.show" "read_only" "failed" false \
      "operation_not_found" "Operation record was not found" '{}' '{}' '[]' '[]'
    exit "${N2API_EXIT_FAILED}"
  fi
  receipt="$(jq -ce . -- "${path}")" || {
    n2api_emit "operations.show" "read_only" "failed" false \
      "operation_record_invalid" "Operation record is invalid" '{}' '{}' '[]' '[]'
    exit "${N2API_EXIT_FAILED}"
  }
  current="$(jq -cn --argjson receipt "${receipt}" '{receipt:$receipt}')"
  n2api_emit "operations.show" "read_only" "succeeded" false \
    "operation_shown" "Operation record loaded" "${current}" '{}' '[]' '[]'
}

n2api_operations() {
  (($# >= 1)) || n2api_usage_error "operations_requires_command"
  local subcommand=$1
  shift
  case "${subcommand}" in
    list) n2api_operations_list "$@" ;;
    show) n2api_operations_show "$@" ;;
    *) n2api_usage_error "unknown_operations_command" ;;
  esac
}

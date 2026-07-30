#!/usr/bin/env bash

declare -A N2API_ENV=()

n2api_trim_space() {
  local value=$1
  value="${value#"${value%%[![:space:]]*}"}"
  value="${value%"${value##*[![:space:]]}"}"
  printf '%s' "${value}"
}

n2api_env_decode_value() {
  local raw value
  raw="$(n2api_trim_space "$1")"
  if [[ "${raw}" == \"*\" && ${#raw} -ge 2 ]]; then
    value="${raw:1:${#raw}-2}"
    value="${value//\\\"/\"}"
    value="${value//\\\\/\\}"
    printf '%s' "${value}"
    return
  fi
  if [[ "${raw}" == \'*\' && ${#raw} -ge 2 ]]; then
    printf '%s' "${raw:1:${#raw}-2}"
    return
  fi
  printf '%s' "${raw}"
}

n2api_env_load() {
  local path=$1 line key raw
  N2API_ENV=()
  while IFS= read -r line || [[ -n "${line}" ]]; do
    line="${line%$'\r'}"
    raw="$(n2api_trim_space "${line}")"
    [[ -n "${raw}" && "${raw:0:1}" != '#' ]] || continue
    [[ "${raw}" == *=* ]] || return 1
    key="$(n2api_trim_space "${raw%%=*}")"
    [[ "${key}" =~ ^[A-Za-z_][A-Za-z0-9_]*$ ]] || return 1
    N2API_ENV["${key}"]="$(n2api_env_decode_value "${raw#*=}")"
  done <"${path}"
}

n2api_env_get() {
  local name=$1 fallback=${2:-}
  if [[ -v "N2API_ENV[${name}]" ]]; then
    printf '%s' "${N2API_ENV[${name}]}"
  else
    printf '%s' "${fallback}"
  fi
}

n2api_env_file_is_safe() {
  local path=$1 mode owner
  [[ -f "${path}" && ! -L "${path}" ]] || return 1
  owner="$(n2api_path_owner "${path}")" || return 1
  mode="$(n2api_path_mode "${path}")" || return 1
  [[ "${owner}" == "$(id -u)" ]] || return 1
  ! n2api_mode_has_group_or_other_access "${mode}"
}

n2api_secret_file_is_safe() {
  n2api_env_file_is_safe "$1"
}

n2api_env_contains_placeholder() {
  local value key
  for key in "${!N2API_ENV[@]}"; do
    value=${N2API_ENV[${key}]}
    case "${value}" in
      *change-me*|*CHANGE_ME*|*YYYYMMDDNN*|*replace-with*|*example-digest*)
        return 0
        ;;
    esac
  done
  return 1
}

n2api_public_url_is_valid() {
  local value=$1
  [[ "${value}" =~ ^https?://[^/@[:space:]]+(:[0-9]{1,5})?(/[^[:space:]]*)?$ ]] || return 1
  [[ "${value}" != *'@'* && "${value}" != *'#'* ]]
}

n2api_bind_address_is_valid() {
  local value=$1 octet
  local -a octets=()
  case "${value}" in
    127.0.0.1|0.0.0.0|::1|::) return 0 ;;
  esac
  if [[ "${value}" =~ ^([0-9]{1,3}\.){3}[0-9]{1,3}$ ]]; then
    IFS='.' read -r -a octets <<<"${value}"
    for octet in "${octets[@]}"; do
      ((10#${octet} <= 255)) || return 1
    done
    return 0
  fi
  [[ "${value}" =~ ^[0-9A-Fa-f:]+$ && "${value}" == *:*:* && "${value}" != *:::* ]]
}

n2api_accepted_risks_are_valid() {
  local value=$1 item
  local -A seen=()
  local -a items=()
  [[ -n "${value}" ]] || return 1
  IFS=',' read -r -a items <<<"${value}"
  for item in "${items[@]}"; do
    item="$(n2api_trim_space "${item}")"
    case "${item}" in
      public-http|public-bind|database-plaintext|database-unverified-tls) ;;
      *) return 1 ;;
    esac
    [[ ! -v "seen[${item}]" ]] || return 1
    seen["${item}"]=1
  done
}

n2api_env_quote() {
  local value=$1
  value="${value//\\/\\\\}"
  value="${value//\"/\\\"}"
  value="${value//\$/\\\$}"
  printf '"%s"' "${value}"
}

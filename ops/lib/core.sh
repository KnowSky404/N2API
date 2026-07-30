#!/usr/bin/env bash

N2API_OPS_CLI_VERSION="1.0.0"
N2API_OPS_SCHEMA_VERSION="n2api.ops/v1"
N2API_PLAN_SCHEMA_VERSION="n2api.ops.plan/v1"
N2API_RECEIPT_SCHEMA_VERSION="n2api.ops.receipt/v1"

N2API_EXIT_ATTENTION=3
N2API_EXIT_BLOCKED=4
N2API_EXIT_CONTENDED=5
N2API_EXIT_FAILED=6
N2API_EXIT_USAGE=64

n2api_now() {
  date -u +'%Y-%m-%dT%H:%M:%SZ'
}

n2api_operation_id() {
  local random
  random="$(od -An -N6 -tx1 /dev/urandom | tr -d ' \n')"
  printf 'op-%s-%s\n' "$(date -u +'%Y%m%dT%H%M%SZ')" "${random}"
}

n2api_usage_error() {
  local reason=$1
  printf 'n2api: invalid invocation (%s); run ./ops/n2api --help\n' "${reason}" >&2
  exit "${N2API_EXIT_USAGE}"
}

n2api_require_uint_range() {
  local name=$1 value=$2 minimum=$3 maximum=$4
  if [[ ! "${value}" =~ ^[0-9]+$ ]] || ((value < minimum || value > maximum)); then
    n2api_usage_error "invalid_${name}"
  fi
}

n2api_validate_project_name() {
  [[ "$1" =~ ^[a-z0-9][a-z0-9_-]{0,62}$ ]]
}

n2api_validate_operation_id() {
  [[ "$1" =~ ^op-[0-9]{8}T[0-9]{6}Z-[0-9a-f]{12}$ ]]
}

n2api_timeout_kill_after() {
  local grace=10
  if ((N2API_TIMEOUT_SECONDS < grace)); then
    grace=${N2API_TIMEOUT_SECONDS}
  fi
  printf '%s\n' "${grace}"
}

n2api_status_is_timeout() {
  [[ "$1" -eq 124 || "$1" -eq 137 ]]
}

n2api_run_timeout() {
  local child_pid status
  timeout --signal=TERM --kill-after="$(n2api_timeout_kill_after)" \
    "${N2API_TIMEOUT_SECONDS}" "$@" &
  child_pid=$!
  if wait "${child_pid}" 2>/dev/null; then
    status=0
  else
    status=$?
  fi
  return "${status}"
}

n2api_path_mode() {
  stat -c '%a' -- "$1"
}

n2api_path_owner() {
  stat -c '%u' -- "$1"
}

n2api_mode_has_group_or_other_write() {
  local mode=$1
  (( (8#${mode} & 8#022) != 0 ))
}

n2api_mode_has_group_or_other_access() {
  local mode=$1
  (( (8#${mode} & 8#077) != 0 ))
}

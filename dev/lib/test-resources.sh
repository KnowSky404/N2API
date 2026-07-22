#!/usr/bin/env bash

if [[ -n "${N2API_TEST_RESOURCES_LOADED:-}" ]]; then
  return 0
fi
N2API_TEST_RESOURCES_LOADED=1

N2API_REPO_ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd -P)"
N2API_DEV_CACHE_ROOT="${N2API_DEV_CACHE_ROOT:-${N2API_REPO_ROOT}/.cache/dev}"
N2API_TMP_ROOT="${N2API_TMP_ROOT:-${TMPDIR:-/tmp}}"
N2API_RESOURCE_LOCK_FILE="${N2API_RESOURCE_LOCK_FILE:-$(dirname "${N2API_DEV_CACHE_ROOT}")/.n2api-test-resources.lock}"

n2api_resource_error() {
  echo "n2api_test_resource_error=$*" >&2
}

n2api_validate_run_id() {
  [[ "$1" =~ ^[a-z0-9][a-z0-9._-]{0,127}$ ]]
}

n2api_run_is_active() {
  local run_id=$1
  local marker="${N2API_DEV_CACHE_ROOT}/active/${run_id}"
  local pid

  n2api_validate_run_id "${run_id}" || return 1
  [[ -f "${marker}" ]] || return 1
  read -r pid <"${marker}" || return 1
  case "${pid}" in
    pid:*) pid=${pid#pid:} ;;
    until:*)
      pid=${pid#until:}
      [[ "${pid}" =~ ^[0-9]+$ ]] || return 1
      (( $(date +%s) < pid ))
      return
      ;;
    *) return 1 ;;
  esac
  [[ "${pid}" =~ ^[0-9]+$ ]] || return 1
  kill -0 "${pid}" 2>/dev/null
}

n2api_acquire_run_lock() {
  command -v flock >/dev/null 2>&1 || {
    n2api_resource_error "missing_command command=flock"
    return 1
  }
  mkdir -p "$(dirname "${N2API_RESOURCE_LOCK_FILE}")"
  if [[ -z "${N2API_RUN_LOCK_FD:-}" ]]; then
    exec {N2API_RUN_LOCK_FD}>"${N2API_RESOURCE_LOCK_FILE}"
    flock -s "${N2API_RUN_LOCK_FD}"
  fi
}

n2api_mark_run_active() {
  local run_id=$1
  n2api_validate_run_id "${run_id}" || {
    n2api_resource_error "invalid_run_id run_id=${run_id}"
    return 1
  }
  n2api_acquire_run_lock
  mkdir -p "${N2API_DEV_CACHE_ROOT}/active"
  printf 'pid:%s\n' "${BASHPID:-$$}" >"${N2API_DEV_CACHE_ROOT}/active/${run_id}"
}

n2api_unmark_run_active() {
  local run_id=$1
  n2api_validate_run_id "${run_id}" || return 0
  rm -f -- "${N2API_DEV_CACHE_ROOT}/active/${run_id}"
  if [[ -n "${N2API_RUN_LOCK_FD:-}" ]]; then
    flock -u "${N2API_RUN_LOCK_FD}" 2>/dev/null || true
    exec {N2API_RUN_LOCK_FD}>&-
    unset N2API_RUN_LOCK_FD
  fi
}

n2api_dir_size_mib() {
  local path=$1
  local size_kib=0
  if [[ -d "${path}" ]]; then
    size_kib="$(du -sk -- "${path}" 2>/dev/null | awk '{print $1}')"
  fi
  echo $((size_kib / 1024))
}

n2api_any_active_runs() {
  local marker run_id
  [[ -d "${N2API_DEV_CACHE_ROOT}/active" ]] || return 1
  shopt -s nullglob
  for marker in "${N2API_DEV_CACHE_ROOT}/active"/*; do
    run_id="${marker##*/}"
    if n2api_run_is_active "${run_id}"; then
      shopt -u nullglob
      return 0
    fi
    rm -f -- "${marker}"
  done
  shopt -u nullglob
  return 1
}

n2api_enforce_cache_limits() {
  local name path limit size lock_fd own_lock=0
  if [[ "${N2API_RESOURCE_EXCLUSIVE_LOCK_HELD:-0}" != "1" ]]; then
    mkdir -p "${N2API_DEV_CACHE_ROOT}"
    mkdir -p "$(dirname "${N2API_RESOURCE_LOCK_FILE}")"
    exec {lock_fd}>"${N2API_RESOURCE_LOCK_FILE}"
    flock -n -x "${lock_fd}" || return 0
    own_lock=1
  fi
  if n2api_any_active_runs; then
    if (( own_lock == 1 )); then
      exec {lock_fd}>&-
    fi
    return 0
  fi

  while IFS='|' read -r name path limit; do
    [[ "${limit}" =~ ^[0-9]+$ ]] || {
      n2api_resource_error "invalid_cache_limit name=${name} value=${limit}"
      continue
    }
    size="$(n2api_dir_size_mib "${path}")"
    if (( size > limit )); then
      echo "n2api_cache_cleanup name=${name} size_mib=${size} limit_mib=${limit}" >&2
      rm -rf -- "${path}"
    fi
  done <<EOF
go-mod|${N2API_DEV_CACHE_ROOT}/go-mod|${N2API_GO_MOD_CACHE_MAX_MIB:-2048}
bun|${N2API_DEV_CACHE_ROOT}/bun|${N2API_BUN_CACHE_MAX_MIB:-1024}
playwright-browsers|${N2API_DEV_CACHE_ROOT}/playwright-browsers|${N2API_PLAYWRIGHT_CACHE_MAX_MIB:-2048}
EOF
  if (( own_lock == 1 )); then
    exec {lock_fd}>&-
  fi
}

n2api_register_compose() {
  local compose_file=$1
  local project=$2

  [[ -n "${N2API_TEST_RUN_ID:-}" ]] || {
    n2api_resource_error "missing_run_id"
    return 1
  }
  n2api_validate_run_id "${N2API_TEST_RUN_ID}" || {
    n2api_resource_error "invalid_run_id run_id=${N2API_TEST_RUN_ID}"
    return 1
  }
  [[ "${N2API_TEST_RUN_ID}" != "deploy" && "${project}" == "n2api-${N2API_TEST_RUN_ID}" ]] || {
    n2api_resource_error "unsafe_compose_project project=${project}"
    return 1
  }
  [[ -f "${compose_file}" ]] || {
    n2api_resource_error "missing_compose_file path=${compose_file}"
    return 1
  }

  N2API_TEST_COMPOSE_FILE="${compose_file}"
  N2API_TEST_COMPOSE_PROJECT="${project}"
  export N2API_TEST_COMPOSE_FILE N2API_TEST_COMPOSE_PROJECT
}

n2api_compose() {
  N2API_TEST_RUN_ID="${N2API_TEST_RUN_ID}" docker compose \
    --project-name "${N2API_TEST_COMPOSE_PROJECT}" \
    --file "${N2API_TEST_COMPOSE_FILE}" "$@"
}

n2api_docker_resource_is_protected() {
  local kind=$1
  local id=$2
  local project name

  case "${kind}" in
    container|image)
      project="$(docker "${kind}" inspect --format '{{ index .Config.Labels "com.docker.compose.project" }}' "${id}" 2>/dev/null || true)"
      ;;
    network|volume)
      project="$(docker "${kind}" inspect --format '{{ index .Labels "com.docker.compose.project" }}' "${id}" 2>/dev/null || true)"
      ;;
  esac
  [[ "${project}" == "deploy" ]] && return 0
  [[ "${kind}" == "volume" && "${id}" == "deploy_n2api-postgres" ]] && return 0
  if [[ "${kind}" == "container" ]]; then
    name="$(docker container inspect --format '{{ .Name }}' "${id}" 2>/dev/null || true)"
    [[ "${name#/}" == deploy-* ]] && return 0
  fi
  return 1
}

n2api_remove_run_docker_resources() {
  local run_id=$1
  local id failed=0
  n2api_validate_run_id "${run_id}" || return 0
  command -v docker >/dev/null 2>&1 || return 0
  docker info >/dev/null 2>&1 || return 0

  while IFS= read -r id; do
    [[ -n "${id}" ]] || continue
    n2api_docker_resource_is_protected container "${id}" && continue
    docker container rm --force "${id}" >/dev/null 2>&1 || failed=1
  done < <(docker container ls --all --quiet \
    --filter "label=io.knowsky.n2api.resource=test" \
    --filter "label=io.knowsky.n2api.run-id=${run_id}")
  while IFS= read -r id; do
    [[ -n "${id}" ]] || continue
    n2api_docker_resource_is_protected network "${id}" && continue
    docker network rm "${id}" >/dev/null 2>&1 || failed=1
  done < <(docker network ls --quiet \
    --filter "label=io.knowsky.n2api.resource=test" \
    --filter "label=io.knowsky.n2api.run-id=${run_id}")
  while IFS= read -r id; do
    [[ -n "${id}" ]] || continue
    n2api_docker_resource_is_protected volume "${id}" && continue
    docker volume rm "${id}" >/dev/null 2>&1 || failed=1
  done < <(docker volume ls --quiet \
    --filter "label=io.knowsky.n2api.resource=test" \
    --filter "label=io.knowsky.n2api.run-id=${run_id}")
  while IFS= read -r id; do
    [[ -n "${id}" ]] || continue
    n2api_docker_resource_is_protected image "${id}" && continue
    docker image rm "${id}" >/dev/null 2>&1 || failed=1
  done < <(docker image ls --quiet \
    --filter "label=io.knowsky.n2api.resource=test" \
    --filter "label=io.knowsky.n2api.run-id=${run_id}")
  return "${failed}"
}

n2api_assert_owned_run_dir() {
  local path=$1
  local resolved_path resolved_root
  [[ -n "${N2API_RUN_ID:-}" && -d "${path}" && -f "${path}/.n2api-test-run" ]] || return 1
  [[ "${path##*/}" == n2api-* ]] || return 1
  [[ "$(<"${path}/.n2api-test-run")" == "${N2API_RUN_ID}" ]] || return 1
  resolved_path="$(readlink -f -- "${path}")"
  resolved_root="$(readlink -f -- "${N2API_TMP_ROOT}")"
  [[ "${resolved_path}" == "${resolved_root}"/n2api-* ]]
}

n2api_run_cleanup() {
  local status=$1
  local artifact_target cleanup_failed=0
  trap - EXIT INT TERM

  if [[ -n "${N2API_TEST_COMPOSE_PROJECT:-}" && -n "${N2API_TEST_COMPOSE_FILE:-}" ]]; then
    n2api_compose down --volumes --remove-orphans --rmi local --timeout 10 >/dev/null 2>&1 || cleanup_failed=1
  fi
  if [[ -n "${N2API_RUN_ID:-}" ]]; then
    n2api_remove_run_docker_resources "${N2API_RUN_ID}" || cleanup_failed=1
  fi

  if (( status != 0 )) && [[ "${N2API_KEEP_FAILED_ARTIFACTS:-0}" == "1" ]] &&
    [[ -d "${N2API_RUN_DIR:-}/artifacts" ]]; then
    artifact_target="${N2API_DEV_CACHE_ROOT}/artifacts/${N2API_RUN_ID}"
    mkdir -p "${artifact_target}"
    cp -a "${N2API_RUN_DIR}/artifacts/." "${artifact_target}/" 2>/dev/null || true
    printf '%s\n' "${status}" >"${artifact_target}/exit-status"
    echo "n2api_failure_artifacts=${artifact_target}" >&2
  fi

  if [[ -n "${N2API_RUN_ID:-}" ]]; then
    n2api_unmark_run_active "${N2API_RUN_ID}"
  fi
  if [[ -n "${N2API_RUN_DIR:-}" ]] && n2api_assert_owned_run_dir "${N2API_RUN_DIR}"; then
    rm -rf -- "${N2API_RUN_DIR}"
  fi
  n2api_enforce_cache_limits || true
  if (( status == 0 && cleanup_failed != 0 )); then
    echo "n2api_test_resource_error=cleanup_incomplete run_id=${N2API_RUN_ID:-unknown}" >&2
    status=1
  fi
  exit "${status}"
}

n2api_signal_exit() {
  local signal=$1
  local status=$2
  trap - INT TERM
  if [[ -n "${N2API_CHILD_PID:-}" ]]; then
    kill -s "${signal}" -- "-${N2API_CHILD_PID}" 2>/dev/null ||
      kill -s "${signal}" "${N2API_CHILD_PID}" 2>/dev/null || true
    for _ in {1..100}; do
      kill -0 "${N2API_CHILD_PID}" 2>/dev/null || break
      sleep 0.1
    done
    if kill -0 "${N2API_CHILD_PID}" 2>/dev/null; then
      kill -KILL -- "-${N2API_CHILD_PID}" 2>/dev/null ||
        kill -KILL "${N2API_CHILD_PID}" 2>/dev/null || true
    fi
    wait "${N2API_CHILD_PID}" 2>/dev/null || true
  fi
  exit "${status}"
}

n2api_run_command() {
  local status
  command -v setsid >/dev/null 2>&1 || {
    n2api_resource_error "missing_command command=setsid"
    return 1
  }
  setsid --wait "$@" &
  N2API_CHILD_PID=$!
  if wait "${N2API_CHILD_PID}"; then
    status=0
  else
    status=$?
  fi
  unset N2API_CHILD_PID
  return "${status}"
}

n2api_run_init() {
  local kind=$1
  local stamp random

  [[ "${kind}" =~ ^[a-z0-9][a-z0-9-]{0,31}$ ]] || {
    n2api_resource_error "invalid_run_kind kind=${kind}"
    return 1
  }
  command -v mktemp >/dev/null 2>&1 || {
    n2api_resource_error "missing_command command=mktemp"
    return 1
  }

  n2api_acquire_run_lock
  mkdir -p "${N2API_TMP_ROOT}" "${N2API_DEV_CACHE_ROOT}"
  stamp="$(date -u +%Y%m%dt%H%M%Sz)"
  random="$(od -An -N4 -tx4 /dev/urandom | tr -d ' ')"
  N2API_RUN_ID="${kind}-${stamp}-${BASHPID:-$$}-${random}"
  N2API_RUN_DIR="$(mktemp -d "${N2API_TMP_ROOT%/}/n2api-${N2API_RUN_ID}.XXXXXXXX")"
  printf '%s\n' "${N2API_RUN_ID}" >"${N2API_RUN_DIR}/.n2api-test-run"
  n2api_mark_run_active "${N2API_RUN_ID}"
  trap 'n2api_run_cleanup $?' EXIT
  trap 'n2api_signal_exit INT 130' INT
  trap 'n2api_signal_exit TERM 143' TERM
  mkdir -p \
    "${N2API_RUN_DIR}/tmp" \
    "${N2API_RUN_DIR}/go-build" \
    "${N2API_RUN_DIR}/go-tmp" \
    "${N2API_RUN_DIR}/go-path" \
    "${N2API_RUN_DIR}/cache" \
    "${N2API_RUN_DIR}/frontend" \
    "${N2API_RUN_DIR}/artifacts/playwright/test-results" \
    "${N2API_RUN_DIR}/artifacts/playwright/report" \
    "${N2API_DEV_CACHE_ROOT}/go-mod" \
    "${N2API_DEV_CACHE_ROOT}/bun" \
    "${N2API_DEV_CACHE_ROOT}/playwright-browsers"

  export N2API_RUN_ID N2API_RUN_DIR
  export N2API_TEST_RUN_ID="${N2API_RUN_ID}"
  export TMPDIR="${N2API_RUN_DIR}/tmp"
  export XDG_CACHE_HOME="${N2API_RUN_DIR}/cache"
  export GOCACHE="${N2API_RUN_DIR}/go-build"
  export GOMODCACHE="${N2API_DEV_CACHE_ROOT}/go-mod"
  export GOTMPDIR="${N2API_RUN_DIR}/go-tmp"
  export GOPATH="${N2API_RUN_DIR}/go-path"
  export BUN_INSTALL_CACHE_DIR="${N2API_DEV_CACHE_ROOT}/bun"
  export PLAYWRIGHT_BROWSERS_PATH="${N2API_DEV_CACHE_ROOT}/playwright-browsers"
  export PLAYWRIGHT_HTML_OUTPUT_DIR="${N2API_RUN_DIR}/artifacts/playwright/report"
  export PLAYWRIGHT_BLOB_OUTPUT_DIR="${N2API_RUN_DIR}/artifacts/playwright/blob-report"
  export PLAYWRIGHT_JUNIT_OUTPUT_NAME="${N2API_RUN_DIR}/artifacts/playwright/results.xml"
  export N2API_FRONTEND_BUILD_DIR="${N2API_RUN_DIR}/frontend/build"

}

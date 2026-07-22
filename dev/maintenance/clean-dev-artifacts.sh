#!/usr/bin/env bash

set -Eeuo pipefail

repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd -P)"
source "${repo_root}/dev/lib/test-resources.sh"

deep=0
dry_run=0
for argument in "$@"; do
  case "${argument}" in
    --deep) deep=1 ;;
    --dry-run) dry_run=1 ;;
    *) echo "usage: $0 [--deep] [--dry-run]" >&2; exit 2 ;;
  esac
done

tmp_ttl_hours="${N2API_TMP_TTL_HOURS:-24}"
artifact_ttl_days="${N2API_ARTIFACT_TTL_DAYS:-7}"
artifact_keep_count="${N2API_ARTIFACT_KEEP_COUNT:-5}"
now="$(date +%s)"
cleanup_failed=0

for value in "${tmp_ttl_hours}" "${artifact_ttl_days}" "${artifact_keep_count}"; do
  [[ "${value}" =~ ^[0-9]+$ ]] || {
    echo "cleanup_status=failed reason=invalid_retention value=${value}" >&2
    exit 2
  }
done

remove_path() {
  local path=$1
  echo "remove path=${path}"
  if (( dry_run == 0 )); then
    rm -rf -- "${path}"
  fi
}

remove_artifact_path() {
  local path=$1
  local resolved_path resolved_root
  resolved_path="$(readlink -f -- "${path}")"
  resolved_root="$(readlink -f -- "${artifact_root}")"
  [[ "$(dirname "${resolved_path}")" == "${resolved_root}" ]] || {
    echo "cleanup_status=failed reason=unsafe_artifact_path path=${path}" >&2
    return 1
  }
  remove_path "${path}"
}

path_is_old_enough() {
  local path=$1
  local age_seconds=$((tmp_ttl_hours * 3600))
  local modified
  modified="$(stat -c %Y -- "${path}")"
  (( now - modified >= age_seconds ))
}

echo "cleanup_scope=n2api-test-resources deep=${deep} dry_run=${dry_run}"
echo "protected_compose_project=deploy protected_volume=deploy_n2api-postgres"

command -v flock >/dev/null 2>&1 || {
  echo "cleanup_status=failed reason=missing_command command=flock" >&2
  exit 1
}
mkdir -p "${N2API_DEV_CACHE_ROOT}"
mkdir -p "$(dirname "${N2API_RESOURCE_LOCK_FILE}")"
exec {cleanup_lock_fd}>"${N2API_RESOURCE_LOCK_FILE}"
if ! flock -n -x "${cleanup_lock_fd}"; then
  if (( deep == 1 )); then
    echo "cleanup_status=blocked reason=active_managed_runs" >&2
    exit 1
  fi
  echo "cleanup_status=passed skipped=active_managed_runs"
  exit 0
fi
N2API_RESOURCE_EXCLUSIVE_LOCK_HELD=1
export N2API_RESOURCE_EXCLUSIVE_LOCK_HELD

if [[ -d "${N2API_TMP_ROOT}" ]]; then
  while IFS= read -r -d '' marker; do
    run_dir="${marker%/.n2api-test-run}"
    run_id="$(<"${marker}")"
    n2api_validate_run_id "${run_id}" || continue
    n2api_run_is_active "${run_id}" && {
      echo "skip active_run=${run_id}"
      continue
    }
    path_is_old_enough "${run_dir}" || continue
    [[ "${run_dir##*/}" == n2api-* ]] || continue
    remove_path "${run_dir}"
    n2api_unmark_run_active "${run_id}"
  done < <(find "${N2API_TMP_ROOT}" -maxdepth 2 -type f -name .n2api-test-run -print0 2>/dev/null)
fi

artifact_root="${N2API_DEV_CACHE_ROOT}/artifacts"
if [[ -d "${artifact_root}" ]]; then
  while IFS= read -r -d '' artifact; do
    remove_artifact_path "${artifact}"
  done < <(find "${artifact_root}" -mindepth 1 -maxdepth 1 -type d \
    -mtime "+${artifact_ttl_days}" -print0 2>/dev/null)

  mapfile -d '' -t artifacts < <(find "${artifact_root}" -mindepth 1 -maxdepth 1 -type d -print0 2>/dev/null)
  while (( ${#artifacts[@]} > artifact_keep_count )); do
    oldest_index=0
    oldest_mtime="$(stat -c %Y -- "${artifacts[0]}")"
    for index in "${!artifacts[@]}"; do
      artifact_mtime="$(stat -c %Y -- "${artifacts[index]}")"
      if (( artifact_mtime < oldest_mtime )); then
        oldest_index=${index}
        oldest_mtime=${artifact_mtime}
      fi
    done
    remove_artifact_path "${artifacts[oldest_index]}"
    unset 'artifacts[oldest_index]'
    artifacts=("${artifacts[@]}")
  done
fi

docker_remove_stale() {
  local kind=$1
  local inspect_format=$2
  local id run_id
  local -a list_command remove_command

  case "${kind}" in
    container)
      list_command=(docker container ls --all --quiet)
      remove_command=(docker container rm --force)
      ;;
    image|network|volume)
      list_command=(docker "${kind}" ls --quiet)
      remove_command=(docker "${kind}" rm)
      ;;
  esac

  while IFS= read -r id; do
    [[ -n "${id}" ]] || continue
    run_id="$(docker "${kind}" inspect --format "${inspect_format}" "${id}" 2>/dev/null || true)"
    n2api_validate_run_id "${run_id}" || {
      echo "skip Docker_${kind}=${id} reason=missing_valid_run_id"
      continue
    }
    n2api_run_is_active "${run_id}" && {
      echo "skip Docker_${kind}=${id} reason=active_run"
      continue
    }
    if n2api_docker_resource_is_protected "${kind}" "${id}"; then
      echo "skip Docker_${kind}=${id} reason=production_resource"
      continue
    fi
    echo "remove Docker_${kind}=${id} run_id=${run_id}"
    if (( dry_run == 0 )); then
      if ! "${remove_command[@]}" "${id}" >/dev/null 2>&1; then
        echo "cleanup_warning=remove_failed Docker_${kind}=${id}" >&2
        cleanup_failed=1
      fi
    fi
  done < <("${list_command[@]}" --filter "label=io.knowsky.n2api.resource=test" | sort -u)
}

if command -v docker >/dev/null 2>&1 && docker info >/dev/null 2>&1; then
  docker_remove_stale container '{{ index .Config.Labels "io.knowsky.n2api.run-id" }}'
  docker_remove_stale network '{{ index .Labels "io.knowsky.n2api.run-id" }}'
  docker_remove_stale volume '{{ index .Labels "io.knowsky.n2api.run-id" }}'
  docker_remove_stale image '{{ index .Config.Labels "io.knowsky.n2api.run-id" }}'
fi

if (( deep == 1 )); then
  if n2api_any_active_runs; then
    echo "cleanup_status=blocked reason=active_managed_runs" >&2
    exit 1
  fi
  echo "deep_cleanup_categories=controlled_caches,legacy_exact_tmp,generated_test_reports"
  for path in \
    "${repo_root}/.cache/dev" \
    "${repo_root}/.cache/go" \
    "${repo_root}/.cache/go-build" \
    "${repo_root}/.cache/go-mod" \
    "${repo_root}/.cache/gomod" \
    "${repo_root}/frontend/.svelte-kit" \
    "${repo_root}/frontend/build" \
    "${repo_root}/frontend/.vite" \
    "${repo_root}/test-results" \
    "${repo_root}/playwright-report"; do
    [[ -e "${path}" ]] && remove_path "${path}"
  done
  for path in \
    "${N2API_TMP_ROOT%/}/n2api-go-build" \
    "${N2API_TMP_ROOT%/}/n2api-go-mod" \
    "${N2API_TMP_ROOT%/}/n2api-request-log-profile-go-build"; do
    [[ -d "${path}" ]] && path_is_old_enough "${path}" && remove_path "${path}"
  done
elif (( dry_run == 0 )); then
  n2api_enforce_cache_limits
fi

if (( cleanup_failed != 0 )); then
  echo "cleanup_status=partial" >&2
  exit 1
fi
echo "cleanup_status=passed"

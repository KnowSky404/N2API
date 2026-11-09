#!/usr/bin/env bash

set -Eeuo pipefail

repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd -P)"
source "${repo_root}/dev/lib/test-resources.sh"

mode=${1:-session}
case "${mode}" in
  session|check|stop) ;;
  *) echo "usage: $0 {session|check|stop}" >&2; exit 2 ;;
esac

worktrees="$(git -C "${repo_root}" worktree list --porcelain | sed -n 's/^worktree //p')"
primary_root="${worktrees%%$'\n'*}"
[[ -n "${primary_root}" ]] || exit 1
# Keep one local kernel lock across worktrees, independent of temporary paths.
mkdir -p "${primary_root}/.cache"
exec {session_lock_fd}>"${primary_root}/.cache/.n2api-dev-session.lock"
flock -n -x "${session_lock_fd}" || {
  echo "development_status=blocked reason=active_development_session" >&2
  exit 1
}

export N2API_ENV_FILE="${primary_root}/.env"
export N2API_DEV_BACKUP_DIR="${primary_root}/backups"
compose=(docker compose --project-name deploy --file "${repo_root}/deploy/compose.yaml")
if [[ -f "${N2API_ENV_FILE}" ]]; then
  compose+=(--env-file "${N2API_ENV_FILE}")
fi
containers="$("${compose[@]}" ps --all --quiet)"
while IFS= read -r container; do
  [[ -n "${container}" ]] || continue
  config_files="$(docker inspect --format '{{ index .Config.Labels "com.docker.compose.project.config_files" }}' "${container}")"
  owned=0
  while IFS= read -r worktree; do
    [[ "${config_files}" == "${worktree}/deploy/compose.yaml" ]] && owned=1
  done <<<"${worktrees}"
  if (( owned == 0 )); then
    echo "development_status=blocked reason=foreign_compose_project container=${container}" >&2
    exit 1
  fi
done <<<"${containers}"

if [[ "${mode}" != stop ]]; then
  "${repo_root}/dev/maintenance/disk-check.sh" --heavy
fi
unset N2API_TEST_COMPOSE_PROJECT N2API_TEST_COMPOSE_FILE
n2api_run_init dev-session
stack_started=0
build_started=0

cleanup_development() {
  local status=$?
  trap - EXIT INT TERM HUP
  if (( stack_started == 1 )); then
    if ! "${compose[@]}" down --remove-orphans --rmi local; then
      echo "development_status=failed reason=teardown_incomplete" >&2
      status=1
    fi
  fi
  if (( build_started == 1 )); then
    docker builder prune --all --force || status=1
  fi
  n2api_run_cleanup "${status}"
}
trap cleanup_development EXIT

if [[ "${mode}" == stop ]]; then
  stack_started=1
  exit 0
fi

docker builder prune --all --force
build_started=1
n2api_run_command "${compose[@]}" build --no-cache
stack_started=1
n2api_run_command "${compose[@]}" up -d --force-recreate --wait --wait-timeout 120
"${compose[@]}" ps
container_id="$("${compose[@]}" ps --quiet n2api)"
[[ -n "${container_id}" ]]
for path in livez readyz version; do
  docker exec "${container_id}" wget -T 10 -qO- "http://127.0.0.1:3000/${path}"
done
docker exec "${container_id}" wget -T 10 -q -O /dev/null http://127.0.0.1:3000/
echo "development_status=verified"

if [[ "${mode}" == session ]]; then
  echo "Development session ready; keep this process open and press Ctrl-C to clean up."
  n2api_run_command "${compose[@]}" up --no-build --abort-on-container-exit
fi

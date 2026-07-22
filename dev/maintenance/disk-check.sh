#!/usr/bin/env bash

set -Eeuo pipefail

repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd -P)"
cache_root="${N2API_DEV_CACHE_ROOT:-${repo_root}/.cache/dev}"
tmp_root="${N2API_TMP_ROOT:-${TMPDIR:-/tmp}}"
check_path="${N2API_DISK_CHECK_PATH:-/}"
warn_percent="${N2API_DISK_WARN_PERCENT:-80}"
min_free_gib="${N2API_DISK_MIN_FREE_GIB:-10}"
heavy=0

if [[ ${1:-} == "--heavy" ]]; then
  heavy=1
elif [[ $# -ne 0 ]]; then
  echo "usage: $0 [--heavy]" >&2
  exit 2
fi

for value in "${warn_percent}" "${min_free_gib}"; do
  [[ "${value}" =~ ^[0-9]+$ ]] || {
    echo "disk_check_status=failed reason=invalid_threshold value=${value}" >&2
    exit 2
  }
done

read -r _ total_kib used_kib available_kib capacity _ < <(df -Pk "${check_path}" | awk 'NR == 2')
used_percent="${capacity%%%}"
available_gib=$((available_kib / 1024 / 1024))

print_summary() {
  local ids count
  echo
  echo "Production persistent data (never auto-cleaned):"
  if command -v docker >/dev/null 2>&1 && docker info >/dev/null 2>&1; then
    ids="$(docker volume ls --quiet --filter 'label=com.docker.compose.project=deploy' 2>/dev/null || true)"
    if [[ -n "${ids}" ]]; then
      sed 's/^/  volume: /' <<<"${ids}"
    else
      echo "  no deploy-labelled volumes found"
    fi
  else
    echo "  Docker unavailable; production Docker inventory not inspected"
  fi

  echo "Reclaimable N2API test resources:"
  du -sh "${cache_root}" 2>/dev/null | sed 's/^/  controlled cache: /' || echo "  controlled cache: 0"
  count="$(find "${tmp_root}" -maxdepth 2 -type f -name .n2api-test-run 2>/dev/null | wc -l | tr -d ' ')"
  echo "  marked temporary runs: ${count}"
  if command -v docker >/dev/null 2>&1 && docker info >/dev/null 2>&1; then
    for kind in container network volume image; do
      case "${kind}" in
        container) ids="$(docker container ls --all --quiet --filter 'label=io.knowsky.n2api.resource=test' 2>/dev/null || true)" ;;
        *) ids="$(docker "${kind}" ls --quiet --filter 'label=io.knowsky.n2api.resource=test' 2>/dev/null || true)" ;;
      esac
      count="$(sed '/^$/d' <<<"${ids}" | wc -l | tr -d ' ')"
      echo "  labelled Docker ${kind}s: ${count}"
    done
  else
    echo "  labelled Docker resources: unavailable"
  fi

  echo "Unknown or shared resources (reported only):"
  if command -v docker >/dev/null 2>&1 && docker info >/dev/null 2>&1; then
    docker system df 2>/dev/null | sed 's/^/  /' || true
  else
    echo "  Docker unavailable"
  fi
}

echo "disk_check_path=${check_path} used_percent=${used_percent} available_gib=${available_gib} warn_percent=${warn_percent} min_free_gib=${min_free_gib}"

if (( used_percent >= warn_percent )); then
  echo "disk_check_warning=root_usage_high used_percent=${used_percent}" >&2
fi

if (( heavy == 1 && available_gib < min_free_gib )); then
  echo "disk_check_status=blocked reason=insufficient_free_space available_gib=${available_gib} required_gib=${min_free_gib}" >&2
  print_summary
  echo
  echo "Run 'make clean-dev-artifacts' and retry. Use 'make clean-dev-artifacts-deep' only for project-scoped cache removal."
  exit 1
fi

if (( used_percent >= warn_percent )); then
  print_summary
fi
echo "disk_check_status=passed"

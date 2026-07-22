#!/usr/bin/env bash

set -Eeuo pipefail

repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd -P)"
source "${repo_root}/dev/lib/test-resources.sh"

mode=${1:-}
if [[ -z "${mode}" ]]; then
  echo "usage: $0 {unit|request-log-profile|gateway-e2e|contracts|playwright-install|playwright} [args...]" >&2
  exit 2
fi
shift

case "${mode}" in
  unit|request-log-profile|gateway-e2e|contracts|playwright-install|playwright)
    "${repo_root}/dev/maintenance/disk-check.sh" --heavy
    ;;
  *) echo "unknown test mode: ${mode}" >&2; exit 2 ;;
esac

n2api_run_init "${mode}"

run_compose() {
  n2api_run_command env N2API_TEST_RUN_ID="${N2API_TEST_RUN_ID}" \
    docker compose --project-name "${N2API_TEST_COMPOSE_PROJECT}" \
    --file "${N2API_TEST_COMPOSE_FILE}" "$@"
}

case "${mode}" in
  unit)
    n2api_run_command bash -c 'cd "$1/backend" && go test ./...' _ "${repo_root}"
    n2api_run_command bash -c '
      cd "$1/frontend"
      bun test
      bun run check
      bun run build
    ' _ "${repo_root}"
    ;;
  request-log-profile)
    n2api_run_command bash -c '
      cd "$1/backend"
      go test -count=1 -run TestRequestLogQueryProfile -v ./internal/store
    ' _ "${repo_root}"
    ;;
  gateway-e2e)
    project="n2api-${N2API_RUN_ID}"
    n2api_register_compose "${repo_root}/deploy/compose.e2e.yaml" "${project}"
    run_compose build gateway-e2e
    run_compose up -d --build --wait postgres mock-openai n2api
    run_compose run --rm --no-deps gateway-e2e
    ;;
  contracts)
    project="n2api-${N2API_RUN_ID}"
    n2api_register_compose "${repo_root}/deploy/compose.e2e.yaml" "${project}"
    run_compose --profile contracts build contracts-javascript contracts-python
    run_compose up -d --build --wait postgres mock-openai n2api
    run_compose --profile contracts run --rm --no-deps contracts-javascript
    run_compose --profile contracts run --rm --no-deps contracts-python
    ;;
  playwright-install)
    version="${N2API_PLAYWRIGHT_VERSION:-1.61.1}"
    n2api_run_command bunx --package "@playwright/test@${version}" playwright install chromium "$@"
    ;;
  playwright)
    version="${N2API_PLAYWRIGHT_VERSION:-1.61.1}"
    if [[ ${1:-} == "test" ]]; then
      shift
      n2api_run_command bunx --package "@playwright/test@${version}" playwright test \
        --output="${N2API_RUN_DIR}/artifacts/playwright/test-results" "$@"
    else
      n2api_run_command bunx --package "@playwright/test@${version}" playwright "$@"
    fi
    ;;
esac

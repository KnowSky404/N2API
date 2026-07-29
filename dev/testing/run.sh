#!/usr/bin/env bash

set -Eeuo pipefail

repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd -P)"
source "${repo_root}/dev/lib/test-resources.sh"

mode=${1:-}
if [[ -z "${mode}" ]]; then
  echo "usage: $0 {unit|go-quality|critical-race|control-connections|postgres-faults|request-log-profile|management-list-profile|gateway-e2e|contracts|playwright-install|playwright} [args...]" >&2
  exit 2
fi
shift

case "${mode}" in
  unit|go-quality|critical-race|control-connections|postgres-faults|request-log-profile|management-list-profile|gateway-e2e|contracts|playwright-install|playwright)
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
    n2api_run_command bash -c '
      set -euo pipefail
      cd "$1/backend"
      go test ./...
    ' _ "${repo_root}"
    n2api_run_command bash -c '
      set -euo pipefail
      cd "$1/frontend"
      bun run check
      bun test
      bun run build
    ' _ "${repo_root}"
    ;;
  go-quality)
    n2api_run_command bash -c '
      set -euo pipefail
      cd "$1/backend"
      tool_bin="$N2API_RUN_DIR/tools"
      mkdir -p "$tool_bin"
      GOBIN="$tool_bin" go install honnef.co/go/tools/cmd/staticcheck@v0.7.0
      version="$($tool_bin/staticcheck -version)"
      case "$version" in
        "staticcheck 2026.1"*) ;;
        *) echo "unexpected Staticcheck version: $version" >&2; exit 1 ;;
      esac
      "$tool_bin/staticcheck" ./...
      go vet ./...
    ' _ "${repo_root}"
    ;;
  critical-race)
    project="n2api-${N2API_RUN_ID}"
    n2api_register_compose "${repo_root}/deploy/compose.e2e.yaml" "${project}"
    run_compose up -d --wait postgres
    postgres_container="$(run_compose ps -q postgres)"
    postgres_host="$(docker inspect --format '{{range .NetworkSettings.Networks}}{{.IPAddress}}{{end}}' "${postgres_container}")"
    if [[ ! "${postgres_host}" =~ ^[0-9]+\.[0-9]+\.[0-9]+\.[0-9]+$ ]]; then
      echo "critical race test database has no isolated Docker network address" >&2
      exit 1
    fi
    n2api_run_command bash -c '
      set -euo pipefail
      cd "$1/backend"
      export N2API_STORE_TEST_ALLOW_DESTRUCTIVE=1
      export N2API_STORE_TEST_DATABASE_URL="$2"
      go test -race -count=1 -p=1 ./cmd/n2api ./internal/admin ./internal/gateway ./internal/httpapi ./internal/provider ./internal/store ./internal/alerting
    ' _ "${repo_root}" "postgres://n2api:e2e-postgres-password@${postgres_host}:5432/n2api_e2e?sslmode=disable"
    ;;
  control-connections)
    project="n2api-${N2API_RUN_ID}"
    n2api_register_compose "${repo_root}/deploy/compose.e2e.yaml" "${project}"
    run_compose up -d --wait postgres
    postgres_container="$(run_compose ps -q postgres)"
    postgres_host="$(docker inspect --format '{{range .NetworkSettings.Networks}}{{.IPAddress}}{{end}}' "${postgres_container}")"
    if [[ ! "${postgres_host}" =~ ^[0-9]+\.[0-9]+\.[0-9]+\.[0-9]+$ ]]; then
      echo "control connection test database has no isolated Docker network address" >&2
      exit 1
    fi
    n2api_run_command bash -c '
      set -euo pipefail
      cd "$1/backend"
      export N2API_STORE_TEST_ALLOW_DESTRUCTIVE=1
      export N2API_STORE_TEST_DATABASE_URL="$2"
      go test -count=1 -run "Test(AllMigrationsRoundTrip|GatewaySettingsRuntime|PostgresConnection|PostgresPool|InstanceLock|MigrationLock|SystemEventSubscription|APIKeyAuthentication|APIKeyBudget|Management)" ./internal/store
      go test -race -count=1 -run "Test(APIKeyAuthentication|APIKeyBudget)" ./internal/store
      go test -count=1 -run TestInstanceLockProcessLifecycle ./cmd/n2api
    ' _ "${repo_root}" "postgres://n2api:e2e-postgres-password@${postgres_host}:5432/n2api_e2e?sslmode=disable"
    ;;
  postgres-faults)
    project="n2api-${N2API_RUN_ID}"
    n2api_register_compose "${repo_root}/deploy/compose.e2e.yaml" "${project}"
    run_compose up -d --wait postgres
    run_compose build n2api
    fault_container="${project}-n2api-fault"
    run_compose run --detach --no-deps \
      --name "${fault_container}" \
      --env N2API_ALLOW_UNSAFE_MULTI_INSTANCE=true \
      n2api >/dev/null
    postgres_container="$(run_compose ps -q postgres)"
    [[ -n "${postgres_container}" ]] || {
      echo "PostgreSQL fault test container is unavailable" >&2
      exit 1
    }
    n2api_run_command bash -c '
      set -Eeuo pipefail
      app_container=$1
      postgres_container=$2
      paused=0
      unpause_postgres() {
        if [[ ${paused} -eq 1 ]]; then
          docker unpause "${postgres_container}" >/dev/null 2>&1 || true
          paused=0
        fi
      }
      trap unpause_postgres EXIT INT TERM

      ready=0
      for _ in {1..150}; do
        if docker exec "${app_container}" wget -q -O /dev/null http://127.0.0.1:3000/readyz; then
          ready=1
          break
        fi
        sleep 0.1
      done
      [[ ${ready} -eq 1 ]] || { echo "N2API did not become ready before PostgreSQL pause" >&2; exit 1; }

      docker pause "${postgres_container}" >/dev/null
      paused=1
      unavailable=0
      for _ in {1..20}; do
        set +e
        response="$(docker exec "${app_container}" wget -S -T 3 -t 1 -O /dev/null http://127.0.0.1:3000/readyz 2>&1)"
        status=$?
        set -e
        if [[ ${status} -ne 0 && "${response}" == *"503 Service Unavailable"* ]]; then
          unavailable=1
          break
        fi
        sleep 0.1
      done
      [[ ${unavailable} -eq 1 ]] || { echo "readiness did not report HTTP 503 while PostgreSQL was paused" >&2; exit 1; }
      docker exec "${app_container}" wget -q -O /dev/null http://127.0.0.1:3000/livez

      docker unpause "${postgres_container}" >/dev/null
      paused=0
      recovered=0
      for _ in {1..150}; do
        if docker exec "${app_container}" wget -q -O /dev/null http://127.0.0.1:3000/readyz; then
          recovered=1
          break
        fi
        sleep 0.1
      done
      [[ ${recovered} -eq 1 ]] || { echo "readiness did not recover after PostgreSQL unpause" >&2; exit 1; }
      echo "postgres_pause_status=passed readiness=503 livez=200 recovered=200"
    ' _ "${fault_container}" "${postgres_container}"
    ;;
  request-log-profile)
	project="n2api-${N2API_RUN_ID}"
	n2api_register_compose "${repo_root}/deploy/compose.e2e.yaml" "${project}"
	run_compose up -d --wait postgres
	postgres_container="$(run_compose ps -q postgres)"
	postgres_host="$(docker inspect --format '{{range .NetworkSettings.Networks}}{{.IPAddress}}{{end}}' "${postgres_container}")"
	if [[ ! "${postgres_host}" =~ ^[0-9]+\.[0-9]+\.[0-9]+\.[0-9]+$ ]]; then
		echo "request log profile database has no isolated Docker network address" >&2
		exit 1
	fi
    n2api_run_command bash -c '
      set -euo pipefail
      cd "$1/backend"
      N2API_REQUEST_LOG_QUERY_PROFILE=1 \
      N2API_STORE_TEST_ALLOW_DESTRUCTIVE=1 \
      N2API_STORE_TEST_DATABASE_URL="$2" \
        go test -count=1 -run TestRequestLogQueryProfile -v ./internal/store
    ' _ "${repo_root}" "postgres://n2api:e2e-postgres-password@${postgres_host}:5432/n2api_e2e?sslmode=disable"
    ;;
  management-list-profile)
	project="n2api-${N2API_RUN_ID}"
	n2api_register_compose "${repo_root}/deploy/compose.e2e.yaml" "${project}"
	run_compose up -d --wait postgres
	postgres_container="$(run_compose ps -q postgres)"
	postgres_host="$(docker inspect --format '{{range .NetworkSettings.Networks}}{{.IPAddress}}{{end}}' "${postgres_container}")"
	if [[ ! "${postgres_host}" =~ ^[0-9]+\.[0-9]+\.[0-9]+\.[0-9]+$ ]]; then
		echo "management list profile database has no isolated Docker network address" >&2
		exit 1
	fi
    n2api_run_command bash -c '
      set -euo pipefail
      cd "$1/backend"
      N2API_MANAGEMENT_LIST_QUERY_PROFILE=1 \
      N2API_STORE_TEST_ALLOW_DESTRUCTIVE=1 \
      N2API_STORE_TEST_DATABASE_URL="$2" \
        go test -count=1 -run TestManagementListQueryProfile -v ./internal/store
    ' _ "${repo_root}" "postgres://n2api:e2e-postgres-password@${postgres_host}:5432/n2api_e2e?sslmode=disable"
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

#!/usr/bin/env bash

set -u
set -o pipefail

if [ "$#" -ne 4 ]; then
  echo "usage: collect-e2e-diagnostics.sh PROJECT SUITE RAW_DIR SAFE_DIR" >&2
  exit 2
fi

project=$1
suite=$2
raw_dir=$3
safe_dir=$4
script_dir=$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)
repo_root=$(cd "$script_dir/../.." && pwd)
run_id="${GITHUB_RUN_ID:-0}-${GITHUB_RUN_ATTEMPT:-0}"
canary_file="$raw_dir/sanitization-canary.txt"

umask 077
mkdir -p "$raw_dir" "$safe_dir"

compose=(
  docker compose
  --project-name "$project"
  -f "$repo_root/deploy/compose.e2e.yaml"
)

# Every collection command is best effort. Raw diagnostics stay in the runner's
# temporary directory and are never written to stdout.
if ! timeout 15s "${compose[@]}" ps --all --format json >"$raw_dir/services.json" 2>/dev/null; then
  : >"$raw_dir/services.json"
fi

case "$suite" in
  mock-smoke)
    services=(mock-openai)
    ;;
  gateway)
    services=(postgres mock-openai n2api gateway-e2e)
    ;;
  sdk-contracts)
    services=(postgres mock-openai n2api contracts-javascript contracts-python)
    ;;
  *)
    services=(postgres mock-openai n2api)
    ;;
esac

for service in "${services[@]}"; do
  log_file="$raw_dir/$service.log"
  if [ -s "$log_file" ]; then
    continue
  fi
  if ! timeout 15s "${compose[@]}" logs --no-color --timestamps --tail 500 "$service" >"$log_file" 2>/dev/null; then
    : >"$log_file"
  fi
done

if ! timeout 15s "${compose[@]}" exec -T mock-openai \
  curl --fail --silent --show-error http://127.0.0.1:8080/__mock/state \
  >"$raw_dir/mock-state.json" 2>/dev/null; then
  : >"$raw_dir/mock-state.json"
fi

request_log_query=$(cat <<'SQL'
COPY (
  SELECT request_id,
    client_key_id,
    COALESCE(provider_account_id, 0) AS provider_account_id,
    COALESCE(routing_pool_id, 0) AS routing_pool_id,
    method,
    route,
    status_code,
    latency_ms,
    error,
    usage_source,
    gateway_attempt_count,
    gateway_fallback_count,
    created_at
  FROM request_logs
  WHERE client_key_id IS NOT NULL
  ORDER BY id DESC
  LIMIT 50
) TO STDOUT WITH (FORMAT CSV, HEADER TRUE);
SQL
)
if ! timeout 15s "${compose[@]}" exec -T postgres \
  psql --no-psqlrc --quiet --set ON_ERROR_STOP=1 --username n2api --dbname n2api_e2e \
  --command "$request_log_query" >"$raw_dir/request-logs.csv" 2>/dev/null; then
  : >"$raw_dir/request-logs.csv"
fi

cat >"$canary_file" <<'CANARY'
e2e-upstream-fixture-key
e2e-postgres-password
e2e-admin-password
e2e-encryption-secret-with-enough-length
protocol contract
JavaScript SDK contract request
Python SDK contract request
n2api-e2e-diagnostic-canary-bearer
n2api-e2e-diagnostic-canary-cookie
n2api-e2e-diagnostic-canary-set-cookie
n2api-e2e-diagnostic-canary-password
n2api-e2e-diagnostic-canary-api-key
n2api-e2e-diagnostic-canary-encrypted
n2api-e2e-diagnostic-canary-prompt
n2api-e2e-diagnostic-canary-response
CANARY

if [ -n "${N2API_E2E_DIAGNOSTIC_SECRET_CANARY:-}" ]; then
  printf '%s\n' "$N2API_E2E_DIAGNOSTIC_SECRET_CANARY" >>"$canary_file"
fi
if [ -n "${N2API_E2E_DIAGNOSTIC_BODY_CANARY:-}" ]; then
  printf '%s\n' "$N2API_E2E_DIAGNOSTIC_BODY_CANARY" >>"$canary_file"
fi

if ! (
  cd "$repo_root/backend"
  GOCACHE="$raw_dir/go-build-cache" timeout 60s go run ./cmd/e2e-diagnostics \
    --suite "$suite" \
    --run-id "$run_id" \
    --raw-dir "$raw_dir" \
    --output-dir "$safe_dir" \
    --canary-file "$canary_file"
) >"$raw_dir/sanitizer.stdout" 2>"$raw_dir/sanitizer.stderr"; then
  echo "E2E diagnostic sanitization failed; artifact upload is disabled" >&2
  exit 1
fi

if [ ! -f "$safe_dir/safe.marker" ]; then
  echo "E2E diagnostic safety marker is missing; artifact upload is disabled" >&2
  exit 1
fi

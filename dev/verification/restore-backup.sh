#!/usr/bin/env bash
set -Eeuo pipefail

umask 077

usage() {
  echo "usage: N2API_RESTORE_IMAGE=... N2API_RESTORE_ADMIN_USERNAME=... N2API_RESTORE_ADMIN_PASSWORD=... N2API_RESTORE_ENCRYPTION_SECRET=... $0 /absolute/path/to/backup.dump" >&2
}

if [[ $# -ne 1 ]]; then
  usage
  exit 2
fi

for name in N2API_RESTORE_IMAGE N2API_RESTORE_ADMIN_USERNAME N2API_RESTORE_ADMIN_PASSWORD N2API_RESTORE_ENCRYPTION_SECRET; do
  if [[ -z "${!name:-}" ]]; then
    echo "restore_status=failed stage=config missing=${name}" >&2
    exit 2
  fi
done

for command in docker readlink; do
  if ! command -v "${command}" >/dev/null 2>&1; then
    echo "restore_status=failed stage=config missing_command=${command}" >&2
    exit 2
  fi
done

if ! dump_path="$(readlink -f -- "$1")"; then
  echo "restore_status=failed stage=config invalid_dump_path" >&2
  exit 2
fi
if [[ ! -f "${dump_path}" || ! -r "${dump_path}" ]]; then
  echo "restore_status=failed stage=config invalid_dump_path" >&2
  exit 2
fi

repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd -P)"
compose_file="${repo_root}/deploy/compose.restore-test.yaml"
suffix="$(date -u +%s)-$$-$(od -An -N4 -tx4 /dev/urandom | tr -d ' ')"
project="n2api-restore-${suffix}"
volume="${project}_restore-postgres"
stage="config"
cleanup_armed=0

export N2API_RESTORE_DUMP_PATH="${dump_path}"
export N2API_RESTORE_API_KEY_ID=""

compose() {
  docker compose --project-name "${project}" --file "${compose_file}" "$@"
}

cleanup() {
  local status=$?
  trap - EXIT INT TERM
  if [[ ${cleanup_armed} -eq 1 ]]; then
    compose down --volumes --remove-orphans --timeout 10 >/dev/null 2>&1 || true
  fi
  if [[ ${status} -ne 0 ]]; then
    echo "restore_status=failed stage=${stage}" >&2
  fi
  exit "${status}"
}

trap cleanup EXIT
trap 'exit 130' INT
trap 'exit 143' TERM

mapfile -t existing_containers < <(docker ps --all --quiet --filter "label=com.docker.compose.project=${project}")
mapfile -t existing_volumes < <(docker volume ls --quiet --filter "label=com.docker.compose.project=${project}")
mapfile -t existing_networks < <(docker network ls --quiet --filter "label=com.docker.compose.project=${project}")
if [[ ${#existing_containers[@]} -ne 0 || ${#existing_volumes[@]} -ne 0 || ${#existing_networks[@]} -ne 0 ]] || docker volume inspect "${volume}" >/dev/null 2>&1; then
  echo "restore_status=failed stage=safety target_project_exists" >&2
  exit 1
fi
cleanup_armed=1

stage="archive_list"
compose up --detach --wait postgres >/dev/null
compose exec --no-TTY postgres pg_restore --list /restore/input.dump >/dev/null

stage="restore"
compose exec --no-TTY postgres pg_restore \
  --exit-on-error \
  --single-transaction \
  --no-owner \
  --no-privileges \
  --username n2api_restore \
  --dbname n2api_restore \
  /restore/input.dump >/dev/null

stage="readiness"
compose up --detach --build --wait mock-openai n2api >/dev/null

db_query() {
  compose exec --no-TTY postgres psql \
    --set ON_ERROR_STOP=1 \
    --username n2api_restore \
    --dbname n2api_restore \
    --tuples-only \
    --no-align \
    --command "$1"
}

stage="integrity"
schema_version="$(db_query "SELECT COALESCE(max(version_id), 0) FROM schema_migrations" | tr -d '[:space:]')"
admin_count="$(db_query "SELECT count(*) FROM admins" | tr -d '[:space:]')"
provider_count="$(db_query "SELECT count(*) FROM provider_accounts" | tr -d '[:space:]')"
client_key_count="$(db_query "SELECT count(*) FROM client_api_keys" | tr -d '[:space:]')"
request_log_count="$(db_query "SELECT count(*) FROM request_logs" | tr -d '[:space:]')"
orphan_count="$(db_query "SELECT (SELECT count(*) FROM provider_account_credentials c LEFT JOIN provider_accounts a ON a.id = c.account_id WHERE a.id IS NULL) + (SELECT count(*) FROM provider_account_models m LEFT JOIN provider_accounts a ON a.id = m.account_id WHERE a.id IS NULL) + (SELECT count(*) FROM client_api_key_models m LEFT JOIN client_api_keys k ON k.id = m.client_key_id WHERE k.id IS NULL) + (SELECT count(*) FROM routing_pool_accounts m LEFT JOIN routing_pools p ON p.id = m.pool_id LEFT JOIN provider_accounts a ON a.id = m.account_id WHERE p.id IS NULL OR a.id IS NULL)" | tr -d '[:space:]')"

if [[ "${schema_version}" -le 0 || "${admin_count}" -le 0 || "${orphan_count}" -ne 0 ]]; then
  exit 1
fi

N2API_RESTORE_API_KEY_ID="$(db_query "SELECT id FROM client_api_keys WHERE encrypted_secret <> '' AND revoked_at IS NULL ORDER BY id LIMIT 1" | tr -d '[:space:]')"
export N2API_RESTORE_API_KEY_ID

compose build gateway-restore >/dev/null

if [[ -n "${N2API_RESTORE_API_KEY_ID}" ]]; then
  stage="restored_secret"
  compose run --rm gateway-restore '-test.run=^TestRestoredAPIKeySecretDecrypts$' >/dev/null
fi

stage="gateway"
compose run --rm gateway-restore '-test.run=^TestGatewayPostgresBackedHappyPath$' >/dev/null

echo "restore_status=passed"
echo "schema_version=${schema_version}"
echo "admin_count=${admin_count}"
echo "provider_account_count=${provider_count}"
echo "client_api_key_count=${client_key_count}"
echo "request_log_count=${request_log_count}"
echo "orphan_count=${orphan_count}"
if [[ -n "${N2API_RESTORE_API_KEY_ID}" ]]; then
  echo "restored_secret_check=passed"
else
  echo "restored_secret_check=skipped_no_reusable_key"
fi
echo "gateway_status=passed"

#!/usr/bin/env bash

set -euo pipefail

repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
cd "$repo_root"

tmp_dir="$(mktemp -d)"
trap 'rm -rf -- "$tmp_dir"' EXIT
digest="sha256:$(printf 'a%.0s' {1..64})"
image="ghcr.io/knowsky404/n2api:2026.07.29@$digest"
for name in database-url admin-password encryption-secret postgres-password metrics-token; do
  printf 'safe-test-%s\n' "$name" >"$tmp_dir/$name"
done
printf '%s\n' 'postgres://n2api:safe-test-postgres-password@postgres:5432/n2api?sslmode=disable' >"$tmp_dir/database-url"
printf '%s\n' 'safe-test-encryption-secret-at-least-32-bytes' >"$tmp_dir/encryption-secret"

env_file="$tmp_dir/release.env"
cat >"$env_file" <<EOF
N2API_ENV_FILE=$env_file
N2API_IMAGE=$image
N2API_PUBLIC_URL=https://n2api.example.test
N2API_ACCEPT_RISKS=database-plaintext
DATABASE_URL=postgres://n2api:safe-test-postgres-password@postgres:5432/n2api?sslmode=disable
N2API_ADMIN_PASSWORD=safe-test-admin-password
N2API_ENCRYPTION_SECRET=safe-test-encryption-secret-at-least-32-bytes
POSTGRES_PASSWORD=safe-test-postgres-password
N2API_METRICS_BEARER_TOKEN=safe-test-metrics-token
N2API_DATABASE_URL_SOURCE_FILE=$tmp_dir/database-url
N2API_ADMIN_PASSWORD_SOURCE_FILE=$tmp_dir/admin-password
N2API_ENCRYPTION_SECRET_SOURCE_FILE=$tmp_dir/encryption-secret
N2API_POSTGRES_PASSWORD_SOURCE_FILE=$tmp_dir/postgres-password
N2API_METRICS_BEARER_TOKEN_SOURCE_FILE=$tmp_dir/metrics-token
EOF

compose=(docker compose --env-file "$env_file" -f deploy/compose.release.yaml)
"${compose[@]}" config --quiet
"${compose[@]}" config --format json >"$tmp_dir/plain.json"
jq -e '
  .services.n2api.cpus == 1 and
  .services.n2api.mem_limit == "536870912" and
  .services.n2api.pids_limit == 256 and
  .services.n2api.ulimits.nofile.soft == 4096 and
  .services.n2api.logging.options["max-size"] == "10m" and
  (.services.n2api.tmpfs[] | contains("size=16m")) and
  .services.postgres.mem_limit == "805306368" and
  .services.postgres.pids_limit == 256 and
  (.services.postgres.tmpfs[] | contains("size=64m"))
' "$tmp_dir/plain.json" >/dev/null

secret_compose=("${compose[@]}" -f deploy/compose.release.secrets.yaml)
"${secret_compose[@]}" config --quiet
"${secret_compose[@]}" config --format json >"$tmp_dir/secrets.json"
jq -e '
  .services.n2api.environment.DATABASE_URL == "" and
  .services.n2api.environment.DATABASE_URL_FILE == "/run/secrets/n2api_database_url" and
  .services.postgres.environment.POSTGRES_PASSWORD == "" and
  .services.postgres.environment.POSTGRES_PASSWORD_FILE == "/run/secrets/n2api_postgres_password"
' "$tmp_dir/secrets.json" >/dev/null

metrics_compose=("${compose[@]}" -f deploy/compose.metrics.yaml)
"${metrics_compose[@]}" config --quiet
"${metrics_compose[@]}" config --format json >"$tmp_dir/metrics.json"
jq -e '.services.n2api.environment.N2API_METRICS_BEARER_TOKEN == "safe-test-metrics-token"' "$tmp_dir/metrics.json" >/dev/null

metrics_secret_compose=("${secret_compose[@]}" -f deploy/compose.metrics.yaml -f deploy/compose.metrics.secrets.yaml)
"${metrics_secret_compose[@]}" config --quiet
"${metrics_secret_compose[@]}" config --format json >"$tmp_dir/metrics-secrets.json"
jq -e '
  .services.n2api.environment.N2API_METRICS_BEARER_TOKEN == "" and
  .services.n2api.environment.N2API_METRICS_BEARER_TOKEN_FILE == "/run/secrets/n2api_metrics_bearer_token"
' "$tmp_dir/metrics-secrets.json" >/dev/null

env N2API_CPU_LIMIT=1.5 N2API_MEMORY_LIMIT=640m N2API_PIDS_LIMIT=192 \
  N2API_NOFILE_SOFT=2048 N2API_NOFILE_HARD=4096 N2API_TMPFS_SIZE=24m \
  POSTGRES_CPU_LIMIT=0.75 POSTGRES_MEMORY_LIMIT=896m POSTGRES_PIDS_LIMIT=160 \
  POSTGRES_NOFILE_SOFT=3072 POSTGRES_NOFILE_HARD=6144 POSTGRES_TMPFS_SIZE=80m \
  "${compose[@]}" config --format json >"$tmp_dir/custom.json"
jq -e '
  .services.n2api.cpus == 1.5 and
  .services.n2api.mem_limit == "671088640" and
  .services.n2api.pids_limit == 192 and
  .services.n2api.ulimits.nofile.hard == 4096 and
  (.services.n2api.tmpfs[] | contains("size=24m")) and
  .services.postgres.cpus == 0.75 and
  .services.postgres.mem_limit == "939524096" and
  .services.postgres.pids_limit == 160 and
  .services.postgres.ulimits.nofile.hard == 6144 and
  (.services.postgres.tmpfs[] | contains("size=80m"))
' "$tmp_dir/custom.json" >/dev/null

for invalid in \
  'N2API_PIDS_LIMIT=not-a-number' \
  'N2API_MEMORY_LIMIT=not-a-size' \
  'POSTGRES_PIDS_LIMIT=not-a-number'; do
  if env "$invalid" "${compose[@]}" config --quiet >/dev/null 2>&1; then
    echo "Compose accepted invalid production setting: $invalid" >&2
    exit 1
  fi
done

dev/ci/test-release-image.sh
echo "Production deployment configuration tests passed."

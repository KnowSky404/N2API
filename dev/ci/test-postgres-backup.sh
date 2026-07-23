#!/usr/bin/env bash

set -Eeuo pipefail

repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd -P)"
backup_script="${repo_root}/deploy/postgres-backup.sh"
fixture="$(mktemp -d "${TMPDIR:-/tmp}/n2api-postgres-backup-test.XXXXXXXX")"

cleanup() {
  rm -rf -- "${fixture}"
}
trap cleanup EXIT

fail() {
  echo "FAIL: $*" >&2
  exit 1
}

fake_bin="${fixture}/bin"
backup_dir="${fixture}/backups"
mkdir -p "${fake_bin}" "${backup_dir}"

cat >"${fake_bin}/pg_dump" <<'PG_DUMP'
#!/usr/bin/env bash
set -euo pipefail
output=
for argument in "$@"; do
  case "${argument}" in
    --file=*) output=${argument#--file=} ;;
  esac
done
[[ -n "${output}" ]] || exit 3
[[ "${N2API_FAKE_PG_DUMP_FAIL:-0}" != "1" ]] || exit 4
printf 'custom archive fixture\n' >"${output}"
PG_DUMP

cat >"${fake_bin}/pg_restore" <<'PG_RESTORE'
#!/usr/bin/env bash
set -euo pipefail
[[ "${1:-}" == "--list" && -s "${2:-}" ]]
printf 'archive list fixture\n'
PG_RESTORE

chmod +x "${fake_bin}/pg_dump" "${fake_bin}/pg_restore"

run_backup() {
  PATH="${fake_bin}:${PATH}" \
  PGDATABASE=n2api_test \
  PGUSER=n2api \
  PGPASSWORD=test-password \
  N2API_BACKUP_DIR="${backup_dir}" \
  N2API_BACKUP_LOCK_DIR="${fixture}/backup.lock" \
  N2API_BACKUP_LAST_SUCCESS_FILE="${fixture}/last-success" \
  N2API_BACKUP_INTERVAL_SECONDS=300 \
  N2API_BACKUP_RETENTION_DAYS=1 \
    "${backup_script}" once
}

run_healthcheck() {
  PATH="${fake_bin}:${PATH}" \
  PGDATABASE=n2api_test \
  PGUSER=n2api \
  PGPASSWORD=test-password \
  N2API_BACKUP_DIR="${backup_dir}" \
  N2API_BACKUP_LOCK_DIR="${fixture}/backup.lock" \
  N2API_BACKUP_LAST_SUCCESS_FILE="${fixture}/last-success" \
  N2API_BACKUP_INTERVAL_SECONDS=300 \
  N2API_BACKUP_RETENTION_DAYS=1 \
    "${backup_script}" healthcheck
}

first_output="$(run_backup)"
grep -Fq 'backup_status=passed' <<<"${first_output}" || fail "successful backup lacked status"
mapfile -t dumps < <(find "${backup_dir}" -maxdepth 1 -type f -name 'n2api-*.dump')
[[ ${#dumps[@]} -eq 1 ]] || fail "first backup created ${#dumps[@]} dumps"
[[ "$(stat -c '%a' "${dumps[0]}")" == "600" ]] || fail "backup permissions are not 600"
[[ -s "${fixture}/last-success" ]] || fail "backup did not record last success"
run_healthcheck || fail "fresh backup failed its health check"
printf '0\n' >"${fixture}/last-success"
set +e
run_healthcheck
stale_health_status=$?
set -e
[[ ${stale_health_status} -ne 0 ]] || fail "stale backup passed its health check"
date -u +%s >"${fixture}/last-success"

old_dump="${backup_dir}/n2api-20000101T000000Z-old.dump"
printf 'old archive\n' >"${old_dump}"
touch -d '3 days ago' "${old_dump}"
run_backup >/dev/null
[[ ! -e "${old_dump}" ]] || fail "expired backup was retained"

before_count="$(find "${backup_dir}" -maxdepth 1 -type f -name 'n2api-*.dump' | wc -l | tr -d ' ')"
set +e
N2API_FAKE_PG_DUMP_FAIL=1 run_backup >"${fixture}/failed.out" 2>&1
failed_status=$?
set -e
[[ ${failed_status} -ne 0 ]] || fail "pg_dump failure was reported as success"
after_count="$(find "${backup_dir}" -maxdepth 1 -type f -name 'n2api-*.dump' | wc -l | tr -d ' ')"
[[ "${before_count}" == "${after_count}" ]] || fail "failed backup left a final archive"
if find "${backup_dir}" -maxdepth 1 -type f -name '.n2api-*' | grep -q .; then
  fail "failed backup left a temporary archive"
fi

echo "PostgreSQL backup script tests passed."

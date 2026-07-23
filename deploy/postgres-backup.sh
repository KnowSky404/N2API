#!/bin/sh

set -eu

umask 077

backup_dir=${N2API_BACKUP_DIR:-/backups}
interval_seconds=${N2API_BACKUP_INTERVAL_SECONDS:-21600}
retention_days=${N2API_BACKUP_RETENTION_DAYS:-7}
lock_dir=${N2API_BACKUP_LOCK_DIR:-/tmp/n2api-postgres-backup.lock}
last_success_file=${N2API_BACKUP_LAST_SUCCESS_FILE:-/tmp/n2api-postgres-backup-last-success}
backup_tmp=
lock_owned=0

fail() {
  echo "backup_status=failed stage=config reason=$1" >&2
  exit 2
}

require_uint_range() {
  name=$1
  value=$2
  minimum=$3
  maximum=$4
  case "${value}" in
    ''|*[!0-9]*) fail "invalid_${name}" ;;
  esac
  if [ "${value}" -lt "${minimum}" ] || [ "${value}" -gt "${maximum}" ]; then
    fail "invalid_${name}"
  fi
}

cleanup() {
  if [ -n "${backup_tmp}" ]; then
    rm -f -- "${backup_tmp}"
    backup_tmp=
  fi
  if [ "${lock_owned}" -eq 1 ]; then
    rmdir -- "${lock_dir}" 2>/dev/null || true
    lock_owned=0
  fi
}

trap cleanup EXIT
trap 'exit 129' HUP
trap 'exit 130' INT
trap 'exit 143' TERM

require_uint_range interval_seconds "${interval_seconds}" 300 604800
require_uint_range retention_days "${retention_days}" 1 3650
: "${PGDATABASE:?PGDATABASE is required}"
: "${PGUSER:?PGUSER is required}"
: "${PGPASSWORD:?PGPASSWORD is required}"

mkdir -p -- "${backup_dir}"
chmod 700 -- "${backup_dir}"

backup_once() {
  if ! mkdir -- "${lock_dir}" 2>/dev/null; then
    echo "backup_status=skipped reason=already_running"
    return 0
  fi
  lock_owned=1

  timestamp=$(date -u +%Y%m%dT%H%M%SZ)
  backup_tmp=$(mktemp "${backup_dir}/.n2api-${timestamp}.XXXXXX")
  suffix=${backup_tmp##*.}
  backup_final="${backup_dir}/n2api-${timestamp}-${suffix}.dump"

  if ! pg_dump \
    --format=custom \
    --no-owner \
    --no-privileges \
    --file="${backup_tmp}"; then
    echo "backup_status=failed stage=dump" >&2
    cleanup
    return 1
  fi
  if ! pg_restore --list "${backup_tmp}" >/dev/null; then
    echo "backup_status=failed stage=archive_list" >&2
    cleanup
    return 1
  fi

  chmod 600 -- "${backup_tmp}"
  mv -- "${backup_tmp}" "${backup_final}"
  backup_tmp=
  find "${backup_dir}" -mindepth 1 -maxdepth 1 -type f \
    -name 'n2api-*.dump' -mtime "+${retention_days}" -delete
  date -u +%s >"${last_success_file}"
  rmdir -- "${lock_dir}"
  lock_owned=0
  echo "backup_status=passed file=${backup_final}"
}

case "${1:-daemon}" in
  once)
    backup_once
    ;;
  daemon)
    while :; do
      backup_once || true
      sleep "${interval_seconds}"
    done
    ;;
  healthcheck)
    last_success=$(cat "${last_success_file}" 2>/dev/null || true)
    case "${last_success}" in
      ''|*[!0-9]*) exit 1 ;;
    esac
    now=$(date -u +%s)
    maximum_age=$((interval_seconds + 300))
    [ $((now - last_success)) -le "${maximum_age}" ]
    ;;
  *)
    echo "usage: $0 [once|daemon|healthcheck]" >&2
    exit 2
    ;;
esac

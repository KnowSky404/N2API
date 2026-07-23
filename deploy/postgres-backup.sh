#!/bin/sh

set -eu

umask 077

backup_dir=${N2API_BACKUP_DIR:-/backups}
interval_seconds=${N2API_BACKUP_INTERVAL_SECONDS:-21600}
retention_days=${N2API_BACKUP_RETENTION_DAYS:-7}
backup_gid=${N2API_BACKUP_GID:-1000}
lock_dir=${N2API_BACKUP_LOCK_DIR:-/tmp/n2api-postgres-backup.lock}
last_success_file=${N2API_BACKUP_LAST_SUCCESS_FILE:-/tmp/n2api-postgres-backup-last-success}
backup_tmp=
backup_state_tmp=
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
  if [ -n "${backup_state_tmp}" ]; then
    rm -f -- "${backup_state_tmp}"
    backup_state_tmp=
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
require_uint_range backup_gid "${backup_gid}" 0 2147483647
: "${PGDATABASE:?PGDATABASE is required}"
: "${PGUSER:?PGUSER is required}"
: "${PGPASSWORD:?PGPASSWORD is required}"

prepare_backup_dir() {
  mkdir -p -- "${backup_dir}"
  chown "0:${backup_gid}" -- "${backup_dir}"
  chmod 750 -- "${backup_dir}"
  find "${backup_dir}" -mindepth 1 -maxdepth 1 -type f \
    -name 'n2api-*.dump' -exec chown "0:${backup_gid}" -- '{}' ';' \
    -exec chmod 640 -- '{}' ';'
}

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

  chown "0:${backup_gid}" -- "${backup_tmp}"
  chmod 640 -- "${backup_tmp}"
  mv -- "${backup_tmp}" "${backup_final}"
  backup_tmp=
  find "${backup_dir}" -mindepth 1 -maxdepth 1 -type f \
    -name 'n2api-*.dump' -mtime "+${retention_days}" -delete
  backup_state_tmp=$(mktemp "${last_success_file}.XXXXXX")
  printf '%s\n%s\n' "$(date -u +%s)" "${backup_final}" >"${backup_state_tmp}"
  mv -- "${backup_state_tmp}" "${last_success_file}"
  backup_state_tmp=
  rmdir -- "${lock_dir}"
  lock_owned=0
  echo "backup_status=passed file=${backup_final}"
}

case "${1:-daemon}" in
  once)
    prepare_backup_dir
    backup_once
    ;;
  daemon)
    prepare_backup_dir
    while :; do
      backup_once || true
      sleep "${interval_seconds}"
    done
    ;;
  healthcheck)
    last_success=
    last_archive=
    if [ -r "${last_success_file}" ]; then
      {
        IFS= read -r last_success || true
        IFS= read -r last_archive || true
      } <"${last_success_file}"
    fi
    case "${last_success}" in
      ''|*[!0-9]*) exit 1 ;;
    esac
    case "${last_archive}" in
      "${backup_dir}"/n2api-*.dump) ;;
      *) exit 1 ;;
    esac
    archive_name=${last_archive#"${backup_dir}"/}
    case "${archive_name}" in
      */*) exit 1 ;;
    esac
    [ -f "${last_archive}" ] && [ -s "${last_archive}" ] && [ -r "${last_archive}" ] || exit 1
    now=$(date -u +%s)
    maximum_age=$((interval_seconds + 300))
    [ $((now - last_success)) -le "${maximum_age}" ]
    ;;
  *)
    echo "usage: $0 [once|daemon|healthcheck]" >&2
    exit 2
    ;;
esac

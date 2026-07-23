#!/usr/bin/env bash

set -Eeuo pipefail

umask 077

repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd -P)"
source "${repo_root}/dev/lib/test-resources.sh"

restore_script="${repo_root}/dev/verification/restore-backup.sh"
compose_file="${repo_root}/deploy/compose.e2e.yaml"
fixture_admin_username="admin"
fixture_admin_password="e2e-admin-password"
fixture_encryption_secret="e2e-encryption-secret-with-enough-length"
wrong_encryption_secret="wrong-restore-encryption-secret-with-enough-length"
historical_schema_version=46
current_schema_version=47
active_scenario="setup"

fail() {
  echo "restore_scenario_status=failed scenario=${active_scenario} reason=$1" >&2
  exit 1
}

require_command() {
  command -v "$1" >/dev/null 2>&1 || fail "missing_command_$1"
}

for command in docker readlink sed sort setsid; do
  require_command "${command}"
done
[[ -x "${restore_script}" ]] || fail "restore_script_not_executable"

"${repo_root}/dev/maintenance/disk-check.sh" --heavy
n2api_run_init restore-backup
project="n2api-${N2API_RUN_ID}"
n2api_register_compose "${compose_file}" "${project}"

current_dump="${N2API_RUN_DIR}/current.dump"
older_dump="${N2API_RUN_DIR}/schema-${historical_schema_version}.dump"
corrupt_dump="${N2API_RUN_DIR}/corrupt.dump"
scenario_root="${N2API_RUN_DIR}/restore-scenarios"
mkdir -p "${scenario_root}"

run_compose() {
  n2api_run_command env N2API_TEST_RUN_ID="${N2API_TEST_RUN_ID}" \
    docker compose --project-name "${project}" --file "${compose_file}" "$@"
}

snapshot_development_stack() {
  local kind id
  for kind in container network volume; do
    while IFS= read -r id; do
      [[ -n "${id}" ]] || continue
      case "${kind}" in
        container)
          docker container inspect --format \
            'container|{{.Id}}|{{.Name}}|{{.Image}}|{{.State.Running}}|{{.State.StartedAt}}|{{.RestartCount}}' "${id}"
          ;;
        network)
          docker network inspect --format 'network|{{.Id}}|{{.Name}}' "${id}"
          ;;
        volume)
          docker volume inspect --format 'volume|{{.Name}}|{{.CreatedAt}}' "${id}"
          ;;
      esac
    done < <(docker "${kind}" ls --quiet --filter "label=com.docker.compose.project=deploy")
  done
  if docker volume inspect deploy_n2api-postgres >/dev/null 2>&1; then
    docker volume inspect --format 'protected-volume|{{.Name}}|{{.CreatedAt}}' deploy_n2api-postgres
  fi
}

assert_development_stack_unchanged() {
  local after
  after="$(snapshot_development_stack | sort)"
  [[ "${after}" == "${development_stack_before}" ]] || fail "development_stack_changed"
}

resource_ids_for_run() {
  local run_id=$1
  local kind
  for kind in container network volume image; do
    docker "${kind}" ls --quiet \
      --filter "label=io.knowsky.n2api.resource=test" \
      --filter "label=io.knowsky.n2api.run-id=${run_id}"
    docker "${kind}" ls --quiet \
      --filter "label=com.docker.compose.project=${run_id}"
  done
}

assert_restore_run_clean() {
  local run_id=$1
  local attempt leftovers
  n2api_validate_run_id "${run_id}" || fail "invalid_restore_run_id"
  for attempt in {1..100}; do
    leftovers="$(resource_ids_for_run "${run_id}" | sort -u)"
    [[ -z "${leftovers}" ]] && return 0
    sleep 0.1
  done
  fail "restore_resources_remain"
}

restore_run_id_from_output() {
  local output_file=$1
  sed -n 's/^restore_run_id=//p' "${output_file}" | tail -n 1
}

run_restore() {
  local scenario=$1
  local dump=$2
  local encryption_secret=$3
  local stdout_file="${scenario_root}/${scenario}.stdout"
  local stderr_file="${scenario_root}/${scenario}.stderr"
  local marker_root="${scenario_root}/${scenario}-markers"
  local status run_id
  mkdir -p "${marker_root}"

  set +e
  n2api_run_command env \
    N2API_DEV_CACHE_ROOT="${marker_root}" \
    N2API_RESOURCE_LOCK_FILE="${marker_root}/resource.lock" \
    N2API_RESTORE_IMAGE="${fixture_image}" \
    N2API_RESTORE_ADMIN_USERNAME="${fixture_admin_username}" \
    N2API_RESTORE_ADMIN_PASSWORD="${fixture_admin_password}" \
    N2API_RESTORE_ENCRYPTION_SECRET="${encryption_secret}" \
    N2API_RESTORE_ENCRYPTION_KEY_ID="default" \
    N2API_RESTORE_ENCRYPTION_PREVIOUS_KEYS="[]" \
    "${restore_script}" "${dump}" >"${stdout_file}" 2>"${stderr_file}"
  status=$?
  set -e

  run_id="$(restore_run_id_from_output "${stdout_file}")"
  [[ -n "${run_id}" ]] || fail "restore_run_id_missing"
  assert_restore_run_clean "${run_id}"
  assert_development_stack_unchanged
  RESTORE_STATUS=${status}
  RESTORE_STDOUT=${stdout_file}
  RESTORE_STDERR=${stderr_file}
}

assert_passed_restore() {
  local expected_schema=$1
  [[ ${RESTORE_STATUS} -eq 0 ]] || fail "unexpected_restore_failure"
  grep -Fxq 'restore_status=passed' "${RESTORE_STDOUT}" || fail "passed_status_missing"
  grep -Fxq "schema_version=${expected_schema}" "${RESTORE_STDOUT}" || fail "schema_version_mismatch"
  grep -Fxq 'restored_secret_check=passed' "${RESTORE_STDOUT}" || fail "restored_secret_check_missing"
  grep -Fxq 'gateway_status=passed' "${RESTORE_STDOUT}" || fail "gateway_check_missing"
}

assert_failed_restore() {
  local expected_stage=$1
  [[ ${RESTORE_STATUS} -ne 0 ]] || fail "unexpected_restore_success"
  grep -Fxq "restore_status=failed stage=${expected_stage}" "${RESTORE_STDERR}" || fail "failure_stage_mismatch"
}

development_stack_before="$(snapshot_development_stack | sort)"

active_scenario="fixture"
run_compose build n2api gateway-e2e >/dev/null
run_compose up --detach --wait postgres n2api >/dev/null
fixture_container="$(run_compose ps --quiet n2api)"
[[ -n "${fixture_container}" ]] || fail "fixture_container_missing"
fixture_image="$(docker container inspect --format '{{.Config.Image}}' "${fixture_container}")"
[[ -n "${fixture_image}" ]] || fail "fixture_image_missing"
docker image inspect "${fixture_image}" >/dev/null

run_compose run --rm --no-deps \
  --env N2API_E2E_CREATE_RESTORE_FIXTURE=1 \
  gateway-e2e '-test.run=^TestCreateRestoreBackupFixture$' >/dev/null

fixture_schema="$(run_compose exec --no-TTY postgres psql \
  --username n2api --dbname n2api_e2e --tuples-only --no-align \
  --command 'SELECT COALESCE(max(version_id), 0) FROM schema_migrations' | tr -d '[:space:]')"
[[ "${fixture_schema}" == "${current_schema_version}" ]] || fail "unexpected_current_schema"

run_compose exec --no-TTY postgres pg_dump \
  --username n2api --dbname n2api_e2e --format=custom --no-owner --no-privileges >"${current_dump}"
[[ -s "${current_dump}" ]] || fail "current_dump_missing"

run_compose stop n2api >/dev/null
run_compose exec --no-TTY postgres psql \
  --username n2api --dbname n2api_e2e --set ON_ERROR_STOP=1 \
  --command "DROP TABLE response_affinities; DELETE FROM schema_migrations WHERE version_id = ${current_schema_version};" >/dev/null
fixture_schema="$(run_compose exec --no-TTY postgres psql \
  --username n2api --dbname n2api_e2e --tuples-only --no-align \
  --command 'SELECT COALESCE(max(version_id), 0) FROM schema_migrations' | tr -d '[:space:]')"
[[ "${fixture_schema}" == "${historical_schema_version}" ]] || fail "historical_schema_not_created"

run_compose exec --no-TTY postgres pg_dump \
  --username n2api --dbname n2api_e2e --format=custom --no-owner --no-privileges >"${older_dump}"
[[ -s "${older_dump}" ]] || fail "older_dump_missing"
printf 'not a PostgreSQL custom archive\n' >"${corrupt_dump}"
assert_development_stack_unchanged

active_scenario="valid_current"
run_restore "${active_scenario}" "${current_dump}" "${fixture_encryption_secret}"
assert_passed_restore "${current_schema_version}"
echo "restore_scenario_status=passed scenario=${active_scenario}"

active_scenario="older_schema"
run_restore "${active_scenario}" "${older_dump}" "${fixture_encryption_secret}"
assert_passed_restore "${current_schema_version}"
echo "restore_scenario_status=passed scenario=${active_scenario} source_schema=${historical_schema_version}"

active_scenario="wrong_key"
run_restore "${active_scenario}" "${current_dump}" "${wrong_encryption_secret}"
assert_failed_restore "restored_secret"
echo "restore_scenario_status=passed scenario=${active_scenario}"

active_scenario="corrupt_archive"
run_restore "${active_scenario}" "${corrupt_dump}" "${fixture_encryption_secret}"
assert_failed_restore "archive_list"
echo "restore_scenario_status=passed scenario=${active_scenario}"

active_scenario="term_cleanup"
term_stdout="${scenario_root}/${active_scenario}.stdout"
term_stderr="${scenario_root}/${active_scenario}.stderr"
term_marker_root="${scenario_root}/${active_scenario}-markers"
mkdir -p "${term_marker_root}"
setsid env \
  N2API_DEV_CACHE_ROOT="${term_marker_root}" \
  N2API_RESOURCE_LOCK_FILE="${term_marker_root}/resource.lock" \
  N2API_RESTORE_IMAGE="${fixture_image}" \
  N2API_RESTORE_ADMIN_USERNAME="${fixture_admin_username}" \
  N2API_RESTORE_ADMIN_PASSWORD="${fixture_admin_password}" \
  N2API_RESTORE_ENCRYPTION_SECRET="${fixture_encryption_secret}" \
  N2API_RESTORE_ENCRYPTION_KEY_ID="default" \
  N2API_RESTORE_ENCRYPTION_PREVIOUS_KEYS="[]" \
  "${restore_script}" "${current_dump}" >"${term_stdout}" 2>"${term_stderr}" &
N2API_CHILD_PID=$!
term_run_id=""
for _ in {1..600}; do
  term_run_id="$(restore_run_id_from_output "${term_stdout}")"
  if [[ -n "${term_run_id}" ]] && [[ -n "$(docker container ls --all --quiet \
    --filter "label=io.knowsky.n2api.resource=test" \
    --filter "label=io.knowsky.n2api.run-id=${term_run_id}")" ]]; then
    break
  fi
  kill -0 "${N2API_CHILD_PID}" 2>/dev/null || break
  sleep 0.1
done
[[ -n "${term_run_id}" ]] || fail "term_restore_run_id_missing"
[[ -n "$(docker container ls --all --quiet \
  --filter "label=io.knowsky.n2api.resource=test" \
  --filter "label=io.knowsky.n2api.run-id=${term_run_id}")" ]] || fail "term_restore_container_missing"
kill -TERM -- "-${N2API_CHILD_PID}" 2>/dev/null || kill -TERM "${N2API_CHILD_PID}"
set +e
wait "${N2API_CHILD_PID}"
term_status=$?
set -e
unset N2API_CHILD_PID
[[ ${term_status} -eq 143 ]] || fail "term_exit_status_mismatch"
grep -Fq 'restore_status=failed stage=' "${term_stderr}" || fail "term_failure_status_missing"
assert_restore_run_clean "${term_run_id}"
assert_development_stack_unchanged
echo "restore_scenario_status=passed scenario=${active_scenario}"

active_scenario="outer_cleanup"
docker image inspect "${fixture_image}" >/dev/null || fail "fixture_image_removed_early"
run_compose down --volumes --remove-orphans --rmi local --timeout 10 >/dev/null
n2api_remove_run_docker_resources "${N2API_TEST_RUN_ID}" || fail "outer_cleanup_failed"
assert_restore_run_clean "${N2API_TEST_RUN_ID}"
assert_development_stack_unchanged
unset N2API_TEST_COMPOSE_FILE N2API_TEST_COMPOSE_PROJECT
echo "restore_scenario_status=passed current_schema=${current_schema_version} historical_schema=${historical_schema_version}"

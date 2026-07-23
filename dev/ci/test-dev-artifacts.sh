#!/usr/bin/env bash

set -Eeuo pipefail

repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd -P)"
common="${repo_root}/dev/lib/test-resources.sh"
cleaner="${repo_root}/dev/maintenance/clean-dev-artifacts.sh"
disk_check="${repo_root}/dev/maintenance/disk-check.sh"
restore_driver="${repo_root}/dev/verification/test-restore-backup.sh"
fixture="$(mktemp -d "${TMPDIR:-/tmp}/n2api-dev-artifact-test.XXXXXXXX")"

cleanup_fixture() {
  rm -rf -- "${fixture}"
}
trap cleanup_fixture EXIT

fail() {
  echo "FAIL: $*" >&2
  exit 1
}

wait_for_file() {
  local path=$1
  local attempt
  for attempt in {1..100}; do
    [[ -s "${path}" ]] && return 0
    sleep 0.02
  done
  fail "timed out waiting for ${path}"
}

run_lifecycle_child() {
  local mode=$1
  local output=$2
  local stop_file=${3:-}
  N2API_TMP_ROOT="${fixture}/tmp" \
  N2API_DEV_CACHE_ROOT="${fixture}/cache" \
    bash -c '
      set -Eeuo pipefail
      source "$1"
      n2api_run_init lifecycle
      printf "%s\n" "${N2API_RUN_DIR}" >"$2"
      case "$3" in
        success) exit 0 ;;
        failure) exit 7 ;;
        wait)
          while [[ ! -e "$4" ]]; do
            read -r -t 0.05 _ || true
          done
          ;;
        signal)
          n2api_run_command sleep 30
          ;;
      esac
    ' _ "${common}" "${output}" "${mode}" "${stop_file}"
}

mkdir -p "${fixture}/tmp" "${fixture}/cache"

normal_path_file="${fixture}/normal.path"
run_lifecycle_child success "${normal_path_file}"
normal_path="$(<"${normal_path_file}")"
[[ ! -e "${normal_path}" ]] || fail "normal exit left ${normal_path}"

failure_path_file="${fixture}/failure.path"
set +e
run_lifecycle_child failure "${failure_path_file}"
failure_status=$?
set -e
[[ ${failure_status} -eq 7 ]] || fail "failure exit status changed to ${failure_status}"
failure_path="$(<"${failure_path_file}")"
[[ ! -e "${failure_path}" ]] || fail "failure exit left ${failure_path}"

for runner_failure in test check; do
  runner_output="${fixture}/runner-${runner_failure}.out"
  case "${runner_failure}" in
    test) runner_expected_status=17 ;;
    check) runner_expected_status=23 ;;
  esac
  set +e
  N2API_TMP_ROOT="${fixture}/tmp" \
  N2API_DEV_CACHE_ROOT="${fixture}/cache" \
  N2API_DISK_MIN_FREE_GIB=0 \
  N2API_FAKE_BUN_FAILURE="${runner_failure}" \
    bash -c '
      go() { return 0; }
      bun() {
        printf "fake bun: %s\n" "$*"
        case "${N2API_FAKE_BUN_FAILURE}:$*" in
          test:test) return 17 ;;
          "check:run check") return 23 ;;
        esac
        return 0
      }
      export -f go bun
      "$1" unit
    ' _ "${repo_root}/dev/testing/run.sh" >"${runner_output}" 2>&1
  runner_status=$?
  set -e
  [[ ${runner_status} -eq ${runner_expected_status} ]] ||
    fail "frontend ${runner_failure} failure became status ${runner_status}, want ${runner_expected_status}"
  if [[ "${runner_failure}" == "test" ]]; then
    grep -Eq '^fake bun: run check$' "${runner_output}" || fail "frontend sync/check did not run before tests"
    grep -Eq '^fake bun: test$' "${runner_output}" || fail "frontend test command did not run"
    check_line="$(grep -n -m1 '^fake bun: run check$' "${runner_output}" | cut -d: -f1)"
    test_line="$(grep -n -m1 '^fake bun: test$' "${runner_output}" | cut -d: -f1)"
    [[ ${check_line} -lt ${test_line} ]] || fail "frontend test ran before sync/check"
  elif grep -Eq '^fake bun: test$' "${runner_output}"; then
    fail "frontend test ran after sync/check failed"
  fi
  if grep -Eq '^fake bun: run build$' "${runner_output}"; then
    fail "frontend build ran after ${runner_failure} failed"
  fi
done

signal_path_file="${fixture}/signal.path"
run_lifecycle_child signal "${signal_path_file}" &
signal_pid=$!
wait_for_file "${signal_path_file}"
signal_path="$(<"${signal_path_file}")"
signal_run_id="$(<"${signal_path}/.n2api-test-run")"
signal_inner_pid="$(<"${fixture}/cache/active/${signal_run_id}")"
signal_inner_pid="${signal_inner_pid#pid:}"
kill -TERM "${signal_inner_pid}"
set +e
wait "${signal_pid}"
signal_status=$?
set -e
[[ ${signal_status} -eq 143 ]] || fail "TERM exit status was ${signal_status}, want 143"
[[ ! -e "${signal_path}" ]] || fail "TERM left ${signal_path}"

first_path_file="${fixture}/first.path"
second_path_file="${fixture}/second.path"
first_stop="${fixture}/first.stop"
second_stop="${fixture}/second.stop"
run_lifecycle_child wait "${first_path_file}" "${first_stop}" &
first_pid=$!
run_lifecycle_child wait "${second_path_file}" "${second_stop}" &
second_pid=$!
wait_for_file "${first_path_file}"
wait_for_file "${second_path_file}"
first_path="$(<"${first_path_file}")"
second_path="$(<"${second_path_file}")"
[[ "${first_path}" != "${second_path}" ]] || fail "concurrent runs shared a directory"

N2API_TMP_ROOT="${fixture}/tmp" \
N2API_DEV_CACHE_ROOT="${fixture}/cache" \
N2API_TMP_TTL_HOURS=0 \
  "${cleaner}"
[[ -d "${first_path}" ]] || fail "cleaner removed the first active run"
[[ -d "${second_path}" ]] || fail "cleaner removed the second active run"
set +e
N2API_TMP_ROOT="${fixture}/tmp" \
N2API_DEV_CACHE_ROOT="${fixture}/cache" \
  "${cleaner}" --deep >/dev/null 2>&1
deep_active_status=$?
set -e
[[ ${deep_active_status} -ne 0 ]] || fail "deep cleanup ran while managed tests were active"

touch "${first_stop}"
wait "${first_pid}"
[[ ! -e "${first_path}" ]] || fail "first run did not clean itself"
[[ -d "${second_path}" ]] || fail "first run removed the second run"
touch "${second_stop}"
wait "${second_pid}"
[[ ! -e "${second_path}" ]] || fail "second run did not clean itself"

set +e
N2API_TEST_RUN_ID=deploy \
N2API_DEV_CACHE_ROOT="${fixture}/cache" \
  bash -c 'source "$1"; n2api_register_compose "$2" deploy' \
  _ "${common}" "${repo_root}/deploy/compose.e2e.yaml" >/dev/null 2>&1
unsafe_project_status=$?
set -e
[[ ${unsafe_project_status} -ne 0 ]] || fail "production Compose project was accepted"
N2API_TEST_RUN_ID=safe-run \
N2API_DEV_CACHE_ROOT="${fixture}/cache" \
  bash -c 'source "$1"; n2api_register_compose "$2" n2api-safe-run' \
  _ "${common}" "${repo_root}/deploy/compose.e2e.yaml"

artifact_run_id_file="${fixture}/artifact-run-id"
set +e
N2API_KEEP_FAILED_ARTIFACTS=1 \
N2API_TMP_ROOT="${fixture}/tmp" \
N2API_DEV_CACHE_ROOT="${fixture}/cache" \
  bash -c '
    set -Eeuo pipefail
    source "$1"
    n2api_run_init artifact
    printf "%s\n" "${N2API_RUN_ID}" >"$2"
    printf "trace\n" >"${N2API_RUN_DIR}/artifacts/playwright/trace.txt"
    exit 9
  ' _ "${common}" "${artifact_run_id_file}"
artifact_status=$?
set -e
[[ ${artifact_status} -eq 9 ]] || fail "failure artifact exit status changed"
artifact_run_id="$(<"${artifact_run_id_file}")"
[[ -f "${fixture}/cache/artifacts/${artifact_run_id}/playwright/trace.txt" ]] ||
  fail "failure evidence was not retained"

mkdir -p "${fixture}/cache-limit/go-mod"
dd if=/dev/zero of="${fixture}/cache-limit/go-mod/blob" bs=1024 count=1024 status=none
N2API_DEV_CACHE_ROOT="${fixture}/cache-limit" \
N2API_GO_MOD_CACHE_MAX_MIB=0 \
  bash -c 'source "$1"; n2api_enforce_cache_limits' _ "${common}"
[[ ! -e "${fixture}/cache-limit/go-mod" ]] || fail "over-limit Go module cache was retained"

artifact_cache="${fixture}/cache root"
artifact_root="${artifact_cache}/artifacts"
mkdir -p \
  "${artifact_root}/old artifact" \
  "${artifact_root}/new artifact" \
  "${artifact_root}/newline"$'\n'"artifact"
touch -d '3 hours ago' "${artifact_root}/old artifact"
touch -d '2 hours ago' "${artifact_root}/newline"$'\n'"artifact"
touch -d '1 hour ago' "${artifact_root}/new artifact"
touch "${fixture}/artifact-sentinel"
N2API_DEV_CACHE_ROOT="${artifact_cache}" \
N2API_TMP_ROOT="${fixture}/tmp" \
N2API_ARTIFACT_KEEP_COUNT=1 \
  "${cleaner}" >/dev/null
artifact_count="$(find "${artifact_root}" -mindepth 1 -maxdepth 1 -type d -printf . | wc -c | tr -d ' ')"
[[ ${artifact_count} -eq 1 ]] || fail "artifact retention kept ${artifact_count} directories"
[[ -f "${fixture}/artifact-sentinel" ]] || fail "artifact cleanup escaped its root"

fake_bin="${fixture}/bin"
docker_log="${fixture}/docker.log"
mkdir -p "${fake_bin}"
cat >"${fake_bin}/docker" <<'DOCKER'
#!/usr/bin/env bash
set -euo pipefail
printf '%s\n' "$*" >>"${N2API_TEST_DOCKER_LOG}"
if [[ "${2:-}" == "rm" && "${N2API_TEST_DOCKER_FAIL_RM:-0}" == "1" ]]; then
  exit 1
fi
case "$1 ${2:-}" in
  "container ls") printf '%s\n' test-container prod-container ;;
  "network ls") printf '%s\n' test-network prod-network ;;
  "volume ls") printf '%s\n' test-volume deploy_n2api-postgres ;;
  "image ls") printf '%s\n' test-image prod-image ;;
  "container inspect"|"image inspect"|"network inspect"|"volume inspect")
    id="${@: -1}"
    if [[ "$*" == *"io.knowsky.n2api.run-id"* ]]; then
      echo stale-test-run
    elif [[ "$*" == *"com.docker.compose.project"* ]]; then
      case "${id}" in prod-*|deploy_n2api-postgres) echo deploy ;; *) echo n2api-test ;; esac
    elif [[ "$*" == *"{{ .Name }}"* ]]; then
      case "${id}" in prod-*) echo /deploy-n2api-1 ;; *) echo /n2api-test ;; esac
    fi
    ;;
esac
DOCKER
chmod +x "${fake_bin}/docker"

PATH="${fake_bin}:${PATH}" \
N2API_TEST_DOCKER_LOG="${docker_log}" \
N2API_TMP_ROOT="${fixture}/tmp" \
N2API_DEV_CACHE_ROOT="${fixture}/cache" \
  "${cleaner}"
PATH="${fake_bin}:${PATH}" \
N2API_TEST_DOCKER_LOG="${docker_log}" \
N2API_TMP_ROOT="${fixture}/tmp" \
N2API_DEV_CACHE_ROOT="${fixture}/cache" \
  "${cleaner}" >/dev/null

for resource in test-container test-network test-volume test-image; do
  grep -Eq "rm .*${resource}|rm ${resource}" "${docker_log}" ||
    fail "test resource ${resource} was not removed"
done
if grep -Eq '^(container|network|volume|image) rm.*(prod-|deploy_n2api-postgres)|system prune|volume prune|image prune' "${docker_log}"; then
  fail "cleanup crossed the production/global boundary"
fi
while IFS= read -r line; do
  case "${line}" in
    *" ls "*)
      [[ "${line}" == *"--filter label=io.knowsky.n2api.resource=test"* ]] ||
        fail "Docker discovery was not label-scoped: ${line}"
      ;;
  esac
done <"${docker_log}"

set +e
PATH="${fake_bin}:${PATH}" \
N2API_TEST_DOCKER_LOG="${docker_log}" \
N2API_TEST_DOCKER_FAIL_RM=1 \
N2API_TMP_ROOT="${fixture}/tmp" \
N2API_DEV_CACHE_ROOT="${fixture}/cache" \
  "${cleaner}" >/dev/null 2>&1
failed_cleanup_status=$?
set -e
[[ ${failed_cleanup_status} -ne 0 ]] || fail "failed Docker removal was reported as success"

cat >"${fake_bin}/df" <<'DF'
#!/usr/bin/env bash
cat <<EOF
Filesystem 1024-blocks Used Available Capacity Mounted on
/dev/fake 47185920 41943040 5242880 89% /
EOF
DF
cat >"${fake_bin}/du" <<'DU'
#!/usr/bin/env bash
echo "0 fake"
DU
chmod +x "${fake_bin}/df" "${fake_bin}/du"

disk_output="${fixture}/disk.out"
set +e
PATH="${fake_bin}:${PATH}" \
N2API_TEST_DOCKER_LOG="${docker_log}" \
N2API_DEV_CACHE_ROOT="${fixture}/cache" \
N2API_TMP_ROOT="${fixture}/tmp" \
  "${disk_check}" --heavy >"${disk_output}" 2>&1
disk_status=$?
set -e
[[ ${disk_status} -ne 0 ]] || fail "low disk did not block a heavy test"
grep -Fq 'make clean-dev-artifacts' "${disk_output}" || fail "disk failure lacked cleanup guidance"
grep -Fq 'Production persistent data' "${disk_output}" || fail "disk failure lacked production summary"
grep -Fq 'Reclaimable N2API test resources' "${disk_output}" || fail "disk failure lacked reclaimable summary"
grep -Fq 'Unknown or shared resources' "${disk_output}" || fail "disk failure lacked unknown summary"

N2API_DISK_MIN_FREE_GIB=4 \
PATH="${fake_bin}:${PATH}" \
N2API_TEST_DOCKER_LOG="${docker_log}" \
N2API_DEV_CACHE_ROOT="${fixture}/cache" \
N2API_TMP_ROOT="${fixture}/tmp" \
  "${disk_check}" --heavy >/dev/null

N2API_TEST_RUN_ID=contract-test docker compose \
  -f "${repo_root}/deploy/compose.e2e.yaml" config >/dev/null
grep -Fq 'io.knowsky.n2api.resource: test' "${repo_root}/deploy/compose.e2e.yaml" ||
  fail "E2E Compose lacks test resource labels"
grep -Fq 'N2API_REQUEST_LOG_QUERY_PROFILE=1' "${repo_root}/dev/testing/run.sh" ||
  fail "request log profile runner does not enable the opt-in profile"
grep -Fq "run_compose ps -q postgres" "${repo_root}/dev/testing/run.sh" ||
  fail "request log profile runner does not select its isolated PostgreSQL container"
grep -Fq '.NetworkSettings.Networks' "${repo_root}/dev/testing/run.sh" ||
  fail "request log profile runner does not use its isolated Docker network"
grep -Fq -- '--rmi local' "${repo_root}/dev/verification/restore-backup.sh" ||
  fail "restore cleanup does not remove local test images"
grep -Fq 'disk-check.sh" --heavy' "${repo_root}/dev/verification/restore-backup.sh" ||
  fail "restore verification lacks the disk preflight"
bash -n "${restore_driver}" || fail "restore scenario driver has invalid shell syntax"
grep -Fq 'n2api_run_init restore-backup' "${restore_driver}" ||
  fail "restore scenario driver does not use managed run lifecycle"
grep -Fq 'n2api_register_compose' "${restore_driver}" ||
  fail "restore scenario driver does not register its isolated Compose project"
grep -Fq 'label=io.knowsky.n2api.run-id=${run_id}' "${restore_driver}" ||
  fail "restore scenario cleanup assertion is not run-label scoped"
grep -Fq 'snapshot_development_stack' "${restore_driver}" ||
  fail "restore scenario driver does not protect the development stack"
grep -Fq 'test-restore-backup:' "${repo_root}/Makefile" ||
  fail "managed restore scenario Make target is missing"
image_build_block="$(sed -n '/name: Build platform image once/,/name: Start PostgreSQL for smoke test/p' "${repo_root}/.github/workflows/ci-image.yml")"
if grep -Fq 'io.knowsky.n2api.resource=test' <<<"${image_build_block}"; then
  fail "publishable image config contains a test resource label"
fi
grep -Fq 'make clean-dev-artifacts' "${repo_root}/docs/development.md" ||
  fail "development cleanup command is undocumented"

echo "Development artifact lifecycle tests passed."

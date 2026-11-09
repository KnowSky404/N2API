#!/usr/bin/env bash

set -Eeuo pipefail

repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd -P)"
runner="${repo_root}/dev/development/run.sh"
fixture="$(mktemp -d "${TMPDIR:-/tmp}/n2api-dev-session-test.XXXXXXXX")"
cleanup() {
  local status=$?
  if (( status != 0 )); then
    cat "${fixture}/run.log" >&2 || true
  fi
  rm -rf -- "${fixture}"
}
trap cleanup EXIT
mkdir -p "${fixture}/bin" "${fixture}/primary" "${fixture}/tmp"

fail() { echo "FAIL: $*" >&2; exit 1; }

cat >"${fixture}/bin/git" <<'GIT'
#!/usr/bin/env bash
printf 'worktree %s\n' "${N2API_SESSION_FIXTURE}/primary"
printf 'worktree %s\n' "${N2API_SESSION_FIXTURE}/other-task"
GIT
cat >"${fixture}/bin/docker" <<'DOCKER'
#!/usr/bin/env bash
set -euo pipefail
printf '%s\n' "$*" >>"${N2API_SESSION_FIXTURE}/docker.log"
case "$*" in
  compose*" ps --all --quiet") echo existing-dev ;;
  inspect*)
    if [[ "${N2API_FAKE_FOREIGN:-0}" == 1 ]]; then
      echo "${N2API_SESSION_FIXTURE}/primary/deploy/compose.release.yaml"
    elif [[ "${N2API_FAKE_PREVIOUS_WORKTREE:-0}" == 1 ]]; then
      echo "${N2API_SESSION_FIXTURE}/other-task/deploy/compose.yaml"
    else
      echo "${N2API_SESSION_FIXTURE}/primary/deploy/compose.yaml"
    fi
    ;;
  compose*" ps --quiet n2api") echo dev-app ;;
  compose*" build --no-cache") exit "${N2API_FAKE_BUILD_STATUS:-0}" ;;
  compose*" up -d "*) exit "${N2API_FAKE_START_STATUS:-0}" ;;
  compose*" down "*) exit "${N2API_FAKE_CLEANUP_STATUS:-0}" ;;
  compose*" up --no-build "*)
    if [[ "${N2API_FAKE_WAIT:-0}" == 1 ]]; then
      touch "${N2API_SESSION_FIXTURE}/ready"
      trap 'exit 0' INT TERM HUP
      while :; do sleep 0.1; done
    fi
    ;;
esac
DOCKER
chmod +x "${fixture}/bin/"*

export PATH="${fixture}/bin:${PATH}"
export N2API_SESSION_FIXTURE="${fixture}"
export N2API_TMP_ROOT="${fixture}/tmp"
export N2API_DEV_CACHE_ROOT="${fixture}/cache"
export N2API_DISK_MIN_FREE_GIB=0
export N2API_TEST_COMPOSE_PROJECT=parent-test
export N2API_TEST_COMPOSE_FILE=/parent/compose.e2e.yaml

assert_cleaned() {
  grep -Fq 'down --remove-orphans --rmi local' "${fixture}/docker.log" || fail "development stack was not removed"
  if grep -Eq 'down .*--volumes|down .* -v|system prune|image prune|volume (rm|prune)' "${fixture}/docker.log"; then
    fail "cleanup crossed the persistent-data boundary"
  fi
  [[ -z "$(find "${fixture}/tmp" -mindepth 1 -print -quit)" ]] || fail "run directory survived cleanup"
}

for mode in check session stop; do
  : >"${fixture}/docker.log"
  "${runner}" "${mode}" >"${fixture}/run.log" 2>&1
  assert_cleaned
  if [[ "${mode}" != stop ]]; then
    [[ "$(grep -c '^builder prune --all --force$' "${fixture}/docker.log")" == 2 ]] || fail "build cache was not cleaned before and after use"
    grep -Fq 'http://127.0.0.1:3000/readyz' "${fixture}/docker.log" || fail "readiness was not checked"
    grep -Fq 'http://127.0.0.1:3000/' "${fixture}/docker.log" || fail "bootstrap page was not checked"
  fi
done

: >"${fixture}/docker.log"
N2API_FAKE_PREVIOUS_WORKTREE=1 "${runner}" stop >"${fixture}/run.log" 2>&1
assert_cleaned

for failure in N2API_FAKE_START_STATUS=7 N2API_FAKE_CLEANUP_STATUS=1; do
  : >"${fixture}/docker.log"
  if env "${failure}" "${runner}" check >"${fixture}/run.log" 2>&1; then
    fail "${failure} was reported as success"
  fi
  assert_cleaned
done

for failure in N2API_FAKE_FOREIGN=1 N2API_FAKE_BUILD_STATUS=7; do
  : >"${fixture}/docker.log"
  if env "${failure}" "${runner}" check >"${fixture}/run.log" 2>&1; then
    fail "${failure} was reported as success"
  fi
  if grep -Eq 'compose.* (up|down) ' "${fixture}/docker.log"; then
    fail "${failure} changed the pre-existing stack"
  fi
done

for signal in INT TERM HUP; do
  : >"${fixture}/docker.log"
  rm -f "${fixture}/ready"
  env --default-signal=INT N2API_FAKE_WAIT=1 "${runner}" session >"${fixture}/run.log" 2>&1 &
  session_pid=$!
  for _ in {1..200}; do
    [[ -f "${fixture}/ready" ]] && break
    sleep 0.02
  done
  [[ -f "${fixture}/ready" ]] || fail "foreground session never started"
  if "${runner}" stop >"${fixture}/concurrent.log" 2>&1; then
    fail "a concurrent stop acquired the active session"
  fi
  grep -Fq 'reason=active_development_session' "${fixture}/concurrent.log" || fail "concurrent stop failed for an unexpected reason"
  kill -s "${signal}" "${session_pid}"
  status=0
  wait "${session_pid}" || status=$?
  case "${signal}" in INT) expected=130 ;; TERM) expected=143 ;; HUP) expected=129 ;; esac
  [[ "${status}" == "${expected}" ]] || fail "${signal} returned ${status}, expected ${expected}"
  assert_cleaned
done

echo "Development session lifecycle tests passed."

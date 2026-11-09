# Development Resource Lifecycle

N2API's local verification commands isolate temporary files and disposable
Docker resources so repeated development runs cannot silently consume the host
disk. Use the root `Makefile` instead of creating ad hoc caches under `/tmp`.

## Development Sessions

Start the local Compose stack explicitly from the current checkout or task
worktree:

```bash
make dev
```

The command rebuilds without cache, waits for healthy services, probes the app,
and stays in the foreground. Keep it open while developing. Ctrl-C, TERM, HUP
(terminal disconnect), a service exit, or a failed start triggers teardown.
The runner removes development containers, the network, the locally built app
image, its temporary directory, and unused builder cache. All services use
`restart: "no"`, so Docker/host restarts do not start a development stack.
Do not add host boot jobs or scheduled startup for local testing. The Docker
daemon is shared infrastructure and stays available to other projects.

For verification that exits after the health and bootstrap-page checks:

```bash
make dev-check
```

Do not leave detached services running after development verification unless
the user explicitly requests a continuing preview. On this VPS the app is at
`http://oc-de-fra-1.knowsky.uk:3000` while the foreground session is open;
Compose publishes the configured port on IPv4 and IPv6. Other hosts can use
`http://localhost:3000`.

For host-side metrics, configure the dedicated bearer token described in the
operator manual and use `N2API_DEV_METRICS=1 make dev` or
`N2API_DEV_METRICS=1 make dev-check`. This adds the existing metrics Compose
override. `N2API_METRICS_ENABLED` alone continues to configure the internal
listener without publishing another host port.

The runner preserves `deploy_n2api-postgres`, the primary checkout's `.env`,
and its `backups/` directory across task worktrees. Closing a session is not a
database reset. Persistent data requires a separate explicit reset request;
never use `down --volumes` for the development project. Disposable test volumes
remain separate and are removed by the creating test runner.

A zero-byte ownership lock in the primary checkout's
`.cache/.n2api-dev-session.lock` is shared across worktrees. It stays local for
kernel locking, independent of temporary/cache overrides. A second session or
`make dev-stop` refuses to interrupt an active foreground owner. The runner
keeps the lock out of Compose child processes so a killed owner permits recovery.
It accepts the base development file or its known metrics override combination
and refuses a `deploy` project belonging to release or unrelated Compose
files before changing it.

SIGKILL, a host crash, or an external detached `compose up -d` cannot run an
exit trap. Close those leftovers explicitly once no session is active:

```bash
make dev-stop
make clean-dev-artifacts
```

After cleanup, require no development containers or network and retain the
database volume:

```bash
docker ps -a --filter label=com.docker.compose.project=deploy
docker network ls --filter label=com.docker.compose.project=deploy
docker volume inspect deploy_n2api-postgres --format '{{.Name}}'
```

On this development VPS, verify the mounted remote filesystem and available
space first, then configure artifacts before invoking managed commands:

```bash
artifact_base=/mnt/oc-de-fra-2/data/oc-de-fra-1/codex
findmnt -T "$artifact_base"
df -h "$artifact_base"
mkdir -p "$artifact_base/tmp/n2api" "$artifact_base/cache/n2api"
task_tmp=$(mktemp -d "$artifact_base/tmp/n2api/session.XXXXXXXX")
export TMPDIR="$task_tmp" TMP="$task_tmp" TEMP="$task_tmp"
export N2API_TMP_ROOT="$task_tmp"
export N2API_DEV_CACHE_ROOT="$artifact_base/cache/n2api"
make dev-check
rmdir "$task_tmp"
```

Keep downloads, build output, reports, screenshots, and traces in those managed
locations. Do not silently fall back if the required remote mount is absent.
Docker's existing storage and installed tools stay at their established paths.
Tests that require exact local ownership and permission bits (operator state
and backup fixtures) cannot use the root-squashed NFS mount. For those small
fixtures only, use a private `mktemp -d /tmp/n2api-*-posix.XXXXXXXX`, remove it
after the test, and keep logs and other artifacts remote.

## Managed Resources

Each test run creates a unique directory with `mktemp -d` under
`${N2API_TMP_ROOT:-${TMPDIR:-/tmp}}`. An `EXIT`/`INT`/`TERM`/`HUP` trap removes only
that directory. Concurrent runs have distinct run IDs and active markers and
hold a shared lifecycle lock. Maintenance requires the exclusive lock, so one
run or maintenance command cannot remove another active run.

The lifecycle exports these locations:

- `GOCACHE`, `GOTMPDIR`, `GOPATH`, `TMPDIR`, `TMP`, `TEMP`, and `XDG_CACHE_HOME` are unique to
  the current run and are deleted at exit.
- `GOMODCACHE` uses `${N2API_DEV_CACHE_ROOT}/go-mod` and is deleted after a run if it exceeds
  `N2API_GO_MOD_CACHE_MAX_MIB` (default `2048`) and no other managed run is
  active.
- `BUN_INSTALL_CACHE_DIR` uses `${N2API_DEV_CACHE_ROOT}/bun`, capped by
  `N2API_BUN_CACHE_MAX_MIB` (default `1024`).
- `PLAYWRIGHT_BROWSERS_PATH` uses `${N2API_DEV_CACHE_ROOT}/playwright-browsers`, capped by
  `N2API_PLAYWRIGHT_CACHE_MAX_MIB` (default `2048`). Playwright output, traces,
  screenshots, and reports stay inside the current run directory.
- SvelteKit's bounded `.svelte-kit` workspace remains under `frontend/` so
  generated server modules can resolve frontend dependencies. The
  adapter-static `build` output is redirected into the current run directory.
  Deep cleanup removes the repository-local `.svelte-kit` workspace. After
  verification, remove current-task generated outputs once no other task uses
  them; preserve installed dependencies and active shared tools.

`N2API_DEV_CACHE_ROOT` defaults to `.cache/dev` on other hosts; set it to the
verified remote cache path on this VPS. Bounded download caches are reused;
per-run intermediate files and build outputs are removed on every exit.

Failed Playwright evidence is deleted by default. Set
`N2API_KEEP_FAILED_ARTIFACTS=1` to retain it under
`${N2API_DEV_CACHE_ROOT}/artifacts/<run-id>`. Regular cleanup keeps at most five sets for
seven days by default; override `N2API_ARTIFACT_KEEP_COUNT` or
`N2API_ARTIFACT_TTL_DAYS` when needed.

Docker E2E and restore resources use a unique Compose project plus both
`io.knowsky.n2api.resource=test` and `io.knowsky.n2api.run-id=<run-id>` labels.
The creating process runs `compose down --volumes --remove-orphans --rmi local`
from its trap. This removes that run's containers, network, test database
volume, and locally built test images without removing pulled or production
images.

## Commands

Run the normal unit checks:

```bash
make test
```

PostgreSQL store tests that create schemas or truncate fixtures require both
`N2API_STORE_TEST_DATABASE_URL` and
`N2API_STORE_TEST_ALLOW_DESTRUCTIVE=1`. The connected database name must contain
a separate `test`, `e2e`, or `restore` segment. The suite checks
`current_database()` before migrations or cleanup and refuses the development
database `n2api` even when a database URL is supplied accidentally. Create a
dedicated disposable database such as `n2api_store_test`; never point store
tests at the `deploy` Compose database.

Run isolated Docker verification:

```bash
make test-go-quality
make test-critical-race
make test-control-connections
make test-e2e
make test-contracts
make test-request-log-profile
make test-management-list-profile
make test-production-deploy
```

`test-go-quality` installs Staticcheck 2026.1 from the fixed Go module version
`v0.7.0` into the run directory, then runs Staticcheck and `go vet` across the
backend. `test-critical-race` and `test-control-connections` start isolated
PostgreSQL projects and cover critical packages, real process lifecycle, locks,
LISTEN reconnect, authentication, and budget concurrency. The Request Log and
management-list profiles use separate labelled PostgreSQL projects without
publishing the database port; the former loads one million synthetic rows.
Managed cleanup removes every test database volume.

## Local Database Backups

The foreground development session starts `postgres-backup` after PostgreSQL is
healthy. It creates a custom-format dump immediately and every six hours by
default, validates each archive with `pg_restore --list`, then atomically moves
it into the ignored `backups/` directory. The directory is `0750` and archives
are `0640`, owned by root and the configured host operator group. Set
`N2API_BACKUP_GID` in `.env` to the numeric result of `id -g` for the operator
who copies or restores backups; it defaults to `1000`. Existing matching dumps
are normalized when the sidecar starts. Files older than seven days are
removed by the backup sidecar's retention policy. The sidecar stops with the
development session and does not run independently between sessions. Override
the bounded interval and retention with
`N2API_BACKUP_INTERVAL_SECONDS` and `N2API_BACKUP_RETENTION_DAYS` in `.env`.

Create and validate an additional backup immediately:

```bash
make backup-dev
```

Inspect the sidecar with `docker compose -f deploy/compose.yaml logs
postgres-backup`. A healthy sidecar proves that its latest local archive passed
the archive-list check, is recent, and still exists as a non-empty readable
file; it does not prove a full restore. Continue running the
isolated restore drill documented in the operator manual, and copy important
backups to encrypted off-host storage. Local dumps are lost with the VPS disk.

Install the pinned Playwright Chromium build into the controlled browser cache,
then run Playwright commands through the wrapper:

```bash
make playwright-install
make test-playwright PLAYWRIGHT_ARGS='test /tmp/n2api-browser-tests/example.spec.ts'
```

The project does not add Playwright as a frontend dependency. Temporary browser
tests and configuration should remain outside the repository.

## Disk Protection

`make disk-check` is read-only. It warns when the checked filesystem is at or
above `N2API_DISK_WARN_PERCENT` (default `80`). Managed verification commands
run the strict check automatically and refuse to start when free space is below
`N2API_DISK_MIN_FREE_GIB` (default `10`). The report separates the protected
`deploy` Compose volume, labelled test resources, and unknown/shared Docker
usage.

Run safe, idempotent maintenance with:

```bash
make clean-dev-artifacts
```

This removes expired marked run directories, expired retained evidence,
over-limit controlled caches, and inactive Docker resources carrying the exact
N2API test label. It never selects the production `deploy` Compose project,
`deploy_n2api-postgres`, unlabelled resources, global Bun cache, or global
Playwright browser cache.

Deep cleanup is explicit:

```bash
make clean-dev-artifacts-deep
```

It additionally removes legacy N2API Go caches at exact known paths and the
repository's controlled caches. It still does not remove production resources,
global user caches, `tmp/imagegen`, backups, bind mounts, or unknown data.

Do not use `docker system prune`, `docker image prune`, or
`docker volume prune` as routine N2API cleanup. They operate beyond this
project's ownership boundary. Builder-wide cleanup used by the documented
no-cache development stack refresh is a separate explicit operation and must
not run concurrently with another build.

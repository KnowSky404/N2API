# Development Resource Lifecycle

N2API's local verification commands isolate temporary files and disposable
Docker resources so repeated development runs cannot silently consume the host
disk. Use the root `Makefile` instead of creating ad hoc caches under `/tmp`.

## Managed Resources

Each test run creates a unique directory with `mktemp -d` under
`${N2API_TMP_ROOT:-${TMPDIR:-/tmp}}`. An `EXIT`/`INT`/`TERM` trap removes only
that directory. Concurrent runs have distinct run IDs and active markers and
hold a shared lifecycle lock. Maintenance requires the exclusive lock, so one
run or maintenance command cannot remove another active run.

The lifecycle exports these locations:

- `GOCACHE`, `GOTMPDIR`, `GOPATH`, `TMPDIR`, and `XDG_CACHE_HOME` are unique to
  the current run and are deleted at exit.
- `GOMODCACHE` uses `.cache/dev/go-mod` and is deleted after a run if it exceeds
  `N2API_GO_MOD_CACHE_MAX_MIB` (default `2048`) and no other managed run is
  active.
- `BUN_INSTALL_CACHE_DIR` uses `.cache/dev/bun`, capped by
  `N2API_BUN_CACHE_MAX_MIB` (default `1024`).
- `PLAYWRIGHT_BROWSERS_PATH` uses `.cache/dev/playwright-browsers`, capped by
  `N2API_PLAYWRIGHT_CACHE_MAX_MIB` (default `2048`). Playwright output, traces,
  screenshots, and reports stay inside the current run directory.
- SvelteKit's bounded `.svelte-kit` workspace remains under `frontend/` so
  generated server modules can resolve frontend dependencies. The
  adapter-static `build` output is redirected into the current run directory.
  Deep cleanup removes the repository-local `.svelte-kit` workspace.

Failed Playwright evidence is deleted by default. Set
`N2API_KEEP_FAILED_ARTIFACTS=1` to retain it under
`.cache/dev/artifacts/<run-id>`. Regular cleanup keeps at most five sets for
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
make test-e2e
make test-contracts
```

## Local Database Backups

The development Compose stack starts `postgres-backup` after PostgreSQL is
healthy. It creates a custom-format dump immediately and every six hours by
default, validates each archive with `pg_restore --list`, then atomically moves
it into the ignored `backups/` directory. The directory is `0750` and archives
are `0640`, owned by root and the configured host operator group. Set
`N2API_BACKUP_GID` in `.env` to the numeric result of `id -g` for the operator
who copies or restores backups; it defaults to `1000`. Existing matching dumps
are normalized when the sidecar starts. Files older than seven days are
removed. Override the bounded interval and retention with
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

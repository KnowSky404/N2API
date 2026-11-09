---
name: n2api-local-refresh
description: Rebuild, recreate, verify, and tear down the local N2API Docker Compose development stack after code changes, preserving persistent data. Use for local development refresh and session cleanup.
---

# N2API Local Refresh

Run this workflow from the task worktree after code or functionality changes.
Apply the repository's current session rules instead of leaving a detached
development stack running. No production operation is authorized here.

## Refresh

1. Confirm the intended changes and relevant tests are complete. Check that no
   other build is using the shared builder before builder-wide cache cleanup.
2. Configure temporary files and controlled caches on the required mounted
   remote filesystem through `TMPDIR`, `TMP`, `TEMP`, `N2API_TMP_ROOT`, and
   `N2API_DEV_CACHE_ROOT`. Verify the mount and available space before writing.
3. Run the managed one-shot refresh:

   ```bash
   make dev-check
   ```

The runner prunes unused builder cache, builds with `--no-cache`, recreates and
waits for the development services to become healthy, and probes `/livez`,
`/readyz`, `/version`, and `/` inside the app container. On exit it removes the
development containers, network, locally built app image, and run directory,
then prunes unused builder cache. Named PostgreSQL data and backups remain.

## Verify

After the command, require no remaining development containers or network and
confirm the existing volume still exists:

```bash
docker ps -a --filter label=com.docker.compose.project=deploy
docker network ls --filter label=com.docker.compose.project=deploy
docker volume inspect deploy_n2api-postgres --format '{{.Name}}'
```

Report the exact commands, probe results, teardown, and preserved data. A failed
probe or incomplete cleanup is a failure; inspect the error and retain useful
diagnostic evidence before resolving it. Do not leave failed containers running
by default.

## Interactive Development

Use `make dev` to keep a foreground session open. Ctrl-C, TERM, or HUP tears it
down. Keep a preview running beyond verification only when the user explicitly
requests it, and report how to stop it. Use `make dev-stop` after a hard kill,
host failure, or a legacy detached startup; never stop another active session.

## Boundaries

- Do not use global system/image/volume prune or development `down --volumes`.
- Do not remove PostgreSQL data, credentials, backups, or unrelated resources.
- Do not substitute production Compose or change the host Docker daemon.
- Disposable labelled test volumes remain managed by their isolated test runner.

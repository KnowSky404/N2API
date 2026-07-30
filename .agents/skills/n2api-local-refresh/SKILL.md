---
name: n2api-local-refresh
description: Rebuild, recreate, and verify the local N2API Docker Compose development stack after repository code or functionality changes. Use when Codex finishes an N2API implementation, needs the running local stack to reflect source changes, or the user asks to refresh, rebuild, or recreate the local N2API deployment.
---

# N2API Local Refresh

Run this workflow from the repository root after relevant code or functionality changes.

## Refresh

1. Confirm the intended repository changes and relevant tests are complete.
2. Remove unused builder cache from earlier builds:

   ```bash
   docker builder prune --all --force
   ```

3. Rebuild without cache:

   ```bash
   docker compose -f deploy/compose.yaml build --no-cache
   ```

4. Recreate the development stack without deleting data:

   ```bash
   docker compose -f deploy/compose.yaml up -d --force-recreate
   ```

## Verify

1. Require `n2api` and `postgres` to be running and healthy:

   ```bash
   docker compose -f deploy/compose.yaml ps
   ```

2. Resolve the current application container and run probes inside it:

   ```bash
   container_id="$(docker compose -f deploy/compose.yaml ps --quiet n2api)"
   docker exec "${container_id}" wget -qO- http://127.0.0.1:3000/livez
   docker exec "${container_id}" wget -qO- http://127.0.0.1:3000/readyz
   docker exec "${container_id}" wget -qO- http://127.0.0.1:3000/version
   ```

3. Run a container-local bootstrap page smoke check:

   ```bash
   docker exec "${container_id}" wget -qO- http://127.0.0.1:3000/
   ```

4. Remove build cache created by the no-cache rebuild only after recreation and verification succeed:

   ```bash
   docker builder prune --all --force
   ```

Report the exact commands, Compose service state, and probe results.

## Boundaries

- Do not run `docker system prune`.
- Do not prune images or volumes.
- Do not run `compose down`, delete the PostgreSQL volume, or reset application data.
- Do not substitute the production Compose file for this local refresh.
- If verification fails, preserve the stack and inspect logs; do not report success or perform destructive cleanup.

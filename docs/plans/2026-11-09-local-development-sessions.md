# Local Development Sessions

## Specification

Local full-stack development uses `deploy/compose.yaml` only when explicitly
started for a development or verification session. All development services use
`restart: "no"`; no host boot task, timer, or Docker daemon policy is installed.
The Docker daemon remains shared host infrastructure.

`make dev` owns the foreground session. `make dev-check` rebuilds, starts,
checks health and the bootstrap page, then exits. Both remove their development
containers, network, local application image, temporary run directory, and
unused builder cache after use. `make dev-stop` closes a leftover detached
development stack. Cleanup preserves the existing PostgreSQL volume, backups,
credentials, unrelated Docker resources, and active test runs. Disposable
isolated test volumes continue to use the existing labelled test lifecycle.

## Implementation Plan

1. Disable development restart policies and avoid a backup-sidecar anonymous
   data volume. Leave release Compose unchanged.
2. Reuse managed test resources and signal forwarding for a session runner;
   serialize ownership of the shared development project and reject containers
   from release or unrelated Compose files before mutation.
3. Add Make targets and focused lifecycle checks for success, startup failure,
   termination, concurrent sessions, cleanup failure, and data preservation.
4. Update startup documentation, Agent instructions, and the repository local
   refresh skill so verification ends with teardown by default.
5. Keep the session lock in the runner so a forced exit permits recovery. Add
   explicit metrics sessions and recognize only the base or base-plus-metrics
   configuration from registered worktrees.

## Acceptance Criteria

- Development Compose renders every service with restart disabled.
- Foreground exit, INT, TERM, and HUP tear down owned development resources;
  failed starts attempt teardown and cleanup failures return nonzero.
- A second session cannot stop an active session. Release Compose resources
  are refused, and no development cleanup command deletes named volumes.
- After SIGKILL, a surviving Compose child cannot block `make dev-stop`.
  `N2API_DEV_METRICS=1` selects the existing metrics override; recovery accepts
  that known file combination and rejects release or unknown extra files.
- `make dev-check` passes a real no-cache rebuild and health/page probes, then
  leaves no development containers or network. Existing database identity and
  backup files remain intact.
- Focused lifecycle tests and Compose configuration checks pass. The exact
  published diff passes local Codex review, PR CI, and target-branch CI.

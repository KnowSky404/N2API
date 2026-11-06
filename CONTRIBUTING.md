# Contributing To N2API

N2API is a personal, self-hosted AI API/account gateway. Keep changes small,
operationally understandable, and consistent with the existing Go monolith,
PostgreSQL store, Bun/SvelteKit frontend, and Docker Compose deployment.

## Scope

N2API V1 focuses on personal use, Codex/OpenAI OAuth, API-key-compatible
upstreams, OpenAI-compatible gateway behavior, routing, and operations. Do not
add public registration, multi-tenant platform behavior, billing, balances,
recharge, payments, merchant accounting, Redis, Kafka, or Kubernetes.

N2API is inspired by sub2api's user experience but is not a fork. Do not copy
source code from `Wei-Shaw/sub2api`.

## Before Starting

1. Read `AGENTS.md`, `README.md`, `docs/README.md`, and the relevant design or
   operations plan.
2. Inspect existing worktrees and preserve unrelated changes. Fetch the target
   branch and create a dedicated task branch and Git worktree from its current
   remote tip before editing; reuse a worktree only to continue the same task.
   Do not develop in the primary checkout or directly on `main`.
3. Reproduce bugs before changing behavior.
4. Keep one coherent behavior, fix, documentation update, or test update per
   change.

For vulnerabilities, follow [SECURITY.md](SECURITY.md) and report privately.
Do not put sensitive security details in an issue or pull request.

## Development And Tests

Prefer the repository's managed commands:

```bash
make test
make test-e2e
make test-contracts
```

Use Bun for frontend work:

```bash
cd frontend
bun install --frozen-lockfile
bun run check
bun test
bun run build
```

Use Docker Compose for relevant full-stack, container, and deployment checks.
Run only the verification proportional to the change, but report every command
run and distinguish passed, failed, skipped, local-only, and remote-CI results.
PostgreSQL store tests must use the repository's isolated test-database safety
gate and must never target a development or production database.

Never include real passwords, API keys, OAuth tokens, cookies, callback codes,
encryption keys, database dumps, or complete request/response bodies in tests,
fixtures, logs, screenshots, traces, artifacts, issues, or pull requests.

## Commits And Pull Requests

Follow [AGENTS.md](AGENTS.md#worktree-and-pull-request-workflow) for every
repository change, including documentation, configuration, tests, and dependency
updates, unless the user explicitly requests an exception:

1. Implement and verify the scoped change in its task worktree, then make atomic
   Conventional Commits.
2. Before every push and before creating a PR, run local Codex `/review` against
   the target branch, or `codex review --base <target-ref>` non-interactively.
   Review the full PR diff, fix actionable findings, rerun affected verification,
   and repeat review until clear. Any later change or rebase requires review
   again; a failed or unavailable review blocks publication.
3. Push the task branch and create a PR with the exact verification results and
   local review outcome. The project workflow authorizes these task-scoped
   actions, rebase merging, and merged-branch cleanup without repeated approval.
4. Check automatic Codex bot review on the latest PR head. If triggered, wait
   for completion, fix actionable findings, and repeat local verification,
   `/review`, push, and remote checks. Never manually trigger or request the bot.
   If it was not triggered, proceed once CI and repository requirements pass.
   Pending, running, failed, or inaccessible review activity is not a clean
   review or evidence that no review was triggered.
5. Confirm the reviewed PR head and passing checks, then rebase merge without
   bypassing protections. After merge confirmation, delete the remote task
   branch, remove its clean worktree, and delete the local task branch. Preserve
   unrelated work, update the primary checkout only when safe, and verify the
   target-branch CI. Report any blocker instead of skipping a gate.

- Make atomic changes and exclude unrelated refactors or formatting churn.
- Use Conventional Commits, for example `fix(gateway): bound response reads`,
  `test(store): cover token cleanup`, or `docs(governance): add security policy`.
- Explain the problem, behavioral change, security impact, compatibility, and
  exact verification in the pull request.
- Keep generated dependencies, build output, caches, and real environment files
  out of git.
- Do not push tags, publish images, or create releases as part of an ordinary
  contribution.

## Database Migrations

- Add a new ordered migration; never rewrite a migration that may have run.
- Include both forward and rollback behavior and explain any rollback limits.
- Preserve compatibility with PostgreSQL as the authoritative runtime store.
- Consider locks, statement duration, indexes, existing data, concurrent
  processes, cancellation, and disk use.
- Add migration contract and PostgreSQL-backed tests using an isolated test
  database.
- Call out backup or maintenance-window requirements explicitly.

## Release Impact

Changes that affect runtime behavior, migrations, images, dependencies, or
operator procedures must review the [release checklist](docs/release-checklist.md).
Record immutable image and workflow evidence only when it actually exists; do
not describe local checks as CI or operator acceptance.

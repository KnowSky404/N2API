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
2. Check the current worktree and preserve unrelated changes.
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

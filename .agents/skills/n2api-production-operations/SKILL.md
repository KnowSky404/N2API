---
name: n2api-production-operations
description: Inspect, check, plan, apply, and diagnose single-node N2API production operations through the repository canonical operator CLI. Use for N2API production deploys, upgrades, guarded application rollback, backups, restore drills, status, verification, logs, receipts, or production-operation failure handling.
---

# N2API Production Operations

Use only `./ops/n2api` as the production control interface. Do not replace it with raw Compose mutation.

## Discover First

Start every production task with read-only discovery:

```bash
./ops/n2api describe --format json
./ops/n2api doctor --format json
./ops/n2api status --format json
```

Read `docs/agent-operations.md` when detailed operator context is needed. Treat stable status, reason codes, checks, artifacts, and next actions as the contract; do not infer success from prose or Compose exit zero alone.

## Check, Plan, Apply

1. Validate configuration and inspect the exact immutable image.
2. Use the operation-specific `plan` command. Planning may write protected state or pull an image, but must not recreate the live stack.
3. Review every blocker, invariant, artifact path, risk, and owner gate.
4. Run `apply --plan PATH` only when the requested live mutation is authorized.
5. Verify runtime health and image identity, then inspect the signed operation receipt.

Never replace the target while applying a plan. Create a new plan after expiry or drift.

## Operation Boundaries

- Deploy and upgrade use separate plan/apply commands and exact `tag@digest` images.
- Upgrade requires a fresh real-operator backup plus current-image and candidate-image restore-drill evidence.
- Repeat apply may be `noop` only after target env, manifest, health, schema, and signed evidence are revalidated.
- Application rollback is an independent high-risk plan/apply operation derived from signed successful receipts. Proceed only when unchanged live schema proves compatibility.
- Database restore is not application rollback. The CLI does not perform live restore, volume deletion, or automatic rollback after failure.
- An isolated restore drill may validate evidence; `ci_fixture` evidence never satisfies a real production upgrade gate.

## Secrets And Traffic

- Never source an env file, print secret values, enable shell tracing, or place secrets in chat.
- Pass interactive or protected secret files only through supported CLI options.
- Do not make real provider traffic by default. Gateway verification is a separate explicit consent gate.
- Keep plans, receipts, checksums, and logs sanitized; report availability rather than secret content.

## Failure Handling

On failure, preserve the live stack, env, backup, plan, and receipt. Run read-only status, bounded logs, and `operations show` for the relevant operation ID. Do not run `compose down`, delete volumes, overwrite dumps, restore the live database, or start an automatic rollback.

Separate repository evidence from external owner gates. Reverse proxy, TLS, DNS, firewall, OAuth/provider traffic, off-host backup custody, GitHub workflows, release publication, and production deployment require their own current proof and authorization.

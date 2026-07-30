# Agent-Native Operations

This is the canonical starting point for production operation by a human or an
automation agent. Run the repository-local CLI on the target host:

```bash
./ops/n2api describe --format json
```

The CLI operates on the local Docker Engine. It does not use SSH, provision a
host, install packages, publish releases, upload backups, or manage a remote
control plane. The default production inputs are `.env`,
`deploy/compose.release.yaml`, Compose project `deploy`, and the protected state
directory `${XDG_STATE_HOME:-$HOME/.local/state}/n2api`.

## Discover The Contract

`describe` is side-effect free and does not require an env file, Docker daemon,
database, network, or state directory. Its JSON result lists every command,
risk, mutation class, global option, service, probe, schema, default path, and
exit code. Run it before choosing another operation.

All non-streaming JSON commands emit exactly one `n2api.ops/v1` document on
stdout. Decisions must use `status`, `reason_code`, `checks`, and the process
exit code rather than parsing human summaries. Bounded diagnostics and JSON log
content use stderr. `logs --follow --format json` is the documented streaming
exception and emits one `n2api.ops.log/v1` object per line.

| Exit | Meaning |
| ---: | --- |
| `0` | Succeeded or noop |
| `3` | Attention is required, but the operation completed |
| `4` | A safety or evidence gate blocked the operation |
| `5` | Another operation holds the shared lock |
| `6` | The operation failed |
| `64` | Invocation is invalid |
| `124` | A bounded operation timed out |
| `130` | Interrupted by SIGINT |
| `143` | Terminated by SIGTERM |

## Risks And Authorization

| Risk | Meaning | Examples |
| --- | --- | --- |
| `read_only` | Reads configuration, evidence, or runtime state | `doctor`, `status`, `verify --level basic`, `logs`, `backup verify` |
| `local_write` | Writes protected local state, an image cache, a backup, or an isolated drill | `config init`, `image resolve`, every `plan`, `backup create`, `restore drill` |
| `service_change` | Pulls and recreates the live production stack from a reviewed plan | `deploy apply`, `upgrade apply` |
| `high_risk` | Requires separate explicit consent and complete evidence | `rollback apply`, `verify --level gateway` |

A request to inspect or plan does not authorize apply. An agent must show the
plan result and obtain authorization to change the target host before running a
service-changing or high-risk apply. `verify --level gateway` additionally
requires `--allow-upstream-call`; it can create real provider traffic and must
not be inferred from permission to deploy.

## Check, Plan, Apply

Start a production workflow with read-only checks:

```bash
./ops/n2api --env-file .env doctor --format json
./ops/n2api --env-file .env config validate --format json
./ops/n2api --env-file .env status --format json
```

`deploy`, `upgrade`, and application `rollback` always use two steps. A plan is
a protected, signed snapshot with an expiry and fingerprints for the env,
Compose inputs, image manifests, runtime image, schema, and required evidence.
Apply accepts only `--plan PATH`; it never accepts a replacement target. Apply
rechecks every invariant while holding the shared lock and rejects expired,
edited, or stale plans. Repeating an already healthy target returns `noop`.

Plans and receipts are sanitized and stored at mode `0600`. Every mutating
operation uses the same lock, records its operation ID, has bounded external
commands, handles SIGINT/SIGTERM, and preserves a failure receipt. Never delete
an unfamiliar lock or edit a plan to bypass a failed check.

## Routine Operations

Use `status` for a current runtime snapshot and `verify --level basic` to check
containers, probes, the exact running digest, and runtime restrictions.
Authenticated verification reads a protected password file or prompts on a TTY:

```bash
./ops/n2api --env-file .env status --format json
./ops/n2api --env-file .env verify --level basic --format json
./ops/n2api --env-file .env verify --level authenticated \
  --admin-password-file /run/n2api-ops/admin-password --format json
```

Logs are bounded by default and redact common credential forms:

```bash
./ops/n2api --env-file .env logs n2api --since 1h --tail 200
./ops/n2api --env-file .env logs postgres --since 1h --tail 200
```

List or inspect sanitized receipts without reading the env file:

```bash
./ops/n2api operations list --format json
./ops/n2api operations show OPERATION_ID --format json
```

## Backup And Restore Drill

`backup create` writes a PostgreSQL custom archive through a temporary file,
validates its archive list, calculates a checksum, writes signed metadata, and
atomically publishes both files outside the Compose volume. Its exit status is
`3` until encrypted off-host custody is completed by the owner.

`backup verify` proves archive structure, checksum, and available metadata
integrity. It does not prove recoverability. `restore drill` reuses the existing
isolated restore verifier and requires protected files for secret inputs. Use
`--evidence-class real_operator` for an operator-created archive with signed
metadata. `ci_fixture` evidence must never be reported as a real-backup drill.

Upgrade planning requires fresh signed evidence from the same real backup: one
successful drill with the current exact image and one with the candidate exact
image. Keep the archive and metadata out of Git and place an encrypted copy in
off-host storage with its decryption material held separately.

## Failure And Rollback Boundary

An apply failure preserves the candidate env when a change may have occurred,
records bounded evidence, and does not automatically roll back. Inspect
`status`, bounded `logs`, and the operation receipt first.

Application rollback and database restore are different operations:

- `rollback plan` may select only the exact previous `tag@digest` from a signed
  successful receipt. It is allowed only when the receipt and live runtime prove
  that the database schema did not change and the previous image is available.
- A schema change or missing evidence returns
  `schema_compatibility_unproven` or another stable blocked reason. Running the
  old image anyway is forbidden.
- Live database restore is not implemented. No operator command deletes a
  volume, overwrites the live database, or turns a failed upgrade into a restore.
  Follow the manual owner-controlled recovery procedure after preserving the
  original volume and validating the backup in isolation.

## Secrets And Prohibited Actions

Keep secrets in an env file or protected regular files at mode `0600`. Do not
pass passwords, API keys, encryption material, cookies, or tokens as CLI
arguments. Do not print resolved Compose environment, raw Docker environment,
request bodies, signed URLs, archive contents, or complete DSNs. The CLI never
sources `.env`, uses `eval`, or enables shell tracing.

Do not bypass the CLI with ad hoc production `docker compose pull/up`, deploy
`latest` or `main`, edit a signed plan, remove the operation lock, run
`docker compose down -v`, remove a production volume, perform a live restore,
or automatically retry a high-risk apply. Do not claim production readiness
from fixture evidence, local tests, or GitHub CI alone.

The following remain external owner gates: target-host authorization, release
approval, exact published-image and supply-chain evidence, real-backup custody,
real-backup restore records, reverse-proxy/TLS validation, real OAuth/provider
traffic, and any manual database recovery decision.

## Scenario A: First Deployment On A New Host

Set non-secret paths and public values. `TARGET_IMAGE` must be a published
CalVer plus its complete manifest digest. The target env file must not exist.

```bash
ENV_FILE=/etc/n2api/n2api.env
STATE_DIR=/var/lib/n2api-ops
BACKUP_DIR=/srv/n2api-backups
PUBLIC_URL=https://n2api.example.com
TARGET_IMAGE='ghcr.io/knowsky404/n2api:YYYYMMDDNN@sha256:<64-lowercase-hex-characters>'

./ops/n2api describe --format json
./ops/n2api --env-file "$ENV_FILE" --state-dir "$STATE_DIR" doctor --format json
./ops/n2api --env-file "$ENV_FILE" --state-dir "$STATE_DIR" config init \
  --public-url "$PUBLIC_URL" \
  --image "$TARGET_IMAGE" \
  --accepted-risks public-bind,database-plaintext \
  --bind-address 127.0.0.1 \
  --backup-dir "$BACKUP_DIR" \
  --format json
./ops/n2api --env-file "$ENV_FILE" --state-dir "$STATE_DIR" config validate --format json

DEPLOY_PLAN="$(./ops/n2api --env-file "$ENV_FILE" --state-dir "$STATE_DIR" \
  deploy plan --format json | jq -r '.artifacts[] | select(.type == "operation_plan") | .path')"
./ops/n2api --env-file "$ENV_FILE" --state-dir "$STATE_DIR" \
  deploy apply --plan "$DEPLOY_PLAN" --format json
./ops/n2api --env-file "$ENV_FILE" --state-dir "$STATE_DIR" \
  verify --level basic --format json
```

Review the doctor and plan JSON before apply. Resolve all blocked checks rather
than suppressing them.

## Scenario B: Check Whether An Upgrade Is Safe

Prepare owner-readable `0600` secret files for the restore drill. They must
contain the administrator password and complete encryption keyring that belong
to the backup; do not derive or print them.

```bash
CURRENT_STATUS="$(./ops/n2api --env-file "$ENV_FILE" --state-dir "$STATE_DIR" \
  status --format json)"
CURRENT_IMAGE="$(jq -r '.current.n2api.configured_image' <<<"$CURRENT_STATUS")"
TARGET_IMAGE="$(./ops/n2api --env-file "$ENV_FILE" --state-dir "$STATE_DIR" \
  image resolve --version YYYYMMDDNN --format json | jq -r '.current.image')"

BACKUP_RESULT="$(./ops/n2api --env-file "$ENV_FILE" --state-dir "$STATE_DIR" \
  backup create --evidence-class real_operator --format json)"
BACKUP_ARCHIVE="$(jq -r '.artifacts[] | select(.type == "backup_archive") | .path' \
  <<<"$BACKUP_RESULT")"
./ops/n2api --env-file "$ENV_FILE" --state-dir "$STATE_DIR" \
  restore drill --archive "$BACKUP_ARCHIVE" --image "$CURRENT_IMAGE" \
  --evidence-class real_operator \
  --admin-password-file /run/n2api-ops/admin-password \
  --encryption-secret-file /run/n2api-ops/encryption-secret \
  --previous-keys-file /run/n2api-ops/previous-keys.json --format json
./ops/n2api --env-file "$ENV_FILE" --state-dir "$STATE_DIR" \
  restore drill --archive "$BACKUP_ARCHIVE" --image "$TARGET_IMAGE" \
  --evidence-class real_operator \
  --admin-password-file /run/n2api-ops/admin-password \
  --encryption-secret-file /run/n2api-ops/encryption-secret \
  --previous-keys-file /run/n2api-ops/previous-keys.json --format json

UPGRADE_PLAN="$(./ops/n2api --env-file "$ENV_FILE" --state-dir "$STATE_DIR" \
  upgrade plan --image "$TARGET_IMAGE" --format json | jq -r \
  '.artifacts[] | select(.type == "operation_plan") | .path')"
```

`backup create` normally returns attention exit `3` for the off-host custody
gate while still emitting a usable result. Complete that gate and review every
upgrade check. This scenario plans only; it does not authorize apply.

## Scenario C: Apply A Reviewed Upgrade

```bash
UPGRADE_RESULT="$(./ops/n2api --env-file "$ENV_FILE" --state-dir "$STATE_DIR" \
  upgrade apply --plan "$UPGRADE_PLAN" --format json)"
./ops/n2api --env-file "$ENV_FILE" --state-dir "$STATE_DIR" \
  verify --level basic --format json
UPGRADE_OPERATION_ID="$(jq -r '.operation_id' <<<"$UPGRADE_RESULT")"
./ops/n2api --state-dir "$STATE_DIR" operations show "$UPGRADE_OPERATION_ID" --format json
```

Run apply only after explicit host-change authorization. A gateway verification
is a separate owner decision because it sends real upstream traffic.

## Scenario D: Readiness Fails After Upgrade

```bash
./ops/n2api --env-file "$ENV_FILE" --state-dir "$STATE_DIR" status --format json
./ops/n2api --env-file "$ENV_FILE" --state-dir "$STATE_DIR" \
  logs n2api --since 30m --tail 300
./ops/n2api --state-dir "$STATE_DIR" \
  operations show "$UPGRADE_OPERATION_ID" --format json

ROLLBACK_PLAN="$(./ops/n2api --env-file "$ENV_FILE" --state-dir "$STATE_DIR" \
  rollback plan --format json | jq -r \
  '.artifacts[] | select(.type == "operation_plan") | .path')"
```

Do not apply the rollback unless its compatibility check passes and a separate
high-risk action is authorized. If planning is blocked because schema
compatibility is unproven, preserve the live volume and use the owner-controlled
database recovery procedure in the [complete manual](manual.md).

## Scenario E: Monthly Restore Drill Only

```bash
./ops/n2api --env-file "$ENV_FILE" --state-dir "$STATE_DIR" \
  backup verify --archive "$BACKUP_ARCHIVE" --format json
./ops/n2api --env-file "$ENV_FILE" --state-dir "$STATE_DIR" \
  restore drill --archive "$BACKUP_ARCHIVE" --image "$CURRENT_IMAGE" \
  --evidence-class real_operator \
  --admin-password-file /run/n2api-ops/admin-password \
  --encryption-secret-file /run/n2api-ops/encryption-secret \
  --previous-keys-file /run/n2api-ops/previous-keys.json --format json
```

Record the non-sensitive backup ID, exact image, source and restored schema,
timestamps, checks, cleanup result, off-host state, and owner sign-off. Never
put secrets, dump paths, signed storage URLs, or row contents in the record.

## Lower-Level Manual Fallback

The [complete manual](manual.md) retains raw Compose and verification commands
for development, incident diagnosis, and an owner-controlled recovery when the
canonical CLI cannot express the required action. Those commands are a
lower-level fallback, not the default agent workflow. Preserve the CLI's exact
image, secret, plan, evidence, lock, volume, and receipt boundaries when using
them.

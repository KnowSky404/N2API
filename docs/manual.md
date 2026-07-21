# N2API Manual

This manual contains the detailed deployment, client configuration, provider
account, routing, and gateway operations guidance for N2API. The default
deployment target is Docker Compose on a small VPS.

## Start Locally

From the repository root:

```bash
docker compose -f deploy/compose.yaml up --build
```

The default app URL is `http://localhost:3000`. This zero-configuration path
uses predictable development-only credentials. To customize them, copy
`.env.example` to `.env`, replace every `change-me` value, and then add
`--env-file .env` to the command.

An existing zero-configuration development volume created before startup
validation still has the legacy PostgreSQL password stored inside the volume.
Migrate that local role once without deleting data, then start the stack again:

```bash
docker compose -f deploy/compose.yaml exec postgres psql -U n2api -d n2api -v ON_ERROR_STOP=1 -c "ALTER ROLE n2api PASSWORD 'n2api-local-postgres-password'"
docker compose -f deploy/compose.yaml up -d --force-recreate
```

Do not run this fixed development-password command on a deployment with custom
credentials. Update its `.env` and PostgreSQL role to the same operator-chosen
random password instead.

## Startup Configuration Safety

N2API validates security-sensitive configuration before opening its listener.
`N2API_PUBLIC_URL` must be an absolute HTTP or HTTPS origin with no credentials,
query, fragment, or non-root path. Administrator passwords must contain at
least 12 bytes, encryption secrets at least 32 bytes, and the two values must
be different. Known template values are rejected. PostgreSQL connection
strings are parsed with the same pgx parser used at runtime, and startup errors
name variables without echoing credentials or connection strings.

Some valid development and container topologies are intentionally unsafe. They
must be acknowledged individually in the comma-separated
`N2API_ACCEPT_RISKS` variable:

- `public-http` permits a non-loopback `N2API_PUBLIC_URL` to use HTTP.
- `public-bind` permits `N2API_HOST` to listen on a non-loopback address.
- `database-plaintext` permits a PostgreSQL primary or fallback connection
  without TLS.

Unknown values, empty list elements, and a blanket `all` value are rejected.
These acknowledgements never bypass malformed URLs or connection strings,
placeholder or short secrets, or identical administrator and encryption
secrets. The development Compose file supplies `public-http`, `public-bind`,
and `database-plaintext` by default and uses separate local-only credentials
when no `.env` exists. Set `N2API_ACCEPT_RISKS` explicitly in `.env` to narrow
or replace those development defaults.

`OPENAI_API_BASE_URL` and the OpenAI OAuth endpoints are also parsed at
startup. OAuth authorization and token endpoints must use HTTPS. The API base
URL may use HTTP only when `N2API_ALLOW_HTTP_API_UPSTREAMS=true`, matching the
explicit opt-in used for API-key upstream accounts.

## Reverse Proxy Trust

N2API ignores `X-Forwarded-For`, `X-Real-IP`, `X-Forwarded-Proto`, and
`X-Forwarded-Host` by default. If a reverse proxy connects directly to N2API,
set `N2API_TRUSTED_PROXY_CIDRS` to the proxy's exact address or network. For a
same-host proxy using the loopback interface:

```dotenv
N2API_TRUSTED_PROXY_CIDRS=127.0.0.1/32,::1/128
```

`N2API_PUBLIC_URL` is the canonical scheme and host for direct requests. A
trusted proxy may override them with one valid `X-Forwarded-Proto` and
`X-Forwarded-Host` value; untrusted requests cannot replace the configured
origin with either forwarding headers or a forged HTTP `Host` value.

For a containerized proxy, use its actual Compose network CIDR after verifying
the direct peer address seen by N2API. Do not configure `0.0.0.0/0` or `::/0`.
An invalid CIDR prevents startup instead of silently weakening the boundary.

In a multi-hop chain, `X-Forwarded-For` is ordered from the original client on
the left to the proxy nearest N2API on the right. N2API walks the list from
right to left, skips only configured trusted proxy hops, and uses the first
untrusted address as the client. Every proxy hop that should be skipped must be
listed in `N2API_TRUSTED_PROXY_CIDRS`; malformed chains are ignored in favor of
the direct peer, including scheme and host metadata. If every visible hop is
trusted, the farthest visible address is recorded. The public edge proxy must
overwrite or sanitize client-sent forwarding headers before appending its own
hop, and the proxy nearest N2API must overwrite `X-Forwarded-Proto` and
`X-Forwarded-Host` as single values rather than comma-separated lists. The
standardized `Forwarded` header is not currently used.

## Administrator Login Protection

Administrator login throttling is enabled by default. N2API tracks failed
attempts independently by normalized client IP and lowercase username; either
dimension can temporarily deny a request. After five failures, denial starts at
one second and doubles up to 60 seconds. Entries expire after 15 minutes of
inactivity, the combined map is bounded to 4096 entries, and a successful login
clears both identities. Concurrent password checks are reserved atomically, so
an initial burst cannot exceed the configured threshold. Rejected requests keep
the same `invalid_credentials` response body for known and unknown usernames.
The threshold response includes `Retry-After`, and requests made during the
denial period return HTTP 429 with an integer `Retry-After` value.

Use `N2API_ADMIN_LOGIN_THROTTLE_FAILURES` to set the threshold from 1 to 20 and
`N2API_ADMIN_LOGIN_THROTTLE_MAX_ENTRIES` to set the memory bound from 128 to
16384. When the bound is exhausted, new identities fail closed instead of
evicting active failure state. Set `N2API_ADMIN_LOGIN_THROTTLE_ENABLED=false`
only as a temporary rollback. State is process-local and resets after a restart,
which matches the single-node deployment model. Reverse-proxy deployments must
configure `N2API_TRUSTED_PROXY_CIDRS` correctly so the IP dimension observes the
client rather than the nearest proxy.

Repeated login failures are globally aggregated into one System Event per
one-minute window while throttling is enabled. The event uses a fixed
administrator target; usernames, passwords, and request bodies are never stored
in those events.

Administrator sessions expire after seven days by default. Set
`N2API_ADMIN_SESSION_TTL_HOURS` from 1 to 8760 to change the absolute lifetime
of newly created sessions. Changing the value does not extend or shorten
sessions that already exist, and activity never extends a session's expiry.
Open **Active sessions** from the administrator menu to inspect active login
sessions, revoke one session, or revoke every session except the current one.
Revoking the current row signs out that browser immediately. Session rows show
only bounded client metadata: the creation IP is reduced to an IPv4 `/24` or
IPv6 `/64` network and the User-Agent is cleaned and truncated. Authentication
tokens and token hashes are never returned by the session API.

## Browser Request Security

N2API rejects unsafe browser requests that carry the administrator session
cookie unless `Origin` matches the trusted external request origin and Fetch
Metadata, when present, reports `Sec-Fetch-Site: same-origin`. Requests from
scripts and CLI tools remain compatible when they omit both browser headers.
The logout endpoint applies the same browser checks even when no session cookie
is present, because its response still changes browser cookie state.
Configure `N2API_PUBLIC_URL` or `N2API_TRUSTED_PROXY_CIDRS` correctly so N2API
can derive the external scheme and host without trusting client-supplied proxy
headers.

All responses include MIME sniffing, referrer, permissions, framing, and Content
Security Policy headers. HSTS is emitted only when the trusted external scheme
is HTTPS. Admin API and OAuth callback responses use `Cache-Control: no-store`.
The CSP permits only same-origin resources and `data:` images. SvelteKit emits
build-specific SHA-256 hashes for the static bootstrap script and generated
style elements. Inline style attributes and the manual OAuth callback style
remain allowed because the current UI requires them; remote scripts are not
allowed.

## Container Runtime Identity

The N2API application image runs as the fixed `n2api` identity with UID and GID
`10001`. The binary and static admin assets are owned by `root:n2api` with no
write bits, and the runtime user has no home directory or login shell. The
image retains the Alpine CA bundle for HTTPS upstream verification but does not
need a persistent writable application path.

Inspect a running Compose container without printing its environment:

```bash
docker compose -f deploy/compose.yaml exec n2api id
docker compose -f deploy/compose.yaml exec n2api stat -c '%u:%g %a %n' /app/n2api /app/frontend/build/200.html
```

The first command must report `uid=10001(n2api) gid=10001(n2api)`. The image
smoke matrix also verifies readiness, static assets, the CA bundle, application
file write denial, and a clean SIGTERM exit within ten seconds on both supported
platforms. Future bind mounts must be readable by UID/GID `10001` and must not
make application files writable.

## Container Runtime Restrictions

The development and release Compose definitions run only the N2API application
container with a read-only root filesystem, all Linux capabilities dropped, and
`no-new-privileges` enabled. A writable 16 MiB `/tmp` tmpfs is available to the
unprivileged application identity with `noexec`, `nosuid`, and `nodev`; it is
ephemeral and must not be used for persistent data. PostgreSQL is intentionally
excluded because its official image requires a persistent writable data path.

N2API uses the `unless-stopped` restart policy and has ten seconds to exit after
SIGTERM before Docker sends SIGKILL. Inspect the effective restrictions without
printing container environment variables:

```bash
docker inspect "$(docker compose -f deploy/compose.yaml ps -q n2api)" \
  --format '{{.HostConfig.ReadonlyRootfs}} {{.HostConfig.SecurityOpt}} {{.HostConfig.CapDrop}} {{json .HostConfig.Tmpfs}}'
```

The output must report a read-only root filesystem,
`no-new-privileges:true`, capability drop `ALL`, and a bounded `/tmp` tmpfs.
Writes outside `/tmp` must fail. Bind mounts added by operators are separate
writable surfaces and should remain read-only unless the application has a
documented persistence requirement.

## Published Images

The `CI Image` workflow tests every pull request without publishing an image.
After a commit reaches `main`, the same image that passed the PostgreSQL smoke
test on both supported platforms is published as a multi-platform image with
two development tags:

- `main` moves to the newest tested commit on the default branch.
- `sha-<12 characters>` identifies one tested source commit and is immutable.

Stable releases add two more tags without rebuilding the image:

- `YYYYMMDDNN` is an immutable Europe/Berlin CalVer release, for example
  `2026071401`.
- `latest` moves only when a stable GitHub Release is published.

The Git tag, GitHub Release tag, and container version tag always use the same
CalVer value without a `v` prefix.

Published images support:

- `linux/amd64`
- `linux/arm64`

All repository-owned container builds pin readable dependency versions and their
multi-platform manifest digests. The current baseline is Bun `1.3.14`, Go
`1.26.4` on Alpine `3.23`, Alpine `3.23.5`, and PostgreSQL `18.4` on Alpine
`3.23`. E2E-only uv and Python images are pinned the same way. A version tag
communicates the intended dependency while the digest makes the resolved bytes
reproducible on both supported architectures. Update both together; never
replace a digest without verifying that it belongs to the adjacent tag.

Dependabot checks Go modules, Bun workspaces, Python contract dependencies,
GitHub Actions, Dockerfiles, and Compose definitions every Monday. Patch and
minor updates are grouped by ecosystem; major updates remain isolated for
explicit review. Every dependency pull request must pass the normal `CI Image`
workflow. Container update pull requests must also pass
`dev/ci/verify-pinned-dependencies.sh`, keep readable version tags next to
digests, and smoke-test both supported image architectures before merge.

Every tested image carries the same build identity in the binary and OCI
metadata:

- `org.opencontainers.image.source` identifies the source repository.
- `org.opencontainers.image.revision` is the complete Git commit SHA.
- `org.opencontainers.image.version` is the immutable `sha-<12 characters>`
  build version.
- `org.opencontainers.image.created` is the source commit time normalized to
  UTC RFC 3339.

The commit time, rather than the workflow start time, keeps rebuilds of the
same commit traceable to one stable identity. CalVer releases promote the
already-tested manifest without rebuilding it, so a running release continues
to report its source `sha-<12 characters>` build version while the CalVer tag
identifies the promoted manifest.

For every published `main` image, CI generates separate SPDX JSON SBOMs and
Trivy vulnerability reports for `linux/amd64` and `linux/arm64`. Both evidence
jobs read the same immutable parent manifest reference, `IMAGE@sha256:...`, and
select their platform without rebuilding the image. Each platform SBOM is
attested to that parent manifest digest in GHCR. CI also retains the SBOM,
Trivy JSON, and a non-sensitive metadata file naming the parent digest,
platform, and evidence filenames for 14 days.

Vulnerability findings are report-only until the owner approves a blocking
severity, fix-availability, and exception-expiry policy. Scanner execution,
invalid JSON or schema, attestation, and artifact upload errors still fail the
platform evidence job. Release preview and publish runs verify that the tested
parent digest has an SPDX attestation issued by this repository's `CI Image`
workflow for the selected source commit; stable tags continue to promote that
same digest without rebuilding it.

Release `2026071401` predates multi-platform publishing and supports only
`linux/amd64`. ARM64 hosts must use a later release. Inspect any tag before
deployment with:

```bash
N2API_VERSION=YYYYMMDDNN
docker buildx imagetools inspect "ghcr.io/knowsky404/n2api:${N2API_VERSION}"
```

## Preview and Publish a Release

Open **Actions > Release > Run workflow** on the `main` branch. Keep `mode` set
to `preview` first. Preview mode verifies the tested `sha-<12 characters>` image,
calculates the next CalVer, generates release notes from Conventional Commits,
and uploads the proposed Release body. It does not create Git tags, container
tags, or GitHub Releases. A later run may select a newer `main` commit, so a
standalone preview is informational rather than a locked release candidate.

The generated Release body records that the two platform SBOM attestations and
report-only vulnerability evidence are tied to the tested manifest digest. The
attestations remain associated with the immutable image in GHCR; the downloadable
per-platform evidence artifacts are retained with the originating `CI Image`
run for 14 days.

Complete the [release checklist](release-checklist.md) for the exact candidate
before publish approval. In particular, record the last successful real-backup
restore drill and every immutable image tag or digest exercised by that drill.
Generated CI fixtures do not satisfy this operator recovery gate.

For an approval gate, configure the repository's `release` environment with a
required reviewer. Run the workflow with `mode` set to `publish`; its prepare
job creates a fresh `release-preview-*` artifact, then the publish job waits for
environment approval and consumes that artifact from the same workflow run.
After approval it creates the Git tag, promotes the tested digest to the CalVer
tag, publishes the GitHub Release, and only then moves `latest`. The workflow
never rebuilds the image and refuses to replace a CalVer tag that points to
another commit or manifest digest. A rerun can repair `latest` after a partial
failure without replacing an existing Release.

## Install Docker on Ubuntu 24.04 ARM64

Remove conflicting distribution packages, then install Docker Engine, Buildx,
and Compose from Docker's official apt repository:

```bash
mapfile -t conflicting_packages < <(
  dpkg --get-selections \
    docker.io docker-compose docker-compose-v2 docker-doc podman-docker containerd runc \
    2>/dev/null \
    | awk '$2 == "install" { print $1 }'
)
if [ "${#conflicting_packages[@]}" -gt 0 ]; then
  sudo apt remove -y "${conflicting_packages[@]}"
fi
sudo apt update
sudo apt install -y ca-certificates curl
sudo install -m 0755 -d /etc/apt/keyrings
sudo curl -fsSL https://download.docker.com/linux/ubuntu/gpg -o /etc/apt/keyrings/docker.asc
sudo chmod a+r /etc/apt/keyrings/docker.asc

sudo tee /etc/apt/sources.list.d/docker.sources >/dev/null <<EOF
Types: deb
URIs: https://download.docker.com/linux/ubuntu
Suites: $(. /etc/os-release && echo "${UBUNTU_CODENAME:-$VERSION_CODENAME}")
Components: stable
Architectures: $(dpkg --print-architecture)
Signed-By: /etc/apt/keyrings/docker.asc
EOF

sudo apt update
sudo apt install -y docker-ce docker-ce-cli containerd.io docker-buildx-plugin docker-compose-plugin
sudo docker run --rm hello-world
```

Confirm that the host and Docker installation report ARM64 and Compose v2:

```bash
uname -m
dpkg --print-architecture
docker compose version
```

Expected architecture values are `aarch64` and `arm64`. Docker daemon access is
root-equivalent; either keep using `sudo` for Docker commands or grant access
only to a trusted administrator.

## Deploy a Published Image

Check out the release being deployed, copy the example environment file, and
pin the same immutable version in `.env`:

```bash
git clone https://github.com/KnowSky404/N2API.git
cd N2API || exit 1
N2API_VERSION=YYYYMMDDNN
git checkout "$N2API_VERSION"
cp .env.example .env
sed -i "s|^N2API_IMAGE=.*|N2API_IMAGE=ghcr.io/knowsky404/n2api:${N2API_VERSION}|" .env
```

Replace every `change-me` value before starting the stack. At minimum, set:

- `POSTGRES_PASSWORD` to a random value and use the same value in
  `DATABASE_URL`. A hex value from `openssl rand -hex 32` avoids URL-encoding
  ambiguity.
- `N2API_ADMIN_PASSWORD` to a unique strong password.
- `N2API_ENCRYPTION_SECRET` to a separate long random value. Losing this value
  makes encrypted provider credentials unrecoverable.
- `N2API_PUBLIC_URL` to the externally visible origin, including `https://`
  when TLS terminates in front of N2API.
- `N2API_ACCEPT_RISKS=public-bind,database-plaintext` when using the bundled
  release topology. The application must listen on the container network, and
  the bundled PostgreSQL service does not enable TLS. For an external
  TLS-required PostgreSQL service, omit `database-plaintext`.

### Encrypted Secret Envelope

New encrypted provider credentials and reusable client-key secrets use the
self-describing format `n2api:v1:<key-id>:<secret-kind>:<payload>`. The version,
non-secret key ID, and fixed credential kind are authenticated with the
ciphertext. Existing unversioned values remain readable and are not rewritten
during an ordinary upgrade.

`N2API_ENCRYPTION_KEY_ID` identifies the current `N2API_ENCRYPTION_SECRET` and
defaults to `default` so existing deployments can upgrade without a new required
setting. IDs contain only ASCII letters, digits, `.`, `_`, or `-` and are at most
64 characters. Keep `N2API_ENCRYPTION_PREVIOUS_KEYS=[]` outside an explicit
rotation window. During a rotation window it is an ordered JSON array:

```dotenv
N2API_ENCRYPTION_KEY_ID=current-202607
N2API_ENCRYPTION_PREVIOUS_KEYS='[{"id":"default","secret":"<previous-secret>"}]'
```

The current and previous secrets must each be at least 32 bytes, must be unique,
and must differ from the administrator password. At most eight previous keys are
accepted. New writes always use the current key. A versioned envelope uses only
its named key and must match the credential kind expected by its consuming
field; moving an access-token envelope into a refresh-token, proxy, or API-key
field is rejected. Only legacy raw-base64 ciphertext tries current and then
previous keys in configured order. Invalid versions, missing keys, kind
mismatches, or authentication failures stop the credential operation without
exposing key material, plaintext, or ciphertext. An unreadable encrypted proxy
also stops the affected outbound operation instead of silently bypassing the
proxy.

Legacy raw-base64 values predate credential-kind binding, so they retain only
the original GCM integrity guarantee until Task 3 re-encrypts them. Version 1
binds the credential kind but not a database row identity; same-kind row
substitution remains outside this task's protection boundary.

Before changing encryption keys, take a database backup and complete the
isolated restore drill with the exact image and keyring that created it. Then
verify every non-empty reversible credential in the live database:

```bash
docker compose -f deploy/compose.yaml exec -T n2api \
  /app/n2api admin verify-encryption
```

`verify-encryption` is a read-only dry run. It does not run migrations,
bootstrap an administrator, rewrite credentials, or start HTTP/background
services. Its single JSON document always contains all eight credential types,
including zero-count types. Counts cover OAuth code verifiers; provider access,
refresh, and ID tokens; provider API keys and proxy URLs; and reusable client
API-key secrets and alert action destinations. Authenticated key IDs are grouped
by `v1` or `legacy` format.
For legacy values, the reported ID is the key that actually decrypted the
value, not an inferred default.

The shortened example below shows one type entry and one failure shape; actual
output is never abbreviated and always includes all eight type entries.

```json
{
  "status": "failed",
  "totals": {"values": 2, "verified": 1, "failed": 1},
  "types": [
    {
      "table": "provider_account_credentials",
      "type": "oauth-refresh-token",
      "values": 2,
      "verified": 1,
      "failed": 1,
      "keyIds": [{"id": "default", "format": "legacy", "count": 1}]
    }
  ],
  "failures": [
    {
      "table": "provider_account_credentials",
      "type": "oauth-refresh-token",
      "rowId": "42",
      "status": "unreadable"
    }
  ]
}
```

The report never includes plaintext, ciphertext, provider identity, key prefix,
state hash, or raw crypto/database errors. Exit code `0` means every value was
verified (including an empty database), `1` means the complete report contains
one or more unreadable rows, and `2` means command usage, configuration,
database access, query, or output failed. Do not begin re-encryption while any
row is unreadable or before the backup restore drill succeeds.

### Alert Rules And Delivery

The database stores notification actions and exact-match System Event rules.
Action destinations use the
dedicated `alert-action-destination` encryption kind; action and rule reads
return only whether a destination is configured, never its plaintext or
ciphertext. Supported action records are `generic_webhook` and `ntfy`. Rules can
filter by category, severity, and action, aggregate within a fixed window,
apply a cooldown, deduplicate by rule or event target, and optionally recognize
one explicit recovery action.

Each rule is limited to 1024 deduplication states. When the limit is reached,
only the oldest idle state may be evicted; active firing state is never silently
discarded. Event evaluation and state admission are serialized per rule so
concurrent events cannot lose aggregation counts or duplicate a notification
decision. Updating a rule atomically clears its prior aggregation and firing
state. The schema creates no action or rule by default.

Authenticated owners manage this configuration from the compact `/alerting`
admin page or the `/api/admin/alert-actions` and `/api/admin/alert-rules`
endpoints. Action responses expose only `destinationConfigured`; they never
return the destination or its ciphertext. Action and rule updates require the
current `expectedUpdatedAt` revision and return `409 stale_update` when another
edit won the race. An action cannot be deleted while a rule references it.
Successful create, update, and delete operations commit their audit System Event
in the same PostgreSQL transaction as the configuration change.

The rule editor also exposes a server-owned template catalog at
`GET /api/admin/alert-rule-templates`. Installing a template requires an
existing delivery action through
`POST /api/admin/alert-rule-templates/{key}/install`. Installation is explicit,
creates the rule disabled, and is idempotent by the persisted template key:
retries return the existing rule without changing its delivery action, enabled
state, or later owner edits. Deleting a template-derived rule is allowed; it is
created again only when an owner explicitly installs the template again. The
catalog includes `oauth-refresh-repeated-v1`, which matches three
automatic OAuth refresh failures for the same event target within 15 minutes,
uses a one-hour cooldown, and can notify on the next automatic refresh success.
Model-test refresh failures use the separate
`oauth.refresh.diagnostic.failed` action and therefore do not contribute to
this operational threshold. It also includes
`request-log-retention-failed-v1`, which matches either complete or partial
automatic Request Log retention failures, uses a 24-hour cooldown, and can
notify when the next scheduled retention cycle succeeds. Shutdown cancellation
updates task status but does not emit the failure signal. Disabling the runner,
disabling the saved retention policy, persistent lock contention, or failure to
record a System Event can leave a firing rule without a recovery event.

`POST /api/admin/alert-actions/{id}/test` tests only the saved destination and
requires the same action revision. It remains available when the dispatcher or
action is disabled, performs one bounded five-second attempt, and returns only a
sanitized result: pass/fail status, HTTP status when available, latency, a fixed
error code, and whether the failure is retryable. Response bodies, response
headers, destination values, ciphertext, and raw network errors are never
returned or recorded. Tests are globally serialized in the running process and
each action has a persisted 30-second cooldown. The latest sanitized result is
stored without changing the action configuration revision and is shown after a
refresh or restart. Test audit events are excluded from alert-rule matching to
prevent recursive notifications.

Bounded delivery is independently gated by
`N2API_ALERT_DELIVERY_ENABLED=false`. When enabled, PostgreSQL publishes each
committed System Event ID to a dedicated listener; events in rolled-back
transactions are never sent. The listener reserves one pool connection, so an
enabled deployment must configure at least two PostgreSQL pool connections.
The listener, one ordered evaluator, and two HTTP workers run outside gateway
request processing. Each rule/deduplication stream is stably assigned to one
worker so its firing and recovery notifications remain ordered; unrelated
streams can deliver concurrently. Queue saturation drops the event without
waiting on the gateway and records one aggregate overflow event per reporting
window.

Generic Webhook sends a bounded JSON summary. ntfy sends a bounded plain-text
summary to its configured topic URL. The dedicated delivery client has a short
timeout, does not use environment proxies, never follows redirects, and never
stores or logs a destination, query string, response body, or raw network error.
Any `2xx` response succeeds. Network failures, `408`, `425`, `429`, and `5xx`
responses receive at most three capped exponential attempts; other responses
fail permanently. Exhausted delivery and queue-overflow events are excluded from
all rule matching to prevent recursive notification storms.

Rule state and cooldown are committed before the outbound attempt. Delivery
failure does not roll them back. The in-process queues are intentionally not a
durable outbox: a crash, restart, listener disconnect, or sustained queue
saturation can lose a notification. Authenticated `GET /api/admin/health`
reports current counters and sanitized last-result state at
`tasks.alertDelivery`; unauthenticated health responses omit it. Disable the
startup gate and all rules to stop outbound delivery.

Changing `N2API_ENCRYPTION_SECRET` invalidates existing Request Log and System
Event cursors because those cursor signatures intentionally use only the current
secret. Previous encryption keys do not keep old cursors valid. This task does
not bulk-rewrite stored values. Once the upgraded application creates or refreshes
a credential, an older image cannot read that new envelope; rollback then requires
the upgraded image with the prior keyring or a database backup taken before new
envelope writes.

The release Compose file requires `.env` and an explicit `N2API_IMAGE`; it has
no `latest` fallback. Use the immutable CalVer matching the checked-out release
or a complete digest reference. Missing required variables are rejected at
Compose interpolation time. The stack publishes N2API on `127.0.0.1` by
default. Set `N2API_BIND_ADDRESS=0.0.0.0` or `::` only when an intentionally
public host listener is protected by the host firewall or an operator-provided
ingress.

### Host Binding Modes

Keep `N2API_HOST=0.0.0.0` inside the container so other Compose services and
the healthcheck can reach N2API. `N2API_BIND_ADDRESS` controls only the host
listener created by Docker:

| Deployment path | `N2API_BIND_ADDRESS` | Required operator action |
| --- | --- | --- |
| Reverse proxy on the same host | `127.0.0.1` (default), or `::1` for an IPv6-only proxy | Set `N2API_PUBLIC_URL` to the external HTTPS origin and keep the proxy on the selected loopback family. |
| Trusted IPv4 LAN | a specific host LAN address, or `0.0.0.0` only when every IPv4 interface is intended | Restrict port 3000 with the host firewall. Add `public-http` to `N2API_ACCEPT_RISKS` only when the external origin really uses HTTP. |
| Direct public IPv4 | `0.0.0.0` | Do not use for secure production: N2API does not terminate TLS, and a firewall does not encrypt credentials or API traffic. Use a TLS ingress on loopback or a Docker network instead. Any temporary public HTTP use requires `public-http`. |
| IPv6 listener | a specific host IPv6 address, `::1` for loopback, or `::` for all IPv6 interfaces | Confirm the firewall has equivalent IPv6 rules and verify the listener with `ss -lntp`. A single release mapping publishes only the selected address family. |
| Docker network only | no published port | Remove the inherited port with a Compose override and connect the ingress container to the same Docker network. |

Docker Compose 2.24.4 or later can remove the release port with an override
file containing:

```yaml
services:
  n2api:
    ports: !override []
```

Apply that file after the release definition. The ingress can then use
`http://n2api:3000` on the shared Compose network while
`N2API_PUBLIC_URL` remains the browser-visible HTTPS origin:

```bash
docker compose -f deploy/compose.release.yaml -f compose.docker-only.yaml \
  --env-file .env config --quiet
docker compose -f deploy/compose.release.yaml -f compose.docker-only.yaml \
  --env-file .env up -d
```

For simultaneous IPv4 and IPv6 publishing, use a separate override with both
long-syntax mappings instead of relying on one `N2API_BIND_ADDRESS` value:

```yaml
services:
  n2api:
    ports: !override
      - name: http-ipv4
        target: 3000
        published: "${N2API_PORT:-3000}"
        host_ip: "0.0.0.0"
        protocol: tcp
      - name: http-ipv6
        target: 3000
        published: "${N2API_PORT:-3000}"
        host_ip: "::"
        protocol: tcp
```

Treat this as public on both address families and apply firewall and TLS ingress
rules to both. A protected dual-stack reverse proxy is preferable to publishing
N2API's plain HTTP listener directly.

Keep `.env` readable only by its owner, validate the Compose model without
printing the resolved secrets, then pull and start the release:

```bash
chmod 600 .env
docker compose -f deploy/compose.release.yaml --env-file .env config --quiet
docker compose -f deploy/compose.release.yaml --env-file .env pull
docker compose -f deploy/compose.release.yaml --env-file .env up -d
docker compose -f deploy/compose.release.yaml --env-file .env ps
curl -fsS http://127.0.0.1:3000/readyz
curl -fsS http://127.0.0.1:3000/livez
curl -fsS http://127.0.0.1:3000/version
curl -fsS http://127.0.0.1:3000/api/admin/health
docker image inspect "ghcr.io/knowsky404/n2api:${N2API_VERSION}" --format '{{.Os}}/{{.Architecture}}'
```

The final command must print `linux/arm64` on an ARM64 host. The release Compose
file publishes port `3000` on host loopback unless `N2API_BIND_ADDRESS` changes
it.

After the stack is healthy, sign in, connect and test a provider account,
enable its supported models, create a client API key, and verify `/v1/models`
and one streaming `/v1/responses` request with that key.

## Back Up and Upgrade

Create a PostgreSQL custom-format backup before every upgrade:

```bash
mkdir -p backups
docker compose -f deploy/compose.release.yaml --env-file .env exec -T postgres \
  sh -c 'pg_dump -U "$POSTGRES_USER" -d "$POSTGRES_DB" -Fc' \
  > "backups/n2api-$(date +%Y%m%d-%H%M%S).dump"
```

Keep backups outside the Compose volume and periodically verify them with
the isolated restore drill below. The repository ignores `backups/`, but a
database dump still belongs in encrypted off-host storage and must never be
committed.

Use the exact N2API image tag or digest that should serve the restored data.
Provide the administrator credentials and complete encryption keyring from the
backup's deployment through the environment; the script does not print them.
It creates its own random Compose project, fixed temporary database, internal
network, and volume. It never accepts a database URL or Compose project name.

```bash
read -rsp 'Restore admin password: ' N2API_RESTORE_ADMIN_PASSWORD; echo
read -rsp 'Restore encryption secret: ' N2API_RESTORE_ENCRYPTION_SECRET; echo
read -rsp 'Restore previous-key JSON (or []): ' N2API_RESTORE_ENCRYPTION_PREVIOUS_KEYS; echo
export N2API_RESTORE_ADMIN_PASSWORD N2API_RESTORE_ENCRYPTION_SECRET N2API_RESTORE_ENCRYPTION_PREVIOUS_KEYS
export N2API_RESTORE_ADMIN_USERNAME='admin'
export N2API_RESTORE_IMAGE='ghcr.io/knowsky404/n2api:YYYYMMDDNN'
export N2API_RESTORE_ENCRYPTION_KEY_ID='default'
dev/verification/restore-backup.sh backups/n2api-YYYYMMDD-HHMMSS.dump
unset N2API_RESTORE_ADMIN_PASSWORD N2API_RESTORE_ENCRYPTION_SECRET N2API_RESTORE_ENCRYPTION_PREVIOUS_KEYS
unset N2API_RESTORE_ENCRYPTION_KEY_ID
```

Use the exact key ID and previous-key array that were active when the backup was
taken. For backups outside a rotation window, enter `[]` and keep the default key
ID unless the deployment had explicitly changed it.

The drill lists and restores the custom archive in one transaction, starts the
selected image so pending migrations run, waits for readiness, checks schema
version/counts/foreign-key integrity, verifies one restored reusable API key
can be decrypted when present, and runs the mock-upstream gateway E2E. Its
report contains counts and status labels only. Success and failure both remove
the exact temporary containers, network, and volume. A successful generated
fixture drill is automated evidence; run the same command on a current real
operator backup before claiming the deployment is recoverable.

### Restore Drill Schedule And Records

Run the isolated restore drill at least once each calendar month and before
every upgrade. Use a fresh pre-upgrade backup. Prove it first with the currently
deployed immutable image; when the target release contains migrations, repeat
the isolated drill with the proposed immutable image before changing the live
stack. A drill is not complete until cleanup has removed its temporary
containers, network, and volume.

Use the [release checklist](release-checklist.md) as the drill record. Record
UTC backup and drill timestamps, source deployment version, exact tested image
tags or digests, planned window, measured duration, redacted check outcomes,
retention expiry or deletion condition, and owner sign-off. Allocate at least
60 minutes or twice the previous measured duration, whichever is longer; this
is a planning window rather than a recovery-time guarantee.

Keep at least the three most recent successful monthly backups. Retain each
pre-upgrade backup until the upgraded deployment passes its next monthly
restore drill. Backups must be encrypted in off-host storage, with decryption
material stored separately from the dump. Keep real operator dumps and storage
credentials out of Git, GitHub Actions, logs, and drill records. CI may validate
only generated non-sensitive fixture dumps.

### Portable Configuration Export

After signing in, open **Gateway** and use **Export JSON** under **Portable
configuration**, or send `GET /api/admin/configuration/export` with an
authenticated administrator session. The response is a no-store JSON
attachment named `n2api-portable-config-v1-YYYYMMDDTHHMMSSZ.json`. The server
rejects an export larger than 5 MiB and records the successful security audit
event before sending the file.

Format version 1 contains routing pools, memberships and fallbacks; active API
non-deleted API key templates (active or disabled) with names, policies,
limits, budgets and pool references;
provider account names, scheduling fields, models and sanitized base URLs;
model, usage-pricing and gateway settings; fingerprint profiles; and error
passthrough rules. File-local references preserve relationships without making
database IDs an import conflict key. API upstream base URLs have user info,
query strings and fragments removed. Every custom fingerprint header value is
replaced with `[redacted]`, including values that do not look sensitive.

The export never contains administrator or session state, OAuth state or
subjects, provider credentials, proxy URLs, client API key hashes, prefixes or
encrypted reusable secrets, request logs, system events, provider test history,
or runtime failure state. Alert rule and action schemas exist, but portable
format version 1 intentionally omits them and reports
`unsupportedSections: ["alertRules", "alertActions"]` instead of claiming that
they were exported or redacted.

Portable configuration is a review and migration aid, not a complete backup
and not currently importable. PostgreSQL remains the authoritative recovery
source because it retains encrypted credentials and all operational state.
Continue taking and verifying database backups even when configuration exports
are stored separately.

For an upgrade or rollback, change `N2API_IMAGE` to the target CalVer, then pull
and recreate the stack:

```bash
docker compose -f deploy/compose.release.yaml --env-file .env pull
docker compose -f deploy/compose.release.yaml --env-file .env up -d
curl -fsS http://127.0.0.1:3000/readyz
```

Use immutable CalVer tags or complete digest references for production. The
moving `latest` and `main` tags are for explicit update experiments and
development validation, not unattended release deployment.

## Health Probes

N2API exposes separate process and dependency probes:

- `GET /livez` reports only that the HTTP process can respond. It does not
  check PostgreSQL or provider accounts.
- `GET /readyz` reports ready only when PostgreSQL responds and the static admin
  build contains its application entry document. Migrations, administrator
  bootstrap, and background runner construction finish before the HTTP server
  starts listening. Provider account availability does not affect readiness.
- `GET /healthz` remains a compatibility alias for the liveness behavior.
- `GET /version` is public and returns only the short build version, for
  example `{"version":"sha-0123456789ab"}`.
- `GET /api/admin/health` remains publicly usable for its existing
  `status`/`database` response. With a valid administrator session cookie, it
  also includes the complete commit SHA and UTC build time under `build`.

After sign-in, the Dashboard shows the short build version in its compact
system status. Hover over the value, or focus and activate it, to inspect the
complete commit and build time. Signing out removes the authenticated build
detail from client state. Local source builds use explicit non-release values:
version `dev`, commit `unknown`, and build time `1970-01-01T00:00:00Z`.

Both development and release Compose configurations use `/readyz` for the
application container healthcheck. A temporary provider outage therefore does
not restart or mark the entire gateway unavailable.

## Downstream Codex CLI

After connecting and testing a Codex OAuth provider account, enable the models
that account can serve and create a client key on the API Keys page. Configure
the downstream Codex CLI with an environment-backed key:

```bash
export N2API_API_KEY="the client key created by N2API"
```

```toml
[model_providers.n2api]
name = "N2API"
base_url = "http://127.0.0.1:3000/v1"
env_key = "N2API_API_KEY"
wire_api = "responses"

[profiles.n2api]
model_provider = "n2api"
model = "gpt-5.4-mini"
```

Use `codex -p n2api`. Replace the base URL when Codex runs on another machine.
Verify `GET /v1/models` with the client key before troubleshooting model
requests; the list reflects the key's routing-pool scope and model policy,
account model capability, enabled state, and account health. A key without a
routing pool returns no models.

## Provider Accounts

Start the stack, log in as admin, and use Provider accounts to connect one or more Codex OAuth accounts or API-key upstream accounts. Provider accounts are gateway exits. N2API supports Codex OAuth accounts and API-key upstream accounts. Both account types share enabled state, priority, health status, and per-account model lists.

The default OAuth flow uses the Codex-compatible OpenAI OAuth client with PKCE, so the OAuth client id, client secret, auth URL, and token URL can usually stay blank in `.env`.
Keep the default `OPENAI_OAUTH_REDIRECT_URL=http://localhost:1455/auth/callback` unless you are using your own registered OpenAI OAuth client. The built-in Codex-compatible client expects that local callback URI; after OpenAI redirects there, copy the browser URL back into N2API's callback field.

- Use the account row to set a display name, priority, and load factor. OAuth account creation also lets you choose whether the account should be enabled after login.
- Select rows on the Provider accounts page to bulk enable or disable provider accounts. Use **Enable selected** or **Disable selected** to change scheduling eligibility for the selected exits, and **Clear selection** to discard the selection without changing accounts.
- Set **Bulk priority**, **Bulk load factor**, or **Bulk max concurrency**, then use **Apply scheduling** to update selected provider accounts together; bulk priority, bulk load factor, and bulk max concurrency fields use the same validation as each account row.
- Configure supported models on each connected account. These per-account model rows describe account capability for gateway routing.
- Use the Providers table **Test models** action to diagnose one or more configured models against one exact account. The modal starts with no selection; row checkboxes support one or multiple models, and the tri-state header checkbox selects only the current filtered result set. Tests use that account's stored OAuth token or API-upstream key, never fall back to another account, and persist each model's latest status and latency. They do not enable or disable models or change account scheduling health.
- Selected provider accounts can also receive the same model capability list. Enter one model per line in **Bulk models**, then use **Apply models** to replace the selected accounts' manual model lists together; this controls which models the scheduler can route to those accounts.
- Use the Provider accounts page to add or remove selected provider accounts from a routing pool without opening the pool editor. Choose **Bulk routing pool**, set **Pool priority** for new pool members, then use **Apply pool** to add the selected accounts to that pool or **Remove pool** to remove them while leaving the pool's other members unchanged.
- Use API Keys to control the routing-pool scope and `all` or `selected` model policy. Account model configuration remains the source of truth for capability. The gateway `defaultModel` is used only when a POST request omits `model`, and the injected model must still be routable in that key's bound pool or explicit fallback chain.
- Client API keys have no model access until they are bound to a routing pool. Within the pool's explicit fallback chain, `all` keys see every routable configured model and `selected` keys see only the routable intersection of their selected models.
- Routing pools partition provider accounts for different agents, devices, or risk profiles. A key without a routing pool returns no models and cannot route model requests. A bound key only schedules accounts in its pool and explicit fallback chain, including sticky session bindings scoped to that chain. Pool membership priority is evaluated before the account's global priority inside that pool, so the same provider account can be ranked differently for different client keys. Missing or deleted pools fail closed with `routing_pool_unavailable`, empty enabled pools fail closed with `routing_pool_empty`, and Request Logs retain the routing pool name/id for attribution.
- The API Keys page supports local search and status filtering by name, prefix, model policy, selected model, active/disabled/deleted status, and limiter state, so a busy or deleted client key can be found without leaving the page.
- API key names can be renamed from the API Keys page without rotating the secret, so labels can be kept in sync with devices, agents, or usage purpose.
- New API keys are stored with an encrypted reusable secret. The Prefix column on the API Keys page can copy the full API key again after creation for active or disabled keys; older keys created before encrypted secret storage may need to be rotated if their full value was not saved.
- API keys have three visible states: active, disabled, and deleted. Active and disabled keys can be toggled directly from the API Keys table status column, and disabled keys cannot authenticate gateway requests. Deleting an active or disabled key performs an irreversible logical delete immediately, keeps the row visible during its 7 day retention window, and exposes the scheduled physical deletion time in the deleted status tooltip. Deleted keys can be physically deleted immediately with a second confirmed Delete action. Keys past the retention window are physically removed by startup and hourly cleanup, with API key listing cleanup as a fallback.
- API key budgets are personal operational safeguards, not billing balances. Each key can have request, token, and estimated cost budgets over rolling 24h and 30d windows; cost budgets use stored estimated request cost, and `0` disables a budget field. When a key is over budget, clients receive OpenAI-compatible `rate_limit_exceeded` responses while Request Logs store the precise local reason as `api_key_request_budget_exceeded`, `api_key_token_budget_exceeded`, or `api_key_cost_budget_exceeded`.
- Use **Refresh** to force a token refresh for one account and clear stale transient state after a successful refresh.
- Use **Reauthorize** on an existing row to bind a fresh OAuth login back to that account instead of creating a second row.
- API upstream credentials can be updated from the account row. Rotating the encrypted API key, base URL, or per-account outbound proxy URL clears local failure status so the account can be scheduled again with the new upstream settings. Proxy URLs are stored encrypted because they may include credentials, and the admin UI only shows a redacted proxy summary.
- New OAuth and API upstream account forms can bind a **Fingerprint profile** at creation time. OAuth profile selections are stored in the pending OAuth state and applied after callback completion; API upstream selections are written directly to the provider account.
- Use **Test account** before sending client traffic through an account. The action probes one provider account with its current OAuth token or API upstream key, clears local failure status on success, and records upstream failure status for 401/403/429/5xx probe responses. The account row keeps the last test status, last test time, and last test error so manual checks remain visible after refresh. Each probe also writes provider account test history; use the Providers page **History action** to expand **Recent test history**, or fetch the same data from `GET /api/admin/provider-accounts/{id}/test-results`.
- The Ops Monitor page shows **Recent account tests** for the selected monitoring window so manual and automatic probe failures are visible without opening each provider account row. Fetch the same aggregate view from `GET /api/admin/ops/account-tests`.
- Use **Test selected** to probe selected provider accounts without probing the whole account pool. It updates the same last-test fields, health fields, and test history as **Test account**.
- Use **Refresh selected** to force credential refresh for selected provider accounts together after rotating, restoring, or reauthorizing a subset of OAuth-backed exits.
- Use **Disconnect account** when an exit should be removed from the gateway. It deletes the provider account, stops scheduling it for new traffic, and removes its stored credentials and account-scoped model configuration through the database cascade.
- Use **Disconnect selected** when several exits should be removed together. It deletes the selected provider accounts, stops scheduling them for new traffic, and removes their stored credentials and account-scoped model configuration through the same database cascade.
- Provider account auto tests are disabled by default. `N2API_PROVIDER_ACCOUNT_AUTO_TEST_ENABLED` and `N2API_PROVIDER_ACCOUNT_AUTO_TEST_INTERVAL_SECONDS` are startup defaults for Gateway Settings; after sign-in, use the Gateway Settings form to save the runtime auto-test setting. Enable it to run **Test all accounts** automatically in the backend, and use an interval of `300` seconds or higher for routine checks. Automatic tests update the same last test status, last test time, last test error, test history, and local account health fields shown in Provider accounts and Routing diagnostics.
- Gateway Settings also shows **Auto-test status** for the in-memory runner. The status row reports whether the runner is active, the last finished time, accounts tested in the last cycle, and the last error when a scheduled probe fails.
- Gateway Settings includes **Request log retention** for bounded request-log cleanup. A value of 0 disables both manual and automatic deletion. Manual and automatic runs delete logs older than the saved retention window. The status panel reports the current UTC cutoff, exact eligible row count, estimated total row count, oldest/newest timestamps, automatic runner state, and the latest automatic outcome. **Clean request logs** remains available for an immediate manual run and is disabled while an automatic run owns the cleanup lock.
- Automatic Request Log cleanup has a separate startup gate, `N2API_REQUEST_LOG_RETENTION_RUNNER_ENABLED`, which defaults to `false`. A historical positive retention value therefore does not silently become automatic after an upgrade. When enabled, the runner executes once at startup and then every `N2API_REQUEST_LOG_RETENTION_INTERVAL_SECONDS` seconds, deleting at most `N2API_REQUEST_LOG_RETENTION_BATCH_SIZE` rows per transaction. Valid intervals are `300`-`604800` seconds and valid batch sizes are `100`-`10000` rows.
- Before enabling automatic Request Log deletion on a long-running instance, restore a current backup successfully in an isolated environment and review the displayed cutoff and eligible count. Keep the runner gate disabled if either check is incomplete. Automatic and manual runs use the same PostgreSQL advisory lock and committed batches are not rolled back if a later batch is canceled or fails.
- Use **Pause scheduling** when a healthy account should stop receiving traffic for a short window. Set **Pause duration seconds** on the Provider accounts page before clicking the action; it temporarily opens the account circuit for that window without disabling or deleting the account. Paused and rate-limited rows show the remaining scheduling block in the status column. Use **Reset local status** to clear the pause early.
- Selected provider accounts can be paused and reset together. Use **Pause selected** to apply the configured **Pause duration seconds** to every selected account, or **Reset selected** to clear local rate-limit, circuit-open, and error status for the selected accounts after recovery.
- Disabled accounts are kept in PostgreSQL but are not selected for gateway traffic.
- Connected accounts with no configured models are kept in PostgreSQL and can be edited later, but they do not receive model-routed POST traffic.
- Provider-account model configuration is the source of truth for model capability. Accounts with no configured models remain editable but cannot serve model-routed requests.
- Lower account priority numbers are selected before higher account priority numbers. Inside a routing pool, lower pool membership priority is evaluated first, followed by the account's global priority.
- Within the same priority scope, load factor is a strict descending preference tier, not a proportional request weight. A continuously eligible account in a higher load-factor tier is considered before every account in a lower tier and can therefore receive every new non-sticky selection. Keep weak or quota-sensitive accounts at load factor `1`; raise the value only when that strict preference is intended.
- Within an exactly equal priority and load-factor tier, accounts without a recent error are considered before accounts with one, followed by least-recently-used time and account ID. A stored sticky binding is reused while its account remains schedulable. For a new session, sticky FNV hashing only changes order inside the highest tier with exactly equal pool priority (when scoped), global account priority, load factor, and recent-error state; it never promotes a lower load-factor tier.
- Provider accounts expose **Max concurrency** as a per-account concurrency override. `0` inherits the gateway default from Gateway Settings; positive values cap that account independently. Each account row also shows active concurrency as a process-local runtime snapshot beside the effective cap; a cap of `0` is shown as unlimited, and the active count resets when the backend process restarts.
- Rate-limited, circuit-open, expired, and disabled accounts are skipped during gateway account selection.
- Upstream 429 responses mark the account as rate-limited, 401/403 mark it expired, and 5xx responses open a short circuit window before traffic tries another account.
- `/v1/models` returns `200` with an empty model list for an unbound API key. A model-routed POST made with that key fails closed with `503` and the error code `routing_pool_required`. For a bound key, `all` exposes every currently routable configured model in the routing-pool fallback chain, while `selected` exposes the routable intersection of the key's selected models.
- `/v1/models` never widens beyond the key's configured routing-pool fallback chain.
- Routing diagnostics can preview scheduler fallback without sending traffic. In Selection preview, choose a **Routing pool** to inspect its bound routing path. Leaving the pool unset runs an unscoped admin diagnostic across provider accounts; it does not represent access available to an unbound API key. Set **Excluded account IDs** to a comma-separated list such as `7, 8` to simulate those provider accounts being unavailable; excluded accounts remain visible as blocked candidates with the reason `account excluded`. Routing preview also shows each candidate's active concurrency and effective account cap; candidates at a positive cap are marked **Concurrency full**. Each schedulable preview candidate includes a **Schedule reason** as diagnostic text with its pool/global priority tier, load-factor tier, recent-error tier, least-recently-used tie-breaker, account ID, and sticky decision; this explains the current rank and does not change scheduler behavior. Runtime concurrency is not a proportional weight: concurrency-full accounts are excluded and the gateway requests another selection, returning `429` only when no eligible account can accept the request.
- If one enabled account cannot refresh a token or fails before streaming starts, N2API tries another eligible account that supports the same requested model.
- Once upstream streaming has started, N2API preserves that stream and does not retry against another account.
- OAuth access tokens, refresh tokens, id tokens, and short-lived PKCE verifier records are encrypted before being stored. Browser/request fingerprints are stored only as hashes.

## Gateway Runtime Limits

Gateway management refreshes provider accounts, model routing, and API keys before reporting readiness, so the counts and prerequisite warnings are valid even when `/gateway` is opened directly.

Gateway management also includes **Scheduling health**, which summarizes enabled, schedulable, and blocked provider accounts; **Blocked reasons** groups disabled, expired, rate-limited, and circuit-open exits so account-pool pressure is visible without opening the full Provider accounts table.

The deployment template includes optional in-process gateway guards:

- `N2API_GATEWAY_MAX_CONCURRENT_REQUESTS` limits total active gateway requests.
- `N2API_GATEWAY_MAX_CONCURRENT_REQUESTS_PER_ACCOUNT` limits active requests per provider account.
- `N2API_GATEWAY_MAX_CONCURRENT_REQUESTS_PER_KEY` limits active requests per client API key.
- `N2API_GATEWAY_REQUESTS_PER_MINUTE_PER_KEY` limits accepted requests per client API key per fixed minute.
- `N2API_GATEWAY_TOKENS_PER_MINUTE_PER_KEY` limits observed request tokens per client API key per fixed minute.

Set any gateway default value to `0` to disable that guard. These limits are process-local; keep them conservative on a single-node VPS and add shared infrastructure later if you need multi-instance coordination. The API Keys page shows the values loaded by the running service. Per-key values set to `0` inherit the matching gateway default and do not disable that guard for only one key. The API Keys page shows active concurrency for each client key as process-local runtime state; keys at a positive effective cap are marked **Concurrency full**. It also shows **Requests window** and **Tokens window** for each key as process-local fixed one-minute counters with remaining capacity; limited windows at capacity are marked **Request limit full** or **Token limit full**, and the counters reset on the next fixed minute or backend restart. Local per-key request/token 429 responses include `Retry-After`; per-account concurrency skips busy accounts when another eligible account is available and returns 429 only when no eligible account can accept the request.

Request Logs keep local gateway rejections diagnosable while client responses stay OpenAI-compatible. Local limit responses still return `rate_limit_exceeded` to clients, but the stored request-log error identifies the guard as `api_key_request_rate_limited`, `api_key_token_rate_limited`, `api_key_request_budget_exceeded`, `api_key_token_budget_exceeded`, `api_key_cost_budget_exceeded`, `gateway_concurrency_limited`, `api_key_concurrency_limited`, or `provider_account_concurrency_limited`.

Request Logs also include gateway fallback diagnostics: attempts count selected provider-account tries, and fallbacks count pre-stream scheduler moves caused by busy accounts or retryable upstream failures.

Routing pool fallback is explicit. The routing pool fallback chain can point to one fallback pool at each step, forming a simple chain such as `primary -> secondary`. A key tries only its configured pool and that explicit chain; there is no implicit fallback outside the chain. A disabled primary pool fails closed with `routing_pool_disabled`, an empty primary pool fails closed with `routing_pool_empty`, cycles fail closed with `routing_pool_cycle`, and exhausted chains are logged as `routing_pool_exhausted`.

Request Logs support exact **Provider account**, **Routing pool**, **API key**, **Model filter**, **Usage source**, and **Session filter** fields. On Gateway management and Dashboard, 24h usage rows for **Top provider accounts**, **Top usage sources**, **Top routing pools**, **Top routing pool chains**, **Top client keys**, **Top models**, and **Top sessions** link to Request Logs with exact provider-account, usage-source, routing-pool, routing-pool-chain, API-key, model, and sticky-session filters when the row identifies a concrete entity.

The authenticated `GET /api/admin/request-logs` endpoint returns `logs`, `hasMore`, and `nextCursor`. When `hasMore` is true, pass the opaque `nextCursor` value back as `cursor` while keeping every filter unchanged to load older rows. Cursors are signed and filter-bound but not encrypted; clients must not parse or modify them. Restart from the first page when the API returns `400 invalid_input`, such as after a cursor is changed, a filter is changed, or the server encryption secret is rotated. Page size may change between requests without invalidating a cursor.

The Request Logs page keeps active filters in the URL and loads 50 rows at a time. Use **Load older** to append the next cursor page without replacing rows already under review. Applying or clearing filters starts a fresh first page; if a saved cursor is no longer valid, the page also recovers by restarting from the current URL-backed filters.

Request Log CSV and JSONL exports stream the currently applied filters over an explicit half-open UTC range (`since <= created_at < before`) and do not accumulate the result in application memory. Select a bounded date range before using those formats. Each export writes at most `N2API_REQUEST_LOG_EXPORT_MAX_ROWS` rows and runs for at most `N2API_REQUEST_LOG_EXPORT_TIMEOUT_SECONDS`; defaults are 100000 rows and 60 seconds, with valid ranges of 1000-1000000 rows and 5-300 seconds. Reaching the row limit produces a valid, explicitly truncated download and records that outcome in the export event. CSV and JSONL also offer gzip downloads. CSV text cells beginning with a spreadsheet formula marker are escaped before CSV encoding. The compatibility JSON download remains limited to 200 rows.

API upstream accounts and `OPENAI_API_BASE_URL` require HTTPS by default so
upstream API keys are not sent over plaintext HTTP. Set
`N2API_ALLOW_HTTP_API_UPSTREAMS=true` only for trusted local or private HTTP
upstreams that you control. This setting does not permit HTTP OpenAI OAuth
authorization or token endpoints.

For sticky session routing, clients can send `session_id` in the POST body. If a client needs a header instead, prefer `X-N2API-Session-ID` through reverse proxies; `session_id` remains supported but contains an underscore and may be dropped by default proxy settings. If N2API is behind Nginx and clients send the `session_id` header, set `underscores_in_headers on;` in the relevant `http` or `server` block. A body `session_id` overrides either header.

Sticky session bindings are persisted by provider, model, and `session_id`. A healthy bound account is reused while it remains schedulable; if fallback excludes it before streaming starts, the successful fallback account can rebind that session.

## Gateway Compatibility Matrix

The secret-free E2E stack validates the public gateway through raw HTTP and
the pinned official OpenAI JavaScript and Python SDKs. All runtime services use
an internal Compose network, and the SDK runners reject any gateway hostname
other than `n2api` before creating a client.

| Contract | Automated evidence |
| --- | --- |
| Models, Chat Completions JSON, and Responses SSE | PostgreSQL-backed Go E2E plus `openai` JavaScript `6.48.0` and Python `2.46.0` runners |
| Upstream 401, 403, 429, 500, and 503 | Go E2E verifies the client status/error contract and persisted attempt/fallback attribution |
| Missing or incorrect content type | Go E2E verifies bounded pass-through behavior without inventing upstream metadata |
| Missing or malformed usage | Go E2E verifies Chat JSON and Responses SSE remain usable while Request Logs retain zero tokens and zero estimated cost instead of fabricating usage |
| Timeout or disconnect before response headers | Go E2E verifies cancellation and pre-stream fallback without leaking response bodies |
| Disconnect or clean EOF after the first SSE event | Go E2E verifies N2API never retries after streaming has begun and never fabricates a completion event |
| Sticky routing, explicit fallback, and local key limits | Go E2E verifies persisted account selection, fallback counters, and OpenAI-compatible 429 responses |
| Concurrent OAuth refresh single-flight | Deterministic provider component tests verify one refresh and reuse of the rotated token |
| Real Codex OAuth and Codex CLI | Manual protected acceptance only; it is not a required PR check and must not upload request or response bodies |

Run the SDK contracts locally after starting their isolated dependencies:

```bash
docker compose --project-name n2api-sdk-contracts-local \
  -f deploy/compose.e2e.yaml \
  up -d --build --wait postgres mock-openai n2api
docker compose --project-name n2api-sdk-contracts-local \
  --profile contracts -f deploy/compose.e2e.yaml \
  run --rm --build --no-deps contracts-javascript
docker compose --project-name n2api-sdk-contracts-local \
  --profile contracts -f deploy/compose.e2e.yaml \
  run --rm --build --no-deps contracts-python
docker compose --project-name n2api-sdk-contracts-local \
  -f deploy/compose.e2e.yaml \
  down --volumes --remove-orphans --timeout 10
```

Each runner creates its own account, routing pool, and client key through the
admin API, keeps generated secrets in memory only, and performs best-effort API
cleanup. Removing the isolated PostgreSQL volume is the final cleanup boundary.

### Request Log Query Profile

Run the opt-in Request Log profile against a disposable PostgreSQL database
after materially changing filters, retention queries, data distribution, or
the PostgreSQL major version. The database role must be allowed to create and
drop schemas. The test creates a uniquely named schema, loads one million
synthetic rows, reports `EXPLAIN (ANALYZE, BUFFERS)` and index/write metrics,
and drops only that schema during cleanup.

```bash
cd backend
GOCACHE=/tmp/n2api-request-log-profile-go-build \
N2API_REQUEST_LOG_QUERY_PROFILE=1 \
N2API_STORE_TEST_DATABASE_URL='postgres://USER:PASSWORD@HOST:5432/DISPOSABLE_DATABASE?sslmode=require' \
go test -count=1 -run TestRequestLogQueryProfile -v ./internal/store
```

Do not treat one profile run as a fixed latency target. Compare plan shape,
temporary I/O, buffer activity, total index bytes, and the write probe before
accepting an index change.

Before upgrading an existing deployment, back up PostgreSQL because the upgrade adds unified provider account tables and client API key model-policy metadata.

## Required Services

- `n2api`: Go application service.
- `postgres`: PostgreSQL database with a persistent Docker volume.

Redis is intentionally not required for V1. Add it later only if distributed rate limiting, queueing, or multi-instance locking becomes necessary.

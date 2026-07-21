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
printf '\nN2API_IMAGE=ghcr.io/knowsky404/n2api:%s\n' "$N2API_VERSION" >> .env
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

The release Compose file requires `.env`, rejects missing required variables at
Compose interpolation time, and publishes N2API on `127.0.0.1` by default. Set
`N2API_BIND_ADDRESS=0.0.0.0` or `::` only when an intentionally public host
listener is protected by the host firewall or an operator-provided ingress.

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
`pg_restore --list backups/n2api-YYYYMMDD-HHMMSS.dump`.

For an upgrade or rollback, change `N2API_IMAGE` to the target CalVer, then pull
and recreate the stack:

```bash
docker compose -f deploy/compose.release.yaml --env-file .env pull
docker compose -f deploy/compose.release.yaml --env-file .env up -d
curl -fsS http://127.0.0.1:3000/readyz
```

Use `latest` only when automatic movement to the newest stable release is
intentional. Use `main` only for development validation, not production.

## Health Probes

N2API exposes separate process and dependency probes:

- `GET /livez` reports only that the HTTP process can respond. It does not
  check PostgreSQL or provider accounts.
- `GET /readyz` reports ready only when PostgreSQL responds and the static admin
  build contains its application entry document. Migrations, administrator
  bootstrap, and background runner construction finish before the HTTP server
  starts listening. Provider account availability does not affect readiness.
- `GET /healthz` remains a compatibility alias for the liveness behavior.
- `GET /api/admin/health` is the existing database-focused status response used
  by the admin UI; it will gain richer authenticated operational detail in a
  later reliability phase.

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
- Gateway Settings includes **Request log retention** for manual request-log cleanup. A value of 0 disables cleanup. Set a positive number of days, save Gateway Settings, then use **Clean request logs** to delete request logs older than the saved retention window.
- Use **Pause scheduling** when a healthy account should stop receiving traffic for a short window. Set **Pause duration seconds** on the Provider accounts page before clicking the action; it temporarily opens the account circuit for that window without disabling or deleting the account. Paused and rate-limited rows show the remaining scheduling block in the status column. Use **Reset local status** to clear the pause early.
- Selected provider accounts can be paused and reset together. Use **Pause selected** to apply the configured **Pause duration seconds** to every selected account, or **Reset selected** to clear local rate-limit, circuit-open, and error status for the selected accounts after recovery.
- Disabled accounts are kept in PostgreSQL but are not selected for gateway traffic.
- Connected accounts with no configured models are kept in PostgreSQL and can be edited later, but they do not receive model-routed POST traffic.
- Provider-account model configuration is the source of truth for model capability. Accounts with no configured models remain editable but cannot serve model-routed requests.
- Lower priority numbers are selected before higher priority numbers.
- Within the same priority and health class, a higher load factor is considered before a lower load factor. Keep weak or quota-sensitive accounts at load factor `1`; raise stronger accounts when they should carry more traffic.
- Provider accounts expose **Max concurrency** as a per-account concurrency override. `0` inherits the gateway default from Gateway Settings; positive values cap that account independently. Each account row also shows active concurrency as a process-local runtime snapshot beside the effective cap; a cap of `0` is shown as unlimited, and the active count resets when the backend process restarts.
- Rate-limited, circuit-open, expired, and disabled accounts are skipped during gateway account selection.
- Upstream 429 responses mark the account as rate-limited, 401/403 mark it expired, and 5xx responses open a short circuit window before traffic tries another account.
- `/v1/models` returns `200` with an empty model list for an unbound API key. A model-routed POST made with that key fails closed with `503` and the error code `routing_pool_required`. For a bound key, `all` exposes every currently routable configured model in the routing-pool fallback chain, while `selected` exposes the routable intersection of the key's selected models.
- `/v1/models` never widens beyond the key's configured routing-pool fallback chain.
- Routing diagnostics can preview scheduler fallback without sending traffic. In Selection preview, choose a **Routing pool** to inspect its bound routing path. Leaving the pool unset runs an unscoped admin diagnostic across provider accounts; it does not represent access available to an unbound API key. Set **Excluded account IDs** to a comma-separated list such as `7, 8` to simulate those provider accounts being unavailable; excluded accounts remain visible as blocked candidates with the reason `account excluded`. Routing preview also shows each candidate's active concurrency and effective account cap; candidates at a positive cap are marked **Concurrency full**. Each schedulable preview candidate includes a **Schedule reason** as diagnostic text, such as sticky session binding or priority/load/least-recently-used order; this explains the current rank and does not change scheduler behavior.
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

Before upgrading an existing deployment, back up PostgreSQL because the upgrade adds unified provider account tables and client API key model-policy metadata.

## Required Services

- `n2api`: Go application service.
- `postgres`: PostgreSQL database with a persistent Docker volume.

Redis is intentionally not required for V1. Add it later only if distributed rate limiting, queueing, or multi-instance locking becomes necessary.

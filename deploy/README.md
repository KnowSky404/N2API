# N2API Deployment

The default deployment target is Docker Compose on a small VPS.

## Start Locally

From the repository root:

```bash
cp .env.example .env
docker compose -f deploy/compose.yaml --env-file .env up --build
```

The default app URL is `http://localhost:3000`.

## Published Images

The `CI Image` workflow tests every pull request without publishing an image.
After a commit reaches `main`, the same image that passed the PostgreSQL smoke
test is published with two development tags:

- `main` moves to the newest tested commit on the default branch.
- `sha-<12 characters>` identifies one tested source commit and is immutable.

Stable releases add two more tags without rebuilding the image:

- `YYYYMMDDNN` is an immutable Europe/Berlin CalVer release, for example
  `2026071401`.
- `latest` moves only when a stable GitHub Release is published.

The Git tag, GitHub Release tag, and container version tag always use the same
CalVer value without a `v` prefix.

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

## Deploy a Published Image

Copy the example environment file and pin an immutable version in `.env`:

```bash
cp .env.example .env
printf '\nN2API_IMAGE=ghcr.io/knowsky404/n2api:2026071401\n' >> .env
docker compose -f deploy/compose.release.yaml --env-file .env pull
docker compose -f deploy/compose.release.yaml --env-file .env up -d
curl -fsS http://127.0.0.1:3000/healthz
```

Back up PostgreSQL before upgrading. For rollback, change `N2API_IMAGE` to the
previous CalVer, then pull and recreate the stack:

```bash
docker compose -f deploy/compose.release.yaml --env-file .env pull
docker compose -f deploy/compose.release.yaml --env-file .env up -d
curl -fsS http://127.0.0.1:3000/healthz
```

Use `latest` only when automatic movement to the newest stable release is
intentional. Use `main` only for development validation, not production.

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
requests; the list reflects global model policy, per-key policy, account model
capability, routing-pool scope, enabled state, and account health.

## Provider Accounts

Start the stack, log in as admin, and use Provider accounts to connect one or more Codex OAuth accounts or API-key upstream accounts. Provider accounts are gateway exits. N2API supports Codex OAuth accounts and API-key upstream accounts. Both account types share enabled state, priority, health status, and per-account model lists.

The default OAuth flow uses the Codex-compatible OpenAI OAuth client with PKCE, so the OAuth client id, client secret, auth URL, and token URL can usually stay blank in `.env`.
Keep the default `OPENAI_OAUTH_REDIRECT_URL=http://localhost:1455/auth/callback` unless you are using your own registered OpenAI OAuth client. The built-in Codex-compatible client expects that local callback URI; after OpenAI redirects there, copy the browser URL back into N2API's callback field.

- Use the account row to set a display name, priority, and load factor. OAuth account creation also lets you choose whether the account should be enabled after login.
- Select rows on the Provider accounts page to bulk enable or disable provider accounts. Use **Enable selected** or **Disable selected** to change scheduling eligibility for the selected exits, and **Clear selection** to discard the selection without changing accounts.
- Set **Bulk priority**, **Bulk load factor**, or **Bulk max concurrency**, then use **Apply scheduling** to update selected provider accounts together; bulk priority, bulk load factor, and bulk max concurrency fields use the same validation as each account row.
- Configure supported models on each connected account. These per-account model rows describe account capability for gateway routing.
- Selected provider accounts can also receive the same model capability list. Enter one model per line in **Bulk models**, then use **Apply models** to replace the selected accounts' manual model lists together; this controls which models the scheduler can route to those accounts.
- Use the Provider accounts page to add or remove selected provider accounts from a routing pool without opening the pool editor. Choose **Bulk routing pool**, set **Pool priority** for new pool members, then use **Apply pool** to add the selected accounts to that pool or **Remove pool** to remove them while leaving the pool's other members unchanged.
- Use API Keys to control which configured models are exposed to clients and which model is used as the default when a POST request omits `model`. Global routable model settings do not make an account eligible for a model it has not configured.
- Client API keys default to all routable models. For narrower access, set a key to selected models on the API Keys page. A selected model must still have at least one enabled healthy provider account before the gateway can route requests to it.
- Routing pools let the admin partition provider accounts into named account pools for different agents, devices, or risk profiles. An API key can be bound to one routing pool from the API Keys page. A pool-bound key only schedules accounts that are members of that pool, including sticky session bindings scoped to that pool; an unbound key keeps using the global provider account pool. Pool membership priority is evaluated before the account's global priority inside that pool, so the same provider account can be ranked differently for different client keys. Missing or deleted pools fail closed with `routing_pool_unavailable`, empty enabled pools fail closed with `routing_pool_empty`, and Request Logs retain the routing pool name/id for attribution.
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
- During migration, an install with a single connected provider account and no account-specific models backfills that account from the global allowed model list. Installs with multiple provider accounts keep models manual so routing does not assume false account capability.
- Lower priority numbers are selected before higher priority numbers.
- Within the same priority and health class, a higher load factor is considered before a lower load factor. Keep weak or quota-sensitive accounts at load factor `1`; raise stronger accounts when they should carry more traffic.
- Provider accounts expose **Max concurrency** as a per-account concurrency override. `0` inherits the gateway default from Gateway Settings; positive values cap that account independently. Each account row also shows active concurrency as a process-local runtime snapshot beside the effective cap; a cap of `0` is shown as unlimited, and the active count resets when the backend process restarts.
- Rate-limited, circuit-open, expired, and disabled accounts are skipped during gateway account selection.
- Upstream 429 responses mark the account as rate-limited, 401/403 mark it expired, and 5xx responses open a short circuit window before traffic tries another account.
- `/v1/models` returns the aggregate exposed models for the authenticated API key. All-model keys see the routable list after applying the global allowed-model list; selected-model keys see the intersection of their selected models and currently routable models.
- For a pool-bound API key, `/v1/models` is also limited to the configured routing pool fallback chain. Models that exist only in the global provider account pool are hidden from that key.
- Routing diagnostics can preview scheduler fallback without sending traffic. In Selection preview, set **Routing pool** to preview a pool-bound key path or leave it on the global provider pool. Set **Excluded account IDs** to a comma-separated list such as `7, 8` to simulate those provider accounts being unavailable; excluded accounts remain visible as blocked candidates with the reason `account excluded`. Routing preview also shows each candidate's active concurrency and effective account cap; candidates at a positive cap are marked **Concurrency full**. Each schedulable preview candidate includes a **Schedule reason** as diagnostic text, such as sticky session binding or priority/load/least-recently-used order; this explains the current rank and does not change scheduler behavior.
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

Routing pool fallback is explicit. The routing pool fallback chain can point to one fallback pool at each step, forming a simple chain such as `primary -> secondary`. A pool-bound key never falls back to the global provider account pool; it tries only its configured pool and that explicit chain. A disabled primary pool fails closed with `routing_pool_disabled`, an empty primary pool fails closed with `routing_pool_empty`, cycles fail closed with `routing_pool_cycle`, and exhausted chains are logged as `routing_pool_exhausted`.

Request Logs support exact **Provider account**, **Routing pool**, **API key**, **Model filter**, **Usage source**, and **Session filter** fields. On Gateway management and Dashboard, 24h usage rows for **Top provider accounts**, **Top usage sources**, **Top routing pools**, **Top routing pool chains**, **Top client keys**, **Top models**, and **Top sessions** link to Request Logs with exact provider-account, usage-source, routing-pool, routing-pool-chain, API-key, model, and sticky-session filters when the row identifies a concrete entity.

API upstream accounts require HTTPS by default so upstream API keys are not sent over plaintext HTTP. Set `N2API_ALLOW_HTTP_API_UPSTREAMS=true` only for trusted local or private HTTP upstreams that you control.

For sticky session routing, clients can send `session_id` in the POST body. If a client needs a header instead, prefer `X-N2API-Session-ID` through reverse proxies; `session_id` remains supported but contains an underscore and may be dropped by default proxy settings. If N2API is behind Nginx and clients send the `session_id` header, set `underscores_in_headers on;` in the relevant `http` or `server` block. A body `session_id` overrides either header.

Sticky session bindings are persisted by provider, model, and `session_id`. A healthy bound account is reused while it remains schedulable; if fallback excludes it before streaming starts, the successful fallback account can rebind that session.

Before upgrading an existing deployment, back up PostgreSQL because the upgrade adds unified provider account tables and client API key model-policy metadata.

## Required Services

- `n2api`: Go application service.
- `postgres`: PostgreSQL database with a persistent Docker volume.

Redis is intentionally not required for V1. Add it later only if distributed rate limiting, queueing, or multi-instance locking becomes necessary.

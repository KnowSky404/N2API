<div align="center">
  <img src="frontend/static/n2api-logo.svg" alt="N2API logo" width="112" height="112" />
  <h1>N2API</h1>
  <p>Personal AI API/account gateway for self-hosted use.</p>
</div>

N2API is a personal AI API/account gateway for self-hosted use. It is inspired by sub2api's practical workflow, but it is a new implementation focused on personal use rather than platform billing or merchant operations.

Brand assets, original generated concepts, exact prompts, and export metadata are documented in [`docs/brand`](docs/brand/README.md).

## V1 Direction

- Go backend service.
- PostgreSQL as the default persistent database.
- Bun + SvelteKit + Tailwind CSS admin UI.
- Docker Compose first for local development and VPS deployment.
- Codex/OpenAI OAuth is the first upstream provider.
- OpenAI-compatible API routes and Codex-oriented adapter behavior share one internal gateway pipeline.

## Non-Goals

- No sub2api source-code fork or copy.
- No public registration, payment, recharge, balance, sponsor, invoice, merchant, or platform billing features.
- No Redis requirement in V1. Redis can be added later for optional distributed rate limiting, queues, or locks.
- No SQLite implementation in V1.

## Repository Layout

```text
backend/   Go service entrypoint and future gateway/provider/store code
frontend/  Bun-managed SvelteKit admin UI
deploy/    Docker Compose and deployment notes
docs/      Design specs and implementation plans
```

## Local Development

Copy the environment template:

```bash
cp .env.example .env
```

Edit `.env` and set real secrets before running the full stack.
For OpenAI/Codex OAuth, the default configuration uses the Codex-compatible OpenAI OAuth client with PKCE, so you normally do not need to create an OAuth app or set `OPENAI_OAUTH_CLIENT_SECRET`.
`OPENAI_API_BASE_URL` defaults to `https://api.openai.com` and can be changed for compatible upstreams.
API upstream accounts require HTTPS by default. Set `N2API_ALLOW_HTTP_API_UPSTREAMS=true` only when you intentionally route to a trusted local or private HTTP upstream.

Backend:

```bash
cd backend
go test ./...
go run ./cmd/n2api
```

Frontend:

```bash
cd frontend
bun install
bun test
bun run check
bun run build
```

Docker Compose:

```bash
docker compose -f deploy/compose.yaml --env-file .env up --build
```

After the stack is up, open `http://localhost:3000`, sign in with the admin credentials from `.env`, and use **Provider accounts** to add Codex/OpenAI OAuth accounts or API-key upstream accounts. Adding or reauthorizing an OAuth account generates an authorization link; open it manually, finish the provider login, then paste the resulting callback URL back into the admin UI to complete the account connection.

## Downstream Codex CLI

Create a client key on the **API Keys** page after at least one enabled provider
account has the requested model enabled. Keep the client key outside
`config.toml` and expose it to Codex through an environment variable:

```bash
export N2API_API_KEY="the client key created by N2API"
```

Add a user-level Codex provider. Use the N2API address that is reachable from
the downstream machine; this local example uses the Compose port directly:

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

Run Codex with the profile:

```bash
codex -p n2api
```

The configured model must be allowed globally, visible to the client key, and
enabled on at least one schedulable provider account in the key's routing pool.
Codex OAuth exits use the Responses API path; `/v1/chat/completions` is provided
for API-key-compatible upstream accounts and is not the Codex OAuth transport.

## Provider Accounts

Provider accounts are gateway exits. N2API supports Codex OAuth accounts and API-key upstream accounts. Both account types share enabled state, priority, load factor, health status, and per-account model lists.

Select rows on the Provider accounts page to bulk enable or disable provider accounts. Use **Enable selected** or **Disable selected** to change scheduling eligibility for the selected exits, and **Clear selection** when you want to discard the current selection without changing accounts.

Selected rows can also receive shared scheduling parameters. Set **Bulk priority**, **Bulk load factor**, or **Bulk max concurrency**, then use **Apply scheduling** to update those selected provider accounts together; bulk priority, bulk load factor, and bulk max concurrency fields use the same validation as each account row.

Configure model capability on each provider account row. The API Keys page controls the gateway default model, the global routable model list, and client-key model access; these settings do not grant capability to accounts that do not list that model.

Use the Providers table **Test models** action to diagnose configured models against one exact account. The modal starts with no selection and supports one, multiple, or all currently filtered models through row checkboxes and the tri-state header checkbox. Tests use that account's stored OAuth token or API-upstream key, run with bounded concurrency, and never fall back to another account. Latest status, latency, and failure details are persisted per model. Model diagnostics do not enable or disable models and do not change account scheduling health; use **Test account** for account-level health updates.

Selected provider accounts can also receive the same model capability list. Enter one model per line in **Bulk models**, then use **Apply models** to replace the selected accounts' manual model lists together; this controls which models the scheduler can route to those accounts.

Use the Provider accounts page to add or remove selected provider accounts from a routing pool without opening the pool editor. Choose **Bulk routing pool**, set **Pool priority** for new pool members, then use **Apply pool** to add the selected accounts to that pool or **Remove pool** to remove them while leaving the pool's other members unchanged.

API upstream credentials can be updated after account creation. Use the provider account row to rotate the encrypted upstream API key, base URL, or per-account outbound proxy URL; saving new credentials clears local failure status so a previously rate-limited, expired, or circuit-open API upstream can be scheduled again with the new settings. Proxy URLs are stored encrypted because they may include credentials, and the admin UI only shows a redacted proxy summary.

New OAuth and API upstream account forms can bind a **Fingerprint profile** at creation time. OAuth profile selections are stored in the pending OAuth state and applied after callback completion; API upstream selections are written directly to the provider account.

Use **Test account** when you want to probe one provider account before sending client traffic through it. The action probes one provider account with its current OAuth token or API upstream key, clears local failure status on a successful probe, and records upstream failure status for 401/403/429/5xx probe responses. The account row keeps the last test status, last test time, and last test error so manual checks remain visible after refresh. Each probe also writes provider account test history; use the Providers page **History action** to expand **Recent test history**, or fetch the same data from `GET /api/admin/provider-accounts/{id}/test-results`.

The Ops Monitor page shows **Recent account tests** for the selected monitoring window so manual and automatic probe failures are visible without opening each provider account row. Fetch the same aggregate view from `GET /api/admin/ops/account-tests`.

Use **Test selected** to probe selected provider accounts without probing the whole account pool. This is useful after filtering, bulk enabling, or restoring a subset of accounts; it updates the same last-test fields, health fields, and test history as **Test account**.

Use **Refresh selected** to force credential refresh for selected provider accounts together. This is useful after rotating, restoring, or reauthorizing a subset of OAuth-backed exits without refreshing the whole pool.

Use **Disconnect account** when an exit should be removed from the gateway. It deletes the provider account, stops scheduling it for new traffic, and removes its stored credentials and account-scoped model configuration through the database cascade.

Use **Disconnect selected** when several exits should be removed together. It deletes the selected provider accounts, stops scheduling them for new traffic, and removes their stored credentials and account-scoped model configuration through the same database cascade.

Provider account auto tests are disabled by default. `N2API_PROVIDER_ACCOUNT_AUTO_TEST_ENABLED` and `N2API_PROVIDER_ACCOUNT_AUTO_TEST_INTERVAL_SECONDS` are startup defaults for Gateway Settings; after sign-in, use the Gateway Settings form to save the runtime auto-test setting. Enable it to run **Test all accounts** automatically in the backend, and use an interval of `300` seconds or higher for routine checks. Automatic tests update the same last test status, last test time, last test error, test history, and local account health fields shown in Provider accounts and Routing diagnostics.

Gateway Settings also shows **Auto-test status** for the in-memory runner. The status row reports whether the runner is active, the last finished time, accounts tested in the last cycle, and the last error when a scheduled probe fails.

Gateway Settings includes **Request log retention** for manual request-log cleanup. A value of 0 disables cleanup. Set a positive number of days, save Gateway Settings, then use **Clean request logs** to delete request logs older than the saved retention window.

Use **Pause scheduling** when you want a healthy account to stop receiving traffic for a short window. Set **Pause duration seconds** on the Provider accounts page before clicking the action; it temporarily opens the account circuit for that window without disabling or deleting the account. Paused and rate-limited rows show the remaining scheduling block in the status column. Use **Reset local status** to clear the pause early when you want the account to rejoin routing immediately.

Selected provider accounts can be paused and reset together. Use **Pause selected** to apply the configured **Pause duration seconds** to every selected account, or **Reset selected** to clear local rate-limit, circuit-open, and error status for the selected accounts after recovery.

During migration, an install with a single connected provider account and no account-specific models backfills that account from the global allowed model list. Installs with multiple provider accounts keep models manual so the gateway does not assume every account can serve every globally allowed model.

Lower priority numbers are selected first. Within the same priority and health class, accounts with a higher load factor are considered before lower-capacity accounts; keep weak or quota-sensitive accounts at load factor `1` and raise stronger accounts when you want them to carry more traffic. Provider accounts also expose **Max concurrency** for per-account concurrency overrides. A value of `0` inherits the gateway default from Gateway Settings; set a positive value when one account should use its own local concurrency cap. The account row shows active concurrency as a process-local runtime snapshot beside the effective cap; a cap of `0` is shown as unlimited, and the active count resets when the backend process restarts.

## API Key Model Access

Client API keys default to all routable models. For narrower access, set a key to selected models on the API Keys page. A selected model must still have at least one enabled healthy provider account before the gateway can route requests to it. `/v1/models` is also filtered by the authenticated API key: `all` keys see the full routable list, while `selected` keys see the intersection of their selected models and currently routable models.

Routing pools let the admin partition provider accounts into named account pools for different agents, devices, or risk profiles. An API key can be bound to one routing pool from the API Keys page. A pool-bound key only schedules accounts that are members of that pool, including sticky session bindings scoped to that pool; an unbound key keeps using the global provider account pool. Pool membership priority is evaluated before the account's global priority inside that pool, so the same provider account can be ranked differently for different client keys. Missing or deleted pools fail closed with `routing_pool_unavailable`, empty enabled pools fail closed with `routing_pool_empty`, and Request Logs retain the routing pool name/id for attribution.

The API Keys page supports local search and status filtering by name, prefix, model policy, selected model, active/disabled/deleted status, and limiter state, so a busy or deleted client key can be found without leaving the page.

API key names can be renamed from the API Keys page without rotating the secret, so labels can be kept in sync with devices, agents, or usage purpose.

New API keys are stored with an encrypted reusable secret. The Prefix column on the API Keys page can copy the full API key again after creation for active or disabled keys; older keys created before encrypted secret storage may need to be rotated if their full value was not saved.

API keys have three visible states: active, disabled, and deleted. Active and disabled keys can be toggled directly from the API Keys table status column, and disabled keys cannot authenticate gateway requests. Deleting an active or disabled key performs an irreversible logical delete immediately, keeps the row visible during its 7 day retention window, and exposes the scheduled physical deletion time in the deleted status tooltip. Deleted keys can be physically deleted immediately with a second confirmed Delete action. Keys past the retention window are physically removed by startup and hourly cleanup, with API key listing cleanup as a fallback.

API key budgets are personal operational safeguards, not billing balances. Each key can have request, token, and estimated cost budgets over rolling 24h and 30d windows; cost budgets use stored estimated request cost, and `0` disables a budget field. When a key is over budget, clients receive OpenAI-compatible `rate_limit_exceeded` responses while Request Logs store the precise local reason as `api_key_request_budget_exceeded`, `api_key_token_budget_exceeded`, or `api_key_cost_budget_exceeded`.

Routing diagnostics can preview scheduler fallback without sending traffic. In Selection preview, set **Routing pool** to preview a pool-bound key path or leave it on the global provider pool. Set **Excluded account IDs** to a comma-separated list such as `7, 8` to simulate those provider accounts being unavailable; excluded accounts remain visible as blocked candidates with the reason `account excluded`. Routing preview also shows each candidate's active concurrency and effective account cap; candidates at a positive cap are marked **Concurrency full**. Each schedulable preview candidate includes a **Schedule reason** as diagnostic text, such as sticky session binding or priority/load/least-recently-used order; this explains the current rank and does not change scheduler behavior.

## Gateway Runtime Limits

Gateway management refreshes provider accounts, model routing, and API keys before reporting readiness, so the counts and prerequisite warnings are valid even when `/gateway` is opened directly.

Gateway management also includes **Scheduling health**, which summarizes enabled, schedulable, and blocked provider accounts; **Blocked reasons** groups disabled, expired, rate-limited, and circuit-open exits so account-pool pressure is visible without opening the full Provider accounts table.

The API Keys page shows the concurrency and rate guards loaded by the running backend. Configure them with:

- `N2API_GATEWAY_MAX_CONCURRENT_REQUESTS`
- `N2API_GATEWAY_MAX_CONCURRENT_REQUESTS_PER_ACCOUNT`
- `N2API_GATEWAY_MAX_CONCURRENT_REQUESTS_PER_KEY`
- `N2API_GATEWAY_REQUESTS_PER_MINUTE_PER_KEY`
- `N2API_GATEWAY_TOKENS_PER_MINUTE_PER_KEY`

All five gateway default limits are local to the running process. Set a gateway default value to `0` to disable that guard. Per-key values set to `0` inherit the matching gateway default and do not disable that guard for only one key. The API Keys page shows active concurrency for each client key as process-local runtime state; keys at a positive effective cap are marked **Concurrency full**. It also shows **Requests window** and **Tokens window** for each key as process-local fixed one-minute counters with remaining capacity; limited windows at capacity are marked **Request limit full** or **Token limit full**, and the counters reset on the next fixed minute or backend restart. Per-account concurrency makes busy accounts temporarily ineligible so the gateway can pick another eligible account when possible; if every eligible account is busy, the gateway returns a local 429. Per-key request and observed-token minute limits use fixed one-minute windows, and local 429 responses include `Retry-After` with the seconds remaining until the next window.

Request Logs keep local gateway rejections diagnosable while client responses stay OpenAI-compatible. Local limit responses still return `rate_limit_exceeded` to clients, but the stored request-log error identifies the guard as `api_key_request_rate_limited`, `api_key_token_rate_limited`, `api_key_request_budget_exceeded`, `api_key_token_budget_exceeded`, `api_key_cost_budget_exceeded`, `gateway_concurrency_limited`, `api_key_concurrency_limited`, or `provider_account_concurrency_limited`.

Request Logs also include gateway fallback diagnostics: attempts count selected provider-account tries, and fallbacks count pre-stream scheduler moves caused by busy accounts or retryable upstream failures.

Routing pool fallback is explicit. A routing pool can point to one fallback pool, forming a simple chain such as `primary -> secondary`. A pool-bound key never falls back to the global provider account pool; it tries only its configured pool and that explicit chain. A disabled primary pool fails closed with `routing_pool_disabled`, an empty primary pool fails closed with `routing_pool_empty`, cycles fail closed with `routing_pool_cycle`, and exhausted chains are logged as `routing_pool_exhausted`.

Request Logs support exact **Provider account**, **Routing pool**, **API key**, **Model filter**, **Usage source**, and **Session filter** fields. On Gateway management and Dashboard, 24h usage rows for **Top provider accounts**, **Top usage sources**, **Top routing pools**, **Top routing pool chains**, **Top client keys**, **Top models**, and **Top sessions** link to Request Logs with exact provider-account, usage-source, routing-pool, routing-pool-chain, API-key, model, and sticky-session filters when the row identifies a concrete entity.

For sticky session routing, POST bodies may include `session_id`. Header-based clients can send `session_id` or the proxy-friendly `X-N2API-Session-ID`; the body value takes precedence when both are present. If N2API is behind Nginx and clients send the `session_id` header, set `underscores_in_headers on;` in the relevant `http` or `server` block so Nginx does not drop that header before it reaches the gateway.

Sticky session bindings are persisted by provider, model, and `session_id`. A healthy bound account is reused while it remains schedulable; if fallback excludes it before streaming starts, the successful fallback account can rebind that session.

## Current Status

The backend includes admin API key management, provider account management, per-account model configuration, request logs, static admin UI serving, and an OpenAI-compatible gateway for `/v1/models`, `/v1/chat/completions`, and core `/v1/responses` routes. The OAuth flow starts from the admin provider page, returns an authorization link, accepts the pasted callback URL, stores encrypted access/refresh/id tokens in PostgreSQL, and records isolated account metadata such as email, account id, plan type, client id, token fingerprint, and browser/request fingerprint hashes. API upstream accounts store encrypted API keys and base URLs. The gateway selects enabled, schedulable provider accounts by requested model, priority, load factor, and recent use; skips disabled/rate-limited/circuit-open/expired accounts; writes upstream 429/401/403/5xx failures back to account status; and can fall back before response streaming begins.

`/v1/models` returns the aggregate exposed model list for the authenticated API key: a model must be enabled on at least one connected account, included in the global allowed-model list, and visible under the API key's model policy to appear. For selected-key policies, the public list is the intersection of the key's selected models and currently routable models. Model-routed POST traffic is sent only to accounts that explicitly support the requested or defaulted model, and fallback is limited to other accounts that support that same model. Connected accounts with no configured models remain visible in admin but do not receive model-routed POST traffic.

For a pool-bound API key, `/v1/models` is also limited to the configured routing pool fallback chain. Models that exist only in the global provider account pool are hidden from that key.

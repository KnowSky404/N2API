# N2API

N2API is a personal AI API/account gateway for self-hosted use. It is inspired by sub2api's practical workflow, but it is a new implementation focused on personal use rather than platform billing or merchant operations.

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
bun run check
bun run build
```

Docker Compose:

```bash
docker compose -f deploy/compose.yaml --env-file .env up --build
```

After the stack is up, open `http://localhost:3000`, sign in with the admin credentials from `.env`, and use **Provider accounts** to add Codex/OpenAI OAuth accounts or API-key upstream accounts. Adding or reauthorizing an OAuth account generates an authorization link; open it manually, finish the provider login, then paste the resulting callback URL back into the admin UI to complete the account connection.

## Provider Accounts

Provider accounts are gateway exits. N2API supports Codex OAuth accounts and API-key upstream accounts. Both account types share enabled state, priority, load factor, health status, and per-account model lists.

Select rows on the Provider accounts page to bulk enable or disable provider accounts. Use **Enable selected** or **Disable selected** to change scheduling eligibility for the selected exits, and **Clear selection** when you want to discard the current selection without changing accounts.

Configure model capability on each provider account row. The API Keys page controls the gateway default model, the global routable model list, and client-key model access; these settings do not grant capability to accounts that do not list that model.

API upstream credentials can be updated after account creation. Use the provider account row to rotate the encrypted upstream API key or base URL; saving new credentials clears local failure status so a previously rate-limited, expired, or circuit-open API upstream can be scheduled again with the new settings.

Use **Test account** when you want to probe one provider account before sending client traffic through it. The action probes one provider account with its current OAuth token or API upstream key, clears local failure status on a successful probe, and records upstream failure status for 401/403/429/5xx probe responses. The account row keeps the last test status, last test time, and last test error so manual checks remain visible after refresh. Each probe also writes provider account test history, available from the admin API at `GET /api/admin/provider-accounts/{id}/test-results`.

Provider account auto tests are disabled by default. `N2API_PROVIDER_ACCOUNT_AUTO_TEST_ENABLED` and `N2API_PROVIDER_ACCOUNT_AUTO_TEST_INTERVAL_SECONDS` are startup defaults for Gateway Settings; after sign-in, use the Gateway Settings form to save the runtime auto-test setting. Enable it to run **Test all accounts** automatically in the backend, and use an interval of `300` seconds or higher for routine checks. Automatic tests update the same last test status, last test time, last test error, test history, and local account health fields shown in Provider accounts and Routing diagnostics.

Use **Pause scheduling** when you want a healthy account to stop receiving traffic for a short window. Set **Pause duration seconds** on the Provider accounts page before clicking the action; it temporarily opens the account circuit for that window without disabling or deleting the account. Paused and rate-limited rows show the remaining scheduling block in the status column. Use **Reset local status** to clear the pause early when you want the account to rejoin routing immediately.

During migration, an install with a single connected provider account and no account-specific models backfills that account from the global allowed model list. Installs with multiple provider accounts keep models manual so the gateway does not assume every account can serve every globally allowed model.

Lower priority numbers are selected first. Within the same priority and health class, accounts with a higher load factor are considered before lower-capacity accounts; keep weak or quota-sensitive accounts at load factor `1` and raise stronger accounts when you want them to carry more traffic.

## API Key Model Access

Client API keys default to all routable models. For narrower access, set a key to selected models on the API Keys page. A selected model must still have at least one enabled healthy provider account before the gateway can route requests to it. `/v1/models` is also filtered by the authenticated API key: `all` keys see the full routable list, while `selected` keys see the intersection of their selected models and currently routable models.

Routing diagnostics can preview scheduler fallback without sending traffic. In Selection preview, set **Excluded account IDs** to a comma-separated list such as `7, 8` to simulate those provider accounts being unavailable; excluded accounts remain visible as blocked candidates with the reason `account excluded`.

## Gateway Runtime Limits

The API Keys page shows the concurrency and rate guards loaded by the running backend. Configure them with:

- `N2API_GATEWAY_MAX_CONCURRENT_REQUESTS`
- `N2API_GATEWAY_MAX_CONCURRENT_REQUESTS_PER_ACCOUNT`
- `N2API_GATEWAY_MAX_CONCURRENT_REQUESTS_PER_KEY`
- `N2API_GATEWAY_REQUESTS_PER_MINUTE_PER_KEY`
- `N2API_GATEWAY_TOKENS_PER_MINUTE_PER_KEY`

All five gateway default limits are local to the running process. Set a gateway default value to `0` to disable that guard. Per-key values set to `0` inherit the matching gateway default and do not disable that guard for only one key. Per-account concurrency makes busy accounts temporarily ineligible so the gateway can pick another eligible account when possible; if every eligible account is busy, the gateway returns a local 429. Per-key request and observed-token minute limits use fixed one-minute windows, and local 429 responses include `Retry-After` with the seconds remaining until the next window.

For sticky session routing, POST bodies may include `session_id`. Header-based clients can send `session_id` or the proxy-friendly `X-N2API-Session-ID`; the body value takes precedence when both are present. If N2API is behind Nginx and clients send the `session_id` header, set `underscores_in_headers on;` in the relevant `http` or `server` block so Nginx does not drop that header before it reaches the gateway.

## Current Status

The backend includes admin API key management, provider account management, per-account model configuration, request logs, static admin UI serving, and an OpenAI-compatible gateway for `/v1/models`, `/v1/chat/completions`, and core `/v1/responses` routes. The OAuth flow starts from the admin provider page, returns an authorization link, accepts the pasted callback URL, stores encrypted access/refresh/id tokens in PostgreSQL, and records isolated account metadata such as email, account id, plan type, client id, token fingerprint, and browser/request fingerprint hashes. API upstream accounts store encrypted API keys and base URLs. The gateway selects enabled, schedulable provider accounts by requested model, priority, load factor, and recent use; skips disabled/rate-limited/circuit-open/expired accounts; writes upstream 429/401/403/5xx failures back to account status; and can fall back before response streaming begins.

`/v1/models` returns the aggregate exposed model list for the authenticated API key: a model must be enabled on at least one connected account, included in the global allowed-model list, and visible under the API key's model policy to appear. For selected-key policies, the public list is the intersection of the key's selected models and currently routable models. Model-routed POST traffic is sent only to accounts that explicitly support the requested or defaulted model, and fallback is limited to other accounts that support that same model. Connected accounts with no configured models remain visible in admin but do not receive model-routed POST traffic.

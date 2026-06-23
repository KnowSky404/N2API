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

Provider accounts are gateway exits. N2API supports Codex OAuth accounts and API-key upstream accounts. Both account types share enabled state, priority, health status, and per-account model lists.

Configure model capability on each provider account row. The API Keys page controls the gateway default model, the global routable model list, and client-key model access; these settings do not grant capability to accounts that do not list that model.

## API Key Model Access

Client API keys default to all routable models. For narrower access, set a key to selected models on the API Keys page. A selected model must still have at least one enabled healthy provider account before the gateway can route requests to it. `/v1/models` is also filtered by the authenticated API key: `all` keys see the full routable list, while `selected` keys see the intersection of their selected models and currently routable models.

## Gateway Runtime Limits

The API Keys page shows the concurrency and rate guards loaded by the running backend. Configure them with:

- `N2API_GATEWAY_MAX_CONCURRENT_REQUESTS`
- `N2API_GATEWAY_MAX_CONCURRENT_REQUESTS_PER_ACCOUNT`
- `N2API_GATEWAY_MAX_CONCURRENT_REQUESTS_PER_KEY`
- `N2API_GATEWAY_REQUESTS_PER_MINUTE_PER_KEY`
- `N2API_GATEWAY_TOKENS_PER_MINUTE_PER_KEY`

All five limits are local to the running process. Set a value to `0` to disable that guard. Per-account concurrency makes busy accounts temporarily ineligible so the gateway can pick another eligible account when possible; if every eligible account is busy, the gateway returns a local 429. Per-key request and observed-token minute limits use fixed one-minute windows, and local 429 responses include `Retry-After` with the seconds remaining until the next window.

For sticky session routing, POST bodies may include `session_id`. Header-based clients can send `session_id` or the proxy-friendly `X-N2API-Session-ID`; the body value takes precedence when both are present. If N2API is behind Nginx and clients send the `session_id` header, set `underscores_in_headers on;` in the relevant `http` or `server` block so Nginx does not drop that header before it reaches the gateway.

## Current Status

The backend includes admin API key management, provider account management, per-account model configuration, request logs, static admin UI serving, and an OpenAI-compatible gateway for `/v1/models`, `/v1/chat/completions`, and core `/v1/responses` routes. The OAuth flow starts from the admin provider page, returns an authorization link, accepts the pasted callback URL, stores encrypted access/refresh/id tokens in PostgreSQL, and records isolated account metadata such as email, account id, plan type, client id, token fingerprint, and browser/request fingerprint hashes. API upstream accounts store encrypted API keys and base URLs. The gateway selects enabled, schedulable provider accounts by requested model, priority, and recent use; skips disabled/rate-limited/circuit-open/expired accounts; writes upstream 429/401/403/5xx failures back to account status; and can fall back before response streaming begins.

`/v1/models` returns the aggregate exposed model list for the authenticated API key: a model must be enabled on at least one connected account, included in the global allowed-model list, and visible under the API key's model policy to appear. For selected-key policies, the public list is the intersection of the key's selected models and currently routable models. Model-routed POST traffic is sent only to accounts that explicitly support the requested or defaulted model, and fallback is limited to other accounts that support that same model. Connected accounts with no configured models remain visible in admin but do not receive model-routed POST traffic.

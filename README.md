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

After the stack is up, open `http://localhost:3000`, sign in with the admin credentials from `.env`, and use **Provider accounts** to add Codex/OpenAI OAuth accounts. Adding or reauthorizing an account generates an authorization link; open it manually, finish the provider login, then paste the resulting callback URL back into the admin UI to complete the account connection. Each row is a separate OAuth account with its own encrypted tokens, priority, enabled flag, status, configured models, manual token refresh action, and reauthorization action.

Configure model capability on each connected account from the provider account row. The global **Models** settings page controls which configured models are exposed through the gateway and which model is injected by default when a request omits `model`; it does not grant capability to accounts that do not list that model.

## Current Status

The backend includes admin API key management, OpenAI/Codex OAuth account pool management, per-account model configuration, request logs, static admin UI serving, and an OpenAI-compatible gateway for `/v1/models`, `/v1/chat/completions`, and core `/v1/responses` routes. The OAuth flow starts from the admin provider page, returns an authorization link, accepts the pasted callback URL, stores encrypted access/refresh/id tokens in PostgreSQL, and records isolated account metadata such as email, account id, plan type, client id, token fingerprint, and browser/request fingerprint hashes. The gateway selects enabled, schedulable OpenAI/Codex accounts by requested model, priority, and recent use; skips disabled/rate-limited/circuit-open/expired accounts; writes upstream 429/401/403/5xx failures back to account status; and can fall back before response streaming begins.

`/v1/models` returns the aggregate exposed model list: a model must be enabled on at least one connected account and included in the global allowed-model list to appear. Model-routed POST traffic is sent only to accounts that explicitly support the requested or defaulted model, and fallback is limited to other accounts that support that same model. Connected accounts with no configured models remain visible in admin but do not receive model-routed POST traffic.

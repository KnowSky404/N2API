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

## Current Status

The backend includes admin API key management, OpenAI/Codex OAuth account pool management, request logs, static admin UI serving, and an OpenAI-compatible gateway for `/v1/models`, `/v1/chat/completions`, and core `/v1/responses` routes. The OAuth flow starts from the admin provider page, redirects to OpenAI login, stores encrypted access/refresh/id tokens in PostgreSQL, and records account metadata such as email, account id, plan type, and client id. The gateway selects enabled OpenAI/Codex accounts by priority and recent use, and can fall back before response streaming begins.

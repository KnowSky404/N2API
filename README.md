<div align="center">
  <img src="frontend/static/n2api-logo.svg" alt="N2API logo" width="112" height="112" />
  <h1>N2API</h1>
  <p>Personal AI API/account gateway for self-hosted use.</p>
</div>

N2API is a personal AI API/account gateway for self-hosted use. It provides a
single OpenAI-compatible entry point for Codex/OpenAI OAuth accounts and
API-key-compatible upstreams, with a compact administration interface for
accounts, routing, client keys, and request logs.

N2API is inspired by sub2api's practical workflow, but it is a new
implementation focused on personal use rather than platform billing or merchant
operations.

## Features

- Codex/OpenAI OAuth and API-key upstream accounts.
- OpenAI-compatible `/v1/models`, `/v1/responses`, and
  `/v1/chat/completions` routes.
- Routing pools, model policies, fallback chains, and sticky sessions.
- Provider health checks, local rate and concurrency guards, and request logs.
- PostgreSQL-backed configuration with encrypted provider credentials.
- Static SvelteKit admin UI served by the Go backend.
- Docker Compose deployment for `linux/amd64` and `linux/arm64`.

## Quick Start

Requirements: Docker Engine with Docker Compose v2.

```bash
git clone https://github.com/KnowSky404/N2API.git
cd N2API
cp .env.example .env
```

Replace every `change-me` value in `.env`. At minimum, configure a strong
`POSTGRES_PASSWORD`, `N2API_ADMIN_PASSWORD`, and `N2API_ENCRYPTION_SECRET`, then
start the stack:

```bash
docker compose -f deploy/compose.yaml --env-file .env up --build
```

Open `http://localhost:3000` and sign in with the admin credentials from
`.env`. The development Compose file explicitly accepts its HTTP public origin,
container wildcard bind, and plaintext private-network database topology;
published-image deployments remain fail-closed until those risks are selected
individually. See the [operator manual](docs/manual.md#host-binding-modes) for
release loopback, LAN, IPv6, dual-stack, and Docker-network-only bindings.

## Basic Setup

1. Add a Codex/OpenAI OAuth account or API-key upstream on **Provider
   accounts**.
2. Test the account and enable the models it can serve.
3. Create a routing pool and add the provider account to it.
4. Create a client key on **API Keys** and bind it to the routing pool.
5. Use `http://localhost:3000/v1` as the OpenAI-compatible base URL and the
   client key as the bearer token.

The OAuth flow returns an authorization link. Complete the provider login in
your browser, then paste the resulting callback URL into N2API to finish the
connection.

## Documentation

The [documentation index](docs/README.md) is the entry point for the N2API
manual. It includes detailed deployment, upgrade, provider-account, routing,
gateway-limit, request-log, and downstream Codex CLI guidance.

- [N2API manual](docs/manual.md)
- [Brand assets](docs/brand/README.md)
- [UI design source of truth](DESIGN.md)

The `docs/` directory is intentionally the home for detailed documentation so
it can later be published as a standalone documentation site.

## Project Scope

N2API V1 uses a Go backend, PostgreSQL, and a Bun + SvelteKit + Tailwind CSS
admin UI. Docker Compose is the primary deployment path. V1 does not include
public registration, payment, recharge, balance, merchant, or platform billing
features, and it does not require Redis or SQLite.

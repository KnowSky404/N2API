# N2API V1 Design

## Summary
N2API is a personal AI API/account gateway for self-hosted use. It is inspired by sub2api's practical goal of making AI service access easier, but it is a new implementation and deliberately avoids sub2api's platform billing and merchant-oriented scope.

V1 prioritizes a stable Docker deployment for one owner, Codex/OpenAI OAuth, OpenAI-compatible API access, Codex-oriented adapter behavior, PostgreSQL persistence, and a small operational admin UI.

## Goals
- Provide a personal gateway for Codex/OpenAI account access.
- Store OAuth credentials, refresh metadata, client API keys, model mappings, admin credentials, and request logs in PostgreSQL.
- Expose OpenAI-compatible `/v1/*` routes for broad client compatibility.
- Expose Codex-specific adapter routes when needed for better Codex CLI behavior.
- Serve a SvelteKit admin UI from the Go backend.
- Run locally and on a small VPS through Docker Compose.

## Non-Goals
- No source-code fork or copy from sub2api.
- No public registration.
- No payment, recharge, balance, invoice, sponsor, merchant, or platform billing features.
- No broad multi-tenant SaaS behavior.
- No Redis requirement in V1.
- No SQLite implementation in V1.
- No Claude/Gemini provider implementation in V1.

## Technical Baseline
- Backend: Go service.
- Database: PostgreSQL.
- Frontend: Bun + SvelteKit + Tailwind CSS.
- Deployment: Docker Compose.
- Admin auth: single administrator password.
- V1 upstream: Codex/OpenAI OAuth only.

## Architecture
The Go backend owns the runtime process. It serves API traffic, OAuth flows, admin API routes, and the compiled SvelteKit admin UI.

Core backend units:
- `gateway`: validates client API keys, recognizes OpenAI-compatible and Codex-specific routes, normalizes requests, preserves streaming responses, and maps upstream errors.
- `provider/openai`: handles Codex/OpenAI OAuth, token refresh, upstream request signing, upstream response handling, and provider-specific errors.
- `store`: manages PostgreSQL migrations and persistence for secrets, settings, API keys, logs, and refresh metadata.
- `admin`: exposes internal JSON APIs for the SvelteKit dashboard.
- `web`: serves static frontend assets and SPA fallback routes.

The frontend is built as static SvelteKit output. Bun is used for frontend development and build scripts, but Bun does not run in production.

## Data and Security
- OAuth access tokens and refresh tokens are encrypted before they are persisted.
- Admin password hashes are stored in PostgreSQL.
- Client API keys are stored as hashes.
- The application requires a server-side encryption secret through environment configuration.
- Request logs avoid storing full sensitive request bodies by default.

## Deployment
The default deployment is Docker Compose with two required services:
- `n2api`: Go application container.
- `postgres`: PostgreSQL data store with a persistent volume.

Redis can be added later through an optional profile if multi-instance rate limiting, distributed queues, or distributed locks become necessary.

## Acceptance Criteria
- A fresh checkout documents the project direction and V1 constraints.
- The repository contains a bootstrap plan for implementing the service.
- The documented stack consistently uses PostgreSQL as the V1 database.
- No V1 document describes SQLite, Redis, billing, or public SaaS features as required scope.

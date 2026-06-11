# N2API Deployment

The default deployment target is Docker Compose on a small VPS.

## Start Locally

From the repository root:

```bash
cp .env.example .env
docker compose -f deploy/compose.yaml --env-file .env up --build
```

The default app URL is `http://localhost:3000`.

## OpenAI/Codex Account Pool

Start the stack, log in as admin, and use the provider section to connect one or more OpenAI/Codex accounts. The default OAuth flow uses the Codex-compatible OpenAI OAuth client with PKCE, so the OAuth client id, client secret, auth URL, and token URL can usually stay blank in `.env`.

- Disabled accounts are kept in PostgreSQL but are not selected for gateway traffic.
- Lower priority numbers are selected before higher priority numbers.
- If one enabled account cannot refresh a token or fails before streaming starts, N2API tries another eligible account.
- Once upstream streaming has started, N2API preserves that stream and does not retry against another account.
- OAuth access tokens, refresh tokens, id tokens, and short-lived PKCE verifier records are encrypted before being stored.

Before upgrading an existing deployment, back up PostgreSQL because the upgrade adds authorization metadata columns to `oauth_states` and account metadata columns to `oauth_accounts`.

## Required Services

- `n2api`: Go application service.
- `postgres`: PostgreSQL database with a persistent Docker volume.

Redis is intentionally not required for V1. Add it later only if distributed rate limiting, queueing, or multi-instance locking becomes necessary.

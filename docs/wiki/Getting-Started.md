# Getting Started

Use a local development session to try N2API. For a persistent VPS deployment,
follow [Production operations](https://github.com/KnowSky404/N2API/wiki/Operations); the development stack does not
restart automatically.

## Start Locally

Requirements: Docker Engine with Docker Compose v2, Git, Make, and Bash.

```bash
git clone https://github.com/KnowSky404/N2API.git
cd N2API
cp .env.example .env
```

Replace every `change-me` value in `.env`. Set a strong
`POSTGRES_PASSWORD`, an `N2API_ADMIN_PASSWORD` of at least 12 bytes, and a
different `N2API_ENCRYPTION_SECRET` of at least 32 bytes. Review the
[startup configuration rules](https://github.com/KnowSky404/N2API/wiki/Manual#startup-configuration-safety).

```bash
make dev
```

Open `http://localhost:3000` on the development machine and sign in with the
admin credentials from `.env`. From another machine, use the server's reachable
hostname and configured port. Follow the
[remote access and binding guidance](https://github.com/KnowSky404/N2API/wiki/Development#development-sessions) before
exposing the service.

Keep the command open while using N2API. Ctrl-C removes development containers,
the network, and the locally built application image while preserving the
database and backups. Use `make dev-check` for a one-shot rebuild/probe/teardown
and `make dev-stop` for abandoned detached containers. See the
[development lifecycle](https://github.com/KnowSky404/N2API/wiki/Development) for details.

## Configure Accounts And Routing

1. On **Provider accounts**, add an API-key upstream with its base URL and API key.
2. Configure and enable the models it supports, then test the account.
3. Create a routing pool and add the provider account.
4. On **API Keys**, create a client key and bind it to that routing pool.
5. Set your client's OpenAI-compatible base URL to `http://localhost:3000/v1`
   and use the client key as its bearer token.

Replace the base URL when the client runs on another machine. A client key
without a routing pool has no model access. Model availability is the
intersection of the key's policy, pool and fallback chain, configured account
models, and account health. See [Provider accounts](https://github.com/KnowSky404/N2API/wiki/Manual#provider-accounts)
for the full routing rules.

The gateway supports `/v1/models`, `/v1/responses`, and
`/v1/chat/completions`. Check `GET /v1/models` with the client key before
sending model requests. For Codex, follow the
[client configuration](https://github.com/KnowSky404/N2API/wiki/Manual#downstream-codex-cli).

## Deploy A Published Image

Use the repository-local `./ops/n2api` workflow for discovery, checks,
reviewed plans, deployment, backup, upgrades, verification, and guarded
application rollback. Start with [First deployment](https://github.com/KnowSky404/N2API/wiki/Operations#scenario-a-first-deployment-on-a-new-host).

Production uses an immutable `N2API_IMAGE` reference with a readable CalVer tag
and exact manifest digest. Review host bindings, HTTPS, database connectivity,
secrets, and backup custody before applying a plan. Application rollback does
not reverse database migrations; live database restoration remains manual.

The [complete manual](https://github.com/KnowSky404/N2API/wiki/Manual#deploy-a-published-image) contains lower-level
Compose details for diagnosis and owner-controlled fallback.

<div align="center">
  <img src="frontend/static/n2api-logo.svg" alt="N2API logo" width="112" height="112" />
  <h1>N2API</h1>
  <p><strong>N Sources, One API.</strong></p>
  <p>A self-hosted AI API gateway for API-key upstreams.</p>
  <p>
    <a href="https://github.com/KnowSky404/N2API/wiki">Documentation</a> ·
    <a href="https://github.com/KnowSky404/N2API/wiki/Getting-Started">Getting started</a> ·
    <a href="https://github.com/KnowSky404/N2API/releases">Releases</a>
  </p>
</div>

N2API brings multiple API-key upstreams behind one OpenAI-compatible endpoint,
with a compact admin UI for accounts, routing, client keys, and request logs.
It is inspired by sub2api's workflow and independently implemented for personal
self-hosting.

## Features

- OpenAI-compatible `/v1/models`, `/v1/responses`, and `/v1/chat/completions`, with streaming support.
- Routing pools, model policies, explicit fallback chains, and sticky sessions.
- Account health checks, rate and concurrency limits, client-key budgets, and request logs.
- PostgreSQL-backed configuration and encrypted credentials.
- Go backend with a SvelteKit + Tailwind CSS admin UI; Docker Compose deployment on `linux/amd64` and `linux/arm64`.

## Quick Start

Follow [Getting started](https://github.com/KnowSky404/N2API/wiki/Getting-Started)
for local startup, account setup, and your first client connection. For a VPS,
use the [production operations guide](https://github.com/KnowSky404/N2API/wiki/Operations).

## Documentation

- [Wiki](https://github.com/KnowSky404/N2API/wiki) — setup, deployment, routing, clients, and troubleshooting.
- [Complete manual](https://github.com/KnowSky404/N2API/wiki/Manual) — configuration and gateway behavior.
- [Development guide](https://github.com/KnowSky404/N2API/wiki/Development) — local sessions, tests, and cleanup.
- [Documentation sources](docs/README.md) · [Operator runbook source](docs/agent-operations.md) · [UI design](DESIGN.md).

N2API supports API-key upstreams only; OAuth upstreams and fingerprint simulation
are retired. It targets personal use, without public registration, payments,
recharge balances, or platform billing. PostgreSQL is required; Redis and SQLite
are not used.

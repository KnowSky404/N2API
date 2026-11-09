# N2API Wiki

**N Sources, One API.** N2API is a personal, self-hosted AI API gateway that
brings API-key upstreams behind an OpenAI-compatible endpoint.

Manage accounts, model availability, routing pools, fallback chains, client
keys, limits, and request logs from a compact web dashboard. The Go backend
serves a static SvelteKit + Tailwind CSS UI and stores configuration and
encrypted credentials in PostgreSQL. Docker Compose is the primary deployment
path, supporting `linux/amd64` and `linux/arm64`.

## Start Here

| Goal | Guide |
| --- | --- |
| Start locally and connect a client | [Getting started](https://github.com/KnowSky404/N2API/wiki/Getting-Started) |
| Deploy or upgrade a VPS instance | [Production operations](https://github.com/KnowSky404/N2API/wiki/Operations) |
| Configure networking, secrets, metrics, or alerts | [Complete manual](https://github.com/KnowSky404/N2API/wiki/Manual) |
| Configure accounts, pools, and client keys | [Provider accounts and routing](https://github.com/KnowSky404/N2API/wiki/Manual#provider-accounts) |
| Connect Codex CLI | [Downstream Codex CLI](https://github.com/KnowSky404/N2API/wiki/Manual#downstream-codex-cli) |
| Diagnose gateway errors and limits | [Gateway runtime limits](https://github.com/KnowSky404/N2API/wiki/Manual#gateway-runtime-limits) |
| Develop, test, or clean up a local session | [Development lifecycle](https://github.com/KnowSky404/N2API/wiki/Development) |
| Prepare a release | [Release checklist](https://github.com/KnowSky404/N2API/wiki/Release-Checklist) |
| Record a real-backup restore drill | [Restore drill record](https://github.com/KnowSky404/N2API/wiki/Restore-Drill-Record) |
| Review intended repository checks | [Repository protections](https://github.com/KnowSky404/N2API/wiki/Repository-Protections) |

## Scope And Compatibility

N2API is independently implemented and inspired by sub2api's workflow. It
targets personal self-hosting, without public registration, payments, recharge
balances, or platform billing. PostgreSQL is required; Redis and SQLite are
not used.

Upstream accounts use API keys. OAuth authorization, token refresh, and
fingerprint simulation are retired. Existing OAuth rows remain archived for
backup compatibility and cannot serve requests; replace their routing-pool
memberships with API-key accounts. Downstream Codex clients still connect
through a client API key. See the
[upgrade scope](https://github.com/KnowSky404/N2API/blob/main/docs/specs/2026-11-09-api-key-only-upstreams.md).

## Documentation Version

This Wiki describes the current `main` branch. A published image may predate
these changes; match its release notes and source revision before upgrading.

[Repository](https://github.com/KnowSky404/N2API) ·
[Releases](https://github.com/KnowSky404/N2API/releases) ·
[Documentation sources](https://github.com/KnowSky404/N2API/tree/main/docs)

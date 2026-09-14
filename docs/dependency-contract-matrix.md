# Dependency and SDK Contract Matrix

Last reviewed: 2026-09-14

This matrix records the versions that are intentionally coupled across the
repository. The files named in the source-of-truth column remain authoritative;
this document is an audit index, not a second lockfile.

## Runtime and build inventory

| Area | Version or range | Source of truth | Cross-check / evidence |
| --- | --- | --- | --- |
| Backend Go toolchain | `1.26.5` | `backend/go.mod` | `deploy/Dockerfile`, `deploy/Dockerfile.e2e`, and CI `setup-go` read or match `backend/go.mod` |
| Backend direct Go dependencies | `pgx/v5 5.10.0`, `goose/v3 3.27.3`, Prometheus `1.24.1/0.6.2/0.70.1`, `utls 1.8.2`, YAML `3.0.5`, `x/crypto 0.54.0` | `backend/go.mod`, `backend/go.sum` | `go test ./...`, `go vet ./...`, Staticcheck `2026.1` |
| Frontend package manager | Bun `1.3.14` | `frontend/package.json` | `frontend/bun.lock`, CI `setup-bun`, and application Docker images |
| Frontend framework | SvelteKit `2.70.1`, Svelte `5.56.8`, Vite `8.1.5` | `frontend/package.json`, `frontend/bun.lock` | `make test` runs check, all Bun tests, and production build |
| Frontend styling and adapter | Tailwind `4.3.3`, `@tailwindcss/vite 4.3.3`, adapter-static declared `^3.0.9` and locked `3.0.10` | `frontend/package.json`, `frontend/bun.lock` | Frozen Bun install in CI and Docker |
| JavaScript SDK fixture | `openai 6.48.0`, Bun `1.3.14` | `tests/contracts/javascript/package.json`, `tests/contracts/javascript/bun.lock` | `deploy/Dockerfile.e2e` contracts-javascript stage; `make test-contracts` |
| Python SDK fixture | `openai 2.48.0`, Python `3.12.13`, requires Python `3.12.*` | `tests/contracts/python/pyproject.toml`, `uv.lock`, `.python-version` | `deploy/Dockerfile.e2e` contracts-python stage; `make test-contracts` |
| Python package manager | uv `0.11.30` | `deploy/Dockerfile.e2e` `uv-bin` stage | `uv sync --locked --no-dev --no-install-project` in the image build |
| Application base images | Bun `1.3.14`, Go `1.26.5-alpine3.23`, Alpine `3.23.5` | `deploy/Dockerfile` and `deploy/Dockerfile.e2e` | Every external image is retained with a readable tag and immutable digest |
| Local and E2E database | PostgreSQL `18.4-alpine3.23` | `deploy/compose.yaml`, `deploy/compose.e2e.yaml`, release/restore Compose files | Digest check plus PostgreSQL-backed managed tests |
| GitHub Actions pins | Checkout v7, setup-go v6, setup-bun v2, upload-artifact v7, CodeQL v4.37.3 | `.github/workflows/*.yml` | Full commit-SHA validation in `dev/ci/verify-pinned-dependencies.sh` |

Run the consistency check with:

```sh
bash dev/ci/verify-pinned-dependencies.sh
```

The check covers image digests and tags, external Action SHAs, Go/Bun
toolchain alignment, and both SDK fixture package/lock/interpreter alignment.
Frozen installs and the contract runners provide the dependency-resolution
check; no candidate upgrade is retained by documentation alone.

## Official OpenAI SDK contract matrix

The JavaScript and Python fixtures use an API-upstream account backed by the
local mock OpenAI service. They exercise the public `/v1` contract through the
official SDKs. A row is `verified` only after both containers pass in the same
`make test-contracts` run.

| Contract | JavaScript | Python | Scope and assertion |
| --- | --- | --- | --- |
| Models list | verified | verified | `client.models.list()` returns the configured model |
| Chat Completions JSON | verified | verified | `chat.completions.create()` returns `chat.completion` and complete usage |
| Responses JSON | verified | verified | `responses.create(stream=false)` returns one completed Response with ID, output, and usage |
| Responses SSE | verified | verified | `responses.create(stream=true)` reaches `response.completed` |
| Responses tools | verified | verified | Tool declaration is forwarded and the SDK decodes a `function_call` output item |
| Authentication errors | verified | verified | Invalid API key maps to the SDK authentication error with HTTP 401 |
| Client cancellation | verified | verified | An already-aborted JavaScript request and a closed Python response stream stop locally without credentials in diagnostics |
| Early stream termination | verified | verified | Both SDK iterators can consume an initial event and terminate before the terminal event |
| Real tool execution | unsupported | unsupported | N2API forwards tool definitions; it does not execute tools or persist tool results in V1 |
| OAuth/Codex SDK login | unsupported | unsupported | OAuth browser/device flows and real provider accounts are outside the local public-API fixture |
| Arbitrary Chat/Responses conversion | unsupported | unsupported | The gateway preserves endpoint semantics and does not promise bidirectional protocol conversion |

The `verified` labels above are repository evidence from the managed contract
runner, not proof of compatibility with every future SDK release or every
OpenAI upstream response. Upgrade candidates must first update the fixture
lockfile, then pass the same matrix and the full managed regression gates in a
separate dependency commit.

## Verification boundaries

- Local mock and Compose results establish repository and gateway behavior;
  they do not establish real-provider OAuth, production routing, deliverability,
  or remote GitHub workflow success.
- SDK errors, cancellation, and early termination are checked for safe public
  behavior. The gateway's deeper timeout, disconnect, terminal-failure, and
  request-log diagnostics remain covered by the PostgreSQL-backed E2E suite.
- Do not upgrade the application or contract SDKs merely to chase a newer
  version. A candidate needs a focused compatibility result and a complete
  lockfile update before it can be retained.

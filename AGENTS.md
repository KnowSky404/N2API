# N2API Agent Instructions

## Communication
- Communicate with the user in Chinese unless they ask otherwise.
- Write code, identifiers, comments, commit messages, and documentation filenames in English by default.
- Keep implementation choices pragmatic and scoped to personal self-hosted use.

## Documentation Lookup
- Use Context7 MCP whenever answering or implementing details about libraries, frameworks, SDKs, APIs, CLI tools, or cloud services.
- Start with `resolve-library-id` unless the user provides an exact `/org/project` Context7 library ID.
- Prefer Context7 over web search for library documentation.

## Project Direction
- N2API is a personal AI API/account gateway inspired by sub2api's user experience, not a fork of sub2api.
- Do not copy source code from `Wei-Shaw/sub2api`; only use it as product and behavior reference.
- V1 focuses on personal use, Codex/OpenAI OAuth, OpenAI-compatible API access, and Codex-oriented adapter behavior.
- V1 must not add platform billing, recharge balances, merchant accounting, public registration, payment providers, sponsor flows, or broad multi-tenant SaaS behavior.

## Technical Baseline
- Backend: Go service.
- Runtime database: PostgreSQL.
- Frontend: Bun + SvelteKit + Tailwind CSS.
- Deployment: Docker Compose first.
- Redis is optional future infrastructure and must not be required for V1 unless explicitly approved later.
- SQLite is not part of the V1 implementation plan.

## Backend Constraints
- Store OAuth tokens, refresh metadata, client API keys, admin credentials, model configuration, and access logs in PostgreSQL.
- Treat OAuth tokens and refresh tokens as sensitive secrets. Encrypt them at rest before storing them.
- Keep provider-specific logic behind provider interfaces so Claude/Gemini can be added later without changing gateway routing.
- Expose OpenAI-compatible `/v1/*` routes and Codex-specific routes through one shared internal request pipeline.
- Preserve streaming behavior end to end for supported upstream responses.

## Frontend Constraints
- Follow the root `DESIGN.md` for all N2API UI design, styling, layout, and component decisions.
- Use Bun for frontend dependency install and script execution.
- Build the SvelteKit admin UI as static assets that can be served by the Go backend.
- Use Tailwind CSS for styling.
- The admin UI is an operational dashboard, not a landing page.

## Development Commands
- Prefer `go test ./...` for backend verification.
- Prefer `bun run check` and `bun run build` for frontend verification.
- Prefer Docker Compose for local full-stack verification.
- Do not introduce npm or yarn unless Bun cannot support a required package or workflow.

## Repository Hygiene
- Keep generated dependencies and build outputs out of git.
- Use `.env.example` for documented configuration and never commit real secrets.
- Before editing files, inspect existing contents and preserve unrelated user changes.

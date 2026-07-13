# N2API Agent Instructions

## Communication
- Communicate with the user in Chinese unless they ask otherwise.
- Write code, identifiers, comments, commit messages, and documentation filenames in English by default.
- Keep implementation choices pragmatic and scoped to personal self-hosted use.

## Documentation Lookup
- Use Context7 MCP whenever answering or implementing details about libraries, frameworks, SDKs, APIs, CLI tools, or cloud services.
- Start with `resolve-library-id` unless the user provides an exact `/org/project` Context7 library ID.
- Prefer Context7 over web search for library documentation.
- When web search is still needed, use it only after Context7 cannot answer the library/framework/API/CLI documentation question or when the user explicitly asks for broader web research.

## Development Workflow
- Use an applicable skill when it is available in the current session, but do not make the workflow depend on a specific plugin.
- Before implementing non-trivial behavior, write a concise specification and implementation plan with clear acceptance criteria.
- Before fixing bugs or failed checks, reproduce the issue, gather evidence, and identify the root cause before changing code.
- Before claiming work is complete, review the diff and worktree state, run the relevant verification commands, and report the exact commands that passed.
- Keep work scoped and incremental. Avoid bundling unrelated refactors into feature or bugfix changes.

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
- Treat `DESIGN.md` as the source of truth for UI visual style. Do not introduce a competing design system unless the user explicitly approves replacing it.
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
- After each atomic change (each feature, fix, refactor, docs update, or test update), you MUST create an atomic git commit. Never accumulate multiple unrelated changes into one commit, and never leave a change uncommitted after it is complete unless the user explicitly asks otherwise.
- Each commit MUST contain exactly one coherent change: for example, one feature, one fix, one refactor, one docs update, or one test update.
- Use Conventional Commits for commit messages, such as `feat: add provider health check`, `fix: preserve streaming response headers`, `docs: update deployment guide`, `test: cover token refresh`, or `chore: update tooling`.
- Do not commit generated build artifacts, dependency directories, local caches, or real environment files.
- After every conversation turn that involves code or functionality changes, rebuild and refresh the local Docker Compose dev stack so the user can test and verify. Use the `n2api-refresh-docker` skill for the exact commands. If no code or functionality was changed during the turn, skip this step.

## DeepSeek Delegation Workflow

N2API inherits the global DeepSeek/subagent policy from `~/.codex/AGENTS.md`,
including agent selection, model settings, worker depth limits, thread lifecycle,
and fallback rules.

Project-specific additions:
- The main agent remains responsible for N2API requirements, architecture,
  planning, review, worker coordination, final acceptance, and user communication.
- Preserve N2API-specific constraints from this file, including personal V1 scope,
  Go/Bun/SvelteKit/PostgreSQL baseline, PostgreSQL-backed secret storage,
  `DESIGN.md` frontend rules, atomic Conventional Commits, and Docker Compose
  refresh after code or functionality changes.
- Verification may use the global worker-first policy, but the main session is
  still responsible for final acceptance and for reporting the exact verification
  commands that passed.

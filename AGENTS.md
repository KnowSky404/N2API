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
- Prefer relevant Superpowers skills for planning, design, debugging, implementation, verification, review, and branch finishing.
- Before implementing non-trivial behavior, use the appropriate Superpowers planning/design workflow.
- Before fixing bugs or failed checks, use the Superpowers systematic debugging workflow.
- Before claiming work is complete, use the Superpowers verification-before-completion workflow and report the exact commands that passed.
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

## DeepSeek Delegation Workflow (Applies to the Main Agent Session)

These rules bind the main agent (gpt-5.5) when working on N2API. They keep code-changing work
worker-first while preserving the main agent's responsibility for Superpowers workflow control,
review, commits, final verification, and Docker refresh.

1. **Architect/reviewer/coordinator.** The main agent's role is focused on requirements clarification,
   architecture, spec/plan, task decomposition, worker coordination, diff/result review,
   acceptance, final user communication, and project-policy closure.

2. **Implementation is worker-first.** Code/config/document edits, bug fixes, mechanical
   refactors, and routine task-level tests should be delegated to DeepSeek whenever bounded:
   `deepseek-worker` for implementation and `deepseek-flash` for read-only scans, logs,
   and diagnostics.

3. **Avoid direct implementation patches.** If a worker result is wrong, prefer sending a
   correction task back to DeepSeek instead of manually patching. The main agent may make tiny
   control-plane or documentation adjustments when delegation would be disproportionate or when
   needed to unblock orchestration.

4. **Control-plane commands are allowed.** The main agent may run commands needed to orchestrate
   and close work: `codex exec` worker dispatch, reading diffs/status/logs, applying accepted
   worker output, git add/commit/push, final smoke checks, CI inspection, and Docker Compose
   refresh required by this project.

5. **Verification is worker-first, not worker-only.** Ask DeepSeek workers to run task-level tests
   and report outputs. The main agent remains responsible for final acceptance and may run or rerun
   narrow verification commands when needed to validate integration, satisfy Superpowers/project
   policy, or diagnose worker/tool failure.

6. **Existing N2API constraints preserved.** All project-specific language, technical baseline,
   workflow, and hygiene rules (Chinese communication + English code, Go/Bun/PostgreSQL baseline,
   DESIGN.md UI rules, atomic commits, Docker Compose refresh) remain in full effect.

7. **Current API/Superpowers fallback.** When the active `spawn_agent` tool cannot select the
   named custom agents or DeepSeek model IDs directly, preserve the Superpowers control flow in
   the main session and delegate bounded execution through `codex exec`:
   `codex exec --sandbox workspace-write --cd /root/Clouds/N2API -m deepseek/deepseek-v4-pro -c model_context_window=1000000 -c model_auto_compact_token_limit=900000 -c model_reasoning_effort='"max"' "<task prompt>"`.
   For read-only scans use:
   `codex exec --sandbox read-only --cd /root/Clouds/N2API -m deepseek/deepseek-v4-flash -c model_context_window=1000000 -c model_auto_compact_token_limit=900000 -c model_reasoning_effort='"high"' "<task prompt>"`.

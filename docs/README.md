# N2API Documentation

Read the [N2API Wiki](https://github.com/KnowSky404/N2API/wiki) for setup,
configuration, and operations. The root README is a short project overview;
this directory holds the version-controlled documentation sources.

## Start Here

- [Wiki home](wiki/Home.md)
- [Quick start and basic setup](wiki/Getting-Started.md)
- [Agent-native production operations](agent-operations.md)
- [Complete manual](manual.md)
- [Development resource lifecycle](development.md)

## Manual

- [Local startup](manual.md#start-locally)
- [Published images](manual.md#published-images)
- [Release update notifications](manual.md#release-update-notifications)
- [Release workflow](manual.md#preview-and-publish-a-release)
- [Release checklist](release-checklist.md)
- [Repository protections](repository-protections.md)
- [Operator restore drill record](restore-drill-record.md)
- [Docker installation on Ubuntu 24.04 ARM64](manual.md#install-docker-on-ubuntu-2404-arm64)
- [Production deployment](manual.md#deploy-a-published-image)
- [Canonical deployment, backup, upgrade, and rollback](agent-operations.md)
- [Lower-level backup and upgrade details](manual.md#back-up-and-upgrade)
- [Portable configuration export](manual.md#portable-configuration-export)
- [Downstream Codex CLI](manual.md#downstream-codex-cli)
- [Provider accounts, API keys, and routing](manual.md#provider-accounts)
- [Gateway limits, logs, and sticky sessions](manual.md#gateway-runtime-limits)
- [Required services](manual.md#required-services)

## Project Reference

- [UI design source of truth](../DESIGN.md)
- [Brand assets and generation records](brand/README.md)
- [Reliability and operations design](specs/2026-07-21-n2api-reliability-and-operations-design.md)
- [Reliability and operations acceptance report](reliability-operations-acceptance.md)
- [Bounded metrics contract](specs/2026-07-21-n2api-metrics-contract.md)
- [Production correctness design](specs/2026-07-29-n2api-production-correctness-hardening.md)
- [Production correctness implementation plan](plans/2026-07-29-n2api-production-correctness-hardening-plan.md)
- [Production correctness acceptance report](production-correctness-hardening-acceptance.md)
- [Agent-native operations acceptance report](agent-native-operations-acceptance.md)
- [Reliability and operations plans](plans/README.md)
- [Historical implementation specifications](superpowers/specs/)
- [Historical implementation plans](superpowers/plans/)

The dated records under `superpowers/` describe completed or partially
completed feature delivery. New cross-cutting reliability and operations work
is tracked under `specs/` and `plans/`; the historical records remain available
for implementation context and are not a second active roadmap.

## Wiki Maintenance

Edit these sources through the normal worktree, review, and PR workflow.
After merging, publish the following files from the merged revision to the
separate `N2API.wiki.git` repository:

| Repository source | Wiki file |
| --- | --- |
| `docs/wiki/Home.md` | `Home.md` |
| `docs/wiki/Getting-Started.md` | `Getting-Started.md` |
| `docs/wiki/_Sidebar.md` | `_Sidebar.md` |
| `docs/manual.md` | `Manual.md` |
| `docs/agent-operations.md` | `Operations.md` |
| `docs/development.md` | `Development.md` |
| `docs/release-checklist.md` | `Release-Checklist.md` |
| `docs/repository-protections.md` | `Repository-Protections.md` |
| `docs/restore-drill-record.md` | `Restore-Drill-Record.md` |

When copying a manual, rewrite relative Markdown links to the corresponding
Wiki page names above without the `.md` extension, preserving heading anchors.
Link other repository files to their absolute GitHub URLs. Keep code examples
unchanged. Preserve unrelated Wiki pages, commit only the listed pages, and
publish with a normal push. Verify the published pages and links from a fresh
clone. The Wiki describes `main`; release users should also check their image's
source revision and release notes.

Implementation specifications, plans, and acceptance records remain in the
repository. Do not treat publication as new runtime or production evidence.

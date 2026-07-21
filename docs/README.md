# N2API Documentation

This directory is the home of the N2API manual and project documentation. The
root project README stays focused on a short introduction and the basic startup
path; detailed usage and operations guidance belongs here.

## Start Here

- [Quick start](../README.md#quick-start)
- [Basic setup](../README.md#basic-setup)
- [Complete manual](manual.md)

## Manual

- [Local startup](manual.md#start-locally)
- [Published images](manual.md#published-images)
- [Release workflow](manual.md#preview-and-publish-a-release)
- [Release checklist](release-checklist.md)
- [Docker installation on Ubuntu 24.04 ARM64](manual.md#install-docker-on-ubuntu-2404-arm64)
- [Production deployment](manual.md#deploy-a-published-image)
- [Backup, upgrade, and rollback](manual.md#back-up-and-upgrade)
- [Portable configuration export](manual.md#portable-configuration-export)
- [Downstream Codex CLI](manual.md#downstream-codex-cli)
- [Provider accounts, API keys, and routing](manual.md#provider-accounts)
- [Gateway limits, logs, and sticky sessions](manual.md#gateway-runtime-limits)
- [Required services](manual.md#required-services)

## Project Reference

- [UI design source of truth](../DESIGN.md)
- [Brand assets and generation records](brand/README.md)
- [Reliability and operations design](specs/2026-07-21-n2api-reliability-and-operations-design.md)
- [Reliability and operations plans](plans/README.md)
- [Historical implementation specifications](superpowers/specs/)
- [Historical implementation plans](superpowers/plans/)

The dated records under `superpowers/` describe completed or partially
completed feature delivery. New cross-cutting reliability and operations work
is tracked under `specs/` and `plans/`; the historical records remain available
for implementation context and are not a second active roadmap.

## Documentation Site Direction

The Markdown files under `docs/` use repository-relative links and keep
user-facing manuals separate from implementation records. This provides a
stable content root for a future Pages documentation site without coupling the
repository README to a specific static-site generator.

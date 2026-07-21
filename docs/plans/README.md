# Reliability and Operations Plans

These plans implement the [reliability and operations design](../specs/2026-07-21-n2api-reliability-and-operations-design.md).
They are the active cross-cutting roadmap. Existing records under
`docs/superpowers/` remain historical feature implementation context.

## Recommended Order

1. [Gateway E2E compatibility](2026-07-21-gateway-e2e-compatibility.md)
2. [Admin security hardening](2026-07-21-admin-security-hardening.md)
3. [Container and deployment hardening](2026-07-21-container-deployment-hardening.md)
4. [Request Log lifecycle](2026-07-21-request-log-lifecycle.md)
5. [Backup and restore verification](2026-07-21-backup-restore-verification.md)
6. [Encryption key rotation](2026-07-21-encryption-key-rotation.md)
7. [System Event alerting](2026-07-21-system-event-alerting.md)
8. [Routing weight semantics](2026-07-21-routing-weight-semantics.md)
9. [Metrics and tracing](2026-07-21-metrics-and-tracing.md)
10. [Repository governance](2026-07-21-repository-governance.md)

Health-probe separation is the first implementation task inside container and
deployment hardening. It can land before the rest of that plan. Request Log
query/index work must use measurements from a representative PostgreSQL data
set. License selection remains an owner decision and does not block technical
hardening.

## Phase Gates

| Phase | Completion gate | Blocks |
| --- | --- | --- |
| 0: baseline | Design and plans are indexed; current capabilities and conflicts are recorded | All later phase claims |
| 1: closed-loop reliability | Mock-upstream full-stack suite passes without real secrets; failure artifacts are sanitized | Major routing or proxy rewrites |
| 2: security baseline | Trusted proxy, login protection, session controls, headers, startup validation, and probes pass | Public deployment guidance |
| 3: logs and deployment | Cursor paging, batched retention, streaming export, non-root image, hardened Compose, build identity | Long-retention production recommendation |
| 4: recovery and alerts | Restore drill, key rotation, and bounded notifications pass | Disaster-recovery claim |
| 5: scheduling and observability | Load-factor semantics are explicit and measured; metrics are bounded; tracing is opt-in | Scheduler behavior change |
| 6: governance | Security/contribution policies and automated security artifacts exist | Supported-version policy |

Phases may overlap only where their data contracts do not conflict. Each task
must remain one coherent Conventional Commit and include the verification
listed in its plan.

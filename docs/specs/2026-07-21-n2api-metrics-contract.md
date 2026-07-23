# N2API Bounded Metrics Contract

Status: accepted contract for Metrics And Tracing Task 1
Date: 2026-07-21
Scope: optional in-process Prometheus metrics for one N2API application instance

## Purpose

This contract freezes metric names, types, units, labels, and cardinality before
an instrumentation library or `/metrics` exposure policy is selected. Metrics
are an ephemeral operational view. PostgreSQL Request Logs and System Events
remain the durable request and audit records.

Prometheus conventions used here are: the `n2api_` namespace, snake_case names,
`_total` for counters, base-unit suffixes such as `_seconds` and `_bytes`, raw
counters instead of precomputed rates, gauges only for values that can move in
both directions, and explicitly bounded histogram buckets. Dynamic metric names
are prohibited.

## Global Cardinality Budget

- N2API-owned metrics must stay below 1,600 active series per process under the
  complete allowed label cross-product.
- The complete scrape, including standard Go runtime and process collectors,
  must stay below 2,000 active series per process.
- A new label or label value requires this document to be updated with a new
  worst-case estimate before implementation.
- Histograms count every bucket plus `_sum` and `_count` for each label set.
- Unknown enum values map to the fixed value `other`; runtime strings never
  become new label values.
- Metrics reset on process restart. PromQL computes rates and increases across
  the surviving scrape history; N2API does not persist counters.

## Prohibited Labels And Values

The following values must never appear in a metric name, label name, label
value, help text, or exemplar:

- request, correlation, session, administrator, API key, provider account, pool,
  fingerprint, System Event, or database row IDs;
- API key prefixes, key names, account names, pool names, model names, OAuth
  subjects, user names, IP addresses, user agents, routes containing a response
  ID, or arbitrary URL/query values;
- tokens, cookies, passwords, encrypted values, proxy URLs, header values,
  request/response bodies, full error messages, SQL text, or stack traces; and
- arbitrary upstream status text, task error text, action names, or other
  pass-through strings.

Prometheus exemplars are disabled for V1 because the current correlation and
request identifiers are explicitly prohibited.

## Allowed Labels

| Label | Allowed values | Source and normalization |
| --- | --- | --- |
| `route` | `models`, `chat_completions`, `responses_create`, `responses_retrieve`, `responses_input_items`, `other` | Map the five patterns accepted by `gateway.isSupportedRoute`; never use the raw path. |
| `status_class` | `2xx`, `4xx`, `5xx`, `other` | Map the final downstream HTTP status after the response/stream finishes. |
| `stream` | `true`, `false` | Derived from the actual response copy mode, not a caller-provided label string. |
| `account_type` | `codex_oauth`, `api_key`, `none`, `other` | Normalize `provider.SelectedAccount.AccountType`; `none` means no account was selected. |
| `usage_source` | `responses`, `chat_completions`, `stream`, `gemini_usage_metadata`, `anthropic_usage`, `json`, `missing`, `other` | Normalize the fixed values emitted by `gateway.Usage.Source`. |
| `token_type` | `input`, `output`, `cached_input`, `reasoning` | Fixed counter dimension; total tokens are derived from input and output. |
| `provider_state` | `active`, `disabled`, `rate_limited`, `circuit_open`, `expired`, `other` | Normalize the fixed provider account states. |
| `task` | `provider_auto_test`, `request_log_retention`, `system_event_retention`, `api_key_purge`, `other` | Fixed background task registry; adding a task requires a contract update. |
| `scope` | `gateway`, `api_key`, `provider_account`, `other` | Fixed enforcement scope; never derived from an object name or ID. |
| `outcome` | Metric-specific fixed allowlist documented below | Never use raw errors. Unexpected values become `other`. |
| `reason` | Metric-specific fixed allowlist documented below | Never use raw gateway, provider, or task error strings. |
| `adapter` | `generic_webhook`, `ntfy`, `gotify`, `other` | Reserved for the future alerting plan; no adapter metric is emitted yet. |

## Gateway Metrics

Gateway request metrics cover only the supported `/v1/*` gateway surface. Admin,
probe, static asset, and future metrics-handler traffic are excluded so scrapes
cannot recursively modify these counters.

| Metric | Type and unit | Labels | Max series | Operator use case |
| --- | --- | --- | ---: | --- |
| `n2api_gateway_requests_total` | Counter, requests | `route`, `status_class`, `stream`, `account_type` | 192 | Traffic, error-rate, streaming, and upstream-type trends. |
| `n2api_gateway_request_duration_seconds` | Histogram, seconds | `route`, `status_class`, `stream` | 816 | End-to-end downstream latency including completed stream lifetime. |
| `n2api_gateway_active_requests` | Gauge, requests | none | 1 | Process-wide concurrency pressure. |
| `n2api_gateway_upstream_attempts_total` | Counter, attempts | `account_type`, `outcome` | 24 | Upstream retry and transport health without account IDs. |
| `n2api_gateway_fallbacks_total` | Counter, fallbacks | `reason` | 4 | Frequency of explicit account fallback. |
| `n2api_gateway_routing_failures_total` | Counter, failures | `reason` | 11 | Pool/model/provider selection exhaustion. |
| `n2api_gateway_limit_rejections_total` | Counter, rejections | `scope`, `reason` | 28 | Concurrency, rate, and budget enforcement. |
| `n2api_gateway_streams_total` | Counter, streams | `route`, `outcome` | 30 | Completed, canceled, and failed stream outcomes after headers. |
| `n2api_gateway_usage_observations_total` | Counter, observations | `usage_source`, `outcome` | 16 | Missing usage and pricing-match coverage. |
| `n2api_gateway_tokens_total` | Counter, tokens | `token_type`, `usage_source` | 32 | Aggregate token volume without model/key/account dimensions. |
| `n2api_gateway_estimated_cost_usd_total` | Counter, USD | none | 1 | Aggregate matched estimated cost; unmatched usage contributes zero. |

`n2api_gateway_request_duration_seconds` uses buckets `0.01`, `0.025`, `0.05`,
`0.1`, `0.25`, `0.5`, `1`, `2.5`, `5`, `10`, `30`, `60`, `120`, `300`, and
`900` seconds. Durations beyond 900 seconds remain visible in `+Inf`, `_sum`,
and `_count`.

Metric-specific enum values are:

- upstream-attempt `outcome`: `success`, `http_error`, `transport_error`,
  `refresh_retry`, `canceled`, `other`;
- fallback `reason`: `account_concurrency`, `transport_error`,
  `retryable_status`, `other`;
- routing-failure `reason`: `routing_pool_disabled`,
  `routing_pool_unavailable`, `routing_pool_empty`, `routing_pool_exhausted`,
  `routing_pool_cycle`, `provider_not_connected`, `provider_not_configured`,
  `provider_accounts_disabled`, `provider_accounts_unavailable`,
  `model_unavailable`, `other`;
- limit `scope`: `gateway`, `api_key`, `provider_account`, `other`; limit
  `reason`: `concurrency`, `request_rate`, `token_rate`, `request_budget`,
  `token_budget`, `cost_budget`, `other`;
- stream `outcome`: `completed`, `client_canceled`, `upstream_error`,
  `server_error`, `other`; and
- usage-observation `outcome`: `priced`, `unpriced`.

The usage counters are updated only after the final observation is known.
`cached_input` and `reasoning` are subdimensions and do not replace `input` or
`output`. No `total` token type is emitted because it would duplicate data.

## Provider And Persistence Metrics

| Metric | Type and unit | Labels | Max series | Operator use case |
| --- | --- | --- | ---: | --- |
| `n2api_provider_accounts` | Gauge, accounts | `account_type`, `provider_state` | 24 | Aggregate schedulable and blocked account inventory. |
| `n2api_provider_refresh_attempts_total` | Counter, attempts | `mode`, `outcome` | 16 | Manual, automatic, and rejected-token refresh reliability. |
| `n2api_request_log_writes_total` | Counter, writes | `outcome` | 2 | Detect loss of durable request attribution. |
| `n2api_system_event_writes_total` | Counter, writes | `outcome` | 2 | Detect audit/event persistence failures. |

Refresh `mode` is `manual`, `automatic`, `rejected_token`, or `other`; refresh
`outcome` is `success`, `failure`, `skipped`, or `other`. Persistence write
`outcome` is only `success` or `failure`. Provider names, account IDs, model
names, System Event categories/actions, and raw database errors are omitted.

## Background Task Metrics

| Metric | Type and unit | Labels | Max series | Operator use case |
| --- | --- | --- | ---: | --- |
| `n2api_background_task_runs_total` | Counter, runs | `task`, `outcome` | 30 | Success/failure/partial/skip/cancel rates per fixed task. |
| `n2api_background_task_duration_seconds` | Histogram, seconds | `task` | 60 | Runtime and stuck-task diagnosis without raw error labels. |
| `n2api_background_task_running` | Gauge, boolean `0` or `1` | `task` | 5 | Current in-process task activity. |
| `n2api_background_task_last_success_timestamp_seconds` | Gauge, Unix seconds | `task` | 5 | Alert on stale successful execution. |
| `n2api_background_task_last_failure_timestamp_seconds` | Gauge, Unix seconds | `task` | 5 | Correlate recent failures with System Events. |

Task `outcome` is `success`, `failure`, `partial`, `skipped`, `canceled`, or
`other`. The duration histogram uses buckets `0.1`, `0.5`, `1`, `5`, `10`,
`30`, `60`, `300`, `900`, and `3600` seconds. Last timestamps remain `0` until
the corresponding outcome occurs. Full failure detail stays in sanitized logs
and System Events.

## PostgreSQL Pool Metrics

These metrics are direct snapshots/deltas from `pgxpool.Stat`; they never run a
query or expose the database URL, SQL, database/user name, or query text.

| Metric | Type and unit | Labels | Max series | Operator use case |
| --- | --- | --- | ---: | --- |
| `n2api_database_pool_connections` | Gauge, connections | `state` | 4 | `total`, `acquired`, `idle`, and configured `max` capacity. |
| `n2api_database_pool_acquires_total` | Counter, acquires | none | 1 | Pool acquisition throughput. |
| `n2api_database_pool_acquire_duration_seconds_total` | Counter, seconds | none | 1 | Derive mean acquisition time with acquires. |
| `n2api_database_pool_empty_acquires_total` | Counter, acquires | none | 1 | Requests that had to wait for a connection. |
| `n2api_database_pool_canceled_acquires_total` | Counter, acquires | none | 1 | Canceled pool waits. |
| `n2api_database_pool_new_connections_total` | Counter, connections | none | 1 | Connection churn. |
| `n2api_database_pool_destroyed_connections_total` | Counter, connections | `reason` | 3 | Max-lifetime, max-idle, or other pool churn. |

The connection `state` allowlist is `total`, `acquired`, `idle`, `max`.
Destroyed-connection `reason` is `max_lifetime`, `max_idle`, or `other`.

## Reserved Alert Metrics

Alerting does not exist yet. The following names and labels are reserved but
must not be registered until the System Event Alerting plan supplies a bounded
dispatcher and exact adapter outcomes:

| Metric | Type and unit | Labels | Max series | Operator use case |
| --- | --- | --- | ---: | --- |
| `n2api_alert_queue_depth` | Gauge, notifications | none | 1 | Queue saturation. |
| `n2api_alert_notifications_total` | Counter, notifications | `adapter`, `outcome` | 24 | Delivered, failed, dropped, deduplicated, and recovery outcomes. |
| `n2api_alert_delivery_duration_seconds` | Histogram, seconds | `adapter` | 48 | Destination latency independent of gateway requests. |

Alert `outcome` is reserved as `delivered`, `failed`, `dropped`,
`deduplicated`, `recovery`, or `other`. Delivery duration buckets are `0.05`,
`0.1`, `0.25`, `0.5`, `1`, `2.5`, `5`, `10`, `20`, and `30` seconds.

## Budget Accounting

The worst-case active N2API-owned series in the implemented sections is 1,316:

- gateway: 1,155;
- provider and persistence: 44;
- background tasks: 105; and
- PostgreSQL pool: 12.

Reserved alert metrics add at most 73 series after their own plan is complete,
keeping the contract below the 1,600 owned-series cap. Standard Go runtime and
process collectors must fit inside the separate 2,000-series complete-scrape
cap. Tests must gather the registry after exercising every allowed label value
and fail when either cap is exceeded.

## Exposure And Implementation Gate

The accepted implementation uses `github.com/prometheus/client_golang`, a
private registry, and a separate listener that is disabled by default. The
listener defaults to loopback; any non-loopback bind requires an
operator-configured bearer token. The public gateway router never exposes the
handler. Disabled startup performs no registration, listener setup, or metrics
inventory query.

Implementation acceptance must also prove:

- every label passes through a closed normalization function with `other` as
  the only fallback;
- E2E traffic changes the expected counters/histograms while gateway responses
  and streaming remain unchanged;
- a scrape contains none of the prohibited canary values; and
- collector failure or scrape cancellation cannot block a gateway request.

## References

- [Prometheus metric and label naming](https://prometheus.io/docs/practices/naming/)
- [Prometheus client-library writing guidance](https://prometheus.io/docs/instrumenting/writing_clientlibs/)
- [Prometheus exposition formats and metric types](https://prometheus.io/docs/instrumenting/exposition_formats/)

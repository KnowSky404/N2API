package metrics

import (
	"net/http"
	"strconv"
	"sync"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/collectors"
	"github.com/prometheus/client_golang/prometheus/promhttp"
)

const (
	MaxOwnedSeries            = 1600
	MaxScrapeSeries           = 2000
	MaxInitializedOwnedSeries = 1516
)

var (
	routes            = []string{"models", "chat_completions", "responses_create", "responses_retrieve", "responses_input_items", "other"}
	statusClasses     = []string{"2xx", "4xx", "5xx", "other"}
	boolValues        = []string{"true", "false"}
	accountTypes      = []string{"codex_oauth", "api_key", "none", "other"}
	usageSources      = []string{"responses", "chat_completions", "stream", "gemini_usage_metadata", "anthropic_usage", "json", "missing", "other"}
	tokenTypes        = []string{"input", "output", "cached_input", "reasoning"}
	providerStates    = []string{"active", "disabled", "rate_limited", "circuit_open", "expired", "other"}
	tasks             = []string{"provider_auto_test", "request_log_retention", "system_event_retention", "api_key_purge", "response_affinity_retention", "api_key_budget_monitor", "routing_exhaustion_projector", "other"}
	taskOutcomes      = []string{"success", "failure", "partial", "skipped", "canceled", "other"}
	upstreamOutcomes  = []string{"success", "http_error", "transport_error", "refresh_retry", "canceled", "other"}
	fallbackReasons   = []string{"account_concurrency", "transport_error", "retryable_status", "other"}
	routingReasons    = []string{"routing_pool_disabled", "routing_pool_unavailable", "routing_pool_empty", "routing_pool_exhausted", "routing_pool_cycle", "provider_not_connected", "provider_not_configured", "provider_accounts_disabled", "provider_accounts_unavailable", "model_unavailable", "other"}
	limitScopes       = []string{"gateway", "api_key", "provider_account", "other"}
	limitReasons      = []string{"concurrency", "request_rate", "token_rate", "request_budget", "token_budget", "cost_budget", "other"}
	streamOutcomes    = []string{"completed", "client_canceled", "upstream_error", "server_error", "other"}
	refreshModes      = []string{"manual", "automatic", "rejected_token", "other"}
	refreshOutcomes   = []string{"success", "failure", "skipped", "other"}
	persistenceStates = []string{"success", "failure"}
	alertAdapters     = []string{"generic_webhook", "ntfy", "gotify", "other"}
	alertOutcomes     = []string{"delivered", "failed", "dropped", "deduplicated", "recovery", "other"}
	readinessParts    = []string{"overall", "database", "static_assets", "other"}
)

type Registry struct {
	registry *prometheus.Registry

	gatewayRequests    *prometheus.CounterVec
	gatewayDuration    *prometheus.HistogramVec
	gatewayActive      prometheus.Gauge
	upstreamAttempts   *prometheus.CounterVec
	fallbacks          *prometheus.CounterVec
	routingFailures    *prometheus.CounterVec
	limitRejections    *prometheus.CounterVec
	streams            *prometheus.CounterVec
	usageObservations  *prometheus.CounterVec
	tokens             *prometheus.CounterVec
	estimatedCost      prometheus.Counter
	providerAccounts   *prometheus.GaugeVec
	providerRefreshes  *prometheus.CounterVec
	requestLogWrites   *prometheus.CounterVec
	systemEventWrites  *prometheus.CounterVec
	taskRuns           *prometheus.CounterVec
	taskDuration       *prometheus.HistogramVec
	taskRunning        *prometheus.GaugeVec
	taskLastSuccess    *prometheus.GaugeVec
	taskLastFailure    *prometheus.GaugeVec
	alertQueueDepth    prometheus.Gauge
	alertNotifications *prometheus.CounterVec
	alertDuration      *prometheus.HistogramVec
	readiness          *prometheus.GaugeVec
	providerAccountsMu sync.Mutex
}

type ProviderAccount struct {
	AccountType string
	State       string
}

func New(pool *pgxpool.Pool) *Registry {
	r := &Registry{registry: prometheus.NewRegistry()}
	r.gatewayRequests = prometheus.NewCounterVec(prometheus.CounterOpts{Name: "n2api_gateway_requests_total", Help: "Total supported gateway requests."}, []string{"route", "status_class", "stream", "account_type"})
	r.gatewayDuration = prometheus.NewHistogramVec(prometheus.HistogramOpts{Name: "n2api_gateway_request_duration_seconds", Help: "End-to-end supported gateway request duration.", Buckets: []float64{0.01, 0.025, 0.05, 0.1, 0.25, 0.5, 1, 2.5, 5, 10, 30, 60, 120, 300, 900}}, []string{"route", "status_class", "stream"})
	r.gatewayActive = prometheus.NewGauge(prometheus.GaugeOpts{Name: "n2api_gateway_active_requests", Help: "Current supported gateway requests."})
	r.upstreamAttempts = prometheus.NewCounterVec(prometheus.CounterOpts{Name: "n2api_gateway_upstream_attempts_total", Help: "Total gateway upstream attempts."}, []string{"account_type", "outcome"})
	r.fallbacks = prometheus.NewCounterVec(prometheus.CounterOpts{Name: "n2api_gateway_fallbacks_total", Help: "Total gateway account fallbacks."}, []string{"reason"})
	r.routingFailures = prometheus.NewCounterVec(prometheus.CounterOpts{Name: "n2api_gateway_routing_failures_total", Help: "Total bounded gateway routing failures."}, []string{"reason"})
	r.limitRejections = prometheus.NewCounterVec(prometheus.CounterOpts{Name: "n2api_gateway_limit_rejections_total", Help: "Total gateway limit rejections."}, []string{"scope", "reason"})
	r.streams = prometheus.NewCounterVec(prometheus.CounterOpts{Name: "n2api_gateway_streams_total", Help: "Total completed gateway stream lifecycles."}, []string{"route", "outcome"})
	r.usageObservations = prometheus.NewCounterVec(prometheus.CounterOpts{Name: "n2api_gateway_usage_observations_total", Help: "Total finalized gateway usage observations."}, []string{"usage_source", "outcome"})
	r.tokens = prometheus.NewCounterVec(prometheus.CounterOpts{Name: "n2api_gateway_tokens_total", Help: "Aggregate observed gateway tokens."}, []string{"token_type", "usage_source"})
	r.estimatedCost = prometheus.NewCounter(prometheus.CounterOpts{Name: "n2api_gateway_estimated_cost_usd_total", Help: "Aggregate matched estimated gateway cost in USD."})
	r.providerAccounts = prometheus.NewGaugeVec(prometheus.GaugeOpts{Name: "n2api_provider_accounts", Help: "Current provider account inventory."}, []string{"account_type", "provider_state"})
	r.providerRefreshes = prometheus.NewCounterVec(prometheus.CounterOpts{Name: "n2api_provider_refresh_attempts_total", Help: "Total provider credential refresh attempts."}, []string{"mode", "outcome"})
	r.requestLogWrites = prometheus.NewCounterVec(prometheus.CounterOpts{Name: "n2api_request_log_writes_total", Help: "Total Request Log persistence attempts."}, []string{"outcome"})
	r.systemEventWrites = prometheus.NewCounterVec(prometheus.CounterOpts{Name: "n2api_system_event_writes_total", Help: "Total System Event persistence attempts."}, []string{"outcome"})
	r.taskRuns = prometheus.NewCounterVec(prometheus.CounterOpts{Name: "n2api_background_task_runs_total", Help: "Total bounded background task runs."}, []string{"task", "outcome"})
	r.taskDuration = prometheus.NewHistogramVec(prometheus.HistogramOpts{Name: "n2api_background_task_duration_seconds", Help: "Background task run duration.", Buckets: []float64{0.1, 0.5, 1, 5, 10, 30, 60, 300, 900, 3600}}, []string{"task"})
	r.taskRunning = prometheus.NewGaugeVec(prometheus.GaugeOpts{Name: "n2api_background_task_running", Help: "Whether a bounded background task is running."}, []string{"task"})
	r.taskLastSuccess = prometheus.NewGaugeVec(prometheus.GaugeOpts{Name: "n2api_background_task_last_success_timestamp_seconds", Help: "Unix timestamp of the last successful task run."}, []string{"task"})
	r.taskLastFailure = prometheus.NewGaugeVec(prometheus.GaugeOpts{Name: "n2api_background_task_last_failure_timestamp_seconds", Help: "Unix timestamp of the last failed task run."}, []string{"task"})
	r.alertQueueDepth = prometheus.NewGauge(prometheus.GaugeOpts{Name: "n2api_alert_queue_depth", Help: "Current in-process alert notification queue depth."})
	r.alertNotifications = prometheus.NewCounterVec(prometheus.CounterOpts{Name: "n2api_alert_notifications_total", Help: "Total bounded alert notification outcomes."}, []string{"adapter", "outcome"})
	r.alertDuration = prometheus.NewHistogramVec(prometheus.HistogramOpts{Name: "n2api_alert_delivery_duration_seconds", Help: "Alert destination delivery duration.", Buckets: []float64{0.05, 0.1, 0.25, 0.5, 1, 2.5, 5, 10, 20, 30}}, []string{"adapter"})
	r.readiness = prometheus.NewGaugeVec(prometheus.GaugeOpts{Name: "n2api_readiness", Help: "Last observed readiness result by fixed component."}, []string{"component"})

	r.registry.MustRegister(
		collectors.NewGoCollector(),
		collectors.NewProcessCollector(collectors.ProcessCollectorOpts{}),
		collectors.NewBuildInfoCollector(),
		r.gatewayRequests, r.gatewayDuration, r.gatewayActive, r.upstreamAttempts,
		r.fallbacks, r.routingFailures, r.limitRejections, r.streams,
		r.usageObservations, r.tokens, r.estimatedCost, r.providerAccounts,
		r.providerRefreshes, r.requestLogWrites, r.systemEventWrites, r.taskRuns,
		r.taskDuration, r.taskRunning, r.taskLastSuccess, r.taskLastFailure,
		r.alertQueueDepth, r.alertNotifications, r.alertDuration, r.readiness,
		newPoolCollector(pool),
	)
	r.initializeBoundedSeries()
	return r
}

func (r *Registry) initializeBoundedSeries() {
	for _, route := range routes {
		for _, status := range statusClasses {
			for _, stream := range boolValues {
				r.gatewayDuration.WithLabelValues(route, status, stream)
				for _, accountType := range accountTypes {
					r.gatewayRequests.WithLabelValues(route, status, stream, accountType)
				}
			}
		}
		for _, outcome := range streamOutcomes {
			r.streams.WithLabelValues(route, outcome)
		}
	}
	for _, accountType := range accountTypes {
		for _, outcome := range upstreamOutcomes {
			r.upstreamAttempts.WithLabelValues(accountType, outcome)
		}
		for _, state := range providerStates {
			r.providerAccounts.WithLabelValues(accountType, state).Set(0)
		}
	}
	for _, reason := range fallbackReasons {
		r.fallbacks.WithLabelValues(reason)
	}
	for _, reason := range routingReasons {
		r.routingFailures.WithLabelValues(reason)
	}
	for _, scope := range limitScopes {
		for _, reason := range limitReasons {
			r.limitRejections.WithLabelValues(scope, reason)
		}
	}
	for _, source := range usageSources {
		for _, outcome := range []string{"priced", "unpriced"} {
			r.usageObservations.WithLabelValues(source, outcome)
		}
		for _, tokenType := range tokenTypes {
			r.tokens.WithLabelValues(tokenType, source)
		}
	}
	for _, mode := range refreshModes {
		for _, outcome := range refreshOutcomes {
			r.providerRefreshes.WithLabelValues(mode, outcome)
		}
	}
	for _, outcome := range persistenceStates {
		r.requestLogWrites.WithLabelValues(outcome)
		r.systemEventWrites.WithLabelValues(outcome)
	}
	for _, task := range tasks {
		for _, outcome := range taskOutcomes {
			r.taskRuns.WithLabelValues(task, outcome)
		}
		r.taskDuration.WithLabelValues(task)
		r.taskRunning.WithLabelValues(task).Set(0)
		r.taskLastSuccess.WithLabelValues(task).Set(0)
		r.taskLastFailure.WithLabelValues(task).Set(0)
	}
	r.alertQueueDepth.Set(0)
	for _, adapter := range alertAdapters {
		for _, outcome := range alertOutcomes {
			r.alertNotifications.WithLabelValues(adapter, outcome)
		}
		r.alertDuration.WithLabelValues(adapter)
	}
	for _, component := range readinessParts {
		r.readiness.WithLabelValues(component).Set(0)
	}
}

func (r *Registry) Handler() http.Handler {
	return promhttp.HandlerFor(r.registry, promhttp.HandlerOpts{EnableOpenMetrics: false, MaxRequestsInFlight: 2})
}

func (r *Registry) Gatherer() prometheus.Gatherer { return r.registry }

func (r *Registry) GatewayRequestStarted() {
	if r != nil {
		r.gatewayActive.Inc()
	}
}

func (r *Registry) GatewayRequestFinished(route string, statusCode int, stream bool, accountType string, duration time.Duration) {
	if r == nil {
		return
	}
	route = normalizeRoute(route)
	status := normalizeStatusClass(statusCode)
	streamValue := strconv.FormatBool(stream)
	r.gatewayRequests.WithLabelValues(route, status, streamValue, normalizeAccountType(accountType)).Inc()
	r.gatewayDuration.WithLabelValues(route, status, streamValue).Observe(duration.Seconds())
	r.gatewayActive.Dec()
}

func (r *Registry) ObserveUpstreamAttempt(accountType, outcome string) {
	if r != nil {
		r.upstreamAttempts.WithLabelValues(normalizeAccountType(accountType), normalize(outcome, upstreamOutcomes)).Inc()
	}
}

func (r *Registry) ObserveFallback(reason string) {
	if r != nil {
		r.fallbacks.WithLabelValues(normalize(reason, fallbackReasons)).Inc()
	}
}

func (r *Registry) ObserveRoutingFailure(reason string) {
	if r != nil {
		r.routingFailures.WithLabelValues(normalize(reason, routingReasons)).Inc()
	}
}

func (r *Registry) ObserveLimitRejection(scope, reason string) {
	if r != nil {
		r.limitRejections.WithLabelValues(normalize(scope, limitScopes), normalize(reason, limitReasons)).Inc()
	}
}

func (r *Registry) ObserveStream(route, outcome string) {
	if r != nil {
		r.streams.WithLabelValues(normalizeRoute(route), normalize(outcome, streamOutcomes)).Inc()
	}
}

func (r *Registry) ObserveUsage(source string, priced bool, input, output, cachedInput, reasoning int, costMicrousd int64) {
	if r == nil {
		return
	}
	source = normalize(source, usageSources)
	outcome := "unpriced"
	if priced {
		outcome = "priced"
	}
	r.usageObservations.WithLabelValues(source, outcome).Inc()
	for tokenType, value := range map[string]int{"input": input, "output": output, "cached_input": cachedInput, "reasoning": reasoning} {
		if value > 0 {
			r.tokens.WithLabelValues(tokenType, source).Add(float64(value))
		}
	}
	if priced && costMicrousd > 0 {
		r.estimatedCost.Add(float64(costMicrousd) / 1_000_000)
	}
}

func (r *Registry) ObserveProviderRefresh(mode, outcome string) {
	if r != nil {
		r.providerRefreshes.WithLabelValues(normalize(mode, refreshModes), normalize(outcome, refreshOutcomes)).Inc()
	}
}

func (r *Registry) ObserveRequestLogWrite(err error) {
	if r != nil {
		r.requestLogWrites.WithLabelValues(persistenceOutcome(err)).Inc()
	}
}

func (r *Registry) ObserveSystemEventWrite(err error) {
	if r != nil {
		r.systemEventWrites.WithLabelValues(persistenceOutcome(err)).Inc()
	}
}

func (r *Registry) BeginBackgroundTask(task string) func(string) {
	if r == nil {
		return func(string) {}
	}
	task = normalize(task, tasks)
	started := time.Now()
	r.taskRunning.WithLabelValues(task).Set(1)
	return func(outcome string) {
		outcome = normalize(outcome, taskOutcomes)
		r.taskRunning.WithLabelValues(task).Set(0)
		r.observeBackgroundTaskRun(task, outcome, time.Since(started))
	}
}

func (r *Registry) ObserveBackgroundTaskRun(task, outcome string, duration time.Duration) {
	if r == nil {
		return
	}
	r.observeBackgroundTaskRun(normalize(task, tasks), normalize(outcome, taskOutcomes), duration)
}

func (r *Registry) observeBackgroundTaskRun(task, outcome string, duration time.Duration) {
	r.taskDuration.WithLabelValues(task).Observe(max(0, duration.Seconds()))
	r.taskRuns.WithLabelValues(task, outcome).Inc()
	now := float64(time.Now().Unix())
	switch outcome {
	case "success":
		r.taskLastSuccess.WithLabelValues(task).Set(now)
	case "failure", "partial":
		r.taskLastFailure.WithLabelValues(task).Set(now)
	}
}

func (r *Registry) UpdateProviderAccounts(accounts []ProviderAccount) {
	if r == nil {
		return
	}
	r.providerAccountsMu.Lock()
	defer r.providerAccountsMu.Unlock()
	for _, accountType := range accountTypes {
		for _, state := range providerStates {
			r.providerAccounts.WithLabelValues(accountType, state).Set(0)
		}
	}
	counts := make(map[[2]string]float64)
	for _, account := range accounts {
		counts[[2]string{normalizeAccountType(account.AccountType), normalize(account.State, providerStates)}]++
	}
	for labels, count := range counts {
		r.providerAccounts.WithLabelValues(labels[0], labels[1]).Set(count)
	}
}

func (r *Registry) SetAlertQueueDepth(depth int) {
	if r == nil {
		return
	}
	r.alertQueueDepth.Set(float64(max(0, depth)))
}

func (r *Registry) AddAlertQueueDepth(delta int) {
	if r == nil {
		return
	}
	r.alertQueueDepth.Add(float64(delta))
}

func (r *Registry) ObserveAlertNotification(adapter, outcome string) {
	if r == nil {
		return
	}
	r.alertNotifications.WithLabelValues(normalize(adapter, alertAdapters), normalize(outcome, alertOutcomes)).Inc()
}

func (r *Registry) ObserveAlertDeliveryDuration(adapter string, duration time.Duration) {
	if r == nil {
		return
	}
	r.alertDuration.WithLabelValues(normalize(adapter, alertAdapters)).Observe(max(0, duration.Seconds()))
}

func (r *Registry) SetReadiness(component string, ready bool) {
	if r == nil {
		return
	}
	value := 0.0
	if ready {
		value = 1
	}
	r.readiness.WithLabelValues(normalize(component, readinessParts)).Set(value)
}

func persistenceOutcome(err error) string {
	if err == nil {
		return "success"
	}
	return "failure"
}

package metrics

import (
	"context"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	dto "github.com/prometheus/client_model/go"
)

func TestNormalizationUsesClosedAllowlists(t *testing.T) {
	if got := normalizeRoute("/v1/responses/resp_secret"); got != "responses_retrieve" {
		t.Fatalf("route = %q", got)
	}
	if got := normalizeRoute("/v1/responses/resp_secret/input_items"); got != "responses_input_items" {
		t.Fatalf("input items route = %q", got)
	}
	if got := normalizeRoute("/v1/responses/input_items"); got != "responses_retrieve" {
		t.Fatalf("response ID route = %q", got)
	}
	if got := normalizeRoute("/v1/private/canary"); got != "other" {
		t.Fatalf("route = %q", got)
	}
	if got := normalizeAccountType("api_upstream"); got != "api_key" {
		t.Fatalf("account type = %q", got)
	}
	if got := normalize("owner@example.com", usageSources); got != "other" {
		t.Fatalf("unknown usage source = %q", got)
	}
}

func TestRegistryStaysWithinSeriesBudgetsAndExcludesCanaries(t *testing.T) {
	r := New(nil)
	r.GatewayRequestStarted()
	r.GatewayRequestFinished("/v1/responses/resp_canary", 200, true, "owner@example.com", time.Millisecond)
	r.ObserveUsage("secret-source-canary", false, 1, 2, 3, 4, 0)
	r.UpdateProviderAccounts([]ProviderAccount{{AccountType: "secret-account-canary", State: "secret-state-canary"}})
	r.SetAlertQueueDepth(2)
	r.ObserveAlertNotification("secret-adapter-canary", "secret-outcome-canary")
	r.ObserveAlertDeliveryDuration("secret-adapter-canary", time.Millisecond)
	r.SetReadiness("secret-component-canary", true)
	families, err := r.Gatherer().Gather()
	if err != nil {
		t.Fatal(err)
	}
	series := 0
	owned := 0
	for _, family := range families {
		familySeries := metricFamilySeries(family)
		series += familySeries
		if strings.HasPrefix(family.GetName(), "n2api_") {
			owned += familySeries
		}
	}
	if owned != MaxInitializedOwnedSeries {
		t.Fatalf("owned series = %d, want exact initialized budget = %d", owned, MaxInitializedOwnedSeries)
	}
	if series >= MaxScrapeSeries {
		t.Fatalf("scrape series = %d, limit = %d", series, MaxScrapeSeries)
	}
	recorder := httptest.NewRecorder()
	r.Handler().ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/metrics", nil))
	body := recorder.Body.String()
	for _, canary := range []string{"resp_canary", "owner@example.com", "secret-source-canary", "secret-account-canary", "secret-state-canary", "secret-adapter-canary", "secret-outcome-canary", "secret-component-canary"} {
		if strings.Contains(body, canary) {
			t.Fatalf("scrape contains prohibited canary %q", canary)
		}
	}
}

func metricFamilySeries(family *dto.MetricFamily) int {
	series := 0
	for _, metric := range family.Metric {
		switch family.GetType() {
		case dto.MetricType_HISTOGRAM:
			// Prometheus exposition adds an implicit +Inf bucket to the finite
			// buckets represented in the protobuf, plus _sum and _count.
			series += len(metric.GetHistogram().Bucket) + 3
		case dto.MetricType_SUMMARY:
			series += len(metric.GetSummary().Quantile) + 2
		default:
			series++
		}
	}
	return series
}

func TestMetricsServerBearerAuthentication(t *testing.T) {
	server := httptest.NewServer(bearerAuth("secret", http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { _, _ = io.WriteString(w, "ok") })))
	defer server.Close()
	response, err := http.Get(server.URL)
	if err != nil {
		t.Fatal(err)
	}
	_ = response.Body.Close()
	if response.StatusCode != http.StatusUnauthorized {
		t.Fatalf("status = %d", response.StatusCode)
	}
	req, _ := http.NewRequest(http.MethodGet, server.URL, nil)
	req.Header.Set("Authorization", "Bearer secret")
	response, err = http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		t.Fatalf("status = %d", response.StatusCode)
	}
}

func TestMetricsServerOnlyServesMetricsPath(t *testing.T) {
	server := NewHTTPServer("127.0.0.1:0", "", http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = io.WriteString(w, "metrics")
	}), nil)
	metricsResponse := httptest.NewRecorder()
	server.Handler.ServeHTTP(metricsResponse, httptest.NewRequest(http.MethodGet, "/metrics", nil))
	if metricsResponse.Code != http.StatusOK || metricsResponse.Body.String() != "metrics" {
		t.Fatalf("metrics response = %d %q", metricsResponse.Code, metricsResponse.Body.String())
	}
	unknownResponse := httptest.NewRecorder()
	server.Handler.ServeHTTP(unknownResponse, httptest.NewRequest(http.MethodGet, "/anything-else", nil))
	if unknownResponse.Code != http.StatusNotFound {
		t.Fatalf("unknown path status = %d", unknownResponse.Code)
	}
}

func TestMetricsServerListenerLifecycleAndBearerAuthentication(t *testing.T) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	server := NewHTTPServer(listener.Addr().String(), "metrics-secret", New(nil).Handler(), context.Background())
	serveErrors := make(chan error, 1)
	go func() { serveErrors <- server.Serve(listener) }()

	url := "http://" + listener.Addr().String() + "/metrics"
	response, err := http.Get(url)
	if err != nil {
		t.Fatal(err)
	}
	_ = response.Body.Close()
	if response.StatusCode != http.StatusUnauthorized {
		t.Fatalf("unauthenticated status = %d", response.StatusCode)
	}
	req, _ := http.NewRequest(http.MethodGet, url, nil)
	req.Header.Set("Authorization", "Bearer metrics-secret")
	response, err = http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	body, _ := io.ReadAll(response.Body)
	_ = response.Body.Close()
	if response.StatusCode != http.StatusOK || !strings.Contains(string(body), "n2api_gateway_active_requests") || !strings.Contains(string(body), "n2api_database_pool_connections") {
		t.Fatalf("authenticated scrape = %d %q", response.StatusCode, body)
	}

	shutdownCtx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	if err := server.Shutdown(shutdownCtx); err != nil {
		t.Fatal(err)
	}
	if err := <-serveErrors; err != nil && err != http.ErrServerClosed {
		t.Fatalf("Serve error = %v", err)
	}
}

func TestMetricsServerReportsBindFailure(t *testing.T) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()
	server := NewHTTPServer(listener.Addr().String(), "", New(nil).Handler(), context.Background())
	if err := server.ListenAndServe(); err == nil || err == http.ErrServerClosed {
		t.Fatalf("ListenAndServe error = %v, want bind failure", err)
	}
}

type blockingCollector struct {
	started chan struct{}
	release chan struct{}
	once    sync.Once
	desc    *prometheus.Desc
}

func (c *blockingCollector) Describe(ch chan<- *prometheus.Desc) { ch <- c.desc }
func (c *blockingCollector) Collect(ch chan<- prometheus.Metric) {
	c.once.Do(func() { close(c.started) })
	<-c.release
	ch <- prometheus.MustNewConstMetric(c.desc, prometheus.GaugeValue, 1)
}

func TestConcurrentScrapeCancellationCannotBlockMetricUpdates(t *testing.T) {
	r := New(nil)
	collector := &blockingCollector{
		started: make(chan struct{}), release: make(chan struct{}),
		desc: prometheus.NewDesc("n2api_test_blocking_collector", "Test-only blocking collector.", nil, nil),
	}
	r.registry.MustRegister(collector)
	requestCtx, cancelRequest := context.WithCancel(context.Background())
	req := httptest.NewRequest(http.MethodGet, "/metrics", nil).WithContext(requestCtx)
	scrapeDone := make(chan struct{})
	go func() {
		r.Handler().ServeHTTP(httptest.NewRecorder(), req)
		close(scrapeDone)
	}()
	<-collector.started

	updateDone := make(chan struct{})
	go func() {
		for range 100 {
			r.GatewayRequestStarted()
			r.GatewayRequestFinished("/v1/models", http.StatusOK, false, "api_key", time.Millisecond)
			r.ObserveAlertNotification("ntfy", "delivered")
		}
		close(updateDone)
	}()
	select {
	case <-updateDone:
	case <-time.After(time.Second):
		t.Fatal("metric updates blocked behind scrape collector")
	}
	cancelRequest()
	close(collector.release)
	select {
	case <-scrapeDone:
	case <-time.After(time.Second):
		t.Fatal("canceled scrape did not terminate after collector release")
	}
}

package gateway

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/KnowSky404/N2API/backend/internal/admin"
	n2metrics "github.com/KnowSky404/N2API/backend/internal/metrics"
	"github.com/KnowSky404/N2API/backend/internal/provider"
	dto "github.com/prometheus/client_model/go"
	"github.com/prometheus/common/expfmt"
	"github.com/prometheus/common/model"
)

func TestMetricsAcceptanceTracksCanceledSSELifecycle(t *testing.T) {
	registry := n2metrics.New(nil)
	metricsServer := httptest.NewServer(n2metrics.NewHTTPServer("127.0.0.1:0", "", registry.Handler(), context.Background()).Handler)
	t.Cleanup(metricsServer.Close)

	body := newMetricsAcceptanceBlockingBody()
	client := &http.Client{Transport: metricsAcceptanceRoundTripper(func(request *http.Request) (*http.Response, error) {
		return &http.Response{
			StatusCode: http.StatusOK,
			Header:     http.Header{"Content-Type": []string{"text/event-stream"}},
			Body:       body,
			Request:    request,
		}, nil
	})}
	accounts := metricsAcceptanceAccountProvider{account: SelectedAccount{
		AccountID: 7, AccountType: provider.AccountTypeAPIUpstream, AuthorizationToken: "upstream-token",
	}}
	proxy := NewProxyWithClient(metricsAcceptanceAuthenticator{}, accounts, Config{
		Metrics:                registry,
		UpstreamSSEIdleTimeout: time.Second,
	}, client)
	t.Cleanup(proxy.Close)

	ctx, cancel := context.WithCancel(context.Background())
	request := httptest.NewRequest(http.MethodPost, "/v1/responses", strings.NewReader(`{"model":"gpt-test","stream":true}`)).WithContext(ctx)
	request.Header.Set("Authorization", "Bearer n2api_client_secret")
	request.Header.Set("Content-Type", "application/json")
	recorder := httptest.NewRecorder()
	done := make(chan struct{})
	go func() {
		proxy.ServeHTTP(recorder, request)
		close(done)
	}()

	select {
	case <-body.readStarted:
	case <-time.After(time.Second):
		t.Fatal("stream copy did not start")
	}
	activeFamilies := scrapeMetricsAcceptanceFamilies(t, metricsServer.URL+"/metrics")
	if got := metricsAcceptanceGauge(t, activeFamilies, "n2api_gateway_active_requests", nil); got != 1 {
		t.Fatalf("active requests during stream = %v, want 1", got)
	}

	time.Sleep(20 * time.Millisecond)
	cancel()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("canceled stream did not unwind")
	}
	if !body.isClosed() {
		t.Fatal("canceled stream body was not closed")
	}
	if recorder.Code != http.StatusOK || recorder.Body.Len() != 0 {
		t.Fatalf("response after cancellation = status:%d body:%q", recorder.Code, recorder.Body.String())
	}

	families := scrapeMetricsAcceptanceFamilies(t, metricsServer.URL+"/metrics")
	requestLabels := map[string]string{
		"route": "responses_create", "status_class": "2xx", "stream": "true", "account_type": "api_key",
	}
	if got := metricsAcceptanceCounter(t, families, "n2api_gateway_requests_total", requestLabels); got != 1 {
		t.Fatalf("canceled stream request count = %v, want 1", got)
	}
	streamLabels := map[string]string{"route": "responses_create", "outcome": "client_canceled"}
	if got := metricsAcceptanceCounter(t, families, "n2api_gateway_streams_total", streamLabels); got != 1 {
		t.Fatalf("client-canceled stream count = %v, want 1", got)
	}
	durationLabels := map[string]string{"route": "responses_create", "status_class": "2xx", "stream": "true"}
	duration := metricsAcceptanceMetric(t, families, "n2api_gateway_request_duration_seconds", durationLabels).GetHistogram()
	if duration.GetSampleCount() != 1 || duration.GetSampleSum() < 0.02 {
		t.Fatalf("canceled stream duration = count:%d sum:%v, want one observation covering stream lifetime", duration.GetSampleCount(), duration.GetSampleSum())
	}
	if got := metricsAcceptanceGauge(t, families, "n2api_gateway_active_requests", nil); got != 0 {
		t.Fatalf("active requests after cancellation = %v, want 0", got)
	}
}

type metricsAcceptanceAuthenticator struct{}

func (metricsAcceptanceAuthenticator) AuthenticateAPIKey(context.Context, string) (admin.APIKey, error) {
	routingPoolID := int64(1)
	return admin.APIKey{ID: 42, Name: "metrics acceptance", RoutingPoolID: &routingPoolID}, nil
}

type metricsAcceptanceAccountProvider struct {
	account SelectedAccount
}

func (provider metricsAcceptanceAccountProvider) SelectAccountForModel(context.Context, string, ...int64) (SelectedAccount, error) {
	return provider.account, nil
}

func (provider metricsAcceptanceAccountProvider) SelectAccountForModelInRoutingPool(context.Context, int64, string, ...int64) (SelectedAccount, error) {
	return provider.account, nil
}

func (provider metricsAcceptanceAccountProvider) SelectAccountForModelAndSessionInRoutingPool(context.Context, int64, string, string, ...int64) (SelectedAccount, error) {
	return provider.account, nil
}

type metricsAcceptanceRoundTripper func(*http.Request) (*http.Response, error)

func (roundTrip metricsAcceptanceRoundTripper) RoundTrip(request *http.Request) (*http.Response, error) {
	return roundTrip(request)
}

type metricsAcceptanceBlockingBody struct {
	readStarted chan struct{}
	closed      chan struct{}
	startOnce   sync.Once
	closeOnce   sync.Once
}

func newMetricsAcceptanceBlockingBody() *metricsAcceptanceBlockingBody {
	return &metricsAcceptanceBlockingBody{readStarted: make(chan struct{}), closed: make(chan struct{})}
}

func (body *metricsAcceptanceBlockingBody) Read([]byte) (int, error) {
	body.startOnce.Do(func() { close(body.readStarted) })
	<-body.closed
	return 0, errors.New("acceptance stream closed")
}

func (body *metricsAcceptanceBlockingBody) Close() error {
	body.closeOnce.Do(func() { close(body.closed) })
	return nil
}

func (body *metricsAcceptanceBlockingBody) isClosed() bool {
	select {
	case <-body.closed:
		return true
	default:
		return false
	}
}

func scrapeMetricsAcceptanceFamilies(t *testing.T, target string) map[string]*dto.MetricFamily {
	t.Helper()
	client := &http.Client{Timeout: time.Second}
	response, err := client.Get(target)
	if err != nil {
		t.Fatalf("scrape metrics: %v", err)
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(response.Body)
		t.Fatalf("scrape status = %d body=%q", response.StatusCode, body)
	}
	parser := expfmt.NewTextParser(model.LegacyValidation)
	families, err := parser.TextToMetricFamilies(response.Body)
	if err != nil {
		t.Fatalf("parse metrics scrape: %v", err)
	}
	return families
}

func metricsAcceptanceCounter(t *testing.T, families map[string]*dto.MetricFamily, name string, labels map[string]string) float64 {
	t.Helper()
	return metricsAcceptanceMetric(t, families, name, labels).GetCounter().GetValue()
}

func metricsAcceptanceGauge(t *testing.T, families map[string]*dto.MetricFamily, name string, labels map[string]string) float64 {
	t.Helper()
	return metricsAcceptanceMetric(t, families, name, labels).GetGauge().GetValue()
}

func metricsAcceptanceMetric(t *testing.T, families map[string]*dto.MetricFamily, name string, labels map[string]string) *dto.Metric {
	t.Helper()
	family := families[name]
	if family == nil {
		t.Fatalf("metric family %q not found", name)
	}
	for _, metric := range family.Metric {
		if metricsAcceptanceLabelsMatch(metric, labels) {
			return metric
		}
	}
	t.Fatalf("metric %q with labels %v not found", name, labels)
	return nil
}

func metricsAcceptanceLabelsMatch(metric *dto.Metric, expected map[string]string) bool {
	if len(metric.Label) != len(expected) {
		return false
	}
	for _, label := range metric.Label {
		if expected[label.GetName()] != label.GetValue() {
			return false
		}
	}
	return true
}

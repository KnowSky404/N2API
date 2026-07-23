package metrics

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
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
	families, err := r.Gatherer().Gather()
	if err != nil {
		t.Fatal(err)
	}
	series := 0
	owned := 0
	for _, family := range families {
		series += len(family.Metric)
		if strings.HasPrefix(family.GetName(), "n2api_") {
			owned += len(family.Metric)
		}
	}
	if owned >= MaxOwnedSeries {
		t.Fatalf("owned series = %d, limit = %d", owned, MaxOwnedSeries)
	}
	if series >= MaxScrapeSeries {
		t.Fatalf("scrape series = %d, limit = %d", series, MaxScrapeSeries)
	}
	recorder := httptest.NewRecorder()
	r.Handler().ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/metrics", nil))
	body := recorder.Body.String()
	for _, canary := range []string{"resp_canary", "owner@example.com", "secret-source-canary", "secret-account-canary", "secret-state-canary"} {
		if strings.Contains(body, canary) {
			t.Fatalf("scrape contains prohibited canary %q", canary)
		}
	}
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

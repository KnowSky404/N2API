package main

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestHealthAndModels(t *testing.T) {
	handler := newMockHandler()

	health := performRequest(t, handler, http.MethodGet, "/healthz", "", "")
	if health.Code != http.StatusOK || health.Header().Get("Content-Type") != "application/json" || health.Body.String() != "{\"status\":\"ok\"}\n" {
		t.Fatalf("health response = %d %q %q", health.Code, health.Header().Get("Content-Type"), health.Body.String())
	}

	models := performRequest(t, handler, http.MethodGet, "/v1/models", "", "")
	if models.Code != http.StatusOK || models.Header().Get("Content-Type") != "application/json" {
		t.Fatalf("models response = %d %q", models.Code, models.Header().Get("Content-Type"))
	}
	var payload struct {
		Object string `json:"object"`
		Data   []struct {
			ID string `json:"id"`
		} `json:"data"`
	}
	if err := json.Unmarshal(models.Body.Bytes(), &payload); err != nil {
		t.Fatalf("decode models response: %v", err)
	}
	if payload.Object != "list" || len(payload.Data) != 1 || payload.Data[0].ID != mockModelID {
		t.Fatalf("models payload = %+v", payload)
	}
}

func TestHappyJSONProtocols(t *testing.T) {
	handler := newMockHandler()
	tests := []struct {
		name        string
		path        string
		body        string
		usageSource string
		input       float64
		output      float64
		total       float64
	}{
		{name: "chat", path: "/v1/chat/completions", body: `{"model":"gpt-5","messages":[{"role":"user","content":"secret prompt"}]}`, usageSource: "usage", input: 20, output: 5, total: 25},
		{name: "responses", path: "/v1/responses", body: `{"model":"gpt-5","input":"secret prompt"}`, usageSource: "usage", input: 11, output: 2, total: 13},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			response := performRequest(t, handler, http.MethodPost, tt.path, tt.body, "")
			if response.Code != http.StatusOK || response.Header().Get("Content-Type") != "application/json" {
				t.Fatalf("response = %d %q body=%s", response.Code, response.Header().Get("Content-Type"), response.Body.String())
			}
			if strings.Contains(response.Body.String(), "secret prompt") {
				t.Fatal("response reflected the request prompt")
			}
			var payload map[string]any
			if err := json.Unmarshal(response.Body.Bytes(), &payload); err != nil {
				t.Fatalf("decode response: %v", err)
			}
			usage, ok := payload[tt.usageSource].(map[string]any)
			if !ok {
				t.Fatalf("usage = %#v", payload[tt.usageSource])
			}
			if tt.name == "chat" {
				if usage["prompt_tokens"] != tt.input || usage["completion_tokens"] != tt.output || usage["total_tokens"] != tt.total {
					t.Fatalf("chat usage = %#v", usage)
				}
			} else if usage["input_tokens"] != tt.input || usage["output_tokens"] != tt.output || usage["total_tokens"] != tt.total {
				t.Fatalf("responses usage = %#v", usage)
			}
		})
	}
}

func TestHappySSEProtocolsFlush(t *testing.T) {
	handler := newMockHandler()
	for _, tt := range []struct {
		name string
		path string
		want string
	}{
		{name: "chat", path: "/v1/chat/completions", want: "data: [DONE]"},
		{name: "responses", path: "/v1/responses", want: "event: response.completed"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			writer := &flushRecorder{ResponseRecorder: httptest.NewRecorder()}
			request := httptest.NewRequest(http.MethodPost, tt.path, strings.NewReader(`{"model":"gpt-5","stream":true}`))
			request.Header.Set("Authorization", "Bearer "+defaultMockAPIKey)
			handler.ServeHTTP(writer, request)
			if writer.Code != http.StatusOK || writer.Header().Get("Content-Type") != "text/event-stream" {
				t.Fatalf("response = %d %q body=%s", writer.Code, writer.Header().Get("Content-Type"), writer.Body.String())
			}
			if !strings.Contains(writer.Body.String(), tt.want) || writer.flushes == 0 {
				t.Fatalf("stream body=%q flushes=%d", writer.Body.String(), writer.flushes)
			}
		})
	}
}

func TestStatusScenarios(t *testing.T) {
	handler := newMockHandler()
	for _, tt := range []struct {
		scenario string
		status   int
	}{
		{scenario: "status-401", status: http.StatusUnauthorized},
		{scenario: "status-403", status: http.StatusForbidden},
		{scenario: "status-429", status: http.StatusTooManyRequests},
		{scenario: "status-500", status: http.StatusInternalServerError},
		{scenario: "status-503", status: http.StatusServiceUnavailable},
	} {
		t.Run(tt.scenario, func(t *testing.T) {
			response := performRequest(t, handler, http.MethodPost, "/v1/responses", `{"model":"gpt-5"}`, tt.scenario)
			if response.Code != tt.status || !strings.Contains(response.Body.String(), `"code":"mock_upstream_error"`) {
				t.Fatalf("response = %d body=%s", response.Code, response.Body.String())
			}
			if tt.status == http.StatusTooManyRequests && response.Header().Get("Retry-After") != "1" {
				t.Fatalf("Retry-After = %q", response.Header().Get("Retry-After"))
			}
		})
	}
}

func TestContentTypeAndUsageScenarios(t *testing.T) {
	handler := newMockHandler()

	missing := performRequest(t, handler, http.MethodPost, "/v1/responses", `{"model":"gpt-5"}`, "missing-content-type")
	if missing.Header().Get("Content-Type") != "" {
		t.Fatalf("missing Content-Type headers = %#v", missing.Header())
	}
	wrong := performRequest(t, handler, http.MethodPost, "/v1/responses", `{"model":"gpt-5"}`, "wrong-content-type")
	if wrong.Header().Get("Content-Type") != "text/plain; charset=utf-8" {
		t.Fatalf("wrong Content-Type = %q", wrong.Header().Get("Content-Type"))
	}
	missingUsage := performRequest(t, handler, http.MethodPost, "/v1/responses", `{"model":"gpt-5"}`, "missing-usage")
	if strings.Contains(missingUsage.Body.String(), `"usage"`) {
		t.Fatalf("missing-usage body = %s", missingUsage.Body.String())
	}
	malformedUsage := performRequest(t, handler, http.MethodPost, "/v1/responses", `{"model":"gpt-5"}`, "malformed-usage")
	var payload map[string]any
	if err := json.Unmarshal(malformedUsage.Body.Bytes(), &payload); err != nil {
		t.Fatalf("malformed usage scenario must keep valid JSON: %v", err)
	}
	usage, ok := payload["usage"].(map[string]any)
	if !ok || usage["input_tokens"] != "invalid" {
		t.Fatalf("malformed usage = %#v", payload["usage"])
	}
}

func TestMissingCompletionStreamEndsCleanly(t *testing.T) {
	server := httptest.NewServer(newMockHandler())
	defer server.Close()

	request, err := http.NewRequest(http.MethodPost, server.URL+"/v1/responses", strings.NewReader(`{"model":"gpt-5","stream":true}`))
	if err != nil {
		t.Fatalf("create request: %v", err)
	}
	request.Header.Set(scenarioHeader, "missing-completion")
	request.Header.Set("Authorization", "Bearer "+defaultMockAPIKey)
	response, err := server.Client().Do(request)
	if err != nil {
		t.Fatalf("perform request: %v", err)
	}
	body, err := io.ReadAll(response.Body)
	_ = response.Body.Close()
	if err != nil {
		t.Fatalf("read stream: %v", err)
	}
	if response.StatusCode != http.StatusOK || response.Header.Get("Content-Type") != "text/event-stream" {
		t.Fatalf("response = %d %q", response.StatusCode, response.Header.Get("Content-Type"))
	}
	stream := string(body)
	if !strings.Contains(stream, "event: response.created") || !strings.Contains(stream, "event: response.output_text.delta") {
		t.Fatalf("stream missing initial events: %q", stream)
	}
	if strings.Contains(stream, "response.completed") {
		t.Fatalf("stream unexpectedly contains completion: %q", stream)
	}
}

func TestStatus503OnceFailsFirstAttemptThenSucceedsUntilReset(t *testing.T) {
	handler := newMockHandler().(*mockHandler)

	first := performRequest(t, handler, http.MethodPost, "/v1/responses", `{"model":"gpt-5","stream":true}`, "status-503-once")
	if first.Code != http.StatusServiceUnavailable || !strings.Contains(first.Body.String(), `"code":"mock_upstream_error"`) {
		t.Fatalf("first response = %d body=%s", first.Code, first.Body.String())
	}
	for attempt := 2; attempt <= 3; attempt++ {
		response := performRequest(t, handler, http.MethodPost, "/v1/responses", `{"model":"gpt-5","stream":true}`, "status-503-once")
		if response.Code != http.StatusOK || !strings.Contains(response.Body.String(), "event: response.completed") {
			t.Fatalf("attempt %d response = %d body=%s", attempt, response.Code, response.Body.String())
		}
	}

	snapshot := handler.state.snapshot()
	if len(snapshot.Entries) != 2 {
		t.Fatalf("diagnostic entries = %+v", snapshot.Entries)
	}
	counts := map[int]int64{}
	for _, entry := range snapshot.Entries {
		if entry.Scenario != "status-503-once" || entry.Method != http.MethodPost || entry.Route != "/v1/responses" {
			t.Fatalf("diagnostic entry = %+v", entry)
		}
		counts[entry.Status] = entry.Count
	}
	if counts[http.StatusServiceUnavailable] != 1 || counts[http.StatusOK] != 2 {
		t.Fatalf("diagnostic counts = %+v", counts)
	}

	reset := performRequest(t, handler, http.MethodPost, "/__mock/reset", "", "")
	if reset.Code != http.StatusNoContent {
		t.Fatalf("reset response = %d body=%s", reset.Code, reset.Body.String())
	}
	afterReset := performRequest(t, handler, http.MethodPost, "/v1/responses", `{"model":"gpt-5","stream":true}`, "status-503-once")
	if afterReset.Code != http.StatusServiceUnavailable {
		t.Fatalf("response after reset = %d body=%s", afterReset.Code, afterReset.Body.String())
	}
}

func TestScenarioAllowlistDoesNotReflectUnknownValue(t *testing.T) {
	const secretScenario = "unknown-secret-scenario"
	response := performRequest(t, newMockHandler(), http.MethodPost, "/v1/responses?token=query-secret", `{"model":"gpt-5","input":"prompt-secret"}`, secretScenario)
	if response.Code != http.StatusBadRequest || !strings.Contains(response.Body.String(), `"code":"mock_scenario_unknown"`) {
		t.Fatalf("response = %d body=%s", response.Code, response.Body.String())
	}
	if strings.Contains(response.Body.String(), secretScenario) || strings.Contains(response.Body.String(), "query-secret") || strings.Contains(response.Body.String(), "prompt-secret") {
		t.Fatalf("response reflected untrusted input: %s", response.Body.String())
	}

	whitespace := performRequest(t, newMockHandler(), http.MethodGet, "/v1/models", "", " happy ")
	if whitespace.Code != http.StatusBadRequest {
		t.Fatalf("whitespace scenario status = %d", whitespace.Code)
	}
}

func TestAuthorizationIsRequiredAndNotReflected(t *testing.T) {
	handler := newMockHandler()
	for _, authorization := range []string{"", "Bearer wrong-secret"} {
		request := httptest.NewRequest(http.MethodGet, "/v1/models", nil)
		request.Header.Set("Authorization", authorization)
		response := httptest.NewRecorder()
		handler.ServeHTTP(response, request)
		if response.Code != http.StatusUnauthorized || !strings.Contains(response.Body.String(), `"code":"mock_unauthorized"`) {
			t.Fatalf("authorization %q response = %d body=%s", authorization, response.Code, response.Body.String())
		}
		if strings.Contains(response.Body.String(), "wrong-secret") {
			t.Fatalf("response reflected authorization: %s", response.Body.String())
		}
	}
}

func TestRequestValidationAndSizeLimit(t *testing.T) {
	handler := newMockHandler()
	invalid := performRequest(t, handler, http.MethodPost, "/v1/responses", `{"model":`, "")
	if invalid.Code != http.StatusBadRequest || strings.Contains(invalid.Body.String(), `{"model":`) {
		t.Fatalf("invalid response = %d body=%s", invalid.Code, invalid.Body.String())
	}
	missingModel := performRequest(t, handler, http.MethodPost, "/v1/responses", `{}`, "")
	if missingModel.Code != http.StatusBadRequest {
		t.Fatalf("missing model response = %d body=%s", missingModel.Code, missingModel.Body.String())
	}
	oversizedBody := `{"model":"gpt-5","input":"` + strings.Repeat("x", maxRequestBodyBytes) + `"}`
	oversized := performRequest(t, handler, http.MethodPost, "/v1/responses", oversizedBody, "")
	if oversized.Code != http.StatusRequestEntityTooLarge || strings.Contains(oversized.Body.String(), strings.Repeat("x", 32)) {
		t.Fatalf("oversized response = %d body=%s", oversized.Code, oversized.Body.String())
	}
	trailing := performRequest(t, handler, http.MethodPost, "/v1/responses", `{"model":"gpt-5"} {}`, "")
	if trailing.Code != http.StatusBadRequest {
		t.Fatalf("trailing JSON response = %d body=%s", trailing.Code, trailing.Body.String())
	}
}

func TestRoutesAndMethodsUseFixedErrors(t *testing.T) {
	handler := newMockHandler()
	notFound := performRequest(t, handler, http.MethodGet, "/v1/secret-query-path", "", "")
	if notFound.Code != http.StatusNotFound || strings.Contains(notFound.Body.String(), "secret-query-path") {
		t.Fatalf("not found response = %d body=%s", notFound.Code, notFound.Body.String())
	}
	method := performRequest(t, handler, http.MethodGet, "/v1/responses", "", "")
	if method.Code != http.StatusMethodNotAllowed {
		t.Fatalf("method response = %d body=%s", method.Code, method.Body.String())
	}
}

func TestDiagnosticStateAndResetAreSanitized(t *testing.T) {
	handler := newMockHandler("authorization-secret")
	request := httptest.NewRequest(http.MethodPost, "/v1/responses?token=query-secret", strings.NewReader(`{"model":"gpt-5","input":"prompt-secret"}`))
	request.Header.Set(scenarioHeader, "status-429")
	request.Header.Set("Authorization", "Bearer authorization-secret")
	request.Header.Set("Cookie", "session=cookie-secret")
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)

	state := performRequest(t, handler, http.MethodGet, "/__mock/state", "", "")
	if state.Code != http.StatusOK {
		t.Fatalf("state response = %d body=%s", state.Code, state.Body.String())
	}
	for _, secret := range []string{"query-secret", "prompt-secret", "authorization-secret", "cookie-secret"} {
		if strings.Contains(state.Body.String(), secret) {
			t.Fatalf("diagnostic state contains %q: %s", secret, state.Body.String())
		}
	}
	var snapshot diagnosticSnapshot
	if err := json.Unmarshal(state.Body.Bytes(), &snapshot); err != nil {
		t.Fatalf("decode diagnostic state: %v", err)
	}
	if len(snapshot.Entries) != 1 {
		t.Fatalf("diagnostic entries = %+v", snapshot.Entries)
	}
	entry := snapshot.Entries[0]
	if entry.Scenario != "status-429" || entry.Method != http.MethodPost || entry.Route != "/v1/responses" || entry.Status != http.StatusTooManyRequests || entry.Count != 1 || entry.LastAt.IsZero() {
		t.Fatalf("diagnostic entry = %+v", entry)
	}

	reset := performRequest(t, handler, http.MethodPost, "/__mock/reset", "", "")
	if reset.Code != http.StatusNoContent {
		t.Fatalf("reset response = %d body=%s", reset.Code, reset.Body.String())
	}
	state = performRequest(t, handler, http.MethodGet, "/__mock/state", "", "")
	if state.Body.String() != "{\"entries\":[]}\n" {
		t.Fatalf("state after reset = %s", state.Body.String())
	}
}

func TestTimeoutBeforeHeadersHonorsCancellation(t *testing.T) {
	handler := newMockHandler().(*mockHandler)
	handler.timeoutDelay = time.Second
	server := httptest.NewServer(handler)
	defer server.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 25*time.Millisecond)
	defer cancel()
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, server.URL+"/v1/responses", strings.NewReader(`{"model":"gpt-5"}`))
	if err != nil {
		t.Fatalf("create request: %v", err)
	}
	request.Header.Set(scenarioHeader, "timeout-before-headers")
	request.Header.Set("Authorization", "Bearer "+defaultMockAPIKey)
	_, err = server.Client().Do(request)
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("request error = %v, want deadline exceeded", err)
	}
}

func TestDisconnectScenariosUseRealConnections(t *testing.T) {
	handler := newMockHandler().(*mockHandler)
	server := httptest.NewServer(handler)
	defer server.Close()

	t.Run("before headers", func(t *testing.T) {
		request, err := http.NewRequest(http.MethodPost, server.URL+"/v1/responses", strings.NewReader(`{"model":"gpt-5"}`))
		if err != nil {
			t.Fatalf("create request: %v", err)
		}
		request.Header.Set(scenarioHeader, "disconnect-before-headers")
		request.Header.Set("Authorization", "Bearer "+defaultMockAPIKey)
		_, err = server.Client().Do(request)
		if err == nil {
			t.Fatal("request unexpectedly succeeded")
		}
	})

	t.Run("after first event", func(t *testing.T) {
		request, err := http.NewRequest(http.MethodPost, server.URL+"/v1/responses", strings.NewReader(`{"model":"gpt-5","stream":true}`))
		if err != nil {
			t.Fatalf("create request: %v", err)
		}
		request.Header.Set(scenarioHeader, "disconnect-after-first-event")
		request.Header.Set("Authorization", "Bearer "+defaultMockAPIKey)
		response, err := server.Client().Do(request)
		if err != nil {
			t.Fatalf("request failed before headers: %v", err)
		}
		body, readErr := io.ReadAll(response.Body)
		_ = response.Body.Close()
		if readErr == nil {
			t.Fatal("stream read unexpectedly completed cleanly")
		}
		if !strings.Contains(string(body), "response.output_text.delta") {
			t.Fatalf("partial stream body = %q", string(body))
		}
	})

	snapshot := handler.state.snapshot()
	if len(snapshot.Entries) != 2 {
		t.Fatalf("disconnect diagnostic entries = %+v", snapshot.Entries)
	}
	statuses := map[string]int{}
	for _, entry := range snapshot.Entries {
		statuses[entry.Scenario] = entry.Status
	}
	if statuses["disconnect-before-headers"] != 0 || statuses["disconnect-after-first-event"] != http.StatusOK {
		t.Fatalf("disconnect diagnostic statuses = %+v", statuses)
	}
}

func TestMissingContentTypeIsAbsentOnWire(t *testing.T) {
	server := httptest.NewServer(newMockHandler())
	defer server.Close()
	request, err := http.NewRequest(http.MethodPost, server.URL+"/v1/responses", strings.NewReader(`{"model":"gpt-5"}`))
	if err != nil {
		t.Fatalf("create request: %v", err)
	}
	request.Header.Set(scenarioHeader, "missing-content-type")
	request.Header.Set("Authorization", "Bearer "+defaultMockAPIKey)
	response, err := server.Client().Do(request)
	if err != nil {
		t.Fatalf("perform request: %v", err)
	}
	defer response.Body.Close()
	if _, ok := response.Header["Content-Type"]; ok || response.Header.Get("Content-Type") != "" {
		t.Fatalf("Content-Type headers = %#v", response.Header)
	}
}

func performRequest(t *testing.T, handler http.Handler, method, target, body, scenario string) *httptest.ResponseRecorder {
	t.Helper()
	request := httptest.NewRequest(method, target, strings.NewReader(body))
	request.Header.Set("Authorization", "Bearer "+defaultMockAPIKey)
	if scenario != "" {
		request.Header.Set(scenarioHeader, scenario)
	}
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	return response
}

type flushRecorder struct {
	*httptest.ResponseRecorder
	flushes int
}

func (w *flushRecorder) Flush() {
	w.flushes++
	w.ResponseRecorder.Flush()
}

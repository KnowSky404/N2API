package main

import (
	"context"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"os/signal"
	"sort"
	"strings"
	"sync"
	"syscall"
	"time"
)

const (
	defaultListenAddress = ":8080"
	maxRequestBodyBytes  = 1 << 20
	defaultTimeoutDelay  = 5 * time.Second
	mockModelID          = "gpt-5"
	defaultMockAPIKey    = "e2e-upstream-fixture-key"
	scenarioHeader       = "X-N2API-E2E-Scenario"
)

var allowedScenarios = map[string]struct{}{
	"happy":                        {},
	"status-401":                   {},
	"status-403":                   {},
	"status-429":                   {},
	"status-500":                   {},
	"status-503":                   {},
	"missing-content-type":         {},
	"wrong-content-type":           {},
	"missing-usage":                {},
	"malformed-usage":              {},
	"timeout-before-headers":       {},
	"disconnect-before-headers":    {},
	"disconnect-after-first-event": {},
}

type mockHandler struct {
	expectedAuthorizationHash [sha256.Size]byte
	state                     *diagnosticState
	now                       func() time.Time
	timeoutDelay              time.Duration
}

type mockRequest struct {
	Model  string `json:"model"`
	Stream bool   `json:"stream"`
}

type diagnosticState struct {
	mu      sync.Mutex
	entries map[diagnosticKey]diagnosticEntry
}

type diagnosticKey struct {
	Scenario string
	Method   string
	Route    string
	Status   int
}

type diagnosticEntry struct {
	Scenario string    `json:"scenario"`
	Method   string    `json:"method"`
	Route    string    `json:"route"`
	Status   int       `json:"status"`
	Count    int64     `json:"count"`
	LastAt   time.Time `json:"last_at"`
}

type diagnosticSnapshot struct {
	Entries []diagnosticEntry `json:"entries"`
}

func main() {
	address := strings.TrimSpace(os.Getenv("N2API_MOCK_ADDR"))
	if address == "" {
		address = defaultListenAddress
	}
	apiKey := os.Getenv("N2API_MOCK_API_KEY")
	if apiKey == "" {
		log.Fatal("N2API_MOCK_API_KEY is required")
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	server := &http.Server{
		Addr:              address,
		Handler:           newMockHandler(apiKey),
		ReadHeaderTimeout: 5 * time.Second,
		IdleTimeout:       30 * time.Second,
	}
	errCh := make(chan error, 1)
	go func() {
		errCh <- server.ListenAndServe()
	}()

	log.Printf("N2API E2E mock listening on %s", address)
	select {
	case err := <-errCh:
		if err != nil && !errors.Is(err, http.ErrServerClosed) {
			log.Fatal(err)
		}
	case <-ctx.Done():
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if err := server.Shutdown(shutdownCtx); err != nil {
			log.Printf("N2API E2E mock shutdown failed: %v", err)
			_ = server.Close()
		}
		if err := <-errCh; err != nil && !errors.Is(err, http.ErrServerClosed) {
			log.Printf("N2API E2E mock stopped with error: %v", err)
		}
	}
}

func newMockHandler(apiKeys ...string) http.Handler {
	apiKey := defaultMockAPIKey
	if len(apiKeys) > 0 {
		apiKey = apiKeys[0]
	}
	return &mockHandler{
		expectedAuthorizationHash: sha256.Sum256([]byte("Bearer " + apiKey)),
		state:                     newDiagnosticState(),
		now:                       time.Now,
		timeoutDelay:              defaultTimeoutDelay,
	}
}

func newDiagnosticState() *diagnosticState {
	return &diagnosticState{entries: make(map[diagnosticKey]diagnosticEntry)}
}

func (h *mockHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if h.serveControl(w, r) {
		return
	}
	if !h.authorized(r) {
		writeOpenAIError(w, http.StatusUnauthorized, "mock_unauthorized", "mock authorization failed")
		return
	}

	scenario, ok := canonicalScenario(r.Header.Get(scenarioHeader))
	if !ok {
		writeOpenAIError(w, http.StatusBadRequest, "mock_scenario_unknown", "unknown mock scenario")
		return
	}

	route, routeOK := canonicalRoute(r.URL.Path)
	method := canonicalMethod(r.Method)
	if !routeOK {
		status := http.StatusNotFound
		h.state.record(scenario, method, "unknown", status, h.now())
		writeOpenAIError(w, status, "mock_route_not_found", "mock route not found")
		return
	}
	if !routeAllowsMethod(route, r.Method) {
		status := http.StatusMethodNotAllowed
		h.state.record(scenario, method, route, status, h.now())
		writeOpenAIError(w, status, "mock_method_not_allowed", "mock method not allowed")
		return
	}

	var request mockRequest
	if r.Method == http.MethodPost {
		var status int
		request, status = decodeMockRequest(w, r)
		if status != 0 {
			h.state.record(scenario, method, route, status, h.now())
			if status == http.StatusRequestEntityTooLarge {
				writeOpenAIError(w, status, "mock_request_too_large", "mock request is too large")
			} else {
				writeOpenAIError(w, status, "mock_invalid_request", "mock request must be valid JSON")
			}
			return
		}
		if strings.TrimSpace(request.Model) == "" {
			status = http.StatusBadRequest
			h.state.record(scenario, method, route, status, h.now())
			writeOpenAIError(w, status, "mock_model_required", "mock model is required")
			return
		}
	}

	status := scenarioStatus(scenario)
	if status != 0 {
		h.state.record(scenario, method, route, status, h.now())
		writeScenarioStatus(w, status)
		return
	}

	switch scenario {
	case "timeout-before-headers":
		h.state.record(scenario, method, route, 0, h.now())
		h.waitBeforeHeaders(r.Context())
		return
	case "disconnect-before-headers":
		disconnect(w, false, func(status int) {
			h.state.record(scenario, method, route, status, h.now())
		})
		return
	case "disconnect-after-first-event":
		disconnect(w, true, func(status int) {
			h.state.record(scenario, method, route, status, h.now())
		})
		return
	}

	status = http.StatusOK
	h.state.record(scenario, method, route, status, h.now())
	setSuccessContentType(w.Header(), scenario, request.Stream)
	switch route {
	case "/v1/models":
		writeModels(w)
	case "/v1/chat/completions":
		if request.Stream {
			writeChatCompletionStream(w, scenario)
		} else {
			writeChatCompletionJSON(w, scenario)
		}
	case "/v1/responses":
		if request.Stream {
			writeResponsesStream(w, scenario)
		} else {
			writeResponsesJSON(w, scenario)
		}
	}
}

func (h *mockHandler) authorized(r *http.Request) bool {
	actual := sha256.Sum256([]byte(r.Header.Get("Authorization")))
	return subtle.ConstantTimeCompare(actual[:], h.expectedAuthorizationHash[:]) == 1
}

func (h *mockHandler) serveControl(w http.ResponseWriter, r *http.Request) bool {
	switch r.URL.Path {
	case "/healthz":
		if r.Method != http.MethodGet {
			writeOpenAIError(w, http.StatusMethodNotAllowed, "mock_method_not_allowed", "mock method not allowed")
			return true
		}
		writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
		return true
	case "/__mock/state":
		if r.Method != http.MethodGet {
			writeOpenAIError(w, http.StatusMethodNotAllowed, "mock_method_not_allowed", "mock method not allowed")
			return true
		}
		writeJSON(w, http.StatusOK, h.state.snapshot())
		return true
	case "/__mock/reset":
		if r.Method != http.MethodPost {
			writeOpenAIError(w, http.StatusMethodNotAllowed, "mock_method_not_allowed", "mock method not allowed")
			return true
		}
		h.state.reset()
		w.WriteHeader(http.StatusNoContent)
		return true
	default:
		return false
	}
}

func (h *mockHandler) waitBeforeHeaders(ctx context.Context) {
	delay := h.timeoutDelay
	if delay <= 0 {
		delay = defaultTimeoutDelay
	}
	timer := time.NewTimer(delay)
	defer timer.Stop()
	select {
	case <-ctx.Done():
	case <-timer.C:
	}
}

func canonicalScenario(raw string) (string, bool) {
	if raw == "" {
		return "happy", true
	}
	if raw != strings.TrimSpace(raw) {
		return "", false
	}
	if _, ok := allowedScenarios[raw]; !ok {
		return "", false
	}
	return raw, true
}

func canonicalRoute(path string) (string, bool) {
	switch path {
	case "/v1/models", "/v1/chat/completions", "/v1/responses":
		return path, true
	default:
		return "", false
	}
}

func canonicalMethod(method string) string {
	switch method {
	case http.MethodGet, http.MethodPost:
		return method
	default:
		return "OTHER"
	}
}

func routeAllowsMethod(route, method string) bool {
	return (route == "/v1/models" && method == http.MethodGet) ||
		(route != "/v1/models" && method == http.MethodPost)
}

func decodeMockRequest(w http.ResponseWriter, r *http.Request) (mockRequest, int) {
	defer r.Body.Close()
	decoder := json.NewDecoder(http.MaxBytesReader(w, r.Body, maxRequestBodyBytes))
	var request mockRequest
	if err := decoder.Decode(&request); err != nil {
		var maxBytesErr *http.MaxBytesError
		if errors.As(err, &maxBytesErr) {
			return mockRequest{}, http.StatusRequestEntityTooLarge
		}
		return mockRequest{}, http.StatusBadRequest
	}
	var trailing any
	if err := decoder.Decode(&trailing); !errors.Is(err, io.EOF) {
		var maxBytesErr *http.MaxBytesError
		if errors.As(err, &maxBytesErr) {
			return mockRequest{}, http.StatusRequestEntityTooLarge
		}
		return mockRequest{}, http.StatusBadRequest
	}
	return request, 0
}

func scenarioStatus(scenario string) int {
	switch scenario {
	case "status-401":
		return http.StatusUnauthorized
	case "status-403":
		return http.StatusForbidden
	case "status-429":
		return http.StatusTooManyRequests
	case "status-500":
		return http.StatusInternalServerError
	case "status-503":
		return http.StatusServiceUnavailable
	default:
		return 0
	}
}

func writeScenarioStatus(w http.ResponseWriter, status int) {
	if status == http.StatusTooManyRequests {
		w.Header().Set("Retry-After", "1")
	}
	writeOpenAIError(w, status, "mock_upstream_error", "mock upstream error")
}

func setSuccessContentType(header http.Header, scenario string, stream bool) {
	switch scenario {
	case "missing-content-type":
		header["Content-Type"] = nil
	case "wrong-content-type":
		header.Set("Content-Type", "text/plain; charset=utf-8")
	default:
		if stream {
			header.Set("Content-Type", "text/event-stream")
		} else {
			header.Set("Content-Type", "application/json")
		}
	}
}

func writeModels(w http.ResponseWriter) {
	writeJSONBody(w, map[string]any{
		"object": "list",
		"data": []map[string]any{{
			"id":       mockModelID,
			"object":   "model",
			"created":  int64(1_700_000_000),
			"owned_by": "openai",
		}},
	})
}

func writeChatCompletionJSON(w http.ResponseWriter, scenario string) {
	payload := map[string]any{
		"id":      "chatcmpl_mock",
		"object":  "chat.completion",
		"created": int64(1_700_000_000),
		"model":   mockModelID,
		"choices": []map[string]any{{
			"index": 0,
			"message": map[string]any{
				"role":    "assistant",
				"content": "mock response",
			},
			"finish_reason": "stop",
		}},
	}
	setChatUsage(payload, scenario)
	writeJSONBody(w, payload)
}

func writeResponsesJSON(w http.ResponseWriter, scenario string) {
	payload := map[string]any{
		"id":     "resp_mock",
		"object": "response",
		"status": "completed",
		"model":  mockModelID,
		"output": []map[string]any{{
			"id":   "msg_mock",
			"type": "message",
			"role": "assistant",
			"content": []map[string]any{{
				"type": "output_text",
				"text": "mock response",
			}},
		}},
	}
	setResponsesUsage(payload, scenario)
	writeJSONBody(w, payload)
}

func setChatUsage(payload map[string]any, scenario string) {
	switch scenario {
	case "missing-usage":
		return
	case "malformed-usage":
		payload["usage"] = map[string]any{
			"prompt_tokens":     "invalid",
			"completion_tokens": []any{},
			"total_tokens":      map[string]any{},
		}
	default:
		payload["usage"] = map[string]any{
			"prompt_tokens":     20,
			"completion_tokens": 5,
			"total_tokens":      25,
			"prompt_tokens_details": map[string]any{
				"cached_tokens": 4,
			},
			"completion_tokens_details": map[string]any{
				"reasoning_tokens": 2,
			},
		}
	}
}

func setResponsesUsage(payload map[string]any, scenario string) {
	switch scenario {
	case "missing-usage":
		return
	case "malformed-usage":
		payload["usage"] = map[string]any{
			"input_tokens":  "invalid",
			"output_tokens": []any{},
			"total_tokens":  map[string]any{},
		}
	default:
		payload["usage"] = map[string]any{
			"input_tokens":  11,
			"output_tokens": 2,
			"total_tokens":  13,
			"input_tokens_details": map[string]any{
				"cached_tokens": 3,
			},
			"output_tokens_details": map[string]any{
				"reasoning_tokens": 1,
			},
		}
	}
}

func writeChatCompletionStream(w http.ResponseWriter, scenario string) {
	events := []any{
		map[string]any{
			"id": "chatcmpl_mock", "object": "chat.completion.chunk", "model": mockModelID,
			"choices": []map[string]any{{"index": 0, "delta": map[string]any{"role": "assistant", "content": "mock response"}, "finish_reason": nil}},
		},
	}
	final := map[string]any{
		"id": "chatcmpl_mock", "object": "chat.completion.chunk", "model": mockModelID,
		"choices": []map[string]any{{"index": 0, "delta": map[string]any{}, "finish_reason": "stop"}},
	}
	setChatUsage(final, scenario)
	events = append(events, final)
	for _, event := range events {
		writeSSEData(w, event)
		flush(w)
	}
	_, _ = io.WriteString(w, "data: [DONE]\n\n")
	flush(w)
}

func writeResponsesStream(w http.ResponseWriter, scenario string) {
	writeSSEEvent(w, "response.created", map[string]any{
		"type":     "response.created",
		"response": map[string]any{"id": "resp_mock", "status": "in_progress", "model": mockModelID},
	})
	flush(w)
	writeSSEEvent(w, "response.output_text.delta", map[string]any{
		"type": "response.output_text.delta", "item_id": "msg_mock", "output_index": 0, "content_index": 0, "delta": "mock response",
	})
	flush(w)
	response := map[string]any{"id": "resp_mock", "status": "completed", "model": mockModelID}
	setResponsesUsage(response, scenario)
	writeSSEEvent(w, "response.completed", map[string]any{
		"type": "response.completed", "response": response,
	})
	flush(w)
}

func writeSSEEvent(w io.Writer, event string, payload any) {
	encoded, _ := json.Marshal(payload)
	_, _ = fmt.Fprintf(w, "event: %s\ndata: %s\n\n", event, encoded)
}

func writeSSEData(w io.Writer, payload any) {
	encoded, _ := json.Marshal(payload)
	_, _ = fmt.Fprintf(w, "data: %s\n\n", encoded)
}

func flush(w http.ResponseWriter) {
	if flusher, ok := w.(http.Flusher); ok {
		flusher.Flush()
	}
}

func disconnect(w http.ResponseWriter, afterFirstEvent bool, record func(status int)) {
	hijacker, ok := w.(http.Hijacker)
	if !ok {
		record(http.StatusInternalServerError)
		writeOpenAIError(w, http.StatusInternalServerError, "mock_disconnect_unsupported", "mock disconnect is unavailable")
		return
	}
	conn, rw, err := hijacker.Hijack()
	if err != nil {
		record(0)
		return
	}
	defer conn.Close()
	if !afterFirstEvent {
		record(0)
		return
	}
	_, _ = io.WriteString(rw, "HTTP/1.1 200 OK\r\nContent-Type: text/event-stream\r\nTransfer-Encoding: chunked\r\nConnection: close\r\n\r\n")
	var event strings.Builder
	writeSSEEvent(&event, "response.output_text.delta", map[string]any{
		"type": "response.output_text.delta", "delta": "partial",
	})
	_, _ = fmt.Fprintf(rw, "%x\r\n%s\r\n", event.Len(), event.String())
	_ = rw.Flush()
	record(http.StatusOK)
}

func writeOpenAIError(w http.ResponseWriter, status int, code, message string) {
	writeJSON(w, status, map[string]any{
		"error": map[string]any{
			"message": message,
			"type":    "mock_error",
			"param":   nil,
			"code":    code,
		},
	})
}

func writeJSON(w http.ResponseWriter, status int, payload any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	writeJSONBody(w, payload)
}

func writeJSONBody(w io.Writer, payload any) {
	_ = json.NewEncoder(w).Encode(payload)
}

func (s *diagnosticState) record(scenario, method, route string, status int, at time.Time) {
	key := diagnosticKey{Scenario: scenario, Method: method, Route: route, Status: status}
	s.mu.Lock()
	defer s.mu.Unlock()
	entry := s.entries[key]
	entry.Scenario = scenario
	entry.Method = method
	entry.Route = route
	entry.Status = status
	entry.Count++
	entry.LastAt = at.UTC()
	s.entries[key] = entry
}

func (s *diagnosticState) snapshot() diagnosticSnapshot {
	s.mu.Lock()
	defer s.mu.Unlock()
	entries := make([]diagnosticEntry, 0, len(s.entries))
	for _, entry := range s.entries {
		entries = append(entries, entry)
	}
	sort.Slice(entries, func(i, j int) bool {
		if entries[i].Scenario != entries[j].Scenario {
			return entries[i].Scenario < entries[j].Scenario
		}
		if entries[i].Method != entries[j].Method {
			return entries[i].Method < entries[j].Method
		}
		if entries[i].Route != entries[j].Route {
			return entries[i].Route < entries[j].Route
		}
		return entries[i].Status < entries[j].Status
	})
	return diagnosticSnapshot{Entries: entries}
}

func (s *diagnosticState) reset() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.entries = make(map[diagnosticKey]diagnosticEntry)
}

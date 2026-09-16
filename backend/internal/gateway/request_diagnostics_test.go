package gateway

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/KnowSky404/N2API/backend/internal/requestlog"
)

func TestRequestDiagnosticsBoundsAttemptTimelineAndPreservesOrder(t *testing.T) {
	diagnostics := newRequestDiagnostics()
	selected := SelectedAccount{AccountID: 42, AccountType: "codex_oauth", DisplayName: "owner"}
	for index := 0; index < requestlog.MaxRequestAttemptTimelineEntries+3; index++ {
		span := diagnostics.beginAttempt("upstream_http", selected, "retryable_status")
		span.finish(503, "upstream_unavailable")
	}

	attempts, truncated, _ := diagnostics.snapshot()
	if len(attempts) != requestlog.MaxRequestAttemptTimelineEntries {
		t.Fatalf("attempt count = %d, want %d", len(attempts), requestlog.MaxRequestAttemptTimelineEntries)
	}
	if !truncated {
		t.Fatal("attempt timeline is not marked truncated")
	}
	for index, attempt := range attempts {
		if attempt.Order != index {
			t.Fatalf("attempt %d order = %d, want %d", index, attempt.Order, index)
		}
		if attempt.DurationMS == nil || attempt.HTTPStatus != 503 || attempt.Error != "upstream_unavailable" {
			t.Fatalf("attempt %d = %+v, want completed bounded diagnostic", index, attempt)
		}
	}
}

func TestResponsePhaseTrackerIgnoresHeartbeatsAndCreationEvents(t *testing.T) {
	tracker := newResponsePhaseTracker("/v1/responses")
	tracker.startedAt = time.Now().Add(-100 * time.Millisecond)
	tracker.observeSSE([]byte(": heartbeat\n\nevent: response.created\ndata: {" +
		`"type":"response.created"` + "}\n\n"))
	if timing := tracker.snapshot(); timing.FirstUsefulOutputMS != nil {
		t.Fatalf("creation-only timing = %+v, want no useful output", timing)
	}

	tracker.observeSSE([]byte("event: response.output_text.delta\ndata: {" +
		`"type":"response.output_text.delta","delta":"hello"` + "}\n\n"))
	timing := tracker.snapshot()
	if timing.FirstUsefulOutputMS == nil || timing.StreamFinishMS == nil {
		t.Fatalf("useful output timing = %+v, want first useful output and finish", timing)
	}
	if *timing.FirstUsefulOutputMS < 0 || *timing.StreamFinishMS < *timing.FirstUsefulOutputMS {
		t.Fatalf("timing = %+v, want monotonic non-negative phases", timing)
	}
}

func TestResponsePhaseTrackerRecognizesSuccessfulResponseTerminal(t *testing.T) {
	tracker := newResponsePhaseTracker("/v1/responses")
	tracker.observeSSEEvent("response.completed", `{"type":"response.completed","response":{"id":"resp_1"}}`)
	if timing := tracker.snapshot(); timing.FirstUsefulOutputMS == nil {
		t.Fatal("successful response.completed was not recognized as useful output")
	}

	if sseEventHasUsefulData("/v1/responses", "response.failed", `{"type":"response.failed"}`) {
		t.Fatal("response.failed should not count as useful output")
	}
	if responseBodyHasUsefulJSON("/v1/responses", 200, []byte(`{"error":{"message":"failed"}}`)) {
		t.Fatal("error response should not count as useful JSON")
	}
}

func TestResponseDiagnosticsClassifyModelErrorAndMissingGeneration(t *testing.T) {
	if !errors.Is(responseBodyDiagnosticError("/v1/responses", 200, []byte(`{"error":{"code":"model_error"}}`)), errUpstreamModelError) {
		t.Fatal("HTTP 200 error object was not classified as a model error")
	}
	if !errors.Is(responseBodyDiagnosticError("/v1/chat/completions", 200, []byte(`{"object":"chat.completion"}`)), errUpstreamGenerationMissing) {
		t.Fatal("HTTP 200 response without choices was not classified as missing generation")
	}
	if got := sseEventDiagnosticError("", `{"type":"response.failed"}`); got != "upstream_response_failed" {
		t.Fatalf("failed SSE diagnostic = %q, want upstream_response_failed", got)
	}
	if got := sseEventDiagnosticError("error", `{"error":{"message":"private model detail"}}`); got != "upstream_sse_error" {
		t.Fatalf("error SSE diagnostic = %q, want upstream_sse_error", got)
	}
}

func TestRequestDiagnosticsSetTypeSeparatesTransportFromHTTP(t *testing.T) {
	diagnostics := newRequestDiagnostics()
	span := diagnostics.beginAttempt(attemptTypeUpstreamHTTP, SelectedAccount{}, "")
	span.setType(attemptTypeUpstreamTransport)
	span.finish(502, "upstream_unavailable")
	attempts, _, _ := diagnostics.snapshot()
	if len(attempts) != 1 || attempts[0].Type != attemptTypeUpstreamTransport {
		t.Fatalf("attempts = %+v, want transport type", attempts)
	}

	encoded, err := json.Marshal(attempts)
	if err != nil {
		t.Fatalf("Marshal returned error: %v", err)
	}
	if len(encoded) > requestlog.MaxRequestAttemptTimelineJSONBytes {
		t.Fatalf("encoded timeline size = %d, exceeds %d", len(encoded), requestlog.MaxRequestAttemptTimelineJSONBytes)
	}
}

func TestRequestDiagnosticsAddsHeaderWaitToBodyPhaseTimings(t *testing.T) {
	diagnostics := newRequestDiagnostics()
	headerWait := 7
	firstUseful := 2
	streamFinish := 5
	diagnostics.responseTiming.HeaderWaitMS = &headerWait
	diagnostics.setResponseTiming(requestlog.ResponseTiming{
		FirstUsefulOutputMS: &firstUseful,
		StreamFinishMS:      &streamFinish,
	})

	_, _, timing := diagnostics.snapshot()
	if timing.HeaderWaitMS == nil || *timing.HeaderWaitMS != 7 || timing.FirstUsefulOutputMS == nil || *timing.FirstUsefulOutputMS != 9 || timing.StreamFinishMS == nil || *timing.StreamFinishMS != 12 {
		t.Fatalf("response timing = %+v, want request-relative phases", timing)
	}
}

func TestCopyStreamingResponseMarksClientCancellationWhenClientWriteFails(t *testing.T) {
	observation, err := copyStreamingResponse(
		context.Background(),
		&diagnosticFailingResponseWriter{},
		io.NopCloser(strings.NewReader("data: {\"choices\":[{\"delta\":{\"content\":\"hello\"}}]}\n\n")),
		"/v1/chat/completions",
		time.Second,
	)
	if err != nil {
		t.Fatalf("copyStreamingResponse returned error: %v", err)
	}
	if observation.DiagnosticError != "client_canceled" || observation.StreamOutcome != "client_canceled" {
		t.Fatalf("stream observation = %+v, want client cancellation diagnostic", observation)
	}
}

type diagnosticFailingResponseWriter struct{}

func (*diagnosticFailingResponseWriter) Header() http.Header { return make(http.Header) }

func (*diagnosticFailingResponseWriter) WriteHeader(int) {}

func (*diagnosticFailingResponseWriter) Write([]byte) (int, error) {
	return 0, errors.New("client disconnected")
}

package gateway

import (
	"bytes"
	"encoding/json"
	"net/http"
	"strings"
	"time"

	"github.com/KnowSky404/N2API/backend/internal/requestlog"
)

const (
	attemptTypeSelection            = "selection"
	attemptTypeConcurrencyRejection = "concurrency_rejection"
	attemptTypeGatewayRejection     = "gateway_rejection"
	attemptTypeUpstreamHTTP         = "upstream_http"
	attemptTypeUpstreamTransport    = "upstream_transport"
	attemptTypeAuthRefreshRetry     = "auth_refresh_retry"
)

type requestDiagnostics struct {
	attempts          []requestlog.RequestAttempt
	nextAttemptOrder  int
	attemptsTruncated bool
	responseTiming    requestlog.ResponseTiming
}

type requestAttemptSpan struct {
	recorder *requestDiagnostics
	index    int
}

func newRequestDiagnostics() *requestDiagnostics {
	return &requestDiagnostics{
		attempts: make([]requestlog.RequestAttempt, 0, requestlog.MaxRequestAttemptTimelineEntries),
	}
}

func (d *requestDiagnostics) beginAttempt(attemptType string, selected SelectedAccount, fallbackReason string) *requestAttemptSpan {
	if d == nil {
		return nil
	}
	if len(d.attempts) >= requestlog.MaxRequestAttemptTimelineEntries {
		d.attemptsTruncated = true
		return &requestAttemptSpan{}
	}
	now := time.Now().UTC()
	d.attempts = append(d.attempts, requestlog.RequestAttempt{
		Order:          d.nextAttemptOrder,
		Type:           requestlog.BoundedText(attemptType, requestlog.MaxRequestDiagnosticTextBytes),
		AccountID:      selected.AccountID,
		AccountType:    requestlog.BoundedText(selected.AccountType, requestlog.MaxRequestDiagnosticTextBytes),
		AccountName:    requestlog.BoundedText(selected.DisplayName, requestlog.MaxRequestDiagnosticTextBytes),
		PoolID:         selected.RoutingPoolID,
		PoolName:       requestlog.BoundedText(selected.RoutingPoolName, requestlog.MaxRequestDiagnosticTextBytes),
		StartedAt:      now,
		FallbackReason: requestlog.BoundedText(fallbackReason, requestlog.MaxRequestDiagnosticTextBytes),
	})
	d.nextAttemptOrder++
	return &requestAttemptSpan{recorder: d, index: len(d.attempts) - 1}
}

func (s *requestAttemptSpan) entry() *requestlog.RequestAttempt {
	if s == nil || s.recorder == nil || s.index < 0 || s.index >= len(s.recorder.attempts) {
		return nil
	}
	return &s.recorder.attempts[s.index]
}

func (s *requestAttemptSpan) setSelected(selected SelectedAccount) {
	entry := s.entry()
	if entry == nil {
		return
	}
	entry.AccountID = selected.AccountID
	entry.AccountType = requestlog.BoundedText(selected.AccountType, requestlog.MaxRequestDiagnosticTextBytes)
	entry.AccountName = requestlog.BoundedText(selected.DisplayName, requestlog.MaxRequestDiagnosticTextBytes)
	entry.PoolID = selected.RoutingPoolID
	entry.PoolName = requestlog.BoundedText(selected.RoutingPoolName, requestlog.MaxRequestDiagnosticTextBytes)
}

func (s *requestAttemptSpan) setType(attemptType string) {
	entry := s.entry()
	if entry != nil {
		entry.Type = requestlog.BoundedText(attemptType, requestlog.MaxRequestDiagnosticTextBytes)
	}
}

func (s *requestAttemptSpan) setUpstreamRequestID(value string) {
	entry := s.entry()
	if entry != nil {
		entry.UpstreamRequestID = requestlog.BoundedText(value, requestlog.MaxRequestDiagnosticTextBytes)
	}
}

func (s *requestAttemptSpan) finish(statusCode int, errorCode string) {
	entry := s.entry()
	if entry == nil {
		return
	}
	if statusCode > 0 {
		entry.HTTPStatus = statusCode
	}
	if errorCode != "" {
		entry.Error = requestlog.BoundedText(errorCode, requestlog.MaxRequestDiagnosticTextBytes)
	}
	if entry.EndedAt != nil {
		return
	}
	now := time.Now().UTC()
	entry.EndedAt = &now
	duration := elapsedMilliseconds(entry.StartedAt, now)
	entry.DurationMS = duration
}

func (s *requestAttemptSpan) setError(errorCode string) {
	entry := s.entry()
	if entry != nil && errorCode != "" {
		entry.Error = requestlog.BoundedText(errorCode, requestlog.MaxRequestDiagnosticTextBytes)
	}
}

func elapsedMilliseconds(start, end time.Time) *int {
	if start.IsZero() || end.IsZero() {
		return nil
	}
	duration := end.Sub(start)
	if duration < 0 {
		duration = 0
	}
	value := int(duration / time.Millisecond)
	return &value
}

func (d *requestDiagnostics) resetResponseTiming() {
	if d != nil {
		d.responseTiming = requestlog.ResponseTiming{}
	}
}

func (d *requestDiagnostics) setHeaderWait(start, end time.Time) {
	if d != nil {
		d.responseTiming.HeaderWaitMS = elapsedMilliseconds(start, end)
	}
}

func (d *requestDiagnostics) setResponseTiming(timing requestlog.ResponseTiming) {
	if d == nil {
		return
	}
	if timing.HeaderWaitMS == nil && d.responseTiming.HeaderWaitMS != nil {
		timing.FirstUsefulOutputMS = addMilliseconds(d.responseTiming.HeaderWaitMS, timing.FirstUsefulOutputMS)
		timing.StreamFinishMS = addMilliseconds(d.responseTiming.HeaderWaitMS, timing.StreamFinishMS)
	}
	if timing.HeaderWaitMS == nil {
		timing.HeaderWaitMS = d.responseTiming.HeaderWaitMS
	}
	d.responseTiming = cloneResponseTiming(timing)
}

func addMilliseconds(offset, value *int) *int {
	if offset == nil || value == nil {
		return value
	}
	result := *offset + *value
	return &result
}

func (d *requestDiagnostics) snapshot() ([]requestlog.RequestAttempt, bool, requestlog.ResponseTiming) {
	if d == nil {
		return nil, false, requestlog.ResponseTiming{}
	}
	attempts := make([]requestlog.RequestAttempt, len(d.attempts))
	copy(attempts, d.attempts)
	for index := range attempts {
		if attempts[index].EndedAt != nil {
			endedAt := *attempts[index].EndedAt
			attempts[index].EndedAt = &endedAt
		}
		if attempts[index].DurationMS != nil {
			duration := *attempts[index].DurationMS
			attempts[index].DurationMS = &duration
		}
	}
	return attempts, d.attemptsTruncated, cloneResponseTiming(d.responseTiming)
}

func cloneResponseTiming(timing requestlog.ResponseTiming) requestlog.ResponseTiming {
	if timing.HeaderWaitMS != nil {
		value := *timing.HeaderWaitMS
		timing.HeaderWaitMS = &value
	}
	if timing.FirstUsefulOutputMS != nil {
		value := *timing.FirstUsefulOutputMS
		timing.FirstUsefulOutputMS = &value
	}
	if timing.StreamFinishMS != nil {
		value := *timing.StreamFinishMS
		timing.StreamFinishMS = &value
	}
	return timing
}

type responsePhaseTracker struct {
	route           string
	startedAt       time.Time
	firstUsefulAt   time.Time
	sseBuffer       []byte
	diagnosticError string
}

func newResponsePhaseTracker(route string) *responsePhaseTracker {
	return &responsePhaseTracker{route: route, startedAt: time.Now()}
}

func (t *responsePhaseTracker) markUseful() {
	if t != nil && t.firstUsefulAt.IsZero() {
		t.firstUsefulAt = time.Now()
	}
}

func (t *responsePhaseTracker) observeSSE(chunk []byte) {
	if t == nil || len(chunk) == 0 {
		return
	}
	const maxBufferedSSEObservation = 128 << 10
	t.sseBuffer = append(t.sseBuffer, chunk...)
	if len(t.sseBuffer) > maxBufferedSSEObservation {
		// Timing observation is best-effort and must remain bounded even when an
		// upstream sends an unbroken malformed line.
		t.sseBuffer = append([]byte(nil), t.sseBuffer[len(t.sseBuffer)-maxBufferedSSEObservation:]...)
	}
	for {
		index, delimiterLength := nextSSEEventDelimiter(t.sseBuffer)
		if index < 0 {
			return
		}
		event := append([]byte(nil), t.sseBuffer[:index]...)
		t.sseBuffer = t.sseBuffer[index+delimiterLength:]
		eventName, data := parseSSEEventForObservation(event)
		t.observeSSEEvent(eventName, data)
	}
}

func (t *responsePhaseTracker) observeSSEEvent(eventName, data string) {
	if t == nil {
		return
	}
	if t.diagnosticError == "" {
		t.diagnosticError = sseEventDiagnosticError(eventName, data)
	}
	if !sseEventHasUsefulData(t.route, eventName, data) {
		return
	}
	t.markUseful()
}

func (t *responsePhaseTracker) diagnosticErrorFor(route string) string {
	if t == nil {
		return ""
	}
	if t.diagnosticError != "" {
		return t.diagnosticError
	}
	if isGenerationRoute(route) && t.firstUsefulAt.IsZero() {
		return "upstream_generation_missing"
	}
	return ""
}

func (t *responsePhaseTracker) snapshot() requestlog.ResponseTiming {
	if t == nil || t.startedAt.IsZero() {
		return requestlog.ResponseTiming{}
	}
	now := time.Now()
	return requestlog.ResponseTiming{
		FirstUsefulOutputMS: elapsedMilliseconds(t.startedAt, t.firstUsefulAt),
		StreamFinishMS:      elapsedMilliseconds(t.startedAt, now),
	}
}

func responseBodyHasUsefulJSON(route string, statusCode int, body []byte) bool {
	if statusCode < http.StatusOK || statusCode >= http.StatusMultipleChoices {
		return false
	}
	var payload map[string]any
	if json.Unmarshal(body, &payload) != nil || len(payload) == 0 {
		return false
	}
	if _, hasError := payload["error"]; hasError {
		return false
	}
	if strings.HasPrefix(route, "/v1/responses") {
		_, hasID := payload["id"]
		return hasID || payload["output"] != nil
	}
	if route == "/v1/chat/completions" {
		choices, ok := payload["choices"].([]any)
		return ok && len(choices) > 0
	}
	return true
}

func responseBodyDiagnosticError(route string, statusCode int, body []byte) error {
	if !isGenerationRoute(route) || statusCode < http.StatusOK || statusCode >= http.StatusMultipleChoices {
		return nil
	}
	var payload map[string]any
	if json.Unmarshal(body, &payload) == nil {
		if _, hasError := payload["error"]; hasError {
			return errUpstreamModelError
		}
	}
	if !responseBodyHasUsefulJSON(route, statusCode, body) {
		return errUpstreamGenerationMissing
	}
	return nil
}

func isGenerationRoute(route string) bool {
	return route == "/v1/responses" || route == "/v1/chat/completions"
}

func parseSSEEventForObservation(event []byte) (string, string) {
	eventName := ""
	dataLines := make([]string, 0, 1)
	for _, rawLine := range bytes.Split(event, []byte("\n")) {
		line := strings.TrimSpace(string(bytes.TrimSuffix(rawLine, []byte("\r"))))
		if line == "" || strings.HasPrefix(line, ":") {
			continue
		}
		if strings.HasPrefix(line, "event:") {
			eventName = strings.TrimSpace(strings.TrimPrefix(line, "event:"))
			continue
		}
		if strings.HasPrefix(line, "data:") {
			dataLines = append(dataLines, strings.TrimSpace(strings.TrimPrefix(line, "data:")))
		}
	}
	return eventName, strings.Join(dataLines, "\n")
}

func sseEventHasUsefulData(route, eventName, data string) bool {
	data = strings.TrimSpace(data)
	if data == "" || data == "[DONE]" {
		return false
	}
	var payload map[string]any
	if json.Unmarshal([]byte(data), &payload) != nil {
		return false
	}
	eventType, _ := payload["type"].(string)
	eventType = strings.TrimSpace(eventType)
	if eventType == "" {
		eventType = strings.TrimSpace(eventName)
	}
	switch eventType {
	case "response.created", "response.queued", "response.in_progress", "response.failed", "response.incomplete":
		return false
	case "response.completed":
		return payload["response"] != nil
	}
	if strings.HasPrefix(route, "/v1/responses") || strings.HasPrefix(eventType, "response.") {
		if strings.HasSuffix(eventType, ".delta") {
			return nonEmptyJSONValue(payload["delta"]) || nonEmptyJSONValue(payload["text"]) || nonEmptyJSONValue(payload["arguments"])
		}
		if eventType == "response.output_item.added" || eventType == "response.content_part.added" || eventType == "response.output_text.done" {
			return payload["item"] != nil || payload["part"] != nil || nonEmptyJSONValue(payload["text"])
		}
	}
	if route == "/v1/chat/completions" {
		choices, ok := payload["choices"].([]any)
		if !ok {
			return false
		}
		for _, rawChoice := range choices {
			choice, ok := rawChoice.(map[string]any)
			if !ok {
				continue
			}
			if nonEmptyJSONValue(choice["delta"]) || nonEmptyJSONValue(choice["message"]) || nonEmptyJSONValue(choice["text"]) {
				return true
			}
		}
		return false
	}
	return nonEmptyJSONValue(payload["delta"]) || nonEmptyJSONValue(payload["text"]) || nonEmptyJSONValue(payload["content"])
}

func sseEventDiagnosticError(eventName, data string) string {
	data = strings.TrimSpace(data)
	if data == "" || data == "[DONE]" {
		return ""
	}
	var payload map[string]any
	if json.Unmarshal([]byte(data), &payload) != nil {
		return ""
	}
	eventType, _ := payload["type"].(string)
	eventType = strings.TrimSpace(eventType)
	if eventType == "" {
		eventType = strings.TrimSpace(eventName)
	}
	switch eventType {
	case "response.failed":
		return "upstream_response_failed"
	case "response.incomplete":
		return "upstream_response_incomplete"
	}
	if strings.EqualFold(strings.TrimSpace(eventName), "error") {
		return "upstream_sse_error"
	}
	if _, hasError := payload["error"]; hasError {
		return "upstream_model_error"
	}
	return ""
}

func nonEmptyJSONValue(value any) bool {
	switch value := value.(type) {
	case nil:
		return false
	case string:
		return strings.TrimSpace(value) != ""
	case []any:
		return len(value) > 0
	case map[string]any:
		return len(value) > 0
	default:
		return true
	}
}

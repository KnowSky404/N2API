package httpapi

import (
	"compress/gzip"
	"context"
	"encoding/csv"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/KnowSky404/N2API/backend/internal/admin"
	"github.com/KnowSky404/N2API/backend/internal/config"
	"github.com/KnowSky404/N2API/backend/internal/systemevent"
)

type failFinalExportRecorder struct {
	calls  int
	events []systemevent.Event
}

type failingExportResponseWriter struct {
	header http.Header
	status int
}

type blockingExportRecorder struct{}

type discardExportResponseWriter struct {
	header   http.Header
	status   int
	bytes    int
	maxWrite int
}

type generatedRequestLogAdminService struct {
	*fakeAdminService
	rowCount int
}

func (w *failingExportResponseWriter) Header() http.Header {
	if w.header == nil {
		w.header = make(http.Header)
	}
	return w.header
}

func (w *failingExportResponseWriter) WriteHeader(status int) { w.status = status }

func (w *failingExportResponseWriter) Write([]byte) (int, error) {
	return 0, errors.New("client write failed")
}

func (blockingExportRecorder) Insert(ctx context.Context, _ systemevent.Event) error {
	<-ctx.Done()
	return ctx.Err()
}

func (w *discardExportResponseWriter) Header() http.Header {
	if w.header == nil {
		w.header = make(http.Header)
	}
	return w.header
}

func (w *discardExportResponseWriter) WriteHeader(status int) { w.status = status }

func (w *discardExportResponseWriter) Write(value []byte) (int, error) {
	w.bytes += len(value)
	if len(value) > w.maxWrite {
		w.maxWrite = len(value)
	}
	return len(value), nil
}

func (w *discardExportResponseWriter) Flush() {}

func (s *generatedRequestLogAdminService) StreamRequestLogs(ctx context.Context, filter admin.RequestLogFilter, maxRows int, visit func(admin.RequestLog) error) (admin.RequestLogExportResult, error) {
	s.requestLogFilter = filter
	s.requestLogExportRows = maxRows
	result := admin.RequestLogExportResult{}
	for index := 0; index < s.rowCount; index++ {
		if err := ctx.Err(); err != nil {
			return result, err
		}
		if result.RowCount == maxRows {
			result.LimitReached = true
			return result, nil
		}
		if err := visit(admin.RequestLog{
			ID: int64(index + 1), RequestID: "req_generated", Model: "gpt-5",
			StatusCode: http.StatusOK, CreatedAt: time.Unix(int64(100+index), 0).UTC(),
		}); err != nil {
			return result, err
		}
		result.RowCount++
	}
	return result, nil
}

func (r *failFinalExportRecorder) Insert(_ context.Context, event systemevent.Event) error {
	r.calls++
	if r.calls == 2 {
		return errors.New("final event unavailable")
	}
	r.events = append(r.events, event)
	return nil
}

func TestParseRequestLogExportRequestRequiresBoundedStreamingRange(t *testing.T) {
	req := httptest.NewRequest("GET", "/api/admin/request-logs/export?format=csv&since=100&before=200&limit=750&gzip=1&statusClass=server_error&gatewayFallbacks=true", nil)
	options, filter, err := parseRequestLogExportRequest(req, 1000)
	if err != nil {
		t.Fatalf("parseRequestLogExportRequest returned error: %v", err)
	}
	if options.Format != requestLogExportFormatCSV || options.Limit != 750 || !options.Gzip {
		t.Fatalf("options = %+v", options)
	}
	if !filter.Since.Equal(time.Unix(100, 0).UTC()) || !filter.Before.Equal(time.Unix(200, 0).UTC()) ||
		filter.StatusClass != admin.RequestLogStatusServerError || !filter.GatewayFallbacks {
		t.Fatalf("filter = %+v", filter)
	}
}

func TestParseRequestLogExportRequestRejectsWideningOrInvalidParameters(t *testing.T) {
	tests := []string{
		"format=csv&before=200",
		"format=jsonl&since=100",
		"format=csv&since=200&before=100",
		"format=csv&since=100&before=200&limit=1001",
		"format=csv&since=100&before=200&gzip=true",
		"format=json&gzip=1",
		"format=json&gzip=0",
		"format=json&limit=201",
		"format=csv&since=bad&before=200",
		"format=csv&since=100&before=200&statusClass=typo",
		"format=csv&since=100&before=200&providerAccountId=0",
		"format=csv&since=100&before=200&gatewayFallbacks=maybe",
		"format=csv&since=100&before=200&q=",
		"format=csv&since=100&before=200&sessionId=",
		"format=csv&since=100&before=200&q=" + strings.Repeat("x", admin.MaxRequestLogQueryLength+1),
		"format=csv&since=100&before=200&model=" + strings.Repeat("x", admin.MaxRequestLogFilterValueLength+1),
		"format=csv&since=100&before=200&unknown=value",
		"format=csv&since=100&since=101&before=200",
		"format=csv;gzip=1&since=100&before=200",
	}
	for _, query := range tests {
		t.Run(query, func(t *testing.T) {
			req := httptest.NewRequest("GET", "/api/admin/request-logs/export?"+query, nil)
			if _, _, err := parseRequestLogExportRequest(req, 1000); err == nil {
				t.Fatal("parseRequestLogExportRequest returned nil error")
			}
		})
	}
}

func TestParseRequestLogExportRequestKeepsSmallJSONCompatibility(t *testing.T) {
	req := httptest.NewRequest("GET", "/api/admin/request-logs/export?format=json&limit=200", nil)
	options, filter, err := parseRequestLogExportRequest(req, 1000)
	if err != nil {
		t.Fatalf("parseRequestLogExportRequest returned error: %v", err)
	}
	if options.Format != requestLogExportFormatJSON || options.Limit != 200 || !filter.Since.IsZero() || !filter.Before.IsZero() {
		t.Fatalf("options/filter = %+v / %+v", options, filter)
	}
}

func TestSpreadsheetSafeCSVCellProtectsFirstNonWhitespaceFormulaRune(t *testing.T) {
	for input, want := range map[string]string{
		"=1+1":          "'=1+1",
		" +cmd":         " '+cmd",
		"\t-cmd":        "\t'-cmd",
		"  @SUM(A1:A2)": "  '@SUM(A1:A2)",
		"plain":         "plain",
		"":              "",
	} {
		if got := spreadsheetSafeCSVCell(input); got != want {
			t.Errorf("spreadsheetSafeCSVCell(%q) = %q, want %q", input, got, want)
		}
	}
}

func TestRequestLogExportRowHasExplicitSafeFieldSet(t *testing.T) {
	encoded, err := json.Marshal(newRequestLogExportRow(admin.RequestLog{ID: 7, RequestID: "req_7", SessionID: "workspace"}))
	if err != nil {
		t.Fatalf("Marshal returned error: %v", err)
	}
	var fields map[string]any
	if err := json.Unmarshal(encoded, &fields); err != nil {
		t.Fatalf("Unmarshal returned error: %v", err)
	}
	want := []string{
		"id", "requestId", "clientKey", "provider", "providerAccountId", "providerAccountType", "providerAccountName",
		"routingPoolId", "routingPoolName", "routingPoolFallbackDepth", "routingPoolFallbackChain", "routingPoolError",
		"model", "sessionId", "route", "method", "statusCode", "latencyMs", "error", "inputTokens", "outputTokens",
		"totalTokens", "cachedInputTokens", "reasoningTokens", "usageSource", "estimatedCostMicrousd", "pricingMatched",
		"gatewayAttemptCount", "gatewayFallbackCount", "createdAt",
	}
	if got := reflect.ValueOf(fields).Len(); got != len(want) {
		t.Fatalf("field count = %d, want %d: %s", got, len(want), encoded)
	}
	for _, name := range want {
		if _, ok := fields[name]; !ok {
			t.Fatalf("missing field %q: %s", name, encoded)
		}
	}
	for _, forbidden := range []string{"token", "secret", "apiKey", "cookie", "proxyUrl", "responseBody", "pricingSnapshot"} {
		if strings.Contains(string(encoded), forbidden) {
			t.Fatalf("export row contains forbidden field %q: %s", forbidden, encoded)
		}
	}
}

func TestRequestLogExportStreamsGzipCSVWithSafeCellsTrailersAndEvents(t *testing.T) {
	admins := newFakeAdminService()
	admins.logs = []admin.RequestLog{{
		ID: 9, RequestID: "=HYPERLINK(\"bad\")", ClientKey: " +key", Provider: "openai", SessionID: "@workspace",
		Model: "gpt-5", StatusCode: 200, CreatedAt: time.Unix(150, 0).UTC(),
	}}
	recorder := &memorySystemEventRecorder{}
	server := NewServer(config.Config{RequestLogExportMaxRows: 1000, RequestLogExportTimeout: time.Second}, staticHealth{}, admins, newFakeProviderService(), recorder)
	req := httptest.NewRequest(http.MethodGet, "/api/admin/request-logs/export?format=csv&since=100&before=200&limit=1&gzip=1&sessionId=SECRET_SESSION_CANARY", nil)
	req.AddCookie(&http.Cookie{Name: adminSessionCookieName, Value: "valid-session"})
	response := httptest.NewRecorder()
	server.ServeHTTP(response, req)

	if response.Code != http.StatusOK || response.Header().Get("Content-Type") != "application/gzip" || response.Header().Get("Content-Encoding") != "" {
		t.Fatalf("status/type/encoding = %d/%q/%q", response.Code, response.Header().Get("Content-Type"), response.Header().Get("Content-Encoding"))
	}
	if got := response.Header().Get("Content-Disposition"); !strings.Contains(got, "n2api-request-logs_19700101T000140Z_19700101T000320Z.csv.gz") {
		t.Fatalf("Content-Disposition = %q", got)
	}
	gzipReader, err := gzip.NewReader(response.Body)
	if err != nil {
		t.Fatalf("gzip.NewReader returned error: %v", err)
	}
	rows, err := csv.NewReader(gzipReader).ReadAll()
	if err != nil {
		t.Fatalf("ReadAll returned error: %v", err)
	}
	if len(rows) != 2 || rows[1][1] != "'=HYPERLINK(\"bad\")" || rows[1][2] != " '+key" || rows[1][13] != "'@workspace" {
		t.Fatalf("CSV rows = %#v", rows)
	}
	trailers := response.Result().Trailer
	if trailers.Get(requestLogExportOutcomeTrailer) != string(systemevent.OutcomeSuccess) || trailers.Get(requestLogExportRowCountTrailer) != "1" || trailers.Get(requestLogExportLimitReachedTrailer) != "false" {
		t.Fatalf("trailers = %#v", trailers)
	}
	if len(recorder.events) != 2 || recorder.events[0].Action != systemevent.ActionRequestLogExportAccepted || recorder.events[1].Action != systemevent.ActionRequestLogExportCompleted {
		t.Fatalf("events = %+v", recorder.events)
	}
	eventsJSON, _ := json.Marshal(recorder.events)
	if strings.Contains(string(eventsJSON), "SECRET_SESSION_CANARY") {
		t.Fatalf("events leaked filter value: %s", eventsJSON)
	}
}

func TestRequestLogExportFailsClosedWhenAcceptedEventCannotBeStored(t *testing.T) {
	admins := newFakeAdminService()
	recorder := &memorySystemEventRecorder{err: errors.New("event store unavailable")}
	server := NewServer(config.Config{RequestLogExportMaxRows: 1000, RequestLogExportTimeout: time.Second}, staticHealth{}, admins, newFakeProviderService(), recorder)
	req := httptest.NewRequest(http.MethodGet, "/api/admin/request-logs/export?format=json&limit=1", nil)
	req.AddCookie(&http.Cookie{Name: adminSessionCookieName, Value: "valid-session"})
	response := httptest.NewRecorder()
	server.ServeHTTP(response, req)
	if response.Code != http.StatusInternalServerError || admins.requestLogExportRows != 0 {
		t.Fatalf("status/stream rows = %d/%d", response.Code, admins.requestLogExportRows)
	}
}

func TestRequestLogExportBoundsAcceptedEventWrite(t *testing.T) {
	admins := newFakeAdminService()
	req := httptest.NewRequest(http.MethodGet, "/api/admin/request-logs/export?format=json&limit=1", nil)
	response := httptest.NewRecorder()
	started := time.Now()
	handleExportRequestLogs(response, req, admins, blockingExportRecorder{}, 1000, time.Minute)

	if response.Code != http.StatusInternalServerError || admins.requestLogExportRows != 0 {
		t.Fatalf("status/stream rows = %d/%d", response.Code, admins.requestLogExportRows)
	}
	if elapsed := time.Since(started); elapsed < requestLogExportEventWait || elapsed > requestLogExportEventWait+time.Second {
		t.Fatalf("accepted event elapsed = %s, want bounded near %s", elapsed, requestLogExportEventWait)
	}
}

func TestRequestLogExportStreamsLargeFixtureInBoundedWrites(t *testing.T) {
	const rowCount = 25000
	admins := &generatedRequestLogAdminService{fakeAdminService: newFakeAdminService(), rowCount: rowCount}
	response := &discardExportResponseWriter{}
	req := httptest.NewRequest(http.MethodGet, "/api/admin/request-logs/export?format=jsonl&since=100&before=9999999999&limit=25000&gzip=1", nil)
	recorder := &memorySystemEventRecorder{}

	handleExportRequestLogs(response, req, admins, recorder, rowCount, 10*time.Second)

	if response.status != http.StatusOK || response.bytes == 0 {
		t.Fatalf("status/bytes = %d/%d", response.status, response.bytes)
	}
	if response.maxWrite > 64*1024 {
		t.Fatalf("largest response write = %d bytes, want bounded streaming chunks", response.maxWrite)
	}
	if response.Header().Get(requestLogExportRowCountTrailer) != "25000" || len(recorder.events) != 2 {
		t.Fatalf("row count/events = %q/%+v", response.Header().Get(requestLogExportRowCountTrailer), recorder.events)
	}
}

func TestRequestLogExportRecordsSafePartialOutcome(t *testing.T) {
	admins := newFakeAdminService()
	admins.logs = []admin.RequestLog{{ID: 1, RequestID: "req_1", CreatedAt: time.Unix(150, 0)}, {ID: 2, RequestID: "req_2", CreatedAt: time.Unix(151, 0)}}
	admins.requestLogErr = errors.New("SECRET_DATABASE_ERROR")
	admins.requestLogErrAfter = 1
	recorder := &memorySystemEventRecorder{}
	server := NewServer(config.Config{RequestLogExportMaxRows: 1000, RequestLogExportTimeout: time.Second}, staticHealth{}, admins, newFakeProviderService(), recorder)
	req := httptest.NewRequest(http.MethodGet, "/api/admin/request-logs/export?format=jsonl&since=100&before=200&limit=2", nil)
	req.AddCookie(&http.Cookie{Name: adminSessionCookieName, Value: "valid-session"})
	response := httptest.NewRecorder()
	server.ServeHTTP(response, req)

	if response.Result().Trailer.Get(requestLogExportOutcomeTrailer) != string(systemevent.OutcomePartial) || len(recorder.events) != 2 {
		t.Fatalf("trailers/events = %#v / %+v", response.Result().Trailer, recorder.events)
	}
	completed := recorder.events[1]
	if completed.Outcome != systemevent.OutcomePartial || completed.ErrorCode != "request_log_export_partial" || completed.Metadata["row_count"] != 1 {
		t.Fatalf("completed event = %+v", completed)
	}
	encoded, _ := json.Marshal(completed)
	if strings.Contains(string(encoded), "SECRET_DATABASE_ERROR") {
		t.Fatalf("completed event leaked raw error: %s", encoded)
	}
}

func TestRequestLogExportJSONFailureBeforeBodyRecordsNoDeliveredRows(t *testing.T) {
	admins := newFakeAdminService()
	admins.logs = []admin.RequestLog{{ID: 1, RequestID: "req_1", CreatedAt: time.Unix(150, 0)}}
	admins.requestLogErr = errors.New("SECRET_DATABASE_ERROR")
	admins.requestLogErrAfter = 1
	recorder := &memorySystemEventRecorder{}
	server := NewServer(config.Config{RequestLogExportMaxRows: 1000, RequestLogExportTimeout: time.Second}, staticHealth{}, admins, newFakeProviderService(), recorder)
	req := httptest.NewRequest(http.MethodGet, "/api/admin/request-logs/export?format=json&limit=2", nil)
	req.AddCookie(&http.Cookie{Name: adminSessionCookieName, Value: "valid-session"})
	response := httptest.NewRecorder()
	server.ServeHTTP(response, req)

	if response.Code != http.StatusInternalServerError || len(recorder.events) != 2 {
		t.Fatalf("status/events = %d / %+v", response.Code, recorder.events)
	}
	completed := recorder.events[1]
	if completed.Outcome != systemevent.OutcomeFailure || completed.ErrorCode != "request_log_export_failed" || completed.Metadata["row_count"] != 0 {
		t.Fatalf("completed event = %+v", completed)
	}
	if completed.StatusCode == nil || *completed.StatusCode != http.StatusInternalServerError {
		t.Fatalf("completed status = %v, want 500", completed.StatusCode)
	}
}

func TestRequestLogExportTimeoutCancelsStreamAndRecordsFailure(t *testing.T) {
	admins := newFakeAdminService()
	admins.requestLogExportWait = true
	recorder := &memorySystemEventRecorder{}
	req := httptest.NewRequest(http.MethodGet, "/api/admin/request-logs/export?format=jsonl&since=100&before=200&limit=2", nil)
	response := httptest.NewRecorder()
	handleExportRequestLogs(response, req, admins, recorder, 1000, 5*time.Millisecond)
	if response.Result().Trailer.Get(requestLogExportOutcomeTrailer) != string(systemevent.OutcomeFailure) {
		t.Fatalf("outcome = %q", response.Result().Trailer.Get(requestLogExportOutcomeTrailer))
	}
	if len(recorder.events) != 2 || recorder.events[1].ErrorCode != "request_log_export_failed" {
		t.Fatalf("events = %+v", recorder.events)
	}
}

func TestRequestLogExportWriterFailureDoesNotOverreportRows(t *testing.T) {
	admins := newFakeAdminService()
	admins.logs = []admin.RequestLog{{ID: 1, RequestID: "req_1", CreatedAt: time.Unix(150, 0)}}
	recorder := &memorySystemEventRecorder{}
	req := httptest.NewRequest(http.MethodGet, "/api/admin/request-logs/export?format=csv&since=100&before=200&limit=2", nil)
	response := &failingExportResponseWriter{}
	handleExportRequestLogs(response, req, admins, recorder, 1000, time.Second)

	if len(recorder.events) != 2 {
		t.Fatalf("events = %+v", recorder.events)
	}
	completed := recorder.events[1]
	if completed.Outcome != systemevent.OutcomeFailure || completed.Metadata["row_count"] != 0 {
		t.Fatalf("completed event = %+v", completed)
	}
	if response.Header().Get(requestLogExportRowCountTrailer) != "0" {
		t.Fatalf("row count trailer = %q", response.Header().Get(requestLogExportRowCountTrailer))
	}
}

func TestRequestLogExportClientDisconnectCancelsStream(t *testing.T) {
	admins := newFakeAdminService()
	admins.requestLogExportWait = true
	admins.requestLogStarted = make(chan struct{})
	admins.requestLogCanceled = make(chan struct{})
	server := httptest.NewServer(NewServer(
		config.Config{RequestLogExportMaxRows: 1000, RequestLogExportTimeout: time.Second},
		staticHealth{}, admins, newFakeProviderService(), &memorySystemEventRecorder{},
	))
	defer server.Close()

	req, err := http.NewRequest(http.MethodGet, server.URL+"/api/admin/request-logs/export?format=jsonl&since=100&before=200&limit=2", nil)
	if err != nil {
		t.Fatalf("NewRequest returned error: %v", err)
	}
	req.AddCookie(&http.Cookie{Name: adminSessionCookieName, Value: "valid-session"})
	response, err := server.Client().Do(req)
	if err != nil {
		t.Fatalf("Do returned error: %v", err)
	}
	select {
	case <-admins.requestLogStarted:
	case <-time.After(time.Second):
		t.Fatal("request log stream did not start")
	}
	if err := response.Body.Close(); err != nil {
		t.Fatalf("Close returned error: %v", err)
	}
	select {
	case <-admins.requestLogCanceled:
	case <-time.After(time.Second):
		t.Fatal("request log stream context was not canceled after client disconnect")
	}
}

func TestRequestLogExportReportsLimitReachedInTrailersAndCompletionEvent(t *testing.T) {
	admins := newFakeAdminService()
	admins.logs = []admin.RequestLog{
		{ID: 1, RequestID: "req_1", CreatedAt: time.Unix(150, 0)},
		{ID: 2, RequestID: "req_2", CreatedAt: time.Unix(151, 0)},
		{ID: 3, RequestID: "req_3", CreatedAt: time.Unix(152, 0)},
	}
	recorder := &memorySystemEventRecorder{}
	server := NewServer(config.Config{RequestLogExportMaxRows: 1000, RequestLogExportTimeout: time.Second}, staticHealth{}, admins, newFakeProviderService(), recorder)
	req := httptest.NewRequest(http.MethodGet, "/api/admin/request-logs/export?format=jsonl&since=100&before=200&limit=2", nil)
	req.AddCookie(&http.Cookie{Name: adminSessionCookieName, Value: "valid-session"})
	response := httptest.NewRecorder()
	server.ServeHTTP(response, req)

	trailers := response.Result().Trailer
	if trailers.Get(requestLogExportOutcomeTrailer) != string(systemevent.OutcomeSuccess) || trailers.Get(requestLogExportRowCountTrailer) != "2" || trailers.Get(requestLogExportLimitReachedTrailer) != "true" {
		t.Fatalf("trailers = %#v", trailers)
	}
	if len(recorder.events) != 2 {
		t.Fatalf("events = %+v", recorder.events)
	}
	completed := recorder.events[1]
	if completed.Metadata["row_count"] != 2 || completed.Metadata["limit_reached"] != true {
		t.Fatalf("completed metadata = %+v", completed.Metadata)
	}
}

func TestRequestLogExportCompletionEventFailureDoesNotBreakDownload(t *testing.T) {
	admins := newFakeAdminService()
	admins.logs = []admin.RequestLog{
		{ID: 1, RequestID: "req_1", CreatedAt: time.Unix(150, 0)},
		{ID: 2, RequestID: "req_2", CreatedAt: time.Unix(151, 0)},
	}
	recorder := &failFinalExportRecorder{}
	server := NewServer(config.Config{RequestLogExportMaxRows: 1000, RequestLogExportTimeout: time.Second}, staticHealth{}, admins, newFakeProviderService(), recorder)
	req := httptest.NewRequest(http.MethodGet, "/api/admin/request-logs/export?format=jsonl&since=100&before=200&limit=2", nil)
	req.AddCookie(&http.Cookie{Name: adminSessionCookieName, Value: "valid-session"})
	response := httptest.NewRecorder()
	server.ServeHTTP(response, req)

	lines := strings.Split(strings.TrimSpace(response.Body.String()), "\n")
	if response.Code != http.StatusOK || len(lines) != 2 {
		t.Fatalf("status/body = %d/%q", response.Code, response.Body.String())
	}
	trailers := response.Result().Trailer
	if trailers.Get(requestLogExportOutcomeTrailer) != string(systemevent.OutcomeSuccess) || trailers.Get(requestLogExportRowCountTrailer) != "2" {
		t.Fatalf("trailers = %#v", trailers)
	}
	if recorder.calls != 2 || len(recorder.events) != 1 || recorder.events[0].Action != systemevent.ActionRequestLogExportAccepted {
		t.Fatalf("recorder = calls %d events %+v", recorder.calls, recorder.events)
	}
}

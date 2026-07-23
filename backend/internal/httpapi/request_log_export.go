package httpapi

import (
	"compress/gzip"
	"context"
	"encoding/csv"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
	"unicode"

	"github.com/KnowSky404/N2API/backend/internal/admin"
	"github.com/KnowSky404/N2API/backend/internal/systemevent"
)

const (
	requestLogExportFormatJSON  = "json"
	requestLogExportFormatCSV   = "csv"
	requestLogExportFormatJSONL = "jsonl"

	requestLogExportJSONMaxRows = 200
	requestLogExportDefaultRows = 100000
	requestLogExportDefaultWait = 60 * time.Second
	requestLogExportEventWait   = 2 * time.Second
	requestLogExportFlushRows   = 128

	requestLogExportOutcomeTrailer      = "X-N2API-Export-Outcome"
	requestLogExportRowCountTrailer     = "X-N2API-Export-Row-Count"
	requestLogExportLimitReachedTrailer = "X-N2API-Export-Limit-Reached"
)

type requestLogExportOptions struct {
	Format string
	Limit  int
	Gzip   bool
}

type requestLogExportRow struct {
	ID                       int64     `json:"id"`
	RequestID                string    `json:"requestId"`
	UpstreamRequestID        string    `json:"upstreamRequestId"`
	ClientKey                string    `json:"clientKey"`
	Provider                 string    `json:"provider"`
	ProviderAccountID        int64     `json:"providerAccountId"`
	ProviderAccountType      string    `json:"providerAccountType"`
	ProviderAccountName      string    `json:"providerAccountName"`
	RoutingPoolID            int64     `json:"routingPoolId"`
	RoutingPoolName          string    `json:"routingPoolName"`
	RoutingPoolFallbackDepth int       `json:"routingPoolFallbackDepth"`
	RoutingPoolFallbackChain string    `json:"routingPoolFallbackChain"`
	RoutingPoolError         string    `json:"routingPoolError"`
	Model                    string    `json:"model"`
	SessionID                string    `json:"sessionId"`
	Route                    string    `json:"route"`
	Method                   string    `json:"method"`
	StatusCode               int       `json:"statusCode"`
	LatencyMS                int       `json:"latencyMs"`
	Error                    string    `json:"error"`
	InputTokens              int       `json:"inputTokens"`
	OutputTokens             int       `json:"outputTokens"`
	TotalTokens              int       `json:"totalTokens"`
	CachedInputTokens        int       `json:"cachedInputTokens"`
	ReasoningTokens          int       `json:"reasoningTokens"`
	UsageSource              string    `json:"usageSource"`
	EstimatedCostMicrousd    int64     `json:"estimatedCostMicrousd"`
	PricingMatched           bool      `json:"pricingMatched"`
	GatewayAttemptCount      int       `json:"gatewayAttemptCount"`
	GatewayFallbackCount     int       `json:"gatewayFallbackCount"`
	CreatedAt                time.Time `json:"createdAt"`
}

func newRequestLogExportRow(log admin.RequestLog) requestLogExportRow {
	return requestLogExportRow{
		ID: log.ID, RequestID: log.RequestID, UpstreamRequestID: log.UpstreamRequestID, ClientKey: log.ClientKey, Provider: log.Provider,
		ProviderAccountID: log.ProviderAccountID, ProviderAccountType: log.ProviderAccountType, ProviderAccountName: log.ProviderAccountName,
		RoutingPoolID: log.RoutingPoolID, RoutingPoolName: log.RoutingPoolName, RoutingPoolFallbackDepth: log.RoutingPoolFallbackDepth,
		RoutingPoolFallbackChain: log.RoutingPoolFallbackChain, RoutingPoolError: log.RoutingPoolError,
		Model: log.Model, SessionID: log.SessionID, Route: log.Route, Method: log.Method, StatusCode: log.StatusCode,
		LatencyMS: log.LatencyMS, Error: log.Error, InputTokens: log.InputTokens, OutputTokens: log.OutputTokens,
		TotalTokens: log.TotalTokens, CachedInputTokens: log.CachedInputTokens, ReasoningTokens: log.ReasoningTokens,
		UsageSource: log.UsageSource, EstimatedCostMicrousd: log.EstimatedCostMicrousd, PricingMatched: log.PricingMatched,
		GatewayAttemptCount: log.GatewayAttemptCount, GatewayFallbackCount: log.GatewayFallbackCount, CreatedAt: log.CreatedAt.UTC(),
	}
}

func parseRequestLogExportRequest(r *http.Request, maxRows int) (requestLogExportOptions, admin.RequestLogFilter, error) {
	if maxRows <= 0 {
		maxRows = requestLogExportDefaultRows
	}
	known := map[string]struct{}{
		"format": {}, "limit": {}, "gzip": {}, "since": {}, "before": {}, "requestId": {}, "q": {},
		"statusClass": {}, "statusCode": {}, "providerAccountId": {}, "routingPoolId": {}, "clientKeyId": {},
		"model": {}, "sessionId": {}, "error": {}, "usageSource": {}, "routingPoolError": {},
		"routingPoolChain": {}, "gatewayFallbacks": {},
	}
	query, err := url.ParseQuery(r.URL.RawQuery)
	if err != nil {
		return requestLogExportOptions{}, admin.RequestLogFilter{}, admin.ErrInvalidInput
	}
	for key, values := range query {
		if _, ok := known[key]; !ok || len(values) != 1 {
			return requestLogExportOptions{}, admin.RequestLogFilter{}, admin.ErrInvalidInput
		}
	}

	options := requestLogExportOptions{Format: strings.TrimSpace(query.Get("format"))}
	if options.Format == "" {
		options.Format = requestLogExportFormatJSON
	}
	switch options.Format {
	case requestLogExportFormatJSON:
		options.Limit = requestLogExportJSONMaxRows
	case requestLogExportFormatCSV, requestLogExportFormatJSONL:
		options.Limit = maxRows
	default:
		return requestLogExportOptions{}, admin.RequestLogFilter{}, admin.ErrInvalidInput
	}
	if raw, ok := singleQueryValue(query, "limit"); ok {
		limit, err := strconv.Atoi(raw)
		if err != nil || limit < 1 || limit > maxRows || options.Format == requestLogExportFormatJSON && limit > requestLogExportJSONMaxRows {
			return requestLogExportOptions{}, admin.RequestLogFilter{}, admin.ErrInvalidInput
		}
		options.Limit = limit
	}
	if raw, ok := singleQueryValue(query, "gzip"); ok {
		if options.Format == requestLogExportFormatJSON {
			return requestLogExportOptions{}, admin.RequestLogFilter{}, admin.ErrInvalidInput
		}
		switch raw {
		case "0":
		case "1":
			options.Gzip = true
		default:
			return requestLogExportOptions{}, admin.RequestLogFilter{}, admin.ErrInvalidInput
		}
	}

	filter := admin.RequestLogFilter{StatusClass: admin.RequestLogStatusAll}
	if filter.Since, err = parseExportTime(query, "since"); err != nil {
		return requestLogExportOptions{}, admin.RequestLogFilter{}, err
	}
	if filter.Before, err = parseExportTime(query, "before"); err != nil {
		return requestLogExportOptions{}, admin.RequestLogFilter{}, err
	}
	if !filter.Since.IsZero() && !filter.Before.IsZero() && !filter.Since.Before(filter.Before) {
		return requestLogExportOptions{}, admin.RequestLogFilter{}, admin.ErrInvalidInput
	}
	if options.Format != requestLogExportFormatJSON && (filter.Since.IsZero() || filter.Before.IsZero()) {
		return requestLogExportOptions{}, admin.RequestLogFilter{}, admin.ErrInvalidInput
	}
	if err := parseRequestLogExportFilters(query, &filter); err != nil {
		return requestLogExportOptions{}, admin.RequestLogFilter{}, err
	}
	return options, filter, nil
}

func singleQueryValue(query url.Values, name string) (string, bool) {
	values, ok := query[name]
	if !ok || len(values) != 1 {
		return "", false
	}
	return strings.TrimSpace(values[0]), true
}

func parseExportTime(query url.Values, name string) (time.Time, error) {
	raw, ok := singleQueryValue(query, name)
	if !ok {
		return time.Time{}, nil
	}
	seconds, err := strconv.ParseInt(raw, 10, 64)
	if err != nil || seconds <= 0 {
		return time.Time{}, admin.ErrInvalidInput
	}
	return time.Unix(seconds, 0).UTC(), nil
}

func parseRequestLogExportFilters(query url.Values, filter *admin.RequestLogFilter) error {
	for name, input := range map[string]struct {
		target *string
		maxLen int
	}{
		"requestId":        {&filter.RequestID, admin.MaxRequestLogFilterValueLength},
		"q":                {&filter.Query, admin.MaxRequestLogQueryLength},
		"model":            {&filter.Model, admin.MaxRequestLogFilterValueLength},
		"sessionId":        {&filter.SessionID, admin.MaxRequestLogFilterValueLength},
		"error":            {&filter.Error, admin.MaxRequestLogFilterValueLength},
		"usageSource":      {&filter.UsageSource, admin.MaxRequestLogFilterValueLength},
		"routingPoolError": {&filter.RoutingPoolError, admin.MaxRequestLogFilterValueLength},
		"routingPoolChain": {&filter.RoutingPoolChain, admin.MaxRequestLogRoutingPoolChainLength},
	} {
		if raw, ok := singleQueryValue(query, name); ok {
			if raw == "" || len(raw) > input.maxLen {
				return admin.ErrInvalidInput
			}
			*input.target = raw
		}
	}
	if raw, ok := singleQueryValue(query, "statusClass"); ok {
		switch raw {
		case admin.RequestLogStatusAll, admin.RequestLogStatusSuccess, admin.RequestLogStatusClientError, admin.RequestLogStatusServerError:
			filter.StatusClass = raw
		default:
			return admin.ErrInvalidInput
		}
	}
	if raw, ok := singleQueryValue(query, "statusCode"); ok {
		value, err := strconv.Atoi(raw)
		if err != nil || value < 100 || value > 599 {
			return admin.ErrInvalidInput
		}
		filter.StatusCode = value
	}
	for name, target := range map[string]*int64{
		"providerAccountId": &filter.ProviderAccountID,
		"routingPoolId":     &filter.RoutingPoolID,
		"clientKeyId":       &filter.ClientKeyID,
	} {
		if raw, ok := singleQueryValue(query, name); ok {
			value, err := strconv.ParseInt(raw, 10, 64)
			if err != nil || value <= 0 {
				return admin.ErrInvalidInput
			}
			*target = value
		}
	}
	if raw, ok := singleQueryValue(query, "gatewayFallbacks"); ok {
		switch raw {
		case "0", "false":
		case "1", "true":
			filter.GatewayFallbacks = true
		default:
			return admin.ErrInvalidInput
		}
	}
	return nil
}

func spreadsheetSafeCSVCell(value string) string {
	for index, character := range value {
		if unicode.IsSpace(character) {
			continue
		}
		if strings.ContainsRune("=+-@", character) {
			return value[:index] + "'" + value[index:]
		}
		break
	}
	return value
}

var requestLogExportCSVHeader = []string{
	"id", "request_id", "upstream_request_id", "client_key", "provider", "provider_account_id", "provider_account_type", "provider_account_name",
	"routing_pool_id", "routing_pool_name", "routing_pool_fallback_depth", "routing_pool_fallback_chain", "routing_pool_error",
	"model", "session_id", "route", "method", "status_code", "latency_ms", "error", "input_tokens", "output_tokens",
	"total_tokens", "cached_input_tokens", "reasoning_tokens", "usage_source", "estimated_cost_microusd", "pricing_matched",
	"gateway_attempt_count", "gateway_fallback_count", "created_at",
}

func requestLogExportCSVRecord(log admin.RequestLog) []string {
	row := newRequestLogExportRow(log)
	safe := spreadsheetSafeCSVCell
	return []string{
		strconv.FormatInt(row.ID, 10), safe(row.RequestID), safe(row.UpstreamRequestID), safe(row.ClientKey), safe(row.Provider), strconv.FormatInt(row.ProviderAccountID, 10),
		safe(row.ProviderAccountType), safe(row.ProviderAccountName), strconv.FormatInt(row.RoutingPoolID, 10), safe(row.RoutingPoolName),
		strconv.Itoa(row.RoutingPoolFallbackDepth), safe(row.RoutingPoolFallbackChain), safe(row.RoutingPoolError), safe(row.Model),
		safe(row.SessionID), safe(row.Route), safe(row.Method), strconv.Itoa(row.StatusCode), strconv.Itoa(row.LatencyMS), safe(row.Error),
		strconv.Itoa(row.InputTokens), strconv.Itoa(row.OutputTokens), strconv.Itoa(row.TotalTokens), strconv.Itoa(row.CachedInputTokens),
		strconv.Itoa(row.ReasoningTokens), safe(row.UsageSource), strconv.FormatInt(row.EstimatedCostMicrousd, 10), strconv.FormatBool(row.PricingMatched),
		strconv.Itoa(row.GatewayAttemptCount), strconv.Itoa(row.GatewayFallbackCount), row.CreatedAt.Format(time.RFC3339Nano),
	}
}

func handleExportRequestLogs(w http.ResponseWriter, r *http.Request, admins AdminService, recorder SystemEventRecorder, maxRows int, timeout time.Duration) {
	started := time.Now()
	options, filter, err := parseRequestLogExportRequest(r, maxRows)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid_input")
		return
	}
	if timeout <= 0 {
		timeout = requestLogExportDefaultWait
	}

	acceptedMetadata, err := requestLogExportMetadata(options, filter, 0, false, false)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal_error")
		return
	}
	acceptedCtx, acceptedCancel := context.WithTimeout(r.Context(), requestLogExportEventWait)
	err = recordHTTPSystemEvent(acceptedCtx, recorder, systemevent.EventIntent{
		Category: systemevent.CategorySecurity, Severity: systemevent.SeverityInfo,
		Action: systemevent.ActionRequestLogExportAccepted, Outcome: systemevent.OutcomeSuccess,
		Target: systemevent.Target{Type: "request_log_collection"}, Metadata: acceptedMetadata,
	}, http.StatusOK, time.Since(started))
	acceptedCancel()
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal_error")
		return
	}

	w.Header().Add("Trailer", requestLogExportOutcomeTrailer)
	w.Header().Add("Trailer", requestLogExportRowCountTrailer)
	w.Header().Add("Trailer", requestLogExportLimitReachedTrailer)
	exportCtx, cancel := context.WithTimeout(r.Context(), timeout)
	defer cancel()

	result, writtenRows, statusCode, streamErr := streamRequestLogExport(exportCtx, w, admins, options, filter)
	result.RowCount = writtenRows
	outcome, errorCode, severity := requestLogExportOutcome(result.RowCount, streamErr)
	setRequestLogExportTrailers(w, outcome, result)
	recordRequestLogExportCompletion(r.Context(), recorder, started, options, filter, result, outcome, errorCode, severity, statusCode)
}

func streamRequestLogExport(ctx context.Context, w http.ResponseWriter, admins AdminService, options requestLogExportOptions, filter admin.RequestLogFilter) (admin.RequestLogExportResult, int, int, error) {
	filename := requestLogExportFilename(options, filter)
	contentDisposition := fmt.Sprintf("attachment; filename=%q; filename*=UTF-8''%s", requestLogExportLegacyFilename(options), filename)

	if options.Format == requestLogExportFormatJSON {
		filter.Limit = options.Limit
		page, err := admins.ListRequestLogs(ctx, filter)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "internal_error")
			return admin.RequestLogExportResult{}, 0, http.StatusInternalServerError, err
		}
		rows := make([]requestLogExportRow, 0, len(page.Logs))
		for _, log := range page.Logs {
			rows = append(rows, newRequestLogExportRow(log))
		}
		result := admin.RequestLogExportResult{RowCount: len(rows), LimitReached: page.HasMore}
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("Content-Disposition", contentDisposition)
		w.WriteHeader(http.StatusOK)
		err = json.NewEncoder(w).Encode(map[string][]requestLogExportRow{"logs": rows})
		if err != nil {
			return result, 0, http.StatusOK, err
		}
		return result, len(rows), http.StatusOK, nil
	}

	w.Header().Set("Content-Disposition", contentDisposition)
	if options.Gzip {
		w.Header().Set("Content-Type", "application/gzip")
	} else if options.Format == requestLogExportFormatCSV {
		w.Header().Set("Content-Type", "text/csv; charset=utf-8")
	} else {
		w.Header().Set("Content-Type", "application/x-ndjson; charset=utf-8")
	}
	w.WriteHeader(http.StatusOK)

	body, closeBody := requestLogExportBody(w, options.Gzip)
	if err := flushRequestLogExport(w, body); err != nil {
		_ = closeBody()
		return admin.RequestLogExportResult{}, 0, http.StatusOK, err
	}
	writtenRows := 0
	pendingRows := 0
	var encoderErr error
	var result admin.RequestLogExportResult
	if options.Format == requestLogExportFormatCSV {
		csvWriter := csv.NewWriter(body)
		if err := csvWriter.Write(requestLogExportCSVHeader); err != nil {
			encoderErr = err
		} else {
			csvWriter.Flush()
			encoderErr = csvWriter.Error()
		}
		if encoderErr == nil {
			result, encoderErr = admins.StreamRequestLogs(ctx, filter, options.Limit, func(log admin.RequestLog) error {
				if err := csvWriter.Write(requestLogExportCSVRecord(log)); err != nil {
					return err
				}
				pendingRows++
				if pendingRows == requestLogExportFlushRows {
					csvWriter.Flush()
					if err := csvWriter.Error(); err != nil {
						return err
					}
					if err := flushRequestLogExport(w, body); err != nil {
						return err
					}
					writtenRows += pendingRows
					pendingRows = 0
				}
				return nil
			})
		}
		csvWriter.Flush()
		csvErr := csvWriter.Error()
		flushErr := flushRequestLogExport(w, body)
		if csvErr == nil && flushErr == nil {
			writtenRows += pendingRows
			pendingRows = 0
		}
		if encoderErr == nil && csvErr != nil {
			encoderErr = csvErr
		}
		if encoderErr == nil && flushErr != nil {
			encoderErr = flushErr
		}
	} else {
		encoder := json.NewEncoder(body)
		result, encoderErr = admins.StreamRequestLogs(ctx, filter, options.Limit, func(log admin.RequestLog) error {
			if err := encoder.Encode(newRequestLogExportRow(log)); err != nil {
				return err
			}
			if options.Gzip {
				pendingRows++
			} else {
				writtenRows++
			}
			if pendingRows == requestLogExportFlushRows {
				if err := flushRequestLogExport(w, body); err != nil {
					return err
				}
				writtenRows += pendingRows
				pendingRows = 0
			}
			return nil
		})
		flushErr := flushRequestLogExport(w, body)
		if flushErr == nil {
			writtenRows += pendingRows
			pendingRows = 0
		} else if encoderErr == nil {
			encoderErr = flushErr
		}
	}
	if closeErr := closeBody(); closeErr != nil {
		if options.Gzip {
			writtenRows = 0
		}
		if encoderErr == nil {
			encoderErr = closeErr
		}
	}
	return result, writtenRows, http.StatusOK, encoderErr
}

func requestLogExportBody(w http.ResponseWriter, compressed bool) (io.Writer, func() error) {
	if !compressed {
		return w, func() error { return nil }
	}
	writer := gzip.NewWriter(w)
	return writer, writer.Close
}

func flushRequestLogExport(w http.ResponseWriter, body io.Writer) error {
	if flusher, ok := body.(interface{ Flush() error }); ok {
		if err := flusher.Flush(); err != nil {
			return err
		}
	}
	if flusher, ok := w.(http.Flusher); ok {
		flusher.Flush()
	}
	return nil
}

func requestLogExportFilename(options requestLogExportOptions, filter admin.RequestLogFilter) string {
	extension := options.Format
	name := "n2api-request-logs"
	if !filter.Since.IsZero() && !filter.Before.IsZero() {
		name += "_" + filter.Since.UTC().Format("20060102T150405Z") + "_" + filter.Before.UTC().Format("20060102T150405Z")
	}
	name += "." + extension
	if options.Gzip {
		name += ".gz"
	}
	return name
}

func requestLogExportLegacyFilename(options requestLogExportOptions) string {
	name := "n2api-request-logs." + options.Format
	if options.Gzip {
		name += ".gz"
	}
	return name
}

func requestLogExportMetadata(options requestLogExportOptions, filter admin.RequestLogFilter, rowCount int, limitReached, completed bool) (map[string]any, error) {
	values := map[string]any{
		"format": options.Format, "compression": map[bool]string{false: "none", true: "gzip"}[options.Gzip],
		"requested_limit": options.Limit,
	}
	allowlist := []string{"format", "compression", "requested_limit", "since", "before"}
	if !filter.Since.IsZero() {
		values["since"] = filter.Since.UTC().Format(time.RFC3339)
	}
	if !filter.Before.IsZero() {
		values["before"] = filter.Before.UTC().Format(time.RFC3339)
	}
	if completed {
		values["row_count"] = rowCount
		values["limit_reached"] = limitReached
		allowlist = append(allowlist, "row_count", "limit_reached")
	}
	return systemevent.SafeMetadata(values, allowlist...)
}

func requestLogExportOutcome(rowCount int, streamErr error) (systemevent.Outcome, string, systemevent.Severity) {
	if streamErr == nil {
		return systemevent.OutcomeSuccess, "", systemevent.SeverityInfo
	}
	if rowCount > 0 {
		return systemevent.OutcomePartial, "request_log_export_partial", systemevent.SeverityWarning
	}
	return systemevent.OutcomeFailure, "request_log_export_failed", systemevent.SeverityError
}

func setRequestLogExportTrailers(w http.ResponseWriter, outcome systemevent.Outcome, result admin.RequestLogExportResult) {
	w.Header().Set(requestLogExportOutcomeTrailer, string(outcome))
	w.Header().Set(requestLogExportRowCountTrailer, strconv.Itoa(result.RowCount))
	w.Header().Set(requestLogExportLimitReachedTrailer, strconv.FormatBool(result.LimitReached))
}

func recordRequestLogExportCompletion(ctx context.Context, recorder SystemEventRecorder, started time.Time, options requestLogExportOptions, filter admin.RequestLogFilter, result admin.RequestLogExportResult, outcome systemevent.Outcome, errorCode string, severity systemevent.Severity, statusCode int) {
	metadata, err := requestLogExportMetadata(options, filter, result.RowCount, result.LimitReached, true)
	if err != nil {
		return
	}
	recordCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), requestLogExportEventWait)
	defer cancel()
	if err := recordHTTPSystemEvent(recordCtx, recorder, systemevent.EventIntent{
		Category: systemevent.CategorySecurity, Severity: severity,
		Action: systemevent.ActionRequestLogExportCompleted, Outcome: outcome, ErrorCode: errorCode,
		Target: systemevent.Target{Type: "request_log_collection"}, Metadata: metadata,
	}, statusCode, time.Since(started)); err != nil {
		slog.ErrorContext(recordCtx, "request log export completion event failed", "error_code", "request_log_export_event_record_failed")
	}
}

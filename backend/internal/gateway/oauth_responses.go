package gateway

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"
	"sync"
	"time"
)

var (
	errUpstreamSSETerminalMissing = errors.New("upstream SSE stream has no terminal response")
	errUpstreamSSETruncated       = errors.New("upstream SSE stream was truncated")
	errUpstreamSSEMalformed       = errors.New("upstream SSE stream contained malformed data")
	errUpstreamSSEError           = errors.New("upstream SSE stream reported an error")
	errUpstreamResponseFailed     = errors.New("upstream response failed")
	errUpstreamResponseIncomplete = errors.New("upstream response was incomplete")
	errUpstreamResponseCanceled   = errors.New("upstream response was canceled")
	errUpstreamResponseDeadline   = errors.New("upstream response deadline exceeded")
)

type oauthSSEEnvelope struct {
	Type     string          `json:"type"`
	Response json.RawMessage `json:"response"`
}

type oauthSSEEventResult struct {
	terminal bool
	outcome  string
	response []byte
	err      error
}

func (p *Proxy) aggregateOAuthResponses(ctx context.Context, body io.ReadCloser, route string) (upstreamResponseObservation, []byte, error) {
	if body == nil {
		return upstreamResponseObservation{Usage: Usage{Source: "missing"}}, nil, errUpstreamResponseRead
	}
	defer body.Close()

	maxBytes := defaultMaxUpstreamResponseBody
	if p != nil && p.maxResponseBody > 0 {
		maxBytes = p.maxResponseBody
	}
	reader := &oauthSSEBoundedReader{
		reader: &oauthSSEIdleReadCloser{body: body, timeout: p.oauthSSEIdleTimeout()},
		max:    maxBytes,
	}
	stopContextClose := func() bool { return true }
	if ctx != nil {
		stopContextClose = context.AfterFunc(ctx, func() {
			_ = body.Close()
		})
	}
	defer stopContextClose()

	parser := NewSSEParser(reader, SSEParserLimits{
		MaxLineBytes:  minPositive(DefaultSSEParserMaxLineBytes, maxBytes),
		MaxEventBytes: minPositive(DefaultSSEParserMaxEventBytes, maxBytes),
		MaxDataBytes:  minPositive(DefaultSSEParserMaxDataBytes, maxBytes),
	})
	for {
		event, err := parser.Next()
		if err != nil {
			return upstreamResponseObservation{Usage: Usage{Source: "missing"}}, nil, classifyOAuthSSEReadError(ctx, err)
		}
		result := parseOAuthSSEEvent(event)
		if result.err != nil && !result.terminal {
			return upstreamResponseObservation{Usage: Usage{Source: "missing"}, StreamOutcome: result.outcome}, nil, result.err
		}
		if !result.terminal {
			continue
		}
		observation := upstreamResponseObservation{
			Usage:         ParseUsageFromJSON(route, result.response),
			ResponseID:    responseIDFromJSON(route, result.response),
			StreamOutcome: result.outcome,
		}
		return observation, result.response, result.err
	}
}

func parseOAuthSSEEvent(event SSEEvent) oauthSSEEventResult {
	if !event.HasData {
		return oauthSSEEventResult{}
	}
	data := strings.TrimSpace(event.Data)
	if data == "" {
		return oauthSSEEventResult{outcome: "malformed", err: errUpstreamSSEMalformed}
	}
	if strings.TrimSpace(event.Event) == "error" {
		return oauthSSEEventResult{outcome: "error", err: errUpstreamSSEError}
	}
	if data == "[DONE]" {
		return oauthSSEEventResult{}
	}
	if !json.Valid([]byte(data)) {
		return oauthSSEEventResult{outcome: "malformed", err: errUpstreamSSEMalformed}
	}

	var envelope oauthSSEEnvelope
	if err := json.Unmarshal([]byte(data), &envelope); err != nil {
		return oauthSSEEventResult{outcome: "malformed", err: errUpstreamSSEMalformed}
	}
	eventType := strings.TrimSpace(envelope.Type)
	if eventType == "" {
		eventType = strings.TrimSpace(event.Event)
	}
	switch eventType {
	case "error":
		return oauthSSEEventResult{outcome: "error", err: errUpstreamSSEError}
	case "response.completed", "response.failed", "response.incomplete":
		responseBody, err := oauthResponseObject(envelope.Response)
		if err != nil {
			return oauthSSEEventResult{outcome: "malformed", err: err}
		}
		result := oauthSSEEventResult{
			terminal: true,
			response: responseBody,
			outcome: map[string]string{
				"response.completed":  "completed",
				"response.failed":     "failed",
				"response.incomplete": "incomplete",
			}[eventType],
		}
		if eventType == "response.failed" {
			result.err = errUpstreamResponseFailed
		}
		if eventType == "response.incomplete" {
			result.err = errUpstreamResponseIncomplete
		}
		return result
	}
	return oauthSSEEventResult{}
}

func oauthResponseObject(raw json.RawMessage) ([]byte, error) {
	responseBody := bytes.TrimSpace(raw)
	if len(responseBody) == 0 || responseBody[0] != '{' || !json.Valid(responseBody) {
		return nil, errUpstreamSSEMalformed
	}
	return append([]byte(nil), responseBody...), nil
}

func classifyOAuthSSEReadError(ctx context.Context, err error) error {
	if ctx != nil {
		switch ctx.Err() {
		case context.Canceled:
			return errUpstreamResponseCanceled
		case context.DeadlineExceeded:
			return errUpstreamResponseDeadline
		}
	}
	if errors.Is(err, errUpstreamSSEIdleTimeout) {
		return errUpstreamSSEIdleTimeout
	}
	if errors.Is(err, errUpstreamResponseTooLarge) ||
		errors.Is(err, ErrSSELineTooLarge) ||
		errors.Is(err, ErrSSEEventTooLarge) ||
		errors.Is(err, ErrSSEDataTooLarge) {
		return errUpstreamResponseTooLarge
	}
	if errors.Is(err, ErrSSEUnexpectedEOF) {
		return errUpstreamSSETruncated
	}
	if errors.Is(err, io.EOF) {
		return errUpstreamSSETerminalMissing
	}
	return errUpstreamResponseRead
}

func (p *Proxy) oauthSSEIdleTimeout() time.Duration {
	if p == nil || p.sseIdleTimeout <= 0 {
		return 0
	}
	return p.sseIdleTimeout
}

func minPositive(left, right int) int {
	if right <= 0 {
		return left
	}
	if left <= right {
		return left
	}
	return right
}

type oauthSSEIdleReadCloser struct {
	body    io.ReadCloser
	timeout time.Duration
}

func (r *oauthSSEIdleReadCloser) Read(p []byte) (int, error) {
	if r == nil || r.body == nil {
		return 0, io.EOF
	}
	if r.timeout <= 0 {
		return r.body.Read(p)
	}
	idleExpired := make(chan struct{})
	idleTimer := time.AfterFunc(r.timeout, func() {
		close(idleExpired)
		_ = r.body.Close()
	})
	n, err := r.body.Read(p)
	if !idleTimer.Stop() {
		<-idleExpired
		return n, errUpstreamSSEIdleTimeout
	}
	return n, err
}

type oauthSSEBoundedReader struct {
	reader io.Reader
	max    int
	read   int
}

func (r *oauthSSEBoundedReader) Read(p []byte) (int, error) {
	if r == nil || r.reader == nil {
		return 0, io.EOF
	}
	if r.max > 0 {
		remaining := r.max - r.read
		if remaining < 0 {
			return 0, errUpstreamResponseTooLarge
		}
		if len(p) > remaining+1 {
			p = p[:remaining+1]
		}
	}
	n, err := r.reader.Read(p)
	r.read += n
	if r.max > 0 && r.read > r.max {
		return n, errUpstreamResponseTooLarge
	}
	return n, err
}

func copyOAuthStreamingResponse(ctx context.Context, w http.ResponseWriter, body io.ReadCloser, route string, idleTimeout time.Duration, maxBytes int) (upstreamResponseObservation, error) {
	observer := NewSSEUsageObserver(route)
	tracker := newOAuthSSEStreamTracker(maxBytes)
	defer tracker.close()
	buffer := make([]byte, 32*1024)
	writer := flushWriter{ResponseWriter: w}
	stopContextClose := func() bool { return true }
	if ctx != nil {
		stopContextClose = context.AfterFunc(ctx, func() {
			_ = body.Close()
		})
	}
	defer stopContextClose()
	readBytes := 0
	for {
		idleExpired := make(chan struct{})
		idleTimer := time.AfterFunc(idleTimeout, func() {
			close(idleExpired)
			_ = body.Close()
		})
		n, readErr := body.Read(buffer)
		if !idleTimer.Stop() {
			<-idleExpired
		}
		if n > 0 {
			if maxBytes > 0 && readBytes > maxBytes-n {
				return upstreamResponseObservation{Usage: observer.Usage(), ResponseID: observer.ResponseID(), StreamOutcome: "upstream_error"}, errUpstreamResponseTooLarge
			}
			readBytes += n
			chunk := buffer[:n]
			observer.Observe(chunk)
			tracker.write(chunk)
			if _, writeErr := writer.Write(chunk); writeErr != nil {
				return upstreamResponseObservation{Usage: observer.Usage(), ResponseID: observer.ResponseID(), StreamOutcome: "client_canceled"}, nil
			}
		}
		select {
		case <-idleExpired:
			return upstreamResponseObservation{Usage: observer.Usage(), ResponseID: observer.ResponseID(), StreamOutcome: "upstream_error"}, errUpstreamSSEIdleTimeout
		default:
		}
		if readErr != nil {
			if ctx != nil {
				switch ctx.Err() {
				case context.Canceled:
					return upstreamResponseObservation{Usage: observer.Usage(), ResponseID: observer.ResponseID(), StreamOutcome: "client_canceled"}, errUpstreamResponseCanceled
				case context.DeadlineExceeded:
					return upstreamResponseObservation{Usage: observer.Usage(), ResponseID: observer.ResponseID(), StreamOutcome: "server_error"}, errUpstreamResponseDeadline
				}
			}
			if errors.Is(readErr, io.EOF) {
				tracker.close()
				if err := tracker.resultError(); err != nil {
					return upstreamResponseObservation{Usage: observer.Usage(), ResponseID: observer.ResponseID(), StreamOutcome: tracker.resultOutcome()}, err
				}
				return upstreamResponseObservation{Usage: observer.Usage(), ResponseID: observer.ResponseID(), StreamOutcome: "completed"}, nil
			}
			return upstreamResponseObservation{Usage: observer.Usage(), ResponseID: observer.ResponseID(), StreamOutcome: "upstream_error"}, errUpstreamResponseRead
		}
	}
}

type oauthSSEStreamTracker struct {
	reader    *io.PipeReader
	writer    *io.PipeWriter
	done      chan struct{}
	closeOnce sync.Once
	err       error
	outcome   string
}

func newOAuthSSEStreamTracker(maxBytes int) *oauthSSEStreamTracker {
	reader, writer := io.Pipe()
	tracker := &oauthSSEStreamTracker{
		reader: reader,
		writer: writer,
		done:   make(chan struct{}),
	}
	if maxBytes <= 0 {
		maxBytes = defaultMaxUpstreamResponseBody
	}
	go func() {
		defer close(tracker.done)
		defer reader.Close()
		bounded := &oauthSSEBoundedReader{reader: reader, max: maxBytes}
		parser := NewSSEParser(bounded, SSEParserLimits{
			MaxLineBytes:  minPositive(DefaultSSEParserMaxLineBytes, maxBytes),
			MaxEventBytes: minPositive(DefaultSSEParserMaxEventBytes, maxBytes),
			MaxDataBytes:  minPositive(DefaultSSEParserMaxDataBytes, maxBytes),
		})
		for {
			event, err := parser.Next()
			if err != nil {
				if errors.Is(err, io.EOF) {
					tracker.err = errUpstreamSSETerminalMissing
					tracker.outcome = "terminal_missing"
				} else {
					tracker.err = classifyOAuthSSEReadError(context.Background(), err)
					tracker.outcome = oauthSSEOutcomeForError(tracker.err)
				}
				return
			}
			result := parseOAuthSSEEvent(event)
			if result.err != nil {
				tracker.err = result.err
				tracker.outcome = result.outcome
				return
			}
			if result.terminal {
				tracker.outcome = result.outcome
				return
			}
		}
	}()
	return tracker
}

func (t *oauthSSEStreamTracker) write(chunk []byte) {
	if t == nil || t.writer == nil || len(chunk) == 0 {
		return
	}
	_, _ = t.writer.Write(chunk)
}

func (t *oauthSSEStreamTracker) close() {
	if t == nil {
		return
	}
	t.closeOnce.Do(func() {
		_ = t.writer.Close()
		<-t.done
	})
}

func (t *oauthSSEStreamTracker) resultError() error {
	if t == nil {
		return errUpstreamSSETerminalMissing
	}
	return t.err
}

func (t *oauthSSEStreamTracker) resultOutcome() string {
	if t == nil || t.outcome == "" {
		return "upstream_error"
	}
	return t.outcome
}

func oauthSSEOutcomeForError(err error) string {
	switch {
	case errors.Is(err, errUpstreamSSETerminalMissing):
		return "terminal_missing"
	case errors.Is(err, errUpstreamSSETruncated):
		return "truncated"
	case errors.Is(err, errUpstreamSSEMalformed):
		return "malformed"
	case errors.Is(err, errUpstreamSSEError):
		return "error"
	case errors.Is(err, errUpstreamResponseFailed):
		return "failed"
	case errors.Is(err, errUpstreamResponseIncomplete):
		return "incomplete"
	case errors.Is(err, errUpstreamResponseTooLarge):
		return "too_large"
	default:
		return "upstream_error"
	}
}

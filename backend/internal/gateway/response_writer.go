package gateway

import (
	"context"
	"errors"
	"io"
	"net/http"
	"time"

	"github.com/KnowSky404/N2API/backend/internal/requestlog"
)

var (
	errUpstreamResponseTooLarge  = errors.New("upstream response exceeds configured limit")
	errUpstreamResponseRead      = errors.New("upstream response read failed")
	errUpstreamSSEIdleTimeout    = errors.New("upstream SSE stream idle timeout")
	errUpstreamModelError        = errors.New("upstream response reported a model error")
	errUpstreamGenerationMissing = errors.New("upstream response contained no generated output")
)

type upstreamResponseObservation struct {
	Usage           Usage
	ResponseID      string
	StreamOutcome   string
	DiagnosticError string
	ResponseTiming  requestlog.ResponseTiming
}

func (p *Proxy) writeUpstreamResponseWithOptions(ctx context.Context, w http.ResponseWriter, resp *http.Response, route string, stream bool) (upstreamResponseObservation, error) {
	if resp == nil || resp.Body == nil {
		return upstreamResponseObservation{Usage: Usage{Source: "missing"}}, errUpstreamResponseRead
	}
	if stream {
		copyResponseHeaders(w.Header(), resp.Header)
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(resp.StatusCode)
		return copyStreamingResponse(ctx, w, resp.Body, route, p.sseIdleTimeout)
	}
	return writeBufferedUpstreamResponse(w, resp, route, p.maxResponseBody)
}

func writeBufferedUpstreamResponse(w http.ResponseWriter, resp *http.Response, route string, maxResponseBody int) (observation upstreamResponseObservation, err error) {
	tracker := newResponsePhaseTracker(route)
	defer func() {
		observation.ResponseTiming = tracker.snapshot()
	}()
	body, err := io.ReadAll(io.LimitReader(resp.Body, int64(maxResponseBody)+1))
	if err != nil {
		return upstreamResponseObservation{Usage: Usage{Source: "missing"}}, errUpstreamResponseRead
	}
	if len(body) > maxResponseBody {
		return upstreamResponseObservation{Usage: Usage{Source: "missing"}}, errUpstreamResponseTooLarge
	}
	if responseBodyHasUsefulJSON(route, resp.StatusCode, body) {
		tracker.markUseful()
	}
	copyResponseHeaders(w.Header(), resp.Header)
	w.WriteHeader(resp.StatusCode)
	if _, writeErr := w.Write(body); writeErr != nil {
		return upstreamResponseObservation{Usage: Usage{Source: "missing"}}, writeErr
	}
	observation = upstreamResponseObservation{
		Usage:      ParseUsageFromJSON(route, body),
		ResponseID: responseIDFromJSON(route, body),
	}
	if diagnosticErr := responseBodyDiagnosticError(route, resp.StatusCode, body); diagnosticErr != nil {
		return observation, diagnosticErr
	}
	return observation, nil
}

func upstreamResponseErrorCode(err error) string {
	switch {
	case errors.Is(err, errUpstreamResponseTooLarge):
		return "upstream_response_too_large"
	case errors.Is(err, errUpstreamModelError):
		return "upstream_model_error"
	case errors.Is(err, errUpstreamGenerationMissing):
		return "upstream_generation_missing"
	case errors.Is(err, errUpstreamSSEIdleTimeout):
		return "upstream_sse_idle_timeout"
	}
	return "upstream_response_error"
}

func upstreamResponseErrorMessage(err error) string {
	switch {
	case errors.Is(err, errUpstreamResponseTooLarge):
		return "upstream response is too large"
	case errors.Is(err, errUpstreamModelError):
		return "upstream response reported a model error"
	case errors.Is(err, errUpstreamGenerationMissing):
		return "upstream response contained no generated output"
	case errors.Is(err, errUpstreamSSEIdleTimeout):
		return "upstream stream timed out"
	}
	return "could not read upstream response"
}

func copyStreamingResponse(ctx context.Context, w http.ResponseWriter, body io.ReadCloser, route string, idleTimeout time.Duration) (observation upstreamResponseObservation, err error) {
	observer := NewSSEUsageObserver(route)
	phaseTracker := newResponsePhaseTracker(route)
	defer func() {
		observation.ResponseTiming = phaseTracker.snapshot()
	}()
	buffer := make([]byte, 32*1024)
	writer := flushWriter{ResponseWriter: w}
	stopContextClose := context.AfterFunc(ctx, func() {
		_ = body.Close()
	})
	defer stopContextClose()
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
			chunk := buffer[:n]
			observer.Observe(chunk)
			phaseTracker.observeSSE(chunk)
			if _, writeErr := writer.Write(chunk); writeErr != nil {
				return upstreamResponseObservation{Usage: observer.Usage(), ResponseID: observer.ResponseID(), StreamOutcome: "client_canceled", DiagnosticError: "client_canceled"}, nil
			}
		}
		select {
		case <-idleExpired:
			return upstreamResponseObservation{Usage: observer.Usage(), ResponseID: observer.ResponseID(), StreamOutcome: "upstream_error"}, errUpstreamSSEIdleTimeout
		default:
		}
		if readErr != nil {
			if ctx.Err() != nil {
				outcome := "client_canceled"
				diagnosticError := "client_canceled"
				if errors.Is(ctx.Err(), context.DeadlineExceeded) {
					outcome = "server_error"
					diagnosticError = "upstream_response_timeout"
				}
				return upstreamResponseObservation{Usage: observer.Usage(), ResponseID: observer.ResponseID(), StreamOutcome: outcome, DiagnosticError: diagnosticError}, nil
			}
			if errors.Is(readErr, io.EOF) {
				return upstreamResponseObservation{Usage: observer.Usage(), ResponseID: observer.ResponseID(), StreamOutcome: "completed", DiagnosticError: phaseTracker.diagnosticErrorFor(route)}, nil
			}
			return upstreamResponseObservation{Usage: observer.Usage(), ResponseID: observer.ResponseID(), StreamOutcome: "upstream_error"}, errUpstreamResponseRead
		}
	}
}

func responseObservationErrorCode(observation upstreamResponseObservation, err error) string {
	if err != nil {
		return upstreamResponseErrorCode(err)
	}
	return observation.DiagnosticError
}

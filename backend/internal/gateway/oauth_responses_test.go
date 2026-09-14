package gateway

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/KnowSky404/N2API/backend/internal/provider"
)

const completeOAuthResponseBody = `{"id":"resp_full","object":"response","status":"completed","model":"gpt-5.4-mini","output":[{"id":"msg_1","type":"message","role":"assistant","content":[{"type":"output_text","text":"hello","annotations":[]}]},{"id":"fc_1","type":"function_call","name":"lookup","arguments":"{\"q\":\"x\"}"}],"usage":{"input_tokens":11,"output_tokens":2,"total_tokens":13,"input_tokens_details":{"cached_tokens":3},"output_tokens_details":{"reasoning_tokens":1}},"metadata":{"keep":"yes"}}`

func TestProxyAggregatesOAuthResponsesForNonStreamingClients(t *testing.T) {
	stream := []string{
		": heartbeat\r\n\r\n",
		"event: response.output_text.delta\r\n",
		"data: {\"type\":\"response.output_text.delta\",\"delta\":\"hel\"}\r\n\r\n",
		"event: response.completed\r\n",
		"data: {\"type\":\"response.completed\",\"response\":\r\n",
		"data: " + completeOAuthResponseBody + "}\r\n\r\n",
		"data: [DONE]\r\n\r\n",
	}
	for _, test := range []struct {
		name string
		body string
	}{
		{name: "omitted", body: `{"model":"gpt-5.4-mini","input":"hi"}`},
		{name: "false", body: `{"model":"gpt-5.4-mini","input":"hi","stream":false}`},
	} {
		t.Run(test.name, func(t *testing.T) {
			logger := &fakeRequestLogger{}
			var upstreamBody map[string]any
			client := &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
				requestBody, err := io.ReadAll(r.Body)
				if err != nil {
					t.Fatalf("ReadAll request body: %v", err)
				}
				if err := json.Unmarshal(requestBody, &upstreamBody); err != nil {
					t.Fatalf("Unmarshal request body: %v", err)
				}
				return &http.Response{
					StatusCode: http.StatusOK,
					Header:     http.Header{"Content-Type": []string{"text/event-stream"}},
					Body:       &chunkedOAuthReadCloser{chunks: stream},
					Request:    r,
				}, nil
			})}
			proxy := newOAuthResponsesTestProxy(client, logger, Config{})
			req := httptest.NewRequest(http.MethodPost, "/v1/responses", strings.NewReader(test.body))
			req.Header.Set("Authorization", "Bearer n2api_client_secret")
			req.Header.Set("Content-Type", "application/json")
			recorder := httptest.NewRecorder()

			proxy.ServeHTTP(recorder, req)

			if recorder.Code != http.StatusOK {
				t.Fatalf("status = %d, want 200; body=%s", recorder.Code, recorder.Body.String())
			}
			if got := recorder.Header().Get("Content-Type"); got != "application/json" {
				t.Fatalf("Content-Type = %q, want application/json", got)
			}
			if got := recorder.Header().Get("Content-Length"); got != stringLength(completeOAuthResponseBody) {
				t.Fatalf("Content-Length = %q, want %d", got, len(completeOAuthResponseBody))
			}
			if recorder.Body.String() != completeOAuthResponseBody {
				t.Fatalf("response body = %q, want complete response object", recorder.Body.String())
			}
			var response map[string]any
			if err := json.Unmarshal(recorder.Body.Bytes(), &response); err != nil {
				t.Fatalf("Unmarshal response: %v", err)
			}
			if response["id"] != "resp_full" || response["status"] != "completed" || response["metadata"].(map[string]any)["keep"] != "yes" {
				t.Fatalf("response identity/status/metadata = %+v", response)
			}
			if upstreamBody["stream"] != true || upstreamBody["store"] != false {
				t.Fatalf("normalized upstream body = %+v, want stream=true/store=false", upstreamBody)
			}
			assertLastLoggedError(t, logger, "")
			if len(logger.entries) != 1 {
				t.Fatalf("logged entries = %d, want one diagnostic entry", len(logger.entries))
			}
			timing := logger.entries[0].ResponseTiming
			if timing.HeaderWaitMS == nil || timing.FirstUsefulOutputMS == nil || timing.StreamFinishMS == nil {
				t.Fatalf("response timing = %+v, want header wait, first useful output, and finish", timing)
			}
		})
	}
}

func TestProxyReturnsOAuthTerminalFailureObjectWithoutCallingItSuccess(t *testing.T) {
	for _, test := range []struct {
		name      string
		eventType string
		status    string
		wantError string
	}{
		{name: "failed", eventType: "response.failed", status: "failed", wantError: "upstream_response_failed"},
		{name: "incomplete", eventType: "response.incomplete", status: "incomplete", wantError: "upstream_response_incomplete"},
	} {
		t.Run(test.name, func(t *testing.T) {
			logger := &fakeRequestLogger{}
			responseBody := `{"id":"resp_terminal","status":"` + test.status + `","error":{"code":"provider_failed","message":"provider detail"}}`
			stream := "event: " + test.eventType + "\ndata: {\"type\":\"" + test.eventType + "\",\"response\":" + responseBody + "}\n\n"
			client := &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
				return &http.Response{
					StatusCode: http.StatusOK,
					Header:     http.Header{"Content-Type": []string{"text/event-stream"}},
					Body:       io.NopCloser(strings.NewReader(stream)),
					Request:    r,
				}, nil
			})}
			proxy := newOAuthResponsesTestProxy(client, logger, Config{})
			req := httptest.NewRequest(http.MethodPost, "/v1/responses", strings.NewReader(`{"model":"gpt-5.4-mini","input":"hi","stream":false}`))
			req.Header.Set("Authorization", "Bearer n2api_client_secret")
			recorder := httptest.NewRecorder()

			proxy.ServeHTTP(recorder, req)

			if recorder.Code != http.StatusOK || recorder.Body.String() != responseBody {
				t.Fatalf("status/body = %d/%q, want terminal response object with HTTP 200", recorder.Code, recorder.Body.String())
			}
			assertLastLoggedError(t, logger, test.wantError)
		})
	}
}

func TestProxyMapsOAuthResponsesStreamFailuresToStableErrors(t *testing.T) {
	for _, test := range []struct {
		name      string
		stream    string
		wantError string
	}{
		{name: "error event", stream: "event: error\ndata: {\"error\":{\"message\":\"private upstream detail\"}}\n\n", wantError: "upstream_sse_error"},
		{name: "done without terminal", stream: ": heartbeat\n\ndata: [DONE]\n\n", wantError: "upstream_sse_terminal_missing"},
		{name: "malformed event", stream: "data: {not-json}\n\n", wantError: "upstream_sse_malformed"},
		{name: "early EOF", stream: "data: {\"type\":\"response.completed\"", wantError: "upstream_sse_truncated"},
	} {
		t.Run(test.name, func(t *testing.T) {
			logger := &fakeRequestLogger{}
			client := &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
				return &http.Response{
					StatusCode: http.StatusOK,
					Header:     http.Header{"Content-Type": []string{"text/event-stream"}},
					Body:       io.NopCloser(strings.NewReader(test.stream)),
					Request:    r,
				}, nil
			})}
			proxy := newOAuthResponsesTestProxy(client, logger, Config{})
			req := httptest.NewRequest(http.MethodPost, "/v1/responses", strings.NewReader(`{"model":"gpt-5.4-mini","input":"hi"}`))
			req.Header.Set("Authorization", "Bearer n2api_client_secret")
			recorder := httptest.NewRecorder()

			proxy.ServeHTTP(recorder, req)

			if recorder.Code != http.StatusBadGateway {
				t.Fatalf("status = %d, want 502; body=%s", recorder.Code, recorder.Body.String())
			}
			assertLastLoggedError(t, logger, test.wantError)
			if strings.Contains(recorder.Body.String(), "private upstream detail") {
				t.Fatalf("gateway error exposed upstream detail: %s", recorder.Body.String())
			}
		})
	}
}

func TestProxyMapsOAuthResponsesSizeAndIdleFailures(t *testing.T) {
	t.Run("size", func(t *testing.T) {
		logger := &fakeRequestLogger{}
		client := &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
			return &http.Response{
				StatusCode: http.StatusOK,
				Header:     http.Header{"Content-Type": []string{"text/event-stream"}},
				Body:       io.NopCloser(strings.NewReader("data: {\"type\":\"response.output_text.delta\",\"delta\":\"too-large\"}\n\n")),
				Request:    r,
			}, nil
		})}
		proxy := newOAuthResponsesTestProxy(client, logger, Config{MaxUpstreamResponseBodyBytes: 16})
		req := httptest.NewRequest(http.MethodPost, "/v1/responses", strings.NewReader(`{"model":"gpt-5.4-mini","input":"hi"}`))
		req.Header.Set("Authorization", "Bearer n2api_client_secret")
		recorder := httptest.NewRecorder()

		proxy.ServeHTTP(recorder, req)

		if recorder.Code != http.StatusBadGateway {
			t.Fatalf("status = %d, want 502; body=%s", recorder.Code, recorder.Body.String())
		}
		assertLastLoggedError(t, logger, "upstream_response_too_large")
	})

	t.Run("idle", func(t *testing.T) {
		logger := &fakeRequestLogger{}
		body := newBlockingReadCloser()
		client := &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
			return &http.Response{
				StatusCode: http.StatusOK,
				Header:     http.Header{"Content-Type": []string{"text/event-stream"}},
				Body:       body,
				Request:    r,
			}, nil
		})}
		proxy := newOAuthResponsesTestProxy(client, logger, Config{UpstreamSSEIdleTimeout: 10 * time.Millisecond})
		req := httptest.NewRequest(http.MethodPost, "/v1/responses", strings.NewReader(`{"model":"gpt-5.4-mini","input":"hi"}`))
		req.Header.Set("Authorization", "Bearer n2api_client_secret")
		recorder := httptest.NewRecorder()

		proxy.ServeHTTP(recorder, req)

		if recorder.Code != http.StatusBadGateway || !body.isClosed() {
			t.Fatalf("status/body/closed = %d/%q/%v", recorder.Code, recorder.Body.String(), body.isClosed())
		}
		assertLastLoggedError(t, logger, "upstream_sse_idle_timeout")
	})
}

func TestAggregateOAuthResponsesHonorsCancellation(t *testing.T) {
	body := newBlockingReadCloser()
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() {
		_, _, err := (&Proxy{sseIdleTimeout: time.Second}).aggregateOAuthResponses(ctx, body, "/v1/responses")
		done <- err
	}()
	cancel()

	select {
	case err := <-done:
		if !errors.Is(err, errUpstreamResponseCanceled) {
			t.Fatalf("aggregate error = %v, want cancellation", err)
		}
	case <-time.After(250 * time.Millisecond):
		t.Fatal("canceled aggregation did not stop promptly")
	}
}

func TestProxyOAuthStreamingRequiresTerminalResponse(t *testing.T) {
	logger := &fakeRequestLogger{}
	stream := "data: {\"type\":\"response.output_text.delta\",\"delta\":\"partial\"}\n\ndata: [DONE]\n\n"
	client := &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
		return &http.Response{
			StatusCode: http.StatusOK,
			Header:     http.Header{"Content-Type": []string{"text/event-stream"}},
			Body:       io.NopCloser(strings.NewReader(stream)),
			Request:    r,
		}, nil
	})}
	proxy := newOAuthResponsesTestProxy(client, logger, Config{})
	req := httptest.NewRequest(http.MethodPost, "/v1/responses", strings.NewReader(`{"model":"gpt-5.4-mini","input":"hi","stream":true}`))
	req.Header.Set("Authorization", "Bearer n2api_client_secret")
	recorder := &flushRecordingResponseWriter{}

	proxy.ServeHTTP(recorder, req)

	if recorder.status != http.StatusOK || recorder.body.String() != stream {
		t.Fatalf("status/body = %d/%q, want raw upstream stream", recorder.status, recorder.body.String())
	}
	assertLastLoggedError(t, logger, "upstream_sse_terminal_missing")
}

func newOAuthResponsesTestProxy(client *http.Client, logger *fakeRequestLogger, config Config) *Proxy {
	config.Logger = logger
	config.CodexResponsesBaseURL = "https://chatgpt.example.test/backend-api/codex"
	return NewProxyWithClient(&fakeAPIKeyAuthenticator{}, &fakeSelectedAccountProvider{accounts: []SelectedAccount{{
		AccountID:          77,
		AccountType:        provider.AccountTypeCodexOAuth,
		AuthorizationToken: "oauth-access-token",
		ChatGPTAccountID:   "acct_chatgpt",
	}}}, config, client)
}

func stringLength(value string) string {
	return strconv.Itoa(len(value))
}

type chunkedOAuthReadCloser struct {
	chunks []string
	index  int
}

func (r *chunkedOAuthReadCloser) Read(p []byte) (int, error) {
	if r.index >= len(r.chunks) {
		return 0, io.EOF
	}
	chunk := r.chunks[r.index]
	r.index++
	return copy(p, chunk), nil
}

func (*chunkedOAuthReadCloser) Close() error { return nil }

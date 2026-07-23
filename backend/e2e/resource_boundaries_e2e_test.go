package e2e_test

import (
	"bufio"
	"bytes"
	"context"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"strings"
	"testing"
	"time"
)

const (
	e2eAcceptedRequestBodyBytes = 64 << 10
	e2eReplayRequestBodyBytes   = 8 << 10
)

func TestGatewayRejectsKnownLengthAndChunkedOversizedBodies(t *testing.T) {
	tests := []struct {
		name string
		body func() io.Reader
	}{
		{name: "known length", body: func() io.Reader { return strings.NewReader(strings.Repeat("x", e2eAcceptedRequestBodyBytes+1)) }},
		{name: "chunked", body: func() io.Reader {
			return io.NopCloser(strings.NewReader(strings.Repeat("x", e2eAcceptedRequestBodyBytes+1)))
		}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			fixture := newProtocolFixture(t)
			accountID := fixture.createAccount("oversized-request-account", 0, 0)
			_, keyID, secret := fixture.createPoolAndKey("oversized-request", []int64{accountID})
			fixture.resetMock()

			result := mustGatewayBodyRequest(t, fixture.client, fixture.env.baseURL+"/v1/chat/completions", secret, test.body())
			if result.status != http.StatusRequestEntityTooLarge || result.readErr || openAIErrorCode(result.body) != "request_too_large" {
				t.Fatalf("stage=oversized_request field=response status=%d body=%s", result.status, result.body)
			}
			if !strings.HasPrefix(strings.ToLower(result.header.Get("Content-Type")), "application/json") || result.header.Get("X-N2API-E2E-Canary") != "" {
				t.Fatalf("stage=oversized_request field=headers headers=%v", result.header)
			}
			logs := waitForRequestLogCount(t, fixture.database, keyID, nil, 1)
			if logs[0].statusCode != http.StatusRequestEntityTooLarge || logs[0].errorCode != "request_too_large" || logs[0].gatewayAttemptCount != 0 || logs[0].gatewayFallbackCount != 0 || logs[0].providerAccountID != 0 {
				t.Fatalf("stage=oversized_request field=request_log log=%+v", logs[0])
			}
			if fixture.mockScenarioCount("happy") != 0 {
				t.Fatal("stage=oversized_request field=upstream_count")
			}
		})
	}
}

func TestGatewayDoesNotReplayAcceptedBodyAboveReplayLimit(t *testing.T) {
	fixture := newProtocolFixture(t)
	profileID := fixture.createFingerprintProfile("large body service unavailable", "status-503-once")
	firstAccountID := fixture.createAccount("large-body-first", 0, profileID)
	secondAccountID := fixture.createAccount("large-body-second", 1, profileID)
	_, keyID, secret := fixture.createPoolAndKey("large-body", []int64{firstAccountID, secondAccountID})
	fixture.resetMock()
	sessionID := "e2e-large-body-" + fixture.suffix

	result := mustGatewayPOST(t, fixture.client, fixture.env.baseURL+"/v1/chat/completions", secret, map[string]any{
		"model":      e2eModel,
		"session_id": sessionID,
		"messages": []map[string]string{{
			"role": "user", "content": strings.Repeat("x", e2eReplayRequestBodyBytes+1024),
		}},
	})
	if result.status != http.StatusServiceUnavailable || result.readErr || openAIErrorCode(result.body) != "mock_upstream_error" {
		t.Fatalf("stage=large_body field=response status=%d body=%s", result.status, result.body)
	}
	logs := waitForRequestLogCount(t, fixture.database, keyID, []string{sessionID}, 1)
	if logs[0].gatewayAttemptCount != 1 || logs[0].gatewayFallbackCount != 0 || logs[0].errorCode != "upstream_unavailable" {
		t.Fatalf("stage=large_body field=request_log log=%+v", logs[0])
	}
	if fixture.mockScenarioCount("status-503-once") != 1 {
		t.Fatal("stage=large_body field=upstream_count")
	}
}

func TestGatewayTimesOutSlowUploadAndReleasesAdmissionSlots(t *testing.T) {
	fixture := newProtocolFixture(t)
	accountID := fixture.createAccount("slow-upload-account", 0, 0)
	_, keyID, secret := fixture.createPoolAndKey("slow-upload", []int64{accountID})
	fixture.resetMock()

	slow := startSlowChunkedUpload(t, fixture.env.baseURL, secret)
	defer slow.close()
	waitForKeyConcurrency(t, fixture, keyID, 1)

	busy := mustGatewayPOST(t, fixture.client, fixture.env.baseURL+"/v1/chat/completions", secret, map[string]any{
		"model": e2eModel, "messages": []map[string]string{{"role": "user", "content": "busy"}},
	})
	if busy.status != http.StatusTooManyRequests || openAIErrorCode(busy.body) != "rate_limit_exceeded" {
		t.Fatalf("stage=slow_upload field=busy_response status=%d body=%s", busy.status, busy.body)
	}

	var timedOut gatewayResult
	select {
	case timedOut = <-slow.result:
	case <-time.After(3 * time.Second):
		t.Fatal("stage=slow_upload failure=timeout_response_missing")
	}
	if timedOut.status != http.StatusRequestTimeout || timedOut.readErr || openAIErrorCode(timedOut.body) != "request_body_timeout" {
		t.Fatalf("stage=slow_upload field=timeout_response status=%d body=%s", timedOut.status, timedOut.body)
	}

	sessionID := "e2e-after-slow-upload-" + fixture.suffix
	after := mustGatewayPOST(t, fixture.client, fixture.env.baseURL+"/v1/chat/completions", secret, map[string]any{
		"model": e2eModel, "session_id": sessionID, "messages": []map[string]string{{"role": "user", "content": "after"}},
	})
	if after.status != http.StatusOK || after.readErr {
		t.Fatalf("stage=slow_upload field=after_response status=%d body=%s", after.status, after.body)
	}

	logs := waitForRequestLogCount(t, fixture.database, keyID, nil, 3)
	assertBoundaryLog(t, logs, "request_body_timeout", http.StatusRequestTimeout, 0, 0)
	assertBoundaryLog(t, logs, "api_key_concurrency_limited", http.StatusTooManyRequests, 0, 0)
	if success, ok := findRequestLog(logs, sessionID); !ok || success.statusCode != http.StatusOK || success.errorCode != "" || success.gatewayAttemptCount != 1 {
		t.Fatalf("stage=slow_upload field=success_log logs=%+v", logs)
	}
	if fixture.mockScenarioCount("happy") != 1 {
		t.Fatal("stage=slow_upload field=upstream_count")
	}
}

func TestGatewayBoundsUpstreamHeadersBodiesAndStreams(t *testing.T) {
	t.Run("response header timeout", func(t *testing.T) {
		fixture := newProtocolFixture(t)
		profileID := fixture.createFingerprintProfile("response header timeout", "timeout-before-headers")
		accountID := fixture.createAccount("response-header-timeout", 0, profileID)
		_, keyID, secret := fixture.createPoolAndKey("response-header-timeout", []int64{accountID})
		fixture.resetMock()
		sessionID := "e2e-response-header-timeout-" + fixture.suffix

		result := mustGatewayPOST(t, fixture.client, fixture.env.baseURL+"/v1/chat/completions", secret, map[string]any{
			"model": e2eModel, "session_id": sessionID,
			"messages": []map[string]string{{"role": "user", "content": strings.Repeat("x", e2eReplayRequestBodyBytes+1024)}},
		})
		if result.status != http.StatusBadGateway || result.readErr || openAIErrorCode(result.body) != "upstream_timeout" || !strings.HasPrefix(strings.ToLower(result.header.Get("Content-Type")), "application/json") {
			t.Fatalf("stage=response_header_timeout field=response status=%d headers=%v body=%s", result.status, result.header, result.body)
		}
		logs := waitForRequestLogCount(t, fixture.database, keyID, []string{sessionID}, 1)
		if logs[0].errorCode != "upstream_timeout" || logs[0].gatewayAttemptCount != 1 || logs[0].gatewayFallbackCount != 0 {
			t.Fatalf("stage=response_header_timeout field=request_log log=%+v", logs[0])
		}
		if fixture.mockScenarioCount("timeout-before-headers") != 1 {
			t.Fatal("stage=response_header_timeout field=upstream_count")
		}
	})

	t.Run("oversized JSON response", func(t *testing.T) {
		fixture := newProtocolFixture(t)
		profileID := fixture.createFingerprintProfile("oversized JSON response", "oversized-json-response")
		accountID := fixture.createAccount("oversized-json-response", 0, profileID)
		_, keyID, secret := fixture.createPoolAndKey("oversized-json-response", []int64{accountID})
		fixture.resetMock()
		sessionID := "e2e-oversized-json-response-" + fixture.suffix

		result := mustGatewayPOST(t, fixture.client, fixture.env.baseURL+"/v1/chat/completions", secret, map[string]any{
			"model": e2eModel, "session_id": sessionID, "messages": []map[string]string{{"role": "user", "content": "oversized"}},
		})
		if result.status != http.StatusBadGateway || result.readErr || openAIErrorCode(result.body) != "upstream_response_too_large" || bytes.Contains(result.body, []byte("response-canary")) || result.header.Get("X-N2API-E2E-Canary") != "" {
			t.Fatalf("stage=oversized_response field=response status=%d headers=%v body=%s", result.status, result.header, result.body)
		}
		logs := waitForRequestLogCount(t, fixture.database, keyID, []string{sessionID}, 1)
		if logs[0].errorCode != "upstream_response_too_large" || logs[0].usageSource != "missing" || logs[0].gatewayAttemptCount != 1 || logs[0].gatewayFallbackCount != 0 {
			t.Fatalf("stage=oversized_response field=request_log log=%+v", logs[0])
		}
	})

	t.Run("stalled SSE", func(t *testing.T) {
		fixture := newProtocolFixture(t)
		profileID := fixture.createFingerprintProfile("stalled SSE", "stalled-sse")
		accountID := fixture.createAccount("stalled-sse", 0, profileID)
		_, keyID, secret := fixture.createPoolAndKey("stalled-sse", []int64{accountID})
		fixture.resetMock()
		sessionID := "e2e-stalled-sse-" + fixture.suffix

		result := mustGatewayPOST(t, fixture.client, fixture.env.baseURL+"/v1/responses", secret, map[string]any{
			"model": e2eModel, "session_id": sessionID, "input": "stall", "stream": true,
		})
		if result.status != http.StatusOK || result.readErr || len(result.body) != 0 || !strings.HasPrefix(strings.ToLower(result.header.Get("Content-Type")), "text/event-stream") {
			t.Fatalf("stage=stalled_sse field=response status=%d headers=%v body=%s", result.status, result.header, result.body)
		}
		logs := waitForRequestLogCount(t, fixture.database, keyID, []string{sessionID}, 1)
		if logs[0].errorCode != "upstream_sse_idle_timeout" || logs[0].gatewayAttemptCount != 1 || logs[0].gatewayFallbackCount != 0 {
			t.Fatalf("stage=stalled_sse field=request_log log=%+v", logs[0])
		}
	})

	t.Run("periodic SSE", func(t *testing.T) {
		fixture := newProtocolFixture(t)
		profileID := fixture.createFingerprintProfile("periodic SSE", "periodic-sse")
		accountID := fixture.createAccount("periodic-sse", 0, profileID)
		_, keyID, secret := fixture.createPoolAndKey("periodic-sse", []int64{accountID})
		fixture.resetMock()
		sessionID := "e2e-periodic-sse-" + fixture.suffix

		result := mustGatewayPOST(t, fixture.client, fixture.env.baseURL+"/v1/responses", secret, map[string]any{
			"model": e2eModel, "session_id": sessionID, "input": "periodic", "stream": true,
		})
		if result.status != http.StatusOK || result.readErr || !hasSSEEvent(result.body, "response.completed") {
			t.Fatalf("stage=periodic_sse field=response status=%d body=%s", result.status, result.body)
		}
		logs := waitForRequestLogCount(t, fixture.database, keyID, []string{sessionID}, 1)
		if logs[0].errorCode != "" || logs[0].usageSource != "stream" || logs[0].totalTokens != 2 || logs[0].gatewayAttemptCount != 1 || logs[0].gatewayFallbackCount != 0 {
			t.Fatalf("stage=periodic_sse field=request_log log=%+v", logs[0])
		}
	})
}

func mustGatewayBodyRequest(t *testing.T, client *http.Client, target, secret string, body io.Reader) gatewayResult {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), requestTimeout)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, target, body)
	if err != nil {
		t.Fatal("stage=gateway_body_request failure=create_request")
	}
	req.Header.Set("Authorization", "Bearer "+secret)
	req.Header.Set("Content-Type", "application/json")
	resp, err := client.Do(req)
	if err != nil {
		t.Fatal("stage=gateway_body_request failure=send_request")
	}
	defer resp.Body.Close()
	responseBody, readErr := io.ReadAll(io.LimitReader(resp.Body, maxResponseBytes))
	return gatewayResult{status: resp.StatusCode, header: resp.Header.Clone(), body: responseBody, readErr: readErr != nil}
}

type slowChunkedUpload struct {
	conn   net.Conn
	result <-chan gatewayResult
}

func startSlowChunkedUpload(t *testing.T, baseURL, secret string) slowChunkedUpload {
	t.Helper()
	parsed, err := url.Parse(baseURL)
	if err != nil || parsed.Host == "" {
		t.Fatal("stage=slow_upload failure=parse_base_url")
	}
	conn, err := net.DialTimeout("tcp", parsed.Host, time.Second)
	if err != nil {
		t.Fatal("stage=slow_upload failure=dial")
	}
	_ = conn.SetDeadline(time.Now().Add(5 * time.Second))
	partial := `{"model":"gpt-5","messages":[{"role":"user","content":"unfinished`
	if _, err := fmt.Fprintf(conn, "POST /v1/chat/completions HTTP/1.1\r\nHost: %s\r\nAuthorization: Bearer %s\r\nContent-Type: application/json\r\nTransfer-Encoding: chunked\r\nConnection: close\r\n\r\n%x\r\n%s\r\n", parsed.Host, secret, len(partial), partial); err != nil {
		_ = conn.Close()
		t.Fatal("stage=slow_upload failure=write_partial_request")
	}
	result := make(chan gatewayResult, 1)
	go func() {
		resp, responseErr := http.ReadResponse(bufio.NewReader(conn), &http.Request{Method: http.MethodPost})
		if responseErr != nil {
			result <- gatewayResult{readErr: true}
			return
		}
		defer resp.Body.Close()
		body, readErr := io.ReadAll(io.LimitReader(resp.Body, maxResponseBytes))
		result <- gatewayResult{status: resp.StatusCode, header: resp.Header.Clone(), body: body, readErr: readErr != nil}
	}()
	return slowChunkedUpload{conn: conn, result: result}
}

func (upload slowChunkedUpload) close() {
	if upload.conn != nil {
		_ = upload.conn.Close()
	}
}

func waitForKeyConcurrency(t *testing.T, fixture *protocolFixture, keyID int64, expected int) {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for {
		var response struct {
			Keys []struct {
				ID                        int64 `json:"id"`
				CurrentConcurrentRequests int   `json:"currentConcurrentRequests"`
			} `json:"keys"`
		}
		status, failure := performJSON(context.Background(), fixture.client, http.MethodGet, fixture.env.baseURL+"/api/admin/keys", nil, nil, &response)
		if failure == "" && status == http.StatusOK {
			for _, key := range response.Keys {
				if key.ID == keyID && key.CurrentConcurrentRequests == expected {
					return
				}
			}
		}
		if time.Now().After(deadline) {
			t.Fatalf("stage=slow_upload field=key_concurrency expected=%d", expected)
		}
		time.Sleep(25 * time.Millisecond)
	}
}

func assertBoundaryLog(t *testing.T, logs []requestLog, errorCode string, status, attempts, fallbacks int) {
	t.Helper()
	for _, entry := range logs {
		if entry.errorCode == errorCode {
			if entry.statusCode != status || entry.gatewayAttemptCount != attempts || entry.gatewayFallbackCount != fallbacks {
				t.Fatalf("stage=boundary_log error=%s log=%+v", errorCode, entry)
			}
			return
		}
	}
	t.Fatalf("stage=boundary_log missing=%s logs=%+v", errorCode, logs)
}

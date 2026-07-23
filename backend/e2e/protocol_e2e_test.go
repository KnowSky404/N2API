package e2e_test

import (
	"context"
	"net/http"
	"strings"
	"testing"
	"time"
)

func TestGatewayNormalizesRetryableUpstreamStatusLogs(t *testing.T) {
	tests := []struct {
		scenario string
		status   int
		logError string
	}{
		{scenario: "status-401", status: http.StatusUnauthorized, logError: "upstream_unauthorized"},
		{scenario: "status-403", status: http.StatusForbidden, logError: "upstream_forbidden"},
		{scenario: "status-429", status: http.StatusTooManyRequests, logError: "upstream_rate_limited"},
		{scenario: "status-500", status: http.StatusInternalServerError, logError: "upstream_unavailable"},
		{scenario: "status-503", status: http.StatusServiceUnavailable, logError: "upstream_unavailable"},
	}
	for _, test := range tests {
		t.Run(test.scenario, func(t *testing.T) {
			fixture := newProtocolFixture(t)
			profileID := fixture.createFingerprintProfile(test.scenario, test.scenario)
			accountIDs := make([]int64, 0, 5)
			for index := 0; index < 5; index++ {
				accountIDs = append(accountIDs, fixture.createAccount(test.scenario+"-account-"+int64String(int64(index)), index, profileID))
			}
			_, keyID, secret := fixture.createPoolAndKey(test.scenario, accountIDs)
			sessionID := "e2e-" + test.scenario + "-" + fixture.suffix

			result := mustGatewayPOST(t, fixture.client, fixture.env.baseURL+"/v1/chat/completions", secret, map[string]any{
				"model":      e2eModel,
				"session_id": sessionID,
				"messages":   []map[string]string{{"role": "user", "content": "protocol contract"}},
			})
			if result.status != test.status {
				t.Fatalf("stage=gateway_status scenario=%s field=status actual=%d expected=%d", test.scenario, result.status, test.status)
			}
			if result.readErr {
				t.Fatalf("stage=gateway_status scenario=%s failure=read_response", test.scenario)
			}
			if code := openAIErrorCode(result.body); code != "mock_upstream_error" {
				t.Fatalf("stage=gateway_status scenario=%s field=error_code actual=%s expected=mock_upstream_error", test.scenario, code)
			}

			logs := waitForRequestLogCount(t, fixture.database, keyID, []string{sessionID}, 1)
			entry := logs[0]
			if entry.statusCode != test.status || entry.errorCode != test.logError {
				t.Fatalf("stage=request_logs scenario=%s field=status_or_error", test.scenario)
			}
			if entry.gatewayAttemptCount != 5 || entry.gatewayFallbackCount != 4 {
				t.Fatalf("stage=request_logs scenario=%s field=retry_counts attempts=%d fallbacks=%d", test.scenario, entry.gatewayAttemptCount, entry.gatewayFallbackCount)
			}
			if entry.providerAccountID <= 0 {
				t.Fatalf("stage=request_logs scenario=%s field=provider_account_id", test.scenario)
			}
		})
	}
}

func TestGatewayHandlesUpstreamContentTypeAndUsageVariants(t *testing.T) {
	tests := []struct {
		scenario     string
		wantUsage    string
		wantTotal    int
		wantJSONType bool
	}{
		{scenario: "missing-content-type", wantUsage: "chat_completions", wantTotal: 25},
		{scenario: "wrong-content-type", wantUsage: "chat_completions", wantTotal: 25},
		{scenario: "missing-usage", wantUsage: "missing", wantJSONType: true},
		{scenario: "malformed-usage", wantUsage: "chat_completions", wantJSONType: true},
	}
	for _, test := range tests {
		t.Run(test.scenario, func(t *testing.T) {
			fixture := newProtocolFixture(t)
			profileID := fixture.createFingerprintProfile(test.scenario, test.scenario)
			accountID := fixture.createAccount(test.scenario+"-account", 0, profileID)
			_, keyID, secret := fixture.createPoolAndKey(test.scenario, []int64{accountID})
			sessionID := "e2e-" + test.scenario + "-" + fixture.suffix

			result := mustGatewayPOST(t, fixture.client, fixture.env.baseURL+"/v1/chat/completions", secret, map[string]any{
				"model":      e2eModel,
				"session_id": sessionID,
				"messages":   []map[string]string{{"role": "user", "content": "protocol contract"}},
			})
			if result.status != http.StatusOK || result.readErr {
				t.Fatalf("stage=gateway_variant scenario=%s field=response", test.scenario)
			}
			if object := responseObject(result.body); object != "chat.completion" {
				t.Fatalf("stage=gateway_variant scenario=%s field=object", test.scenario)
			}
			contentType := strings.ToLower(result.header.Get("Content-Type"))
			if test.wantJSONType && !strings.HasPrefix(contentType, "application/json") {
				t.Fatalf("stage=gateway_variant scenario=%s field=content_type", test.scenario)
			}
			if !test.wantJSONType && strings.HasPrefix(contentType, "application/json") {
				t.Fatalf("stage=gateway_variant scenario=%s field=content_type", test.scenario)
			}

			logs := waitForRequestLogCount(t, fixture.database, keyID, []string{sessionID}, 1)
			entry := logs[0]
			if entry.statusCode != http.StatusOK || entry.errorCode != "" || entry.usageSource != test.wantUsage {
				t.Fatalf("stage=request_logs scenario=%s field=status_error_or_usage", test.scenario)
			}
			if entry.totalTokens != test.wantTotal {
				t.Fatalf("stage=request_logs scenario=%s field=total_tokens actual=%d expected=%d", test.scenario, entry.totalTokens, test.wantTotal)
			}
			if test.wantTotal == 0 && (entry.inputTokens != 0 || entry.outputTokens != 0 || entry.cachedInputTokens != 0 || entry.reasoningTokens != 0 || entry.estimatedCostMicrousd != 0) {
				t.Fatalf("stage=request_logs scenario=%s field=zero_usage", test.scenario)
			}
		})
	}
}

func TestGatewayHandlesResponsesStreamUsageVariants(t *testing.T) {
	tests := []struct {
		scenario  string
		wantUsage string
	}{
		{scenario: "missing-usage", wantUsage: "missing"},
		{scenario: "malformed-usage", wantUsage: "stream"},
	}
	for _, test := range tests {
		t.Run(test.scenario, func(t *testing.T) {
			fixture := newProtocolFixture(t)
			profileID := fixture.createFingerprintProfile("responses "+test.scenario, test.scenario)
			accountID := fixture.createAccount("responses-"+test.scenario+"-account", 0, profileID)
			_, keyID, secret := fixture.createPoolAndKey("responses-"+test.scenario, []int64{accountID})
			sessionID := "e2e-responses-" + test.scenario + "-" + fixture.suffix

			result := mustGatewayPOST(t, fixture.client, fixture.env.baseURL+"/v1/responses", secret, map[string]any{
				"model":      e2eModel,
				"session_id": sessionID,
				"input":      "protocol contract",
				"stream":     true,
			})
			if result.status != http.StatusOK || result.readErr || !hasSSEEvent(result.body, "response.completed") {
				t.Fatalf("stage=responses_usage scenario=%s field=response", test.scenario)
			}

			logs := waitForRequestLogCount(t, fixture.database, keyID, []string{sessionID}, 1)
			entry := logs[0]
			if entry.statusCode != http.StatusOK || entry.errorCode != "" || entry.usageSource != test.wantUsage {
				t.Fatalf("stage=responses_usage scenario=%s field=status_error_or_usage status=%d error=%s usage=%s expected_usage=%s", test.scenario, entry.statusCode, entry.errorCode, entry.usageSource, test.wantUsage)
			}
			if entry.inputTokens != 0 || entry.outputTokens != 0 || entry.totalTokens != 0 || entry.cachedInputTokens != 0 || entry.reasoningTokens != 0 || entry.estimatedCostMicrousd != 0 {
				t.Fatalf("stage=responses_usage scenario=%s field=zero_usage", test.scenario)
			}
		})
	}
}

func TestGatewayFallsBackWhenUpstreamDisconnectsBeforeHeaders(t *testing.T) {
	fixture := newProtocolFixture(t)
	disconnectProfileID := fixture.createFingerprintProfile("disconnect before headers", "disconnect-before-headers")
	failedAccountID := fixture.createAccount("disconnect-before-account", 0, disconnectProfileID)
	happyAccountID := fixture.createAccount("disconnect-fallback-account", 1, 0)
	_, keyID, secret := fixture.createPoolAndKey("disconnect-before", []int64{failedAccountID, happyAccountID})
	fixture.resetMock()
	sessionID := "e2e-disconnect-before-" + fixture.suffix

	result := mustGatewayPOST(t, fixture.client, fixture.env.baseURL+"/v1/responses", secret, map[string]any{
		"model":      e2eModel,
		"session_id": sessionID,
		"input":      "protocol contract",
		"stream":     true,
	})
	if result.status != http.StatusOK || result.readErr || !hasSSEEvent(result.body, "response.completed") {
		t.Fatal("stage=disconnect_before field=fallback_response")
	}
	logs := waitForRequestLogCount(t, fixture.database, keyID, []string{sessionID}, 1)
	entry := logs[0]
	if entry.providerAccountID != happyAccountID || entry.gatewayAttemptCount != 2 || entry.gatewayFallbackCount != 1 || entry.errorCode != "" {
		t.Fatal("stage=disconnect_before field=request_log")
	}
	if fixture.mockScenarioCount("disconnect-before-headers") != 1 || fixture.mockScenarioCount("happy") != 1 {
		t.Fatal("stage=disconnect_before field=mock_counts")
	}
}

func TestGatewayFallsBackAfterOnePreStreamServiceUnavailable(t *testing.T) {
	fixture := newProtocolFixture(t)
	retryProfileID := fixture.createFingerprintProfile("service unavailable once", "status-503-once")
	failedAccountID := fixture.createAccount("status-once-account", 0, retryProfileID)
	happyAccountID := fixture.createAccount("status-once-fallback-account", 1, retryProfileID)
	_, keyID, secret := fixture.createPoolAndKey("status-once", []int64{failedAccountID, happyAccountID})
	fixture.resetMock()
	sessionID := "e2e-status-once-" + fixture.suffix

	result := mustGatewayPOST(t, fixture.client, fixture.env.baseURL+"/v1/responses", secret, map[string]any{
		"model":      e2eModel,
		"session_id": sessionID,
		"input":      "protocol contract",
		"stream":     true,
	})
	if result.status != http.StatusOK || result.readErr || !hasSSEEvent(result.body, "response.completed") {
		t.Fatal("stage=status_once field=fallback_response")
	}
	logs := waitForRequestLogCount(t, fixture.database, keyID, []string{sessionID}, 1)
	entry := logs[0]
	if entry.providerAccountID != happyAccountID || entry.gatewayAttemptCount != 2 || entry.gatewayFallbackCount != 1 || entry.errorCode != "" {
		t.Fatal("stage=status_once field=request_log")
	}
	if fixture.mockScenarioCount("status-503-once") != 2 {
		t.Fatal("stage=status_once field=mock_count")
	}
}

func TestGatewayDoesNotRetryAfterFirstStreamEvent(t *testing.T) {
	fixture := newProtocolFixture(t)
	disconnectProfileID := fixture.createFingerprintProfile("disconnect after event", "disconnect-after-first-event")
	failedAccountID := fixture.createAccount("disconnect-after-account", 0, disconnectProfileID)
	happyAccountID := fixture.createAccount("disconnect-unused-account", 1, 0)
	_, keyID, secret := fixture.createPoolAndKey("disconnect-after", []int64{failedAccountID, happyAccountID})
	fixture.resetMock()
	sessionID := "e2e-disconnect-after-" + fixture.suffix

	result := mustGatewayPOST(t, fixture.client, fixture.env.baseURL+"/v1/responses", secret, map[string]any{
		"model":      e2eModel,
		"session_id": sessionID,
		"input":      "protocol contract",
		"stream":     true,
	})
	if result.status != http.StatusOK || result.readErr || !hasSSEEvent(result.body, "response.output_text.delta") || hasSSEEvent(result.body, "response.completed") {
		t.Fatal("stage=disconnect_after field=partial_response")
	}
	logs := waitForRequestLogCount(t, fixture.database, keyID, []string{sessionID}, 1)
	entry := logs[0]
	if entry.providerAccountID != failedAccountID || entry.gatewayAttemptCount != 1 || entry.gatewayFallbackCount != 0 {
		t.Fatal("stage=disconnect_after field=request_log")
	}
	if fixture.mockScenarioCount("disconnect-after-first-event") != 1 || fixture.mockScenarioCount("happy") != 0 {
		t.Fatal("stage=disconnect_after field=mock_counts")
	}
}

func TestGatewayDoesNotRetryWhenStreamEndsWithoutCompletion(t *testing.T) {
	fixture := newProtocolFixture(t)
	missingProfileID := fixture.createFingerprintProfile("missing completion", "missing-completion")
	accountID := fixture.createAccount("missing-completion-account", 0, missingProfileID)
	_, keyID, secret := fixture.createPoolAndKey("missing-completion", []int64{accountID})
	fixture.resetMock()
	sessionID := "e2e-missing-completion-" + fixture.suffix

	result := mustGatewayPOST(t, fixture.client, fixture.env.baseURL+"/v1/responses", secret, map[string]any{
		"model":      e2eModel,
		"session_id": sessionID,
		"input":      "protocol contract",
		"stream":     true,
	})
	if result.status != http.StatusOK || result.readErr || !hasSSEEvent(result.body, "response.output_text.delta") || hasSSEEvent(result.body, "response.completed") {
		t.Fatal("stage=missing_completion field=partial_response")
	}
	logs := waitForRequestLogCount(t, fixture.database, keyID, []string{sessionID}, 1)
	entry := logs[0]
	if entry.providerAccountID != accountID || entry.gatewayAttemptCount != 1 || entry.gatewayFallbackCount != 0 || entry.usageSource != "missing" || entry.errorCode != "" {
		t.Fatal("stage=missing_completion field=request_log")
	}
	if entry.inputTokens != 0 || entry.outputTokens != 0 || entry.totalTokens != 0 || entry.cachedInputTokens != 0 || entry.reasoningTokens != 0 || entry.estimatedCostMicrousd != 0 {
		t.Fatal("stage=missing_completion field=zero_usage")
	}
	if fixture.mockScenarioCount("missing-completion") != 1 {
		t.Fatal("stage=missing_completion field=mock_count")
	}
}

func TestGatewayStickySessionUsesSameAccountTwice(t *testing.T) {
	fixture := newProtocolFixture(t)
	firstAccountID := fixture.createAccount("sticky-first-account", 0, 0)
	secondAccountID := fixture.createAccount("sticky-second-account", 0, 0)
	poolID, keyID, secret := fixture.createPoolAndKey("sticky", []int64{firstAccountID, secondAccountID})
	sessionID := "e2e-sticky-" + fixture.suffix
	request := map[string]any{
		"model":      e2eModel,
		"session_id": sessionID,
		"messages":   []map[string]string{{"role": "user", "content": "protocol contract"}},
	}

	for requestNumber := 0; requestNumber < 2; requestNumber++ {
		result := mustGatewayPOST(t, fixture.client, fixture.env.baseURL+"/v1/chat/completions", secret, request)
		if result.status != http.StatusOK || result.readErr {
			t.Fatalf("stage=sticky_request request=%d field=response", requestNumber+1)
		}
	}
	logs := waitForRequestLogCount(t, fixture.database, keyID, []string{sessionID}, 2)
	if logs[0].providerAccountID <= 0 || logs[0].providerAccountID != logs[1].providerAccountID {
		t.Fatal("stage=sticky_request field=provider_account_id")
	}

	ctx, cancel := context.WithTimeout(context.Background(), requestTimeout)
	defer cancel()
	var boundAccountID int64
	err := fixture.database.QueryRow(ctx, `
		SELECT account_id
		FROM provider_session_bindings
		WHERE provider = 'openai' AND model = $1 AND session_id = $2 AND routing_pool_id = $3
	`, e2eModel, sessionID, poolID).Scan(&boundAccountID)
	if err != nil {
		t.Fatal("stage=sticky_binding failure=query")
	}
	if boundAccountID != logs[0].providerAccountID {
		t.Fatal("stage=sticky_binding field=provider_account_id")
	}
}

func TestGatewayEnforcesPerKeyRequestLimitBeforeUpstream(t *testing.T) {
	fixture := newProtocolFixture(t)
	accountID := fixture.createAccount("rate-limit-account", 0, 0)
	_, keyID, secret := fixture.createPoolAndKey("rate-limit", []int64{accountID})
	mustJSON(t, fixture.client, http.MethodPut, fixture.env.baseURL, "/api/admin/keys/"+int64String(keyID)+"/limits", "key_limits", nil, map[string]int{
		"requestsPerMinute": 1,
		"tokensPerMinute":   0,
	}, nil, http.StatusOK)
	fixture.resetMock()

	firstSessionID := "e2e-rate-first-" + fixture.suffix
	first := mustGatewayPOST(t, fixture.client, fixture.env.baseURL+"/v1/chat/completions", secret, map[string]any{
		"model":      e2eModel,
		"session_id": firstSessionID,
		"messages":   []map[string]string{{"role": "user", "content": "first"}},
	})
	if first.status != http.StatusOK || first.readErr {
		t.Fatal("stage=rate_limit request=first field=response")
	}
	second := mustGatewayPOST(t, fixture.client, fixture.env.baseURL+"/v1/chat/completions", secret, map[string]any{
		"model":      e2eModel,
		"session_id": "e2e-rate-second-" + fixture.suffix,
		"messages":   []map[string]string{{"role": "user", "content": "second"}},
	})
	if second.status != http.StatusTooManyRequests || openAIErrorCode(second.body) != "rate_limit_exceeded" || second.header.Get("Retry-After") == "" {
		t.Fatal("stage=rate_limit request=second field=response")
	}

	logs := waitForRequestLogCount(t, fixture.database, keyID, nil, 2)
	if logs[0].statusCode != http.StatusOK || logs[0].sessionID != firstSessionID {
		t.Fatal("stage=rate_limit request=first field=request_log")
	}
	if logs[1].statusCode != http.StatusTooManyRequests || logs[1].errorCode != "api_key_request_rate_limited" || logs[1].gatewayAttemptCount != 0 || logs[1].providerAccountID != 0 {
		t.Fatal("stage=rate_limit request=second field=request_log")
	}
	if fixture.mockScenarioCount("happy") != 1 {
		t.Fatal("stage=rate_limit field=mock_count")
	}
}

func TestGatewayPropagatesCancellationWhileUpstreamWaitsForHeaders(t *testing.T) {
	fixture := newProtocolFixture(t)
	timeoutProfileID := fixture.createFingerprintProfile("timeout before headers", "timeout-before-headers")
	accountID := fixture.createAccount("timeout-account", 0, timeoutProfileID)
	_, keyID, secret := fixture.createPoolAndKey("timeout-cancel", []int64{accountID})
	fixture.resetMock()
	sessionID := "e2e-timeout-cancel-" + fixture.suffix

	ctx, cancel := context.WithCancel(context.Background())
	resultCh := make(chan string, 1)
	go func() {
		_, failure := performGatewayPOST(ctx, fixture.client, fixture.env.baseURL+"/v1/chat/completions", secret, map[string]any{
			"model":      e2eModel,
			"session_id": sessionID,
			"messages":   []map[string]string{{"role": "user", "content": "cancel"}},
		})
		resultCh <- failure
	}()
	fixture.waitForMockScenario("timeout-before-headers", 1)
	cancel()

	select {
	case failure := <-resultCh:
		if failure != "send_request" {
			t.Fatalf("stage=timeout_cancel field=client_result actual=%s expected=send_request", failure)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("stage=timeout_cancel failure=request_did_not_cancel")
	}
	logs := waitForRequestLogCount(t, fixture.database, keyID, []string{sessionID}, 1)
	if logs[0].statusCode != http.StatusOK || logs[0].errorCode != "request_canceled" || logs[0].gatewayAttemptCount != 1 || logs[0].gatewayFallbackCount != 0 {
		t.Fatal("stage=timeout_cancel field=request_log")
	}
}

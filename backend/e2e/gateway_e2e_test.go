package e2e_test

import (
	"net/http"
	"testing"
	"time"
)

func TestGatewayPostgresBackedHappyPath(t *testing.T) {
	env := loadE2EEnvironment(t)
	client := newE2EHTTPClient(t)
	database := newE2EDatabase(t, env.databaseURL)
	resources := &e2eResources{}
	registerCleanup(t, client, env, resources)
	suffix := uniqueE2ESuffix(t)
	chatSessionID := "e2e-chat-" + suffix
	responsesSessionID := "e2e-responses-" + suffix

	mustJSON(t, client, http.MethodPost, env.baseURL, "/api/admin/login", "admin_login", nil, map[string]string{
		"username": env.adminUsername,
		"password": env.adminPassword,
	}, nil, http.StatusOK)

	mustJSON(t, client, http.MethodGet, env.baseURL, "/api/admin/usage-pricing", "pricing_read", nil, nil, &resources.pricingBefore, http.StatusOK)
	pricing := clonePricing(resources.pricingBefore)
	pricing.Models[e2eModel] = usagePrice{
		InputMicrousdPerMillion:       1_000_000,
		CachedInputMicrousdPerMillion: 2_000_000,
		OutputMicrousdPerMillion:      3_000_000,
	}
	var savedPricing usagePricing
	mustJSON(t, client, http.MethodPut, env.baseURL, "/api/admin/usage-pricing", "pricing_update", nil, pricing, &savedPricing, http.StatusOK)
	resources.pricingUpdated = true
	resources.pricingUpdatedAt = savedPricing.UpdatedAt
	if resources.pricingUpdatedAt.IsZero() {
		t.Fatal("stage=pricing_update field=updated_at")
	}
	if _, ok := savedPricing.Models[e2eModel]; !ok {
		t.Fatal("stage=pricing_update field=model")
	}

	var accountResponse struct {
		Account struct {
			ID int64 `json:"id"`
		} `json:"account"`
	}
	resources.accountName = "E2E upstream " + suffix
	mustJSON(t, client, http.MethodPost, env.baseURL, "/api/admin/provider-accounts/api-upstream", "account_create", nil, map[string]any{
		"name":       resources.accountName,
		"baseUrl":    env.mockBaseURL,
		"apiKey":     env.mockAPIKey,
		"enabled":    true,
		"priority":   0,
		"loadFactor": 1,
		"models":     []string{e2eModel},
	}, &accountResponse, http.StatusCreated)
	resources.accountID = accountResponse.Account.ID
	if resources.accountID <= 0 {
		t.Fatal("stage=account_create field=id")
	}

	var modelResponse struct {
		Models []struct {
			Model   string `json:"model"`
			Enabled bool   `json:"enabled"`
		} `json:"models"`
	}
	mustJSON(t, client, http.MethodGet, env.baseURL, "/api/admin/provider-accounts/"+int64String(resources.accountID)+"/models", "account_models", nil, nil, &modelResponse, http.StatusOK)
	if len(modelResponse.Models) != 1 || modelResponse.Models[0].Model != e2eModel || !modelResponse.Models[0].Enabled {
		t.Fatal("stage=account_models field=models")
	}

	var poolResponse struct {
		Pool struct {
			ID int64 `json:"id"`
		} `json:"pool"`
	}
	resources.poolName = "e2e-pool-" + suffix
	mustJSON(t, client, http.MethodPost, env.baseURL, "/api/admin/routing-pools", "routing_pool_create", nil, map[string]any{
		"name":        resources.poolName,
		"description": "isolated gateway e2e",
		"enabled":     true,
	}, &poolResponse, http.StatusCreated)
	resources.poolID = poolResponse.Pool.ID
	if resources.poolID <= 0 {
		t.Fatal("stage=routing_pool_create field=id")
	}

	mustJSON(t, client, http.MethodPut, env.baseURL, "/api/admin/routing-pools/"+int64String(resources.poolID)+"/accounts", "routing_pool_membership", nil, map[string]any{
		"accounts": []map[string]any{{
			"accountId": resources.accountID,
			"priority":  0,
		}},
	}, nil, http.StatusOK)

	var keyResponse struct {
		Key struct {
			ID int64 `json:"id"`
		} `json:"key"`
		Secret string `json:"secret"`
	}
	mustJSON(t, client, http.MethodPost, env.baseURL, "/api/admin/keys", "client_key_create", nil, map[string]any{
		"name":          "e2e-client-" + suffix,
		"routingPoolId": resources.poolID,
	}, &keyResponse, http.StatusCreated)
	resources.clientKeyID = keyResponse.Key.ID
	resources.clientSecret = keyResponse.Secret
	if resources.clientKeyID <= 0 || resources.clientSecret == "" {
		t.Fatal("stage=client_key_create field=credentials")
	}

	gatewayHeaders := map[string]string{"Authorization": "Bearer " + resources.clientSecret}
	var modelsResponse struct {
		Data []struct {
			ID string `json:"id"`
		} `json:"data"`
	}
	mustJSON(t, client, http.MethodGet, env.baseURL, "/v1/models", "gateway_models", gatewayHeaders, nil, &modelsResponse, http.StatusOK)
	foundModel := false
	for _, model := range modelsResponse.Data {
		if model.ID == e2eModel {
			foundModel = true
			break
		}
	}
	if !foundModel {
		t.Fatal("stage=gateway_models field=model")
	}

	var chatResponse struct {
		Object string `json:"object"`
	}
	mustJSON(t, client, http.MethodPost, env.baseURL, "/v1/chat/completions", "gateway_chat", gatewayHeaders, map[string]any{
		"model":      e2eModel,
		"session_id": chatSessionID,
		"messages": []map[string]string{{
			"role":    "user",
			"content": "e2e request",
		}},
	}, &chatResponse, http.StatusOK)
	if chatResponse.Object != "chat.completion" {
		t.Fatal("stage=gateway_chat field=object")
	}

	responseHeaders := map[string]string{
		"Authorization":      "Bearer " + resources.clientSecret,
		"X-N2API-Session-ID": responsesSessionID,
	}
	mustSSE(t, client, env.baseURL, "/v1/responses", "gateway_responses_stream", responseHeaders, map[string]any{
		"model":  e2eModel,
		"input":  "e2e request",
		"stream": true,
	}, "response.completed")

	logs := waitForRequestLogs(t, database, resources.clientKeyID, []string{chatSessionID, responsesSessionID})
	expected := map[string]requestLog{
		"/v1/chat/completions": {
			route:                 "/v1/chat/completions",
			sessionID:             chatSessionID,
			inputTokens:           20,
			outputTokens:          5,
			totalTokens:           25,
			cachedInputTokens:     4,
			reasoningTokens:       2,
			estimatedCostMicrousd: 39,
			usageSource:           "chat_completions",
		},
		"/v1/responses": {
			route:                 "/v1/responses",
			sessionID:             responsesSessionID,
			inputTokens:           11,
			outputTokens:          2,
			totalTokens:           13,
			cachedInputTokens:     3,
			reasoningTokens:       1,
			estimatedCostMicrousd: 20,
			usageSource:           "stream",
		},
	}
	for _, actual := range logs {
		want, ok := expected[actual.route]
		if !ok {
			t.Fatalf("stage=request_logs field=route")
		}
		assertRequestLog(t, actual, want, resources)
		delete(expected, actual.route)
	}
	if len(expected) != 0 {
		t.Fatal("stage=request_logs field=missing_route")
	}
}

func assertRequestLog(t *testing.T, actual, want requestLog, resources *e2eResources) {
	t.Helper()
	checks := []struct {
		field string
		ok    bool
	}{
		{"method", actual.method == http.MethodPost},
		{"provider", actual.provider == "openai"},
		{"provider_account_id", actual.providerAccountID == resources.accountID},
		{"provider_account_type", actual.providerAccountType == "api_upstream"},
		{"provider_account_name", actual.providerAccountName == resources.accountName},
		{"client_key_id", actual.clientKeyID == resources.clientKeyID},
		{"routing_pool_id", actual.routingPoolID == resources.poolID},
		{"routing_pool_name", actual.routingPoolName == resources.poolName},
		{"routing_pool_fallback_depth", actual.fallbackDepth == 0},
		{"routing_pool_fallback_chain", actual.fallbackChain == resources.poolName},
		{"routing_pool_error", actual.routingPoolError == ""},
		{"session_id", actual.sessionID == want.sessionID},
		{"model", actual.model == e2eModel},
		{"input_tokens", actual.inputTokens == want.inputTokens},
		{"output_tokens", actual.outputTokens == want.outputTokens},
		{"total_tokens", actual.totalTokens == want.totalTokens},
		{"cached_input_tokens", actual.cachedInputTokens == want.cachedInputTokens},
		{"reasoning_tokens", actual.reasoningTokens == want.reasoningTokens},
		{"estimated_cost_microusd", actual.estimatedCostMicrousd == want.estimatedCostMicrousd},
		{"pricing_matched", actual.pricingMatched},
		{"pricing_model", actual.pricingModel == e2eModel},
		{"pricing_resolved_model", actual.pricingResolvedModel == e2eModel},
		{"pricing_currency", actual.pricingCurrency == "USD"},
		{"pricing_unit", actual.pricingUnit == "1M_tokens"},
		{"pricing_version", actual.pricingVersion == 1},
		{"pricing_input_rate", actual.pricingInputRate == 1_000_000},
		{"pricing_cached_rate", actual.pricingCachedRate == 2_000_000},
		{"pricing_output_rate", actual.pricingOutputRate == 3_000_000},
		{"pricing_updated_at", actual.pricingUpdatedAt == resources.pricingUpdatedAt.Format(time.RFC3339Nano)},
		{"usage_source", actual.usageSource == want.usageSource},
		{"gateway_attempt_count", actual.gatewayAttemptCount == 1},
		{"gateway_fallback_count", actual.gatewayFallbackCount == 0},
		{"status_code", actual.statusCode == http.StatusOK},
		{"error", actual.errorCode == ""},
	}
	for _, check := range checks {
		if !check.ok {
			t.Fatalf("stage=request_logs route=%s field=%s", actual.route, check.field)
		}
	}
}

package e2e_test

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

type protocolFixture struct {
	t         *testing.T
	env       e2eEnvironment
	client    *http.Client
	database  *pgxpool.Pool
	resources *e2eResources
	suffix    string
}

type gatewayResult struct {
	status  int
	header  http.Header
	body    []byte
	readErr bool
}

type mockDiagnosticSnapshot struct {
	Entries []struct {
		Scenario string `json:"scenario"`
		Count    int64  `json:"count"`
	} `json:"entries"`
}

func newProtocolFixture(t *testing.T) *protocolFixture {
	t.Helper()
	env := loadE2EEnvironment(t)
	client := newE2EHTTPClient(t)
	resources := &e2eResources{}
	registerCleanup(t, client, env, resources)
	mustJSON(t, client, http.MethodPost, env.baseURL, "/api/admin/login", "admin_login", nil, map[string]string{
		"username": env.adminUsername,
		"password": env.adminPassword,
	}, nil, http.StatusOK)
	return &protocolFixture{
		t:         t,
		env:       env,
		client:    client,
		database:  newE2EDatabase(t, env.databaseURL),
		resources: resources,
		suffix:    uniqueE2ESuffix(t),
	}
}

func (fixture *protocolFixture) createFingerprintProfile(name, scenario string) int64 {
	fixture.t.Helper()
	var response struct {
		Profile struct {
			ID int64 `json:"id"`
		} `json:"profile"`
	}
	mustJSON(fixture.t, fixture.client, http.MethodPost, fixture.env.baseURL, "/api/admin/fingerprint-profiles", "fingerprint_create", nil, map[string]any{
		"name":        name + " " + fixture.suffix,
		"description": "gateway protocol e2e fixture",
		"headers": map[string]string{
			"X-N2API-E2E-Scenario": scenario,
		},
		"enabled": true,
	}, &response, http.StatusCreated)
	if response.Profile.ID <= 0 {
		fixture.t.Fatal("stage=fingerprint_create field=id")
	}
	fixture.resources.fingerprintIDs = append(fixture.resources.fingerprintIDs, response.Profile.ID)
	return response.Profile.ID
}

func (fixture *protocolFixture) createAccount(name string, priority int, fingerprintProfileID int64) int64 {
	fixture.t.Helper()
	input := map[string]any{
		"name":       name + " " + fixture.suffix,
		"baseUrl":    fixture.env.mockBaseURL,
		"apiKey":     fixture.env.mockAPIKey,
		"enabled":    true,
		"priority":   priority,
		"loadFactor": 1,
		"models":     []string{e2eModel},
	}
	if fingerprintProfileID > 0 {
		input["fingerprintProfileId"] = fingerprintProfileID
	}
	var response struct {
		Account struct {
			ID int64 `json:"id"`
		} `json:"account"`
	}
	mustJSON(fixture.t, fixture.client, http.MethodPost, fixture.env.baseURL, "/api/admin/provider-accounts/api-upstream", "account_create", nil, input, &response, http.StatusCreated)
	if response.Account.ID <= 0 {
		fixture.t.Fatal("stage=account_create field=id")
	}
	fixture.resources.accountIDs = append(fixture.resources.accountIDs, response.Account.ID)
	return response.Account.ID
}

func (fixture *protocolFixture) createPoolAndKey(name string, accountIDs []int64) (int64, int64, string) {
	fixture.t.Helper()
	var poolResponse struct {
		Pool struct {
			ID int64 `json:"id"`
		} `json:"pool"`
	}
	mustJSON(fixture.t, fixture.client, http.MethodPost, fixture.env.baseURL, "/api/admin/routing-pools", "routing_pool_create", nil, map[string]any{
		"name":        name + "-" + fixture.suffix,
		"description": "gateway protocol e2e fixture",
		"enabled":     true,
	}, &poolResponse, http.StatusCreated)
	if poolResponse.Pool.ID <= 0 {
		fixture.t.Fatal("stage=routing_pool_create field=id")
	}
	fixture.resources.poolIDs = append(fixture.resources.poolIDs, poolResponse.Pool.ID)

	accounts := make([]map[string]any, 0, len(accountIDs))
	for priority, accountID := range accountIDs {
		accounts = append(accounts, map[string]any{
			"accountId": accountID,
			"priority":  priority,
		})
	}
	mustJSON(fixture.t, fixture.client, http.MethodPut, fixture.env.baseURL, "/api/admin/routing-pools/"+int64String(poolResponse.Pool.ID)+"/accounts", "routing_pool_membership", nil, map[string]any{
		"accounts": accounts,
	}, nil, http.StatusOK)

	var keyResponse struct {
		Key struct {
			ID int64 `json:"id"`
		} `json:"key"`
		Secret string `json:"secret"`
	}
	mustJSON(fixture.t, fixture.client, http.MethodPost, fixture.env.baseURL, "/api/admin/keys", "client_key_create", nil, map[string]any{
		"name":          name + "-key-" + fixture.suffix,
		"routingPoolId": poolResponse.Pool.ID,
	}, &keyResponse, http.StatusCreated)
	if keyResponse.Key.ID <= 0 || keyResponse.Secret == "" {
		fixture.t.Fatal("stage=client_key_create field=credentials")
	}
	fixture.resources.clientKeyIDs = append(fixture.resources.clientKeyIDs, keyResponse.Key.ID)
	return poolResponse.Pool.ID, keyResponse.Key.ID, keyResponse.Secret
}

func performGatewayPOST(ctx context.Context, client *http.Client, target, secret string, input any) (gatewayResult, string) {
	encoded, err := json.Marshal(input)
	if err != nil {
		return gatewayResult{}, "encode_request"
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, target, bytes.NewReader(encoded))
	if err != nil {
		return gatewayResult{}, "create_request"
	}
	req.Header.Set("Authorization", "Bearer "+secret)
	req.Header.Set("Content-Type", "application/json")
	resp, err := client.Do(req)
	if err != nil {
		return gatewayResult{}, "send_request"
	}
	defer resp.Body.Close()
	body, readErr := io.ReadAll(io.LimitReader(resp.Body, maxResponseBytes))
	return gatewayResult{
		status:  resp.StatusCode,
		header:  resp.Header.Clone(),
		body:    body,
		readErr: readErr != nil,
	}, ""
}

func mustGatewayPOST(t *testing.T, client *http.Client, target, secret string, input any) gatewayResult {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), requestTimeout)
	defer cancel()
	result, failure := performGatewayPOST(ctx, client, target, secret, input)
	if failure != "" {
		t.Fatalf("stage=gateway_request failure=%s", failure)
	}
	return result
}

func openAIErrorCode(body []byte) string {
	var payload struct {
		Error struct {
			Code string `json:"code"`
		} `json:"error"`
	}
	if json.Unmarshal(body, &payload) != nil {
		return ""
	}
	return payload.Error.Code
}

func responseObject(body []byte) string {
	var payload struct {
		Object string `json:"object"`
	}
	if json.Unmarshal(body, &payload) != nil {
		return ""
	}
	return payload.Object
}

func (fixture *protocolFixture) resetMock() {
	fixture.t.Helper()
	mustJSON(fixture.t, fixture.client, http.MethodPost, fixture.env.mockBaseURL, "/__mock/reset", "mock_reset", nil, nil, nil, http.StatusNoContent)
}

func (fixture *protocolFixture) mockScenarioCount(scenario string) int64 {
	fixture.t.Helper()
	var snapshot mockDiagnosticSnapshot
	status, failure := performJSON(context.Background(), fixture.client, http.MethodGet, fixture.env.mockBaseURL+"/__mock/state", nil, nil, &snapshot)
	if failure != "" || status != http.StatusOK {
		return -1
	}
	var count int64
	for _, entry := range snapshot.Entries {
		if entry.Scenario == scenario {
			count += entry.Count
		}
	}
	return count
}

func (fixture *protocolFixture) waitForMockScenario(scenario string, expected int64) {
	fixture.t.Helper()
	deadline := time.Now().Add(requestLogTimeout)
	for {
		count := fixture.mockScenarioCount(scenario)
		if count >= expected {
			return
		}
		if time.Now().After(deadline) {
			fixture.t.Fatalf("stage=mock_state scenario=%s field=count actual=%d expected=%d", scenario, count, expected)
		}
		time.Sleep(50 * time.Millisecond)
	}
}

func findRequestLog(logs []requestLog, sessionID string) (requestLog, bool) {
	for _, entry := range logs {
		if entry.sessionID == sessionID {
			return entry, true
		}
	}
	return requestLog{}, false
}

func hasSSEEvent(body []byte, event string) bool {
	return bytes.Contains(body, []byte("event: "+strings.TrimSpace(event)+"\n"))
}

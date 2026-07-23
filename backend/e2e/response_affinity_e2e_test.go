package e2e_test

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/KnowSky404/N2API/backend/internal/admin"
	"github.com/KnowSky404/N2API/backend/internal/gateway"
	"github.com/KnowSky404/N2API/backend/internal/provider"
	"github.com/KnowSky404/N2API/backend/internal/store"
	"github.com/jackc/pgx/v5/pgxpool"
)

const responseAffinityE2ESecret = "response-affinity-e2e-encryption-secret"

func TestResponseAffinityPersistsAcrossGatewayRebuild(t *testing.T) {
	fixture := newProtocolFixture(t)
	upstreamA := newResponseAffinityUpstream(t, "account-a-key", "resp-a-json-"+fixture.suffix, "resp-a-stream-"+fixture.suffix, "resp-a-child-"+fixture.suffix)
	upstreamB := newResponseAffinityUpstream(t, "account-b-key", "resp-b-json-"+fixture.suffix, "resp-b-stream-"+fixture.suffix, "resp-b-child-"+fixture.suffix)

	ctx, cancel := context.WithTimeout(context.Background(), requestTimeout)
	defer cancel()
	providerService := newResponseAffinityProviderService(fixture.database)
	accountA := createResponseAffinityAccount(t, ctx, providerService, "affinity-account-a-"+fixture.suffix, upstreamA.URL, "account-a-key", 0)
	accountB := createResponseAffinityAccount(t, ctx, providerService, "affinity-account-b-"+fixture.suffix, upstreamB.URL, "account-b-key", 1)
	fixture.resources.accountIDs = append(fixture.resources.accountIDs, accountA.ID, accountB.ID)

	adminService := admin.NewService(store.NewAdminRepository(fixture.database, responseAffinityE2ESecret), admin.Config{EncryptionSecret: responseAffinityE2ESecret})
	pool, err := adminService.CreateRoutingPool(ctx, "affinity-pool-"+fixture.suffix, "response affinity e2e", true, nil)
	if err != nil {
		t.Fatalf("stage=affinity_setup action=create_pool error=%v", err)
	}
	fixture.resources.poolIDs = append(fixture.resources.poolIDs, pool.ID)
	if _, err := adminService.ReplaceRoutingPoolAccounts(ctx, pool.ID, []admin.RoutingPoolAccount{
		{AccountID: accountA.ID, Priority: 0},
		{AccountID: accountB.ID, Priority: 1},
	}); err != nil {
		t.Fatalf("stage=affinity_setup action=assign_accounts error=%v", err)
	}
	createdKey, err := adminService.CreateAPIKey(ctx, "affinity-key-"+fixture.suffix, &pool.ID)
	if err != nil {
		t.Fatalf("stage=affinity_setup action=create_key error=%v", err)
	}
	fixture.resources.clientKeyIDs = append(fixture.resources.clientKeyIDs, createdKey.Key.ID)

	firstGateway := newResponseAffinityGateway(t, fixture.database)
	t.Cleanup(firstGateway.Close)
	jsonCreate := responseAffinityRequest(t, firstGateway.URL, http.MethodPost, "/v1/responses", createdKey.Secret, map[string]any{
		"model": e2eModel,
		"input": "create json response on account A",
	})
	if jsonCreate.status != http.StatusOK || responseID(jsonCreate.body) != upstreamA.jsonID {
		t.Fatalf("stage=affinity_json_create status=%d response_id=%q", jsonCreate.status, responseID(jsonCreate.body))
	}

	disabled := false
	if _, err := providerService.UpdateAccount(ctx, accountA.ID, provider.AccountUpdate{Enabled: &disabled}); err != nil {
		t.Fatalf("stage=affinity_setup action=disable_account_a error=%v", err)
	}
	sseCreate := responseAffinityRequest(t, firstGateway.URL, http.MethodPost, "/v1/responses", createdKey.Secret, map[string]any{
		"model":  e2eModel,
		"input":  "create streaming response on account B",
		"stream": true,
	})
	if sseCreate.status != http.StatusOK || !hasSSEEvent(sseCreate.body, "response.completed") || !strings.Contains(string(sseCreate.body), upstreamB.streamID) {
		t.Fatalf("stage=affinity_sse_create status=%d", sseCreate.status)
	}
	enabled := true
	if _, err := providerService.UpdateAccount(ctx, accountA.ID, provider.AccountUpdate{Enabled: &enabled}); err != nil {
		t.Fatalf("stage=affinity_setup action=enable_account_a error=%v", err)
	}

	assertPersistedResponseAffinities(t, fixture.database, pool.ID, accountA.ID, accountB.ID)
	firstGateway.Close()

	secondGateway := newResponseAffinityGateway(t, fixture.database)
	t.Cleanup(secondGateway.Close)

	getJSON := responseAffinityRequest(t, secondGateway.URL, http.MethodGet, "/v1/responses/"+upstreamA.jsonID, createdKey.Secret, nil)
	if getJSON.status != http.StatusOK || responseID(getJSON.body) != upstreamA.jsonID {
		t.Fatalf("stage=affinity_get status=%d response_id=%q", getJSON.status, responseID(getJSON.body))
	}
	getInputItems := responseAffinityRequest(t, secondGateway.URL, http.MethodGet, "/v1/responses/"+upstreamB.streamID+"/input_items", createdKey.Secret, nil)
	if getInputItems.status != http.StatusOK || responseObject(getInputItems.body) != "list" {
		t.Fatalf("stage=affinity_input_items status=%d object=%q", getInputItems.status, responseObject(getInputItems.body))
	}

	childCreate := responseAffinityRequest(t, secondGateway.URL, http.MethodPost, "/v1/responses", createdKey.Secret, map[string]any{
		"model":                e2eModel,
		"input":                "continue the account A response",
		"previous_response_id": upstreamA.jsonID,
	})
	if childCreate.status != http.StatusOK || responseID(childCreate.body) != upstreamA.childID {
		t.Fatalf("stage=affinity_previous_response status=%d response_id=%q", childCreate.status, responseID(childCreate.body))
	}
	childGet := responseAffinityRequest(t, secondGateway.URL, http.MethodGet, "/v1/responses/"+upstreamA.childID, createdKey.Secret, nil)
	if childGet.status != http.StatusOK || responseID(childGet.body) != upstreamA.childID {
		t.Fatalf("stage=affinity_child_get status=%d response_id=%q", childGet.status, responseID(childGet.body))
	}

	beforeUnknownA, beforeUnknownB := upstreamA.requestCount(), upstreamB.requestCount()
	unknown := responseAffinityRequest(t, secondGateway.URL, http.MethodGet, "/v1/responses/resp-unknown-"+fixture.suffix, createdKey.Secret, nil)
	if unknown.status != http.StatusConflict || openAIErrorCode(unknown.body) != "response_affinity_unknown" {
		t.Fatalf("stage=affinity_unknown status=%d error_code=%q", unknown.status, openAIErrorCode(unknown.body))
	}
	if upstreamA.requestCount() != beforeUnknownA || upstreamB.requestCount() != beforeUnknownB {
		t.Fatal("stage=affinity_unknown field=upstream_not_called")
	}

	if !upstreamA.saw(http.MethodGet, "/v1/responses/"+upstreamA.jsonID) ||
		!upstreamA.sawPreviousResponse(upstreamA.jsonID) ||
		!upstreamA.saw(http.MethodGet, "/v1/responses/"+upstreamA.childID) {
		t.Fatal("stage=affinity_account_a field=expected_routes")
	}
	if !upstreamB.saw(http.MethodGet, "/v1/responses/"+upstreamB.streamID+"/input_items") {
		t.Fatal("stage=affinity_account_b field=input_items_route")
	}
}

type responseAffinityGateway struct {
	URL       string
	server    *httptest.Server
	proxy     *gateway.Proxy
	closeOnce sync.Once
}

func newResponseAffinityGateway(t *testing.T, database *pgxpool.Pool) *responseAffinityGateway {
	t.Helper()
	admins := admin.NewService(store.NewAdminRepository(database, responseAffinityE2ESecret), admin.Config{EncryptionSecret: responseAffinityE2ESecret})
	providers := newResponseAffinityProviderService(database)
	proxy := gateway.NewProxy(admins, responseAffinityAccountProvider{service: providers}, gateway.Config{
		ResponseAffinityStore:         store.NewResponseAffinityRepository(database, responseAffinityE2ESecret),
		ResponseAffinityTTL:           time.Hour,
		UpstreamResponseHeaderTimeout: 3 * time.Second,
		UpstreamConnectTimeout:        3 * time.Second,
		UpstreamTLSHandshakeTimeout:   3 * time.Second,
		UpstreamSSEIdleTimeout:        3 * time.Second,
	})
	server := httptest.NewServer(proxy)
	return &responseAffinityGateway{URL: server.URL, server: server, proxy: proxy}
}

func (g *responseAffinityGateway) Close() {
	if g == nil {
		return
	}
	g.closeOnce.Do(func() {
		if g.server != nil {
			g.server.Close()
		}
		if g.proxy != nil {
			g.proxy.Close()
		}
	})
}

func newResponseAffinityProviderService(database *pgxpool.Pool) *provider.Service {
	return provider.NewService(store.NewProviderRepository(database), provider.NewHTTPClient(http.DefaultClient), provider.Config{
		Provider:              "openai",
		Secret:                responseAffinityE2ESecret,
		AllowHTTPAPIUpstreams: true,
	})
}

func createResponseAffinityAccount(t *testing.T, ctx context.Context, service *provider.Service, name, baseURL, apiKey string, priority int) provider.Account {
	t.Helper()
	enabled := true
	account, err := service.CreateAPIUpstreamAccount(ctx, provider.APIUpstreamInput{
		Name:       name,
		BaseURL:    baseURL,
		APIKey:     apiKey,
		Enabled:    &enabled,
		Priority:   priority,
		LoadFactor: 1,
		Models:     []string{e2eModel},
	})
	if err != nil {
		t.Fatalf("stage=affinity_setup action=create_account error=%v", err)
	}
	return account
}

type responseAffinityAccountProvider struct {
	service *provider.Service
}

func (p responseAffinityAccountProvider) SelectAccountForModel(ctx context.Context, model string, excludedAccountIDs ...int64) (gateway.SelectedAccount, error) {
	selected, err := p.service.SelectAccountForModel(ctx, model, excludedAccountIDs...)
	return responseAffinitySelectedAccount(selected, err)
}

func (p responseAffinityAccountProvider) SelectAccountForModelInRoutingPoolChain(ctx context.Context, routingPoolID int64, model string, excludedAccountIDs ...int64) (gateway.SelectedAccount, error) {
	selected, err := p.service.SelectAccountForModelInRoutingPoolChain(ctx, routingPoolID, model, excludedAccountIDs...)
	return responseAffinitySelectedAccount(selected, err)
}

func (p responseAffinityAccountProvider) SelectAccountForModelAndSessionInRoutingPoolChain(ctx context.Context, routingPoolID int64, model, sessionID string, excludedAccountIDs ...int64) (gateway.SelectedAccount, error) {
	selected, err := p.service.SelectAccountForModelAndSessionInRoutingPoolChain(ctx, routingPoolID, model, sessionID, excludedAccountIDs...)
	return responseAffinitySelectedAccount(selected, err)
}

func (p responseAffinityAccountProvider) SelectAccountByIDInRoutingPoolChain(ctx context.Context, routingPoolID, accountID int64, model string) (gateway.SelectedAccount, error) {
	selected, err := p.service.SelectAccountByIDInRoutingPoolChain(ctx, routingPoolID, accountID, model)
	return responseAffinitySelectedAccount(selected, err)
}

func (p responseAffinityAccountProvider) SelectSingleAccountInRoutingPoolChain(ctx context.Context, routingPoolID int64, model string) (gateway.SelectedAccount, bool, error) {
	selected, unique, err := p.service.SelectSingleAccountInRoutingPoolChain(ctx, routingPoolID, model)
	mapped, mappedErr := responseAffinitySelectedAccount(selected, err)
	return mapped, unique, mappedErr
}

func responseAffinitySelectedAccount(selected provider.SelectedAccount, err error) (gateway.SelectedAccount, error) {
	mapped := gateway.SelectedAccount{
		AccountID:                selected.AccountID,
		Provider:                 selected.Provider,
		AccountType:              selected.AccountType,
		DisplayName:              selected.DisplayName,
		AuthorizationToken:       selected.AuthorizationToken,
		BaseURL:                  selected.BaseURL,
		ProxyURL:                 selected.ProxyURL,
		MaxConcurrentRequests:    selected.MaxConcurrentRequests,
		RoutingPoolID:            selected.RoutingPoolID,
		RoutingPoolName:          selected.RoutingPoolName,
		RoutingPoolFallbackDepth: selected.RoutingPoolFallbackDepth,
		RoutingPoolFallbackChain: selected.RoutingPoolFallbackChain,
		RoutingPoolError:         selected.RoutingPoolError,
		FingerprintUA:            selected.FingerprintUA,
		FingerprintTLS:           selected.FingerprintTLS,
		FingerprintHeaders:       selected.FingerprintHeaders,
	}
	return mapped, err
}

func responseAffinityRequest(t *testing.T, baseURL, method, path, secret string, input any) gatewayResult {
	t.Helper()
	var body io.Reader
	if input != nil {
		encoded, err := json.Marshal(input)
		if err != nil {
			t.Fatalf("stage=affinity_request action=encode error=%v", err)
		}
		body = strings.NewReader(string(encoded))
	}
	ctx, cancel := context.WithTimeout(context.Background(), requestTimeout)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, method, baseURL+path, body)
	if err != nil {
		t.Fatalf("stage=affinity_request action=create error=%v", err)
	}
	req.Header.Set("Authorization", "Bearer "+secret)
	if input != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("stage=affinity_request action=send error=%v", err)
	}
	defer resp.Body.Close()
	responseBody, readErr := io.ReadAll(io.LimitReader(resp.Body, maxResponseBytes))
	return gatewayResult{status: resp.StatusCode, header: resp.Header.Clone(), body: responseBody, readErr: readErr != nil}
}

func responseID(body []byte) string {
	var payload struct {
		ID string `json:"id"`
	}
	if json.Unmarshal(body, &payload) != nil {
		return ""
	}
	return payload.ID
}

func assertPersistedResponseAffinities(t *testing.T, database *pgxpool.Pool, poolID, accountAID, accountBID int64) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), requestTimeout)
	defer cancel()
	rows, err := database.Query(ctx, `
		SELECT provider_account_id, octet_length(response_id_hash)
		FROM response_affinities
		WHERE routing_pool_id = $1
		ORDER BY provider_account_id
	`, poolID)
	if err != nil {
		t.Fatalf("stage=affinity_persistence action=query error=%v", err)
	}
	defer rows.Close()
	found := map[int64]int{}
	rowCount := 0
	for rows.Next() {
		var accountID int64
		var hashLength int
		if err := rows.Scan(&accountID, &hashLength); err != nil {
			t.Fatalf("stage=affinity_persistence action=scan error=%v", err)
		}
		rowCount++
		found[accountID] = hashLength
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("stage=affinity_persistence action=iterate error=%v", err)
	}
	if rowCount != 2 || len(found) != 2 || found[accountAID] != 32 || found[accountBID] != 32 {
		t.Fatalf("stage=affinity_persistence row_count=%d bindings=%v", rowCount, found)
	}
}

type responseAffinityUpstream struct {
	*httptest.Server
	apiKey   string
	jsonID   string
	streamID string
	childID  string

	mu                  sync.Mutex
	requests            []string
	previousResponseIDs []string
}

func newResponseAffinityUpstream(t *testing.T, apiKey, jsonID, streamID, childID string) *responseAffinityUpstream {
	t.Helper()
	upstream := &responseAffinityUpstream{apiKey: apiKey, jsonID: jsonID, streamID: streamID, childID: childID}
	upstream.Server = httptest.NewServer(http.HandlerFunc(upstream.serveHTTP))
	t.Cleanup(upstream.Close)
	return upstream
}

func (u *responseAffinityUpstream) serveHTTP(w http.ResponseWriter, r *http.Request) {
	if r.Header.Get("Authorization") != "Bearer "+u.apiKey {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}
	if !strings.HasPrefix(r.URL.Path, "/v1/responses") {
		http.NotFound(w, r)
		return
	}

	previousResponseID := ""
	stream := false
	if r.Method == http.MethodPost {
		var payload struct {
			PreviousResponseID string `json:"previous_response_id"`
			Stream             bool   `json:"stream"`
		}
		if err := json.NewDecoder(io.LimitReader(r.Body, maxResponseBytes)).Decode(&payload); err != nil {
			http.Error(w, "invalid json", http.StatusBadRequest)
			return
		}
		previousResponseID = payload.PreviousResponseID
		stream = payload.Stream
	}

	u.mu.Lock()
	u.requests = append(u.requests, r.Method+" "+r.URL.Path)
	if previousResponseID != "" {
		u.previousResponseIDs = append(u.previousResponseIDs, previousResponseID)
	}
	u.mu.Unlock()

	if r.Method == http.MethodGet && strings.HasSuffix(r.URL.Path, "/input_items") {
		writeResponseAffinityJSON(w, map[string]any{"object": "list", "data": []any{}})
		return
	}
	if r.Method == http.MethodGet {
		responseID := strings.TrimPrefix(r.URL.Path, "/v1/responses/")
		writeResponseAffinityJSON(w, map[string]any{"id": responseID, "object": "response"})
		return
	}
	if r.Method != http.MethodPost || r.URL.Path != "/v1/responses" {
		http.NotFound(w, r)
		return
	}

	responseID := u.jsonID
	if previousResponseID != "" {
		responseID = u.childID
	} else if stream {
		responseID = u.streamID
	}
	if stream {
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)
		_, _ = fmt.Fprintf(w, "event: response.created\ndata: {\"type\":\"response.created\",\"response\":{\"id\":%q}}\n\n", responseID)
		_, _ = fmt.Fprintf(w, "event: response.completed\ndata: {\"type\":\"response.completed\",\"response\":{\"id\":%q}}\n\n", responseID)
		return
	}
	writeResponseAffinityJSON(w, map[string]any{"id": responseID, "object": "response"})
}

func writeResponseAffinityJSON(w http.ResponseWriter, payload any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_ = json.NewEncoder(w).Encode(payload)
}

func (u *responseAffinityUpstream) requestCount() int {
	u.mu.Lock()
	defer u.mu.Unlock()
	return len(u.requests)
}

func (u *responseAffinityUpstream) saw(method, path string) bool {
	u.mu.Lock()
	defer u.mu.Unlock()
	want := method + " " + path
	for _, request := range u.requests {
		if request == want {
			return true
		}
	}
	return false
}

func (u *responseAffinityUpstream) sawPreviousResponse(responseID string) bool {
	u.mu.Lock()
	defer u.mu.Unlock()
	for _, previousResponseID := range u.previousResponseIDs {
		if previousResponseID == responseID {
			return true
		}
	}
	return false
}

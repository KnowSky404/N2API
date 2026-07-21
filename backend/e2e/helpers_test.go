package e2e_test

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"io"
	"net/http"
	"net/http/cookiejar"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

const (
	e2eModel          = "gpt-5"
	maxResponseBytes  = 2 << 20
	requestTimeout    = 15 * time.Second
	requestLogTimeout = 10 * time.Second
)

type e2eEnvironment struct {
	baseURL       string
	databaseURL   string
	adminUsername string
	adminPassword string
	mockBaseURL   string
	mockAPIKey    string
}

type e2eResources struct {
	accountID        int64
	accountName      string
	poolID           int64
	poolName         string
	clientKeyID      int64
	clientSecret     string
	fingerprintIDs   []int64
	accountIDs       []int64
	poolIDs          []int64
	clientKeyIDs     []int64
	pricingBefore    usagePricing
	pricingUpdatedAt time.Time
	pricingUpdated   bool
}

type usagePricing struct {
	Version       int                   `json:"version"`
	Currency      string                `json:"currency"`
	Unit          string                `json:"unit"`
	UpdatedAt     time.Time             `json:"updatedAt"`
	Models        map[string]usagePrice `json:"models"`
	IgnoredModels []string              `json:"ignoredModels,omitempty"`
}

type usagePrice struct {
	InputMicrousdPerMillion           int64 `json:"inputMicrousdPerMillion"`
	CachedInputMicrousdPerMillion     int64 `json:"cachedInputMicrousdPerMillion"`
	OutputMicrousdPerMillion          int64 `json:"outputMicrousdPerMillion"`
	LongInputMicrousdPerMillion       int64 `json:"longInputMicrousdPerMillion"`
	LongCachedInputMicrousdPerMillion int64 `json:"longCachedInputMicrousdPerMillion"`
	LongOutputMicrousdPerMillion      int64 `json:"longOutputMicrousdPerMillion"`
}

type requestLog struct {
	route                 string
	method                string
	provider              string
	providerAccountID     int64
	providerAccountType   string
	providerAccountName   string
	clientKeyID           int64
	routingPoolID         int64
	routingPoolName       string
	fallbackDepth         int
	fallbackChain         string
	routingPoolError      string
	sessionID             string
	model                 string
	inputTokens           int
	outputTokens          int
	totalTokens           int
	cachedInputTokens     int
	reasoningTokens       int
	estimatedCostMicrousd int64
	pricingMatched        bool
	pricingModel          string
	pricingResolvedModel  string
	pricingCurrency       string
	pricingUnit           string
	pricingVersion        int
	pricingInputRate      int64
	pricingCachedRate     int64
	pricingOutputRate     int64
	pricingUpdatedAt      string
	usageSource           string
	gatewayAttemptCount   int
	gatewayFallbackCount  int
	statusCode            int
	errorCode             string
}

func loadE2EEnvironment(t *testing.T) e2eEnvironment {
	t.Helper()
	baseURL := strings.TrimRight(strings.TrimSpace(os.Getenv("N2API_E2E_BASE_URL")), "/")
	if baseURL == "" {
		t.Skip("N2API gateway E2E environment is not configured")
	}

	env := e2eEnvironment{
		baseURL:       baseURL,
		databaseURL:   strings.TrimSpace(os.Getenv("N2API_E2E_DATABASE_URL")),
		adminUsername: strings.TrimSpace(os.Getenv("N2API_E2E_ADMIN_USERNAME")),
		adminPassword: os.Getenv("N2API_E2E_ADMIN_PASSWORD"),
		mockBaseURL:   strings.TrimRight(strings.TrimSpace(os.Getenv("N2API_E2E_MOCK_BASE_URL")), "/"),
		mockAPIKey:    os.Getenv("N2API_E2E_MOCK_API_KEY"),
	}
	for name, value := range map[string]string{
		"N2API_E2E_DATABASE_URL":   env.databaseURL,
		"N2API_E2E_ADMIN_USERNAME": env.adminUsername,
		"N2API_E2E_ADMIN_PASSWORD": env.adminPassword,
		"N2API_E2E_MOCK_BASE_URL":  env.mockBaseURL,
		"N2API_E2E_MOCK_API_KEY":   env.mockAPIKey,
	} {
		if value == "" {
			t.Fatalf("stage=config missing=%s", name)
		}
	}
	return env
}

func newE2EHTTPClient(t *testing.T) *http.Client {
	t.Helper()
	jar, err := cookiejar.New(nil)
	if err != nil {
		t.Fatal("stage=http_client action=create_cookie_jar")
	}
	return &http.Client{Jar: jar, Timeout: requestTimeout}
}

func newE2EDatabase(t *testing.T, databaseURL string) *pgxpool.Pool {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), requestTimeout)
	defer cancel()
	pool, err := pgxpool.New(ctx, databaseURL)
	if err != nil {
		t.Fatal("stage=database action=create_pool")
	}
	if err := pool.Ping(ctx); err != nil {
		pool.Close()
		t.Fatal("stage=database action=ping")
	}
	t.Cleanup(pool.Close)
	return pool
}

func mustJSON(t *testing.T, client *http.Client, method, baseURL, path, stage string, headers map[string]string, input, output any, expectedStatus int) {
	t.Helper()
	status, failure := performJSON(context.Background(), client, method, baseURL+path, headers, input, output)
	if failure != "" {
		t.Fatalf("stage=%s status=%d failure=%s", stage, status, failure)
	}
	if status != expectedStatus {
		t.Fatalf("stage=%s status=%d expected=%d", stage, status, expectedStatus)
	}
}

func performJSON(ctx context.Context, client *http.Client, method, target string, headers map[string]string, input, output any) (int, string) {
	var body io.Reader
	if input != nil {
		encoded, err := json.Marshal(input)
		if err != nil {
			return 0, "encode_request"
		}
		body = bytes.NewReader(encoded)
	}
	requestCtx, cancel := context.WithTimeout(ctx, requestTimeout)
	defer cancel()
	req, err := http.NewRequestWithContext(requestCtx, method, target, body)
	if err != nil {
		return 0, "create_request"
	}
	if input != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	for name, value := range headers {
		req.Header.Set(name, value)
	}
	resp, err := client.Do(req)
	if err != nil {
		return 0, "send_request"
	}
	defer resp.Body.Close()
	if output != nil && resp.StatusCode >= 200 && resp.StatusCode < 300 {
		if err := json.NewDecoder(io.LimitReader(resp.Body, maxResponseBytes)).Decode(output); err != nil {
			return resp.StatusCode, "decode_response"
		}
	} else {
		_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, maxResponseBytes))
	}
	return resp.StatusCode, ""
}

func mustSSE(t *testing.T, client *http.Client, baseURL, path, stage string, headers map[string]string, input any, expectedEvent string) {
	t.Helper()
	encoded, err := json.Marshal(input)
	if err != nil {
		t.Fatalf("stage=%s status=0 failure=encode_request", stage)
	}
	ctx, cancel := context.WithTimeout(context.Background(), requestTimeout)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, baseURL+path, bytes.NewReader(encoded))
	if err != nil {
		t.Fatalf("stage=%s status=0 failure=create_request", stage)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "text/event-stream")
	for name, value := range headers {
		req.Header.Set(name, value)
	}
	resp, err := client.Do(req)
	if err != nil {
		t.Fatalf("stage=%s status=0 failure=send_request", stage)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, maxResponseBytes))
		t.Fatalf("stage=%s status=%d expected=%d", stage, resp.StatusCode, http.StatusOK)
	}
	if !strings.HasPrefix(strings.ToLower(resp.Header.Get("Content-Type")), "text/event-stream") {
		t.Fatalf("stage=%s status=%d field=content_type", stage, resp.StatusCode)
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, maxResponseBytes))
	if err != nil {
		t.Fatalf("stage=%s status=%d failure=read_stream", stage, resp.StatusCode)
	}
	if !bytes.Contains(body, []byte("event: "+expectedEvent+"\n")) {
		t.Fatalf("stage=%s status=%d field=expected_event", stage, resp.StatusCode)
	}
}

func clonePricing(source usagePricing) usagePricing {
	cloned := source
	cloned.Models = make(map[string]usagePrice, len(source.Models)+1)
	for model, price := range source.Models {
		cloned.Models[model] = price
	}
	cloned.IgnoredModels = append([]string(nil), source.IgnoredModels...)
	return cloned
}

func uniqueE2ESuffix(t *testing.T) string {
	t.Helper()
	var value [8]byte
	if _, err := rand.Read(value[:]); err != nil {
		t.Fatal("stage=identifier action=generate")
	}
	return hex.EncodeToString(value[:])
}

func registerCleanup(t *testing.T, client *http.Client, env e2eEnvironment, resources *e2eResources) {
	t.Helper()
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), requestTimeout)
		defer cancel()
		for _, id := range uniquePositiveIDs(resources.clientKeyID, resources.clientKeyIDs) {
			_, _ = performJSON(ctx, client, http.MethodPost, env.baseURL+"/api/admin/keys/"+int64String(id)+"/revoke", nil, nil, nil)
			_, _ = performJSON(ctx, client, http.MethodDelete, env.baseURL+"/api/admin/keys/"+int64String(id), nil, nil, nil)
		}
		for _, id := range uniquePositiveIDs(resources.poolID, resources.poolIDs) {
			_, _ = performJSON(ctx, client, http.MethodDelete, env.baseURL+"/api/admin/routing-pools/"+int64String(id), nil, nil, nil)
		}
		for _, id := range uniquePositiveIDs(resources.accountID, resources.accountIDs) {
			_, _ = performJSON(ctx, client, http.MethodDelete, env.baseURL+"/api/admin/provider-accounts/"+int64String(id), nil, nil, nil)
		}
		for _, id := range uniquePositiveIDs(0, resources.fingerprintIDs) {
			_, _ = performJSON(ctx, client, http.MethodDelete, env.baseURL+"/api/admin/fingerprint-profiles/"+int64String(id), nil, nil, nil)
		}
		if resources.pricingUpdated {
			_, _ = performJSON(ctx, client, http.MethodPut, env.baseURL+"/api/admin/usage-pricing", nil, resources.pricingBefore, nil)
		}
		_, _ = performJSON(ctx, client, http.MethodPost, env.baseURL+"/api/admin/logout", nil, nil, nil)
		resources.clientSecret = ""
	})
}

func uniquePositiveIDs(single int64, values []int64) []int64 {
	seen := make(map[int64]struct{}, len(values)+1)
	result := make([]int64, 0, len(values)+1)
	appendID := func(id int64) {
		if id <= 0 {
			return
		}
		if _, ok := seen[id]; ok {
			return
		}
		seen[id] = struct{}{}
		result = append(result, id)
	}
	appendID(single)
	for _, id := range values {
		appendID(id)
	}
	return result
}

func int64String(value int64) string {
	const digits = "0123456789"
	if value == 0 {
		return "0"
	}
	var buffer [20]byte
	index := len(buffer)
	for value > 0 {
		index--
		buffer[index] = digits[value%10]
		value /= 10
	}
	return string(buffer[index:])
}

func waitForRequestLogs(t *testing.T, pool *pgxpool.Pool, clientKeyID int64, sessionIDs []string) []requestLog {
	t.Helper()
	return waitForRequestLogCount(t, pool, clientKeyID, sessionIDs, len(sessionIDs))
}

func waitForRequestLogCount(t *testing.T, pool *pgxpool.Pool, clientKeyID int64, sessionIDs []string, expected int) []requestLog {
	t.Helper()
	deadline := time.Now().Add(requestLogTimeout)
	for {
		logs, ok := queryRequestLogs(pool, clientKeyID, sessionIDs)
		if ok && len(logs) == expected {
			return logs
		}
		if time.Now().After(deadline) {
			t.Fatalf("stage=request_logs field=row_count actual=%d expected=%d", len(logs), expected)
		}
		time.Sleep(100 * time.Millisecond)
	}
}

func queryRequestLogs(pool *pgxpool.Pool, clientKeyID int64, sessionIDs []string) ([]requestLog, bool) {
	ctx, cancel := context.WithTimeout(context.Background(), requestTimeout)
	defer cancel()
	if sessionIDs == nil {
		sessionIDs = []string{}
	}
	rows, err := pool.Query(ctx, `
		SELECT route,
			method,
			provider,
			COALESCE(provider_account_id, 0),
			COALESCE(provider_account_type, ''),
			COALESCE(provider_account_name, ''),
			COALESCE(client_key_id, 0),
			COALESCE(routing_pool_id, 0),
			COALESCE(routing_pool_name, ''),
			COALESCE(routing_pool_fallback_depth, 0),
			COALESCE(routing_pool_fallback_chain, ''),
			COALESCE(routing_pool_error, ''),
			session_id,
			model,
			input_tokens,
			output_tokens,
			total_tokens,
			cached_input_tokens,
			reasoning_tokens,
			estimated_cost_microusd,
			COALESCE((pricing_snapshot->>'matched')::boolean, false),
			COALESCE(pricing_snapshot->>'model', ''),
			COALESCE(pricing_snapshot->>'pricingModel', ''),
			COALESCE(pricing_snapshot->>'currency', ''),
			COALESCE(pricing_snapshot->>'unit', ''),
			COALESCE((pricing_snapshot->>'version')::integer, 0),
			COALESCE((pricing_snapshot->>'inputMicrousdPerMillion')::bigint, 0),
			COALESCE((pricing_snapshot->>'cachedInputMicrousdPerMillion')::bigint, 0),
			COALESCE((pricing_snapshot->>'outputMicrousdPerMillion')::bigint, 0),
			COALESCE(pricing_snapshot->>'updatedAt', ''),
			usage_source,
			gateway_attempt_count,
			gateway_fallback_count,
			status_code,
			error
		FROM request_logs
		WHERE client_key_id = $1
			AND (cardinality($2::text[]) = 0 OR session_id = ANY($2::text[]))
		ORDER BY id
	`, clientKeyID, sessionIDs)
	if err != nil {
		return nil, false
	}
	defer rows.Close()
	logs := make([]requestLog, 0, 2)
	for rows.Next() {
		var entry requestLog
		if err := rows.Scan(
			&entry.route,
			&entry.method,
			&entry.provider,
			&entry.providerAccountID,
			&entry.providerAccountType,
			&entry.providerAccountName,
			&entry.clientKeyID,
			&entry.routingPoolID,
			&entry.routingPoolName,
			&entry.fallbackDepth,
			&entry.fallbackChain,
			&entry.routingPoolError,
			&entry.sessionID,
			&entry.model,
			&entry.inputTokens,
			&entry.outputTokens,
			&entry.totalTokens,
			&entry.cachedInputTokens,
			&entry.reasoningTokens,
			&entry.estimatedCostMicrousd,
			&entry.pricingMatched,
			&entry.pricingModel,
			&entry.pricingResolvedModel,
			&entry.pricingCurrency,
			&entry.pricingUnit,
			&entry.pricingVersion,
			&entry.pricingInputRate,
			&entry.pricingCachedRate,
			&entry.pricingOutputRate,
			&entry.pricingUpdatedAt,
			&entry.usageSource,
			&entry.gatewayAttemptCount,
			&entry.gatewayFallbackCount,
			&entry.statusCode,
			&entry.errorCode,
		); err != nil {
			return nil, false
		}
		logs = append(logs, entry)
	}
	if rows.Err() != nil {
		return nil, false
	}
	return logs, true
}

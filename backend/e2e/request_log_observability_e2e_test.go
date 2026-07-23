package e2e_test

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/KnowSky404/N2API/backend/internal/requestlog"
	"github.com/KnowSky404/N2API/backend/internal/systemevent"
	"github.com/jackc/pgx/v5/pgxpool"
)

type requestLogObservation struct {
	sessionID string
	requestID string
}

type requestLogCorrelationRow struct {
	requestID  string
	sessionID  string
	statusCode int
}

type requestLogWriteHealth struct {
	LastSucceededAt     *time.Time `json:"lastSucceededAt"`
	LastFailedAt        *time.Time `json:"lastFailedAt"`
	LastErrorCode       string     `json:"lastErrorCode"`
	ConsecutiveFailures uint64     `json:"consecutiveFailures"`
	TotalFailures       uint64     `json:"totalFailures"`
}

func TestGatewayRequestLogObservabilityAndWriteFailure(t *testing.T) {
	fixture := newProtocolFixture(t)
	upstreamURL, observations := startRequestLogObservationUpstream(t, fixture.env.baseURL)
	accountID := createRequestLogObservationAccount(t, fixture, upstreamURL)
	_, clientKeyID, secret := fixture.createPoolAndKey("request-log-observability", []int64{accountID})

	baselineHealth, _ := readRequestLogWriteHealth(t, fixture)
	spoofedRequestID := "spoofed request id"
	sessions := []string{
		"e2e-request-log-correlation-a-" + fixture.suffix,
		"e2e-request-log-correlation-b-" + fixture.suffix,
	}
	responseIDs := make([]string, 0, len(sessions))
	var successfulResponse gatewayResult

	for index, sessionID := range sessions {
		result := mustGatewayPOSTWithRequestID(t, fixture.client, fixture.env.baseURL+"/v1/chat/completions", secret, spoofedRequestID, requestLogChatInput(sessionID))
		assertSuccessfulRequestLogResponse(t, result)
		if index == 0 {
			successfulResponse = result
		} else if !bytes.Equal(result.body, successfulResponse.body) {
			t.Fatal("stage=correlation field=response_body_consistency")
		}

		responseID := result.header.Get("X-Request-ID")
		if responseID == spoofedRequestID || !systemevent.ValidCorrelationID(responseID) {
			t.Fatalf("stage=correlation session=%s field=response_request_id value=%q", sessionID, responseID)
		}
		if values := result.header.Values("X-Request-ID"); len(values) != 1 {
			t.Fatalf("stage=correlation session=%s field=response_request_id_count actual=%d expected=1", sessionID, len(values))
		}

		observation := waitForRequestLogObservation(t, observations, sessionID)
		if observation.requestID != responseID {
			t.Fatalf("stage=correlation session=%s field=upstream_request_id actual=%q expected=%q", sessionID, observation.requestID, responseID)
		}
		row := waitForRequestLogCorrelationRow(t, fixture.database, clientKeyID, sessionID)
		if row.requestID != responseID || row.sessionID != sessionID || row.statusCode != http.StatusOK {
			t.Fatalf("stage=correlation session=%s row=%+v response_request_id=%q", sessionID, row, responseID)
		}
		responseIDs = append(responseIDs, responseID)
	}
	if responseIDs[0] == responseIDs[1] {
		t.Fatalf("stage=correlation field=request_id_isolation value=%q", responseIDs[0])
	}

	faultSessionID := "e2e-request-log-fault-" + fixture.suffix
	canary := "request-log-db-canary-" + fixture.suffix
	removeFailureTrigger := installRequestLogFailureTrigger(t, fixture.database, fixture.suffix, faultSessionID, canary)

	faultResult := mustGatewayPOSTWithRequestID(t, fixture.client, fixture.env.baseURL+"/v1/chat/completions", secret, spoofedRequestID, requestLogChatInput(faultSessionID))
	assertSuccessfulRequestLogResponse(t, faultResult)
	if faultResult.status != successfulResponse.status ||
		faultResult.header.Get("Content-Type") != successfulResponse.header.Get("Content-Type") ||
		!bytes.Equal(faultResult.body, successfulResponse.body) {
		t.Fatal("stage=request_log_write_failure field=client_response_changed")
	}
	if bytes.Contains(faultResult.body, []byte(canary)) || strings.Contains(faultResult.header.Get("X-Request-ID"), canary) {
		t.Fatal("stage=request_log_write_failure field=database_error_leaked_to_client")
	}
	faultObservation := waitForRequestLogObservation(t, observations, faultSessionID)
	if faultObservation.requestID != faultResult.header.Get("X-Request-ID") {
		t.Fatalf("stage=request_log_write_failure field=upstream_request_id actual=%q expected=%q", faultObservation.requestID, faultResult.header.Get("X-Request-ID"))
	}

	failedHealth, rawHealth := waitForRequestLogWriteFailure(t, fixture, baselineHealth.TotalFailures)
	if failedHealth.LastFailedAt == nil || failedHealth.LastErrorCode != requestlog.WriteFailedErrorCode ||
		failedHealth.ConsecutiveFailures < 1 || failedHealth.TotalFailures <= baselineHealth.TotalFailures {
		t.Fatalf("stage=request_log_write_failure field=health_status status=%+v baseline_total=%d", failedHealth, baselineHealth.TotalFailures)
	}
	for _, secretValue := range []string{canary, "reject_request_log_" + fixture.suffix} {
		if bytes.Contains(rawHealth, []byte(secretValue)) {
			t.Fatalf("stage=request_log_write_failure field=database_error_leaked_to_health value=%q", secretValue)
		}
	}
	if count := requestLogCount(t, fixture.database, clientKeyID, faultSessionID); count != 0 {
		t.Fatalf("stage=request_log_write_failure field=persisted_row_count actual=%d expected=0", count)
	}

	removeFailureTrigger()
	recoverySessionID := "e2e-request-log-recovery-" + fixture.suffix
	recoveryResult := mustGatewayPOSTWithRequestID(t, fixture.client, fixture.env.baseURL+"/v1/chat/completions", secret, spoofedRequestID, requestLogChatInput(recoverySessionID))
	assertSuccessfulRequestLogResponse(t, recoveryResult)
	recoveryObservation := waitForRequestLogObservation(t, observations, recoverySessionID)
	if recoveryObservation.requestID != recoveryResult.header.Get("X-Request-ID") {
		t.Fatalf("stage=request_log_write_recovery field=upstream_request_id actual=%q expected=%q", recoveryObservation.requestID, recoveryResult.header.Get("X-Request-ID"))
	}
	recoveryRow := waitForRequestLogCorrelationRow(t, fixture.database, clientKeyID, recoverySessionID)
	if recoveryRow.requestID != recoveryResult.header.Get("X-Request-ID") || recoveryRow.statusCode != http.StatusOK {
		t.Fatalf("stage=request_log_write_recovery row=%+v", recoveryRow)
	}
	recoveredHealth := waitForRequestLogWriteRecovery(t, fixture, failedHealth)
	if recoveredHealth.LastSucceededAt == nil || recoveredHealth.LastFailedAt == nil ||
		recoveredHealth.LastSucceededAt.Before(*recoveredHealth.LastFailedAt) ||
		recoveredHealth.ConsecutiveFailures != 0 || recoveredHealth.TotalFailures != failedHealth.TotalFailures {
		t.Fatalf("stage=request_log_write_recovery field=health_status status=%+v failed=%+v", recoveredHealth, failedHealth)
	}
}

func requestLogChatInput(sessionID string) map[string]any {
	return map[string]any{
		"model":      e2eModel,
		"session_id": sessionID,
		"messages": []map[string]string{{
			"role":    "user",
			"content": "request log observability contract",
		}},
	}
}

func startRequestLogObservationUpstream(t *testing.T, gatewayBaseURL string) (string, <-chan requestLogObservation) {
	t.Helper()
	listener, err := net.Listen("tcp4", "0.0.0.0:0")
	if err != nil {
		t.Fatalf("stage=observation_upstream action=listen error=%v", err)
	}
	observations := make(chan requestLogObservation, 8)
	server := &http.Server{
		ReadHeaderTimeout: 2 * time.Second,
		Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			var payload struct {
				SessionID string `json:"session_id"`
			}
			if err := json.NewDecoder(io.LimitReader(r.Body, maxResponseBytes)).Decode(&payload); err != nil {
				http.Error(w, "invalid request", http.StatusBadRequest)
				return
			}
			observations <- requestLogObservation{
				sessionID: strings.TrimSpace(payload.SessionID),
				requestID: r.Header.Get("X-Request-ID"),
			}
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(map[string]any{
				"id":      "chatcmpl-request-log-observability",
				"object":  "chat.completion",
				"created": 0,
				"model":   e2eModel,
				"choices": []map[string]any{{
					"index": 0,
					"message": map[string]string{
						"role":    "assistant",
						"content": "ok",
					},
					"finish_reason": "stop",
				}},
				"usage": map[string]int{
					"prompt_tokens":     1,
					"completion_tokens": 1,
					"total_tokens":      2,
				},
			})
		}),
	}
	go func() {
		_ = server.Serve(listener)
	}()
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		_ = server.Shutdown(ctx)
	})

	callbackIP := callbackIPv4ForGateway(t, gatewayBaseURL)
	port := listener.Addr().(*net.TCPAddr).Port
	return "http://" + net.JoinHostPort(callbackIP.String(), fmt.Sprintf("%d", port)), observations
}

func callbackIPv4ForGateway(t *testing.T, gatewayBaseURL string) net.IP {
	t.Helper()
	parsed, err := url.Parse(gatewayBaseURL)
	if err != nil {
		t.Fatalf("stage=observation_upstream action=parse_gateway_url error=%v", err)
	}
	address := parsed.Host
	if parsed.Port() == "" {
		address = net.JoinHostPort(parsed.Hostname(), "80")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	connection, err := (&net.Dialer{}).DialContext(ctx, "udp4", address)
	if err != nil {
		t.Fatalf("stage=observation_upstream action=resolve_callback_interface error=%v", err)
	}
	defer connection.Close()
	localAddress, ok := connection.LocalAddr().(*net.UDPAddr)
	if !ok || localAddress.IP == nil || localAddress.IP.IsLoopback() {
		t.Fatalf("stage=observation_upstream field=callback_ip value=%v", connection.LocalAddr())
	}
	return localAddress.IP
}

func createRequestLogObservationAccount(t *testing.T, fixture *protocolFixture, baseURL string) int64 {
	t.Helper()
	var response struct {
		Account struct {
			ID int64 `json:"id"`
		} `json:"account"`
	}
	mustJSON(t, fixture.client, http.MethodPost, fixture.env.baseURL, "/api/admin/provider-accounts/api-upstream", "observation_account_create", nil, map[string]any{
		"name":       "Request log observation " + fixture.suffix,
		"baseUrl":    baseURL,
		"apiKey":     "request-log-observation-key",
		"enabled":    true,
		"priority":   0,
		"loadFactor": 1,
		"models":     []string{e2eModel},
	}, &response, http.StatusCreated)
	if response.Account.ID <= 0 {
		t.Fatal("stage=observation_account_create field=id")
	}
	fixture.resources.accountIDs = append(fixture.resources.accountIDs, response.Account.ID)
	return response.Account.ID
}

func mustGatewayPOSTWithRequestID(t *testing.T, client *http.Client, target, secret, requestID string, input any) gatewayResult {
	t.Helper()
	encoded, err := json.Marshal(input)
	if err != nil {
		t.Fatal("stage=gateway_request failure=encode_request")
	}
	ctx, cancel := context.WithTimeout(context.Background(), requestTimeout)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, target, bytes.NewReader(encoded))
	if err != nil {
		t.Fatal("stage=gateway_request failure=create_request")
	}
	req.Header.Set("Authorization", "Bearer "+secret)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Request-ID", requestID)
	response, err := client.Do(req)
	if err != nil {
		t.Fatal("stage=gateway_request failure=send_request")
	}
	defer response.Body.Close()
	body, readErr := io.ReadAll(io.LimitReader(response.Body, maxResponseBytes))
	return gatewayResult{
		status:  response.StatusCode,
		header:  response.Header.Clone(),
		body:    body,
		readErr: readErr != nil,
	}
}

func assertSuccessfulRequestLogResponse(t *testing.T, result gatewayResult) {
	t.Helper()
	if result.status != http.StatusOK || result.readErr {
		t.Fatalf("stage=gateway_response status=%d read_error=%t body=%s", result.status, result.readErr, string(result.body))
	}
	if contentType := strings.ToLower(result.header.Get("Content-Type")); !strings.HasPrefix(contentType, "application/json") {
		t.Fatalf("stage=gateway_response field=content_type actual=%q", contentType)
	}
	if object := responseObject(result.body); object != "chat.completion" {
		t.Fatalf("stage=gateway_response field=object actual=%q", object)
	}
	if requestID := result.header.Get("X-Request-ID"); !systemevent.ValidCorrelationID(requestID) {
		t.Fatalf("stage=gateway_response field=request_id actual=%q", requestID)
	}
}

func waitForRequestLogObservation(t *testing.T, observations <-chan requestLogObservation, sessionID string) requestLogObservation {
	t.Helper()
	select {
	case observation := <-observations:
		if observation.sessionID != sessionID {
			t.Fatalf("stage=observation_upstream field=session_id actual=%q expected=%q", observation.sessionID, sessionID)
		}
		return observation
	case <-time.After(requestTimeout):
		t.Fatalf("stage=observation_upstream session=%s failure=timeout", sessionID)
		return requestLogObservation{}
	}
}

func waitForRequestLogCorrelationRow(t *testing.T, pool *pgxpool.Pool, clientKeyID int64, sessionID string) requestLogCorrelationRow {
	t.Helper()
	deadline := time.Now().Add(requestLogTimeout)
	for {
		rows := queryRequestLogCorrelationRows(t, pool, clientKeyID, sessionID)
		if len(rows) == 1 {
			return rows[0]
		}
		if len(rows) > 1 || time.Now().After(deadline) {
			t.Fatalf("stage=request_logs session=%s field=row_count actual=%d expected=1", sessionID, len(rows))
		}
		time.Sleep(100 * time.Millisecond)
	}
}

func queryRequestLogCorrelationRows(t *testing.T, pool *pgxpool.Pool, clientKeyID int64, sessionID string) []requestLogCorrelationRow {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), requestTimeout)
	defer cancel()
	rows, err := pool.Query(ctx, `
		SELECT request_id, session_id, status_code
		FROM request_logs
		WHERE client_key_id = $1 AND session_id = $2
		ORDER BY id
	`, clientKeyID, sessionID)
	if err != nil {
		t.Fatalf("stage=request_logs action=query error=%v", err)
	}
	defer rows.Close()
	result := make([]requestLogCorrelationRow, 0, 1)
	for rows.Next() {
		var row requestLogCorrelationRow
		if err := rows.Scan(&row.requestID, &row.sessionID, &row.statusCode); err != nil {
			t.Fatalf("stage=request_logs action=scan error=%v", err)
		}
		result = append(result, row)
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("stage=request_logs action=iterate error=%v", err)
	}
	return result
}

func installRequestLogFailureTrigger(t *testing.T, pool *pgxpool.Pool, suffix, sessionID, canary string) func() {
	t.Helper()
	functionName := "n2api_e2e_reject_request_log_" + suffix
	triggerName := functionName + "_trigger"
	statement := fmt.Sprintf(`
		CREATE FUNCTION %s() RETURNS trigger LANGUAGE plpgsql AS $function$
		BEGIN
			RAISE EXCEPTION USING MESSAGE = '%s';
		END
		$function$;
		CREATE TRIGGER %s
			BEFORE INSERT ON request_logs
			FOR EACH ROW
			WHEN (NEW.session_id = '%s')
			EXECUTE FUNCTION %s();
	`, functionName, canary, triggerName, sessionID, functionName)
	ctx, cancel := context.WithTimeout(context.Background(), requestTimeout)
	defer cancel()
	if _, err := pool.Exec(ctx, statement); err != nil {
		t.Fatalf("stage=request_log_write_failure action=install_trigger error=%v", err)
	}
	cleanup := func() {
		cleanupCtx, cleanupCancel := context.WithTimeout(context.Background(), requestTimeout)
		defer cleanupCancel()
		cleanup := fmt.Sprintf("DROP TRIGGER IF EXISTS %s ON request_logs; DROP FUNCTION IF EXISTS %s();", triggerName, functionName)
		if _, err := pool.Exec(cleanupCtx, cleanup); err != nil {
			t.Errorf("stage=request_log_write_failure action=remove_trigger error=%v", err)
		}
	}
	t.Cleanup(cleanup)
	return cleanup
}

func readRequestLogWriteHealth(t *testing.T, fixture *protocolFixture) (requestLogWriteHealth, []byte) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), requestTimeout)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, fixture.env.baseURL+"/api/admin/health", nil)
	if err != nil {
		t.Fatal("stage=admin_health failure=create_request")
	}
	response, err := fixture.client.Do(req)
	if err != nil {
		t.Fatal("stage=admin_health failure=send_request")
	}
	defer response.Body.Close()
	raw, err := io.ReadAll(io.LimitReader(response.Body, maxResponseBytes))
	if err != nil {
		t.Fatal("stage=admin_health failure=read_response")
	}
	if response.StatusCode != http.StatusOK {
		t.Fatalf("stage=admin_health status=%d expected=%d", response.StatusCode, http.StatusOK)
	}
	var health struct {
		Status   string                     `json:"status"`
		Database string                     `json:"database"`
		Tasks    map[string]json.RawMessage `json:"tasks"`
	}
	if err := json.Unmarshal(raw, &health); err != nil {
		t.Fatal("stage=admin_health failure=decode_response")
	}
	if health.Status != "ok" || health.Database != "ok" {
		t.Fatalf("stage=admin_health field=service_status status=%q database=%q", health.Status, health.Database)
	}
	var status requestLogWriteHealth
	if err := json.Unmarshal(health.Tasks["requestLogWrite"], &status); err != nil {
		t.Fatal("stage=admin_health field=request_log_write failure=decode_status")
	}
	return status, raw
}

func waitForRequestLogWriteFailure(t *testing.T, fixture *protocolFixture, baselineTotal uint64) (requestLogWriteHealth, []byte) {
	t.Helper()
	deadline := time.Now().Add(requestLogTimeout)
	for {
		status, raw := readRequestLogWriteHealth(t, fixture)
		if status.TotalFailures > baselineTotal {
			return status, raw
		}
		if time.Now().After(deadline) {
			t.Fatalf("stage=request_log_write_failure field=health_timeout total_failures=%d baseline=%d", status.TotalFailures, baselineTotal)
		}
		time.Sleep(100 * time.Millisecond)
	}
}

func waitForRequestLogWriteRecovery(t *testing.T, fixture *protocolFixture, failed requestLogWriteHealth) requestLogWriteHealth {
	t.Helper()
	deadline := time.Now().Add(requestLogTimeout)
	for {
		status, _ := readRequestLogWriteHealth(t, fixture)
		if status.LastSucceededAt != nil && status.LastFailedAt != nil &&
			!status.LastSucceededAt.Before(*status.LastFailedAt) && status.ConsecutiveFailures == 0 {
			return status
		}
		if time.Now().After(deadline) {
			t.Fatalf("stage=request_log_write_recovery field=health_timeout status=%+v failed=%+v", status, failed)
		}
		time.Sleep(100 * time.Millisecond)
	}
}

func requestLogCount(t *testing.T, pool *pgxpool.Pool, clientKeyID int64, sessionID string) int {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), requestTimeout)
	defer cancel()
	var count int
	if err := pool.QueryRow(ctx, `SELECT COUNT(*) FROM request_logs WHERE client_key_id = $1 AND session_id = $2`, clientKeyID, sessionID).Scan(&count); err != nil {
		t.Fatalf("stage=request_logs action=count error=%v", err)
	}
	return count
}

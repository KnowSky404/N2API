package main

import (
	"bufio"
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"strconv"
	"strings"
	"testing"
	"time"
)

func TestRunBuildsStrictSanitizedArtifacts(t *testing.T) {
	rawDir := t.TempDir()
	outputDir := t.TempDir()
	canaries := []string{
		"LEAK_CLIENT_SECRET_123",
		"LEAK_ACCOUNT_NAME_123",
		"LEAK_SESSION_123",
		"LEAK_MODEL_123",
		"LEAK_BODY_123",
	}
	writeTestFile(t, rawDir, "canaries.txt", strings.Join(canaries, "\n"))
	writeTestFile(t, rawDir, "gateway-e2e.log", strings.Join([]string{
		"=== RUN   TestGatewayFallback/status-503-once",
		"protocol_e2e_test.go:10: stage=request_logs scenario=status-503-once field=retry_counts status=503",
		"--- FAIL: TestGatewayFallback/status-503-once (0.01s)",
		"raw response LEAK_BODY_123 Authorization: Bearer raw-credential",
	}, "\n"))
	writeTestFile(t, rawDir, "n2api.log", "n2api-1 | 2026-07-21T12:00:00Z INFO request completed LEAK_BODY_123\n")
	writeTestFile(t, rawDir, "services.json", strings.Join([]string{
		`{"Service":"n2api","State":"running","Health":"healthy","ExitCode":0,"Secret":"LEAK_CLIENT_SECRET_123"}`,
		`{"Service":"unlisted","State":"invented","Health":"mystery","ExitCode":999,"Cookie":"LEAK_BODY_123"}`,
	}, "\n"))
	writeTestFile(t, rawDir, "mock-state.json", `{
  "entries": [
    {"scenario":"status-503-once","method":"POST","route":"/v1/responses","status":503,"count":1,"body":"LEAK_BODY_123"},
    {"scenario":"invented","method":"DELETE","route":"/private","status":999,"count":-2,"authorization":"LEAK_CLIENT_SECRET_123"}
  ],
  "unknown":"LEAK_BODY_123"
}`)
	writeTestFile(t, rawDir, "request-logs.csv", strings.Join([]string{
		"request_id,client_key_id,provider_account_id,routing_pool_id,method,route,status_code,latency_ms,error,usage_source,gateway_attempt_count,gateway_fallback_count,created_at,client_key,provider_account_name,model,session_id,total_tokens,pricing_snapshot",
		"req_safe123,12,7,9,POST,/v1/responses/resp_private/input_items,200,42,arbitrary free text,stream,2,1,2026-07-21T12:00:00Z,LEAK_CLIENT_SECRET_123,LEAK_ACCOUNT_NAME_123,LEAK_MODEL_123,LEAK_SESSION_123,25,LEAK_BODY_123",
	}, "\n"))

	err := run(config{
		suite:      "gateway",
		runID:      "12345-2",
		rawDir:     rawDir,
		outputDir:  outputDir,
		canaryFile: filepath.Join(rawDir, "canaries.txt"),
		now:        func() time.Time { return time.Date(2026, 7, 21, 12, 30, 0, 0, time.UTC) },
	})
	if err != nil {
		t.Fatal("run returned error")
	}

	files, err := artifactFiles(outputDir)
	if err != nil {
		t.Fatal("list artifacts")
	}
	wantFiles := []string{"events.jsonl", "manifest.json", "request-logs.jsonl", "safe.marker", "scenarios.json", "services.json"}
	if !reflect.DeepEqual(files, wantFiles) {
		t.Fatalf("artifact files = %v, want %v", files, wantFiles)
	}
	for _, name := range files {
		raw, err := os.ReadFile(filepath.Join(outputDir, name))
		if err != nil {
			t.Fatalf("read artifact %s", name)
		}
		for _, canary := range canaries {
			if strings.Contains(string(raw), canary) {
				t.Fatalf("artifact %s contains canary", name)
			}
		}
	}

	requests := readJSONLines[safeRequestLog](t, filepath.Join(outputDir, "request-logs.jsonl"))
	if len(requests) != 1 {
		t.Fatalf("requests = %d, want 1", len(requests))
	}
	request := requests[0]
	if request.RequestID != "req_safe123" || request.ClientKeyID != 12 || request.ProviderAccountID != 7 || request.RoutingPoolID != 9 {
		t.Fatalf("request attribution = %+v", request)
	}
	if request.Route != "/v1/responses/:id/input_items" || request.ErrorCode != "unknown" || request.UsageSource != "stream" {
		t.Fatalf("request canonical fields = %+v", request)
	}

	var scenarios safeScenarios
	readTestJSON(t, filepath.Join(outputDir, "scenarios.json"), &scenarios)
	if len(scenarios.Scenarios) != 2 || scenarios.Scenarios[1] != (safeScenario{Scenario: "unknown", Method: "unknown", Route: "unknown", Status: 0, Count: 0}) {
		t.Fatalf("scenarios = %+v", scenarios.Scenarios)
	}
}

func TestRunRejectsExactCanaryWithoutSafeMarker(t *testing.T) {
	rawDir := t.TempDir()
	outputDir := t.TempDir()
	writeTestFile(t, rawDir, "canaries.txt", "EXACT_CANARY_123\n")
	writeTestFile(t, rawDir, "gateway-e2e.log", "stage=EXACT_CANARY_123 scenario=happy field=status\n")

	err := run(config{
		suite:      "gateway",
		runID:      "99-1",
		rawDir:     rawDir,
		outputDir:  outputDir,
		canaryFile: filepath.Join(rawDir, "canaries.txt"),
	})
	if err == nil {
		t.Fatal("run succeeded with leaked canary")
	}
	if _, err := os.Stat(filepath.Join(outputDir, "safe.marker")); !os.IsNotExist(err) {
		t.Fatal("safe.marker exists after rejected artifact")
	}
}

func TestRunEnforcesBounds(t *testing.T) {
	rawDir := t.TempDir()
	outputDir := t.TempDir()
	var logs strings.Builder
	for index := 0; index < maxEvents+3; index++ {
		logs.WriteString("=== RUN   TestBound/")
		logs.WriteString(intString(index))
		logs.WriteByte('\n')
	}
	writeTestFile(t, rawDir, "gateway-e2e.log", logs.String())

	entries := make([]map[string]any, maxScenarios+2)
	for index := range entries {
		entries[index] = map[string]any{"scenario": "happy", "method": "POST", "route": "/v1/responses", "status": 200, "count": index + 1}
	}
	mockRaw, _ := json.Marshal(map[string]any{"entries": entries})
	writeTestFile(t, rawDir, "mock-state.json", string(mockRaw))

	var csv strings.Builder
	csv.WriteString("request_id,client_key_id,provider_account_id,routing_pool_id,method,route,status_code,latency_ms,error,usage_source,gateway_attempt_count,gateway_fallback_count,created_at\n")
	for index := 0; index < maxRequests+2; index++ {
		csv.WriteString("req_bound")
		csv.WriteString(intString(index))
		csv.WriteString(",1,2,3,POST,/v1/responses,200,1,,missing,1,0,2026-07-21T12:00:00Z\n")
	}
	writeTestFile(t, rawDir, "request-logs.csv", csv.String())

	if err := run(config{suite: "gateway", runID: "100-1", rawDir: rawDir, outputDir: outputDir}); err != nil {
		t.Fatal("run returned error")
	}
	if got := len(readJSONLines[safeEvent](t, filepath.Join(outputDir, "events.jsonl"))); got != maxEvents {
		t.Fatalf("events = %d, want %d", got, maxEvents)
	}
	if got := len(readJSONLines[safeRequestLog](t, filepath.Join(outputDir, "request-logs.jsonl"))); got != maxRequests {
		t.Fatalf("requests = %d, want %d", got, maxRequests)
	}
	var scenarios safeScenarios
	readTestJSON(t, filepath.Join(outputDir, "scenarios.json"), &scenarios)
	if len(scenarios.Scenarios) != maxScenarios {
		t.Fatalf("scenarios = %d, want %d", len(scenarios.Scenarios), maxScenarios)
	}
	var meta manifest
	readTestJSON(t, filepath.Join(outputDir, "manifest.json"), &meta)
	if !meta.Truncated || meta.EventCount != maxEvents || meta.ScenarioCount != maxScenarios || meta.RequestCount != maxRequests {
		t.Fatalf("manifest bounds = %+v", meta)
	}
}

func TestCollectServicesAcceptsComposeNDJSONAndDropsUnknownFields(t *testing.T) {
	rawDir := t.TempDir()
	path := filepath.Join(rawDir, "services.json")
	writeTestFile(t, rawDir, "services.json", strings.Join([]string{
		`{"Service":"postgres","State":"running","Health":"healthy","ExitCode":0,"Publishers":[{"URL":"https://user:pass@example.test"}]}`,
		`{"Service":"gateway-e2e","State":"exited","Health":"","ExitCode":1,"Command":"Bearer secret"}`,
	}, "\n"))

	services, err := collectServices(path)
	if err != nil {
		t.Fatal("collectServices returned error")
	}
	want := []safeService{
		{Service: "gateway-e2e", State: "exited", Health: "none", ExitCode: 1},
		{Service: "postgres", State: "running", Health: "healthy", ExitCode: 0},
	}
	if !reflect.DeepEqual(services, want) {
		t.Fatalf("services = %+v, want %+v", services, want)
	}
}

func TestCollectEventsIncludesMockSmokeLog(t *testing.T) {
	rawDir := t.TempDir()
	writeTestFile(t, rawDir, "mock-smoke.log", strings.Join([]string{
		"=== RUN   TestMockOpenAIResponses",
		"--- PASS: TestMockOpenAIResponses (0.01s)",
	}, "\n"))

	events, truncated, err := collectEvents(rawDir)
	if err != nil {
		t.Fatal("collectEvents returned error")
	}
	if truncated {
		t.Fatal("collectEvents unexpectedly truncated smoke log")
	}
	want := []safeEvent{
		{Service: "mock-smoke", Category: "go_test", Status: "run", Test: "TestMockOpenAIResponses"},
		{Service: "mock-smoke", Category: "go_test", Status: "pass", Test: "TestMockOpenAIResponses"},
	}
	if !reflect.DeepEqual(events, want) {
		t.Fatalf("events = %+v, want %+v", events, want)
	}
}

func TestRunRejectsUnknownSuiteAndMalformedRunID(t *testing.T) {
	for _, cfg := range []config{
		{suite: "other", runID: "1-1", rawDir: t.TempDir(), outputDir: t.TempDir()},
		{suite: "gateway", runID: "branch-main", rawDir: t.TempDir(), outputDir: t.TempDir()},
	} {
		if err := run(cfg); err == nil {
			t.Fatalf("run accepted suite=%s runID=%s", cfg.suite, cfg.runID)
		}
	}
}

func TestVerifyOutputsRejectsSensitivePatterns(t *testing.T) {
	for name, unsafe := range map[string]string{
		"bearer":       `{"stage":"Bearer abc.def"}`,
		"cookie":       `{"cookie":"value"}`,
		"jwt":          `{"stage":"eyJhbGciOiJIUzI1NiJ9.eyJzdWIiOiIxIn0.signature"}`,
		"n2api-token":  `{"stage":"n2api_abcdefghijklmnopqrstuvwxyz"}`,
		"openai-token": `{"stage":"sk-abcdefghijklmnopqrstuvwxyz"}`,
		"url-userinfo": `{"stage":"https://user:pass@example.test"}`,
		"sse-data":     "data: secret\n",
		"body":         `{"body":"secret"}`,
		"token-key":    `{"total_tokens":25}`,
	} {
		t.Run(name, func(t *testing.T) {
			dir := t.TempDir()
			for _, outputName := range outputNames[:len(outputNames)-1] {
				content := "{}\n"
				if outputName == "events.jsonl" {
					content = unsafe
				}
				writeTestFile(t, dir, outputName, content)
			}
			if err := verifyOutputs(dir, nil); err == nil {
				t.Fatal("verifyOutputs accepted sensitive pattern")
			}
		})
	}
}

func TestVerifyOutputsEnforcesPerFileSizeLimit(t *testing.T) {
	for _, test := range []struct {
		name    string
		size    int
		wantErr bool
	}{
		{name: "at-limit", size: maxOutputBytes},
		{name: "over-limit", size: maxOutputBytes + 1, wantErr: true},
	} {
		t.Run(test.name, func(t *testing.T) {
			dir := t.TempDir()
			for _, outputName := range outputNames[:len(outputNames)-1] {
				content := []byte("{}\n")
				if outputName == "events.jsonl" {
					content = []byte(strings.Repeat(" ", test.size))
				}
				if err := os.WriteFile(filepath.Join(dir, outputName), content, 0o600); err != nil {
					t.Fatalf("write %s", outputName)
				}
			}
			err := verifyOutputs(dir, nil)
			if test.wantErr && err == nil {
				t.Fatal("verifyOutputs accepted oversized file")
			}
			if !test.wantErr && err != nil {
				t.Fatalf("verifyOutputs rejected boundary-sized file: %v", err)
			}
		})
	}
}

func TestVerifyOutputsEnforcesTotalArtifactSizeLimit(t *testing.T) {
	dir := t.TempDir()
	for _, outputName := range outputNames[:len(outputNames)-1] {
		content := []byte(strings.Repeat(" ", 220<<10))
		if err := os.WriteFile(filepath.Join(dir, outputName), content, 0o600); err != nil {
			t.Fatalf("write %s", outputName)
		}
	}
	if err := verifyOutputs(dir, nil); err == nil {
		t.Fatal("verifyOutputs accepted oversized artifact")
	}
}

func writeTestFile(t *testing.T, dir, name, content string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(dir, name), []byte(content), 0o600); err != nil {
		t.Fatalf("write %s", name)
	}
}

func readTestJSON(t *testing.T, path string, output any) {
	t.Helper()
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal("read JSON")
	}
	if err := json.Unmarshal(raw, output); err != nil {
		t.Fatal("decode JSON")
	}
}

func readJSONLines[T any](t *testing.T, path string) []T {
	t.Helper()
	file, err := os.Open(path)
	if err != nil {
		t.Fatal("open JSONL")
	}
	defer file.Close()
	var result []T
	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		var value T
		if err := json.Unmarshal(scanner.Bytes(), &value); err != nil {
			t.Fatal("decode JSONL")
		}
		result = append(result, value)
	}
	if scanner.Err() != nil {
		t.Fatal("scan JSONL")
	}
	return result
}

func intString(value int) string {
	return strconv.Itoa(value)
}

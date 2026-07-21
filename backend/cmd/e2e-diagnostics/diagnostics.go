package main

import (
	"bufio"
	"bytes"
	"encoding/csv"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"
)

const (
	maxEvents        = 500
	maxScenarios     = 32
	maxRequests      = 50
	maxStringLength  = 100
	maxOutputBytes   = 256 << 10
	maxArtifactBytes = 1 << 20
)

var (
	tokenPattern     = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9_.:/-]{0,99}$`)
	runIDPattern     = regexp.MustCompile(`^[0-9]{1,40}-[0-9]{1,10}$`)
	requestIDPattern = regexp.MustCompile(`^req_[A-Za-z0-9_-]{1,96}$`)
	timestampPattern = regexp.MustCompile(`\d{4}-\d{2}-\d{2}T\d{2}:\d{2}:\d{2}(?:\.\d+)?Z`)
	goRunPattern     = regexp.MustCompile(`^=== RUN\s+([^\s]+)`)
	goResultPattern  = regexp.MustCompile(`^--- (PASS|FAIL|SKIP):\s+([^\s]+)`)
	keyValuePattern  = regexp.MustCompile(`(?:^|\s)(stage|scenario|field|failure|status)=([^\s]+)`)
	levelPattern     = regexp.MustCompile(`(?i)(?:^|[^A-Za-z])(debug|info|warn|warning|error|fatal|panic)(?:[^A-Za-z]|$)`)

	sensitivePatterns = []*regexp.Regexp{
		regexp.MustCompile(`(?i)\bbearer\s+[A-Za-z0-9._~+/=-]+`),
		regexp.MustCompile(`(?i)["']?(?:cookie|set-cookie|authorization)["']?\s*[:=]`),
		regexp.MustCompile(`\beyJ[A-Za-z0-9_-]+\.eyJ[A-Za-z0-9_-]+\.[A-Za-z0-9_-]+\b`),
		regexp.MustCompile(`\bn2api_[A-Za-z0-9_-]{12,}\b`),
		regexp.MustCompile(`\bsk-[A-Za-z0-9_-]{12,}\b`),
		regexp.MustCompile(`\bgh[opsu]_[A-Za-z0-9]{20,}\b`),
		regexp.MustCompile(`(?i)\b[a-z][a-z0-9+.-]*://[^\s/:@]+:[^\s/@]+@`),
		regexp.MustCompile(`(?im)^\s*(?:data|event):\s*`),
		regexp.MustCompile(`(?i)["']?(?:body|[a-z0-9_]*(?:secret|password|token)[a-z0-9_]*|api_key|encrypted_[a-z0-9_]+)["']?\s*[:=]`),
	}
)

var outputNames = []string{
	"manifest.json",
	"events.jsonl",
	"services.json",
	"scenarios.json",
	"request-logs.jsonl",
	"safe.marker",
}

var logSources = []struct {
	name     string
	service  string
	category string
}{
	{"gateway-e2e.log", "gateway-e2e", "test"},
	{"mock-smoke.log", "mock-smoke", "test"},
	{"contracts-javascript.log", "contracts-javascript", "test"},
	{"contracts-python.log", "contracts-python", "test"},
	{"n2api.log", "n2api", "application"},
	{"postgres.log", "postgres", "database"},
	{"mock-openai.log", "mock-openai", "mock"},
}

var knownServices = stringSet(
	"gateway-e2e", "mock-smoke", "contracts-javascript", "contracts-python", "n2api", "postgres", "mock-openai",
)

var knownSuites = stringSet("mock-smoke", "gateway", "sdk-contracts")

var knownScenarios = stringSet(
	"happy", "status-401", "status-403", "status-429", "status-500", "status-503", "status-503-once",
	"missing-content-type", "wrong-content-type", "missing-usage", "malformed-usage", "missing-completion",
	"timeout-before-headers", "disconnect-before-headers", "disconnect-after-first-event",
)

var knownErrors = stringSet(
	"api_key_concurrency_limited", "api_key_cost_budget_exceeded", "api_key_request_budget_exceeded",
	"api_key_request_rate_limited", "api_key_token_budget_exceeded", "api_key_token_rate_limited",
	"gateway_concurrency_limited", "internal_error", "invalid_request", "model_not_found", "model_unavailable",
	"provider_account_concurrency_limited", "provider_accounts_disabled", "provider_accounts_unavailable",
	"provider_not_configured", "provider_not_connected", "routing_pool_cycle", "routing_pool_disabled",
	"routing_pool_empty", "routing_pool_exhausted", "routing_pool_required", "routing_pool_unavailable",
	"service_unavailable", "upstream_error", "upstream_forbidden", "upstream_rate_limited",
	"upstream_request_error", "upstream_token_error", "upstream_unauthorized", "upstream_unavailable",
)

var knownUsageSources = stringSet(
	"missing", "chat_completions", "responses", "stream", "provider_test", "json",
	"anthropic_usage", "gemini_usage_metadata",
)

type config struct {
	suite      string
	runID      string
	rawDir     string
	outputDir  string
	canaryFile string
	now        func() time.Time
}

type manifest struct {
	SchemaVersion int    `json:"schemaVersion"`
	Suite         string `json:"suite"`
	RunID         string `json:"runId"`
	GeneratedAt   string `json:"generatedAt"`
	EventCount    int    `json:"eventCount"`
	ServiceCount  int    `json:"serviceCount"`
	ScenarioCount int    `json:"scenarioCount"`
	RequestCount  int    `json:"requestCount"`
	Truncated     bool   `json:"truncated"`
}

type safeEvent struct {
	Service   string `json:"service"`
	Timestamp string `json:"timestamp,omitempty"`
	Level     string `json:"level,omitempty"`
	Category  string `json:"category"`
	Status    string `json:"status,omitempty"`
	Test      string `json:"test,omitempty"`
	Stage     string `json:"stage,omitempty"`
	Scenario  string `json:"scenario,omitempty"`
	Field     string `json:"field,omitempty"`
	Failure   string `json:"failure,omitempty"`
}

type safeService struct {
	Service  string `json:"service"`
	State    string `json:"state"`
	Health   string `json:"health"`
	ExitCode int    `json:"exitCode"`
}

type safeServices struct {
	Services []safeService `json:"services"`
}

type safeScenario struct {
	Scenario string `json:"scenario"`
	Method   string `json:"method"`
	Route    string `json:"route"`
	Status   int    `json:"status"`
	Count    int64  `json:"count"`
}

type safeScenarios struct {
	Scenarios []safeScenario `json:"scenarios"`
}

type safeRequestLog struct {
	RequestID         string `json:"requestId"`
	ClientKeyID       int64  `json:"clientKeyId"`
	ProviderAccountID int64  `json:"providerAccountId"`
	RoutingPoolID     int64  `json:"routingPoolId"`
	Method            string `json:"method"`
	Route             string `json:"route"`
	StatusCode        int    `json:"statusCode"`
	LatencyMS         int    `json:"latencyMs"`
	ErrorCode         string `json:"errorCode"`
	UsageSource       string `json:"usageSource"`
	AttemptCount      int    `json:"attemptCount"`
	FallbackCount     int    `json:"fallbackCount"`
	CreatedAt         string `json:"createdAt"`
}

func run(cfg config) error {
	suite := canonicalKnown(strings.TrimSpace(cfg.suite), knownSuites)
	runID := strings.TrimSpace(cfg.runID)
	if suite == "unknown" || !runIDPattern.MatchString(runID) || strings.TrimSpace(cfg.rawDir) == "" || strings.TrimSpace(cfg.outputDir) == "" {
		return errors.New("invalid configuration")
	}
	if cfg.now == nil {
		cfg.now = time.Now
	}
	if err := prepareOutputDir(cfg.outputDir); err != nil {
		return err
	}

	events, eventTruncated, err := collectEvents(cfg.rawDir)
	if err != nil {
		return err
	}
	services, err := collectServices(filepath.Join(cfg.rawDir, "services.json"))
	if err != nil {
		return err
	}
	scenarios, scenarioTruncated, err := collectScenarios(filepath.Join(cfg.rawDir, "mock-state.json"))
	if err != nil {
		return err
	}
	requests, requestTruncated, err := collectRequestLogs(filepath.Join(cfg.rawDir, "request-logs.csv"))
	if err != nil {
		return err
	}

	meta := manifest{
		SchemaVersion: 1,
		Suite:         suite,
		RunID:         runID,
		GeneratedAt:   cfg.now().UTC().Format(time.RFC3339Nano),
		EventCount:    len(events),
		ServiceCount:  len(services),
		ScenarioCount: len(scenarios),
		RequestCount:  len(requests),
		Truncated:     eventTruncated || scenarioTruncated || requestTruncated,
	}

	if err := writeJSON(filepath.Join(cfg.outputDir, "manifest.json"), meta); err != nil {
		return err
	}
	if err := writeJSONLines(filepath.Join(cfg.outputDir, "events.jsonl"), events); err != nil {
		return err
	}
	if err := writeJSON(filepath.Join(cfg.outputDir, "services.json"), safeServices{Services: services}); err != nil {
		return err
	}
	if err := writeJSON(filepath.Join(cfg.outputDir, "scenarios.json"), safeScenarios{Scenarios: scenarios}); err != nil {
		return err
	}
	if err := writeJSONLines(filepath.Join(cfg.outputDir, "request-logs.jsonl"), requests); err != nil {
		return err
	}

	canaries, err := readCanaries(cfg.canaryFile)
	if err != nil {
		return err
	}
	if err := verifyOutputs(cfg.outputDir, canaries); err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(cfg.outputDir, "safe.marker"), []byte("safe\n"), 0o600)
}

func prepareOutputDir(dir string) error {
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return err
	}
	if err := os.Chmod(dir, 0o700); err != nil {
		return err
	}
	allowed := make(map[string]struct{}, len(outputNames))
	for _, name := range outputNames {
		allowed[name] = struct{}{}
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		return err
	}
	for _, entry := range entries {
		if _, ok := allowed[entry.Name()]; !ok {
			return errors.New("output directory contains unknown files")
		}
		if err := os.Remove(filepath.Join(dir, entry.Name())); err != nil {
			return err
		}
	}
	return nil
}

func collectEvents(rawDir string) ([]safeEvent, bool, error) {
	events := make([]safeEvent, 0)
	truncated := false
	for _, source := range logSources {
		path := filepath.Join(rawDir, source.name)
		file, err := os.Open(path)
		if errors.Is(err, os.ErrNotExist) {
			continue
		}
		if err != nil {
			return nil, false, err
		}
		scanner := bufio.NewScanner(file)
		scanner.Buffer(make([]byte, 4096), 256*1024)
		for scanner.Scan() {
			event, ok := parseEvent(scanner.Text(), source.service, source.category)
			if !ok {
				continue
			}
			if len(events) >= maxEvents {
				truncated = true
				continue
			}
			events = append(events, event)
		}
		scanErr := scanner.Err()
		_ = file.Close()
		if scanErr != nil {
			return nil, false, scanErr
		}
	}
	return events, truncated, nil
}

func parseEvent(line, service, category string) (safeEvent, bool) {
	line = stripComposePrefix(strings.TrimSpace(line))
	event := safeEvent{Service: service, Category: category}
	if match := timestampPattern.FindString(line); match != "" {
		if parsed, err := time.Parse(time.RFC3339Nano, match); err == nil {
			event.Timestamp = parsed.UTC().Format(time.RFC3339Nano)
		}
	}
	if match := levelPattern.FindStringSubmatch(line); len(match) == 2 {
		event.Level = canonicalLevel(match[1])
	}
	if match := goRunPattern.FindStringSubmatch(line); len(match) == 2 {
		event.Category = "go_test"
		event.Status = "run"
		event.Test = canonicalToken(match[1])
		return event, true
	}
	if match := goResultPattern.FindStringSubmatch(line); len(match) == 3 {
		event.Category = "go_test"
		event.Status = strings.ToLower(match[1])
		event.Test = canonicalToken(match[2])
		return event, true
	}
	foundField := false
	for _, match := range keyValuePattern.FindAllStringSubmatch(line, -1) {
		value := canonicalToken(strings.Trim(match[2], `"',;`))
		switch match[1] {
		case "stage":
			event.Stage = value
		case "scenario":
			event.Scenario = canonicalScenario(value)
		case "field":
			event.Field = value
		case "failure":
			event.Failure = value
		case "status":
			event.Status = value
		}
		foundField = true
	}
	if foundField {
		event.Category = "test_failure"
		if event.Level == "" {
			event.Level = "error"
		}
		return event, true
	}
	return event, event.Timestamp != "" || event.Level != ""
}

func stripComposePrefix(line string) string {
	if _, rest, ok := strings.Cut(line, " | "); ok {
		return strings.TrimSpace(rest)
	}
	return line
}

func collectServices(path string) ([]safeService, error) {
	raw, err := readOptional(path)
	if err != nil || len(raw) == 0 {
		return nil, err
	}
	objects, err := decodeJSONObjectStream(raw)
	if err != nil {
		return nil, err
	}
	services := make([]safeService, 0, len(objects))
	for _, object := range objects {
		service := safeService{
			Service:  canonicalKnown(rawString(object, "service"), knownServices),
			State:    canonicalKnown(strings.ToLower(rawString(object, "state")), stringSet("running", "exited", "restarting", "paused", "created", "dead")),
			Health:   canonicalKnown(strings.ToLower(rawString(object, "health")), stringSet("healthy", "unhealthy", "starting", "none")),
			ExitCode: boundedInt(rawInt64(object, "exitcode"), -1, 255),
		}
		if service.Health == "unknown" && strings.TrimSpace(rawString(object, "health")) == "" {
			service.Health = "none"
		}
		services = append(services, service)
	}
	if len(services) > 64 {
		services = services[:64]
	}
	sort.SliceStable(services, func(i, j int) bool { return services[i].Service < services[j].Service })
	return services, nil
}

func collectScenarios(path string) ([]safeScenario, bool, error) {
	raw, err := readOptional(path)
	if err != nil || len(raw) == 0 {
		return nil, false, err
	}
	var envelope struct {
		Entries []map[string]json.RawMessage `json:"entries"`
	}
	if err := json.Unmarshal(raw, &envelope); err != nil {
		return nil, false, err
	}
	truncated := len(envelope.Entries) > maxScenarios
	entries := envelope.Entries
	if truncated {
		entries = entries[:maxScenarios]
	}
	result := make([]safeScenario, 0, len(entries))
	for _, entry := range entries {
		lower := lowerRawMap(entry)
		result = append(result, safeScenario{
			Scenario: canonicalScenario(rawString(lower, "scenario")),
			Method:   canonicalMethod(rawString(lower, "method")),
			Route:    canonicalRoute(rawString(lower, "route")),
			Status:   boundedInt(rawInt64(lower, "status"), 0, 599),
			Count:    maxInt64(rawInt64(lower, "count"), 0),
		})
	}
	return result, truncated, nil
}

func collectRequestLogs(path string) ([]safeRequestLog, bool, error) {
	file, err := os.Open(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil, false, nil
	}
	if err != nil {
		return nil, false, err
	}
	defer file.Close()
	reader := csv.NewReader(file)
	reader.ReuseRecord = true
	header, err := reader.Read()
	if errors.Is(err, io.EOF) {
		return nil, false, nil
	}
	if err != nil {
		return nil, false, err
	}
	indexes := make(map[string]int, len(header))
	for index, name := range header {
		indexes[normalizeColumn(name)] = index
	}
	result := make([]safeRequestLog, 0)
	truncated := false
	for {
		record, err := reader.Read()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return nil, false, err
		}
		if len(result) >= maxRequests {
			truncated = true
			continue
		}
		value := func(name string) string {
			index, ok := indexes[name]
			if !ok || index >= len(record) {
				return ""
			}
			return record[index]
		}
		createdAt := canonicalTimestamp(value("createdat"))
		requestID := value("requestid")
		if !requestIDPattern.MatchString(requestID) {
			requestID = "unknown"
		}
		result = append(result, safeRequestLog{
			RequestID:         requestID,
			ClientKeyID:       nonNegativeInt64(value("clientkeyid")),
			ProviderAccountID: nonNegativeInt64(value("provideraccountid")),
			RoutingPoolID:     nonNegativeInt64(value("routingpoolid")),
			Method:            canonicalMethod(value("method")),
			Route:             canonicalRoute(value("route")),
			StatusCode:        boundedInt(parseInt64(value("statuscode")), 0, 599),
			LatencyMS:         boundedInt(parseInt64(value("latencyms")), 0, int64(^uint(0)>>1)),
			ErrorCode:         canonicalError(value("error")),
			UsageSource:       canonicalKnown(value("usagesource"), knownUsageSources),
			AttemptCount:      boundedInt(parseInt64(value("gatewayattemptcount")), 0, 1000),
			FallbackCount:     boundedInt(parseInt64(value("gatewayfallbackcount")), 0, 1000),
			CreatedAt:         createdAt,
		})
	}
	return result, truncated, nil
}

func decodeJSONObjectStream(raw []byte) ([]map[string]json.RawMessage, error) {
	trimmed := bytes.TrimSpace(raw)
	if len(trimmed) == 0 {
		return nil, nil
	}
	if trimmed[0] == '[' {
		var objects []map[string]json.RawMessage
		if err := json.Unmarshal(trimmed, &objects); err != nil {
			return nil, err
		}
		for index := range objects {
			objects[index] = lowerRawMap(objects[index])
		}
		return objects, nil
	}
	decoder := json.NewDecoder(bytes.NewReader(trimmed))
	var objects []map[string]json.RawMessage
	for {
		var object map[string]json.RawMessage
		if err := decoder.Decode(&object); errors.Is(err, io.EOF) {
			break
		} else if err != nil {
			return nil, err
		}
		objects = append(objects, lowerRawMap(object))
	}
	if len(objects) == 0 {
		return nil, errors.New("services input is not JSON objects")
	}
	return objects, nil
}

func lowerRawMap(input map[string]json.RawMessage) map[string]json.RawMessage {
	result := make(map[string]json.RawMessage, len(input))
	for key, value := range input {
		result[strings.ToLower(strings.TrimSpace(key))] = value
	}
	return result
}

func rawString(object map[string]json.RawMessage, key string) string {
	raw, ok := object[strings.ToLower(key)]
	if !ok {
		return ""
	}
	var value string
	if json.Unmarshal(raw, &value) == nil {
		return strings.TrimSpace(value)
	}
	return ""
}

func rawInt64(object map[string]json.RawMessage, key string) int64 {
	raw, ok := object[strings.ToLower(key)]
	if !ok {
		return 0
	}
	var number json.Number
	if json.Unmarshal(raw, &number) == nil {
		if value, err := number.Int64(); err == nil {
			return value
		}
	}
	var value string
	if json.Unmarshal(raw, &value) == nil {
		return parseInt64(value)
	}
	return 0
}

func readOptional(path string) ([]byte, error) {
	raw, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	return raw, err
}

func writeJSON(path string, value any) error {
	raw, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		return err
	}
	raw = append(raw, '\n')
	return os.WriteFile(path, raw, 0o600)
}

func writeJSONLines[T any](path string, values []T) error {
	file, err := os.OpenFile(path, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0o600)
	if err != nil {
		return err
	}
	encoder := json.NewEncoder(file)
	for _, value := range values {
		if err := encoder.Encode(value); err != nil {
			_ = file.Close()
			return err
		}
	}
	return file.Close()
}

func readCanaries(path string) ([]string, error) {
	if strings.TrimSpace(path) == "" {
		return nil, nil
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var values []string
	if json.Unmarshal(raw, &values) == nil {
		return nonEmptyStrings(values), nil
	}
	return nonEmptyStrings(strings.Split(string(raw), "\n")), nil
}

func nonEmptyStrings(values []string) []string {
	result := make([]string, 0, len(values))
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value != "" {
			result = append(result, value)
		}
	}
	return result
}

func verifyOutputs(dir string, canaries []string) error {
	var total int64
	for _, name := range outputNames[:len(outputNames)-1] {
		raw, err := os.ReadFile(filepath.Join(dir, name))
		if err != nil {
			return err
		}
		if len(raw) > maxOutputBytes {
			return errors.New("artifact file too large")
		}
		total += int64(len(raw))
		if total+int64(len("safe\n")) > maxArtifactBytes {
			return errors.New("artifact too large")
		}
		for _, canary := range canaries {
			if strings.Contains(string(raw), canary) || jsonStreamContainsString(raw, canary) {
				return errors.New("unsafe content detected")
			}
		}
		for _, pattern := range sensitivePatterns {
			if pattern.Match(raw) {
				return errors.New("unsafe content detected")
			}
		}
	}
	return nil
}

func jsonStreamContainsString(raw []byte, target string) bool {
	decoder := json.NewDecoder(bytes.NewReader(raw))
	for {
		var value any
		if err := decoder.Decode(&value); errors.Is(err, io.EOF) {
			return false
		} else if err != nil {
			return false
		}
		if valueContainsString(value, target) {
			return true
		}
	}
}

func valueContainsString(value any, target string) bool {
	switch typed := value.(type) {
	case string:
		return strings.Contains(typed, target)
	case []any:
		for _, item := range typed {
			if valueContainsString(item, target) {
				return true
			}
		}
	case map[string]any:
		for _, item := range typed {
			if valueContainsString(item, target) {
				return true
			}
		}
	}
	return false
}

func canonicalToken(value string) string {
	value = strings.TrimSpace(value)
	if len(value) > maxStringLength || !tokenPattern.MatchString(value) {
		return "unknown"
	}
	return value
}

func canonicalKnown(value string, known map[string]struct{}) string {
	value = strings.TrimSpace(value)
	if _, ok := known[value]; !ok {
		return "unknown"
	}
	return value
}

func canonicalScenario(value string) string {
	return canonicalKnown(strings.TrimSpace(value), knownScenarios)
}

func canonicalMethod(value string) string {
	switch strings.ToUpper(strings.TrimSpace(value)) {
	case "GET":
		return "GET"
	case "POST":
		return "POST"
	default:
		return "unknown"
	}
}

func canonicalRoute(value string) string {
	value = strings.TrimSpace(strings.SplitN(value, "?", 2)[0])
	switch value {
	case "/v1/models", "/v1/chat/completions", "/v1/responses":
		return value
	}
	if strings.HasPrefix(value, "/v1/responses/") {
		parts := strings.Split(strings.TrimPrefix(value, "/v1/responses/"), "/")
		if len(parts) == 1 && parts[0] != "" {
			return "/v1/responses/:id"
		}
		if len(parts) == 2 && parts[0] != "" && parts[1] == "input_items" {
			return "/v1/responses/:id/input_items"
		}
	}
	return "unknown"
}

func canonicalError(value string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return ""
	}
	return canonicalKnown(value, knownErrors)
}

func canonicalLevel(value string) string {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "debug":
		return "debug"
	case "info":
		return "info"
	case "warn", "warning":
		return "warn"
	case "error":
		return "error"
	case "fatal", "panic":
		return "fatal"
	default:
		return "unknown"
	}
}

func canonicalTimestamp(value string) string {
	parsed, err := time.Parse(time.RFC3339Nano, strings.TrimSpace(value))
	if err != nil {
		return ""
	}
	return parsed.UTC().Format(time.RFC3339Nano)
}

func normalizeColumn(value string) string {
	value = strings.ToLower(strings.TrimSpace(value))
	value = strings.NewReplacer("_", "", "-", "", " ", "").Replace(value)
	return value
}

func parseInt64(value string) int64 {
	parsed, _ := strconv.ParseInt(strings.TrimSpace(value), 10, 64)
	return parsed
}

func nonNegativeInt64(value string) int64 {
	return maxInt64(parseInt64(value), 0)
}

func boundedInt(value, minimum, maximum int64) int {
	if value < minimum || value > maximum {
		return int(minimum)
	}
	return int(value)
}

func maxInt64(value, minimum int64) int64 {
	if value < minimum {
		return minimum
	}
	return value
}

func stringSet(values ...string) map[string]struct{} {
	result := make(map[string]struct{}, len(values))
	for _, value := range values {
		result[value] = struct{}{}
	}
	return result
}

func artifactFiles(dir string) ([]string, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, err
	}
	result := make([]string, 0, len(entries))
	for _, entry := range entries {
		if !entry.IsDir() {
			result = append(result, entry.Name())
		}
	}
	sort.Strings(result)
	return result, nil
}

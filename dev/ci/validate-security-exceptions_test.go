package main

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"
)

var validationNow = time.Date(2026, time.July, 23, 12, 0, 0, 0, time.UTC)

func TestValidateRegistryAcceptsEmptyAndActiveExceptions(t *testing.T) {
	for _, data := range []string{
		`{"version":1,"exceptions":[]}`,
		validRegistryJSON(`{
            "scanner":"trivy",
            "cve":"CVE-2026-12345",
            "platform":"linux/amd64",
            "reason":"Upstream fix is not published for this platform.",
            "owner":"security-team",
            "created_at":"2026-07-01T12:00:00Z",
            "expires_at":"2026-07-31T12:00:00Z"
        }`),
		validRegistryJSON(`{
            "scanner":"codeql",
            "rule":"go/example-rule",
            "platform":"source",
            "reason":"The finding is isolated pending the reviewed replacement.",
            "owner":"@maintainer",
            "created_at":"2026-07-22T12:00:00+00:00",
            "expires_at":"2026-07-24T12:00:00+00:00"
        }`),
	} {
		registry, err := parseRegistry([]byte(data))
		if err != nil {
			t.Fatalf("parseRegistry returned error: %v", err)
		}
		if err := validateRegistry(registry, validationNow); err != nil {
			t.Fatalf("validateRegistry returned error: %v", err)
		}
	}
}

func TestRegistryRejectsInvalidStructureAndSemantics(t *testing.T) {
	valid := validExceptionMap()
	tests := []struct {
		name string
		data string
	}{
		{name: "malformed JSON", data: `{"version":1`},
		{name: "trailing JSON", data: `{"version":1,"exceptions":[]} {}`},
		{name: "unknown root field", data: `{"version":1,"exceptions":[],"extra":true}`},
		{name: "wrong case field", data: `{"Version":1,"exceptions":[]}`},
		{name: "duplicate root key", data: `{"version":1,"version":1,"exceptions":[]}`},
		{name: "null registry field", data: `{"version":1,"exceptions":null}`},
		{name: "unsupported version", data: `{"version":2,"exceptions":[]}`},
		{name: "exception is not object", data: `{"version":1,"exceptions":["bad"]}`},
		{name: "unknown exception field", data: registryWith(overrides(valid, "extra", true))},
		{name: "duplicate exception key", data: `{"version":1,"exceptions":[{"scanner":"trivy","scanner":"trivy","package":"pkg","platform":"linux/amd64","reason":"temporary reason","owner":"team","created_at":"2026-07-22T12:00:00Z","expires_at":"2026-07-24T12:00:00Z"}]}`},
		{name: "null exception field", data: registryWith(overrides(valid, "reason", nil))},
		{name: "missing identity", data: registryWith(without(valid, "package"))},
		{name: "multiple identities", data: registryWith(overrides(valid, "cve", "CVE-2026-12345"))},
		{name: "empty required field", data: registryWith(overrides(valid, "owner", ""))},
		{name: "surrounding whitespace", data: registryWith(overrides(valid, "reason", " padded "))},
		{name: "identity whitespace", data: registryWith(overrides(valid, "package", "golang.org/x/ crypto"))},
		{name: "wildcard package", data: registryWith(overrides(valid, "package", "golang.org/x/*"))},
		{name: "wildcard owner", data: registryWith(overrides(valid, "owner", "team-*"))},
		{name: "unsupported scanner", data: registryWith(overrides(valid, "scanner", "unknown"))},
		{name: "unsupported scanner identity", data: registryWith(overrides(valid, "scanner", "bun-audit"))},
		{name: "unsupported platform", data: registryWith(overrides(valid, "platform", "all"))},
		{name: "invalid CVE", data: registryWith(overrides(valid, "package", "", "cve", "cve-any"))},
		{name: "invalid created timestamp", data: registryWith(overrides(valid, "created_at", "2026-07-22"))},
		{name: "invalid expiry timestamp", data: registryWith(overrides(valid, "expires_at", "tomorrow"))},
		{name: "future created", data: registryWith(overrides(valid, "created_at", "2026-07-23T12:00:01Z"))},
		{name: "expired", data: registryWith(overrides(valid, "expires_at", "2026-07-23T12:00:00Z"))},
		{name: "expiry before creation", data: registryWith(overrides(valid, "created_at", "2026-07-22T12:00:00Z", "expires_at", "2026-07-21T12:00:00Z"))},
		{name: "over 30 days", data: registryWith(overrides(valid, "created_at", "2026-06-23T12:00:00Z", "expires_at", "2026-07-24T12:00:01Z"))},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			registry, err := parseRegistry([]byte(test.data))
			if err == nil {
				err = validateRegistry(registry, validationNow)
			}
			if err == nil {
				t.Fatal("validation succeeded, want error")
			}
		})
	}
}

func TestValidateRegistryRejectsDuplicateFindingIdentity(t *testing.T) {
	first := validExceptionMap()
	second := overrides(validExceptionMap(),
		"reason", "A different justification must not hide a duplicate.",
		"owner", "other-owner",
		"created_at", "2026-07-21T12:00:00Z",
		"expires_at", "2026-07-25T12:00:00Z",
	)
	data, err := json.Marshal(map[string]any{"version": 1, "exceptions": []any{first, second}})
	if err != nil {
		t.Fatalf("Marshal returned error: %v", err)
	}
	registry, err := parseRegistry(data)
	if err != nil {
		t.Fatalf("parseRegistry returned error: %v", err)
	}
	if err := validateRegistry(registry, validationNow); err == nil || !strings.Contains(err.Error(), "duplicates") {
		t.Fatalf("validateRegistry error = %v, want duplicate", err)
	}
}

func TestRunUsesTemporaryRegistryWithoutLeakingValues(t *testing.T) {
	directory := t.TempDir()
	path := filepath.Join(directory, "exceptions.json")
	canary := "SECRET_REASON_CANARY_918273"
	data := registryWith(overrides(validExceptionMap(),
		"reason", canary,
		"expires_at", "2026-07-23T12:00:00Z",
	))
	if err := os.WriteFile(path, []byte(data), 0o600); err != nil {
		t.Fatalf("WriteFile returned error: %v", err)
	}

	var stdout, stderr bytes.Buffer
	if code := run([]string{path}, &stdout, &stderr, validationNow); code != 1 {
		t.Fatalf("run code = %d, want 1", code)
	}
	if strings.Contains(stdout.String(), canary) || strings.Contains(stderr.String(), canary) {
		t.Fatalf("validator output leaked registry value: stdout=%q stderr=%q", stdout.String(), stderr.String())
	}
	if !strings.Contains(stderr.String(), "expires_at has expired") {
		t.Fatalf("stderr = %q, want stable expiry error", stderr.String())
	}
}

func TestRunIsQuietByDefaultAndQueriesOnlyActiveIdentifiers(t *testing.T) {
	directory := t.TempDir()
	path := filepath.Join(directory, "exceptions.json")
	canaryReason := "SECRET_REASON_CANARY_112233"
	canaryOwner := "SECRET_OWNER_CANARY_445566"
	data := validRegistryJSON(`{
        "scanner":"trivy",
        "cve":"CVE-2026-12345",
        "platform":"linux/amd64",
        "reason":"` + canaryReason + `",
        "owner":"` + canaryOwner + `",
        "created_at":"2026-07-22T12:00:00Z",
        "expires_at":"2026-07-24T12:00:00Z"
    }`)
	if err := os.WriteFile(path, []byte(data), 0o600); err != nil {
		t.Fatalf("WriteFile returned error: %v", err)
	}

	var stdout, stderr bytes.Buffer
	if code := run([]string{path}, &stdout, &stderr, validationNow); code != 0 {
		t.Fatalf("validate code = %d stderr=%q, want 0", code, stderr.String())
	}
	if stdout.Len() != 0 || stderr.Len() != 0 {
		t.Fatalf("default validation output = stdout:%q stderr:%q, want quiet", stdout.String(), stderr.String())
	}

	stdout.Reset()
	stderr.Reset()
	args := []string{"--active-identifiers", "trivy", "linux/amd64", path}
	if code := run(args, &stdout, &stderr, validationNow); code != 0 {
		t.Fatalf("query code = %d stderr=%q, want 0", code, stderr.String())
	}
	if got, want := stdout.String(), "cve:CVE-2026-12345\n"; got != want {
		t.Fatalf("query output = %q, want %q", got, want)
	}
	if strings.Contains(stdout.String(), canaryReason) || strings.Contains(stdout.String(), canaryOwner) {
		t.Fatalf("query output leaked metadata: %q", stdout.String())
	}
}

func TestRunRejectsUnsupportedActiveIdentifierQuery(t *testing.T) {
	var stdout, stderr bytes.Buffer
	args := []string{"--active-identifiers", "all", "linux/amd64", "unused.json"}
	if code := run(args, &stdout, &stderr, validationNow); code != 2 {
		t.Fatalf("run code = %d, want 2", code)
	}
	if stdout.Len() != 0 || !strings.Contains(stderr.String(), "unsupported scanner or platform") {
		t.Fatalf("query output = stdout:%q stderr:%q", stdout.String(), stderr.String())
	}
}

func TestCheckedInRegistryAndSchemaAreValidJSON(t *testing.T) {
	_, filename, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("runtime.Caller failed")
	}
	repoRoot := filepath.Clean(filepath.Join(filepath.Dir(filename), "..", ".."))

	registryData, err := os.ReadFile(filepath.Join(repoRoot, "security", "exceptions.json"))
	if err != nil {
		t.Fatalf("ReadFile registry returned error: %v", err)
	}
	registry, err := parseRegistry(registryData)
	if err != nil {
		t.Fatalf("parseRegistry returned error: %v", err)
	}
	if err := validateRegistry(registry, time.Now().UTC()); err != nil {
		t.Fatalf("validateRegistry returned error: %v", err)
	}

	schemaData, err := os.ReadFile(filepath.Join(repoRoot, "security", "exceptions.schema.json"))
	if err != nil {
		t.Fatalf("ReadFile schema returned error: %v", err)
	}
	var schema map[string]any
	if err := json.Unmarshal(schemaData, &schema); err != nil {
		t.Fatalf("schema is not valid JSON: %v", err)
	}
	if schema["$schema"] != "https://json-schema.org/draft/2020-12/schema" || schema["additionalProperties"] != false {
		t.Fatalf("schema does not declare the strict expected root: %+v", schema)
	}
}

func validRegistryJSON(exception string) string {
	return `{"version":1,"exceptions":[` + exception + `]}`
}

func validExceptionMap() map[string]any {
	return map[string]any{
		"scanner": "trivy", "package": "golang.org/x/crypto", "platform": "linux/arm64",
		"reason": "The upstream fix is pending verification.", "owner": "security-team",
		"created_at": "2026-07-22T12:00:00Z", "expires_at": "2026-07-24T12:00:00Z",
	}
}

func overrides(source map[string]any, pairs ...any) map[string]any {
	result := make(map[string]any, len(source)+len(pairs)/2)
	for key, value := range source {
		result[key] = value
	}
	for index := 0; index < len(pairs); index += 2 {
		result[pairs[index].(string)] = pairs[index+1]
	}
	return result
}

func without(source map[string]any, key string) map[string]any {
	result := overrides(source)
	delete(result, key)
	return result
}

func registryWith(exception map[string]any) string {
	data, err := json.Marshal(map[string]any{"version": 1, "exceptions": []any{exception}})
	if err != nil {
		panic(err)
	}
	return string(data)
}

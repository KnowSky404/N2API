package config

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestValidateReportsValidConfigurationAndPostgresPasswordMatch(t *testing.T) {
	report := Validate(strictConfigLookup(map[string]string{
		"DATABASE_URL":      "postgres://owner:database%40secret@203.0.113.1:5432/n2api?sslmode=verify-full&connect_timeout=30",
		"POSTGRES_PASSWORD": "database@secret",
	}))

	if report.SchemaVersion != ConfigValidationSchemaVersion ||
		report.Status != ConfigValidationStatusValid ||
		report.ReasonCode != ConfigValidationReasonValid ||
		report.PostgresPasswordCheck != PostgresPasswordCheckMatched {
		t.Fatalf("report = %+v", report)
	}
}

func TestValidateDoesNotConnectToUnreachableDatabase(t *testing.T) {
	started := time.Now()
	report := Validate(strictConfigLookup(map[string]string{
		"DATABASE_URL":      "postgres://owner:database-secret@203.0.113.1:5432/n2api?sslmode=verify-full&connect_timeout=30",
		"POSTGRES_PASSWORD": "database-secret",
	}))
	if elapsed := time.Since(started); elapsed >= time.Second {
		t.Fatalf("Validate took %s, want offline parsing only", elapsed)
	}
	if report.Status != ConfigValidationStatusValid {
		t.Fatalf("report = %+v", report)
	}
}

func TestValidateReportsPostgresPasswordMismatchWithoutLeaks(t *testing.T) {
	values := map[string]string{
		"DATABASE_URL":      "postgres://owner:database-password-canary@db.internal/n2api?sslmode=verify-full",
		"POSTGRES_PASSWORD": "postgres-password-canary",
	}
	report := Validate(strictConfigLookup(values))
	if report.Status != ConfigValidationStatusInvalid ||
		report.ReasonCode != ConfigValidationReasonPostgresPasswordMismatch ||
		report.PostgresPasswordCheck != PostgresPasswordCheckMismatch {
		t.Fatalf("report = %+v", report)
	}
	assertConfigValidationReportRedacted(t, report, values["DATABASE_URL"], "database-password-canary", values["POSTGRES_PASSWORD"])
}

func TestValidateTreatsUnavailablePostgresPasswordAsNotApplicable(t *testing.T) {
	report := Validate(strictConfigLookup(nil))
	if report.Status != ConfigValidationStatusValid ||
		report.ReasonCode != ConfigValidationReasonValid ||
		report.PostgresPasswordCheck != PostgresPasswordCheckNotApplicable {
		t.Fatalf("report = %+v", report)
	}
}

func TestValidateReadsPostgresPasswordFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "postgres-password")
	const password = "postgres-file-password-canary"
	if err := os.WriteFile(path, []byte(password+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	report := Validate(strictConfigLookup(map[string]string{
		"DATABASE_URL":           "postgres://owner:postgres-file-password-canary@db.internal/n2api?sslmode=verify-full",
		"POSTGRES_PASSWORD_FILE": path,
	}))
	if report.Status != ConfigValidationStatusValid ||
		report.ReasonCode != ConfigValidationReasonValid ||
		report.PostgresPasswordCheck != PostgresPasswordCheckMatched {
		t.Fatalf("report = %+v", report)
	}
	assertConfigValidationReportRedacted(t, report, path, password)
}

func TestValidateRejectsPostgresPasswordSourceConflictWithoutLeaks(t *testing.T) {
	const pathCanary = "/run/secrets/postgres-path-canary"
	const directCanary = "postgres-direct-canary"
	report := Validate(strictConfigLookup(map[string]string{
		"POSTGRES_PASSWORD":      directCanary,
		"POSTGRES_PASSWORD_FILE": pathCanary,
	}))
	if report.Status != ConfigValidationStatusInvalid ||
		report.ReasonCode != ConfigValidationReasonPostgresPasswordSource ||
		report.PostgresPasswordCheck != PostgresPasswordCheckSourceInvalid {
		t.Fatalf("report = %+v", report)
	}
	assertConfigValidationReportRedacted(t, report, pathCanary, directCanary)
}

func TestValidateRejectsUnsafePostgresPasswordFileWithoutLeaks(t *testing.T) {
	dir := t.TempDir()
	regular := filepath.Join(dir, "postgres-password-value-canary")
	if err := os.WriteFile(regular, []byte("postgres-password-canary"), 0o600); err != nil {
		t.Fatal(err)
	}
	symlink := filepath.Join(dir, "postgres-password-symlink-canary")
	if err := os.Symlink(regular, symlink); err != nil {
		t.Fatal(err)
	}
	report := Validate(strictConfigLookup(map[string]string{
		"POSTGRES_PASSWORD_FILE": symlink,
	}))
	if report.Status != ConfigValidationStatusInvalid ||
		report.ReasonCode != ConfigValidationReasonPostgresPasswordSource ||
		report.PostgresPasswordCheck != PostgresPasswordCheckSourceInvalid {
		t.Fatalf("report = %+v", report)
	}
	assertConfigValidationReportRedacted(t, report, regular, symlink, "postgres-password-canary")
}

func TestValidateRedactsLoadErrorsAndSecretFilePaths(t *testing.T) {
	const pathCanary = "/missing/secret-file-path-canary"
	report := Validate(strictConfigLookup(map[string]string{
		"N2API_ADMIN_PASSWORD":      "",
		"N2API_ADMIN_PASSWORD_FILE": pathCanary,
	}))
	if report.Status != ConfigValidationStatusInvalid ||
		report.ReasonCode != ConfigValidationReasonInvalid ||
		report.PostgresPasswordCheck != PostgresPasswordCheckNotEvaluated {
		t.Fatalf("report = %+v", report)
	}
	assertConfigValidationReportRedacted(t, report, pathCanary)
}

func assertConfigValidationReportRedacted(t *testing.T, report ConfigValidationReport, forbidden ...string) {
	t.Helper()
	encoded, err := json.Marshal(report)
	if err != nil {
		t.Fatal(err)
	}
	for _, value := range forbidden {
		if value != "" && strings.Contains(string(encoded), value) {
			t.Fatalf("report leaked configuration value %q: %s", value, encoded)
		}
	}
}

package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"strings"
	"testing"

	"github.com/KnowSky404/N2API/backend/internal/config"
	"github.com/KnowSky404/N2API/backend/internal/encryptioninventory"
)

type failingWriter struct{}

func (failingWriter) Write([]byte) (int, error) {
	return 0, errors.New("writer-canary")
}

func TestRunAdminCommandWritesJSONAndMapsVerificationStatus(t *testing.T) {
	for name, tt := range map[string]struct {
		report encryptioninventory.Report
		want   int
	}{
		"verified":  {report: encryptioninventory.Report{Status: encryptioninventory.StatusOK}, want: 0},
		"attention": {report: encryptioninventory.Report{Status: encryptioninventory.StatusAttention}, want: 0},
		"failed":    {report: encryptioninventory.Report{Status: encryptioninventory.StatusFailed}, want: 1},
	} {
		t.Run(name, func(t *testing.T) {
			var stdout bytes.Buffer
			var stderr bytes.Buffer
			code := runAdminCommand(context.Background(), []string{"admin", "verify-encryption"}, &stdout, &stderr, func(context.Context) (encryptioninventory.Report, error) {
				return tt.report, nil
			})
			if code != tt.want {
				t.Fatalf("exit code = %d, want %d", code, tt.want)
			}
			if !strings.HasSuffix(stdout.String(), "\n") || !strings.Contains(stdout.String(), `"status":"`+tt.report.Status+`"`) {
				t.Fatalf("stdout = %q, want one JSON document", stdout.String())
			}
			if stderr.Len() != 0 {
				t.Fatalf("stderr = %q, want empty", stderr.String())
			}
		})
	}
}

func TestRunAdminCommandRejectsInvalidArgumentsWithoutRunningVerifier(t *testing.T) {
	for _, args := range [][]string{
		nil,
		{"admin"},
		{"verify-encryption"},
		{"admin", "verify-encryption", "extra"},
	} {
		var stdout bytes.Buffer
		var stderr bytes.Buffer
		called := false
		code := runAdminCommand(context.Background(), args, &stdout, &stderr, func(context.Context) (encryptioninventory.Report, error) {
			called = true
			return encryptioninventory.Report{}, nil
		})
		if code != 2 || called || stdout.Len() != 0 || stderr.String() != "usage: n2api admin verify-encryption\n" {
			t.Fatalf("args %q: code=%d called=%v stdout=%q stderr=%q", args, code, called, stdout.String(), stderr.String())
		}
	}
}

func TestRunAdminCommandRedactsOperationalErrors(t *testing.T) {
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	code := runAdminCommand(context.Background(), []string{"admin", "verify-encryption"}, &stdout, &stderr, func(context.Context) (encryptioninventory.Report, error) {
		return encryptioninventory.Report{}, errors.New("database-password-canary")
	})
	if code != 2 || stdout.Len() != 0 || stderr.String() != "verify-encryption failed\n" {
		t.Fatalf("code=%d stdout=%q stderr=%q", code, stdout.String(), stderr.String())
	}
	if strings.Contains(stderr.String(), "database-password-canary") {
		t.Fatal("stderr leaked operational error")
	}
}

func TestRunAdminCommandMapsOutputFailureWithoutLeakingWriterError(t *testing.T) {
	var stderr bytes.Buffer
	code := runAdminCommand(context.Background(), []string{"admin", "verify-encryption"}, failingWriter{}, &stderr, func(context.Context) (encryptioninventory.Report, error) {
		return encryptioninventory.Report{Status: encryptioninventory.StatusOK}, nil
	})
	if code != 2 || stderr.String() != "write verify-encryption report failed\n" {
		t.Fatalf("code=%d stderr=%q", code, stderr.String())
	}
	if strings.Contains(stderr.String(), "writer-canary") {
		t.Fatal("stderr leaked writer error")
	}
}

func TestRunValidateConfigCommandWritesOneJSONDocumentAndMapsStatus(t *testing.T) {
	for _, test := range []struct {
		name   string
		report config.ConfigValidationReport
		want   int
	}{
		{
			name: "valid",
			report: config.ConfigValidationReport{
				SchemaVersion:         config.ConfigValidationSchemaVersion,
				Status:                config.ConfigValidationStatusValid,
				ReasonCode:            config.ConfigValidationReasonValid,
				PostgresPasswordCheck: config.PostgresPasswordCheckMatched,
			},
			want: 0,
		},
		{
			name: "invalid",
			report: config.ConfigValidationReport{
				SchemaVersion:         config.ConfigValidationSchemaVersion,
				Status:                config.ConfigValidationStatusInvalid,
				ReasonCode:            config.ConfigValidationReasonInvalid,
				PostgresPasswordCheck: config.PostgresPasswordCheckNotEvaluated,
			},
			want: 1,
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			var stdout, stderr bytes.Buffer
			code := runAdminCommandWithOperations(context.Background(), []string{"admin", "validate-config"}, &stdout, &stderr, nil, nil, nil, func() config.ConfigValidationReport {
				return test.report
			})
			if code != test.want || stderr.Len() != 0 {
				t.Fatalf("code=%d stderr=%q", code, stderr.String())
			}
			decoder := json.NewDecoder(&stdout)
			var got config.ConfigValidationReport
			if err := decoder.Decode(&got); err != nil {
				t.Fatalf("decode report: %v", err)
			}
			if got != test.report {
				t.Fatalf("report=%+v want=%+v", got, test.report)
			}
			var extra any
			if err := decoder.Decode(&extra); !errors.Is(err, io.EOF) {
				t.Fatalf("stdout does not contain exactly one JSON document: %q (%v)", stdout.String(), err)
			}
		})
	}
}

func TestRunValidateConfigCommandRejectsArgumentsWithoutRunningValidator(t *testing.T) {
	var stdout, stderr bytes.Buffer
	called := false
	code := runAdminCommandWithOperations(context.Background(), []string{"admin", "validate-config", "extra"}, &stdout, &stderr, nil, nil, nil, func() config.ConfigValidationReport {
		called = true
		return config.ConfigValidationReport{}
	})
	if code != 2 || called || stdout.Len() != 0 || stderr.String() != "usage: n2api admin validate-config\n" {
		t.Fatalf("code=%d called=%v stdout=%q stderr=%q", code, called, stdout.String(), stderr.String())
	}
}

func TestRunValidateConfigCommandMapsOutputFailureWithoutLeaks(t *testing.T) {
	var stderr bytes.Buffer
	code := runAdminCommandWithOperations(context.Background(), []string{"admin", "validate-config"}, failingWriter{}, &stderr, nil, nil, nil, func() config.ConfigValidationReport {
		return config.ConfigValidationReport{
			SchemaVersion: config.ConfigValidationSchemaVersion,
			Status:        config.ConfigValidationStatusValid,
			ReasonCode:    config.ConfigValidationReasonValid,
		}
	})
	if code != 2 || stderr.String() != "write validate-config report failed\n" || strings.Contains(stderr.String(), "writer-canary") {
		t.Fatalf("code=%d stderr=%q", code, stderr.String())
	}
}

func TestValidateConfigCommandRedactsConfigurationCanaries(t *testing.T) {
	values := map[string]string{
		"N2API_HOST":              "127.0.0.1",
		"N2API_PUBLIC_URL":        "https://n2api.knowsky.uk",
		"DATABASE_URL":            "postgres://owner:database-password-canary@db.internal/n2api?sslmode=verify-full",
		"POSTGRES_PASSWORD":       "postgres-password-canary",
		"N2API_ADMIN_PASSWORD":    "admin-password-canary",
		"N2API_ENCRYPTION_SECRET": "encryption-secret-canary-at-least-32-bytes",
	}
	lookup := func(name string) string { return values[name] }
	var stdout, stderr bytes.Buffer
	code := runAdminCommandWithOperations(context.Background(), []string{"admin", "validate-config"}, &stdout, &stderr, nil, nil, nil, newValidateConfigFunc(lookup))
	if code != 1 || stderr.Len() != 0 {
		t.Fatalf("code=%d stdout=%q stderr=%q", code, stdout.String(), stderr.String())
	}
	for _, canary := range []string{
		values["DATABASE_URL"], values["POSTGRES_PASSWORD"], values["N2API_ADMIN_PASSWORD"], values["N2API_ENCRYPTION_SECRET"],
	} {
		if strings.Contains(stdout.String(), canary) {
			t.Fatalf("stdout leaked configuration canary: %q", stdout.String())
		}
	}
	if !strings.Contains(stdout.String(), `"reason_code":"postgres_password_mismatch"`) {
		t.Fatalf("stdout=%q", stdout.String())
	}
}

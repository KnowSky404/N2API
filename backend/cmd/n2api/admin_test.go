package main

import (
	"bytes"
	"context"
	"errors"
	"strings"
	"testing"

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

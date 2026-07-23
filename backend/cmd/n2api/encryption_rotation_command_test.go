package main

import (
	"bytes"
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/KnowSky404/N2API/backend/internal/encryptionrotation"
)

func TestRunEncryptionRotationGateCommandParsesConfirmationAndWritesJSON(t *testing.T) {
	var got encryptionrotation.Options
	var stdout, stderr bytes.Buffer
	code := runAdminCommandWithOperations(context.Background(), []string{
		"admin", "check-encryption-rotation",
		"--backup-id=restore-record-20260723-01",
		"--backup-created-at=2026-07-23T13:00:00+02:00",
		"--backup-restored-at=2026-07-23T12:00:00Z",
	}, &stdout, &stderr, nil, nil, func(_ context.Context, options encryptionrotation.Options) (encryptionrotation.Result, error) {
		got = options
		return encryptionrotation.Result{Status: encryptionrotation.StatusReady, DryRun: true}, nil
	})
	if code != 0 || got.BackupIdentifier != "restore-record-20260723-01" || got.BackupCreatedAt.Location() != time.UTC || got.BackupRestoredAt.Location() != time.UTC {
		t.Fatalf("code=%d options=%+v", code, got)
	}
	if stderr.Len() != 0 || !strings.Contains(stdout.String(), `"status":"ready"`) || !strings.Contains(stdout.String(), `"dryRun":true`) {
		t.Fatalf("stdout=%q stderr=%q", stdout.String(), stderr.String())
	}
}

func TestRunEncryptionRotationGateCommandMapsBlockedAndContended(t *testing.T) {
	for _, status := range []string{encryptionrotation.StatusBlocked, encryptionrotation.StatusContended} {
		var stdout, stderr bytes.Buffer
		code := runAdminCommandWithOperations(context.Background(), validRotationGateArgs(), &stdout, &stderr, nil, nil, func(context.Context, encryptionrotation.Options) (encryptionrotation.Result, error) {
			return encryptionrotation.Result{Status: status, DryRun: true}, nil
		})
		if code != 1 || stderr.Len() != 0 || !strings.Contains(stdout.String(), `"status":"`+status+`"`) {
			t.Fatalf("status=%s code=%d stdout=%q stderr=%q", status, code, stdout.String(), stderr.String())
		}
	}
}

func TestRunEncryptionRotationGateCommandRejectsInvalidArguments(t *testing.T) {
	for _, args := range [][]string{
		{"admin", "check-encryption-rotation"},
		{"admin", "check-encryption-rotation", "--backup-id=record", "--backup-created-at=invalid", "--backup-restored-at=2026-07-23T12:00:00Z"},
		{"admin", "check-encryption-rotation", "--backup-id=record", "--backup-created-at=2026-07-23T11:00:00Z", "--backup-restored-at=2026-07-23T12:00:00Z", "extra"},
	} {
		var stdout, stderr bytes.Buffer
		called := false
		code := runAdminCommandWithOperations(context.Background(), args, &stdout, &stderr, nil, nil, func(context.Context, encryptionrotation.Options) (encryptionrotation.Result, error) {
			called = true
			return encryptionrotation.Result{}, nil
		})
		if code != 2 || called || stdout.Len() != 0 || !strings.HasPrefix(stderr.String(), "usage: n2api admin check-encryption-rotation") {
			t.Fatalf("args=%q code=%d called=%v stdout=%q stderr=%q", args, code, called, stdout.String(), stderr.String())
		}
	}
}

func TestRunEncryptionRotationGateCommandLogsOnlyStableFailureCode(t *testing.T) {
	const canary = "postgres://user:password-canary@database/internal"
	var stdout, stderr bytes.Buffer
	code := runAdminCommandWithOperations(context.Background(), validRotationGateArgs(), &stdout, &stderr, nil, nil, func(context.Context, encryptionrotation.Options) (encryptionrotation.Result, error) {
		return encryptionrotation.Result{}, errors.New(canary)
	})
	if code != 2 || stdout.Len() != 0 || !strings.Contains(stderr.String(), `"error_code":"encryption_rotation_gate_failed"`) {
		t.Fatalf("code=%d stdout=%q stderr=%q", code, stdout.String(), stderr.String())
	}
	if strings.Contains(stderr.String(), canary) || strings.Contains(stderr.String(), "password-canary") {
		t.Fatalf("stderr leaked failure: %s", stderr.String())
	}
}

func TestRunEncryptionRotationGateCommandMapsOutputFailure(t *testing.T) {
	var stderr bytes.Buffer
	code := runAdminCommandWithOperations(context.Background(), validRotationGateArgs(), failingWriter{}, &stderr, nil, nil, func(context.Context, encryptionrotation.Options) (encryptionrotation.Result, error) {
		return encryptionrotation.Result{Status: encryptionrotation.StatusReady, DryRun: true}, nil
	})
	if code != 2 || !strings.Contains(stderr.String(), `"error_code":"encryption_rotation_gate_output_failed"`) {
		t.Fatalf("code=%d stderr=%q", code, stderr.String())
	}
	if strings.Contains(stderr.String(), "writer-canary") {
		t.Fatalf("stderr leaked writer failure: %s", stderr.String())
	}
}

func validRotationGateArgs() []string {
	return []string{
		"admin", "check-encryption-rotation",
		"--backup-id=restore-record-20260723-01",
		"--backup-created-at=2026-07-23T11:00:00Z",
		"--backup-restored-at=2026-07-23T12:00:00Z",
	}
}

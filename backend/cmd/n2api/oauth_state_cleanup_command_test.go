package main

import (
	"bytes"
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/KnowSky404/N2API/backend/internal/oauthstatecleanup"
)

func TestRunCleanupOAuthStatesCommandDefaultsToDryRun(t *testing.T) {
	cutoff := "2026-07-23T12:30:45Z"
	var got oauthstatecleanup.Options
	var stdout, stderr bytes.Buffer
	code := runAdminCommandWithCleanup(context.Background(), []string{
		"admin", "cleanup-expired-oauth-states", "--cutoff", cutoff, "--batch-size", "250",
	}, &stdout, &stderr, nil, func(_ context.Context, options oauthstatecleanup.Options) (oauthstatecleanup.Result, error) {
		got = options
		return oauthstatecleanup.Result{Status: oauthstatecleanup.StatusDryRun, DryRun: true, Cutoff: options.Cutoff, BatchSize: options.BatchSize, EligibleCount: 3}, nil
	})
	if code != 0 || !got.DryRun || got.BatchSize != 250 || got.Cutoff.Format(time.RFC3339) != cutoff {
		t.Fatalf("code=%d options=%+v", code, got)
	}
	if stderr.Len() != 0 || !strings.Contains(stdout.String(), `"eligibleCount":3`) || !strings.Contains(stdout.String(), `"status":"dry_run"`) {
		t.Fatalf("stdout=%q stderr=%q", stdout.String(), stderr.String())
	}
}

func TestRunCleanupOAuthStatesCommandRequiresExplicitExecute(t *testing.T) {
	var got oauthstatecleanup.Options
	code := runAdminCommandWithCleanup(context.Background(), []string{
		"admin", "cleanup-expired-oauth-states", "--cutoff=2026-07-23T12:30:45+02:00", "--execute",
	}, ioDiscardBuffer{}, ioDiscardBuffer{}, nil, func(_ context.Context, options oauthstatecleanup.Options) (oauthstatecleanup.Result, error) {
		got = options
		return oauthstatecleanup.Result{Status: oauthstatecleanup.StatusCompleted}, nil
	})
	if code != 0 || got.DryRun || got.Cutoff.Location() != time.UTC {
		t.Fatalf("code=%d options=%+v", code, got)
	}
}

func TestRunCleanupOAuthStatesCommandMapsContention(t *testing.T) {
	var stdout, stderr bytes.Buffer
	code := runAdminCommandWithCleanup(context.Background(), []string{
		"admin", "cleanup-expired-oauth-states", "--cutoff=2026-07-23T12:30:45Z",
	}, &stdout, &stderr, nil, func(context.Context, oauthstatecleanup.Options) (oauthstatecleanup.Result, error) {
		return oauthstatecleanup.Result{Status: oauthstatecleanup.StatusContended}, nil
	})
	if code != 1 || stderr.Len() != 0 || !strings.Contains(stdout.String(), `"status":"contended"`) {
		t.Fatalf("code=%d stdout=%q stderr=%q", code, stdout.String(), stderr.String())
	}
}

func TestRunCleanupOAuthStatesCommandRejectsUnsafeArguments(t *testing.T) {
	for _, args := range [][]string{
		{"admin", "cleanup-expired-oauth-states"},
		{"admin", "cleanup-expired-oauth-states", "--cutoff=not-a-time"},
		{"admin", "cleanup-expired-oauth-states", "--cutoff=2026-07-23T12:30:45Z", "--batch-size=0"},
		{"admin", "cleanup-expired-oauth-states", "--cutoff=2026-07-23T12:30:45Z", "extra"},
	} {
		var stdout, stderr bytes.Buffer
		called := false
		code := runAdminCommandWithCleanup(context.Background(), args, &stdout, &stderr, nil, func(context.Context, oauthstatecleanup.Options) (oauthstatecleanup.Result, error) {
			called = true
			return oauthstatecleanup.Result{}, nil
		})
		if code != 2 || called || stdout.Len() != 0 || !strings.HasPrefix(stderr.String(), "usage: n2api admin cleanup-expired-oauth-states") {
			t.Fatalf("args=%q code=%d called=%v stdout=%q stderr=%q", args, code, called, stdout.String(), stderr.String())
		}
	}
}

func TestRunCleanupOAuthStatesCommandLogsOnlyStableFailureCode(t *testing.T) {
	const canary = "postgres://user:password-canary@database/internal"
	var stdout, stderr bytes.Buffer
	code := runAdminCommandWithCleanup(context.Background(), []string{
		"admin", "cleanup-expired-oauth-states", "--cutoff=2026-07-23T12:30:45Z", "--execute",
	}, &stdout, &stderr, nil, func(context.Context, oauthstatecleanup.Options) (oauthstatecleanup.Result, error) {
		return oauthstatecleanup.Result{}, errors.New(canary)
	})
	if code != 2 || stdout.Len() != 0 || !strings.Contains(stderr.String(), `"error_code":"oauth_state_cleanup_failed"`) {
		t.Fatalf("code=%d stdout=%q stderr=%q", code, stdout.String(), stderr.String())
	}
	if strings.Contains(stderr.String(), canary) || strings.Contains(stderr.String(), "password-canary") {
		t.Fatalf("failure log leaked error: %s", stderr.String())
	}
}

type ioDiscardBuffer struct{}

func (ioDiscardBuffer) Write(value []byte) (int, error) { return len(value), nil }

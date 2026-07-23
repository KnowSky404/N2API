package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"log/slog"
	"time"

	"github.com/KnowSky404/N2API/backend/internal/config"
	"github.com/KnowSky404/N2API/backend/internal/encryptioninventory"
	"github.com/KnowSky404/N2API/backend/internal/oauthstatecleanup"
	"github.com/KnowSky404/N2API/backend/internal/store"
)

type verifyEncryptionFunc func(context.Context) (encryptioninventory.Report, error)
type cleanupOAuthStatesFunc func(context.Context, oauthstatecleanup.Options) (oauthstatecleanup.Result, error)

func runAdminCommand(ctx context.Context, args []string, stdout, stderr io.Writer, verify verifyEncryptionFunc) int {
	return runAdminCommandWithCleanup(ctx, args, stdout, stderr, verify, nil)
}

func runAdminCommandWithCleanup(ctx context.Context, args []string, stdout, stderr io.Writer, verify verifyEncryptionFunc, cleanup cleanupOAuthStatesFunc) int {
	if len(args) >= 2 && args[0] == "admin" && args[1] == "cleanup-expired-oauth-states" {
		return runCleanupOAuthStatesCommand(ctx, args[2:], stdout, stderr, cleanup)
	}
	if len(args) != 2 || args[0] != "admin" || args[1] != "verify-encryption" || verify == nil {
		fmt.Fprintln(stderr, "usage: n2api admin verify-encryption")
		return 2
	}
	report, err := verify(ctx)
	if err != nil {
		fmt.Fprintln(stderr, "verify-encryption failed")
		return 2
	}
	if err := json.NewEncoder(stdout).Encode(report); err != nil {
		fmt.Fprintln(stderr, "write verify-encryption report failed")
		return 2
	}
	if report.Status == encryptioninventory.StatusFailed {
		return 1
	}
	return 0
}

func runCleanupOAuthStatesCommand(ctx context.Context, args []string, stdout, stderr io.Writer, cleanup cleanupOAuthStatesFunc) int {
	usage := func() int {
		fmt.Fprintln(stderr, "usage: n2api admin cleanup-expired-oauth-states --cutoff RFC3339 [--batch-size 1000] [--execute]")
		return 2
	}
	if cleanup == nil {
		return usage()
	}
	flags := flag.NewFlagSet("cleanup-expired-oauth-states", flag.ContinueOnError)
	flags.SetOutput(io.Discard)
	cutoffValue := flags.String("cutoff", "", "exclusive UTC cleanup cutoff")
	batchSize := flags.Int("batch-size", 1000, "maximum rows per transaction")
	execute := flags.Bool("execute", false, "delete eligible rows instead of dry-run")
	if err := flags.Parse(args); err != nil || flags.NArg() != 0 || *cutoffValue == "" || *batchSize < 1 || *batchSize > oauthstatecleanup.MaxBatchSize {
		return usage()
	}
	cutoff, err := time.Parse(time.RFC3339Nano, *cutoffValue)
	if err != nil {
		return usage()
	}
	result, err := cleanup(ctx, oauthstatecleanup.Options{Cutoff: cutoff.UTC(), BatchSize: *batchSize, DryRun: !*execute})
	if err != nil {
		errorCode := "oauth_state_cleanup_failed"
		if errors.Is(err, context.Canceled) || errors.Is(ctx.Err(), context.Canceled) {
			errorCode = "oauth_state_cleanup_canceled"
		}
		slog.New(slog.NewJSONHandler(stderr, nil)).Error("OAuth state cleanup failed", "error_code", errorCode)
		return 2
	}
	if err := json.NewEncoder(stdout).Encode(result); err != nil {
		slog.New(slog.NewJSONHandler(stderr, nil)).Error("OAuth state cleanup output failed", "error_code", "oauth_state_cleanup_output_failed")
		return 2
	}
	if result.Status == oauthstatecleanup.StatusContended {
		return 1
	}
	return 0
}

func newVerifyEncryptionFunc(getenv func(string) string) verifyEncryptionFunc {
	return func(ctx context.Context) (encryptioninventory.Report, error) {
		cfg, err := config.Load(getenv)
		if err != nil {
			return encryptioninventory.Report{}, fmt.Errorf("invalid configuration")
		}
		pool, err := store.OpenPool(ctx, cfg.DatabaseURL)
		if err != nil {
			return encryptioninventory.Report{}, fmt.Errorf("database unavailable")
		}
		defer pool.Close()

		repo := store.NewEncryptionInventoryRepository(pool)
		report, err := encryptioninventory.Verify(ctx, repo, cfg.EncryptionKeyring)
		if err != nil {
			return encryptioninventory.Report{}, fmt.Errorf("encrypted credential inventory failed")
		}
		return report, nil
	}
}

func newCleanupOAuthStatesFunc(getenv func(string) string) cleanupOAuthStatesFunc {
	return func(ctx context.Context, options oauthstatecleanup.Options) (oauthstatecleanup.Result, error) {
		cfg, err := config.Load(getenv)
		if err != nil {
			return oauthstatecleanup.Result{}, fmt.Errorf("invalid configuration")
		}
		pool, err := store.OpenPool(ctx, cfg.DatabaseURL)
		if err != nil {
			return oauthstatecleanup.Result{}, fmt.Errorf("database unavailable")
		}
		defer pool.Close()
		repo := store.NewOAuthStateCleanupRepository(pool)
		events := store.NewSystemEventRepository(pool, cfg.EncryptionSecret)
		return oauthstatecleanup.Run(ctx, repo, events, options, time.Now)
	}
}

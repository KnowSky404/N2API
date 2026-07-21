package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io"

	"github.com/KnowSky404/N2API/backend/internal/config"
	"github.com/KnowSky404/N2API/backend/internal/encryptioninventory"
	"github.com/KnowSky404/N2API/backend/internal/store"
)

type verifyEncryptionFunc func(context.Context) (encryptioninventory.Report, error)

func runAdminCommand(ctx context.Context, args []string, stdout, stderr io.Writer, verify verifyEncryptionFunc) int {
	if len(args) != 2 || args[0] != "admin" || args[1] != "verify-encryption" {
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
	if report.Status != encryptioninventory.StatusOK {
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

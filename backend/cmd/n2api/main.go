package main

import (
	"context"
	"log/slog"
	"net/http"
	"os"
	"time"

	"github.com/KnowSky404/N2API/backend/internal/admin"
	"github.com/KnowSky404/N2API/backend/internal/config"
	"github.com/KnowSky404/N2API/backend/internal/httpapi"
	"github.com/KnowSky404/N2API/backend/internal/store"
)

func main() {
	cfg, err := config.Load(os.Getenv)
	if err != nil {
		slog.Error("invalid configuration", "error", err)
		os.Exit(1)
	}

	ctx := context.Background()
	pool, err := store.OpenPool(ctx, cfg.DatabaseURL)
	if err != nil {
		slog.Error("database unavailable", "error", err)
		os.Exit(1)
	}
	defer pool.Close()

	if err := store.RunMigrations(ctx, pool); err != nil {
		slog.Error("database migration failed", "error", err)
		os.Exit(1)
	}

	adminRepo := store.NewAdminRepository(pool)
	adminService := admin.NewService(adminRepo, admin.Config{SessionTTL: 7 * 24 * time.Hour})
	if err := adminService.BootstrapAdmin(ctx, cfg.AdminUsername, cfg.AdminPassword); err != nil {
		slog.Error("admin bootstrap failed", "error", err)
		os.Exit(1)
	}

	server := &http.Server{
		Addr:              cfg.Addr(),
		Handler:           httpapi.NewServer(cfg, pool, adminService),
		ReadHeaderTimeout: 5 * time.Second,
	}

	slog.Info("starting n2api", "addr", cfg.Addr())
	if err := server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
		slog.Error("server stopped", "error", err)
		os.Exit(1)
	}
}

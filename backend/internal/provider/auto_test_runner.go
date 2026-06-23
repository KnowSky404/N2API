package provider

import (
	"context"
	"log/slog"
	"sync/atomic"
	"time"
)

type autoTestService interface {
	TestAccounts(ctx context.Context) ([]Account, error)
}

type AutoTestRunnerConfig struct {
	Enabled  bool
	Interval time.Duration
}

type AutoTestRunner struct {
	service autoTestService
	cfg     AutoTestRunnerConfig
	logger  *slog.Logger
	running atomic.Bool
}

func NewAutoTestRunner(service autoTestService, cfg AutoTestRunnerConfig, logger *slog.Logger) *AutoTestRunner {
	if logger == nil {
		logger = slog.Default()
	}
	return &AutoTestRunner{
		service: service,
		cfg:     cfg,
		logger:  logger,
	}
}

func (r *AutoTestRunner) Run(ctx context.Context) {
	if r == nil || r.service == nil || !r.cfg.Enabled {
		return
	}
	interval := r.cfg.Interval
	if interval <= 0 {
		interval = 5 * time.Minute
	}

	r.runCycle(ctx)

	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			r.runCycle(ctx)
		}
	}
}

func (r *AutoTestRunner) runCycle(ctx context.Context) {
	if !r.running.CompareAndSwap(false, true) {
		r.logger.Debug("provider account auto test skipped because previous cycle is still running")
		return
	}
	defer r.running.Store(false)

	started := time.Now()
	accounts, err := r.service.TestAccounts(ctx)
	if err != nil {
		if ctx.Err() != nil {
			return
		}
		r.logger.Warn("provider account auto test failed", "error", err, "duration", time.Since(started))
		return
	}
	r.logger.Info("provider account auto test completed", "accounts", len(accounts), "duration", time.Since(started))
}

package admin

import (
	"context"
	"log/slog"
	"sync"
	"sync/atomic"
	"time"
)

const (
	defaultAPIKeyBudgetMaintenanceInterval   = 30 * time.Second
	defaultAPIKeyBudgetMaintenanceBatchSize  = 1000
	defaultAPIKeyBudgetMaintenanceMaxBatches = 10
	defaultAPIKeyBudgetAbandonAfter          = 24 * time.Hour
)

type APIKeyBudgetMaintenanceConfig struct {
	Interval     time.Duration
	BatchSize    int
	MaxBatches   int
	AbandonAfter time.Duration
}

type APIKeyBudgetMaintenanceStatus struct {
	Running               bool       `json:"running"`
	LastStartedAt         *time.Time `json:"lastStartedAt,omitempty"`
	LastSucceededAt       *time.Time `json:"lastSucceededAt,omitempty"`
	LastErrorAt           *time.Time `json:"lastErrorAt,omitempty"`
	LastErrorCode         string     `json:"lastErrorCode"`
	LastInitializedLogs   int        `json:"lastInitializedLogs"`
	LastSettledRows       int        `json:"lastSettledRows"`
	LastExpiredRows       int        `json:"lastExpiredRows"`
	LastAbandonedRows     int        `json:"lastAbandonedRows"`
	InitializationPending bool       `json:"initializationPending"`
}

type APIKeyBudgetMaintenance struct {
	store    APIKeyBudgetLedgerStore
	cfg      APIKeyBudgetMaintenanceConfig
	logger   *slog.Logger
	running  atomic.Bool
	statusMu sync.Mutex
	status   APIKeyBudgetMaintenanceStatus
	now      func() time.Time
	metrics  BackgroundTaskObserver
}

func NewAPIKeyBudgetMaintenance(store APIKeyBudgetLedgerStore, cfg APIKeyBudgetMaintenanceConfig, logger *slog.Logger) *APIKeyBudgetMaintenance {
	if cfg.Interval <= 0 {
		cfg.Interval = defaultAPIKeyBudgetMaintenanceInterval
	}
	if cfg.BatchSize <= 0 || cfg.BatchSize > defaultAPIKeyBudgetMaintenanceBatchSize {
		cfg.BatchSize = defaultAPIKeyBudgetMaintenanceBatchSize
	}
	if cfg.MaxBatches <= 0 || cfg.MaxBatches > defaultAPIKeyBudgetMaintenanceMaxBatches {
		cfg.MaxBatches = defaultAPIKeyBudgetMaintenanceMaxBatches
	}
	if cfg.AbandonAfter <= 0 {
		cfg.AbandonAfter = defaultAPIKeyBudgetAbandonAfter
	}
	if logger == nil {
		logger = slog.Default()
	}
	return &APIKeyBudgetMaintenance{store: store, cfg: cfg, logger: logger, now: time.Now}
}

func (m *APIKeyBudgetMaintenance) SetMetricsObserver(observer BackgroundTaskObserver) {
	if m != nil {
		m.metrics = observer
	}
}

func (m *APIKeyBudgetMaintenance) APIKeyBudgetMaintenanceStatus() APIKeyBudgetMaintenanceStatus {
	if m == nil {
		return APIKeyBudgetMaintenanceStatus{}
	}
	m.statusMu.Lock()
	defer m.statusMu.Unlock()
	return cloneAPIKeyBudgetMaintenanceStatus(m.status)
}

func (m *APIKeyBudgetMaintenance) Run(ctx context.Context) {
	if m == nil || m.store == nil {
		return
	}
	for {
		m.runCycle(ctx)
		timer := time.NewTimer(m.cfg.Interval)
		select {
		case <-ctx.Done():
			timer.Stop()
			return
		case <-timer.C:
		}
	}
}

func (m *APIKeyBudgetMaintenance) runCycle(ctx context.Context) {
	if !m.running.CompareAndSwap(false, true) {
		if m.metrics != nil {
			m.metrics.ObserveBackgroundTaskRun("api_key_budget_maintenance", "skipped", 0)
		}
		return
	}
	defer m.running.Store(false)
	outcome := "failure"
	finishMetrics := func(string) {}
	if m.metrics != nil {
		finishMetrics = m.metrics.BeginBackgroundTask("api_key_budget_maintenance")
	}
	defer func() { finishMetrics(outcome) }()

	started := m.now().UTC()
	m.statusMu.Lock()
	m.status.Running = true
	m.status.LastStartedAt = timePointer(started)
	m.statusMu.Unlock()

	initialized, pending, settled, expired, abandoned, err := m.runBoundedWork(ctx, started)
	finished := m.now().UTC()
	m.statusMu.Lock()
	m.status.Running = false
	m.status.LastInitializedLogs = initialized
	m.status.LastSettledRows = settled
	m.status.LastExpiredRows = expired
	m.status.LastAbandonedRows = abandoned
	m.status.InitializationPending = pending
	if err == nil {
		outcome = "success"
		m.status.LastSucceededAt = timePointer(finished)
		m.status.LastErrorAt = nil
		m.status.LastErrorCode = ""
		m.statusMu.Unlock()
		return
	}
	m.status.LastErrorAt = timePointer(finished)
	m.status.LastErrorCode = "api_key_budget_maintenance_failed"
	if ctx.Err() != nil {
		outcome = "canceled"
		m.status.LastErrorCode = "api_key_budget_maintenance_canceled"
	} else if initialized > 0 || settled > 0 || expired > 0 || abandoned > 0 {
		outcome = "partial"
	}
	m.statusMu.Unlock()
	if ctx.Err() == nil {
		m.logger.Warn("API key budget maintenance cycle failed", "error_code", "api_key_budget_maintenance_failed")
	}
}

func (m *APIKeyBudgetMaintenance) runBoundedWork(ctx context.Context, now time.Time) (initialized int, pending bool, settled int, expired int, abandoned int, err error) {
	for range m.cfg.MaxBatches {
		result, runErr := m.store.RunAPIKeyBudgetInitializationCycle(ctx, now, m.cfg.BatchSize)
		initialized += result.Processed
		pending = result.HasPending
		if runErr != nil {
			return initialized, pending, settled, expired, abandoned, runErr
		}
		if !result.HasPending {
			break
		}
	}
	for range m.cfg.MaxBatches {
		result, runErr := m.store.RunAPIKeyBudgetSettlementCycle(ctx, now, m.cfg.BatchSize)
		settled += result.Processed
		if runErr != nil {
			return initialized, pending, settled, expired, abandoned, runErr
		}
		if result.Processed < m.cfg.BatchSize {
			break
		}
	}
	for range m.cfg.MaxBatches {
		result, runErr := m.store.RunAPIKeyBudgetExpiryCycle(ctx, now, m.cfg.BatchSize)
		expired += result.Processed
		if runErr != nil {
			return initialized, pending, settled, expired, abandoned, runErr
		}
		if result.Processed < m.cfg.BatchSize {
			break
		}
	}
	cutoff := now.Add(-m.cfg.AbandonAfter)
	for range m.cfg.MaxBatches {
		result, runErr := m.store.RunAPIKeyBudgetAbandonedCycle(ctx, cutoff, now, m.cfg.BatchSize)
		abandoned += result.Processed
		if runErr != nil {
			return initialized, pending, settled, expired, abandoned, runErr
		}
		if result.Processed < m.cfg.BatchSize {
			break
		}
	}
	return initialized, pending, settled, expired, abandoned, nil
}

func cloneAPIKeyBudgetMaintenanceStatus(status APIKeyBudgetMaintenanceStatus) APIKeyBudgetMaintenanceStatus {
	status.LastStartedAt = cloneTimePointer(status.LastStartedAt)
	status.LastSucceededAt = cloneTimePointer(status.LastSucceededAt)
	status.LastErrorAt = cloneTimePointer(status.LastErrorAt)
	return status
}

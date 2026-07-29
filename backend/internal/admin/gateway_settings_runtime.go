package admin

import (
	"context"
	"errors"
	"log/slog"
	"math/rand/v2"
	"sync"
	"sync/atomic"
	"time"
)

const (
	GatewaySettingsSourcePersisted      = "persisted"
	GatewaySettingsSourceStartupDefault = "startup-default"
	GatewaySettingsSourceLastKnownGood  = "last-known-good"

	GatewaySettingsLoadFailedErrorCode = "gateway_settings_load_failed"
	GatewaySettingsInvalidErrorCode    = "gateway_settings_invalid"
	GatewaySettingsMissingErrorCode    = "gateway_settings_missing"
	GatewaySettingsVersionErrorCode    = "gateway_settings_version_conflict"

	defaultGatewaySettingsRefreshInterval = time.Minute
)

var (
	ErrGatewaySettingsUnavailable = errors.New("gateway settings unavailable")
	ErrGatewaySettingsInvalid     = errors.New("gateway settings invalid")
)

type GatewaySettingsRecord struct {
	Settings  GatewaySettings
	UpdatedAt time.Time
}

type GatewaySettingsLoader interface {
	LoadGatewaySettings(ctx context.Context) (GatewaySettingsRecord, error)
}

type GatewaySettingsRuntimeObserver interface {
	ObserveGatewaySettingsRefresh(outcome string)
	SetGatewaySettingsSnapshot(valid, stale bool, loadedAt time.Time)
}

type GatewaySettingsRuntimeConfig struct {
	RefreshInterval time.Duration
	Logger          *slog.Logger
	Observer        GatewaySettingsRuntimeObserver
	Now             func() time.Time
	Jitter          func(time.Duration) time.Duration
}

type GatewaySettingsSnapshot struct {
	Settings               GatewaySettings
	Version                *time.Time
	LoadedAt               *time.Time
	LastRefreshAttemptAt   *time.Time
	LastRefreshSucceededAt *time.Time
	LastRefreshFailedAt    *time.Time
	LastErrorCode          string
	ConsecutiveFailures    uint64
	RefreshSuccesses       uint64
	RefreshFailures        uint64
	Source                 string
	Valid                  bool
	Stale                  bool
	PersistedLoaded        bool
}

type GatewaySettingsRuntimeStatus struct {
	Version                *time.Time `json:"version,omitempty"`
	LoadedAt               *time.Time `json:"loadedAt,omitempty"`
	LastRefreshAttemptAt   *time.Time `json:"lastRefreshAttemptAt,omitempty"`
	LastRefreshSucceededAt *time.Time `json:"lastRefreshSucceededAt,omitempty"`
	LastRefreshFailedAt    *time.Time `json:"lastRefreshFailedAt,omitempty"`
	LastErrorCode          string     `json:"lastErrorCode"`
	ConsecutiveFailures    uint64     `json:"consecutiveFailures"`
	RefreshSuccesses       uint64     `json:"refreshSuccesses"`
	RefreshFailures        uint64     `json:"refreshFailures"`
	Source                 string     `json:"source"`
	Valid                  bool       `json:"valid"`
	Stale                  bool       `json:"stale"`
	PersistedLoaded        bool       `json:"persistedLoaded"`
}

type GatewaySettingsRuntime struct {
	loader          GatewaySettingsLoader
	defaults        GatewaySettings
	refreshInterval time.Duration
	logger          *slog.Logger
	observer        GatewaySettingsRuntimeObserver
	now             func() time.Time
	jitter          func(time.Duration) time.Duration

	refreshMu      sync.Mutex
	mu             sync.Mutex
	refreshTrigger chan struct{}
	snapshot       atomic.Pointer[GatewaySettingsSnapshot]
}

func NewGatewaySettingsRuntime(loader GatewaySettingsLoader, defaults GatewaySettings, cfg GatewaySettingsRuntimeConfig) (*GatewaySettingsRuntime, error) {
	if loader == nil {
		return nil, ErrGatewaySettingsUnavailable
	}
	normalized, err := normalizeGatewaySettings(defaults)
	if err != nil {
		return nil, ErrGatewaySettingsInvalid
	}
	if cfg.RefreshInterval <= 0 {
		cfg.RefreshInterval = defaultGatewaySettingsRefreshInterval
	}
	if cfg.Logger == nil {
		cfg.Logger = slog.Default()
	}
	if cfg.Now == nil {
		cfg.Now = time.Now
	}
	if cfg.Jitter == nil {
		cfg.Jitter = boundedGatewaySettingsJitter
	}
	return &GatewaySettingsRuntime{
		loader:          loader,
		defaults:        normalized,
		refreshInterval: cfg.RefreshInterval,
		logger:          cfg.Logger,
		observer:        cfg.Observer,
		now:             cfg.Now,
		jitter:          cfg.Jitter,
		refreshTrigger:  make(chan struct{}, 1),
	}, nil
}

func (r *GatewaySettingsRuntime) LoadInitial(ctx context.Context) error {
	if r == nil {
		return ErrGatewaySettingsUnavailable
	}
	return r.refresh(ctx, true)
}

func (r *GatewaySettingsRuntime) Refresh(ctx context.Context) error {
	if r == nil {
		return ErrGatewaySettingsUnavailable
	}
	return r.refresh(ctx, false)
}

func (r *GatewaySettingsRuntime) PublishPersisted(record GatewaySettingsRecord) error {
	if r == nil {
		return ErrGatewaySettingsUnavailable
	}
	settings, err := normalizeGatewaySettings(record.Settings)
	if err != nil || record.UpdatedAt.IsZero() {
		return ErrGatewaySettingsInvalid
	}
	record.Settings = settings
	record.UpdatedAt = record.UpdatedAt.UTC()

	r.mu.Lock()
	defer r.mu.Unlock()
	current := r.snapshot.Load()
	if current != nil && current.Version != nil && record.UpdatedAt.Before(*current.Version) {
		return nil
	}
	if current != nil && current.Version != nil && record.UpdatedAt.Equal(*current.Version) && record.Settings != current.Settings {
		r.publishVersionConflict(current)
		r.scheduleRefresh()
		return ErrGatewaySettingsInvalid
	}
	r.publishCommitted(current, record)
	return nil
}

func (r *GatewaySettingsRuntime) ScheduleRefresh() {
	if r != nil {
		r.scheduleRefresh()
	}
}

func (r *GatewaySettingsRuntime) GetGatewaySettings(context.Context) (GatewaySettings, error) {
	if r == nil {
		return GatewaySettings{}, ErrGatewaySettingsUnavailable
	}
	snapshot := r.snapshot.Load()
	if snapshot == nil || !snapshot.Valid {
		return GatewaySettings{}, ErrGatewaySettingsUnavailable
	}
	return snapshot.Settings, nil
}

func (r *GatewaySettingsRuntime) Snapshot() GatewaySettingsSnapshot {
	if r == nil {
		return GatewaySettingsSnapshot{}
	}
	snapshot := r.snapshot.Load()
	if snapshot == nil {
		return GatewaySettingsSnapshot{}
	}
	return cloneGatewaySettingsSnapshot(snapshot)
}

func (r *GatewaySettingsRuntime) GatewaySettingsRuntimeStatus() GatewaySettingsRuntimeStatus {
	snapshot := r.Snapshot()
	return GatewaySettingsRuntimeStatus{
		Version:                snapshot.Version,
		LoadedAt:               snapshot.LoadedAt,
		LastRefreshAttemptAt:   snapshot.LastRefreshAttemptAt,
		LastRefreshSucceededAt: snapshot.LastRefreshSucceededAt,
		LastRefreshFailedAt:    snapshot.LastRefreshFailedAt,
		LastErrorCode:          snapshot.LastErrorCode,
		ConsecutiveFailures:    snapshot.ConsecutiveFailures,
		RefreshSuccesses:       snapshot.RefreshSuccesses,
		RefreshFailures:        snapshot.RefreshFailures,
		Source:                 snapshot.Source,
		Valid:                  snapshot.Valid,
		Stale:                  snapshot.Stale,
		PersistedLoaded:        snapshot.PersistedLoaded,
	}
}

func (r *GatewaySettingsRuntime) Run(ctx context.Context) {
	if r == nil {
		return
	}
	for {
		delay := r.refreshInterval + r.jitter(r.refreshInterval)
		timer := time.NewTimer(delay)
		select {
		case <-ctx.Done():
			stopGatewaySettingsTimer(timer)
			return
		case <-timer.C:
			_ = r.Refresh(ctx)
		case <-r.refreshTrigger:
			stopGatewaySettingsTimer(timer)
			_ = r.Refresh(ctx)
		}
	}
}

func (r *GatewaySettingsRuntime) refresh(ctx context.Context, initial bool) error {
	r.refreshMu.Lock()
	record, err := r.loader.LoadGatewaySettings(ctx)
	r.refreshMu.Unlock()
	if ctx.Err() != nil {
		return ctx.Err()
	}

	r.mu.Lock()
	defer r.mu.Unlock()

	current := r.snapshot.Load()
	if errors.Is(err, ErrNotFound) {
		if current != nil && current.PersistedLoaded {
			r.publishFailure(current, GatewaySettingsMissingErrorCode)
			return ErrGatewaySettingsUnavailable
		}
		r.publishDefaultSuccess(current)
		return nil
	}
	if errors.Is(err, ErrGatewaySettingsInvalid) {
		r.publishFailure(current, GatewaySettingsInvalidErrorCode)
		return ErrGatewaySettingsInvalid
	}
	if err != nil {
		r.publishFailure(current, GatewaySettingsLoadFailedErrorCode)
		return ErrGatewaySettingsUnavailable
	}

	settings, normalizeErr := normalizeGatewaySettings(record.Settings)
	if normalizeErr != nil || record.UpdatedAt.IsZero() {
		r.publishFailure(current, GatewaySettingsInvalidErrorCode)
		return ErrGatewaySettingsInvalid
	}
	record.Settings = settings
	record.UpdatedAt = record.UpdatedAt.UTC()
	if !initial && current != nil && current.Version != nil && record.UpdatedAt.Before(*current.Version) {
		r.publishFailure(current, GatewaySettingsVersionErrorCode)
		return ErrGatewaySettingsInvalid
	}
	r.publishSuccess(current, record, GatewaySettingsSourcePersisted, true)
	return nil
}

func (r *GatewaySettingsRuntime) publishCommitted(current *GatewaySettingsSnapshot, record GatewaySettingsRecord) {
	now := r.now().UTC()
	next := cloneGatewaySettingsSnapshot(current)
	next.Settings = record.Settings
	next.Version = timePointer(record.UpdatedAt)
	next.LoadedAt = timePointer(now)
	next.LastErrorCode = ""
	next.ConsecutiveFailures = 0
	next.Source = GatewaySettingsSourcePersisted
	next.Valid = true
	next.Stale = false
	next.PersistedLoaded = true
	r.storeSnapshot(current, &next, "")
}

func (r *GatewaySettingsRuntime) publishDefaultSuccess(current *GatewaySettingsSnapshot) {
	now := r.now().UTC()
	next := cloneGatewaySettingsSnapshot(current)
	if !next.Valid {
		next.Settings = r.defaults
		next.LoadedAt = timePointer(now)
	}
	next.LastRefreshAttemptAt = timePointer(now)
	next.LastRefreshSucceededAt = timePointer(now)
	next.LastErrorCode = ""
	next.ConsecutiveFailures = 0
	next.RefreshSuccesses++
	next.Source = GatewaySettingsSourceStartupDefault
	next.Valid = true
	next.Stale = false
	r.storeSnapshot(current, &next, "success")
}

func (r *GatewaySettingsRuntime) publishSuccess(current *GatewaySettingsSnapshot, record GatewaySettingsRecord, source string, persisted bool) {
	now := r.now().UTC()
	next := cloneGatewaySettingsSnapshot(current)
	next.Settings = record.Settings
	next.Version = timePointer(record.UpdatedAt)
	next.LoadedAt = timePointer(now)
	next.LastRefreshAttemptAt = timePointer(now)
	next.LastRefreshSucceededAt = timePointer(now)
	next.LastErrorCode = ""
	next.ConsecutiveFailures = 0
	next.RefreshSuccesses++
	next.Source = source
	next.Valid = true
	next.Stale = false
	next.PersistedLoaded = next.PersistedLoaded || persisted
	r.storeSnapshot(current, &next, "success")
}

func (r *GatewaySettingsRuntime) publishFailure(current *GatewaySettingsSnapshot, errorCode string) {
	now := r.now().UTC()
	next := cloneGatewaySettingsSnapshot(current)
	next.LastRefreshAttemptAt = timePointer(now)
	next.LastRefreshFailedAt = timePointer(now)
	next.LastErrorCode = errorCode
	next.ConsecutiveFailures++
	next.RefreshFailures++
	next.Stale = next.Valid
	if next.Valid {
		next.Source = GatewaySettingsSourceLastKnownGood
	}
	r.storeSnapshot(current, &next, "failure")
}

func (r *GatewaySettingsRuntime) publishVersionConflict(current *GatewaySettingsSnapshot) {
	next := cloneGatewaySettingsSnapshot(current)
	next.LastErrorCode = GatewaySettingsVersionErrorCode
	next.Stale = next.Valid
	if next.Valid {
		next.Source = GatewaySettingsSourceLastKnownGood
	}
	r.storeSnapshot(current, &next, "")
}

func (r *GatewaySettingsRuntime) storeSnapshot(previous, next *GatewaySettingsSnapshot, outcome string) {
	r.snapshot.Store(next)
	if r.observer != nil {
		if outcome != "" {
			r.observer.ObserveGatewaySettingsRefresh(outcome)
		}
		loadedAt := time.Time{}
		if next.LoadedAt != nil {
			loadedAt = *next.LoadedAt
		}
		r.observer.SetGatewaySettingsSnapshot(next.Valid, next.Stale, loadedAt)
	}
	previousFailures := uint64(0)
	if previous != nil {
		previousFailures = previous.ConsecutiveFailures
	}
	if outcome == "failure" && previousFailures == 0 {
		r.logger.Warn("gateway settings refresh failed", "error_code", next.LastErrorCode)
	}
	if outcome == "success" && previousFailures > 0 {
		r.logger.Info("gateway settings refresh recovered", "error_code", "gateway_settings_refresh_recovered")
	}
}

func cloneGatewaySettingsSnapshot(snapshot *GatewaySettingsSnapshot) GatewaySettingsSnapshot {
	if snapshot == nil {
		return GatewaySettingsSnapshot{}
	}
	next := *snapshot
	next.Version = cloneTimePointer(snapshot.Version)
	next.LoadedAt = cloneTimePointer(snapshot.LoadedAt)
	next.LastRefreshAttemptAt = cloneTimePointer(snapshot.LastRefreshAttemptAt)
	next.LastRefreshSucceededAt = cloneTimePointer(snapshot.LastRefreshSucceededAt)
	next.LastRefreshFailedAt = cloneTimePointer(snapshot.LastRefreshFailedAt)
	return next
}

func cloneTimePointer(value *time.Time) *time.Time {
	if value == nil {
		return nil
	}
	return timePointer(*value)
}

func timePointer(value time.Time) *time.Time {
	value = value.UTC()
	return &value
}

func boundedGatewaySettingsJitter(interval time.Duration) time.Duration {
	window := interval / 10
	if window <= 0 {
		return 0
	}
	return time.Duration(rand.Int64N(int64(window) + 1))
}

func (r *GatewaySettingsRuntime) scheduleRefresh() {
	select {
	case r.refreshTrigger <- struct{}{}:
	default:
	}
}

func stopGatewaySettingsTimer(timer *time.Timer) {
	if timer.Stop() {
		return
	}
	select {
	case <-timer.C:
	default:
	}
}

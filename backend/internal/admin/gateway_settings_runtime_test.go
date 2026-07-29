package admin

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"sync"
	"testing"
	"time"
)

type gatewaySettingsLoaderStub struct {
	mu     sync.Mutex
	record GatewaySettingsRecord
	err    error
	calls  int
}

func (s *gatewaySettingsLoaderStub) LoadGatewaySettings(context.Context) (GatewaySettingsRecord, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.calls++
	return s.record, s.err
}

func (s *gatewaySettingsLoaderStub) set(record GatewaySettingsRecord, err error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.record = record
	s.err = err
}

func (s *gatewaySettingsLoaderStub) callCount() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.calls
}

func newTestGatewaySettingsRuntime(t *testing.T, loader GatewaySettingsLoader, defaults GatewaySettings, cfg GatewaySettingsRuntimeConfig) *GatewaySettingsRuntime {
	t.Helper()
	if cfg.Logger == nil {
		cfg.Logger = slog.New(slog.NewTextHandler(io.Discard, nil))
	}
	runtime, err := NewGatewaySettingsRuntime(loader, defaults, cfg)
	if err != nil {
		t.Fatalf("NewGatewaySettingsRuntime: %v", err)
	}
	return runtime
}

func TestGatewaySettingsRuntimeInitialLoadUsesPersistedRecord(t *testing.T) {
	version := time.Date(2026, time.July, 29, 10, 0, 0, 0, time.UTC)
	loadedAt := version.Add(time.Minute)
	loader := &gatewaySettingsLoaderStub{record: GatewaySettingsRecord{
		Settings:  GatewaySettings{MaxConcurrentGatewayRequests: 17},
		UpdatedAt: version,
	}}
	runtime := newTestGatewaySettingsRuntime(t, loader, GatewaySettings{}, GatewaySettingsRuntimeConfig{
		Now: func() time.Time { return loadedAt },
	})

	if err := runtime.LoadInitial(context.Background()); err != nil {
		t.Fatalf("LoadInitial: %v", err)
	}
	snapshot := runtime.Snapshot()
	if !snapshot.Valid || snapshot.Stale || !snapshot.PersistedLoaded || snapshot.Source != GatewaySettingsSourcePersisted {
		t.Fatalf("snapshot state = %+v", snapshot)
	}
	if snapshot.Settings.MaxConcurrentGatewayRequests != 17 || snapshot.Version == nil || !snapshot.Version.Equal(version) {
		t.Fatalf("snapshot value/version = %+v / %v", snapshot.Settings, snapshot.Version)
	}
	if snapshot.LoadedAt == nil || !snapshot.LoadedAt.Equal(loadedAt) || snapshot.RefreshSuccesses != 1 {
		t.Fatalf("snapshot timestamps/counts = %+v", snapshot)
	}
}

func TestGatewaySettingsRuntimeInitialLoadUsesValidatedDefaultsWhenMissing(t *testing.T) {
	loader := &gatewaySettingsLoaderStub{err: ErrNotFound}
	runtime := newTestGatewaySettingsRuntime(t, loader, GatewaySettings{
		MaxConcurrentGatewayRequests:           9,
		ProviderAccountAutoTestIntervalSeconds: 300,
	}, GatewaySettingsRuntimeConfig{})

	if err := runtime.LoadInitial(context.Background()); err != nil {
		t.Fatalf("LoadInitial: %v", err)
	}
	snapshot := runtime.Snapshot()
	if !snapshot.Valid || snapshot.Stale || snapshot.PersistedLoaded || snapshot.Source != GatewaySettingsSourceStartupDefault {
		t.Fatalf("snapshot state = %+v", snapshot)
	}
	if snapshot.Settings.MaxConcurrentGatewayRequests != 9 || snapshot.Settings.ProviderAccountAutoTestIntervalSeconds != 300 {
		t.Fatalf("snapshot defaults = %+v", snapshot.Settings)
	}
}

func TestGatewaySettingsRuntimeInitialFailureLeavesNoValidSnapshot(t *testing.T) {
	for _, test := range []struct {
		name      string
		record    GatewaySettingsRecord
		loadErr   error
		wantErr   error
		errorCode string
	}{
		{name: "database", loadErr: errors.New("database unavailable"), wantErr: ErrGatewaySettingsUnavailable, errorCode: GatewaySettingsLoadFailedErrorCode},
		{name: "corrupt json", loadErr: ErrGatewaySettingsInvalid, wantErr: ErrGatewaySettingsInvalid, errorCode: GatewaySettingsInvalidErrorCode},
		{name: "invalid value", record: GatewaySettingsRecord{Settings: GatewaySettings{MaxConcurrentGatewayRequests: -1}, UpdatedAt: time.Now()}, wantErr: ErrGatewaySettingsInvalid, errorCode: GatewaySettingsInvalidErrorCode},
		{name: "missing version", record: GatewaySettingsRecord{Settings: GatewaySettings{MaxConcurrentGatewayRequests: 1}}, wantErr: ErrGatewaySettingsInvalid, errorCode: GatewaySettingsInvalidErrorCode},
	} {
		t.Run(test.name, func(t *testing.T) {
			loader := &gatewaySettingsLoaderStub{record: test.record, err: test.loadErr}
			runtime := newTestGatewaySettingsRuntime(t, loader, GatewaySettings{}, GatewaySettingsRuntimeConfig{})
			if err := runtime.LoadInitial(context.Background()); !errors.Is(err, test.wantErr) {
				t.Fatalf("LoadInitial error = %v, want %v", err, test.wantErr)
			}
			snapshot := runtime.Snapshot()
			if snapshot.Valid || snapshot.Stale || snapshot.LastErrorCode != test.errorCode || snapshot.RefreshFailures != 1 {
				t.Fatalf("snapshot = %+v", snapshot)
			}
			if _, err := runtime.GetGatewaySettings(context.Background()); !errors.Is(err, ErrGatewaySettingsUnavailable) {
				t.Fatalf("GetGatewaySettings error = %v, want unavailable", err)
			}
		})
	}
}

type blockingGatewaySettingsLoader struct {
	started chan struct{}
	release chan struct{}
	record  GatewaySettingsRecord
}

func (l *blockingGatewaySettingsLoader) LoadGatewaySettings(ctx context.Context) (GatewaySettingsRecord, error) {
	close(l.started)
	select {
	case <-l.release:
		return l.record, nil
	case <-ctx.Done():
		return GatewaySettingsRecord{}, ctx.Err()
	}
}

func TestGatewaySettingsRuntimeRefreshDoesNotBlockCommittedPublication(t *testing.T) {
	initialLoader := &gatewaySettingsLoaderStub{err: ErrNotFound}
	runtime := newTestGatewaySettingsRuntime(t, initialLoader, GatewaySettings{}, GatewaySettingsRuntimeConfig{})
	if err := runtime.LoadInitial(context.Background()); err != nil {
		t.Fatalf("LoadInitial: %v", err)
	}
	blockingLoader := &blockingGatewaySettingsLoader{
		started: make(chan struct{}), release: make(chan struct{}),
		record: GatewaySettingsRecord{Settings: GatewaySettings{}, UpdatedAt: time.Now().UTC()},
	}
	runtime.loader = blockingLoader
	refreshDone := make(chan error, 1)
	go func() { refreshDone <- runtime.Refresh(context.Background()) }()
	<-blockingLoader.started

	published := make(chan error, 1)
	go func() {
		published <- runtime.PublishPersisted(GatewaySettingsRecord{
			Settings:  GatewaySettings{MaxConcurrentGatewayRequests: 41},
			UpdatedAt: time.Now().UTC().Add(time.Second),
		})
	}()
	select {
	case err := <-published:
		if err != nil {
			t.Fatalf("PublishPersisted: %v", err)
		}
	case <-time.After(100 * time.Millisecond):
		t.Fatal("PublishPersisted blocked behind database refresh")
	}
	settings, _ := runtime.GetGatewaySettings(context.Background())
	if settings.MaxConcurrentGatewayRequests != 41 {
		t.Fatalf("published settings = %+v", settings)
	}
	close(blockingLoader.release)
	if err := <-refreshDone; err == nil {
		t.Fatal("older blocked refresh unexpectedly replaced the committed publication")
	}
}

func TestGatewaySettingsRuntimeSnapshotIsImmutableToCallers(t *testing.T) {
	version := time.Date(2026, time.July, 29, 10, 0, 0, 0, time.UTC)
	loader := &gatewaySettingsLoaderStub{record: GatewaySettingsRecord{Settings: GatewaySettings{}, UpdatedAt: version}}
	runtime := newTestGatewaySettingsRuntime(t, loader, GatewaySettings{}, GatewaySettingsRuntimeConfig{})
	if err := runtime.LoadInitial(context.Background()); err != nil {
		t.Fatalf("LoadInitial: %v", err)
	}
	external := runtime.Snapshot()
	*external.Version = external.Version.Add(24 * time.Hour)
	*external.LoadedAt = external.LoadedAt.Add(24 * time.Hour)
	internal := runtime.Snapshot()
	if internal.Version == nil || !internal.Version.Equal(version) {
		t.Fatalf("caller changed internal version: %v", internal.Version)
	}
	if internal.LoadedAt == nil || internal.LoadedAt.Equal(*external.LoadedAt) {
		t.Fatalf("caller changed internal loadedAt: %v", internal.LoadedAt)
	}
}

func TestGatewaySettingsRuntimeEqualVersionConflictTriggersConfirmedReload(t *testing.T) {
	version := time.Date(2026, time.July, 29, 10, 0, 0, 0, time.UTC)
	loader := &gatewaySettingsLoaderStub{record: GatewaySettingsRecord{
		Settings: GatewaySettings{MaxConcurrentGatewayRequests: 10}, UpdatedAt: version,
	}}
	runtime := newTestGatewaySettingsRuntime(t, loader, GatewaySettings{}, GatewaySettingsRuntimeConfig{
		RefreshInterval: time.Hour,
		Jitter:          func(time.Duration) time.Duration { return 0 },
	})
	if err := runtime.LoadInitial(context.Background()); err != nil {
		t.Fatalf("LoadInitial: %v", err)
	}
	loader.set(GatewaySettingsRecord{Settings: GatewaySettings{MaxConcurrentGatewayRequests: 20}, UpdatedAt: version}, nil)
	if err := runtime.PublishPersisted(GatewaySettingsRecord{
		Settings: GatewaySettings{MaxConcurrentGatewayRequests: 20}, UpdatedAt: version,
	}); !errors.Is(err, ErrGatewaySettingsInvalid) {
		t.Fatalf("PublishPersisted conflict error = %v, want invalid", err)
	}
	conflict := runtime.Snapshot()
	if !conflict.Valid || !conflict.Stale || conflict.LastErrorCode != GatewaySettingsVersionErrorCode || conflict.Settings.MaxConcurrentGatewayRequests != 10 {
		t.Fatalf("conflict snapshot = %+v", conflict)
	}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() { runtime.Run(ctx); close(done) }()
	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		settings, _ := runtime.GetGatewaySettings(context.Background())
		if settings.MaxConcurrentGatewayRequests == 20 {
			break
		}
		time.Sleep(time.Millisecond)
	}
	cancel()
	<-done
	settings, _ := runtime.GetGatewaySettings(context.Background())
	if settings.MaxConcurrentGatewayRequests != 20 || loader.callCount() < 2 {
		t.Fatalf("confirmed reload settings/calls = %+v/%d", settings, loader.callCount())
	}
}

func TestGatewaySettingsRuntimeRefreshFailureRetainsLastKnownGood(t *testing.T) {
	version := time.Date(2026, time.July, 29, 10, 0, 0, 0, time.UTC)
	loader := &gatewaySettingsLoaderStub{record: GatewaySettingsRecord{
		Settings:  GatewaySettings{MaxConcurrentGatewayRequests: 13},
		UpdatedAt: version,
	}}
	runtime := newTestGatewaySettingsRuntime(t, loader, GatewaySettings{}, GatewaySettingsRuntimeConfig{})
	if err := runtime.LoadInitial(context.Background()); err != nil {
		t.Fatalf("LoadInitial: %v", err)
	}

	loader.set(GatewaySettingsRecord{}, ErrGatewaySettingsInvalid)
	if err := runtime.Refresh(context.Background()); !errors.Is(err, ErrGatewaySettingsInvalid) {
		t.Fatalf("Refresh error = %v, want invalid", err)
	}
	snapshot := runtime.Snapshot()
	if !snapshot.Valid || !snapshot.Stale || snapshot.Source != GatewaySettingsSourceLastKnownGood || snapshot.LastErrorCode != GatewaySettingsInvalidErrorCode {
		t.Fatalf("snapshot state = %+v", snapshot)
	}
	settings, err := runtime.GetGatewaySettings(context.Background())
	if err != nil || settings.MaxConcurrentGatewayRequests != 13 {
		t.Fatalf("last known good settings = %+v, %v", settings, err)
	}

	loader.set(GatewaySettingsRecord{}, ErrNotFound)
	if err := runtime.Refresh(context.Background()); !errors.Is(err, ErrGatewaySettingsUnavailable) {
		t.Fatalf("missing persisted Refresh error = %v, want unavailable", err)
	}
	if snapshot = runtime.Snapshot(); snapshot.LastErrorCode != GatewaySettingsMissingErrorCode || snapshot.Settings.MaxConcurrentGatewayRequests != 13 {
		t.Fatalf("missing-row snapshot = %+v", snapshot)
	}
}

func TestGatewaySettingsRuntimePublishIsImmediateAndRejectsOlderVersion(t *testing.T) {
	version := time.Date(2026, time.July, 29, 10, 0, 0, 0, time.UTC)
	loader := &gatewaySettingsLoaderStub{err: ErrNotFound}
	runtime := newTestGatewaySettingsRuntime(t, loader, GatewaySettings{}, GatewaySettingsRuntimeConfig{})
	if err := runtime.LoadInitial(context.Background()); err != nil {
		t.Fatalf("LoadInitial: %v", err)
	}
	if err := runtime.PublishPersisted(GatewaySettingsRecord{
		Settings: GatewaySettings{MaxConcurrentGatewayRequests: 21}, UpdatedAt: version,
	}); err != nil {
		t.Fatalf("PublishPersisted: %v", err)
	}
	settings, err := runtime.GetGatewaySettings(context.Background())
	if err != nil || settings.MaxConcurrentGatewayRequests != 21 {
		t.Fatalf("published settings = %+v, %v", settings, err)
	}
	if err := runtime.PublishPersisted(GatewaySettingsRecord{
		Settings: GatewaySettings{MaxConcurrentGatewayRequests: 3}, UpdatedAt: version.Add(-time.Second),
	}); err != nil {
		t.Fatalf("PublishPersisted older version: %v", err)
	}
	settings, _ = runtime.GetGatewaySettings(context.Background())
	if settings.MaxConcurrentGatewayRequests != 21 {
		t.Fatalf("older version replaced snapshot: %+v", settings)
	}
}

func TestGatewaySettingsRuntimeRunRefreshesPeriodically(t *testing.T) {
	version := time.Date(2026, time.July, 29, 10, 0, 0, 0, time.UTC)
	loader := &gatewaySettingsLoaderStub{record: GatewaySettingsRecord{Settings: GatewaySettings{}, UpdatedAt: version}}
	runtime := newTestGatewaySettingsRuntime(t, loader, GatewaySettings{}, GatewaySettingsRuntimeConfig{
		RefreshInterval: 5 * time.Millisecond,
		Jitter:          func(time.Duration) time.Duration { return 0 },
	})
	if err := runtime.LoadInitial(context.Background()); err != nil {
		t.Fatalf("LoadInitial: %v", err)
	}
	loader.set(GatewaySettingsRecord{Settings: GatewaySettings{MaxConcurrentGatewayRequests: 22}, UpdatedAt: version.Add(time.Second)}, nil)
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		runtime.Run(ctx)
		close(done)
	}()
	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		settings, _ := runtime.GetGatewaySettings(context.Background())
		if settings.MaxConcurrentGatewayRequests == 22 {
			break
		}
		time.Sleep(time.Millisecond)
	}
	cancel()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("Run did not stop after cancellation")
	}
	settings, _ := runtime.GetGatewaySettings(context.Background())
	if settings.MaxConcurrentGatewayRequests != 22 || loader.callCount() < 2 {
		t.Fatalf("periodic refresh settings/calls = %+v/%d", settings, loader.callCount())
	}
}

func TestGatewaySettingsRuntimeJitterIsBoundedToTenPercent(t *testing.T) {
	interval := time.Minute
	for range 1000 {
		jitter := boundedGatewaySettingsJitter(interval)
		if jitter < 0 || jitter > interval/10 {
			t.Fatalf("jitter = %s, want [0, %s]", jitter, interval/10)
		}
	}
}

func TestGatewaySettingsRuntimeConcurrentReadsAndPublishes(t *testing.T) {
	loader := &gatewaySettingsLoaderStub{err: ErrNotFound}
	runtime := newTestGatewaySettingsRuntime(t, loader, GatewaySettings{}, GatewaySettingsRuntimeConfig{})
	if err := runtime.LoadInitial(context.Background()); err != nil {
		t.Fatalf("LoadInitial: %v", err)
	}

	var readers sync.WaitGroup
	for range 16 {
		readers.Add(1)
		go func() {
			defer readers.Done()
			for range 500 {
				if _, err := runtime.GetGatewaySettings(context.Background()); err != nil {
					t.Errorf("GetGatewaySettings: %v", err)
					return
				}
				_ = runtime.GatewaySettingsRuntimeStatus()
			}
		}()
	}
	base := time.Date(2026, time.July, 29, 10, 0, 0, 0, time.UTC)
	for i := 1; i <= 100; i++ {
		if err := runtime.PublishPersisted(GatewaySettingsRecord{
			Settings: GatewaySettings{MaxConcurrentGatewayRequests: i}, UpdatedAt: base.Add(time.Duration(i) * time.Second),
		}); err != nil {
			t.Fatalf("PublishPersisted %d: %v", i, err)
		}
	}
	readers.Wait()
	settings, _ := runtime.GetGatewaySettings(context.Background())
	if settings.MaxConcurrentGatewayRequests != 100 {
		t.Fatalf("final settings = %+v", settings)
	}
}

func TestGatewaySettingsServicePublishesOnlyCommittedSaves(t *testing.T) {
	repo := newMemoryRepo()
	runtime := newTestGatewaySettingsRuntime(t, repo, GatewaySettings{}, GatewaySettingsRuntimeConfig{})
	if err := runtime.LoadInitial(context.Background()); err != nil {
		t.Fatalf("LoadInitial: %v", err)
	}
	service := NewService(repo, Config{GatewaySettingsRuntime: runtime})

	repo.gatewaySettingsUpdatedAt = time.Date(2026, time.July, 29, 10, 0, 0, 0, time.UTC)
	saved, err := service.UpdateGatewaySettings(context.Background(), GatewaySettings{MaxConcurrentGatewayRequests: 31})
	if err != nil || saved.MaxConcurrentGatewayRequests != 31 {
		t.Fatalf("UpdateGatewaySettings = %+v, %v", saved, err)
	}
	found, err := service.GetGatewaySettings(context.Background())
	if err != nil || found.MaxConcurrentGatewayRequests != 31 {
		t.Fatalf("immediate runtime settings = %+v, %v", found, err)
	}
	loadCount := repo.gatewaySettingsLoadCount
	for range 10 {
		if _, err := service.GetGatewaySettings(context.Background()); err != nil {
			t.Fatalf("runtime GetGatewaySettings: %v", err)
		}
	}
	if repo.gatewaySettingsLoadCount != loadCount {
		t.Fatalf("runtime reads reached repository: before=%d after=%d", loadCount, repo.gatewaySettingsLoadCount)
	}

	repo.gatewaySettingsSaveErr = errors.New("save failed")
	if _, err := service.UpdateGatewaySettings(context.Background(), GatewaySettings{MaxConcurrentGatewayRequests: 44}); err == nil {
		t.Fatal("UpdateGatewaySettings succeeded despite save failure")
	}
	found, _ = service.GetGatewaySettings(context.Background())
	if found.MaxConcurrentGatewayRequests != 31 {
		t.Fatalf("failed save replaced runtime snapshot: %+v", found)
	}
}

func TestGatewaySettingsServiceSchedulesReloadWhenCommittedPublicationCannotBeConfirmed(t *testing.T) {
	repo := newMemoryRepo()
	runtime := newTestGatewaySettingsRuntime(t, repo, GatewaySettings{}, GatewaySettingsRuntimeConfig{
		RefreshInterval: time.Hour,
		Jitter:          func(time.Duration) time.Duration { return 0 },
	})
	if err := runtime.LoadInitial(context.Background()); err != nil {
		t.Fatalf("LoadInitial: %v", err)
	}
	service := NewService(repo, Config{GatewaySettingsRuntime: runtime})
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() { runtime.Run(ctx); close(done) }()
	defer func() {
		cancel()
		<-done
	}()

	repo.gatewaySettingsZeroSaveVersion = true
	if _, err := service.UpdateGatewaySettings(context.Background(), GatewaySettings{MaxConcurrentGatewayRequests: 52}); !errors.Is(err, ErrGatewaySettingsInvalid) {
		t.Fatalf("UpdateGatewaySettings error = %v, want invalid publication", err)
	}
	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		settings, _ := runtime.GetGatewaySettings(context.Background())
		if settings.MaxConcurrentGatewayRequests == 52 {
			return
		}
		time.Sleep(time.Millisecond)
	}
	settings, _ := runtime.GetGatewaySettings(context.Background())
	t.Fatalf("scheduled reload did not publish committed settings: %+v", settings)
}

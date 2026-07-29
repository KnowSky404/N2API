package store

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/KnowSky404/N2API/backend/internal/admin"
)

func TestDecodeGatewaySettingsRecordPreservesVersionAndBoundsInvalidJSON(t *testing.T) {
	updatedAt := time.Date(2026, time.July, 29, 10, 0, 0, 0, time.FixedZone("test", 2*60*60))
	record, err := decodeGatewaySettingsRecord([]byte(`{"maxConcurrentGatewayRequests":12}`), updatedAt)
	if err != nil {
		t.Fatalf("decodeGatewaySettingsRecord: %v", err)
	}
	if record.Settings.MaxConcurrentGatewayRequests != 12 || !record.UpdatedAt.Equal(updatedAt) || record.UpdatedAt.Location() != time.UTC {
		t.Fatalf("record = %+v", record)
	}
	for _, raw := range [][]byte{
		[]byte(`{"maxConcurrentGatewayRequests":"secret-canary"}`),
		[]byte(`{"maxConcurrentGatewayRequests":`),
	} {
		if _, err := decodeGatewaySettingsRecord(raw, updatedAt); !errors.Is(err, admin.ErrGatewaySettingsInvalid) {
			t.Fatalf("invalid JSON error = %v, want bounded invalid sentinel", err)
		}
	}
}

func TestGatewaySettingsRuntimeRetainsLastKnownGoodAcrossPostgresReadFailure(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	alertRepo := newTestAlertingRepository(t, ctx)
	repo := NewAdminRepository(alertRepo.pool, "gateway-settings-profile-secret")

	if _, err := alertRepo.pool.Exec(ctx, `
		INSERT INTO settings (key, value, updated_at)
		VALUES ($1, $2, TIMESTAMPTZ '2026-07-29 10:00:00+00')
	`, gatewaySettingsKey, []byte(`{"maxConcurrentGatewayRequests":13,"providerAccountAutoTestIntervalSeconds":300}`)); err != nil {
		t.Fatalf("seed gateway settings: %v", err)
	}
	runtime, err := admin.NewGatewaySettingsRuntime(repo, admin.GatewaySettings{}, admin.GatewaySettingsRuntimeConfig{})
	if err != nil {
		t.Fatalf("NewGatewaySettingsRuntime: %v", err)
	}
	if err := runtime.LoadInitial(ctx); err != nil {
		t.Fatalf("LoadInitial: %v", err)
	}

	if _, err := alertRepo.pool.Exec(ctx, "ALTER TABLE settings RENAME TO settings_read_failure"); err != nil {
		t.Fatalf("inject settings relation failure: %v", err)
	}
	relationRenamed := true
	t.Cleanup(func() {
		if !relationRenamed {
			return
		}
		cleanupCtx, cleanupCancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cleanupCancel()
		if _, err := alertRepo.pool.Exec(cleanupCtx, "ALTER TABLE settings_read_failure RENAME TO settings"); err != nil {
			t.Errorf("restore settings relation: %v", err)
		}
	})

	if err := runtime.Refresh(ctx); !errors.Is(err, admin.ErrGatewaySettingsUnavailable) {
		t.Fatalf("Refresh during PostgreSQL failure = %v, want unavailable", err)
	}
	failed := runtime.Snapshot()
	if !failed.Valid || !failed.Stale || failed.Source != admin.GatewaySettingsSourceLastKnownGood || failed.LastErrorCode != admin.GatewaySettingsLoadFailedErrorCode {
		t.Fatalf("failed refresh snapshot = %+v", failed)
	}
	settings, err := runtime.GetGatewaySettings(ctx)
	if err != nil || settings.MaxConcurrentGatewayRequests != 13 {
		t.Fatalf("last-known-good settings/error = %+v/%v", settings, err)
	}

	if _, err := alertRepo.pool.Exec(ctx, "ALTER TABLE settings_read_failure RENAME TO settings"); err != nil {
		t.Fatalf("restore settings relation: %v", err)
	}
	relationRenamed = false
	if err := runtime.Refresh(ctx); err != nil {
		t.Fatalf("Refresh after PostgreSQL recovery: %v", err)
	}
	recovered := runtime.Snapshot()
	if !recovered.Valid || recovered.Stale || recovered.Source != admin.GatewaySettingsSourcePersisted || recovered.LastErrorCode != "" {
		t.Fatalf("recovered snapshot = %+v", recovered)
	}
}

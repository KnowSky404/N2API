package store

import (
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

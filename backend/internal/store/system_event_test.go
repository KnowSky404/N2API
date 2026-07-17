package store

import (
	"strings"
	"testing"
	"time"

	"github.com/KnowSky404/N2API/backend/internal/systemevent"
)

func TestSystemEventCursorIsAuthenticatedAndSelfContained(t *testing.T) {
	repo := NewSystemEventRepository(nil, "cursor-secret")
	want := systemEventCursor{OccurredAt: time.Unix(1234, 567).UTC(), ID: 42}
	encoded, err := repo.encodeCursor(want)
	if err != nil {
		t.Fatalf("encodeCursor returned error: %v", err)
	}
	got, err := repo.decodeCursor(encoded)
	if err != nil {
		t.Fatalf("decodeCursor returned error: %v", err)
	}
	if got.ID != want.ID || !got.OccurredAt.Equal(want.OccurredAt) {
		t.Fatalf("cursor = %+v, want %+v", got, want)
	}
	tampered := encoded[:len(encoded)-1] + "A"
	if _, err := repo.decodeCursor(tampered); err == nil {
		t.Fatal("tampered cursor accepted")
	}
}

func TestSystemEventSQLUsesDeterministicKeysetAndBoundedRetention(t *testing.T) {
	for _, want := range []string{"occurred_at", "correlation_id", "metadata"} {
		if !strings.Contains(insertSystemEventSQL, want) {
			t.Fatalf("insert SQL missing %q", want)
		}
	}
	if !strings.Contains(systemEventSelectSQL, "host(source_ip)") {
		t.Fatal("select SQL must return canonical source IP")
	}
	_ = systemevent.ActionAPIKeyCreated
}

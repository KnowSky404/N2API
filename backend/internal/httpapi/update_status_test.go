package httpapi

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/KnowSky404/N2API/backend/internal/buildinfo"
	"github.com/KnowSky404/N2API/backend/internal/config"
	"github.com/KnowSky404/N2API/backend/internal/updatecheck"
)

type fakeUpdateStatusService struct {
	snapshot   updatecheck.Snapshot
	refreshErr error
	refreshed  bool
}

func (s *fakeUpdateStatusService) Snapshot() updatecheck.Snapshot {
	return s.snapshot
}

func (s *fakeUpdateStatusService) Refresh(context.Context) (updatecheck.Snapshot, error) {
	s.refreshed = true
	return s.snapshot, s.refreshErr
}

func TestUpdateStatusRequiresAdminSession(t *testing.T) {
	updates := &fakeUpdateStatusService{snapshot: testUpdateSnapshot()}
	server := NewServer(config.Config{}, staticHealth{err: nil}, newFakeAdminService(), nil, updates)
	recorder := httptest.NewRecorder()
	server.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/api/admin/update-status", nil))
	if recorder.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401", recorder.Code)
	}
}

func TestUpdateStatusReturnsAuthenticatedSnapshot(t *testing.T) {
	updates := &fakeUpdateStatusService{snapshot: testUpdateSnapshot()}
	server := NewServer(config.Config{}, staticHealth{err: nil}, newFakeAdminService(), nil, updates)
	request := httptest.NewRequest(http.MethodGet, "/api/admin/update-status", nil)
	request.AddCookie(&http.Cookie{Name: adminSessionCookieName, Value: "valid-session"})
	recorder := httptest.NewRecorder()
	server.ServeHTTP(recorder, request)
	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", recorder.Code, recorder.Body.String())
	}
	var got updatecheck.Snapshot
	if err := json.Unmarshal(recorder.Body.Bytes(), &got); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if got.Status != updatecheck.StatusUpdateAvailable || got.Latest == nil || got.Latest.Version != "2026073101" {
		t.Fatalf("snapshot = %+v", got)
	}
}

func TestUpdateStatusRefreshReturnsCooldown(t *testing.T) {
	updates := &fakeUpdateStatusService{
		snapshot:   testUpdateSnapshot(),
		refreshErr: &updatecheck.RefreshCooldownError{RetryAfter: 31 * time.Second},
	}
	server := NewServer(config.Config{}, staticHealth{err: nil}, newFakeAdminService(), nil, updates)
	request := httptest.NewRequest(http.MethodPost, "/api/admin/update-status/refresh", nil)
	request.AddCookie(&http.Cookie{Name: adminSessionCookieName, Value: "valid-session"})
	recorder := httptest.NewRecorder()
	server.ServeHTTP(recorder, request)
	if recorder.Code != http.StatusTooManyRequests || recorder.Header().Get("Retry-After") != "31" || !updates.refreshed {
		t.Fatalf("status = %d, Retry-After = %q, refreshed = %t, body = %s", recorder.Code, recorder.Header().Get("Retry-After"), updates.refreshed, recorder.Body.String())
	}
}

func TestUpdateStatusUnavailableWithoutService(t *testing.T) {
	server := NewServer(config.Config{}, staticHealth{err: nil}, newFakeAdminService(), nil)
	request := httptest.NewRequest(http.MethodGet, "/api/admin/update-status", nil)
	request.AddCookie(&http.Cookie{Name: adminSessionCookieName, Value: "valid-session"})
	recorder := httptest.NewRecorder()
	server.ServeHTTP(recorder, request)
	if recorder.Code != http.StatusServiceUnavailable {
		t.Fatalf("status = %d, want 503", recorder.Code)
	}
}

func testUpdateSnapshot() updatecheck.Snapshot {
	return updatecheck.Snapshot{
		Status:  updatecheck.StatusUpdateAvailable,
		Current: buildinfo.Info{Version: "sha-current", Commit: "1111111111111111111111111111111111111111", BuiltAt: "2026-07-24T00:00:00Z"},
		Latest: &updatecheck.Release{
			Version: "2026073101", Name: "N2API 2026073101", PublishedAt: "2026-07-31T02:14:12Z",
			URL: "https://github.com/KnowSky404/N2API/releases/tag/2026073101",
		},
		CheckedAt: "2026-07-31T08:00:00Z",
	}
}

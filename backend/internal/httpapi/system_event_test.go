package httpapi

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"strings"
	"testing"

	"github.com/KnowSky404/N2API/backend/internal/admin"
	"github.com/KnowSky404/N2API/backend/internal/config"
	"github.com/KnowSky404/N2API/backend/internal/provider"
	"github.com/KnowSky404/N2API/backend/internal/systemevent"
)

type memorySystemEventRecorder struct {
	events []systemevent.Event
	err    error
}

func (r *memorySystemEventRecorder) Insert(_ context.Context, event systemevent.Event) error {
	if r.err != nil {
		return r.err
	}
	r.events = append(r.events, event)
	return nil
}

func TestSystemEventRequestContextUsesDirectRemoteIPAndValidatedRequestID(t *testing.T) {
	var captured systemevent.RequestContext
	mux := http.NewServeMux()
	mux.HandleFunc("GET /test/{id}", func(w http.ResponseWriter, r *http.Request) {
		captured, _ = systemevent.FromContext(r.Context())
		w.WriteHeader(http.StatusNoContent)
	})
	req := httptest.NewRequest(http.MethodGet, "/test/42", nil)
	req.RemoteAddr = "192.0.2.10:1234"
	req.Header.Set("X-Request-ID", "valid-request-42")
	req.Header.Set("X-Forwarded-For", "198.51.100.1")
	res := httptest.NewRecorder()
	withSystemEventRequestContext(mux, nil).ServeHTTP(res, req)
	if captured.CorrelationID != "valid-request-42" || captured.SourceIP != "192.0.2.10" || captured.RoutePattern != "GET /test/{id}" {
		t.Fatalf("request context = %+v", captured)
	}
	if res.Header().Get("X-Request-ID") != "valid-request-42" {
		t.Fatalf("X-Request-ID = %q", res.Header().Get("X-Request-ID"))
	}
}

func TestSystemEventRequestContextUsesTrustedProxyClientIP(t *testing.T) {
	recorder := &memorySystemEventRecorder{}
	cfg := config.Config{TrustedProxyCIDRs: []netip.Prefix{netip.MustParsePrefix("10.0.0.0/8")}}
	server := NewServer(cfg, staticHealth{}, newFakeAdminService(), newFakeProviderService(), recorder)
	req := httptest.NewRequest(http.MethodPost, "/api/admin/login", strings.NewReader(`{"username":"owner","password":"wrong"}`))
	req.RemoteAddr = "10.0.0.2:1234"
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Forwarded-For", "203.0.113.8, 10.0.0.1")
	res := httptest.NewRecorder()

	server.ServeHTTP(res, req)

	if len(recorder.events) != 1 || recorder.events[0].SourceIP != "203.0.113.8" {
		t.Fatalf("events = %+v, want trusted proxy client source", recorder.events)
	}
}

func TestSystemEventsEndpointReturnsOpaquePage(t *testing.T) {
	admins := newFakeAdminService()
	admins.systemEventPage = admin.SystemEventPage{
		Events:     []systemevent.Event{{ID: 7, Action: systemevent.ActionAPIKeyCreated}},
		NextCursor: "opaque", HasMore: true,
	}
	server := NewServer(config.Config{}, staticHealth{}, admins, newFakeProviderService())
	req := authenticatedSystemEventRequest("/api/admin/system-events?limit=25&category=audit&targetType=api_key&targetId=7")
	res := httptest.NewRecorder()
	server.ServeHTTP(res, req)
	if res.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", res.Code, res.Body.String())
	}
	if admins.systemEventFilter.Limit != 25 || admins.systemEventFilter.Category != systemevent.CategoryAudit || admins.systemEventFilter.TargetID != "7" {
		t.Fatalf("filter = %+v", admins.systemEventFilter)
	}
	if body := res.Body.String(); body == "" || !containsAll(body, `"nextCursor":"opaque"`, `"hasMore":true`) {
		t.Fatalf("body = %s", body)
	}
}

func TestSystemEventsEndpointRejectsInvalidSince(t *testing.T) {
	server := NewServer(config.Config{}, staticHealth{}, newFakeAdminService(), newFakeProviderService())
	req := authenticatedSystemEventRequest("/api/admin/system-events?since=not-a-time")
	res := httptest.NewRecorder()
	server.ServeHTTP(res, req)
	if res.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, body = %s", res.Code, res.Body.String())
	}
}

func TestInvalidLoginRecordsFixedSecurityEventWithoutCredentials(t *testing.T) {
	recorder := &memorySystemEventRecorder{}
	server := NewServer(config.Config{}, staticHealth{}, newFakeAdminService(), newFakeProviderService(), recorder)
	username := strings.Repeat("owner", 1024)
	req := httptest.NewRequest(http.MethodPost, "/api/admin/login", strings.NewReader(`{"username":"`+username+`","password":"canary-password"}`))
	req.Header.Set("Content-Type", "application/json")
	res := httptest.NewRecorder()
	server.ServeHTTP(res, req)
	if res.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, body = %s", res.Code, res.Body.String())
	}
	if len(recorder.events) != 1 {
		t.Fatalf("events = %d, want 1", len(recorder.events))
	}
	event := recorder.events[0]
	if event.Action != systemevent.ActionAuthLoginFailed || event.ErrorCode != "invalid_credentials" || event.Target.Name != "administrator" || strings.Contains(event.Message, "canary-password") || strings.Contains(event.Target.Name, username) {
		t.Fatalf("event = %+v", event)
	}
}

func TestLoginThrottleAggregatesRepeatedSecurityEvents(t *testing.T) {
	recorder := &memorySystemEventRecorder{}
	cfg := config.Config{AdminLoginThrottleEnabled: true, AdminLoginThrottleFailures: 1, AdminLoginThrottleMaxEntries: 128}
	server := NewServer(cfg, staticHealth{}, newFakeAdminService(), newFakeProviderService(), recorder)
	for i, username := range []string{"owner-one", "owner-two"} {
		req := httptest.NewRequest(http.MethodPost, "/api/admin/login", strings.NewReader(`{"username":"`+username+`","password":"wrong"}`))
		req.RemoteAddr = fmt.Sprintf("192.0.2.%d:1234", i+10)
		server.ServeHTTP(httptest.NewRecorder(), req)
	}
	if len(recorder.events) != 1 {
		t.Fatalf("events = %d, want one aggregated event", len(recorder.events))
	}
}

func TestDisabledLoginThrottleRecordsEveryFailedLoginEvent(t *testing.T) {
	recorder := &memorySystemEventRecorder{}
	cfg := config.Config{AdminLoginThrottleEnabled: false}
	server := NewServer(cfg, staticHealth{}, newFakeAdminService(), newFakeProviderService(), recorder)
	for range 2 {
		req := httptest.NewRequest(http.MethodPost, "/api/admin/login", strings.NewReader(`{"username":"owner","password":"wrong"}`))
		req.RemoteAddr = "192.0.2.10:1234"
		server.ServeHTTP(httptest.NewRecorder(), req)
	}
	if len(recorder.events) != 2 {
		t.Fatalf("events = %d, want one event per failure while throttling is disabled", len(recorder.events))
	}
}

func TestAPIKeySecretReadFailsClosedWhenSecurityEventCannotBeStored(t *testing.T) {
	recorder := &memorySystemEventRecorder{err: errors.New("event store unavailable")}
	server := NewServer(config.Config{}, staticHealth{}, newFakeAdminService(), newFakeProviderService(), recorder)
	req := authenticatedSystemEventRequest("/api/admin/keys/7/secret")
	res := httptest.NewRecorder()
	server.ServeHTTP(res, req)
	if res.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, body = %s", res.Code, res.Body.String())
	}
	if strings.Contains(res.Body.String(), "n2api_") {
		t.Fatalf("secret leaked in failed response: %s", res.Body.String())
	}
}

func TestProviderBulkPartialFailureRecordsAccurateSummary(t *testing.T) {
	providers := newFakeProviderService()
	providers.accounts = []provider.Account{
		{ID: 7, Provider: "openai", DisplayName: "Account A", Enabled: true},
		{ID: 8, Provider: "openai", DisplayName: "Account B", Enabled: true},
	}
	recorder := &memorySystemEventRecorder{}
	server := NewServer(config.Config{}, staticHealth{}, newFakeAdminService(), providers, recorder)
	req := httptest.NewRequest(http.MethodPost, "/api/admin/provider-accounts/bulk-update", strings.NewReader(`{"accountIds":[7,99,8],"enabled":false}`))
	req.Header.Set("Content-Type", "application/json")
	req.AddCookie(&http.Cookie{Name: adminSessionCookieName, Value: "valid-session"})
	res := httptest.NewRecorder()

	server.ServeHTTP(res, req)

	if res.Code != http.StatusNotFound {
		t.Fatalf("status = %d, body = %s", res.Code, res.Body.String())
	}
	if len(recorder.events) != 1 {
		t.Fatalf("events = %d, want one batch summary", len(recorder.events))
	}
	event := recorder.events[0]
	if event.Action != systemevent.ActionProviderAccountBatchUpdated || event.Outcome != systemevent.OutcomePartial || event.ErrorCode != "not_found" {
		t.Fatalf("event = %+v", event)
	}
	if event.Metadata["requested_count"] != 3 || event.Metadata["attempted_count"] != 2 || event.Metadata["succeeded_count"] != 1 || event.Metadata["failed_count"] != 1 || event.Metadata["skipped_count"] != 1 {
		t.Fatalf("metadata counts = %+v", event.Metadata)
	}
	if got, ok := event.Metadata["skipped_ids"].([]int64); !ok || len(got) != 1 || got[0] != 8 {
		t.Fatalf("skipped_ids = %#v, want [8]", event.Metadata["skipped_ids"])
	}
	if err := systemevent.ValidateEvent(event); err != nil {
		t.Fatalf("ValidateEvent returned error: %v", err)
	}
}

func TestAuthenticatedMutationFailureRecordsFixedAuditEvent(t *testing.T) {
	recorder := &memorySystemEventRecorder{}
	server := NewServer(config.Config{}, staticHealth{}, newFakeAdminService(), newFakeProviderService(), recorder)
	req := httptest.NewRequest(http.MethodPost, "/api/admin/change-password", strings.NewReader(`{`))
	req.Header.Set("Content-Type", "application/json")
	req.AddCookie(&http.Cookie{Name: adminSessionCookieName, Value: "valid-session"})
	res := httptest.NewRecorder()

	server.ServeHTTP(res, req)

	if res.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, body = %s", res.Code, res.Body.String())
	}
	if len(recorder.events) != 1 {
		t.Fatalf("events = %d, want one failure event", len(recorder.events))
	}
	event := recorder.events[0]
	if event.Action != systemevent.ActionAuthPasswordChangeFailed || event.Outcome != systemevent.OutcomeFailure || event.ErrorCode != "invalid_input" || event.Actor.Type != systemevent.ActorAdmin {
		t.Fatalf("event = %+v", event)
	}
	if err := systemevent.ValidateEvent(event); err != nil {
		t.Fatalf("ValidateEvent returned error: %v", err)
	}
}

func authenticatedSystemEventRequest(target string) *http.Request {
	req := httptest.NewRequest(http.MethodGet, target, nil)
	req.AddCookie(&http.Cookie{Name: adminSessionCookieName, Value: "valid-session"})
	return req
}

func containsAll(value string, parts ...string) bool {
	for _, part := range parts {
		if !strings.Contains(value, part) {
			return false
		}
	}
	return true
}

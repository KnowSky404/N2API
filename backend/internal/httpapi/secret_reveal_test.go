package httpapi

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/KnowSky404/N2API/backend/internal/admin"
	"github.com/KnowSky404/N2API/backend/internal/config"
	"github.com/KnowSky404/N2API/backend/internal/systemevent"
)

func TestRevealAPIKeySecretRequiresCurrentSession(t *testing.T) {
	admins := newFakeAdminService()
	recorder := &memorySystemEventRecorder{}
	server := NewServer(config.Config{}, staticHealth{}, admins, nil, recorder)
	response := performSecretRevealRequest(server, 7, "secret", false)

	if response.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401; body=%s", response.Code, response.Body.String())
	}
	if admins.reveal.calls != 0 {
		t.Fatalf("reveal calls = %d, want 0", admins.reveal.calls)
	}
	event := findSecretRevealEvent(t, recorder.events, systemevent.OutcomeFailure)
	if event.ErrorCode != "unauthorized" || event.Actor.Type != systemevent.ActorSystem {
		t.Fatalf("failure event = %+v, want sanitized unauthenticated reveal failure", event)
	}
}

func TestRevealAPIKeySecretPreservesPasswordAndReturnsNoStoreResponse(t *testing.T) {
	admins := newFakeAdminService()
	admins.reveal.expectedPassword = "correct-password-canary"
	admins.reveal.secret = "n2api_reusable_secret"
	recorder := &memorySystemEventRecorder{}
	server := NewServer(config.Config{}, staticHealth{}, admins, nil, recorder)
	response := performSecretRevealRequest(server, 7, " correct-password-canary ", true)

	if response.Code != http.StatusBadRequest {
		t.Fatalf("surrounding-space status = %d, want 400; body=%s", response.Code, response.Body.String())
	}
	if admins.reveal.password != " correct-password-canary " {
		t.Fatalf("password = %q, want exact bytes", admins.reveal.password)
	}

	response = performSecretRevealRequest(server, 7, "correct-password-canary", true)
	if response.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body=%s", response.Code, response.Body.String())
	}
	if response.Header().Get("Cache-Control") != "no-store" {
		t.Fatalf("Cache-Control = %q, want no-store", response.Header().Get("Cache-Control"))
	}
	var payload struct {
		Secret string `json:"secret"`
	}
	if err := json.Unmarshal(response.Body.Bytes(), &payload); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if payload.Secret != admins.reveal.secret {
		t.Fatalf("secret = %q, want reusable secret", payload.Secret)
	}
	if admins.reveal.adminID != 1 || admins.reveal.keyID != 7 || admins.reveal.password != "correct-password-canary" {
		t.Fatalf("reveal input = admin:%d key:%d password:%q", admins.reveal.adminID, admins.reveal.keyID, admins.reveal.password)
	}
	event := findSecretRevealEvent(t, recorder.events, systemevent.OutcomeSuccess)
	if event.Category != systemevent.CategorySecurity || event.Target.ID != "7" {
		t.Fatalf("success event = %+v, want key 7 security event", event)
	}
	assertSecretRevealEventSanitized(t, event, "correct-password-canary", admins.reveal.secret, "n2api_abc")
}

func TestRevealAPIKeySecretUsesUniformPasswordFailureWithoutRevokingSession(t *testing.T) {
	for _, password := range []string{"", "wrong-password"} {
		t.Run(password, func(t *testing.T) {
			admins := newFakeAdminService()
			recorder := &memorySystemEventRecorder{}
			server := NewServer(config.Config{}, staticHealth{}, admins, nil, recorder)
			response := performSecretRevealRequest(server, 7, password, true)

			if response.Code != http.StatusBadRequest || response.Body.String() != "{\"error\":\"invalid_current_password\"}\n" {
				t.Fatalf("response = %d %q, want uniform invalid-current-password", response.Code, response.Body.String())
			}
			if strings.Contains(strings.Join(response.Header().Values("Set-Cookie"), "\n"), "Max-Age=0") {
				t.Fatal("wrong password cleared a valid admin session")
			}
			event := findSecretRevealEvent(t, recorder.events, systemevent.OutcomeFailure)
			if event.ErrorCode != "invalid_current_password" {
				t.Fatalf("failure event error code = %q, want invalid_current_password", event.ErrorCode)
			}
			assertSecretRevealEventSanitized(t, event, password, "n2api_abc_secret", "n2api_abc")
		})
	}
}

func TestRevealAPIKeySecretThrottleCountsMalformedRequests(t *testing.T) {
	admins := newFakeAdminService()
	throttle := admin.NewSecretRevealThrottle(admin.SecretRevealThrottleConfig{Limit: 1, Window: time.Minute, MaxEntries: 100})
	server := NewServer(config.Config{}, staticHealth{}, admins, nil, throttle)

	request := httptest.NewRequest(http.MethodPost, "/api/admin/keys/7/reveal-secret", strings.NewReader(`{"currentPassword":`))
	request.RemoteAddr = "192.0.2.1:1234"
	request.AddCookie(&http.Cookie{Name: adminSessionCookieName, Value: "valid-session"})
	response := httptest.NewRecorder()
	server.ServeHTTP(response, request)
	if response.Code != http.StatusBadRequest {
		t.Fatalf("malformed status = %d, want 400; body=%s", response.Code, response.Body.String())
	}

	blocked := performSecretRevealRequest(server, 7, "secret", true)
	if blocked.Code != http.StatusTooManyRequests {
		t.Fatalf("status after malformed attempt = %d, want 429; body=%s", blocked.Code, blocked.Body.String())
	}
	if admins.reveal.calls != 0 {
		t.Fatalf("password verification calls = %d, want 0", admins.reveal.calls)
	}
}

func TestRevealAPIKeySecretReturnsNotFoundForMissingOrRevokedKey(t *testing.T) {
	admins := newFakeAdminService()
	server := NewServer(config.Config{}, staticHealth{}, admins, nil)
	response := performSecretRevealRequest(server, 99, "secret", true)
	if response.Code != http.StatusNotFound || response.Body.String() != "{\"error\":\"not_found\"}\n" {
		t.Fatalf("response = %d %q, want not found", response.Code, response.Body.String())
	}
}

func TestRevealAPIKeySecretThrottleCountsSuccessesAndFailures(t *testing.T) {
	tests := []struct {
		name      string
		password  string
		firstCode int
	}{
		{name: "successes", password: "secret", firstCode: http.StatusOK},
		{name: "password failures", password: "wrong-password", firstCode: http.StatusBadRequest},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			admins := newFakeAdminService()
			throttle := admin.NewSecretRevealThrottle(admin.SecretRevealThrottleConfig{Limit: 2, Window: time.Minute, MaxEntries: 100})
			server := NewServer(config.Config{}, staticHealth{}, admins, nil, throttle)
			for range 2 {
				response := performSecretRevealRequest(server, 7, test.password, true)
				if response.Code != test.firstCode {
					t.Fatalf("admitted status = %d, want %d", response.Code, test.firstCode)
				}
			}
			firstBlocked := performSecretRevealRequest(server, 7, test.password, true)
			secondBlocked := performSecretRevealRequest(server, 7, test.password, true)
			if firstBlocked.Code != http.StatusTooManyRequests || secondBlocked.Code != http.StatusTooManyRequests {
				t.Fatalf("blocked statuses = %d/%d, want 429/429", firstBlocked.Code, secondBlocked.Code)
			}
			if firstBlocked.Header().Get("Retry-After") != "60" || secondBlocked.Header().Get("Retry-After") != "60" {
				t.Fatalf("Retry-After = %q/%q, want stable 60", firstBlocked.Header().Get("Retry-After"), secondBlocked.Header().Get("Retry-After"))
			}
			if admins.reveal.calls != 2 {
				t.Fatalf("password verification calls = %d, want 2", admins.reveal.calls)
			}
		})
	}
}

func TestRevealAPIKeySecretEventFailureDoesNotReturnSensitiveValues(t *testing.T) {
	admins := newFakeAdminService()
	admins.reveal.secret = "n2api_event_failure_secret"
	recorder := &memorySystemEventRecorder{err: errors.New("event store raw failure")}
	server := NewServer(config.Config{}, staticHealth{}, admins, nil, recorder)
	response := performSecretRevealRequest(server, 7, "secret", true)

	if response.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, want 500", response.Code)
	}
	for _, sensitive := range []string{"secret", admins.reveal.secret, "event store raw failure", "n2api_abc"} {
		if strings.Contains(response.Body.String(), sensitive) {
			t.Fatalf("response leaked %q: %s", sensitive, response.Body.String())
		}
	}
}

func performSecretRevealRequest(handler http.Handler, keyID int64, password string, authenticated bool) *httptest.ResponseRecorder {
	body := `{"currentPassword":` + strconvQuote(password) + `}`
	request := httptest.NewRequest(http.MethodPost, "/api/admin/keys/"+strconv.FormatInt(keyID, 10)+"/reveal-secret", strings.NewReader(body))
	request.RemoteAddr = "192.0.2.1:1234"
	request.Header.Set("Content-Type", "application/json")
	if authenticated {
		request.AddCookie(&http.Cookie{Name: adminSessionCookieName, Value: "valid-session"})
	}
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	return response
}

func strconvQuote(value string) string {
	encoded, _ := json.Marshal(value)
	return string(encoded)
}

func findSecretRevealEvent(t *testing.T, events []systemevent.Event, outcome systemevent.Outcome) systemevent.Event {
	t.Helper()
	for _, event := range events {
		if event.Action == systemevent.ActionAPIKeySecretViewed && event.Outcome == outcome {
			return event
		}
	}
	t.Fatalf("secret reveal %s event not found in %+v", outcome, events)
	return systemevent.Event{}
}

func assertSecretRevealEventSanitized(t *testing.T, event systemevent.Event, sensitive ...string) {
	t.Helper()
	encoded, err := json.Marshal(event)
	if err != nil {
		t.Fatalf("marshal event: %v", err)
	}
	for _, value := range sensitive {
		if value != "" && strings.Contains(string(encoded), value) {
			t.Fatalf("event leaked %q: %s", value, encoded)
		}
	}
}

package alerting

import (
	"context"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/KnowSky404/N2API/backend/internal/systemevent"
)

func TestHTTPAdapterDeliversGenericWebhookWithBoundedFields(t *testing.T) {
	want := testNotification()
	const querySecret = "query-secret-canary"
	var got Notification
	var gotMethod, gotRequestURI, gotContentType, gotUserAgent, gotDeliveryID string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotMethod = r.Method
		gotRequestURI = r.RequestURI
		gotContentType = r.Header.Get("Content-Type")
		gotUserAgent = r.Header.Get("User-Agent")
		gotDeliveryID = r.Header.Get("X-N2API-Delivery-ID")
		if err := json.NewDecoder(r.Body).Decode(&got); err != nil {
			t.Errorf("decode generic webhook body: %v", err)
		}
		w.WriteHeader(http.StatusAccepted)
	}))
	defer server.Close()

	result := NewHTTPAdapter(server.Client()).Deliver(context.Background(), ResolvedAction{
		ID: 7, Kind: ActionKindGenericWebhook, Enabled: true,
		Destination: server.URL + "/hook?auth=" + querySecret + "&tenant=personal",
	}, want)

	if !result.Success || result.StatusCode != http.StatusAccepted || result.Retryable || result.ErrorCode != "" {
		t.Fatalf("Deliver result = %+v, want successful 202", result)
	}
	if got != want {
		t.Fatalf("generic webhook notification = %+v, want %+v", got, want)
	}
	if gotMethod != http.MethodPost || gotRequestURI != "/hook?auth="+querySecret+"&tenant=personal" {
		t.Fatalf("generic webhook method/URI = %q %q", gotMethod, gotRequestURI)
	}
	if gotContentType != "application/json" || gotUserAgent != "N2API-alert-delivery/1" || gotDeliveryID != want.DeliveryID {
		t.Fatalf("generic webhook headers content-type=%q user-agent=%q delivery-id=%q", gotContentType, gotUserAgent, gotDeliveryID)
	}
	if strings.Contains(fmt.Sprintf("%+v", result), querySecret) {
		t.Fatalf("delivery result exposed destination query secret: %+v", result)
	}
}

func TestHTTPAdapterDeliversNtfyPlainText(t *testing.T) {
	notification := testNotification()
	notification.RuleName = "OAuth health"
	notification.Severity = systemevent.SeverityError
	notification.Message = "refresh failed"
	var gotBody, gotContentType, gotTitle, gotPriority, gotTags, gotDeliveryID string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, err := io.ReadAll(r.Body)
		if err != nil {
			t.Errorf("read ntfy body: %v", err)
		}
		gotBody = string(body)
		gotContentType = r.Header.Get("Content-Type")
		gotTitle = r.Header.Get("X-Title")
		gotPriority = r.Header.Get("X-Priority")
		gotTags = r.Header.Get("X-Tags")
		gotDeliveryID = r.Header.Get("X-N2API-Delivery-ID")
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	result := NewHTTPAdapter(server.Client()).Deliver(context.Background(), ResolvedAction{
		ID: 8, Kind: ActionKindNtfy, Enabled: true, Destination: server.URL + "/personal-topic",
	}, notification)

	if !result.Success || result.StatusCode != http.StatusOK {
		t.Fatalf("Deliver result = %+v, want successful ntfy request", result)
	}
	wantBody := "error: " + string(notification.Action) + " - refresh failed"
	if gotBody != wantBody || gotContentType != "text/plain; charset=utf-8" {
		t.Fatalf("ntfy body/content-type = %q / %q, want %q", gotBody, gotContentType, wantBody)
	}
	if gotTitle != "N2API alert: OAuth health" || gotPriority != "5" || gotTags != "warning" || gotDeliveryID != notification.DeliveryID {
		t.Fatalf("ntfy headers title=%q priority=%q tags=%q delivery-id=%q", gotTitle, gotPriority, gotTags, gotDeliveryID)
	}
}

func TestHTTPAdapterAcceptsAnyTwoHundredStatus(t *testing.T) {
	for _, statusCode := range []int{200, 201, 202, 204, 206, 299} {
		t.Run(fmt.Sprintf("status_%d", statusCode), func(t *testing.T) {
			body := &trackingBody{reader: strings.NewReader("ok")}
			adapter := NewHTTPAdapter(&http.Client{Transport: deliveryRoundTripFunc(func(req *http.Request) (*http.Response, error) {
				return &http.Response{StatusCode: statusCode, Header: make(http.Header), Body: body, Request: req}, nil
			})})

			result := adapter.Deliver(context.Background(), testResolvedAction(), testNotification())

			if !result.Success || result.StatusCode != statusCode || result.Retryable || result.ErrorCode != "" {
				t.Fatalf("Deliver result = %+v, want successful %d", result, statusCode)
			}
			if !body.closed {
				t.Fatal("response body was not closed")
			}
		})
	}
}

func TestHTTPAdapterClassifiesRetryableHTTPStatuses(t *testing.T) {
	for _, statusCode := range []int{http.StatusRequestTimeout, http.StatusTooEarly, http.StatusTooManyRequests, 500, 501, 502, 503, 504, 599} {
		t.Run(fmt.Sprintf("status_%d", statusCode), func(t *testing.T) {
			headers := make(http.Header)
			if statusCode == http.StatusTooManyRequests {
				headers.Set("Retry-After", "2")
			}
			adapter := NewHTTPAdapter(&http.Client{Transport: deliveryRoundTripFunc(func(req *http.Request) (*http.Response, error) {
				return &http.Response{StatusCode: statusCode, Header: headers, Body: http.NoBody, Request: req}, nil
			})})

			result := adapter.Deliver(context.Background(), testResolvedAction(), testNotification())

			if result.Success || !result.Retryable || result.StatusCode != statusCode || result.ErrorCode != "alert_delivery_http_status" {
				t.Fatalf("Deliver result = %+v, want retryable HTTP status", result)
			}
			if statusCode == http.StatusTooManyRequests && result.RetryAfter != 2*time.Second {
				t.Fatalf("RetryAfter = %s, want 2s", result.RetryAfter)
			}
		})
	}
}

func TestHTTPAdapterClassifiesPermanentHTTPStatuses(t *testing.T) {
	for _, statusCode := range []int{300, 301, 307, 308, 400, 401, 403, 404, 409, 422, 499} {
		t.Run(fmt.Sprintf("status_%d", statusCode), func(t *testing.T) {
			adapter := NewHTTPAdapter(&http.Client{Transport: deliveryRoundTripFunc(func(req *http.Request) (*http.Response, error) {
				return &http.Response{StatusCode: statusCode, Header: make(http.Header), Body: http.NoBody, Request: req}, nil
			})})

			result := adapter.Deliver(context.Background(), testResolvedAction(), testNotification())

			if result.Success || result.Retryable || result.StatusCode != statusCode || result.ErrorCode != "alert_delivery_http_status" {
				t.Fatalf("Deliver result = %+v, want permanent HTTP status", result)
			}
		})
	}
}

func TestHTTPAdapterClassifiesNetworkAndTimeoutErrorsWithoutDestination(t *testing.T) {
	const querySecret = "network-secret-canary"
	tests := []struct {
		name     string
		err      error
		wantCode string
	}{
		{name: "network", err: errors.New("connection reset"), wantCode: "alert_delivery_network"},
		{name: "timeout", err: deliveryTimeoutError{}, wantCode: "alert_delivery_timeout"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			adapter := NewHTTPAdapter(&http.Client{Transport: deliveryRoundTripFunc(func(*http.Request) (*http.Response, error) {
				return nil, test.err
			})})
			action := testResolvedAction()
			action.Destination += "?auth=" + querySecret

			result := adapter.Deliver(context.Background(), action, testNotification())

			if result.Success || !result.Retryable || result.ErrorCode != test.wantCode {
				t.Fatalf("Deliver result = %+v, want retryable %q", result, test.wantCode)
			}
			if strings.Contains(fmt.Sprintf("%+v", result), querySecret) {
				t.Fatalf("delivery result exposed destination query secret: %+v", result)
			}
		})
	}
}

func TestHTTPAdapterDoesNotFollowRedirects(t *testing.T) {
	var redirectedRequests atomic.Int64
	target := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		redirectedRequests.Add(1)
	}))
	defer target.Close()
	source := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, target.URL+"/stolen", http.StatusTemporaryRedirect)
	}))
	defer source.Close()

	result := NewHTTPAdapter(source.Client()).Deliver(context.Background(), ResolvedAction{
		ID: 7, Kind: ActionKindGenericWebhook, Enabled: true, Destination: source.URL + "/hook?auth=query-secret",
	}, testNotification())

	if result.Success || result.Retryable || result.StatusCode != http.StatusTemporaryRedirect {
		t.Fatalf("Deliver result = %+v, want permanent 307", result)
	}
	if got := redirectedRequests.Load(); got != 0 {
		t.Fatalf("redirect target requests = %d, want 0", got)
	}
}

func TestParseRetryAfterAndRetryDelayCap(t *testing.T) {
	now := time.Date(2026, time.July, 21, 12, 0, 0, 0, time.UTC)
	tests := []struct {
		name  string
		value string
		want  time.Duration
	}{
		{name: "empty", value: "", want: 0},
		{name: "zero", value: "0", want: 0},
		{name: "seconds", value: " 15 ", want: 15 * time.Second},
		{name: "future date", value: now.Add(45 * time.Second).Format(http.TimeFormat), want: 45 * time.Second},
		{name: "past date", value: now.Add(-time.Second).Format(http.TimeFormat), want: 0},
		{name: "negative", value: "-1", want: 0},
		{name: "invalid", value: "later", want: 0},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if got := parseRetryAfter(test.value, now); got != test.want {
				t.Fatalf("parseRetryAfter(%q) = %s, want %s", test.value, got, test.want)
			}
		})
	}

	parsed := parseRetryAfter("9223372037", now)
	if got := retryDelay(250*time.Millisecond, 2*time.Second, 1, parsed); got != 2*time.Second {
		t.Fatalf("retryDelay with oversized Retry-After = %s (parsed %s), want 2s cap", got, parsed)
	}
}

func TestHTTPAdapterBoundsResponseDrainAndClosesBody(t *testing.T) {
	body := &trackingBody{reader: strings.NewReader(strings.Repeat("x", maxDeliveryResponseDrain*2))}
	adapter := NewHTTPAdapter(&http.Client{Transport: deliveryRoundTripFunc(func(req *http.Request) (*http.Response, error) {
		return &http.Response{StatusCode: http.StatusBadGateway, Header: make(http.Header), Body: body, Request: req}, nil
	})})

	result := adapter.Deliver(context.Background(), testResolvedAction(), testNotification())

	if !result.Retryable || result.StatusCode != http.StatusBadGateway {
		t.Fatalf("Deliver result = %+v, want retryable 502", result)
	}
	if body.bytesRead != maxDeliveryResponseDrain+1 {
		t.Fatalf("response bytes read = %d, want %d", body.bytesRead, maxDeliveryResponseDrain+1)
	}
	if !body.closed {
		t.Fatal("response body was not closed")
	}
}

func TestNotificationForHasStableDeliveryID(t *testing.T) {
	rule := Rule{ID: 17, Name: "OAuth failures"}
	event := systemevent.Event{
		ID: 91, OccurredAt: time.Date(2026, time.July, 21, 12, 0, 0, 0, time.UTC),
		Category: systemevent.CategoryOAuth, Severity: systemevent.SeverityError,
		Action: systemevent.ActionOAuthRefreshAutomaticFailed, Outcome: systemevent.OutcomeFailure,
		Target:        systemevent.Target{Type: "provider_account", ID: "9", Name: "personal"},
		CorrelationID: "delivery-correlation", ErrorCode: "oauth_refresh_failed", Message: "refresh failed",
	}

	first := notificationFor(rule, DecisionNotify, event)
	second := notificationFor(rule, DecisionNotify, event)
	if first != second || first.DeliveryID == "" {
		t.Fatalf("notificationFor was not stable: first=%+v second=%+v", first, second)
	}
	if len(first.DeliveryID) != 32 {
		t.Fatalf("delivery ID length = %d, want 32", len(first.DeliveryID))
	}
	if _, err := hex.DecodeString(first.DeliveryID); err != nil {
		t.Fatalf("delivery ID is not hex: %q", first.DeliveryID)
	}
	changedEvent := event
	changedEvent.ID++
	if notificationFor(rule, DecisionNotify, changedEvent).DeliveryID == first.DeliveryID ||
		notificationFor(rule, DecisionRecover, event).DeliveryID == first.DeliveryID {
		t.Fatal("delivery ID did not change with event ID or decision")
	}
}

func testNotification() Notification {
	return Notification{
		DeliveryID: "0123456789abcdef0123456789abcdef", Decision: DecisionNotify,
		RuleID: 17, RuleName: "Repeated OAuth refresh failure",
		OccurredAt: time.Date(2026, time.July, 21, 12, 0, 0, 0, time.UTC),
		Category:   systemevent.CategoryOAuth, Severity: systemevent.SeverityError,
		Action: systemevent.ActionOAuthRefreshAutomaticFailed, Outcome: systemevent.OutcomeFailure,
		Target:    systemevent.Target{Type: "provider_account", ID: "9", Name: "personal"},
		ErrorCode: "oauth_refresh_failed", Message: "refresh failed", CorrelationID: "delivery-correlation",
	}
}

func testResolvedAction() ResolvedAction {
	return ResolvedAction{
		ID: 7, Kind: ActionKindGenericWebhook, Enabled: true, Destination: "https://webhook.example.test/notify",
	}
}

type deliveryRoundTripFunc func(*http.Request) (*http.Response, error)

func (function deliveryRoundTripFunc) RoundTrip(request *http.Request) (*http.Response, error) {
	return function(request)
}

type deliveryTimeoutError struct{}

func (deliveryTimeoutError) Error() string   { return "timed out" }
func (deliveryTimeoutError) Timeout() bool   { return true }
func (deliveryTimeoutError) Temporary() bool { return true }

type trackingBody struct {
	reader    io.Reader
	bytesRead int
	closed    bool
}

func (body *trackingBody) Read(destination []byte) (int, error) {
	count, err := body.reader.Read(destination)
	body.bytesRead += count
	return count, err
}

func (body *trackingBody) Close() error {
	body.closed = true
	return nil
}

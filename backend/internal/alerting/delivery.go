package alerting

import (
	"bytes"
	"context"
	"crypto/sha256"
	"crypto/tls"
	"encoding/hex"
	"encoding/json"
	"io"
	"net"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/KnowSky404/N2API/backend/internal/systemevent"
)

const maxDeliveryResponseDrain = 8 * 1024

type Notification struct {
	DeliveryID    string               `json:"deliveryId"`
	Decision      Decision             `json:"decision"`
	RuleID        int64                `json:"ruleId"`
	RuleName      string               `json:"ruleName"`
	OccurredAt    time.Time            `json:"occurredAt"`
	Category      systemevent.Category `json:"category"`
	Severity      systemevent.Severity `json:"severity"`
	Action        systemevent.Action   `json:"action"`
	Outcome       systemevent.Outcome  `json:"outcome"`
	Target        systemevent.Target   `json:"target"`
	ErrorCode     string               `json:"errorCode,omitempty"`
	Message       string               `json:"message,omitempty"`
	CorrelationID string               `json:"correlationId"`
}

type DeliveryAttempt struct {
	Success    bool
	Retryable  bool
	StatusCode int
	ErrorCode  string
	RetryAfter time.Duration
}

type DeliveryAdapter interface {
	Deliver(context.Context, ResolvedAction, Notification) DeliveryAttempt
}

type HTTPAdapter struct {
	client *http.Client
	now    func() time.Time
}

func NewHTTPAdapter(client *http.Client) *HTTPAdapter {
	if client == nil {
		client = &http.Client{
			Timeout: 5 * time.Second,
			Transport: &http.Transport{
				Proxy:                  nil,
				DialContext:            (&net.Dialer{Timeout: 2 * time.Second, KeepAlive: 30 * time.Second}).DialContext,
				ForceAttemptHTTP2:      true,
				MaxIdleConns:           8,
				MaxIdleConnsPerHost:    2,
				MaxConnsPerHost:        2,
				IdleConnTimeout:        30 * time.Second,
				TLSHandshakeTimeout:    3 * time.Second,
				ResponseHeaderTimeout:  3 * time.Second,
				MaxResponseHeaderBytes: 64 * 1024,
				TLSClientConfig:        &tls.Config{MinVersion: tls.VersionTLS12},
			},
		}
	} else {
		clone := *client
		client = &clone
		if client.Timeout <= 0 {
			client.Timeout = 5 * time.Second
		}
	}
	client.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	return &HTTPAdapter{client: client, now: time.Now}
}

func (adapter *HTTPAdapter) Deliver(ctx context.Context, action ResolvedAction, notification Notification) DeliveryAttempt {
	body, contentType, headers, errCode := deliveryRequestBody(action.Kind, notification)
	if errCode != "" {
		return DeliveryAttempt{ErrorCode: errCode}
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, action.Destination, bytes.NewReader(body))
	if err != nil {
		return DeliveryAttempt{ErrorCode: "alert_delivery_request_invalid"}
	}
	req.Header.Set("Content-Type", contentType)
	req.Header.Set("User-Agent", "N2API-alert-delivery/1")
	req.Header.Set("X-N2API-Delivery-ID", notification.DeliveryID)
	for key, value := range headers {
		req.Header.Set(key, value)
	}

	resp, err := adapter.client.Do(req)
	if err != nil {
		if ctx.Err() != nil {
			return DeliveryAttempt{ErrorCode: "alert_delivery_canceled"}
		}
		if timeout, ok := err.(interface{ Timeout() bool }); ok && timeout.Timeout() {
			return DeliveryAttempt{Retryable: true, ErrorCode: "alert_delivery_timeout"}
		}
		return DeliveryAttempt{Retryable: true, ErrorCode: "alert_delivery_network"}
	}
	_, _ = io.CopyN(io.Discard, resp.Body, maxDeliveryResponseDrain+1)
	_ = resp.Body.Close()

	result := DeliveryAttempt{StatusCode: resp.StatusCode}
	if resp.StatusCode >= 200 && resp.StatusCode < 300 {
		result.Success = true
		return result
	}
	result.ErrorCode = "alert_delivery_http_status"
	result.Retryable = resp.StatusCode == http.StatusRequestTimeout || resp.StatusCode == http.StatusTooEarly ||
		resp.StatusCode == http.StatusTooManyRequests || resp.StatusCode >= 500
	if result.Retryable {
		result.RetryAfter = parseRetryAfter(resp.Header.Get("Retry-After"), adapter.now())
	}
	return result
}

func deliveryRequestBody(kind ActionKind, notification Notification) ([]byte, string, map[string]string, string) {
	switch kind {
	case ActionKindGenericWebhook:
		body, err := json.Marshal(notification)
		if err != nil {
			return nil, "", nil, "alert_delivery_payload_invalid"
		}
		return body, "application/json", nil, ""
	case ActionKindNtfy:
		summary := string(notification.Severity) + ": " + string(notification.Action)
		if notification.Message != "" {
			summary += " - " + notification.Message
		}
		if len(summary) > 4096 {
			summary = summary[:4096]
		}
		headers := map[string]string{
			"X-Title":    "N2API alert: " + notification.RuleName,
			"X-Priority": ntfyPriority(notification.Severity),
			"X-Tags":     "warning",
		}
		return []byte(summary), "text/plain; charset=utf-8", headers, ""
	default:
		return nil, "", nil, "alert_delivery_adapter_unsupported"
	}
}

func ntfyPriority(severity systemevent.Severity) string {
	switch severity {
	case systemevent.SeverityError:
		return "5"
	case systemevent.SeverityWarning:
		return "4"
	default:
		return "3"
	}
}

func parseRetryAfter(value string, now time.Time) time.Duration {
	value = strings.TrimSpace(value)
	if value == "" {
		return 0
	}
	if seconds, err := strconv.ParseInt(value, 10, 64); err == nil && seconds >= 0 {
		const maxDuration = time.Duration(1<<63 - 1)
		if seconds > int64(maxDuration/time.Second) {
			return maxDuration
		}
		return time.Duration(seconds) * time.Second
	}
	if deadline, err := http.ParseTime(value); err == nil && deadline.After(now) {
		return deadline.Sub(now)
	}
	return 0
}

func notificationFor(rule Rule, decision Decision, event systemevent.Event) Notification {
	digest := sha256.Sum256([]byte(strconv.FormatInt(rule.ID, 10) + "\x00" + strconv.FormatInt(event.ID, 10) + "\x00" + string(decision)))
	return Notification{
		DeliveryID: hex.EncodeToString(digest[:16]), Decision: decision, RuleID: rule.ID, RuleName: rule.Name,
		OccurredAt: event.OccurredAt, Category: event.Category, Severity: event.Severity, Action: event.Action,
		Outcome: event.Outcome, Target: event.Target, ErrorCode: event.ErrorCode, Message: event.Message,
		CorrelationID: event.CorrelationID,
	}
}

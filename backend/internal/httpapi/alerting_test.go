package httpapi

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/KnowSky404/N2API/backend/internal/alerting"
	"github.com/KnowSky404/N2API/backend/internal/config"
	"github.com/KnowSky404/N2API/backend/internal/systemevent"
)

type fakeAlertingAdminService struct {
	actions              []alerting.Action
	rules                []alerting.Rule
	action               alerting.Action
	rule                 alerting.Rule
	err                  error
	updateActionID       int64
	updateActionInput    alerting.ActionUpdateInput
	updateRuleID         int64
	updateRule           alerting.Rule
	updateRuleExpectedAt time.Time
	installTemplateKey   string
	installActionID      int64
	installCreated       bool
}

func (service *fakeAlertingAdminService) ListActions(context.Context) ([]alerting.Action, error) {
	return service.actions, service.err
}
func (service *fakeAlertingAdminService) CreateAction(context.Context, alerting.ActionInput) (alerting.Action, error) {
	return service.action, service.err
}
func (service *fakeAlertingAdminService) UpdateAction(_ context.Context, id int64, input alerting.ActionUpdateInput) (alerting.Action, error) {
	service.updateActionID, service.updateActionInput = id, input
	return service.action, service.err
}
func (service *fakeAlertingAdminService) DeleteAction(context.Context, int64) error {
	return service.err
}
func (service *fakeAlertingAdminService) ListRules(context.Context) ([]alerting.Rule, error) {
	return service.rules, service.err
}
func (service *fakeAlertingAdminService) CreateRule(context.Context, alerting.Rule) (alerting.Rule, error) {
	return service.rule, service.err
}
func (service *fakeAlertingAdminService) UpdateRule(_ context.Context, id int64, rule alerting.Rule, expected time.Time) (alerting.Rule, error) {
	service.updateRuleID, service.updateRule, service.updateRuleExpectedAt = id, rule, expected
	return service.rule, service.err
}
func (service *fakeAlertingAdminService) DeleteRule(context.Context, int64) error {
	return service.err
}
func (service *fakeAlertingAdminService) InstallRuleTemplate(_ context.Context, key string, actionID int64) (alerting.Rule, bool, error) {
	service.installTemplateKey, service.installActionID = key, actionID
	return service.rule, service.installCreated, service.err
}

type fakeAlertActionTester struct {
	result   alerting.ActionTestResult
	err      error
	id       int64
	expected time.Time
}

func (tester *fakeAlertActionTester) TestAction(_ context.Context, id int64, expected time.Time) (alerting.ActionTestResult, error) {
	tester.id, tester.expected = id, expected
	return tester.result, tester.err
}

func TestAlertingAdminRoutesRequireAuthentication(t *testing.T) {
	server := NewServer(config.Config{}, staticHealth{}, newFakeAdminService(), nil, &fakeAlertingAdminService{})
	for _, request := range []*http.Request{
		httptest.NewRequest(http.MethodGet, "/api/admin/alert-actions", nil),
		httptest.NewRequest(http.MethodPost, "/api/admin/alert-actions/7/test", strings.NewReader(`{"expectedUpdatedAt":"2026-07-21T12:00:00Z"}`)),
		httptest.NewRequest(http.MethodGet, "/api/admin/alert-rules", nil),
		httptest.NewRequest(http.MethodGet, "/api/admin/alert-rule-templates", nil),
		httptest.NewRequest(http.MethodPost, "/api/admin/alert-rule-templates/oauth-refresh-repeated-v1/install", strings.NewReader(`{"actionId":7}`)),
	} {
		response := httptest.NewRecorder()
		server.ServeHTTP(response, request)
		if response.Code != http.StatusUnauthorized {
			t.Fatalf("%s %s status = %d, want 401", request.Method, request.URL.Path, response.Code)
		}
	}
}

func TestAlertRuleTemplateCatalogAndInstallRoutes(t *testing.T) {
	service := &fakeAlertingAdminService{rule: alerting.Rule{ID: 9, TemplateKey: alerting.OAuthRefreshRepeatedTemplateKey, ActionID: 7}}
	server := NewServer(config.Config{}, staticHealth{}, newFakeAdminService(), nil, service)

	response := httptest.NewRecorder()
	server.ServeHTTP(response, authenticatedAlertingRequest(http.MethodGet, "/api/admin/alert-rule-templates", ""))
	if response.Code != http.StatusOK {
		t.Fatalf("catalog status=%d body=%s", response.Code, response.Body.String())
	}
	var catalog struct {
		Templates []alerting.RuleTemplate `json:"templates"`
	}
	if err := json.NewDecoder(response.Body).Decode(&catalog); err != nil || len(catalog.Templates) != 6 ||
		catalog.Templates[0].Key != alerting.OAuthRefreshRepeatedTemplateKey || catalog.Templates[1].Key != alerting.RequestLogRetentionFailedTemplateKey ||
		catalog.Templates[2].Key != alerting.ProviderAutoTestFailedTemplateKey || catalog.Templates[3].Key != alerting.ProviderAccountExpiredTemplateKey ||
		catalog.Templates[4].Key != alerting.ProviderAccountCircuitOpenTemplateKey || catalog.Templates[5].Key != alerting.APIKeyBudget80PercentTemplateKey {
		t.Fatalf("catalog=%+v err=%v", catalog, err)
	}

	service.rule.TemplateKey = alerting.RequestLogRetentionFailedTemplateKey
	service.installCreated = true
	response = httptest.NewRecorder()
	server.ServeHTTP(response, authenticatedAlertingRequest(http.MethodPost, "/api/admin/alert-rule-templates/request-log-retention-failed-v1/install", `{"actionId":7}`))
	if response.Code != http.StatusCreated || service.installTemplateKey != alerting.RequestLogRetentionFailedTemplateKey || service.installActionID != 7 ||
		!strings.Contains(response.Body.String(), `"created":true`) || !strings.Contains(response.Body.String(), `"templateKey":"request-log-retention-failed-v1"`) {
		t.Fatalf("created install status=%d body=%s service=%+v", response.Code, response.Body.String(), service)
	}

	service.rule.TemplateKey = alerting.OAuthRefreshRepeatedTemplateKey
	service.installCreated = false
	response = httptest.NewRecorder()
	server.ServeHTTP(response, authenticatedAlertingRequest(http.MethodPost, "/api/admin/alert-rule-templates/oauth-refresh-repeated-v1/install", `{"actionId":7}`))
	if response.Code != http.StatusOK || !strings.Contains(response.Body.String(), `"created":false`) {
		t.Fatalf("existing install status=%d body=%s", response.Code, response.Body.String())
	}

	response = httptest.NewRecorder()
	server.ServeHTTP(response, authenticatedAlertingRequest(http.MethodPost, "/api/admin/alert-rule-templates/oauth-refresh-repeated-v1/install", `{"actionId":7,"extra":true}`))
	if response.Code != http.StatusBadRequest || !strings.Contains(response.Body.String(), "bad_request") {
		t.Fatalf("strict install status=%d body=%s", response.Code, response.Body.String())
	}
}

func TestAlertActionRoutesRedactResponsesAndPreserveOmittedDestination(t *testing.T) {
	revision := time.Date(2026, time.July, 21, 12, 0, 0, 123, time.UTC)
	service := &fakeAlertingAdminService{action: alerting.Action{
		ID: 7, Name: "primary", Kind: alerting.ActionKindGenericWebhook, Enabled: true,
		DestinationConfigured: true, UpdatedAt: revision,
	}}
	server := NewServer(config.Config{}, staticHealth{}, newFakeAdminService(), nil, service)
	body := fmt.Sprintf(`{"name":"primary","kind":"generic_webhook","enabled":false,"expectedUpdatedAt":%q}`, revision.Format(time.RFC3339Nano))
	request := authenticatedAlertingRequest(http.MethodPatch, "/api/admin/alert-actions/7", body)
	response := httptest.NewRecorder()
	server.ServeHTTP(response, request)
	if response.Code != http.StatusOK {
		t.Fatalf("status = %d body=%s", response.Code, response.Body.String())
	}
	if service.updateActionID != 7 || service.updateActionInput.Destination != nil || !service.updateActionInput.ExpectedUpdatedAt.Equal(revision) {
		t.Fatalf("update input = %+v id=%d", service.updateActionInput, service.updateActionID)
	}
	for _, forbidden := range []string{"destination\"", "encrypted", "secret.example"} {
		if strings.Contains(response.Body.String(), forbidden) {
			t.Fatalf("response leaked %q: %s", forbidden, response.Body.String())
		}
	}
}

func TestAlertActionPatchRejectsNullDestination(t *testing.T) {
	service := &fakeAlertingAdminService{}
	server := NewServer(config.Config{}, staticHealth{}, newFakeAdminService(), nil, service)
	request := authenticatedAlertingRequest(http.MethodPatch, "/api/admin/alert-actions/7", `{"name":"primary","kind":"generic_webhook","destination":null,"enabled":true,"expectedUpdatedAt":"2026-07-21T12:00:00Z"}`)
	response := httptest.NewRecorder()
	server.ServeHTTP(response, request)
	if response.Code != http.StatusBadRequest || !strings.Contains(response.Body.String(), "invalid_input") {
		t.Fatalf("status = %d body=%s", response.Code, response.Body.String())
	}
	if service.updateActionID != 0 {
		t.Fatalf("UpdateAction called with id = %d", service.updateActionID)
	}
}

func TestAlertingRoutesRejectUnknownFieldsAndMapConflicts(t *testing.T) {
	service := &fakeAlertingAdminService{}
	recorder := &memorySystemEventRecorder{}
	server := NewServer(config.Config{}, staticHealth{}, newFakeAdminService(), nil, service, recorder)
	response := httptest.NewRecorder()
	server.ServeHTTP(response, authenticatedAlertingRequest(http.MethodPost, "/api/admin/alert-actions", `{"name":"x","kind":"generic_webhook","destination":"https://example.test","enabled":true,"extra":true}`))
	if response.Code != http.StatusBadRequest || !strings.Contains(response.Body.String(), "bad_request") {
		t.Fatalf("unknown field status=%d body=%s", response.Code, response.Body.String())
	}

	service.err = alerting.ErrConflict
	response = httptest.NewRecorder()
	server.ServeHTTP(response, authenticatedAlertingRequest(http.MethodPatch, "/api/admin/alert-actions/7", `{"name":"x","kind":"generic_webhook","enabled":true,"expectedUpdatedAt":"2026-07-21T12:00:00Z"}`))
	if response.Code != http.StatusConflict || !strings.Contains(response.Body.String(), "stale_update") {
		t.Fatalf("stale status=%d body=%s", response.Code, response.Body.String())
	}
	response = httptest.NewRecorder()
	server.ServeHTTP(response, authenticatedAlertingRequest(http.MethodDelete, "/api/admin/alert-actions/7", ""))
	if response.Code != http.StatusConflict || !strings.Contains(response.Body.String(), `"conflict"`) {
		t.Fatalf("delete conflict status=%d body=%s", response.Code, response.Body.String())
	}
	if len(recorder.events) != 3 {
		t.Fatalf("failure audit events = %d, want 3", len(recorder.events))
	}
	if event := recorder.events[1]; event.Action != systemevent.ActionAlertActionUpdated || event.Outcome != systemevent.OutcomeFailure || event.Target.ID != "7" {
		t.Fatalf("patch failure event = %+v", event)
	}
	if event := recorder.events[2]; event.Action != systemevent.ActionAlertActionDeleted || event.Outcome != systemevent.OutcomeFailure || event.Target.ID != "7" {
		t.Fatalf("delete failure event = %+v", event)
	}
}

func TestAlertActionTestReturnsStructuredFailureAndRateLimit(t *testing.T) {
	revision := time.Date(2026, time.July, 21, 12, 0, 0, 0, time.UTC)
	status := http.StatusServiceUnavailable
	tester := &fakeAlertActionTester{result: alerting.ActionTestResult{
		TestedAt: revision.Add(time.Minute), Status: alerting.ActionTestStatusFailed,
		HTTPStatus: &status, LatencyMS: 125, ErrorCode: "alert_delivery_http_status", Retryable: true,
	}}
	server := NewServer(config.Config{}, staticHealth{}, newFakeAdminService(), nil, &fakeAlertingAdminService{}, tester)
	body := fmt.Sprintf(`{"expectedUpdatedAt":%q}`, revision.Format(time.RFC3339Nano))
	response := httptest.NewRecorder()
	server.ServeHTTP(response, authenticatedAlertingRequest(http.MethodPost, "/api/admin/alert-actions/7/test", body))
	if response.Code != http.StatusOK || tester.id != 7 || !tester.expected.Equal(revision) {
		t.Fatalf("test status=%d body=%s id=%d expected=%s", response.Code, response.Body.String(), tester.id, tester.expected)
	}
	var decoded struct {
		Result alerting.ActionTestResult `json:"result"`
	}
	if err := json.NewDecoder(response.Body).Decode(&decoded); err != nil || decoded.Result.Status != alerting.ActionTestStatusFailed || decoded.Result.HTTPStatus == nil || *decoded.Result.HTTPStatus != 503 {
		t.Fatalf("decoded result=%+v err=%v", decoded.Result, err)
	}

	tester.err = &alerting.RateLimitError{RetryAfter: 20 * time.Second}
	response = httptest.NewRecorder()
	server.ServeHTTP(response, authenticatedAlertingRequest(http.MethodPost, "/api/admin/alert-actions/7/test", body))
	if response.Code != http.StatusTooManyRequests || response.Header().Get("Retry-After") != "20" || !strings.Contains(response.Body.String(), "rate_limited") {
		t.Fatalf("rate limit status=%d retry=%q body=%s", response.Code, response.Header().Get("Retry-After"), response.Body.String())
	}
}

func TestAlertRulePatchCarriesRevisionAndMapsMissingAction(t *testing.T) {
	revision := time.Date(2026, time.July, 21, 12, 0, 0, 0, time.UTC)
	service := &fakeAlertingAdminService{rule: alerting.Rule{ID: 9, Name: "oauth", ActionID: 7, UpdatedAt: revision}}
	server := NewServer(config.Config{}, staticHealth{}, newFakeAdminService(), nil, service)
	body := fmt.Sprintf(`{"name":"oauth","actionId":7,"enabled":true,"category":"oauth","severity":"error","eventAction":"oauth.refresh.automatic.failed","recoveryAction":"","aggregationCount":1,"aggregationWindowSeconds":0,"cooldownSeconds":300,"deduplicationScope":"target","notifyRecovery":false,"expectedUpdatedAt":%q}`, revision.Format(time.RFC3339Nano))
	response := httptest.NewRecorder()
	server.ServeHTTP(response, authenticatedAlertingRequest(http.MethodPatch, "/api/admin/alert-rules/9", body))
	if response.Code != http.StatusOK || service.updateRuleID != 9 || !service.updateRuleExpectedAt.Equal(revision) {
		t.Fatalf("rule patch status=%d body=%s captured=%+v", response.Code, response.Body.String(), service)
	}
	service.err = alerting.ErrNotFound
	response = httptest.NewRecorder()
	server.ServeHTTP(response, authenticatedAlertingRequest(http.MethodPost, "/api/admin/alert-rules", strings.Replace(body, fmt.Sprintf(`,"expectedUpdatedAt":%q`, revision.Format(time.RFC3339Nano)), "", 1)))
	if response.Code != http.StatusNotFound || !strings.Contains(response.Body.String(), "not_found") {
		t.Fatalf("missing action status=%d body=%s", response.Code, response.Body.String())
	}
}

func authenticatedAlertingRequest(method, target, body string) *http.Request {
	request := httptest.NewRequest(method, target, strings.NewReader(body))
	request.Header.Set("Content-Type", "application/json")
	request.AddCookie(&http.Cookie{Name: adminSessionCookieName, Value: "valid-session"})
	return request
}

var _ AlertingAdminService = (*fakeAlertingAdminService)(nil)
var _ AlertActionTester = (*fakeAlertActionTester)(nil)

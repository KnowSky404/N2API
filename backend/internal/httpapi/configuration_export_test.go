package httpapi

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/KnowSky404/N2API/backend/internal/admin"
	"github.com/KnowSky404/N2API/backend/internal/buildinfo"
	"github.com/KnowSky404/N2API/backend/internal/config"
	"github.com/KnowSky404/N2API/backend/internal/systemevent"
)

func TestConfigurationExportReturnsBoundedAuditedAttachment(t *testing.T) {
	admins := newFakeAdminService()
	admins.configurationExport = configurationExportFixture()
	recorder := &memorySystemEventRecorder{}
	build := buildinfo.Info{Version: "sha-0123456789ab", Commit: "0123456789abcdef", BuiltAt: "2026-07-21T12:00:00Z"}
	server := NewServer(config.Config{}, staticHealth{}, admins, nil, recorder, build)

	request := httptest.NewRequest(http.MethodGet, "/api/admin/configuration/export", nil)
	request.AddCookie(&http.Cookie{Name: adminSessionCookieName, Value: "valid-session"})
	response := httptest.NewRecorder()
	server.ServeHTTP(response, request)

	if response.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", response.Code, response.Body.String())
	}
	if got := response.Header().Get("Content-Type"); got != "application/json; charset=utf-8" {
		t.Fatalf("content type = %q", got)
	}
	if got := response.Header().Get("Content-Disposition"); !strings.Contains(got, `filename="n2api-portable-config-v1-`) || !strings.HasSuffix(got, `.json"`) {
		t.Fatalf("content disposition = %q", got)
	}
	if response.Header().Get("Cache-Control") != "no-store" {
		t.Fatalf("cache control = %q", response.Header().Get("Cache-Control"))
	}
	var document configurationExportDocument
	if err := json.Unmarshal(response.Body.Bytes(), &document); err != nil {
		t.Fatalf("decode export: %v", err)
	}
	if document.FormatVersion != 1 || document.Application != build || document.ExportedAt.IsZero() {
		t.Fatalf("document identity = %+v", document)
	}
	if document.UnsupportedSections == nil || len(document.UnsupportedSections) != 0 {
		t.Fatalf("unsupported sections = %#v", document.UnsupportedSections)
	}
	if !slices.Contains(document.Redactions, "alertActionDestinations") {
		t.Fatalf("redactions = %#v, want alertActionDestinations", document.Redactions)
	}
	if len(document.Configuration.AlertActions) != 1 || len(document.Configuration.AlertRules) != 1 {
		t.Fatalf("alert configuration = actions %#v rules %#v", document.Configuration.AlertActions, document.Configuration.AlertRules)
	}
	if len(recorder.events) != 1 {
		t.Fatalf("events = %d, want 1", len(recorder.events))
	}
	event := recorder.events[0]
	if event.Action != systemevent.ActionConfigurationExported || event.Category != systemevent.CategorySecurity || event.Outcome != systemevent.OutcomeSuccess {
		t.Fatalf("event = %+v", event)
	}
	if event.Metadata["format_version"] != float64(1) && event.Metadata["format_version"] != 1 {
		t.Fatalf("event metadata = %#v", event.Metadata)
	}
	if event.Metadata["alert_action_count"] != float64(1) && event.Metadata["alert_action_count"] != 1 {
		t.Fatalf("event metadata = %#v", event.Metadata)
	}
	if event.Metadata["alert_rule_count"] != float64(1) && event.Metadata["alert_rule_count"] != 1 {
		t.Fatalf("event metadata = %#v", event.Metadata)
	}
	for _, forbidden := range []string{"primary-key-hash-canary", "oauth-access-token-canary", "proxy-url-canary", "fingerprint-header-canary", "alert-destination-canary"} {
		if strings.Contains(response.Body.String(), forbidden) {
			t.Fatalf("export contains forbidden canary %q", forbidden)
		}
	}
}

func TestConfigurationExportRequiresAuthentication(t *testing.T) {
	server := NewServer(config.Config{}, staticHealth{}, newFakeAdminService(), nil)
	request := httptest.NewRequest(http.MethodGet, "/api/admin/configuration/export", nil)
	response := httptest.NewRecorder()
	server.ServeHTTP(response, request)
	if response.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401", response.Code)
	}
}

func TestConfigurationExportFailsClosedWhenAuditInsertFails(t *testing.T) {
	admins := newFakeAdminService()
	admins.configurationExport = configurationExportFixture()
	recorder := &memorySystemEventRecorder{err: errors.New("event unavailable")}
	server := NewServer(config.Config{}, staticHealth{}, admins, nil, recorder)
	request := httptest.NewRequest(http.MethodGet, "/api/admin/configuration/export", nil)
	request.AddCookie(&http.Cookie{Name: adminSessionCookieName, Value: "valid-session"})
	response := httptest.NewRecorder()
	server.ServeHTTP(response, request)
	if response.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, want 500", response.Code)
	}
	if strings.Contains(response.Body.String(), "routingPools") || response.Header().Get("Content-Disposition") != "" {
		t.Fatalf("configuration body escaped audit failure: headers=%v body=%s", response.Header(), response.Body.String())
	}
}

func TestConfigurationExportRecordsSnapshotFailure(t *testing.T) {
	admins := newFakeAdminService()
	admins.configurationErr = errors.New("snapshot failed")
	recorder := &memorySystemEventRecorder{}
	server := NewServer(config.Config{}, staticHealth{}, admins, nil, recorder)
	request := httptest.NewRequest(http.MethodGet, "/api/admin/configuration/export", nil)
	request.AddCookie(&http.Cookie{Name: adminSessionCookieName, Value: "valid-session"})
	response := httptest.NewRecorder()
	server.ServeHTTP(response, request)
	if response.Code != http.StatusInternalServerError || len(recorder.events) != 1 {
		t.Fatalf("status/events = %d/%d", response.Code, len(recorder.events))
	}
	event := recorder.events[0]
	if event.Outcome != systemevent.OutcomeFailure || event.ErrorCode != "configuration_export_failed" {
		t.Fatalf("event = %+v", event)
	}
}

func TestConfigurationExportRejectsOversizedDocument(t *testing.T) {
	admins := newFakeAdminService()
	admins.configurationExport = configurationExportFixture()
	admins.configurationExport.ErrorPassthroughRules = []admin.ConfigurationErrorPassthroughRule{{
		Ref: "error_passthrough_rule:1", Pattern: strings.Repeat("x", configurationExportMaxBytes), MatchType: "contains",
	}}
	recorder := &memorySystemEventRecorder{}
	server := NewServer(config.Config{}, staticHealth{}, admins, nil, recorder)
	request := httptest.NewRequest(http.MethodGet, "/api/admin/configuration/export", nil)
	request.AddCookie(&http.Cookie{Name: adminSessionCookieName, Value: "valid-session"})
	response := httptest.NewRecorder()
	server.ServeHTTP(response, request)
	if response.Code != http.StatusInternalServerError || len(recorder.events) != 1 || recorder.events[0].ErrorCode != "configuration_export_too_large" {
		t.Fatalf("status/event = %d/%+v", response.Code, recorder.events)
	}
}

func configurationExportFixture() admin.ConfigurationSnapshot {
	return admin.ConfigurationSnapshot{
		RoutingPools:          []admin.ConfigurationRoutingPool{{Ref: "routing_pool:1", Name: "primary", Enabled: true, Accounts: []admin.ConfigurationRoutingPoolMembership{}}},
		APIKeyTemplates:       []admin.ConfigurationAPIKeyTemplate{{Ref: "api_key_template:1", Name: "laptop", Enabled: true, ModelPolicy: "all", AllowedModels: []string{}}},
		ProviderAccounts:      []admin.ConfigurationProviderAccount{{Ref: "provider_account:1", Provider: "openai", AccountType: "codex_oauth", Name: "daily", Enabled: true, LoadFactor: 1, Models: []admin.ConfigurationProviderModel{}}},
		ModelSettings:         admin.ModelSettings{DefaultModel: "gpt-5.1-codex"},
		UsagePricing:          admin.UsagePricing{Version: 1, Currency: "USD", Unit: "1M_tokens", UpdatedAt: time.Unix(1, 0).UTC(), Models: map[string]admin.UsagePrice{}},
		GatewaySettings:       admin.GatewaySettings{ProviderAccountAutoTestIntervalSeconds: 300},
		FingerprintProfiles:   []admin.ConfigurationFingerprintProfile{{Ref: "fingerprint_profile:1", Name: "default", Headers: map[string]string{"X-Private": admin.ConfigurationRedactedValue}, Enabled: true}},
		ErrorPassthroughRules: []admin.ConfigurationErrorPassthroughRule{},
		AlertActions: []admin.ConfigurationAlertAction{{
			Ref: "alert_action:1", Name: "Primary webhook", Kind: "generic_webhook", Enabled: true, DestinationConfigured: true,
		}},
		AlertRules: []admin.ConfigurationAlertRule{{
			Ref: "alert_rule:1", Name: "Provider expiry", ActionRef: "alert_action:1", Enabled: true,
			Category: "runtime", Severity: "warning", EventAction: "provider_account.expired", RecoveryAction: "provider_account.recovered",
			AggregationCount: 1, CooldownSeconds: 86400, DeduplicationScope: "target", NotifyRecovery: true,
		}},
	}
}

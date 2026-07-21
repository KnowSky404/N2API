package store

import (
	"context"
	"encoding/json"
	"os"
	"reflect"
	"strings"
	"testing"

	"github.com/KnowSky404/N2API/backend/internal/admin"
)

func TestConfigurationExportQueriesExcludeSensitiveColumns(t *testing.T) {
	source, err := os.ReadFile("configuration_export.go")
	if err != nil {
		t.Fatalf("read configuration export source: %v", err)
	}
	queries := strings.ToLower(string(source))
	for _, column := range []string{
		"key_hash", "encrypted_secret", "encrypted_access_token",
		"encrypted_refresh_token", "encrypted_id_token", "encrypted_api_key",
		"encrypted_proxy_url", "subject", "metadata", "last_refresh_error",
		"encrypted_destination", "last_test_",
	} {
		if strings.Contains(queries, column) {
			t.Fatalf("configuration export source references sensitive column %q", column)
		}
	}
}

func TestSanitizeConfigurationBaseURL(t *testing.T) {
	tests := map[string]string{
		"https://user:password@example.com/v1/?token=secret#private": "https://example.com/v1",
		"https://example.com/v1/":                                    "https://example.com/v1",
		"not-a-url":                                                  "",
		"":                                                           "",
	}
	for input, want := range tests {
		if got := sanitizeConfigurationBaseURL(input); got != want {
			t.Errorf("sanitizeConfigurationBaseURL(%q) = %q, want %q", input, got, want)
		}
	}
}

func TestRedactConfigurationHeadersRedactsEveryValue(t *testing.T) {
	got, err := redactConfigurationHeaders([]byte(`{"Authorization":"secret-canary","X-Harmless":"innocent-canary"}`))
	if err != nil {
		t.Fatalf("redact headers: %v", err)
	}
	want := map[string]string{
		"Authorization": admin.ConfigurationRedactedValue,
		"X-Harmless":    admin.ConfigurationRedactedValue,
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("headers = %#v, want %#v", got, want)
	}
	for _, forbidden := range []string{"secret-canary", "innocent-canary"} {
		for _, value := range got {
			if value == forbidden {
				t.Fatalf("header value %q was not redacted", forbidden)
			}
		}
	}
}

func TestValidateConfigurationSnapshotReferences(t *testing.T) {
	valid := admin.ConfigurationSnapshot{
		RoutingPools: []admin.ConfigurationRoutingPool{{
			Ref: "routing_pool:1", FallbackRef: "routing_pool:2",
			Accounts: []admin.ConfigurationRoutingPoolMembership{{AccountRef: "provider_account:1"}},
		}, {Ref: "routing_pool:2", Accounts: []admin.ConfigurationRoutingPoolMembership{}}},
		APIKeyTemplates:     []admin.ConfigurationAPIKeyTemplate{{Ref: "api_key_template:1", RoutingPoolRef: "routing_pool:1"}},
		ProviderAccounts:    []admin.ConfigurationProviderAccount{{Ref: "provider_account:1", FingerprintProfileRef: "fingerprint_profile:1"}},
		FingerprintProfiles: []admin.ConfigurationFingerprintProfile{{Ref: "fingerprint_profile:1"}},
		AlertActions:        []admin.ConfigurationAlertAction{{Ref: "alert_action:1"}},
		AlertRules:          []admin.ConfigurationAlertRule{{Ref: "alert_rule:1", ActionRef: "alert_action:1"}},
	}
	if err := validateConfigurationSnapshotReferences(valid); err != nil {
		t.Fatalf("valid snapshot: %v", err)
	}

	tests := []struct {
		name   string
		mutate func(*admin.ConfigurationSnapshot)
	}{
		{"fallback", func(snapshot *admin.ConfigurationSnapshot) {
			snapshot.RoutingPools[0].FallbackRef = "routing_pool:missing"
		}},
		{"membership", func(snapshot *admin.ConfigurationSnapshot) {
			snapshot.RoutingPools[0].Accounts[0].AccountRef = "provider_account:missing"
		}},
		{"key pool", func(snapshot *admin.ConfigurationSnapshot) {
			snapshot.APIKeyTemplates[0].RoutingPoolRef = "routing_pool:missing"
		}},
		{"fingerprint", func(snapshot *admin.ConfigurationSnapshot) {
			snapshot.ProviderAccounts[0].FingerprintProfileRef = "fingerprint_profile:missing"
		}},
		{"alert action", func(snapshot *admin.ConfigurationSnapshot) {
			snapshot.AlertRules[0].ActionRef = "alert_action:missing"
		}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			snapshot := valid
			snapshot.RoutingPools = append([]admin.ConfigurationRoutingPool(nil), valid.RoutingPools...)
			snapshot.RoutingPools[0].Accounts = append([]admin.ConfigurationRoutingPoolMembership(nil), valid.RoutingPools[0].Accounts...)
			snapshot.APIKeyTemplates = append([]admin.ConfigurationAPIKeyTemplate(nil), valid.APIKeyTemplates...)
			snapshot.ProviderAccounts = append([]admin.ConfigurationProviderAccount(nil), valid.ProviderAccounts...)
			snapshot.AlertRules = append([]admin.ConfigurationAlertRule(nil), valid.AlertRules...)
			test.mutate(&snapshot)
			if err := validateConfigurationSnapshotReferences(snapshot); err == nil {
				t.Fatal("expected missing reference error")
			}
		})
	}
}

func TestExportConfigurationSnapshotIncludesDeterministicAlertDefinitions(t *testing.T) {
	repo := newTestAdminRepository(t)
	ctx := context.Background()
	const destinationCanary = "https://secret.example.test/hook?token=destination-canary"

	var zuluActionID, alphaActionID int64
	if err := repo.pool.QueryRow(ctx, `
		INSERT INTO alert_actions (name, kind, encrypted_destination, enabled)
		VALUES ('Zulu action', 'ntfy', $1, false)
		RETURNING id
	`, destinationCanary).Scan(&zuluActionID); err != nil {
		t.Fatalf("insert zulu alert action: %v", err)
	}
	if err := repo.pool.QueryRow(ctx, `
		INSERT INTO alert_actions (name, kind, encrypted_destination, enabled)
		VALUES ('Alpha action', 'generic_webhook', $1, true)
		RETURNING id
	`, destinationCanary).Scan(&alphaActionID); err != nil {
		t.Fatalf("insert alpha alert action: %v", err)
	}
	if _, err := repo.pool.Exec(ctx, `
		INSERT INTO alert_rules (
			template_key, name, action_id, enabled, category, severity, event_action, recovery_action,
			aggregation_count, aggregation_window_seconds, cooldown_seconds, deduplication_scope, notify_recovery
		) VALUES
			('', 'Zulu rule', $1, false, 'runtime', 'error', 'provider_account.expired', '', 1, 0, 86400, 'rule', false),
			('provider-auto-test-failed-v1', 'Alpha rule', $2, true, 'scheduler', 'warning',
			 'scheduler.provider_account_auto_test.failed', 'scheduler.provider_account_auto_test.completed', 2, 900, 3600, 'target', true)
	`, zuluActionID, alphaActionID); err != nil {
		t.Fatalf("insert alert rules: %v", err)
	}

	snapshot, err := repo.ExportConfigurationSnapshot(ctx)
	if err != nil {
		t.Fatalf("ExportConfigurationSnapshot: %v", err)
	}
	if len(snapshot.AlertActions) != 2 || snapshot.AlertActions[0].Name != "Alpha action" || snapshot.AlertActions[1].Name != "Zulu action" {
		t.Fatalf("alert actions = %#v", snapshot.AlertActions)
	}
	if snapshot.AlertActions[0].Kind != "generic_webhook" || !snapshot.AlertActions[0].Enabled ||
		snapshot.AlertActions[1].Kind != "ntfy" || snapshot.AlertActions[1].Enabled ||
		!snapshot.AlertActions[0].DestinationConfigured || !snapshot.AlertActions[1].DestinationConfigured {
		t.Fatalf("alert action state = %#v", snapshot.AlertActions)
	}
	if len(snapshot.AlertRules) != 2 || snapshot.AlertRules[0].Name != "Alpha rule" || snapshot.AlertRules[1].Name != "Zulu rule" {
		t.Fatalf("alert rules = %#v", snapshot.AlertRules)
	}
	alphaRule := snapshot.AlertRules[0]
	if alphaRule.ActionRef != configurationRef("alert_action", alphaActionID) || alphaRule.TemplateKey != "provider-auto-test-failed-v1" ||
		alphaRule.Category != "scheduler" || alphaRule.Severity != "warning" ||
		alphaRule.EventAction != "scheduler.provider_account_auto_test.failed" ||
		alphaRule.RecoveryAction != "scheduler.provider_account_auto_test.completed" || alphaRule.AggregationCount != 2 ||
		alphaRule.AggregationWindowSeconds != 900 || alphaRule.CooldownSeconds != 3600 || alphaRule.DeduplicationScope != "target" || !alphaRule.NotifyRecovery {
		t.Fatalf("alpha alert rule = %#v", alphaRule)
	}
	encoded, err := json.Marshal(snapshot)
	if err != nil {
		t.Fatalf("marshal snapshot: %v", err)
	}
	if strings.Contains(string(encoded), destinationCanary) || strings.Contains(string(encoded), `"destination"`) {
		t.Fatalf("snapshot leaked alert destination: %s", encoded)
	}
}

func TestExportConfigurationSnapshotUsesEmptyAlertArrays(t *testing.T) {
	repo := newTestAdminRepository(t)
	snapshot, err := repo.ExportConfigurationSnapshot(context.Background())
	if err != nil {
		t.Fatalf("ExportConfigurationSnapshot: %v", err)
	}
	if snapshot.AlertActions == nil || len(snapshot.AlertActions) != 0 || snapshot.AlertRules == nil || len(snapshot.AlertRules) != 0 {
		t.Fatalf("empty alert collections = actions %#v rules %#v", snapshot.AlertActions, snapshot.AlertRules)
	}
}

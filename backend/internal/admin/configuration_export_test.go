package admin

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
)

type configurationExportTestRepository struct {
	Repository
	snapshot ConfigurationSnapshot
	err      error
}

func (r *configurationExportTestRepository) ExportConfigurationSnapshot(context.Context) (ConfigurationSnapshot, error) {
	return r.snapshot, r.err
}

func TestExportConfigurationUsesEffectiveDefaults(t *testing.T) {
	repo := &configurationExportTestRepository{snapshot: ConfigurationSnapshot{
		RoutingPools:          []ConfigurationRoutingPool{},
		APIKeyTemplates:       []ConfigurationAPIKeyTemplate{},
		ProviderAccounts:      []ConfigurationProviderAccount{},
		FingerprintProfiles:   []ConfigurationFingerprintProfile{},
		ErrorPassthroughRules: []ConfigurationErrorPassthroughRule{},
		AlertActions:          []ConfigurationAlertAction{},
		AlertRules:            []ConfigurationAlertRule{},
	}}
	service := NewService(repo, Config{DefaultGatewaySettings: GatewaySettings{
		MaxConcurrentGatewayRequests: 11,
	}})

	snapshot, err := service.ExportConfiguration(context.Background())
	if err != nil {
		t.Fatalf("export configuration: %v", err)
	}
	if snapshot.ModelSettings.DefaultModel == "" {
		t.Fatal("default model was not populated")
	}
	if snapshot.UsagePricing.Version == 0 || snapshot.UsagePricing.Models == nil {
		t.Fatalf("default usage pricing = %#v", snapshot.UsagePricing)
	}
	if snapshot.GatewaySettings.MaxConcurrentGatewayRequests != 11 || snapshot.GatewaySettings.ProviderAccountAutoTestIntervalSeconds != 300 {
		t.Fatalf("default gateway settings = %#v", snapshot.GatewaySettings)
	}
}

func TestExportConfigurationReturnsRepositoryFailure(t *testing.T) {
	want := errors.New("snapshot unavailable")
	service := NewService(&configurationExportTestRepository{err: want}, Config{})
	_, err := service.ExportConfiguration(context.Background())
	if !errors.Is(err, want) {
		t.Fatalf("error = %v, want %v", err, want)
	}
}

func TestConfigurationSnapshotJSONExcludesSensitiveFields(t *testing.T) {
	snapshot := ConfigurationSnapshot{
		RoutingPools: []ConfigurationRoutingPool{{
			Ref: "routing_pool:1", Name: "primary", FallbackRef: "routing_pool:2",
			Accounts: []ConfigurationRoutingPoolMembership{{AccountRef: "provider_account:1", Priority: 1}},
		}},
		APIKeyTemplates: []ConfigurationAPIKeyTemplate{{
			Ref: "api_key_template:1", Name: "workstation", AllowedModels: []string{"gpt-example"}, RoutingPoolRef: "routing_pool:1",
		}},
		ProviderAccounts: []ConfigurationProviderAccount{{
			Ref: "provider_account:1", Provider: "openai", AccountType: "api_key", Name: "primary",
			FingerprintProfileRef: "fingerprint_profile:1", Models: []ConfigurationProviderModel{{Model: "gpt-example"}},
		}},
		UsagePricing: UsagePricing{Models: map[string]UsagePrice{"gpt-example": {}}},
		FingerprintProfiles: []ConfigurationFingerprintProfile{{
			Ref: "fingerprint_profile:1", Name: "default", Headers: map[string]string{"X-Example": ConfigurationRedactedValue},
		}},
		ErrorPassthroughRules: []ConfigurationErrorPassthroughRule{{
			Ref: "error_passthrough_rule:1", Pattern: "429", MatchType: "status_code",
		}},
		AlertActions: []ConfigurationAlertAction{{
			Ref: "alert_action:1", Name: "Primary webhook", Kind: "generic_webhook", Enabled: true, DestinationConfigured: true,
		}},
		AlertRules: []ConfigurationAlertRule{{
			Ref: "alert_rule:1", TemplateKey: "provider-auto-test-failed-v1", Name: "Provider tests", ActionRef: "alert_action:1",
			Enabled: true, Category: "scheduler", Severity: "warning", EventAction: "scheduler.provider_account_auto_test.failed",
			RecoveryAction: "scheduler.provider_account_auto_test.completed", AggregationCount: 2, AggregationWindowSeconds: 900,
			CooldownSeconds: 3600, DeduplicationScope: "target", NotifyRecovery: true,
		}},
	}
	encoded, err := json.Marshal(snapshot)
	if err != nil {
		t.Fatalf("marshal configuration snapshot: %v", err)
	}
	var document any
	if err := json.Unmarshal(encoded, &document); err != nil {
		t.Fatalf("decode configuration snapshot: %v", err)
	}
	forbidden := []string{
		"credential", "secret", "accesstoken", "refreshtoken", "password", "hash", "prefix",
		"proxy", "oauth", "session", "cookie", "subject", "metadata",
		"systemevent", "testhistory", "runtimefailure", "responsebody",
	}
	assertConfigurationJSONKeysSafe(t, document, forbidden)
	root := document.(map[string]any)
	actions := root["alertActions"].([]any)
	action := actions[0].(map[string]any)
	if _, ok := action["destination"]; ok {
		t.Fatalf("configuration alert action contains destination field: %#v", action)
	}
	if action["destinationConfigured"] != true {
		t.Fatalf("configuration alert action = %#v", action)
	}
}

func assertConfigurationJSONKeysSafe(t *testing.T, value any, forbidden []string) {
	t.Helper()
	switch typed := value.(type) {
	case map[string]any:
		for key, child := range typed {
			normalized := strings.ToLower(strings.ReplaceAll(key, "_", ""))
			for _, word := range forbidden {
				if strings.Contains(normalized, word) {
					t.Fatalf("configuration export contains forbidden JSON field %q", key)
				}
			}
			assertConfigurationJSONKeysSafe(t, child, forbidden)
		}
	case []any:
		for _, child := range typed {
			assertConfigurationJSONKeysSafe(t, child, forbidden)
		}
	}
}

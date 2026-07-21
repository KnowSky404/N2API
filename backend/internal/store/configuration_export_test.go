package store

import (
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
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			snapshot := valid
			snapshot.RoutingPools = append([]admin.ConfigurationRoutingPool(nil), valid.RoutingPools...)
			snapshot.RoutingPools[0].Accounts = append([]admin.ConfigurationRoutingPoolMembership(nil), valid.RoutingPools[0].Accounts...)
			snapshot.APIKeyTemplates = append([]admin.ConfigurationAPIKeyTemplate(nil), valid.APIKeyTemplates...)
			snapshot.ProviderAccounts = append([]admin.ConfigurationProviderAccount(nil), valid.ProviderAccounts...)
			test.mutate(&snapshot)
			if err := validateConfigurationSnapshotReferences(snapshot); err == nil {
				t.Fatal("expected missing reference error")
			}
		})
	}
}

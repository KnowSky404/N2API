package config

import "testing"

func TestLoadUsesDefaultsForOptionalServerValues(t *testing.T) {
	cfg, err := Load(mapLookup(map[string]string{
		"DATABASE_URL":              "postgres://n2api:secret@localhost:5432/n2api?sslmode=disable",
		"N2API_ENCRYPTION_SECRET":   "a-long-enough-secret",
		"N2API_ADMIN_USERNAME":      "owner",
		"N2API_ADMIN_PASSWORD":      "change-me",
		"OPENAI_OAUTH_CLIENT_ID":    "client-id",
		"OPENAI_OAUTH_REDIRECT_URL": "http://localhost:3000/oauth/openai/callback",
	}))
	if err != nil {
		t.Fatalf("Load returned error: %v", err)
	}

	if cfg.Host != "0.0.0.0" {
		t.Fatalf("Host = %q, want default 0.0.0.0", cfg.Host)
	}
	if cfg.Port != 3000 {
		t.Fatalf("Port = %d, want default 3000", cfg.Port)
	}
	if cfg.Addr() != "0.0.0.0:3000" {
		t.Fatalf("Addr() = %q, want 0.0.0.0:3000", cfg.Addr())
	}
}

func TestLoadRequiresDatabaseURL(t *testing.T) {
	_, err := Load(mapLookup(map[string]string{
		"N2API_ENCRYPTION_SECRET": "a-long-enough-secret",
		"N2API_ADMIN_PASSWORD":    "change-me",
	}))
	if err == nil {
		t.Fatal("Load returned nil error, want missing DATABASE_URL error")
	}
}

func TestLoadRequiresEncryptionSecret(t *testing.T) {
	_, err := Load(mapLookup(map[string]string{
		"DATABASE_URL":         "postgres://n2api:secret@localhost:5432/n2api?sslmode=disable",
		"N2API_ADMIN_PASSWORD": "change-me",
	}))
	if err == nil {
		t.Fatal("Load returned nil error, want missing N2API_ENCRYPTION_SECRET error")
	}
}

func TestLoadRejectsInvalidPort(t *testing.T) {
	_, err := Load(mapLookup(map[string]string{
		"DATABASE_URL":            "postgres://n2api:secret@localhost:5432/n2api?sslmode=disable",
		"N2API_ENCRYPTION_SECRET": "a-long-enough-secret",
		"N2API_ADMIN_PASSWORD":    "change-me",
		"N2API_PORT":              "abc",
	}))
	if err == nil {
		t.Fatal("Load returned nil error, want invalid N2API_PORT error")
	}
}

func mapLookup(values map[string]string) func(string) string {
	return func(key string) string {
		return values[key]
	}
}

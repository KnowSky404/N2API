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

func TestLoadOpenAIOAuthEndpointConfig(t *testing.T) {
	env := map[string]string{
		"DATABASE_URL":               "postgres://example",
		"N2API_ENCRYPTION_SECRET":    "encryption-secret",
		"N2API_ADMIN_PASSWORD":       "admin-password",
		"OPENAI_OAUTH_CLIENT_ID":     "client-id",
		"OPENAI_OAUTH_CLIENT_SECRET": "client-secret",
		"OPENAI_OAUTH_REDIRECT_URL":  "http://localhost:3000/oauth/openai/callback",
		"OPENAI_OAUTH_AUTH_URL":      "https://auth.example.test/authorize",
		"OPENAI_OAUTH_TOKEN_URL":     "https://auth.example.test/token",
	}
	cfg, err := Load(func(key string) string { return env[key] })
	if err != nil {
		t.Fatalf("Load returned error: %v", err)
	}
	if cfg.OpenAIOAuthAuthURL != "https://auth.example.test/authorize" {
		t.Fatalf("OpenAIOAuthAuthURL = %q", cfg.OpenAIOAuthAuthURL)
	}
	if cfg.OpenAIOAuthTokenURL != "https://auth.example.test/token" {
		t.Fatalf("OpenAIOAuthTokenURL = %q", cfg.OpenAIOAuthTokenURL)
	}
}

func TestLoadDefaultsOpenAIOAuthForCodexPKCE(t *testing.T) {
	cfg, err := Load(mapLookup(map[string]string{
		"DATABASE_URL":            "postgres://example",
		"N2API_ENCRYPTION_SECRET": "encryption-secret",
		"N2API_ADMIN_PASSWORD":    "admin-password",
	}))
	if err != nil {
		t.Fatalf("Load returned error: %v", err)
	}
	if cfg.OpenAIOAuthClientID == "" {
		t.Fatal("OpenAIOAuthClientID was empty")
	}
	if cfg.OpenAIOAuthSecret != "" {
		t.Fatalf("OpenAIOAuthSecret = %q, want empty public PKCE client secret", cfg.OpenAIOAuthSecret)
	}
	if cfg.OpenAIOAuthAuthURL == "" || cfg.OpenAIOAuthTokenURL == "" || cfg.OpenAIOAuthRedirectURL == "" {
		t.Fatalf("OAuth defaults incomplete: auth=%q token=%q redirect=%q", cfg.OpenAIOAuthAuthURL, cfg.OpenAIOAuthTokenURL, cfg.OpenAIOAuthRedirectURL)
	}
}

func TestLoadOpenAIAPIBaseURLConfig(t *testing.T) {
	cfg, err := Load(mapLookup(map[string]string{
		"DATABASE_URL":              "postgres://example",
		"N2API_ENCRYPTION_SECRET":   "encryption-secret",
		"N2API_ADMIN_PASSWORD":      "admin-password",
		"OPENAI_API_BASE_URL":       "https://api.example.test",
		"OPENAI_OAUTH_CLIENT_ID":    "client-id",
		"OPENAI_OAUTH_REDIRECT_URL": "http://localhost:3000/oauth/openai/callback",
	}))
	if err != nil {
		t.Fatalf("Load returned error: %v", err)
	}
	if cfg.OpenAIAPIBaseURL != "https://api.example.test" {
		t.Fatalf("OpenAIAPIBaseURL = %q", cfg.OpenAIAPIBaseURL)
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

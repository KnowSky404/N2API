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
	if cfg.OpenAIOAuthRedirectURL != "http://localhost:1455/auth/callback" {
		t.Fatalf("OpenAIOAuthRedirectURL = %q, want Codex callback", cfg.OpenAIOAuthRedirectURL)
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

func TestLoadGatewayConcurrencyLimitConfig(t *testing.T) {
	defaultCfg, err := Load(mapLookup(map[string]string{
		"DATABASE_URL":            "postgres://example",
		"N2API_ENCRYPTION_SECRET": "encryption-secret",
		"N2API_ADMIN_PASSWORD":    "admin-password",
	}))
	if err != nil {
		t.Fatalf("Load default returned error: %v", err)
	}
	if defaultCfg.GatewayMaxConcurrentRequests != 0 {
		t.Fatalf("GatewayMaxConcurrentRequests = %d, want default disabled 0", defaultCfg.GatewayMaxConcurrentRequests)
	}

	cfg, err := Load(mapLookup(map[string]string{
		"DATABASE_URL":                          "postgres://example",
		"N2API_ENCRYPTION_SECRET":               "encryption-secret",
		"N2API_ADMIN_PASSWORD":                  "admin-password",
		"N2API_GATEWAY_MAX_CONCURRENT_REQUESTS": "3",
	}))
	if err != nil {
		t.Fatalf("Load returned error: %v", err)
	}
	if cfg.GatewayMaxConcurrentRequests != 3 {
		t.Fatalf("GatewayMaxConcurrentRequests = %d, want 3", cfg.GatewayMaxConcurrentRequests)
	}
}

func TestLoadGatewayAccountConcurrencyLimitConfig(t *testing.T) {
	defaultCfg, err := Load(mapLookup(map[string]string{
		"DATABASE_URL":            "postgres://example",
		"N2API_ENCRYPTION_SECRET": "encryption-secret",
		"N2API_ADMIN_PASSWORD":    "admin-password",
	}))
	if err != nil {
		t.Fatalf("Load default returned error: %v", err)
	}
	if defaultCfg.GatewayMaxConcurrentRequestsPerAccount != 0 {
		t.Fatalf("GatewayMaxConcurrentRequestsPerAccount = %d, want default disabled 0", defaultCfg.GatewayMaxConcurrentRequestsPerAccount)
	}

	cfg, err := Load(mapLookup(map[string]string{
		"DATABASE_URL":            "postgres://example",
		"N2API_ENCRYPTION_SECRET": "encryption-secret",
		"N2API_ADMIN_PASSWORD":    "admin-password",
		"N2API_GATEWAY_MAX_CONCURRENT_REQUESTS_PER_ACCOUNT": "2",
	}))
	if err != nil {
		t.Fatalf("Load returned error: %v", err)
	}
	if cfg.GatewayMaxConcurrentRequestsPerAccount != 2 {
		t.Fatalf("GatewayMaxConcurrentRequestsPerAccount = %d, want 2", cfg.GatewayMaxConcurrentRequestsPerAccount)
	}
}

func TestLoadGatewayAPIKeyConcurrencyLimitConfig(t *testing.T) {
	defaultCfg, err := Load(mapLookup(map[string]string{
		"DATABASE_URL":            "postgres://example",
		"N2API_ENCRYPTION_SECRET": "encryption-secret",
		"N2API_ADMIN_PASSWORD":    "admin-password",
	}))
	if err != nil {
		t.Fatalf("Load default returned error: %v", err)
	}
	if defaultCfg.GatewayMaxConcurrentRequestsPerKey != 0 {
		t.Fatalf("GatewayMaxConcurrentRequestsPerKey = %d, want default disabled 0", defaultCfg.GatewayMaxConcurrentRequestsPerKey)
	}

	cfg, err := Load(mapLookup(map[string]string{
		"DATABASE_URL":                                  "postgres://example",
		"N2API_ENCRYPTION_SECRET":                       "encryption-secret",
		"N2API_ADMIN_PASSWORD":                          "admin-password",
		"N2API_GATEWAY_MAX_CONCURRENT_REQUESTS_PER_KEY": "3",
	}))
	if err != nil {
		t.Fatalf("Load returned error: %v", err)
	}
	if cfg.GatewayMaxConcurrentRequestsPerKey != 3 {
		t.Fatalf("GatewayMaxConcurrentRequestsPerKey = %d, want 3", cfg.GatewayMaxConcurrentRequestsPerKey)
	}
}

func TestLoadGatewayRequestRateLimitConfig(t *testing.T) {
	defaultCfg, err := Load(mapLookup(map[string]string{
		"DATABASE_URL":            "postgres://example",
		"N2API_ENCRYPTION_SECRET": "encryption-secret",
		"N2API_ADMIN_PASSWORD":    "admin-password",
	}))
	if err != nil {
		t.Fatalf("Load default returned error: %v", err)
	}
	if defaultCfg.GatewayRequestsPerMinutePerKey != 0 {
		t.Fatalf("GatewayRequestsPerMinutePerKey = %d, want default disabled 0", defaultCfg.GatewayRequestsPerMinutePerKey)
	}

	cfg, err := Load(mapLookup(map[string]string{
		"DATABASE_URL":                              "postgres://example",
		"N2API_ENCRYPTION_SECRET":                   "encryption-secret",
		"N2API_ADMIN_PASSWORD":                      "admin-password",
		"N2API_GATEWAY_REQUESTS_PER_MINUTE_PER_KEY": "60",
	}))
	if err != nil {
		t.Fatalf("Load returned error: %v", err)
	}
	if cfg.GatewayRequestsPerMinutePerKey != 60 {
		t.Fatalf("GatewayRequestsPerMinutePerKey = %d, want 60", cfg.GatewayRequestsPerMinutePerKey)
	}
}

func TestLoadGatewayTokenRateLimitConfig(t *testing.T) {
	defaultCfg, err := Load(mapLookup(map[string]string{
		"DATABASE_URL":            "postgres://example",
		"N2API_ENCRYPTION_SECRET": "encryption-secret",
		"N2API_ADMIN_PASSWORD":    "admin-password",
	}))
	if err != nil {
		t.Fatalf("Load default returned error: %v", err)
	}
	if defaultCfg.GatewayTokensPerMinutePerKey != 0 {
		t.Fatalf("GatewayTokensPerMinutePerKey = %d, want default disabled 0", defaultCfg.GatewayTokensPerMinutePerKey)
	}

	cfg, err := Load(mapLookup(map[string]string{
		"DATABASE_URL":                            "postgres://example",
		"N2API_ENCRYPTION_SECRET":                 "encryption-secret",
		"N2API_ADMIN_PASSWORD":                    "admin-password",
		"N2API_GATEWAY_TOKENS_PER_MINUTE_PER_KEY": "60000",
	}))
	if err != nil {
		t.Fatalf("Load returned error: %v", err)
	}
	if cfg.GatewayTokensPerMinutePerKey != 60000 {
		t.Fatalf("GatewayTokensPerMinutePerKey = %d, want 60000", cfg.GatewayTokensPerMinutePerKey)
	}
}

func TestLoadRejectsInvalidGatewayConcurrencyLimit(t *testing.T) {
	for _, value := range []string{"abc", "-1"} {
		t.Run(value, func(t *testing.T) {
			_, err := Load(mapLookup(map[string]string{
				"DATABASE_URL":                          "postgres://example",
				"N2API_ENCRYPTION_SECRET":               "encryption-secret",
				"N2API_ADMIN_PASSWORD":                  "admin-password",
				"N2API_GATEWAY_MAX_CONCURRENT_REQUESTS": value,
			}))
			if err == nil {
				t.Fatal("Load returned nil error, want invalid gateway concurrency limit error")
			}
		})
	}
}

func TestLoadRejectsInvalidGatewayAccountConcurrencyLimit(t *testing.T) {
	for _, value := range []string{"abc", "-1"} {
		t.Run(value, func(t *testing.T) {
			_, err := Load(mapLookup(map[string]string{
				"DATABASE_URL":            "postgres://example",
				"N2API_ENCRYPTION_SECRET": "encryption-secret",
				"N2API_ADMIN_PASSWORD":    "admin-password",
				"N2API_GATEWAY_MAX_CONCURRENT_REQUESTS_PER_ACCOUNT": value,
			}))
			if err == nil {
				t.Fatal("Load returned nil error, want invalid gateway account concurrency limit error")
			}
		})
	}
}

func TestLoadRejectsInvalidGatewayAPIKeyConcurrencyLimit(t *testing.T) {
	for _, value := range []string{"abc", "-1"} {
		t.Run(value, func(t *testing.T) {
			_, err := Load(mapLookup(map[string]string{
				"DATABASE_URL":                                  "postgres://example",
				"N2API_ENCRYPTION_SECRET":                       "encryption-secret",
				"N2API_ADMIN_PASSWORD":                          "admin-password",
				"N2API_GATEWAY_MAX_CONCURRENT_REQUESTS_PER_KEY": value,
			}))
			if err == nil {
				t.Fatal("Load returned nil error, want invalid gateway api key concurrency limit error")
			}
		})
	}
}

func TestLoadRejectsInvalidGatewayRequestRateLimit(t *testing.T) {
	for _, value := range []string{"abc", "-1"} {
		t.Run(value, func(t *testing.T) {
			_, err := Load(mapLookup(map[string]string{
				"DATABASE_URL":                              "postgres://example",
				"N2API_ENCRYPTION_SECRET":                   "encryption-secret",
				"N2API_ADMIN_PASSWORD":                      "admin-password",
				"N2API_GATEWAY_REQUESTS_PER_MINUTE_PER_KEY": value,
			}))
			if err == nil {
				t.Fatal("Load returned nil error, want invalid gateway request rate limit error")
			}
		})
	}
}

func TestLoadRejectsInvalidGatewayTokenRateLimit(t *testing.T) {
	for _, value := range []string{"abc", "-1"} {
		t.Run(value, func(t *testing.T) {
			_, err := Load(mapLookup(map[string]string{
				"DATABASE_URL":                            "postgres://example",
				"N2API_ENCRYPTION_SECRET":                 "encryption-secret",
				"N2API_ADMIN_PASSWORD":                    "admin-password",
				"N2API_GATEWAY_TOKENS_PER_MINUTE_PER_KEY": value,
			}))
			if err == nil {
				t.Fatal("Load returned nil error, want invalid gateway token rate limit error")
			}
		})
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

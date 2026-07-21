package config

import (
	"maps"
	"net/netip"
	"slices"
	"strconv"
	"strings"
	"testing"
	"time"
)

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
	if len(cfg.TrustedProxyCIDRs) != 0 {
		t.Fatalf("TrustedProxyCIDRs = %v, want empty", cfg.TrustedProxyCIDRs)
	}
	if !cfg.AdminLoginThrottleEnabled || cfg.AdminLoginThrottleFailures != 5 || cfg.AdminLoginThrottleMaxEntries != 4096 {
		t.Fatalf("login throttle defaults = enabled:%t failures:%d entries:%d", cfg.AdminLoginThrottleEnabled, cfg.AdminLoginThrottleFailures, cfg.AdminLoginThrottleMaxEntries)
	}
}

func TestLoadAdminLoginThrottleConfig(t *testing.T) {
	base := map[string]string{
		"DATABASE_URL": "postgres://example", "N2API_ENCRYPTION_SECRET": "encryption-secret", "N2API_ADMIN_PASSWORD": "admin-password",
	}
	values := maps.Clone(base)
	values["N2API_ADMIN_LOGIN_THROTTLE_ENABLED"] = "false"
	values["N2API_ADMIN_LOGIN_THROTTLE_FAILURES"] = "8"
	values["N2API_ADMIN_LOGIN_THROTTLE_MAX_ENTRIES"] = "8192"
	cfg, err := Load(mapLookup(values))
	if err != nil {
		t.Fatalf("Load returned error: %v", err)
	}
	if cfg.AdminLoginThrottleEnabled || cfg.AdminLoginThrottleFailures != 8 || cfg.AdminLoginThrottleMaxEntries != 8192 {
		t.Fatalf("login throttle config = enabled:%t failures:%d entries:%d", cfg.AdminLoginThrottleEnabled, cfg.AdminLoginThrottleFailures, cfg.AdminLoginThrottleMaxEntries)
	}

	for name, value := range map[string]string{
		"N2API_ADMIN_LOGIN_THROTTLE_ENABLED":     "sometimes",
		"N2API_ADMIN_LOGIN_THROTTLE_FAILURES":    "0",
		"N2API_ADMIN_LOGIN_THROTTLE_MAX_ENTRIES": "127",
	} {
		t.Run(name, func(t *testing.T) {
			invalid := maps.Clone(base)
			invalid[name] = value
			if _, err := Load(mapLookup(invalid)); err == nil {
				t.Fatal("Load returned nil error")
			}
		})
	}
	tooManyEntries := maps.Clone(base)
	tooManyEntries["N2API_ADMIN_LOGIN_THROTTLE_MAX_ENTRIES"] = "16385"
	if _, err := Load(mapLookup(tooManyEntries)); err == nil {
		t.Fatal("Load accepted N2API_ADMIN_LOGIN_THROTTLE_MAX_ENTRIES above 16384")
	}
}

func TestLoadTrustedProxyCIDRs(t *testing.T) {
	base := map[string]string{
		"DATABASE_URL": "postgres://example", "N2API_ENCRYPTION_SECRET": "encryption-secret", "N2API_ADMIN_PASSWORD": "admin-password",
	}
	values := maps.Clone(base)
	values["N2API_TRUSTED_PROXY_CIDRS"] = " 10.0.0.9/8, 2001:db8::1/32, ::ffff:192.0.2.8/120, 10.0.0.0/8 "
	cfg, err := Load(mapLookup(values))
	if err != nil {
		t.Fatalf("Load returned error: %v", err)
	}
	want := []netip.Prefix{
		netip.MustParsePrefix("10.0.0.0/8"),
		netip.MustParsePrefix("2001:db8::/32"),
		netip.MustParsePrefix("192.0.2.0/24"),
	}
	if !slices.Equal(cfg.TrustedProxyCIDRs, want) {
		t.Fatalf("TrustedProxyCIDRs = %v, want %v", cfg.TrustedProxyCIDRs, want)
	}
}

func TestLoadRejectsInvalidTrustedProxyCIDRs(t *testing.T) {
	for _, value := range []string{"not-a-cidr", "10.0.0.1", "10.0.0.0/8,,192.0.2.0/24", "::ffff:192.0.2.0/80"} {
		t.Run(value, func(t *testing.T) {
			_, err := Load(mapLookup(map[string]string{
				"DATABASE_URL": "postgres://example", "N2API_ENCRYPTION_SECRET": "encryption-secret", "N2API_ADMIN_PASSWORD": "admin-password",
				"N2API_TRUSTED_PROXY_CIDRS": value,
			}))
			if err == nil {
				t.Fatal("Load returned nil error")
			}
		})
	}
}

func TestLoadSystemEventRetentionDays(t *testing.T) {
	base := map[string]string{
		"DATABASE_URL": "postgres://example", "N2API_ENCRYPTION_SECRET": "encryption-secret", "N2API_ADMIN_PASSWORD": "admin-password",
	}
	cfg, err := Load(mapLookup(base))
	if err != nil {
		t.Fatalf("Load default returned error: %v", err)
	}
	if cfg.SystemEventRetentionDays != 365 {
		t.Fatalf("SystemEventRetentionDays = %d, want 365", cfg.SystemEventRetentionDays)
	}
	for _, value := range []string{"0", "30", "3650"} {
		t.Run("valid_"+value, func(t *testing.T) {
			values := maps.Clone(base)
			values["N2API_SYSTEM_EVENT_RETENTION_DAYS"] = value
			cfg, err := Load(mapLookup(values))
			if err != nil {
				t.Fatalf("Load returned error: %v", err)
			}
			want, _ := strconv.Atoi(value)
			if cfg.SystemEventRetentionDays != want {
				t.Fatalf("SystemEventRetentionDays = %d, want %d", cfg.SystemEventRetentionDays, want)
			}
		})
	}
	for _, value := range []string{"bad", "-1", "1", "29", "3651"} {
		t.Run("invalid_"+value, func(t *testing.T) {
			values := maps.Clone(base)
			values["N2API_SYSTEM_EVENT_RETENTION_DAYS"] = value
			if _, err := Load(mapLookup(values)); err == nil {
				t.Fatal("Load returned nil error")
			}
		})
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

func TestLoadHTTPAPIUpstreamOptInConfig(t *testing.T) {
	defaultCfg, err := Load(mapLookup(map[string]string{
		"DATABASE_URL":            "postgres://example",
		"N2API_ENCRYPTION_SECRET": "encryption-secret",
		"N2API_ADMIN_PASSWORD":    "admin-password",
	}))
	if err != nil {
		t.Fatalf("Load default returned error: %v", err)
	}
	if defaultCfg.AllowHTTPAPIUpstreams {
		t.Fatal("AllowHTTPAPIUpstreams = true, want default false")
	}

	cfg, err := Load(mapLookup(map[string]string{
		"DATABASE_URL":                   "postgres://example",
		"N2API_ENCRYPTION_SECRET":        "encryption-secret",
		"N2API_ADMIN_PASSWORD":           "admin-password",
		"N2API_ALLOW_HTTP_API_UPSTREAMS": "true",
	}))
	if err != nil {
		t.Fatalf("Load returned error: %v", err)
	}
	if !cfg.AllowHTTPAPIUpstreams {
		t.Fatal("AllowHTTPAPIUpstreams = false, want true")
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

func TestLoadProviderAccountAutoTestDefaultsDisabled(t *testing.T) {
	cfg, err := Load(mapLookup(map[string]string{
		"DATABASE_URL":            "postgres://example",
		"N2API_ENCRYPTION_SECRET": "encryption-secret",
		"N2API_ADMIN_PASSWORD":    "admin-password",
	}))
	if err != nil {
		t.Fatalf("Load returned error: %v", err)
	}

	if cfg.ProviderAccountAutoTestEnabled {
		t.Fatal("ProviderAccountAutoTestEnabled = true, want false by default")
	}
	if cfg.ProviderAccountAutoTestInterval != 5*time.Minute {
		t.Fatalf("ProviderAccountAutoTestInterval = %v, want 5m", cfg.ProviderAccountAutoTestInterval)
	}
}

func TestLoadProviderAccountAutoTestEnabledWithInterval(t *testing.T) {
	cfg, err := Load(mapLookup(map[string]string{
		"DATABASE_URL":                                      "postgres://example",
		"N2API_ENCRYPTION_SECRET":                           "encryption-secret",
		"N2API_ADMIN_PASSWORD":                              "admin-password",
		"N2API_PROVIDER_ACCOUNT_AUTO_TEST_ENABLED":          "true",
		"N2API_PROVIDER_ACCOUNT_AUTO_TEST_INTERVAL_SECONDS": "120",
	}))
	if err != nil {
		t.Fatalf("Load returned error: %v", err)
	}

	if !cfg.ProviderAccountAutoTestEnabled {
		t.Fatal("ProviderAccountAutoTestEnabled = false, want true")
	}
	if cfg.ProviderAccountAutoTestInterval != 2*time.Minute {
		t.Fatalf("ProviderAccountAutoTestInterval = %v, want 2m", cfg.ProviderAccountAutoTestInterval)
	}
}

func TestLoadProviderAccountAutoTestRejectsTooSmallEnabledInterval(t *testing.T) {
	_, err := Load(mapLookup(map[string]string{
		"DATABASE_URL":                                      "postgres://example",
		"N2API_ENCRYPTION_SECRET":                           "encryption-secret",
		"N2API_ADMIN_PASSWORD":                              "admin-password",
		"N2API_PROVIDER_ACCOUNT_AUTO_TEST_ENABLED":          "true",
		"N2API_PROVIDER_ACCOUNT_AUTO_TEST_INTERVAL_SECONDS": "30",
	}))
	if err == nil {
		t.Fatal("Load returned nil error, want auto test interval validation error")
	}
	if !strings.Contains(err.Error(), "N2API_PROVIDER_ACCOUNT_AUTO_TEST_INTERVAL_SECONDS must be at least 60 when auto test is enabled") {
		t.Fatalf("Load error = %q, want auto test interval validation error", err.Error())
	}
}

func TestLoadProviderAccountAutoTestRejectsInvalidBoolean(t *testing.T) {
	_, err := Load(mapLookup(map[string]string{
		"DATABASE_URL":                             "postgres://example",
		"N2API_ENCRYPTION_SECRET":                  "encryption-secret",
		"N2API_ADMIN_PASSWORD":                     "admin-password",
		"N2API_PROVIDER_ACCOUNT_AUTO_TEST_ENABLED": "sometimes",
	}))
	if err == nil {
		t.Fatal("Load returned nil error, want auto test boolean validation error")
	}
	if !strings.Contains(err.Error(), "N2API_PROVIDER_ACCOUNT_AUTO_TEST_ENABLED must be a boolean") {
		t.Fatalf("Load error = %q, want auto test boolean validation error", err.Error())
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

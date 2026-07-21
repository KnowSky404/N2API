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
		"N2API_ENCRYPTION_SECRET":   "test-encryption-secret-at-least-32-bytes",
		"N2API_ADMIN_USERNAME":      "owner",
		"N2API_ADMIN_PASSWORD":      "test-admin-password",
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
	if cfg.AdminSessionTTL != 168*time.Hour {
		t.Fatalf("AdminSessionTTL = %s, want 168h", cfg.AdminSessionTTL)
	}
}

func TestLoadAdminSessionTTL(t *testing.T) {
	base := map[string]string{
		"DATABASE_URL": "postgres://example", "N2API_ENCRYPTION_SECRET": "test-encryption-secret-at-least-32-bytes", "N2API_ADMIN_PASSWORD": "admin-password",
	}
	for _, tt := range []struct {
		name  string
		value string
		want  time.Duration
	}{
		{name: "minimum", value: "1", want: time.Hour},
		{name: "custom", value: "24", want: 24 * time.Hour},
		{name: "maximum", value: "8760", want: 8760 * time.Hour},
	} {
		t.Run(tt.name, func(t *testing.T) {
			values := maps.Clone(base)
			values["N2API_ADMIN_SESSION_TTL_HOURS"] = tt.value
			cfg, err := Load(mapLookup(values))
			if err != nil {
				t.Fatalf("Load returned error: %v", err)
			}
			if cfg.AdminSessionTTL != tt.want {
				t.Fatalf("AdminSessionTTL = %s, want %s", cfg.AdminSessionTTL, tt.want)
			}
		})
	}

	for _, value := range []string{"0", "-1", "8761", "one-day", "999999999999999999999999"} {
		t.Run("invalid_"+value, func(t *testing.T) {
			values := maps.Clone(base)
			values["N2API_ADMIN_SESSION_TTL_HOURS"] = value
			if _, err := Load(mapLookup(values)); err == nil {
				t.Fatal("Load returned nil error")
			}
		})
	}
}

func TestLoadAdminLoginThrottleConfig(t *testing.T) {
	base := map[string]string{
		"DATABASE_URL": "postgres://example", "N2API_ENCRYPTION_SECRET": "test-encryption-secret-at-least-32-bytes", "N2API_ADMIN_PASSWORD": "admin-password",
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
		"DATABASE_URL": "postgres://example", "N2API_ENCRYPTION_SECRET": "test-encryption-secret-at-least-32-bytes", "N2API_ADMIN_PASSWORD": "admin-password",
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
				"DATABASE_URL": "postgres://example", "N2API_ENCRYPTION_SECRET": "test-encryption-secret-at-least-32-bytes", "N2API_ADMIN_PASSWORD": "admin-password",
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
		"DATABASE_URL": "postgres://example", "N2API_ENCRYPTION_SECRET": "test-encryption-secret-at-least-32-bytes", "N2API_ADMIN_PASSWORD": "admin-password",
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

func TestLoadRequestLogRetentionRunnerConfig(t *testing.T) {
	base := map[string]string{
		"DATABASE_URL": "postgres://example", "N2API_ENCRYPTION_SECRET": "test-encryption-secret-at-least-32-bytes", "N2API_ADMIN_PASSWORD": "admin-password",
	}
	cfg, err := Load(mapLookup(base))
	if err != nil {
		t.Fatalf("Load default returned error: %v", err)
	}
	if cfg.RequestLogRetentionRunnerEnabled || cfg.RequestLogRetentionInterval != 24*time.Hour || cfg.RequestLogRetentionBatchSize != 1000 {
		t.Fatalf("default request log retention runner config = enabled %v interval %s batch %d", cfg.RequestLogRetentionRunnerEnabled, cfg.RequestLogRetentionInterval, cfg.RequestLogRetentionBatchSize)
	}
	values := maps.Clone(base)
	values["N2API_REQUEST_LOG_RETENTION_RUNNER_ENABLED"] = "true"
	values["N2API_REQUEST_LOG_RETENTION_INTERVAL_SECONDS"] = "300"
	values["N2API_REQUEST_LOG_RETENTION_BATCH_SIZE"] = "10000"
	cfg, err = Load(mapLookup(values))
	if err != nil {
		t.Fatalf("Load configured returned error: %v", err)
	}
	if !cfg.RequestLogRetentionRunnerEnabled || cfg.RequestLogRetentionInterval != 5*time.Minute || cfg.RequestLogRetentionBatchSize != 10000 {
		t.Fatalf("configured request log retention runner config = enabled %v interval %s batch %d", cfg.RequestLogRetentionRunnerEnabled, cfg.RequestLogRetentionInterval, cfg.RequestLogRetentionBatchSize)
	}
	for name, value := range map[string]string{
		"N2API_REQUEST_LOG_RETENTION_RUNNER_ENABLED":   "sometimes",
		"N2API_REQUEST_LOG_RETENTION_INTERVAL_SECONDS": "299",
		"N2API_REQUEST_LOG_RETENTION_BATCH_SIZE":       "10001",
	} {
		t.Run(name, func(t *testing.T) {
			invalid := maps.Clone(base)
			invalid[name] = value
			if _, err := Load(mapLookup(invalid)); err == nil {
				t.Fatal("Load returned nil error")
			}
		})
	}
}

func TestLoadOpenAIOAuthEndpointConfig(t *testing.T) {
	env := map[string]string{
		"DATABASE_URL":               "postgres://example",
		"N2API_ENCRYPTION_SECRET":    "test-encryption-secret-at-least-32-bytes",
		"N2API_ADMIN_PASSWORD":       "admin-password",
		"OPENAI_OAUTH_CLIENT_ID":     "client-id",
		"OPENAI_OAUTH_CLIENT_SECRET": "client-secret",
		"OPENAI_OAUTH_REDIRECT_URL":  "http://localhost:3000/oauth/openai/callback",
		"OPENAI_OAUTH_AUTH_URL":      "https://auth.example.test/authorize",
		"OPENAI_OAUTH_TOKEN_URL":     "https://auth.example.test/token",
	}
	cfg, err := Load(mapLookup(env))
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
		"N2API_ENCRYPTION_SECRET": "test-encryption-secret-at-least-32-bytes",
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
		"N2API_ENCRYPTION_SECRET":   "test-encryption-secret-at-least-32-bytes",
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
		"N2API_ENCRYPTION_SECRET": "test-encryption-secret-at-least-32-bytes",
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
		"N2API_ENCRYPTION_SECRET":        "test-encryption-secret-at-least-32-bytes",
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
		"N2API_ENCRYPTION_SECRET": "test-encryption-secret-at-least-32-bytes",
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
		"N2API_ENCRYPTION_SECRET":               "test-encryption-secret-at-least-32-bytes",
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
		"N2API_ENCRYPTION_SECRET": "test-encryption-secret-at-least-32-bytes",
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
		"N2API_ENCRYPTION_SECRET": "test-encryption-secret-at-least-32-bytes",
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
		"N2API_ENCRYPTION_SECRET": "test-encryption-secret-at-least-32-bytes",
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
		"N2API_ENCRYPTION_SECRET":                       "test-encryption-secret-at-least-32-bytes",
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
		"N2API_ENCRYPTION_SECRET": "test-encryption-secret-at-least-32-bytes",
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
		"N2API_ENCRYPTION_SECRET":                   "test-encryption-secret-at-least-32-bytes",
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
		"N2API_ENCRYPTION_SECRET": "test-encryption-secret-at-least-32-bytes",
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
		"N2API_ENCRYPTION_SECRET":                 "test-encryption-secret-at-least-32-bytes",
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
				"N2API_ENCRYPTION_SECRET":               "test-encryption-secret-at-least-32-bytes",
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
				"N2API_ENCRYPTION_SECRET": "test-encryption-secret-at-least-32-bytes",
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
				"N2API_ENCRYPTION_SECRET":                       "test-encryption-secret-at-least-32-bytes",
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
				"N2API_ENCRYPTION_SECRET":                   "test-encryption-secret-at-least-32-bytes",
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
				"N2API_ENCRYPTION_SECRET":                 "test-encryption-secret-at-least-32-bytes",
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
		"N2API_ENCRYPTION_SECRET": "test-encryption-secret-at-least-32-bytes",
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
		"N2API_ENCRYPTION_SECRET":                           "test-encryption-secret-at-least-32-bytes",
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
		"N2API_ENCRYPTION_SECRET":                           "test-encryption-secret-at-least-32-bytes",
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
		"N2API_ENCRYPTION_SECRET":                  "test-encryption-secret-at-least-32-bytes",
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
		"N2API_ENCRYPTION_SECRET": "test-encryption-secret-at-least-32-bytes",
		"N2API_ADMIN_PASSWORD":    "test-admin-password",
	}))
	if err == nil {
		t.Fatal("Load returned nil error, want missing DATABASE_URL error")
	}
}

func TestLoadRequiresEncryptionSecret(t *testing.T) {
	_, err := Load(mapLookup(map[string]string{
		"DATABASE_URL":         "postgres://n2api:secret@localhost:5432/n2api?sslmode=disable",
		"N2API_ADMIN_PASSWORD": "test-admin-password",
	}))
	if err == nil {
		t.Fatal("Load returned nil error, want missing N2API_ENCRYPTION_SECRET error")
	}
}

func TestLoadRejectsInvalidPort(t *testing.T) {
	_, err := Load(mapLookup(map[string]string{
		"DATABASE_URL":            "postgres://n2api:secret@localhost:5432/n2api?sslmode=disable",
		"N2API_ENCRYPTION_SECRET": "test-encryption-secret-at-least-32-bytes",
		"N2API_ADMIN_PASSWORD":    "test-admin-password",
		"N2API_PORT":              "abc",
	}))
	if err == nil {
		t.Fatal("Load returned nil error, want invalid N2API_PORT error")
	}
}

func TestLoadAcceptsStrictProductionConfiguration(t *testing.T) {
	cfg, err := Load(strictConfigLookup(nil))
	if err != nil {
		t.Fatalf("Load returned error: %v", err)
	}
	if cfg.PublicURL != "https://n2api.knowsky.uk" {
		t.Fatalf("PublicURL = %q", cfg.PublicURL)
	}
}

func TestLoadRejectsUnsafePublicURL(t *testing.T) {
	tests := []struct {
		name  string
		value string
	}{
		{name: "relative", value: "n2api.knowsky.uk"},
		{name: "unsupported scheme", value: "ftp://n2api.knowsky.uk"},
		{name: "missing host", value: "https:///"},
		{name: "empty hostname", value: "https://:443"},
		{name: "empty port", value: "https://n2api.knowsky.uk:"},
		{name: "zero port", value: "https://n2api.knowsky.uk:0"},
		{name: "oversized port", value: "https://n2api.knowsky.uk:99999"},
		{name: "invalid host character", value: "https://n2api,knowsky.uk"},
		{name: "userinfo", value: "https://owner:secret@n2api.knowsky.uk"},
		{name: "non-root path", value: "https://n2api.knowsky.uk/admin"},
		{name: "query", value: "https://n2api.knowsky.uk?mode=admin"},
		{name: "empty query", value: "https://n2api.knowsky.uk?"},
		{name: "fragment", value: "https://n2api.knowsky.uk#admin"},
		{name: "placeholder host", value: "https://example.com"},
		{name: "placeholder subdomain", value: "https://n2api.example.com"},
		{name: "reserved test host", value: "https://n2api.example.test"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := Load(strictConfigLookup(map[string]string{"N2API_PUBLIC_URL": tt.value}))
			assertSafeConfigError(t, err, "N2API_PUBLIC_URL", tt.value)
		})
	}
}

func TestLoadRequiresExplicitRiskAcceptance(t *testing.T) {
	tests := []struct {
		name       string
		overrides  map[string]string
		risk       string
		secretText string
	}{
		{
			name:       "public http",
			overrides:  map[string]string{"N2API_PUBLIC_URL": "http://n2api.knowsky.uk"},
			risk:       "public-http",
			secretText: "http://n2api.knowsky.uk",
		},
		{
			name:       "public bind",
			overrides:  map[string]string{"N2API_HOST": "0.0.0.0"},
			risk:       "public-bind",
			secretText: "0.0.0.0",
		},
		{
			name:       "database plaintext fallback",
			overrides:  map[string]string{"DATABASE_URL": "postgres://owner:database-secret@db.internal/n2api?sslmode=prefer"},
			risk:       "database-plaintext",
			secretText: "database-secret",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := Load(strictConfigLookup(tt.overrides))
			assertSafeConfigError(t, err, "N2API_ACCEPT_RISKS", tt.secretText)

			accepted := maps.Clone(tt.overrides)
			accepted["N2API_ACCEPT_RISKS"] = tt.risk
			if _, err := Load(strictConfigLookup(accepted)); err != nil {
				t.Fatalf("Load with accepted risk returned error: %v", err)
			}
		})
	}
}

func TestLoadAcceptsLoopbackHTTPAndRootPublicURL(t *testing.T) {
	for _, tt := range []struct {
		value string
		want  string
	}{
		{value: "http://localhost:3000", want: "http://localhost:3000"},
		{value: "http://api.localhost:3000/", want: "http://api.localhost:3000"},
		{value: "http://127.0.0.1:3000", want: "http://127.0.0.1:3000"},
		{value: "http://[::1]:3000", want: "http://[::1]:3000"},
		{value: "HTTPS://n2api.knowsky.uk/", want: "https://n2api.knowsky.uk"},
	} {
		t.Run(tt.value, func(t *testing.T) {
			cfg, err := Load(strictConfigLookup(map[string]string{"N2API_PUBLIC_URL": tt.value}))
			if err != nil {
				t.Fatalf("Load returned error: %v", err)
			}
			if cfg.PublicURL != tt.want {
				t.Fatalf("PublicURL = %q, want %q", cfg.PublicURL, tt.want)
			}
		})
	}
}

func TestLoadRejectsInvalidRiskAcceptance(t *testing.T) {
	for _, value := range []string{"all", "public-http,unknown", "public-http,,public-bind"} {
		t.Run(value, func(t *testing.T) {
			_, err := Load(strictConfigLookup(map[string]string{"N2API_ACCEPT_RISKS": value}))
			assertSafeConfigError(t, err, "N2API_ACCEPT_RISKS", value)
		})
	}
}

func TestLoadRejectsUnsafeSecrets(t *testing.T) {
	tests := []struct {
		name      string
		overrides map[string]string
		wantName  string
		secret    string
	}{
		{name: "short admin password", overrides: map[string]string{"N2API_ADMIN_PASSWORD": "too-short"}, wantName: "N2API_ADMIN_PASSWORD", secret: "too-short"},
		{name: "admin placeholder", overrides: map[string]string{"N2API_ADMIN_PASSWORD": "change-me"}, wantName: "N2API_ADMIN_PASSWORD", secret: "change-me"},
		{name: "short encryption secret", overrides: map[string]string{"N2API_ENCRYPTION_SECRET": "short-encryption-secret"}, wantName: "N2API_ENCRYPTION_SECRET", secret: "short-encryption-secret"},
		{name: "encryption placeholder", overrides: map[string]string{"N2API_ENCRYPTION_SECRET": "change-me-to-a-long-random-secret"}, wantName: "N2API_ENCRYPTION_SECRET", secret: "change-me-to-a-long-random-secret"},
		{name: "equal secrets", overrides: map[string]string{"N2API_ADMIN_PASSWORD": "same-secret-value-at-least-32-bytes", "N2API_ENCRYPTION_SECRET": "same-secret-value-at-least-32-bytes"}, wantName: "N2API_ADMIN_PASSWORD", secret: "same-secret-value-at-least-32-bytes"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := Load(strictConfigLookup(tt.overrides))
			assertSafeConfigError(t, err, tt.wantName, tt.secret)
		})
	}
}

func TestLoadRejectsUnsafeDatabaseConfiguration(t *testing.T) {
	tests := []struct {
		name  string
		value string
	}{
		{name: "invalid", value: "postgres://owner:%zz@db.internal/n2api"},
		{name: "placeholder password", value: "postgres://owner:change-me@db.internal/n2api?sslmode=require"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := Load(strictConfigLookup(map[string]string{"DATABASE_URL": tt.value}))
			assertSafeConfigError(t, err, "DATABASE_URL", tt.value)
		})
	}
}

func TestLoadDatabaseTLSModes(t *testing.T) {
	for _, sslMode := range []string{"disable", "allow", "prefer"} {
		t.Run("risk_"+sslMode, func(t *testing.T) {
			databaseURL := "postgres://owner:database-secret@db.internal/n2api?sslmode=" + sslMode
			_, err := Load(strictConfigLookup(map[string]string{"DATABASE_URL": databaseURL}))
			assertSafeConfigError(t, err, "N2API_ACCEPT_RISKS", "database-secret")

			if _, err := Load(strictConfigLookup(map[string]string{
				"DATABASE_URL":       databaseURL,
				"N2API_ACCEPT_RISKS": "database-plaintext",
			})); err != nil {
				t.Fatalf("Load with accepted database risk returned error: %v", err)
			}
		})
	}

	for name, databaseURL := range map[string]string{
		"url require":     "postgres://owner:database-secret@db.internal/n2api?sslmode=require",
		"keyword require": "host=db.internal user=owner password=database-secret dbname=n2api sslmode=require",
	} {
		t.Run(name, func(t *testing.T) {
			if _, err := Load(strictConfigLookup(map[string]string{"DATABASE_URL": databaseURL})); err != nil {
				t.Fatalf("Load returned error: %v", err)
			}
		})
	}
}

func TestLoadRejectsUnsafeUpstreamURLs(t *testing.T) {
	tests := []struct {
		name      string
		overrides map[string]string
		wantName  string
		secret    string
	}{
		{name: "http api without opt in", overrides: map[string]string{"OPENAI_API_BASE_URL": "http://upstream.internal/v1"}, wantName: "N2API_ALLOW_HTTP_API_UPSTREAMS", secret: "upstream.internal"},
		{name: "api userinfo", overrides: map[string]string{"OPENAI_API_BASE_URL": "https://owner:secret@upstream.internal/v1"}, wantName: "OPENAI_API_BASE_URL", secret: "secret"},
		{name: "api invalid host", overrides: map[string]string{"OPENAI_API_BASE_URL": "https://upstream,internal/v1"}, wantName: "OPENAI_API_BASE_URL", secret: "upstream,internal"},
		{name: "oauth auth http", overrides: map[string]string{"OPENAI_OAUTH_AUTH_URL": "http://auth.internal/authorize"}, wantName: "OPENAI_OAUTH_AUTH_URL", secret: "auth.internal"},
		{name: "oauth token invalid scheme", overrides: map[string]string{"OPENAI_OAUTH_TOKEN_URL": "ftp://auth.internal/token"}, wantName: "OPENAI_OAUTH_TOKEN_URL", secret: "auth.internal"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := Load(strictConfigLookup(tt.overrides))
			assertSafeConfigError(t, err, tt.wantName, tt.secret)
		})

	}

	if _, err := Load(strictConfigLookup(map[string]string{
		"OPENAI_API_BASE_URL":            "http://upstream.internal/v1",
		"N2API_ALLOW_HTTP_API_UPSTREAMS": "true",
	})); err != nil {
		t.Fatalf("Load with HTTP API upstream opt-in returned error: %v", err)
	}
}

func TestLoadNormalizesUpstreamURLSchemes(t *testing.T) {
	cfg, err := Load(strictConfigLookup(map[string]string{
		"OPENAI_API_BASE_URL":    "HTTPS://api.internal/v1",
		"OPENAI_OAUTH_AUTH_URL":  "HTTPS://auth.internal/authorize",
		"OPENAI_OAUTH_TOKEN_URL": "HTTPS://auth.internal/token",
	}))
	if err != nil {
		t.Fatalf("Load returned error: %v", err)
	}
	if cfg.OpenAIAPIBaseURL != "https://api.internal/v1" {
		t.Fatalf("OpenAIAPIBaseURL = %q", cfg.OpenAIAPIBaseURL)
	}
	if cfg.OpenAIOAuthAuthURL != "https://auth.internal/authorize" {
		t.Fatalf("OpenAIOAuthAuthURL = %q", cfg.OpenAIOAuthAuthURL)
	}
	if cfg.OpenAIOAuthTokenURL != "https://auth.internal/token" {
		t.Fatalf("OpenAIOAuthTokenURL = %q", cfg.OpenAIOAuthTokenURL)
	}
}

func strictConfigLookup(overrides map[string]string) func(string) string {
	values := map[string]string{
		"N2API_HOST":              "127.0.0.1",
		"N2API_PUBLIC_URL":        "https://n2api.knowsky.uk",
		"N2API_ACCEPT_RISKS":      "",
		"DATABASE_URL":            "postgres://owner:database-secret@db.internal/n2api?sslmode=require",
		"N2API_ADMIN_PASSWORD":    "strong-admin-password",
		"N2API_ENCRYPTION_SECRET": "strong-encryption-secret-at-least-32-bytes",
	}
	for key, value := range overrides {
		values[key] = value
	}
	return mapLookup(values)
}

func assertSafeConfigError(t *testing.T, err error, wantName, secret string) {
	t.Helper()
	if err == nil {
		t.Fatal("Load returned nil error")
	}
	if !strings.Contains(err.Error(), wantName) {
		t.Fatalf("Load error = %q, want variable name %s", err.Error(), wantName)
	}
	if secret != "" && strings.Contains(err.Error(), secret) {
		t.Fatalf("Load error leaked configuration value: %q", err.Error())
	}
}

func mapLookup(values map[string]string) func(string) string {
	return func(key string) string {
		if key == "N2API_ACCEPT_RISKS" {
			if value, ok := values[key]; ok {
				return value
			}
			return "public-bind,database-plaintext"
		}
		return values[key]
	}
}

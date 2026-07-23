package config

import (
	"maps"
	"net/netip"
	"slices"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/KnowSky404/N2API/backend/internal/secret"
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
	if cfg.EncryptionKeyID != secret.DefaultEncryptionKeyID {
		t.Fatalf("EncryptionKeyID = %q, want %q", cfg.EncryptionKeyID, secret.DefaultEncryptionKeyID)
	}
	if cfg.EncryptionKeyring == nil || cfg.EncryptionKeyring.PreviousKeyCount() != 0 {
		t.Fatal("default encryption keyring was not initialized without previous keys")
	}
}

func TestLoadParsesNamedEncryptionKeys(t *testing.T) {
	const currentSecret = "current-encryption-secret-at-least-32-bytes"
	const previousSecret = "previous-encryption-secret-at-least-32-bytes"
	previousKeyring, err := secret.NewKeyring(secret.EncryptionKey{ID: "previous-202606", Secret: previousSecret}, nil)
	if err != nil {
		t.Fatalf("NewKeyring returned error: %v", err)
	}
	previousCiphertext, err := previousKeyring.EncryptString("previous-provider-token")
	if err != nil {
		t.Fatalf("EncryptString returned error: %v", err)
	}

	cfg, err := Load(strictConfigLookup(map[string]string{
		"N2API_ENCRYPTION_SECRET":        currentSecret,
		"N2API_ENCRYPTION_KEY_ID":        "current-202607",
		"N2API_ENCRYPTION_PREVIOUS_KEYS": `[{"id":"previous-202606","secret":"` + previousSecret + `"}]`,
	}))
	if err != nil {
		t.Fatalf("Load returned error: %v", err)
	}
	if cfg.EncryptionKeyID != "current-202607" || cfg.EncryptionKeyring.CurrentKeyID() != "current-202607" {
		t.Fatalf("current key IDs = config:%q keyring:%q", cfg.EncryptionKeyID, cfg.EncryptionKeyring.CurrentKeyID())
	}
	if cfg.EncryptionKeyring.PreviousKeyCount() != 1 {
		t.Fatalf("PreviousKeyCount = %d, want 1", cfg.EncryptionKeyring.PreviousKeyCount())
	}
	decrypted, err := cfg.EncryptionKeyring.DecryptString(previousCiphertext)
	if err != nil {
		t.Fatalf("DecryptString returned error for previous envelope: %v", err)
	}
	if decrypted != "previous-provider-token" {
		t.Fatalf("DecryptString = %q, want previous-provider-token", decrypted)
	}
	currentCiphertext, err := cfg.EncryptionKeyring.EncryptString("current-provider-token")
	if err != nil {
		t.Fatalf("EncryptString returned error: %v", err)
	}
	if !strings.HasPrefix(currentCiphertext, "n2api:v1:current-202607:generic:") {
		t.Fatalf("EncryptString = %q, want current key envelope", currentCiphertext)
	}
}

func TestLoadRejectsInvalidEncryptionKeyConfigurationWithoutLeaks(t *testing.T) {
	const previousSecret = "previous-encryption-secret-at-least-32-bytes"
	const secondSecret = "second-previous-secret-at-least-32-bytes"
	tests := []struct {
		name      string
		overrides map[string]string
		forbidden string
	}{
		{name: "invalid current ID", overrides: map[string]string{"N2API_ENCRYPTION_KEY_ID": "invalid:key"}},
		{name: "invalid JSON", overrides: map[string]string{"N2API_ENCRYPTION_PREVIOUS_KEYS": `[{`}},
		{name: "null JSON", overrides: map[string]string{"N2API_ENCRYPTION_PREVIOUS_KEYS": `null`}},
		{name: "trailing JSON", overrides: map[string]string{"N2API_ENCRYPTION_PREVIOUS_KEYS": `[] []`}},
		{name: "unknown field", overrides: map[string]string{"N2API_ENCRYPTION_PREVIOUS_KEYS": `[{"id":"previous","secret":"` + previousSecret + `","extra":true}]`}},
		{name: "short previous secret", overrides: map[string]string{"N2API_ENCRYPTION_PREVIOUS_KEYS": `[{"id":"previous","secret":"short"}]`}, forbidden: "short"},
		{name: "placeholder previous secret", overrides: map[string]string{"N2API_ENCRYPTION_PREVIOUS_KEYS": `[{"id":"previous","secret":"change-me-to-a-long-random-secret"}]`}, forbidden: "change-me-to-a-long-random-secret"},
		{name: "same as current", overrides: map[string]string{"N2API_ENCRYPTION_PREVIOUS_KEYS": `[{"id":"previous","secret":"strong-encryption-secret-at-least-32-bytes"}]`}, forbidden: "strong-encryption-secret-at-least-32-bytes"},
		{name: "same as admin", overrides: map[string]string{"N2API_ADMIN_PASSWORD": previousSecret, "N2API_ENCRYPTION_PREVIOUS_KEYS": `[{"id":"previous","secret":"` + previousSecret + `"}]`}, forbidden: previousSecret},
		{name: "duplicate ID", overrides: map[string]string{"N2API_ENCRYPTION_PREVIOUS_KEYS": `[{"id":"previous","secret":"` + previousSecret + `"},{"id":"previous","secret":"` + secondSecret + `"}]`}},
		{name: "duplicate secret", overrides: map[string]string{"N2API_ENCRYPTION_PREVIOUS_KEYS": `[{"id":"previous-one","secret":"` + previousSecret + `"},{"id":"previous-two","secret":"` + previousSecret + `"}]`}, forbidden: previousSecret},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := Load(strictConfigLookup(tt.overrides))
			assertSafeConfigError(t, err, "N2API_ENCRYPTION", tt.forbidden)
		})
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

func TestLoadAlertDeliveryEnabled(t *testing.T) {
	base := map[string]string{
		"DATABASE_URL": "postgres://example", "N2API_ENCRYPTION_SECRET": "test-encryption-secret-at-least-32-bytes", "N2API_ADMIN_PASSWORD": "admin-password",
	}
	cfg, err := Load(mapLookup(base))
	if err != nil {
		t.Fatalf("Load default returned error: %v", err)
	}
	if cfg.AlertDeliveryEnabled {
		t.Fatal("AlertDeliveryEnabled = true, want false by default")
	}

	enabled := maps.Clone(base)
	enabled["N2API_ALERT_DELIVERY_ENABLED"] = "true"
	cfg, err = Load(mapLookup(enabled))
	if err != nil {
		t.Fatalf("Load enabled returned error: %v", err)
	}
	if !cfg.AlertDeliveryEnabled {
		t.Fatal("AlertDeliveryEnabled = false, want true")
	}

	invalid := maps.Clone(base)
	invalid["N2API_ALERT_DELIVERY_ENABLED"] = "sometimes"
	if _, err := Load(mapLookup(invalid)); err == nil {
		t.Fatal("Load invalid alert delivery flag returned nil error")
	}

	limitedPool := maps.Clone(base)
	limitedPool["DATABASE_URL"] = "postgres://example?pool_max_conns=1"
	if _, err := Load(mapLookup(limitedPool)); err != nil {
		t.Fatalf("Load disabled with one pool connection returned error: %v", err)
	}
	limitedPool["N2API_ALERT_DELIVERY_ENABLED"] = "true"
	if _, err := Load(mapLookup(limitedPool)); err == nil || !strings.Contains(err.Error(), "pool_max_conns") {
		t.Fatalf("Load enabled with one pool connection error = %v, want pool_max_conns validation", err)
	}
}

func TestLoadRequestLogExportConfig(t *testing.T) {
	base := map[string]string{
		"DATABASE_URL": "postgres://example", "N2API_ENCRYPTION_SECRET": "test-encryption-secret-at-least-32-bytes", "N2API_ADMIN_PASSWORD": "admin-password",
	}
	cfg, err := Load(mapLookup(base))
	if err != nil {
		t.Fatalf("Load default returned error: %v", err)
	}
	if cfg.RequestLogExportMaxRows != 100000 || cfg.RequestLogExportTimeout != 60*time.Second {
		t.Fatalf("default request log export config = rows %d timeout %s", cfg.RequestLogExportMaxRows, cfg.RequestLogExportTimeout)
	}

	values := maps.Clone(base)
	values["N2API_REQUEST_LOG_EXPORT_MAX_ROWS"] = "1000000"
	values["N2API_REQUEST_LOG_EXPORT_TIMEOUT_SECONDS"] = "300"
	cfg, err = Load(mapLookup(values))
	if err != nil {
		t.Fatalf("Load configured returned error: %v", err)
	}
	if cfg.RequestLogExportMaxRows != 1000000 || cfg.RequestLogExportTimeout != 300*time.Second {
		t.Fatalf("configured request log export config = rows %d timeout %s", cfg.RequestLogExportMaxRows, cfg.RequestLogExportTimeout)
	}

	for _, test := range []struct {
		name  string
		value string
	}{
		{name: "N2API_REQUEST_LOG_EXPORT_MAX_ROWS", value: "999"},
		{name: "N2API_REQUEST_LOG_EXPORT_MAX_ROWS", value: "1000001"},
		{name: "N2API_REQUEST_LOG_EXPORT_TIMEOUT_SECONDS", value: "4"},
		{name: "N2API_REQUEST_LOG_EXPORT_TIMEOUT_SECONDS", value: "301"},
	} {
		t.Run(test.name+"="+test.value, func(t *testing.T) {
			invalid := maps.Clone(base)
			invalid[test.name] = test.value
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

func TestLoadGatewayResourceBoundaryDefaultsAndOverrides(t *testing.T) {
	defaultCfg, err := Load(strictConfigLookup(nil))
	if err != nil {
		t.Fatalf("Load defaults returned error: %v", err)
	}
	if defaultCfg.GatewayMaxAcceptedRequestBodyBytes != 4<<20 ||
		defaultCfg.GatewayMaxInMemoryReplayBodyBytes != 1<<20 ||
		defaultCfg.GatewayMaxUpstreamResponseBodyBytes != 8<<20 {
		t.Fatalf("body defaults = accepted:%d replay:%d response:%d", defaultCfg.GatewayMaxAcceptedRequestBodyBytes, defaultCfg.GatewayMaxInMemoryReplayBodyBytes, defaultCfg.GatewayMaxUpstreamResponseBodyBytes)
	}
	if defaultCfg.HTTPIdleTimeout != 60*time.Second || defaultCfg.HTTPMaxHeaderBytes != 1<<20 ||
		defaultCfg.HTTPRequestBodyTimeout != 30*time.Second || defaultCfg.UpstreamResponseHeaderTimeout != 30*time.Second ||
		defaultCfg.UpstreamConnectTimeout != 10*time.Second || defaultCfg.UpstreamTLSHandshakeTimeout != 10*time.Second ||
		defaultCfg.UpstreamSSEIdleTimeout != 60*time.Second {
		t.Fatalf("timeout defaults = %+v", defaultCfg)
	}

	cfg, err := Load(strictConfigLookup(map[string]string{
		"N2API_GATEWAY_MAX_ACCEPTED_REQUEST_BODY_BYTES":  "8388608",
		"N2API_GATEWAY_MAX_IN_MEMORY_REPLAY_BODY_BYTES":  "2097152",
		"N2API_GATEWAY_MAX_UPSTREAM_RESPONSE_BODY_BYTES": "16777216",
		"N2API_HTTP_IDLE_TIMEOUT_SECONDS":                "90",
		"N2API_HTTP_MAX_HEADER_BYTES":                    "524288",
		"N2API_HTTP_REQUEST_BODY_TIMEOUT_SECONDS":        "45",
		"N2API_UPSTREAM_RESPONSE_HEADER_TIMEOUT_SECONDS": "20",
		"N2API_UPSTREAM_CONNECT_TIMEOUT_SECONDS":         "8",
		"N2API_UPSTREAM_TLS_HANDSHAKE_TIMEOUT_SECONDS":   "9",
		"N2API_UPSTREAM_SSE_IDLE_TIMEOUT_SECONDS":        "120",
	}))
	if err != nil {
		t.Fatalf("Load overrides returned error: %v", err)
	}
	if cfg.GatewayMaxAcceptedRequestBodyBytes != 8388608 || cfg.GatewayMaxInMemoryReplayBodyBytes != 2097152 || cfg.GatewayMaxUpstreamResponseBodyBytes != 16777216 ||
		cfg.HTTPIdleTimeout != 90*time.Second || cfg.HTTPMaxHeaderBytes != 524288 || cfg.HTTPRequestBodyTimeout != 45*time.Second ||
		cfg.UpstreamResponseHeaderTimeout != 20*time.Second || cfg.UpstreamConnectTimeout != 8*time.Second || cfg.UpstreamTLSHandshakeTimeout != 9*time.Second || cfg.UpstreamSSEIdleTimeout != 120*time.Second {
		t.Fatalf("resource overrides = %+v", cfg)
	}
}

func TestLoadRejectsUnsafeGatewayResourceBoundariesWithoutEchoingValues(t *testing.T) {
	tests := []struct {
		name      string
		overrides map[string]string
		wantName  string
		forbidden string
	}{
		{name: "zero accepted", overrides: map[string]string{"N2API_GATEWAY_MAX_ACCEPTED_REQUEST_BODY_BYTES": "0"}, wantName: "N2API_GATEWAY_MAX_ACCEPTED_REQUEST_BODY_BYTES", forbidden: "0"},
		{name: "negative replay", overrides: map[string]string{"N2API_GATEWAY_MAX_IN_MEMORY_REPLAY_BODY_BYTES": "-9"}, wantName: "N2API_GATEWAY_MAX_IN_MEMORY_REPLAY_BODY_BYTES", forbidden: "-9"},
		{name: "oversized response", overrides: map[string]string{"N2API_GATEWAY_MAX_UPSTREAM_RESPONSE_BODY_BYTES": "999999999"}, wantName: "N2API_GATEWAY_MAX_UPSTREAM_RESPONSE_BODY_BYTES", forbidden: "999999999"},
		{name: "invalid header", overrides: map[string]string{"N2API_HTTP_MAX_HEADER_BYTES": "header-canary"}, wantName: "N2API_HTTP_MAX_HEADER_BYTES", forbidden: "header-canary"},
		{name: "zero timeout", overrides: map[string]string{"N2API_UPSTREAM_SSE_IDLE_TIMEOUT_SECONDS": "0"}, wantName: "N2API_UPSTREAM_SSE_IDLE_TIMEOUT_SECONDS", forbidden: "0"},
		{name: "replay exceeds accepted", overrides: map[string]string{"N2API_GATEWAY_MAX_ACCEPTED_REQUEST_BODY_BYTES": "1048576", "N2API_GATEWAY_MAX_IN_MEMORY_REPLAY_BODY_BYTES": "2097152"}, wantName: "N2API_GATEWAY_MAX_IN_MEMORY_REPLAY_BODY_BYTES", forbidden: "2097152"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			_, err := Load(strictConfigLookup(test.overrides))
			if err == nil || !strings.Contains(err.Error(), test.wantName) {
				t.Fatalf("Load error = %v, want name %s", err, test.wantName)
			}
			if strings.Contains(err.Error(), test.forbidden) {
				t.Fatalf("Load error echoed unsafe value: %v", err)
			}
		})
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

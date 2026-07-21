package config

import (
	"errors"
	"fmt"
	"net"
	"net/netip"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

type Config struct {
	Host                                   string
	Port                                   int
	PublicURL                              string
	DatabaseURL                            string
	AdminUsername                          string
	AdminPassword                          string
	EncryptionSecret                       string
	OpenAIOAuthClientID                    string
	OpenAIOAuthSecret                      string
	OpenAIOAuthRedirectURL                 string
	OpenAIOAuthAuthURL                     string
	OpenAIOAuthTokenURL                    string
	OpenAIAPIBaseURL                       string
	AllowHTTPAPIUpstreams                  bool
	GatewayMaxConcurrentRequests           int
	GatewayMaxConcurrentRequestsPerAccount int
	GatewayMaxConcurrentRequestsPerKey     int
	GatewayRequestsPerMinutePerKey         int
	GatewayTokensPerMinutePerKey           int
	ProviderAccountAutoTestEnabled         bool
	ProviderAccountAutoTestInterval        time.Duration
	RequestLogRetentionRunnerEnabled       bool
	RequestLogRetentionInterval            time.Duration
	RequestLogRetentionBatchSize           int
	RequestLogExportMaxRows                int
	RequestLogExportTimeout                time.Duration
	SystemEventRetentionDays               int
	TrustedProxyCIDRs                      []netip.Prefix
	AdminLoginThrottleEnabled              bool
	AdminLoginThrottleFailures             int
	AdminLoginThrottleMaxEntries           int
	AdminSessionTTL                        time.Duration
}

const (
	defaultOpenAIOAuthClientID = "app_EMoamEEZ73f0CkXaXp7hrann"
	defaultOpenAIOAuthRedirect = "http://localhost:1455/auth/callback"
	defaultOpenAIOAuthAuthURL  = "https://auth.openai.com/oauth/authorize"
	defaultOpenAIOAuthTokenURL = "https://auth.openai.com/oauth/token"

	defaultProviderAccountAutoTestInterval = 5 * time.Minute
	minProviderAccountAutoTestInterval     = time.Minute
	defaultRequestLogRetentionInterval     = 24 * time.Hour
	minRequestLogRetentionInterval         = 5 * time.Minute
	maxRequestLogRetentionInterval         = 7 * 24 * time.Hour
	defaultRequestLogRetentionBatchSize    = 1000
	defaultRequestLogExportMaxRows         = 100000
	defaultRequestLogExportTimeout         = 60 * time.Second
	defaultSystemEventRetentionDays        = 365
	defaultAdminLoginThrottleFailures      = 5
	defaultAdminLoginThrottleMaxEntries    = 4096
	defaultAdminSessionTTLHours            = 168
	minimumAdminPasswordBytes              = 12
	minimumEncryptionSecretBytes           = 32
)

const (
	riskPublicHTTP        = "public-http"
	riskPublicBind        = "public-bind"
	riskDatabasePlaintext = "database-plaintext"
)

func Load(lookup func(string) string) (Config, error) {
	acceptedRisks, err := parseAcceptedRisks(lookup("N2API_ACCEPT_RISKS"))
	if err != nil {
		return Config{}, err
	}
	cfg := Config{
		Host:          valueOrDefault(lookup("N2API_HOST"), "0.0.0.0"),
		PublicURL:     valueOrDefault(lookup("N2API_PUBLIC_URL"), "http://localhost:3000"),
		AdminUsername: valueOrDefault(lookup("N2API_ADMIN_USERNAME"), "admin"),
		AdminPassword: lookup("N2API_ADMIN_PASSWORD"),

		DatabaseURL:            lookup("DATABASE_URL"),
		EncryptionSecret:       lookup("N2API_ENCRYPTION_SECRET"),
		OpenAIOAuthClientID:    valueOrDefault(lookup("OPENAI_OAUTH_CLIENT_ID"), defaultOpenAIOAuthClientID),
		OpenAIOAuthSecret:      lookup("OPENAI_OAUTH_CLIENT_SECRET"),
		OpenAIOAuthRedirectURL: valueOrDefault(lookup("OPENAI_OAUTH_REDIRECT_URL"), defaultOpenAIOAuthRedirect),
		OpenAIOAuthAuthURL:     valueOrDefault(lookup("OPENAI_OAUTH_AUTH_URL"), defaultOpenAIOAuthAuthURL),
		OpenAIOAuthTokenURL:    valueOrDefault(lookup("OPENAI_OAUTH_TOKEN_URL"), defaultOpenAIOAuthTokenURL),
		OpenAIAPIBaseURL:       valueOrDefault(lookup("OPENAI_API_BASE_URL"), "https://api.openai.com"),
	}
	allowHTTPAPIUpstreams, err := parseBool(lookup("N2API_ALLOW_HTTP_API_UPSTREAMS"), "N2API_ALLOW_HTTP_API_UPSTREAMS")
	if err != nil {
		return Config{}, err
	}
	cfg.AllowHTTPAPIUpstreams = allowHTTPAPIUpstreams
	adminLoginThrottleEnabled, err := parseBool(valueOrDefault(lookup("N2API_ADMIN_LOGIN_THROTTLE_ENABLED"), "true"), "N2API_ADMIN_LOGIN_THROTTLE_ENABLED")
	if err != nil {
		return Config{}, err
	}
	cfg.AdminLoginThrottleEnabled = adminLoginThrottleEnabled
	adminLoginThrottleFailures, err := parsePositiveIntWithDefault(
		lookup("N2API_ADMIN_LOGIN_THROTTLE_FAILURES"),
		"N2API_ADMIN_LOGIN_THROTTLE_FAILURES",
		defaultAdminLoginThrottleFailures,
		1,
		20,
	)
	if err != nil {
		return Config{}, err
	}
	cfg.AdminLoginThrottleFailures = adminLoginThrottleFailures
	adminLoginThrottleMaxEntries, err := parsePositiveIntWithDefault(
		lookup("N2API_ADMIN_LOGIN_THROTTLE_MAX_ENTRIES"),
		"N2API_ADMIN_LOGIN_THROTTLE_MAX_ENTRIES",
		defaultAdminLoginThrottleMaxEntries,
		128,
		16384,
	)
	if err != nil {
		return Config{}, err
	}
	cfg.AdminLoginThrottleMaxEntries = adminLoginThrottleMaxEntries
	adminSessionTTLHours, err := parsePositiveIntWithDefault(
		lookup("N2API_ADMIN_SESSION_TTL_HOURS"),
		"N2API_ADMIN_SESSION_TTL_HOURS",
		defaultAdminSessionTTLHours,
		1,
		8760,
	)
	if err != nil {
		return Config{}, err
	}
	cfg.AdminSessionTTL = time.Duration(adminSessionTTLHours) * time.Hour
	trustedProxyCIDRs, err := parseTrustedProxyCIDRs(lookup("N2API_TRUSTED_PROXY_CIDRS"))
	if err != nil {
		return Config{}, err
	}
	cfg.TrustedProxyCIDRs = trustedProxyCIDRs

	autoTestEnabled, err := parseBool(lookup("N2API_PROVIDER_ACCOUNT_AUTO_TEST_ENABLED"), "N2API_PROVIDER_ACCOUNT_AUTO_TEST_ENABLED")
	if err != nil {
		return Config{}, err
	}
	cfg.ProviderAccountAutoTestEnabled = autoTestEnabled
	requestLogRetentionRunnerEnabled, err := parseBool(lookup("N2API_REQUEST_LOG_RETENTION_RUNNER_ENABLED"), "N2API_REQUEST_LOG_RETENTION_RUNNER_ENABLED")
	if err != nil {
		return Config{}, err
	}
	cfg.RequestLogRetentionRunnerEnabled = requestLogRetentionRunnerEnabled
	requestLogRetentionIntervalSeconds, err := parsePositiveIntWithDefault(
		lookup("N2API_REQUEST_LOG_RETENTION_INTERVAL_SECONDS"),
		"N2API_REQUEST_LOG_RETENTION_INTERVAL_SECONDS",
		int(defaultRequestLogRetentionInterval/time.Second),
		int(minRequestLogRetentionInterval/time.Second),
		int(maxRequestLogRetentionInterval/time.Second),
	)
	if err != nil {
		return Config{}, err
	}
	cfg.RequestLogRetentionInterval = time.Duration(requestLogRetentionIntervalSeconds) * time.Second
	requestLogRetentionBatchSize, err := parsePositiveIntWithDefault(
		lookup("N2API_REQUEST_LOG_RETENTION_BATCH_SIZE"),
		"N2API_REQUEST_LOG_RETENTION_BATCH_SIZE",
		defaultRequestLogRetentionBatchSize,
		100,
		10000,
	)
	if err != nil {
		return Config{}, err
	}
	cfg.RequestLogRetentionBatchSize = requestLogRetentionBatchSize
	requestLogExportMaxRows, err := parsePositiveIntWithDefault(
		lookup("N2API_REQUEST_LOG_EXPORT_MAX_ROWS"),
		"N2API_REQUEST_LOG_EXPORT_MAX_ROWS",
		defaultRequestLogExportMaxRows,
		1000,
		1000000,
	)
	if err != nil {
		return Config{}, err
	}
	cfg.RequestLogExportMaxRows = requestLogExportMaxRows
	requestLogExportTimeoutSeconds, err := parsePositiveIntWithDefault(
		lookup("N2API_REQUEST_LOG_EXPORT_TIMEOUT_SECONDS"),
		"N2API_REQUEST_LOG_EXPORT_TIMEOUT_SECONDS",
		int(defaultRequestLogExportTimeout/time.Second),
		5,
		300,
	)
	if err != nil {
		return Config{}, err
	}
	cfg.RequestLogExportTimeout = time.Duration(requestLogExportTimeoutSeconds) * time.Second
	retentionDays, err := parseSystemEventRetentionDays(lookup("N2API_SYSTEM_EVENT_RETENTION_DAYS"))
	if err != nil {
		return Config{}, err
	}
	cfg.SystemEventRetentionDays = retentionDays
	autoTestIntervalSeconds, err := parseNonNegativeInt(
		lookup("N2API_PROVIDER_ACCOUNT_AUTO_TEST_INTERVAL_SECONDS"),
		"N2API_PROVIDER_ACCOUNT_AUTO_TEST_INTERVAL_SECONDS",
	)
	if err != nil {
		return Config{}, err
	}
	if autoTestIntervalSeconds == 0 {
		cfg.ProviderAccountAutoTestInterval = defaultProviderAccountAutoTestInterval
	} else {
		cfg.ProviderAccountAutoTestInterval = time.Duration(autoTestIntervalSeconds) * time.Second
	}
	if cfg.ProviderAccountAutoTestEnabled && cfg.ProviderAccountAutoTestInterval < minProviderAccountAutoTestInterval {
		return Config{}, fmt.Errorf("N2API_PROVIDER_ACCOUNT_AUTO_TEST_INTERVAL_SECONDS must be at least 60 when auto test is enabled")
	}

	port, err := parsePort(valueOrDefault(lookup("N2API_PORT"), "3000"))
	if err != nil {
		return Config{}, err
	}
	cfg.Port = port
	maxConcurrent, err := parseNonNegativeInt(lookup("N2API_GATEWAY_MAX_CONCURRENT_REQUESTS"), "N2API_GATEWAY_MAX_CONCURRENT_REQUESTS")
	if err != nil {
		return Config{}, err
	}
	cfg.GatewayMaxConcurrentRequests = maxConcurrent
	maxConcurrentPerAccount, err := parseNonNegativeInt(lookup("N2API_GATEWAY_MAX_CONCURRENT_REQUESTS_PER_ACCOUNT"), "N2API_GATEWAY_MAX_CONCURRENT_REQUESTS_PER_ACCOUNT")
	if err != nil {
		return Config{}, err
	}
	cfg.GatewayMaxConcurrentRequestsPerAccount = maxConcurrentPerAccount
	maxConcurrentPerKey, err := parseNonNegativeInt(lookup("N2API_GATEWAY_MAX_CONCURRENT_REQUESTS_PER_KEY"), "N2API_GATEWAY_MAX_CONCURRENT_REQUESTS_PER_KEY")
	if err != nil {
		return Config{}, err
	}
	cfg.GatewayMaxConcurrentRequestsPerKey = maxConcurrentPerKey
	requestsPerMinute, err := parseNonNegativeInt(lookup("N2API_GATEWAY_REQUESTS_PER_MINUTE_PER_KEY"), "N2API_GATEWAY_REQUESTS_PER_MINUTE_PER_KEY")
	if err != nil {
		return Config{}, err
	}
	cfg.GatewayRequestsPerMinutePerKey = requestsPerMinute
	tokensPerMinute, err := parseNonNegativeInt(lookup("N2API_GATEWAY_TOKENS_PER_MINUTE_PER_KEY"), "N2API_GATEWAY_TOKENS_PER_MINUTE_PER_KEY")
	if err != nil {
		return Config{}, err
	}
	cfg.GatewayTokensPerMinutePerKey = tokensPerMinute

	if cfg.DatabaseURL == "" {
		return Config{}, errors.New("DATABASE_URL is required")
	}
	if cfg.EncryptionSecret == "" {
		return Config{}, errors.New("N2API_ENCRYPTION_SECRET is required")
	}
	if cfg.AdminPassword == "" {
		return Config{}, errors.New("N2API_ADMIN_PASSWORD is required")
	}
	if err := validateStartupSecurity(&cfg, acceptedRisks); err != nil {
		return Config{}, err
	}

	return cfg, nil
}

func (c Config) Addr() string {
	return fmt.Sprintf("%s:%d", c.Host, c.Port)
}

func valueOrDefault(value, fallback string) string {
	if value == "" {
		return fallback
	}
	return value
}

func parsePort(value string) (int, error) {
	port, err := strconv.Atoi(value)
	if err != nil {
		return 0, fmt.Errorf("N2API_PORT must be a number: %w", err)
	}
	if port < 1 || port > 65535 {
		return 0, fmt.Errorf("N2API_PORT must be between 1 and 65535")
	}
	return port, nil
}

func parseNonNegativeInt(value, name string) (int, error) {
	if value == "" {
		return 0, nil
	}
	parsed, err := strconv.Atoi(value)
	if err != nil {
		return 0, fmt.Errorf("%s must be a number: %w", name, err)
	}
	if parsed < 0 {
		return 0, fmt.Errorf("%s must be greater than or equal to 0", name)
	}
	return parsed, nil
}

func parsePositiveIntWithDefault(value, name string, fallback, minimum, maximum int) (int, error) {
	if strings.TrimSpace(value) == "" {
		return fallback, nil
	}
	parsed, err := strconv.Atoi(value)
	if err != nil {
		return 0, fmt.Errorf("%s must be a number: %w", name, err)
	}
	if parsed < minimum || parsed > maximum {
		return 0, fmt.Errorf("%s must be between %d and %d", name, minimum, maximum)
	}
	return parsed, nil
}

func parseBool(value, name string) (bool, error) {
	if value == "" {
		return false, nil
	}
	parsed, err := strconv.ParseBool(value)
	if err != nil {
		return false, fmt.Errorf("%s must be a boolean: %w", name, err)
	}
	return parsed, nil
}

func parseSystemEventRetentionDays(value string) (int, error) {
	if strings.TrimSpace(value) == "" {
		return defaultSystemEventRetentionDays, nil
	}
	days, err := strconv.Atoi(value)
	if err != nil {
		return 0, fmt.Errorf("N2API_SYSTEM_EVENT_RETENTION_DAYS must be a number: %w", err)
	}
	if days != 0 && (days < 30 || days > 3650) {
		return 0, fmt.Errorf("N2API_SYSTEM_EVENT_RETENTION_DAYS must be 0 or between 30 and 3650")
	}
	return days, nil
}

func parseTrustedProxyCIDRs(value string) ([]netip.Prefix, error) {
	if strings.TrimSpace(value) == "" {
		return nil, nil
	}
	prefixes := make([]netip.Prefix, 0)
	seen := make(map[netip.Prefix]struct{})
	for _, raw := range strings.Split(value, ",") {
		raw = strings.TrimSpace(raw)
		prefix, err := netip.ParsePrefix(raw)
		if err != nil {
			return nil, fmt.Errorf("N2API_TRUSTED_PROXY_CIDRS contains invalid CIDR %q: %w", raw, err)
		}
		if prefix.Addr().Is4In6() {
			if prefix.Bits() < 96 {
				return nil, fmt.Errorf("N2API_TRUSTED_PROXY_CIDRS contains invalid mapped IPv4 CIDR %q", raw)
			}
			prefix = netip.PrefixFrom(prefix.Addr().Unmap(), prefix.Bits()-96)
		}
		prefix = prefix.Masked()
		if _, ok := seen[prefix]; ok {
			continue
		}
		seen[prefix] = struct{}{}
		prefixes = append(prefixes, prefix)
	}
	return prefixes, nil
}

func parseAcceptedRisks(value string) (map[string]struct{}, error) {
	accepted := make(map[string]struct{})
	if strings.TrimSpace(value) == "" {
		return accepted, nil
	}
	allowed := map[string]struct{}{
		riskPublicHTTP:        {},
		riskPublicBind:        {},
		riskDatabasePlaintext: {},
	}
	for _, raw := range strings.Split(value, ",") {
		risk := strings.TrimSpace(raw)
		if risk == "" {
			return nil, errors.New("N2API_ACCEPT_RISKS must contain only comma-separated documented risk names")
		}
		if _, ok := allowed[risk]; !ok {
			return nil, errors.New("N2API_ACCEPT_RISKS must contain only comma-separated documented risk names")
		}
		accepted[risk] = struct{}{}
	}
	return accepted, nil
}

func validateStartupSecurity(cfg *Config, acceptedRisks map[string]struct{}) error {
	publicURL, err := validatePublicURL(cfg.PublicURL, acceptedRisks)
	if err != nil {
		return err
	}
	cfg.PublicURL = publicURL
	if !isLoopbackHost(cfg.Host) && !acceptsRisk(acceptedRisks, riskPublicBind) {
		return errors.New("N2API_ACCEPT_RISKS must include public-bind when N2API_HOST is not loopback")
	}
	if err := validateSecrets(cfg.AdminPassword, cfg.EncryptionSecret); err != nil {
		return err
	}
	if err := validateDatabaseURL(cfg.DatabaseURL, acceptedRisks); err != nil {
		return err
	}
	openAIAPIBaseURL, err := validateUpstreamURL("OPENAI_API_BASE_URL", cfg.OpenAIAPIBaseURL, cfg.AllowHTTPAPIUpstreams)
	if err != nil {
		return err
	}
	cfg.OpenAIAPIBaseURL = openAIAPIBaseURL
	openAIOAuthAuthURL, err := validateUpstreamURL("OPENAI_OAUTH_AUTH_URL", cfg.OpenAIOAuthAuthURL, false)
	if err != nil {
		return err
	}
	cfg.OpenAIOAuthAuthURL = openAIOAuthAuthURL
	openAIOAuthTokenURL, err := validateUpstreamURL("OPENAI_OAUTH_TOKEN_URL", cfg.OpenAIOAuthTokenURL, false)
	if err != nil {
		return err
	}
	cfg.OpenAIOAuthTokenURL = openAIOAuthTokenURL
	return nil
}

func validatePublicURL(value string, acceptedRisks map[string]struct{}) (string, error) {
	parsed, err := url.Parse(value)
	if err != nil || !parsed.IsAbs() || parsed.Opaque != "" || validURLHost(parsed.Host) == "" {
		return "", errors.New("N2API_PUBLIC_URL must be an absolute HTTP or HTTPS origin")
	}
	scheme := strings.ToLower(parsed.Scheme)
	if scheme != "http" && scheme != "https" {
		return "", errors.New("N2API_PUBLIC_URL must be an absolute HTTP or HTTPS origin")
	}
	if parsed.User != nil || parsed.ForceQuery || parsed.RawQuery != "" || parsed.Fragment != "" || (parsed.Path != "" && parsed.Path != "/") {
		return "", errors.New("N2API_PUBLIC_URL must not contain credentials, query, fragment, or a non-root path")
	}
	if isPlaceholderHost(parsed.Hostname()) {
		return "", errors.New("N2API_PUBLIC_URL must not use a placeholder host")
	}
	if scheme == "http" && !isLoopbackHost(parsed.Hostname()) && !acceptsRisk(acceptedRisks, riskPublicHTTP) {
		return "", errors.New("N2API_ACCEPT_RISKS must include public-http when N2API_PUBLIC_URL uses HTTP with a non-loopback host")
	}
	parsed.Scheme = scheme
	parsed.Path = ""
	parsed.RawPath = ""
	return parsed.String(), nil
}

func validateSecrets(adminPassword, encryptionSecret string) error {
	if len(adminPassword) < minimumAdminPasswordBytes || isKnownPlaceholder(adminPassword) {
		return fmt.Errorf("N2API_ADMIN_PASSWORD must be at least %d bytes and must not be a known placeholder", minimumAdminPasswordBytes)
	}
	if len(encryptionSecret) < minimumEncryptionSecretBytes || isKnownPlaceholder(encryptionSecret) {
		return fmt.Errorf("N2API_ENCRYPTION_SECRET must be at least %d bytes and must not be a known placeholder", minimumEncryptionSecretBytes)
	}
	if adminPassword == encryptionSecret {
		return errors.New("N2API_ADMIN_PASSWORD and N2API_ENCRYPTION_SECRET must be different")
	}
	return nil
}

func validateDatabaseURL(value string, acceptedRisks map[string]struct{}) error {
	poolConfig, err := pgxpool.ParseConfig(value)
	if err != nil {
		return errors.New("DATABASE_URL must be a valid PostgreSQL connection string")
	}
	if isKnownPlaceholder(poolConfig.ConnConfig.Password) {
		return errors.New("DATABASE_URL must not contain a placeholder password")
	}
	permitsPlaintext := poolConfig.ConnConfig.TLSConfig == nil
	for _, fallback := range poolConfig.ConnConfig.Fallbacks {
		if fallback.TLSConfig == nil {
			permitsPlaintext = true
			break
		}
	}
	if permitsPlaintext && !acceptsRisk(acceptedRisks, riskDatabasePlaintext) {
		return errors.New("N2API_ACCEPT_RISKS must include database-plaintext when DATABASE_URL permits a plaintext connection")
	}
	return nil
}

func validateUpstreamURL(name, value string, allowHTTP bool) (string, error) {
	parsed, err := url.Parse(value)
	if err != nil || !parsed.IsAbs() || parsed.Opaque != "" || validURLHost(parsed.Host) == "" || parsed.User != nil {
		return "", fmt.Errorf("%s must be an absolute HTTP or HTTPS URL without credentials", name)
	}
	scheme := strings.ToLower(parsed.Scheme)
	if scheme != "http" && scheme != "https" {
		return "", fmt.Errorf("%s must use HTTP or HTTPS", name)
	}
	if scheme == "http" && !allowHTTP {
		if name == "OPENAI_API_BASE_URL" {
			return "", errors.New("N2API_ALLOW_HTTP_API_UPSTREAMS must be true when OPENAI_API_BASE_URL uses HTTP")
		}
		return "", fmt.Errorf("%s must use HTTPS", name)
	}
	parsed.Scheme = scheme
	return parsed.String(), nil
}

func acceptsRisk(accepted map[string]struct{}, risk string) bool {
	_, ok := accepted[risk]
	return ok
}

func validURLHost(raw string) string {
	host := strings.TrimSpace(raw)
	if host == "" || strings.ContainsAny(host, ",/\\?#@\r\n\t ") {
		return ""
	}
	if strings.Count(host, ":") > 1 && !strings.HasPrefix(host, "[") {
		return ""
	}
	parsed, err := url.Parse("//" + host)
	if err != nil || parsed.Host != host || parsed.User != nil || parsed.Path != "" || parsed.RawQuery != "" || parsed.Fragment != "" {
		return ""
	}
	if parsed.Hostname() == "" || strings.HasSuffix(host, ":") {
		return ""
	}
	if port := parsed.Port(); port != "" {
		value, err := strconv.ParseUint(port, 10, 16)
		if err != nil || value == 0 {
			return ""
		}
	}
	return host
}

func isLoopbackHost(value string) bool {
	host := strings.TrimSuffix(strings.ToLower(strings.TrimSpace(value)), ".")
	host = strings.TrimPrefix(strings.TrimSuffix(host, "]"), "[")
	if host == "localhost" || strings.HasSuffix(host, ".localhost") {
		return true
	}
	if ip := net.ParseIP(host); ip != nil {
		return ip.IsLoopback()
	}
	return false
}

func isPlaceholderHost(value string) bool {
	host := strings.TrimSuffix(strings.ToLower(strings.TrimSpace(value)), ".")
	if host == "example.com" || strings.HasSuffix(host, ".example.com") ||
		host == "example.net" || strings.HasSuffix(host, ".example.net") ||
		host == "example.org" || strings.HasSuffix(host, ".example.org") ||
		host == "example" || strings.HasSuffix(host, ".example") ||
		host == "test" || strings.HasSuffix(host, ".test") ||
		host == "invalid" || strings.HasSuffix(host, ".invalid") {
		return true
	}
	return strings.Contains(host, "your-domain") || strings.Contains(host, "change-me")
}

func isKnownPlaceholder(value string) bool {
	normalized := strings.ToLower(strings.TrimSpace(value))
	if normalized == "" {
		return false
	}
	return strings.Contains(normalized, "change-me") ||
		strings.Contains(normalized, "replace-me") ||
		normalized == "changeme" ||
		normalized == "password" ||
		normalized == "admin" ||
		normalized == "your-password" ||
		normalized == "your-secret"
}

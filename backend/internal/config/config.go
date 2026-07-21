package config

import (
	"errors"
	"fmt"
	"net/netip"
	"strconv"
	"strings"
	"time"
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
	SystemEventRetentionDays               int
	TrustedProxyCIDRs                      []netip.Prefix
}

const (
	defaultOpenAIOAuthClientID = "app_EMoamEEZ73f0CkXaXp7hrann"
	defaultOpenAIOAuthRedirect = "http://localhost:1455/auth/callback"
	defaultOpenAIOAuthAuthURL  = "https://auth.openai.com/oauth/authorize"
	defaultOpenAIOAuthTokenURL = "https://auth.openai.com/oauth/token"

	defaultProviderAccountAutoTestInterval = 5 * time.Minute
	minProviderAccountAutoTestInterval     = time.Minute
	defaultSystemEventRetentionDays        = 365
)

func Load(lookup func(string) string) (Config, error) {
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

package admin

import (
	"context"
	"errors"
)

const ConfigurationRedactedValue = "[redacted]"

type ConfigurationSnapshot struct {
	RoutingPools          []ConfigurationRoutingPool          `json:"routingPools"`
	APIKeyTemplates       []ConfigurationAPIKeyTemplate       `json:"apiKeyTemplates"`
	ProviderAccounts      []ConfigurationProviderAccount      `json:"providerAccounts"`
	ModelSettings         ModelSettings                       `json:"modelSettings"`
	UsagePricing          UsagePricing                        `json:"usagePricing"`
	GatewaySettings       GatewaySettings                     `json:"gatewaySettings"`
	FingerprintProfiles   []ConfigurationFingerprintProfile   `json:"fingerprintProfiles"`
	ErrorPassthroughRules []ConfigurationErrorPassthroughRule `json:"errorPassthroughRules"`

	ModelSettingsPresent   bool `json:"-"`
	UsagePricingPresent    bool `json:"-"`
	GatewaySettingsPresent bool `json:"-"`
}

type ConfigurationRoutingPool struct {
	Ref         string                               `json:"ref"`
	Name        string                               `json:"name"`
	Description string                               `json:"description"`
	Enabled     bool                                 `json:"enabled"`
	FallbackRef string                               `json:"fallbackRef,omitempty"`
	Accounts    []ConfigurationRoutingPoolMembership `json:"accounts"`
}

type ConfigurationRoutingPoolMembership struct {
	AccountRef string `json:"accountRef"`
	Priority   int    `json:"priority"`
}

type ConfigurationAPIKeyTemplate struct {
	Ref                   string   `json:"ref"`
	Name                  string   `json:"name"`
	Enabled               bool     `json:"enabled"`
	ModelPolicy           string   `json:"modelPolicy"`
	AllowedModels         []string `json:"allowedModels"`
	RequestsPerMinute     int      `json:"requestsPerMinute"`
	TokensPerMinute       int      `json:"tokensPerMinute"`
	RequestBudget24h      int      `json:"requestBudget24h"`
	TokenBudget24h        int      `json:"tokenBudget24h"`
	CostBudgetMicrousd24h int64    `json:"costBudgetMicrousd24h"`
	RequestBudget30d      int      `json:"requestBudget30d"`
	TokenBudget30d        int      `json:"tokenBudget30d"`
	CostBudgetMicrousd30d int64    `json:"costBudgetMicrousd30d"`
	RoutingPoolRef        string   `json:"routingPoolRef,omitempty"`
}

type ConfigurationProviderAccount struct {
	Ref                   string                       `json:"ref"`
	Provider              string                       `json:"provider"`
	AccountType           string                       `json:"accountType"`
	Name                  string                       `json:"name"`
	BaseURL               string                       `json:"baseUrl,omitempty"`
	Enabled               bool                         `json:"enabled"`
	Priority              int                          `json:"priority"`
	LoadFactor            int                          `json:"loadFactor"`
	MaxConcurrentRequests int                          `json:"maxConcurrentRequests"`
	FingerprintProfileRef string                       `json:"fingerprintProfileRef,omitempty"`
	Models                []ConfigurationProviderModel `json:"models"`
}

type ConfigurationProviderModel struct {
	Model   string `json:"model"`
	Enabled bool   `json:"enabled"`
	Source  string `json:"source"`
}

type ConfigurationFingerprintProfile struct {
	Ref            string            `json:"ref"`
	SystemKey      string            `json:"systemKey,omitempty"`
	Name           string            `json:"name"`
	Description    string            `json:"description"`
	UserAgent      string            `json:"userAgent"`
	TLSFingerprint string            `json:"tlsFingerprint"`
	Headers        map[string]string `json:"headers"`
	Enabled        bool              `json:"enabled"`
}

type ConfigurationErrorPassthroughRule struct {
	Ref         string `json:"ref"`
	Pattern     string `json:"pattern"`
	MatchType   string `json:"matchType"`
	Description string `json:"description"`
	Enabled     bool   `json:"enabled"`
	Priority    int    `json:"priority"`
}

type configurationExportRepository interface {
	ExportConfigurationSnapshot(ctx context.Context) (ConfigurationSnapshot, error)
}

func (s *Service) ExportConfiguration(ctx context.Context) (ConfigurationSnapshot, error) {
	repo, ok := s.repo.(configurationExportRepository)
	if !ok {
		return ConfigurationSnapshot{}, errors.New("configuration export repository is not configured")
	}
	snapshot, err := repo.ExportConfigurationSnapshot(ctx)
	if err != nil {
		return ConfigurationSnapshot{}, err
	}
	if snapshot.ModelSettingsPresent {
		snapshot.ModelSettings, err = normalizeModelSettings(snapshot.ModelSettings)
	} else {
		snapshot.ModelSettings = defaultModelSettings()
	}
	if err != nil {
		return ConfigurationSnapshot{}, err
	}
	if snapshot.UsagePricingPresent {
		snapshot.UsagePricing, err = normalizeUsagePricing(snapshot.UsagePricing)
	} else {
		snapshot.UsagePricing = defaultUsagePricing()
	}
	if err != nil {
		return ConfigurationSnapshot{}, err
	}
	if snapshot.GatewaySettingsPresent {
		snapshot.GatewaySettings, err = normalizeGatewaySettings(snapshot.GatewaySettings)
	} else {
		snapshot.GatewaySettings, err = normalizeGatewaySettings(s.defaultGatewaySettings)
	}
	if err != nil {
		return ConfigurationSnapshot{}, err
	}
	return snapshot, nil
}

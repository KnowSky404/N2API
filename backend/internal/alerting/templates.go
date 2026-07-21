package alerting

import "github.com/KnowSky404/N2API/backend/internal/systemevent"

const (
	OAuthRefreshRepeatedTemplateKey       = "oauth-refresh-repeated-v1"
	RequestLogRetentionFailedTemplateKey  = "request-log-retention-failed-v1"
	ProviderAutoTestFailedTemplateKey     = "provider-auto-test-failed-v1"
	ProviderAccountExpiredTemplateKey     = "provider-account-expired-v1"
	ProviderAccountCircuitOpenTemplateKey = "provider-account-circuit-open-v1"
)

var ruleTemplateCatalog = []RuleTemplate{
	{
		Key:                      OAuthRefreshRepeatedTemplateKey,
		Name:                     "Repeated OAuth refresh failures",
		Enabled:                  false,
		Category:                 systemevent.CategoryOAuth,
		Severity:                 systemevent.SeverityWarning,
		EventAction:              systemevent.ActionOAuthRefreshAutomaticFailed,
		RecoveryAction:           systemevent.ActionOAuthRefreshAutomaticSucceeded,
		AggregationCount:         3,
		AggregationWindowSeconds: 900,
		CooldownSeconds:          3600,
		DeduplicationScope:       DeduplicationScopeTarget,
		NotifyRecovery:           true,
	},
	{
		Key:                RequestLogRetentionFailedTemplateKey,
		Name:               "Request log retention failures",
		Enabled:            false,
		Category:           systemevent.CategoryScheduler,
		EventAction:        systemevent.ActionSchedulerRequestLogRetentionFailed,
		RecoveryAction:     systemevent.ActionSchedulerRequestLogRetentionSucceeded,
		AggregationCount:   1,
		CooldownSeconds:    86400,
		DeduplicationScope: DeduplicationScopeRule,
		NotifyRecovery:     true,
	},
	{
		Key:                      ProviderAutoTestFailedTemplateKey,
		Name:                     "Provider account auto-test failures",
		Enabled:                  false,
		Category:                 systemevent.CategoryScheduler,
		EventAction:              systemevent.ActionSchedulerProviderAutoTestFailed,
		RecoveryAction:           systemevent.ActionSchedulerProviderAutoTestCompleted,
		AggregationCount:         2,
		AggregationWindowSeconds: 900,
		CooldownSeconds:          3600,
		DeduplicationScope:       DeduplicationScopeTarget,
		NotifyRecovery:           true,
	},
	{
		Key:                ProviderAccountExpiredTemplateKey,
		Name:               "Provider account expiry",
		Enabled:            false,
		Category:           systemevent.CategoryRuntime,
		Severity:           systemevent.SeverityWarning,
		EventAction:        systemevent.ActionProviderAccountExpired,
		RecoveryAction:     systemevent.ActionProviderAccountRecovered,
		AggregationCount:   1,
		CooldownSeconds:    86400,
		DeduplicationScope: DeduplicationScopeTarget,
		NotifyRecovery:     true,
	},
	{
		Key:                ProviderAccountCircuitOpenTemplateKey,
		Name:               "Provider account circuit open",
		Enabled:            false,
		Category:           systemevent.CategoryRuntime,
		Severity:           systemevent.SeverityWarning,
		EventAction:        systemevent.ActionProviderAccountCircuitOpened,
		RecoveryAction:     systemevent.ActionProviderAccountRecovered,
		AggregationCount:   1,
		CooldownSeconds:    3600,
		DeduplicationScope: DeduplicationScopeTarget,
		NotifyRecovery:     true,
	},
}

func RuleTemplates() []RuleTemplate {
	return append([]RuleTemplate(nil), ruleTemplateCatalog...)
}

func ruleTemplate(key string) (RuleTemplate, bool) {
	for _, template := range ruleTemplateCatalog {
		if template.Key == key {
			return template, true
		}
	}
	return RuleTemplate{}, false
}

func (template RuleTemplate) rule(actionID int64) Rule {
	return Rule{
		TemplateKey: template.Key, Name: template.Name, ActionID: actionID, Enabled: template.Enabled,
		Category: template.Category, Severity: template.Severity, EventAction: template.EventAction,
		RecoveryAction: template.RecoveryAction, AggregationCount: template.AggregationCount,
		AggregationWindowSeconds: template.AggregationWindowSeconds, CooldownSeconds: template.CooldownSeconds,
		DeduplicationScope: template.DeduplicationScope, NotifyRecovery: template.NotifyRecovery,
	}
}

package alerting

import "github.com/KnowSky404/N2API/backend/internal/systemevent"

const OAuthRefreshRepeatedTemplateKey = "oauth-refresh-repeated-v1"

var ruleTemplateCatalog = []RuleTemplate{{
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
}}

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

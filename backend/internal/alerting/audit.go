package alerting

import (
	"context"

	"github.com/KnowSky404/N2API/backend/internal/systemevent"
)

func withAuditIntent(ctx context.Context, action systemevent.Action, targetType string) context.Context {
	return systemevent.WithIntent(ctx, systemevent.EventIntent{
		Category: systemevent.CategoryAudit,
		Severity: systemevent.SeverityInfo,
		Action:   action,
		Outcome:  systemevent.OutcomeSuccess,
		Target:   systemevent.Target{Type: targetType},
	})
}

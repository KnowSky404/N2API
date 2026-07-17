package admin

import (
	"context"
	"strconv"

	"github.com/KnowSky404/N2API/backend/internal/systemevent"
)

func withAuditIntent(ctx context.Context, action systemevent.Action, targetType, targetID, targetName string, metadata map[string]any) context.Context {
	return systemevent.WithIntent(ctx, systemevent.EventIntent{
		Category: systemevent.CategoryAudit,
		Severity: systemevent.SeverityInfo,
		Action:   action,
		Outcome:  systemevent.OutcomeSuccess,
		Target: systemevent.Target{
			Type: targetType,
			ID:   targetID,
			Name: targetName,
		},
		Metadata: metadata,
	})
}

func withSecurityIntent(ctx context.Context, action systemevent.Action, targetType, targetID, targetName string) context.Context {
	return systemevent.WithIntent(ctx, systemevent.EventIntent{
		Category: systemevent.CategorySecurity,
		Severity: systemevent.SeverityInfo,
		Action:   action,
		Outcome:  systemevent.OutcomeSuccess,
		Target: systemevent.Target{
			Type: targetType,
			ID:   targetID,
			Name: targetName,
		},
	})
}

func withSchedulerIntent(ctx context.Context, action systemevent.Action, targetType string, metadata map[string]any) context.Context {
	return systemevent.WithIntent(ctx, systemevent.EventIntent{
		Category: systemevent.CategoryScheduler,
		Severity: systemevent.SeverityInfo,
		Action:   action,
		Outcome:  systemevent.OutcomeSuccess,
		Target:   systemevent.Target{Type: targetType},
		Metadata: metadata,
	})
}

func withAuthenticatedActor(ctx context.Context, current Admin) context.Context {
	request, ok := systemevent.FromContext(ctx)
	if !ok {
		request = systemevent.RequestContext{CorrelationID: systemevent.NewCorrelationID()}
	}
	request.Actor = systemevent.Actor{Type: systemevent.ActorAdmin, ID: current.ID, Name: current.Username}
	return systemevent.WithRequestContext(ctx, request)
}

func auditID(id int64) string {
	if id <= 0 {
		return ""
	}
	return strconv.FormatInt(id, 10)
}

func changedFields(fields ...string) map[string]any {
	return map[string]any{"changed_fields": fields}
}

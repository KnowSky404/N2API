package httpapi

import (
	"context"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/KnowSky404/N2API/backend/internal/admin"
	"github.com/KnowSky404/N2API/backend/internal/systemevent"
)

type SystemEventRecorder interface {
	Insert(ctx context.Context, event systemevent.Event) error
}

func withSystemEventRequestContext(mux *http.ServeMux, recorder SystemEventRecorder) http.Handler {
	return withSystemEventRequestContextAround(mux, mux, recorder)
}

func withSystemEventRequestContextAround(routes *http.ServeMux, next http.Handler, recorder SystemEventRecorder) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requestID := systemevent.NormalizeCorrelationID(r.Header.Get("X-Request-ID"))
		_, pattern := routes.Handler(r)
		request := systemevent.RequestContext{
			CorrelationID: requestID,
			SourceIP:      requestInfoForRequest(r).ClientIP,
			HTTPMethod:    r.Method,
			RoutePattern:  pattern,
			Actor:         systemevent.Actor{Type: systemevent.ActorSystem},
		}
		w.Header().Set("X-Request-ID", requestID)
		// The recorder is intentionally carried as a server option for HTTP-only
		// security events. Ordinary request middleware never fabricates events.
		_ = recorder
		next.ServeHTTP(w, r.WithContext(systemevent.WithRequestContext(r.Context(), request)))
	})
}

func withAdminEventActor(r *http.Request, currentAdmin admin.Admin) *http.Request {
	request, ok := systemevent.FromContext(r.Context())
	if !ok {
		request = systemevent.RequestContext{
			CorrelationID: systemevent.NewCorrelationID(),
			SourceIP:      requestInfoForRequest(r).ClientIP,
			HTTPMethod:    r.Method,
			RoutePattern:  r.Pattern,
		}
	}
	request.Actor = systemevent.Actor{Type: systemevent.ActorAdmin, ID: currentAdmin.ID, Name: currentAdmin.Username}
	if request.RoutePattern == "" {
		request.RoutePattern = r.Pattern
	}
	return r.WithContext(systemevent.WithRequestContext(r.Context(), request))
}

func buildSystemEventFilter(r *http.Request) (admin.SystemEventFilter, error) {
	filter := admin.SystemEventFilter{
		Cursor:     r.URL.Query().Get("cursor"),
		Category:   systemevent.Category(r.URL.Query().Get("category")),
		Outcome:    systemevent.Outcome(r.URL.Query().Get("outcome")),
		Severity:   systemevent.Severity(r.URL.Query().Get("severity")),
		Action:     systemevent.Action(r.URL.Query().Get("action")),
		Actor:      r.URL.Query().Get("actor"),
		TargetType: r.URL.Query().Get("targetType"),
		TargetID:   r.URL.Query().Get("targetId"),
		Query:      r.URL.Query().Get("q"),
	}
	if raw := strings.TrimSpace(r.URL.Query().Get("limit")); raw != "" {
		limit, err := strconv.Atoi(raw)
		if err != nil {
			return admin.SystemEventFilter{}, admin.ErrInvalidInput
		}
		filter.Limit = limit
	}
	if raw := strings.TrimSpace(r.URL.Query().Get("since")); raw != "" {
		seconds, err := strconv.ParseInt(raw, 10, 64)
		if err != nil || seconds < 0 {
			return admin.SystemEventFilter{}, admin.ErrInvalidInput
		}
		filter.Since = time.Unix(seconds, 0).UTC()
	}
	return filter, nil
}

func systemEventRecorderFromOptions(options ...any) SystemEventRecorder {
	for _, option := range options {
		if recorder, ok := option.(SystemEventRecorder); ok {
			return recorder
		}
	}
	return nil
}

func recordHTTPSystemEvent(ctx context.Context, recorder SystemEventRecorder, intent systemevent.EventIntent, statusCode int, duration time.Duration) error {
	if recorder == nil {
		return nil
	}
	event := systemevent.BuildEvent(ctx, intent, intent.Target, time.Now().UTC(), duration)
	event.StatusCode = &statusCode
	return recorder.Insert(ctx, event)
}

type providerBatchAudit struct {
	id           string
	action       systemevent.Action
	requestedIDs []int64
	succeededIDs []int64
	failedIDs    []int64
	errorCode    string
}

type statusCapturingResponseWriter struct {
	http.ResponseWriter
	statusCode int
}

func (w *statusCapturingResponseWriter) WriteHeader(statusCode int) {
	if w.statusCode != 0 {
		return
	}
	w.statusCode = statusCode
	w.ResponseWriter.WriteHeader(statusCode)
}

func (w *statusCapturingResponseWriter) Write(body []byte) (int, error) {
	if w.statusCode == 0 {
		w.statusCode = http.StatusOK
	}
	return w.ResponseWriter.Write(body)
}

func (w *statusCapturingResponseWriter) Unwrap() http.ResponseWriter { return w.ResponseWriter }

func (w *statusCapturingResponseWriter) Flush() {
	if w.statusCode == 0 {
		w.WriteHeader(http.StatusOK)
	}
	if flusher, ok := w.ResponseWriter.(http.Flusher); ok {
		flusher.Flush()
	}
}

func recordAdminMutationFailure(ctx context.Context, recorder SystemEventRecorder, r *http.Request, statusCode int, duration time.Duration) {
	if statusCode < http.StatusBadRequest {
		return
	}
	action, category, targetType, ok := adminFailureEventForRequest(r)
	if !ok {
		return
	}
	severity := systemevent.SeverityWarning
	if statusCode >= http.StatusInternalServerError {
		severity = systemevent.SeverityError
	}
	_ = recordHTTPSystemEvent(ctx, recorder, systemevent.EventIntent{
		Category: category, Severity: severity, Action: action, Outcome: systemevent.OutcomeFailure,
		Target:    systemevent.Target{Type: targetType, ID: r.PathValue("id")},
		ErrorCode: adminFailureErrorCode(statusCode),
	}, statusCode, duration)
}

func adminFailureErrorCode(statusCode int) string {
	switch statusCode {
	case http.StatusBadRequest:
		return "invalid_input"
	case http.StatusUnauthorized:
		return "unauthorized"
	case http.StatusForbidden:
		return "forbidden"
	case http.StatusNotFound:
		return "not_found"
	case http.StatusConflict:
		return "conflict"
	case http.StatusTooManyRequests:
		return "rate_limited"
	default:
		return "operation_failed"
	}
}

func adminFailureEventForRequest(r *http.Request) (systemevent.Action, systemevent.Category, string, bool) {
	type definition struct {
		action     systemevent.Action
		category   systemevent.Category
		targetType string
	}
	definitions := map[string]definition{
		"POST /api/admin/change-password":                             {systemevent.ActionAuthPasswordChangeFailed, systemevent.CategorySecurity, "admin"},
		"DELETE /api/admin/sessions/{id}":                             {systemevent.ActionAuthSessionRevoked, systemevent.CategorySecurity, "admin_session"},
		"POST /api/admin/sessions/revoke-others":                      {systemevent.ActionAuthSessionsRevokedOthers, systemevent.CategorySecurity, "admin_session_collection"},
		"POST /api/admin/keys":                                        {systemevent.ActionAPIKeyCreated, systemevent.CategoryAudit, "client_api_key"},
		"POST /api/admin/keys/{id}/revoke":                            {systemevent.ActionAPIKeyRevoked, systemevent.CategoryAudit, "client_api_key"},
		"DELETE /api/admin/keys/{id}":                                 {systemevent.ActionAPIKeyDeleted, systemevent.CategoryAudit, "client_api_key"},
		"PATCH /api/admin/keys/{id}":                                  {systemevent.ActionAPIKeyRenamed, systemevent.CategoryAudit, "client_api_key"},
		"PUT /api/admin/keys/{id}/disabled":                           {systemevent.ActionAPIKeyStatusUpdateFailed, systemevent.CategoryAudit, "client_api_key"},
		"PUT /api/admin/keys/{id}/model-policy":                       {systemevent.ActionAPIKeyModelPolicyUpdated, systemevent.CategoryAudit, "client_api_key"},
		"PUT /api/admin/keys/{id}/limits":                             {systemevent.ActionAPIKeyLimitsUpdated, systemevent.CategoryAudit, "client_api_key"},
		"PUT /api/admin/keys/{id}/budgets":                            {systemevent.ActionAPIKeyBudgetsUpdated, systemevent.CategoryAudit, "client_api_key"},
		"PUT /api/admin/keys/{id}/routing-pool":                       {systemevent.ActionAPIKeyRoutingPoolUpdated, systemevent.CategoryAudit, "client_api_key"},
		"POST /api/admin/routing-pools":                               {systemevent.ActionRoutingPoolCreated, systemevent.CategoryAudit, "routing_pool"},
		"PATCH /api/admin/routing-pools/{id}":                         {systemevent.ActionRoutingPoolUpdated, systemevent.CategoryAudit, "routing_pool"},
		"DELETE /api/admin/routing-pools/{id}":                        {systemevent.ActionRoutingPoolDeleted, systemevent.CategoryAudit, "routing_pool"},
		"PUT /api/admin/routing-pools/{id}/accounts":                  {systemevent.ActionRoutingPoolAccountsReplaced, systemevent.CategoryAudit, "routing_pool"},
		"POST /api/admin/request-logs/cleanup":                        {systemevent.ActionRequestLogCleanupCompleted, systemevent.CategoryAudit, "request_log_collection"},
		"PUT /api/admin/gateway-settings":                             {systemevent.ActionGatewaySettingsUpdated, systemevent.CategoryAudit, "gateway_settings"},
		"PUT /api/admin/usage-pricing":                                {systemevent.ActionUsagePricingUpdated, systemevent.CategoryAudit, "usage_pricing"},
		"POST /api/admin/usage-pricing/sync-official":                 {systemevent.ActionUsagePricingSynced, systemevent.CategoryAudit, "usage_pricing"},
		"POST /api/admin/usage-pricing/remove-shutdown":               {systemevent.ActionUsagePricingShutdownRemoved, systemevent.CategoryAudit, "usage_pricing"},
		"POST /api/admin/usage-pricing/ignore-upcoming":               {systemevent.ActionUsagePricingUpcomingIgnored, systemevent.CategoryAudit, "usage_pricing"},
		"POST /api/admin/fingerprint-profiles":                        {systemevent.ActionFingerprintProfileCreated, systemevent.CategoryAudit, "fingerprint_profile"},
		"PATCH /api/admin/fingerprint-profiles/{id}":                  {systemevent.ActionFingerprintProfileUpdated, systemevent.CategoryAudit, "fingerprint_profile"},
		"DELETE /api/admin/fingerprint-profiles/{id}":                 {systemevent.ActionFingerprintProfileDeleted, systemevent.CategoryAudit, "fingerprint_profile"},
		"POST /api/admin/error-passthrough-rules":                     {systemevent.ActionErrorPassthroughRuleCreated, systemevent.CategoryAudit, "error_passthrough_rule"},
		"PATCH /api/admin/error-passthrough-rules/{id}":               {systemevent.ActionErrorPassthroughRuleUpdated, systemevent.CategoryAudit, "error_passthrough_rule"},
		"DELETE /api/admin/error-passthrough-rules/{id}":              {systemevent.ActionErrorPassthroughRuleDeleted, systemevent.CategoryAudit, "error_passthrough_rule"},
		"POST /api/admin/alert-actions":                               {systemevent.ActionAlertActionCreated, systemevent.CategoryAudit, "alert_action"},
		"PATCH /api/admin/alert-actions/{id}":                         {systemevent.ActionAlertActionUpdated, systemevent.CategoryAudit, "alert_action"},
		"DELETE /api/admin/alert-actions/{id}":                        {systemevent.ActionAlertActionDeleted, systemevent.CategoryAudit, "alert_action"},
		"POST /api/admin/alert-actions/{id}/test":                     {systemevent.ActionAlertDeliveryTested, systemevent.CategoryAudit, "alert_action"},
		"POST /api/admin/alert-rules":                                 {systemevent.ActionAlertRuleCreated, systemevent.CategoryAudit, "alert_rule"},
		"PATCH /api/admin/alert-rules/{id}":                           {systemevent.ActionAlertRuleUpdated, systemevent.CategoryAudit, "alert_rule"},
		"DELETE /api/admin/alert-rules/{id}":                          {systemevent.ActionAlertRuleDeleted, systemevent.CategoryAudit, "alert_rule"},
		"PUT /api/admin/model-settings":                               {systemevent.ActionModelSettingsUpdated, systemevent.CategoryAudit, "model_settings"},
		"POST /api/admin/provider-accounts/api-upstream":              {systemevent.ActionProviderAccountCreated, systemevent.CategoryAudit, "provider_account"},
		"POST /api/admin/provider-accounts/codex-oauth/connect":       {systemevent.ActionOAuthConnectFailed, systemevent.CategoryOAuth, "oauth_connection"},
		"POST /api/admin/provider-accounts/codex-oauth/callback":      {systemevent.ActionOAuthCallbackFailed, systemevent.CategoryOAuth, "oauth_connection"},
		"PATCH /api/admin/provider-accounts/{id}":                     {systemevent.ActionProviderAccountUpdated, systemevent.CategoryAudit, "provider_account"},
		"DELETE /api/admin/provider-accounts/{id}":                    {systemevent.ActionProviderAccountDisconnected, systemevent.CategoryAudit, "provider_account"},
		"POST /api/admin/provider-accounts/{id}/disconnect":           {systemevent.ActionProviderAccountDisconnected, systemevent.CategoryAudit, "provider_account"},
		"PUT /api/admin/provider-accounts/{id}/models":                {systemevent.ActionProviderAccountModelsReplaced, systemevent.CategoryAudit, "provider_account"},
		"POST /api/admin/provider-accounts/{id}/models/sync":          {systemevent.ActionProviderAccountModelsSynced, systemevent.CategoryAudit, "provider_account"},
		"POST /api/admin/provider-accounts/{id}/model-tests":          {systemevent.ActionProviderAccountModelTested, systemevent.CategoryAudit, "provider_account"},
		"POST /api/admin/provider-accounts/{id}/test":                 {systemevent.ActionProviderAccountTested, systemevent.CategoryAudit, "provider_account"},
		"POST /api/admin/provider-accounts/{id}/pause":                {systemevent.ActionProviderAccountPaused, systemevent.CategoryAudit, "provider_account"},
		"POST /api/admin/provider-accounts/{id}/reset-status":         {systemevent.ActionProviderAccountStatusReset, systemevent.CategoryAudit, "provider_account"},
		"POST /api/admin/providers/openai/connect":                    {systemevent.ActionOAuthConnectFailed, systemevent.CategoryOAuth, "oauth_connection"},
		"POST /api/admin/providers/openai/callback":                   {systemevent.ActionOAuthCallbackFailed, systemevent.CategoryOAuth, "oauth_connection"},
		"POST /api/admin/providers/openai/disconnect":                 {systemevent.ActionProviderAccountDisconnectAll, systemevent.CategoryAudit, "provider_account_collection"},
		"PATCH /api/admin/providers/openai/accounts/{id}":             {systemevent.ActionProviderAccountUpdated, systemevent.CategoryAudit, "provider_account"},
		"PUT /api/admin/providers/openai/accounts/{id}/models":        {systemevent.ActionProviderAccountModelsReplaced, systemevent.CategoryAudit, "provider_account"},
		"POST /api/admin/providers/openai/accounts/{id}/test":         {systemevent.ActionProviderAccountTested, systemevent.CategoryAudit, "provider_account"},
		"POST /api/admin/providers/openai/accounts/{id}/pause":        {systemevent.ActionProviderAccountPaused, systemevent.CategoryAudit, "provider_account"},
		"POST /api/admin/providers/openai/accounts/{id}/reset-status": {systemevent.ActionProviderAccountStatusReset, systemevent.CategoryAudit, "provider_account"},
		"POST /api/admin/providers/openai/accounts/{id}/disconnect":   {systemevent.ActionProviderAccountDisconnected, systemevent.CategoryAudit, "provider_account"},
	}
	key := r.Pattern
	if !strings.HasPrefix(key, r.Method+" ") {
		key = r.Method + " " + key
	}
	matched, ok := definitions[key]
	return matched.action, matched.category, matched.targetType, ok
}

func newProviderBatchAudit(action systemevent.Action, requestedIDs []int64) *providerBatchAudit {
	return &providerBatchAudit{
		id:           systemevent.NewCorrelationID(),
		action:       action,
		requestedIDs: append([]int64(nil), requestedIDs...),
	}
}

func (audit *providerBatchAudit) targetContext(ctx context.Context) context.Context {
	return systemevent.WithIntent(ctx, systemevent.EventIntent{Metadata: map[string]any{"batch_id": audit.id}})
}

func (audit *providerBatchAudit) succeeded(id int64) {
	audit.succeededIDs = append(audit.succeededIDs, id)
}

func (audit *providerBatchAudit) failed(id int64, errorCode string) {
	audit.failedIDs = append(audit.failedIDs, id)
	audit.errorCode = errorCode
}

func (audit *providerBatchAudit) record(ctx context.Context, recorder SystemEventRecorder, statusCode int, duration time.Duration) {
	attempted := len(audit.succeededIDs) + len(audit.failedIDs)
	skippedIDs := append([]int64(nil), audit.requestedIDs[attempted:]...)
	outcome := systemevent.OutcomeSuccess
	severity := systemevent.SeverityInfo
	if len(audit.failedIDs) > 0 {
		outcome = systemevent.OutcomeFailure
		severity = systemevent.SeverityWarning
		if len(audit.succeededIDs) > 0 {
			outcome = systemevent.OutcomePartial
		}
	}
	metadata := map[string]any{
		"batch_id":        audit.id,
		"requested_count": len(audit.requestedIDs),
		"attempted_count": attempted,
		"succeeded_count": len(audit.succeededIDs),
		"failed_count":    len(audit.failedIDs),
		"skipped_count":   len(skippedIDs),
		"requested_ids":   audit.requestedIDs,
		"succeeded_ids":   audit.succeededIDs,
		"failed_ids":      audit.failedIDs,
		"skipped_ids":     skippedIDs,
	}
	if audit.errorCode != "" {
		metadata["first_error_code"] = audit.errorCode
	}
	_ = recordHTTPSystemEvent(ctx, recorder, systemevent.EventIntent{
		Category:  systemevent.CategoryAudit,
		Severity:  severity,
		Action:    audit.action,
		Outcome:   outcome,
		Target:    systemevent.Target{Type: "provider_account_batch", ID: audit.id},
		ErrorCode: audit.errorCode,
		Metadata:  metadata,
	}, statusCode, duration)
}

package httpapi

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"time"

	"github.com/KnowSky404/N2API/backend/internal/admin"
	"github.com/KnowSky404/N2API/backend/internal/alerting"
	"github.com/KnowSky404/N2API/backend/internal/systemevent"
)

type AlertingAdminService interface {
	ListActions(context.Context) ([]alerting.Action, error)
	CreateAction(context.Context, alerting.ActionInput) (alerting.Action, error)
	UpdateAction(context.Context, int64, alerting.ActionUpdateInput) (alerting.Action, error)
	DeleteAction(context.Context, int64) error
	ListRules(context.Context) ([]alerting.Rule, error)
	CreateRule(context.Context, alerting.Rule) (alerting.Rule, error)
	InstallRuleTemplate(context.Context, string, int64) (alerting.Rule, bool, error)
	UpdateRule(context.Context, int64, alerting.Rule, time.Time) (alerting.Rule, error)
	DeleteRule(context.Context, int64) error
}

type AlertActionTester interface {
	TestAction(context.Context, int64, time.Time) (alerting.ActionTestResult, error)
}

type alertActionCreateRequest struct {
	Name        string              `json:"name"`
	Kind        alerting.ActionKind `json:"kind"`
	Destination string              `json:"destination"`
	Enabled     bool                `json:"enabled"`
}

type alertActionUpdateRequest struct {
	Name              string              `json:"name"`
	Kind              alerting.ActionKind `json:"kind"`
	Destination       optionalJSONString  `json:"destination"`
	Enabled           bool                `json:"enabled"`
	ExpectedUpdatedAt time.Time           `json:"expectedUpdatedAt"`
}

type optionalJSONString struct {
	value   string
	present bool
	isNull  bool
}

func (value *optionalJSONString) UnmarshalJSON(data []byte) error {
	if bytes.Equal(bytes.TrimSpace(data), []byte("null")) {
		value.present = true
		value.isNull = true
		return nil
	}
	if err := json.Unmarshal(data, &value.value); err != nil {
		return err
	}
	value.present = true
	return nil
}

func (value optionalJSONString) pointer() *string {
	if !value.present {
		return nil
	}
	return &value.value
}

type alertRuleRequest struct {
	Name                     string                      `json:"name"`
	ActionID                 int64                       `json:"actionId"`
	Enabled                  bool                        `json:"enabled"`
	Category                 systemevent.Category        `json:"category"`
	Severity                 systemevent.Severity        `json:"severity"`
	EventAction              systemevent.Action          `json:"eventAction"`
	RecoveryAction           systemevent.Action          `json:"recoveryAction"`
	AggregationCount         int                         `json:"aggregationCount"`
	AggregationWindowSeconds int                         `json:"aggregationWindowSeconds"`
	CooldownSeconds          int                         `json:"cooldownSeconds"`
	DeduplicationScope       alerting.DeduplicationScope `json:"deduplicationScope"`
	NotifyRecovery           bool                        `json:"notifyRecovery"`
}

type alertRuleUpdateRequest struct {
	alertRuleRequest
	ExpectedUpdatedAt time.Time `json:"expectedUpdatedAt"`
}

func (request alertRuleRequest) rule() alerting.Rule {
	return alerting.Rule{
		Name: request.Name, ActionID: request.ActionID, Enabled: request.Enabled,
		Category: request.Category, Severity: request.Severity, EventAction: request.EventAction,
		RecoveryAction: request.RecoveryAction, AggregationCount: request.AggregationCount,
		AggregationWindowSeconds: request.AggregationWindowSeconds, CooldownSeconds: request.CooldownSeconds,
		DeduplicationScope: request.DeduplicationScope, NotifyRecovery: request.NotifyRecovery,
	}
}

func registerAlertingAdminRoutes(
	mux *http.ServeMux,
	requireAdmin func(func(http.ResponseWriter, *http.Request, admin.Admin)) http.HandlerFunc,
	service AlertingAdminService,
	tester AlertActionTester,
) {
	mux.HandleFunc("GET /api/admin/alert-actions", requireAdmin(func(w http.ResponseWriter, r *http.Request, _ admin.Admin) {
		if service == nil {
			writeError(w, http.StatusServiceUnavailable, "service_unavailable")
			return
		}
		actions, err := service.ListActions(r.Context())
		if err != nil {
			writeAlertingError(w, err, false)
			return
		}
		writeJSON(w, http.StatusOK, map[string][]alerting.Action{"actions": actions})
	}))

	mux.HandleFunc("POST /api/admin/alert-actions", requireAdmin(func(w http.ResponseWriter, r *http.Request, _ admin.Admin) {
		if service == nil {
			writeError(w, http.StatusServiceUnavailable, "service_unavailable")
			return
		}
		var request alertActionCreateRequest
		if err := decodeJSON(w, r, &request); err != nil {
			writeError(w, http.StatusBadRequest, "bad_request")
			return
		}
		action, err := service.CreateAction(r.Context(), alerting.ActionInput{
			Name: request.Name, Kind: request.Kind, Destination: request.Destination, Enabled: request.Enabled,
		})
		if err != nil {
			writeAlertingError(w, err, false)
			return
		}
		writeJSON(w, http.StatusCreated, map[string]alerting.Action{"action": action})
	}))

	mux.HandleFunc("PATCH /api/admin/alert-actions/{id}", requireAdmin(func(w http.ResponseWriter, r *http.Request, _ admin.Admin) {
		if service == nil {
			writeError(w, http.StatusServiceUnavailable, "service_unavailable")
			return
		}
		id, err := parsePositivePathID(r, "id")
		if err != nil {
			writeError(w, http.StatusBadRequest, "bad_request")
			return
		}
		var request alertActionUpdateRequest
		if err := decodeJSON(w, r, &request); err != nil {
			writeError(w, http.StatusBadRequest, "bad_request")
			return
		}
		if request.ExpectedUpdatedAt.IsZero() {
			writeError(w, http.StatusBadRequest, "invalid_input")
			return
		}
		if request.Destination.isNull {
			writeError(w, http.StatusBadRequest, "invalid_input")
			return
		}
		action, err := service.UpdateAction(r.Context(), id, alerting.ActionUpdateInput{
			Name: request.Name, Kind: request.Kind, Destination: request.Destination.pointer(),
			Enabled: request.Enabled, ExpectedUpdatedAt: request.ExpectedUpdatedAt,
		})
		if err != nil {
			writeAlertingError(w, err, true)
			return
		}
		writeJSON(w, http.StatusOK, map[string]alerting.Action{"action": action})
	}))

	mux.HandleFunc("DELETE /api/admin/alert-actions/{id}", requireAdmin(func(w http.ResponseWriter, r *http.Request, _ admin.Admin) {
		if service == nil {
			writeError(w, http.StatusServiceUnavailable, "service_unavailable")
			return
		}
		id, err := parsePositivePathID(r, "id")
		if err != nil {
			writeError(w, http.StatusBadRequest, "bad_request")
			return
		}
		if err := service.DeleteAction(r.Context(), id); err != nil {
			writeAlertingError(w, err, false)
			return
		}
		w.WriteHeader(http.StatusNoContent)
	}))

	mux.HandleFunc("POST /api/admin/alert-actions/{id}/test", requireAdmin(func(w http.ResponseWriter, r *http.Request, _ admin.Admin) {
		if tester == nil {
			writeError(w, http.StatusServiceUnavailable, "service_unavailable")
			return
		}
		id, err := parsePositivePathID(r, "id")
		if err != nil {
			writeError(w, http.StatusBadRequest, "bad_request")
			return
		}
		var request struct {
			ExpectedUpdatedAt time.Time `json:"expectedUpdatedAt"`
		}
		if err := decodeJSON(w, r, &request); err != nil {
			writeError(w, http.StatusBadRequest, "bad_request")
			return
		}
		if request.ExpectedUpdatedAt.IsZero() {
			writeError(w, http.StatusBadRequest, "invalid_input")
			return
		}
		result, err := tester.TestAction(r.Context(), id, request.ExpectedUpdatedAt)
		if err != nil {
			writeAlertingError(w, err, true)
			return
		}
		writeJSON(w, http.StatusOK, map[string]alerting.ActionTestResult{"result": result})
	}))

	mux.HandleFunc("GET /api/admin/alert-rules", requireAdmin(func(w http.ResponseWriter, r *http.Request, _ admin.Admin) {
		if service == nil {
			writeError(w, http.StatusServiceUnavailable, "service_unavailable")
			return
		}
		rules, err := service.ListRules(r.Context())
		if err != nil {
			writeAlertingError(w, err, false)
			return
		}
		writeJSON(w, http.StatusOK, map[string][]alerting.Rule{"rules": rules})
	}))

	mux.HandleFunc("GET /api/admin/alert-rule-templates", requireAdmin(func(w http.ResponseWriter, _ *http.Request, _ admin.Admin) {
		writeJSON(w, http.StatusOK, map[string][]alerting.RuleTemplate{"templates": alerting.RuleTemplates()})
	}))

	mux.HandleFunc("POST /api/admin/alert-rule-templates/{key}/install", requireAdmin(func(w http.ResponseWriter, r *http.Request, _ admin.Admin) {
		if service == nil {
			writeError(w, http.StatusServiceUnavailable, "service_unavailable")
			return
		}
		var request struct {
			ActionID int64 `json:"actionId"`
		}
		if err := decodeJSON(w, r, &request); err != nil {
			writeError(w, http.StatusBadRequest, "bad_request")
			return
		}
		rule, created, err := service.InstallRuleTemplate(r.Context(), r.PathValue("key"), request.ActionID)
		if err != nil {
			writeAlertingError(w, err, false)
			return
		}
		status := http.StatusOK
		if created {
			status = http.StatusCreated
		}
		writeJSON(w, status, struct {
			Rule    alerting.Rule `json:"rule"`
			Created bool          `json:"created"`
		}{Rule: rule, Created: created})
	}))

	mux.HandleFunc("POST /api/admin/alert-rules", requireAdmin(func(w http.ResponseWriter, r *http.Request, _ admin.Admin) {
		if service == nil {
			writeError(w, http.StatusServiceUnavailable, "service_unavailable")
			return
		}
		var request alertRuleRequest
		if err := decodeJSON(w, r, &request); err != nil {
			writeError(w, http.StatusBadRequest, "bad_request")
			return
		}
		rule, err := service.CreateRule(r.Context(), request.rule())
		if err != nil {
			writeAlertingError(w, err, false)
			return
		}
		writeJSON(w, http.StatusCreated, map[string]alerting.Rule{"rule": rule})
	}))

	mux.HandleFunc("PATCH /api/admin/alert-rules/{id}", requireAdmin(func(w http.ResponseWriter, r *http.Request, _ admin.Admin) {
		if service == nil {
			writeError(w, http.StatusServiceUnavailable, "service_unavailable")
			return
		}
		id, err := parsePositivePathID(r, "id")
		if err != nil {
			writeError(w, http.StatusBadRequest, "bad_request")
			return
		}
		var request alertRuleUpdateRequest
		if err := decodeJSON(w, r, &request); err != nil {
			writeError(w, http.StatusBadRequest, "bad_request")
			return
		}
		if request.ExpectedUpdatedAt.IsZero() {
			writeError(w, http.StatusBadRequest, "invalid_input")
			return
		}
		rule, err := service.UpdateRule(r.Context(), id, request.rule(), request.ExpectedUpdatedAt)
		if err != nil {
			writeAlertingError(w, err, true)
			return
		}
		writeJSON(w, http.StatusOK, map[string]alerting.Rule{"rule": rule})
	}))

	mux.HandleFunc("DELETE /api/admin/alert-rules/{id}", requireAdmin(func(w http.ResponseWriter, r *http.Request, _ admin.Admin) {
		if service == nil {
			writeError(w, http.StatusServiceUnavailable, "service_unavailable")
			return
		}
		id, err := parsePositivePathID(r, "id")
		if err != nil {
			writeError(w, http.StatusBadRequest, "bad_request")
			return
		}
		if err := service.DeleteRule(r.Context(), id); err != nil {
			writeAlertingError(w, err, false)
			return
		}
		w.WriteHeader(http.StatusNoContent)
	}))
}

func writeAlertingError(w http.ResponseWriter, err error, staleConflict bool) {
	switch {
	case errors.Is(err, alerting.ErrInvalidInput):
		writeError(w, http.StatusBadRequest, "invalid_input")
	case errors.Is(err, alerting.ErrNotFound):
		writeError(w, http.StatusNotFound, "not_found")
	case errors.Is(err, alerting.ErrConflict):
		code := "conflict"
		if staleConflict {
			code = "stale_update"
		}
		writeError(w, http.StatusConflict, code)
	case errors.Is(err, alerting.ErrRateLimited):
		var rateLimit *alerting.RateLimitError
		if errors.As(err, &rateLimit) {
			setRetryAfter(w, rateLimit.RetryAfter)
		}
		writeError(w, http.StatusTooManyRequests, "rate_limited")
	default:
		writeError(w, http.StatusInternalServerError, "internal_error")
	}
}

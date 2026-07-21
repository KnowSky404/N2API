package httpapi

import (
	"context"
	"encoding/json"
	"net/http"
	"strconv"
	"time"

	"github.com/KnowSky404/N2API/backend/internal/admin"
	"github.com/KnowSky404/N2API/backend/internal/buildinfo"
	"github.com/KnowSky404/N2API/backend/internal/systemevent"
)

const (
	configurationExportFormatVersion = 1
	configurationExportMaxBytes      = 5 << 20
	configurationExportEventTimeout  = 2 * time.Second
)

type configurationExportDocument struct {
	FormatVersion       int                         `json:"formatVersion"`
	Application         buildinfo.Info              `json:"application"`
	ExportedAt          time.Time                   `json:"exportedAt"`
	UnsupportedSections []string                    `json:"unsupportedSections"`
	Redactions          []string                    `json:"redactions"`
	Configuration       admin.ConfigurationSnapshot `json:"configuration"`
}

func handleExportConfiguration(w http.ResponseWriter, r *http.Request, admins AdminService, recorder SystemEventRecorder, build buildinfo.Info) {
	started := time.Now()
	exportedAt := time.Now().UTC()
	snapshot, err := admins.ExportConfiguration(r.Context())
	if err != nil {
		recordConfigurationExportFailure(r.Context(), recorder, started, "configuration_export_failed")
		writeError(w, http.StatusInternalServerError, "internal_error")
		return
	}
	document := configurationExportDocument{
		FormatVersion:       configurationExportFormatVersion,
		Application:         build,
		ExportedAt:          exportedAt,
		UnsupportedSections: []string{},
		Redactions: []string{
			"providerCredentials",
			"providerProxyURLs",
			"apiKeyCredentials",
			"fingerprintHeaderValues",
			"alertActionDestinations",
		},
		Configuration: snapshot,
	}
	body, err := json.MarshalIndent(document, "", "  ")
	if err != nil {
		recordConfigurationExportFailure(r.Context(), recorder, started, "configuration_export_encode_failed")
		writeError(w, http.StatusInternalServerError, "internal_error")
		return
	}
	body = append(body, '\n')
	if len(body) > configurationExportMaxBytes {
		recordConfigurationExportFailure(r.Context(), recorder, started, "configuration_export_too_large")
		writeError(w, http.StatusInternalServerError, "internal_error")
		return
	}

	metadata, err := configurationExportMetadata(build.Version, snapshot, len(body))
	if err != nil {
		recordConfigurationExportFailure(r.Context(), recorder, started, "configuration_export_metadata_failed")
		writeError(w, http.StatusInternalServerError, "internal_error")
		return
	}
	eventCtx, cancel := context.WithTimeout(r.Context(), configurationExportEventTimeout)
	err = recordHTTPSystemEvent(eventCtx, recorder, systemevent.EventIntent{
		Category: systemevent.CategorySecurity, Severity: systemevent.SeverityInfo,
		Action: systemevent.ActionConfigurationExported, Outcome: systemevent.OutcomeSuccess,
		Target:   systemevent.Target{Type: "portable_configuration", ID: "v1"},
		Metadata: metadata,
	}, http.StatusOK, time.Since(started))
	cancel()
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal_error")
		return
	}

	filename := "n2api-portable-config-v1-" + exportedAt.Format("20060102T150405Z") + ".json"
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.Header().Set("Content-Disposition", "attachment; filename=\""+filename+"\"")
	w.Header().Set("Content-Length", strconv.Itoa(len(body)))
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(body)
}

func configurationExportMetadata(applicationVersion string, snapshot admin.ConfigurationSnapshot, bodyBytes int) (map[string]any, error) {
	values := map[string]any{
		"format_version":            configurationExportFormatVersion,
		"application_version":       applicationVersion,
		"body_bytes":                bodyBytes,
		"routing_pool_count":        len(snapshot.RoutingPools),
		"key_template_count":        len(snapshot.APIKeyTemplates),
		"provider_account_count":    len(snapshot.ProviderAccounts),
		"fingerprint_profile_count": len(snapshot.FingerprintProfiles),
		"error_rule_count":          len(snapshot.ErrorPassthroughRules),
		"alert_action_count":        len(snapshot.AlertActions),
		"alert_rule_count":          len(snapshot.AlertRules),
	}
	return systemevent.SafeMetadata(values,
		"format_version", "application_version", "body_bytes", "routing_pool_count",
		"key_template_count", "provider_account_count", "fingerprint_profile_count", "error_rule_count",
		"alert_action_count", "alert_rule_count",
	)
}

func recordConfigurationExportFailure(ctx context.Context, recorder SystemEventRecorder, started time.Time, errorCode string) {
	recordCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), configurationExportEventTimeout)
	defer cancel()
	_ = recordHTTPSystemEvent(recordCtx, recorder, systemevent.EventIntent{
		Category: systemevent.CategorySecurity, Severity: systemevent.SeverityError,
		Action: systemevent.ActionConfigurationExported, Outcome: systemevent.OutcomeFailure,
		Target: systemevent.Target{Type: "portable_configuration", ID: "v1"}, ErrorCode: errorCode,
	}, http.StatusInternalServerError, time.Since(started))
}

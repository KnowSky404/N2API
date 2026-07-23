package encryptioninventory

import (
	"context"
	"fmt"
	"sort"
	"time"

	"github.com/KnowSky404/N2API/backend/internal/secret"
)

const (
	StatusOK        = "ok"
	StatusAttention = "attention"
	StatusFailed    = "failed"
)

type LifecycleStatus string

const (
	LifecycleReadableCurrentKey         LifecycleStatus = "readable_current_key"
	LifecycleReadablePreviousKey        LifecycleStatus = "readable_previous_key"
	LifecycleReadableLegacy             LifecycleStatus = "readable_legacy"
	LifecycleUnreadableRequired         LifecycleStatus = "unreadable_required"
	LifecycleUnreadableExpiredPurgeable LifecycleStatus = "unreadable_expired_or_purgeable"
	LifecycleUnreadableUnknown          LifecycleStatus = "unreadable_unknown"
)

type ReasonCode string

const (
	ReasonCurrentKeyVerified  ReasonCode = "current_key_verified"
	ReasonPreviousKeyVerified ReasonCode = "previous_key_verified"
	ReasonLegacyCiphertext    ReasonCode = "legacy_ciphertext_verified"
	ReasonCredentialRequired  ReasonCode = "credential_required"
	ReasonOAuthStateActive    ReasonCode = "oauth_state_active"
	ReasonOAuthStateExpired   ReasonCode = "oauth_state_expired"
	ReasonOAuthStateConsumed  ReasonCode = "oauth_state_consumed"
	ReasonLifecycleUnknown    ReasonCode = "lifecycle_unknown"
)

type EncryptedValue struct {
	Table      string
	Type       secret.SecretKind
	RowID      int64
	Ciphertext string `json:"-"`
	ExpiresAt  *time.Time
	ConsumedAt *time.Time
}

type Repository interface {
	ListEncryptedValues(ctx context.Context) ([]EncryptedValue, error)
}

type LifecycleCount struct {
	Status LifecycleStatus `json:"lifecycleStatus"`
	Count  int             `json:"count"`
}

type TypeReport struct {
	Table           string            `json:"table"`
	CredentialKind  secret.SecretKind `json:"credentialKind"`
	Count           int               `json:"count"`
	LifecycleCounts []LifecycleCount  `json:"lifecycleCounts"`
}

type ValueReport struct {
	Table              string                  `json:"table"`
	CredentialKind     secret.SecretKind       `json:"credentialKind"`
	RowID              int64                   `json:"rowId"`
	EnvelopeFormat     secret.CiphertextFormat `json:"envelopeFormat"`
	AuthenticatedKeyID string                  `json:"authenticatedKeyId,omitempty"`
	LifecycleStatus    LifecycleStatus         `json:"lifecycleStatus"`
	ReasonCode         ReasonCode              `json:"reasonCode"`
}

type Report struct {
	Status          string           `json:"status"`
	Count           int              `json:"count"`
	LifecycleCounts []LifecycleCount `json:"lifecycleCounts"`
	Types           []TypeReport     `json:"types"`
	Values          []ValueReport    `json:"values"`
}

type credentialClass struct {
	table string
	kind  secret.SecretKind
}

var credentialClasses = []credentialClass{
	{table: "oauth_states", kind: secret.SecretKindOAuthCodeVerifier},
	{table: "provider_account_credentials", kind: secret.SecretKindOAuthAccessToken},
	{table: "provider_account_credentials", kind: secret.SecretKindOAuthRefreshToken},
	{table: "provider_account_credentials", kind: secret.SecretKindOAuthIDToken},
	{table: "provider_account_credentials", kind: secret.SecretKindProviderAPIKey},
	{table: "provider_account_credentials", kind: secret.SecretKindProviderProxyURL},
	{table: "client_api_keys", kind: secret.SecretKindClientAPIKey},
	{table: "alert_actions", kind: secret.SecretKindAlertActionDestination},
}

var lifecycleStatuses = []LifecycleStatus{
	LifecycleReadableCurrentKey,
	LifecycleReadablePreviousKey,
	LifecycleReadableLegacy,
	LifecycleUnreadableRequired,
	LifecycleUnreadableExpiredPurgeable,
	LifecycleUnreadableUnknown,
}

func Verify(ctx context.Context, repo Repository, keyring *secret.Keyring) (Report, error) {
	return VerifyAt(ctx, repo, keyring, time.Now().UTC())
}

func VerifyAt(ctx context.Context, repo Repository, keyring *secret.Keyring, now time.Time) (Report, error) {
	if repo == nil || keyring == nil {
		return Report{}, fmt.Errorf("encrypted credential inventory is not configured")
	}
	values, err := repo.ListEncryptedValues(ctx)
	if err != nil {
		return Report{}, fmt.Errorf("list encrypted credential inventory failed")
	}

	report := Report{
		Status:          StatusOK,
		LifecycleCounts: emptyLifecycleCounts(),
		Types:           make([]TypeReport, len(credentialClasses)),
		Values:          []ValueReport{},
	}
	classIndexes := make(map[credentialClass]int, len(credentialClasses))
	for index, class := range credentialClasses {
		classIndexes[class] = index
		report.Types[index] = TypeReport{
			Table: class.table, CredentialKind: class.kind, LifecycleCounts: emptyLifecycleCounts(),
		}
	}

	sort.SliceStable(values, func(i, j int) bool {
		left := credentialClass{table: values[i].Table, kind: values[i].Type}
		right := credentialClass{table: values[j].Table, kind: values[j].Type}
		leftIndex, leftOK := classIndexes[left]
		rightIndex, rightOK := classIndexes[right]
		if leftOK && rightOK && leftIndex != rightIndex {
			return leftIndex < rightIndex
		}
		if leftOK != rightOK {
			return leftOK
		}
		return values[i].RowID < values[j].RowID
	})

	for _, value := range values {
		if value.Ciphertext == "" {
			continue
		}
		class := credentialClass{table: value.Table, kind: value.Type}
		index, ok := classIndexes[class]
		if !ok || value.RowID <= 0 {
			return Report{}, fmt.Errorf("encrypted credential inventory row is invalid")
		}

		typeReport := &report.Types[index]
		typeReport.Count++
		report.Count++
		verification, err := keyring.VerifyStringFor(class.kind, value.Ciphertext)
		valueReport := ValueReport{
			Table: class.table, CredentialKind: class.kind, RowID: value.RowID,
			EnvelopeFormat: secret.InspectCiphertextFormat(value.Ciphertext),
		}
		if err != nil {
			valueReport.LifecycleStatus, valueReport.ReasonCode = unreadableLifecycle(value, now)
		} else {
			valueReport.EnvelopeFormat = verification.Format
			valueReport.AuthenticatedKeyID = verification.KeyID
			valueReport.LifecycleStatus, valueReport.ReasonCode = readableLifecycle(verification, keyring.CurrentKeyID())
		}
		incrementLifecycleCount(report.LifecycleCounts, valueReport.LifecycleStatus)
		incrementLifecycleCount(typeReport.LifecycleCounts, valueReport.LifecycleStatus)
		report.Values = append(report.Values, valueReport)
	}

	if lifecycleCount(report.LifecycleCounts, LifecycleUnreadableRequired) > 0 ||
		lifecycleCount(report.LifecycleCounts, LifecycleUnreadableUnknown) > 0 {
		report.Status = StatusFailed
	} else if lifecycleCount(report.LifecycleCounts, LifecycleUnreadableExpiredPurgeable) > 0 {
		report.Status = StatusAttention
	}
	return report, nil
}

func emptyLifecycleCounts() []LifecycleCount {
	counts := make([]LifecycleCount, len(lifecycleStatuses))
	for index, status := range lifecycleStatuses {
		counts[index] = LifecycleCount{Status: status}
	}
	return counts
}

func incrementLifecycleCount(counts []LifecycleCount, status LifecycleStatus) {
	for index := range counts {
		if counts[index].Status == status {
			counts[index].Count++
			return
		}
	}
}

func lifecycleCount(counts []LifecycleCount, status LifecycleStatus) int {
	for _, count := range counts {
		if count.Status == status {
			return count.Count
		}
	}
	return 0
}

func readableLifecycle(verification secret.CiphertextVerification, currentKeyID string) (LifecycleStatus, ReasonCode) {
	if verification.Format == secret.CiphertextFormatLegacy {
		return LifecycleReadableLegacy, ReasonLegacyCiphertext
	}
	if verification.KeyID == currentKeyID {
		return LifecycleReadableCurrentKey, ReasonCurrentKeyVerified
	}
	return LifecycleReadablePreviousKey, ReasonPreviousKeyVerified
}

func unreadableLifecycle(value EncryptedValue, now time.Time) (LifecycleStatus, ReasonCode) {
	if value.Type == secret.SecretKindOAuthCodeVerifier {
		if value.ConsumedAt != nil {
			return LifecycleUnreadableExpiredPurgeable, ReasonOAuthStateConsumed
		}
		if value.ExpiresAt == nil {
			return LifecycleUnreadableUnknown, ReasonLifecycleUnknown
		}
		if !value.ExpiresAt.After(now) {
			return LifecycleUnreadableExpiredPurgeable, ReasonOAuthStateExpired
		}
		return LifecycleUnreadableRequired, ReasonOAuthStateActive
	}
	return LifecycleUnreadableRequired, ReasonCredentialRequired
}

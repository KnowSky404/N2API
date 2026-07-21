package encryptioninventory

import (
	"context"
	"fmt"
	"sort"

	"github.com/KnowSky404/N2API/backend/internal/secret"
)

const (
	StatusOK     = "ok"
	StatusFailed = "failed"

	FailureUnreadable = "unreadable"
)

type EncryptedValue struct {
	Table      string
	Type       secret.SecretKind
	RowID      int64
	Ciphertext string `json:"-"`
}

type Repository interface {
	ListEncryptedValues(ctx context.Context) ([]EncryptedValue, error)
}

type Totals struct {
	Values   int `json:"values"`
	Verified int `json:"verified"`
	Failed   int `json:"failed"`
}

type KeyIDCount struct {
	ID     string                  `json:"id"`
	Format secret.CiphertextFormat `json:"format"`
	Count  int                     `json:"count"`
}

type TypeReport struct {
	Table    string            `json:"table"`
	Type     secret.SecretKind `json:"type"`
	Values   int               `json:"values"`
	Verified int               `json:"verified"`
	Failed   int               `json:"failed"`
	KeyIDs   []KeyIDCount      `json:"keyIds"`
}

type Failure struct {
	Table  string            `json:"table"`
	Type   secret.SecretKind `json:"type"`
	RowID  string            `json:"rowId"`
	Status string            `json:"status"`
}

type Report struct {
	Status   string       `json:"status"`
	Totals   Totals       `json:"totals"`
	Types    []TypeReport `json:"types"`
	Failures []Failure    `json:"failures"`
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

type keyCountKey struct {
	id     string
	format secret.CiphertextFormat
}

func Verify(ctx context.Context, repo Repository, keyring *secret.Keyring) (Report, error) {
	if repo == nil || keyring == nil {
		return Report{}, fmt.Errorf("encrypted credential inventory is not configured")
	}
	values, err := repo.ListEncryptedValues(ctx)
	if err != nil {
		return Report{}, fmt.Errorf("list encrypted credential inventory failed")
	}

	report := Report{
		Status:   StatusOK,
		Types:    make([]TypeReport, len(credentialClasses)),
		Failures: []Failure{},
	}
	classIndexes := make(map[credentialClass]int, len(credentialClasses))
	keyCounts := make([]map[keyCountKey]int, len(credentialClasses))
	for index, class := range credentialClasses {
		classIndexes[class] = index
		keyCounts[index] = make(map[keyCountKey]int)
		report.Types[index] = TypeReport{Table: class.table, Type: class.kind, KeyIDs: []KeyIDCount{}}
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
		typeReport.Values++
		report.Totals.Values++
		verification, err := keyring.VerifyStringFor(class.kind, value.Ciphertext)
		if err != nil {
			typeReport.Failed++
			report.Totals.Failed++
			report.Failures = append(report.Failures, Failure{
				Table: class.table, Type: class.kind, RowID: fmt.Sprintf("%d", value.RowID), Status: FailureUnreadable,
			})
			continue
		}

		typeReport.Verified++
		report.Totals.Verified++
		keyCounts[index][keyCountKey{id: verification.KeyID, format: verification.Format}]++
	}

	for index, counts := range keyCounts {
		keys := make([]keyCountKey, 0, len(counts))
		for key := range counts {
			keys = append(keys, key)
		}
		sort.Slice(keys, func(i, j int) bool {
			if keys[i].id != keys[j].id {
				return keys[i].id < keys[j].id
			}
			return keys[i].format < keys[j].format
		})
		for _, key := range keys {
			report.Types[index].KeyIDs = append(report.Types[index].KeyIDs, KeyIDCount{
				ID: key.id, Format: key.format, Count: counts[key],
			})
		}
	}

	if report.Totals.Failed > 0 {
		report.Status = StatusFailed
	}
	return report, nil
}

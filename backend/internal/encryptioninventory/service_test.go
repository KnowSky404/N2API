package encryptioninventory

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/KnowSky404/N2API/backend/internal/secret"
)

type memoryRepository struct {
	values []EncryptedValue
	err    error
}

func (r memoryRepository) ListEncryptedValues(context.Context) ([]EncryptedValue, error) {
	return r.values, r.err
}

func TestVerifyClassifiesCredentialLifecycleWithoutLeakingValues(t *testing.T) {
	now := time.Date(2026, 7, 23, 12, 0, 0, 0, time.UTC)
	activeExpiry := now.Add(time.Hour)
	expiredAt := now.Add(-time.Hour)
	consumedAt := now.Add(-30 * time.Minute)
	keyring := testKeyring(t)
	previousWriter := mustKeyring(t, "previous", "legacy-encryption-secret", nil)

	current := mustEncryptFor(t, keyring, secret.SecretKindOAuthAccessToken, "current-access-token")
	previous := mustEncryptFor(t, previousWriter, secret.SecretKindOAuthRefreshToken, "previous-refresh-token")
	crossKind := mustEncryptFor(t, keyring, secret.SecretKindOAuthAccessToken, "cross-kind-canary")
	unknownKeyWriter := mustKeyring(t, "missing", "missing-key-secret-at-least-32-bytes", nil)
	unknownKey := mustEncryptFor(t, unknownKeyWriter, secret.SecretKindProviderAPIKey, "unknown-key-canary")
	corrupt := "n2api:v1:current:provider-proxy-url:corrupt-envelope-canary"

	repo := memoryRepository{values: []EncryptedValue{
		{Table: "provider_account_credentials", Type: secret.SecretKindOAuthRefreshToken, RowID: 20, Ciphertext: previous},
		{Table: "oauth_states", Type: secret.SecretKindOAuthCodeVerifier, RowID: 12, Ciphertext: "active-state-canary", ExpiresAt: &activeExpiry},
		{Table: "provider_account_credentials", Type: secret.SecretKindOAuthAccessToken, RowID: 20, Ciphertext: current},
		{Table: "oauth_states", Type: secret.SecretKindOAuthCodeVerifier, RowID: 10, Ciphertext: "expired-state-canary", ExpiresAt: &expiredAt},
		{Table: "provider_account_credentials", Type: secret.SecretKindOAuthIDToken, RowID: 20, Ciphertext: legacyCiphertextFixture},
		{Table: "provider_account_credentials", Type: secret.SecretKindProviderAPIKey, RowID: 21, Ciphertext: unknownKey},
		{Table: "provider_account_credentials", Type: secret.SecretKindProviderProxyURL, RowID: 21, Ciphertext: corrupt},
		{Table: "client_api_keys", Type: secret.SecretKindClientAPIKey, RowID: 30, Ciphertext: crossKind},
		{Table: "alert_actions", Type: secret.SecretKindAlertActionDestination, RowID: 40, Ciphertext: mustEncryptFor(t, keyring, secret.SecretKindAlertActionDestination, "https://alerts.example.test/topic")},
		{Table: "oauth_states", Type: secret.SecretKindOAuthCodeVerifier, RowID: 11, Ciphertext: "consumed-state-canary", ExpiresAt: &activeExpiry, ConsumedAt: &consumedAt},
	}}

	report, err := VerifyAt(context.Background(), repo, keyring, now)
	if err != nil {
		t.Fatalf("VerifyAt returned error: %v", err)
	}
	if report.Status != StatusFailed || report.Count != 10 || len(report.Values) != 10 || len(report.Types) != 8 {
		t.Fatalf("report summary = %+v, want ten classified values", report)
	}

	assertValueLifecycle(t, report, "oauth_states", secret.SecretKindOAuthCodeVerifier, 10, LifecycleUnreadableExpiredPurgeable, ReasonOAuthStateExpired, secret.CiphertextFormatLegacy, "")
	assertValueLifecycle(t, report, "oauth_states", secret.SecretKindOAuthCodeVerifier, 11, LifecycleUnreadableExpiredPurgeable, ReasonOAuthStateConsumed, secret.CiphertextFormatLegacy, "")
	assertValueLifecycle(t, report, "oauth_states", secret.SecretKindOAuthCodeVerifier, 12, LifecycleUnreadableRequired, ReasonOAuthStateActive, secret.CiphertextFormatLegacy, "")
	assertValueLifecycle(t, report, "provider_account_credentials", secret.SecretKindOAuthAccessToken, 20, LifecycleReadableCurrentKey, ReasonCurrentKeyVerified, secret.CiphertextFormatV1, "current")
	assertValueLifecycle(t, report, "provider_account_credentials", secret.SecretKindOAuthRefreshToken, 20, LifecycleReadablePreviousKey, ReasonPreviousKeyVerified, secret.CiphertextFormatV1, "previous")
	assertValueLifecycle(t, report, "provider_account_credentials", secret.SecretKindOAuthIDToken, 20, LifecycleReadableLegacy, ReasonLegacyCiphertext, secret.CiphertextFormatLegacy, "previous")
	assertValueLifecycle(t, report, "provider_account_credentials", secret.SecretKindProviderAPIKey, 21, LifecycleUnreadableRequired, ReasonCredentialRequired, secret.CiphertextFormatV1, "")
	assertValueLifecycle(t, report, "provider_account_credentials", secret.SecretKindProviderProxyURL, 21, LifecycleUnreadableRequired, ReasonCredentialRequired, secret.CiphertextFormatV1, "")
	assertValueLifecycle(t, report, "client_api_keys", secret.SecretKindClientAPIKey, 30, LifecycleUnreadableRequired, ReasonCredentialRequired, secret.CiphertextFormatV1, "")

	encoded, err := json.Marshal(report)
	if err != nil {
		t.Fatalf("Marshal returned error: %v", err)
	}
	for _, forbidden := range []string{
		"current-access-token", "previous-refresh-token", "cross-kind-canary", "unknown-key-canary",
		"corrupt-envelope-canary", "active-state-canary", "expired-state-canary", "consumed-state-canary", "missing",
	} {
		if strings.Contains(string(encoded), forbidden) {
			t.Fatalf("report leaked %q: %s", forbidden, encoded)
		}
	}
}

func TestVerifyReportsAttentionForOnlyExplicitlyPurgeableUnreadableValues(t *testing.T) {
	now := time.Date(2026, 7, 23, 12, 0, 0, 0, time.UTC)
	expiredAt := now.Add(-time.Second)
	report, err := VerifyAt(context.Background(), memoryRepository{values: []EncryptedValue{{
		Table: "oauth_states", Type: secret.SecretKindOAuthCodeVerifier, RowID: 1,
		Ciphertext: "expired-canary", ExpiresAt: &expiredAt,
	}}}, testKeyring(t), now)
	if err != nil {
		t.Fatalf("VerifyAt returned error: %v", err)
	}
	if report.Status != StatusAttention || lifecycleCount(report.LifecycleCounts, LifecycleUnreadableExpiredPurgeable) != 1 {
		t.Fatalf("report = %+v, want non-blocking attention", report)
	}
}

func TestVerifyTreatsMissingTemporaryLifecycleAsUnknownAndBlocking(t *testing.T) {
	report, err := VerifyAt(context.Background(), memoryRepository{values: []EncryptedValue{{
		Table: "oauth_states", Type: secret.SecretKindOAuthCodeVerifier, RowID: 1, Ciphertext: "unknown-lifecycle-canary",
	}}}, testKeyring(t), time.Now())
	if err != nil {
		t.Fatalf("VerifyAt returned error: %v", err)
	}
	assertValueLifecycle(t, report, "oauth_states", secret.SecretKindOAuthCodeVerifier, 1, LifecycleUnreadableUnknown, ReasonLifecycleUnknown, secret.CiphertextFormatLegacy, "")
	if report.Status != StatusFailed {
		t.Fatalf("status = %q, want failed", report.Status)
	}
}

func TestVerifyKeepsEveryCredentialClassAndLifecycleStatusAtZero(t *testing.T) {
	report, err := VerifyAt(context.Background(), memoryRepository{}, testKeyring(t), time.Time{})
	if err != nil {
		t.Fatalf("VerifyAt returned error: %v", err)
	}
	if report.Status != StatusOK || report.Count != 0 || len(report.Types) != 8 || len(report.Values) != 0 {
		t.Fatalf("empty report = %+v", report)
	}
	for _, typeReport := range report.Types {
		if typeReport.Count != 0 || len(typeReport.LifecycleCounts) != len(lifecycleStatuses) {
			t.Fatalf("empty type report = %+v", typeReport)
		}
		for index, count := range typeReport.LifecycleCounts {
			if count.Status != lifecycleStatuses[index] || count.Count != 0 {
				t.Fatalf("lifecycle count %d = %+v", index, count)
			}
		}
	}
}

func TestVerifyOutputIsDeterministic(t *testing.T) {
	keyring := testKeyring(t)
	now := time.Date(2026, 7, 23, 12, 0, 0, 0, time.UTC)
	values := []EncryptedValue{
		{Table: "client_api_keys", Type: secret.SecretKindClientAPIKey, RowID: 8, Ciphertext: "broken-eight"},
		{Table: "client_api_keys", Type: secret.SecretKindClientAPIKey, RowID: 7, Ciphertext: "broken-seven"},
	}
	first, err := VerifyAt(context.Background(), memoryRepository{values: append([]EncryptedValue(nil), values...)}, keyring, now)
	if err != nil {
		t.Fatal(err)
	}
	second, err := VerifyAt(context.Background(), memoryRepository{values: []EncryptedValue{values[1], values[0]}}, keyring, now)
	if err != nil {
		t.Fatal(err)
	}
	firstJSON, _ := json.Marshal(first)
	secondJSON, _ := json.Marshal(second)
	if string(firstJSON) != string(secondJSON) {
		t.Fatalf("reports differ:\n%s\n%s", firstJSON, secondJSON)
	}
}

func TestVerifyRejectsRepositoryAndUnexpectedRowErrors(t *testing.T) {
	keyring := testKeyring(t)
	if _, err := Verify(context.Background(), memoryRepository{err: errors.New("database-canary")}, keyring); err == nil || strings.Contains(err.Error(), "database-canary") {
		t.Fatalf("repository error = %v, want redacted error", err)
	}
	if _, err := Verify(context.Background(), memoryRepository{values: []EncryptedValue{{Table: "unknown", Type: secret.SecretKindGeneric, RowID: 1, Ciphertext: "value-canary"}}}, keyring); err == nil || strings.Contains(err.Error(), "value-canary") {
		t.Fatalf("unexpected-row error = %v, want redacted error", err)
	}
}

func assertValueLifecycle(t *testing.T, report Report, table string, kind secret.SecretKind, rowID int64, status LifecycleStatus, reason ReasonCode, format secret.CiphertextFormat, keyID string) {
	t.Helper()
	for _, value := range report.Values {
		if value.Table == table && value.CredentialKind == kind && value.RowID == rowID {
			if value.LifecycleStatus != status || value.ReasonCode != reason || value.EnvelopeFormat != format || value.AuthenticatedKeyID != keyID {
				t.Fatalf("value = %+v, want status=%s reason=%s format=%s key=%q", value, status, reason, format, keyID)
			}
			return
		}
	}
	t.Fatalf("missing value %s/%s/%d", table, kind, rowID)
}

func testKeyring(t *testing.T) *secret.Keyring {
	t.Helper()
	return mustKeyring(t, "current", "current-encryption-secret-at-least-32-bytes", []secret.EncryptionKey{{ID: "previous", Secret: "legacy-encryption-secret"}})
}

func mustKeyring(t *testing.T, id, value string, previous []secret.EncryptionKey) *secret.Keyring {
	t.Helper()
	keyring, err := secret.NewKeyring(secret.EncryptionKey{ID: id, Secret: value}, previous)
	if err != nil {
		t.Fatalf("NewKeyring returned error: %v", err)
	}
	return keyring
}

func mustEncryptFor(t *testing.T, keyring *secret.Keyring, kind secret.SecretKind, value string) string {
	t.Helper()
	encrypted, err := keyring.EncryptStringFor(kind, value)
	if err != nil {
		t.Fatalf("EncryptStringFor returned error: %v", err)
	}
	return encrypted
}

const legacyCiphertextFixture = "AAECAwQFBgcICQoLshPzMSnIGUlIyhB+W347vBUF57bAkCtXBN4l54ODVswuO/ASFnqXSM2t"

package encryptioninventory

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/KnowSky404/N2API/backend/internal/secret"
)

type memoryRepository struct {
	values []EncryptedValue
	err    error
}

func (r memoryRepository) ListEncryptedValues(context.Context) ([]EncryptedValue, error) {
	return r.values, r.err
}

func TestVerifyAccountsForEveryEncryptedCredentialClass(t *testing.T) {
	keyring := testKeyring(t)
	legacy := legacyCiphertextFixture
	previous := mustEncryptFor(t, keyring, secret.SecretKindOAuthRefreshToken, "previous-refresh-token")
	current := mustEncryptFor(t, keyring, secret.SecretKindOAuthAccessToken, "current-access-token")

	repo := memoryRepository{values: []EncryptedValue{
		{Table: "provider_account_credentials", Type: secret.SecretKindOAuthRefreshToken, RowID: 20, Ciphertext: previous},
		{Table: "oauth_states", Type: secret.SecretKindOAuthCodeVerifier, RowID: 10, Ciphertext: legacy},
		{Table: "provider_account_credentials", Type: secret.SecretKindOAuthAccessToken, RowID: 20, Ciphertext: current},
		{Table: "provider_account_credentials", Type: secret.SecretKindOAuthIDToken, RowID: 20, Ciphertext: legacy},
		{Table: "provider_account_credentials", Type: secret.SecretKindProviderAPIKey, RowID: 21, Ciphertext: legacy},
		{Table: "provider_account_credentials", Type: secret.SecretKindProviderProxyURL, RowID: 21, Ciphertext: legacy},
		{Table: "client_api_keys", Type: secret.SecretKindClientAPIKey, RowID: 30, Ciphertext: legacy},
		{Table: "client_api_keys", Type: secret.SecretKindClientAPIKey, RowID: 31, Ciphertext: ""},
	}}

	report, err := Verify(context.Background(), repo, keyring)
	if err != nil {
		t.Fatalf("Verify returned error: %v", err)
	}
	if report.Status != StatusOK || report.Totals != (Totals{Values: 7, Verified: 7}) {
		t.Fatalf("report summary = %+v, want seven verified values", report)
	}
	if len(report.Types) != 7 {
		t.Fatalf("type count = %d, want 7", len(report.Types))
	}
	for _, typeReport := range report.Types {
		if typeReport.Values != 1 || typeReport.Verified != 1 || typeReport.Failed != 0 {
			t.Fatalf("type report = %+v, want one verified value", typeReport)
		}
	}
	access := report.Types[1]
	if len(access.KeyIDs) != 1 || access.KeyIDs[0] != (KeyIDCount{ID: "current", Format: secret.CiphertextFormatV1, Count: 1}) {
		t.Fatalf("access-token key IDs = %+v, want current v1", access.KeyIDs)
	}
	refresh := report.Types[2]
	if len(refresh.KeyIDs) != 1 || refresh.KeyIDs[0].ID != "current" || refresh.KeyIDs[0].Format != secret.CiphertextFormatV1 {
		t.Fatalf("refresh-token key IDs = %+v, want authenticated current v1", refresh.KeyIDs)
	}
	codeVerifier := report.Types[0]
	if len(codeVerifier.KeyIDs) != 1 || codeVerifier.KeyIDs[0] != (KeyIDCount{ID: "previous", Format: secret.CiphertextFormatLegacy, Count: 1}) {
		t.Fatalf("legacy key IDs = %+v, want actual previous key", codeVerifier.KeyIDs)
	}
}

func TestVerifyReportsAllUnreadableRowsWithoutLeakingValues(t *testing.T) {
	keyring := testKeyring(t)
	const ciphertextCanary = "n2api:v1:missing:oauth-access-token:ciphertext-canary"
	repo := memoryRepository{values: []EncryptedValue{
		{Table: "provider_account_credentials", Type: secret.SecretKindOAuthAccessToken, RowID: 42, Ciphertext: ciphertextCanary},
		{Table: "client_api_keys", Type: secret.SecretKindClientAPIKey, RowID: 7, Ciphertext: "plaintext-canary"},
		{Table: "client_api_keys", Type: secret.SecretKindClientAPIKey, RowID: 8, Ciphertext: "   "},
	}}

	report, err := Verify(context.Background(), repo, keyring)
	if err != nil {
		t.Fatalf("Verify returned error: %v", err)
	}
	if report.Status != StatusFailed || report.Totals != (Totals{Values: 3, Failed: 3}) {
		t.Fatalf("report summary = %+v, want three failures", report)
	}
	if len(report.Failures) != 3 {
		t.Fatalf("failures = %+v, want three", report.Failures)
	}
	for _, failure := range report.Failures {
		if failure.Status != FailureUnreadable {
			t.Fatalf("failure = %+v, want unreadable status", failure)
		}
	}

	encoded, err := json.Marshal(report)
	if err != nil {
		t.Fatalf("Marshal returned error: %v", err)
	}
	for _, forbidden := range []string{ciphertextCanary, "plaintext-canary", "missing"} {
		if strings.Contains(string(encoded), forbidden) {
			t.Fatalf("report leaked %q: %s", forbidden, encoded)
		}
	}
}

func TestVerifyKeepsEmptyCredentialClassesAndDeterministicOrder(t *testing.T) {
	report, err := Verify(context.Background(), memoryRepository{}, testKeyring(t))
	if err != nil {
		t.Fatalf("Verify returned error: %v", err)
	}
	if report.Status != StatusOK || report.Totals != (Totals{}) || len(report.Types) != 7 || len(report.Failures) != 0 {
		t.Fatalf("empty report = %+v", report)
	}
	wants := []secret.SecretKind{
		secret.SecretKindOAuthCodeVerifier,
		secret.SecretKindOAuthAccessToken,
		secret.SecretKindOAuthRefreshToken,
		secret.SecretKindOAuthIDToken,
		secret.SecretKindProviderAPIKey,
		secret.SecretKindProviderProxyURL,
		secret.SecretKindClientAPIKey,
	}
	for index, want := range wants {
		if report.Types[index].Type != want {
			t.Fatalf("type %d = %q, want %q", index, report.Types[index].Type, want)
		}
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

func testKeyring(t *testing.T) *secret.Keyring {
	t.Helper()
	keyring, err := secret.NewKeyring(
		secret.EncryptionKey{ID: "current", Secret: "current-encryption-secret-at-least-32-bytes"},
		[]secret.EncryptionKey{{ID: "previous", Secret: "legacy-encryption-secret"}},
	)
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

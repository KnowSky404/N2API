package secret

import (
	"crypto/pbkdf2"
	"crypto/sha256"
	"encoding/base64"
	"fmt"
	"strings"
	"testing"
)

func TestHashAPIKeyVerifiesOriginalKey(t *testing.T) {
	hash := HashAPIKey("n2_live_secret")

	if hash == "" {
		t.Fatal("HashAPIKey returned empty hash")
	}
	if hash == "n2_live_secret" {
		t.Fatal("HashAPIKey returned the original API key")
	}
	if !VerifyAPIKey(hash, "n2_live_secret") {
		t.Fatal("VerifyAPIKey returned false for original API key")
	}
	if VerifyAPIKey(hash, "different") {
		t.Fatal("VerifyAPIKey returned true for different API key")
	}
}

func TestPasswordHashVerifiesOriginalPassword(t *testing.T) {
	hash, err := HashPassword("owner-password")
	if err != nil {
		t.Fatalf("HashPassword returned error: %v", err)
	}
	if hash == "" || hash == "owner-password" {
		t.Fatalf("HashPassword returned unsafe hash %q", hash)
	}
	if !VerifyPassword(hash, "owner-password") {
		t.Fatal("VerifyPassword returned false for original password")
	}
	if VerifyPassword(hash, "wrong-password") {
		t.Fatal("VerifyPassword returned true for wrong password")
	}
}

func TestVerifyPasswordRejectsUnexpectedIterationCount(t *testing.T) {
	hash := passwordHashForTest(t, "owner-password", []byte("1234567890abcdef"), passwordIterations-1, passwordKeyBytes)

	if VerifyPassword(hash, "owner-password") {
		t.Fatal("VerifyPassword returned true for unexpected iteration count")
	}
}

func TestVerifyPasswordRejectsUnexpectedSaltLength(t *testing.T) {
	hash := passwordHashForTest(t, "owner-password", []byte("short-salt-1234"), passwordIterations, passwordKeyBytes)

	if VerifyPassword(hash, "owner-password") {
		t.Fatal("VerifyPassword returned true for unexpected salt length")
	}
}

func TestVerifyPasswordRejectsUnexpectedKeyLength(t *testing.T) {
	hash := passwordHashForTest(t, "owner-password", []byte("1234567890abcdef"), passwordIterations, passwordKeyBytes-1)

	if VerifyPassword(hash, "owner-password") {
		t.Fatal("VerifyPassword returned true for unexpected key length")
	}
}

func TestGenerateTokenUsesPrefixAndRandomSecret(t *testing.T) {
	first, err := GenerateToken("n2api")
	if err != nil {
		t.Fatalf("GenerateToken returned error: %v", err)
	}
	second, err := GenerateToken("n2api")
	if err != nil {
		t.Fatalf("GenerateToken returned error: %v", err)
	}
	if !strings.HasPrefix(first, "n2api_") {
		t.Fatalf("token = %q, want n2api_ prefix", first)
	}
	if first == second {
		t.Fatal("GenerateToken returned duplicate tokens")
	}

	secret := strings.TrimPrefix(first, "n2api_")
	if len(secret) != 43 {
		t.Fatalf("secret length = %d, want 43", len(secret))
	}
	if strings.Contains(secret, "=") {
		t.Fatalf("secret = %q, want no padding", secret)
	}
	decoded, err := base64.RawURLEncoding.DecodeString(secret)
	if err != nil {
		t.Fatalf("secret did not decode as raw URL base64: %v", err)
	}
	if len(decoded) != 32 {
		t.Fatalf("decoded secret length = %d, want 32", len(decoded))
	}
}

func TestGeneratedTokenCanReuseAPIKeyHashVerification(t *testing.T) {
	token, err := GenerateToken("n2api")
	if err != nil {
		t.Fatalf("GenerateToken returned error: %v", err)
	}

	hash := HashAPIKey(token)

	if !VerifyAPIKey(hash, token) {
		t.Fatal("VerifyAPIKey returned false for generated token")
	}
	if VerifyAPIKey(hash, token+"x") {
		t.Fatal("VerifyAPIKey returned true for modified generated token")
	}
}

func TestTokenPrefixReturnsDisplayPrefix(t *testing.T) {
	prefix := TokenPrefix("n2api_abcdefghijklmnopqrstuvwxyz")
	if prefix != "n2api_abcdefgh" {
		t.Fatalf("TokenPrefix = %q, want n2api_abcdefgh", prefix)
	}
}

func TestEncryptDecryptRoundTrip(t *testing.T) {
	encrypted, err := EncryptString("long-encryption-secret", "oauth-refresh-token")
	if err != nil {
		t.Fatalf("EncryptString returned error: %v", err)
	}
	if encrypted == "" {
		t.Fatal("EncryptString returned empty ciphertext")
	}
	if encrypted == "oauth-refresh-token" {
		t.Fatal("EncryptString returned plaintext")
	}
	if !strings.HasPrefix(encrypted, "n2api:v1:default:generic:") {
		t.Fatalf("EncryptString = %q, want versioned default-key envelope", encrypted)
	}

	decrypted, err := DecryptString("long-encryption-secret", encrypted)
	if err != nil {
		t.Fatalf("DecryptString returned error: %v", err)
	}
	if decrypted != "oauth-refresh-token" {
		t.Fatalf("DecryptString = %q, want oauth-refresh-token", decrypted)
	}
}

func TestDecryptRejectsWrongSecret(t *testing.T) {
	encrypted, err := EncryptString("long-encryption-secret", "oauth-refresh-token")
	if err != nil {
		t.Fatalf("EncryptString returned error: %v", err)
	}

	if _, err := DecryptString("different-secret", encrypted); err == nil {
		t.Fatal("DecryptString returned nil error for wrong secret")
	}
}

func TestDecryptReadsImmutableLegacyFixture(t *testing.T) {
	const legacyCiphertext = "AAECAwQFBgcICQoLshPzMSnIGUlIyhB+W347vBUF57bAkCtXBN4l54ODVswuO/ASFnqXSM2t"

	plaintext, err := DecryptString("legacy-encryption-secret", legacyCiphertext)
	if err != nil {
		t.Fatalf("DecryptString returned error for legacy fixture: %v", err)
	}
	if plaintext != "legacy-oauth-refresh-token" {
		t.Fatalf("DecryptString = %q, want legacy-oauth-refresh-token", plaintext)
	}
}

func TestKeyringReadsLegacyValuesInConfiguredOrder(t *testing.T) {
	const legacyCiphertext = "AAECAwQFBgcICQoLshPzMSnIGUlIyhB+W347vBUF57bAkCtXBN4l54ODVswuO/ASFnqXSM2t"
	keyring, err := NewKeyring(
		EncryptionKey{ID: "current", Secret: "current-encryption-secret"},
		[]EncryptionKey{
			{ID: "previous-wrong", Secret: "wrong-previous-encryption-secret"},
			{ID: "previous-match", Secret: "legacy-encryption-secret"},
		},
	)
	if err != nil {
		t.Fatalf("NewKeyring returned error: %v", err)
	}

	plaintext, err := keyring.DecryptString(legacyCiphertext)
	if err != nil {
		t.Fatalf("DecryptString returned error for previous key: %v", err)
	}
	if plaintext != "legacy-oauth-refresh-token" {
		t.Fatalf("DecryptString = %q, want legacy-oauth-refresh-token", plaintext)
	}
}

func TestKeyringWritesCurrentKeyEnvelope(t *testing.T) {
	keyring, err := NewKeyring(
		EncryptionKey{ID: "current-202607", Secret: "current-encryption-secret"},
		[]EncryptionKey{{ID: "previous-202606", Secret: "previous-encryption-secret"}},
	)
	if err != nil {
		t.Fatalf("NewKeyring returned error: %v", err)
	}

	encrypted, err := keyring.EncryptString("provider-api-key")
	if err != nil {
		t.Fatalf("EncryptString returned error: %v", err)
	}
	if !strings.HasPrefix(encrypted, "n2api:v1:current-202607:generic:") {
		t.Fatalf("EncryptString = %q, want current key ID", encrypted)
	}
	decrypted, err := keyring.DecryptString(encrypted)
	if err != nil {
		t.Fatalf("DecryptString returned error: %v", err)
	}
	if decrypted != "provider-api-key" {
		t.Fatalf("DecryptString = %q, want provider-api-key", decrypted)
	}
}

func TestKeyringEnvelopeSupportsEdgeValuesAndRandomNonces(t *testing.T) {
	keyring, err := NewKeyring(EncryptionKey{ID: "current", Secret: "current-encryption-secret"}, nil)
	if err != nil {
		t.Fatalf("NewKeyring returned error: %v", err)
	}
	for name, value := range map[string]string{
		"empty":   "",
		"unicode": "\u79d8\u5bc6-token-\u2713",
		"long":    strings.Repeat("x", 16*1024),
	} {
		t.Run(name, func(t *testing.T) {
			encrypted, err := keyring.EncryptString(value)
			if err != nil {
				t.Fatalf("EncryptString returned error: %v", err)
			}
			decrypted, err := keyring.DecryptString(encrypted)
			if err != nil {
				t.Fatalf("DecryptString returned error: %v", err)
			}
			if decrypted != value {
				t.Fatalf("DecryptString length = %d, want %d", len(decrypted), len(value))
			}
		})
	}

	first, err := keyring.EncryptString("same-value")
	if err != nil {
		t.Fatalf("first EncryptString returned error: %v", err)
	}
	second, err := keyring.EncryptString("same-value")
	if err != nil {
		t.Fatalf("second EncryptString returned error: %v", err)
	}
	if first == second {
		t.Fatal("EncryptString reused a nonce for repeated plaintext")
	}
}

func TestKeyringRejectsEnvelopeMetadataTampering(t *testing.T) {
	keyring, err := NewKeyring(
		EncryptionKey{ID: "current", Secret: "current-encryption-secret"},
		[]EncryptionKey{{ID: "previous", Secret: "previous-encryption-secret"}},
	)
	if err != nil {
		t.Fatalf("NewKeyring returned error: %v", err)
	}
	encrypted, err := keyring.EncryptString("oauth-access-token")
	if err != nil {
		t.Fatalf("EncryptString returned error: %v", err)
	}

	tampered := strings.Replace(encrypted, ":current:", ":previous:", 1)
	if _, err := keyring.DecryptString(tampered); err == nil {
		t.Fatal("DecryptString returned nil error for tampered key ID")
	}
}

func TestKeyringRejectsCrossKindEnvelopeSubstitution(t *testing.T) {
	keyring, err := NewKeyring(EncryptionKey{ID: "current", Secret: "current-encryption-secret"}, nil)
	if err != nil {
		t.Fatalf("NewKeyring returned error: %v", err)
	}
	encrypted, err := keyring.EncryptStringFor(SecretKindOAuthAccessToken, "oauth-access-token")
	if err != nil {
		t.Fatalf("EncryptStringFor returned error: %v", err)
	}
	if _, err := keyring.DecryptStringFor(SecretKindOAuthRefreshToken, encrypted); err == nil {
		t.Fatal("DecryptStringFor accepted an access-token envelope as a refresh token")
	}

	tampered := strings.Replace(encrypted, ":oauth-access-token:", ":oauth-refresh-token:", 1)
	if _, err := keyring.DecryptStringFor(SecretKindOAuthRefreshToken, tampered); err == nil {
		t.Fatal("DecryptStringFor accepted a tampered secret kind")
	}
}

func TestKeyringEncryptsAlertActionDestinationWithDedicatedKind(t *testing.T) {
	keyring, err := NewKeyring(EncryptionKey{ID: "current", Secret: "current-encryption-secret-at-least-32-bytes"}, nil)
	if err != nil {
		t.Fatalf("NewKeyring returned error: %v", err)
	}

	encrypted, err := keyring.EncryptStringFor(SecretKindAlertActionDestination, "https://example.test/topic?token=canary")
	if err != nil {
		t.Fatalf("EncryptStringFor returned error: %v", err)
	}
	if !strings.HasPrefix(encrypted, "n2api:v1:current:alert-action-destination:") {
		t.Fatalf("encrypted destination has unexpected envelope: %q", encrypted)
	}
	decrypted, err := keyring.DecryptStringFor(SecretKindAlertActionDestination, encrypted)
	if err != nil {
		t.Fatalf("DecryptStringFor returned error: %v", err)
	}
	if decrypted != "https://example.test/topic?token=canary" {
		t.Fatalf("decrypted destination = %q", decrypted)
	}
	if _, err := keyring.DecryptStringFor(SecretKindProviderProxyURL, encrypted); err == nil {
		t.Fatal("DecryptStringFor accepted an alert destination as a provider proxy URL")
	}
}

func TestKeyringVerificationReportsAuthenticatedFormatAndActualKey(t *testing.T) {
	keyring, err := NewKeyring(
		EncryptionKey{ID: "current", Secret: "current-encryption-secret"},
		[]EncryptionKey{{ID: "previous", Secret: "legacy-encryption-secret"}},
	)
	if err != nil {
		t.Fatalf("NewKeyring returned error: %v", err)
	}
	encrypted, err := keyring.EncryptStringFor(SecretKindOAuthAccessToken, "access-token")
	if err != nil {
		t.Fatalf("EncryptStringFor returned error: %v", err)
	}
	verification, err := keyring.VerifyStringFor(SecretKindOAuthAccessToken, encrypted)
	if err != nil {
		t.Fatalf("VerifyStringFor returned error: %v", err)
	}
	if verification != (CiphertextVerification{KeyID: "current", Format: CiphertextFormatV1}) {
		t.Fatalf("verification = %+v, want current v1", verification)
	}
	previousWriter, err := NewKeyring(EncryptionKey{ID: "previous", Secret: "legacy-encryption-secret"}, nil)
	if err != nil {
		t.Fatalf("NewKeyring previous writer returned error: %v", err)
	}
	previousEnvelope, err := previousWriter.EncryptStringFor(SecretKindOAuthAccessToken, "previous-access-token")
	if err != nil {
		t.Fatalf("EncryptStringFor previous returned error: %v", err)
	}
	verification, err = keyring.VerifyStringFor(SecretKindOAuthAccessToken, previousEnvelope)
	if err != nil {
		t.Fatalf("VerifyStringFor previous envelope returned error: %v", err)
	}
	if verification != (CiphertextVerification{KeyID: "previous", Format: CiphertextFormatV1}) {
		t.Fatalf("previous verification = %+v, want previous v1", verification)
	}

	const legacyCiphertext = "AAECAwQFBgcICQoLshPzMSnIGUlIyhB+W347vBUF57bAkCtXBN4l54ODVswuO/ASFnqXSM2t"
	verification, err = keyring.VerifyStringFor(SecretKindOAuthRefreshToken, legacyCiphertext)
	if err != nil {
		t.Fatalf("VerifyStringFor legacy returned error: %v", err)
	}
	if verification != (CiphertextVerification{KeyID: "previous", Format: CiphertextFormatLegacy}) {
		t.Fatalf("legacy verification = %+v, want actual previous key", verification)
	}
}

func TestKeyringVerificationRejectsUnauthenticatedMetadata(t *testing.T) {
	keyring, err := NewKeyring(EncryptionKey{ID: "current", Secret: "current-encryption-secret"}, nil)
	if err != nil {
		t.Fatalf("NewKeyring returned error: %v", err)
	}
	for _, encoded := range []string{
		"n2api:v1:current:oauth-access-token:AA",
		"n2api:v1:missing:oauth-access-token:AA",
		"plaintext-canary",
	} {
		if _, err := keyring.VerifyStringFor(SecretKindOAuthAccessToken, encoded); err == nil {
			t.Fatalf("VerifyStringFor accepted %q", encoded)
		}
	}
}

func TestKeyringRejectsUnknownEnvelopeVersionAndKey(t *testing.T) {
	keyring, err := NewKeyring(EncryptionKey{ID: "current", Secret: "current-encryption-secret"}, nil)
	if err != nil {
		t.Fatalf("NewKeyring returned error: %v", err)
	}
	encrypted, err := keyring.EncryptString("oauth-access-token")
	if err != nil {
		t.Fatalf("EncryptString returned error: %v", err)
	}

	for name, tampered := range map[string]string{
		"version": strings.Replace(encrypted, ":v1:", ":v2:", 1),
		"key":     strings.Replace(encrypted, ":current:", ":missing:", 1),
	} {
		t.Run(name, func(t *testing.T) {
			if _, err := keyring.DecryptString(tampered); err == nil {
				t.Fatal("DecryptString returned nil error")
			}
		})
	}
}

func TestKeyringRejectsTamperedAndMalformedEnvelopePayloadsWithoutLegacyFallback(t *testing.T) {
	keyring, err := NewKeyring(EncryptionKey{ID: "current", Secret: "legacy-encryption-secret"}, nil)
	if err != nil {
		t.Fatalf("NewKeyring returned error: %v", err)
	}
	encrypted, err := keyring.EncryptString("oauth-access-token")
	if err != nil {
		t.Fatalf("EncryptString returned error: %v", err)
	}
	separator := strings.LastIndexByte(encrypted, ':')
	payload, err := base64.RawStdEncoding.DecodeString(encrypted[separator+1:])
	if err != nil {
		t.Fatalf("DecodeString returned error: %v", err)
	}
	for name, index := range map[string]int{
		"nonce":      0,
		"ciphertext": len(payload) / 2,
		"tag":        len(payload) - 1,
	} {
		t.Run(name, func(t *testing.T) {
			tamperedPayload := append([]byte(nil), payload...)
			tamperedPayload[index] ^= 0xff
			tampered := encrypted[:separator+1] + base64.RawStdEncoding.EncodeToString(tamperedPayload)
			if _, err := keyring.DecryptString(tampered); err == nil {
				t.Fatal("DecryptString returned nil error for tampered payload")
			}
		})
	}

	const legacyCiphertext = "AAECAwQFBgcICQoLshPzMSnIGUlIyhB+W347vBUF57bAkCtXBN4l54ODVswuO/ASFnqXSM2t"
	for name, candidate := range map[string]string{
		"invalid base64":  "n2api:v1:current:generic:***",
		"truncated":       "n2api:v1:current:generic:AA",
		"malformed":       "n2api:v1:current",
		"forged envelope": "n2api:v1:current:generic:" + legacyCiphertext,
	} {
		t.Run(name, func(t *testing.T) {
			if _, err := keyring.DecryptString(candidate); err == nil {
				t.Fatal("DecryptString returned nil error")
			}
		})
	}
}

func TestNewKeyringRejectsInvalidAndDuplicateKeyIDs(t *testing.T) {
	for name, tt := range map[string]struct {
		current  EncryptionKey
		previous []EncryptionKey
	}{
		"invalid current": {
			current: EncryptionKey{ID: "bad:id", Secret: "current-secret"},
		},
		"duplicate": {
			current:  EncryptionKey{ID: "same", Secret: "current-secret"},
			previous: []EncryptionKey{{ID: "same", Secret: "previous-secret"}},
		},
		"duplicate secret": {
			current:  EncryptionKey{ID: "current", Secret: "same-secret"},
			previous: []EncryptionKey{{ID: "previous", Secret: "same-secret"}},
		},
		"empty previous secret": {
			current:  EncryptionKey{ID: "current", Secret: "current-secret"},
			previous: []EncryptionKey{{ID: "previous", Secret: ""}},
		},
	} {
		t.Run(name, func(t *testing.T) {
			if _, err := NewKeyring(tt.current, tt.previous); err == nil {
				t.Fatal("NewKeyring returned nil error")
			}
		})
	}
}

func TestEncryptionErrorsDoNotLeakSensitiveValues(t *testing.T) {
	const keyMaterial = "sensitive-encryption-key-material"
	const plaintext = "sensitive-provider-token"
	keyring, err := NewKeyring(EncryptionKey{ID: "current", Secret: keyMaterial}, nil)
	if err != nil {
		t.Fatalf("NewKeyring returned error: %v", err)
	}
	encrypted, err := keyring.EncryptString(plaintext)
	if err != nil {
		t.Fatalf("EncryptString returned error: %v", err)
	}
	separator := strings.LastIndexByte(encrypted, ':')
	payload, err := base64.RawStdEncoding.DecodeString(encrypted[separator+1:])
	if err != nil {
		t.Fatalf("DecodeString returned error: %v", err)
	}
	payload[len(payload)-1] ^= 0xff
	tampered := encrypted[:separator+1] + base64.RawStdEncoding.EncodeToString(payload)
	_, err = keyring.DecryptString(tampered)
	if err == nil {
		t.Fatal("DecryptString returned nil error")
	}
	for _, forbidden := range []string{keyMaterial, plaintext, encrypted, tampered} {
		if strings.Contains(err.Error(), forbidden) {
			t.Fatalf("DecryptString error leaked sensitive value: %q", err)
		}
	}
}

func passwordHashForTest(t *testing.T, password string, salt []byte, iterations, keyBytes int) string {
	t.Helper()

	key, err := pbkdf2.Key(sha256.New, password, salt, iterations, keyBytes)
	if err != nil {
		t.Fatalf("pbkdf2.Key returned error: %v", err)
	}

	return fmt.Sprintf(
		"%s$%d$%s$%s",
		passwordHashVersion,
		iterations,
		base64.RawStdEncoding.EncodeToString(salt),
		base64.RawStdEncoding.EncodeToString(key),
	)
}

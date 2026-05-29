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

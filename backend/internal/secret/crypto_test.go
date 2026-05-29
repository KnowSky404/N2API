package secret

import (
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

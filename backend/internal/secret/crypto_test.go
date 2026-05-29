package secret

import "testing"

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

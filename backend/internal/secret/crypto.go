package secret

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/pbkdf2"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"encoding/hex"
	"fmt"
	"strconv"
	"strings"
)

const (
	passwordHashVersion = "pbkdf2-sha256"
	passwordIterations  = 210000
	passwordSaltBytes   = 16
	passwordKeyBytes    = 32
	randomTokenBytes    = 32
)

func HashAPIKey(apiKey string) string {
	sum := sha256.Sum256([]byte(apiKey))
	return hex.EncodeToString(sum[:])
}

func VerifyAPIKey(hash, apiKey string) bool {
	candidate := HashAPIKey(apiKey)
	return subtle.ConstantTimeCompare([]byte(hash), []byte(candidate)) == 1
}

func HashPassword(password string) (string, error) {
	salt := make([]byte, passwordSaltBytes)
	if _, err := rand.Read(salt); err != nil {
		return "", fmt.Errorf("generate password salt: %w", err)
	}

	key, err := pbkdf2.Key(sha256.New, password, salt, passwordIterations, passwordKeyBytes)
	if err != nil {
		return "", fmt.Errorf("derive password key: %w", err)
	}

	return fmt.Sprintf(
		"%s$%d$%s$%s",
		passwordHashVersion,
		passwordIterations,
		base64.RawStdEncoding.EncodeToString(salt),
		base64.RawStdEncoding.EncodeToString(key),
	), nil
}

func VerifyPassword(hash, password string) bool {
	parts := strings.Split(hash, "$")
	if len(parts) != 4 || parts[0] != passwordHashVersion {
		return false
	}

	iterations, err := strconv.Atoi(parts[1])
	if err != nil || iterations != passwordIterations {
		return false
	}

	salt, err := base64.RawStdEncoding.DecodeString(parts[2])
	if err != nil || len(salt) != passwordSaltBytes {
		return false
	}

	expected, err := base64.RawStdEncoding.DecodeString(parts[3])
	if err != nil || len(expected) != passwordKeyBytes {
		return false
	}

	candidate, err := pbkdf2.Key(sha256.New, password, salt, iterations, passwordKeyBytes)
	if err != nil {
		return false
	}

	return subtle.ConstantTimeCompare(expected, candidate) == 1
}

func GenerateToken(prefix string) (string, error) {
	token := make([]byte, randomTokenBytes)
	if _, err := rand.Read(token); err != nil {
		return "", fmt.Errorf("generate token: %w", err)
	}

	return prefix + "_" + base64.RawURLEncoding.EncodeToString(token), nil
}

func TokenPrefix(token string) string {
	if len(token) <= 14 {
		return token
	}
	return token[:14]
}

func EncryptString(secret, plaintext string) (string, error) {
	gcm, err := newGCM(secret)
	if err != nil {
		return "", err
	}

	nonce := make([]byte, gcm.NonceSize())
	if _, err := rand.Read(nonce); err != nil {
		return "", fmt.Errorf("generate nonce: %w", err)
	}

	ciphertext := gcm.Seal(nil, nonce, []byte(plaintext), nil)
	payload := append(nonce, ciphertext...)
	return base64.RawStdEncoding.EncodeToString(payload), nil
}

func DecryptString(secret, encoded string) (string, error) {
	gcm, err := newGCM(secret)
	if err != nil {
		return "", err
	}

	payload, err := base64.RawStdEncoding.DecodeString(encoded)
	if err != nil {
		return "", fmt.Errorf("decode ciphertext: %w", err)
	}
	if len(payload) <= gcm.NonceSize() {
		return "", fmt.Errorf("ciphertext is too short")
	}

	nonce := payload[:gcm.NonceSize()]
	ciphertext := payload[gcm.NonceSize():]

	plaintext, err := gcm.Open(nil, nonce, ciphertext, nil)
	if err != nil {
		return "", fmt.Errorf("decrypt ciphertext: %w", err)
	}
	return string(plaintext), nil
}

func newGCM(secret string) (cipher.AEAD, error) {
	key := sha256.Sum256([]byte(secret))
	block, err := aes.NewCipher(key[:])
	if err != nil {
		return nil, fmt.Errorf("create cipher: %w", err)
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, fmt.Errorf("create gcm: %w", err)
	}
	return gcm, nil
}

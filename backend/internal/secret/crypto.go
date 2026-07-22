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

	ciphertextEnvelopeNamespace = "n2api"
	ciphertextEnvelopeVersion   = "v1"
	maxEncryptionKeyIDBytes     = 64
	maxPreviousEncryptionKeys   = 8
)

const (
	DefaultEncryptionKeyID    = "default"
	MinimumAdminPasswordBytes = 12
)

type SecretKind string

type CiphertextFormat string

const (
	SecretKindGeneric                SecretKind = "generic"
	SecretKindClientAPIKey           SecretKind = "client-api-key"
	SecretKindOAuthCodeVerifier      SecretKind = "oauth-code-verifier"
	SecretKindProviderAPIKey         SecretKind = "provider-api-key"
	SecretKindProviderProxyURL       SecretKind = "provider-proxy-url"
	SecretKindOAuthAccessToken       SecretKind = "oauth-access-token"
	SecretKindOAuthRefreshToken      SecretKind = "oauth-refresh-token"
	SecretKindOAuthIDToken           SecretKind = "oauth-id-token"
	SecretKindAlertActionDestination SecretKind = "alert-action-destination"

	CiphertextFormatV1     CiphertextFormat = "v1"
	CiphertextFormatLegacy CiphertextFormat = "legacy"
)

type CiphertextVerification struct {
	KeyID  string
	Format CiphertextFormat
}

type EncryptionKey struct {
	ID     string `json:"id"`
	Secret string `json:"secret"`
}

type Keyring struct {
	current    EncryptionKey
	byID       map[string]EncryptionKey
	legacyKeys []EncryptionKey
}

func NewKeyring(current EncryptionKey, previous []EncryptionKey) (*Keyring, error) {
	if err := validateEncryptionKey(current, "current"); err != nil {
		return nil, err
	}
	if len(previous) > maxPreviousEncryptionKeys {
		return nil, fmt.Errorf("previous encryption key count must not exceed %d", maxPreviousEncryptionKeys)
	}

	byID := make(map[string]EncryptionKey, len(previous)+1)
	byID[current.ID] = current
	secrets := map[string]struct{}{current.Secret: {}}
	legacyKeys := make([]EncryptionKey, 1, len(previous)+1)
	legacyKeys[0] = current
	for index, key := range previous {
		if err := validateEncryptionKey(key, "previous"); err != nil {
			return nil, fmt.Errorf("previous encryption key %d: %w", index, err)
		}
		if _, exists := byID[key.ID]; exists {
			return nil, fmt.Errorf("encryption key IDs must be unique")
		}
		if _, exists := secrets[key.Secret]; exists {
			return nil, fmt.Errorf("encryption key secrets must be unique")
		}
		byID[key.ID] = key
		secrets[key.Secret] = struct{}{}
		legacyKeys = append(legacyKeys, key)
	}

	return &Keyring{
		current:    current,
		byID:       byID,
		legacyKeys: legacyKeys,
	}, nil
}

func (k *Keyring) CurrentKeyID() string {
	if k == nil {
		return ""
	}
	return k.current.ID
}

func (k *Keyring) PreviousKeyCount() int {
	if k == nil || len(k.legacyKeys) == 0 {
		return 0
	}
	return len(k.legacyKeys) - 1
}

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
	keyring, err := NewKeyring(EncryptionKey{ID: DefaultEncryptionKeyID, Secret: secret}, nil)
	if err != nil {
		return "", err
	}
	return keyring.EncryptString(plaintext)
}

func (k *Keyring) EncryptString(plaintext string) (string, error) {
	return k.EncryptStringFor(SecretKindGeneric, plaintext)
}

func (k *Keyring) EncryptStringFor(kind SecretKind, plaintext string) (string, error) {
	if k == nil {
		return "", fmt.Errorf("encryption keyring is not configured")
	}
	if !validSecretKind(kind) {
		return "", fmt.Errorf("encryption secret kind is invalid")
	}
	gcm, err := newRandomNonceGCM(k.current.Secret)
	if err != nil {
		return "", err
	}

	header := ciphertextEnvelopeHeader(k.current.ID, kind)
	ciphertext := gcm.Seal(nil, nil, []byte(plaintext), []byte(header))
	return header + ":" + base64.RawStdEncoding.EncodeToString(ciphertext), nil
}

func DecryptString(secret, encoded string) (string, error) {
	keyring, err := NewKeyring(EncryptionKey{ID: DefaultEncryptionKeyID, Secret: secret}, nil)
	if err != nil {
		return "", err
	}
	return keyring.DecryptString(encoded)
}

func (k *Keyring) DecryptString(encoded string) (string, error) {
	return k.DecryptStringFor(SecretKindGeneric, encoded)
}

func (k *Keyring) DecryptStringFor(kind SecretKind, encoded string) (string, error) {
	plaintext, _, err := k.decryptBytesFor(kind, encoded)
	if err != nil {
		return "", err
	}
	return string(plaintext), nil
}

func (k *Keyring) VerifyStringFor(kind SecretKind, encoded string) (CiphertextVerification, error) {
	plaintext, verification, err := k.decryptBytesFor(kind, encoded)
	if err != nil {
		return CiphertextVerification{}, err
	}
	clear(plaintext)
	return verification, nil
}

func (k *Keyring) decryptBytesFor(kind SecretKind, encoded string) ([]byte, CiphertextVerification, error) {
	if k == nil {
		return nil, CiphertextVerification{}, fmt.Errorf("encryption keyring is not configured")
	}
	if !validSecretKind(kind) {
		return nil, CiphertextVerification{}, fmt.Errorf("encryption secret kind is invalid")
	}
	if strings.HasPrefix(encoded, ciphertextEnvelopeNamespace+":") {
		return k.decryptEnvelope(kind, encoded)
	}
	return k.decryptLegacy(encoded)
}

func (k *Keyring) decryptEnvelope(kind SecretKind, encoded string) ([]byte, CiphertextVerification, error) {
	parts := strings.SplitN(encoded, ":", 5)
	if len(parts) != 5 || parts[0] != ciphertextEnvelopeNamespace {
		return nil, CiphertextVerification{}, fmt.Errorf("ciphertext envelope is malformed")
	}
	if parts[1] != ciphertextEnvelopeVersion {
		return nil, CiphertextVerification{}, fmt.Errorf("ciphertext envelope version is unsupported")
	}
	if !validEncryptionKeyID(parts[2]) {
		return nil, CiphertextVerification{}, fmt.Errorf("ciphertext envelope key ID is invalid")
	}
	key, ok := k.byID[parts[2]]
	if !ok {
		return nil, CiphertextVerification{}, fmt.Errorf("ciphertext envelope key is unavailable")
	}
	envelopeKind := SecretKind(parts[3])
	if !validSecretKind(envelopeKind) {
		return nil, CiphertextVerification{}, fmt.Errorf("ciphertext envelope secret kind is invalid")
	}
	if envelopeKind != kind {
		return nil, CiphertextVerification{}, fmt.Errorf("ciphertext envelope secret kind does not match")
	}

	payload, err := base64.RawStdEncoding.DecodeString(parts[4])
	if err != nil {
		return nil, CiphertextVerification{}, fmt.Errorf("ciphertext payload encoding is invalid")
	}
	gcm, err := newRandomNonceGCM(key.Secret)
	if err != nil {
		return nil, CiphertextVerification{}, err
	}
	if len(payload) < gcm.Overhead() {
		return nil, CiphertextVerification{}, fmt.Errorf("ciphertext is too short")
	}

	header := ciphertextEnvelopeHeader(parts[2], envelopeKind)
	plaintext, err := gcm.Open(nil, nil, payload, []byte(header))
	if err != nil {
		return nil, CiphertextVerification{}, fmt.Errorf("decrypt ciphertext: authentication failed")
	}
	return plaintext, CiphertextVerification{KeyID: key.ID, Format: CiphertextFormatV1}, nil
}

func (k *Keyring) decryptLegacy(encoded string) ([]byte, CiphertextVerification, error) {
	payload, err := base64.RawStdEncoding.DecodeString(encoded)
	if err != nil {
		return nil, CiphertextVerification{}, fmt.Errorf("ciphertext payload encoding is invalid")
	}
	for _, key := range k.legacyKeys {
		gcm, err := newGCM(key.Secret)
		if err != nil {
			return nil, CiphertextVerification{}, err
		}
		if len(payload) <= gcm.NonceSize() {
			return nil, CiphertextVerification{}, fmt.Errorf("ciphertext is too short")
		}

		nonce := payload[:gcm.NonceSize()]
		ciphertext := payload[gcm.NonceSize():]
		plaintext, err := gcm.Open(nil, nonce, ciphertext, nil)
		if err == nil {
			return plaintext, CiphertextVerification{KeyID: key.ID, Format: CiphertextFormatLegacy}, nil
		}
	}
	return nil, CiphertextVerification{}, fmt.Errorf("decrypt ciphertext: authentication failed")
}

func ciphertextEnvelopeHeader(keyID string, kind SecretKind) string {
	return ciphertextEnvelopeNamespace + ":" + ciphertextEnvelopeVersion + ":" + keyID + ":" + string(kind)
}

func validateEncryptionKey(key EncryptionKey, position string) error {
	if !validEncryptionKeyID(key.ID) {
		return fmt.Errorf("%s encryption key ID is invalid", position)
	}
	if key.Secret == "" {
		return fmt.Errorf("%s encryption key secret is empty", position)
	}
	return nil
}

func validEncryptionKeyID(value string) bool {
	if len(value) == 0 || len(value) > maxEncryptionKeyIDBytes {
		return false
	}
	for _, char := range value {
		if (char >= 'a' && char <= 'z') || (char >= 'A' && char <= 'Z') ||
			(char >= '0' && char <= '9') || char == '.' || char == '_' || char == '-' {
			continue
		}
		return false
	}
	return true
}

func validSecretKind(kind SecretKind) bool {
	switch kind {
	case SecretKindGeneric,
		SecretKindClientAPIKey,
		SecretKindOAuthCodeVerifier,
		SecretKindProviderAPIKey,
		SecretKindProviderProxyURL,
		SecretKindOAuthAccessToken,
		SecretKindOAuthRefreshToken,
		SecretKindOAuthIDToken,
		SecretKindAlertActionDestination:
		return true
	default:
		return false
	}
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

func newRandomNonceGCM(secret string) (cipher.AEAD, error) {
	key := sha256.Sum256([]byte(secret))
	block, err := aes.NewCipher(key[:])
	if err != nil {
		return nil, fmt.Errorf("create cipher: %w", err)
	}
	gcm, err := cipher.NewGCMWithRandomNonce(block)
	if err != nil {
		return nil, fmt.Errorf("create gcm: %w", err)
	}
	return gcm, nil
}

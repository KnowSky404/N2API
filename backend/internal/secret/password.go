package secret

import (
	"crypto/pbkdf2"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"fmt"
	"io"
	"strconv"
	"strings"

	"golang.org/x/crypto/argon2"
)

const (
	argon2PasswordHashVersion = "argon2id"
	argon2MemoryKiB           = 64 * 1024
	argon2Iterations          = 2
	argon2Lanes               = 1
	passwordSaltBytes         = 16
	passwordKeyBytes          = 32

	maxArgon2MemoryKiB   = 128 * 1024
	maxArgon2Iterations  = 5
	maxArgon2Lanes       = 4
	minPasswordSaltBytes = 8
	maxPasswordSaltBytes = 32
	minPasswordKeyBytes  = 16
	maxPasswordKeyBytes  = 64

	legacyPasswordHashVersion = "pbkdf2-sha256"
	legacyPasswordIterations  = 210000

	maxEncodedPasswordHashBytes = 512
	passwordWorkConcurrency     = 1
)

var passwordBase64 = base64.RawStdEncoding.Strict()

type PasswordHasher interface {
	Hash(password string) (string, error)
	Verify(encoded, password string) (valid bool, needsRehash bool)
}

type argon2idPasswordHasher struct {
	random    io.Reader
	workSlots chan struct{}
	deriveKey func(password, salt []byte, iterations, memory uint32, lanes uint8, keyLen uint32) []byte
}

type parsedPasswordHash struct {
	algorithm  string
	memory     uint32
	iterations uint32
	lanes      uint8
	salt       []byte
	expected   []byte
}

var processPasswordHasher PasswordHasher = &argon2idPasswordHasher{
	random:    rand.Reader,
	workSlots: make(chan struct{}, passwordWorkConcurrency),
	deriveKey: argon2.IDKey,
}

// NewPasswordHasher returns the process-wide hasher so every password path
// shares the same bound on memory-hard work.
func NewPasswordHasher() PasswordHasher {
	return processPasswordHasher
}

func (h *argon2idPasswordHasher) Hash(password string) (string, error) {
	salt := make([]byte, passwordSaltBytes)
	if _, err := io.ReadFull(h.random, salt); err != nil {
		return "", fmt.Errorf("generate password salt: %w", err)
	}

	key := h.derivePasswordKey(
		[]byte(password), salt, argon2Iterations, argon2MemoryKiB, argon2Lanes, passwordKeyBytes,
	)
	return fmt.Sprintf(
		"$%s$v=%d$m=%d,t=%d,p=%d$%s$%s",
		argon2PasswordHashVersion,
		argon2.Version,
		argon2MemoryKiB,
		argon2Iterations,
		argon2Lanes,
		passwordBase64.EncodeToString(salt),
		passwordBase64.EncodeToString(key),
	), nil
}

func (h *argon2idPasswordHasher) Verify(encoded, password string) (bool, bool) {
	parsed, ok := parsePasswordHash(encoded)
	if !ok {
		return false, false
	}

	var candidate []byte
	switch parsed.algorithm {
	case argon2PasswordHashVersion:
		candidate = h.derivePasswordKey(
			[]byte(password), parsed.salt, parsed.iterations, parsed.memory, parsed.lanes, uint32(len(parsed.expected)),
		)
	case legacyPasswordHashVersion:
		h.workSlots <- struct{}{}
		var err error
		candidate, err = pbkdf2.Key(sha256.New, password, parsed.salt, int(parsed.iterations), len(parsed.expected))
		<-h.workSlots
		if err != nil {
			return false, false
		}
	default:
		return false, false
	}

	if subtle.ConstantTimeCompare(parsed.expected, candidate) != 1 {
		return false, false
	}
	return true, passwordHashNeedsRehash(parsed)
}

func (h *argon2idPasswordHasher) derivePasswordKey(password, salt []byte, iterations, memory uint32, lanes uint8, keyLen uint32) []byte {
	h.workSlots <- struct{}{}
	defer func() { <-h.workSlots }()
	return h.deriveKey(password, salt, iterations, memory, lanes, keyLen)
}

func parsePasswordHash(encoded string) (parsedPasswordHash, bool) {
	if encoded == "" || len(encoded) > maxEncodedPasswordHashBytes {
		return parsedPasswordHash{}, false
	}
	if strings.HasPrefix(encoded, "$"+argon2PasswordHashVersion+"$") {
		return parseArgon2idPasswordHash(encoded)
	}
	if strings.HasPrefix(encoded, legacyPasswordHashVersion+"$") {
		return parseLegacyPasswordHash(encoded)
	}
	return parsedPasswordHash{}, false
}

func parseArgon2idPasswordHash(encoded string) (parsedPasswordHash, bool) {
	parts := strings.Split(encoded, "$")
	if len(parts) != 6 || parts[0] != "" || parts[1] != argon2PasswordHashVersion || parts[2] != "v="+strconv.Itoa(argon2.Version) {
		return parsedPasswordHash{}, false
	}

	parameters := strings.Split(parts[3], ",")
	if len(parameters) != 3 {
		return parsedPasswordHash{}, false
	}
	values := make(map[string]uint64, len(parameters))
	for _, parameter := range parameters {
		pair := strings.Split(parameter, "=")
		if len(pair) != 2 || (pair[0] != "m" && pair[0] != "t" && pair[0] != "p") || !isCanonicalPasswordParameter(pair[1]) {
			return parsedPasswordHash{}, false
		}
		if _, duplicate := values[pair[0]]; duplicate {
			return parsedPasswordHash{}, false
		}
		value, err := strconv.ParseUint(pair[1], 10, 32)
		if err != nil || value == 0 {
			return parsedPasswordHash{}, false
		}
		values[pair[0]] = value
	}
	if len(values) != 3 || values["m"] > maxArgon2MemoryKiB || values["t"] > maxArgon2Iterations || values["p"] > maxArgon2Lanes {
		return parsedPasswordHash{}, false
	}

	salt, ok := decodeBoundedPasswordField(parts[4], minPasswordSaltBytes, maxPasswordSaltBytes)
	if !ok {
		return parsedPasswordHash{}, false
	}
	expected, ok := decodeBoundedPasswordField(parts[5], minPasswordKeyBytes, maxPasswordKeyBytes)
	if !ok {
		return parsedPasswordHash{}, false
	}
	return parsedPasswordHash{
		algorithm:  argon2PasswordHashVersion,
		memory:     uint32(values["m"]),
		iterations: uint32(values["t"]),
		lanes:      uint8(values["p"]),
		salt:       salt,
		expected:   expected,
	}, true
}

func isCanonicalPasswordParameter(value string) bool {
	if value == "" || len(value) > 6 || (len(value) > 1 && value[0] == '0') {
		return false
	}
	for _, digit := range value {
		if digit < '0' || digit > '9' {
			return false
		}
	}
	return true
}

func parseLegacyPasswordHash(encoded string) (parsedPasswordHash, bool) {
	parts := strings.Split(encoded, "$")
	if len(parts) != 4 || parts[0] != legacyPasswordHashVersion || parts[1] != strconv.Itoa(legacyPasswordIterations) {
		return parsedPasswordHash{}, false
	}
	salt, ok := decodeBoundedPasswordField(parts[2], passwordSaltBytes, passwordSaltBytes)
	if !ok {
		return parsedPasswordHash{}, false
	}
	expected, ok := decodeBoundedPasswordField(parts[3], passwordKeyBytes, passwordKeyBytes)
	if !ok {
		return parsedPasswordHash{}, false
	}
	return parsedPasswordHash{
		algorithm: legacyPasswordHashVersion, iterations: legacyPasswordIterations,
		salt: salt, expected: expected,
	}, true
}

func decodeBoundedPasswordField(encoded string, minimum, maximum int) ([]byte, bool) {
	if !isRawStandardBase64(encoded) || passwordBase64.DecodedLen(len(encoded)) > maximum {
		return nil, false
	}
	decoded, err := passwordBase64.DecodeString(encoded)
	if err != nil || len(decoded) < minimum || len(decoded) > maximum {
		return nil, false
	}
	return decoded, true
}

func isRawStandardBase64(value string) bool {
	if value == "" {
		return false
	}
	for _, character := range value {
		if (character < 'A' || character > 'Z') &&
			(character < 'a' || character > 'z') &&
			(character < '0' || character > '9') && character != '+' && character != '/' {
			return false
		}
	}
	return true
}

func passwordHashNeedsRehash(parsed parsedPasswordHash) bool {
	return parsed.algorithm != argon2PasswordHashVersion ||
		parsed.memory != argon2MemoryKiB || parsed.iterations != argon2Iterations || parsed.lanes != argon2Lanes ||
		len(parsed.salt) != passwordSaltBytes || len(parsed.expected) != passwordKeyBytes
}

func HashPassword(password string) (string, error) {
	return NewPasswordHasher().Hash(password)
}

func VerifyPassword(hash, password string) bool {
	valid, _ := NewPasswordHasher().Verify(hash, password)
	return valid
}

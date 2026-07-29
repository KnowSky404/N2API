package secret

import (
	"bytes"
	"crypto/pbkdf2"
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"fmt"
	"strings"
	"sync"
	"testing"

	"golang.org/x/crypto/argon2"
)

func TestPasswordHasherCreatesCurrentArgon2idHash(t *testing.T) {
	hasher := NewPasswordHasher()
	hash, err := hasher.Hash("owner-password")
	if err != nil {
		t.Fatalf("Hash returned error: %v", err)
	}
	if !strings.HasPrefix(hash, "$argon2id$v=19$m=65536,t=2,p=1$") {
		t.Fatalf("Hash returned unexpected PHC envelope %q", hash)
	}
	valid, needsRehash := hasher.Verify(hash, "owner-password")
	if !valid || needsRehash {
		t.Fatalf("Verify original password = valid:%t needsRehash:%t, want true/false", valid, needsRehash)
	}
	if valid, _ := hasher.Verify(hash, "wrong-password"); valid {
		t.Fatal("Verify returned true for wrong password")
	}
}

func TestPasswordHasherPreservesExactPasswordBytes(t *testing.T) {
	hasher := NewPasswordHasher()
	password := "  密碼 password  "
	hash, err := hasher.Hash(password)
	if err != nil {
		t.Fatalf("Hash returned error: %v", err)
	}
	if valid, _ := hasher.Verify(hash, password); !valid {
		t.Fatal("Verify rejected exact password bytes")
	}
	for _, changed := range []string{strings.TrimSpace(password), "  密碼 password ", " 密碼 password  "} {
		if valid, _ := hasher.Verify(hash, changed); valid {
			t.Fatalf("Verify accepted changed password %q", changed)
		}
	}
}

func TestPasswordHasherVerifiesLegacyPBKDF2AndRequestsRehash(t *testing.T) {
	legacy := legacyPasswordHashForTest(t, "owner-password", []byte("1234567890abcdef"), legacyPasswordIterations, passwordKeyBytes)
	valid, needsRehash := NewPasswordHasher().Verify(legacy, "owner-password")
	if !valid || !needsRehash {
		t.Fatalf("Verify legacy password = valid:%t needsRehash:%t, want true/true", valid, needsRehash)
	}
	if valid, needsRehash := NewPasswordHasher().Verify(legacy, "wrong-password"); valid || needsRehash {
		t.Fatalf("Verify wrong legacy password = valid:%t needsRehash:%t, want false/false", valid, needsRehash)
	}
}

func TestPasswordHasherRequestsRehashForBoundedNonDefaultArgon2id(t *testing.T) {
	encoded := argon2PasswordHashForTest("owner-password", []byte("12345678abcdefgh"), 8, 1, 1, passwordKeyBytes)
	valid, needsRehash := NewPasswordHasher().Verify(encoded, "owner-password")
	if !valid || !needsRehash {
		t.Fatalf("Verify non-default Argon2id = valid:%t needsRehash:%t, want true/true", valid, needsRehash)
	}
}

func TestPasswordHasherRejectsMalformedOrExcessiveHashesBeforeWork(t *testing.T) {
	validSalt := passwordBase64.EncodeToString([]byte("1234567890abcdef"))
	validKey := passwordBase64.EncodeToString(bytes.Repeat([]byte{1}, passwordKeyBytes))
	cases := map[string]string{
		"unknown algorithm":       "$argon2i$v=19$m=65536,t=2,p=1$" + validSalt + "$" + validKey,
		"unknown version":         "$argon2id$v=18$m=65536,t=2,p=1$" + validSalt + "$" + validKey,
		"missing parameter":       "$argon2id$v=19$m=65536,t=2$" + validSalt + "$" + validKey,
		"duplicate parameter":     "$argon2id$v=19$m=65536,t=2,t=1$" + validSalt + "$" + validKey,
		"noncanonical parameter":  "$argon2id$v=19$m=065536,t=2,p=1$" + validSalt + "$" + validKey,
		"zero parameter":          "$argon2id$v=19$m=0,t=2,p=1$" + validSalt + "$" + validKey,
		"memory above limit":      "$argon2id$v=19$m=131073,t=2,p=1$" + validSalt + "$" + validKey,
		"iterations above limit":  "$argon2id$v=19$m=65536,t=6,p=1$" + validSalt + "$" + validKey,
		"lanes above limit":       "$argon2id$v=19$m=65536,t=2,p=5$" + validSalt + "$" + validKey,
		"oversized salt":          "$argon2id$v=19$m=65536,t=2,p=1$" + passwordBase64.EncodeToString(bytes.Repeat([]byte{1}, maxPasswordSaltBytes+1)) + "$" + validKey,
		"oversized output":        "$argon2id$v=19$m=65536,t=2,p=1$" + validSalt + "$" + passwordBase64.EncodeToString(bytes.Repeat([]byte{1}, maxPasswordKeyBytes+1)),
		"newline in salt":         "$argon2id$v=19$m=65536,t=2,p=1$" + validSalt[:8] + "\n" + validSalt[8:] + "$" + validKey,
		"padded output":           "$argon2id$v=19$m=65536,t=2,p=1$" + validSalt + "$" + validKey + "=",
		"excessive encoded hash":  strings.Repeat("x", maxEncodedPasswordHashBytes+1),
		"legacy wrong iterations": legacyPasswordHashForTest(t, "owner-password", []byte("1234567890abcdef"), legacyPasswordIterations-1, passwordKeyBytes),
		"legacy short salt":       legacyPasswordHashForTest(t, "owner-password", []byte("short-salt-1234"), legacyPasswordIterations, passwordKeyBytes),
		"legacy short output":     legacyPasswordHashForTest(t, "owner-password", []byte("1234567890abcdef"), legacyPasswordIterations, passwordKeyBytes-1),
	}
	for name, encoded := range cases {
		t.Run(name, func(t *testing.T) {
			if valid, needsRehash := NewPasswordHasher().Verify(encoded, "owner-password"); valid || needsRehash {
				t.Fatalf("Verify = valid:%t needsRehash:%t, want false/false", valid, needsRehash)
			}
		})
	}
}

func TestPasswordHasherBoundsConcurrentDerivation(t *testing.T) {
	started := make(chan struct{}, 2)
	release := make(chan struct{})
	derived := bytes.Repeat([]byte{7}, passwordKeyBytes)
	hasher := &argon2idPasswordHasher{
		random:    bytes.NewReader(bytes.Repeat([]byte{1}, passwordSaltBytes)),
		workSlots: make(chan struct{}, 1),
		deriveKey: func(_, _ []byte, _, _ uint32, _ uint8, _ uint32) []byte {
			started <- struct{}{}
			<-release
			return derived
		},
	}
	encoded := fmt.Sprintf(
		"$argon2id$v=19$m=8,t=1,p=1$%s$%s",
		passwordBase64.EncodeToString([]byte("1234567890abcdef")),
		passwordBase64.EncodeToString(derived),
	)

	var wait sync.WaitGroup
	wait.Add(2)
	for range 2 {
		go func() {
			defer wait.Done()
			valid, _ := hasher.Verify(encoded, "password")
			if !valid {
				t.Error("Verify returned false")
			}
		}()
	}
	<-started
	if len(started) != 0 {
		t.Fatal("more than one derivation entered the bounded section")
	}
	release <- struct{}{}
	<-started
	release <- struct{}{}
	wait.Wait()
}

func TestPasswordHasherReportsSaltGenerationFailure(t *testing.T) {
	want := errors.New("random unavailable")
	hasher := &argon2idPasswordHasher{
		random:    errorReader{err: want},
		workSlots: make(chan struct{}, 1),
		deriveKey: argon2.IDKey,
	}
	if _, err := hasher.Hash("password"); !errors.Is(err, want) {
		t.Fatalf("Hash error = %v, want wrapped random error", err)
	}
}

func TestHashPasswordCompatibilityWrappers(t *testing.T) {
	hash, err := HashPassword("owner-password")
	if err != nil {
		t.Fatalf("HashPassword returned error: %v", err)
	}
	if !VerifyPassword(hash, "owner-password") || VerifyPassword(hash, "wrong-password") {
		t.Fatal("password compatibility wrappers returned unexpected verification result")
	}
}

func BenchmarkPasswordHasherArgon2idSmallVPS(b *testing.B) {
	hasher := NewPasswordHasher()
	b.ReportAllocs()
	b.ReportMetric(argon2MemoryKiB/1024, "MiB-memory/op")
	for range b.N {
		if _, err := hasher.Hash("benchmark-owner-password"); err != nil {
			b.Fatal(err)
		}
	}
}

type errorReader struct {
	err error
}

func (r errorReader) Read([]byte) (int, error) {
	return 0, r.err
}

func legacyPasswordHashForTest(t *testing.T, password string, salt []byte, iterations, keyBytes int) string {
	t.Helper()
	key, err := pbkdf2.Key(sha256.New, password, salt, iterations, keyBytes)
	if err != nil {
		t.Fatalf("pbkdf2.Key returned error: %v", err)
	}
	return fmt.Sprintf(
		"%s$%d$%s$%s",
		legacyPasswordHashVersion,
		iterations,
		base64.RawStdEncoding.EncodeToString(salt),
		base64.RawStdEncoding.EncodeToString(key),
	)
}

func argon2PasswordHashForTest(password string, salt []byte, memory, iterations uint32, lanes uint8, keyBytes int) string {
	key := argon2.IDKey([]byte(password), salt, iterations, memory, lanes, uint32(keyBytes))
	return fmt.Sprintf(
		"$argon2id$v=19$m=%d,t=%d,p=%d$%s$%s",
		memory,
		iterations,
		lanes,
		passwordBase64.EncodeToString(salt),
		passwordBase64.EncodeToString(key),
	)
}

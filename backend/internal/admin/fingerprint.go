package admin

import (
	"encoding/json"
	"net/http"
	"strings"
	"time"
)

// FingerprintProfile defines a TLS/UA fingerprint configuration.
type FingerprintProfile struct {
	ID             int64             `json:"id"`
	SystemKey      string            `json:"systemKey"`
	Name           string            `json:"name"`
	Description    string            `json:"description"`
	UserAgent      string            `json:"userAgent"`
	TLSFingerprint string            `json:"tlsFingerprint"`
	Headers        map[string]string `json:"headers"`
	Enabled        bool              `json:"enabled"`
	CreatedAt      time.Time         `json:"createdAt"`
	UpdatedAt      time.Time         `json:"updatedAt"`
}

// FingerprintProfileInput is the write payload.
type FingerprintProfileInput struct {
	Name           string            `json:"name"`
	Description    string            `json:"description"`
	UserAgent      string            `json:"userAgent"`
	TLSFingerprint string            `json:"tlsFingerprint"`
	Headers        map[string]string `json:"headers"`
	Enabled        bool              `json:"enabled"`
}

// Normalize trims and defaults the input.
func (input *FingerprintProfileInput) Normalize() error {
	input.Name = strings.TrimSpace(input.Name)
	input.Description = strings.TrimSpace(input.Description)
	input.UserAgent = strings.TrimSpace(input.UserAgent)
	tlsFingerprint := strings.TrimSpace(input.TLSFingerprint)
	input.TLSFingerprint = normalizeFingerprintTLS(tlsFingerprint)
	if tlsFingerprint != "" && input.TLSFingerprint == "" {
		return ErrInvalidInput
	}
	if input.Headers == nil {
		input.Headers = map[string]string{}
		return nil
	}
	headers := make(map[string]string, len(input.Headers))
	for key, value := range input.Headers {
		key = strings.TrimSpace(key)
		if !validFingerprintHeaderName(key) {
			return ErrInvalidInput
		}
		value = strings.TrimSpace(value)
		if !validFingerprintHeaderValue(value) {
			return ErrInvalidInput
		}
		headers[http.CanonicalHeaderKey(key)] = value
	}
	input.Headers = headers
	return nil
}

func normalizeFingerprintTLS(value string) string {
	value = strings.TrimSpace(strings.ToLower(value))
	if value == "" {
		return ""
	}
	value = strings.ReplaceAll(value, "_", "-")
	value = strings.ReplaceAll(value, " ", "-")
	switch value {
	case "chrome", "hellochrome", "hellochrome-auto", "chrome-auto":
		return "chrome"
	case "firefox", "hellofirefox", "hellofirefox-auto", "firefox-auto":
		return "firefox"
	case "safari", "hellosafari", "hellosafari-auto", "safari-auto":
		return "safari"
	case "ios", "helloios", "helloios-auto", "ios-auto":
		return "ios"
	case "android", "android-okhttp", "helloandroid", "helloandroid-11-okhttp":
		return "android"
	case "edge", "helloedge", "helloedge-auto", "edge-auto":
		return "edge"
	case "random", "randomized", "hellorandomized":
		return "randomized"
	case "randomized-alpn", "hellorandomizedalpn":
		return "randomized-alpn"
	case "randomized-no-alpn", "hellorandomizednoalpn":
		return "randomized-no-alpn"
	case "go", "golang", "hello-golang", "hellogolang":
		return "golang"
	default:
		return ""
	}
}

func validFingerprintHeaderName(value string) bool {
	if value == "" {
		return false
	}
	for _, r := range value {
		if !isHTTPTokenRune(r) {
			return false
		}
	}
	return true
}

func isHTTPTokenRune(r rune) bool {
	if r >= '0' && r <= '9' {
		return true
	}
	if r >= 'a' && r <= 'z' {
		return true
	}
	if r >= 'A' && r <= 'Z' {
		return true
	}
	switch r {
	case '!', '#', '$', '%', '&', '\'', '*', '+', '-', '.', '^', '_', '`', '|', '~':
		return true
	default:
		return false
	}
}

func validFingerprintHeaderValue(value string) bool {
	for _, r := range value {
		if r == '\t' {
			continue
		}
		if r < 0x20 || r == 0x7f {
			return false
		}
	}
	return true
}

// HeadersJSON returns headers as JSON bytes for storage.
func (f *FingerprintProfile) HeadersJSON() ([]byte, error) {
	if f.Headers == nil {
		return []byte(`{}`), nil
	}
	return json.Marshal(f.Headers)
}

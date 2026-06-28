package admin

import (
	"encoding/json"
	"time"
)

// FingerprintProfile defines a TLS/UA fingerprint configuration.
type FingerprintProfile struct {
	ID          int64             `json:"id"`
	Name        string            `json:"name"`
	Description string            `json:"description"`
	UserAgent   string            `json:"userAgent"`
	TLSFingerprint string         `json:"tlsFingerprint"`
	Headers     map[string]string `json:"headers"`
	Enabled     bool              `json:"enabled"`
	CreatedAt   time.Time         `json:"createdAt"`
	UpdatedAt   time.Time         `json:"updatedAt"`
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
	if input.Headers == nil {
		input.Headers = map[string]string{}
	}
	return nil
}

// HeadersJSON returns headers as JSON bytes for storage.
func (f *FingerprintProfile) HeadersJSON() ([]byte, error) {
	if f.Headers == nil {
		return []byte(`{}`), nil
	}
	return json.Marshal(f.Headers)
}

package requestlog

import (
	"strings"
	"time"
	"unicode"
	"unicode/utf8"
)

const (
	MaxRequestAttemptTimelineEntries   = 16
	MaxRequestAttemptTimelineJSONBytes = 64 << 10
	MaxRequestDiagnosticTextBytes      = 128
)

// RequestAttempt is a bounded, redacted description of one gateway decision
// or upstream operation. It intentionally contains no request or response
// payload data.
type RequestAttempt struct {
	Order             int        `json:"order"`
	Type              string     `json:"type"`
	AccountID         int64      `json:"accountId,omitempty"`
	AccountType       string     `json:"accountType,omitempty"`
	AccountName       string     `json:"accountName,omitempty"`
	PoolID            int64      `json:"poolId,omitempty"`
	PoolName          string     `json:"poolName,omitempty"`
	StartedAt         time.Time  `json:"startedAt"`
	EndedAt           *time.Time `json:"endedAt,omitempty"`
	DurationMS        *int       `json:"durationMs,omitempty"`
	HTTPStatus        int        `json:"httpStatus,omitempty"`
	Error             string     `json:"error,omitempty"`
	FallbackReason    string     `json:"fallbackReason,omitempty"`
	UpstreamRequestID string     `json:"upstreamRequestId,omitempty"`
}

// ResponseTiming contains phase durations relative to the selected upstream
// request. A nil value means the phase was not observed.
type ResponseTiming struct {
	HeaderWaitMS        *int `json:"headerWaitMs"`
	FirstUsefulOutputMS *int `json:"firstUsefulOutputMs"`
	StreamFinishMS      *int `json:"streamFinishMs"`
}

// BoundedText removes control characters and truncates to a byte limit before
// diagnostic data is persisted or rendered.
func BoundedText(value string, maxBytes int) string {
	if maxBytes <= 0 {
		return ""
	}
	value = strings.ToValidUTF8(strings.TrimSpace(value), "")
	var builder strings.Builder
	for _, character := range value {
		if unicode.IsControl(character) {
			continue
		}
		builder.WriteRune(character)
	}
	value = strings.TrimSpace(builder.String())
	if len(value) <= maxBytes {
		return value
	}
	for len(value) > maxBytes {
		_, size := utf8.DecodeLastRuneInString(value)
		if size == 0 {
			break
		}
		value = value[:len(value)-size]
	}
	return strings.TrimSpace(value)
}

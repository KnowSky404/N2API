package gateway

import (
	"bufio"
	"bytes"
	"errors"
	"fmt"
	"io"
	"strings"
)

const (
	// DefaultSSEParserMaxLineBytes includes the line ending when one is present.
	DefaultSSEParserMaxLineBytes = 64 * 1024
	// DefaultSSEParserMaxEventBytes includes all non-blank lines in one event.
	DefaultSSEParserMaxEventBytes = 1 * 1024 * 1024
	// DefaultSSEParserMaxDataBytes includes data bytes and the separators between data lines.
	DefaultSSEParserMaxDataBytes = 8 * 1024 * 1024
)

var (
	// ErrSSELineTooLarge indicates that one physical SSE line exceeded its limit.
	ErrSSELineTooLarge = errors.New("SSE line exceeds limit")
	// ErrSSEEventTooLarge indicates that the current event exceeded its limit.
	ErrSSEEventTooLarge = errors.New("SSE event exceeds limit")
	// ErrSSEDataTooLarge indicates that the concatenated data fields exceeded their limit.
	ErrSSEDataTooLarge = errors.New("SSE data exceeds limit")
	// ErrSSEUnexpectedEOF indicates that EOF arrived before the current event ended.
	ErrSSEUnexpectedEOF = errors.New("unexpected EOF in SSE event")
)

// SSEParserLimits bounds memory consumed while reading an SSE stream. Values less
// than or equal to zero use the corresponding default.
type SSEParserLimits struct {
	MaxLineBytes  int
	MaxEventBytes int
	MaxDataBytes  int
}

// SSEEvent is one event dispatched at an SSE blank-line boundary.
//
// HasData distinguishes an absent data field from a present, empty data field.
// Event and Data are copied out of the parser and remain stable after Next
// advances to the following event.
type SSEEvent struct {
	Event   string
	Data    string
	HasData bool
}

// SSEParser incrementally parses Server-Sent Events from an io.Reader.
//
// It accepts LF and CRLF line endings, preserves the SSE event field, joins
// multiple data fields with a single LF, and ignores comment lines. A blank
// line dispatches an event, including an event with no data. EOF without a
// blank-line boundary is reported as ErrSSEUnexpectedEOF rather than as a
// successful terminal event.
type SSEParser struct {
	reader   *bufio.Reader
	limits   SSEParserLimits
	event    strings.Builder
	data     strings.Builder
	eventN   int
	dataN    int
	hasField bool
	hasData  bool
}

// NewSSEParser creates an incremental SSE parser with bounded defaults for any
// non-positive limit.
func NewSSEParser(reader io.Reader, limits SSEParserLimits) *SSEParser {
	if limits.MaxLineBytes <= 0 {
		limits.MaxLineBytes = DefaultSSEParserMaxLineBytes
	}
	if limits.MaxEventBytes <= 0 {
		limits.MaxEventBytes = DefaultSSEParserMaxEventBytes
	}
	if limits.MaxDataBytes <= 0 {
		limits.MaxDataBytes = DefaultSSEParserMaxDataBytes
	}
	return &SSEParser{
		reader: bufio.NewReader(reader),
		limits: limits,
	}
}

// Next returns the next event or an error. A clean EOF after an event boundary
// is returned as io.EOF. A non-empty partial event at EOF is returned as
// ErrSSEUnexpectedEOF.
func (p *SSEParser) Next() (SSEEvent, error) {
	if p == nil || p.reader == nil {
		return SSEEvent{}, io.EOF
	}

	for {
		line, err := p.readLine()
		if err != nil {
			if errors.Is(err, io.EOF) && len(line) == 0 {
				if p.hasPendingFields() {
					p.resetEvent()
					return SSEEvent{}, ErrSSEUnexpectedEOF
				}
				return SSEEvent{}, io.EOF
			}
			if errors.Is(err, io.EOF) {
				p.resetEvent()
				return SSEEvent{}, ErrSSEUnexpectedEOF
			}
			return SSEEvent{}, err
		}

		line = trimSSELineEnding(line)
		if len(line) == 0 {
			return p.dispatchEvent(), nil
		}

		if err := p.accountEventLine(len(line)); err != nil {
			p.resetEvent()
			return SSEEvent{}, err
		}
		if line[0] == ':' {
			continue
		}
		p.hasField = true

		field, value := splitSSEField(line)
		switch field {
		case "event":
			p.event.Reset()
			_, _ = p.event.WriteString(value)
		case "data":
			if err := p.appendData(value); err != nil {
				p.resetEvent()
				return SSEEvent{}, err
			}
		}
	}
}

func (p *SSEParser) readLine() ([]byte, error) {
	var line []byte
	for {
		chunk, err := p.reader.ReadSlice('\n')
		if len(chunk) > 0 {
			if len(line) > p.limits.MaxLineBytes-len(chunk) {
				return nil, fmt.Errorf("%w: maximum %d bytes", ErrSSELineTooLarge, p.limits.MaxLineBytes)
			}
			line = append(line, chunk...)
		}
		if err == nil {
			return line, nil
		}
		if errors.Is(err, bufio.ErrBufferFull) {
			continue
		}
		if errors.Is(err, io.EOF) {
			return line, io.EOF
		}
		return nil, err
	}
}

func (p *SSEParser) accountEventLine(lineBytes int) error {
	if lineBytes > p.limits.MaxEventBytes-p.eventN {
		return fmt.Errorf("%w: maximum %d bytes", ErrSSEEventTooLarge, p.limits.MaxEventBytes)
	}
	p.eventN += lineBytes
	return nil
}

func (p *SSEParser) appendData(value string) error {
	separatorBytes := 0
	if p.hasData {
		separatorBytes = 1
	}
	if separatorBytes > p.limits.MaxDataBytes-p.dataN || len(value) > p.limits.MaxDataBytes-p.dataN-separatorBytes {
		return fmt.Errorf("%w: maximum %d bytes", ErrSSEDataTooLarge, p.limits.MaxDataBytes)
	}
	if p.hasData {
		_ = p.data.WriteByte('\n')
	}
	_, _ = p.data.WriteString(value)
	p.dataN += separatorBytes + len(value)
	p.hasData = true
	return nil
}

func (p *SSEParser) dispatchEvent() SSEEvent {
	event := SSEEvent{
		Event:   strings.Clone(p.event.String()),
		Data:    strings.Clone(p.data.String()),
		HasData: p.hasData,
	}
	p.resetEvent()
	return event
}

func (p *SSEParser) hasPendingFields() bool {
	return p.hasField
}

func (p *SSEParser) resetEvent() {
	p.event.Reset()
	p.data.Reset()
	p.eventN = 0
	p.dataN = 0
	p.hasField = false
	p.hasData = false
}

func trimSSELineEnding(line []byte) []byte {
	if len(line) > 0 && line[len(line)-1] == '\n' {
		line = line[:len(line)-1]
	}
	if len(line) > 0 && line[len(line)-1] == '\r' {
		line = line[:len(line)-1]
	}
	return line
}

func splitSSEField(line []byte) (string, string) {
	if colon := bytes.IndexByte(line, ':'); colon >= 0 {
		value := line[colon+1:]
		if len(value) > 0 && value[0] == ' ' {
			value = value[1:]
		}
		return string(line[:colon]), string(value)
	}
	return string(line), ""
}

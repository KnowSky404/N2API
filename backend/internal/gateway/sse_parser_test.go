package gateway

import (
	"errors"
	"io"
	"strings"
	"testing"
)

func TestSSEParserHandlesChunkBoundariesAndMultilineData(t *testing.T) {
	parser := NewSSEParser(&sseChunkReader{chunks: []string{
		"eve", "nt: message\n", "data: hello\n", "data: wor", "ld\n", "\n",
	}}, SSEParserLimits{})

	event, err := parser.Next()
	if err != nil {
		t.Fatalf("Next() error = %v", err)
	}
	if event.Event != "message" || event.Data != "hello\nworld" || !event.HasData {
		t.Fatalf("event = %+v, want message with joined data", event)
	}
	if _, err := parser.Next(); !errors.Is(err, io.EOF) {
		t.Fatalf("Next() after complete event error = %v, want io.EOF", err)
	}
}

func TestSSEParserHandlesCRLFCommentsAndEmptyEvents(t *testing.T) {
	parser := NewSSEParser(strings.NewReader(
		": heartbeat\r\n"+
			"event: empty\r\n"+
			"\r\n"+
			"\r\n"+
			"event: payload\r\n"+
			"data:\r\n"+
			"\r\n",
	), SSEParserLimits{})

	first, err := parser.Next()
	if err != nil {
		t.Fatalf("first Next() error = %v", err)
	}
	if first.Event != "empty" || first.Data != "" || first.HasData {
		t.Fatalf("first event = %+v, want empty event field without data", first)
	}

	second, err := parser.Next()
	if err != nil {
		t.Fatalf("second Next() error = %v", err)
	}
	if second.Event != "" || second.Data != "" || second.HasData {
		t.Fatalf("second event = %+v, want blank empty event", second)
	}

	third, err := parser.Next()
	if err != nil {
		t.Fatalf("third Next() error = %v", err)
	}
	if third.Event != "payload" || third.Data != "" || !third.HasData {
		t.Fatalf("third event = %+v, want present empty data", third)
	}
}

func TestSSEParserIgnoresCommentOnlyEOF(t *testing.T) {
	parser := NewSSEParser(strings.NewReader(": heartbeat\n"), SSEParserLimits{})
	if _, err := parser.Next(); !errors.Is(err, io.EOF) {
		t.Fatalf("Next() after comment-only EOF error = %v, want io.EOF", err)
	}
}

func TestSSEParserEnforcesLineEventAndDataLimits(t *testing.T) {
	tests := []struct {
		name  string
		input string
		limit SSEParserLimits
		want  error
	}{
		{
			name:  "line",
			input: "data:123\n\n",
			limit: SSEParserLimits{MaxLineBytes: 8, MaxEventBytes: 100, MaxDataBytes: 100},
			want:  ErrSSELineTooLarge,
		},
		{
			name:  "event",
			input: "event:x\ndata:y\n\n",
			limit: SSEParserLimits{MaxLineBytes: 100, MaxEventBytes: 10, MaxDataBytes: 100},
			want:  ErrSSEEventTooLarge,
		},
		{
			name:  "cumulative data",
			input: "data:abc\ndata:de\n\n",
			limit: SSEParserLimits{MaxLineBytes: 100, MaxEventBytes: 100, MaxDataBytes: 5},
			want:  ErrSSEDataTooLarge,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			parser := NewSSEParser(strings.NewReader(test.input), test.limit)
			_, err := parser.Next()
			if !errors.Is(err, test.want) {
				t.Fatalf("Next() error = %v, want errors.Is(..., %v)", err, test.want)
			}
		})
	}
}

func TestSSEParserReportsUnexpectedEOFForPartialEvent(t *testing.T) {
	for _, input := range []string{"data:partial\n", "event:partial"} {
		parser := NewSSEParser(strings.NewReader(input), SSEParserLimits{})
		if _, err := parser.Next(); !errors.Is(err, ErrSSEUnexpectedEOF) {
			t.Errorf("input %q: Next() error = %v, want ErrSSEUnexpectedEOF", input, err)
		}
	}
}

func TestSSEParserDoesNotTreatEOFAsTerminalSuccess(t *testing.T) {
	parser := NewSSEParser(strings.NewReader("event:response.completed\ndata:{\"ok\":true}"), SSEParserLimits{})
	if _, err := parser.Next(); !errors.Is(err, ErrSSEUnexpectedEOF) {
		t.Fatalf("Next() error = %v, want partial-event error", err)
	}
}

type sseChunkReader struct {
	chunks []string
	index  int
}

func (r *sseChunkReader) Read(p []byte) (int, error) {
	if r.index >= len(r.chunks) {
		return 0, io.EOF
	}
	chunk := r.chunks[r.index]
	r.index++
	return copy(p, chunk), nil
}

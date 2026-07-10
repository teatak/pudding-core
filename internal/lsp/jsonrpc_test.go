package lsp

import (
	"bytes"
	"errors"
	"io"
	"strings"
	"testing"
)

type chunkReader struct {
	reader io.Reader
	max    int
}

func (r chunkReader) Read(p []byte) (int, error) {
	if len(p) > r.max {
		p = p[:r.max]
	}
	return r.reader.Read(p)
}

type chunkWriter struct {
	buffer bytes.Buffer
	max    int
}

func (w *chunkWriter) Write(p []byte) (int, error) {
	if len(p) > w.max {
		p = p[:w.max]
	}
	return w.buffer.Write(p)
}

func TestFramePartialReadWrite(t *testing.T) {
	payload := []byte(`{"jsonrpc":"2.0","id":1,"result":{"ok":true}}`)
	var writer chunkWriter
	writer.max = 3
	if err := writeFrame(&writer, payload, 1024); err != nil {
		t.Fatal(err)
	}
	reader := newFrameReader(chunkReader{reader: bytes.NewReader(writer.buffer.Bytes()), max: 2}, 1024, 1024)
	got, err := reader.Read()
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got, payload) {
		t.Fatalf("payload = %q, want %q", got, payload)
	}
}

func TestFrameRejectsInvalidAndOversizedPayload(t *testing.T) {
	tests := []struct {
		name  string
		frame string
		max   int
	}{
		{name: "missing length", frame: "X-Test: 1\r\n\r\n{}", max: 64},
		{name: "duplicate length", frame: "Content-Length: 2\r\nContent-Length: 2\r\n\r\n{}", max: 64},
		{name: "oversized", frame: "Content-Length: 10\r\n\r\n0123456789", max: 5},
		{name: "not object", frame: "Content-Length: 2\r\n\r\n[]", max: 64},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			_, err := newFrameReader(strings.NewReader(test.frame), test.max, 1024).Read()
			if !errors.Is(err, errProtocol) {
				t.Fatalf("error = %v, want protocol error", err)
			}
		})
	}
}

func TestFrameRejectsHeaderWithoutBoundedNewline(t *testing.T) {
	frame := strings.Repeat("X", 2048)
	_, err := newFrameReader(strings.NewReader(frame), 4096, 128).Read()
	if !errors.Is(err, errProtocol) {
		t.Fatalf("error = %v, want protocol error", err)
	}
}

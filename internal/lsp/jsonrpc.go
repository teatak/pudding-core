package lsp

import (
	"bufio"
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strconv"
	"strings"
)

var errProtocol = errors.New("lsp protocol error")

type frameReader struct {
	r               *bufio.Reader
	maxMessageBytes int
	maxHeaderBytes  int
}

func newFrameReader(r io.Reader, maxMessageBytes, maxHeaderBytes int) *frameReader {
	if maxMessageBytes <= 0 {
		maxMessageBytes = DefaultMaxMessageBytes
	}
	if maxHeaderBytes <= 0 {
		maxHeaderBytes = DefaultMaxHeaderBytes
	}
	return &frameReader{
		r:               bufio.NewReader(r),
		maxMessageBytes: maxMessageBytes,
		maxHeaderBytes:  maxHeaderBytes,
	}
}

func (r *frameReader) Read() ([]byte, error) {
	headerBytes := 0
	contentLength := -1
	for {
		line, size, err := r.readHeaderLine(r.maxHeaderBytes - headerBytes)
		if err != nil {
			return nil, err
		}
		headerBytes += size
		line = strings.TrimSuffix(line, "\n")
		line = strings.TrimSuffix(line, "\r")
		if line == "" {
			break
		}
		name, value, ok := strings.Cut(line, ":")
		if !ok {
			return nil, fmt.Errorf("%w: invalid header", errProtocol)
		}
		if !strings.EqualFold(strings.TrimSpace(name), "Content-Length") {
			continue
		}
		if contentLength >= 0 {
			return nil, fmt.Errorf("%w: duplicate Content-Length", errProtocol)
		}
		n, err := strconv.Atoi(strings.TrimSpace(value))
		if err != nil || n <= 0 {
			return nil, fmt.Errorf("%w: invalid Content-Length", errProtocol)
		}
		if n > r.maxMessageBytes {
			return nil, fmt.Errorf("%w: message exceeds %d bytes", errProtocol, r.maxMessageBytes)
		}
		contentLength = n
	}
	if contentLength < 0 {
		return nil, fmt.Errorf("%w: missing Content-Length", errProtocol)
	}
	payload := make([]byte, contentLength)
	if _, err := io.ReadFull(r.r, payload); err != nil {
		return nil, err
	}
	trimmed := bytes.TrimSpace(payload)
	if len(trimmed) == 0 || trimmed[0] != '{' || !json.Valid(trimmed) {
		return nil, fmt.Errorf("%w: payload must be a JSON object", errProtocol)
	}
	return payload, nil
}

func (r *frameReader) readHeaderLine(remaining int) (string, int, error) {
	if remaining <= 0 {
		return "", 0, fmt.Errorf("%w: headers exceed %d bytes", errProtocol, r.maxHeaderBytes)
	}
	line := make([]byte, 0, min(remaining, 4096))
	for {
		fragment, err := r.r.ReadSlice('\n')
		if len(line)+len(fragment) > remaining {
			return "", 0, fmt.Errorf("%w: headers exceed %d bytes", errProtocol, r.maxHeaderBytes)
		}
		line = append(line, fragment...)
		if err == nil {
			return string(line), len(line), nil
		}
		if errors.Is(err, bufio.ErrBufferFull) {
			continue
		}
		return "", 0, err
	}
}

func writeFrame(w io.Writer, payload []byte, maxMessageBytes int) error {
	if maxMessageBytes <= 0 {
		maxMessageBytes = DefaultMaxMessageBytes
	}
	trimmed := bytes.TrimSpace(payload)
	if len(trimmed) == 0 || trimmed[0] != '{' || !json.Valid(trimmed) {
		return fmt.Errorf("%w: payload must be a JSON object", errProtocol)
	}
	if len(payload) > maxMessageBytes {
		return fmt.Errorf("%w: message exceeds %d bytes", errProtocol, maxMessageBytes)
	}
	header := []byte(fmt.Sprintf("Content-Length: %d\r\n\r\n", len(payload)))
	if err := writeAll(w, header); err != nil {
		return err
	}
	return writeAll(w, payload)
}

func writeAll(w io.Writer, data []byte) error {
	for len(data) > 0 {
		n, err := w.Write(data)
		if err != nil {
			return err
		}
		if n == 0 {
			return io.ErrShortWrite
		}
		data = data[n:]
	}
	return nil
}

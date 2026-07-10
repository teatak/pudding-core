package lsp

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

const defaultStderrBytes = 64 << 10

type processOptions struct {
	initializeTimeout time.Duration
	shutdownTimeout   time.Duration
	maxMessageBytes   int
	maxHeaderBytes    int
	stderrBytes       int
}

// Process is one initialized LSP subprocess. It is safe for concurrent use.
type Process struct {
	spec             ServerSpec
	cmd              *exec.Cmd
	stdin            io.WriteCloser
	stdout           io.ReadCloser
	reader           *frameReader
	maxMessageBytes  int
	shutdownTimeout  time.Duration
	rootURI          string
	positionEncoding string
	diagnostics      *diagnosticsCache
	stderr           *byteRing
	documentsMu      sync.Mutex
	documents        map[string]documentState

	writeMu sync.Mutex
	mu      sync.Mutex
	pending map[string]chan wireMessage
	err     error
	done    chan struct{}
	nextID  atomic.Int64
	endOnce sync.Once
	closeMu sync.Mutex
	closing bool
}

func startProcess(ctx context.Context, spec ServerSpec, opts processOptions) (*Process, error) {
	cmd := exec.Command(spec.Command, spec.Args...)
	cmd.Dir = spec.Dir
	if cmd.Dir == "" {
		cmd.Dir = spec.Key.LanguageRoot
	}
	if spec.Env == nil {
		cmd.Env = os.Environ()
	} else {
		cmd.Env = append([]string(nil), spec.Env...)
	}
	configureProcess(cmd)

	stdin, err := cmd.StdinPipe()
	if err != nil {
		return nil, fmt.Errorf("lsp stdin: %w", err)
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		_ = stdin.Close()
		return nil, fmt.Errorf("lsp stdout: %w", err)
	}
	stderr, err := cmd.StderrPipe()
	if err != nil {
		_ = stdin.Close()
		_ = stdout.Close()
		return nil, fmt.Errorf("lsp stderr: %w", err)
	}

	p := &Process{
		spec:             spec,
		cmd:              cmd,
		stdin:            stdin,
		stdout:           stdout,
		reader:           newFrameReader(stdout, opts.maxMessageBytes, opts.maxHeaderBytes),
		maxMessageBytes:  opts.maxMessageBytes,
		shutdownTimeout:  opts.shutdownTimeout,
		rootURI:          fileURI(spec.Key.LanguageRoot),
		positionEncoding: "utf-16",
		diagnostics:      newDiagnosticsCache(),
		stderr:           newByteRing(opts.stderrBytes),
		documents:        map[string]documentState{},
		pending:          map[string]chan wireMessage{},
		done:             make(chan struct{}),
	}
	if err := cmd.Start(); err != nil {
		_ = stdin.Close()
		_ = stdout.Close()
		return nil, fmt.Errorf("start language server: %w", err)
	}
	go func() { _, _ = io.Copy(p.stderr, stderr) }()
	go p.readLoop()
	go p.waitLoop()

	initCtx, cancel := context.WithTimeout(ctx, opts.initializeTimeout)
	defer cancel()
	if err := p.initialize(initCtx); err != nil {
		p.forceTerminate()
		return nil, fmt.Errorf("initialize language server: %w", err)
	}
	return p, nil
}

func (p *Process) initialize(ctx context.Context) error {
	var capabilities clientCapabilities
	capabilities.General.PositionEncodings = []string{"utf-16", "utf-8", "utf-32"}
	params := initializeParams{
		ProcessID:    os.Getpid(),
		RootURI:      p.rootURI,
		ClientInfo:   clientInfo{Name: "pudding"},
		Capabilities: capabilities,
		WorkspaceFolders: []workspaceFolder{{
			URI:  p.rootURI,
			Name: filepath.Base(p.spec.Key.LanguageRoot),
		}},
	}
	var result initializeResult
	if err := p.Request(ctx, "initialize", params, &result); err != nil {
		return err
	}
	if encoding := strings.TrimSpace(result.Capabilities.PositionEncoding); encoding != "" {
		p.positionEncoding = encoding
	}
	return p.Notify("initialized", struct{}{})
}

// Request sends one JSON-RPC request. Cancelling ctx only cancels this request.
func (p *Process) Request(ctx context.Context, method string, params, result any) error {
	if method == "" {
		return errors.New("lsp request method is required")
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	id := p.nextID.Add(1)
	idRaw := json.RawMessage(strconv.FormatInt(id, 10))
	paramsRaw, err := marshalOptional(params)
	if err != nil {
		return fmt.Errorf("marshal lsp params: %w", err)
	}
	responseCh := make(chan wireMessage, 1)
	key := string(idRaw)

	p.mu.Lock()
	if !p.aliveLocked() {
		err := fmt.Errorf("%w: %v", ErrRequestNotSent, p.processErrorLocked())
		p.mu.Unlock()
		return err
	}
	p.pending[key] = responseCh
	p.mu.Unlock()

	message := wireMessage{JSONRPC: "2.0", ID: idRaw, Method: method, Params: paramsRaw}
	if err := p.writeMessage(message); err != nil {
		p.removePending(key, responseCh)
		p.finish(fmt.Errorf("write lsp request: %w", err))
		p.forceTerminate()
		return err
	}

	select {
	case response := <-responseCh:
		if response.Error != nil {
			return response.Error
		}
		if result == nil || len(response.Result) == 0 || string(response.Result) == "null" {
			return nil
		}
		if err := json.Unmarshal(response.Result, result); err != nil {
			return fmt.Errorf("decode lsp response: %w", err)
		}
		return nil
	case <-ctx.Done():
		if p.removePending(key, responseCh) {
			_ = p.Notify("$/cancelRequest", map[string]any{"id": id})
		}
		return ctx.Err()
	case <-p.done:
		p.removePending(key, responseCh)
		return p.processError()
	}
}

// Notify sends one JSON-RPC notification.
func (p *Process) Notify(method string, params any) error {
	paramsRaw, err := marshalOptional(params)
	if err != nil {
		return fmt.Errorf("marshal lsp notification: %w", err)
	}
	return p.writeMessage(wireMessage{JSONRPC: "2.0", Method: method, Params: paramsRaw})
}

func (p *Process) writeMessage(message wireMessage) error {
	payload, err := json.Marshal(message)
	if err != nil {
		return err
	}
	p.writeMu.Lock()
	defer p.writeMu.Unlock()
	select {
	case <-p.done:
		return p.processError()
	default:
	}
	return writeFrame(p.stdin, payload, p.maxMessageBytes)
}

func (p *Process) readLoop() {
	for {
		payload, err := p.reader.Read()
		if err != nil {
			p.finish(fmt.Errorf("read lsp response: %w", err))
			p.forceTerminate()
			return
		}
		var message wireMessage
		if err := json.Unmarshal(payload, &message); err != nil {
			p.finish(fmt.Errorf("decode lsp response: %w", err))
			p.forceTerminate()
			return
		}
		if message.JSONRPC != "2.0" {
			p.finish(fmt.Errorf("%w: unsupported jsonrpc version", errProtocol))
			p.forceTerminate()
			return
		}
		p.handleMessage(message)
	}
}

func (p *Process) handleMessage(message wireMessage) {
	if len(message.ID) > 0 && message.Method == "" {
		key := string(message.ID)
		p.mu.Lock()
		responseCh := p.pending[key]
		delete(p.pending, key)
		p.mu.Unlock()
		if responseCh != nil {
			responseCh <- message
		}
		return
	}
	if message.Method == "" {
		return
	}
	if len(message.ID) > 0 {
		p.handleServerRequest(message)
		return
	}
	if message.Method == "textDocument/publishDiagnostics" {
		p.diagnostics.update(message.Params)
	}
}

func (p *Process) handleServerRequest(message wireMessage) {
	var result any
	var responseErr *ResponseError
	switch message.Method {
	case "workspace/configuration":
		var params struct {
			Items []json.RawMessage `json:"items"`
		}
		_ = json.Unmarshal(message.Params, &params)
		result = make([]any, len(params.Items))
	case "client/registerCapability", "window/workDoneProgress/create":
		result = nil
	case "workspace/workspaceFolders":
		result = []workspaceFolder{{URI: p.rootURI, Name: filepath.Base(p.spec.Key.LanguageRoot)}}
	case "window/showMessageRequest":
		result = nil
	case "workspace/applyEdit":
		responseErr = &ResponseError{Code: -32601, Message: "workspace/applyEdit is not supported"}
	default:
		responseErr = &ResponseError{Code: -32601, Message: "method not supported"}
	}
	resultRaw, err := json.Marshal(result)
	if err != nil {
		resultRaw = []byte("null")
	}
	if responseErr != nil {
		resultRaw = nil
	}
	_ = p.writeMessage(wireMessage{
		JSONRPC: "2.0",
		ID:      append(json.RawMessage(nil), message.ID...),
		Result:  resultRaw,
		Error:   responseErr,
	})
}

func (p *Process) waitLoop() {
	err := p.cmd.Wait()
	if err == nil {
		err = ErrProcessClosed
	} else {
		err = fmt.Errorf("language server exited: %w", err)
	}
	p.finish(err)
}

func (p *Process) finish(err error) {
	p.endOnce.Do(func() {
		p.mu.Lock()
		p.err = err
		p.mu.Unlock()
		close(p.done)
		_ = p.stdin.Close()
		_ = p.stdout.Close()
	})
}

func (p *Process) removePending(key string, responseCh chan wireMessage) bool {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.pending[key] != responseCh {
		return false
	}
	delete(p.pending, key)
	return true
}

func (p *Process) aliveLocked() bool {
	select {
	case <-p.done:
		return false
	default:
		return true
	}
}

func (p *Process) processErrorLocked() error {
	if p.err == nil {
		return ErrProcessClosed
	}
	return p.err
}

func (p *Process) processError() error {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.processErrorLocked()
}

func (p *Process) forceTerminate() {
	_ = terminateProcess(p.cmd)
}

// Close performs the LSP shutdown handshake, then force-terminates on timeout.
func (p *Process) Close(ctx context.Context) error {
	p.closeMu.Lock()
	if p.closing {
		p.closeMu.Unlock()
		select {
		case <-p.done:
			return nil
		case <-ctx.Done():
			return ctx.Err()
		}
	}
	p.closing = true
	p.closeMu.Unlock()

	select {
	case <-p.done:
		return nil
	default:
	}
	shutdownCtx, cancel := withTimeout(ctx, p.shutdownTimeout)
	defer cancel()
	var ignored any
	requestErr := p.Request(shutdownCtx, "shutdown", nil, &ignored)
	_ = p.Notify("exit", nil)
	select {
	case <-p.done:
		return requestErr
	case <-shutdownCtx.Done():
		p.forceTerminate()
		return fmt.Errorf("language server shutdown: %w (stderr: %q)", shutdownCtx.Err(), p.Stderr())
	}
}

func (p *Process) Alive() bool {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.aliveLocked()
}

func (p *Process) Pending() int {
	p.mu.Lock()
	defer p.mu.Unlock()
	return len(p.pending)
}

func (p *Process) Done() <-chan struct{} { return p.done }

func (p *Process) PID() int {
	if p.cmd == nil || p.cmd.Process == nil {
		return 0
	}
	return p.cmd.Process.Pid
}

func (p *Process) PositionEncoding() string { return p.positionEncoding }

func (p *Process) Diagnostics(uri string) (DiagnosticSnapshot, bool) {
	return p.diagnostics.get(uri)
}

func (p *Process) Stderr() string { return p.stderr.String() }

func marshalOptional(value any) (json.RawMessage, error) {
	if value == nil {
		return nil, nil
	}
	raw, err := json.Marshal(value)
	return json.RawMessage(raw), err
}

func fileURI(path string) string {
	path = filepath.ToSlash(path)
	if !strings.HasPrefix(path, "/") {
		path = "/" + path
	}
	return (&url.URL{Scheme: "file", Path: path}).String()
}

func withTimeout(ctx context.Context, timeout time.Duration) (context.Context, context.CancelFunc) {
	if timeout <= 0 {
		return context.WithCancel(ctx)
	}
	return context.WithTimeout(ctx, timeout)
}

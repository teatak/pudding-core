// Package acp hosts a local Agent Client Protocol process. It owns transport
// and agent sessions, not Pudding conversation history or tool authorization.
package acp

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"time"
)

const Provider = "local_agent:antigravity"

func IsProvider(name string) bool { return strings.HasPrefix(strings.TrimSpace(name), "local_agent:") }

// Candidates prefers the standalone installation, then PATH, then the app copy.
func Candidates() []string {
	home, _ := os.UserHomeDir()
	path, err := exec.LookPath("agy")
	if err != nil {
		path = ""
	}
	return candidateExecutables(runtime.GOOS, home, os.Getenv("LOCALAPPDATA"), path)
}

func candidateExecutables(goos, home, localAppData, pathExecutable string) []string {
	var paths []string
	name := "agy"
	if goos == "windows" {
		name = "agy.exe"
		if localAppData != "" {
			paths = append(paths, filepath.Join(localAppData, "agy", "bin", name))
		}
	} else if home != "" {
		paths = append(paths, filepath.Join(home, ".local", "bin", name))
	}
	if pathExecutable != "" {
		paths = append(paths, pathExecutable)
	}
	if home != "" {
		paths = append(paths, filepath.Join(home, ".gemini", "bin", name))
	}
	seen := map[string]bool{}
	var out []string
	for _, p := range paths {
		real, err := filepath.EvalSymlinks(p)
		if err != nil || seen[real] {
			continue
		}
		seen[real] = true
		out = append(out, real)
	}
	return out
}

type RPCError struct {
	Code    int             `json:"code"`
	Message string          `json:"message"`
	Data    json.RawMessage `json:"data,omitempty"`
}

func (e *RPCError) Error() string { return fmt.Sprintf("ACP (%d): %s", e.Code, e.Message) }

type message struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id,omitempty"`
	Method  string          `json:"method,omitempty"`
	Params  json.RawMessage `json:"params,omitempty"`
	Result  json.RawMessage `json:"result,omitempty"`
	Error   *RPCError       `json:"error,omitempty"`
}

type Capabilities struct {
	LoadSession bool `json:"loadSession"`
	Prompt      struct {
		Image           bool `json:"image"`
		Audio           bool `json:"audio"`
		EmbeddedContext bool `json:"embeddedContext"`
	} `json:"promptCapabilities"`
	Session struct {
		Resume *struct{} `json:"resume"`
		Close  *struct{} `json:"close"`
	} `json:"sessionCapabilities"`
}
type Initialization struct {
	ProtocolVersion int          `json:"protocolVersion"`
	Capabilities    Capabilities `json:"agentCapabilities"`
	AgentInfo       struct {
		Name    string `json:"name"`
		Version string `json:"version"`
	} `json:"agentInfo"`
	AuthMethods []struct {
		ID   string `json:"id"`
		Name string `json:"name"`
	} `json:"authMethods"`
}
type ConfigOption struct {
	ID           string `json:"id"`
	Name         string `json:"name"`
	Category     string `json:"category"`
	Type         string `json:"type"`
	CurrentValue string `json:"currentValue"`
	Options      []struct {
		Value string `json:"value"`
		Name  string `json:"name"`
	} `json:"options"`
}
type SessionResult struct {
	SessionID     string         `json:"sessionId"`
	ConfigOptions []ConfigOption `json:"configOptions"`
}
type Content struct {
	Type     string `json:"type"`
	Text     string `json:"text,omitempty"`
	Data     string `json:"data,omitempty"`
	MIMEType string `json:"mimeType,omitempty"`
	Resource any    `json:"resource,omitempty"`
}
type Update struct {
	SessionUpdate string          `json:"sessionUpdate"`
	ToolCallID    string          `json:"toolCallId"`
	Name          string          `json:"name"`
	Title         string          `json:"title"`
	Kind          string          `json:"kind"`
	Status        string          `json:"status"`
	Content       json.RawMessage `json:"content"`
	RawInput      json.RawMessage `json:"rawInput"`
	RawOutput     json.RawMessage `json:"rawOutput"`
	Used          int             `json:"used"`
	Size          int             `json:"size"`
}
type PermissionOption struct {
	ID   string `json:"optionId"`
	Name string `json:"name"`
	Kind string `json:"kind"`
}
type Permission struct {
	SessionID string             `json:"sessionId"`
	ToolCall  Update             `json:"toolCall"`
	Options   []PermissionOption `json:"options"`
}
type Outcome struct {
	Outcome  string `json:"outcome"`
	OptionID string `json:"optionId,omitempty"`
}
type PromptResult struct {
	Requests   int    `json:"-"`
	StopReason string `json:"stopReason"`
	Usage      *struct {
		Input       int `json:"inputTokens"`
		Output      int `json:"outputTokens"`
		CachedRead  int `json:"cachedReadTokens"`
		CachedWrite int `json:"cachedWriteTokens"`
		Thought     int `json:"thoughtTokens"`
	} `json:"usage"`
}
type Handlers struct {
	Update     func(Update)
	Permission func(context.Context, Permission) Outcome
	Ready      func()
	Steering   <-chan struct{}
	// NextPrompt consumes accepted steers. When seal is true it atomically
	// closes admission if none remain, before this prompt group completes.
	NextPrompt func(seal bool) ([]Content, error)
}

type Client struct {
	Path      string
	Init      Initialization
	cmd       *exec.Cmd
	stdin     io.WriteCloser
	writeMu   sync.Mutex
	mu        sync.Mutex
	seq       int
	pending   map[string]chan message
	active    *promptHandlers
	done      chan struct{}
	err       error
	closeOnce sync.Once
}
type promptHandlers struct {
	ctx         context.Context
	sessionID   string
	handlers    Handlers
	permissions int
}

func Open(ctx context.Context, paths []string, cwd string) (*Client, error) {
	var failures []error
	if len(paths) == 0 {
		return nil, errors.New("Antigravity CLI not found; install agy or add its directory to PATH")
	}
	for _, p := range paths {
		attempt, cancel := context.WithTimeout(ctx, 10*time.Second)
		c, err := start(attempt, p, cwd)
		cancel()
		if err == nil {
			return c, nil
		}
		failures = append(failures, fmt.Errorf("%s: %w", p, err))
		if ctx.Err() != nil {
			break
		}
	}
	return nil, errors.Join(failures...)
}
func start(ctx context.Context, executable, cwd string) (*Client, error) {
	cmd := exec.Command(executable, "acp")
	cmd.Dir = cwd
	configureProcess(cmd)
	stdin, err := cmd.StdinPipe()
	if err != nil {
		return nil, err
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		stdin.Close()
		return nil, err
	}
	// stdout is exclusively JSON-RPC. Agent diagnostics go to the daemon log.
	cmd.Stderr = os.Stderr
	c := &Client{Path: executable, cmd: cmd, stdin: stdin, pending: map[string]chan message{}, done: make(chan struct{})}
	if err = cmd.Start(); err != nil {
		stdin.Close()
		return nil, err
	}
	go c.read(stdout)
	var init Initialization
	err = c.Call(ctx, "initialize", map[string]any{"protocolVersion": 1, "clientInfo": map[string]string{"name": "pudding", "title": "Pudding", "version": "1"}, "clientCapabilities": map[string]any{}}, &init)
	if err == nil && init.ProtocolVersion != 1 {
		err = fmt.Errorf("unsupported ACP protocol version %d", init.ProtocolVersion)
	}
	if err != nil {
		c.Close()
		return nil, err
	}
	c.Init = init
	return c, nil
}
func (c *Client) read(r io.Reader) {
	scanner := bufio.NewScanner(r)
	scanner.Buffer(make([]byte, 64*1024), 32*1024*1024)
	var readErr error
	for scanner.Scan() {
		var m message
		if err := json.Unmarshal(scanner.Bytes(), &m); err != nil {
			readErr = fmt.Errorf("invalid ACP stdout: %w", err)
			_ = terminateProcess(c.cmd)
			break
		}
		if m.Method != "" {
			if len(m.ID) > 0 {
				var owner *promptHandlers
				if m.Method == "session/request_permission" {
					c.mu.Lock()
					owner = c.active
					if owner != nil {
						owner.permissions++
					}
					c.mu.Unlock()
				}
				go c.handleRequest(m, owner)
			} else {
				c.handleNotification(m)
			}
			continue
		}
		c.mu.Lock()
		ch := c.pending[string(m.ID)]
		delete(c.pending, string(m.ID))
		c.mu.Unlock()
		if ch != nil {
			ch <- m
		}
	}
	if readErr == nil {
		readErr = scanner.Err()
	}
	if readErr != nil {
		_ = terminateProcess(c.cmd)
	}
	waitErr := c.cmd.Wait()
	if readErr == nil {
		readErr = waitErr
	}
	if readErr == nil {
		readErr = io.EOF
	}
	c.mu.Lock()
	c.err = fmt.Errorf("ACP process exited: %w", readErr)
	close(c.done)
	c.mu.Unlock()
}
func (c *Client) send(m message) error {
	c.writeMu.Lock()
	defer c.writeMu.Unlock()
	m.JSONRPC = "2.0"
	return json.NewEncoder(c.stdin).Encode(m)
}

type rpcCall struct {
	id    string
	reply chan message
}

func (c *Client) startCall(method string, params any) (*rpcCall, error) {
	raw, err := json.Marshal(params)
	if err != nil {
		return nil, err
	}
	c.mu.Lock()
	c.seq++
	call := &rpcCall{id: fmt.Sprint(c.seq), reply: make(chan message, 1)}
	c.pending[call.id] = call.reply
	c.mu.Unlock()
	if err = c.send(message{ID: json.RawMessage(call.id), Method: method, Params: raw}); err != nil {
		c.mu.Lock()
		delete(c.pending, call.id)
		c.mu.Unlock()
		return nil, err
	}
	return call, nil
}
func (c *Client) waitCall(ctx context.Context, call *rpcCall, result any) error {
	defer func() { c.mu.Lock(); delete(c.pending, call.id); c.mu.Unlock() }()
	select {
	case m := <-call.reply:
		if m.Error != nil {
			return m.Error
		}
		if result != nil {
			return json.Unmarshal(m.Result, result)
		}
		return nil
	case <-ctx.Done():
		return ctx.Err()
	case <-c.done:
		c.mu.Lock()
		err := c.err
		c.mu.Unlock()
		return err
	}
}
func (c *Client) Call(ctx context.Context, method string, params, result any) error {
	call, err := c.startCall(method, params)
	if err != nil {
		return err
	}
	return c.waitCall(ctx, call, result)
}
func (c *Client) Notify(method string, params any) error {
	raw, err := json.Marshal(params)
	if err != nil {
		return err
	}
	return c.send(message{Method: method, Params: raw})
}
func (c *Client) Alive() bool {
	select {
	case <-c.done:
		return false
	default:
		return true
	}
}
func (c *Client) Close() {
	c.closeOnce.Do(func() {
		_ = c.stdin.Close()
		select {
		case <-c.done:
		case <-time.After(2 * time.Second):
			_ = terminateProcess(c.cmd)
			<-c.done
		}
	})
}
func (c *Client) handleNotification(m message) {
	if m.Method != "session/update" {
		return
	}
	var p struct {
		SessionID string `json:"sessionId"`
		Update    Update `json:"update"`
	}
	if json.Unmarshal(m.Params, &p) != nil {
		return
	}
	c.mu.Lock()
	h := c.active
	c.mu.Unlock()
	// No active prompt while new/resume/load is preparing: replay is ignored.
	if h != nil && h.sessionID == p.SessionID && h.handlers.Update != nil {
		h.handlers.Update(p.Update)
	}
}
func (c *Client) handleRequest(m message, h *promptHandlers) {
	if m.Method != "session/request_permission" {
		_ = c.send(message{ID: m.ID, Error: &RPCError{Code: -32601, Message: "Client capability not supported"}})
		return
	}
	defer func() {
		c.mu.Lock()
		if h != nil {
			h.permissions--
		}
		c.mu.Unlock()
	}()
	var p Permission
	if err := json.Unmarshal(m.Params, &p); err != nil {
		_ = c.send(message{ID: m.ID, Error: &RPCError{Code: -32602, Message: err.Error()}})
		return
	}
	outcome := Outcome{Outcome: "cancelled"}
	if h != nil && h.ctx.Err() == nil && h.sessionID == p.SessionID && h.handlers.Permission != nil {
		outcome = h.handlers.Permission(h.ctx, p)
	}
	raw, _ := json.Marshal(map[string]any{"outcome": outcome})
	_ = c.send(message{ID: m.ID, Result: raw})
}

type MCPServer struct {
	Name    string   `json:"name"`
	Command string   `json:"command"`
	Args    []string `json:"args"`
	Env     []EnvVar `json:"env"`
}
type EnvVar struct {
	Name  string `json:"name"`
	Value string `json:"value"`
}

// ErrSessionNotRestorable means the client cannot restore a saved context.
var ErrSessionNotRestorable = errors.New("Antigravity cannot resume this session")

// IsSessionNotRestorable deliberately excludes generic RPC/transport errors.
func IsSessionNotRestorable(err error) bool {
	if errors.Is(err, ErrSessionNotRestorable) {
		return true
	}
	var rpc *RPCError
	if !errors.As(err, &rpc) {
		return false
	}
	message := strings.ToLower(strings.TrimSpace(rpc.Message))
	// agy session/load reports this code and includes the missing session ID.
	if rpc.Code == -32002 && strings.HasPrefix(message, "session not found: ") {
		return strings.TrimSpace(strings.TrimPrefix(message, "session not found: ")) != ""
	}
	switch message {
	case "session not found", "unknown session", "session expired":
		return rpc.Code == -32603 || rpc.Code == -32602
	}
	return false
}

func (c *Client) Session(ctx context.Context, cwd, id string, servers ...MCPServer) (SessionResult, error) {
	method := "session/new"
	if servers == nil {
		servers = []MCPServer{}
	}
	params := map[string]any{"cwd": cwd, "mcpServers": servers}
	if id != "" {
		params["sessionId"] = id
		if c.Init.Capabilities.Session.Resume != nil {
			method = "session/resume"
		} else if c.Init.Capabilities.LoadSession {
			method = "session/load"
		} else {
			return SessionResult{}, ErrSessionNotRestorable
		}
	}
	var out SessionResult
	err := c.Call(ctx, method, params, &out)
	var rpc *RPCError
	if method == "session/resume" && c.Init.Capabilities.LoadSession && ctx.Err() == nil &&
		errors.As(err, &rpc) && rpc.Code == -32603 &&
		rpc.Message == "Failed to read session "+id+"; retry, or load it with session/load" {
		// A failed resume can still be loadable. Follow agy's explicit recovery
		// instruction once; only the load result may confirm the context is gone.
		err = c.Call(ctx, "session/load", params, &out)
	}
	if err == nil && out.SessionID == "" {
		out.SessionID = id
	}
	if err == nil && out.SessionID == "" {
		err = errors.New("ACP returned an empty session ID")
	}
	return out, err
}
func (c *Client) Configure(ctx context.Context, id, model, effort string) ([]ConfigOption, error) {
	var out SessionResult
	for _, choice := range [][2]string{{"model", model}, {"thought_level", effort}} {
		if choice[1] == "" {
			continue
		}
		if err := c.Call(ctx, "session/set_config_option", map[string]string{"sessionId": id, "configId": choice[0], "value": choice[1]}, &out); err != nil {
			return nil, err
		}
		matched := false
		for _, option := range out.ConfigOptions {
			if option.ID == choice[0] && option.CurrentValue == choice[1] {
				matched = true
			}
		}
		if !matched {
			return nil, fmt.Errorf("ACP did not confirm %s=%s", choice[0], choice[1])
		}
	}
	return out.ConfigOptions, nil
}
func (c *Client) CloseSession(ctx context.Context, id string) error {
	if c.Init.Capabilities.Session.Close == nil {
		return nil
	}
	return c.Call(ctx, "session/close", map[string]string{"sessionId": id}, nil)
}

var ErrSteerBlocked = errors.New("answer the pending permission or question before steering")

func (c *Client) CheckSteer() error {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.active != nil && c.active.permissions > 0 {
		return ErrSteerBlocked
	}
	return nil
}

func (c *Client) Prompt(ctx context.Context, id string, blocks []Content, h Handlers) (PromptResult, error) {
	if err := ctx.Err(); err != nil {
		return PromptResult{}, err
	}
	c.mu.Lock()
	if c.active != nil {
		c.mu.Unlock()
		return PromptResult{}, errors.New("ACP prompt already running")
	}
	c.active = &promptHandlers{ctx: ctx, sessionID: id, handlers: h}
	c.mu.Unlock()
	defer func() { c.mu.Lock(); c.active = nil; c.mu.Unlock() }()
	callCtx, stop := context.WithCancel(context.Background())
	defer stop()
	type response struct {
		result   PromptResult
		err      error
		sequence int
	}
	done := make(chan response, 1)
	pending, sequence, lastSequence := 0, 0, 0
	launch := func(prompt []Content) error {
		// Write in accepted message order. Each response can complete independently:
		// agy ends a superseded RPC before the current tool/new prompt finishes.
		call, err := c.startCall("session/prompt", map[string]any{"sessionId": id, "prompt": prompt})
		if err != nil {
			return err
		}
		pending++
		sequence++
		n := sequence
		go func() {
			var result PromptResult
			err := c.waitCall(callCtx, call, &result)
			done <- response{result, err, n}
		}()
		return nil
	}
	if err := launch(blocks); err != nil {
		return PromptResult{}, err
	}
	if h.Ready != nil {
		h.Ready()
	}

	var result PromptResult
	var promptErr error
	cancelled := ctx.Done()
	steering := h.Steering
	var timeout <-chan time.Time
	var timer *time.Timer
	defer func() {
		if timer != nil {
			timer.Stop()
		}
	}()
	cancelAll := func(err error) {
		if promptErr == nil {
			promptErr = err
		}
		if timer == nil {
			_ = c.Notify("session/cancel", map[string]string{"sessionId": id})
			timer = time.NewTimer(10 * time.Second)
			timeout = timer.C
		}
		cancelled, steering = nil, nil
	}
	for {
		if pending == 0 {
			if promptErr == nil && ctx.Err() == nil && h.NextPrompt != nil {
				next, err := h.NextPrompt(true)
				if err != nil {
					return result, err
				}
				if len(next) > 0 {
					if err := launch(next); err != nil {
						return result, err
					}
					continue
				}
			}
			if ctx.Err() != nil {
				return result, ctx.Err()
			}
			return result, promptErr
		}
		select {
		case reply := <-done:
			pending--
			if reply.sequence > lastSequence {
				result.StopReason, lastSequence = reply.result.StopReason, reply.sequence
			}
			if u := reply.result.Usage; u != nil {
				result.Requests++
				if result.Usage == nil {
					result.Usage = u
				} else {
					result.Usage.Input += u.Input
					result.Usage.Output += u.Output
					result.Usage.CachedRead += u.CachedRead
					result.Usage.CachedWrite += u.CachedWrite
					result.Usage.Thought += u.Thought
				}
			}
			if reply.err != nil {
				cancelAll(reply.err)
			}
		case <-steering:
			next, err := h.NextPrompt(false)
			if err != nil {
				cancelAll(err)
			} else if len(next) > 0 {
				if err := launch(next); err != nil {
					cancelAll(err)
				}
			}
		case <-cancelled:
			cancelAll(ctx.Err())
		case <-timeout:
			c.Close()
			timeout = nil
		}
	}
}

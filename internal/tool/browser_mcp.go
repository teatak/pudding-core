package tool

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/coder/websocket"
	"github.com/teatak/pudding-core/internal/provider"
	"github.com/teatak/pudding-core/internal/store"
)

type BrowserMCPRunner struct {
	mu       sync.Mutex
	nextID   atomic.Int64
	sessions []*browserMCPSession
}

type BrowserMCPSessionSnapshot struct {
	ID            string                   `json:"id"`
	ConnectedAt   time.Time                `json:"connectedAt"`
	ServerName    string                   `json:"serverName"`
	ServerVersion string                   `json:"serverVersion"`
	Tools         []BrowserMCPToolSnapshot `json:"tools"`
}

type BrowserMCPToolSnapshot struct {
	Name        string          `json:"name"`
	Description string          `json:"description,omitempty"`
	Capability  store.AgentMode `json:"capability,omitempty"`
}

func NewBrowserMCPRunner() *BrowserMCPRunner {
	return &BrowserMCPRunner{}
}

func (r *BrowserMCPRunner) ServeHTTP(w http.ResponseWriter, req *http.Request) {
	conn, err := websocket.Accept(w, req, &websocket.AcceptOptions{InsecureSkipVerify: true})
	if err != nil {
		slog.Warn("browser mcp: accept failed", "err", err)
		return
	}
	conn.SetReadLimit(1 << 20)
	session := &browserMCPSession{
		id:          store.NewID("mcp"),
		connectedAt: time.Now(),
		runner:      r,
		conn:        conn,
		pending:     map[string]chan rpcEnvelope{},
		done:        make(chan struct{}),
	}
	r.addSession(session)
	defer r.removeSession(session)
	session.run(req.Context())
}

func (r *BrowserMCPRunner) BrowserSessions() []BrowserMCPSessionSnapshot {
	r.mu.Lock()
	sessions := append([]*browserMCPSession(nil), r.sessions...)
	r.mu.Unlock()
	out := make([]BrowserMCPSessionSnapshot, 0, len(sessions))
	for i := len(sessions) - 1; i >= 0; i-- {
		out = append(out, sessions[i].snapshot())
	}
	return out
}

func (r *BrowserMCPRunner) Definitions(context.Context, string) ([]provider.ToolDef, error) {
	r.mu.Lock()
	sessions := append([]*browserMCPSession(nil), r.sessions...)
	r.mu.Unlock()

	seen := map[string]bool{}
	var defs []provider.ToolDef
	for i := len(sessions) - 1; i >= 0; i-- {
		for _, def := range sessions[i].definitions() {
			if def.Name == "" || seen[def.Name] {
				continue
			}
			seen[def.Name] = true
			defs = append(defs, def)
		}
	}
	return defs, nil
}

func (r *BrowserMCPRunner) Call(ctx context.Context, call Call) Result {
	session := r.latestSessionWithTool(call.Name)
	if session == nil {
		return Result{CallID: call.CallID, Name: call.Name, Ok: false, Content: fmt.Sprintf("unknown tool: %s", call.Name)}
	}
	args, err := browserToolArgs(call)
	if err != nil {
		return Result{CallID: call.CallID, Name: call.Name, Ok: false, Content: err.Error()}
	}
	raw, err := session.call(ctx, "tools/call", map[string]any{
		"name":      call.Name,
		"arguments": args,
	})
	if err != nil {
		return Result{CallID: call.CallID, Name: call.Name, Ok: false, Content: err.Error()}
	}
	return browserToolResult(call, raw)
}

func (r *BrowserMCPRunner) addSession(session *browserMCPSession) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.sessions = append(r.sessions, session)
}

func (r *BrowserMCPRunner) removeSession(session *browserMCPSession) {
	r.mu.Lock()
	defer r.mu.Unlock()
	for i, item := range r.sessions {
		if item == session {
			r.sessions = append(r.sessions[:i], r.sessions[i+1:]...)
			return
		}
	}
}

func (r *BrowserMCPRunner) latestSessionWithTool(name string) *browserMCPSession {
	r.mu.Lock()
	sessions := append([]*browserMCPSession(nil), r.sessions...)
	r.mu.Unlock()
	for i := len(sessions) - 1; i >= 0; i-- {
		if sessions[i].hasTool(name) {
			return sessions[i]
		}
	}
	return nil
}

type browserMCPSession struct {
	id          string
	connectedAt time.Time
	runner      *BrowserMCPRunner
	conn        *websocket.Conn

	writeMu       sync.Mutex
	mu            sync.Mutex
	serverName    string
	serverVersion string
	tools         []provider.ToolDef
	pending       map[string]chan rpcEnvelope
	done          chan struct{}
}

func (s *browserMCPSession) run(parent context.Context) {
	go s.readLoop(parent)
	ctx, cancel := context.WithTimeout(parent, 5*time.Second)
	defer cancel()
	raw, err := s.call(ctx, "initialize", map[string]any{
		"protocolVersion": "2024-11-05",
		"clientInfo": map[string]any{
			"name":    "pudding-core",
			"version": "1.0",
		},
	})
	if err != nil {
		slog.Warn("browser mcp: initialize failed", "err", err)
		_ = s.conn.Close(websocket.StatusProtocolError, err.Error())
		<-s.done
		return
	}
	s.setServerInfo(raw)
	if err := s.refreshTools(ctx); err != nil {
		slog.Warn("browser mcp: tools/list failed", "err", err)
		_ = s.conn.Close(websocket.StatusProtocolError, err.Error())
		<-s.done
		return
	}
	<-s.done
}

func (s *browserMCPSession) definitions() []provider.ToolDef {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]provider.ToolDef(nil), s.tools...)
}

func (s *browserMCPSession) snapshot() BrowserMCPSessionSnapshot {
	s.mu.Lock()
	defer s.mu.Unlock()
	tools := make([]BrowserMCPToolSnapshot, 0, len(s.tools))
	for _, def := range s.tools {
		tools = append(tools, BrowserMCPToolSnapshot{
			Name:        def.Name,
			Description: def.Description,
			Capability:  def.Capability,
		})
	}
	return BrowserMCPSessionSnapshot{
		ID:            s.id,
		ConnectedAt:   s.connectedAt,
		ServerName:    s.serverName,
		ServerVersion: s.serverVersion,
		Tools:         tools,
	}
}

func (s *browserMCPSession) hasTool(name string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, def := range s.tools {
		if def.Name == name {
			return true
		}
	}
	return false
}

func (s *browserMCPSession) setServerInfo(raw json.RawMessage) {
	var out struct {
		ServerInfo struct {
			Name    string `json:"name"`
			Version string `json:"version"`
		} `json:"serverInfo"`
	}
	if err := json.Unmarshal(raw, &out); err != nil {
		return
	}
	s.mu.Lock()
	s.serverName = strings.TrimSpace(out.ServerInfo.Name)
	s.serverVersion = strings.TrimSpace(out.ServerInfo.Version)
	s.mu.Unlock()
}

func (s *browserMCPSession) refreshTools(ctx context.Context) error {
	raw, err := s.call(ctx, "tools/list", map[string]any{})
	if err != nil {
		return err
	}
	var out struct {
		Tools []struct {
			Name        string          `json:"name"`
			Description string          `json:"description"`
			InputSchema json.RawMessage `json:"inputSchema"`
			Capability  store.AgentMode `json:"capability"`
		} `json:"tools"`
	}
	if err := json.Unmarshal(raw, &out); err != nil {
		return err
	}
	defs := make([]provider.ToolDef, 0, len(out.Tools))
	for _, tool := range out.Tools {
		name := strings.TrimSpace(tool.Name)
		if name == "" {
			continue
		}
		capability := store.NormalizeAgentMode(tool.Capability)
		if capability == "" {
			capability = store.ModeChat
		}
		defs = append(defs, provider.ToolDef{
			Name:        name,
			Description: tool.Description,
			InputSchema: tool.InputSchema,
			Capability:  capability,
		})
	}
	s.mu.Lock()
	s.tools = defs
	s.mu.Unlock()
	return nil
}

func (s *browserMCPSession) call(ctx context.Context, method string, params any) (json.RawMessage, error) {
	id := strconv.FormatInt(s.runner.nextID.Add(1), 10)
	key := strconv.Quote(id)
	ch := make(chan rpcEnvelope, 1)
	s.mu.Lock()
	s.pending[key] = ch
	s.mu.Unlock()
	defer func() {
		s.mu.Lock()
		delete(s.pending, key)
		s.mu.Unlock()
	}()

	req := rpcRequest{JSONRPC: "2.0", ID: id, Method: method, Params: params}
	payload, err := json.Marshal(req)
	if err != nil {
		return nil, err
	}
	s.writeMu.Lock()
	err = s.conn.Write(ctx, websocket.MessageText, payload)
	s.writeMu.Unlock()
	if err != nil {
		return nil, err
	}
	select {
	case resp, ok := <-ch:
		if !ok {
			return nil, errors.New("browser mcp session closed")
		}
		if resp.Error != nil {
			return nil, errors.New(resp.Error.Message)
		}
		return resp.Result, nil
	case <-s.done:
		return nil, errors.New("browser mcp session closed")
	case <-ctx.Done():
		return nil, ctx.Err()
	}
}

func (s *browserMCPSession) readLoop(ctx context.Context) {
	defer close(s.done)
	defer s.failPending()
	defer s.conn.Close(websocket.StatusNormalClosure, "")
	for {
		typ, payload, err := s.conn.Read(ctx)
		if err != nil {
			return
		}
		if typ != websocket.MessageText && typ != websocket.MessageBinary {
			continue
		}
		var envelope rpcEnvelope
		if err := json.Unmarshal(payload, &envelope); err != nil {
			continue
		}
		if len(envelope.ID) > 0 && (envelope.Result != nil || envelope.Error != nil) {
			s.mu.Lock()
			ch := s.pending[string(envelope.ID)]
			s.mu.Unlock()
			if ch != nil {
				ch <- envelope
			}
			continue
		}
		if envelope.Method == "notifications/tools/list_changed" {
			go func() {
				refreshCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
				defer cancel()
				if err := s.refreshTools(refreshCtx); err != nil {
					slog.Warn("browser mcp: refresh tools failed", "err", err)
				}
			}()
		}
	}
}

func (s *browserMCPSession) failPending() {
	s.mu.Lock()
	defer s.mu.Unlock()
	for key, ch := range s.pending {
		close(ch)
		delete(s.pending, key)
	}
}

type rpcRequest struct {
	JSONRPC string `json:"jsonrpc"`
	ID      string `json:"id"`
	Method  string `json:"method"`
	Params  any    `json:"params,omitempty"`
}

type rpcEnvelope struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id,omitempty"`
	Method  string          `json:"method,omitempty"`
	Params  json.RawMessage `json:"params,omitempty"`
	Result  json.RawMessage `json:"result,omitempty"`
	Error   *rpcError       `json:"error,omitempty"`
}

type rpcError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

func browserToolArgs(call Call) (map[string]any, error) {
	args := map[string]any{}
	if len(call.Args) > 0 && string(call.Args) != "null" {
		if err := json.Unmarshal(call.Args, &args); err != nil {
			return nil, fmt.Errorf("invalid arguments: %w", err)
		}
	}
	if strings.HasPrefix(call.Name, "canvas_") {
		sessionID := strings.TrimSpace(call.SessionID)
		if sessionID != "" {
			args["_pudding_session_id"] = sessionID
		}
	}
	return args, nil
}

func browserToolResult(call Call, raw json.RawMessage) Result {
	out := Result{CallID: call.CallID, Name: call.Name, Ok: true}
	var decoded struct {
		IsError bool `json:"isError"`
		Content []struct {
			Type string `json:"type"`
			Text string `json:"text"`
		} `json:"content"`
	}
	if err := json.Unmarshal(raw, &decoded); err == nil && len(decoded.Content) > 0 {
		var parts []string
		for _, item := range decoded.Content {
			if item.Text != "" {
				parts = append(parts, item.Text)
			}
		}
		out.Ok = !decoded.IsError
		out.Content = strings.Join(parts, "\n")
		return out
	}
	if len(raw) > 0 {
		out.Content = string(raw)
	}
	return out
}

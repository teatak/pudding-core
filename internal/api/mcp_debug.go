package api

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"mime"
	"net/http"
	"strings"
	"sync"

	"github.com/teatak/pudding-core/internal/plugin"
	"github.com/teatak/pudding-core/internal/provider"
	"github.com/teatak/pudding-core/internal/store"
)

const debugMCPProtocolVersion = "2025-06-18"
const debugMCPSessionTool = "pudding_mcp_session"

// One local developer binding per daemon. The transport is stateless; switching
// this binding also affects other clients attached to this debug endpoint.
type debugMCPBinding struct {
	mu              sync.Mutex
	sessionID       string
	debugSessionID  string
	preparedPlugins map[string]bool
}

type debugMCPRequest struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id"`
	Method  string          `json:"method"`
	Params  json.RawMessage `json:"params"`
}

type debugMCPTool struct {
	Name        string          `json:"name"`
	Description string          `json:"description,omitempty"`
	InputSchema json.RawMessage `json:"inputSchema"`
}

func (s *Server) serveDebugMCP(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	if origin := r.Header.Get("Origin"); origin != "" && !isAllowedCORSOrigin(origin) {
		http.Error(w, "origin not allowed", http.StatusForbidden)
		return
	}
	if r.Method != http.MethodPost {
		w.Header().Set("Allow", http.MethodPost)
		http.Error(w, "this MCP endpoint accepts JSON POST requests", http.StatusMethodNotAllowed)
		return
	}
	mediaType, _, err := mime.ParseMediaType(r.Header.Get("Content-Type"))
	if err != nil || mediaType != "application/json" {
		http.Error(w, "Content-Type must be application/json", http.StatusUnsupportedMediaType)
		return
	}
	if !debugMCPAccepts(r.Header.Get("Accept"), "application/json") || !debugMCPAccepts(r.Header.Get("Accept"), "text/event-stream") {
		http.Error(w, "Accept must include application/json and text/event-stream", http.StatusNotAcceptable)
		return
	}
	if version := r.Header.Get("MCP-Protocol-Version"); version != "" && version != debugMCPProtocolVersion {
		http.Error(w, "unsupported MCP protocol version", http.StatusBadRequest)
		return
	}
	decoder := json.NewDecoder(http.MaxBytesReader(w, r.Body, 16<<20))
	var payload json.RawMessage
	if err := decoder.Decode(&payload); err != nil {
		debugMCPError(w, http.StatusBadRequest, nil, -32700, "invalid JSON request")
		return
	}
	if err := decoder.Decode(new(any)); err != io.EOF {
		debugMCPError(w, http.StatusBadRequest, nil, -32700, "expected one JSON request")
		return
	}
	var request debugMCPRequest
	if !debugMCPObject(payload) || json.Unmarshal(payload, &request) != nil || request.JSONRPC != "2.0" || request.Method == "" || (len(request.ID) > 0 && !debugMCPValidID(request.ID)) {
		debugMCPError(w, http.StatusBadRequest, nil, -32600, "invalid JSON-RPC request")
		return
	}
	if len(request.Params) > 0 && !debugMCPObject(request.Params) {
		debugMCPError(w, http.StatusBadRequest, request.ID, -32602, "params must be an object")
		return
	}
	if strings.HasPrefix(request.Method, "notifications/") && len(request.ID) == 0 {
		// No server-to-client stream or replay state is needed for this endpoint.
		w.WriteHeader(http.StatusAccepted)
		return
	}
	if len(request.ID) == 0 {
		debugMCPError(w, http.StatusBadRequest, nil, -32600, "request id is required")
		return
	}
	ctx := s.debugMCPRuntimeContext(r.Context())
	var result any
	switch request.Method {
	case "initialize":
		var params struct {
			ProtocolVersion string          `json:"protocolVersion"`
			Capabilities    json.RawMessage `json:"capabilities"`
			ClientInfo      struct {
				Name    string `json:"name"`
				Version string `json:"version"`
			} `json:"clientInfo"`
		}
		if json.Unmarshal(request.Params, &params) != nil || params.ProtocolVersion == "" || !debugMCPObject(params.Capabilities) || params.ClientInfo.Name == "" || params.ClientInfo.Version == "" {
			debugMCPError(w, http.StatusOK, request.ID, -32602, "initialize requires protocolVersion, capabilities and clientInfo")
			return
		}
		result = map[string]any{
			"protocolVersion": debugMCPProtocolVersion,
			"serverInfo":      map[string]string{"name": "pudding-core-debug", "version": "1.0"},
			"capabilities":    map[string]any{"tools": map[string]bool{"listChanged": false}},
			"instructions":    "Local development access to Pudding tools. The daemon shares one session binding across these clients. Use pudding_mcp_session to inspect it or bind debug, personal, or an existing session ID. Tools follow that session's mode, approvals and plugin loading. Refresh tools/list after changing the binding or plugins; automatic list-change notifications are unavailable.",
		}
	case "ping":
		result = map[string]any{}
	case "tools/list":
		var params struct {
			Cursor string `json:"cursor"`
		}
		if len(request.Params) > 0 && json.Unmarshal(request.Params, &params) != nil || params.Cursor != "" {
			debugMCPError(w, http.StatusOK, request.ID, -32602, "tools/list returns the complete list and does not accept a cursor")
			return
		}
		result, err = s.debugMCPList(ctx)
	case "tools/call":
		var params struct {
			Name      string          `json:"name"`
			Arguments json.RawMessage `json:"arguments"`
		}
		if json.Unmarshal(request.Params, &params) != nil || strings.TrimSpace(params.Name) == "" || len(params.Arguments) > 0 && !debugMCPObject(params.Arguments) {
			debugMCPError(w, http.StatusOK, request.ID, -32602, "tools/call requires name and object arguments")
			return
		}
		if len(params.Arguments) == 0 {
			params.Arguments = json.RawMessage(`{}`)
		}
		result = s.debugMCPCall(ctx, params.Name, params.Arguments)
	default:
		debugMCPError(w, http.StatusOK, request.ID, -32601, "method not found")
		return
	}
	if err != nil {
		debugMCPError(w, http.StatusOK, request.ID, -32603, err.Error())
		return
	}
	debugMCPJSON(w, http.StatusOK, map[string]any{"jsonrpc": "2.0", "id": request.ID, "result": result})
}

func debugMCPAccepts(header, wanted string) bool {
	for _, value := range strings.Split(header, ",") {
		mediaType, params, err := mime.ParseMediaType(strings.TrimSpace(value))
		if err == nil && params["q"] != "0" && (mediaType == wanted || mediaType == "*/*") {
			return true
		}
	}
	return false
}

func debugMCPValidID(raw json.RawMessage) bool {
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.UseNumber()
	var value any
	if decoder.Decode(&value) != nil {
		return false
	}
	switch value.(type) {
	case string, json.Number:
		return true
	default:
		return false
	}
}

func debugMCPObject(raw json.RawMessage) bool {
	raw = bytes.TrimSpace(raw)
	return len(raw) > 0 && raw[0] == '{' && json.Valid(raw)
}

func debugMCPError(w http.ResponseWriter, status int, id json.RawMessage, code int, message string) {
	debugMCPJSON(w, status, map[string]any{"jsonrpc": "2.0", "id": id, "error": map[string]any{"code": code, "message": message}})
}

func debugMCPJSON(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(value)
}

func (s *Server) debugMCPRuntimeContext(ctx context.Context) context.Context {
	if plugin.RuntimeIDFromContext(ctx) != "" || s.browserMCP == nil {
		return ctx
	}
	// BrowserSessions is already ordered newest first. Web-only connections do
	// not provide Desktop tools and must not hide an active desktop connection.
	for _, runtime := range s.browserMCP.BrowserSessions() {
		if runtime.Runtime == "desktop" && runtime.RuntimeID != "" {
			return plugin.WithRuntimeID(ctx, runtime.RuntimeID)
		}
	}
	return ctx
}

func (s *Server) debugMCPSession(ctx context.Context, prepare bool) (*store.Session, error) {
	s.debugMCP.mu.Lock()
	defer s.debugMCP.mu.Unlock()
	if s.debugMCP.sessionID == "" {
		session, err := s.engine.DebugSession(ctx)
		if err != nil {
			return nil, err
		}
		s.debugMCP.sessionID, s.debugMCP.debugSessionID = session.ID, session.ID
	}
	session, err := s.store.GetSession(ctx, s.debugMCP.sessionID)
	if err != nil {
		return nil, err
	}
	if prepare && session.ID == s.debugMCP.debugSessionID {
		if s.debugMCP.preparedPlugins == nil {
			s.debugMCP.preparedPlugins = make(map[string]bool)
		}
		if err := s.engine.PrepareDebugPlugins(ctx, session.ID, session.ActiveMode, s.debugMCP.preparedPlugins); err != nil {
			return nil, err
		}
	}
	return session, nil
}

func (s *Server) debugMCPList(ctx context.Context) (any, error) {
	session, err := s.debugMCPSession(ctx, true)
	if err != nil {
		return nil, err
	}
	definitions, err := s.engine.ToolDefinitions(ctx, session.ID, session.ActiveMode)
	if err != nil {
		return nil, err
	}
	definitions = append(definitions, debugMCPSessionDefinition())
	tools := make([]debugMCPTool, 0, len(definitions))
	for _, definition := range definitions {
		tools = append(tools, debugMCPTool{Name: definition.Name, Description: definition.Description, InputSchema: definition.InputSchema})
	}
	return map[string]any{"tools": tools}, nil
}

func (s *Server) debugMCPCall(ctx context.Context, name string, args json.RawMessage) debugMCPResult {
	if name == debugMCPSessionTool {
		value, err := s.setDebugMCPSession(ctx, args)
		if err != nil {
			return debugMCPFailure(err)
		}
		data, _ := json.Marshal(value)
		return debugMCPText(string(data))
	}
	session, err := s.debugMCPSession(ctx, true)
	if err != nil {
		return debugMCPFailure(err)
	}
	result, err := s.engine.CallTool(ctx, session.ID, session.ActiveMode, name, args)
	if err != nil {
		return debugMCPFailure(err)
	}
	out, err := s.debugMCPToolResult(session.ID, result)
	if err != nil {
		return debugMCPFailure(err)
	}
	return out
}

func debugMCPSessionDefinition() provider.ToolDef {
	return provider.ToolDef{
		Name:        debugMCPSessionTool,
		Description: "Inspect or change the daemon's shared local-debug session binding. Omit arguments to inspect it. sessionID accepts debug, personal (the existing assistant conversation), or an existing session ID. An explicit mode updates that actual session; omitting it preserves the current mode. Existing sessions retain their plugin loading state. Refresh tools/list after changes.",
		InputSchema: json.RawMessage(`{"type":"object","properties":{"sessionID":{"type":"string","minLength":1},"mode":{"type":"string","enum":["chat","work","code"]}},"additionalProperties":false}`),
	}
}

func (s *Server) setDebugMCPSession(ctx context.Context, args json.RawMessage) (any, error) {
	var input struct {
		SessionID *string          `json:"sessionID"`
		Mode      *store.AgentMode `json:"mode"`
	}
	decoder := json.NewDecoder(bytes.NewReader(args))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&input); err != nil {
		return nil, fmt.Errorf("invalid session arguments: %w", err)
	}
	if input.SessionID != nil && strings.TrimSpace(*input.SessionID) == "" {
		return nil, errors.New("sessionID must not be empty")
	}
	if input.Mode != nil && !store.ValidAgentMode(*input.Mode) {
		return nil, errors.New("mode must be chat, work or code")
	}
	s.debugMCP.mu.Lock()
	defer s.debugMCP.mu.Unlock()
	selection := s.debugMCP.sessionID
	if input.SessionID != nil {
		selection = strings.TrimSpace(*input.SessionID)
	}
	var session *store.Session
	var err error
	switch selection {
	case "", "debug":
		session, err = s.engine.DebugSession(ctx)
		if err == nil {
			s.debugMCP.debugSessionID = session.ID
		}
	case "personal":
		var assistant *store.AssistantSnapshot
		assistant, err = s.store.GetAssistant(ctx)
		if err == nil {
			if assistant.Assistant == nil || assistant.Assistant.SessionID == "" {
				err = errors.New("the personal assistant conversation has not been opened")
			} else {
				session, err = s.store.GetSession(ctx, assistant.Assistant.SessionID)
			}
		}
	default:
		session, err = s.store.GetSession(ctx, selection)
	}
	if err != nil {
		return nil, err
	}
	if session.ArchivedAt != nil {
		return nil, errors.New("cannot bind an archived session; restore it first")
	}
	if input.Mode != nil {
		mode := store.NormalizeAgentMode(*input.Mode)
		lease := store.ModeLeaseSession
		if mode == store.ModeChat {
			lease = store.ModeLeaseNone
		}
		session, err = s.store.UpdateSession(ctx, session.ID, store.SessionUpdate{ActiveMode: &mode, ModeLease: &lease})
		if err != nil {
			return nil, err
		}
		if mode != store.ModeCode {
			s.engine.RevokeCommandApprovals(session.ID)
		}
	}
	s.debugMCP.sessionID = session.ID
	return map[string]any{"sessionID": session.ID, "mode": session.ActiveMode, "runtimeID": plugin.RuntimeIDFromContext(ctx), "debug": session.ID == s.debugMCP.debugSessionID}, nil
}

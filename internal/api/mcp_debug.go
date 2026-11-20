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
	"github.com/teatak/pudding-core/internal/tool"
)

const debugMCPProtocolVersion = "2025-06-18"
const debugMCPSessionTool = "pudding_mcp_session"
const debugMCPToolSearchTool = "pudding_tool_search"
const debugMCPToolCallTool = "pudding_tool_call"

var debugMCPCompactCoreTools = map[string]bool{
	tool.FileList:        true,
	tool.FileRead:        true,
	tool.FileSlice:       true,
	tool.FileSearch:      true,
	tool.FilePatch:       true,
	tool.FileWrite:       true,
	tool.GitStatus:       true,
	tool.GitDiff:         true,
	tool.GitLog:          true,
	tool.CodeDiagnostics: true,
	tool.CommandRun:      true,
	tool.TimeGetCurrent:  true,
	tool.PluginLoad:      true,
	tool.PluginUnload:    true,
}

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
	toolsMode := strings.TrimSpace(r.URL.Query().Get("tools"))
	if toolsMode == "" {
		toolsMode = "compact"
	}
	if toolsMode != "compact" && toolsMode != "all" {
		http.Error(w, "tools query parameter must be compact or all", http.StatusBadRequest)
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
			"instructions":    "Local development access to Pudding tools. The daemon shares one session binding across these clients. Use pudding_mcp_session to inspect it or bind debug, personal, or an existing session ID. By default tools/list returns core tools plus pudding_tool_search and pudding_tool_call so schemas stay compact; use pudding_tool_search to discover additional tools (browser, computer, widget, subsession, etc.) and inspect their full inputSchema, then call them via pudding_tool_call (or connect with ?tools=all for the full list).",
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
		result, err = s.debugMCPList(ctx, toolsMode == "all")
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

func isDebugMCPCompactDirectTool(name string) bool {
	return debugMCPCompactCoreTools[name] || tool.IsAssistantTool(name)
}

func debugMCPToolGroup(name string) string {
	switch {
	case strings.HasPrefix(name, "builtin_file_"):
		return "file"
	case strings.HasPrefix(name, "builtin_git_"):
		return "git"
	case strings.HasPrefix(name, "builtin_code_"):
		return "code"
	case strings.HasPrefix(name, "builtin_command_"):
		return "command"
	case strings.HasPrefix(name, "builtin_browser_"):
		return "browser"
	case strings.HasPrefix(name, "builtin_computer_") || strings.HasPrefix(name, "builtin_desktop_"):
		return "computer"
	case strings.HasPrefix(name, "widget_"):
		return "widget"
	case strings.HasPrefix(name, "builtin_subsession_"):
		return "subsession"
	case strings.HasPrefix(name, "builtin_plugin_"):
		return "plugin"
	case strings.HasPrefix(name, "builtin_web_") || strings.HasPrefix(name, "builtin_rest_") || strings.HasPrefix(name, "builtin_graphql_"):
		return "web"
	case strings.HasPrefix(name, "builtin_page_") || strings.HasPrefix(name, "builtin_sheet_"):
		return "document"
	case strings.HasPrefix(name, "builtin_history_"):
		return "history"
	case tool.IsAssistantTool(name):
		return "assistant"
	default:
		return "other"
	}
}

func (s *Server) debugMCPList(ctx context.Context, includeAll bool) (any, error) {
	session, err := s.debugMCPSession(ctx, true)
	if err != nil {
		return nil, err
	}
	definitions, err := s.engine.ToolDefinitions(ctx, session.ID, session.ActiveMode)
	if err != nil {
		return nil, err
	}
	tools := make([]debugMCPTool, 0, len(definitions)+3)
	for _, definition := range definitions {
		if !includeAll && !isDebugMCPCompactDirectTool(definition.Name) {
			continue
		}
		tools = append(tools, debugMCPTool{Name: definition.Name, Description: definition.Description, InputSchema: definition.InputSchema})
	}
	for _, meta := range []provider.ToolDef{debugMCPSessionDefinition(), debugMCPToolSearchDefinition(), debugMCPToolCallDefinition()} {
		tools = append(tools, debugMCPTool{Name: meta.Name, Description: meta.Description, InputSchema: meta.InputSchema})
	}
	return map[string]any{"tools": tools}, nil
}

func (s *Server) debugMCPCall(ctx context.Context, name string, args json.RawMessage) debugMCPResult {
	switch name {
	case debugMCPSessionTool:
		value, err := s.setDebugMCPSession(ctx, args)
		if err != nil {
			return debugMCPFailure(err)
		}
		data, _ := json.Marshal(value)
		return debugMCPText(string(data))
	case debugMCPToolSearchTool:
		value, err := s.debugMCPToolSearch(ctx, args)
		if err != nil {
			return debugMCPFailure(err)
		}
		data, _ := json.Marshal(value)
		return debugMCPText(string(data))
	case debugMCPToolCallTool:
		return s.debugMCPToolCallProxy(ctx, args)
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

func debugMCPToolSearchDefinition() provider.ToolDef {
	return provider.ToolDef{
		Name:        debugMCPToolSearchTool,
		Description: "Discover additional Pudding tools available in the bound session and inspect their full inputSchema. Omit all filters to list available tool names grouped by category; pass query, group, or names to inspect full descriptions and JSON schemas before invoking via pudding_tool_call.",
		InputSchema: json.RawMessage(`{"type":"object","properties":{"query":{"type":"string","description":"Case-insensitive keyword matched against tool name, group, and description."},"group":{"type":"string","description":"Filter by tool group (file, git, code, command, browser, computer, widget, subsession, plugin, web, document, history, assistant, other)."},"names":{"type":"array","items":{"type":"string","minLength":1},"description":"Exact tool names to inspect."},"limit":{"type":"integer","minimum":1,"maximum":50,"description":"Maximum number of tool definitions to return (default 20)."}},"additionalProperties":false}`),
	}
}

func debugMCPToolCallDefinition() provider.ToolDef {
	return provider.ToolDef{
		Name:        debugMCPToolCallTool,
		Description: "Invoke any tool available in the bound Pudding session by exact name and JSON object arguments. Use pudding_tool_search first to inspect the tool's inputSchema when it is not listed directly.",
		InputSchema: json.RawMessage(`{"type":"object","properties":{"name":{"type":"string","minLength":1,"description":"Exact tool name to invoke."},"arguments":{"type":"object","description":"JSON object arguments matching the target tool's inputSchema."}},"required":["name"],"additionalProperties":false}`),
	}
}

func (s *Server) debugMCPToolSearch(ctx context.Context, args json.RawMessage) (any, error) {
	var input struct {
		Query string   `json:"query"`
		Group string   `json:"group"`
		Names []string `json:"names"`
		Limit *int     `json:"limit"`
	}
	decoder := json.NewDecoder(bytes.NewReader(args))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&input); err != nil {
		return nil, fmt.Errorf("invalid tool search arguments: %w", err)
	}
	limit := 20
	if input.Limit != nil {
		if *input.Limit < 1 || *input.Limit > 50 {
			return nil, errors.New("limit must be between 1 and 50")
		}
		limit = *input.Limit
	}
	session, err := s.debugMCPSession(ctx, true)
	if err != nil {
		return nil, err
	}
	definitions, err := s.engine.ToolDefinitions(ctx, session.ID, session.ActiveMode)
	if err != nil {
		return nil, err
	}
	query := strings.ToLower(strings.TrimSpace(input.Query))
	group := strings.ToLower(strings.TrimSpace(input.Group))
	wantedNames := make(map[string]bool, len(input.Names))
	for _, name := range input.Names {
		if trimmed := strings.TrimSpace(name); trimmed != "" {
			wantedNames[trimmed] = true
		}
	}
	if query == "" && group == "" && len(wantedNames) == 0 {
		groups := make(map[string][]string)
		for _, def := range definitions {
			g := debugMCPToolGroup(def.Name)
			groups[g] = append(groups[g], def.Name)
		}
		return map[string]any{
			"totalCount": len(definitions),
			"groups":     groups,
		}, nil
	}
	type searchToolItem struct {
		Name        string          `json:"name"`
		Group       string          `json:"group"`
		Description string          `json:"description,omitempty"`
		InputSchema json.RawMessage `json:"inputSchema"`
	}
	matched := make([]searchToolItem, 0, min(limit, len(definitions)))
	totalMatches := 0
	for _, def := range definitions {
		defGroup := debugMCPToolGroup(def.Name)
		if len(wantedNames) > 0 && !wantedNames[def.Name] {
			continue
		}
		if group != "" && defGroup != group {
			continue
		}
		if query != "" &&
			!strings.Contains(strings.ToLower(def.Name), query) &&
			!strings.Contains(strings.ToLower(def.Description), query) &&
			!strings.Contains(defGroup, query) {
			continue
		}
		totalMatches++
		if len(matched) < limit {
			matched = append(matched, searchToolItem{
				Name:        def.Name,
				Group:       defGroup,
				Description: def.Description,
				InputSchema: def.InputSchema,
			})
		}
	}
	return map[string]any{
		"totalMatches": totalMatches,
		"truncated":    totalMatches > len(matched),
		"tools":        matched,
	}, nil
}

func (s *Server) debugMCPToolCallProxy(ctx context.Context, args json.RawMessage) debugMCPResult {
	var input struct {
		Name      string          `json:"name"`
		Arguments json.RawMessage `json:"arguments"`
	}
	decoder := json.NewDecoder(bytes.NewReader(args))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&input); err != nil {
		return debugMCPFailure(fmt.Errorf("invalid tool call arguments: %w", err))
	}
	name := strings.TrimSpace(input.Name)
	if name == "" {
		return debugMCPFailure(errors.New("name is required"))
	}
	if name == debugMCPToolCallTool {
		return debugMCPFailure(errors.New("cannot recursively call pudding_tool_call"))
	}
	callArgs := input.Arguments
	if len(callArgs) == 0 {
		callArgs = json.RawMessage(`{}`)
	} else if !debugMCPObject(callArgs) {
		return debugMCPFailure(errors.New("arguments must be a JSON object"))
	}
	return s.debugMCPCall(ctx, name, callArgs)
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

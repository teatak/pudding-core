package api

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
)

var localHostTools = []debugMCPTool{
	{Name: "pudding_tools", Description: "Discover tools, plugins and Skills on demand. Set query to a tool name or search text to get matching inputSchemas; omit query for the catalog and live instructions. Reuse schemas already in context. No per-turn refresh is required: Pudding supplies current project and selected Skill/plugin instructions with each prompt and refreshes them in tool results.", InputSchema: json.RawMessage(`{"type":"object","properties":{"query":{"type":"string"}},"additionalProperties":false}`)},
	{Name: "pudding_call", Description: "Execute a Pudding tool in the current conversation and user turn. Use the exact tool name and arguments from pudding_tools. No bindingID or turnID is needed. All operations inherit the conversation's permissions. Read updated projectInstructions and instructions in results.", InputSchema: json.RawMessage(`{"type":"object","properties":{"tool":{"type":"string"},"arguments":{"type":"object"}},"required":["tool","arguments"],"additionalProperties":false}`)},
}

func (s *Server) serveLocalAgentMCP(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	secret := strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer ")
	if r.Header.Get("Origin") != "" || r.Header.Get("X-Pudding-Request-Source") != "" || !s.engine.HasLocalHost(secret) {
		http.Error(w, "host connection unavailable", http.StatusUnauthorized)
		return
	}
	if r.Method != http.MethodPost {
		http.Error(w, "POST required", 405)
		return
	}
	var request debugMCPRequest
	decoder := json.NewDecoder(http.MaxBytesReader(w, r.Body, 16<<20))
	if err := decoder.Decode(&request); err != nil || request.JSONRPC != "2.0" || request.Method == "" || (len(request.ID) > 0 && !debugMCPValidID(request.ID)) {
		debugMCPError(w, 400, nil, -32600, "invalid JSON-RPC request")
		return
	}
	if decoder.Decode(new(any)) != io.EOF {
		debugMCPError(w, 400, request.ID, -32600, "expected one request")
		return
	}
	if len(request.ID) == 0 {
		w.WriteHeader(202)
		return
	}
	var result any
	switch request.Method {
	case "initialize":
		result = map[string]any{"protocolVersion": debugMCPProtocolVersion, "serverInfo": map[string]string{"name": "pudding", "version": "1"}, "capabilities": map[string]any{"tools": map[string]any{}}, "instructions": "Pudding supplies current rules with each prompt. Discover tools and missing schemas with pudding_tools only when needed; reuse known schemas and execute with pudding_call. No per-turn catalog call is required. The host owns the conversation and Turn. Do not connect or begin/end turns."}
	case "ping":
		result = map[string]any{}
	case "tools/list":
		result = map[string]any{"tools": localHostTools}
	case "tools/call":
		var in struct {
			Name      string          `json:"name"`
			Arguments json.RawMessage `json:"arguments"`
		}
		if err := json.Unmarshal(request.Params, &in); err != nil {
			debugMCPError(w, 200, request.ID, -32602, "tools/call requires name and arguments")
			return
		}
		var err error
		result, err = s.localHostCall(r.Context(), secret, in.Name, in.Arguments)
		if err != nil {
			result = debugMCPFailure(err)
		}
	default:
		debugMCPError(w, 200, request.ID, -32601, "method not supported")
		return
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]any{"jsonrpc": "2.0", "id": request.ID, "result": result})
}
func (s *Server) localHostCall(ctx context.Context, secret, name string, args json.RawMessage) (any, error) {
	if len(args) == 0 {
		args = json.RawMessage(`{}`)
	}
	if !debugMCPObject(args) {
		return nil, errors.New(`arguments must be an object. Example: pudding_tools({"query":"builtin_file_read"}) or pudding_call({"tool":"builtin_file_read","arguments":{"path":"README.md"}})`)
	}
	var (
		query    string
		toolName string
		toolArgs json.RawMessage
	)
	switch name {
	case "pudding_tools":
		var in struct {
			Query string `json:"query"`
		}
		decode := json.NewDecoder(bytes.NewReader(args))
		decode.DisallowUnknownFields()
		if err := decode.Decode(&in); err != nil {
			return nil, fmt.Errorf("invalid arguments for pudding_tools: %w. Allowed properties: query (optional string). Example: pudding_tools({\"query\":\"builtin_file_read\"})", err)
		}
		query = in.Query
	case "pudding_call":
		var in struct {
			Tool      string          `json:"tool"`
			Arguments json.RawMessage `json:"arguments"`
		}
		decode := json.NewDecoder(bytes.NewReader(args))
		decode.DisallowUnknownFields()
		if err := decode.Decode(&in); err != nil {
			return nil, fmt.Errorf("invalid arguments for pudding_call: %w. Required properties: tool (string), arguments (object). Example: pudding_call({\"tool\":\"builtin_file_read\",\"arguments\":{\"path\":\"README.md\"}})", err)
		}
		if in.Tool == "" || !debugMCPObject(in.Arguments) {
			return nil, errors.New(`tool must be a nonempty name and arguments an object. Example: {"tool":"builtin_file_read","arguments":{"path":"README.md"}}. Query pudding_tools for the tool's exact schema.`)
		}
		toolName = in.Tool
		toolArgs = in.Arguments
	default:
		return nil, fmt.Errorf("unknown tool %q; use pudding_tools or pudding_call", name)
	}
	turn, release, err := s.engine.AcquireLocalHost(secret)
	if err != nil {
		return nil, fmt.Errorf("this user turn is no longer active: %w; wait for the next user request", err)
	}
	defer release()
	callCtx, cancel := context.WithCancel(turn.Context)
	stop := context.AfterFunc(ctx, cancel)
	defer stop()
	defer cancel()
	sess, err := s.store.GetSession(callCtx, turn.SessionID)
	if err != nil {
		return nil, err
	}
	if name == "pudding_tools" {
		catalog, err := s.sessionToolCatalog(callCtx, sess, turn.TurnID, s.engine.LocalHostMode(turn, sess.ActiveMode), query, false)
		if err != nil {
			return nil, err
		}
		// Lifecycle identifiers belong to the host, not model arguments.
		delete(catalog, "activeTurnID")
		raw, err := json.Marshal(catalog)
		return debugMCPText(string(raw)), err
	}
	reply, err := s.engine.CallLocalHost(callCtx, turn, toolName, toolArgs)
	if err != nil {
		return nil, err
	}
	result, err := s.agentToolReply(callCtx, sess, turn.TurnID, reply)
	if err != nil {
		return nil, err
	}
	// Include refreshed rules as MCP text blocks; clients may discard extension
	// fields outside the standard CallToolResult content array.
	raw, err := json.Marshal(result)
	if err != nil {
		return nil, err
	}
	var out struct {
		debugMCPResult
		Instructions        map[string]json.RawMessage `json:"instructions"`
		ProjectInstructions *string                    `json:"projectInstructions"`
	}
	if err = json.Unmarshal(raw, &out); err != nil {
		return nil, err
	}
	if len(out.Instructions) > 0 || out.ProjectInstructions != nil {
		instructions, _ := json.Marshal(map[string]any{"instructions": out.Instructions, "projectInstructions": out.ProjectInstructions})
		out.Content = append(out.Content, map[string]any{"type": "text", "text": string(instructions)})
	}
	return out.debugMCPResult, nil
}

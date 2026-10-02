package tool

import (
	"bufio"
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/teatak/pudding-core/internal/pluginexec"
	"hash/fnv"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"os/exec"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/teatak/pudding-core/internal/plugin"
	"github.com/teatak/pudding-core/internal/provider"
	"github.com/teatak/pudding-core/internal/store"
)

const (
	pluginMCPToolPrefix        = "plugin_mcp__"
	pluginMCPProtocolVersion   = "2025-06-18"
	pluginMCPRequestTimeout    = 20 * time.Second
	pluginMCPDiscoverTimeout   = 8 * time.Second
	pluginMCPToolCacheTTL      = 5 * time.Minute
	pluginMCPEmptyToolCacheTTL = 30 * time.Second
	pluginMCPMaxResponseBytes  = 1 * 1024 * 1024
	pluginMCPMaxTools          = 300
	pluginMCPMaxToolNameLen    = 64
)

type PluginMCPSource interface {
	ListEndpointBindings(ctx context.Context, kind string) ([]*plugin.EndpointBinding, error)
}

type PluginMCPOption func(*PluginMCPRunner)

type PluginMCPRunner struct {
	source     PluginMCPSource
	httpClient *http.Client
	nextID     atomic.Int64

	mu           sync.Mutex
	sessionDefs  map[string][]provider.ToolDef
	sessionTools map[string]map[string]pluginMCPDiscoveredTool
	caches       map[string]pluginMCPToolCache
}

type PluginMCPProbeStatus string

const (
	PluginMCPProbeAvailable       PluginMCPProbeStatus = "available"
	PluginMCPProbeUnavailable     PluginMCPProbeStatus = "unavailable"
	PluginMCPProbeUnsupported     PluginMCPProbeStatus = "unsupported"
	PluginMCPProbeNeedsConnection PluginMCPProbeStatus = "needs_connection"
)

type PluginMCPProbeEndpoint struct {
	PluginID     string               `json:"pluginID"`
	EndpointName string               `json:"endpointName"`
	ConnectionID string               `json:"connectionID,omitempty"`
	Transport    string               `json:"transport,omitempty"`
	Configured   bool                 `json:"configured,omitempty"`
	Status       PluginMCPProbeStatus `json:"status"`
	Error        string               `json:"error,omitempty"`
	Tools        []PluginMCPProbeTool `json:"tools,omitempty"`
}

type PluginMCPProbeTool struct {
	Name         string          `json:"name"`
	ProviderName string          `json:"providerName,omitempty"`
	Title        string          `json:"title,omitempty"`
	Description  string          `json:"description,omitempty"`
	InputSchema  json.RawMessage `json:"inputSchema,omitempty"`
}

type pluginMCPDiscoveredTool struct {
	binding    *plugin.EndpointBinding
	remoteName string
}

type pluginMCPClient interface {
	initialize(ctx context.Context) error
	listTools(ctx context.Context) ([]pluginMCPRemoteTool, error)
	call(ctx context.Context, method string, params any) (json.RawMessage, error)
	close()
}

type pluginMCPToolCache struct {
	key       string
	expiresAt time.Time
	defs      []provider.ToolDef
	tools     map[string]pluginMCPDiscoveredTool
}

func NewPluginMCPRunner(source PluginMCPSource, opts ...PluginMCPOption) *PluginMCPRunner {
	r := &PluginMCPRunner{
		source:       source,
		httpClient:   &http.Client{Timeout: pluginMCPRequestTimeout},
		sessionDefs:  map[string][]provider.ToolDef{},
		sessionTools: map[string]map[string]pluginMCPDiscoveredTool{},
		caches:       map[string]pluginMCPToolCache{},
	}
	for _, opt := range opts {
		opt(r)
	}
	return r
}

func WithPluginMCPHTTPClient(client *http.Client) PluginMCPOption {
	return func(r *PluginMCPRunner) {
		if client != nil {
			r.httpClient = client
		}
	}
}

func (r *PluginMCPRunner) Definitions(_ context.Context, sessionID string) ([]provider.ToolDef, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	return clonePluginMCPToolDefs(r.sessionDefs[sessionID]), nil
}

func (r *PluginMCPRunner) DefinitionsForPlugins(ctx context.Context, sessionID string, pluginIDs []string) ([]provider.ToolDef, error) {
	pluginIDs = normalizePluginIDs(pluginIDs)
	if len(pluginIDs) == 0 {
		r.setSessionTools(sessionID, nil, nil)
		return nil, nil
	}
	bindings, err := r.listBindings(ctx)
	if err != nil {
		r.setSessionTools(sessionID, nil, nil)
		return nil, nil
	}
	bindings = filterPluginMCPBindings(bindings, pluginIDs)
	cacheKey := pluginMCPBindingsCacheKey(bindings)
	if defs, tools, ok := r.cached(cacheKey); ok {
		r.setSessionTools(sessionID, defs, tools)
		return defs, nil
	}
	defs, tools := r.discoverBindings(ctx, bindings)
	r.setSessionTools(sessionID, defs, tools)
	r.mu.Lock()
	r.caches[cacheKey] = pluginMCPToolCache{
		key:       cacheKey,
		expiresAt: time.Now().Add(pluginMCPCacheTTL(defs)),
		defs:      clonePluginMCPToolDefs(defs),
		tools:     clonePluginMCPTools(tools),
	}
	r.mu.Unlock()
	return defs, nil
}

func (r *PluginMCPRunner) Call(ctx context.Context, call Call) Result {
	out := Result{CallID: call.CallID, Name: call.Name}
	tool, ok := r.lookup(call.SessionID, call.Name)
	if !ok {
		return toolJSON(out, false, map[string]any{"ok": false, "reason": "unknown_tool", "tool": call.Name})
	}
	args, err := decodeToolArgs(call.Args)
	if err != nil {
		return toolJSON(out, false, map[string]any{"ok": false, "reason": "invalid_arguments", "error": err.Error()})
	}
	client, err := r.newClient(tool.binding)
	if err != nil {
		return toolJSON(out, false, map[string]any{"ok": false, "reason": "mcp_endpoint_error", "error": err.Error()})
	}
	defer client.close()
	if err := client.initialize(ctx); err != nil {
		return toolJSON(out, false, map[string]any{"ok": false, "reason": "mcp_initialize_failed", "error": err.Error()})
	}
	raw, err := client.call(ctx, "tools/call", map[string]any{
		"name":      tool.remoteName,
		"arguments": args,
	})
	if err != nil {
		return toolJSON(out, false, map[string]any{"ok": false, "reason": "mcp_tool_failed", "error": err.Error()})
	}
	return pluginMCPToolResult(call, raw)
}

func (r *PluginMCPRunner) lookup(sessionID, name string) (pluginMCPDiscoveredTool, bool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	tool, ok := r.sessionTools[sessionID][name]
	return tool, ok
}

func (r *PluginMCPRunner) setSessionTools(sessionID string, defs []provider.ToolDef, tools map[string]pluginMCPDiscoveredTool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.sessionDefs[sessionID] = clonePluginMCPToolDefs(defs)
	r.sessionTools[sessionID] = clonePluginMCPTools(tools)
}

func (r *PluginMCPRunner) CloseSession(sessionID string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	delete(r.sessionDefs, sessionID)
	delete(r.sessionTools, sessionID)
}

func (r *PluginMCPRunner) cached(cacheKey string) ([]provider.ToolDef, map[string]pluginMCPDiscoveredTool, bool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	cache, ok := r.caches[cacheKey]
	if !ok || cache.key == "" || time.Now().After(cache.expiresAt) {
		delete(r.caches, cacheKey)
		return nil, nil, false
	}
	return clonePluginMCPToolDefs(cache.defs), clonePluginMCPTools(cache.tools), true
}

func normalizePluginIDs(pluginIDs []string) []string {
	seen := make(map[string]bool, len(pluginIDs))
	out := make([]string, 0, len(pluginIDs))
	for _, pluginID := range pluginIDs {
		pluginID = strings.TrimSpace(pluginID)
		if pluginID == "" || seen[pluginID] {
			continue
		}
		seen[pluginID] = true
		out = append(out, pluginID)
	}
	sort.Strings(out)
	return out
}

func filterPluginMCPBindings(bindings []*plugin.EndpointBinding, pluginIDs []string) []*plugin.EndpointBinding {
	allowed := make(map[string]bool, len(pluginIDs))
	for _, pluginID := range pluginIDs {
		allowed[pluginID] = true
	}
	out := make([]*plugin.EndpointBinding, 0, len(bindings))
	for _, binding := range bindings {
		if binding != nil && allowed[binding.PluginID] {
			out = append(out, binding)
		}
	}
	return out
}

func (r *PluginMCPRunner) listBindings(ctx context.Context) ([]*plugin.EndpointBinding, error) {
	if r == nil || r.source == nil {
		return nil, nil
	}
	bindings, err := r.source.ListEndpointBindings(ctx, plugin.EndpointKindMCP)
	if err != nil {
		slog.Warn("plugin mcp: list endpoint bindings failed", "err", err)
		return nil, err
	}
	return bindings, nil
}

func (r *PluginMCPRunner) discoverBindings(ctx context.Context, bindings []*plugin.EndpointBinding) ([]provider.ToolDef, map[string]pluginMCPDiscoveredTool) {
	tools := map[string]pluginMCPDiscoveredTool{}
	defs := make([]provider.ToolDef, 0)
	for _, binding := range bindings {
		if binding == nil || binding.Endpoint.Kind != plugin.EndpointKindMCP || !pluginMCPSupportedTransport(binding.Endpoint.Transport) {
			continue
		}
		bindingDefs, bindingTools, err := r.discoverBinding(ctx, binding)
		if err != nil {
			slog.Warn("plugin mcp: discover endpoint failed", "plugin", binding.PluginID, "endpoint", binding.EndpointName, "connection", binding.ConnectionID, "err", err)
			continue
		}
		for i, def := range bindingDefs {
			if def.Name == "" {
				continue
			}
			if _, exists := tools[def.Name]; exists {
				continue
			}
			defs = append(defs, def)
			tools[def.Name] = bindingTools[i]
		}
	}
	return defs, tools
}

func (r *PluginMCPRunner) discoverBinding(ctx context.Context, binding *plugin.EndpointBinding) ([]provider.ToolDef, []pluginMCPDiscoveredTool, error) {
	remoteTools, err := r.listBindingRemoteTools(ctx, binding)
	if err != nil {
		return nil, nil, err
	}
	defs := make([]provider.ToolDef, 0, len(remoteTools))
	tools := make([]pluginMCPDiscoveredTool, 0, len(remoteTools))
	for _, remote := range remoteTools {
		remoteName := strings.TrimSpace(remote.Name)
		if remoteName == "" {
			continue
		}
		name := pluginMCPProviderToolName(binding, remoteName)
		description := pluginMCPToolDescription(binding, remote)
		inputSchema := remote.InputSchema
		if len(inputSchema) == 0 {
			inputSchema = remote.inputSnake
		}
		defs = append(defs, provider.ToolDef{
			Name:        name,
			Description: description,
			InputSchema: pluginMCPInputSchema(inputSchema),
			Capability:  store.ModeWork,
			PluginID:    binding.PluginID,
		})
		tools = append(tools, pluginMCPDiscoveredTool{
			binding:    clonePluginMCPBinding(binding),
			remoteName: remoteName,
		})
	}
	return defs, tools, nil
}

func (r *PluginMCPRunner) ProbeBinding(ctx context.Context, binding *plugin.EndpointBinding) PluginMCPProbeEndpoint {
	out := PluginMCPProbeEndpoint{Status: PluginMCPProbeUnavailable}
	if binding == nil {
		out.Error = "mcp endpoint unavailable"
		return out
	}
	out.PluginID = binding.PluginID
	out.EndpointName = binding.EndpointName
	out.ConnectionID = binding.ConnectionID
	out.Transport = strings.TrimSpace(binding.Endpoint.Transport)
	if binding.Endpoint.Kind != plugin.EndpointKindMCP || !pluginMCPSupportedTransport(binding.Endpoint.Transport) {
		out.Status = PluginMCPProbeUnsupported
		out.Error = fmt.Sprintf("unsupported mcp transport %q", binding.Endpoint.Transport)
		return out
	}
	remoteTools, err := r.listBindingRemoteTools(ctx, binding)
	if err != nil {
		out.Error = err.Error()
		return out
	}
	out.Status = PluginMCPProbeAvailable
	out.Tools = make([]PluginMCPProbeTool, 0, len(remoteTools))
	for _, remote := range remoteTools {
		remoteName := strings.TrimSpace(remote.Name)
		if remoteName == "" {
			continue
		}
		inputSchema := remote.InputSchema
		if len(inputSchema) == 0 {
			inputSchema = remote.inputSnake
		}
		out.Tools = append(out.Tools, PluginMCPProbeTool{
			Name:         remoteName,
			ProviderName: pluginMCPProviderToolName(binding, remoteName),
			Title:        strings.TrimSpace(remote.Title),
			Description:  strings.TrimSpace(remote.Description),
			InputSchema:  pluginMCPInputSchema(inputSchema),
		})
	}
	return out
}

func (r *PluginMCPRunner) listBindingRemoteTools(ctx context.Context, binding *plugin.EndpointBinding) ([]pluginMCPRemoteTool, error) {
	discoverCtx, cancel := context.WithTimeout(ctx, pluginMCPDiscoverTimeout)
	defer cancel()
	client, err := r.newClient(binding)
	if err != nil {
		return nil, err
	}
	defer client.close()
	if err := client.initialize(discoverCtx); err != nil {
		return nil, err
	}
	return client.listTools(discoverCtx)
}

func (r *PluginMCPRunner) newClient(binding *plugin.EndpointBinding) (pluginMCPClient, error) {
	switch strings.TrimSpace(binding.Endpoint.Transport) {
	case plugin.EndpointTransportStreamableHTTP:
		return r.newStreamableHTTPClient(binding), nil
	case plugin.EndpointTransportStdio:
		return r.newStdioClient(binding), nil
	default:
		return nil, fmt.Errorf("unsupported mcp transport %q", binding.Endpoint.Transport)
	}
}

func (r *PluginMCPRunner) newStreamableHTTPClient(binding *plugin.EndpointBinding) *pluginMCPHTTPClient {
	return &pluginMCPHTTPClient{
		runner:   r,
		binding:  clonePluginMCPBinding(binding),
		client:   r.httpClient,
		protocol: pluginMCPProtocolVersion,
	}
}

func (r *PluginMCPRunner) newStdioClient(binding *plugin.EndpointBinding) *pluginMCPStdioClient {
	return &pluginMCPStdioClient{
		runner:   r,
		binding:  clonePluginMCPBinding(binding),
		protocol: pluginMCPProtocolVersion,
	}
}

type pluginMCPRemoteTool struct {
	Name        string          `json:"name"`
	Title       string          `json:"title"`
	Description string          `json:"description"`
	InputSchema json.RawMessage `json:"inputSchema"`
	inputSnake  json.RawMessage
}

func (t *pluginMCPRemoteTool) UnmarshalJSON(data []byte) error {
	var raw struct {
		Name             string          `json:"name"`
		Title            string          `json:"title"`
		Description      string          `json:"description"`
		InputSchema      json.RawMessage `json:"inputSchema"`
		InputSchemaSnake json.RawMessage `json:"input_schema"`
	}
	if err := json.Unmarshal(data, &raw); err != nil {
		return err
	}
	t.Name = raw.Name
	t.Title = raw.Title
	t.Description = raw.Description
	t.InputSchema = raw.InputSchema
	t.inputSnake = raw.InputSchemaSnake
	return nil
}

type pluginMCPHTTPClient struct {
	runner    *PluginMCPRunner
	binding   *plugin.EndpointBinding
	client    *http.Client
	sessionID string
	protocol  string
}

func (c *pluginMCPHTTPClient) initialize(ctx context.Context) error {
	raw, err := c.call(ctx, "initialize", map[string]any{
		"protocolVersion": pluginMCPProtocolVersion,
		"capabilities":    map[string]any{},
		"clientInfo": map[string]any{
			"name":    "pudding-core",
			"title":   "Pudding",
			"version": "1.0.0",
		},
	})
	if err != nil {
		return err
	}
	var out struct {
		ProtocolVersion string `json:"protocolVersion"`
	}
	if err := json.Unmarshal(raw, &out); err == nil && strings.TrimSpace(out.ProtocolVersion) != "" {
		c.protocol = strings.TrimSpace(out.ProtocolVersion)
	}
	return c.notify(ctx, "notifications/initialized", nil)
}

func (c *pluginMCPHTTPClient) listTools(ctx context.Context) ([]pluginMCPRemoteTool, error) {
	var tools []pluginMCPRemoteTool
	cursor := ""
	for page := 0; page < 20; page++ {
		params := map[string]any{}
		if cursor != "" {
			params["cursor"] = cursor
		}
		raw, err := c.call(ctx, "tools/list", params)
		if err != nil {
			return nil, err
		}
		var out struct {
			Tools      []pluginMCPRemoteTool `json:"tools"`
			NextCursor string                `json:"nextCursor"`
		}
		if err := json.Unmarshal(raw, &out); err != nil {
			return nil, err
		}
		tools = append(tools, out.Tools...)
		if len(tools) >= pluginMCPMaxTools {
			return tools[:pluginMCPMaxTools], nil
		}
		cursor = strings.TrimSpace(out.NextCursor)
		if cursor == "" {
			return tools, nil
		}
	}
	return tools, nil
}

func (c *pluginMCPHTTPClient) call(ctx context.Context, method string, params any) (json.RawMessage, error) {
	id := fmt.Sprintf("%d", c.runner.nextID.Add(1))
	return c.post(ctx, pluginMCPRPCMessage{
		JSONRPC: "2.0",
		ID:      id,
		Method:  method,
		Params:  params,
	}, id, true)
}

func (c *pluginMCPHTTPClient) notify(ctx context.Context, method string, params any) error {
	_, err := c.post(ctx, pluginMCPRPCMessage{
		JSONRPC: "2.0",
		Method:  method,
		Params:  params,
	}, "", false)
	return err
}

func (c *pluginMCPHTTPClient) post(ctx context.Context, msg pluginMCPRPCMessage, id string, wantResponse bool) (json.RawMessage, error) {
	if c == nil || c.binding == nil {
		return nil, errors.New("mcp endpoint unavailable")
	}
	target, err := c.requestURL()
	if err != nil {
		return nil, err
	}
	body, err := json.Marshal(msg)
	if err != nil {
		return nil, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, target.String(), bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Accept", "application/json, text/event-stream")
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("MCP-Protocol-Version", c.protocol)
	if c.sessionID != "" {
		req.Header.Set("Mcp-Session-Id", c.sessionID)
	}
	if err := applyPluginMCPHeaders(req.Header, c.binding.Endpoint.Headers); err != nil {
		return nil, err
	}
	if err := pluginexec.ApplyEndpointAuth(req.Header, c.binding.Auth); err != nil {
		return nil, err
	}
	if err := pluginexec.ApplyEndpointConnectionHeaders(req.Header, http.MethodPost, c.binding.ConnectionFields, c.binding.ConnectionFieldDefs); err != nil {
		return nil, err
	}
	resp, err := c.client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if sessionID := strings.TrimSpace(resp.Header.Get("Mcp-Session-Id")); sessionID != "" {
		c.sessionID = sessionID
	}
	if !wantResponse {
		if resp.StatusCode < 200 || resp.StatusCode >= 300 {
			return nil, pluginMCPHTTPError(resp)
		}
		io.Copy(io.Discard, io.LimitReader(resp.Body, pluginMCPMaxResponseBytes))
		return nil, nil
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, pluginMCPHTTPError(resp)
	}
	contentType := strings.ToLower(strings.TrimSpace(resp.Header.Get("Content-Type")))
	if strings.HasPrefix(contentType, "text/event-stream") {
		return readPluginMCPSSE(ctx, resp.Body, id)
	}
	data, err := readPluginMCPBody(resp.Body)
	if err != nil {
		return nil, err
	}
	return pluginMCPEnvelopeResult(data, id)
}

func (c *pluginMCPHTTPClient) requestURL() (*url.URL, error) {
	target, err := url.Parse(strings.TrimSpace(c.binding.Endpoint.URL))
	if err != nil {
		return nil, err
	}
	if target.Scheme != "http" && target.Scheme != "https" {
		return nil, fmt.Errorf("mcp endpoint url scheme %q not allowed", target.Scheme)
	}
	if target.Host == "" {
		return nil, errors.New("mcp endpoint url missing host")
	}
	if err := pluginexec.ApplyEndpointConnectionQuery(target, http.MethodPost, c.binding.ConnectionFields, c.binding.ConnectionFieldDefs); err != nil {
		return nil, err
	}
	return target, nil
}

func (c *pluginMCPHTTPClient) close() {}

type pluginMCPStdioClient struct {
	runner   *PluginMCPRunner
	binding  *plugin.EndpointBinding
	protocol string

	mu       sync.Mutex
	started  bool
	cmd      *exec.Cmd
	stdin    io.WriteCloser
	decoder  *json.Decoder
	waitDone chan error
	stderrMu sync.Mutex
	stderr   strings.Builder
}

func (c *pluginMCPStdioClient) initialize(ctx context.Context) error {
	if err := c.start(ctx); err != nil {
		return err
	}
	raw, err := c.call(ctx, "initialize", map[string]any{
		"protocolVersion": pluginMCPProtocolVersion,
		"capabilities":    map[string]any{},
		"clientInfo": map[string]any{
			"name":    "pudding-core",
			"title":   "Pudding",
			"version": "1.0.0",
		},
	})
	if err != nil {
		return err
	}
	var out struct {
		ProtocolVersion string `json:"protocolVersion"`
	}
	if err := json.Unmarshal(raw, &out); err == nil && strings.TrimSpace(out.ProtocolVersion) != "" {
		c.protocol = strings.TrimSpace(out.ProtocolVersion)
	}
	return c.notify(ctx, "notifications/initialized", nil)
}

func (c *pluginMCPStdioClient) listTools(ctx context.Context) ([]pluginMCPRemoteTool, error) {
	var tools []pluginMCPRemoteTool
	cursor := ""
	for page := 0; page < 20; page++ {
		params := map[string]any{}
		if cursor != "" {
			params["cursor"] = cursor
		}
		raw, err := c.call(ctx, "tools/list", params)
		if err != nil {
			return nil, err
		}
		var out struct {
			Tools      []pluginMCPRemoteTool `json:"tools"`
			NextCursor string                `json:"nextCursor"`
		}
		if err := json.Unmarshal(raw, &out); err != nil {
			return nil, err
		}
		tools = append(tools, out.Tools...)
		if len(tools) >= pluginMCPMaxTools {
			return tools[:pluginMCPMaxTools], nil
		}
		cursor = strings.TrimSpace(out.NextCursor)
		if cursor == "" {
			return tools, nil
		}
	}
	return tools, nil
}

func (c *pluginMCPStdioClient) call(ctx context.Context, method string, params any) (json.RawMessage, error) {
	if err := c.start(ctx); err != nil {
		return nil, err
	}
	id := fmt.Sprintf("%d", c.runner.nextID.Add(1))
	if err := c.write(pluginMCPRPCMessage{
		JSONRPC: "2.0",
		ID:      id,
		Method:  method,
		Params:  params,
	}); err != nil {
		return nil, err
	}
	return c.readResponse(ctx, id)
}

func (c *pluginMCPStdioClient) notify(ctx context.Context, method string, params any) error {
	if err := c.start(ctx); err != nil {
		return err
	}
	return c.write(pluginMCPRPCMessage{
		JSONRPC: "2.0",
		Method:  method,
		Params:  params,
	})
}

func (c *pluginMCPStdioClient) start(ctx context.Context) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.started {
		return nil
	}
	if c == nil || c.binding == nil {
		return errors.New("mcp endpoint unavailable")
	}
	command := strings.TrimSpace(c.binding.Endpoint.Command)
	if command == "" {
		return errors.New("stdio mcp endpoint command is required")
	}
	extraEnv, err := pluginexec.ApplyEndpointConnectionEnv(c.binding.Endpoint.Env, c.binding.ConnectionFields, c.binding.ConnectionFieldDefs)
	if err != nil {
		return err
	}
	env, err := pluginMCPStdioEnv(extraEnv)
	if err != nil {
		return err
	}
	commandPath, err := pluginMCPResolveCommand(command, env)
	if err != nil {
		return err
	}
	cmd := exec.CommandContext(ctx, commandPath, c.binding.Endpoint.Args...)
	cmd.Env = env
	stdin, err := cmd.StdinPipe()
	if err != nil {
		return err
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		_ = stdin.Close()
		return err
	}
	stderr, err := cmd.StderrPipe()
	if err != nil {
		_ = stdin.Close()
		_ = stdout.Close()
		return err
	}
	if err := cmd.Start(); err != nil {
		_ = stdin.Close()
		_ = stdout.Close()
		_ = stderr.Close()
		return err
	}
	c.cmd = cmd
	c.stdin = stdin
	c.decoder = json.NewDecoder(stdout)
	c.waitDone = make(chan error, 1)
	c.started = true
	go c.drainStderr(stderr)
	go func() {
		c.waitDone <- cmd.Wait()
		close(c.waitDone)
	}()
	return nil
}

func (c *pluginMCPStdioClient) write(msg pluginMCPRPCMessage) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.stdin == nil {
		return errors.New("stdio mcp stdin unavailable")
	}
	data, err := json.Marshal(msg)
	if err != nil {
		return err
	}
	data = append(data, '\n')
	if _, err := c.stdin.Write(data); err != nil {
		return fmt.Errorf("write stdio mcp request: %w", err)
	}
	return nil
}

func (c *pluginMCPStdioClient) readResponse(ctx context.Context, id string) (json.RawMessage, error) {
	for {
		got, err := c.readEnvelope(ctx)
		if err != nil {
			return nil, err
		}
		if len(got.ID) == 0 {
			continue
		}
		if !pluginMCPIDMatches(got.ID, id) {
			continue
		}
		if got.Error != nil {
			return nil, errors.New(got.Error.Message)
		}
		if got.Result == nil {
			return nil, errors.New("mcp response missing result")
		}
		return got.Result, nil
	}
}

func (c *pluginMCPStdioClient) readEnvelope(ctx context.Context) (rpcEnvelope, error) {
	type decodedEnvelope struct {
		envelope rpcEnvelope
		err      error
	}
	ch := make(chan decodedEnvelope, 1)
	go func() {
		var envelope rpcEnvelope
		err := c.decoder.Decode(&envelope)
		ch <- decodedEnvelope{envelope: envelope, err: err}
	}()
	select {
	case got := <-ch:
		if got.err != nil {
			if detail := c.stderrText(); detail != "" {
				return rpcEnvelope{}, fmt.Errorf("read stdio mcp response: %w; stderr: %s", got.err, detail)
			}
			return rpcEnvelope{}, fmt.Errorf("read stdio mcp response: %w", got.err)
		}
		return got.envelope, nil
	case <-ctx.Done():
		return rpcEnvelope{}, ctx.Err()
	case err, ok := <-c.waitDone:
		if !ok {
			return rpcEnvelope{}, errors.New("stdio mcp process exited")
		}
		if err != nil {
			if detail := c.stderrText(); detail != "" {
				return rpcEnvelope{}, fmt.Errorf("stdio mcp process exited: %w; stderr: %s", err, detail)
			}
			return rpcEnvelope{}, fmt.Errorf("stdio mcp process exited: %w", err)
		}
		return rpcEnvelope{}, errors.New("stdio mcp process exited")
	}
}

func (c *pluginMCPStdioClient) close() {
	c.mu.Lock()
	if c.stdin != nil {
		_ = c.stdin.Close()
		c.stdin = nil
	}
	cmd := c.cmd
	waitDone := c.waitDone
	c.mu.Unlock()
	if cmd == nil || waitDone == nil {
		return
	}
	select {
	case <-waitDone:
	case <-time.After(500 * time.Millisecond):
		if cmd.Process != nil {
			_ = cmd.Process.Kill()
		}
		<-waitDone
	}
}

func (c *pluginMCPStdioClient) drainStderr(stderr io.Reader) {
	data, _ := io.ReadAll(io.LimitReader(stderr, 16*1024))
	if len(data) == 0 {
		return
	}
	c.stderrMu.Lock()
	defer c.stderrMu.Unlock()
	c.stderr.WriteString(string(data))
}

func (c *pluginMCPStdioClient) stderrText() string {
	c.stderrMu.Lock()
	defer c.stderrMu.Unlock()
	return strings.TrimSpace(c.stderr.String())
}

func pluginMCPStdioEnv(extra map[string]string) ([]string, error) {
	// Plugin processes receive only the ordinary command baseline plus values
	// explicitly declared by the endpoint/connection. Do not leak daemon
	// tokens, provider credentials, or unrelated parent-process secrets.
	return commandEnvironment(extra)
}

func pluginMCPResolveCommand(command string, env []string) (string, error) {
	return resolveExecutableFromEnv(command, "", env)
}

func pluginMCPEnvValue(env []string, key string) string {
	for _, item := range env {
		gotKey, value, ok := strings.Cut(item, "=")
		if ok && gotKey == key {
			return value
		}
	}
	return ""
}

type pluginMCPRPCMessage struct {
	JSONRPC string `json:"jsonrpc"`
	ID      string `json:"id,omitempty"`
	Method  string `json:"method"`
	Params  any    `json:"params,omitempty"`
}

func readPluginMCPBody(body io.Reader) ([]byte, error) {
	data, err := io.ReadAll(io.LimitReader(body, pluginMCPMaxResponseBytes+1))
	if err != nil {
		return nil, err
	}
	if len(data) > pluginMCPMaxResponseBytes {
		return nil, fmt.Errorf("mcp response exceeds %d bytes", pluginMCPMaxResponseBytes)
	}
	return data, nil
}

func readPluginMCPSSE(ctx context.Context, body io.Reader, id string) (json.RawMessage, error) {
	scanner := bufio.NewScanner(body)
	scanner.Buffer(make([]byte, 0, 64*1024), pluginMCPMaxResponseBytes)
	var dataLines []string
	dataBytes := 0
	flush := func() (json.RawMessage, bool, error) {
		if len(dataLines) == 0 {
			return nil, false, nil
		}
		data := strings.Join(dataLines, "\n")
		dataLines = nil
		dataBytes = 0
		raw, err := pluginMCPEnvelopeResult([]byte(data), id)
		if err != nil {
			var mismatch pluginMCPIDMismatchError
			if errors.As(err, &mismatch) {
				return nil, false, nil
			}
			return nil, false, err
		}
		return raw, true, nil
	}
	for scanner.Scan() {
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		default:
		}
		line := strings.TrimSuffix(scanner.Text(), "\r")
		if line == "" {
			if raw, ok, err := flush(); ok || err != nil {
				return raw, err
			}
			continue
		}
		if strings.HasPrefix(line, "data:") {
			data := strings.TrimPrefix(line, "data:")
			data = strings.TrimPrefix(data, " ")
			separator := 0
			if len(dataLines) > 0 {
				separator = 1
			}
			if dataBytes+separator+len(data) > pluginMCPMaxResponseBytes {
				return nil, fmt.Errorf("mcp response exceeds %d bytes", pluginMCPMaxResponseBytes)
			}
			dataLines = append(dataLines, data)
			dataBytes += separator + len(data)
		}
	}
	if err := scanner.Err(); err != nil {
		return nil, err
	}
	if raw, ok, err := flush(); ok || err != nil {
		return raw, err
	}
	return nil, errors.New("mcp sse stream ended without response")
}

type pluginMCPIDMismatchError struct{}

func (pluginMCPIDMismatchError) Error() string {
	return "mcp response id did not match request"
}

func pluginMCPEnvelopeResult(data []byte, id string) (json.RawMessage, error) {
	var envelope rpcEnvelope
	if err := json.Unmarshal(data, &envelope); err != nil {
		return nil, err
	}
	if id != "" && !pluginMCPIDMatches(envelope.ID, id) {
		return nil, pluginMCPIDMismatchError{}
	}
	if envelope.Error != nil {
		return nil, errors.New(envelope.Error.Message)
	}
	if envelope.Result == nil {
		return nil, errors.New("mcp response missing result")
	}
	return envelope.Result, nil
}

func pluginMCPIDMatches(raw json.RawMessage, id string) bool {
	if len(raw) == 0 {
		return false
	}
	var s string
	if err := json.Unmarshal(raw, &s); err == nil {
		return s == id
	}
	var n json.Number
	if err := json.Unmarshal(raw, &n); err == nil {
		return n.String() == id
	}
	return false
}

func pluginMCPHTTPError(resp *http.Response) error {
	data, _ := readPluginMCPBody(resp.Body)
	if len(data) > 0 {
		return fmt.Errorf("mcp http %d: %s", resp.StatusCode, strings.TrimSpace(string(data)))
	}
	return fmt.Errorf("mcp http %d", resp.StatusCode)
}

func applyPluginMCPHeaders(headers http.Header, configured map[string]string) error {
	for name, value := range configured {
		name = strings.TrimSpace(name)
		if name == "" {
			continue
		}
		if !plugin.IsAllowedRequestHeaderName(name) {
			return fmt.Errorf("mcp endpoint header %q is invalid", name)
		}
		if !plugin.IsAllowedRequestHeaderValue(value) {
			return fmt.Errorf("mcp endpoint header %q has an invalid value", name)
		}
		headers.Set(name, value)
	}
	return nil
}

func pluginMCPProviderToolName(binding *plugin.EndpointBinding, remoteName string) string {
	// Keep provider-visible names compact; app, connection, and endpoint identity remain in the hash.
	hash := pluginMCPToolHash(binding, remoteName)
	maxToolLen := pluginMCPMaxToolNameLen - len(pluginMCPToolPrefix) - len("__") - len(hash)
	tool := pluginMCPSanitizeNameSegment(remoteName, maxToolLen)
	return pluginMCPToolPrefix + tool + "__" + hash
}

func pluginMCPSanitizeNameSegment(value string, maxLen int) string {
	value = strings.ToLower(strings.TrimSpace(value))
	var b strings.Builder
	lastUnderscore := false
	for _, r := range value {
		ok := (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9')
		if ok {
			b.WriteRune(r)
			lastUnderscore = false
			continue
		}
		if !lastUnderscore {
			b.WriteByte('_')
			lastUnderscore = true
		}
	}
	out := strings.Trim(b.String(), "_")
	if out == "" {
		out = "x"
	}
	if maxLen > 0 && len(out) > maxLen {
		out = strings.TrimRight(out[:maxLen], "_")
		if out == "" {
			out = "x"
		}
	}
	return out
}

func pluginMCPToolHash(binding *plugin.EndpointBinding, remoteName string) string {
	h := fnv.New32a()
	_, _ = h.Write([]byte(binding.PluginID))
	_, _ = h.Write([]byte{0})
	_, _ = h.Write([]byte(binding.ConnectionID))
	_, _ = h.Write([]byte{0})
	_, _ = h.Write([]byte(binding.EndpointName))
	_, _ = h.Write([]byte{0})
	_, _ = h.Write([]byte(remoteName))
	sum := h.Sum(nil)
	return hex.EncodeToString(sum)
}

func pluginMCPToolDescription(binding *plugin.EndpointBinding, tool pluginMCPRemoteTool) string {
	description := strings.TrimSpace(tool.Description)
	if description == "" {
		description = strings.TrimSpace(tool.Title)
	}
	prefix := fmt.Sprintf("MCP tool %q from plugin %q endpoint %q.", strings.TrimSpace(tool.Name), binding.PluginID, binding.EndpointName)
	if description == "" {
		return prefix
	}
	return prefix + " " + description
}

func pluginMCPCacheTTL(defs []provider.ToolDef) time.Duration {
	if len(defs) == 0 {
		return pluginMCPEmptyToolCacheTTL
	}
	return pluginMCPToolCacheTTL
}

func pluginMCPBindingsCacheKey(bindings []*plugin.EndpointBinding) string {
	parts := make([]string, 0, len(bindings))
	for _, binding := range bindings {
		if binding == nil || binding.Endpoint.Kind != plugin.EndpointKindMCP || !pluginMCPSupportedTransport(binding.Endpoint.Transport) {
			continue
		}
		fields := pluginMCPMapSignature(binding.ConnectionFields)
		headers := pluginMCPMapSignature(binding.Endpoint.Headers)
		env := pluginMCPMapSignature(binding.Endpoint.Env)
		args := pluginMCPSliceSignature(binding.Endpoint.Args)
		auth := pluginMCPAuthSignature(binding.Auth)
		fieldDefs, _ := json.Marshal(binding.ConnectionFieldDefs)
		parts = append(parts, strings.Join([]string{
			binding.PluginID,
			binding.ConnectionID,
			binding.EndpointName,
			binding.Endpoint.Transport,
			binding.Endpoint.URL,
			binding.Endpoint.Command,
			args,
			env,
			auth,
			fields,
			string(fieldDefs),
			headers,
		}, "\x00"))
	}
	sort.Strings(parts)
	h := sha256.New()
	for _, part := range parts {
		_, _ = h.Write([]byte(part))
		_, _ = h.Write([]byte{0})
	}
	return hex.EncodeToString(h.Sum(nil))
}

func pluginMCPAuthSignature(auth plugin.Auth) string {
	return strings.Join([]string{
		strings.TrimSpace(auth.MethodID),
		strings.TrimSpace(auth.Type),
		auth.Token,
		auth.AccessToken,
		auth.RefreshToken,
		strings.TrimSpace(auth.TokenType),
		auth.ExpiresAt.UTC().Format(time.RFC3339Nano),
		strings.Join(auth.Scopes, "\x00"),
		auth.Prefix,
		auth.Header,
		auth.Username,
		auth.Password,
	}, "\x00")
}

func pluginMCPSupportedTransport(transport string) bool {
	switch strings.TrimSpace(transport) {
	case plugin.EndpointTransportStreamableHTTP, plugin.EndpointTransportStdio:
		return true
	default:
		return false
	}
}

func pluginMCPMapSignature(values map[string]string) string {
	if len(values) == 0 {
		return ""
	}
	keys := make([]string, 0, len(values))
	for key := range values {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	var b strings.Builder
	for _, key := range keys {
		b.WriteString(key)
		b.WriteByte('=')
		b.WriteString(values[key])
		b.WriteByte('\n')
	}
	return b.String()
}

func pluginMCPSliceSignature(values []string) string {
	if len(values) == 0 {
		return ""
	}
	return strings.Join(values, "\x00")
}

func clonePluginMCPToolDefs(in []provider.ToolDef) []provider.ToolDef {
	if len(in) == 0 {
		return nil
	}
	out := append([]provider.ToolDef(nil), in...)
	for i := range out {
		out[i].InputSchema = plugin.CloneJSON(in[i].InputSchema)
	}
	return out
}

func clonePluginMCPTools(in map[string]pluginMCPDiscoveredTool) map[string]pluginMCPDiscoveredTool {
	if len(in) == 0 {
		return map[string]pluginMCPDiscoveredTool{}
	}
	out := make(map[string]pluginMCPDiscoveredTool, len(in))
	for key, tool := range in {
		out[key] = pluginMCPDiscoveredTool{
			binding:    clonePluginMCPBinding(tool.binding),
			remoteName: tool.remoteName,
		}
	}
	return out
}

func pluginMCPInputSchema(raw json.RawMessage) json.RawMessage {
	if len(raw) == 0 || !json.Valid(raw) || strings.TrimSpace(string(raw)) == "null" {
		return json.RawMessage(`{"type":"object","additionalProperties":true}`)
	}
	return plugin.CloneJSON(raw)
}

func pluginMCPToolResult(call Call, raw json.RawMessage) Result {
	out := Result{CallID: call.CallID, Name: call.Name, Ok: true}
	var decoded struct {
		IsError           bool               `json:"isError"`
		Content           []pluginMCPContent `json:"content"`
		StructuredContent json.RawMessage    `json:"structuredContent"`
	}
	if err := json.Unmarshal(raw, &decoded); err != nil {
		out.Content = string(raw)
		return out
	}
	out.Ok = !decoded.IsError
	parts := make([]string, 0, len(decoded.Content)+1)
	for _, item := range decoded.Content {
		if item.Type == "text" && item.Text != "" {
			parts = append(parts, item.Text)
		}
	}
	if len(decoded.StructuredContent) > 0 && string(decoded.StructuredContent) != "null" {
		parts = append(parts, string(decoded.StructuredContent))
	}
	if len(parts) > 0 {
		out.Content = strings.Join(parts, "\n")
		return out
	}
	out.Content = string(raw)
	return out
}

type pluginMCPContent struct {
	Type string `json:"type"`
	Text string `json:"text"`
}

func clonePluginMCPBinding(in *plugin.EndpointBinding) *plugin.EndpointBinding {
	if in == nil {
		return nil
	}
	out := *in
	out.Auth = plugin.CloneAuth(in.Auth)
	out.ConnectionFields = cloneStringMapForPluginMCP(in.ConnectionFields)
	out.ConnectionFieldDefs = append([]plugin.ConnectionField(nil), in.ConnectionFieldDefs...)
	for i := range out.ConnectionFieldDefs {
		out.ConnectionFieldDefs[i].Inject = append([]plugin.ConnectionFieldInject(nil), in.ConnectionFieldDefs[i].Inject...)
		for j := range out.ConnectionFieldDefs[i].Inject {
			out.ConnectionFieldDefs[i].Inject[j].Methods = append([]string(nil), in.ConnectionFieldDefs[i].Inject[j].Methods...)
		}
	}
	out.Endpoint.Args = append([]string(nil), in.Endpoint.Args...)
	out.Endpoint.Env = cloneStringMapForPluginMCP(in.Endpoint.Env)
	out.Endpoint.Headers = cloneStringMapForPluginMCP(in.Endpoint.Headers)
	return &out
}

func cloneStringMapForPluginMCP(in map[string]string) map[string]string {
	if len(in) == 0 {
		return nil
	}
	out := make(map[string]string, len(in))
	for k, v := range in {
		out[k] = v
	}
	return out
}

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

	"github.com/teatak/pudding-core/internal/app"
	"github.com/teatak/pudding-core/internal/provider"
	"github.com/teatak/pudding-core/internal/store"
)

const (
	appMCPToolPrefix        = "app_mcp__"
	appMCPProtocolVersion   = "2025-06-18"
	appMCPRequestTimeout    = 20 * time.Second
	appMCPDiscoverTimeout   = 8 * time.Second
	appMCPToolCacheTTL      = 5 * time.Minute
	appMCPEmptyToolCacheTTL = 30 * time.Second
	appMCPMaxResponseBytes  = 1 * 1024 * 1024
	appMCPMaxTools          = 300
	appMCPMaxToolNameLen    = 64
)

type AppMCPSource interface {
	ListEndpointBindings(ctx context.Context, kind string) ([]*app.EndpointBinding, error)
}

type AppMCPOption func(*AppMCPRunner)

type AppMCPRunner struct {
	source     AppMCPSource
	httpClient *http.Client
	nextID     atomic.Int64

	mu           sync.Mutex
	sessionDefs  map[string][]provider.ToolDef
	sessionTools map[string]map[string]appMCPDiscoveredTool
	caches       map[string]appMCPToolCache
}

type AppMCPProbeStatus string

const (
	AppMCPProbeAvailable       AppMCPProbeStatus = "available"
	AppMCPProbeUnavailable     AppMCPProbeStatus = "unavailable"
	AppMCPProbeUnsupported     AppMCPProbeStatus = "unsupported"
	AppMCPProbeNeedsConnection AppMCPProbeStatus = "needs_connection"
)

type AppMCPProbeEndpoint struct {
	AppID        string            `json:"appID"`
	EndpointName string            `json:"endpointName"`
	ConnectionID string            `json:"connectionID,omitempty"`
	Transport    string            `json:"transport,omitempty"`
	Configured   bool              `json:"configured,omitempty"`
	Status       AppMCPProbeStatus `json:"status"`
	Error        string            `json:"error,omitempty"`
	Tools        []AppMCPProbeTool `json:"tools,omitempty"`
}

type AppMCPProbeTool struct {
	Name         string          `json:"name"`
	ProviderName string          `json:"providerName,omitempty"`
	Title        string          `json:"title,omitempty"`
	Description  string          `json:"description,omitempty"`
	InputSchema  json.RawMessage `json:"inputSchema,omitempty"`
}

type appMCPDiscoveredTool struct {
	binding    *app.EndpointBinding
	remoteName string
}

type appMCPClient interface {
	initialize(ctx context.Context) error
	listTools(ctx context.Context) ([]appMCPRemoteTool, error)
	call(ctx context.Context, method string, params any) (json.RawMessage, error)
	close()
}

type appMCPToolCache struct {
	key       string
	expiresAt time.Time
	defs      []provider.ToolDef
	tools     map[string]appMCPDiscoveredTool
}

func NewAppMCPRunner(source AppMCPSource, opts ...AppMCPOption) *AppMCPRunner {
	r := &AppMCPRunner{
		source:       source,
		httpClient:   &http.Client{Timeout: appMCPRequestTimeout},
		sessionDefs:  map[string][]provider.ToolDef{},
		sessionTools: map[string]map[string]appMCPDiscoveredTool{},
		caches:       map[string]appMCPToolCache{},
	}
	for _, opt := range opts {
		opt(r)
	}
	return r
}

func WithAppMCPHTTPClient(client *http.Client) AppMCPOption {
	return func(r *AppMCPRunner) {
		if client != nil {
			r.httpClient = client
		}
	}
}

func (r *AppMCPRunner) Definitions(_ context.Context, sessionID string) ([]provider.ToolDef, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	return cloneAppMCPToolDefs(r.sessionDefs[sessionID]), nil
}

func (r *AppMCPRunner) DefinitionsForApps(ctx context.Context, sessionID string, appIDs []string) ([]provider.ToolDef, error) {
	appIDs = normalizeAppIDs(appIDs)
	if len(appIDs) == 0 {
		r.setSessionTools(sessionID, nil, nil)
		return nil, nil
	}
	bindings, err := r.listBindings(ctx)
	if err != nil {
		r.setSessionTools(sessionID, nil, nil)
		return nil, nil
	}
	bindings = filterAppMCPBindings(bindings, appIDs)
	cacheKey := appMCPBindingsCacheKey(bindings)
	if defs, tools, ok := r.cached(cacheKey); ok {
		r.setSessionTools(sessionID, defs, tools)
		return defs, nil
	}
	defs, tools := r.discoverBindings(ctx, bindings)
	r.setSessionTools(sessionID, defs, tools)
	r.mu.Lock()
	r.caches[cacheKey] = appMCPToolCache{
		key:       cacheKey,
		expiresAt: time.Now().Add(appMCPCacheTTL(defs)),
		defs:      cloneAppMCPToolDefs(defs),
		tools:     cloneAppMCPTools(tools),
	}
	r.mu.Unlock()
	return defs, nil
}

func (r *AppMCPRunner) Call(ctx context.Context, call Call) Result {
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
	return appMCPToolResult(call, raw)
}

func (r *AppMCPRunner) lookup(sessionID, name string) (appMCPDiscoveredTool, bool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	tool, ok := r.sessionTools[sessionID][name]
	return tool, ok
}

func (r *AppMCPRunner) setSessionTools(sessionID string, defs []provider.ToolDef, tools map[string]appMCPDiscoveredTool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.sessionDefs[sessionID] = cloneAppMCPToolDefs(defs)
	r.sessionTools[sessionID] = cloneAppMCPTools(tools)
}

func (r *AppMCPRunner) CloseSession(sessionID string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	delete(r.sessionDefs, sessionID)
	delete(r.sessionTools, sessionID)
}

func (r *AppMCPRunner) cached(cacheKey string) ([]provider.ToolDef, map[string]appMCPDiscoveredTool, bool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	cache, ok := r.caches[cacheKey]
	if !ok || cache.key == "" || time.Now().After(cache.expiresAt) {
		delete(r.caches, cacheKey)
		return nil, nil, false
	}
	return cloneAppMCPToolDefs(cache.defs), cloneAppMCPTools(cache.tools), true
}

func normalizeAppIDs(appIDs []string) []string {
	seen := make(map[string]bool, len(appIDs))
	out := make([]string, 0, len(appIDs))
	for _, appID := range appIDs {
		appID = strings.TrimSpace(appID)
		if appID == "" || seen[appID] {
			continue
		}
		seen[appID] = true
		out = append(out, appID)
	}
	sort.Strings(out)
	return out
}

func filterAppMCPBindings(bindings []*app.EndpointBinding, appIDs []string) []*app.EndpointBinding {
	allowed := make(map[string]bool, len(appIDs))
	for _, appID := range appIDs {
		allowed[appID] = true
	}
	out := make([]*app.EndpointBinding, 0, len(bindings))
	for _, binding := range bindings {
		if binding != nil && allowed[binding.AppID] {
			out = append(out, binding)
		}
	}
	return out
}

func (r *AppMCPRunner) listBindings(ctx context.Context) ([]*app.EndpointBinding, error) {
	if r == nil || r.source == nil {
		return nil, nil
	}
	bindings, err := r.source.ListEndpointBindings(ctx, app.EndpointKindMCP)
	if err != nil {
		slog.Warn("app mcp: list endpoint bindings failed", "err", err)
		return nil, err
	}
	return bindings, nil
}

func (r *AppMCPRunner) discoverBindings(ctx context.Context, bindings []*app.EndpointBinding) ([]provider.ToolDef, map[string]appMCPDiscoveredTool) {
	tools := map[string]appMCPDiscoveredTool{}
	defs := make([]provider.ToolDef, 0)
	for _, binding := range bindings {
		if binding == nil || binding.Endpoint.Kind != app.EndpointKindMCP || !appMCPSupportedTransport(binding.Endpoint.Transport) {
			continue
		}
		bindingDefs, bindingTools, err := r.discoverBinding(ctx, binding)
		if err != nil {
			slog.Warn("app mcp: discover endpoint failed", "app", binding.AppID, "endpoint", binding.EndpointName, "connection", binding.ConnectionID, "err", err)
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

func (r *AppMCPRunner) discoverBinding(ctx context.Context, binding *app.EndpointBinding) ([]provider.ToolDef, []appMCPDiscoveredTool, error) {
	remoteTools, err := r.listBindingRemoteTools(ctx, binding)
	if err != nil {
		return nil, nil, err
	}
	defs := make([]provider.ToolDef, 0, len(remoteTools))
	tools := make([]appMCPDiscoveredTool, 0, len(remoteTools))
	for _, remote := range remoteTools {
		remoteName := strings.TrimSpace(remote.Name)
		if remoteName == "" {
			continue
		}
		name := appMCPProviderToolName(binding, remoteName)
		description := appMCPToolDescription(binding, remote)
		inputSchema := remote.InputSchema
		if len(inputSchema) == 0 {
			inputSchema = remote.inputSnake
		}
		defs = append(defs, provider.ToolDef{
			Name:        name,
			Description: description,
			InputSchema: appMCPInputSchema(inputSchema),
			Capability:  store.ModeWork,
			AppID:       binding.AppID,
		})
		tools = append(tools, appMCPDiscoveredTool{
			binding:    cloneAppMCPBinding(binding),
			remoteName: remoteName,
		})
	}
	return defs, tools, nil
}

func (r *AppMCPRunner) ProbeBinding(ctx context.Context, binding *app.EndpointBinding) AppMCPProbeEndpoint {
	out := AppMCPProbeEndpoint{Status: AppMCPProbeUnavailable}
	if binding == nil {
		out.Error = "mcp endpoint unavailable"
		return out
	}
	out.AppID = binding.AppID
	out.EndpointName = binding.EndpointName
	out.ConnectionID = binding.ConnectionID
	out.Transport = strings.TrimSpace(binding.Endpoint.Transport)
	if binding.Endpoint.Kind != app.EndpointKindMCP || !appMCPSupportedTransport(binding.Endpoint.Transport) {
		out.Status = AppMCPProbeUnsupported
		out.Error = fmt.Sprintf("unsupported mcp transport %q", binding.Endpoint.Transport)
		return out
	}
	remoteTools, err := r.listBindingRemoteTools(ctx, binding)
	if err != nil {
		out.Error = err.Error()
		return out
	}
	out.Status = AppMCPProbeAvailable
	out.Tools = make([]AppMCPProbeTool, 0, len(remoteTools))
	for _, remote := range remoteTools {
		remoteName := strings.TrimSpace(remote.Name)
		if remoteName == "" {
			continue
		}
		inputSchema := remote.InputSchema
		if len(inputSchema) == 0 {
			inputSchema = remote.inputSnake
		}
		out.Tools = append(out.Tools, AppMCPProbeTool{
			Name:         remoteName,
			ProviderName: appMCPProviderToolName(binding, remoteName),
			Title:        strings.TrimSpace(remote.Title),
			Description:  strings.TrimSpace(remote.Description),
			InputSchema:  appMCPInputSchema(inputSchema),
		})
	}
	return out
}

func (r *AppMCPRunner) listBindingRemoteTools(ctx context.Context, binding *app.EndpointBinding) ([]appMCPRemoteTool, error) {
	discoverCtx, cancel := context.WithTimeout(ctx, appMCPDiscoverTimeout)
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

func (r *AppMCPRunner) newClient(binding *app.EndpointBinding) (appMCPClient, error) {
	switch strings.TrimSpace(binding.Endpoint.Transport) {
	case app.EndpointTransportStreamableHTTP:
		return r.newStreamableHTTPClient(binding), nil
	case app.EndpointTransportStdio:
		return r.newStdioClient(binding), nil
	default:
		return nil, fmt.Errorf("unsupported mcp transport %q", binding.Endpoint.Transport)
	}
}

func (r *AppMCPRunner) newStreamableHTTPClient(binding *app.EndpointBinding) *appMCPHTTPClient {
	return &appMCPHTTPClient{
		runner:   r,
		binding:  cloneAppMCPBinding(binding),
		client:   r.httpClient,
		protocol: appMCPProtocolVersion,
	}
}

func (r *AppMCPRunner) newStdioClient(binding *app.EndpointBinding) *appMCPStdioClient {
	return &appMCPStdioClient{
		runner:   r,
		binding:  cloneAppMCPBinding(binding),
		protocol: appMCPProtocolVersion,
	}
}

type appMCPRemoteTool struct {
	Name        string          `json:"name"`
	Title       string          `json:"title"`
	Description string          `json:"description"`
	InputSchema json.RawMessage `json:"inputSchema"`
	inputSnake  json.RawMessage
}

func (t *appMCPRemoteTool) UnmarshalJSON(data []byte) error {
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

type appMCPHTTPClient struct {
	runner    *AppMCPRunner
	binding   *app.EndpointBinding
	client    *http.Client
	sessionID string
	protocol  string
}

func (c *appMCPHTTPClient) initialize(ctx context.Context) error {
	raw, err := c.call(ctx, "initialize", map[string]any{
		"protocolVersion": appMCPProtocolVersion,
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

func (c *appMCPHTTPClient) listTools(ctx context.Context) ([]appMCPRemoteTool, error) {
	var tools []appMCPRemoteTool
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
			Tools      []appMCPRemoteTool `json:"tools"`
			NextCursor string             `json:"nextCursor"`
		}
		if err := json.Unmarshal(raw, &out); err != nil {
			return nil, err
		}
		tools = append(tools, out.Tools...)
		if len(tools) >= appMCPMaxTools {
			return tools[:appMCPMaxTools], nil
		}
		cursor = strings.TrimSpace(out.NextCursor)
		if cursor == "" {
			return tools, nil
		}
	}
	return tools, nil
}

func (c *appMCPHTTPClient) call(ctx context.Context, method string, params any) (json.RawMessage, error) {
	id := fmt.Sprintf("%d", c.runner.nextID.Add(1))
	return c.post(ctx, appMCPRPCMessage{
		JSONRPC: "2.0",
		ID:      id,
		Method:  method,
		Params:  params,
	}, id, true)
}

func (c *appMCPHTTPClient) notify(ctx context.Context, method string, params any) error {
	_, err := c.post(ctx, appMCPRPCMessage{
		JSONRPC: "2.0",
		Method:  method,
		Params:  params,
	}, "", false)
	return err
}

func (c *appMCPHTTPClient) post(ctx context.Context, msg appMCPRPCMessage, id string, wantResponse bool) (json.RawMessage, error) {
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
	if err := applyAppMCPHeaders(req.Header, c.binding.Endpoint.Headers); err != nil {
		return nil, err
	}
	if err := applyEndpointAuth(req.Header, c.binding.Auth); err != nil {
		return nil, err
	}
	if err := applyEndpointConnectionHeaders(req.Header, http.MethodPost, c.binding.ConnectionFields, c.binding.ConnectionFieldDefs); err != nil {
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
			return nil, appMCPHTTPError(resp)
		}
		io.Copy(io.Discard, io.LimitReader(resp.Body, appMCPMaxResponseBytes))
		return nil, nil
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, appMCPHTTPError(resp)
	}
	contentType := strings.ToLower(strings.TrimSpace(resp.Header.Get("Content-Type")))
	if strings.HasPrefix(contentType, "text/event-stream") {
		return readAppMCPSSE(ctx, resp.Body, id)
	}
	data, err := readAppMCPBody(resp.Body)
	if err != nil {
		return nil, err
	}
	return appMCPEnvelopeResult(data, id)
}

func (c *appMCPHTTPClient) requestURL() (*url.URL, error) {
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
	if err := applyEndpointConnectionQuery(target, http.MethodPost, c.binding.ConnectionFields, c.binding.ConnectionFieldDefs); err != nil {
		return nil, err
	}
	return target, nil
}

func (c *appMCPHTTPClient) close() {}

type appMCPStdioClient struct {
	runner   *AppMCPRunner
	binding  *app.EndpointBinding
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

func (c *appMCPStdioClient) initialize(ctx context.Context) error {
	if err := c.start(ctx); err != nil {
		return err
	}
	raw, err := c.call(ctx, "initialize", map[string]any{
		"protocolVersion": appMCPProtocolVersion,
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

func (c *appMCPStdioClient) listTools(ctx context.Context) ([]appMCPRemoteTool, error) {
	var tools []appMCPRemoteTool
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
			Tools      []appMCPRemoteTool `json:"tools"`
			NextCursor string             `json:"nextCursor"`
		}
		if err := json.Unmarshal(raw, &out); err != nil {
			return nil, err
		}
		tools = append(tools, out.Tools...)
		if len(tools) >= appMCPMaxTools {
			return tools[:appMCPMaxTools], nil
		}
		cursor = strings.TrimSpace(out.NextCursor)
		if cursor == "" {
			return tools, nil
		}
	}
	return tools, nil
}

func (c *appMCPStdioClient) call(ctx context.Context, method string, params any) (json.RawMessage, error) {
	if err := c.start(ctx); err != nil {
		return nil, err
	}
	id := fmt.Sprintf("%d", c.runner.nextID.Add(1))
	if err := c.write(appMCPRPCMessage{
		JSONRPC: "2.0",
		ID:      id,
		Method:  method,
		Params:  params,
	}); err != nil {
		return nil, err
	}
	return c.readResponse(ctx, id)
}

func (c *appMCPStdioClient) notify(ctx context.Context, method string, params any) error {
	if err := c.start(ctx); err != nil {
		return err
	}
	return c.write(appMCPRPCMessage{
		JSONRPC: "2.0",
		Method:  method,
		Params:  params,
	})
}

func (c *appMCPStdioClient) start(ctx context.Context) error {
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
	extraEnv, err := applyEndpointConnectionEnv(c.binding.Endpoint.Env, c.binding.ConnectionFields, c.binding.ConnectionFieldDefs)
	if err != nil {
		return err
	}
	env, err := appMCPStdioEnv(extraEnv)
	if err != nil {
		return err
	}
	commandPath, err := appMCPResolveCommand(command, env)
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

func (c *appMCPStdioClient) write(msg appMCPRPCMessage) error {
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

func (c *appMCPStdioClient) readResponse(ctx context.Context, id string) (json.RawMessage, error) {
	for {
		got, err := c.readEnvelope(ctx)
		if err != nil {
			return nil, err
		}
		if len(got.ID) == 0 {
			continue
		}
		if !appMCPIDMatches(got.ID, id) {
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

func (c *appMCPStdioClient) readEnvelope(ctx context.Context) (rpcEnvelope, error) {
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

func (c *appMCPStdioClient) close() {
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

func (c *appMCPStdioClient) drainStderr(stderr io.Reader) {
	data, _ := io.ReadAll(io.LimitReader(stderr, 16*1024))
	if len(data) == 0 {
		return
	}
	c.stderrMu.Lock()
	defer c.stderrMu.Unlock()
	c.stderr.WriteString(string(data))
}

func (c *appMCPStdioClient) stderrText() string {
	c.stderrMu.Lock()
	defer c.stderrMu.Unlock()
	return strings.TrimSpace(c.stderr.String())
}

func appMCPStdioEnv(extra map[string]string) ([]string, error) {
	// App processes receive only the ordinary command baseline plus values
	// explicitly declared by the endpoint/connection. Do not leak daemon
	// tokens, provider credentials, or unrelated parent-process secrets.
	return commandEnvironment(extra)
}

func appMCPResolveCommand(command string, env []string) (string, error) {
	return resolveExecutableFromEnv(command, "", env)
}

func appMCPEnvValue(env []string, key string) string {
	for _, item := range env {
		gotKey, value, ok := strings.Cut(item, "=")
		if ok && gotKey == key {
			return value
		}
	}
	return ""
}

type appMCPRPCMessage struct {
	JSONRPC string `json:"jsonrpc"`
	ID      string `json:"id,omitempty"`
	Method  string `json:"method"`
	Params  any    `json:"params,omitempty"`
}

func readAppMCPBody(body io.Reader) ([]byte, error) {
	data, err := io.ReadAll(io.LimitReader(body, appMCPMaxResponseBytes+1))
	if err != nil {
		return nil, err
	}
	if len(data) > appMCPMaxResponseBytes {
		return nil, fmt.Errorf("mcp response exceeds %d bytes", appMCPMaxResponseBytes)
	}
	return data, nil
}

func readAppMCPSSE(ctx context.Context, body io.Reader, id string) (json.RawMessage, error) {
	scanner := bufio.NewScanner(body)
	scanner.Buffer(make([]byte, 0, 64*1024), appMCPMaxResponseBytes)
	var dataLines []string
	dataBytes := 0
	flush := func() (json.RawMessage, bool, error) {
		if len(dataLines) == 0 {
			return nil, false, nil
		}
		data := strings.Join(dataLines, "\n")
		dataLines = nil
		dataBytes = 0
		raw, err := appMCPEnvelopeResult([]byte(data), id)
		if err != nil {
			var mismatch appMCPIDMismatchError
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
			if dataBytes+separator+len(data) > appMCPMaxResponseBytes {
				return nil, fmt.Errorf("mcp response exceeds %d bytes", appMCPMaxResponseBytes)
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

type appMCPIDMismatchError struct{}

func (appMCPIDMismatchError) Error() string {
	return "mcp response id did not match request"
}

func appMCPEnvelopeResult(data []byte, id string) (json.RawMessage, error) {
	var envelope rpcEnvelope
	if err := json.Unmarshal(data, &envelope); err != nil {
		return nil, err
	}
	if id != "" && !appMCPIDMatches(envelope.ID, id) {
		return nil, appMCPIDMismatchError{}
	}
	if envelope.Error != nil {
		return nil, errors.New(envelope.Error.Message)
	}
	if envelope.Result == nil {
		return nil, errors.New("mcp response missing result")
	}
	return envelope.Result, nil
}

func appMCPIDMatches(raw json.RawMessage, id string) bool {
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

func appMCPHTTPError(resp *http.Response) error {
	data, _ := readAppMCPBody(resp.Body)
	if len(data) > 0 {
		return fmt.Errorf("mcp http %d: %s", resp.StatusCode, strings.TrimSpace(string(data)))
	}
	return fmt.Errorf("mcp http %d", resp.StatusCode)
}

func applyAppMCPHeaders(headers http.Header, configured map[string]string) error {
	for name, value := range configured {
		name = strings.TrimSpace(name)
		if name == "" {
			continue
		}
		if !app.IsAllowedRequestHeaderName(name) {
			return fmt.Errorf("mcp endpoint header %q is invalid", name)
		}
		if !app.IsAllowedRequestHeaderValue(value) {
			return fmt.Errorf("mcp endpoint header %q has an invalid value", name)
		}
		headers.Set(name, value)
	}
	return nil
}

func appMCPProviderToolName(binding *app.EndpointBinding, remoteName string) string {
	// Keep provider-visible names compact; app, connection, and endpoint identity remain in the hash.
	hash := appMCPToolHash(binding, remoteName)
	maxToolLen := appMCPMaxToolNameLen - len(appMCPToolPrefix) - len("__") - len(hash)
	tool := appMCPSanitizeNameSegment(remoteName, maxToolLen)
	return appMCPToolPrefix + tool + "__" + hash
}

func appMCPSanitizeNameSegment(value string, maxLen int) string {
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

func appMCPToolHash(binding *app.EndpointBinding, remoteName string) string {
	h := fnv.New32a()
	_, _ = h.Write([]byte(binding.AppID))
	_, _ = h.Write([]byte{0})
	_, _ = h.Write([]byte(binding.ConnectionID))
	_, _ = h.Write([]byte{0})
	_, _ = h.Write([]byte(binding.EndpointName))
	_, _ = h.Write([]byte{0})
	_, _ = h.Write([]byte(remoteName))
	sum := h.Sum(nil)
	return hex.EncodeToString(sum)
}

func appMCPToolDescription(binding *app.EndpointBinding, tool appMCPRemoteTool) string {
	description := strings.TrimSpace(tool.Description)
	if description == "" {
		description = strings.TrimSpace(tool.Title)
	}
	prefix := fmt.Sprintf("MCP tool %q from app %q endpoint %q.", strings.TrimSpace(tool.Name), binding.AppID, binding.EndpointName)
	if description == "" {
		return prefix
	}
	return prefix + " " + description
}

func appMCPCacheTTL(defs []provider.ToolDef) time.Duration {
	if len(defs) == 0 {
		return appMCPEmptyToolCacheTTL
	}
	return appMCPToolCacheTTL
}

func appMCPBindingsCacheKey(bindings []*app.EndpointBinding) string {
	parts := make([]string, 0, len(bindings))
	for _, binding := range bindings {
		if binding == nil || binding.Endpoint.Kind != app.EndpointKindMCP || !appMCPSupportedTransport(binding.Endpoint.Transport) {
			continue
		}
		fields := appMCPMapSignature(binding.ConnectionFields)
		headers := appMCPMapSignature(binding.Endpoint.Headers)
		env := appMCPMapSignature(binding.Endpoint.Env)
		args := appMCPSliceSignature(binding.Endpoint.Args)
		auth := appMCPAuthSignature(binding.Auth)
		fieldDefs, _ := json.Marshal(binding.ConnectionFieldDefs)
		parts = append(parts, strings.Join([]string{
			binding.AppID,
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

func appMCPAuthSignature(auth app.Auth) string {
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

func appMCPSupportedTransport(transport string) bool {
	switch strings.TrimSpace(transport) {
	case app.EndpointTransportStreamableHTTP, app.EndpointTransportStdio:
		return true
	default:
		return false
	}
}

func appMCPMapSignature(values map[string]string) string {
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

func appMCPSliceSignature(values []string) string {
	if len(values) == 0 {
		return ""
	}
	return strings.Join(values, "\x00")
}

func cloneAppMCPToolDefs(in []provider.ToolDef) []provider.ToolDef {
	if len(in) == 0 {
		return nil
	}
	out := append([]provider.ToolDef(nil), in...)
	for i := range out {
		out[i].InputSchema = app.CloneJSON(in[i].InputSchema)
	}
	return out
}

func cloneAppMCPTools(in map[string]appMCPDiscoveredTool) map[string]appMCPDiscoveredTool {
	if len(in) == 0 {
		return map[string]appMCPDiscoveredTool{}
	}
	out := make(map[string]appMCPDiscoveredTool, len(in))
	for key, tool := range in {
		out[key] = appMCPDiscoveredTool{
			binding:    cloneAppMCPBinding(tool.binding),
			remoteName: tool.remoteName,
		}
	}
	return out
}

func appMCPInputSchema(raw json.RawMessage) json.RawMessage {
	if len(raw) == 0 || !json.Valid(raw) || strings.TrimSpace(string(raw)) == "null" {
		return json.RawMessage(`{"type":"object","additionalProperties":true}`)
	}
	return app.CloneJSON(raw)
}

func appMCPToolResult(call Call, raw json.RawMessage) Result {
	out := Result{CallID: call.CallID, Name: call.Name, Ok: true}
	var decoded struct {
		IsError           bool            `json:"isError"`
		Content           []appMCPContent `json:"content"`
		StructuredContent json.RawMessage `json:"structuredContent"`
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

type appMCPContent struct {
	Type string `json:"type"`
	Text string `json:"text"`
}

func cloneAppMCPBinding(in *app.EndpointBinding) *app.EndpointBinding {
	if in == nil {
		return nil
	}
	out := *in
	out.Auth = app.CloneAuth(in.Auth)
	out.ConnectionFields = cloneStringMapForAppMCP(in.ConnectionFields)
	out.ConnectionFieldDefs = append([]app.ConnectionField(nil), in.ConnectionFieldDefs...)
	for i := range out.ConnectionFieldDefs {
		out.ConnectionFieldDefs[i].Inject = append([]app.ConnectionFieldInject(nil), in.ConnectionFieldDefs[i].Inject...)
		for j := range out.ConnectionFieldDefs[i].Inject {
			out.ConnectionFieldDefs[i].Inject[j].Methods = append([]string(nil), in.ConnectionFieldDefs[i].Inject[j].Methods...)
		}
	}
	out.Endpoint.Args = append([]string(nil), in.Endpoint.Args...)
	out.Endpoint.Env = cloneStringMapForAppMCP(in.Endpoint.Env)
	out.Endpoint.Headers = cloneStringMapForAppMCP(in.Endpoint.Headers)
	return &out
}

func cloneStringMapForAppMCP(in map[string]string) map[string]string {
	if len(in) == 0 {
		return nil
	}
	out := make(map[string]string, len(in))
	for k, v := range in {
		out[k] = v
	}
	return out
}

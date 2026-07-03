// Package google implements the Gemini native streaming protocol
// (generateContent / streamGenerateContent, v1beta)。
//
// 边界与 openai 实现一致(AGENTS.md 硬约束 9 / 17):只产模型流、
// 无跨 turn 状态、不做流级重试;终止 chunk 阻塞发送。
package google

import (
	"bufio"
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/teatak/pudding-core/internal/provider"
)

const defaultBaseURL = "https://generativelanguage.googleapis.com"

type Config struct {
	BaseURL    string // 默认官方端点,留空即可
	APIKey     string
	HTTPClient *http.Client
}

type Client struct {
	baseURL string
	apiKey  string
	http    *http.Client
}

func New(cfg Config) *Client {
	base := strings.TrimRight(cfg.BaseURL, "/")
	if base == "" {
		base = defaultBaseURL
	}
	return &Client{baseURL: base, apiKey: cfg.APIKey, http: httpClient(cfg.HTTPClient)}
}

var _ provider.Client = (*Client)(nil)

func (c *Client) Name() string { return "google" }

func (c *Client) Stream(ctx context.Context, req provider.Request) (<-chan provider.Chunk, error) {
	out := make(chan provider.Chunk)
	go func() {
		defer close(out)
		if err := c.stream(ctx, req, out); err != nil {
			out <- provider.Chunk{Err: err}
		}
	}()
	return out, nil
}

func (c *Client) stream(ctx context.Context, req provider.Request, out chan<- provider.Chunk) error {
	httpReq, err := c.newRequest(ctx, req)
	if err != nil {
		return err
	}
	resp, err := c.http.Do(httpReq)
	if err != nil {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("google: status %d: %s", resp.StatusCode, redact(bodySummary(resp.Body), c.apiKey))
	}
	return readSSE(ctx, resp.Body, out)
}

// Gemini 协议形状:system_instruction 独立字段,对话角色为 user / model。
type generateRequest struct {
	SystemInstruction *content          `json:"system_instruction,omitempty"`
	Contents          []content         `json:"contents"`
	GenerationConfig  *generationConfig `json:"generationConfig,omitempty"`
	Tools             []tool            `json:"tools,omitempty"`
}

type tool struct {
	FunctionDeclarations []functionDeclaration `json:"functionDeclarations,omitempty"`
}

type functionDeclaration struct {
	Name        string          `json:"name"`
	Description string          `json:"description,omitempty"`
	Parameters  json.RawMessage `json:"parameters,omitempty"`
}

type generationConfig struct {
	Temperature     *float64        `json:"temperature,omitempty"`
	MaxOutputTokens *int            `json:"maxOutputTokens,omitempty"`
	ThinkingConfig  *thinkingConfig `json:"thinkingConfig,omitempty"`
}

type thinkingConfig struct {
	IncludeThoughts bool   `json:"includeThoughts,omitempty"`
	ThinkingBudget  *int   `json:"thinkingBudget,omitempty"`
	ThinkingLevel   string `json:"thinkingLevel,omitempty"`
}

type content struct {
	Role  string `json:"role,omitempty"`
	Parts []part `json:"parts"`
}

type part struct {
	Text string `json:"text,omitempty"`
	// Thought 标记 3.x thinking 摘要帧,不属于答案正文,解析时跳过。
	// 同帧可能携带 thoughtSignature(加密推理签名,多轮工具调用才需要回传,
	// text-only 阶段忽略);字段缺省序列化时不发出,新老协议同一形状。
	Thought          bool              `json:"thought,omitempty"`
	FunctionCall     *functionCall     `json:"functionCall,omitempty"`
	FunctionResponse *functionResponse `json:"functionResponse,omitempty"`
	InlineData       *inlineData       `json:"inlineData,omitempty"`
}

type inlineData struct {
	MIMEType string `json:"mimeType"`
	Data     string `json:"data"`
}

type functionCall struct {
	Name string          `json:"name"`
	Args json.RawMessage `json:"args,omitempty"`
}

type functionResponse struct {
	Name     string          `json:"name"`
	Response json.RawMessage `json:"response,omitempty"`
}

func (c *Client) newRequest(ctx context.Context, req provider.Request) (*http.Request, error) {
	if req.Model == "" {
		return nil, errors.New("google: model is required")
	}
	body := generateRequest{Contents: make([]content, 0, len(req.Messages))}
	var gen generationConfig
	opts := req.Config.GoogleOptions()
	if v, ok := provider.FloatOption(opts, "temperature"); ok {
		gen.Temperature = &v
	}
	if v, ok := req.Config.MaxOutputTokens(); ok {
		gen.MaxOutputTokens = &v
	} else if v, ok := provider.IntOption(opts, "maxOutputTokens", "max_output_tokens", "max_tokens"); ok {
		gen.MaxOutputTokens = &v
	}
	if thinking := googleThinkingConfig(req.Model, opts); thinking != nil {
		gen.ThinkingConfig = thinking
	}
	if gen.Temperature != nil || gen.MaxOutputTokens != nil || gen.ThinkingConfig != nil {
		body.GenerationConfig = &gen
	}
	if req.System != "" {
		body.SystemInstruction = &content{Parts: []part{{Text: req.System}}}
	}
	if len(req.Tools) > 0 {
		declarations := make([]functionDeclaration, 0, len(req.Tools))
		for _, t := range req.Tools {
			declarations = append(declarations, functionDeclaration{
				Name:        t.Name,
				Description: t.Description,
				Parameters:  sanitizeToolParameters(t.InputSchema),
			})
		}
		body.Tools = []tool{{FunctionDeclarations: declarations}}
	}
	body.Contents = append(body.Contents, contentsForMessages(req.Messages)...)
	var buf bytes.Buffer
	if err := json.NewEncoder(&buf).Encode(body); err != nil {
		return nil, err
	}
	endpoint := fmt.Sprintf("%s/v1beta/models/%s:streamGenerateContent?alt=sse", c.baseURL, url.PathEscape(req.Model))
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, &buf)
	if err != nil {
		return nil, err
	}
	httpReq.Header.Set("Content-Type", "application/json")
	if c.apiKey != "" {
		httpReq.Header.Set("x-goog-api-key", c.apiKey)
	}
	return httpReq, nil
}

func googleThinkingConfig(model string, opts map[string]any) *thinkingConfig {
	thinking, _ := mapOption(opts, "thinking", "thinkingConfig", "thinking_config")
	includeThoughts, includeSet := boolOption(thinking, "include_thoughts", "includeThoughts")
	if !includeSet {
		includeThoughts, includeSet = boolOption(opts, "include_thoughts", "includeThoughts")
	}
	level, _ := provider.StringOption(thinking, "level", "thinkingLevel", "thinking_level")
	if level == "" {
		level, _ = provider.StringOption(opts, "thinkingLevel", "thinking_level")
	}
	budget, budgetSet := provider.IntOption(thinking, "budget", "thinkingBudget", "thinking_budget")
	if !budgetSet {
		budget, budgetSet = provider.IntOption(opts, "thinkingBudget", "thinking_budget")
	}
	if !includeSet && level == "" && !budgetSet {
		return nil
	}
	cfg := &thinkingConfig{IncludeThoughts: includeThoughts || !includeSet}
	if usesGemini3ThinkingLevel(model) {
		if parsed := parseThinkingLevel(level); parsed != "" {
			cfg.ThinkingLevel = parsed
		}
		return cfg
	}
	if budgetSet {
		cfg.ThinkingBudget = &budget
		return cfg
	}
	budget = thinkingBudgetFromLevel(level)
	cfg.ThinkingBudget = &budget
	return cfg
}

func mapOption(opts map[string]any, names ...string) (map[string]any, bool) {
	for _, name := range names {
		value, ok := opts[name]
		if !ok || value == nil {
			continue
		}
		if typed, ok := value.(map[string]any); ok {
			return typed, true
		}
	}
	return nil, false
}

func boolOption(opts map[string]any, names ...string) (bool, bool) {
	for _, name := range names {
		value, ok := opts[name]
		if !ok || value == nil {
			continue
		}
		switch typed := value.(type) {
		case bool:
			return typed, true
		case string:
			switch strings.ToLower(strings.TrimSpace(typed)) {
			case "true", "1", "yes", "on":
				return true, true
			case "false", "0", "no", "off":
				return false, true
			}
		}
	}
	return false, false
}

func usesGemini3ThinkingLevel(model string) bool {
	m := strings.ToLower(strings.TrimSpace(model))
	m = strings.TrimPrefix(m, "models/")
	switch m {
	case "gemini-flash-latest", "gemini-pro-latest", "gemini-flash-lite-latest":
		return true
	default:
		return strings.HasPrefix(m, "gemini-3")
	}
}

func parseThinkingLevel(level string) string {
	switch strings.ToLower(strings.TrimSpace(level)) {
	case "minimal":
		return "MINIMAL"
	case "low":
		return "LOW"
	case "medium":
		return "MEDIUM"
	case "high":
		return "HIGH"
	default:
		return ""
	}
}

func thinkingBudgetFromLevel(level string) int {
	switch strings.ToLower(strings.TrimSpace(level)) {
	case "minimal":
		return 128
	case "low":
		return 1024
	case "medium":
		return 8192
	case "high":
		return 24576
	default:
		return -1
	}
}

func sanitizeToolParameters(schema json.RawMessage) json.RawMessage {
	var value any
	if err := json.Unmarshal(schema, &value); err != nil {
		return schema
	}
	cleanValue, ok := sanitizeSchemaValue(value).(map[string]any)
	if !ok || len(cleanValue) == 0 {
		cleanValue = map[string]any{"type": "object"}
	}
	clean, err := json.Marshal(cleanValue)
	if err != nil {
		return schema
	}
	return clean
}

// Gemini function declarations accept only a subset of JSON Schema. This mirrors
// the old project adapter: keep the structural fields Google understands and
// drop JSON Schema extras such as additionalProperties/default/oneOf.
func sanitizeSchemaValue(value any) any {
	switch typed := value.(type) {
	case map[string]any:
		out := make(map[string]any)
		if schemaType := stringValue(typed["type"]); schemaType != "" {
			out["type"] = strings.ToLower(schemaType)
		}
		if description := stringValue(typed["description"]); description != "" {
			out["description"] = description
		}
		if required := stringListValue(typed["required"]); len(required) > 0 {
			out["required"] = required
		}
		if enum := scalarListValue(typed["enum"]); len(enum) > 0 {
			out["enum"] = enum
		}
		if props, ok := typed["properties"].(map[string]any); ok {
			cleanProps := make(map[string]any, len(props))
			for name, prop := range props {
				if cleanProp, ok := sanitizeSchemaValue(prop).(map[string]any); ok {
					cleanProps[name] = cleanProp
				}
			}
			if len(cleanProps) > 0 {
				out["properties"] = cleanProps
			}
		}
		if items, ok := sanitizeSchemaValue(typed["items"]).(map[string]any); ok && len(items) > 0 {
			out["items"] = items
		}
		if _, ok := out["type"]; !ok {
			out["type"] = "object"
		}
		return out
	default:
		return nil
	}
}

func stringValue(value any) string {
	s, _ := value.(string)
	return strings.TrimSpace(s)
}

func stringListValue(value any) []string {
	items, ok := value.([]any)
	if !ok {
		return nil
	}
	out := make([]string, 0, len(items))
	for _, item := range items {
		if s := stringValue(item); s != "" {
			out = append(out, s)
		}
	}
	return out
}

func scalarListValue(value any) []any {
	items, ok := value.([]any)
	if !ok {
		return nil
	}
	out := make([]any, 0, len(items))
	for _, item := range items {
		switch item.(type) {
		case string, float64, bool:
			out = append(out, item)
		}
	}
	return out
}

type streamFrame struct {
	Candidates []struct {
		Content struct {
			Parts []part `json:"parts"`
		} `json:"content"`
		FinishReason string `json:"finishReason"`
	} `json:"candidates"`
	PromptFeedback *struct {
		BlockReason string `json:"blockReason"`
	} `json:"promptFeedback"`
	UsageMetadata *usageMetadata `json:"usageMetadata"`
}

type usageMetadata struct {
	PromptTokenCount        int `json:"promptTokenCount"`
	CandidatesTokenCount    int `json:"candidatesTokenCount"`
	CachedContentTokenCount int `json:"cachedContentTokenCount"`
	ThoughtsTokenCount      int `json:"thoughtsTokenCount"`
}

// readSSE 解析 Gemini 的 SSE 流。协议没有 [DONE] 哨兵,
// 以末帧的 finishReason 标记收尾,EOF 时据此判定 Done 或异常截断。
func readSSE(ctx context.Context, body io.Reader, out chan<- provider.Chunk) error {
	scanner := bufio.NewScanner(body)
	scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	sawFinish := false
	finish := provider.FinishStop
	var latestUsage *usageMetadata
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || !strings.HasPrefix(line, "data:") {
			continue
		}
		data := strings.TrimSpace(strings.TrimPrefix(line, "data:"))
		var frame streamFrame
		if err := json.Unmarshal([]byte(data), &frame); err != nil {
			return fmt.Errorf("google: parse stream frame: %w", err)
		}
		if frame.PromptFeedback != nil && frame.PromptFeedback.BlockReason != "" {
			return fmt.Errorf("google: prompt blocked: %s", frame.PromptFeedback.BlockReason)
		}
		if frame.UsageMetadata != nil {
			cp := *frame.UsageMetadata
			latestUsage = &cp
		}
		for _, cand := range frame.Candidates {
			for partIndex, p := range cand.Content.Parts {
				if p.Thought {
					if p.Text != "" && !emitChunk(ctx, out, provider.Chunk{Part: provider.PartThought, Delta: p.Text}) {
						return ctx.Err()
					}
					continue
				}
				if p.Text != "" {
					if !emitChunk(ctx, out, provider.Chunk{Part: provider.PartText, Delta: p.Text}) {
						return ctx.Err()
					}
				}
				if p.FunctionCall != nil {
					finish = provider.FinishToolCalls
					if !emitChunk(ctx, out, provider.Chunk{Tool: &provider.ToolCallChunk{
						Index:     partIndex,
						Name:      p.FunctionCall.Name,
						ArgsDelta: string(p.FunctionCall.Args),
					}}) {
						return ctx.Err()
					}
				}
			}
			if cand.FinishReason != "" {
				sawFinish = true
				if cand.FinishReason != "STOP" && cand.FinishReason != "MAX_TOKENS" {
					return fmt.Errorf("google: finished with reason %s", cand.FinishReason)
				}
			}
		}
	}
	if err := scanner.Err(); err != nil {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		return err
	}
	if ctx.Err() != nil {
		return ctx.Err()
	}
	if !sawFinish {
		return errors.New("google: stream ended without finish reason")
	}
	if latestUsage != nil {
		usage := googleUsageInfo(*latestUsage)
		if !usage.Empty() && !emitChunk(ctx, out, provider.Chunk{Usage: &usage}) {
			return ctx.Err()
		}
	}
	out <- provider.Chunk{Done: true, Finish: finish} // 终止 chunk 阻塞发送,消费方 drain 到 close
	return nil
}

func googleUsageInfo(u usageMetadata) provider.UsageInfo {
	cached := clampUsage(u.CachedContentTokenCount)
	input := clampUsage(u.PromptTokenCount - cached)
	return provider.UsageInfo{
		InputUncachedTokens:   input,
		InputCachedTokens:     cached,
		OutputContentTokens:   clampUsage(u.CandidatesTokenCount),
		OutputReasoningTokens: clampUsage(u.ThoughtsTokenCount),
	}
}

func clampUsage(v int) int {
	if v < 0 {
		return 0
	}
	return v
}

func emitChunk(ctx context.Context, out chan<- provider.Chunk, chunk provider.Chunk) bool {
	select {
	case out <- chunk:
		return true
	case <-ctx.Done():
		return false
	}
}

func messageText(msg provider.Message) string {
	if len(msg.Parts) == 0 {
		return msg.Text
	}
	var b strings.Builder
	for _, part := range msg.Parts {
		if part.Type == "" || part.Type == provider.PartText {
			b.WriteString(part.Text)
		}
	}
	if b.Len() == 0 {
		return msg.Text
	}
	return b.String()
}

func contentsForMessages(messages []provider.Message) []content {
	var out []content
	toolNames := map[string]string{}
	for _, msg := range messages {
		out = append(out, contentsForMessage(msg, toolNames)...)
	}
	return out
}

func contentsForMessage(msg provider.Message, toolNames map[string]string) []content {
	role := "user"
	if msg.Role == provider.RoleAssistant {
		role = "model"
	}
	if len(msg.Parts) == 0 {
		return []content{{Role: role, Parts: []part{{Text: msg.Text}}}}
	}
	var out []content
	var parts []part
	flush := func(flushRole string) {
		if len(parts) == 0 {
			return
		}
		out = append(out, content{Role: flushRole, Parts: parts})
		parts = nil
	}
	for _, p := range msg.Parts {
		switch p.Type {
		case "", provider.PartText:
			if p.Text != "" {
				parts = append(parts, part{Text: p.Text})
			}
		case provider.PartImage, provider.PartAudio:
			if len(p.Data) > 0 {
				parts = append(parts, part{InlineData: &inlineData{MIMEType: p.MIME, Data: base64.StdEncoding.EncodeToString(p.Data)}})
			}
		case provider.PartToolUse:
			args := p.Args
			if len(args) == 0 {
				args = json.RawMessage(`{}`)
			}
			if p.CallID != "" && p.Name != "" {
				toolNames[p.CallID] = p.Name
			}
			parts = append(parts, part{FunctionCall: &functionCall{
				Name: p.Name,
				Args: append(json.RawMessage(nil), args...),
			}})
		case provider.PartToolResult:
			flush(role)
			name := p.Name
			if name == "" {
				name = toolNames[p.CallID]
			}
			response, err := json.Marshal(map[string]any{
				"ok":      p.Ok,
				"content": p.Content,
			})
			if err != nil {
				response = json.RawMessage(`{"ok":false,"content":"failed to encode tool result"}`)
			}
			parts = append(parts, part{FunctionResponse: &functionResponse{
				Name:     name,
				Response: response,
			}})
			flush("user")
		}
	}
	flush(role)
	if len(out) == 0 {
		out = append(out, content{Role: role, Parts: []part{{Text: msg.Text}}})
	}
	return out
}

// ListModels 拉取支持 generateContent 的模型目录。包级函数,
// 不进 provider.Client 流式契约(与 openai.ListModels 同理)。
func ListModels(ctx context.Context, cfg Config) ([]string, error) {
	base := strings.TrimRight(cfg.BaseURL, "/")
	if base == "" {
		base = defaultBaseURL
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, base+"/v1beta/models?pageSize=200", nil)
	if err != nil {
		return nil, err
	}
	if cfg.APIKey != "" {
		req.Header.Set("x-goog-api-key", cfg.APIKey)
	}
	resp, err := httpClient(cfg.HTTPClient).Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("google: status %d: %s", resp.StatusCode, redact(bodySummary(resp.Body), cfg.APIKey))
	}
	var payload struct {
		Models []struct {
			Name    string   `json:"name"`
			Methods []string `json:"supportedGenerationMethods"`
		} `json:"models"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&payload); err != nil {
		return nil, fmt.Errorf("google: parse models: %w", err)
	}
	var models []string
	for _, m := range payload.Models {
		supported := false
		for _, method := range m.Methods {
			if method == "generateContent" {
				supported = true
				break
			}
		}
		if supported {
			models = append(models, strings.TrimPrefix(m.Name, "models/"))
		}
	}
	return models, nil
}

func bodySummary(r io.Reader) string {
	b, err := io.ReadAll(io.LimitReader(r, 2048))
	if err != nil {
		return err.Error()
	}
	return strings.TrimSpace(string(b))
}

func redact(s, secret string) string {
	if secret == "" {
		return s
	}
	return strings.ReplaceAll(s, secret, "[redacted]")
}

func httpClient(client *http.Client) *http.Client {
	if client != nil {
		return client
	}
	dialer := &net.Dialer{Timeout: 30 * time.Second, KeepAlive: 30 * time.Second}
	return &http.Client{
		Transport: &http.Transport{
			Proxy:                 http.ProxyFromEnvironment,
			DialContext:           dialer.DialContext,
			ForceAttemptHTTP2:     true,
			MaxIdleConns:          100,
			IdleConnTimeout:       90 * time.Second,
			TLSHandshakeTimeout:   10 * time.Second,
			ResponseHeaderTimeout: 30 * time.Second,
			ExpectContinueTimeout: 1 * time.Second,
		},
	}
}

// Package anthropic implements the Anthropic Messages API streaming
// protocol(POST /v1/messages,stream: true)。
//
// 边界与 openai / google 实现一致(AGENTS.md 硬约束 9 / 17):只产模型流和
// provider-native continuation、不在 client 内保存状态、不做流级重试;
// 终止 chunk 阻塞发送。
package anthropic

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
	"strings"
	"time"

	"github.com/teatak/pudding-core/internal/provider"
)

const (
	defaultBaseURL = "https://api.anthropic.com"
	// anthropicVersion 是 Messages API 的必填版本头(日期固定,非 SDK 版本)。
	anthropicVersion = "2023-06-01"
	// defaultMaxTokens:Messages API 的 max_tokens 必填。当前在售模型族
	// (Sonnet/Haiku 4.5+ 64K,Opus 4.6+ 128K)输出上限都 ≥64K,
	// 流式下取 64K 给长回复留足空间;远古模型(8K 上限)会 400,
	// 报错信息明确,可换模型解决。
	defaultMaxTokens = 64000
)

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

func (c *Client) Name() string { return "anthropic" }

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
		return fmt.Errorf("anthropic: status %d: %s", resp.StatusCode, redact(bodySummary(resp.Body), c.apiKey))
	}
	return readSSE(ctx, resp.Body, out)
}

// Messages API 形状:system 是顶层字段,messages 角色 user / assistant。
// 纯文本历史继续用 string content;工具历史用 content block 数组。
type messagesRequest struct {
	Model        string    `json:"model"`
	MaxTokens    int       `json:"max_tokens"`
	Stream       bool      `json:"stream"`
	System       string    `json:"system,omitempty"`
	Messages     []message `json:"messages"`
	Tools        []tool    `json:"tools,omitempty"`
	Temperature  *float64  `json:"temperature,omitempty"`
	OutputConfig any       `json:"output_config,omitempty"`
}

type tool struct {
	Name        string          `json:"name"`
	Description string          `json:"description,omitempty"`
	InputSchema json.RawMessage `json:"input_schema"`
}

type message struct {
	Role    string `json:"role"`
	Content any    `json:"content"`
}

type contentBlock struct {
	Type      string          `json:"type"`
	Text      string          `json:"text,omitempty"`
	Thinking  string          `json:"thinking,omitempty"`
	Signature string          `json:"signature,omitempty"`
	Data      string          `json:"data,omitempty"`
	Source    *contentSource  `json:"source,omitempty"`
	ID        string          `json:"id,omitempty"`
	Name      string          `json:"name,omitempty"`
	Input     json.RawMessage `json:"input,omitempty"`
	ToolUseID string          `json:"tool_use_id,omitempty"`
	Content   string          `json:"content,omitempty"`
	IsError   bool            `json:"is_error,omitempty"`
}

type contentSource struct {
	Type      string `json:"type"`
	MediaType string `json:"media_type"`
	Data      string `json:"data"`
}

func (c *Client) newRequest(ctx context.Context, req provider.Request) (*http.Request, error) {
	if req.Model == "" {
		return nil, errors.New("anthropic: model is required")
	}
	body := messagesRequest{
		Model:     req.Model,
		MaxTokens: defaultMaxTokens,
		Stream:    true,
		System:    req.System,
		Messages:  make([]message, 0, len(req.Messages)),
	}
	opts := req.Config.AnthropicOptions()
	if v, ok := req.Config.MaxOutputTokens(); ok {
		body.MaxTokens = v
	} else if v, ok := provider.IntOption(opts, "max_tokens", "max_output_tokens"); ok {
		body.MaxTokens = v
	}
	if v, ok := provider.FloatOption(opts, "temperature"); ok {
		body.Temperature = &v
	}
	if outputConfig, ok := opts["output_config"]; ok {
		body.OutputConfig = outputConfig
	}
	if len(req.Tools) > 0 {
		body.Tools = make([]tool, 0, len(req.Tools))
		for _, t := range req.Tools {
			body.Tools = append(body.Tools, tool{
				Name:        t.Name,
				Description: t.Description,
				InputSchema: t.InputSchema,
			})
		}
	}
	for _, msg := range req.Messages {
		body.Messages = append(body.Messages, messagesFor(msg)...)
	}
	var buf bytes.Buffer
	if err := json.NewEncoder(&buf).Encode(body); err != nil {
		return nil, err
	}
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+"/v1/messages", &buf)
	if err != nil {
		return nil, err
	}
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("anthropic-version", anthropicVersion)
	if c.apiKey != "" {
		httpReq.Header.Set("x-api-key", c.apiKey)
	}
	return httpReq, nil
}

// streamEvent 覆盖 Messages API 的全部流事件:data JSON 自带 type,
// 无需依赖 event: 行。thinking 块经 content_block_start 标记 index,
// 其 delta 不进正文(与 google 跳过 thought part 同理)。
type streamEvent struct {
	Type  string `json:"type"`
	Index int    `json:"index"`

	Message *struct {
		Usage anthUsage `json:"usage"`
	} `json:"message"`

	ContentBlock *struct {
		Type      string          `json:"type"`
		Text      string          `json:"text"`
		Thinking  string          `json:"thinking"`
		Signature string          `json:"signature"`
		Data      string          `json:"data"`
		ID        string          `json:"id"`
		Name      string          `json:"name"`
		Input     json.RawMessage `json:"input"`
	} `json:"content_block"`

	Delta *struct {
		Type        string `json:"type"`
		Text        string `json:"text"`
		Thinking    string `json:"thinking"`
		Signature   string `json:"signature"`
		PartialJSON string `json:"partial_json"`
		StopReason  string `json:"stop_reason"`
	} `json:"delta"`

	Usage *anthUsage `json:"usage"`

	Error *struct {
		Type    string `json:"type"`
		Message string `json:"message"`
	} `json:"error"`
}

type anthUsage struct {
	InputTokens              int `json:"input_tokens"`
	OutputTokens             int `json:"output_tokens"`
	CacheCreationInputTokens int `json:"cache_creation_input_tokens"`
	CacheReadInputTokens     int `json:"cache_read_input_tokens"`
}

// readSSE 解析 Messages API 的 SSE 流。协议没有 [DONE] 哨兵,
// 以 message_stop 事件收尾,EOF 时据此判定 Done 或异常截断。
func readSSE(ctx context.Context, body io.Reader, out chan<- provider.Chunk) error {
	scanner := bufio.NewScanner(body)
	scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	blockCalls := map[int]string{}
	sawStop := false
	finish := provider.FinishStop
	var usage provider.UsageInfo
	var responseBlocks []contentBlock
	toolInputs := map[int]*strings.Builder{}
	ensureBlock := func(index int) *contentBlock {
		for len(responseBlocks) <= index {
			responseBlocks = append(responseBlocks, contentBlock{})
		}
		return &responseBlocks[index]
	}
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || !strings.HasPrefix(line, "data:") {
			continue
		}
		data := strings.TrimSpace(strings.TrimPrefix(line, "data:"))
		var event streamEvent
		if err := json.Unmarshal([]byte(data), &event); err != nil {
			return fmt.Errorf("anthropic: parse stream event: %w", err)
		}
		switch event.Type {
		case "message_start":
			if event.Message != nil {
				usage.InputUncachedTokens = event.Message.Usage.InputTokens
				usage.OutputContentTokens = event.Message.Usage.OutputTokens
				usage.CacheCreationTokens = event.Message.Usage.CacheCreationInputTokens
				usage.InputCachedTokens = event.Message.Usage.CacheReadInputTokens
			}
		case "content_block_start":
			if event.ContentBlock != nil {
				block := ensureBlock(event.Index)
				*block = contentBlock{
					Type:      event.ContentBlock.Type,
					Text:      event.ContentBlock.Text,
					Thinking:  event.ContentBlock.Thinking,
					Signature: event.ContentBlock.Signature,
					Data:      event.ContentBlock.Data,
					ID:        event.ContentBlock.ID,
					Name:      event.ContentBlock.Name,
					Input:     append(json.RawMessage(nil), event.ContentBlock.Input...),
				}
				if event.ContentBlock.Type == "tool_use" {
					blockCalls[event.Index] = event.ContentBlock.ID
					toolInputs[event.Index] = &strings.Builder{}
					if !emitChunk(ctx, out, provider.Chunk{Tool: &provider.ToolCallChunk{
						Index:  event.Index,
						CallID: event.ContentBlock.ID,
						Name:   event.ContentBlock.Name,
					}}) {
						return ctx.Err()
					}
				}
			}
		case "content_block_delta":
			if event.Delta == nil {
				continue
			}
			switch event.Delta.Type {
			case "text_delta":
				ensureBlock(event.Index).Text += event.Delta.Text
				if event.Delta.Text != "" && !emitChunk(ctx, out, provider.Chunk{Part: provider.PartText, Delta: event.Delta.Text}) {
					return ctx.Err()
				}
			case "thinking_delta":
				ensureBlock(event.Index).Thinking += event.Delta.Thinking
				if event.Delta.Thinking != "" && !emitChunk(ctx, out, provider.Chunk{Part: provider.PartThought, Delta: event.Delta.Thinking}) {
					return ctx.Err()
				}
			case "signature_delta":
				ensureBlock(event.Index).Signature += event.Delta.Signature
			case "input_json_delta":
				if builder := toolInputs[event.Index]; builder != nil {
					builder.WriteString(event.Delta.PartialJSON)
				}
				if event.Delta.PartialJSON != "" && !emitChunk(ctx, out, provider.Chunk{Tool: &provider.ToolCallChunk{
					Index:     event.Index,
					CallID:    blockCalls[event.Index],
					ArgsDelta: event.Delta.PartialJSON,
				}}) {
					return ctx.Err()
				}
			}
		case "message_delta":
			if event.Usage != nil {
				if event.Usage.OutputTokens > 0 {
					usage.OutputContentTokens = event.Usage.OutputTokens
				}
				if event.Usage.InputTokens > 0 {
					usage.InputUncachedTokens = event.Usage.InputTokens
				}
				if event.Usage.CacheCreationInputTokens > 0 {
					usage.CacheCreationTokens = event.Usage.CacheCreationInputTokens
				}
				if event.Usage.CacheReadInputTokens > 0 {
					usage.InputCachedTokens = event.Usage.CacheReadInputTokens
				}
			}
			if event.Delta != nil && event.Delta.StopReason != "" {
				switch event.Delta.StopReason {
				case "end_turn", "max_tokens", "stop_sequence":
					finish = provider.FinishStop
				case "tool_use":
					finish = provider.FinishToolCalls
				default:
					return fmt.Errorf("anthropic: finished with stop reason %s", event.Delta.StopReason)
				}
			}
		case "message_stop":
			sawStop = true
		case "error":
			if event.Error != nil {
				return fmt.Errorf("anthropic: stream error %s: %s", event.Error.Type, event.Error.Message)
			}
			return errors.New("anthropic: stream error")
		}
		// message_start / content_block_stop / ping:无正文负载,跳过
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
	if !sawStop {
		return errors.New("anthropic: stream ended without message_stop")
	}
	for index, builder := range toolInputs {
		if builder == nil || builder.Len() == 0 {
			continue
		}
		if raw := json.RawMessage(builder.String()); json.Valid(raw) {
			ensureBlock(index).Input = append(json.RawMessage(nil), raw...)
		}
	}
	if !usage.Empty() && !emitChunk(ctx, out, provider.Chunk{Usage: &usage}) {
		return ctx.Err()
	}
	done := provider.Chunk{Done: true, Finish: finish}
	if data, err := json.Marshal(responseBlocks); err == nil && len(responseBlocks) > 0 {
		done.Continuation = &provider.Continuation{Kind: provider.ContinuationAnthropic, Data: data}
	}
	out <- done // 终止 chunk 阻塞发送,消费方 drain 到 close
	return nil
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

func messagesFor(msg provider.Message) []message {
	if segments := provider.SplitMessage(msg); len(segments) > 1 {
		var out []message
		for _, segment := range segments {
			out = append(out, messagesFor(segment)...)
		}
		return out
	}
	role := "user"
	if msg.Role == provider.RoleAssistant {
		role = "assistant"
	}
	if continuation := provider.ContinuationFor(msg, provider.ContinuationAnthropic); role == "assistant" && continuation != nil {
		var blocks []contentBlock
		if json.Unmarshal(continuation.Data, &blocks) == nil && len(blocks) > 0 {
			return []message{{Role: role, Content: blocks}}
		}
	}
	if len(msg.Parts) == 0 {
		return []message{{Role: role, Content: msg.Text}}
	}
	var out []message
	var blocks []contentBlock
	blockRole := role
	flushBlocks := func() {
		if len(blocks) == 0 {
			return
		}
		out = append(out, message{Role: blockRole, Content: blocks})
		blocks = nil
	}
	appendBlock := func(targetRole string, block contentBlock) {
		if len(blocks) > 0 && blockRole != targetRole {
			flushBlocks()
		}
		if len(blocks) == 0 {
			blockRole = targetRole
		}
		blocks = append(blocks, block)
	}
	for _, part := range msg.Parts {
		switch part.Type {
		case "", provider.PartText:
			if part.Text != "" {
				appendBlock(role, contentBlock{Type: "text", Text: part.Text})
			}
		case provider.PartImage:
			if len(part.Data) > 0 {
				appendBlock(role, contentBlock{
					Type: "image",
					Source: &contentSource{
						Type:      "base64",
						MediaType: part.MIME,
						Data:      base64.StdEncoding.EncodeToString(part.Data),
					},
				})
			}
		case provider.PartToolUse:
			args := part.Args
			if len(args) == 0 {
				args = json.RawMessage(`{}`)
			}
			appendBlock(role, contentBlock{
				Type:  "tool_use",
				ID:    part.CallID,
				Name:  part.Name,
				Input: append(json.RawMessage(nil), args...),
			})
		case provider.PartToolResult:
			appendBlock("user", contentBlock{
				Type:      "tool_result",
				ToolUseID: part.CallID,
				Content:   part.Content,
				IsError:   !part.Ok,
			})
		}
	}
	flushBlocks()
	if len(out) == 0 {
		out = append(out, message{Role: role, Content: msg.Text})
	}
	return out
}

// ListModels 拉取模型目录(GET /v1/models)。包级函数,
// 不进 provider.Client 流式契约(与 openai / google 同理)。
func ListModels(ctx context.Context, cfg Config) ([]string, error) {
	base := strings.TrimRight(cfg.BaseURL, "/")
	if base == "" {
		base = defaultBaseURL
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, base+"/v1/models?limit=100", nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("anthropic-version", anthropicVersion)
	if cfg.APIKey != "" {
		req.Header.Set("x-api-key", cfg.APIKey)
	}
	resp, err := httpClient(cfg.HTTPClient).Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("anthropic: status %d: %s", resp.StatusCode, redact(bodySummary(resp.Body), cfg.APIKey))
	}
	var payload struct {
		Data []struct {
			ID string `json:"id"`
		} `json:"data"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&payload); err != nil {
		return nil, fmt.Errorf("anthropic: parse models: %w", err)
	}
	models := make([]string, 0, len(payload.Data))
	for _, m := range payload.Data {
		models = append(models, m.ID)
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

// Package anthropic implements the Anthropic Messages API streaming
// protocol(POST /v1/messages,stream: true)。
//
// 边界与 openai / google 实现一致(AGENTS.md 硬约束 9 / 17):只产模型流、
// 无跨 turn 状态、不做流级重试;终止 chunk 阻塞发送。
package anthropic

import (
	"bufio"
	"bytes"
	"context"
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

// Messages API 形状:system 是顶层字段,messages 角色 user / assistant,
// content 取字符串简写(text-only 阶段;块数组形态留给多模态)。
type messagesRequest struct {
	Model       string    `json:"model"`
	MaxTokens   int       `json:"max_tokens"`
	Stream      bool      `json:"stream"`
	System      string    `json:"system,omitempty"`
	Messages    []message `json:"messages"`
	Tools       []tool    `json:"tools,omitempty"`
	Temperature *float64  `json:"temperature,omitempty"`
}

type tool struct {
	Name        string          `json:"name"`
	Description string          `json:"description,omitempty"`
	InputSchema json.RawMessage `json:"input_schema"`
}

type message struct {
	Role    string `json:"role"`
	Content string `json:"content"`
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
	if v, ok := provider.IntOption(req.Config.Anthropic, "max_tokens", "max_output_tokens"); ok {
		body.MaxTokens = v
	}
	if v, ok := provider.FloatOption(req.Config.Anthropic, "temperature"); ok {
		body.Temperature = &v
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
		role := "user"
		if msg.Role == provider.RoleAssistant {
			role = "assistant"
		}
		body.Messages = append(body.Messages, message{Role: role, Content: messageText(msg)})
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

	ContentBlock *struct {
		Type  string          `json:"type"`
		ID    string          `json:"id"`
		Name  string          `json:"name"`
		Input json.RawMessage `json:"input"`
	} `json:"content_block"`

	Delta *struct {
		Type        string `json:"type"`
		Text        string `json:"text"`
		Thinking    string `json:"thinking"`
		PartialJSON string `json:"partial_json"`
		StopReason  string `json:"stop_reason"`
	} `json:"delta"`

	Error *struct {
		Type    string `json:"type"`
		Message string `json:"message"`
	} `json:"error"`
}

// readSSE 解析 Messages API 的 SSE 流。协议没有 [DONE] 哨兵,
// 以 message_stop 事件收尾,EOF 时据此判定 Done 或异常截断。
func readSSE(ctx context.Context, body io.Reader, out chan<- provider.Chunk) error {
	scanner := bufio.NewScanner(body)
	scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	blockCalls := map[int]string{}
	sawStop := false
	finish := provider.FinishStop
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
		case "content_block_start":
			if event.ContentBlock != nil {
				if event.ContentBlock.Type == "tool_use" {
					blockCalls[event.Index] = event.ContentBlock.ID
					if !emitChunk(ctx, out, provider.Chunk{Tool: &provider.ToolCallChunk{
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
				if event.Delta.Text != "" && !emitChunk(ctx, out, provider.Chunk{Part: provider.PartText, Delta: event.Delta.Text}) {
					return ctx.Err()
				}
			case "thinking_delta":
				if event.Delta.Thinking != "" && !emitChunk(ctx, out, provider.Chunk{Part: provider.PartThought, Delta: event.Delta.Thinking}) {
					return ctx.Err()
				}
			case "input_json_delta":
				if event.Delta.PartialJSON != "" && !emitChunk(ctx, out, provider.Chunk{Tool: &provider.ToolCallChunk{
					CallID:    blockCalls[event.Index],
					ArgsDelta: event.Delta.PartialJSON,
				}}) {
					return ctx.Err()
				}
			}
		case "message_delta":
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
	out <- provider.Chunk{Done: true, Finish: finish} // 终止 chunk 阻塞发送,消费方 drain 到 close
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

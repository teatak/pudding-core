// Package openai implements an OpenAI-compatible Chat Completions streaming client.
package openai

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

const defaultResponseHeaderTimeout = 30 * time.Second

type Config struct {
	BaseURL    string
	APIKey     string
	HTTPClient *http.Client
}

type Client struct {
	baseURL string
	apiKey  string
	http    *http.Client
}

func New(cfg Config) *Client {
	return &Client{
		baseURL: strings.TrimRight(cfg.BaseURL, "/"),
		apiKey:  cfg.APIKey,
		http:    httpClient(cfg.HTTPClient),
	}
}

var _ provider.Client = (*Client)(nil)

func (c *Client) Name() string { return "openai-compatible" }

func (c *Client) Stream(ctx context.Context, req provider.Request) (<-chan provider.Chunk, error) {
	out := make(chan provider.Chunk)
	go func() {
		defer close(out)
		if err := c.stream(ctx, req, out); err != nil {
			emit(ctx, out, provider.Chunk{Err: err})
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
		return fmt.Errorf("openai: status %d: %s", resp.StatusCode, redact(bodySummary(resp.Body), c.apiKey))
	}
	return readSSE(ctx, resp.Body, out)
}

func (c *Client) newRequest(ctx context.Context, req provider.Request) (*http.Request, error) {
	if c.baseURL == "" {
		return nil, errors.New("openai: base url is required")
	}
	body := chatRequest{
		Model:    req.Model,
		Stream:   true,
		Messages: make([]chatMessage, 0, len(req.Messages)+1),
	}
	opts := req.Config.OpenAIOptions()
	if v, ok := provider.FloatOption(opts, "temperature"); ok {
		body.Temperature = &v
	}
	if v, ok := req.Config.MaxOutputTokens(); ok {
		body.MaxCompletionTokens = &v
	} else if v, ok := provider.IntOption(opts, "max_completion_tokens", "max_output_tokens", "max_tokens"); ok {
		body.MaxCompletionTokens = &v
	}
	if v, ok := provider.StringOption(opts, "reasoning_effort"); ok {
		body.ReasoningEffort = v
	}
	if req.System != "" {
		body.Messages = append(body.Messages, chatMessage{Role: "system", Content: req.System})
	}
	for _, msg := range req.Messages {
		body.Messages = append(body.Messages, chatMessage{Role: string(msg.Role), Content: messageText(msg)})
	}
	if len(req.Tools) > 0 {
		body.Tools = make([]chatTool, 0, len(req.Tools))
		for _, tool := range req.Tools {
			body.Tools = append(body.Tools, chatTool{
				Type: "function",
				Function: chatToolFunction{
					Name:        tool.Name,
					Description: tool.Description,
					Parameters:  tool.InputSchema,
				},
			})
		}
	}
	var buf bytes.Buffer
	if err := json.NewEncoder(&buf).Encode(body); err != nil {
		return nil, err
	}
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+"/chat/completions", &buf)
	if err != nil {
		return nil, err
	}
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("Accept", "text/event-stream")
	if c.apiKey != "" {
		httpReq.Header.Set("Authorization", "Bearer "+c.apiKey)
	}
	return httpReq, nil
}

func readSSE(ctx context.Context, body io.Reader, out chan<- provider.Chunk) error {
	scanner := bufio.NewScanner(body)
	scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, ":") || !strings.HasPrefix(line, "data:") {
			continue
		}
		data := strings.TrimSpace(strings.TrimPrefix(line, "data:"))
		if data == "[DONE]" {
			emit(ctx, out, provider.Chunk{Done: true})
			return nil
		}
		var frame chatStreamFrame
		if err := json.Unmarshal([]byte(data), &frame); err != nil {
			return fmt.Errorf("openai: parse stream frame: %w", err)
		}
		for _, choice := range frame.Choices {
			if choice.Delta.Content != "" {
				if !emit(ctx, out, provider.Chunk{Part: provider.PartText, Delta: choice.Delta.Content}) {
					return ctx.Err()
				}
			}
			if choice.Delta.ReasoningContent != "" {
				if !emit(ctx, out, provider.Chunk{Part: provider.PartThought, Delta: choice.Delta.ReasoningContent}) {
					return ctx.Err()
				}
			}
			for _, call := range choice.Delta.ToolCalls {
				if !emit(ctx, out, provider.Chunk{Tool: &provider.ToolCallChunk{
					Index:     call.Index,
					CallID:    call.ID,
					Name:      call.Function.Name,
					ArgsDelta: call.Function.Arguments,
				}}) {
					return ctx.Err()
				}
			}
			if choice.FinishReason != "" {
				finish := provider.FinishStop
				if choice.FinishReason == "tool_calls" {
					finish = provider.FinishToolCalls
				}
				emit(ctx, out, provider.Chunk{Done: true, Finish: finish})
				return nil
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
	return errors.New("openai: stream ended without [DONE]")
}

func emit(ctx context.Context, out chan<- provider.Chunk, chunk provider.Chunk) bool {
	// 终止 chunk 必须阻塞发送:消费方契约是 drain 到 channel close,
	// 非阻塞发送会在 cancel 竞态下丢终止帧,使 engine 把 cancelled 误判为
	// "stream ended without terminal chunk" 的 failed。
	if chunk.Err != nil || chunk.Done {
		out <- chunk
		return true
	}
	select {
	case out <- chunk:
		return true
	case <-ctx.Done():
		return false
	}
}

// ListModels 拉取端点的模型目录(GET /models)。包级函数而非 Client 方法:
// 模型目录是配置面能力,不进 provider.Client 流式契约。
func ListModels(ctx context.Context, cfg Config) ([]string, error) {
	base := strings.TrimRight(cfg.BaseURL, "/")
	if base == "" {
		return nil, errors.New("openai: base url is required")
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, base+"/models", nil)
	if err != nil {
		return nil, err
	}
	if cfg.APIKey != "" {
		req.Header.Set("Authorization", "Bearer "+cfg.APIKey)
	}
	resp, err := httpClient(cfg.HTTPClient).Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("openai: status %d: %s", resp.StatusCode, redact(bodySummary(resp.Body), cfg.APIKey))
	}
	var payload struct {
		Data []struct {
			ID string `json:"id"`
		} `json:"data"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&payload); err != nil {
		return nil, fmt.Errorf("openai: parse models: %w", err)
	}
	models := make([]string, 0, len(payload.Data))
	for _, m := range payload.Data {
		if m.ID != "" {
			models = append(models, m.ID)
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
			ResponseHeaderTimeout: defaultResponseHeaderTimeout,
			ExpectContinueTimeout: 1 * time.Second,
		},
	}
}

type chatRequest struct {
	Model               string        `json:"model"`
	Stream              bool          `json:"stream"`
	Messages            []chatMessage `json:"messages"`
	Tools               []chatTool    `json:"tools,omitempty"`
	Temperature         *float64      `json:"temperature,omitempty"`
	MaxCompletionTokens *int          `json:"max_completion_tokens,omitempty"`
	ReasoningEffort     string        `json:"reasoning_effort,omitempty"`
}

type chatMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

type chatTool struct {
	Type     string           `json:"type"`
	Function chatToolFunction `json:"function"`
}

type chatToolFunction struct {
	Name        string          `json:"name"`
	Description string          `json:"description,omitempty"`
	Parameters  json.RawMessage `json:"parameters,omitempty"`
}

type chatStreamFrame struct {
	Choices []struct {
		Delta struct {
			Content          string `json:"content"`
			ReasoningContent string `json:"reasoning_content"`
			ToolCalls        []struct {
				Index    int    `json:"index"`
				ID       string `json:"id"`
				Function struct {
					Name      string `json:"name"`
					Arguments string `json:"arguments"`
				} `json:"function"`
			} `json:"tool_calls"`
		} `json:"delta"`
		FinishReason string `json:"finish_reason"`
	} `json:"choices"`
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

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
	SystemInstruction *content  `json:"system_instruction,omitempty"`
	Contents          []content `json:"contents"`
}

type content struct {
	Role  string `json:"role,omitempty"`
	Parts []part `json:"parts"`
}

type part struct {
	Text string `json:"text"`
}

func (c *Client) newRequest(ctx context.Context, req provider.Request) (*http.Request, error) {
	if req.Model == "" {
		return nil, errors.New("google: model is required")
	}
	body := generateRequest{Contents: make([]content, 0, len(req.Messages))}
	if req.System != "" {
		body.SystemInstruction = &content{Parts: []part{{Text: req.System}}}
	}
	for _, msg := range req.Messages {
		role := "user"
		if msg.Role == provider.RoleAssistant {
			role = "model"
		}
		body.Contents = append(body.Contents, content{Role: role, Parts: []part{{Text: msg.Text}}})
	}
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
}

// readSSE 解析 Gemini 的 SSE 流。协议没有 [DONE] 哨兵,
// 以末帧的 finishReason 标记收尾,EOF 时据此判定 Done 或异常截断。
func readSSE(ctx context.Context, body io.Reader, out chan<- provider.Chunk) error {
	scanner := bufio.NewScanner(body)
	scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	sawFinish := false
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
		for _, cand := range frame.Candidates {
			for _, p := range cand.Content.Parts {
				if p.Text != "" {
					if !emitDelta(ctx, out, p.Text) {
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
	out <- provider.Chunk{Done: true} // 终止 chunk 阻塞发送,消费方 drain 到 close
	return nil
}

func emitDelta(ctx context.Context, out chan<- provider.Chunk, delta string) bool {
	select {
	case out <- provider.Chunk{Delta: delta}:
		return true
	case <-ctx.Done():
		return false
	}
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

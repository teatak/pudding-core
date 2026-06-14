package openai

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"

	"github.com/teatak/pudding-core/internal/provider"
)

type ResponsesClient struct {
	baseURL string
	apiKey  string
	http    *http.Client
}

func NewResponses(cfg Config) *ResponsesClient {
	return &ResponsesClient{
		baseURL: strings.TrimRight(cfg.BaseURL, "/"),
		apiKey:  cfg.APIKey,
		http:    httpClient(cfg.HTTPClient),
	}
}

var _ provider.Client = (*ResponsesClient)(nil)

func (c *ResponsesClient) Name() string { return "openai-responses" }

func (c *ResponsesClient) Stream(ctx context.Context, req provider.Request) (<-chan provider.Chunk, error) {
	out := make(chan provider.Chunk)
	go func() {
		defer close(out)
		if err := c.stream(ctx, req, out); err != nil {
			emit(ctx, out, provider.Chunk{Err: err})
		}
	}()
	return out, nil
}

func (c *ResponsesClient) stream(ctx context.Context, req provider.Request, out chan<- provider.Chunk) error {
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
		return fmt.Errorf("openai responses: status %d: %s", resp.StatusCode, redact(bodySummary(resp.Body), c.apiKey))
	}
	return readResponsesSSE(ctx, resp.Body, out)
}

func (c *ResponsesClient) newRequest(ctx context.Context, req provider.Request) (*http.Request, error) {
	if c.baseURL == "" {
		return nil, errors.New("openai responses: base url is required")
	}
	body := responsesRequest{
		Model:  req.Model,
		Stream: true,
		Store:  boolPtr(false),
		Input:  make([]responsesInputMessage, 0, len(req.Messages)),
	}
	if req.System != "" {
		body.Instructions = req.System
	}
	for _, msg := range req.Messages {
		body.Input = append(body.Input, responsesInputMessage{Role: string(msg.Role), Content: msg.Text})
	}
	var buf bytes.Buffer
	if err := json.NewEncoder(&buf).Encode(body); err != nil {
		return nil, err
	}
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+"/responses", &buf)
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

func readResponsesSSE(ctx context.Context, body io.Reader, out chan<- provider.Chunk) error {
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
		var frame responsesStreamFrame
		if err := json.Unmarshal([]byte(data), &frame); err != nil {
			return fmt.Errorf("openai responses: parse stream frame: %w", err)
		}
		switch frame.Type {
		case "response.output_text.delta":
			if frame.Delta != "" {
				if !emit(ctx, out, provider.Chunk{Delta: frame.Delta}) {
					return ctx.Err()
				}
			}
		case "response.completed":
			emit(ctx, out, provider.Chunk{Done: true})
			return nil
		case "response.failed":
			if frame.Response.Error.Message != "" {
				return errors.New("openai responses: " + frame.Response.Error.Message)
			}
			return errors.New("openai responses: response failed")
		case "response.incomplete":
			if frame.Response.IncompleteDetails.Reason != "" {
				return errors.New("openai responses: incomplete: " + frame.Response.IncompleteDetails.Reason)
			}
			return errors.New("openai responses: response incomplete")
		case "error":
			if frame.Error.Message != "" {
				return errors.New("openai responses: " + frame.Error.Message)
			}
			return errors.New("openai responses: stream error")
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
	return errors.New("openai responses: stream ended without response.completed")
}

func boolPtr(v bool) *bool { return &v }

type responsesRequest struct {
	Model        string                  `json:"model"`
	Instructions string                  `json:"instructions,omitempty"`
	Input        []responsesInputMessage `json:"input"`
	Stream       bool                    `json:"stream"`
	Store        *bool                   `json:"store,omitempty"`
}

type responsesInputMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

type responsesStreamFrame struct {
	Type     string `json:"type"`
	Delta    string `json:"delta"`
	Response struct {
		Error struct {
			Message string `json:"message"`
		} `json:"error"`
		IncompleteDetails struct {
			Reason string `json:"reason"`
		} `json:"incomplete_details"`
	} `json:"response"`
	Error struct {
		Message string `json:"message"`
	} `json:"error"`
}

package openai

import (
	"bufio"
	"bytes"
	"context"
	"encoding/base64"
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
		Model:   req.Model,
		Stream:  true,
		Store:   boolPtr(false),
		Include: []string{"reasoning.encrypted_content"},
		Input:   make([]responsesInputMessage, 0, len(req.Messages)),
	}
	opts := req.Config.OpenAIOptions()
	if v, ok := provider.FloatOption(opts, "temperature"); ok {
		body.Temperature = &v
	}
	if v, ok := req.Config.MaxOutputTokens(); ok {
		body.MaxOutputTokens = &v
	} else if v, ok := provider.IntOption(opts, "max_output_tokens", "max_completion_tokens", "max_tokens"); ok {
		body.MaxOutputTokens = &v
	}
	if v, ok := provider.StringOption(opts, "reasoning_effort"); ok {
		body.Reasoning = &responsesReasoning{Effort: v}
	}
	if req.System != "" {
		body.Instructions = req.System
	}
	for _, msg := range req.Messages {
		body.Input = append(body.Input, responsesInputsFor(msg)...)
	}
	if len(req.Tools) > 0 {
		body.Tools = make([]responsesTool, 0, len(req.Tools))
		for _, tool := range req.Tools {
			body.Tools = append(body.Tools, responsesTool{
				Type:        "function",
				Name:        tool.Name,
				Description: tool.Description,
				Parameters:  tool.InputSchema,
			})
		}
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
	sawTool := false
	callIDs := map[string]string{}
	var responseItems []json.RawMessage
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
				if !emit(ctx, out, provider.Chunk{Part: provider.PartText, Delta: frame.Delta}) {
					return ctx.Err()
				}
			}
		case "response.reasoning_text.delta", "response.reasoning_summary_text.delta":
			if frame.Delta != "" {
				if !emit(ctx, out, provider.Chunk{Part: provider.PartThought, Delta: frame.Delta}) {
					return ctx.Err()
				}
			}
		case "response.output_item.added":
			var item responsesOutputItem
			if json.Unmarshal(frame.Item, &item) != nil {
				continue
			}
			if item.Type == "function_call" {
				sawTool = true
				callID := item.CallID
				if callID == "" {
					callID = item.ID
				}
				callIDs[item.ID] = callID
				if !emit(ctx, out, provider.Chunk{Tool: &provider.ToolCallChunk{
					CallID: callID,
					Name:   item.Name,
				}}) {
					return ctx.Err()
				}
			}
		case "response.output_item.done":
			if len(frame.Item) > 0 {
				responseItems = append(responseItems, append(json.RawMessage(nil), frame.Item...))
			}
		case "response.function_call_arguments.delta":
			sawTool = true
			if frame.Delta != "" {
				callID := callIDs[frame.ItemID]
				if callID == "" {
					callID = frame.ItemID
				}
				if !emit(ctx, out, provider.Chunk{Tool: &provider.ToolCallChunk{
					CallID:    callID,
					ArgsDelta: frame.Delta,
				}}) {
					return ctx.Err()
				}
			}
		case "response.completed":
			finish := provider.FinishStop
			if sawTool {
				finish = provider.FinishToolCalls
			}
			if len(responseItems) == 0 && len(frame.Response.Output) > 0 {
				responseItems = cloneResponseItems(frame.Response.Output)
			}
			if frame.Response.Usage != nil {
				usage := responsesUsageInfo(*frame.Response.Usage)
				if !usage.Empty() && !emit(ctx, out, provider.Chunk{Usage: &usage}) {
					return ctx.Err()
				}
			}
			done := provider.Chunk{Done: true, Finish: finish}
			if encoded, err := json.Marshal(responseItems); err == nil && len(responseItems) > 0 {
				done.Continuation = &provider.Continuation{Kind: provider.ContinuationOpenAIResponses, Data: encoded}
			}
			emit(ctx, out, done)
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
	Model           string                  `json:"model"`
	Instructions    string                  `json:"instructions,omitempty"`
	Input           []responsesInputMessage `json:"input"`
	Stream          bool                    `json:"stream"`
	Store           *bool                   `json:"store,omitempty"`
	Tools           []responsesTool         `json:"tools,omitempty"`
	Temperature     *float64                `json:"temperature,omitempty"`
	MaxOutputTokens *int                    `json:"max_output_tokens,omitempty"`
	Reasoning       *responsesReasoning     `json:"reasoning,omitempty"`
	Include         []string                `json:"include,omitempty"`
}

type responsesReasoning struct {
	Effort string `json:"effort,omitempty"`
}

type responsesInputMessage struct {
	Type      string `json:"type,omitempty"`
	Role      string `json:"role,omitempty"`
	Content   any    `json:"content,omitempty"`
	CallID    string `json:"call_id,omitempty"`
	Name      string `json:"name,omitempty"`
	Arguments string `json:"arguments,omitempty"`
	Output    string `json:"output,omitempty"`
	Raw       string `json:"-"`
}

func (m responsesInputMessage) MarshalJSON() ([]byte, error) {
	if m.Raw != "" {
		if !json.Valid([]byte(m.Raw)) {
			return nil, errors.New("openai responses: invalid continuation item")
		}
		return []byte(m.Raw), nil
	}
	type alias responsesInputMessage
	return json.Marshal(alias(m))
}

type responsesContentPart struct {
	Type       string               `json:"type"`
	Text       string               `json:"text,omitempty"`
	ImageURL   string               `json:"image_url,omitempty"`
	InputAudio *responsesInputAudio `json:"input_audio,omitempty"`
}

type responsesInputAudio struct {
	Data   string `json:"data"`
	Format string `json:"format"`
}

type responsesTool struct {
	Type        string          `json:"type"`
	Name        string          `json:"name"`
	Description string          `json:"description,omitempty"`
	Parameters  json.RawMessage `json:"parameters,omitempty"`
}

type responsesStreamFrame struct {
	Type     string          `json:"type"`
	Delta    string          `json:"delta"`
	ItemID   string          `json:"item_id"`
	Item     json.RawMessage `json:"item"`
	Response struct {
		Error struct {
			Message string `json:"message"`
		} `json:"error"`
		IncompleteDetails struct {
			Reason string `json:"reason"`
		} `json:"incomplete_details"`
		Usage  *responsesUsage   `json:"usage,omitempty"`
		Output []json.RawMessage `json:"output,omitempty"`
	} `json:"response"`
	Error struct {
		Message string `json:"message"`
	} `json:"error"`
}

func cloneResponseItems(items []json.RawMessage) []json.RawMessage {
	out := make([]json.RawMessage, 0, len(items))
	for _, item := range items {
		if len(item) > 0 {
			out = append(out, append(json.RawMessage(nil), item...))
		}
	}
	return out
}

type responsesOutputItem struct {
	ID     string `json:"id"`
	CallID string `json:"call_id"`
	Type   string `json:"type"`
	Name   string `json:"name"`
}

type responsesUsage struct {
	InputTokens         int                     `json:"input_tokens"`
	OutputTokens        int                     `json:"output_tokens"`
	InputTokensDetails  *promptTokensDetails    `json:"input_tokens_details,omitempty"`
	OutputTokensDetails *completionTokenDetails `json:"output_tokens_details,omitempty"`
}

func responsesUsageInfo(u responsesUsage) provider.UsageInfo {
	cached := clampUsage(u.InputTokensDetails.cachedTokens())
	input := clampUsage(u.InputTokens - cached)
	reasoning := clampUsage(u.OutputTokensDetails.reasoningTokens())
	output := clampUsage(u.OutputTokens - reasoning)
	return provider.UsageInfo{
		InputUncachedTokens:   input,
		InputCachedTokens:     cached,
		OutputContentTokens:   output,
		OutputReasoningTokens: reasoning,
	}
}

func responsesInputsFor(msg provider.Message) []responsesInputMessage {
	if segments := provider.SplitMessage(msg); len(segments) > 1 {
		var out []responsesInputMessage
		for _, segment := range segments {
			out = append(out, responsesInputsFor(segment)...)
		}
		return out
	}
	if continuation := provider.ContinuationFor(msg, provider.ContinuationOpenAIResponses); msg.Role == provider.RoleAssistant && continuation != nil {
		var items []json.RawMessage
		if json.Unmarshal(continuation.Data, &items) == nil && len(items) > 0 {
			out := make([]responsesInputMessage, 0, len(items))
			for _, item := range items {
				if len(item) > 0 {
					out = append(out, responsesInputMessage{Raw: string(item)})
				}
			}
			if len(out) > 0 {
				return out
			}
		}
	}
	if len(msg.Parts) == 0 {
		return []responsesInputMessage{{Role: string(msg.Role), Content: msg.Text}}
	}
	role := string(msg.Role)
	if content := responsesMediaContent(msg.Parts); len(content) > 0 {
		return []responsesInputMessage{{Role: role, Content: content}}
	}
	var out []responsesInputMessage
	var text strings.Builder
	flushText := func() {
		if text.Len() == 0 {
			return
		}
		out = append(out, responsesInputMessage{Role: role, Content: text.String()})
		text.Reset()
	}
	for _, part := range msg.Parts {
		switch part.Type {
		case "", provider.PartText:
			text.WriteString(part.Text)
		case provider.PartToolUse:
			flushText()
			args := string(part.Args)
			if args == "" {
				args = "{}"
			}
			out = append(out, responsesInputMessage{
				Type:      "function_call",
				CallID:    part.CallID,
				Name:      part.Name,
				Arguments: args,
			})
		case provider.PartToolResult:
			flushText()
			out = append(out, responsesInputMessage{
				Type:   "function_call_output",
				CallID: part.CallID,
				Output: part.Content,
			})
		}
	}
	flushText()
	if len(out) == 0 {
		out = append(out, responsesInputMessage{Role: role, Content: msg.Text})
	}
	return out
}

func responsesMediaContent(parts []provider.Part) []responsesContentPart {
	hasMedia := false
	for _, part := range parts {
		if (part.Type == provider.PartImage || part.Type == provider.PartAudio) && len(part.Data) > 0 {
			hasMedia = true
			break
		}
	}
	if !hasMedia {
		return nil
	}
	out := make([]responsesContentPart, 0, len(parts))
	for _, part := range parts {
		switch part.Type {
		case "", provider.PartText:
			if part.Text != "" {
				out = append(out, responsesContentPart{Type: "input_text", Text: part.Text})
			}
		case provider.PartImage:
			if len(part.Data) > 0 {
				out = append(out, responsesContentPart{Type: "input_image", ImageURL: provider.ImageDataURL(part.MIME, part.Data)})
			}
		case provider.PartAudio:
			if len(part.Data) > 0 {
				out = append(out, responsesContentPart{Type: "input_audio", InputAudio: &responsesInputAudio{
					Data:   base64.StdEncoding.EncodeToString(part.Data),
					Format: provider.AudioFormat(part.MIME),
				}})
			}
		}
	}
	return out
}

// Package openai implements an OpenAI-compatible Chat Completions streaming client.
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
	err := c.streamOnce(ctx, req, out, true)
	if isUnsupportedUsageStreamOption(err) {
		return c.streamOnce(ctx, req, out, false)
	}
	return err
}

func (c *Client) streamOnce(ctx context.Context, req provider.Request, out chan<- provider.Chunk, includeUsage bool) error {
	httpReq, err := c.newRequest(ctx, req, includeUsage)
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
		return &httpStatusError{status: resp.StatusCode, summary: redact(bodySummary(resp.Body), c.apiKey)}
	}
	return readSSE(ctx, resp.Body, out)
}

type httpStatusError struct {
	status  int
	summary string
}

func (e *httpStatusError) Error() string {
	return fmt.Sprintf("openai: status %d: %s", e.status, e.summary)
}

func isUnsupportedUsageStreamOption(err error) bool {
	var statusErr *httpStatusError
	if !errors.As(err, &statusErr) {
		return false
	}
	if statusErr.status != http.StatusBadRequest && statusErr.status != http.StatusUnprocessableEntity {
		return false
	}
	summary := strings.ToLower(statusErr.summary)
	return strings.Contains(summary, "stream_options") ||
		strings.Contains(summary, "stream options") ||
		strings.Contains(summary, "include_usage") ||
		strings.Contains(summary, "include usage")
}

func (c *Client) newRequest(ctx context.Context, req provider.Request, includeUsage bool) (*http.Request, error) {
	if c.baseURL == "" {
		return nil, errors.New("openai: base url is required")
	}
	body := chatRequest{
		Model:    req.Model,
		Stream:   true,
		Messages: make([]chatMessage, 0, len(req.Messages)+1),
	}
	if includeUsage {
		body.StreamOptions = &chatStreamOptions{IncludeUsage: true}
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
		body.Messages = append(body.Messages, chatMessagesFor(msg)...)
	}
	body.Messages = sanitizeChatToolMessages(body.Messages)
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
	finish := provider.FinishStop
	continuation := chatMessage{Role: "assistant"}
	var responseText strings.Builder
	var responseReasoning strings.Builder
	var responseReasoningContent strings.Builder
	toolIndexes := map[int]int{}
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, ":") || !strings.HasPrefix(line, "data:") {
			continue
		}
		data := strings.TrimSpace(strings.TrimPrefix(line, "data:"))
		if data == "[DONE]" {
			continuation.Content = responseText.String()
			continuation.Reasoning = responseReasoning.String()
			continuation.ReasoningContent = responseReasoningContent.String()
			continuation.ReasoningDetails = compactReasoningDetails(continuation.ReasoningDetails)
			done := provider.Chunk{Done: true, Finish: finish}
			if encoded, err := json.Marshal(continuation); err == nil &&
				(responseText.Len() > 0 ||
					responseReasoning.Len() > 0 ||
					responseReasoningContent.Len() > 0 ||
					len(continuation.ReasoningDetails) > 0 ||
					len(continuation.ToolCalls) > 0) {
				done.Continuation = &provider.Continuation{Kind: provider.ContinuationOpenAIChat, Data: encoded}
			}
			emit(ctx, out, done)
			return nil
		}
		var frame chatStreamFrame
		if err := json.Unmarshal([]byte(data), &frame); err != nil {
			return fmt.Errorf("openai: parse stream frame: %w", err)
		}
		if frame.Usage != nil {
			usage := chatUsageInfo(*frame.Usage)
			if !usage.Empty() && !emit(ctx, out, provider.Chunk{Usage: &usage}) {
				return ctx.Err()
			}
		}
		for _, choice := range frame.Choices {
			responseReasoning.WriteString(choice.Delta.Reasoning)
			responseReasoningContent.WriteString(choice.Delta.ReasoningContent)
			for _, detail := range choice.Delta.ReasoningDetails {
				continuation.ReasoningDetails = append(
					continuation.ReasoningDetails,
					append(json.RawMessage(nil), detail...),
				)
			}
			if choice.Delta.Content != "" {
				responseText.WriteString(choice.Delta.Content)
				if !emit(ctx, out, provider.Chunk{Part: provider.PartText, Delta: choice.Delta.Content}) {
					return ctx.Err()
				}
			}
			for _, reasoning := range choice.Delta.reasoningDeltas() {
				if !emit(ctx, out, provider.Chunk{Part: provider.PartThought, Delta: reasoning}) {
					return ctx.Err()
				}
			}
			for _, call := range choice.Delta.ToolCalls {
				target, ok := toolIndexes[call.Index]
				if !ok {
					target = len(continuation.ToolCalls)
					toolIndexes[call.Index] = target
					continuation.ToolCalls = append(continuation.ToolCalls, chatToolCall{Type: "function"})
				}
				stored := &continuation.ToolCalls[target]
				if call.ID != "" {
					stored.ID = call.ID
				}
				if call.Function.Name != "" {
					stored.Function.Name = call.Function.Name
				}
				stored.Function.Arguments += call.Function.Arguments
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
				finish = provider.FinishStop
				if choice.FinishReason == "tool_calls" {
					finish = provider.FinishToolCalls
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
	Model               string             `json:"model"`
	Stream              bool               `json:"stream"`
	Messages            []chatMessage      `json:"messages"`
	Tools               []chatTool         `json:"tools,omitempty"`
	Temperature         *float64           `json:"temperature,omitempty"`
	MaxCompletionTokens *int               `json:"max_completion_tokens,omitempty"`
	ReasoningEffort     string             `json:"reasoning_effort,omitempty"`
	StreamOptions       *chatStreamOptions `json:"stream_options,omitempty"`
}

type chatStreamOptions struct {
	IncludeUsage bool `json:"include_usage,omitempty"`
}

type chatMessage struct {
	Role             string            `json:"role"`
	Content          any               `json:"content"`
	Reasoning        string            `json:"reasoning,omitempty"`
	ReasoningContent string            `json:"reasoning_content,omitempty"`
	ReasoningDetails []json.RawMessage `json:"reasoning_details,omitempty"`
	ToolCalls        []chatToolCall    `json:"tool_calls,omitempty"`
	ToolCallID       string            `json:"tool_call_id,omitempty"`
}

type chatContentPart struct {
	Type       string          `json:"type"`
	Text       string          `json:"text,omitempty"`
	ImageURL   *chatImageURL   `json:"image_url,omitempty"`
	InputAudio *chatInputAudio `json:"input_audio,omitempty"`
}

type chatImageURL struct {
	URL string `json:"url"`
}

type chatInputAudio struct {
	Data   string `json:"data"`
	Format string `json:"format"`
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

type chatToolCall struct {
	ID       string               `json:"id"`
	Type     string               `json:"type"`
	Function chatToolCallFunction `json:"function"`
}

type chatToolCallFunction struct {
	Name      string `json:"name"`
	Arguments string `json:"arguments"`
}

type chatStreamFrame struct {
	Choices []struct {
		Delta        chatStreamDelta `json:"delta"`
		FinishReason string          `json:"finish_reason"`
	} `json:"choices"`
	Usage *chatUsage `json:"usage,omitempty"`
}

type chatStreamDelta struct {
	Content          string            `json:"content"`
	Reasoning        string            `json:"reasoning"`
	ReasoningContent string            `json:"reasoning_content"`
	ReasoningDetails []json.RawMessage `json:"reasoning_details"`
	ToolCalls        []struct {
		Index    int    `json:"index"`
		ID       string `json:"id"`
		Function struct {
			Name      string `json:"name"`
			Arguments string `json:"arguments"`
		} `json:"function"`
	} `json:"tool_calls"`
}

type chatReasoningDetailText struct {
	Text    string `json:"text"`
	Summary string `json:"summary"`
}

func (d chatStreamDelta) reasoningDeltas() []string {
	out := make([]string, 0, 1+len(d.ReasoningDetails))
	appendUnique := func(s string) {
		if s == "" {
			return
		}
		for _, existing := range out {
			if existing == s {
				return
			}
		}
		out = append(out, s)
	}
	appendUnique(d.ReasoningContent)
	appendUnique(d.Reasoning)
	for _, raw := range d.ReasoningDetails {
		var detail chatReasoningDetailText
		if json.Unmarshal(raw, &detail) != nil {
			continue
		}
		appendUnique(detail.Text)
		appendUnique(detail.Summary)
	}
	return out
}

type chatUsage struct {
	PromptTokens            int                     `json:"prompt_tokens"`
	CompletionTokens        int                     `json:"completion_tokens"`
	PromptTokensDetails     *promptTokensDetails    `json:"prompt_tokens_details,omitempty"`
	CompletionTokensDetails *completionTokenDetails `json:"completion_tokens_details,omitempty"`
}

type promptTokensDetails struct {
	CachedTokens int `json:"cached_tokens"`
}

type completionTokenDetails struct {
	ReasoningTokens int `json:"reasoning_tokens"`
}

func chatUsageInfo(u chatUsage) provider.UsageInfo {
	cached := clampUsage(u.PromptTokensDetails.cachedTokens())
	input := clampUsage(u.PromptTokens - cached)
	reasoning := clampUsage(u.CompletionTokensDetails.reasoningTokens())
	output := clampUsage(u.CompletionTokens - reasoning)
	return provider.UsageInfo{
		InputUncachedTokens:   input,
		InputCachedTokens:     cached,
		OutputContentTokens:   output,
		OutputReasoningTokens: reasoning,
	}
}

func (d *promptTokensDetails) cachedTokens() int {
	if d == nil {
		return 0
	}
	return d.CachedTokens
}

func (d *completionTokenDetails) reasoningTokens() int {
	if d == nil {
		return 0
	}
	return d.ReasoningTokens
}

func clampUsage(v int) int {
	if v < 0 {
		return 0
	}
	return v
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

func chatMessagesFor(msg provider.Message) []chatMessage {
	if segments := provider.SplitMessage(msg); len(segments) > 1 {
		var out []chatMessage
		for _, segment := range segments {
			out = append(out, chatMessagesFor(segment)...)
		}
		return out
	}
	if continuation := provider.ContinuationFor(msg, provider.ContinuationOpenAIChat); msg.Role == provider.RoleAssistant && continuation != nil {
		var replay chatMessage
		if json.Unmarshal(continuation.Data, &replay) == nil && replay.Role == "assistant" {
			replay.ReasoningDetails = compactReasoningDetails(replay.ReasoningDetails)
			return []chatMessage{replay}
		}
	}
	if len(msg.Parts) == 0 {
		return []chatMessage{{Role: string(msg.Role), Content: msg.Text}}
	}
	role := string(msg.Role)
	if content := chatMediaContent(msg.Parts); len(content) > 0 {
		return []chatMessage{{Role: role, Content: content}}
	}
	var out []chatMessage
	var text strings.Builder
	var reasoning strings.Builder
	var calls []chatToolCall
	flushToolCalls := func() {
		if len(calls) == 0 {
			return
		}
		out = append(out, chatMessage{
			Role:             "assistant",
			Content:          text.String(),
			ReasoningContent: reasoning.String(),
			ToolCalls:        calls,
		})
		text.Reset()
		reasoning.Reset()
		calls = nil
	}
	flushText := func() {
		if text.Len() == 0 {
			return
		}
		out = append(out, chatMessage{Role: role, Content: text.String()})
		text.Reset()
	}
	for _, part := range msg.Parts {
		switch part.Type {
		case "", provider.PartText:
			text.WriteString(part.Text)
		case provider.PartThought:
			reasoning.WriteString(part.Text)
		case provider.PartToolUse:
			args := string(part.Args)
			if args == "" {
				args = "{}"
			}
			calls = append(calls, chatToolCall{
				ID:   part.CallID,
				Type: "function",
				Function: chatToolCallFunction{
					Name:      part.Name,
					Arguments: args,
				},
			})
		case provider.PartToolResult:
			flushToolCalls()
			flushText()
			out = append(out, chatMessage{Role: "tool", Content: part.Content, ToolCallID: part.CallID})
		}
	}
	flushToolCalls()
	flushText()
	if len(out) == 0 {
		out = append(out, chatMessage{Role: role, Content: msg.Text})
	}
	return out
}

func compactReasoningDetails(details []json.RawMessage) []json.RawMessage {
	if len(details) < 2 {
		return details
	}
	type detailGroup struct {
		fieldName string
		fields    map[string]json.RawMessage
		firstRaw  json.RawMessage
		fragments strings.Builder
		count     int
	}
	type detailItem struct {
		raw   json.RawMessage
		group *detailGroup
	}
	items := make([]detailItem, 0, len(details))
	groups := make(map[string]*detailGroup, len(details))
	for _, raw := range details {
		var fields map[string]json.RawMessage
		if json.Unmarshal(raw, &fields) != nil {
			items = append(items, detailItem{raw: append(json.RawMessage(nil), raw...)})
			continue
		}
		fieldName, fragment := reasoningDetailFragment(fields)
		if fieldName == "" || fragment == "" {
			items = append(items, detailItem{raw: append(json.RawMessage(nil), raw...)})
			continue
		}
		delete(fields, fieldName)
		identity, err := json.Marshal(fields)
		if err != nil {
			items = append(items, detailItem{raw: append(json.RawMessage(nil), raw...)})
			continue
		}
		key := fieldName + "\x00" + string(identity)
		group := groups[key]
		if group == nil {
			group = &detailGroup{
				fieldName: fieldName,
				fields:    fields,
				firstRaw:  append(json.RawMessage(nil), raw...),
			}
			groups[key] = group
			items = append(items, detailItem{group: group})
		}
		group.fragments.WriteString(fragment)
		group.count++
	}
	out := make([]json.RawMessage, 0, len(items))
	for _, item := range items {
		if item.group == nil {
			out = append(out, item.raw)
			continue
		}
		group := item.group
		if group.count == 1 {
			out = append(out, group.firstRaw)
			continue
		}
		encoded, err := json.Marshal(group.fragments.String())
		if err != nil {
			out = append(out, group.firstRaw)
			continue
		}
		group.fields[group.fieldName] = encoded
		compacted, err := json.Marshal(group.fields)
		if err != nil {
			out = append(out, group.firstRaw)
			continue
		}
		out = append(out, compacted)
	}
	return out
}

func reasoningDetailFragment(fields map[string]json.RawMessage) (string, string) {
	for _, fieldName := range []string{"text", "summary"} {
		raw, ok := fields[fieldName]
		if !ok {
			continue
		}
		var value string
		if json.Unmarshal(raw, &value) == nil {
			return fieldName, value
		}
	}
	return "", ""
}

func sanitizeChatToolMessages(messages []chatMessage) []chatMessage {
	if len(messages) == 0 {
		return messages
	}
	out := make([]chatMessage, 0, len(messages))
	for i := 0; i < len(messages); {
		msg := messages[i]
		if msg.Role == "tool" {
			i++
			continue
		}
		if msg.Role != "assistant" || len(msg.ToolCalls) == 0 {
			out = append(out, msg)
			i++
			continue
		}

		required, ok := requiredToolCallIDs(msg.ToolCalls)
		if !ok {
			i++
			continue
		}
		seen := make(map[string]bool, len(required))
		j := i + 1
		valid := true
		for j < len(messages) && messages[j].Role == "tool" {
			toolCallID := messages[j].ToolCallID
			if !required[toolCallID] || seen[toolCallID] {
				valid = false
				break
			}
			seen[toolCallID] = true
			j++
			if len(seen) == len(required) {
				break
			}
		}
		if valid && len(seen) == len(required) {
			out = append(out, msg)
			out = append(out, messages[i+1:j]...)
			i = j
			continue
		}
		i++
	}
	return out
}

func requiredToolCallIDs(calls []chatToolCall) (map[string]bool, bool) {
	if len(calls) == 0 {
		return nil, false
	}
	ids := make(map[string]bool, len(calls))
	for _, call := range calls {
		if call.ID == "" || ids[call.ID] {
			return nil, false
		}
		ids[call.ID] = true
	}
	return ids, true
}

func chatMediaContent(parts []provider.Part) []chatContentPart {
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
	out := make([]chatContentPart, 0, len(parts))
	for _, part := range parts {
		switch part.Type {
		case "", provider.PartText:
			if part.Text != "" {
				out = append(out, chatContentPart{Type: "text", Text: part.Text})
			}
		case provider.PartImage:
			if len(part.Data) > 0 {
				out = append(out, chatContentPart{Type: "image_url", ImageURL: &chatImageURL{URL: provider.ImageDataURL(part.MIME, part.Data)}})
			}
		case provider.PartAudio:
			if len(part.Data) > 0 {
				out = append(out, chatContentPart{Type: "input_audio", InputAudio: &chatInputAudio{
					Data:   base64.StdEncoding.EncodeToString(part.Data),
					Format: provider.AudioFormat(part.MIME),
				}})
			}
		}
	}
	return out
}

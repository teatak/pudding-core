package tool

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"
)

const (
	webDefaultTimeout = 15 * time.Second

	webDefaultMaxResults = 5
	webMaxResults        = 10

	webDefaultMaxChars = 4000
	webMaxChars        = 20000

	tavilySearchEndpoint  = "https://api.tavily.com/search"
	tavilyExtractEndpoint = "https://api.tavily.com/extract"
)

func (r *BuiltinRunner) webSearch(ctx context.Context, call Call) Result {
	out := Result{CallID: call.CallID, Name: call.Name}
	args, err := decodeToolArgs(call.Args)
	if err != nil {
		return toolJSON(out, false, map[string]any{"ok": false, "reason": "invalid_arguments", "error": err.Error()})
	}
	query := strings.TrimSpace(stringArg(args, "query"))
	if query == "" {
		return toolJSON(out, false, map[string]any{"ok": false, "reason": "missing_query"})
	}
	provider, apiKey, err := r.resolveWebTool(ctx, "search")
	if err != nil {
		return toolJSON(out, false, map[string]any{"ok": false, "reason": "config_error", "error": err.Error()})
	}
	if provider == "" || apiKey == "" {
		return toolJSON(out, false, webKeyMissingResult(provider))
	}

	maxResults := clampInt(intArg(args, "max_results"), 1, webMaxResults)
	if maxResults == 0 {
		maxResults = webDefaultMaxResults
	}
	if provider == "deepseek" {
		return r.deepSeekWebSearch(ctx, out, args, query, apiKey, maxResults)
	}
	depth := strings.ToLower(strings.TrimSpace(stringArg(args, "depth")))
	if depth != "advanced" {
		depth = "basic"
	}
	topic := strings.ToLower(strings.TrimSpace(stringArg(args, "topic")))
	if topic != "news" {
		topic = "general"
	}
	includeAnswer := true
	if v, exists := boolArg(args, "include_answer"); exists {
		includeAnswer = v
	}

	payload := map[string]any{
		"query":               query,
		"max_results":         maxResults,
		"search_depth":        depth,
		"topic":               topic,
		"include_answer":      includeAnswer,
		"include_raw_content": false,
	}
	data, elapsed, status, err := r.webPost(ctx, r.tavilySearch, apiKey, payload, 512*1024)
	if err != nil {
		return toolJSON(out, false, webRequestError(provider, status, err))
	}
	if status != http.StatusOK {
		return toolJSON(out, false, webHTTPError(provider, status, data))
	}

	var parsed tavilySearchResponse
	if err := json.Unmarshal(data, &parsed); err != nil {
		return toolJSON(out, false, map[string]any{"ok": false, "reason": "decode_error", "error": err.Error(), "body": truncateString(string(data), 512)})
	}
	results := make([]map[string]any, 0, len(parsed.Results))
	for _, item := range parsed.Results {
		result := map[string]any{
			"title":   item.Title,
			"url":     item.URL,
			"snippet": item.Content,
		}
		if item.Score > 0 {
			result["score"] = item.Score
		}
		if item.PublishedDate != "" {
			result["published_date"] = item.PublishedDate
		}
		results = append(results, result)
	}
	response := map[string]any{
		"ok":               true,
		"provider":         "tavily",
		"query":            query,
		"results":          results,
		"result_count":     len(results),
		"response_time_ms": elapsed.Milliseconds(),
	}
	if parsed.Answer != "" {
		response["answer"] = parsed.Answer
	}
	return withResultSummary(toolJSON(out, true, response), SummaryReturnedItems, len(results))
}

func (r *BuiltinRunner) webFetch(ctx context.Context, call Call) Result {
	out := Result{CallID: call.CallID, Name: call.Name}
	args, err := decodeToolArgs(call.Args)
	if err != nil {
		return toolJSON(out, false, map[string]any{"ok": false, "reason": "invalid_arguments", "error": err.Error()})
	}
	rawURL := strings.TrimSpace(stringArg(args, "url"))
	if rawURL == "" {
		return toolJSON(out, false, map[string]any{"ok": false, "reason": "missing_url"})
	}
	parsedURL, err := url.Parse(rawURL)
	if err != nil || (parsedURL.Scheme != "http" && parsedURL.Scheme != "https") || parsedURL.Host == "" || parsedURL.User != nil {
		return toolJSON(out, false, map[string]any{"ok": false, "reason": "invalid_url", "hint": "url must start with http:// or https:// and must not contain credentials"})
	}
	provider, apiKey, err := r.resolveWebTool(ctx, "fetch")
	if err != nil {
		return toolJSON(out, false, map[string]any{"ok": false, "reason": "config_error", "error": err.Error()})
	}
	if provider == "" || apiKey == "" {
		return toolJSON(out, false, webKeyMissingResult(provider))
	}

	depth := strings.ToLower(strings.TrimSpace(stringArg(args, "depth")))
	if depth != "advanced" {
		depth = "basic"
	}
	maxChars := clampInt(intArg(args, "max_chars"), 0, webMaxChars)
	if maxChars == 0 {
		maxChars = webDefaultMaxChars
	}
	if provider == "deepseek" {
		return r.deepSeekWebFetch(ctx, out, args, rawURL, apiKey, maxChars)
	}
	payload := map[string]any{
		"urls":          []string{rawURL},
		"extract_depth": depth,
	}
	data, elapsed, status, err := r.webPost(ctx, r.tavilyExtract, apiKey, payload, 2*1024*1024)
	if err != nil {
		return toolJSON(out, false, webRequestError(provider, status, err))
	}
	if status != http.StatusOK {
		return toolJSON(out, false, webHTTPError(provider, status, data))
	}

	var parsed tavilyExtractResponse
	if err := json.Unmarshal(data, &parsed); err != nil {
		return toolJSON(out, false, map[string]any{"ok": false, "reason": "decode_error", "error": err.Error(), "body": truncateString(string(data), 512)})
	}
	if len(parsed.Results) == 0 {
		detail := ""
		if len(parsed.FailedResults) > 0 {
			detail = parsed.FailedResults[0].Error
		}
		return toolJSON(out, false, map[string]any{"ok": false, "reason": "extract_failed", "url": rawURL, "detail": detail})
	}
	first := parsed.Results[0]
	content := first.RawContent
	truncated := false
	if len(content) > maxChars {
		content = safeTruncateUTF8(content, maxChars)
		truncated = true
	}
	return withResultSummary(toolJSON(out, true, map[string]any{
		"ok":               true,
		"provider":         "tavily",
		"url":              first.URL,
		"content":          content,
		"content_length":   len(first.RawContent),
		"truncated":        truncated,
		"response_time_ms": elapsed.Milliseconds(),
	}), SummaryReadChars, utf8.RuneCountInString(content))
}

func (r *BuiltinRunner) resolveWebTool(ctx context.Context, feature string) (string, string, error) {
	if r.webConfig == nil {
		return "", "", nil
	}
	provider, key, err := r.webConfig.ResolveWebTool(ctx, feature)
	if err == nil && provider != "" && provider != "tavily" && provider != "deepseek" {
		err = fmt.Errorf("unsupported web provider %q", provider)
	}
	return provider, strings.TrimSpace(key), err
}

func (r *BuiltinRunner) webPost(ctx context.Context, endpoint, apiKey string, payload map[string]any, limit int64) ([]byte, time.Duration, int, error) {
	body, err := json.Marshal(payload)
	if err != nil {
		return nil, 0, 0, err
	}
	ctx, cancel := context.WithTimeout(ctx, webDefaultTimeout)
	defer cancel()
	start := time.Now()
	for attempt := 0; ; attempt++ {
		req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(body))
		if err != nil {
			return nil, time.Since(start), 0, err
		}
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Authorization", "Bearer "+apiKey)
		resp, err := r.webHTTPClient.Do(req)
		status := 0
		var data []byte
		delay := 250 * time.Millisecond
		if resp != nil && err == nil {
			status = resp.StatusCode
			data, err = io.ReadAll(io.LimitReader(resp.Body, limit+1))
			resp.Body.Close()
			if int64(len(data)) > limit {
				return nil, time.Since(start), status, fmt.Errorf("web response exceeds %d bytes", limit)
			}
			if value := resp.Header.Get("Retry-After"); value != "" {
				if seconds, parseErr := strconv.Atoi(value); parseErr == nil {
					if seconds > 2 {
						delay = 3 * time.Second
					} else if seconds >= 0 {
						delay = time.Duration(seconds) * time.Second
					}
				} else if until, parseErr := http.ParseTime(value); parseErr == nil {
					delay = time.Until(until)
				}
			}
		}
		var netErr net.Error
		retry := status == http.StatusRequestTimeout || status == http.StatusTooManyRequests || status >= 500 || (errors.As(err, &netErr) && netErr.Timeout())
		if ctx.Err() != nil {
			return nil, time.Since(start), status, ctx.Err()
		}
		if attempt >= 1 || !retry || delay > 2*time.Second {
			return data, time.Since(start), status, err
		}
		timer := time.NewTimer(max(delay, 0))
		select {
		case <-ctx.Done():
			timer.Stop()
			return nil, time.Since(start), status, ctx.Err()
		case <-timer.C:
		}
	}
}

func webHTTPError(provider string, status int, data []byte) map[string]any {
	result := map[string]any{"ok": false, "provider": provider, "reason": "http_error", "status": status, "body": truncateString(string(data), 512)}
	switch status {
	case http.StatusUnauthorized, http.StatusForbidden:
		if status == http.StatusForbidden && provider == "deepseek" {
			result["reason"] = "access_denied"
			result["hint"] = "DeepSeek rejected this request; Fetch may block a URL by security policy"
			break
		}
		result = webKeyMissingResult(provider)
		result["reason"] = "auth_failed"
		result["status"] = status
		result["hint"] = "check or replace the selected provider's API Key in Plugins > Web Search & Fetch > Provider configuration"
	case http.StatusPaymentRequired:
		result["reason"] = "insufficient_balance"
		result["hint"] = "top up the selected provider account before retrying"
	case http.StatusNotFound:
		result["reason"] = "not_found"
		result["hint"] = "the provider could not retrieve usable content; check the URL or use another source"
	case http.StatusTooManyRequests:
		result["reason"] = "rate_limited"
		result["hint"] = "provider rate limit or quota reached; wait before retrying"
	case http.StatusRequestTimeout, http.StatusGatewayTimeout:
		result["reason"] = "timeout"
	}
	if status == http.StatusRequestTimeout || status == http.StatusTooManyRequests || status >= 500 {
		result["retryable"] = true
	}
	return result
}

func webRequestError(provider string, status int, err error) map[string]any {
	reason := "network_error"
	if status != 0 {
		reason = "read_error"
	}
	var netErr net.Error
	if errors.Is(err, context.DeadlineExceeded) || (errors.As(err, &netErr) && netErr.Timeout()) {
		reason = "timeout"
	} else if errors.Is(err, context.Canceled) {
		reason = "canceled"
	}
	return map[string]any{"ok": false, "provider": provider, "reason": reason, "status": status, "error": err.Error()}
}

func webKeyMissingResult(provider string) map[string]any {
	result := map[string]any{
		"ok":            false,
		"reason":        "api_key_missing",
		"provider":      provider,
		"settings_path": "Plugins > Web Search & Fetch > Provider configuration",
		"hint":          "save an API Key, then select a provider in Plugins > Web Search & Fetch > Provider configuration",
		"next_step":     "Open Plugins > Web Search & Fetch > Provider configuration, save a Tavily or DeepSeek API Key, then select the provider. Saving a key does not change the selected provider or the plugin enablement.",
	}
	if provider == "deepseek" {
		result["signup_url"] = "https://platform.deepseek.com/api_keys"
	} else if provider == "tavily" {
		result["signup_url"] = "https://app.tavily.com/home"
	} else {
		result["reason"] = "provider_not_configured"
	}
	return result
}

func decodeToolArgs(raw json.RawMessage) (map[string]any, error) {
	if len(strings.TrimSpace(string(raw))) == 0 {
		return map[string]any{}, nil
	}
	var args map[string]any
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.UseNumber()
	if err := dec.Decode(&args); err != nil {
		return nil, err
	}
	if args == nil {
		return nil, fmt.Errorf("arguments must be an object")
	}
	return args, nil
}

func toolJSON(out Result, ok bool, payload map[string]any) Result {
	data, err := json.Marshal(payload)
	if err != nil {
		out.Ok = false
		out.Content = err.Error()
		return out
	}
	out.Ok = ok
	out.Content = string(data)
	return out
}

func withResultSummary(out Result, kind string, count int) Result {
	if out.Ok && kind != "" {
		out.SummaryKind = kind
		out.SummaryCount = count
	}
	return out
}

func stringArg(args map[string]any, name string) string {
	switch v := args[name].(type) {
	case string:
		return v
	case json.Number:
		return v.String()
	case float64:
		return strconv.FormatFloat(v, 'f', -1, 64)
	case bool:
		return strconv.FormatBool(v)
	default:
		return ""
	}
}

func intArg(args map[string]any, name string) int {
	switch v := args[name].(type) {
	case int:
		return v
	case int64:
		return int(v)
	case float64:
		return int(v)
	case json.Number:
		i, _ := v.Int64()
		return int(i)
	case string:
		i, _ := strconv.Atoi(strings.TrimSpace(v))
		return i
	default:
		return 0
	}
}

func boolArg(args map[string]any, name string) (bool, bool) {
	v, ok := args[name]
	if !ok {
		return false, false
	}
	switch value := v.(type) {
	case bool:
		return value, true
	case string:
		switch strings.ToLower(strings.TrimSpace(value)) {
		case "true", "1", "yes":
			return true, true
		case "false", "0", "no":
			return false, true
		}
	}
	return false, false
}

func clampInt(v, min, max int) int {
	if v == 0 {
		return 0
	}
	if v < min {
		return min
	}
	if v > max {
		return max
	}
	return v
}

type tavilySearchResponse struct {
	Answer  string               `json:"answer"`
	Results []tavilySearchResult `json:"results"`
}

type tavilySearchResult struct {
	Title         string  `json:"title"`
	URL           string  `json:"url"`
	Content       string  `json:"content"`
	Score         float64 `json:"score"`
	PublishedDate string  `json:"published_date"`
}

type tavilyExtractResponse struct {
	Results       []tavilyExtractResult `json:"results"`
	FailedResults []tavilyExtractFailed `json:"failed_results"`
}

type tavilyExtractResult struct {
	URL        string `json:"url"`
	RawContent string `json:"raw_content"`
}

type tavilyExtractFailed struct {
	URL   string `json:"url"`
	Error string `json:"error"`
}

func safeTruncateUTF8(s string, maxBytes int) string {
	if len(s) <= maxBytes {
		return s
	}
	cut := maxBytes
	for cut > 0 && (s[cut]&0xC0) == 0x80 {
		cut--
	}
	return s[:cut]
}

func truncateString(s string, max int) string {
	if len(s) <= max {
		return s
	}
	return s[:max] + "...(truncated)"
}

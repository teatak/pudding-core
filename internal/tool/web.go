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
	apiKey, ok, err := r.tavilyAPIKey(ctx)
	if err != nil {
		return toolJSON(out, false, map[string]any{"ok": false, "reason": "config_error", "error": err.Error()})
	}
	if !ok {
		return toolJSON(out, false, tavilyKeyMissingResult())
	}

	maxResults := clampInt(intArg(args, "max_results"), 1, webMaxResults)
	if maxResults == 0 {
		maxResults = webDefaultMaxResults
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
	data, elapsed, status, err := r.tavilyPost(ctx, r.tavilySearch, apiKey, payload, 512*1024)
	if err != nil {
		return toolJSON(out, false, tavilyError("network_error", status, err, data))
	}
	if status != http.StatusOK {
		return toolJSON(out, false, tavilyHTTPError(status, data))
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
	if err != nil || (parsedURL.Scheme != "http" && parsedURL.Scheme != "https") || parsedURL.Host == "" {
		return toolJSON(out, false, map[string]any{"ok": false, "reason": "invalid_url", "hint": "url must start with http:// or https://"})
	}
	apiKey, ok, err := r.tavilyAPIKey(ctx)
	if err != nil {
		return toolJSON(out, false, map[string]any{"ok": false, "reason": "config_error", "error": err.Error()})
	}
	if !ok {
		return toolJSON(out, false, tavilyKeyMissingResult())
	}

	depth := strings.ToLower(strings.TrimSpace(stringArg(args, "depth")))
	if depth != "advanced" {
		depth = "basic"
	}
	maxChars := clampInt(intArg(args, "max_chars"), 0, webMaxChars)
	if maxChars == 0 {
		maxChars = webDefaultMaxChars
	}
	payload := map[string]any{
		"urls":          []string{rawURL},
		"extract_depth": depth,
	}
	data, elapsed, status, err := r.tavilyPost(ctx, r.tavilyExtract, apiKey, payload, 2*1024*1024)
	if err != nil {
		return toolJSON(out, false, tavilyError("network_error", status, err, data))
	}
	if status != http.StatusOK {
		return toolJSON(out, false, tavilyHTTPError(status, data))
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

func (r *BuiltinRunner) tavilyAPIKey(ctx context.Context) (string, bool, error) {
	if r.webConfig == nil {
		return "", false, nil
	}
	key, ok, err := r.webConfig.TavilyAPIKey(ctx)
	return strings.TrimSpace(key), ok && strings.TrimSpace(key) != "", err
}

func (r *BuiltinRunner) tavilyPost(ctx context.Context, endpoint, apiKey string, payload map[string]any, limit int64) ([]byte, time.Duration, int, error) {
	body, err := json.Marshal(payload)
	if err != nil {
		return nil, 0, 0, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(body))
	if err != nil {
		return nil, 0, 0, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+apiKey)
	start := time.Now()
	resp, err := r.webHTTPClient.Do(req)
	elapsed := time.Since(start)
	if err != nil {
		return nil, elapsed, 0, err
	}
	defer resp.Body.Close()
	data, err := io.ReadAll(io.LimitReader(resp.Body, limit))
	if err != nil {
		return data, elapsed, resp.StatusCode, err
	}
	return data, elapsed, resp.StatusCode, nil
}

func tavilyHTTPError(status int, data []byte) map[string]any {
	switch status {
	case http.StatusUnauthorized, http.StatusForbidden:
		result := tavilyKeyMissingResult()
		result["reason"] = "auth_failed"
		result["status"] = status
		result["hint"] = "check or replace the Tavily API Key in Settings > Tools"
		result["next_step"] = "Open Settings > Tools > Web search (Tavily), get a Tavily API Key if needed, paste it, then save."
		return result
	case http.StatusTooManyRequests:
		return map[string]any{"ok": false, "reason": "rate_limited", "status": status, "hint": "Tavily quota exhausted; try later or upgrade"}
	default:
		return map[string]any{"ok": false, "reason": "http_error", "status": status, "body": truncateString(string(data), 512)}
	}
}

func tavilyError(reason string, status int, err error, data []byte) map[string]any {
	if status != 0 {
		return map[string]any{"ok": false, "reason": "read_error", "status": status, "error": err.Error(), "body": truncateString(string(data), 512)}
	}
	var netErr net.Error
	if errors.Is(err, context.DeadlineExceeded) || (errors.As(err, &netErr) && netErr.Timeout()) {
		reason = "timeout"
	}
	return map[string]any{"ok": false, "reason": reason, "error": err.Error()}
}

func tavilyKeyMissingResult() map[string]any {
	return map[string]any{
		"ok":            false,
		"reason":        "api_key_missing",
		"provider":      "tavily",
		"settings_path": "Settings > Tools > Web search (Tavily)",
		"signup_url":    "https://app.tavily.com/home",
		"hint":          "configure Tavily API Key in Settings > Tools",
		"next_step":     "Open Settings > Tools > Web search (Tavily), get a Tavily API Key at https://app.tavily.com/home, paste it, then save.",
	}
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

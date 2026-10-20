package tool

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"unicode/utf8"
)

const (
	deepseekSearchEndpoint = "https://api.deepseek.com/v1/tools/search"
	deepseekFetchEndpoint  = "https://api.deepseek.com/v1/tools/fetch"
)

func (r *BuiltinRunner) deepSeekWebSearch(ctx context.Context, out Result, args map[string]any, query, apiKey string, count int) Result {
	if utf8.RuneCountInString(query) > 128 {
		return toolJSON(out, false, map[string]any{
			"ok": false, "provider": "deepseek", "reason": "invalid_arguments",
			"hint": "DeepSeek query must be 1-128 Unicode characters; shorten the query and try again",
		})
	}
	data, elapsed, status, err := r.webPost(ctx, r.deepseekSearch, apiKey, map[string]any{
		"query": query, "count": count, "need_content": false,
	}, 512*1024)
	if err != nil {
		return toolJSON(out, false, webRequestError("deepseek", status, err))
	}
	if status != http.StatusOK {
		return toolJSON(out, false, webHTTPError("deepseek", status, data))
	}
	var parsed struct {
		Results []deepseekSearchResult `json:"results"`
	}
	if err := json.Unmarshal(data, &parsed); err != nil || parsed.Results == nil {
		return toolJSON(out, false, map[string]any{"ok": false, "provider": "deepseek", "reason": "decode_error", "hint": "expected a search results array"})
	}
	results := make([]map[string]any, 0, len(parsed.Results))
	for _, item := range parsed.Results {
		result := map[string]any{
			"title": item.Title, "url": item.URL, "snippet": item.Snippet,
			"summary": item.Summary, "rerank_score": item.RerankScore,
		}
		if item.Date != "" {
			result["published_date"] = item.Date
		}
		if item.SiteName != "" {
			result["site_name"] = item.SiteName
		}
		if item.SiteIcon != "" {
			result["site_icon"] = item.SiteIcon
		}
		if item.Authority >= 0 {
			result["authority"] = item.Authority
		}
		results = append(results, result)
	}
	response := map[string]any{
		"ok": true, "provider": "deepseek", "query": query, "results": results,
		"result_count": len(results), "response_time_ms": elapsed.Milliseconds(),
	}
	deepSeekIgnoredParameters(response, args, "depth", "topic", "include_answer")
	return withResultSummary(toolJSON(out, true, response), SummaryReturnedItems, len(results))
}

func (r *BuiltinRunner) deepSeekWebFetch(ctx context.Context, out Result, args map[string]any, rawURL, apiKey string, maxChars int) Result {
	if utf8.RuneCountInString(rawURL) > 8192 {
		return toolJSON(out, false, map[string]any{"ok": false, "provider": "deepseek", "reason": "invalid_url", "hint": "DeepSeek URL must not exceed 8192 characters"})
	}
	data, elapsed, status, err := r.webPost(ctx, r.deepseekFetch, apiKey, map[string]any{"url": rawURL}, 2*1024*1024)
	if err != nil {
		return toolJSON(out, false, webRequestError("deepseek", status, err))
	}
	if status != http.StatusOK {
		return toolJSON(out, false, webHTTPError("deepseek", status, data))
	}
	var parsed struct {
		Title   string  `json:"title"`
		Content *string `json:"content"`
	}
	if err := json.Unmarshal(data, &parsed); err != nil || parsed.Content == nil {
		return toolJSON(out, false, map[string]any{"ok": false, "provider": "deepseek", "reason": "decode_error", "hint": "expected Markdown page content"})
	}
	content := *parsed.Content
	if strings.TrimSpace(content) == "" {
		return toolJSON(out, false, map[string]any{"ok": false, "provider": "deepseek", "reason": "extract_failed", "url": rawURL})
	}
	truncated := len(content) > maxChars
	if truncated {
		content = safeTruncateUTF8(content, maxChars)
	}
	response := map[string]any{
		"ok": true, "provider": "deepseek", "url": rawURL, "title": parsed.Title,
		"content": content, "content_length": len(*parsed.Content), "truncated": truncated,
		"response_time_ms": elapsed.Milliseconds(),
	}
	deepSeekIgnoredParameters(response, args, "depth")
	return withResultSummary(toolJSON(out, true, response), SummaryReadChars, utf8.RuneCountInString(content))
}

func deepSeekIgnoredParameters(response, args map[string]any, names ...string) {
	var ignored []string
	for _, name := range names {
		if _, exists := args[name]; exists {
			ignored = append(ignored, name)
		}
	}
	if len(ignored) > 0 {
		response["ignored_parameters"] = ignored
		response["hint"] = "these parameters are Tavily-only and were not applied by DeepSeek"
	}
}

type deepseekSearchResult struct {
	Title       string  `json:"title"`
	URL         string  `json:"url"`
	Snippet     string  `json:"snippet"`
	Summary     string  `json:"summary"`
	RerankScore float64 `json:"rerank_score"`
	SiteName    string  `json:"site_name"`
	SiteIcon    string  `json:"site_icon"`
	Date        string  `json:"date"`
	Authority   int     `json:"authority"`
}

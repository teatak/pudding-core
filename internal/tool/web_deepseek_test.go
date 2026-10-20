package tool

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"sync/atomic"
	"testing"
	"time"
	"unicode/utf8"

	"github.com/teatak/pudding-core/internal/config"
)

func TestBuiltinWebSearchCallsDeepSeek(t *testing.T) {
	query := strings.Repeat("中", 128)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/search" || r.Method != http.MethodPost || r.Header.Get("Authorization") != "Bearer ds-key" {
			t.Errorf("unexpected request: %s %s", r.Method, r.URL.Path)
		}
		var body map[string]any
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Error(err)
		}
		if !reflect.DeepEqual(body, map[string]any{"query": query, "count": float64(3), "need_content": false}) {
			t.Errorf("unexpected payload: %+v", body)
		}
		_, _ = w.Write([]byte(`{"results":[{"title":"Source","url":"https://example.com","snippet":"short","summary":"long model summary","rerank_score":2.4,"site_name":"Example","site_icon":null,"date":"2026-10-20T00:00:00Z","authority":3}]}`))
	}))
	defer srv.Close()
	runner := NewBuiltinRunner(WithWebConfig(fakeWebConfig{provider: "deepseek", key: "ds-key"}), WithDeepSeekEndpoints(srv.URL+"/search", srv.URL+"/fetch"))
	args, _ := json.Marshal(map[string]any{"query": query, "max_results": 3, "depth": "advanced", "topic": "news", "include_answer": true})
	res := runner.Call(context.Background(), Call{Name: WebSearch, Args: args})
	if !res.Ok || res.SummaryKind != SummaryReturnedItems || res.SummaryCount != 1 {
		t.Fatalf("unexpected result: %+v", res)
	}
	payload := decodeToolResult(t, res)
	first := payload["results"].([]any)[0].(map[string]any)
	if payload["provider"] != "deepseek" || payload["answer"] != nil || first["snippet"] != "short" || first["summary"] != "long model summary" || first["published_date"] != "2026-10-20T00:00:00Z" || first["rerank_score"] != 2.4 {
		t.Fatalf("unexpected mapped result: %+v", payload)
	}
	if !reflect.DeepEqual(payload["ignored_parameters"], []any{"depth", "topic", "include_answer"}) {
		t.Fatalf("Tavily options must be explicitly reported: %+v", payload)
	}
}

func TestBuiltinWebFetchCallsDeepSeek(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body map[string]any
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Error(err)
		}
		if r.URL.Path != "/fetch" || r.Header.Get("Authorization") != "Bearer ds-key" || !reflect.DeepEqual(body, map[string]any{"url": "https://example.com/a"}) {
			t.Errorf("unexpected request: %s %+v", r.URL.Path, body)
		}
		_, _ = w.Write([]byte(`{"title":"Article","content":"中文正文"}`))
	}))
	defer srv.Close()
	runner := NewBuiltinRunner(WithWebConfig(fakeWebConfig{provider: "deepseek", key: "ds-key"}), WithDeepSeekEndpoints(srv.URL+"/search", srv.URL+"/fetch"))
	res := runner.Call(context.Background(), Call{Name: WebFetch, Args: json.RawMessage(`{"url":"https://example.com/a","depth":"advanced","max_chars":5}`)})
	if !res.Ok || res.SummaryKind != SummaryReadChars || res.SummaryCount != 1 {
		t.Fatalf("unexpected result: %+v", res)
	}
	payload := decodeToolResult(t, res)
	if payload["content"] != "中" || !utf8.ValidString(payload["content"].(string)) || payload["title"] != "Article" || payload["truncated"] != true || payload["content_length"] != float64(12) {
		t.Fatalf("unexpected fetch result: %+v", payload)
	}
	if !reflect.DeepEqual(payload["ignored_parameters"], []any{"depth"}) {
		t.Fatalf("ignored extract depth must be reported: %+v", payload)
	}
}

func TestDeepSeekArgumentLimitsDoNotCallUpstream(t *testing.T) {
	var calls atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { calls.Add(1) }))
	defer srv.Close()
	runner := NewBuiltinRunner(WithWebConfig(fakeWebConfig{provider: "deepseek", key: "ds-key"}), WithDeepSeekEndpoints(srv.URL, srv.URL))
	for _, tc := range []struct{ name, field, value, reason string }{
		{WebSearch, "query", strings.Repeat("中", 129), "invalid_arguments"},
		{WebSearch, "query", "  ", "missing_query"},
		{WebFetch, "url", "https://example.com/" + strings.Repeat("x", 8192), "invalid_url"},
		{WebFetch, "url", "https://user:password@example.com/", "invalid_url"},
		{WebFetch, "url", "file:///tmp/content", "invalid_url"},
	} {
		args, _ := json.Marshal(map[string]string{tc.field: tc.value})
		res := runner.Call(context.Background(), Call{Name: tc.name, Args: args})
		if res.Ok || decodeToolResult(t, res)["reason"] != tc.reason {
			t.Fatalf("unexpected validation result: %+v", res)
		}
	}
	if calls.Load() != 0 {
		t.Fatalf("invalid arguments made %d upstream calls", calls.Load())
	}
}

func TestDeepSeekErrorsAndEmptyResults(t *testing.T) {
	for _, tc := range []struct {
		name, tool, body, reason string
		status, calls            int
	}{
		{"no results", WebSearch, `{"results":[]}`, "", 200, 1},
		{"missing results", WebSearch, `{}`, "decode_error", 200, 1},
		{"bad json", WebFetch, `{`, "decode_error", 200, 1},
		{"empty content", WebFetch, `{"title":"Empty","content":""}`, "extract_failed", 200, 1},
		{"missing content", WebFetch, `{"title":"Missing"}`, "decode_error", 200, 1},
		{"bad key", WebSearch, `{}`, "auth_failed", 401, 1},
		{"balance", WebSearch, `{}`, "insufficient_balance", 402, 1},
		{"blocked URL", WebFetch, `{}`, "access_denied", 403, 1},
		{"unavailable URL", WebFetch, `{}`, "not_found", 404, 1},
		{"rate limit", WebSearch, `{}`, "rate_limited", 429, 2},
		{"timeout", WebFetch, `{}`, "timeout", 504, 2},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var calls atomic.Int32
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				calls.Add(1)
				w.Header().Set("Retry-After", "0")
				w.WriteHeader(tc.status)
				_, _ = w.Write([]byte(tc.body))
			}))
			defer srv.Close()
			runner := NewBuiltinRunner(WithWebConfig(fakeWebConfig{provider: "deepseek", key: "ds-key"}), WithDeepSeekEndpoints(srv.URL, srv.URL))
			res := runner.Call(context.Background(), Call{Name: tc.tool, Args: json.RawMessage(`{"query":"test","url":"https://example.com"}`)})
			payload := decodeToolResult(t, res)
			if res.Ok != (tc.reason == "") || (tc.reason != "" && payload["reason"] != tc.reason) || calls.Load() != int32(tc.calls) {
				t.Fatalf("result=%+v calls=%d", res, calls.Load())
			}
			if tc.reason == "" && (payload["result_count"] != float64(0) || len(payload["results"].([]any)) != 0) {
				t.Fatalf("empty results must succeed: %+v", payload)
			}
		})
	}
}

func TestWebProviderSelectionControlsExecution(t *testing.T) {
	ctx := context.Background()
	cfg := config.NewManager(t.TempDir())
	if err := cfg.Prepare(); err != nil {
		t.Fatal(err)
	}
	keyT, keyD := "tvly-key", "ds-key"
	if _, err := cfg.PatchWebTools(ctx, config.WebToolsUpdate{Providers: map[string]config.WebToolProviderUpdate{
		"tavily": {APIKey: &keyT}, "deepseek": {APIKey: &keyD},
	}}); err != nil {
		t.Fatal(err)
	}
	var calls atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		switch r.URL.Path {
		case "/tavily/search":
			if r.Header.Get("Authorization") != "Bearer tvly-key" {
				t.Error("wrong Tavily key")
			}
			_, _ = w.Write([]byte(`{"results":[]}`))
		case "/tavily/fetch":
			if r.Header.Get("Authorization") != "Bearer tvly-key" {
				t.Error("wrong Tavily key")
			}
			_, _ = w.Write([]byte(`{"results":[{"url":"https://example.com","raw_content":"Tavily"}]}`))
		case "/deepseek/search":
			if r.Header.Get("Authorization") != "Bearer ds-key" {
				t.Error("wrong DeepSeek key")
			}
			_, _ = w.Write([]byte(`{"results":[]}`))
		case "/deepseek/fetch":
			if r.Header.Get("Authorization") != "Bearer ds-key" {
				t.Error("wrong DeepSeek key")
			}
			_, _ = w.Write([]byte(`{"title":"DeepSeek","content":"Markdown"}`))
		default:
			t.Errorf("unexpected endpoint %s", r.URL.Path)
		}
	}))
	defer srv.Close()
	runner := NewBuiltinRunner(WithWebConfig(cfg), WithTavilyEndpoints(srv.URL+"/tavily/search", srv.URL+"/tavily/fetch"), WithDeepSeekEndpoints(srv.URL+"/deepseek/search", srv.URL+"/deepseek/fetch"))
	args := json.RawMessage(`{"query":"test","url":"https://example.com"}`)
	res := runner.Call(ctx, Call{Name: WebSearch, Args: args})
	if res.Ok || decodeToolResult(t, res)["reason"] != "provider_not_configured" || calls.Load() != 0 {
		t.Fatalf("saving credentials must not enable search: %+v calls=%d", res, calls.Load())
	}
	for _, selection := range [][2]string{{"tavily", "tavily"}, {"deepseek", "deepseek"}, {"deepseek", "tavily"}} {
		if _, err := cfg.PatchWebTools(ctx, config.WebToolsUpdate{SearchProvider: &selection[0], FetchProvider: &selection[1]}); err != nil {
			t.Fatal(err)
		}
		for i, name := range []string{WebSearch, WebFetch} {
			res := runner.Call(ctx, Call{Name: name, Args: args})
			if !res.Ok || decodeToolResult(t, res)["provider"] != selection[i] {
				t.Fatalf("selected %v: %+v", selection, res)
			}
		}
	}
}

func TestWebRetryStopsOnCancellation(t *testing.T) {
	var calls atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		w.WriteHeader(http.StatusTooManyRequests)
	}))
	defer srv.Close()
	runner := NewBuiltinRunner(WithWebConfig(fakeWebConfig{provider: "deepseek", key: "ds-key"}), WithDeepSeekEndpoints(srv.URL, srv.URL))
	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()
	res := runner.Call(ctx, Call{Name: WebSearch, Args: json.RawMessage(`{"query":"test"}`)})
	if res.Ok || decodeToolResult(t, res)["reason"] != "timeout" || calls.Load() != 1 {
		t.Fatalf("retry must stop when the caller deadline expires: %+v calls=%d", res, calls.Load())
	}
}

func TestWebTransientRetryAndRetryAfter(t *testing.T) {
	for _, retryAfter := range []string{"0", "120"} {
		t.Run(retryAfter, func(t *testing.T) {
			var calls atomic.Int32
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if calls.Add(1) == 1 {
					w.Header().Set("Retry-After", retryAfter)
					w.WriteHeader(http.StatusServiceUnavailable)
					return
				}
				_, _ = w.Write([]byte(`{"results":[]}`))
			}))
			defer srv.Close()
			runner := NewBuiltinRunner(WithWebConfig(fakeWebConfig{provider: "deepseek", key: "ds-key"}), WithDeepSeekEndpoints(srv.URL, srv.URL))
			res := runner.Call(context.Background(), Call{Name: WebSearch, Args: json.RawMessage(`{"query":"test"}`)})
			wantCalls := int32(2)
			if retryAfter == "120" {
				wantCalls = 1
			}
			if calls.Load() != wantCalls || res.Ok != (retryAfter == "0") {
				t.Fatalf("Retry-After=%s: %+v calls=%d", retryAfter, res, calls.Load())
			}
		})
	}
}

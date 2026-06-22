package tool

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

type fakeWebConfig struct {
	key string
	ok  bool
	err error
}

func (f fakeWebConfig) TavilyAPIKey(context.Context) (string, bool, error) {
	return f.key, f.ok, f.err
}

func TestBuiltinWebSearchCallsTavily(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/search" {
			http.NotFound(w, r)
			return
		}
		if got := r.Header.Get("Authorization"); got != "Bearer tvly-secret" {
			t.Fatalf("unexpected authorization header %q", got)
		}
		var body map[string]any
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Fatal(err)
		}
		if body["query"] != "pudding latest" || body["topic"] != "news" || body["include_answer"] != true {
			t.Fatalf("unexpected search payload: %+v", body)
		}
		if body["max_results"] != float64(3) {
			t.Fatalf("unexpected max_results: %+v", body)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"answer":"Pudding is current.","results":[{"title":"A","url":"https://example.com/a","content":"snippet","score":0.9,"published_date":"2026-06-22"}]}`))
	}))
	defer srv.Close()

	runner := NewBuiltinRunner(WithWebConfig(fakeWebConfig{key: "tvly-secret", ok: true}), WithTavilyEndpoints(srv.URL+"/search", srv.URL+"/extract"))
	res := runner.Call(context.Background(), Call{Name: WebSearch, Args: json.RawMessage(`{"query":"pudding latest","max_results":3,"topic":"news"}`)})
	if !res.Ok {
		t.Fatalf("search should succeed: %+v", res)
	}
	if res.SummaryKind != SummaryReturnedItems || res.SummaryCount != 1 {
		t.Fatalf("unexpected search summary: %+v", res)
	}
	payload := decodeToolResult(t, res)
	if payload["ok"] != true || payload["answer"] != "Pudding is current." || payload["result_count"] != float64(1) {
		t.Fatalf("unexpected search result: %+v", payload)
	}
	results := payload["results"].([]any)
	first := results[0].(map[string]any)
	if first["url"] != "https://example.com/a" || first["published_date"] != "2026-06-22" {
		t.Fatalf("unexpected first result: %+v", first)
	}
}

func TestBuiltinWebFetchCallsTavilyExtract(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/extract" {
			http.NotFound(w, r)
			return
		}
		if got := r.Header.Get("Authorization"); got != "Bearer tvly-secret" {
			t.Fatalf("unexpected authorization header %q", got)
		}
		var body map[string]any
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Fatal(err)
		}
		urls := body["urls"].([]any)
		if len(urls) != 1 || urls[0] != "https://example.com/a" || body["extract_depth"] != "advanced" {
			t.Fatalf("unexpected extract payload: %+v", body)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"results":[{"url":"https://example.com/a","raw_content":"abcdef"}]}`))
	}))
	defer srv.Close()

	runner := NewBuiltinRunner(WithWebConfig(fakeWebConfig{key: "tvly-secret", ok: true}), WithTavilyEndpoints(srv.URL+"/search", srv.URL+"/extract"))
	res := runner.Call(context.Background(), Call{Name: WebFetch, Args: json.RawMessage(`{"url":"https://example.com/a","depth":"advanced","max_chars":4}`)})
	if !res.Ok {
		t.Fatalf("fetch should succeed: %+v", res)
	}
	if res.SummaryKind != SummaryReadChars || res.SummaryCount != 4 {
		t.Fatalf("unexpected fetch summary: %+v", res)
	}
	payload := decodeToolResult(t, res)
	if payload["ok"] != true || payload["content"] != "abcd" || payload["truncated"] != true || payload["content_length"] != float64(6) {
		t.Fatalf("unexpected fetch result: %+v", payload)
	}
}

func TestBuiltinWebSearchMissingAPIKey(t *testing.T) {
	runner := NewBuiltinRunner(WithWebConfig(fakeWebConfig{}))
	res := runner.Call(context.Background(), Call{Name: WebSearch, Args: json.RawMessage(`{"query":"x"}`)})
	if res.Ok {
		t.Fatalf("missing key should fail: %+v", res)
	}
	payload := decodeToolResult(t, res)
	if payload["reason"] != "api_key_missing" {
		t.Fatalf("unexpected missing key result: %+v", payload)
	}
	if payload["signup_url"] != "https://app.tavily.com/home" || payload["settings_path"] == "" || payload["next_step"] == "" {
		t.Fatalf("missing key result should guide user to configure Tavily: %+v", payload)
	}
}

func decodeToolResult(t *testing.T, res Result) map[string]any {
	t.Helper()
	var payload map[string]any
	if err := json.Unmarshal([]byte(res.Content), &payload); err != nil {
		t.Fatal(err)
	}
	return payload
}

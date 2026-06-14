package google

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/teatak/pudding-core/internal/provider"
)

func collect(t *testing.T, ch <-chan provider.Chunk) (string, bool, error) {
	t.Helper()
	var text strings.Builder
	for chunk := range ch {
		if chunk.Err != nil {
			return text.String(), false, chunk.Err
		}
		if chunk.Done {
			return text.String(), true, nil
		}
		text.WriteString(chunk.Delta)
	}
	return text.String(), false, nil
}

func sseFrame(texts []string, finish string) string {
	type p struct {
		Text string `json:"text"`
	}
	parts := make([]p, 0, len(texts))
	for _, t := range texts {
		parts = append(parts, p{Text: t})
	}
	frame := map[string]any{
		"candidates": []map[string]any{{
			"content":      map[string]any{"parts": parts},
			"finishReason": finish,
		}},
	}
	b, _ := json.Marshal(frame)
	return "data: " + string(b) + "\n\n"
}

func TestStreamHappyPath(t *testing.T) {
	var gotPath, gotKey string
	var gotBody generateRequest
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path + "?" + r.URL.RawQuery
		gotKey = r.Header.Get("x-goog-api-key")
		body, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(body, &gotBody)
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = io.WriteString(w, sseFrame([]string{"你好"}, ""))
		_, _ = io.WriteString(w, sseFrame([]string{",世界"}, "STOP"))
	}))
	defer srv.Close()

	client := New(Config{BaseURL: srv.URL, APIKey: "test-key", HTTPClient: srv.Client()})
	ch, err := client.Stream(context.Background(), provider.Request{
		Model:  "gemini-3-flash",
		System: "be nice",
		Config: provider.ModelConfig{
			Google: map[string]any{
				"temperature":     0.5,
				"maxOutputTokens": 2048,
			},
		},
		Messages: []provider.Message{
			{Role: provider.RoleUser, Text: "hi"},
			{Role: provider.RoleAssistant, Text: "hello"},
			{Role: provider.RoleUser, Text: "again"},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	text, done, err := collect(t, ch)
	if err != nil || !done || text != "你好,世界" {
		t.Fatalf("text=%q done=%v err=%v", text, done, err)
	}

	if !strings.Contains(gotPath, "/v1beta/models/gemini-3-flash:streamGenerateContent") || !strings.Contains(gotPath, "alt=sse") {
		t.Fatalf("unexpected path %q", gotPath)
	}
	if gotKey != "test-key" {
		t.Fatalf("api key header missing, got %q", gotKey)
	}
	if gotBody.SystemInstruction == nil || gotBody.SystemInstruction.Parts[0].Text != "be nice" {
		t.Fatalf("system_instruction not set: %+v", gotBody.SystemInstruction)
	}
	if gotBody.GenerationConfig == nil || gotBody.GenerationConfig.Temperature == nil || *gotBody.GenerationConfig.Temperature != 0.5 || gotBody.GenerationConfig.MaxOutputTokens == nil || *gotBody.GenerationConfig.MaxOutputTokens != 2048 {
		t.Fatalf("model config not applied: %+v", gotBody.GenerationConfig)
	}
	roles := []string{}
	for _, c := range gotBody.Contents {
		roles = append(roles, c.Role)
	}
	if strings.Join(roles, ",") != "user,model,user" {
		t.Fatalf("role mapping wrong: %v", roles)
	}
}

// gemini-3.5-flash 实测帧形状(2026-06):thought 摘要帧、
// 空 text + thoughtSignature 的收尾帧,均不得进入正文。
func TestThinkingProtocolFramesCompatible(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = io.WriteString(w, `data: {"candidates":[{"content":{"parts":[{"text":"推理摘要...","thought":true}]},"index":0}]}`+"\n\n")
		_, _ = io.WriteString(w, `data: {"candidates":[{"content":{"parts":[{"text":"答案"}]},"index":0}]}`+"\n\n")
		_, _ = io.WriteString(w, `data: {"candidates":[{"content":{"parts":[{"text":"","thoughtSignature":"AbCd=="}]},"finishReason":"STOP","index":0}],"usageMetadata":{"thoughtsTokenCount":286}}`+"\n\n")
	}))
	defer srv.Close()

	client := New(Config{BaseURL: srv.URL, HTTPClient: srv.Client()})
	ch, _ := client.Stream(context.Background(), provider.Request{Model: "gemini-3.5-flash"})
	text, done, err := collect(t, ch)
	if err != nil || !done {
		t.Fatalf("done=%v err=%v", done, err)
	}
	if text != "答案" {
		t.Fatalf("thought frames must not leak into answer, got %q", text)
	}
}

func TestNon2xxEmitsErrWithRedactedKey(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusForbidden)
		_, _ = io.WriteString(w, `{"error":{"message":"bad key test-key"}}`)
	}))
	defer srv.Close()

	client := New(Config{BaseURL: srv.URL, APIKey: "test-key", HTTPClient: srv.Client()})
	ch, _ := client.Stream(context.Background(), provider.Request{Model: "m"})
	_, _, err := collect(t, ch)
	if err == nil || !strings.Contains(err.Error(), "403") {
		t.Fatalf("want 403 error, got %v", err)
	}
	if strings.Contains(err.Error(), "test-key") {
		t.Fatalf("api key leaked into error: %v", err)
	}
}

func TestStreamEndsWithoutFinishEmitsErr(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = io.WriteString(w, sseFrame([]string{"partial"}, ""))
	}))
	defer srv.Close()

	client := New(Config{BaseURL: srv.URL, HTTPClient: srv.Client()})
	ch, _ := client.Stream(context.Background(), provider.Request{Model: "m"})
	text, done, err := collect(t, ch)
	if done || err == nil || text != "partial" {
		t.Fatalf("want truncation error after partial, got text=%q done=%v err=%v", text, done, err)
	}
}

func TestBlockedPromptEmitsErr(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = io.WriteString(w, `data: {"promptFeedback":{"blockReason":"SAFETY"}}`+"\n\n")
	}))
	defer srv.Close()

	client := New(Config{BaseURL: srv.URL, HTTPClient: srv.Client()})
	ch, _ := client.Stream(context.Background(), provider.Request{Model: "m"})
	_, _, err := collect(t, ch)
	if err == nil || !strings.Contains(err.Error(), "SAFETY") {
		t.Fatalf("want blocked error, got %v", err)
	}
}

func TestContextCancelTerminatesPromptly(t *testing.T) {
	started := make(chan struct{})
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		close(started)
		time.Sleep(5 * time.Second)
	}))
	defer srv.Close()

	ctx, cancel := context.WithCancel(context.Background())
	client := New(Config{BaseURL: srv.URL, HTTPClient: srv.Client()})
	ch, err := client.Stream(ctx, provider.Request{Model: "m"})
	if err != nil {
		t.Fatal(err)
	}
	<-started
	cancel()

	select {
	case chunk, ok := <-ch:
		if !ok || !errors.Is(chunk.Err, context.Canceled) {
			t.Fatalf("want context.Canceled, got %+v ok=%v", chunk, ok)
		}
	case <-time.After(500 * time.Millisecond):
		t.Fatal("cancel did not terminate promptly")
	}
}

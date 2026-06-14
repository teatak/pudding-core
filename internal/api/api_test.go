package api

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/teatak/pudding-core/internal/engine"
	"github.com/teatak/pudding-core/internal/event"
	"github.com/teatak/pudding-core/internal/provider/mock"
	"github.com/teatak/pudding-core/internal/provider/registry"
	"github.com/teatak/pudding-core/internal/store"
	"github.com/teatak/pudding-core/internal/store/memstore"
)

const testToken = "test-token"

func newTestServer(t *testing.T) (*httptest.Server, store.Store) {
	t.Helper()
	ms := memstore.New()
	hub := event.NewHub()
	eng := engine.New(ms, hub, registry.Static(mock.New(mock.WithScript([]string{"你好", "世界"}), mock.WithDelay(5*time.Millisecond))), ms, "m")
	srv := httptest.NewServer(New(eng, ms, ms, hub).Handler(testToken, nil))
	t.Cleanup(srv.Close)
	return srv, ms
}

func req(t *testing.T, method, url string, body any) *http.Response {
	t.Helper()
	var buf bytes.Buffer
	if body != nil {
		if err := json.NewEncoder(&buf).Encode(body); err != nil {
			t.Fatal(err)
		}
	}
	r, err := http.NewRequest(method, url, &buf)
	if err != nil {
		t.Fatal(err)
	}
	r.Header.Set("Authorization", "Bearer "+testToken)
	resp, err := http.DefaultClient.Do(r)
	if err != nil {
		t.Fatal(err)
	}
	return resp
}

func decodeJSON[T any](t *testing.T, resp *http.Response) T {
	t.Helper()
	defer resp.Body.Close()
	var v T
	if err := json.NewDecoder(resp.Body).Decode(&v); err != nil {
		t.Fatal(err)
	}
	return v
}

func TestAuthRequired(t *testing.T) {
	srv, _ := newTestServer(t)
	resp, err := http.Get(srv.URL + "/sessions")
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("want 401, got %d", resp.StatusCode)
	}
}

type sseFrame struct {
	id    string
	event string
}

// readSSE 读取 SSE 流直到看到 stopKind 事件或超时,返回收到的帧序列。
func readSSE(t *testing.T, url string, stopKind string, timeout time.Duration) []sseFrame {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	r, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		t.Fatal(err)
	}
	resp, err := http.DefaultClient.Do(r)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if ct := resp.Header.Get("Content-Type"); ct != "text/event-stream" {
		t.Fatalf("want text/event-stream, got %q", ct)
	}

	var frames []sseFrame
	var cur sseFrame
	scanner := bufio.NewScanner(resp.Body)
	for scanner.Scan() {
		line := scanner.Text()
		switch {
		case strings.HasPrefix(line, "id: "):
			cur.id = strings.TrimPrefix(line, "id: ")
		case strings.HasPrefix(line, "event: "):
			cur.event = strings.TrimPrefix(line, "event: ")
		case line == "":
			if cur.event != "" {
				frames = append(frames, cur)
				if cur.event == stopKind {
					return frames
				}
			}
			cur = sseFrame{}
		}
	}
	t.Fatalf("stream ended before %s, frames: %+v", stopKind, frames)
	return nil
}

func TestProvidersCRUDRedactsAPIKey(t *testing.T) {
	srv, _ := newTestServer(t)

	resp := req(t, http.MethodPost, srv.URL+"/providers", map[string]string{
		"id": "work", "name": "Work", "type": "openai-compatible",
		"baseURL": "https://example.com/v1/", "apiKey": "sk-secret",
	})
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("want 201, got %d", resp.StatusCode)
	}
	created := decodeJSON[map[string]any](t, resp)
	if created["apiKeySet"] != true {
		t.Fatalf("want apiKeySet true, got %+v", created)
	}
	if created["baseURL"] != "https://example.com/v1" {
		t.Fatalf("baseURL must be trimmed: %+v", created)
	}
	for k := range created {
		if k == "apiKey" {
			t.Fatal("api key must never appear in responses")
		}
	}

	// 重名 409
	resp = req(t, http.MethodPost, srv.URL+"/providers", map[string]string{
		"id": "work", "name": "Work 2", "type": "openai-compatible", "baseURL": "https://x.com",
	})
	resp.Body.Close()
	if resp.StatusCode != http.StatusConflict {
		t.Fatalf("duplicate name must 409, got %d", resp.StatusCode)
	}

	// 非法 type 400
	resp = req(t, http.MethodPost, srv.URL+"/providers", map[string]string{
		"id": "bad", "name": "Bad", "type": "nope",
	})
	resp.Body.Close()
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("unsupported type must 400, got %d", resp.StatusCode)
	}

	// PATCH:空 apiKey 不覆盖,非空覆盖
	resp = req(t, http.MethodPatch, srv.URL+"/providers/work", map[string]any{"apiKey": ""})
	patched := decodeJSON[map[string]any](t, resp)
	if patched["apiKeySet"] != true {
		t.Fatalf("empty apiKey must not clear stored key: %+v", patched)
	}
	resp = req(t, http.MethodPatch, srv.URL+"/providers/work", map[string]any{"type": "google", "apiKey": "g-key"})
	patched = decodeJSON[map[string]any](t, resp)
	if patched["type"] != "google" {
		t.Fatalf("type patch failed: %+v", patched)
	}

	// 列表 + 删除
	resp = req(t, http.MethodGet, srv.URL+"/providers", nil)
	list := decodeJSON[map[string][]map[string]any](t, resp)
	if len(list["providers"]) != 1 {
		t.Fatalf("want 1 profile, got %+v", list)
	}
	resp = req(t, http.MethodDelete, srv.URL+"/providers/work", nil)
	resp.Body.Close()
	if resp.StatusCode != http.StatusNoContent {
		t.Fatalf("want 204, got %d", resp.StatusCode)
	}
	resp = req(t, http.MethodGet, srv.URL+"/providers/work", nil)
	resp.Body.Close()
	if resp.StatusCode != http.StatusNotFound {
		t.Fatalf("want 404 after delete, got %d", resp.StatusCode)
	}
}

func TestProviderModelsProxy(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/models" {
			http.NotFound(w, r)
			return
		}
		_, _ = w.Write([]byte(`{"data":[{"id":"m-alpha"},{"id":"m-beta"}]}`))
	}))
	defer upstream.Close()

	srv, _ := newTestServer(t)
	resp := req(t, http.MethodPost, srv.URL+"/providers", map[string]string{
		"id": "up", "name": "Upstream", "type": "openai-compatible", "baseURL": upstream.URL,
	})
	resp.Body.Close()

	got := decodeJSON[map[string][]string](t, req(t, http.MethodGet, srv.URL+"/providers/up/models", nil))
	if len(got["models"]) != 2 || got["models"][0] != "m-alpha" {
		t.Fatalf("unexpected models: %+v", got)
	}

	resp = req(t, http.MethodGet, srv.URL+"/providers/nope/models", nil)
	resp.Body.Close()
	if resp.StatusCode != http.StatusNotFound {
		t.Fatalf("missing profile must 404, got %d", resp.StatusCode)
	}
}

func TestCreateSessionCarriesProviderAndModel(t *testing.T) {
	srv, _ := newTestServer(t)
	sess := decodeJSON[store.Session](t, req(t, http.MethodPost, srv.URL+"/sessions",
		map[string]string{"title": "x", "provider": "gem", "model": "m1"}))
	if sess.Provider != "gem" || sess.Model != "m1" {
		t.Fatalf("create must persist provider/model: %+v", sess)
	}
	got := decodeJSON[store.Session](t, req(t, http.MethodGet, srv.URL+"/sessions/"+sess.ID, nil))
	if got.Provider != "gem" {
		t.Fatalf("provider lost on read: %+v", got)
	}
}

func TestCreateSessionBodyValidation(t *testing.T) {
	srv, _ := newTestServer(t)

	// 空 body 允许:建出匿名会话
	resp, err := http.NewRequest(http.MethodPost, srv.URL+"/sessions", nil)
	if err != nil {
		t.Fatal(err)
	}
	resp.Header.Set("Authorization", "Bearer "+testToken)
	r, err := http.DefaultClient.Do(resp)
	if err != nil {
		t.Fatal(err)
	}
	r.Body.Close()
	if r.StatusCode != http.StatusCreated {
		t.Fatalf("empty body must 201, got %d", r.StatusCode)
	}

	// 非空坏 JSON 必须 400,不能静默建空会话
	bad, err := http.NewRequest(http.MethodPost, srv.URL+"/sessions", strings.NewReader(`{bad json`))
	if err != nil {
		t.Fatal(err)
	}
	bad.Header.Set("Authorization", "Bearer "+testToken)
	br, err := http.DefaultClient.Do(bad)
	if err != nil {
		t.Fatal(err)
	}
	br.Body.Close()
	if br.StatusCode != http.StatusBadRequest {
		t.Fatalf("malformed json must 400, got %d", br.StatusCode)
	}

	// 库里只应有那 1 个空 body 会话,坏 JSON 没留垃圾
	list := decodeJSON[map[string][]store.Session](t, req(t, http.MethodGet, srv.URL+"/sessions", nil))
	if len(list["sessions"]) != 1 {
		t.Fatalf("malformed body must not create a session, got %d", len(list["sessions"]))
	}
}

func TestDeleteSessionCancelsRunningTurn(t *testing.T) {
	// mock 慢流:submit 后 turn 持续 running,delete 必须 cancel 它而非
	// 留 goroutine 跑到自然结束。
	ms := memstore.New()
	hub := event.NewHub()
	eng := engine.New(ms, hub,
		registry.Static(mock.New(mock.WithScript([]string{"slow"}), mock.WithDelay(2*time.Second))), ms, "m")
	srv := httptest.NewServer(New(eng, ms, ms, hub).Handler(testToken, nil))
	t.Cleanup(srv.Close)

	sess := decodeJSON[store.Session](t, req(t, http.MethodPost, srv.URL+"/sessions", map[string]string{"title": "d"}))
	resp := req(t, http.MethodPost, srv.URL+"/sessions/"+sess.ID+"/submit",
		map[string]string{"clientMessageID": "c1", "text": "hi"})
	resp.Body.Close()
	time.Sleep(100 * time.Millisecond) // 让 turn 进入 running

	resp = req(t, http.MethodDelete, srv.URL+"/sessions/"+sess.ID, nil)
	resp.Body.Close()
	if resp.StatusCode != http.StatusNoContent {
		t.Fatalf("delete must 204, got %d", resp.StatusCode)
	}

	// engine.Wait 应很快返回(turn 已被 cancel),不会等满 2s 的 mock 延迟
	waited := make(chan struct{})
	go func() { eng.Wait(); close(waited) }()
	select {
	case <-waited:
	case <-time.After(1 * time.Second):
		t.Fatal("delete did not cancel the running turn; goroutine still streaming")
	}
}

func TestSubmitStreamAndResume(t *testing.T) {
	srv, _ := newTestServer(t)

	sess := decodeJSON[store.Session](t, req(t, http.MethodPost, srv.URL+"/sessions", map[string]string{"title": "demo"}))
	eventsURL := fmt.Sprintf("%s/sessions/%s/events?token=%s", srv.URL, sess.ID, testToken)

	// live 流:先订阅再 submit
	done := make(chan []sseFrame, 1)
	go func() { done <- readSSE(t, eventsURL, "turn.completed", 5*time.Second) }()
	time.Sleep(100 * time.Millisecond)

	resp := req(t, http.MethodPost, srv.URL+"/sessions/"+sess.ID+"/submit",
		map[string]string{"clientMessageID": "c1", "text": "hi"})
	resp.Body.Close()
	if resp.StatusCode != http.StatusAccepted {
		t.Fatalf("want 202, got %d", resp.StatusCode)
	}

	frames := <-done
	var kinds []string
	seqs := map[string]bool{}
	for _, f := range frames {
		kinds = append(kinds, f.event)
		if f.id != "" {
			if seqs[f.id] {
				t.Fatalf("duplicate seq %s in live stream: %+v", f.id, frames)
			}
			seqs[f.id] = true
		}
		if f.event == "turn.delta" && f.id != "" {
			t.Fatalf("delta must not carry id: %+v", f)
		}
	}
	got := strings.Join(kinds, ",")
	if !strings.HasPrefix(got, "turn.started,turn.delta") || !strings.HasSuffix(got, "turn.completed") {
		t.Fatalf("unexpected live sequence: %s", got)
	}

	// 续传:after=1 只应补发 seq>1 的 lifecycle,不丢不重
	frames = readSSE(t, eventsURL+"&after=1", "turn.completed", 5*time.Second)
	if len(frames) != 1 || frames[0].event != "turn.completed" || frames[0].id != "2" {
		t.Fatalf("resume after=1 must replay exactly seq 2, got %+v", frames)
	}

	// tail:无位点的全新连接不回放历史,只看到连接后的新 turn
	done2 := make(chan []sseFrame, 1)
	go func() { done2 <- readSSE(t, eventsURL, "turn.completed", 5*time.Second) }()
	time.Sleep(100 * time.Millisecond)
	resp = req(t, http.MethodPost, srv.URL+"/sessions/"+sess.ID+"/submit",
		map[string]string{"clientMessageID": "c2", "text": "again"})
	resp.Body.Close()
	for _, f := range <-done2 {
		if f.id == "1" || f.id == "2" {
			t.Fatalf("fresh connection must tail, replayed old seq %s", f.id)
		}
	}
}

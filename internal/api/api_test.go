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

	"github.com/teatak/pudding-core/internal/config"
	"github.com/teatak/pudding-core/internal/engine"
	"github.com/teatak/pudding-core/internal/event"
	"github.com/teatak/pudding-core/internal/provider/mock"
	"github.com/teatak/pudding-core/internal/provider/registry"
	"github.com/teatak/pudding-core/internal/store"
	"github.com/teatak/pudding-core/internal/store/memstore"
	"github.com/teatak/pudding-core/internal/tool"
)

const testToken = "test-token"

func newTestServer(t *testing.T) (*httptest.Server, store.Store) {
	t.Helper()
	ms := memstore.New()
	hub := event.NewHub()
	eng := engine.New(ms, hub, registry.Static(mock.New(mock.WithScript([]string{"你好", "世界"}), mock.WithDelay(5*time.Millisecond))), ms)
	srv := httptest.NewServer(New(eng, ms, ms, hub).Handler(testToken, nil))
	t.Cleanup(srv.Close)
	return srv, ms
}

func newConfigTestServer(t *testing.T) (*httptest.Server, store.Store, *config.Manager) {
	t.Helper()
	ms := memstore.New()
	cfg := config.NewManager(t.TempDir())
	if err := cfg.Prepare(); err != nil {
		t.Fatal(err)
	}
	hub := event.NewHub()
	eng := engine.New(ms, hub, registry.Static(mock.New(mock.WithScript([]string{"你好", "世界"}), mock.WithDelay(5*time.Millisecond))), cfg)
	srv := httptest.NewServer(New(eng, ms, cfg, hub).Handler(testToken, nil))
	t.Cleanup(srv.Close)
	return srv, ms, cfg
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

func TestBuiltinToolsAPI(t *testing.T) {
	srv, _ := newTestServer(t)

	resp := req(t, http.MethodGet, srv.URL+"/tools/builtin", nil)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("want 200, got %d", resp.StatusCode)
	}
	got := decodeJSON[map[string][]map[string]any](t, resp)
	tools := got["tools"]
	if len(tools) != 3 {
		t.Fatalf("unexpected builtin tools: %+v", got)
	}
	if tools[0]["id"] != tool.TimeGetCurrent {
		t.Fatalf("unexpected builtin tool id: %+v", tools[0])
	}
	if tools[0]["description"] == "" || tools[0]["inputSchema"] == nil {
		t.Fatalf("builtin tool should include description and input schema: %+v", tools[0])
	}
}

func TestWebToolsAPI(t *testing.T) {
	srv, _, cfg := newConfigTestServer(t)

	resp := req(t, http.MethodGet, srv.URL+"/tools/web", nil)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("want 200, got %d", resp.StatusCode)
	}
	got := decodeJSON[config.WebToolsView](t, resp)
	if got.SearchProvider != "" || got.FetchProvider != "" || len(got.Providers) != 1 {
		t.Fatalf("unexpected initial web tools: %+v", got)
	}
	if got.Providers[0].Name != "tavily" || got.Providers[0].APIKeySet {
		t.Fatalf("unexpected initial tavily provider: %+v", got.Providers[0])
	}

	resp = req(t, http.MethodPatch, srv.URL+"/tools/web", map[string]any{
		"providers": map[string]any{
			"tavily": map[string]string{"apiKey": "tvly-http-secret"},
		},
	})
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("want 200, got %d", resp.StatusCode)
	}
	got = decodeJSON[config.WebToolsView](t, resp)
	if got.SearchProvider != "tavily" || got.FetchProvider != "tavily" {
		t.Fatalf("patch should enable tavily providers: %+v", got)
	}
	if got.Providers[0].APIKey != "tvly-http-secret" || !got.Providers[0].APIKeySet {
		t.Fatalf("patch should return editable key and status: %+v", got.Providers[0])
	}
	stored, ok, err := cfg.TavilyAPIKey(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if !ok || stored != "tvly-http-secret" {
		t.Fatalf("unexpected stored tavily api key: %q %v", stored, ok)
	}

	resp = req(t, http.MethodPatch, srv.URL+"/tools/web", map[string]any{"searchProvider": "unsupported"})
	resp.Body.Close()
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("unsupported web provider must 400, got %d", resp.StatusCode)
	}
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

func TestCORSPreflightAllowsWailsLoopback(t *testing.T) {
	srv, _ := newTestServer(t)
	r, err := http.NewRequest(http.MethodOptions, srv.URL+"/sessions/s1", nil)
	if err != nil {
		t.Fatal(err)
	}
	r.Header.Set("Origin", "wails://localhost:5174")
	r.Header.Set("Access-Control-Request-Method", "PATCH")
	r.Header.Set("Access-Control-Request-Headers", "Authorization, Content-Type")
	resp, err := http.DefaultClient.Do(r)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusNoContent {
		t.Fatalf("want 204, got %d", resp.StatusCode)
	}
	if got := resp.Header.Get("Access-Control-Allow-Origin"); got != "wails://localhost:5174" {
		t.Fatalf("unexpected allow origin %q", got)
	}
	if !strings.Contains(resp.Header.Get("Access-Control-Allow-Methods"), "PATCH") {
		t.Fatalf("PATCH must be allowed, got %q", resp.Header.Get("Access-Control-Allow-Methods"))
	}
}

func TestCORSRejectsNonLoopbackOrigin(t *testing.T) {
	srv, _ := newTestServer(t)
	r, err := http.NewRequest(http.MethodOptions, srv.URL+"/sessions", nil)
	if err != nil {
		t.Fatal(err)
	}
	r.Header.Set("Origin", "https://example.com")
	r.Header.Set("Access-Control-Request-Method", "GET")
	resp, err := http.DefaultClient.Do(r)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusForbidden {
		t.Fatalf("want 403, got %d", resp.StatusCode)
	}
}

func TestListMessagesPagination(t *testing.T) {
	srv, st := newTestServer(t)
	if err := st.CreateSession(context.Background(), &store.Session{ID: "sess_1", Provider: "mock", Model: "mock"}); err != nil {
		t.Fatal(err)
	}
	for i := 1; i <= 4; i++ {
		appendAPITestTurn(t, st, "sess_1", i)
	}

	resp := req(t, http.MethodGet, srv.URL+"/sessions/sess_1/messages?limit=4", nil)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("want 200, got %d", resp.StatusCode)
	}
	first := decodeJSON[messagePageResponse](t, resp)
	if !first.HasMore {
		t.Fatal("recent page should report older messages")
	}
	if got, want := messageValueIDs(first.Messages), []string{"msg_3", "msg_turn_3", "msg_4", "msg_turn_4"}; !sameStringValues(got, want) {
		t.Fatalf("unexpected recent page: got %v want %v", got, want)
	}

	resp = req(t, http.MethodGet, srv.URL+"/sessions/sess_1/messages?limit=4&before="+first.Messages[0].ID, nil)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("want 200, got %d", resp.StatusCode)
	}
	older := decodeJSON[messagePageResponse](t, resp)
	if older.HasMore {
		t.Fatal("older page should be exhausted")
	}
	if got, want := messageValueIDs(older.Messages), []string{"msg_1", "msg_turn_1", "msg_2", "msg_turn_2"}; !sameStringValues(got, want) {
		t.Fatalf("unexpected older page: got %v want %v", got, want)
	}
}

func TestListTurnsPagination(t *testing.T) {
	srv, st := newTestServer(t)
	if err := st.CreateSession(context.Background(), &store.Session{ID: "sess_1", Provider: "mock", Model: "mock"}); err != nil {
		t.Fatal(err)
	}
	for i := 1; i <= 4; i++ {
		appendAPITestTurn(t, st, "sess_1", i)
	}

	resp := req(t, http.MethodGet, srv.URL+"/sessions/sess_1/turns?limit=2", nil)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("want 200, got %d", resp.StatusCode)
	}
	first := decodeJSON[turnPageResponse](t, resp)
	if !first.HasMore {
		t.Fatal("recent page should report older turns")
	}
	if got, want := turnValueIDs(first.Turns), []string{"turn_3", "turn_4"}; !sameStringValues(got, want) {
		t.Fatalf("unexpected recent page: got %v want %v", got, want)
	}
	if got, want := messagePtrValueIDs(first.Turns[0].Messages), []string{"msg_3", "msg_turn_3"}; !sameStringValues(got, want) {
		t.Fatalf("turn should include complete messages: got %v want %v", got, want)
	}

	resp = req(t, http.MethodGet, srv.URL+"/sessions/sess_1/turns?limit=2&before="+first.Turns[0].ID, nil)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("want 200, got %d", resp.StatusCode)
	}
	older := decodeJSON[turnPageResponse](t, resp)
	if older.HasMore {
		t.Fatal("older page should be exhausted")
	}
	if got, want := turnValueIDs(older.Turns), []string{"turn_1", "turn_2"}; !sameStringValues(got, want) {
		t.Fatalf("unexpected older page: got %v want %v", got, want)
	}
}

func TestGetTurn(t *testing.T) {
	srv, st := newTestServer(t)
	if err := st.CreateSession(context.Background(), &store.Session{ID: "sess_1", Provider: "mock", Model: "mock"}); err != nil {
		t.Fatal(err)
	}
	appendAPITestTurn(t, st, "sess_1", 1)

	resp := req(t, http.MethodGet, srv.URL+"/sessions/sess_1/turns/turn_1", nil)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("want 200, got %d", resp.StatusCode)
	}
	turn := decodeJSON[store.ConversationTurn](t, resp)
	if turn.ID != "turn_1" {
		t.Fatalf("unexpected turn id %q", turn.ID)
	}
	if got, want := messagePtrValueIDs(turn.Messages), []string{"msg_1", "msg_turn_1"}; !sameStringValues(got, want) {
		t.Fatalf("turn should include complete messages: got %v want %v", got, want)
	}
}

type messagePageResponse struct {
	Messages []store.Message `json:"messages"`
	HasMore  bool            `json:"hasMore"`
}

type turnPageResponse struct {
	Turns   []store.ConversationTurn `json:"turns"`
	HasMore bool                     `json:"hasMore"`
}

func appendAPITestTurn(t *testing.T, st store.Store, sessionID string, index int) {
	t.Helper()
	turnID := fmt.Sprintf("turn_%d", index)
	_, err := st.BeginTurn(context.Background(), store.BeginTurnInput{
		SessionID:       sessionID,
		TurnID:          turnID,
		UserMessageID:   fmt.Sprintf("msg_%d", index),
		ClientMessageID: fmt.Sprintf("client_%d", index),
		UserText:        fmt.Sprintf("user %d", index),
	})
	if err != nil {
		t.Fatal(err)
	}
	text := fmt.Sprintf("assistant %d", index)
	if _, err := st.FinishTurn(context.Background(), store.FinishTurnInput{
		TurnID:         turnID,
		Status:         store.TurnCompleted,
		AssistantParts: store.TextPart(text),
	}); err != nil {
		t.Fatal(err)
	}
}

func messageValueIDs(messages []store.Message) []string {
	out := make([]string, 0, len(messages))
	for _, msg := range messages {
		out = append(out, msg.ID)
	}
	return out
}

func messagePtrValueIDs(messages []*store.Message) []string {
	out := make([]string, 0, len(messages))
	for _, msg := range messages {
		out = append(out, msg.ID)
	}
	return out
}

func turnValueIDs(turns []store.ConversationTurn) []string {
	out := make([]string, 0, len(turns))
	for _, turn := range turns {
		out = append(out, turn.ID)
	}
	return out
}

func sameStringValues(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
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

func TestProvidersCRUDReturnsEditableAPIKey(t *testing.T) {
	srv, _ := newTestServer(t)

	resp := req(t, http.MethodPost, srv.URL+"/providers", map[string]string{
		"id": "work", "displayName": "Work", "protocol": "openai-compatible",
		"baseURL": "https://example.com/v1/", "apiKey": "sk-secret",
	})
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("want 201, got %d", resp.StatusCode)
	}
	created := decodeJSON[map[string]any](t, resp)
	if created["apiKeySet"] != true {
		t.Fatalf("want apiKeySet true, got %+v", created)
	}
	if created["apiKey"] != "sk-secret" {
		t.Fatalf("want editable apiKey in response, got %+v", created)
	}
	if created["baseURL"] != "https://example.com/v1" {
		t.Fatalf("baseURL must be trimmed: %+v", created)
	}

	// 重名 409
	resp = req(t, http.MethodPost, srv.URL+"/providers", map[string]string{
		"id": "work", "displayName": "Work 2", "protocol": "openai-compatible", "baseURL": "https://x.com",
	})
	resp.Body.Close()
	if resp.StatusCode != http.StatusConflict {
		t.Fatalf("duplicate name must 409, got %d", resp.StatusCode)
	}

	// 非法 protocol 400
	resp = req(t, http.MethodPost, srv.URL+"/providers", map[string]string{
		"id": "bad", "displayName": "Bad", "protocol": "nope",
	})
	resp.Body.Close()
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("unsupported protocol must 400, got %d", resp.StatusCode)
	}

	// PATCH:空 apiKey 不覆盖,非空覆盖
	resp = req(t, http.MethodPatch, srv.URL+"/providers/work", map[string]any{"apiKey": ""})
	patched := decodeJSON[map[string]any](t, resp)
	if patched["apiKeySet"] != true {
		t.Fatalf("empty apiKey must not clear stored key: %+v", patched)
	}
	if patched["apiKey"] != "sk-secret" {
		t.Fatalf("empty apiKey must keep stored key: %+v", patched)
	}
	resp = req(t, http.MethodPatch, srv.URL+"/providers/work", map[string]any{"protocol": "google", "apiKey": "g-key"})
	patched = decodeJSON[map[string]any](t, resp)
	if patched["protocol"] != "google" {
		t.Fatalf("protocol patch failed: %+v", patched)
	}
	if patched["apiKey"] != "g-key" {
		t.Fatalf("apiKey patch failed: %+v", patched)
	}

	// 列表 + 删除
	resp = req(t, http.MethodGet, srv.URL+"/providers", nil)
	list := decodeJSON[map[string][]map[string]any](t, resp)
	if len(list["providers"]) != 1 {
		t.Fatalf("want 1 profile, got %+v", list)
	}
	if list["providers"][0]["apiKey"] != "g-key" {
		t.Fatalf("list must return editable apiKey: %+v", list)
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
		"id": "up", "displayName": "Upstream", "protocol": "openai-compatible", "baseURL": upstream.URL,
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

	// 空 body 不允许:session 必须显式带 provider/model
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
	if r.StatusCode != http.StatusBadRequest {
		t.Fatalf("empty body must 400, got %d", r.StatusCode)
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

	// 空 body 和坏 JSON 都不能留下垃圾 session
	list := decodeJSON[map[string][]store.Session](t, req(t, http.MethodGet, srv.URL+"/sessions", nil))
	if len(list["sessions"]) != 0 {
		t.Fatalf("invalid create must not create a session, got %d", len(list["sessions"]))
	}
}

func TestDeleteSessionCancelsRunningTurn(t *testing.T) {
	// mock 慢流:submit 后 turn 持续 running,delete 必须 cancel 它而非
	// 留 goroutine 跑到自然结束。
	ms := memstore.New()
	hub := event.NewHub()
	eng := engine.New(ms, hub,
		registry.Static(mock.New(mock.WithScript([]string{"slow"}), mock.WithDelay(2*time.Second))), ms)
	srv := httptest.NewServer(New(eng, ms, ms, hub).Handler(testToken, nil))
	t.Cleanup(srv.Close)

	sess := decodeJSON[store.Session](t, req(t, http.MethodPost, srv.URL+"/sessions", map[string]string{"title": "d", "provider": "mock", "model": "m"}))
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

	sess := decodeJSON[store.Session](t, req(t, http.MethodPost, srv.URL+"/sessions", map[string]string{"title": "demo", "provider": "mock", "model": "m"}))
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

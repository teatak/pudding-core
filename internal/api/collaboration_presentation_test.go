package api

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"

	"github.com/teatak/pudding-core/internal/engine"
	"github.com/teatak/pudding-core/internal/event"
	"github.com/teatak/pudding-core/internal/store"
	"github.com/teatak/pudding-core/internal/store/memstore"
	"github.com/teatak/pudding-core/internal/store/sqlitestore"
)

func TestChildPresentationTracksCurrentTaskAndResult(t *testing.T) {
	for _, kind := range []string{"memory", "sqlite"} {
		t.Run(kind, func(t *testing.T) {
			ctx := context.Background()
			var st store.Store = memstore.New()
			if kind == "sqlite" {
				db, err := sqlitestore.Open(filepath.Join(t.TempDir(), "test.db"))
				if err != nil {
					t.Fatal(err)
				}
				defer db.Close()
				st = db
			}
			if err := st.CreateSession(ctx, &store.Session{ID: "parent", Provider: "mock", Model: "model"}); err != nil {
				t.Fatal(err)
			}
			if err := st.CreateChildSession(ctx, "parent", &store.Session{ID: "child", Title: "北京天气", Provider: "mock", Model: "model"}); err != nil {
				t.Fatal(err)
			}
			hub := event.NewHub()
			eng := engine.New(st, hub, nil, nil)
			t.Cleanup(func() { eng.Stop(); eng.Wait() })
			handler := New(eng, st, nil, hub).Handler(testToken, nil)
			read := func() childSessionView {
				t.Helper()
				r := httptest.NewRequest(http.MethodGet, "/sessions/parent/children", nil)
				r.Header.Set("Authorization", "Bearer "+testToken)
				w := httptest.NewRecorder()
				handler.ServeHTTP(w, r)
				var payload struct {
					Children []childSessionView `json:"children"`
				}
				if err := json.Unmarshal(w.Body.Bytes(), &payload); err != nil {
					t.Fatal(err)
				}
				if w.Code != http.StatusOK || len(payload.Children) != 1 {
					t.Fatalf("snapshot: %d %s", w.Code, w.Body.String())
				}
				return payload.Children[0]
			}
			finish := func(id string, status store.TurnStatus, text string) {
				t.Helper()
				if _, err := st.FinishTurn(ctx, store.FinishTurnInput{TurnID: id, Status: status, AssistantParts: []store.ContentPart{{Type: store.ContentPartText, Text: text}}}); err != nil {
					t.Fatal(err)
				}
			}
			if _, err := st.BeginTurn(ctx, store.BeginTurnInput{SessionID: "child", TurnID: "first", ClientMessageID: "first", UserMessageID: "first", UserText: "请查询北京今天的天气。\n要求：使用 builtin_weather 查询。"}); err != nil {
				t.Fatal(err)
			}
			if v := read(); v.TaskTitle != "北京天气" || v.Summary != "请查询北京今天的天气" {
				t.Fatalf("initial: %+v", v)
			}
			finish("first", store.TurnCompleted, "## 北京天气\n**晴，30°C**。更多详情。")
			if v := read(); v.TaskTitle != "北京天气" || v.Summary != "晴，30°C" {
				t.Fatalf("result: %+v", v)
			}
			if _, err := st.CollectChildResults(ctx, "parent"); err != nil {
				t.Fatal(err)
			}
			if _, err := st.QueueInput(ctx, store.QueueInputInput{SessionID: "child", ClientMessageID: "reuse", Text: "新任务：这次查杭州今天的天气，格式和上次一致。\n- 使用 builtin_weather 查询", Provider: "mock", Model: "model"}); err != nil {
				t.Fatal(err)
			}
			queued := read()
			if queued.TaskTitle != "这次查杭州今天的天气" || queued.Status != "queued" || queued.ResultCollected || queued.Summary != "这次查杭州今天的天气，格式和上次一致" {
				t.Fatalf("queued reuse: %+v", queued)
			}
			if _, err := st.PromoteNextQueuedInput(ctx, store.PromoteQueuedInputInput{SessionID: "child", TurnID: "reuse", UserMessageID: "reuse"}); err != nil {
				t.Fatal(err)
			}
			if v := read(); v.TaskTitle != queued.TaskTitle || v.Summary != queued.Summary || v.Status != "running" {
				t.Fatalf("running reuse: %+v", v)
			}
			finish("reuse", store.TurnFailed, "旧尝试的部分结果")
			if _, err := st.BeginSystemTurn(ctx, store.BeginSystemTurnInput{SessionID: "child", TurnID: "retry", RetryOfTurnID: "reuse", ClientMessageID: "retry", SystemMessageID: "retry", Text: "retry"}); err != nil {
				t.Fatal(err)
			}
			if v := read(); v.TaskTitle != queued.TaskTitle || v.Summary != queued.Summary || v.Status != "running" {
				t.Fatalf("retry: %+v", v)
			}
			finish("retry", store.TurnCompleted, "杭州多云，26°C。")
			if v := read(); v.TaskTitle != queued.TaskTitle || v.Summary != "杭州多云，26°C" {
				t.Fatalf("latest result: %+v", v)
			}
			answer := store.ContentPart{Type: store.ContentPartFormResult, Title: "查询范围", Schema: json.RawMessage(`{"type":"form","steps":[{"id":"range","type":"text_input","title":"范围"}]}`), Result: json.RawMessage(`{"range":"全天"}`)}
			if _, err := st.QueueInput(ctx, store.QueueInputInput{SessionID: "child", ClientMessageID: "answer", Text: "已填写：全天", Parts: []store.ContentPart{answer}, Provider: "mock", Model: "model"}); err != nil {
				t.Fatal(err)
			}
			if v := read(); v.TaskTitle != queued.TaskTitle || v.Summary != queued.Summary {
				t.Fatalf("queued answer replaced the task: %+v", v)
			}
			if _, err := st.PromoteNextQueuedInput(ctx, store.PromoteQueuedInputInput{SessionID: "child", TurnID: "answer", UserMessageID: "answer"}); err != nil {
				t.Fatal(err)
			}
			if v := read(); v.TaskTitle != queued.TaskTitle || v.Summary != queued.Summary {
				t.Fatalf("answer continuation lost the task: %+v", v)
			}
			// Presentation never rewrites the stored conversation name or messages.
			session, err := st.GetSession(ctx, "child")
			if err != nil || session.Title != "北京天气" {
				t.Fatalf("session changed: %+v %v", session, err)
			}
		})
	}
}

func TestChildTextExcerpt(t *testing.T) {
	for _, tc := range []struct{ text, want string }{
		{"请查询成都今天的天气（当前实况 + 今天预报）。要求：使用 builtin_weather", "请查询成都今天的天气（当前实况 + 今天预报）"},
		{"## 结果\n```json\n{\"tool\":\"builtin_weather\"}\n```\n- **多云，26.5°C**。", "多云，26.5°C"},
		{"Use builtin_weather.\nSunny, 26°C. More details.", "Sunny, 26°C"},
		{"[All checks passed](https://example.test). Details.", "All checks passed"},
		{"```\nbuiltin_weather\n```", ""},
	} {
		if got := childTextExcerpt(tc.text, 96); got != tc.want {
			t.Errorf("excerpt %q = %q, want %q", tc.text, got, tc.want)
		}
	}
	if got := childTextExcerpt("杭州天气晴朗", 4); got != "杭州天气…" {
		t.Fatalf("unicode truncation: %q", got)
	}
	if childTaskInput(&store.Message{Role: store.RoleUser, Text: "已填写：方案 B", Parts: []store.ContentPart{{Type: store.ContentPartFormResult}}}) {
		t.Fatal("an answer must not rename the task")
	}
}

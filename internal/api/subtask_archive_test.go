package api

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/teatak/pudding-core/internal/engine"
	"github.com/teatak/pudding-core/internal/event"
	"github.com/teatak/pudding-core/internal/store"
	"github.com/teatak/pudding-core/internal/store/storetest"
)

func TestSubtaskArchiveAPIAndParentRetention(t *testing.T) {
	ctx := context.Background()
	st := storetest.New(t)
	hub := event.NewHub()
	eng := engine.New(st, hub, nil, st)
	t.Cleanup(func() { eng.Stop(); eng.Wait() })
	for _, id := range []string{"parent", "other"} {
		if err := st.CreateSession(ctx, &store.Session{ID: id, Provider: "mock", Model: "model"}); err != nil {
			t.Fatal(err)
		}
	}
	for _, id := range []string{"child", "busy"} {
		if err := st.CreateChildSession(ctx, "parent", &store.Session{ID: id, Provider: "mock", Model: "model"}); err != nil {
			t.Fatal(err)
		}
		if _, err := st.BeginTurn(ctx, store.BeginTurnInput{SessionID: id, TurnID: id, ClientMessageID: id, UserMessageID: id + "-message", UserText: "work"}); err != nil {
			t.Fatal(err)
		}
		if _, err := st.FinishTurn(ctx, store.FinishTurnInput{TurnID: id, Status: store.TurnCompleted}); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := st.QueueInput(ctx, store.QueueInputInput{SessionID: "busy", ClientMessageID: "queued", Text: "more"}); err != nil {
		t.Fatal(err)
	}
	srv := New(eng, st, st, hub).WithHome(t.TempDir())
	handler := srv.Handler(testToken, nil)
	request := func(path string, status int) []engine.ChildSessionView {
		t.Helper()
		r := httptest.NewRequest(http.MethodPost, path, nil)
		r.Header.Set("Authorization", "Bearer "+testToken)
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, r)
		if w.Code != status {
			t.Fatalf("%s: %d %s", path, w.Code, w.Body.String())
		}
		var response struct {
			Children []engine.ChildSessionView `json:"children"`
		}
		if status == 200 {
			if err := json.Unmarshal(w.Body.Bytes(), &response); err != nil {
				t.Fatal(err)
			}
		}
		return response.Children
	}
	request("/sessions/other/children/child/archive", 400)
	request("/sessions/parent/children/busy/archive", 409)
	children := request("/sessions/parent/children/archive", 200)
	archived := 0
	for _, child := range children {
		if child.ArchivedAt != nil {
			archived++
		}
	}
	if archived != 1 || len(children) != 2 {
		t.Fatalf("batch result: %+v", children)
	}
	request("/sessions/parent/children/child/restore", 200)
	if archives, _ := st.ListSubtaskArchives(ctx, "parent"); len(archives) != 0 {
		t.Fatal("restore failed")
	}
	request("/sessions/parent/children/child/archive", 200)
	if err := srv.purgeExpiredSessionArchives(ctx, time.Now().Add(90*24*time.Hour)); err != nil {
		t.Fatal(err)
	}
	if _, err := st.GetSession(ctx, "child"); err != nil {
		t.Fatal("subtask archive expired independently", err)
	}
	if _, err := st.ArchiveSession(ctx, "parent"); err != nil {
		t.Fatal(err)
	}
	if err := srv.purgeExpiredSessionArchives(ctx, time.Now().Add(31*24*time.Hour)); err != nil {
		t.Fatal(err)
	}
	for _, id := range []string{"parent", "child", "busy"} {
		if _, err := st.ParentSessionID(ctx, id); !errors.Is(err, store.ErrNotFound) {
			t.Fatalf("retained %s: %v", id, err)
		}
	}
	if archives, _ := st.ListSubtaskArchives(ctx, "parent"); len(archives) != 0 {
		t.Fatal("retained orphan archive state")
	}
}

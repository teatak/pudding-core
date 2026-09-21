package api

import (
	"context"
	"encoding/json"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/teatak/pudding-core/internal/engine"
	"github.com/teatak/pudding-core/internal/event"
	"github.com/teatak/pudding-core/internal/provider/mock"
	"github.com/teatak/pudding-core/internal/provider/registry"
	"github.com/teatak/pudding-core/internal/store"
	"github.com/teatak/pudding-core/internal/store/memstore"
)

func TestScheduledTaskHTTPAdmissionScopeAndHistory(t *testing.T) {
	ctx := context.Background()
	st := memstore.New()
	hub := event.NewHub()
	for _, id := range []string{"target", "other"} {
		if err := st.CreateSession(ctx, &store.Session{ID: id, Title: id, Provider: "mock", Model: "model"}); err != nil {
			t.Fatal(err)
		}
	}
	if err := st.CreateChildSession(ctx, "target", &store.Session{ID: "child", Provider: "mock", Model: "model"}); err != nil {
		t.Fatal(err)
	}
	if err := st.PutProviderProfile(ctx, &store.ProviderProfile{ID: "mock", Protocol: "openai-compatible", Models: []store.ProviderModel{{ID: "model"}}}); err != nil {
		t.Fatal(err)
	}
	eng := engine.New(st, hub, registry.Static(mock.New()), st)
	defer eng.Stop()
	defer eng.Wait()
	handler := New(eng, st, st, hub).Handler(testToken, nil)
	request := func(method, path, body string) *httptest.ResponseRecorder {
		r := httptest.NewRequest(method, path, strings.NewReader(body))
		r.Header.Set("Authorization", "Bearer "+testToken)
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, r)
		return w
	}
	body := `{"sessionID":"target","requestID":"create","name":"Check","prompt":"Check the project","schedule":{"kind":"once","timezone":"UTC"},"delaySeconds":60}`
	response := request("POST", "/scheduled-tasks", body)
	if response.Code != 201 {
		t.Fatal(response.Code, response.Body.String())
	}
	newBody := strings.Replace(body, `"sessionID":"target"`, `"newSession":{"provider":"mock","model":"model"}`, 1)
	created := request("POST", "/scheduled-tasks", newBody)
	if created.Code != 201 {
		t.Fatal(created.Code, created.Body.String())
	}
	var newTask store.ScheduledTask
	if err := json.Unmarshal(created.Body.Bytes(), &newTask); err != nil {
		t.Fatal(err)
	}
	newSession, err := st.GetSession(ctx, newTask.SessionID)
	if err != nil || newSession.Title != "Check" || newSession.ActiveMode != store.ModeChat {
		t.Fatalf("new session: %+v %v", newSession, err)
	}
	for _, invalid := range []string{
		strings.Replace(body, `"sessionID":"target",`, "", 1),
		strings.Replace(newBody, `"newSession":`, `"sessionID":"target","newSession":`, 1),
		strings.Replace(newBody, `"model":"model"`, `"model":""`, 1),
	} {
		if response := request("POST", "/scheduled-tasks", invalid); response.Code != 400 {
			t.Fatal("invalid target admitted", response.Code, response.Body.String())
		}
	}
	var task store.ScheduledTask
	if err := json.Unmarshal(response.Body.Bytes(), &task); err != nil {
		t.Fatal(err)
	}
	if response = request("POST", "/scheduled-tasks", strings.Replace(body, "target", "child", 1)); response.Code != 400 {
		t.Fatal("child admission", response.Code, response.Body.String())
	}
	if response = request("PATCH", "/scheduled-tasks/"+task.ID, `{"revision":0,"enabled":false}`); response.Code != 409 {
		t.Fatal("stale edit", response.Code)
	}
	response = request("POST", "/scheduled-tasks/"+task.ID+"/runs", `{"requestID":"run"}`)
	if response.Code != 202 {
		t.Fatal(response.Code, response.Body.String())
	}
	var run engine.ScheduledRunView
	if err := json.Unmarshal(response.Body.Bytes(), &run); err != nil {
		t.Fatal(err)
	}
	eng.Wait()
	if response = request("GET", "/sessions/other/scheduled-task-runs/"+run.ID, ""); response.Code != 404 {
		t.Fatal("cross-session run leaked", response.Code)
	}
	response = request("GET", "/scheduled-tasks/"+task.ID+"/runs", "")
	if response.Code != 200 || !strings.Contains(response.Body.String(), `"status":"completed"`) {
		t.Fatal(response.Code, response.Body.String())
	}
	if _, err := st.ArchiveSession(ctx, "target"); err != nil {
		t.Fatal(err)
	}
	response = request("GET", "/scheduled-tasks", "")
	if response.Code != 200 || !strings.Contains(response.Body.String(), `"state":"paused"`) || !strings.Contains(response.Body.String(), `"sessionArchived":true`) {
		t.Fatal("archived history", response.Code, response.Body.String())
	}
}

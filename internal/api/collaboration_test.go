package api

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/teatak/pudding-core/internal/engine"
	"github.com/teatak/pudding-core/internal/event"
	"github.com/teatak/pudding-core/internal/provider"
	"github.com/teatak/pudding-core/internal/store"
	"github.com/teatak/pudding-core/internal/store/memstore"
	"github.com/teatak/pudding-core/internal/store/sqlitestore"
	"github.com/teatak/pudding-core/internal/tool"
)

func TestChildApprovalIsListedInMainWithOriginalScope(t *testing.T) {
	ctx := context.Background()
	st := memstore.New()
	hub := event.NewHub()
	eng := engine.New(st, hub, approvalResolver{client: collaborationApprovalClient{}}, st, engine.WithTools(tool.NewBuiltinRunner()), engine.WithAttachmentHome(t.TempDir()))
	t.Cleanup(func() { _ = eng.StopCollaboration(ctx, "main"); eng.Stop(); eng.Wait() })
	if err := st.CreateSession(ctx, &store.Session{ID: "main", Title: "Main", Provider: "approval", Model: "model"}); err != nil {
		t.Fatal(err)
	}
	if err := st.CreateSession(ctx, &store.Session{ID: "other", Provider: "approval", Model: "model"}); err != nil {
		t.Fatal(err)
	}
	if err := st.CreateChildSession(ctx, "main", &store.Session{ID: "child", Title: "Authentication", Provider: "approval", Model: "model"}); err != nil {
		t.Fatal(err)
	}
	if err := st.PutProviderProfile(ctx, &store.ProviderProfile{ID: "approval", Protocol: "openai-compatible", Models: []store.ProviderModel{{ID: "model", Capabilities: &store.ModelCaps{Tools: true}}}}); err != nil {
		t.Fatal(err)
	}
	events, unsubscribe := hub.Subscribe("child")
	defer unsubscribe()
	submitted, err := eng.Submit(ctx, engine.SubmitInput{SessionID: "child", ClientMessageID: "child_input", Text: "write"})
	if err != nil {
		t.Fatal(err)
	}
	_ = submitted
	var approvalID string
	deadline := time.After(time.Second)
	for approvalID == "" {
		select {
		case ev := <-events:
			if ev.Kind == event.ApprovalRequested {
				approvalID = ev.ApprovalID
			}
		case <-deadline:
			t.Fatal("child approval not emitted")
		}
	}
	handler := New(eng, st, st, hub).Handler(testToken, nil)
	request := func(method, path, body string) *httptest.ResponseRecorder {
		r := httptest.NewRequest(method, path, strings.NewReader(body))
		r.Header.Set("Authorization", "Bearer "+testToken)
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, r)
		return w
	}
	response := request(http.MethodGet, "/sessions/main/approvals", "")
	var pending struct {
		Approvals []approvalView `json:"approvals"`
	}
	if err := json.Unmarshal(response.Body.Bytes(), &pending); err != nil {
		t.Fatal(err)
	}
	if response.Code != 200 || len(pending.Approvals) != 1 {
		t.Fatalf("main approvals: %d %s", response.Code, response.Body.String())
	}
	approval := pending.Approvals[0]
	if approval.SessionID != "child" || approval.SourceTitle != "Authentication" || approval.ID != approvalID {
		t.Fatalf("origin rewritten: %+v", approval)
	}
	if err := st.CreateChildSession(ctx, "main", &store.Session{ID: "second", Title: "Regression", Provider: "approval", Model: "model"}); err != nil {
		t.Fatal(err)
	}
	secondEvents, unsubscribeSecond := hub.Subscribe("second")
	defer unsubscribeSecond()
	if _, err := eng.Submit(ctx, engine.SubmitInput{SessionID: "second", ClientMessageID: "second_input", Text: "write"}); err != nil {
		t.Fatal(err)
	}
	var secondApprovalID string
	for secondApprovalID == "" {
		select {
		case ev := <-secondEvents:
			if ev.Kind == event.ApprovalRequested {
				secondApprovalID = ev.ApprovalID
			}
		case <-deadline:
			t.Fatal("second approval not emitted")
		}
	}
	response = request(http.MethodGet, "/sessions/main/approvals", "")
	if err := json.Unmarshal(response.Body.Bytes(), &pending); err != nil {
		t.Fatal(err)
	}
	if len(pending.Approvals) != 2 || pending.Approvals[0].ID != approvalID || pending.Approvals[1].ID != secondApprovalID || pending.Approvals[1].SourceTitle != "Regression" {
		t.Fatalf("concurrent approvals lost identity/order: %s", response.Body.String())
	}
	response = request(http.MethodGet, "/sessions/other/approvals", "")
	if response.Code != 200 || !strings.Contains(response.Body.String(), `"approvals":[]`) {
		t.Fatal("approval leaked across parents")
	}
	response = request(http.MethodPost, "/sessions/main/approvals/"+approvalID+"/deny", `{}`)
	if response.Code < 400 {
		t.Fatal("main session ID was incorrectly accepted as approval origin")
	}
	response = request(http.MethodPost, "/sessions/child/approvals/"+approvalID+"/deny", `{}`)
	if response.Code != http.StatusAccepted {
		t.Fatalf("deny child: %d %s", response.Code, response.Body.String())
	}
	response = request(http.MethodGet, "/sessions/main/approvals", "")
	if err := json.Unmarshal(response.Body.Bytes(), &pending); err != nil {
		t.Fatal(err)
	}
	if len(pending.Approvals) != 1 || pending.Approvals[0].ID != secondApprovalID {
		t.Fatal("denying one child changed another approval")
	}
	response = request(http.MethodPost, "/sessions/second/approvals/"+secondApprovalID+"/deny", `{}`)
	if response.Code != http.StatusAccepted {
		t.Fatalf("deny second child: %d", response.Code)
	}
	eng.Wait()
	response = request(http.MethodGet, "/sessions/main/approvals", "")
	if !strings.Contains(response.Body.String(), `"approvals":[]`) {
		t.Fatal("resolved approval remained pending")
	}
}

// Each request derives its continuation from that conversation's canonical
// history, so concurrent children never share a fixture counter.
type collaborationApprovalClient struct{}

func (collaborationApprovalClient) Name() string { return "approval-project" }
func (collaborationApprovalClient) Stream(_ context.Context, req provider.Request) (<-chan provider.Chunk, error) {
	answered := false
	for _, message := range req.Messages {
		for _, part := range message.Parts {
			answered = answered || part.Type == provider.PartToolResult
		}
	}
	out := make(chan provider.Chunk, 2)
	if answered {
		out <- provider.Chunk{Delta: "done"}
		out <- provider.Chunk{Done: true, Finish: provider.FinishStop}
	} else {
		out <- provider.Chunk{Tool: &provider.ToolCallChunk{Index: 0, CallID: "approval", Name: tool.RequestCapability, ArgsDelta: `{"targetMode":"code","reason":"write a file","needsProjectDir":true}`}}
		out <- provider.Chunk{Done: true, Finish: provider.FinishToolCalls}
	}
	close(out)
	return out, nil
}

func TestChildSnapshotUsesLatestRetry(t *testing.T) {
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
			if err := st.CreateChildSession(ctx, "parent", &store.Session{ID: "child", Provider: "mock", Model: "model"}); err != nil {
				t.Fatal(err)
			}
			if _, err := st.BeginTurn(ctx, store.BeginTurnInput{SessionID: "child", TurnID: "failed", ClientMessageID: "input", UserMessageID: "message", UserText: "work"}); err != nil {
				t.Fatal(err)
			}
			if _, err := st.FinishTurn(ctx, store.FinishTurnInput{TurnID: "failed", Status: store.TurnFailed}); err != nil {
				t.Fatal(err)
			}
			if _, err := st.BeginSystemTurn(ctx, store.BeginSystemTurnInput{SessionID: "child", TurnID: "retry", RetryOfTurnID: "failed", ClientMessageID: "retry_input", SystemMessageID: "retry_message", Text: "retry"}); err != nil {
				t.Fatal(err)
			}
			hub := event.NewHub()
			eng := engine.New(st, hub, nil, nil)
			t.Cleanup(func() { eng.Stop(); eng.Wait() })
			handler := New(eng, st, nil, hub).Handler(testToken, nil)
			request := httptest.NewRequest(http.MethodGet, "/sessions/parent/children", nil)
			request.Header.Set("Authorization", "Bearer "+testToken)
			response := httptest.NewRecorder()
			handler.ServeHTTP(response, request)
			var payload struct {
				Children []childSessionView `json:"children"`
			}
			if err := json.Unmarshal(response.Body.Bytes(), &payload); err != nil {
				t.Fatal(err)
			}
			if response.Code != 200 || len(payload.Children) != 1 || payload.Children[0].LatestTurnID != "retry" || payload.Children[0].Status != "running" || payload.Children[0].ResultCollected || payload.Children[0].Summary != "work" {
				t.Fatalf("retry snapshot: %d %s", response.Code, response.Body.String())
			}
		})
	}
}

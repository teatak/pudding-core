package api

import (
	"context"
	"encoding/json"
	"net/http"
	"testing"

	"github.com/teatak/pudding-core/internal/engine"
	"github.com/teatak/pudding-core/internal/store"
	"github.com/teatak/pudding-core/internal/tool"
)

func TestUserInputRequestAPI(t *testing.T) {
	srv, st := newTestServer(t)
	ctx := context.Background()
	for _, id := range []string{"question-session", "other-session"} {
		if err := st.CreateSession(ctx, &store.Session{ID: id, Title: "test", Provider: "mock", Model: "mock"}); err != nil {
			t.Fatal(err)
		}
	}
	_, err := st.BeginTurn(ctx, store.BeginTurnInput{SessionID: "question-session", TurnID: "original", UserMessageID: "u", ClientMessageID: "initial", UserText: "ask"})
	if err != nil {
		t.Fatal(err)
	}
	_, err = st.AppendTurnOutput(ctx, store.AppendTurnOutputInput{TurnID: "original", Parts: []store.ContentPart{
		{Type: store.ContentPartToolUse, CallID: "q", Name: tool.RequestUserInput, Args: json.RawMessage(`{"title":"test","type":"form","waitSeconds":10,"steps":[{"id":"x","type":"text_input","title":"answer"}]}`)},
		{Type: store.ContentPartToolResult, CallID: "q", Name: tool.RequestUserInput, Ok: true, Content: `{"requestID":"original:q","status":"timeout"}`},
	}})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := st.FinishTurn(ctx, store.FinishTurnInput{TurnID: "original", Status: store.TurnCompleted}); err != nil {
		t.Fatal(err)
	}
	url := srv.URL + "/sessions/question-session/input-requests/original%3Aq"
	response := req(t, http.MethodGet, url, nil)
	if response.StatusCode != http.StatusOK {
		t.Fatalf("get: %d", response.StatusCode)
	}
	snapshot := decodeJSON[engine.UserInputRequest](t, response)
	if snapshot.Status != "timeout" || snapshot.Deadline != nil || snapshot.TurnID != "original" {
		t.Fatalf("snapshot: %+v", snapshot)
	}
	for _, action := range []string{"touch", "dismiss"} {
		response = req(t, http.MethodPost, url, map[string]any{"action": action})
		if response.StatusCode != http.StatusOK {
			t.Fatalf("%s: %d", action, response.StatusCode)
		}
		got := decodeJSON[engine.UserInputReply](t, response)
		if got.Request.Status != "timeout" || got.Request.Deadline != nil {
			t.Fatalf("ended wait renewed: %+v", got)
		}
	}
	for _, body := range []map[string]any{{"action": "unknown"}, {"action": "answer"}, {"action": "answer", "text": "bad", "parts": []map[string]any{{"type": "tool_use", "name": "fake"}}}} {
		response = req(t, http.MethodPost, url, body)
		response.Body.Close()
		if response.StatusCode != http.StatusBadRequest {
			t.Fatalf("invalid action: %d", response.StatusCode)
		}
	}
	for _, method := range []string{http.MethodGet, http.MethodPost} {
		response = req(t, method, srv.URL+"/sessions/other-session/input-requests/original%3Aq", map[string]any{"action": "touch"})
		response.Body.Close()
		if response.StatusCode != http.StatusNotFound {
			t.Fatalf("cross-session %s: %d", method, response.StatusCode)
		}
	}
	response, err = http.Get(url)
	if err != nil {
		t.Fatal(err)
	}
	response.Body.Close()
	if response.StatusCode != http.StatusUnauthorized {
		t.Fatalf("unauthenticated: %d", response.StatusCode)
	}
	// A late answer starts a new turn once; retries recover its canonical identity.
	for i := 0; i < 2; i++ {
		response = req(t, http.MethodPost, url, map[string]any{"action": "answer", "text": "reply", "parts": []map[string]any{{"type": "text", "text": "reply"}}})
		if response.StatusCode != http.StatusOK {
			t.Fatalf("late answer: %d", response.StatusCode)
		}
		got := decodeJSON[engine.UserInputReply](t, response)
		if got.Request.Status != "answered" {
			t.Fatalf("late answer: %+v", got)
		}
	}
	messages, err := st.ListMessages(ctx, "question-session", 0)
	if err != nil {
		t.Fatal(err)
	}
	answers := 0
	for _, message := range messages {
		if message.ClientMessageID == "input-flow-original:q" {
			answers++
		}
	}
	if answers != 1 {
		t.Fatalf("late answer duplicated: %d", answers)
	}
}

package contextbuilder

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"reflect"
	"testing"

	"github.com/teatak/pudding-core/internal/provider"
	"github.com/teatak/pudding-core/internal/provider/openai"
	"github.com/teatak/pudding-core/internal/store"
	"github.com/teatak/pudding-core/internal/store/storetest"
)

// A widget can record a notification while its invocation is still running.
// Both call and result exist, but chronological history puts a new user input
// between them. Replay must keep the original model exchange intact.
func TestBuildReplaysNotificationDuringToolCall(t *testing.T) {
	ctx := context.Background()
	st := storetest.New(t)
	if err := st.CreateSession(ctx, &store.Session{ID: "s", Provider: "test", Model: "model"}); err != nil {
		t.Fatal(err)
	}
	if _, err := st.BeginTurn(ctx, store.BeginTurnInput{SessionID: "s", TurnID: "action", UserMessageID: "input", ClientMessageID: "input", UserText: "Do the action"}); err != nil {
		t.Fatal(err)
	}
	native := &store.ProviderState{Provider: "test", Model: "model", Kind: provider.ContinuationOpenAIResponses, Data: json.RawMessage(`[{"type":"function_call","id":"fc_action","call_id":"call_action","name":"widget_invoke","arguments":"{}"}]`)}
	if _, err := st.AppendTurnOutput(ctx, store.AppendTurnOutputInput{TurnID: "action", Parts: []store.ContentPart{{Type: store.ContentPartToolUse, CallID: "call_action", Name: "widget_invoke", Args: json.RawMessage(`{}`)}}, ProviderState: native}); err != nil {
		t.Fatal(err)
	}
	if _, err := st.RecordWidgetNotice(ctx, store.WidgetNoticeInput{SessionID: "s", ClientMessageID: "notice", Text: "Action finished", Metadata: json.RawMessage(`{"widgetNotification":{"notificationID":"result"}}`)}); err != nil {
		t.Fatal(err)
	}
	if _, err := st.AppendTurnOutput(ctx, store.AppendTurnOutputInput{TurnID: "action", Parts: []store.ContentPart{{Type: store.ContentPartToolResult, CallID: "call_action", Name: "widget_invoke", Ok: true, Content: `{"accepted":true}`}}}); err != nil {
		t.Fatal(err)
	}
	if _, err := st.FinishTurn(ctx, store.FinishTurnInput{TurnID: "action", Status: store.TurnCompleted}); err != nil {
		t.Fatal(err)
	}
	before, err := st.ListMessages(ctx, "s", 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(before) != 4 || before[2].Text != "Action finished" {
		t.Fatalf("fixture must reproduce interleaved history: %+v", before)
	}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body struct {
			Input []struct {
				Type    string `json:"type"`
				Role    string `json:"role"`
				CallID  string `json:"call_id"`
				Content string `json:"content"`
				Output  string `json:"output"`
			} `json:"input"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Error(err)
			w.WriteHeader(400)
			return
		}
		pending := map[string]bool{}
		calls, outputs, notices := 0, 0, 0
		for _, item := range body.Input {
			switch item.Type {
			case "function_call":
				pending[item.CallID] = true
				calls++
			case "function_call_output":
				delete(pending, item.CallID)
				outputs++
			default:
				if item.Role == "user" && len(pending) > 0 {
					http.Error(w, "No tool output found for tool call call_action", 400)
					return
				}
				if item.Content == "Action finished" {
					notices++
				}
			}
		}
		if calls != 1 || outputs != 1 || notices != 1 {
			t.Errorf("lost canonical context: calls=%d outputs=%d notices=%d", calls, outputs, notices)
		}
		w.Header().Set("Content-Type", "text/event-stream")
		fmt.Fprint(w, "data: {\"type\":\"response.output_text.delta\",\"delta\":\"OK\"}\n\ndata: {\"type\":\"response.completed\"}\n\n")
	}))
	defer srv.Close()
	// Rebuilding twice also covers retry/reopening the same old conversation.
	for i := 0; i < 2; i++ {
		req, err := New(st, nil).BuildForProviderWithTools(ctx, "s", "test", "model", "chat", []provider.ToolDef{{Name: "widget_invoke"}})
		if err != nil {
			t.Fatal(err)
		}
		stream, err := openai.NewResponses(openai.Config{BaseURL: srv.URL}).Stream(ctx, req)
		if err != nil {
			t.Fatal(err)
		}
		for chunk := range stream {
			if chunk.Err != nil {
				t.Error(chunk.Err)
			}
		}
	}
	after, err := st.ListMessages(ctx, "s", 0)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(before, after) {
		t.Fatal("request projection changed canonical history")
	}
}

func TestHistoryTurnOrderingPreservesSteeringAndCompactionBoundaries(t *testing.T) {
	message := func(id, turn string, role store.Role) *store.Message {
		return &store.Message{ID: id, TurnID: turn, Role: role}
	}
	// Retained history follows a summary that belongs to the still-running turn.
	// Same-turn steering must remain between that turn's assistant segments.
	original := []*store.Message{
		message("summary", "active", store.RoleSummary),
		message("old-input", "old", store.RoleUser),
		message("old-output", "old", store.RoleAssistant),
		message("call", "active", store.RoleAssistant),
		message("notice-1", "notice-1", store.RoleUser),
		message("result", "active", store.RoleTool),
		message("steer", "active", store.RoleUser),
		message("notice-2", "notice-2", store.RoleUser),
		message("answer", "active", store.RoleAssistant),
		message("compact", "active", store.RoleSummary),
		message("next", "next", store.RoleUser),
	}
	before := append([]*store.Message(nil), original...)
	var ids []string
	for _, m := range historyInTurnOrder(original) {
		ids = append(ids, m.ID)
	}
	expected := []string{"summary", "old-input", "old-output", "call", "result", "steer", "answer", "notice-1", "notice-2", "compact", "next"}
	if !reflect.DeepEqual(ids, expected) {
		t.Fatalf("history order: %v", ids)
	}
	if !reflect.DeepEqual(original, before) {
		t.Fatal("canonical ordering mutated")
	}
}

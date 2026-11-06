package contextbuilder

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"

	"github.com/teatak/pudding-core/internal/provider"
	"github.com/teatak/pudding-core/internal/provider/openai"
	"github.com/teatak/pudding-core/internal/store"
	"github.com/teatak/pudding-core/internal/store/storetest"
	"github.com/teatak/pudding-core/internal/tool"
)

func TestToolImageBudgetBatchesAndPreservesOtherContent(t *testing.T) {
	messages := []provider.Message{{Role: provider.RoleUser, Text: "keep my request"}, {Role: provider.RoleUser, Parts: []provider.Part{{Type: provider.PartImage, ImageKey: "upload", Data: []byte("user image")}}}}
	for i := 1; i <= 32; i++ {
		key := fmt.Sprintf("shot-%d", i)
		messages = append(messages, provider.Message{Role: provider.RoleUser, Parts: []provider.Part{{Type: provider.PartImage, Name: tool.BrowserScreenshot, CallID: key, ImageKey: key, MIME: "image/png", Data: []byte(key), Width: 1401, Height: 856}}})
	}
	budget := DefaultToolImageBudget(1000000)
	for _, n := range []int{8, 9, 15, 18} {
		through, err := ToolImageCandidate(messages[:n+2], budget, false)
		if err != nil || through != "" {
			t.Fatalf("premature cleanup at %d: %q %v", n, through, err)
		}
	}
	before := fmt.Sprintf("%#v", messages)
	through, err := ToolImageCandidate(messages, budget, false)
	if err != nil || through == "" {
		t.Fatalf("missing budget cleanup: %v", err)
	}
	got := ApplyToolImageWatermark(messages, through)
	count := 0
	for _, m := range got[2:] {
		for _, p := range m.Parts {
			if p.Type == provider.PartImage {
				count++
			} else if p.ImageKey != "" && (len(p.Data) > 0 || !strings.Contains(p.Text, tool.MediaRead)) {
				t.Fatal("bad historical placeholder")
			}
		}
	}
	if count != 13 || got[0].Text != "keep my request" || string(got[1].Parts[0].Data) != "user image" {
		t.Fatalf("wrong bounded content: %d", count)
	}
	if !reflect.DeepEqual(got, ApplyToolImageWatermark(got, through)) || before != fmt.Sprintf("%#v", messages) {
		t.Fatal("projection changed source or was not idempotent")
	}
	appended := append(append([]provider.Message(nil), got...), messages[2])
	if next, err := ToolImageCandidate(appended, budget, false); err != nil || next != "" {
		t.Fatal("cleanup slid on next observation")
	}
	if !reflect.DeepEqual(got, appended[:len(got)]) {
		t.Fatal("stable prefix changed")
	}
	// Encoded byte pressure is independent of token estimates.
	tiny := ToolImageBudget{MaxTokens: 100000, MaxBytes: 12}
	candidate, err := ToolImageCandidate(messages[2:5], tiny, false)
	if err != nil || candidate == "" {
		t.Fatalf("byte budget did not clean: %v", err)
	}
	if _, err := ToolImageCandidate(messages[2:3], ToolImageBudget{MaxTokens: 1, MaxBytes: 1}, false); err == nil {
		t.Fatal("oversized latest image silently accepted")
	}
}

func cleanImagesForRewrite(t *testing.T, messages []provider.Message) []provider.Message {
	t.Helper()
	through, err := ToolImageCandidate(messages, DefaultToolImageBudget(1000000), true)
	if err != nil {
		t.Fatal(err)
	}
	return ApplyToolImageWatermark(messages, through)
}

func TestSavedScreenshotWatermarkSurvivesFilteredBoundary(t *testing.T) {
	var canonical []*store.Message
	makeImage := func(turn, toolName string) provider.Part {
		return provider.Part{Type: provider.PartImage, ImageTurnID: turn, CallID: "reused", Name: toolName, ImageKey: "same.png", MIME: "image/png", Data: []byte(turn)}
	}
	for _, pair := range [][2]string{{"old", tool.BrowserScreenshot}, {"boundary", tool.MediaRead}, {"new", tool.BrowserScreenshot}} {
		canonical = append(canonical, &store.Message{ID: pair[0], TurnID: pair[0], Role: store.RoleTool, Parts: []store.ContentPart{{Type: store.ContentPartToolUse, Name: pair[1], CallID: "reused", Args: json.RawMessage(`{"purpose":"observation"}`)}, {Type: store.ContentPartToolResult, Name: pair[1], CallID: "reused"}, {Type: store.ContentPartAttachment, Origin: "tool", AttachmentKey: "same.png", MIME: "image/png"}}})
	}
	// The watermark's media_read has been filtered from this mode, while captures remain.
	projected := []provider.Message{{Parts: []provider.Part{makeImage("old", tool.BrowserScreenshot)}}, {Parts: []provider.Part{makeImage("new", tool.BrowserScreenshot)}}}
	state := store.ContextCooling{ImageThroughID: ToolImageID(makeImage("boundary", tool.MediaRead))}
	got := applySavedToolImages(projected, canonical, state)
	if got[0].Parts[0].Type != provider.PartText || got[1].Parts[0].Type != provider.PartImage {
		t.Fatal("filtered watermark revived an old image or removed a later reread")
	}
}

func TestToolImagePruningPreservesCallsButDropsBoundAnthropicThinking(t *testing.T) {
	states := []provider.Continuation{
		{Kind: provider.ContinuationAnthropic, Data: json.RawMessage(`[{"type":"thinking","thinking":"private","signature":"bound"},{"type":"redacted_thinking","data":"bound"},{"type":"tool_use","id":"click","name":"click","input":{}}]`)},
		{Kind: provider.ContinuationGoogle, Data: json.RawMessage(`[{"functionCall":{"name":"click"},"thoughtSignature":"required"}]`)},
	}
	messages := []provider.Message{{Role: provider.RoleAssistant, Continuations: states}}
	for i := 0; i < 9; i++ {
		messages = append(messages, provider.Message{Role: provider.RoleUser, Parts: []provider.Part{{Type: provider.PartImage, Name: tool.BrowserScreenshot, ImageKey: fmt.Sprint(i), Data: []byte("image")}}})
	}
	messages = append(messages, provider.Message{Role: provider.RoleAssistant, Continuations: states, Parts: []provider.Part{{Type: provider.PartToolUse, CallID: "click", Name: "click"}}})
	got := cleanImagesForRewrite(t, messages)
	if !reflect.DeepEqual(got[0].Continuations, states) {
		t.Fatal("state before omitted image changed")
	}
	last := got[len(got)-1]
	if strings.Contains(string(last.Continuations[0].Data), "thinking") || !strings.Contains(string(last.Continuations[0].Data), "tool_use") {
		t.Fatal("bound thinking/call handling failed")
	}
	if !reflect.DeepEqual(last.Continuations[1], states[1]) || !strings.Contains(string(states[0].Data), "thinking") {
		t.Fatal("other provider or canonical state changed")
	}
}

func TestScreenshotBudgetSeparatesReferencesAndTemporaryReads(t *testing.T) {
	var messages []provider.Message
	addRead := func(purpose, key string) {
		// Deliberately reuse a call ID in later exchanges to check chronological
		// attribution instead of assigning the last call's purpose to all images.
		args := json.RawMessage(`{}`)
		if purpose != "" {
			args, _ = json.Marshal(map[string]string{"purpose": purpose})
		}
		messages = append(messages,
			provider.Message{Role: provider.RoleAssistant, Parts: []provider.Part{{Type: provider.PartToolUse, Name: tool.MediaRead, CallID: "read", Args: args}}},
			provider.Message{Role: provider.RoleUser, Parts: []provider.Part{{Type: provider.PartToolResult, Name: tool.MediaRead, CallID: "read", Content: `{"cold_only":true}`}}},
			provider.Message{Role: provider.RoleUser, Parts: []provider.Part{{Type: provider.PartImage, Name: tool.MediaRead, CallID: "read", ImageKey: key, Data: []byte(key)}}},
		)
	}
	addRead("", "legacy-reference")
	addRead(tool.MediaPurposeReference, "explicit-reference")
	addRead(tool.MediaPurposeObservation, "temporary-read")
	for i := 0; i < 8; i++ {
		key := fmt.Sprintf("shot-%d", i)
		messages = append(messages, provider.Message{Role: provider.RoleUser, Parts: []provider.Part{{Type: provider.PartImage, Name: tool.BrowserScreenshot, ImageKey: key, Data: []byte(key)}}})
	}
	addRead(tool.MediaPurposeReference, "promoted-screenshot")
	projected := cleanImagesForRewrite(t, messages)
	var images []string
	for _, m := range projected {
		for _, p := range m.Parts {
			if p.Type == provider.PartImage {
				images = append(images, p.ImageKey)
			}
		}
	}
	want := []string{"legacy-reference", "explicit-reference", "shot-5", "shot-6", "shot-7", "promoted-screenshot"}
	if !reflect.DeepEqual(images, want) {
		t.Fatalf("image purposes lost: got %v want %v", images, want)
	}
	if !reflect.DeepEqual(projected, cleanImagesForRewrite(t, projected)) {
		t.Fatal("reprojection changed references or screenshot boundary")
	}
	for _, p := range projected[8].Parts {
		if p.ImageKey == "temporary-read" && (!strings.Contains(p.Text, `"purpose":"observation"`) || !strings.Contains(p.Text, "purpose=reference")) {
			t.Fatal("omitted screenshot lacks readback purpose guidance")
		}
	}
}

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
	wantNotice := fmt.Sprintf("<message-meta>\ntime: %s\n</message-meta>\nAction finished", before[2].CreatedAt.Format("2006-01-02 15:04:05 -07:00"))
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
				if item.Content == wantNotice {
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

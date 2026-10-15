package contextbuilder

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/teatak/pudding-core/internal/provider"
	"github.com/teatak/pudding-core/internal/store"
	"github.com/teatak/pudding-core/internal/store/storetest"
)

func TestSubagentResetCutsAllAutomaticHistoryBeforeProviderProjection(t *testing.T) {
	ctx := context.Background()
	st := storetest.New(t)
	if err := st.CreateSession(ctx, &store.Session{ID: "child", Provider: "openai", Model: "model", ActiveMode: store.ModeWork}); err != nil {
		t.Fatal(err)
	}
	input := store.SubagentResetInput{ParentSessionID: "parent", ParentTurnID: "parent-turn", CallID: "reset", SessionID: "child", ExpectedTurnID: "old-turn"}
	turnID, messageID := store.SubagentResetIDs(input.ParentSessionID, input.ParentTurnID, input.CallID)
	old := []*store.Message{
		{ID: "old-user", SessionID: "child", TurnID: "old-turn", Role: store.RoleUser, Parts: store.TextPart("OLD_TASK_CONTEXT")},
		{ID: "old-assistant", SessionID: "child", TurnID: "old-turn", Role: store.RoleAssistant, Parts: store.TextPart("OLD_RESULT"), ProviderState: &store.ProviderState{Provider: "openai", Model: "model", Kind: provider.ContinuationOpenAIResponses, Data: json.RawMessage(`[{"type":"message","role":"assistant","content":[{"type":"output_text","text":"OLD_NATIVE_STATE"}]}]`)}},
		{ID: "old-summary", SessionID: "child", TurnID: "old-compact", Role: store.RoleSummary, Kind: store.MessageKindSummary, Parts: store.TextPart("OLD_COMPACT_SUMMARY"), Metadata: store.CompactMessageMetadata([]string{"old-user", "old-assistant"}, nil)},
	}
	checkpoint := &store.Message{ID: messageID, SessionID: "child", TurnID: turnID, Role: store.RoleSummary, Kind: store.MessageKindSummary, Parts: store.TextPart("RESET_MAINTENANCE"), Metadata: store.NewSubagentResetMetadata(input).JSON()}
	fresh := &store.Message{ID: "new-user", SessionID: "child", TurnID: "new-turn", Role: store.RoleUser, Parts: store.TextPart("NEW_SELF_CONTAINED_TASK")}
	history := append(append([]*store.Message(nil), old...), checkpoint, fresh)
	before, _ := json.Marshal(history)
	b := New(st, nil)
	req, err := b.BuildForProviderWithHistory(ctx, "child", "openai", "model", "work", []provider.ToolDef{}, history, provider.ModelConfig{})
	if err != nil {
		t.Fatal(err)
	}
	raw, err := json.Marshal(req.Messages)
	if err != nil {
		t.Fatal(err)
	}
	if len(req.Messages) != 1 || !strings.Contains(string(raw), "NEW_SELF_CONTAINED_TASK") {
		t.Fatalf("new task was lost: %s", raw)
	}
	for _, forbidden := range []string{"OLD_TASK_CONTEXT", "OLD_RESULT", "OLD_COMPACT_SUMMARY", "OLD_NATIVE_STATE", "RESET_MAINTENANCE"} {
		if strings.Contains(string(raw), forbidden) {
			t.Fatalf("reset reintroduced %s: %s", forbidden, raw)
		}
	}
	for _, message := range req.Messages {
		if len(message.Continuations) != 0 {
			t.Fatal("pre-reset provider continuation was reused")
		}
	}
	after, _ := json.Marshal(history)
	if string(before) != string(after) {
		t.Fatal("model projection changed stored history")
	}
	// A later compaction may not recover an old tail ID across the boundary.
	compact := &store.Message{ID: "new-summary", SessionID: "child", TurnID: "compact-new", Role: store.RoleSummary, Kind: store.MessageKindSummary, Parts: store.TextPart("NEW_SUMMARY"), Metadata: store.CompactMessageMetadata([]string{"new-user"}, []string{"old-user"})}
	effective := EffectiveMessages(append(history, compact))
	for _, message := range effective {
		if message.ID == "old-user" || message.ID == "old-summary" {
			t.Fatal("compaction restored a pre-reset tail")
		}
	}
	// Reset-only produces no task messages; ordinary continuation still does.
	empty, err := b.BuildForProviderWithHistory(ctx, "child", "openai", "model", "work", []provider.ToolDef{}, history[:len(history)-1], provider.ModelConfig{})
	if err != nil || len(empty.Messages) != 0 {
		t.Fatalf("reset-only retained automatic context: %+v %v", empty.Messages, err)
	}
	continued, err := b.BuildForProviderWithHistory(ctx, "child", "openai", "model", "work", []provider.ToolDef{}, append(append([]*store.Message(nil), old...), fresh), provider.ModelConfig{})
	if err != nil {
		t.Fatal(err)
	}
	raw, _ = json.Marshal(continued.Messages)
	if !strings.Contains(string(raw), "OLD_COMPACT_SUMMARY") || !strings.Contains(string(raw), "NEW_SELF_CONTAINED_TASK") {
		t.Fatalf("ordinary send unexpectedly reset history: %s", raw)
	}
}

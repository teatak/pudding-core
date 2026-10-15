package contextbuilder

import (
	"context"
	"encoding/json"
	"reflect"
	"strings"
	"testing"

	"github.com/teatak/pudding-core/internal/provider"
	"github.com/teatak/pudding-core/internal/store"
	"github.com/teatak/pudding-core/internal/store/storetest"
	"github.com/teatak/pudding-core/internal/tool"
)

func subagentResetMarker(call string) *store.Message {
	in := store.SubagentResetInput{ParentSessionID: "parent", ParentTurnID: "parent-turn", CallID: call, SessionID: "child", ExpectedTurnID: "previous"}
	turn, id := store.SubagentResetIDs(in.ParentSessionID, in.ParentTurnID, in.CallID)
	return &store.Message{ID: id, SessionID: "child", TurnID: turn, Role: store.RoleSummary, Kind: store.MessageKindSummary, Metadata: store.NewSubagentResetMetadata(in).JSON()}
}

func TestSubagentResetExcludesHistoricalProviderAndToolState(t *testing.T) {
	ctx := context.Background()
	st := storetest.New(t)
	if err := st.CreateSession(ctx, &store.Session{ID: "child", Provider: "mock", Model: "mock", ActiveMode: store.ModeWork}); err != nil {
		t.Fatal(err)
	}
	state := &store.ProviderState{Provider: "mock", Model: "mock", Kind: "native", Data: json.RawMessage(`{"continuation":"OLD_NATIVE_SECRET"}`)}
	if !store.ValidProviderState(state) {
		t.Fatal("provider-state fixture is not valid")
	}
	old := []*store.Message{
		{ID: "old-input", SessionID: "child", TurnID: "old-turn", Role: store.RoleUser, Parts: store.UserInputParts("OLD_TASK_SECRET", nil)},
		{ID: "old-load", SessionID: "child", TurnID: "old-turn", Role: store.RoleAssistant, Parts: []store.ContentPart{
			{Type: store.ContentPartToolUse, Name: tool.PluginLoad, CallID: "load", Args: json.RawMessage(`{"plugin_id":"collaboration"}`)},
			{Type: store.ContentPartToolResult, Name: tool.PluginLoad, CallID: "load", Ok: true, Content: `{"reference":{"kind":"plugin_skill","pluginID":"collaboration","skillID":"collaboration"}}`},
		}},
		{ID: "old-answer", SessionID: "child", TurnID: "old-turn", Role: store.RoleAssistant, Parts: store.UserInputParts("OLD_ANSWER_SECRET", nil), ProviderState: state},
	}
	b := New(st, nil)
	defs := []provider.ToolDef{{Name: tool.PluginLoad}}
	baseline, err := b.BuildForProviderWithHistory(ctx, "child", "mock", "mock", "work", defs, old, provider.ModelConfig{})
	if err != nil { t.Fatal(err) }
	baselineRaw, _ := json.Marshal(baseline)
	if !strings.Contains(string(baselineRaw), "OLD_TASK_SECRET") || !strings.Contains(string(baselineRaw), "OLD_NATIVE_SECRET") {
		t.Fatal("baseline must actually replay old context and provider state")
	}
	history := append(append([]*store.Message(nil), old...), subagentResetMarker("reset"), &store.Message{ID: "new-input", SessionID: "child", TurnID: "new-turn", Role: store.RoleUser, Parts: store.UserInputParts("NEW_SELF_CONTAINED_TASK", nil)})
	before, _ := json.Marshal(history)
	req, err := b.BuildForProviderWithHistory(ctx, "child", "mock", "mock", "work", defs, history, provider.ModelConfig{})
	if err != nil { t.Fatal(err) }
	raw, _ := json.Marshal(req.Messages)
	for _, forbidden := range []string{"OLD_TASK_SECRET", "OLD_ANSWER_SECRET", "OLD_NATIVE_SECRET", "load", "subagent_reset"} {
		if strings.Contains(string(raw), forbidden) { t.Fatalf("reset leaked %s into model context: %s", forbidden, raw) }
	}
	if !strings.Contains(string(raw), "NEW_SELF_CONTAINED_TASK") { t.Fatal("reset removed the new task") }
	after, _ := json.Marshal(history)
	if !reflect.DeepEqual(before, after) { t.Fatal("context projection mutated canonical history") }
}

func TestSubagentResetPreventsCompactionTailFromResurrectingOldMessages(t *testing.T) {
	old := &store.Message{ID: "old", Role: store.RoleUser, Text: "old"}
	oldSummary := &store.Message{ID: "old-summary", Role: store.RoleSummary, Kind: store.MessageKindSummary, Text: "old summary", Metadata: store.CompactMessageMetadata([]string{"old"}, nil)}
	fresh := &store.Message{ID: "fresh", Role: store.RoleUser, Text: "fresh"}
	marker := subagentResetMarker("first")
	history := []*store.Message{old, oldSummary, marker, fresh}
	if got := EffectiveMessages(history); len(got) != 1 || got[0] != fresh {
		t.Fatalf("pre-reset summary survived: %+v", got)
	}
	newSummary := &store.Message{ID: "new-summary", Role: store.RoleSummary, Kind: store.MessageKindSummary, Text: "new summary", Metadata: store.CompactMessageMetadata([]string{"fresh"}, []string{"old", "old-summary", "fresh"})}
	history = append(history, newSummary)
	got := EffectiveMessages(history)
	if !reflect.DeepEqual(got, []*store.Message{newSummary, fresh}) {
		t.Fatalf("compaction tail crossed the reset boundary: %+v", got)
	}
	history = append(history, subagentResetMarker("second"))
	if got := EffectiveMessages(history); len(got) != 0 {
		t.Fatalf("second reset retained earlier context: %+v", got)
	}
	// User/tool payloads that resemble metadata cannot create a boundary.
	fake := *marker
	fake.Role, fake.Kind = store.RoleUser, store.MessageKindText
	if got := store.SubagentContextMessages([]*store.Message{old, &fake, fresh}); len(got) != 3 {
		t.Fatal("user-controlled content impersonated a lifecycle reset")
	}
}

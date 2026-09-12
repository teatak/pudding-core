package engine

import (
	"context"
	"encoding/json"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/teatak/pudding-core/internal/contextbuilder"
	"github.com/teatak/pudding-core/internal/provider"
	"github.com/teatak/pudding-core/internal/store"
	"github.com/teatak/pudding-core/internal/store/sqlitestore"
	"github.com/teatak/pudding-core/internal/tool"
)

func TestResultPreviewReadbackSurvivesRunningTurnRestartAndCompaction(t *testing.T) {
	ctx := context.Background()
	dbPath := filepath.Join(t.TempDir(), "test.db")
	db, err := sqlitestore.Open(dbPath)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = db.Close() }()
	for _, id := range []string{"s1", "s2"} {
		if err := db.CreateSession(ctx, &store.Session{ID: id, Provider: "mock", Model: "test"}); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := db.BeginTurn(ctx, store.BeginTurnInput{SessionID: "s1", TurnID: "t1", UserMessageID: "m1", ClientMessageID: "c1", UserText: "inspect", Mode: store.ModeCode}); err != nil {
		t.Fatal(err)
	}
	stdout := strings.Repeat("log 中文🙂\n", 3000) + "MIDDLE-EVIDENCE" + strings.Repeat("tail\n", 3000)
	raw, _ := json.Marshal(map[string]any{"ok": false, "exitCode": 2, "stdout": stdout, "stderr": "failure details"})
	var parts turnPartAccumulator
	parts.AppendTool(provider.ToolCallChunk{CallID: "call1", Name: tool.CommandRun, ArgsDelta: `{"scope":"project","command":"test"}`})
	e := &Engine{store: db}
	native := &store.ProviderState{Provider: "mock", Model: "test", Kind: provider.ContinuationOpenAIChat,
		Data: json.RawMessage(`{"role":"assistant","reasoning_content":"retain protocol state","tool_calls":[{"id":"call1","type":"function","function":{"name":"builtin_command_run","arguments":"{}"}}]}`)}
	if err := e.commitTurnPartsWithProviderState("t1", &parts, native); err != nil {
		t.Fatal(err)
	}
	parts.AppendToolResult(tool.Result{CallID: "call1", Name: tool.CommandRun, Ok: false, Content: string(raw)})
	if err := e.commitTurnParts("t1", &parts, true); err != nil {
		t.Fatal(err)
	}
	continuations := []provider.Continuation{{Kind: native.Kind, Data: native.Data}}
	current := requestMessagesWithTurnParts(nil, parts.Parts(), continuations, "s1", "t1", "", provider.ModelConfig{}, true)
	resultContents := func(messages []provider.Message) []string {
		var out []string
		for _, msg := range messages {
			for _, part := range msg.Parts {
				if part.Type == provider.PartToolResult {
					out = append(out, part.Content)
				}
			}
		}
		return out
	}
	views := resultContents(current)
	if len(views) != 1 || len(views[0]) >= len(raw) || !strings.Contains(views[0], `"preview_only":true`) {
		t.Fatalf("live result was not projected: %d views", len(views))
	}
	var preview struct {
		Ref tool.ResultReference `json:"result_ref"`
	}
	if err := json.Unmarshal([]byte(views[0]), &preview); err != nil {
		t.Fatal(err)
	}
	read := func(sessionID string, args any) tool.Result {
		encoded, _ := json.Marshal(args)
		return tool.NewBuiltinRunner(tool.WithHistorySearch(db)).Call(ctx, tool.Call{SessionID: sessionID, CallID: "read", Name: tool.HistoryGetMessage, Args: encoded})
	}
	assertReadable := func() {
		t.Helper()
		focused := read("s1", map[string]any{"result_ref": preview.Ref, "field": "stdout", "unit": "lines", "query": "MIDDLE-EVIDENCE"})
		var evidence struct {
			Lines []struct {
				Line int    `json:"line"`
				Text string `json:"text"`
			} `json:"lines"`
			HasMore bool `json:"has_more"`
		}
		if !focused.Ok || json.Unmarshal([]byte(focused.Content), &evidence) != nil || len(evidence.Lines) != 1 || evidence.HasMore || evidence.Lines[0].Line != 3001 || evidence.Lines[0].Text != "MIDDLE-EVIDENCEtail\n" {
			t.Fatalf("focused historical evidence lookup failed: %s", focused.Content)
		}
		var restored strings.Builder
		offset := 0
		for {
			result := read("s1", map[string]any{"result_ref": preview.Ref, "field": "stdout", "offset": offset, "limit": 997})
			if !result.Ok {
				t.Fatalf("readback failed: %s", result.Content)
			}
			var page struct {
				Content    string `json:"content"`
				NextOffset int    `json:"next_offset"`
				HasMore    bool   `json:"has_more"`
				ResultOK   bool   `json:"result_ok"`
				Snapshot   bool   `json:"snapshot"`
			}
			if err := json.Unmarshal([]byte(result.Content), &page); err != nil {
				t.Fatal(err)
			}
			if page.ResultOK || !page.Snapshot {
				t.Fatal("read success confused with original command success")
			}
			restored.WriteString(page.Content)
			if !page.HasMore {
				break
			}
			if page.NextOffset <= offset {
				t.Fatal("readback did not progress")
			}
			offset = page.NextOffset
		}
		if restored.String() != stdout {
			t.Fatal("paged UTF-8 snapshot differs from original")
		}
	}
	assertReadable() // Already readable while the originating turn is running.
	for _, args := range []map[string]any{
		{"result_ref": preview.Ref, "offset": -1},
		{"result_ref": preview.Ref, "offset": 999999},
		{"result_ref": preview.Ref, "limit": 8001},
		{"result_ref": preview.Ref, "limit": 0},
		{"result_ref": preview.Ref, "field": "_provider_state"},
		{"result_ref": preview.Ref, "message_id": "m1"},
		{"result_ref": map[string]string{"turn_id": "t1", "call_id": "missing"}},
		{"result_ref": map[string]string{"turn_id": "t1", "call_id": "call1", "unknown": "x"}},
		{"message_id": "m1", "offset": 0},
		{"message_id": "m1", "unit": "lines"},
		{"message_id": "m1", "query": "MIDDLE-EVIDENCE"},
	} {
		if result := read("s1", args); result.Ok {
			t.Fatalf("invalid read accepted: %+v", args)
		}
	}
	if result := read("s2", map[string]any{"result_ref": preview.Ref}); result.Ok || strings.Contains(result.Content, "MIDDLE-EVIDENCE") {
		t.Fatal("cross-session result leaked")
	}
	if result := read("s2", map[string]any{"result_ref": preview.Ref, "field": "stdout", "unit": "lines", "query": "MIDDLE-EVIDENCE"}); result.Ok || strings.Contains(result.Content, "MIDDLE-EVIDENCE") {
		t.Fatal("focused read leaked a cross-session result")
	}
	if _, err := db.FinishTurn(ctx, store.FinishTurnInput{TurnID: "t1", Status: store.TurnCompleted}); err != nil {
		t.Fatal(err)
	}
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}
	db, err = sqlitestore.Open(dbPath)
	if err != nil {
		t.Fatal(err)
	}
	assertReadable() // No runtime cache is needed after restarting.
	defs := []provider.ToolDef{{Name: tool.CommandRun}, {Name: tool.HistoryGetMessage}}
	replay, err := contextbuilder.New(db, nil).BuildForProviderWithTools(ctx, "s1", "mock", "test", string(store.ModeCode), defs, provider.ModelConfig{})
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(views, resultContents(replay.Messages)) {
		t.Fatal("live and historical projections differ")
	}
	nativeFound := false
	for _, msg := range replay.Messages {
		for _, state := range msg.Continuations {
			if string(state.Data) == string(native.Data) {
				nativeFound = true
			}
		}
	}
	if !nativeFound {
		t.Fatal("native reasoning protocol state changed")
	}
	turn, err := db.GetConversationTurn(ctx, "s1", "t1")
	if err != nil {
		t.Fatal(err)
	}
	var messageIDs []string
	originalFound := false
	for _, msg := range turn.Messages {
		messageIDs = append(messageIDs, msg.ID)
		for _, part := range msg.Parts {
			if part.Type == store.ContentPartToolResult && part.Content == string(raw) {
				originalFound = true
			}
		}
	}
	if !originalFound {
		t.Fatal("canonical tool result was replaced by preview")
	}
	metadata, _ := json.Marshal(map[string]any{"compact": map[string]any{"source_message_ids": messageIDs}})
	_, err = db.AppendCompactSummary(ctx, store.AppendCompactSummaryInput{SessionID: "s1", TurnID: "compact", MessageID: "summary", ClientMessageID: "compact-client", Text: "Finished inspection", Metadata: metadata})
	if err != nil {
		t.Fatal(err)
	}
	assertReadable() // A retained reference can still read an older compacted turn.
}

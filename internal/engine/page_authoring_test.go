package engine

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/teatak/pudding-core/internal/event"
	"github.com/teatak/pudding-core/internal/plugin"
	"github.com/teatak/pudding-core/internal/provider"
	"github.com/teatak/pudding-core/internal/store"
	"github.com/teatak/pudding-core/internal/store/storetest"
	"github.com/teatak/pudding-core/internal/tool"
)

func TestPageAuthoringRejectsOneShotWritesFromHistory(t *testing.T) {
	ctx := context.Background()
	st := storetest.New(t)
	eng := New(st, event.NewHub(), nil, st, WithPlugins(plugin.NewService(t.TempDir(), nil)))
	if err := st.CreateSession(ctx, &store.Session{ID: "writer", Provider: "mock", Model: "mock", LoadedPluginIDs: []string{plugin.BuiltinPageAuthoringID}}); err != nil {
		t.Fatal(err)
	}
	if _, err := st.BeginTurn(ctx, store.BeginTurnInput{SessionID: "writer", TurnID: "turn", UserMessageID: "message", ClientMessageID: "input", UserText: "Revise again"}); err != nil {
		t.Fatal(err)
	}
	defs, err := eng.toolDefinitions(ctx, "writer", store.ModeChat)
	if err != nil {
		t.Fatal(err)
	}
	note, err := st.CreatePage(ctx, &store.StudioItem{ID: "page_test", Kind: store.StudioItemKindPage, Name: "Test"}, "Keep this paragraph.\n\nChange this paragraph.", store.ContentAuthor{Kind: "user"})
	if err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		name string
		args any
	}{
		{tool.PageCreate, map[string]any{"name": "Unwanted duplicate", "body": "Full draft bypass"}},
		{tool.PageEdit, map[string]any{"item_id": note.ItemID, "expected_hash": note.ContentHash, "body": "Silent replacement"}},
		{tool.PageEdit, map[string]any{"item_id": note.ItemID, "edits": []map[string]string{{"old": "Change", "new": "Silent"}}}},
		{tool.PageEdit, map[string]any{"item_id": note.ItemID, "action": "commit", "body": "Bypass via commit"}},
	} {
		raw, _ := json.Marshal(tc.args)
		parts := &turnPartAccumulator{}
		parts.AppendTool(provider.ToolCallChunk{Index: 0, CallID: store.NewID("call"), Name: tc.name, ArgsDelta: string(raw)})
		status, _, _, _ := eng.executePendingTools(ctx, "writer", "turn", store.ModeChat, defs, parts)
		if status != store.TurnRunning {
			t.Fatal("model must be able to recover", status)
		}
		found := false
		for _, part := range parts.Parts() {
			if part.Type == store.ContentPartToolResult {
				found = true
				if part.Ok || !strings.Contains(part.Content, "page_edit_requires_begin") {
					t.Fatalf("one-shot call accepted: %+v", part)
				}
			}
		}
		if !found {
			t.Fatal("missing recovery instructions")
		}
		unchanged, _ := st.GetPage(ctx, note.ItemID)
		items, _ := st.ListStudioItems(ctx, store.StudioItemsActive)
		revisions, _ := st.ListStudioItemRevisions(ctx, note.ItemID)
		if unchanged.ContentHash != note.ContentHash || len(items) != 1 || len(revisions) != 1 {
			t.Fatal("rejected call changed content or history")
		}
	}
}

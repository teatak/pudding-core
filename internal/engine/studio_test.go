package engine

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/teatak/pudding-core/internal/event"
	"github.com/teatak/pudding-core/internal/plugin"
	"github.com/teatak/pudding-core/internal/store"
	"github.com/teatak/pudding-core/internal/store/storetest"
	"github.com/teatak/pudding-core/internal/tool"
)

func TestStudioChatToolsWithoutDesktop(t *testing.T) {
	ctx := context.Background()
	st := storetest.New(t)
	eng := New(st, event.NewHub(), nil, st, WithPlugins(plugin.NewService(t.TempDir(), nil)))
	for _, id := range []string{"writer", "other"} {
		loaded := []string{}
		if id == "writer" {
			loaded = []string{plugin.BuiltinStudioID}
		}
		if err := st.CreateSession(ctx, &store.Session{ID: id, Provider: "mock", Model: "mock", LoadedPluginIDs: loaded}); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := st.BeginTurn(ctx, store.BeginTurnInput{SessionID: "writer", TurnID: "turn", UserMessageID: "message", ClientMessageID: "input", UserText: "Edit a document"}); err != nil {
		t.Fatal(err)
	}
	defs, err := eng.toolDefinitions(ctx, "writer", store.ModeChat)
	if err != nil {
		t.Fatal(err)
	}
	for _, def := range tool.StudioDefinitions() {
		if !tool.HasDefinition(defs, def.Name) {
			t.Fatal("Chat tool missing", def.Name)
		}
	}
	other, _ := eng.toolDefinitions(ctx, "other", store.ModeChat)
	if tool.HasDefinition(other, tool.DocEdit) {
		t.Fatal("unloaded plugin exposed")
	}
	run := func(name, id string, args any) tool.Result {
		t.Helper()
		raw, _ := json.Marshal(args)
		out := eng.executeAllowedTool(ctx, "writer", "turn", store.ModeChat, tool.Call{Name: name, CallID: id, Args: raw})
		if !out.Ok {
			t.Fatalf("%s failed: %s", name, out.Content)
		}
		return out
	}
	result := run(tool.DocCreate, "create", map[string]any{"name": "Notes", "body": "甲\n\n乙"})
	var created struct {
		ItemID      string `json:"itemID"`
		ContentHash string `json:"contentHash"`
	}
	json.Unmarshal([]byte(result.Content), &created)
	run(tool.DocCreate, "create", map[string]any{"name": "Notes", "body": "甲\n\n乙"})
	items, _ := st.ListStudioItems(ctx)
	if len(items) != 1 {
		t.Fatal("retry created duplicate document")
	}
	run(tool.DocEdit, "edit", map[string]any{"item_id": created.ItemID, "edits": []map[string]string{{"old": "乙", "new": "AI 修改"}}})
	read := run(tool.DocRead, "read", map[string]any{"item_id": created.ItemID, "offset": 0, "limit": 2})
	if !strings.Contains(read.Content, `"nextOffset":2`) {
		t.Fatal("read pagination missing", read.Content)
	}
	mounts, _ := st.ListStudioMounts(ctx, "writer")
	others, _ := st.ListStudioMounts(ctx, "other")
	if len(mounts) != 1 || len(others) != 0 || mounts[0].Kind != "doc" {
		t.Fatal("mount scope incorrect")
	}
	revisions, _ := st.ListStudioItemRevisions(ctx, created.ItemID)
	if len(revisions) != 2 || revisions[0].Author.SessionID != "writer" || revisions[0].Author.TurnID != "turn" {
		t.Fatal("wrong version authors", revisions)
	}
	doc, _ := st.GetDocument(ctx, created.ItemID)
	if doc.Body != "甲\n\nAI 修改" {
		t.Fatal(doc.Body)
	}
}

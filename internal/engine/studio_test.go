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

func TestStudioChatToolsWithoutDesktop(t *testing.T) {
	for _, prefix := range []string{"page", "note", "doc"} {
		t.Run(prefix, func(t *testing.T) { testStudioChatTools(t, prefix) })
	}
}

func testStudioChatTools(t *testing.T, prefix string) {
	ctx := context.Background()
	st := storetest.New(t)
	eng := New(st, event.NewHub(), nil, st, WithPlugins(plugin.NewService(t.TempDir(), nil)))
	for _, id := range []string{"writer", "other"} {
		loaded := []string{}
		if id == "writer" {
			loaded = []string{plugin.BuiltinPageAuthoringID}
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
	for _, name := range []string{tool.PageCreate, tool.PageRead, tool.PageEdit, tool.PageRename} {
		if !tool.HasDefinition(defs, name) {
			t.Fatal("Chat tool missing", name)
		}
	}
	other, _ := eng.toolDefinitions(ctx, "other", store.ModeChat)
	if tool.HasDefinition(other, tool.PageEdit) {
		t.Fatal("unloaded plugin exposed")
	}
	run := func(name, id string, args any) tool.Result {
		t.Helper()
		raw, _ := json.Marshal(args)
		name = strings.Replace(name, "builtin_page_", "builtin_"+prefix+"_", 1)
		parts := &turnPartAccumulator{}
		parts.AppendTool(provider.ToolCallChunk{Index: 0, CallID: id, Name: name, ArgsDelta: string(raw)})
		status, message, _, _ := eng.executePendingTools(ctx, "writer", "turn", store.ModeChat, defs, parts)
		if status != store.TurnRunning {
			t.Fatal(status, message)
		}
		for _, part := range parts.Parts() {
			if part.Type == store.ContentPartToolResult {
				if !part.Ok || part.Name != name || part.CallID != id {
					t.Fatalf("call/result identity or execution failed: %+v", part)
				}
				return tool.Result{CallID: part.CallID, Name: part.Name, Ok: part.Ok, Content: part.Content}
			}
		}
		t.Fatal("missing result")
		return tool.Result{}
	}
	result := run(tool.PageCreate, "create", map[string]any{"name": "Notes", "body": "甲\n\n乙"})
	var created struct {
		ItemID      string `json:"itemID"`
		ContentHash string `json:"contentHash"`
	}
	json.Unmarshal([]byte(result.Content), &created)
	originalPrefix := prefix
	prefix = "page" // Retry an old create under the canonical name without duplicating the item.
	run(tool.PageCreate, "create", map[string]any{"name": "Notes", "body": "甲\n\n乙"})
	prefix = originalPrefix
	items, _ := st.ListStudioItems(ctx, store.StudioItemsActive)
	if len(items) != 1 {
		t.Fatal("retry created duplicate document")
	}
	run(tool.PageEdit, "edit", map[string]any{"item_id": created.ItemID, "edits": []map[string]string{{"old": "乙", "new": "AI 修改"}}})
	read := run(tool.PageRead, "read", map[string]any{"item_id": created.ItemID, "offset": 0, "limit": 2})
	if !strings.Contains(read.Content, `"nextOffset":2`) {
		t.Fatal("read pagination missing", read.Content)
	}
	renameRes := run(tool.PageRename, "rename", map[string]any{"item_id": created.ItemID, "name": "Renamed Page"})
	if !strings.Contains(renameRes.Content, `"name":"Renamed Page"`) {
		t.Fatal("rename result missing new name", renameRes.Content)
	}
	renamedItem, err := st.GetStudioItem(ctx, created.ItemID)
	if err != nil || renamedItem.Name != "Renamed Page" {
		t.Fatal("renamed item check failed", renamedItem, err)
	}
	mounts, _ := st.ListStudioMounts(ctx, "writer")
	others, _ := st.ListStudioMounts(ctx, "other")
	if len(mounts) != 1 || len(others) != 0 || mounts[0].Kind != "page" {
		t.Fatal("mount scope incorrect")
	}
	revisions, _ := st.ListStudioItemRevisions(ctx, created.ItemID)
	if len(revisions) != 2 || revisions[0].Author.SessionID != "writer" || revisions[0].Author.TurnID != "turn" {
		t.Fatal("wrong version authors", revisions)
	}
	doc, _ := st.GetPage(ctx, created.ItemID)
	if doc.Body != "甲\n\nAI 修改" {
		t.Fatal(doc.Body)
	}
	lease := run(tool.PageEdit, "begin", map[string]any{"item_id": created.ItemID, "action": "begin", "expected_hash": doc.ContentHash, "target": "AI 修改"})
	var op store.PageEditOperation
	if err := json.Unmarshal([]byte(lease.Content), &op); err != nil || op.LeaseToken == "" {
		t.Fatal("missing lease", lease.Content, err)
	}
	run(tool.PageEdit, "renew", map[string]any{"item_id": created.ItemID, "action": "renew", "operation_id": op.ID, "lease_token": op.LeaseToken})
	run(tool.PageEdit, "commit", map[string]any{"item_id": created.ItemID, "action": "commit", "operation_id": op.ID, "lease_token": op.LeaseToken, "replacement": "AI 完成🙂"})
	doc, _ = st.GetPage(ctx, created.ItemID)
	if doc.Body != "甲\n\nAI 完成🙂" {
		t.Fatal("leased tool commit failed", doc.Body)
	}
	// Appending Chinese/emoji text must use the authoritative UTF-8 endpoint,
	// including when the model only reads a small page of Unicode characters.
	read = run(tool.PageRead, "read-end", map[string]any{"item_id": created.ItemID, "limit": 1})
	var endpoint struct {
		BodyBytes   int    `json:"bodyBytes"`
		ContentHash string `json:"contentHash"`
	}
	if err := json.Unmarshal([]byte(read.Content), &endpoint); err != nil || endpoint.BodyBytes != len(doc.Body) {
		t.Fatal("full byte length missing from paginated read", read.Content, err)
	}
	lease = run(tool.PageEdit, "append-begin", map[string]any{"item_id": created.ItemID, "action": "begin", "expected_hash": endpoint.ContentHash, "from": endpoint.BodyBytes, "to": endpoint.BodyBytes})
	if err := json.Unmarshal([]byte(lease.Content), &op); err != nil || op.From != len(doc.Body) || op.To != op.From {
		t.Fatal("append must reserve only the endpoint", lease.Content, err)
	}
	appended := run(tool.PageEdit, "append-commit", map[string]any{"item_id": created.ItemID, "action": "commit", "operation_id": op.ID, "lease_token": op.LeaseToken, "replacement": "\n\n新增🙂"})
	final, _ := st.GetPage(ctx, created.ItemID)
	if err := json.Unmarshal([]byte(appended.Content), &endpoint); err != nil || final.Body != doc.Body+"\n\n新增🙂" || endpoint.BodyBytes != len(final.Body) {
		t.Fatal("append result must preserve the prefix and return its new byte length", appended.Content, err)
	}
}

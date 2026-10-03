package engine

import (
	"context"
	"encoding/json"
	"github.com/teatak/pudding-core/internal/event"
	"github.com/teatak/pudding-core/internal/plugin"
	"github.com/teatak/pudding-core/internal/store"
	"github.com/teatak/pudding-core/internal/store/storetest"
	"github.com/teatak/pudding-core/internal/tool"
	"strings"
	"testing"
)

func TestTableChatToolsAndCSVWithoutDesktop(t *testing.T) {
	ctx := context.Background()
	st := storetest.New(t)
	eng := New(st, event.NewHub(), nil, st, WithPlugins(plugin.NewService(t.TempDir(), nil)))
	if err := st.CreateSession(ctx, &store.Session{ID: "writer", Provider: "mock", Model: "mock", LoadedPluginIDs: []string{plugin.BuiltinStudioID}}); err != nil {
		t.Fatal(err)
	}
	if _, err := st.BeginTurn(ctx, store.BeginTurnInput{SessionID: "writer", TurnID: "turn", UserMessageID: "message", ClientMessageID: "input", UserText: "table"}); err != nil {
		t.Fatal(err)
	}
	run := func(name, id string, args any) tool.Result {
		t.Helper()
		raw, _ := json.Marshal(args)
		result := eng.executeAllowedTool(ctx, "writer", "turn", store.ModeChat, tool.Call{Name: name, CallID: id, Args: raw})
		if !result.Ok {
			t.Fatal(name, result.Content)
		}
		return result
	}
	args := map[string]any{"name": "Data", "table": map[string]any{"columns": []any{map[string]any{"id": "c", "name": "Text", "type": "text"}}, "rows": []any{map[string]any{"id": "r1", "cells": map[string]any{"c": "One"}}, map[string]any{"id": "r2", "cells": map[string]any{"c": "Two"}}}}}
	created := run(tool.TableCreate, "create", args)
	run(tool.TableCreate, "create", args)
	var result struct {
		ItemID string `json:"itemID"`
	}
	json.Unmarshal([]byte(created.Content), &result)
	items, _ := st.ListStudioItems(ctx)
	if len(items) != 1 {
		t.Fatal("duplicate create")
	}
	read := run(tool.TableRead, "read", map[string]any{"item_id": result.ItemID, "limit": 1})
	if !strings.Contains(read.Content, `"nextOffset":1`) {
		t.Fatal(read.Content)
	}
	run(tool.TableUpdate, "edit", map[string]any{"item_id": result.ItemID, "operations": []any{map[string]any{"kind": "set_cell", "rowID": "r2", "columnID": "c", "value": "中文,\"text\"", "expected": "Two"}}})
	csv := run(tool.TableRead, "csv", map[string]any{"item_id": result.ItemID, "format": "csv", "row_ids": []string{"r2"}})
	if !strings.Contains(csv.Content, `"rowIDs":["r2"]`) || !strings.Contains(csv.Content, `"csv":`) {
		t.Fatal(csv.Content)
	}
	revs, _ := st.ListStudioItemRevisions(ctx, result.ItemID)
	if len(revs) != 2 || revs[0].Author.SessionID != "writer" || revs[0].Author.TurnID != "turn" {
		t.Fatal("author missing")
	}
	mounts, _ := st.ListStudioMounts(ctx, "writer")
	if len(mounts) != 1 || mounts[0].Kind != "table" {
		t.Fatal("table not mounted")
	}
}

func TestTableReadSelectedRowsPagination(t *testing.T) {
	table := &store.TableContent{Body: store.TableBody{Columns: []store.TableColumn{}, Rows: []store.TableRow{{ID: "a", Cells: map[string]any{}}, {ID: "b", Cells: map[string]any{}}}}}
	first, err := readTableRange(table, tool.StudioArgs{RowIDs: []string{"a", "b"}, Limit: 1})
	if err != nil {
		t.Fatal(err)
	}
	offset := first.(map[string]any)["nextOffset"].(int)
	next, err := readTableRange(table, tool.StudioArgs{RowIDs: []string{"a", "b"}, Limit: 1, Offset: offset})
	if err != nil {
		t.Fatal(err)
	}
	if next.(map[string]any)["rowIDs"].([]string)[0] != "b" {
		t.Fatal(next)
	}
}

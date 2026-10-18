package engine

import (
	"context"
	"encoding/json"
	"errors"

	"github.com/teatak/pudding-core/internal/store"
	"github.com/teatak/pudding-core/internal/tool"
)

func (e *Engine) executeStudio(ctx context.Context, sessionID, turnID string, call tool.Call) tool.Result {
	out := tool.Result{CallID: call.CallID, Name: call.Name}
	respond := func(value any, err error) tool.Result {
		if err != nil {
			value = map[string]any{"error": err.Error()}
			var locked *store.NoteLocked
			if errors.As(err, &locked) {
				value = map[string]any{"error": "document_locked", "operationID": locked.OperationID}
			}
			var cellConflict *store.SheetCellConflict
			if errors.As(err, &cellConflict) {
				value = map[string]any{"error": "cell_conflict", "rowID": cellConflict.RowID, "columnID": cellConflict.ColumnID}
			}
			var conflict *store.ContentConflict
			if errors.As(err, &conflict) {
				value = map[string]any{"error": "content_conflict", "currentHash": conflict.CurrentHash}
			}
		}
		data, _ := json.Marshal(value)
		out.Content = string(data)
		out.Ok = err == nil
		return out
	}
	args, err := tool.DecodeStudioArgs(call.Args)
	if err != nil {
		return respond(nil, err)
	}
	if call.Name != tool.ArtifactList && call.Name != tool.PageCreate && call.Name != tool.NoteCreate && call.Name != tool.SheetCreate && args.ItemID == "" {
		return respond(nil, errors.New("item_id is required"))
	}
	switch call.Name {
	case tool.SheetCreate, tool.SheetRead, tool.SheetEdit:
		result, err := e.executeSheet(ctx, sessionID, turnID, call, args)
		return respond(result, err)
	case tool.ArtifactList:
		kind := args.Kind
		switch kind {
		case "table":
			kind = store.StudioItemKindSheet
		case "doc", "note":
			kind = store.StudioItemKindPage
		}
		items, err := e.store.ListStudioItems(ctx, store.StudioItemsActive)
		if err != nil {
			return respond(nil, err)
		}
		filtered := []map[string]any{}
		for _, item := range items {
			if kind == "" || kind == item.Kind {
				filtered = append(filtered, map[string]any{"id": item.ID, "kind": item.Kind, "name": item.Name, "updatedAt": item.UpdatedAt})
			}
		}
		return respond(map[string]any{"items": filtered}, nil)
	case tool.ArtifactOpen:
		mount, err := e.store.OpenStudioItem(ctx, sessionID, args.ItemID, store.NewID("mount"))
		return respond(mount, err)
	case tool.PageCreate, tool.PageRead, tool.PageEdit, tool.PageRename:
		value, err := e.executeNote(ctx, sessionID, turnID, call, args)
		return respond(value, err)
	}
	return respond(nil, errors.New("unknown Studio tool"))
}

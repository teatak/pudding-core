package engine

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/teatak/pudding-core/contracts"
	"github.com/teatak/pudding-core/internal/store"
	"github.com/teatak/pudding-core/internal/tool"
)

func (e *Engine) executeStudio(ctx context.Context, sessionID, turnID string, call tool.Call) tool.Result {
	out := tool.Result{CallID: call.CallID, Name: call.Name}
	respond := func(value any, err error) tool.Result {
		if err != nil {
			value = map[string]any{"error": err.Error()}
			var cellConflict *store.TableCellConflict
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
	if call.Name != tool.ArtifactList && call.Name != tool.DocCreate && call.Name != tool.TableCreate && args.ItemID == "" {
		return respond(nil, errors.New("item_id is required"))
	}
	author := store.ContentAuthor{Kind: "session", SessionID: sessionID, TurnID: turnID}
	switch call.Name {
	case tool.TableCreate, tool.TableRead, tool.TableUpdate:
		result, err := e.executeTable(ctx, sessionID, turnID, call, args)
		return respond(result, err)
	case tool.ArtifactList:
		items, err := e.store.ListStudioItems(ctx, store.StudioItemsActive)
		if err != nil {
			return respond(nil, err)
		}
		filtered := []map[string]any{}
		for _, item := range items {
			if args.Kind == "" || args.Kind == item.Kind {
				filtered = append(filtered, map[string]any{"id": item.ID, "kind": item.Kind, "name": item.Name, "updatedAt": item.UpdatedAt})
			}
		}
		return respond(map[string]any{"items": filtered}, nil)
	case tool.ArtifactOpen:
		mount, err := e.store.OpenStudioItem(ctx, sessionID, args.ItemID, store.NewID("mount"))
		return respond(mount, err)
	case tool.DocCreate:
		name := strings.TrimSpace(args.Name)
		if name == "" || len(name) > 200 || args.Body == nil {
			return respond(nil, errors.New("name (max 200 bytes) and body are required"))
		}
		// A retried call reuses the item created by this exact canonical tool call.
		id := "doc_" + store.DocumentHash(sessionID + "\x00" + turnID + "\x00" + call.CallID)[:32]
		doc, err := e.store.GetDocument(ctx, id)
		if errors.Is(err, store.ErrNotFound) {
			now := time.Now().UTC()
			doc, err = e.store.CreateDocument(ctx, &store.StudioItem{ID: id, Kind: store.StudioItemKindDoc, Name: name, Icon: "file-text", SourceSessionID: sessionID, CreatedAt: now, UpdatedAt: now}, *args.Body, author)
		}
		if err != nil {
			return respond(nil, err)
		}
		mount, err := e.store.OpenStudioItem(ctx, sessionID, id, store.NewID("mount"))
		if err != nil {
			return respond(nil, err)
		}
		return respond(map[string]any{"itemID": id, "mountID": mount.ID, "contentHash": doc.ContentHash, "revisionID": doc.RevisionID}, nil)
	case tool.DocRead:
		doc, err := e.store.GetDocument(ctx, args.ItemID)
		if err != nil {
			return respond(nil, err)
		}
		if args.Limit == 0 {
			args.Limit = 16000
		}
		if args.Offset < 0 || args.Limit < 1 || args.Limit > contracts.Studio().MaxReadBytes/utf8.UTFMax {
			return respond(nil, errors.New("invalid document read range"))
		}
		chars := []rune(doc.Body)
		start := min(args.Offset, len(chars))
		end := min(start+args.Limit, len(chars))
		result := map[string]any{"itemID": doc.ItemID, "body": string(chars[start:end]), "contentHash": doc.ContentHash, "revisionID": doc.RevisionID, "offset": start, "totalCharacters": len(chars)}
		if end < len(chars) {
			result["nextOffset"] = end
		}
		return respond(result, nil)
	case tool.DocEdit:
		doc, err := e.store.WriteDocument(ctx, args.ItemID, store.DocumentWrite{ClientRequestID: turnID + "/" + call.CallID, ExpectedHash: args.ExpectedHash, Body: args.Body, Edits: args.Edits, Author: author})
		if err != nil {
			return respond(nil, err)
		}
		return respond(map[string]any{"itemID": doc.ItemID, "contentHash": doc.ContentHash, "revisionID": doc.RevisionID}, nil)
	}
	return respond(nil, errors.New("unknown Studio tool"))
}

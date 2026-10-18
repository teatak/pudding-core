package engine

import (
	"context"
	"errors"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/teatak/pudding-core/contracts"
	"github.com/teatak/pudding-core/internal/store"
	"github.com/teatak/pudding-core/internal/tool"
)

func (e *Engine) executeNote(ctx context.Context, sessionID, turnID string, call tool.Call, args tool.StudioArgs) (any, error) {
	author := store.ContentAuthor{Kind: "session", SessionID: sessionID, TurnID: turnID}
	switch call.Name {
	case tool.NoteCreate:
		name := strings.TrimSpace(args.Name)
		if name == "" || len(name) > 200 || args.Body == nil {
			return nil, errors.New("name (max 200 bytes) and body are required")
		}
		// A retried call reuses the item created by this exact canonical tool call.
		id := "page_" + store.ContentHash(sessionID + "\x00" + turnID + "\x00" + call.CallID)[:32]
		note, err := e.store.GetNote(ctx, id)
		if errors.Is(err, store.ErrNotFound) {
			now := time.Now().UTC()
			note, err = e.store.CreateNote(ctx, &store.StudioItem{ID: id, Kind: store.StudioItemKindPage, Name: name, Icon: "file-text", SourceSessionID: sessionID, CreatedAt: now, UpdatedAt: now}, *args.Body, author)
		}
		if err != nil {
			return nil, err
		}
		mount, err := e.store.OpenStudioItem(ctx, sessionID, id, store.NewID("mount"))
		if err != nil {
			return nil, err
		}
		return map[string]any{"itemID": id, "mountID": mount.ID, "contentHash": note.ContentHash, "revisionID": note.RevisionID}, nil
	case tool.NoteRead:
		note, err := e.store.GetNote(ctx, args.ItemID)
		if err != nil {
			return nil, err
		}
		if args.Limit == 0 {
			args.Limit = 16000
		}
		if args.Offset < 0 || args.Limit < 1 || args.Limit > contracts.Studio().MaxReadBytes/utf8.UTFMax {
			return nil, errors.New("invalid document read range")
		}
		chars := []rune(note.Body)
		start := min(args.Offset, len(chars))
		end := min(start+args.Limit, len(chars))
		result := map[string]any{"itemID": note.ItemID, "body": string(chars[start:end]), "contentHash": note.ContentHash, "revisionID": note.RevisionID, "offset": start, "totalCharacters": len(chars)}
		if end < len(chars) {
			result["nextOffset"] = end
		}
		return result, nil
	case tool.NoteEdit:
		if args.Action == "begin" || args.Action == "renew" || args.Action == "cancel" {
			hash := ""
			if args.ExpectedHash != nil {
				hash = *args.ExpectedHash
			}
			op, err := e.store.ChangeNoteEdit(ctx, args.ItemID, store.NoteEditRequest{Action: args.Action, ClientRequestID: turnID + "/" + call.CallID, OperationID: args.OperationID, LeaseToken: args.LeaseToken, ExpectedHash: hash, From: args.From, To: args.To, Target: args.Target, Author: author})
			return op, err
		}
		if args.Action != "" && args.Action != "commit" {
			return nil, errors.New("invalid document edit action")
		}
		if args.Action == "commit" && args.Replacement == nil {
			return nil, errors.New("commit requires replacement")
		}
		note, err := e.store.WriteNote(ctx, args.ItemID, store.NoteWrite{ClientRequestID: turnID + "/" + call.CallID, ExpectedHash: args.ExpectedHash, Body: args.Body, Edits: args.Edits, Author: author, OperationID: args.OperationID, LeaseToken: args.LeaseToken, Replacement: args.Replacement})
		if err != nil {
			return nil, err
		}
		return map[string]any{"itemID": note.ItemID, "contentHash": note.ContentHash, "revisionID": note.RevisionID}, nil
	case tool.NoteRename:
		name := strings.TrimSpace(args.Name)
		if name == "" || len(name) > 200 {
			return nil, errors.New("name is required (max 200 bytes)")
		}
		item, err := e.store.GetStudioItem(ctx, args.ItemID)
		if err != nil {
			return nil, err
		}
		if item.Kind != store.StudioItemKindPage {
			return nil, errors.New("item is not a page")
		}
		item.Name = name
		updated, err := e.store.UpdateStudioItem(ctx, item, item.Revision)
		if err != nil {
			return nil, err
		}
		return map[string]any{"itemID": updated.ID, "name": updated.Name, "revision": updated.Revision}, nil
	}
	return nil, errors.New("unknown page tool")
}

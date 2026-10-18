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

// Current model calls must reserve a range before generating text. Prompt-only
// guidance is insufficient: existing conversations may repeat one-shot writes
// from their history, bypassing the caret, cancellation and streaming events.
// Check the original name after authorization, before alias normalization loses
// that distinction. Released doc/note calls retain their legacy payloads.
func (e *Engine) checkPageAuthoringCall(ctx context.Context, sessionID, turnID, originalName string, call tool.Call) *tool.Result {
	if originalName != tool.PageCreate && originalName != tool.PageEdit {
		return nil
	}
	args, err := tool.DecodeStudioArgs(call.Args)
	if err != nil {
		return nil // The regular handler reports malformed arguments.
	}
	message := ""
	if originalName == tool.PageEdit && (args.Action == "" || args.Body != nil || args.Edits != nil) {
		message = "No content was changed. page_edit requires action=begin before action=commit; body and edits are no longer accepted. Read the current page, begin with expected_hash and the unique target (or ranges for disjoint passages), then commit with operation_id, lease_token and replacement/replacements. For an empty page use from=0,to=0; for an append use from=to=bodyBytes; for an entire rewrite use from=0,to=bodyBytes. Do not repeat one-shot writes from earlier conversation history."
	}
	if originalName == tool.PageCreate && args.Body != nil && *args.Body != "" {
		// Preserve an idempotent retry of a creation already saved by an older
		// version, including a retry under the canonical name of a legacy call.
		id := "page_" + store.ContentHash(sessionID + "\x00" + turnID + "\x00" + call.CallID)[:32]
		if _, err := e.store.GetPage(ctx, id); err == nil {
			return nil
		}
		message = "No page was created. Create with body set to an empty string, then begin an edit with the returned itemID/contentHash and from=0,to=0; write the content through commit replacement. This keeps first-draft writing visible and cancellable."
	}
	if message == "" {
		return nil
	}
	content, _ := json.Marshal(map[string]string{"error": "page_edit_requires_begin", "message": message})
	return &tool.Result{CallID: call.CallID, Name: call.Name, Ok: false, Content: string(content)}
}

func (e *Engine) executePage(ctx context.Context, sessionID, turnID string, call tool.Call, args tool.StudioArgs) (any, error) {
	author := store.ContentAuthor{Kind: "session", SessionID: sessionID, TurnID: turnID}
	switch call.Name {
	case tool.PageCreate:
		name := strings.TrimSpace(args.Name)
		if name == "" || len(name) > 200 || args.Body == nil {
			return nil, errors.New("name (max 200 bytes) and body are required")
		}
		// A retried call reuses the item created by this exact canonical tool call.
		id := "page_" + store.ContentHash(sessionID + "\x00" + turnID + "\x00" + call.CallID)[:32]
		page, err := e.store.GetPage(ctx, id)
		if errors.Is(err, store.ErrNotFound) {
			now := time.Now().UTC()
			page, err = e.store.CreatePage(ctx, &store.StudioItem{ID: id, Kind: store.StudioItemKindPage, Name: name, Icon: "file-text", SourceSessionID: sessionID, CreatedAt: now, UpdatedAt: now}, *args.Body, author)
		}
		if err != nil {
			return nil, err
		}
		mount, err := e.store.OpenStudioItem(ctx, sessionID, id, store.NewID("mount"))
		if err != nil {
			return nil, err
		}
		return map[string]any{"itemID": id, "mountID": mount.ID, "contentHash": page.ContentHash, "revisionID": page.RevisionID, "bodyBytes": len(page.Body)}, nil
	case tool.PageRead:
		page, err := e.store.GetPage(ctx, args.ItemID)
		if err != nil {
			return nil, err
		}
		if args.Limit == 0 {
			args.Limit = 16000
		}
		if args.Offset < 0 || args.Limit < 1 || args.Limit > contracts.Studio().MaxReadBytes/utf8.UTFMax {
			return nil, errors.New("invalid document read range")
		}
		chars := []rune(page.Body)
		start := min(args.Offset, len(chars))
		end := min(start+args.Limit, len(chars))
		result := map[string]any{"itemID": page.ItemID, "body": string(chars[start:end]), "contentHash": page.ContentHash, "revisionID": page.RevisionID, "offset": start, "totalCharacters": len(chars), "bodyBytes": len(page.Body)}
		structure := page.Structure
		if structure == nil {
			structure = store.LegacyPageDocument(page.ItemID, page.Body)
		}
		blockInfo := []map[string]any{}
		byteStart, byteEnd := len(string(chars[:start])), len(string(chars[:end]))
		for i, h := range structure.Hints() {
			if h.To > byteStart && h.From < byteEnd {
				b := structure.Blocks[i]
				blockInfo = append(blockInfo, map[string]any{"id": b.ID, "type": b.Type, "version": b.Version, "from": h.From, "to": h.To, "complete": h.From >= byteStart && h.To <= byteEnd})
			}
		}
		result["blocks"] = blockInfo
		if end < len(chars) {
			result["nextOffset"] = end
		}
		return result, nil
	case tool.PageEdit:
		if args.Action == "begin" || args.Action == "renew" || args.Action == "cancel" {
			hash := ""
			if args.ExpectedHash != nil {
				hash = *args.ExpectedHash
			}
			op, err := e.store.ChangePageEdit(ctx, args.ItemID, store.PageEditRequest{Action: args.Action, ClientRequestID: turnID + "/" + call.CallID, OperationID: args.OperationID, LeaseToken: args.LeaseToken, ExpectedHash: hash, From: args.From, To: args.To, Target: args.Target, Ranges: args.Ranges, Blocks: args.Blocks, Author: author})
			return op, err
		}
		if args.Action != "" && args.Action != "commit" {
			return nil, errors.New("invalid document edit action")
		}
		if args.Action == "commit" && args.Replacement == nil && args.Replacements == nil {
			return nil, errors.New("commit requires replacement")
		}
		page, err := e.store.WritePage(ctx, args.ItemID, store.PageWrite{ClientRequestID: turnID + "/" + call.CallID, ExpectedHash: args.ExpectedHash, Body: args.Body, Edits: args.Edits, Author: author, OperationID: args.OperationID, LeaseToken: args.LeaseToken, Replacement: args.Replacement, Replacements: args.Replacements})
		if err != nil {
			return nil, err
		}
		result := map[string]any{"itemID": page.ItemID, "contentHash": page.ContentHash, "revisionID": page.RevisionID, "bodyBytes": len(page.Body)}
		if args.Action == "commit" {
			// Deliver final anchors with the success event, before the renderer
			// refreshes the body. This prevents a fast commit from flashing the
			// entire replacement while its typewriter presentation catches up.
			if snapshot, snapshotErr := e.store.GetPageEditState(ctx, args.ItemID); snapshotErr == nil {
				result["editState"] = snapshot
			}
		}
		return result, nil
	case tool.PageRename:
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

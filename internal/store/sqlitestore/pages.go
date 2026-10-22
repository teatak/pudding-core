package sqlitestore

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"strings"
	"time"

	"github.com/teatak/pudding-core/internal/store"
)

const pageSelect = `SELECT w.id,c.body,c.content_hash,w.head_revision,c.content_format FROM studio_items w JOIN studio_item_content c ON c.item_id=w.id WHERE w.id=? AND w.kind IN ('page','note') AND w.deleted=0 AND w.archived_at=0`

func (s *Store) GetPage(ctx context.Context, id string) (*store.PageContent, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return scanContentSnapshot(s.db.QueryRowContext(ctx, pageSelect, id))
}

func (s *Store) CreatePage(ctx context.Context, item *store.StudioItem, body string, author store.ContentAuthor, requestID ...string) (*store.PageContent, error) {
	if err := store.ValidatePage(body); err != nil {
		return nil, err
	}
	if item.Kind != store.StudioItemKindPage && item.Kind != "note" {
		return nil, &store.InvalidContent{Message: "document kind required"}
	}
	if item.Kind == "note" {
		item.Kind = store.StudioItemKindPage
	}
	if item.NameMode == "" {
		item.NameMode = store.PageNameAuto
	}
	if item.NameMode != store.PageNameAuto && item.NameMode != store.PageNameManual {
		return nil, &store.InvalidContent{Message: "invalid name mode"}
	}
	if item.NameFallback == "" {
		item.NameFallback = store.PageNameFallback
	}
	requestHash, err := prepareStudioCreation(item, body, author, requestID)
	if err != nil {
		return nil, err
	}
	var out *store.PageContent
	err = s.tx(ctx, func(ctx context.Context, tx *sql.Tx) error {
		if err := validateContentAuthor(ctx, tx, author); err != nil {
			return err
		}
		if requestHash != "" {
			var err error
			out, err = replayNativeCreation(ctx, tx, item.ID, requestHash)
			if err != nil || out != nil {
				return err
			}
		}
		if err := createStudioItemTx(ctx, tx, item); err != nil {
			return err
		}
		hash := store.ContentHash(body)
		if _, err := tx.ExecContext(ctx, `INSERT INTO studio_item_content(item_id,body,content_hash) VALUES(?,?,?)`, item.ID, body, hash); err != nil {
			return err
		}
		out = &store.PageContent{ItemID: item.ID, Body: body, ContentHash: hash, Structure: store.LegacyPageDocument(item.ID, body)}
		return saveNativeContentTx(ctx, tx, out, nativeContentSave{ClientRequestID: "create", Author: author}, requestHash)
	})
	return out, err
}

func (s *Store) WritePage(ctx context.Context, id string, in store.PageWrite) (*store.PageContent, error) {
	if in.ClientRequestID == "" || len(in.ClientRequestID) > 200 {
		return nil, &store.InvalidContent{Message: "clientRequestID is required (max 200 bytes)"}
	}
	modes := 0
	if in.Body != nil {
		modes++
	}
	if len(in.Edits) > 0 {
		modes++
	}
	if in.RestoreRevision != "" {
		modes++
	}
	if in.UndoRevision != "" {
		modes++
	}
	if in.Replacement != nil {
		modes++
	}
	if in.Replacements != nil {
		modes++
	}
	leased := in.Replacement != nil || in.Replacements != nil
	if leased != (in.OperationID != "" && in.LeaseToken != "") || (!leased && (in.OperationID != "" || in.LeaseToken != "")) {
		return nil, &store.InvalidContent{Message: "replacement requires operationID and leaseToken together"}
	}
	if in.UndoRevision != "" && in.Author.Kind != "user" {
		return nil, &store.InvalidContent{Message: "undo is a user operation"}
	}
	if modes != 1 || (in.PreserveOnly && (in.Body == nil || in.Author.Kind != "user")) {
		return nil, &store.InvalidContent{Message: "provide exactly one of body, edits, restoreRevision, undoRevision or replacement"}
	}
	if (in.Body != nil || in.RestoreRevision != "") && in.ExpectedHash == nil {
		return nil, &store.InvalidContent{Message: "expectedHash is required for replacement or restore"}
	}
	payload, _ := json.Marshal(struct {
		Write  store.PageWrite
		Author store.ContentAuthor
	}{in, in.Author})
	requestHash := store.ContentHash(string(payload))
	var out *store.PageContent
	err := s.tx(ctx, func(ctx context.Context, tx *sql.Tx) error {
		if err := validateContentAuthor(ctx, tx, in.Author); err != nil {
			return err
		}
		current, err := scanContentSnapshot(tx.QueryRowContext(ctx, pageSelect, id))
		if err != nil {
			return err
		}
		var savedHash, savedRevision string
		err = tx.QueryRowContext(ctx, `SELECT request_hash,hash FROM studio_item_saves WHERE item_id=? AND client_request_id=?`, id, in.ClientRequestID).Scan(&savedHash, &savedRevision)
		if err == nil {
			if savedHash != requestHash {
				return &store.InvalidContent{Message: "clientRequestID reused with different content"}
			}
			if in.PreserveOnly {
				out = current
				return nil
			}
			out, err = scanContentSnapshot(tx.QueryRowContext(ctx, `SELECT item_id,content,content_hash,hash,content_format FROM studio_item_revisions WHERE item_id=? AND hash=?`, id, savedRevision))
			return err
		}
		if !errors.Is(err, sql.ErrNoRows) {
			return err
		}
		if !in.PreserveOnly && in.ExpectedHash != nil && *in.ExpectedHash != current.ContentHash {
			return &store.ContentConflict{CurrentHash: current.ContentHash}
		}
		body := current.Body
		var undoStructure *store.PageDocument
		var operation *store.PageEditOperation
		if leased {
			if in.Author.Kind == "session" {
				turn, err := getTurnTx(ctx, tx, in.Author.TurnID)
				if err != nil {
					return err
				}
				if turn.Status != store.TurnRunning {
					return &store.InvalidContent{Message: "editing turn has ended"}
				}
			}
			operation, err = loadPageEditTx(ctx, tx, id, in.OperationID)
			if err != nil {
				return err
			}
			if err = checkPageEditCommit(operation, in, body, time.Now().UnixMilli()); err != nil {
				return err
			}
			ranges := append([]store.PageEditRange(nil), operation.EditRanges()...)
			originalRanges := append([]store.PageEditRange(nil), ranges...)
			replacements := in.Replacements
			if in.Replacement != nil {
				replacements = []string{*in.Replacement}
			}
			if len(replacements) != len(ranges) {
				return &store.InvalidContent{Message: "one replacement per reserved range is required"}
			}
			var result strings.Builder
			cursor, shift := 0, 0
			for i, r := range ranges {
				result.WriteString(body[cursor:r.From])
				result.WriteString(replacements[i])
				cursor = r.To
				ranges[i].From = r.From + shift
				ranges[i].To = ranges[i].From + len(replacements[i])
				ranges[i].Target = ""
				shift += len(replacements[i]) - (r.To - r.From)
			}
			result.WriteString(body[cursor:])
			body = result.String()
			previous := current.Structure
			if previous == nil {
				previous = store.LegacyPageDocument(id, current.Body)
			}
			in.BlockHints = store.PageIdentityHintsAfterEdits(previous, body, originalRanges, replacements)
			operation.SetRanges(ranges)
		}
		if in.Body != nil {
			body = *in.Body
		}
		if len(in.Edits) > 0 {
			body, err = store.ApplyPageEdits(body, in.Edits)
			if err != nil {
				return err
			}
		}
		if in.RestoreRevision != "" {
			var content sql.NullString
			var format string
			if err := tx.QueryRowContext(ctx, `SELECT content,content_format FROM studio_item_revisions WHERE item_id=? AND hash=?`, id, in.RestoreRevision).Scan(&content, &format); err != nil {
				if errors.Is(err, sql.ErrNoRows) {
					return store.ErrNotFound
				}
				return err
			}
			if !content.Valid {
				return &store.InvalidContent{Message: "revision has no document content"}
			}
			var restored *store.PageDocument
			body, restored, err = store.DecodeStoredContent(id, content.String, format)
			if err != nil {
				return err
			}
			if restored != nil {
				in.BlockHints = restored.Hints()
			}
		}

		if in.UndoRevision != "" {
			before, after, err := nativeUndoSourceTx(ctx, tx, id, current.RevisionID, in.UndoRevision)
			if err == nil {
				var rawBefore, formatBefore, rawAfter, formatAfter string
				err = tx.QueryRowContext(ctx, `SELECT b.content,b.content_format,a.content,a.content_format FROM studio_item_revisions a JOIN studio_item_revisions b ON b.item_id=a.item_id AND b.hash=a.parent_revision WHERE a.item_id=? AND a.hash=?`, id, in.UndoRevision).Scan(&rawBefore, &formatBefore, &rawAfter, &formatAfter)
				if err == nil && formatAfter == store.PageDocumentFormat {
					_, beforeDoc, decodeErr := store.DecodeStoredContent(id, rawBefore, formatBefore)
					err = decodeErr
					if beforeDoc == nil && err == nil {
						beforeDoc = store.LegacyPageDocument(id, before)
					}
					_, afterDoc, decodeErr := store.DecodeStoredContent(id, rawAfter, formatAfter)
					if err == nil {
						err = decodeErr
					}
					if err == nil {
						undoStructure, err = store.UndoPageDocument(beforeDoc, afterDoc, current.Structure)
						if err == nil {
							body = undoStructure.Markdown()
							in.BlockHints = undoStructure.Hints()
						}
					}
				} else if err == nil {
					body, err = store.UndoPage(before, after, current.Body)
				}
			}
			if errors.Is(err, store.ErrUndoConflict) {
				return &store.ContentConflict{CurrentHash: current.ContentHash}
			}
			if err != nil {
				return err
			}
		}
		if err := store.ValidatePage(body); err != nil {
			return err
		}
		if !in.PreserveOnly {
			if err := guardPageEditsTx(ctx, tx, id, current.Body, body, in.OperationID, time.Now().UnixMilli()); err != nil {
				return err
			}
		}
		previous := current.Structure
		if previous == nil {
			previous = store.LegacyPageDocument(id, current.Body)
		}
		structure, err := store.ReconcilePageDocument(body, previous, in.BlockHints)
		if err != nil {
			return err
		}

		out = &store.PageContent{ItemID: id, Body: body, ContentHash: store.ContentHash(body), RevisionID: current.RevisionID, Structure: structure}
		if err := saveNativeContentTx(ctx, tx, out, nativeContentSave{ClientRequestID: in.ClientRequestID, Author: in.Author, PreserveOnly: in.PreserveOnly}, requestHash); err != nil {
			return err
		}
		if in.PreserveOnly {
			out = current
		}
		if operation != nil {
			operation.State = "committed"
			operation.ContentHash = out.ContentHash
			operation.RevisionID = out.RevisionID
			operation.UpdatedAt = time.Now().UnixMilli()
			operation.Sequence++
			if err := savePageEditTx(ctx, tx, operation); err != nil {
				return err
			}
		}
		return nil
	})
	return out, err
}

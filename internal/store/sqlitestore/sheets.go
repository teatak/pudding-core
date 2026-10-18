package sqlitestore

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"

	"github.com/teatak/pudding-core/internal/store"
)

const sheetSelect = `SELECT w.id,c.body,c.content_hash,w.head_revision,c.content_format FROM studio_items w JOIN studio_item_content c ON c.item_id=w.id WHERE w.id=? AND w.kind='sheet' AND w.deleted=0 AND w.archived_at=0`

func sheetFromContent(d *store.ContentSnapshot) (*store.SheetContent, error) {
	var body store.SheetBody
	if err := json.Unmarshal([]byte(d.Body), &body); err != nil {
		return nil, err
	}
	return &store.SheetContent{ItemID: d.ItemID, Body: body, ContentHash: d.ContentHash, RevisionID: d.RevisionID}, nil
}
func (s *Store) GetSheet(ctx context.Context, id string) (*store.SheetContent, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	d, err := scanContentSnapshot(s.db.QueryRowContext(ctx, sheetSelect, id))
	if err != nil {
		return nil, err
	}
	return sheetFromContent(d)
}
func recordSheetIDs(ctx context.Context, tx *sql.Tx, id string, body store.SheetBody, existing *store.SheetBody, restore bool, operations []store.SheetOperation) error {
	previous := map[string]bool{}
	if existing != nil {
		for _, r := range existing.Rows {
			previous["row/"+r.ID] = true
		}
		for _, c := range existing.Columns {
			previous["column/"+c.ID] = true
		}
	}
	recorded := map[string]bool{}
	record := func(kind, key string) error {
		identity := kind + "/" + key
		if recorded[identity] {
			return nil
		}
		recorded[identity] = true
		if previous[identity] {
			return nil
		}
		var used int
		err := tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM studio_sheet_ids WHERE item_id=? AND entity_kind=? AND entity_id=?`, id, kind, key).Scan(&used)
		if err != nil {
			return err
		}
		if used > 0 {
			if restore {
				return nil
			}
			return &store.InvalidSheet{Message: "deleted " + kind + " ID cannot be reused: " + key}
		}
		_, err = tx.ExecContext(ctx, `INSERT INTO studio_sheet_ids(item_id,entity_kind,entity_id) VALUES(?,?,?)`, id, kind, key)
		return err
	}
	for _, r := range body.Rows {
		if err := record("row", r.ID); err != nil {
			return err
		}
	}
	for _, c := range body.Columns {
		if err := record("column", c.ID); err != nil {
			return err
		}
	}

	// Even entities added and removed in one atomic batch consume their IDs.
	for _, op := range operations {
		if op.Kind == "add_row" {
			if err := record("row", op.Row.ID); err != nil {
				return err
			}
		}
		if op.Kind == "add_column" {
			if err := record("column", op.Column.ID); err != nil {
				return err
			}
		}
	}
	return nil
}
func (s *Store) CreateSheet(ctx context.Context, item *store.StudioItem, body store.SheetBody, author store.ContentAuthor) (*store.SheetContent, error) {
	if err := store.ValidateSheet(body); err != nil {
		return nil, err
	}
	if item.Kind != store.StudioItemKindSheet {
		return nil, &store.InvalidSheet{Message: "table kind required"}
	}
	var d *store.ContentSnapshot
	err := s.tx(ctx, func(ctx context.Context, tx *sql.Tx) error {
		if err := validateContentAuthor(ctx, tx, author); err != nil {
			return err
		}
		if err := createStudioItemTx(ctx, tx, item); err != nil {
			return err
		}
		raw, _ := json.Marshal(body)
		d = &store.ContentSnapshot{ItemID: item.ID, Body: string(raw), ContentHash: store.ContentHash(string(raw))}
		if _, err := tx.ExecContext(ctx, `INSERT INTO studio_item_content(item_id,body,content_hash) VALUES(?,?,?)`, item.ID, d.Body, d.ContentHash); err != nil {
			return err
		}
		if err := recordSheetIDs(ctx, tx, item.ID, body, nil, false, nil); err != nil {
			return err
		}
		return saveNativeContentTx(ctx, tx, d, nativeContentSave{ClientRequestID: "create", Author: author}, "")
	})
	if err != nil {
		return nil, err
	}
	return sheetFromContent(d)
}
func (s *Store) WriteSheet(ctx context.Context, id string, in store.SheetWrite) (*store.SheetContent, error) {
	if in.ClientRequestID == "" || len(in.ClientRequestID) > 200 {
		return nil, &store.InvalidSheet{Message: "clientRequestID is required (max 200 bytes)"}
	}
	modes := 0
	if len(in.Operations) > 0 {
		modes++
	}
	if in.RestoreRevision != "" {
		modes++
	}
	if in.UndoRevision != "" {
		modes++
	}
	if modes != 1 || (in.RowID != "" && in.UndoRevision == "") {
		return nil, &store.InvalidSheet{Message: "provide operations, restoreRevision or undoRevision; rowID is only for undo"}
	}
	if in.UndoRevision != "" && in.Author.Kind != "user" {
		return nil, &store.InvalidSheet{Message: "undo is a user operation"}
	}
	if in.RestoreRevision != "" && in.ExpectedHash == nil {
		return nil, &store.InvalidSheet{Message: "expectedHash is required for restore"}
	}
	payload, _ := json.Marshal(struct {
		Write  store.SheetWrite
		Author store.ContentAuthor
	}{in, in.Author})
	requestHash := store.ContentHash(string(payload))
	var out *store.ContentSnapshot
	err := s.tx(ctx, func(ctx context.Context, tx *sql.Tx) error {
		if err := validateContentAuthor(ctx, tx, in.Author); err != nil {
			return err
		}
		current, err := scanContentSnapshot(tx.QueryRowContext(ctx, sheetSelect, id))
		if err != nil {
			return err
		}
		var savedHash, savedRevision string
		err = tx.QueryRowContext(ctx, `SELECT request_hash,hash FROM studio_item_saves WHERE item_id=? AND client_request_id=?`, id, in.ClientRequestID).Scan(&savedHash, &savedRevision)
		if err == nil {
			if savedHash != requestHash {
				return &store.InvalidSheet{Message: "clientRequestID reused with different operations"}
			}
			out, err = scanContentSnapshot(tx.QueryRowContext(ctx, `SELECT item_id,content,content_hash,hash,content_format FROM studio_item_revisions WHERE item_id=? AND hash=?`, id, savedRevision))
			return err
		}
		if !errors.Is(err, sql.ErrNoRows) {
			return err
		}
		if in.ExpectedHash != nil && *in.ExpectedHash != current.ContentHash {
			return &store.ContentConflict{CurrentHash: current.ContentHash}
		}
		before, err := sheetFromContent(current)
		if err != nil {
			return err
		}
		var body store.SheetBody
		if in.RestoreRevision != "" {
			var raw string
			if err := tx.QueryRowContext(ctx, `SELECT content FROM studio_item_revisions WHERE item_id=? AND hash=?`, id, in.RestoreRevision).Scan(&raw); err != nil {
				if errors.Is(err, sql.ErrNoRows) {
					return store.ErrNotFound
				}
				return err
			}
			if err := json.Unmarshal([]byte(raw), &body); err != nil {
				return err
			}

		} else if in.UndoRevision != "" {
			prior, changed, undoErr := nativeUndoSourceTx(ctx, tx, id, current.RevisionID, in.UndoRevision)
			var priorBody, changedBody store.SheetBody
			if undoErr == nil {
				undoErr = json.Unmarshal([]byte(prior), &priorBody)
			}
			if undoErr == nil {
				undoErr = json.Unmarshal([]byte(changed), &changedBody)
			}
			if undoErr == nil {
				body, undoErr = store.UndoSheet(priorBody, changedBody, before.Body, in.RowID)
			}
			if errors.Is(undoErr, store.ErrUndoConflict) {
				return &store.ContentConflict{CurrentHash: current.ContentHash}
			}
			if undoErr != nil {
				return undoErr
			}
		} else {
			body, err = store.ApplySheetOperations(before.Body, in.Operations)
			if err != nil {
				return err
			}
		}
		if err := store.ValidateSheet(body); err != nil {
			return err
		}
		if err := recordSheetIDs(ctx, tx, id, body, &before.Body, in.RestoreRevision != "" || in.UndoRevision != "", in.Operations); err != nil {
			return err
		}
		raw, _ := json.Marshal(body)
		out = &store.ContentSnapshot{ItemID: id, Body: string(raw), ContentHash: store.ContentHash(string(raw)), RevisionID: current.RevisionID}
		return saveNativeContentTx(ctx, tx, out, nativeContentSave{ClientRequestID: in.ClientRequestID, Author: in.Author}, requestHash)
	})
	if err != nil {
		return nil, err
	}
	return sheetFromContent(out)
}

package sqlitestore

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"

	"github.com/teatak/pudding-core/internal/store"
)

const tableSelect = `SELECT w.id,c.body,c.content_hash,w.head_revision FROM studio_items w JOIN studio_item_content c ON c.item_id=w.id WHERE w.id=? AND w.kind='table' AND w.deleted=0`

func tableFromContent(d *store.DocumentContent) (*store.TableContent, error) {
	var body store.TableBody
	if err := json.Unmarshal([]byte(d.Body), &body); err != nil {
		return nil, err
	}
	return &store.TableContent{ItemID: d.ItemID, Body: body, ContentHash: d.ContentHash, RevisionID: d.RevisionID}, nil
}
func (s *Store) GetTable(ctx context.Context, id string) (*store.TableContent, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	d, err := scanDocument(s.db.QueryRowContext(ctx, tableSelect, id))
	if err != nil {
		return nil, err
	}
	return tableFromContent(d)
}
func recordTableIDs(ctx context.Context, tx *sql.Tx, id string, body store.TableBody, existing *store.TableBody, restore bool, operations []store.TableOperation) error {
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
		err := tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM studio_table_ids WHERE item_id=? AND entity_kind=? AND entity_id=?`, id, kind, key).Scan(&used)
		if err != nil {
			return err
		}
		if used > 0 {
			if restore {
				return nil
			}
			return &store.InvalidTable{Message: "deleted " + kind + " ID cannot be reused: " + key}
		}
		_, err = tx.ExecContext(ctx, `INSERT INTO studio_table_ids(item_id,entity_kind,entity_id) VALUES(?,?,?)`, id, kind, key)
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
func (s *Store) CreateTable(ctx context.Context, item *store.StudioItem, body store.TableBody, author store.ContentAuthor) (*store.TableContent, error) {
	if err := store.ValidateTable(body); err != nil {
		return nil, err
	}
	if item.Kind != store.StudioItemKindTable {
		return nil, &store.InvalidTable{Message: "table kind required"}
	}
	var d *store.DocumentContent
	err := s.tx(ctx, func(tx *sql.Tx) error {
		if err := validateContentAuthor(ctx, tx, author); err != nil {
			return err
		}
		if err := createStudioItemTx(ctx, tx, item); err != nil {
			return err
		}
		raw, _ := json.Marshal(body)
		d = &store.DocumentContent{ItemID: item.ID, Body: string(raw), ContentHash: store.DocumentHash(string(raw))}
		if _, err := tx.ExecContext(ctx, `INSERT INTO studio_item_content(item_id,body,content_hash) VALUES(?,?,?)`, item.ID, d.Body, d.ContentHash); err != nil {
			return err
		}
		if err := recordTableIDs(ctx, tx, item.ID, body, nil, false, nil); err != nil {
			return err
		}
		return saveNativeContentTx(ctx, tx, d, store.DocumentWrite{ClientRequestID: "create", Author: author}, "")
	})
	if err != nil {
		return nil, err
	}
	return tableFromContent(d)
}
func (s *Store) WriteTable(ctx context.Context, id string, in store.TableWrite) (*store.TableContent, error) {
	if in.ClientRequestID == "" || len(in.ClientRequestID) > 200 {
		return nil, &store.InvalidTable{Message: "clientRequestID is required (max 200 bytes)"}
	}
	if (len(in.Operations) == 0) == (in.RestoreRevision == "") {
		return nil, &store.InvalidTable{Message: "provide operations or restoreRevision"}
	}
	if in.RestoreRevision != "" && in.ExpectedHash == nil {
		return nil, &store.InvalidTable{Message: "expectedHash is required for restore"}
	}
	payload, _ := json.Marshal(struct {
		Write  store.TableWrite
		Author store.ContentAuthor
	}{in, in.Author})
	requestHash := store.DocumentHash(string(payload))
	var out *store.DocumentContent
	err := s.tx(ctx, func(tx *sql.Tx) error {
		if err := validateContentAuthor(ctx, tx, in.Author); err != nil {
			return err
		}
		current, err := scanDocument(tx.QueryRowContext(ctx, tableSelect, id))
		if err != nil {
			return err
		}
		var savedHash, savedRevision string
		err = tx.QueryRowContext(ctx, `SELECT request_hash,hash FROM studio_item_saves WHERE item_id=? AND client_request_id=?`, id, in.ClientRequestID).Scan(&savedHash, &savedRevision)
		if err == nil {
			if savedHash != requestHash {
				return &store.InvalidTable{Message: "clientRequestID reused with different operations"}
			}
			out, err = scanDocument(tx.QueryRowContext(ctx, `SELECT item_id,content,content_hash,hash FROM studio_item_revisions WHERE item_id=? AND hash=?`, id, savedRevision))
			return err
		}
		if !errors.Is(err, sql.ErrNoRows) {
			return err
		}
		if in.ExpectedHash != nil && *in.ExpectedHash != current.ContentHash {
			return &store.ContentConflict{CurrentHash: current.ContentHash}
		}
		before, err := tableFromContent(current)
		if err != nil {
			return err
		}
		var body store.TableBody
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
		} else {
			body, err = store.ApplyTableOperations(before.Body, in.Operations)
			if err != nil {
				return err
			}
		}
		if err := store.ValidateTable(body); err != nil {
			return err
		}
		if err := recordTableIDs(ctx, tx, id, body, &before.Body, in.RestoreRevision != "", in.Operations); err != nil {
			return err
		}
		raw, _ := json.Marshal(body)
		out = &store.DocumentContent{ItemID: id, Body: string(raw), ContentHash: store.DocumentHash(string(raw)), RevisionID: current.RevisionID}
		return saveNativeContentTx(ctx, tx, out, store.DocumentWrite{ClientRequestID: in.ClientRequestID, Author: in.Author}, requestHash)
	})
	if err != nil {
		return nil, err
	}
	return tableFromContent(out)
}

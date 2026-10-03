package sqlitestore

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"time"

	"github.com/teatak/pudding-core/internal/store"
)

const studioItemColumns = `id,kind,name,icon,icon_color,coalesce(source_session_id,''),revision,head_revision,active_revision,bindings,binding_version,deleted,created_at,updated_at`

func scanStudioItem(row messageScanner) (*store.StudioItem, error) {
	w := &store.StudioItem{}
	var bindings string
	var created, updated int64
	if err := row.Scan(&w.ID, &w.Kind, &w.Name, &w.Icon, &w.IconColor, &w.SourceSessionID, &w.Revision, &w.HeadRevision, &w.ActiveRevision, &bindings, &w.BindingVersion, &w.Deleted, &created, &updated); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, store.ErrNotFound
		}
		return nil, err
	}
	if err := json.Unmarshal([]byte(bindings), &w.Bindings); err != nil {
		return nil, err
	}
	w.CreatedAt, w.UpdatedAt = time.UnixMilli(created).UTC(), time.UnixMilli(updated).UTC()
	return w, nil
}
func (s *Store) ListStudioItems(ctx context.Context) ([]*store.StudioItem, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	rows, err := s.db.QueryContext(ctx, `SELECT `+studioItemColumns+` FROM studio_items WHERE deleted=0 ORDER BY updated_at DESC,id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []*store.StudioItem{}
	for rows.Next() {
		w, err := scanStudioItem(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, w)
	}
	return out, rows.Err()
}
func (s *Store) GetStudioItem(ctx context.Context, id string) (*store.StudioItem, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return scanStudioItem(s.db.QueryRowContext(ctx, `SELECT `+studioItemColumns+` FROM studio_items WHERE id=? AND deleted=0`, id))
}
func (s *Store) CreateStudioItem(ctx context.Context, w *store.StudioItem) (*store.StudioItem, error) {
	err := s.tx(ctx, func(tx *sql.Tx) error { return createStudioItemTx(ctx, tx, w) })
	if err != nil {
		return nil, err
	}
	return s.GetStudioItem(ctx, w.ID)
}
func createStudioItemTx(ctx context.Context, tx *sql.Tx, w *store.StudioItem) error {
	var session any
	if w.SourceSessionID != "" {
		if _, err := getSessionTx(ctx, tx, w.SourceSessionID); err != nil {
			return err
		}
		session = w.SourceSessionID
	}
	_, err := tx.ExecContext(ctx, `INSERT INTO studio_items(id,kind,name,icon,icon_color,source_session_id,revision,head_revision,active_revision,bindings,binding_version,deleted,created_at,updated_at) VALUES(?,?,?,?,?,?,1,'','','{}',1,0,?,?)`, w.ID, w.Kind, w.Name, w.Icon, w.IconColor, session, unixMS(w.CreatedAt), unixMS(w.UpdatedAt))
	return err
}
func (s *Store) UpdateStudioItem(ctx context.Context, w *store.StudioItem, expected int64) (*store.StudioItem, error) {
	err := s.tx(ctx, func(tx *sql.Tx) error {
		if w.ActiveRevision != "" {
			var receipt string
			if err := tx.QueryRowContext(ctx, `SELECT build_receipt FROM studio_item_revisions WHERE item_id=? AND hash=?`, w.ID, w.ActiveRevision).Scan(&receipt); err != nil {
				return err
			}
			if receipt == "" {
				return store.ErrStudioItemConflict
			}
		}
		bindings, err := json.Marshal(w.Bindings)
		if err != nil {
			return err
		}
		result, err := tx.ExecContext(ctx, `UPDATE studio_items SET name=?,icon=?,icon_color=?,active_revision=?,bindings=?,binding_version=?,deleted=?,revision=revision+1 WHERE id=? AND revision=? AND deleted=0`, w.Name, w.Icon, w.IconColor, w.ActiveRevision, string(bindings), w.BindingVersion, w.Deleted, w.ID, expected)
		if err != nil {
			return err
		}
		count, err := result.RowsAffected()
		if err != nil {
			return err
		}
		if count != 1 {
			return store.ErrStudioItemConflict
		}
		if w.Deleted {
			_, err = tx.ExecContext(ctx, `DELETE FROM studio_mounts WHERE item_id=?`, w.ID)
			return err
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	if w.Deleted {
		return w, nil
	}
	return s.GetStudioItem(ctx, w.ID)
}
func (s *Store) SaveStudioItemRevision(ctx context.Context, r *store.StudioItemRevision, baseHash string) (*store.StudioItem, error) {
	err := s.tx(ctx, func(tx *sql.Tx) error { return saveStudioItemRevisionTx(ctx, tx, r, baseHash) })
	if err != nil {
		return nil, err
	}
	return s.GetStudioItem(ctx, r.ItemID)
}
func saveStudioItemRevisionTx(ctx context.Context, tx *sql.Tx, r *store.StudioItemRevision, baseHash string) error {
	current, err := scanStudioItem(tx.QueryRowContext(ctx, `SELECT `+studioItemColumns+` FROM studio_items WHERE id=? AND deleted=0`, r.ItemID))
	if err != nil {
		return err
	}
	var hash string
	err = tx.QueryRowContext(ctx, `SELECT hash FROM studio_item_saves WHERE item_id=? AND client_request_id=?`, r.ItemID, r.ClientRequestID).Scan(&hash)
	if err == nil {
		if hash != r.Hash {
			return store.ErrStudioItemConflict
		}
		return nil
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return err
	}
	if current.HeadRevision != baseHash {
		return store.ErrStudioItemConflict
	}
	if _, err := tx.ExecContext(ctx, `INSERT INTO studio_item_saves(item_id,client_request_id,hash) VALUES(?,?,?)`, r.ItemID, r.ClientRequestID, r.Hash); err != nil {
		return err
	}
	// A source hash names one immutable package; saving it again reuses that version.
	_, err = tx.ExecContext(ctx, `INSERT INTO studio_item_revisions(item_id,hash,parent_revision,client_request_id,created_at,build_receipt) VALUES(?,?,?,?,?,'') ON CONFLICT(item_id,hash) DO NOTHING`, r.ItemID, r.Hash, current.HeadRevision, r.ClientRequestID, unixMS(r.CreatedAt))
	if err != nil {
		return err
	}
	_, err = tx.ExecContext(ctx, `UPDATE studio_items SET head_revision=?,revision=revision+1,updated_at=? WHERE id=?`, r.Hash, unixMS(r.CreatedAt), r.ItemID)

	if err != nil {
		return err
	}

	return err
}
func scanStudioItemRevision(row messageScanner) (*store.StudioItemRevision, error) {
	r := &store.StudioItemRevision{}
	var created int64
	var receipt string
	var body sql.NullString
	var author store.ContentAuthor
	if err := row.Scan(&r.ItemID, &r.Hash, &r.ParentRevision, &r.ClientRequestID, &created, &receipt, &body, &r.ContentHash, &author.Kind, &author.SessionID, &author.TurnID); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, store.ErrNotFound
		}
		return nil, err
	}
	r.CreatedAt = time.UnixMilli(created).UTC()
	if body.Valid {
		r.Body = &body.String
	}
	if author.Kind != "" {
		r.Author = &author
	}

	if receipt != "" {
		r.BuildReceipt = json.RawMessage(receipt)
	}
	return r, nil
}
func (s *Store) ListStudioItemRevisions(ctx context.Context, id string) ([]*store.StudioItemRevision, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	rows, err := s.db.QueryContext(ctx, `SELECT item_id,hash,parent_revision,client_request_id,created_at,build_receipt,NULL,content_hash,author_kind,author_session_id,author_turn_id FROM studio_item_revisions WHERE item_id=? ORDER BY created_at DESC,rowid DESC`, id)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []*store.StudioItemRevision{}
	for rows.Next() {
		r, err := scanStudioItemRevision(rows)
		if err != nil {
			return nil, err
		}
		r.Body = nil
		out = append(out, r)
	}
	return out, rows.Err()
}
func (s *Store) GetStudioItemRevision(ctx context.Context, id, hash string) (*store.StudioItemRevision, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return scanStudioItemRevision(s.db.QueryRowContext(ctx, `SELECT item_id,hash,parent_revision,client_request_id,created_at,build_receipt,content,content_hash,author_kind,author_session_id,author_turn_id FROM studio_item_revisions WHERE item_id=? AND hash=?`, id, hash))
}
func (s *Store) PutWidgetBuildReceipt(ctx context.Context, id, hash string, receipt json.RawMessage) error {
	return s.tx(ctx, func(tx *sql.Tx) error {
		result, err := tx.ExecContext(ctx, `UPDATE studio_item_revisions SET build_receipt=? WHERE item_id=? AND hash=?`, string(receipt), id, hash)
		if err != nil {
			return err
		}
		count, err := result.RowsAffected()
		if err != nil {
			return err
		}
		if count != 1 {
			return store.ErrNotFound
		}
		return nil
	})
}

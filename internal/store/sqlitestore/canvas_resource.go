package sqlitestore

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"time"

	"github.com/teatak/pudding-core/internal/store"
)

const canvasColumns = `id,name,icon,icon_color,coalesce(source_session_id,''),revision,head_revision,active_revision,bindings,binding_version,deleted,created_at,updated_at`

func scanCanvas(row messageScanner) (*store.Canvas, error) {
	w := &store.Canvas{}
	var bindings string
	var created, updated int64
	if err := row.Scan(&w.ID, &w.Name, &w.Icon, &w.IconColor, &w.SourceSessionID, &w.Revision, &w.HeadRevision, &w.ActiveRevision, &bindings, &w.BindingVersion, &w.Deleted, &created, &updated); err != nil {
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
func (s *Store) ListCanvases(ctx context.Context) ([]*store.Canvas, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	rows, err := s.db.QueryContext(ctx, `SELECT `+canvasColumns+` FROM canvas_resources WHERE deleted=0 ORDER BY updated_at DESC,id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []*store.Canvas{}
	for rows.Next() {
		w, err := scanCanvas(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, w)
	}
	return out, rows.Err()
}
func (s *Store) GetCanvas(ctx context.Context, id string) (*store.Canvas, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return scanCanvas(s.db.QueryRowContext(ctx, `SELECT `+canvasColumns+` FROM canvas_resources WHERE id=? AND deleted=0`, id))
}
func (s *Store) CreateCanvas(ctx context.Context, w *store.Canvas) (*store.Canvas, error) {
	err := s.tx(ctx, func(tx *sql.Tx) error {
		var session any
		if w.SourceSessionID != "" {
			if _, err := getSessionTx(ctx, tx, w.SourceSessionID); err != nil {
				return err
			}
			session = w.SourceSessionID
		}
		_, err := tx.ExecContext(ctx, `INSERT INTO canvas_resources(id,name,icon,icon_color,source_session_id,revision,head_revision,active_revision,bindings,binding_version,deleted,created_at,updated_at) VALUES(?,?,?,?,?,1,'','','{}',1,0,?,?)`, w.ID, w.Name, w.Icon, w.IconColor, session, unixMS(w.CreatedAt), unixMS(w.UpdatedAt))
		return err
	})
	if err != nil {
		return nil, err
	}
	return s.GetCanvas(ctx, w.ID)
}
func (s *Store) UpdateCanvas(ctx context.Context, w *store.Canvas, expected int64) (*store.Canvas, error) {
	err := s.tx(ctx, func(tx *sql.Tx) error {
		if w.ActiveRevision != "" {
			var receipt string
			if err := tx.QueryRowContext(ctx, `SELECT build_receipt FROM canvas_revisions WHERE canvas_id=? AND hash=?`, w.ID, w.ActiveRevision).Scan(&receipt); err != nil {
				return err
			}
			if receipt == "" {
				return store.ErrCanvasConflict
			}
		}
		bindings, err := json.Marshal(w.Bindings)
		if err != nil {
			return err
		}
		result, err := tx.ExecContext(ctx, `UPDATE canvas_resources SET name=?,icon=?,icon_color=?,active_revision=?,bindings=?,binding_version=?,deleted=?,revision=revision+1 WHERE id=? AND revision=? AND deleted=0`, w.Name, w.Icon, w.IconColor, w.ActiveRevision, string(bindings), w.BindingVersion, w.Deleted, w.ID, expected)
		if err != nil {
			return err
		}
		count, err := result.RowsAffected()
		if err != nil {
			return err
		}
		if count != 1 {
			return store.ErrCanvasConflict
		}
		if w.Deleted {
			_, err = tx.ExecContext(ctx, `DELETE FROM canvas_mounts WHERE resource_id=?`, w.ID)
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
	return s.GetCanvas(ctx, w.ID)
}
func (s *Store) SaveCanvasRevision(ctx context.Context, r *store.CanvasRevision, baseHash string) (*store.Canvas, error) {
	err := s.tx(ctx, func(tx *sql.Tx) error { return saveCanvasRevisionTx(ctx, tx, r, baseHash) })
	if err != nil {
		return nil, err
	}
	return s.GetCanvas(ctx, r.CanvasID)
}
func saveCanvasRevisionTx(ctx context.Context, tx *sql.Tx, r *store.CanvasRevision, baseHash string) error {
	current, err := scanCanvas(tx.QueryRowContext(ctx, `SELECT `+canvasColumns+` FROM canvas_resources WHERE id=? AND deleted=0`, r.CanvasID))
	if err != nil {
		return err
	}
	var hash string
	err = tx.QueryRowContext(ctx, `SELECT hash FROM canvas_saves WHERE canvas_id=? AND client_request_id=?`, r.CanvasID, r.ClientRequestID).Scan(&hash)
	if err == nil {
		if hash != r.Hash {
			return store.ErrCanvasConflict
		}
		return nil
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return err
	}
	if current.HeadRevision != baseHash {
		return store.ErrCanvasConflict
	}
	if _, err := tx.ExecContext(ctx, `INSERT INTO canvas_saves(canvas_id,client_request_id,hash) VALUES(?,?,?)`, r.CanvasID, r.ClientRequestID, r.Hash); err != nil {
		return err
	}
	// A source hash names one immutable package; saving it again reuses that version.
	_, err = tx.ExecContext(ctx, `INSERT INTO canvas_revisions(canvas_id,hash,parent_revision,client_request_id,created_at,build_receipt) VALUES(?,?,?,?,?,'') ON CONFLICT(canvas_id,hash) DO NOTHING`, r.CanvasID, r.Hash, current.HeadRevision, r.ClientRequestID, unixMS(r.CreatedAt))
	if err != nil {
		return err
	}
	_, err = tx.ExecContext(ctx, `UPDATE canvas_resources SET head_revision=?,revision=revision+1,updated_at=? WHERE id=?`, r.Hash, unixMS(r.CreatedAt), r.CanvasID)

	if err != nil {
		return err
	}

	return err
}
func scanCanvasRevision(row messageScanner) (*store.CanvasRevision, error) {
	r := &store.CanvasRevision{}
	var created int64
	var receipt string
	if err := row.Scan(&r.CanvasID, &r.Hash, &r.ParentRevision, &r.ClientRequestID, &created, &receipt); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, store.ErrNotFound
		}
		return nil, err
	}
	r.CreatedAt = time.UnixMilli(created).UTC()

	if receipt != "" {
		r.BuildReceipt = json.RawMessage(receipt)
	}
	return r, nil
}
func (s *Store) ListCanvasRevisions(ctx context.Context, id string) ([]*store.CanvasRevision, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	rows, err := s.db.QueryContext(ctx, `SELECT canvas_id,hash,parent_revision,client_request_id,created_at,build_receipt FROM canvas_revisions WHERE canvas_id=? ORDER BY created_at DESC,hash`, id)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []*store.CanvasRevision{}
	for rows.Next() {
		r, err := scanCanvasRevision(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, rows.Err()
}
func (s *Store) GetCanvasRevision(ctx context.Context, id, hash string) (*store.CanvasRevision, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return scanCanvasRevision(s.db.QueryRowContext(ctx, `SELECT canvas_id,hash,parent_revision,client_request_id,created_at,build_receipt FROM canvas_revisions WHERE canvas_id=? AND hash=?`, id, hash))
}
func (s *Store) PutCanvasBuildReceipt(ctx context.Context, id, hash string, receipt json.RawMessage) error {
	return s.tx(ctx, func(tx *sql.Tx) error {
		result, err := tx.ExecContext(ctx, `UPDATE canvas_revisions SET build_receipt=? WHERE canvas_id=? AND hash=?`, string(receipt), id, hash)
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

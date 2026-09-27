package sqlitestore

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"time"

	"github.com/teatak/pudding-core/internal/store"
)

const workbenchColumns = `id,name,coalesce(source_session_id,''),revision,head_revision,active_revision,bindings,grants,binding_version,deleted,created_at,updated_at`

func scanWorkbench(row messageScanner) (*store.Workbench, error) {
	w := &store.Workbench{}
	var bindings, grants string
	var created, updated int64
	if err := row.Scan(&w.ID, &w.Name, &w.SourceSessionID, &w.Revision, &w.HeadRevision, &w.ActiveRevision, &bindings, &grants, &w.BindingVersion, &w.Deleted, &created, &updated); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, store.ErrNotFound
		}
		return nil, err
	}
	if err := json.Unmarshal([]byte(bindings), &w.Bindings); err != nil {
		return nil, err
	}
	if err := json.Unmarshal([]byte(grants), &w.Grants); err != nil {
		return nil, err
	}
	w.CreatedAt, w.UpdatedAt = time.UnixMilli(created).UTC(), time.UnixMilli(updated).UTC()
	return w, nil
}
func (s *Store) ListWorkbenches(ctx context.Context) ([]*store.Workbench, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	rows, err := s.db.QueryContext(ctx, `SELECT `+workbenchColumns+` FROM canvas_resources WHERE deleted=0 ORDER BY updated_at DESC,id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []*store.Workbench{}
	for rows.Next() {
		w, err := scanWorkbench(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, w)
	}
	return out, rows.Err()
}
func (s *Store) GetWorkbench(ctx context.Context, id string) (*store.Workbench, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return scanWorkbench(s.db.QueryRowContext(ctx, `SELECT `+workbenchColumns+` FROM canvas_resources WHERE id=? AND deleted=0`, id))
}
func (s *Store) CreateWorkbench(ctx context.Context, w *store.Workbench) (*store.Workbench, error) {
	err := s.tx(ctx, func(tx *sql.Tx) error {
		var session any
		if w.SourceSessionID != "" {
			if _, err := getSessionTx(ctx, tx, w.SourceSessionID); err != nil {
				return err
			}
			session = w.SourceSessionID
		}
		_, err := tx.ExecContext(ctx, `INSERT INTO canvas_resources(id,name,source_session_id,revision,head_revision,active_revision,bindings,grants,binding_version,deleted,created_at,updated_at) VALUES(?,?,?,1,'','','{}','{}',1,0,?,?)`, w.ID, w.Name, session, unixMS(w.CreatedAt), unixMS(w.UpdatedAt))
		return err
	})
	if err != nil {
		return nil, err
	}
	return s.GetWorkbench(ctx, w.ID)
}
func (s *Store) UpdateWorkbench(ctx context.Context, w *store.Workbench, expected int64) (*store.Workbench, error) {
	err := s.tx(ctx, func(tx *sql.Tx) error {
		if w.ActiveRevision != "" {
			var receipt string
			if err := tx.QueryRowContext(ctx, `SELECT build_receipt FROM canvas_revisions WHERE workbench_id=? AND hash=?`, w.ID, w.ActiveRevision).Scan(&receipt); err != nil {
				return err
			}
			if receipt == "" {
				return store.ErrWorkbenchConflict
			}
		}
		grants, err := json.Marshal(w.Grants)
		if err != nil {
			return err
		}
		bindings, err := json.Marshal(w.Bindings)
		if err != nil {
			return err
		}
		result, err := tx.ExecContext(ctx, `UPDATE canvas_resources SET name=?,active_revision=?,bindings=?,grants=?,binding_version=?,deleted=?,revision=revision+1,updated_at=? WHERE id=? AND revision=? AND deleted=0`, w.Name, w.ActiveRevision, string(bindings), string(grants), w.BindingVersion, w.Deleted, unixMS(time.Now()), w.ID, expected)
		if err != nil {
			return err
		}
		count, err := result.RowsAffected()
		if err != nil {
			return err
		}
		if count != 1 {
			return store.ErrWorkbenchConflict
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
	return s.GetWorkbench(ctx, w.ID)
}
func (s *Store) SaveWorkbenchRevision(ctx context.Context, r *store.WorkbenchRevision, expected int64) (*store.Workbench, error) {
	err := s.tx(ctx, func(tx *sql.Tx) error { return saveCanvasRevisionTx(ctx, tx, r, expected) })
	if err != nil {
		return nil, err
	}
	return s.GetWorkbench(ctx, r.WorkbenchID)
}
func saveCanvasRevisionTx(ctx context.Context, tx *sql.Tx, r *store.WorkbenchRevision, expected int64) error {
	current, err := scanWorkbench(tx.QueryRowContext(ctx, `SELECT `+workbenchColumns+` FROM canvas_resources WHERE id=? AND deleted=0`, r.WorkbenchID))
	if err != nil {
		return err
	}
	var hash string
	err = tx.QueryRowContext(ctx, `SELECT hash FROM canvas_saves WHERE workbench_id=? AND client_request_id=?`, r.WorkbenchID, r.ClientRequestID).Scan(&hash)
	if err == nil {
		if hash != r.Hash {
			return store.ErrWorkbenchConflict
		}
		return nil
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return err
	}
	if current.Revision != expected {
		return store.ErrWorkbenchConflict
	}
	if _, err := tx.ExecContext(ctx, `INSERT INTO canvas_saves(workbench_id,client_request_id,hash) VALUES(?,?,?)`, r.WorkbenchID, r.ClientRequestID, r.Hash); err != nil {
		return err
	}
	// A source hash names one immutable package; saving it again reuses that version.
	_, err = tx.ExecContext(ctx, `INSERT INTO canvas_revisions(workbench_id,hash,parent_revision,client_request_id,created_at,build_receipt) VALUES(?,?,?,?,?,'') ON CONFLICT(workbench_id,hash) DO NOTHING`, r.WorkbenchID, r.Hash, current.HeadRevision, r.ClientRequestID, unixMS(r.CreatedAt))
	if err != nil {
		return err
	}
	_, err = tx.ExecContext(ctx, `UPDATE canvas_resources SET head_revision=?,revision=revision+1,updated_at=? WHERE id=?`, r.Hash, unixMS(r.CreatedAt), r.WorkbenchID)

	if err != nil {
		return err
	}

	return err
}
func scanWorkbenchRevision(row messageScanner) (*store.WorkbenchRevision, error) {
	r := &store.WorkbenchRevision{}
	var created int64
	var receipt string
	if err := row.Scan(&r.WorkbenchID, &r.Hash, &r.ParentRevision, &r.ClientRequestID, &created, &receipt); err != nil {
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
func (s *Store) ListWorkbenchRevisions(ctx context.Context, id string) ([]*store.WorkbenchRevision, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	rows, err := s.db.QueryContext(ctx, `SELECT workbench_id,hash,parent_revision,client_request_id,created_at,build_receipt FROM canvas_revisions WHERE workbench_id=? ORDER BY created_at DESC,hash`, id)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []*store.WorkbenchRevision{}
	for rows.Next() {
		r, err := scanWorkbenchRevision(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, rows.Err()
}
func (s *Store) GetWorkbenchRevision(ctx context.Context, id, hash string) (*store.WorkbenchRevision, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return scanWorkbenchRevision(s.db.QueryRowContext(ctx, `SELECT workbench_id,hash,parent_revision,client_request_id,created_at,build_receipt FROM canvas_revisions WHERE workbench_id=? AND hash=?`, id, hash))
}
func (s *Store) PutWorkbenchBuildReceipt(ctx context.Context, id, hash string, receipt json.RawMessage) error {
	return s.tx(ctx, func(tx *sql.Tx) error {
		result, err := tx.ExecContext(ctx, `UPDATE canvas_revisions SET build_receipt=? WHERE workbench_id=? AND hash=?`, string(receipt), id, hash)
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

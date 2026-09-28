package sqlitestore

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"github.com/teatak/pudding-core/internal/store"
	"time"
)

const actionColumns = `id,workbench_id,client_request_id,request_hash,state,spec,result,created_at`

func scanCanvasAction(row messageScanner) (*store.CanvasAction, error) {
	a := &store.CanvasAction{}
	var spec, result string
	var created int64
	if err := row.Scan(&a.ID, &a.CanvasID, &a.ClientRequestID, &a.RequestHash, &a.State, &spec, &result, &created); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, store.ErrNotFound
		}
		return nil, err
	}
	if err := json.Unmarshal([]byte(spec), &a.Spec); err != nil {
		return nil, err
	}
	if result != "" {
		a.Result = json.RawMessage(result)
	}
	a.CreatedAt = time.UnixMilli(created).UTC()
	return a, nil
}
func (s *Store) CreateCanvasAction(ctx context.Context, a *store.CanvasAction) (*store.CanvasAction, error) {
	var out *store.CanvasAction
	err := s.tx(ctx, func(tx *sql.Tx) error {
		old, err := scanCanvasAction(tx.QueryRowContext(ctx, `SELECT `+actionColumns+` FROM canvas_actions WHERE workbench_id=? AND client_request_id=?`, a.CanvasID, a.ClientRequestID))
		if err == nil {
			if old.RequestHash != a.RequestHash {
				return store.ErrCanvasConflict
			}
			out = old
			return nil
		}
		if !errors.Is(err, store.ErrNotFound) {
			return err
		}
		spec, err := json.Marshal(a.Spec)
		if err != nil {
			return err
		}
		_, err = tx.ExecContext(ctx, `INSERT INTO canvas_actions(id,workbench_id,client_request_id,request_hash,state,spec,created_at) VALUES(?,?,?,?,'prepared',?,?)`, a.ID, a.CanvasID, a.ClientRequestID, a.RequestHash, string(spec), unixMS(a.CreatedAt))
		out = a
		return err
	})
	return out, err
}
func (s *Store) GetCanvasAction(ctx context.Context, wid, id string) (*store.CanvasAction, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return scanCanvasAction(s.db.QueryRowContext(ctx, `SELECT `+actionColumns+` FROM canvas_actions WHERE workbench_id=? AND id=?`, wid, id))
}
func (s *Store) ListCanvasActions(ctx context.Context, wid string) ([]*store.CanvasAction, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	rows, err := s.db.QueryContext(ctx, `SELECT `+actionColumns+` FROM canvas_actions WHERE workbench_id=? ORDER BY created_at DESC LIMIT 100`, wid)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []*store.CanvasAction{}
	for rows.Next() {
		a, err := scanCanvasAction(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, a)
	}
	return out, rows.Err()
}
func (s *Store) ClaimCanvasAction(ctx context.Context, wid, id string) error {
	return s.tx(ctx, func(tx *sql.Tx) error {
		a, err := scanCanvasAction(tx.QueryRowContext(ctx, `SELECT `+actionColumns+` FROM canvas_actions WHERE workbench_id=? AND id=?`, wid, id))
		if err != nil {
			return err
		}
		w, err := scanCanvas(tx.QueryRowContext(ctx, `SELECT `+canvasColumns+` FROM canvas_resources WHERE id=? AND deleted=0`, wid))
		if err != nil {
			return err
		}
		if a.State != "prepared" || w.Revision != a.Spec.ResourceRevision || w.BindingVersion != a.Spec.BindingVersion || w.ActiveRevision != a.Spec.RevisionHash {
			return store.ErrCanvasConflict
		}
		_, err = tx.ExecContext(ctx, `UPDATE canvas_actions SET state='executing' WHERE id=?`, id)
		return err
	})
}
func (s *Store) FinishCanvasAction(ctx context.Context, wid, id, state string, result json.RawMessage) error {
	if state != "succeeded" && state != "failed" && state != "unknown" {
		return store.ErrCanvasConflict
	}
	return s.tx(ctx, func(tx *sql.Tx) error {
		r, err := tx.ExecContext(ctx, `UPDATE canvas_actions SET state=?,result=? WHERE workbench_id=? AND id=? AND state='executing'`, state, string(result), wid, id)
		if err != nil {
			return err
		}
		n, err := r.RowsAffected()
		if err == nil && n != 1 {
			return store.ErrCanvasConflict
		}
		return err
	})
}

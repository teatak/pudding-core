package sqlitestore

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"github.com/teatak/pudding-core/internal/store"
	"time"
)

const actionColumns = `id,item_id,client_request_id,request_hash,state,spec,result,created_at`

func scanWidgetAction(row messageScanner) (*store.WidgetAction, error) {
	a := &store.WidgetAction{}
	var spec, result string
	var created int64
	if err := row.Scan(&a.ID, &a.ItemID, &a.ClientRequestID, &a.RequestHash, &a.State, &spec, &result, &created); err != nil {
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
func (s *Store) CreateWidgetAction(ctx context.Context, a *store.WidgetAction) (*store.WidgetAction, error) {
	var out *store.WidgetAction
	err := s.tx(ctx, func(tx *sql.Tx) error {
		old, err := scanWidgetAction(tx.QueryRowContext(ctx, `SELECT `+actionColumns+` FROM widget_actions WHERE item_id=? AND client_request_id=?`, a.ItemID, a.ClientRequestID))
		if err == nil {
			if old.RequestHash != a.RequestHash {
				return store.ErrStudioItemConflict
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
		_, err = tx.ExecContext(ctx, `INSERT INTO widget_actions(id,item_id,client_request_id,request_hash,state,spec,created_at) VALUES(?,?,?,?,'prepared',?,?)`, a.ID, a.ItemID, a.ClientRequestID, a.RequestHash, string(spec), unixMS(a.CreatedAt))
		out = a
		return err
	})
	return out, err
}
func (s *Store) GetWidgetAction(ctx context.Context, wid, id string) (*store.WidgetAction, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return scanWidgetAction(s.db.QueryRowContext(ctx, `SELECT `+actionColumns+` FROM widget_actions WHERE item_id=? AND id=?`, wid, id))
}
func (s *Store) ListWidgetActions(ctx context.Context, wid string) ([]*store.WidgetAction, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	rows, err := s.db.QueryContext(ctx, `SELECT `+actionColumns+` FROM widget_actions WHERE item_id=? ORDER BY created_at DESC LIMIT 100`, wid)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []*store.WidgetAction{}
	for rows.Next() {
		a, err := scanWidgetAction(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, a)
	}
	return out, rows.Err()
}
func (s *Store) ClaimWidgetAction(ctx context.Context, wid, id string) error {
	return s.tx(ctx, func(tx *sql.Tx) error {
		a, err := scanWidgetAction(tx.QueryRowContext(ctx, `SELECT `+actionColumns+` FROM widget_actions WHERE item_id=? AND id=?`, wid, id))
		if err != nil {
			return err
		}
		w, err := scanStudioItem(tx.QueryRowContext(ctx, `SELECT `+studioItemColumns+` FROM studio_items WHERE id=? AND deleted=0 AND archived_at=0`, wid))
		if err != nil {
			return err
		}
		if a.State != "prepared" || w.Revision != a.Spec.ResourceRevision || w.BindingVersion != a.Spec.BindingVersion || w.ActiveRevision != a.Spec.RevisionHash {
			return store.ErrStudioItemConflict
		}
		_, err = tx.ExecContext(ctx, `UPDATE widget_actions SET state='executing' WHERE id=?`, id)
		return err
	})
}
func (s *Store) FinishWidgetAction(ctx context.Context, wid, id, state string, result json.RawMessage) error {
	if state != "succeeded" && state != "failed" && state != "unknown" {
		return store.ErrStudioItemConflict
	}
	return s.tx(ctx, func(tx *sql.Tx) error {
		r, err := tx.ExecContext(ctx, `UPDATE widget_actions SET state=?,result=? WHERE item_id=? AND id=? AND state='executing'`, state, string(result), wid, id)
		if err != nil {
			return err
		}
		n, err := r.RowsAffected()
		if err == nil && n != 1 {
			return store.ErrStudioItemConflict
		}
		return err
	})
}

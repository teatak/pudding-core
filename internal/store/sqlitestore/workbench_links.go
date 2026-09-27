package sqlitestore

import (
	"context"
	"database/sql"
	"encoding/json"
	"github.com/teatak/pudding-core/internal/store"
	"time"
)

func (s *Store) ListWorkbenchLinks(ctx context.Context, wid string) ([]*store.WorkbenchLink, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	rows, err := s.db.QueryContext(ctx, `SELECT id,left_entity,right_entity,created_at FROM canvas_links WHERE workbench_id=? ORDER BY created_at DESC`, wid)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []*store.WorkbenchLink{}
	for rows.Next() {
		l := &store.WorkbenchLink{WorkbenchID: wid}
		var left, right string
		var created int64
		if err = rows.Scan(&l.ID, &left, &right, &created); err != nil {
			return nil, err
		}
		if err = json.Unmarshal([]byte(left), &l.Left); err != nil {
			return nil, err
		}
		if err = json.Unmarshal([]byte(right), &l.Right); err != nil {
			return nil, err
		}
		l.CreatedAt = time.UnixMilli(created).UTC()
		out = append(out, l)
	}
	return out, rows.Err()
}
func (s *Store) PutWorkbenchLink(ctx context.Context, l *store.WorkbenchLink, expected int64) error {
	return s.tx(ctx, func(tx *sql.Tx) error {
		w, err := scanWorkbench(tx.QueryRowContext(ctx, `SELECT `+workbenchColumns+` FROM canvas_resources WHERE id=? AND deleted=0`, l.WorkbenchID))
		if err != nil {
			return err
		}
		if w.Revision != expected {
			return store.ErrWorkbenchConflict
		}
		left, _ := json.Marshal(l.Left)
		right, _ := json.Marshal(l.Right)
		_, err = tx.ExecContext(ctx, `INSERT INTO canvas_links(id,workbench_id,left_entity,right_entity,created_at) VALUES(?,?,?,?,?) ON CONFLICT(workbench_id,left_entity,right_entity) DO NOTHING`, l.ID, l.WorkbenchID, string(left), string(right), unixMS(l.CreatedAt))
		return err
	})
}
func (s *Store) DeleteWorkbenchLink(ctx context.Context, wid, id string, expected int64) error {
	return s.tx(ctx, func(tx *sql.Tx) error {
		w, err := scanWorkbench(tx.QueryRowContext(ctx, `SELECT `+workbenchColumns+` FROM canvas_resources WHERE id=? AND deleted=0`, wid))
		if err != nil {
			return err
		}
		if w.Revision != expected {
			return store.ErrWorkbenchConflict
		}
		_, err = tx.ExecContext(ctx, `DELETE FROM canvas_links WHERE workbench_id=? AND id=?`, wid, id)
		return err
	})
}

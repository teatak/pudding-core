package sqlitestore

import (
	"context"
	"database/sql"
	"encoding/json"
	"github.com/teatak/pudding-core/internal/store"
	"time"
)

func (s *Store) ListWidgetLinks(ctx context.Context, wid string) ([]*store.WidgetLink, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	rows, err := s.db.QueryContext(ctx, `SELECT id,left_entity,right_entity,created_at FROM widget_links WHERE item_id=? ORDER BY created_at DESC`, wid)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []*store.WidgetLink{}
	for rows.Next() {
		l := &store.WidgetLink{ItemID: wid}
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
func (s *Store) PutWidgetLink(ctx context.Context, l *store.WidgetLink, expected int64) error {
	return s.tx(ctx, func(tx *sql.Tx) error {
		w, err := scanStudioItem(tx.QueryRowContext(ctx, `SELECT `+studioItemColumns+` FROM studio_items WHERE id=? AND deleted=0 AND archived_at=0`, l.ItemID))
		if err != nil {
			return err
		}
		if w.Revision != expected {
			return store.ErrStudioItemConflict
		}
		left, _ := json.Marshal(l.Left)
		right, _ := json.Marshal(l.Right)
		_, err = tx.ExecContext(ctx, `INSERT INTO widget_links(id,item_id,left_entity,right_entity,created_at) VALUES(?,?,?,?,?) ON CONFLICT(item_id,left_entity,right_entity) DO NOTHING`, l.ID, l.ItemID, string(left), string(right), unixMS(l.CreatedAt))
		return err
	})
}
func (s *Store) DeleteWidgetLink(ctx context.Context, wid, id string, expected int64) error {
	return s.tx(ctx, func(tx *sql.Tx) error {
		w, err := scanStudioItem(tx.QueryRowContext(ctx, `SELECT `+studioItemColumns+` FROM studio_items WHERE id=? AND deleted=0 AND archived_at=0`, wid))
		if err != nil {
			return err
		}
		if w.Revision != expected {
			return store.ErrStudioItemConflict
		}
		_, err = tx.ExecContext(ctx, `DELETE FROM widget_links WHERE item_id=? AND id=?`, wid, id)
		return err
	})
}

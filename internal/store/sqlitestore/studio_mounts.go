package sqlitestore

import (
	"context"
	"database/sql"
	"errors"
	"time"

	"github.com/teatak/pudding-core/internal/store"
)

const canvasMountSelect = `SELECT m.session_id,m.id,m.resource_id,m.visible,m.created_at,
 w.name,w.icon,w.icon_color,coalesce(w.source_session_id,''),w.revision,w.updated_at
 FROM canvas_mounts m JOIN canvas_resources w ON w.id=m.resource_id AND w.deleted=0`

func scanCanvasItem(row messageScanner) (*store.CanvasItem, error) {
	m := &store.CanvasItem{}
	w := &store.Canvas{}
	var created, updated int64

	if err := row.Scan(&m.SessionID, &m.ID, &m.ResourceID, &m.Visible, &created, &w.Name, &w.Icon, &w.IconColor, &w.SourceSessionID, &w.Revision, &updated); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, store.ErrNotFound
		}
		return nil, err
	}
	m.CreatedAt = timeFromMS(created)
	m.CreatedBySessionID = m.SessionID
	m.UpdatedBySessionID = m.SessionID
	w.ID = m.ResourceID
	w.UpdatedAt = timeFromMS(updated)
	return store.ProjectCanvasItem(m, w), nil
}

func (s *Store) ListCanvasItems(ctx context.Context, session string) ([]*store.CanvasItem, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, err := s.getSessionDB(ctx, session); err != nil {
		return nil, err
	}
	rows, err := s.db.QueryContext(ctx, canvasMountSelect+` WHERE m.session_id=? ORDER BY m.created_at,m.id`, session)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []*store.CanvasItem{}
	for rows.Next() {
		v, err := scanCanvasItem(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, v)
	}
	return out, rows.Err()
}

// Removing a session mount closes that view; the independent resource survives.
func (s *Store) DeleteCanvasItem(ctx context.Context, session, id string) error {
	return s.tx(ctx, func(tx *sql.Tx) error {
		if _, err := getSessionTx(ctx, tx, session); err != nil {
			return err
		}
		res, err := tx.ExecContext(ctx, `DELETE FROM canvas_mounts WHERE session_id=? AND id=?`, session, id)
		if err != nil {
			return err
		}
		n, err := res.RowsAffected()
		if err != nil {
			return err
		}
		if n == 0 {
			return store.ErrNotFound
		}
		return nil
	})
}
func (s *Store) OpenCanvasResource(ctx context.Context, session, id, itemID string) (*store.CanvasItem, error) {
	var out *store.CanvasItem
	err := s.tx(ctx, func(tx *sql.Tx) error {
		if _, err := getSessionTx(ctx, tx, session); err != nil {
			return err
		}
		if _, err := scanCanvas(tx.QueryRowContext(ctx, `SELECT `+canvasColumns+` FROM canvas_resources WHERE id=? AND deleted=0`, id)); err != nil {
			return err
		}
		if _, err := tx.ExecContext(ctx, `INSERT INTO canvas_mounts(session_id,id,resource_id,created_at) VALUES(?,?,?,?) ON CONFLICT(session_id,resource_id) DO UPDATE SET visible=1`, session, itemID, id, unixMS(time.Now())); err != nil {
			return err
		}
		var err error
		out, err = scanCanvasItem(tx.QueryRowContext(ctx, canvasMountSelect+` WHERE m.session_id=? AND m.resource_id=?`, session, id))
		return err
	})
	return out, err
}

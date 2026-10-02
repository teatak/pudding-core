package sqlitestore

import (
	"context"
	"database/sql"
	"errors"
	"time"

	"github.com/teatak/pudding-core/internal/store"
)

const studioMountSelect = `SELECT m.session_id,m.id,m.item_id,m.visible,m.created_at,
 w.kind,w.name,w.icon,w.icon_color,coalesce(w.source_session_id,''),w.revision,w.updated_at
 FROM studio_mounts m JOIN studio_items w ON w.id=m.item_id AND w.deleted=0`

func scanStudioMount(row messageScanner) (*store.StudioMount, error) {
	m := &store.StudioMount{}
	w := &store.StudioItem{}
	var created, updated int64

	if err := row.Scan(&m.SessionID, &m.ID, &m.ItemID, &m.Visible, &created, &w.Kind, &w.Name, &w.Icon, &w.IconColor, &w.SourceSessionID, &w.Revision, &updated); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, store.ErrNotFound
		}
		return nil, err
	}
	m.CreatedAt = timeFromMS(created)
	m.CreatedBySessionID = m.SessionID
	m.UpdatedBySessionID = m.SessionID
	w.ID = m.ItemID
	w.UpdatedAt = timeFromMS(updated)
	return store.ProjectStudioMount(m, w), nil
}

func (s *Store) ListStudioMounts(ctx context.Context, session string) ([]*store.StudioMount, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, err := s.getSessionDB(ctx, session); err != nil {
		return nil, err
	}
	rows, err := s.db.QueryContext(ctx, studioMountSelect+` WHERE m.session_id=? ORDER BY m.created_at,m.id`, session)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []*store.StudioMount{}
	for rows.Next() {
		v, err := scanStudioMount(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, v)
	}
	return out, rows.Err()
}

// Removing a session mount closes that view; the independent resource survives.
func (s *Store) DeleteStudioMount(ctx context.Context, session, mountID string) error {
	return s.tx(ctx, func(tx *sql.Tx) error {
		if _, err := getSessionTx(ctx, tx, session); err != nil {
			return err
		}
		res, err := tx.ExecContext(ctx, `DELETE FROM studio_mounts WHERE session_id=? AND id=?`, session, mountID)
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
func (s *Store) OpenStudioItem(ctx context.Context, session, itemID, mountID string) (*store.StudioMount, error) {
	var out *store.StudioMount
	err := s.tx(ctx, func(tx *sql.Tx) error {
		if _, err := getSessionTx(ctx, tx, session); err != nil {
			return err
		}
		if _, err := scanStudioItem(tx.QueryRowContext(ctx, `SELECT `+studioItemColumns+` FROM studio_items WHERE id=? AND deleted=0`, itemID)); err != nil {
			return err
		}
		if _, err := tx.ExecContext(ctx, `INSERT INTO studio_mounts(session_id,id,item_id,created_at) VALUES(?,?,?,?) ON CONFLICT(session_id,item_id) DO UPDATE SET visible=1`, session, mountID, itemID, unixMS(time.Now())); err != nil {
			return err
		}
		var err error
		out, err = scanStudioMount(tx.QueryRowContext(ctx, studioMountSelect+` WHERE m.session_id=? AND m.item_id=?`, session, itemID))
		return err
	})
	return out, err
}

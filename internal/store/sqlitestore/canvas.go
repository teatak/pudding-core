package sqlitestore

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"time"

	"github.com/teatak/pudding-core/internal/store"
)

const canvasMountSelect = `SELECT m.session_id,m.id,m.resource_id,m.window_json,m.visible,m.created_at,
 w.name,coalesce(w.source_session_id,''),w.revision,w.updated_at,coalesce(r.content_json,'')
 FROM canvas_mounts m JOIN canvas_resources w ON w.id=m.resource_id AND w.deleted=0
 LEFT JOIN canvas_revisions r ON r.workbench_id=w.id AND r.hash=CASE WHEN w.active_revision<>'' THEN w.active_revision ELSE w.head_revision END`

func scanCanvasItem(row messageScanner) (*store.CanvasItem, error) {
	m := &store.CanvasItem{CanvasID: store.DefaultCanvasID}
	w := &store.Workbench{}
	r := &store.WorkbenchRevision{}
	var created, updated int64
	var window, content string
	if err := row.Scan(&m.SessionID, &m.ID, &m.ResourceID, &window, &m.Visible, &created, &w.Name, &w.SourceSessionID, &w.Revision, &updated, &content); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, store.ErrNotFound
		}
		return nil, err
	}
	m.CreatedAt = timeFromMS(created)
	m.CreatedBySessionID = m.SessionID
	m.UpdatedBySessionID = m.SessionID
	if window != "" {
		m.Window = json.RawMessage(window)
	}
	w.ID = m.ResourceID
	w.UpdatedAt = timeFromMS(updated)
	if content != "" {
		r.Content = json.RawMessage(content)
	}
	return store.ProjectCanvasItem(m, w, r)
}
func getCanvasItemTx(ctx context.Context, tx *sql.Tx, session, id string) (*store.CanvasItem, error) {
	return scanCanvasItem(tx.QueryRowContext(ctx, canvasMountSelect+` WHERE m.session_id=? AND m.id=?`, session, id))
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
func (s *Store) PutCanvasItem(ctx context.Context, in store.CanvasItemInput) (*store.CanvasItem, error) {
	if err := store.NormalizeCanvasItemInput(&in); err != nil {
		return nil, err
	}
	content, hash, err := (store.CanvasContent{Kind: in.Kind, Title: in.Title, Item: in.Item}).Encode()
	if err != nil {
		return nil, err
	}
	var out *store.CanvasItem
	err = s.tx(ctx, func(tx *sql.Tx) error {
		if _, err := getSessionTx(ctx, tx, in.ActorSessionID); err != nil {
			return err
		}
		now := time.Now().UTC()
		id := store.NewID("canvas")
		expected := in.ExpectedRevision
		old, err := getCanvasItemTx(ctx, tx, in.ActorSessionID, in.ID)
		if err == nil {
			id = old.ResourceID
			if expected != old.Revision {
				return store.ErrWorkbenchConflict
			}
		} else if errors.Is(err, store.ErrNotFound) {
			if expected != 0 {
				return store.ErrWorkbenchConflict
			}
			_, err = tx.ExecContext(ctx, `INSERT INTO canvas_resources(id,name,source_session_id,revision,head_revision,active_revision,bindings,grants,binding_version,deleted,created_at,updated_at) VALUES(?,?,?,1,'','','{}','{}',1,0,?,?)`, id, in.Title, in.ActorSessionID, unixMS(now), unixMS(now))
			if err != nil {
				return err
			}
			_, err = tx.ExecContext(ctx, `INSERT INTO canvas_mounts(session_id,id,resource_id,window_json,visible,created_at) VALUES(?,?,?,?,1,?)`, in.ActorSessionID, in.ID, id, string(in.Window), unixMS(now))
			if err != nil {
				return err
			}
			expected = 1
		} else {
			return err
		}
		if err := saveCanvasRevisionTx(ctx, tx, &store.WorkbenchRevision{WorkbenchID: id, Hash: hash, Content: content, ClientRequestID: store.NewID("edit"), CreatedAt: now}, expected); err != nil {
			return err
		}
		out, err = getCanvasItemTx(ctx, tx, in.ActorSessionID, in.ID)
		return err
	})
	return out, err
}
func (s *Store) UpdateCanvasItemWindow(ctx context.Context, p store.CanvasItemWindowPatch) (*store.CanvasItem, error) {
	if err := store.NormalizeCanvasItemWindowPatch(&p); err != nil {
		return nil, err
	}
	var out *store.CanvasItem
	err := s.tx(ctx, func(tx *sql.Tx) error {
		if _, err := getSessionTx(ctx, tx, p.ActorSessionID); err != nil {
			return err
		}
		if _, err := tx.ExecContext(ctx, `UPDATE canvas_mounts SET window_json=? WHERE session_id=? AND id=?`, string(p.Window), p.ActorSessionID, p.ItemID); err != nil {
			return err
		}
		var err error
		out, err = getCanvasItemTx(ctx, tx, p.ActorSessionID, p.ItemID)
		return err
	})
	return out, err
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
		if _, err := scanWorkbench(tx.QueryRowContext(ctx, `SELECT `+workbenchColumns+` FROM canvas_resources WHERE id=? AND deleted=0`, id)); err != nil {
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

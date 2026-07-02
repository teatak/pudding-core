package sqlitestore

import (
	"context"
	"database/sql"
	"errors"
	"time"

	"github.com/teatak/pudding-core/internal/store"
)

const canvasItemColumns = `id,canvas_id,source_session_id,created_by_session_id,updated_by_session_id,kind,title,item_json,window_json,visible,created_at,updated_at`
const closedCanvasItemColumns = `id,source_item_id,actor_session_id,kind,title,item_json,window_json,closed_at,created_at,updated_at`

func (s *Store) ListCanvasItems(ctx context.Context, actorSessionID string) ([]*store.CanvasItem, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, err := s.getSessionDB(ctx, actorSessionID); err != nil {
		return nil, err
	}
	rows, err := s.db.QueryContext(ctx,
		`SELECT `+canvasItemColumns+` FROM canvas_items WHERE canvas_id=? AND visible=1 ORDER BY created_at ASC, id ASC`,
		store.DefaultCanvasID,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := make([]*store.CanvasItem, 0)
	for rows.Next() {
		item, err := scanCanvasItem(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, item)
	}
	return out, rows.Err()
}

func (s *Store) PutCanvasItem(ctx context.Context, in store.CanvasItemInput) (*store.CanvasItem, error) {
	if err := store.NormalizeCanvasItemInput(&in); err != nil {
		return nil, err
	}
	var out *store.CanvasItem
	err := s.tx(ctx, func(tx *sql.Tx) error {
		if _, err := getSessionTx(ctx, tx, in.ActorSessionID); err != nil {
			return err
		}
		now := time.Now()
		created := unixMS(now)
		createdBy := in.ActorSessionID
		sourceSessionID := in.SourceSessionID
		if sourceSessionID == "" {
			sourceSessionID = in.ActorSessionID
		}
		err := tx.QueryRowContext(ctx,
			`SELECT created_at,created_by_session_id,source_session_id FROM canvas_items WHERE canvas_id=? AND id=?`,
			in.CanvasID, in.ID,
		).Scan(&created, &createdBy, &sourceSessionID)
		switch {
		case err == nil:
		case errors.Is(err, sql.ErrNoRows):
		default:
			return err
		}
		_, err = tx.ExecContext(ctx,
			`INSERT INTO canvas_items(
				id,canvas_id,source_session_id,created_by_session_id,updated_by_session_id,kind,title,item_json,window_json,visible,created_at,updated_at
			 ) VALUES(?,?,?,?,?,?,?,?,?,?,?,?)
			 ON CONFLICT(id) DO UPDATE SET
			   canvas_id=excluded.canvas_id,
			   updated_by_session_id=excluded.updated_by_session_id,
			   kind=excluded.kind,
			   title=excluded.title,
			   item_json=excluded.item_json,
			   window_json=excluded.window_json,
			   visible=1,
			   updated_at=excluded.updated_at`,
			in.ID, in.CanvasID, sourceSessionID, createdBy, in.ActorSessionID, in.Kind, in.Title,
			string(in.Item), string(in.Window), 1, created, unixMS(now),
		)
		if err != nil {
			return err
		}
		out, err = getCanvasItemTx(ctx, tx, in.CanvasID, in.ID)
		return err
	})
	return out, err
}

func (s *Store) UpdateCanvasItemWindow(ctx context.Context, patch store.CanvasItemWindowPatch) (*store.CanvasItem, error) {
	if err := store.NormalizeCanvasItemWindowPatch(&patch); err != nil {
		return nil, err
	}
	var out *store.CanvasItem
	err := s.tx(ctx, func(tx *sql.Tx) error {
		if _, err := getSessionTx(ctx, tx, patch.ActorSessionID); err != nil {
			return err
		}
		now := time.Now()
		res, err := tx.ExecContext(ctx,
			`UPDATE canvas_items SET window_json=?, updated_by_session_id=?, updated_at=? WHERE canvas_id=? AND id=? AND visible=1`,
			string(patch.Window), patch.ActorSessionID, unixMS(now), patch.CanvasID, patch.ItemID,
		)
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
		out, err = getCanvasItemTx(ctx, tx, patch.CanvasID, patch.ItemID)
		return err
	})
	return out, err
}

func (s *Store) DeleteCanvasItem(ctx context.Context, actorSessionID, itemID string) error {
	return s.tx(ctx, func(tx *sql.Tx) error {
		if _, err := getSessionTx(ctx, tx, actorSessionID); err != nil {
			return err
		}
		res, err := tx.ExecContext(ctx, `DELETE FROM canvas_items WHERE canvas_id=? AND id=?`, store.DefaultCanvasID, itemID)
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

func (s *Store) ListClosedCanvasItems(ctx context.Context, actorSessionID string, limit int) ([]*store.ClosedCanvasItem, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, err := s.getSessionDB(ctx, actorSessionID); err != nil {
		return nil, err
	}
	limit = normalizeClosedCanvasLimit(limit)
	rows, err := s.db.QueryContext(ctx,
		`SELECT `+closedCanvasItemColumns+` FROM canvas_closed_items ORDER BY closed_at DESC, created_at DESC LIMIT ?`,
		limit,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := make([]*store.ClosedCanvasItem, 0)
	for rows.Next() {
		item, err := scanClosedCanvasItem(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, item)
	}
	return out, rows.Err()
}

func (s *Store) PutClosedCanvasItem(ctx context.Context, in store.ClosedCanvasItemInput, keepLimit int) (*store.ClosedCanvasItem, error) {
	if err := store.NormalizeClosedCanvasItemInput(&in); err != nil {
		return nil, err
	}
	var out *store.ClosedCanvasItem
	err := s.tx(ctx, func(tx *sql.Tx) error {
		if _, err := getSessionTx(ctx, tx, in.ActorSessionID); err != nil {
			return err
		}
		now := time.Now()
		created := unixMS(now)
		err := tx.QueryRowContext(ctx,
			`SELECT created_at FROM canvas_closed_items WHERE source_item_id=?`,
			in.SourceItemID,
		).Scan(&created)
		switch {
		case err == nil:
		case errors.Is(err, sql.ErrNoRows):
		default:
			return err
		}
		_, err = tx.ExecContext(ctx,
			`INSERT INTO canvas_closed_items(
				id,source_item_id,actor_session_id,kind,title,item_json,window_json,closed_at,created_at,updated_at
			 ) VALUES(?,?,?,?,?,?,?,?,?,?)
			 ON CONFLICT(source_item_id) DO UPDATE SET
			   id=excluded.id,
			   actor_session_id=excluded.actor_session_id,
			   kind=excluded.kind,
			   title=excluded.title,
			   item_json=excluded.item_json,
			   window_json=excluded.window_json,
			   closed_at=excluded.closed_at,
			   updated_at=excluded.updated_at`,
			in.ID, in.SourceItemID, in.ActorSessionID, in.Kind, in.Title, string(in.Item), string(in.Window),
			unixMS(in.ClosedAt), created, unixMS(now),
		)
		if err != nil {
			return err
		}
		if err := trimClosedCanvasItemsTx(ctx, tx, normalizeClosedCanvasKeepLimit(keepLimit)); err != nil {
			return err
		}
		out, err = getClosedCanvasItemTx(ctx, tx, in.ID)
		return err
	})
	return out, err
}

func (s *Store) DeleteClosedCanvasItem(ctx context.Context, actorSessionID, id string) error {
	return s.tx(ctx, func(tx *sql.Tx) error {
		if _, err := getSessionTx(ctx, tx, actorSessionID); err != nil {
			return err
		}
		res, err := tx.ExecContext(ctx, `DELETE FROM canvas_closed_items WHERE id=?`, id)
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

func (s *Store) ClearClosedCanvasItems(ctx context.Context, actorSessionID string) error {
	return s.tx(ctx, func(tx *sql.Tx) error {
		if _, err := getSessionTx(ctx, tx, actorSessionID); err != nil {
			return err
		}
		_, err := tx.ExecContext(ctx, `DELETE FROM canvas_closed_items`)
		return err
	})
}

func getCanvasItemTx(ctx context.Context, tx *sql.Tx, canvasID, itemID string) (*store.CanvasItem, error) {
	item, err := scanCanvasItem(tx.QueryRowContext(ctx,
		`SELECT `+canvasItemColumns+` FROM canvas_items WHERE canvas_id=? AND id=?`,
		canvasID, itemID,
	))
	if errors.Is(err, sql.ErrNoRows) {
		return nil, store.ErrNotFound
	}
	return item, err
}

func scanCanvasItem(row messageScanner) (*store.CanvasItem, error) {
	var item store.CanvasItem
	var itemJSON, windowJSON string
	var visible int
	var created, updated int64
	if err := row.Scan(
		&item.ID,
		&item.CanvasID,
		&item.SourceSessionID,
		&item.CreatedBySessionID,
		&item.UpdatedBySessionID,
		&item.Kind,
		&item.Title,
		&itemJSON,
		&windowJSON,
		&visible,
		&created,
		&updated,
	); err != nil {
		return nil, err
	}
	item.Item = []byte(itemJSON)
	if windowJSON != "" {
		item.Window = []byte(windowJSON)
	}
	item.Visible = visible != 0
	item.CreatedAt = timeFromMS(created)
	item.UpdatedAt = timeFromMS(updated)
	return &item, nil
}

func getClosedCanvasItemTx(ctx context.Context, tx *sql.Tx, id string) (*store.ClosedCanvasItem, error) {
	item, err := scanClosedCanvasItem(tx.QueryRowContext(ctx,
		`SELECT `+closedCanvasItemColumns+` FROM canvas_closed_items WHERE id=?`,
		id,
	))
	if errors.Is(err, sql.ErrNoRows) {
		return nil, store.ErrNotFound
	}
	return item, err
}

func scanClosedCanvasItem(row messageScanner) (*store.ClosedCanvasItem, error) {
	var item store.ClosedCanvasItem
	var itemJSON, windowJSON string
	var closed, created, updated int64
	if err := row.Scan(
		&item.ID,
		&item.SourceItemID,
		&item.ActorSessionID,
		&item.Kind,
		&item.Title,
		&itemJSON,
		&windowJSON,
		&closed,
		&created,
		&updated,
	); err != nil {
		return nil, err
	}
	item.Item = []byte(itemJSON)
	if windowJSON != "" {
		item.Window = []byte(windowJSON)
	}
	item.ClosedAt = timeFromMS(closed)
	item.CreatedAt = timeFromMS(created)
	item.UpdatedAt = timeFromMS(updated)
	return &item, nil
}

func trimClosedCanvasItemsTx(ctx context.Context, tx *sql.Tx, limit int) error {
	_, err := tx.ExecContext(ctx,
		`DELETE FROM canvas_closed_items
		 WHERE id NOT IN (
		   SELECT id FROM canvas_closed_items ORDER BY closed_at DESC, created_at DESC LIMIT ?
		 )`,
		limit,
	)
	return err
}

func normalizeClosedCanvasLimit(limit int) int {
	if limit <= 0 {
		return store.ClosedCanvasDefaultLimit
	}
	if limit > store.ClosedCanvasMaxLimit {
		return store.ClosedCanvasMaxLimit
	}
	return limit
}

func normalizeClosedCanvasKeepLimit(limit int) int {
	if limit <= 0 {
		return store.ClosedCanvasKeepLimit
	}
	return limit
}

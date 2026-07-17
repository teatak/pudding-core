package sqlitestore

import (
	"bytes"
	"context"
	"database/sql"
	"errors"
	"strings"
	"time"

	"github.com/teatak/pudding-core/internal/store"
)

const canvasItemColumns = `session_id,id,canvas_id,source_session_id,created_by_session_id,updated_by_session_id,kind,title,item_json,window_json,source_saved_item_id,base_saved_revision,saved_dirty,visible,created_at,updated_at`
const closedCanvasItemColumns = `session_id,id,source_item_id,actor_session_id,kind,title,item_json,window_json,closed_at,created_at,updated_at`
const savedCanvasItemColumns = `id,source_session_id,source_item_id,kind,title,item_json,window_json,revision,created_at,updated_at`

func (s *Store) ListCanvasItems(ctx context.Context, actorSessionID string) ([]*store.CanvasItem, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, err := s.getSessionDB(ctx, actorSessionID); err != nil {
		return nil, err
	}
	rows, err := s.db.QueryContext(ctx,
		`SELECT `+canvasItemColumns+` FROM canvas_items WHERE session_id=? AND visible=1 ORDER BY created_at ASC, id ASC`,
		actorSessionID,
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
		sourceSavedItemID := in.SourceSavedItemID
		baseSavedRevision := in.BaseSavedRevision
		savedDirty := false
		existing, err := getCanvasItemTx(ctx, tx, in.ActorSessionID, in.ID)
		switch {
		case err == nil:
			created = unixMS(existing.CreatedAt)
			createdBy = existing.CreatedBySessionID
			sourceSessionID = existing.SourceSessionID
			sourceSavedItemID = existing.SourceSavedItemID
			baseSavedRevision = existing.BaseSavedRevision
			savedDirty = existing.SavedDirty || (sourceSavedItemID != "" && (existing.Kind != in.Kind || existing.Title != in.Title ||
				!bytes.Equal(existing.Item, in.Item) || !bytes.Equal(existing.Window, in.Window)))
		case errors.Is(err, store.ErrNotFound):
		case err != nil:
			return err
		}
		_, err = tx.ExecContext(ctx,
			`INSERT INTO canvas_items(
				session_id,id,canvas_id,source_session_id,created_by_session_id,updated_by_session_id,kind,title,item_json,window_json,
				source_saved_item_id,base_saved_revision,saved_dirty,visible,created_at,updated_at
			 ) VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)
			 ON CONFLICT(session_id,id) DO UPDATE SET
			   canvas_id=excluded.canvas_id,
			   updated_by_session_id=excluded.updated_by_session_id,
			   kind=excluded.kind,
			   title=excluded.title,
			   item_json=excluded.item_json,
			   window_json=excluded.window_json,
			   source_saved_item_id=excluded.source_saved_item_id,
			   base_saved_revision=excluded.base_saved_revision,
			   saved_dirty=excluded.saved_dirty,
			   visible=1,
			   updated_at=excluded.updated_at`,
			in.ActorSessionID, in.ID, in.CanvasID, sourceSessionID, createdBy, in.ActorSessionID, in.Kind, in.Title,
			string(in.Item), string(in.Window), sourceSavedItemID, baseSavedRevision, boolInt(savedDirty), 1, created, unixMS(now),
		)
		if err != nil {
			return err
		}
		out, err = getCanvasItemTx(ctx, tx, in.ActorSessionID, in.ID)
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
			`UPDATE canvas_items SET window_json=?, updated_by_session_id=?, saved_dirty=CASE WHEN source_saved_item_id<>'' THEN 1 ELSE saved_dirty END, updated_at=? WHERE session_id=? AND id=? AND visible=1`,
			string(patch.Window), patch.ActorSessionID, unixMS(now), patch.ActorSessionID, patch.ItemID,
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
		out, err = getCanvasItemTx(ctx, tx, patch.ActorSessionID, patch.ItemID)
		return err
	})
	return out, err
}

func (s *Store) DeleteCanvasItem(ctx context.Context, actorSessionID, itemID string) error {
	return s.tx(ctx, func(tx *sql.Tx) error {
		if _, err := getSessionTx(ctx, tx, actorSessionID); err != nil {
			return err
		}
		res, err := tx.ExecContext(ctx, `DELETE FROM canvas_items WHERE session_id=? AND id=?`, actorSessionID, itemID)
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

func (s *Store) ListSavedCanvasItems(ctx context.Context, actorSessionID string) ([]*store.SavedCanvasItem, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, err := s.getSessionDB(ctx, actorSessionID); err != nil {
		return nil, err
	}
	rows, err := s.db.QueryContext(ctx, `SELECT `+savedCanvasItemColumns+` FROM canvas_saved_items ORDER BY updated_at DESC,id ASC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := make([]*store.SavedCanvasItem, 0)
	for rows.Next() {
		item, err := scanSavedCanvasItem(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, item)
	}
	return out, rows.Err()
}

func (s *Store) SaveCanvasItem(ctx context.Context, actorSessionID, itemID, savedItemID string) (*store.CanvasSaveResult, error) {
	actorSessionID = strings.TrimSpace(actorSessionID)
	itemID = strings.TrimSpace(itemID)
	savedItemID = strings.TrimSpace(savedItemID)
	if actorSessionID == "" || itemID == "" {
		return nil, store.ErrInvalidCanvas
	}
	var out *store.CanvasSaveResult
	err := s.tx(ctx, func(tx *sql.Tx) error {
		if _, err := getSessionTx(ctx, tx, actorSessionID); err != nil {
			return err
		}
		item, err := getCanvasItemTx(ctx, tx, actorSessionID, itemID)
		if err != nil {
			return err
		}
		now := unixMS(time.Now())
		targetID := item.SourceSavedItemID
		if targetID == "" {
			if savedItemID == "" {
				return store.ErrInvalidCanvas
			}
			targetID = savedItemID
			_, err = tx.ExecContext(ctx,
				`INSERT INTO canvas_saved_items(id,source_session_id,source_item_id,kind,title,item_json,window_json,revision,created_at,updated_at) VALUES(?,?,?,?,?,?,?,?,?,?)`,
				targetID, actorSessionID, item.ID, item.Kind, item.Title, string(item.Item), string(item.Window), 1, now, now,
			)
			if err != nil {
				return err
			}
		} else if item.SavedDirty {
			res, err := tx.ExecContext(ctx,
				`UPDATE canvas_saved_items SET kind=?,title=?,item_json=?,window_json=?,revision=revision+1,updated_at=? WHERE id=? AND revision=?`,
				item.Kind, item.Title, string(item.Item), string(item.Window), now, targetID, item.BaseSavedRevision,
			)
			if err != nil {
				return err
			}
			n, err := res.RowsAffected()
			if err != nil {
				return err
			}
			if n == 0 {
				return store.ErrCanvasConflict
			}
		}
		saved, err := getSavedCanvasItemTx(ctx, tx, targetID)
		if err != nil {
			return err
		}
		if _, err := tx.ExecContext(ctx,
			`UPDATE canvas_items SET source_saved_item_id=?,base_saved_revision=?,saved_dirty=0,updated_at=? WHERE session_id=? AND id=?`,
			targetID, saved.Revision, now, actorSessionID, itemID,
		); err != nil {
			return err
		}
		item, err = getCanvasItemTx(ctx, tx, actorSessionID, itemID)
		if err != nil {
			return err
		}
		out = &store.CanvasSaveResult{Item: item, SavedItem: saved}
		return nil
	})
	return out, err
}

func (s *Store) OpenSavedCanvasItem(ctx context.Context, actorSessionID, savedItemID, itemID string) (*store.CanvasItem, error) {
	actorSessionID = strings.TrimSpace(actorSessionID)
	savedItemID = strings.TrimSpace(savedItemID)
	itemID = strings.TrimSpace(itemID)
	if actorSessionID == "" || savedItemID == "" || itemID == "" {
		return nil, store.ErrInvalidCanvas
	}
	var out *store.CanvasItem
	err := s.tx(ctx, func(tx *sql.Tx) error {
		if _, err := getSessionTx(ctx, tx, actorSessionID); err != nil {
			return err
		}
		existing, err := scanCanvasItem(tx.QueryRowContext(ctx,
			`SELECT `+canvasItemColumns+` FROM canvas_items WHERE session_id=? AND source_saved_item_id=?`, actorSessionID, savedItemID,
		))
		if err == nil {
			out = existing
			return nil
		}
		if !errors.Is(err, sql.ErrNoRows) {
			return err
		}
		saved, err := getSavedCanvasItemTx(ctx, tx, savedItemID)
		if err != nil {
			return err
		}
		now := unixMS(time.Now())
		_, err = tx.ExecContext(ctx,
			`INSERT INTO canvas_items(session_id,id,canvas_id,source_session_id,created_by_session_id,updated_by_session_id,kind,title,item_json,window_json,source_saved_item_id,base_saved_revision,saved_dirty,visible,created_at,updated_at) VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)`,
			actorSessionID, itemID, store.DefaultCanvasID, actorSessionID, actorSessionID, actorSessionID,
			saved.Kind, saved.Title, string(saved.Item), string(saved.Window), saved.ID, saved.Revision, 0, 1, now, now,
		)
		if err != nil {
			return err
		}
		out, err = getCanvasItemTx(ctx, tx, actorSessionID, itemID)
		return err
	})
	return out, err
}

func (s *Store) DeleteSavedCanvasItem(ctx context.Context, actorSessionID, savedItemID string) error {
	actorSessionID = strings.TrimSpace(actorSessionID)
	savedItemID = strings.TrimSpace(savedItemID)
	if actorSessionID == "" || savedItemID == "" {
		return store.ErrInvalidCanvas
	}
	return s.tx(ctx, func(tx *sql.Tx) error {
		if _, err := getSessionTx(ctx, tx, actorSessionID); err != nil {
			return err
		}
		if _, err := tx.ExecContext(ctx, `UPDATE canvas_items SET source_saved_item_id='',base_saved_revision=0,saved_dirty=0 WHERE source_saved_item_id=?`, savedItemID); err != nil {
			return err
		}
		res, err := tx.ExecContext(ctx, `DELETE FROM canvas_saved_items WHERE id=?`, savedItemID)
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
		`SELECT `+closedCanvasItemColumns+` FROM canvas_closed_items WHERE session_id=? ORDER BY closed_at DESC, created_at DESC LIMIT ?`,
		actorSessionID, limit,
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
			`SELECT created_at FROM canvas_closed_items WHERE session_id=? AND source_item_id=?`,
			in.ActorSessionID, in.SourceItemID,
		).Scan(&created)
		switch {
		case err == nil:
		case errors.Is(err, sql.ErrNoRows):
		default:
			return err
		}
		_, err = tx.ExecContext(ctx,
			`INSERT INTO canvas_closed_items(
				session_id,id,source_item_id,actor_session_id,kind,title,item_json,window_json,closed_at,created_at,updated_at
			 ) VALUES(?,?,?,?,?,?,?,?,?,?,?)
			 ON CONFLICT(session_id,source_item_id) DO UPDATE SET
			   id=excluded.id,
			   actor_session_id=excluded.actor_session_id,
			   kind=excluded.kind,
			   title=excluded.title,
			   item_json=excluded.item_json,
			   window_json=excluded.window_json,
			   closed_at=excluded.closed_at,
			   updated_at=excluded.updated_at`,
			in.ActorSessionID, in.ID, in.SourceItemID, in.ActorSessionID, in.Kind, in.Title, string(in.Item), string(in.Window),
			unixMS(in.ClosedAt), created, unixMS(now),
		)
		if err != nil {
			return err
		}
		if err := trimClosedCanvasItemsTx(ctx, tx, in.ActorSessionID, normalizeClosedCanvasKeepLimit(keepLimit)); err != nil {
			return err
		}
		out, err = getClosedCanvasItemTx(ctx, tx, in.ActorSessionID, in.ID)
		return err
	})
	return out, err
}

func (s *Store) DeleteClosedCanvasItem(ctx context.Context, actorSessionID, id string) error {
	return s.tx(ctx, func(tx *sql.Tx) error {
		if _, err := getSessionTx(ctx, tx, actorSessionID); err != nil {
			return err
		}
		res, err := tx.ExecContext(ctx, `DELETE FROM canvas_closed_items WHERE session_id=? AND id=?`, actorSessionID, id)
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
		_, err := tx.ExecContext(ctx, `DELETE FROM canvas_closed_items WHERE session_id=?`, actorSessionID)
		return err
	})
}

func getCanvasItemTx(ctx context.Context, tx *sql.Tx, sessionID, itemID string) (*store.CanvasItem, error) {
	item, err := scanCanvasItem(tx.QueryRowContext(ctx,
		`SELECT `+canvasItemColumns+` FROM canvas_items WHERE session_id=? AND id=?`,
		sessionID, itemID,
	))
	if errors.Is(err, sql.ErrNoRows) {
		return nil, store.ErrNotFound
	}
	return item, err
}

func scanCanvasItem(row messageScanner) (*store.CanvasItem, error) {
	var item store.CanvasItem
	var itemJSON, windowJSON string
	var savedDirty, visible int
	var created, updated int64
	if err := row.Scan(
		&item.SessionID,
		&item.ID,
		&item.CanvasID,
		&item.SourceSessionID,
		&item.CreatedBySessionID,
		&item.UpdatedBySessionID,
		&item.Kind,
		&item.Title,
		&itemJSON,
		&windowJSON,
		&item.SourceSavedItemID,
		&item.BaseSavedRevision,
		&savedDirty,
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
	item.SavedDirty = savedDirty != 0
	item.CreatedAt = timeFromMS(created)
	item.UpdatedAt = timeFromMS(updated)
	return &item, nil
}

func getSavedCanvasItemTx(ctx context.Context, tx *sql.Tx, id string) (*store.SavedCanvasItem, error) {
	item, err := scanSavedCanvasItem(tx.QueryRowContext(ctx,
		`SELECT `+savedCanvasItemColumns+` FROM canvas_saved_items WHERE id=?`, id,
	))
	if errors.Is(err, sql.ErrNoRows) {
		return nil, store.ErrNotFound
	}
	return item, err
}

func scanSavedCanvasItem(row messageScanner) (*store.SavedCanvasItem, error) {
	var item store.SavedCanvasItem
	var itemJSON, windowJSON string
	var created, updated int64
	if err := row.Scan(
		&item.ID,
		&item.SourceSessionID,
		&item.SourceItemID,
		&item.Kind,
		&item.Title,
		&itemJSON,
		&windowJSON,
		&item.Revision,
		&created,
		&updated,
	); err != nil {
		return nil, err
	}
	item.Item = []byte(itemJSON)
	if windowJSON != "" {
		item.Window = []byte(windowJSON)
	}
	item.CreatedAt = timeFromMS(created)
	item.UpdatedAt = timeFromMS(updated)
	return &item, nil
}

func getClosedCanvasItemTx(ctx context.Context, tx *sql.Tx, sessionID, id string) (*store.ClosedCanvasItem, error) {
	item, err := scanClosedCanvasItem(tx.QueryRowContext(ctx,
		`SELECT `+closedCanvasItemColumns+` FROM canvas_closed_items WHERE session_id=? AND id=?`,
		sessionID, id,
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
		&item.SessionID,
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

func trimClosedCanvasItemsTx(ctx context.Context, tx *sql.Tx, sessionID string, limit int) error {
	_, err := tx.ExecContext(ctx,
		`DELETE FROM canvas_closed_items
		 WHERE session_id=? AND id NOT IN (
		   SELECT id FROM canvas_closed_items WHERE session_id=? ORDER BY closed_at DESC, created_at DESC LIMIT ?
		 )`,
		sessionID, sessionID, limit,
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

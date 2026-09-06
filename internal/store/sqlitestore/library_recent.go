package sqlitestore

import (
	"context"
	"database/sql"
	"path/filepath"
	"time"

	"github.com/teatak/pudding-core/internal/store"
)

func (s *Store) ListLibraryRecentOpens(ctx context.Context, actor string) ([]*store.LibraryRecentOpen, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, err := s.getSessionDB(ctx, actor); err != nil {
		return nil, err
	}
	rows, err := s.db.QueryContext(ctx, `SELECT r.id,r.kind,r.source_session_id,COALESCE(r.canvas_item_id,''),r.root_path,r.path,COALESCE(c.title,''),COALESCE(c.kind,''),r.opened_at
 FROM library_recent_opens r LEFT JOIN canvas_items c ON c.session_id=r.source_session_id AND c.id=r.canvas_item_id ORDER BY r.opened_at DESC,r.id DESC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := make([]*store.LibraryRecentOpen, 0)
	for rows.Next() {
		var e store.LibraryRecentOpen
		var opened int64
		if err := rows.Scan(&e.ID, &e.Kind, &e.SourceSessionID, &e.ItemID, &e.RootPath, &e.Path, &e.Title, &e.CanvasKind, &opened); err != nil {
			return nil, err
		}
		e.OpenedAt = timeFromMS(opened)
		if e.Kind == "file" {
			e.Title = filepath.Base(e.Path)
		}
		out = append(out, &e)
	}
	return out, rows.Err()
}

func (s *Store) RecordLibraryRecentOpen(ctx context.Context, actor string, e store.LibraryRecentOpen) error {
	if err := store.ValidateLibraryRecentOpen(e); err != nil {
		return err
	}
	return s.tx(ctx, func(tx *sql.Tx) error {
		if _, err := getSessionTx(ctx, tx, actor); err != nil {
			return err
		}
		var itemID any
		if e.Kind == "canvas" {
			if _, err := getCanvasItemTx(ctx, tx, actor, e.ItemID); err != nil {
				return err
			}
			itemID = e.ItemID
		}
		_, err := tx.ExecContext(ctx, `INSERT INTO library_recent_opens(id,kind,source_session_id,canvas_item_id,root_path,path,opened_at) VALUES(?,?,?,?,?,?,?)
   ON CONFLICT DO UPDATE SET source_session_id=excluded.source_session_id,opened_at=excluded.opened_at`, store.NewID("recent"), e.Kind, actor, itemID, e.RootPath, e.Path, unixMS(time.Now()))
		if err != nil {
			return err
		}
		_, err = tx.ExecContext(ctx, `DELETE FROM library_recent_opens WHERE id IN (SELECT id FROM library_recent_opens ORDER BY opened_at DESC,id DESC LIMIT -1 OFFSET ?)`, store.LibraryRecentRetainLimit)
		return err
	})
}

func (s *Store) DeleteLibraryRecentOpen(ctx context.Context, actor, kind, id string) error {
	return s.tx(ctx, func(tx *sql.Tx) error {
		if _, err := getSessionTx(ctx, tx, actor); err != nil {
			return err
		}
		var err error
		if kind == "web" {
			_, err = tx.ExecContext(ctx, `DELETE FROM browser_history WHERE id=?`, id)
		} else {
			_, err = tx.ExecContext(ctx, `DELETE FROM library_recent_opens WHERE id=? AND kind=?`, id, kind)
		}
		return err
	})
}

// Clearing the combined history is atomic; content and favorites are independent.
func (s *Store) ClearLibraryRecentOpens(ctx context.Context, actor, kind string) error {
	return s.tx(ctx, func(tx *sql.Tx) error {
		if _, err := getSessionTx(ctx, tx, actor); err != nil {
			return err
		}
		if kind == "" || kind == "web" {
			if _, err := tx.ExecContext(ctx, `DELETE FROM browser_history`); err != nil {
				return err
			}
		}
		_, err := tx.ExecContext(ctx, `DELETE FROM library_recent_opens WHERE ?='' OR kind=?`, kind, kind)
		return err
	})
}

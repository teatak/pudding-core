package sqlitestore

import (
	"context"
	"database/sql"
	"time"

	"github.com/teatak/pudding-core/internal/store"
)

func (s *Store) ListLibraryFavorites(ctx context.Context, actorSessionID string) ([]*store.LibraryFavorite, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, err := s.getSessionDB(ctx, actorSessionID); err != nil {
		return nil, err
	}
	rows, err := s.db.QueryContext(ctx, `SELECT id,kind,source_session_id,COALESCE(saved_item_id,''),url,title,created_at FROM library_favorites ORDER BY created_at DESC,id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := make([]*store.LibraryFavorite, 0)
	for rows.Next() {
		var f store.LibraryFavorite
		var created int64
		if err := rows.Scan(&f.ID, &f.Kind, &f.SourceSessionID, &f.SavedItemID, &f.URL, &f.Title, &created); err != nil {
			return nil, err
		}
		f.CreatedAt = timeFromMS(created)
		out = append(out, &f)
	}
	return out, rows.Err()
}
func (s *Store) PutLibraryFavorite(ctx context.Context, actorSessionID string, f store.LibraryFavorite) error {
	return s.tx(ctx, func(tx *sql.Tx) error {
		if _, err := getSessionTx(ctx, tx, actorSessionID); err != nil {
			return err
		}
		var savedID any
		if f.Kind == "canvas" {
			if _, err := getSavedCanvasItemTx(ctx, tx, f.SavedItemID); err != nil {
				return err
			}
			savedID = f.SavedItemID
		}
		_, err := tx.ExecContext(ctx, `INSERT INTO library_favorites(id,kind,source_session_id,saved_item_id,url,title,created_at) VALUES(?,?,?,?,?,?,?) ON CONFLICT DO NOTHING`, f.ID, f.Kind, f.SourceSessionID, savedID, f.URL, f.Title, unixMS(time.Now()))
		return err
	})
}
func (s *Store) DeleteLibraryFavorite(ctx context.Context, actorSessionID, id string) error {
	return s.tx(ctx, func(tx *sql.Tx) error {
		if _, err := getSessionTx(ctx, tx, actorSessionID); err != nil {
			return err
		}
		_, err := tx.ExecContext(ctx, `DELETE FROM library_favorites WHERE id=?`, id)
		return err
	})
}

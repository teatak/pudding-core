package sqlitestore

import (
	"context"
	"database/sql"
	"strings"
	"time"

	"github.com/teatak/pudding-core/internal/store"
)

func (s *Store) ListLibrarySources(ctx context.Context, sessionIDs []string) (map[string]store.LibrarySourceInfo, error) {
	seen := make(map[string]struct{}, len(sessionIDs))
	ids := make([]string, 0, len(sessionIDs))
	for _, id := range sessionIDs {
		if id == "" {
			continue
		}
		if _, ok := seen[id]; ok {
			continue
		}
		seen[id] = struct{}{}
		ids = append(ids, id)
	}
	out := make(map[string]store.LibrarySourceInfo, len(ids))
	if len(ids) == 0 {
		return out, nil
	}

	defer s.readLock()()
	// 分批 IN 联表查询，消除 /library 接口按来源会话逐个加锁查库导致的 N+1 开销。
	for start := 0; start < len(ids); start += 500 {
		end := min(start+500, len(ids))
		batch := ids[start:end]
		args := make([]any, 0, len(batch))
		for _, id := range batch {
			args = append(args, id)
		}
		placeholders := strings.TrimSuffix(strings.Repeat("?,", len(batch)), ",")
		rows, err := s.readDB.QueryContext(ctx,
			`SELECT s.id,s.title,COALESCE(p.name,'') FROM sessions s LEFT JOIN projects p ON p.id=s.project_id WHERE s.id IN (`+placeholders+`)`,
			args...,
		)
		if err != nil {
			return nil, err
		}
		for rows.Next() {
			var id, title, projectName string
			if scanErr := rows.Scan(&id, &title, &projectName); scanErr != nil {
				_ = rows.Close()
				return nil, scanErr
			}
			out[id] = store.LibrarySourceInfo{SessionTitle: title, ProjectName: projectName}
		}
		if rowErr := rows.Err(); rowErr != nil {
			_ = rows.Close()
			return nil, rowErr
		}
		if closeErr := rows.Close(); closeErr != nil {
			return nil, closeErr
		}
	}
	return out, nil
}

func (s *Store) ListLibraryFavorites(ctx context.Context, actorSessionID string) ([]*store.LibraryFavorite, error) {
	out := make([]*store.LibraryFavorite, 0)
	err := s.readTx(ctx, func(ctx context.Context, tx *sql.Tx) error {
		if _, err := getSessionTx(ctx, tx, actorSessionID); err != nil {
			return err
		}
		rows, err := tx.QueryContext(ctx, `SELECT id,kind,source_session_id,COALESCE(saved_item_id,''),url,title,created_at FROM library_favorites ORDER BY created_at DESC,id`)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			var f store.LibraryFavorite
			var created int64
			if err := rows.Scan(&f.ID, &f.Kind, &f.SourceSessionID, &f.SavedItemID, &f.URL, &f.Title, &created); err != nil {
				return err
			}
			f.CreatedAt = timeFromMS(created)
			out = append(out, &f)
		}
		return rows.Err()
	})
	if err != nil {
		return nil, err
	}
	return out, nil
}
func (s *Store) PutLibraryFavorite(ctx context.Context, actorSessionID string, f store.LibraryFavorite) error {
	return s.tx(ctx, func(ctx context.Context, tx *sql.Tx) error {
		if _, err := getSessionTx(ctx, tx, actorSessionID); err != nil {
			return err
		}
		var savedID any
		if f.Kind == "studio" {
			if _, err := scanStudioItem(tx.QueryRowContext(ctx, `SELECT `+studioItemColumns+` FROM studio_items WHERE id=? AND deleted=0 AND archived_at=0`, f.SavedItemID)); err != nil {
				return err
			}
			savedID = f.SavedItemID
		}
		_, err := tx.ExecContext(ctx, `INSERT INTO library_favorites(id,kind,source_session_id,saved_item_id,url,title,created_at) VALUES(?,?,?,?,?,?,?) ON CONFLICT DO NOTHING`, f.ID, f.Kind, f.SourceSessionID, savedID, f.URL, f.Title, unixMS(time.Now()))
		return err
	})
}
func (s *Store) DeleteLibraryFavorite(ctx context.Context, actorSessionID, id string) error {
	return s.tx(ctx, func(ctx context.Context, tx *sql.Tx) error {
		if _, err := getSessionTx(ctx, tx, actorSessionID); err != nil {
			return err
		}
		_, err := tx.ExecContext(ctx, `DELETE FROM library_favorites WHERE id=?`, id)
		return err
	})
}

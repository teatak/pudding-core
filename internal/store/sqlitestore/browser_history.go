package sqlitestore

import (
	"context"
	"database/sql"
	"errors"
	"strings"
	"time"

	"github.com/teatak/pudding-core/internal/store"
)

const browserHistoryColumns = `id,url,title,favicon_url,visited_at,created_at,updated_at`

func (s *Store) ListBrowserHistory(ctx context.Context, query string, limit int) ([]*store.BrowserHistoryEntry, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	query = strings.TrimSpace(query)
	limit = store.NormalizeBrowserHistoryLimit(limit)
	statement := `SELECT ` + browserHistoryColumns + ` FROM browser_history ORDER BY visited_at DESC, id DESC`
	args := make([]any, 0, 1)
	if query == "" {
		statement += ` LIMIT ?`
		args = append(args, limit)
	}
	rows, err := s.db.QueryContext(ctx, statement, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := make([]*store.BrowserHistoryEntry, 0)
	for rows.Next() {
		entry, err := scanBrowserHistory(rows)
		if err != nil {
			return nil, err
		}
		if !store.BrowserHistoryMatches(entry, query) {
			continue
		}
		out = append(out, entry)
		if len(out) == limit {
			break
		}
	}
	return out, rows.Err()
}

func (s *Store) PutBrowserHistory(ctx context.Context, in store.BrowserHistoryInput) (*store.BrowserHistoryEntry, error) {
	if err := store.NormalizeBrowserHistoryInput(&in); err != nil {
		return nil, err
	}
	var out *store.BrowserHistoryEntry
	err := s.tx(ctx, func(tx *sql.Tx) error {
		now := time.Now().UTC()
		id := store.NewID("history")
		created := unixMS(now)
		err := tx.QueryRowContext(ctx, `SELECT id,created_at FROM browser_history WHERE url=?`, in.URL).Scan(&id, &created)
		switch {
		case err == nil:
		case errors.Is(err, sql.ErrNoRows):
		default:
			return err
		}
		_, err = tx.ExecContext(ctx,
			`INSERT INTO browser_history(id,url,title,favicon_url,visited_at,created_at,updated_at)
			 VALUES(?,?,?,?,?,?,?)
			 ON CONFLICT(url) DO UPDATE SET
			   title=CASE WHEN excluded.title<>'' THEN excluded.title ELSE browser_history.title END,
			   favicon_url=CASE WHEN excluded.favicon_url<>'' THEN excluded.favicon_url ELSE browser_history.favicon_url END,
			   visited_at=excluded.visited_at,
			   updated_at=excluded.updated_at`,
			id, in.URL, in.Title, in.FaviconURL, unixMS(in.VisitedAt), created, unixMS(now),
		)
		if err != nil {
			return err
		}
		if _, err := tx.ExecContext(ctx,
			`DELETE FROM browser_history WHERE id IN (
			   SELECT id FROM browser_history ORDER BY visited_at DESC,id DESC LIMIT -1 OFFSET ?
			 )`,
			store.BrowserHistoryRetainLimit,
		); err != nil {
			return err
		}
		out, err = getBrowserHistoryTx(ctx, tx, id)
		return err
	})
	return out, err
}

func (s *Store) UpdateBrowserHistoryMetadata(ctx context.Context, in store.BrowserHistoryInput) error {
	if err := store.NormalizeBrowserHistoryInput(&in); err != nil {
		return err
	}
	return s.tx(ctx, func(tx *sql.Tx) error {
		_, err := tx.ExecContext(ctx,
			`UPDATE browser_history SET
			   title=CASE WHEN ?<>'' THEN ? ELSE title END,
			   favicon_url=CASE WHEN ?<>'' THEN ? ELSE favicon_url END,
			   updated_at=?
			 WHERE url=?`,
			in.Title, in.Title, in.FaviconURL, in.FaviconURL, unixMS(time.Now().UTC()), in.URL,
		)
		return err
	})
}

func (s *Store) DeleteBrowserHistory(ctx context.Context, historyID string) error {
	historyID = strings.TrimSpace(historyID)
	if historyID == "" {
		return store.ErrInvalidBrowserHistory
	}
	return s.tx(ctx, func(tx *sql.Tx) error {
		result, err := tx.ExecContext(ctx, `DELETE FROM browser_history WHERE id=?`, historyID)
		if err != nil {
			return err
		}
		deleted, err := result.RowsAffected()
		if err != nil {
			return err
		}
		if deleted == 0 {
			return store.ErrNotFound
		}
		return nil
	})
}

func (s *Store) ClearBrowserHistory(ctx context.Context) error {
	return s.tx(ctx, func(tx *sql.Tx) error {
		_, err := tx.ExecContext(ctx, `DELETE FROM browser_history`)
		return err
	})
}

func getBrowserHistoryTx(ctx context.Context, tx *sql.Tx, id string) (*store.BrowserHistoryEntry, error) {
	entry, err := scanBrowserHistory(tx.QueryRowContext(ctx,
		`SELECT `+browserHistoryColumns+` FROM browser_history WHERE id=?`, id,
	))
	if errors.Is(err, sql.ErrNoRows) {
		return nil, store.ErrNotFound
	}
	return entry, err
}

func scanBrowserHistory(row messageScanner) (*store.BrowserHistoryEntry, error) {
	var entry store.BrowserHistoryEntry
	var visited, created, updated int64
	if err := row.Scan(
		&entry.ID,
		&entry.URL,
		&entry.Title,
		&entry.FaviconURL,
		&visited,
		&created,
		&updated,
	); err != nil {
		return nil, err
	}
	entry.VisitedAt = timeFromMS(visited)
	entry.CreatedAt = timeFromMS(created)
	entry.UpdatedAt = timeFromMS(updated)
	return &entry, nil
}

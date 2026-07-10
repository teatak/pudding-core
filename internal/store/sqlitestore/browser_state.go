package sqlitestore

import (
	"context"
	"database/sql"
	"errors"
	"time"

	"github.com/teatak/pudding-core/internal/store"
)

const browserStateColumns = `session_id,tab_id,url,title,favicon_url,mode,created_at,updated_at`

func (s *Store) GetBrowserState(ctx context.Context, sessionID string) (*store.BrowserState, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, err := s.getSessionDB(ctx, sessionID); err != nil {
		return nil, err
	}
	state, err := scanBrowserState(s.db.QueryRowContext(ctx,
		`SELECT `+browserStateColumns+` FROM session_browser_tabs WHERE session_id=? ORDER BY updated_at DESC, rowid DESC LIMIT 1`,
		sessionID,
	))
	if errors.Is(err, sql.ErrNoRows) {
		return nil, store.ErrNotFound
	}
	return state, err
}

func (s *Store) GetBrowserTabState(ctx context.Context, sessionID, tabID string) (*store.BrowserState, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, err := s.getSessionDB(ctx, sessionID); err != nil {
		return nil, err
	}
	state, err := scanBrowserState(s.db.QueryRowContext(ctx,
		`SELECT `+browserStateColumns+` FROM session_browser_tabs WHERE session_id=? AND tab_id=?`,
		sessionID, tabID,
	))
	if errors.Is(err, sql.ErrNoRows) {
		return nil, store.ErrNotFound
	}
	return state, err
}

func (s *Store) ListBrowserStates(ctx context.Context, sessionID string) ([]*store.BrowserState, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, err := s.getSessionDB(ctx, sessionID); err != nil {
		return nil, err
	}
	rows, err := s.db.QueryContext(ctx,
		`SELECT `+browserStateColumns+` FROM session_browser_tabs WHERE session_id=? ORDER BY updated_at DESC, rowid DESC`,
		sessionID,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := make([]*store.BrowserState, 0)
	for rows.Next() {
		state, err := scanBrowserState(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, state)
	}
	return out, rows.Err()
}

func (s *Store) PutBrowserState(ctx context.Context, in store.BrowserStateInput) (*store.BrowserState, error) {
	if err := store.NormalizeBrowserStateInput(&in); err != nil {
		return nil, err
	}
	var out *store.BrowserState
	err := s.tx(ctx, func(tx *sql.Tx) error {
		if _, err := getSessionTx(ctx, tx, in.SessionID); err != nil {
			return err
		}
		now := time.Now()
		created := unixMS(now)
		err := tx.QueryRowContext(ctx,
			`SELECT created_at FROM session_browser_tabs WHERE session_id=? AND tab_id=?`,
			in.SessionID, in.TabID,
		).Scan(&created)
		switch {
		case err == nil:
		case errors.Is(err, sql.ErrNoRows):
		default:
			return err
		}
		_, err = tx.ExecContext(ctx,
			`INSERT INTO session_browser_tabs(
				session_id,tab_id,url,title,favicon_url,mode,created_at,updated_at
			 ) VALUES(?,?,?,?,?,?,?,?)
			 ON CONFLICT(session_id,tab_id) DO UPDATE SET
			   url=excluded.url,
			   title=excluded.title,
			   favicon_url=excluded.favicon_url,
			   mode=excluded.mode,
			   updated_at=excluded.updated_at`,
			in.SessionID, in.TabID, in.URL, in.Title, in.FaviconURL, in.Mode, created, unixMS(now),
		)
		if err != nil {
			return err
		}
		out, err = getBrowserTabStateTx(ctx, tx, in.SessionID, in.TabID)
		return err
	})
	return out, err
}

func (s *Store) DeleteBrowserState(ctx context.Context, sessionID, tabID string) error {
	return s.tx(ctx, func(tx *sql.Tx) error {
		if _, err := getSessionTx(ctx, tx, sessionID); err != nil {
			return err
		}
		_, err := tx.ExecContext(ctx, `DELETE FROM session_browser_tabs WHERE session_id=? AND tab_id=?`, sessionID, tabID)
		return err
	})
}

func (s *Store) ClearBrowserState(ctx context.Context, sessionID string) error {
	return s.tx(ctx, func(tx *sql.Tx) error {
		if _, err := getSessionTx(ctx, tx, sessionID); err != nil {
			return err
		}
		_, err := tx.ExecContext(ctx, `DELETE FROM session_browser_tabs WHERE session_id=?`, sessionID)
		return err
	})
}

func getBrowserTabStateTx(ctx context.Context, tx *sql.Tx, sessionID, tabID string) (*store.BrowserState, error) {
	state, err := scanBrowserState(tx.QueryRowContext(ctx,
		`SELECT `+browserStateColumns+` FROM session_browser_tabs WHERE session_id=? AND tab_id=?`,
		sessionID, tabID,
	))
	if errors.Is(err, sql.ErrNoRows) {
		return nil, store.ErrNotFound
	}
	return state, err
}

func scanBrowserState(row messageScanner) (*store.BrowserState, error) {
	var state store.BrowserState
	var created, updated int64
	if err := row.Scan(
		&state.SessionID,
		&state.TabID,
		&state.URL,
		&state.Title,
		&state.FaviconURL,
		&state.Mode,
		&created,
		&updated,
	); err != nil {
		return nil, err
	}
	state.CreatedAt = timeFromMS(created)
	state.UpdatedAt = timeFromMS(updated)
	return &state, nil
}

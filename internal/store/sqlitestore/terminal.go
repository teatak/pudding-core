package sqlitestore

import (
	"context"
	"database/sql"
	"errors"
	"time"

	"github.com/teatak/pudding-core/internal/store"
)

const terminalColumns = `terminal_id,session_id,title,cwd,shell,status,exit_code,created_at,updated_at`

func (s *Store) CreateTerminal(ctx context.Context, item *store.Terminal) error {
	if err := store.NormalizeTerminal(item); err != nil {
		return err
	}
	return s.tx(ctx, func(tx *sql.Tx) error {
		if _, err := getSessionTx(ctx, tx, item.SessionID); err != nil {
			return err
		}
		now := time.Now()
		item.CreatedAt = now
		item.UpdatedAt = now
		_, err := tx.ExecContext(ctx,
			`INSERT INTO session_terminals(session_id,terminal_id,title,cwd,shell,status,exit_code,created_at,updated_at) VALUES(?,?,?,?,?,?,?,?,?)`,
			item.SessionID, item.ID, item.Title, item.CWD, item.Shell, item.Status, nullableInt(item.ExitCode), unixMS(now), unixMS(now),
		)
		return err
	})
}

func (s *Store) GetTerminal(ctx context.Context, sessionID, terminalID string) (*store.Terminal, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	item, err := scanTerminal(s.db.QueryRowContext(ctx,
		`SELECT `+terminalColumns+` FROM session_terminals WHERE session_id=? AND terminal_id=?`, sessionID, terminalID,
	))
	if errors.Is(err, sql.ErrNoRows) {
		return nil, store.ErrNotFound
	}
	return item, err
}

func (s *Store) ListTerminals(ctx context.Context, sessionID string) ([]*store.Terminal, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, err := s.getSessionDB(ctx, sessionID); err != nil {
		return nil, err
	}
	rows, err := s.db.QueryContext(ctx,
		`SELECT `+terminalColumns+` FROM session_terminals WHERE session_id=? ORDER BY created_at ASC, terminal_id ASC`, sessionID,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := make([]*store.Terminal, 0)
	for rows.Next() {
		item, err := scanTerminal(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, item)
	}
	return out, rows.Err()
}

func (s *Store) UpdateTerminalStatus(ctx context.Context, sessionID, terminalID string, status store.TerminalStatus, exitCode *int) (*store.Terminal, error) {
	if status != store.TerminalRunning && status != store.TerminalExited {
		return nil, store.ErrInvalidTerminal
	}
	var out *store.Terminal
	err := s.tx(ctx, func(tx *sql.Tx) error {
		now := time.Now()
		res, err := tx.ExecContext(ctx,
			`UPDATE session_terminals SET status=?,exit_code=?,updated_at=? WHERE session_id=? AND terminal_id=?`,
			status, nullableInt(exitCode), unixMS(now), sessionID, terminalID,
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
		out, err = scanTerminal(tx.QueryRowContext(ctx,
			`SELECT `+terminalColumns+` FROM session_terminals WHERE session_id=? AND terminal_id=?`, sessionID, terminalID,
		))
		return err
	})
	return out, err
}

func (s *Store) DeleteTerminal(ctx context.Context, sessionID, terminalID string) error {
	return s.tx(ctx, func(tx *sql.Tx) error {
		res, err := tx.ExecContext(ctx, `DELETE FROM session_terminals WHERE session_id=? AND terminal_id=?`, sessionID, terminalID)
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

func (s *Store) ResetRunningTerminals(ctx context.Context) error {
	return s.tx(ctx, func(tx *sql.Tx) error {
		_, err := tx.ExecContext(ctx,
			`UPDATE session_terminals SET status=?,exit_code=NULL,updated_at=? WHERE status=?`,
			store.TerminalExited, unixMS(time.Now()), store.TerminalRunning,
		)
		return err
	})
}

type terminalScanner interface {
	Scan(dest ...any) error
}

func scanTerminal(scanner terminalScanner) (*store.Terminal, error) {
	var item store.Terminal
	var status string
	var exitCode sql.NullInt64
	var createdAt, updatedAt int64
	if err := scanner.Scan(&item.ID, &item.SessionID, &item.Title, &item.CWD, &item.Shell, &status, &exitCode, &createdAt, &updatedAt); err != nil {
		return nil, err
	}
	item.Status = store.TerminalStatus(status)
	if exitCode.Valid {
		value := int(exitCode.Int64)
		item.ExitCode = &value
	}
	item.CreatedAt = timeFromMS(createdAt)
	item.UpdatedAt = timeFromMS(updatedAt)
	return &item, nil
}

func nullableInt(value *int) any {
	if value == nil {
		return nil
	}
	return *value
}

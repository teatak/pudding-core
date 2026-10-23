package sqlitestore

import (
	"context"
	"database/sql"
	"errors"

	"github.com/teatak/pudding-core/internal/store"
)

const contextCoolingSchema = `CREATE TABLE IF NOT EXISTS session_context_cooling (
    session_id TEXT PRIMARY KEY REFERENCES sessions(id) ON DELETE CASCADE,
    through_turn_id TEXT NOT NULL,
    summary_message_id TEXT NOT NULL
);`

func (s *Store) ContextCooling(ctx context.Context, sessionID string) (store.ContextCooling, error) {
	var state store.ContextCooling
	err := s.db.QueryRowContext(ctx, `SELECT through_turn_id,summary_message_id FROM session_context_cooling WHERE session_id=?`, sessionID).Scan(&state.ThroughTurnID, &state.SummaryMessageID)
	if errors.Is(err, sql.ErrNoRows) {
		return state, nil
	}
	return state, err
}

// Commit only against the exact snapshot and running owner that were estimated.
func (s *Store) AdvanceContextCooling(ctx context.Context, in store.AdvanceContextCoolingInput) error {
	return s.tx(ctx, func(ctx context.Context, tx *sql.Tx) error {
		running, err := runningTurnTx(ctx, tx, in.SessionID)
		if errors.Is(err, store.ErrNotFound) {
			return store.ErrHistoryChanged
		}
		if err != nil {
			return err
		}
		if running.ID != in.RunningTurnID {
			return store.ErrHistoryChanged
		}
		var lastID string
		if err := tx.QueryRowContext(ctx, `SELECT id FROM messages WHERE session_id=? ORDER BY created_at DESC,rowid DESC LIMIT 1`, in.SessionID).Scan(&lastID); err != nil {
			return err
		}
		if lastID != in.ExpectedLastMessageID {
			return store.ErrHistoryChanged
		}
		var current store.ContextCooling
		err = tx.QueryRowContext(ctx, `SELECT through_turn_id,summary_message_id FROM session_context_cooling WHERE session_id=?`, in.SessionID).Scan(&current.ThroughTurnID, &current.SummaryMessageID)
		if err != nil && !errors.Is(err, sql.ErrNoRows) {
			return err
		}
		if current != in.Expected {
			return store.ErrHistoryChanged
		}
		_, err = tx.ExecContext(ctx, `INSERT INTO session_context_cooling(session_id,through_turn_id,summary_message_id) VALUES(?,?,?) ON CONFLICT(session_id) DO UPDATE SET through_turn_id=excluded.through_turn_id,summary_message_id=excluded.summary_message_id`, in.SessionID, in.Next.ThroughTurnID, in.Next.SummaryMessageID)
		return err
	})
}

package sqlitestore

import (
	"context"
	"database/sql"
	"errors"
	"time"

	"github.com/teatak/pudding-core/internal/event"
	"github.com/teatak/pudding-core/internal/store"
)

func (s *Store) ListSubtaskArchives(ctx context.Context, parentID string) (map[string]time.Time, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	rows, err := s.db.QueryContext(ctx, `SELECT a.child_session_id,a.archived_at FROM subtask_archives a
		JOIN session_children c ON c.child_session_id=a.child_session_id WHERE c.parent_session_id=?`, parentID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := make(map[string]time.Time)
	for rows.Next() {
		var id string
		var at int64
		if err := rows.Scan(&id, &at); err != nil {
			return nil, err
		}
		out[id] = time.UnixMilli(at).UTC()
	}
	return out, rows.Err()
}

// Archive state is separate from sessions.archived_at: it does not close the
// conversation or start retention, and is cascaded away with the parent group.
func (s *Store) SetSubtaskArchived(ctx context.Context, parentID, childID, expectedTurnID string, archived bool) error {
	return s.tx(ctx, func(ctx context.Context, tx *sql.Tx) error {
		if _, err := getSessionTx(ctx, tx, parentID); err != nil {
			return err
		}
		owner, err := parentSessionIDTx(ctx, tx, childID)
		if err != nil {
			return err
		}
		if owner != parentID || owner == "" {
			return store.ErrInvalidSessionRelation
		}
		if !archived {
			return restoreSubtaskTx(ctx, tx, childID)
		}
		var exists bool
		if err := tx.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM subtask_archives WHERE child_session_id=?)`, childID).Scan(&exists); err != nil {
			return err
		}
		if exists {
			return nil
		}
		var latestID, status string
		err = tx.QueryRowContext(ctx, `SELECT id,status FROM turns WHERE session_id=? ORDER BY created_at DESC,rowid DESC LIMIT 1`, childID).Scan(&latestID, &status)
		if errors.Is(err, sql.ErrNoRows) {
			return store.ErrSubtaskArchiveBusy
		}
		if err != nil {
			return err
		}
		if latestID != expectedTurnID || !store.SubagentArchiveStatusEligible(status) {
			return store.ErrSubtaskArchiveBusy
		}
		var busy bool
		if err := tx.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM turns WHERE session_id=? AND status='running')
			OR EXISTS(SELECT 1 FROM queued_inputs WHERE session_id=? AND status IN ('queued','editing'))`, childID, childID).Scan(&busy); err != nil {
			return err
		}
		if busy {
			return store.ErrSubtaskArchiveBusy
		}
		if _, err := tx.ExecContext(ctx, `INSERT INTO subtask_archives(child_session_id,archived_at) VALUES(?,?)`, childID, unixMS(time.Now())); err != nil {
			return err
		}
		return insertEventTx(ctx, tx, &event.Event{SessionID: parentID, Kind: event.CollaborationChanged})
	})
}

func restoreSubtaskTx(ctx context.Context, tx *sql.Tx, childID string) error {
	res, err := tx.ExecContext(ctx, `DELETE FROM subtask_archives WHERE child_session_id=?`, childID)
	if err != nil {
		return err
	}
	n, err := res.RowsAffected()
	if err != nil || n == 0 {
		return err
	}
	parentID, err := parentSessionIDTx(ctx, tx, childID)
	if err != nil {
		return err
	}
	return insertEventTx(ctx, tx, &event.Event{SessionID: parentID, Kind: event.CollaborationChanged})
}

// DispatchChild invokes this inside its admission transaction, after resolving
// duplicate tool calls and before creating either the child or its queued input.
// Count relationships, not the visible list: archived and failed children count.
func checkSubtaskBudgetTx(ctx context.Context, tx *sql.Tx, parentID, turnID string) error {
	var total int
	if err := tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM session_children WHERE parent_session_id=?`, parentID).Scan(&total); err != nil {
		return err
	}
	if total >= store.SubagentTotalLimit {
		return store.ErrSubagentTotalLimit
	}
	// Derive the request boundary from canonical initial user messages. System
	// wakes, retries and form answers share the preceding request's budget. Rowid
	// breaks same-millisecond ties; this also works across process restarts.
	var count int
	err := tx.QueryRowContext(ctx, `WITH boundary AS (
		SELECT COALESCE(MAX(t.rowid),0) AS first_row FROM turns t
		JOIN messages m ON m.turn_id=t.id AND m.client_message_id=t.client_message_id
		WHERE t.session_id=? AND t.rowid<=(SELECT rowid FROM turns WHERE id=?) AND m.role='user'
		AND t.client_message_id NOT GLOB 'input-flow-*'
		AND NOT EXISTS(SELECT 1 FROM json_each(m.parts) p WHERE json_extract(p.value,'$.type')='form_result')
	)
	SELECT COUNT(*) FROM session_dispatches d JOIN turns t ON t.id=d.parent_turn_id
	WHERE t.session_id=? AND t.rowid>=(SELECT first_row FROM boundary)`, parentID, turnID, parentID).Scan(&count)
	if err != nil {
		return err
	}
	if count >= store.SubtaskDispatchLimit {
		return store.ErrSubtaskLimit
	}
	return nil
}

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
		if latestID != expectedTurnID || !store.SubagentSettledStatus(status) {
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

// New-object admission calls this after its idempotency lookup. Count every
// relationship, including archives. Old over-limit groups are never deleted;
// the total cap does not block send, restore or reset of an existing identity.
func checkSubtaskBudgetTx(ctx context.Context, tx *sql.Tx, parentID, turnID string) error {
	var total int
	if err := tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM session_children WHERE parent_session_id=?`, parentID).Scan(&total); err != nil {
		return err
	}
	if total >= store.SubagentTotalLimit {
		return store.ErrSubagentTotalLimit
	}
	return checkSubagentFreshBudgetTx(ctx, tx, parentID, turnID)
}

// Canonical initial user messages define the request boundary. System wakes,
// retries and form answers retain its budget. Rowid breaks millisecond ties.
// Dispatch rows plus reset checkpoint receipts are the accounting authority;
// no writable counter can be refunded by hiding or reusing a child.
const subagentFreshContextCountSQL = `WITH boundary AS (
	SELECT COALESCE(MAX(t.rowid),0) AS first_row FROM turns t
	JOIN messages m ON m.turn_id=t.id AND m.client_message_id=t.client_message_id
	WHERE t.session_id=? AND t.rowid<=(SELECT rowid FROM turns WHERE id=?) AND m.role='user'
	AND t.client_message_id NOT GLOB 'input-flow-*'
	AND NOT EXISTS(SELECT 1 FROM json_each(m.parts) p WHERE json_extract(p.value,'$.type')='form_result')
), resets AS (
	SELECT CASE WHEN json_valid(m.metadata) THEN m.metadata ELSE '{}' END AS metadata
	FROM messages m JOIN session_children c ON c.child_session_id=m.session_id
	WHERE c.parent_session_id=? AND m.role='summary' AND m.kind='summary'
)
SELECT (SELECT COUNT(*) FROM session_dispatches d JOIN turns t ON t.id=d.parent_turn_id
	WHERE t.session_id=? AND t.rowid>=(SELECT first_row FROM boundary))
	+ (SELECT COUNT(*) FROM resets r JOIN turns t ON t.id=json_extract(r.metadata,'$.subagent_reset.parent_turn_id')
	WHERE t.session_id=? AND t.rowid>=(SELECT first_row FROM boundary)
	AND json_extract(r.metadata,'$.subagent_reset.version')=1
	AND json_extract(r.metadata,'$.subagent_reset.parent_session_id')=?)`

func checkSubagentFreshBudgetTx(ctx context.Context, tx *sql.Tx, parentID, turnID string) error {
	var count int
	if err := tx.QueryRowContext(ctx, subagentFreshContextCountSQL, parentID, turnID, parentID, parentID, parentID, parentID).Scan(&count); err != nil {
		return err
	}
	if count >= store.SubtaskDispatchLimit {
		return store.ErrSubtaskLimit
	}
	return nil
}

func (s *Store) SubagentFreshContextCount(ctx context.Context, parentID, turnID string) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	var count int
	err := s.db.QueryRowContext(ctx, subagentFreshContextCountSQL, parentID, turnID, parentID, parentID, parentID, parentID).Scan(&count)
	return count, err
}

package sqlitestore

import (
	"context"
	"database/sql"
	"errors"
	"time"

	"github.com/teatak/pudding-core/internal/event"
	"github.com/teatak/pudding-core/internal/store"
)

var _ store.SubagentLifecycleStore = (*Store)(nil)

// Both dispatch and reset admission use this query inside the write transaction.
// Only canonical reset checkpoints count; user text and tool-result JSON cannot
// manufacture budget records. The original user request survives system wakes,
// retries and form answers, just as it does for dispatch admission.
const subagentFreshContextCountSQL = `WITH boundary AS (
 SELECT COALESCE(MAX(t.rowid),0) AS first_row FROM turns t
 JOIN messages m ON m.turn_id=t.id AND m.client_message_id=t.client_message_id
 WHERE t.session_id=? AND t.rowid<=(SELECT rowid FROM turns WHERE id=?) AND m.role='user'
 AND t.client_message_id NOT GLOB 'input-flow-*'
 AND NOT EXISTS(SELECT 1 FROM json_each(m.parts) p WHERE json_extract(p.value,'$.type')='form_result')
)
SELECT
 (SELECT COUNT(*) FROM session_dispatches d JOIN turns t ON t.id=d.parent_turn_id
  WHERE t.session_id=? AND t.rowid>=(SELECT first_row FROM boundary))
 +
 (SELECT COUNT(*) FROM messages m
  JOIN turns t ON t.id=json_extract(m.metadata,'$.subagent_reset.parent_turn_id')
  JOIN session_children c ON c.child_session_id=m.session_id
  WHERE c.parent_session_id=? AND t.session_id=c.parent_session_id
  AND m.role='summary' AND m.kind='summary'
  AND json_extract(m.metadata,'$.subagent_reset.version')=1
  AND json_extract(m.metadata,'$.subagent_reset.parent_session_id')=c.parent_session_id
  AND t.rowid>=(SELECT first_row FROM boundary))`

func (s *Store) SubagentFreshContextCount(ctx context.Context, parentID, turnID string) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	var count int
	err := s.db.QueryRowContext(ctx, subagentFreshContextCountSQL, parentID, turnID, parentID, parentID).Scan(&count)
	return count, err
}

func checkSubagentFreshContextBudgetTx(ctx context.Context, tx *sql.Tx, parentID, turnID string) error {
	var count int
	if err := tx.QueryRowContext(ctx, subagentFreshContextCountSQL, parentID, turnID, parentID, parentID).Scan(&count); err != nil {
		return err
	}
	if count >= store.SubtaskDispatchLimit {
		return store.ErrSubtaskLimit
	}
	return nil
}

// ResetSubagent preserves canonical history and writes a maintenance checkpoint.
// It must be called through Engine admission while the collaboration lock guards
// live approvals, unanswered questions, background processes and compaction.
// Persistent ownership, the latest task snapshot, queue, collection receipt,
// request quota, checkpoint and optional enqueue are checked/committed together.
func (s *Store) ResetSubagent(ctx context.Context, in store.SubagentResetInput) (*store.SubagentResetResult, error) {
	if err := store.ValidateSubagentReset(in); err != nil {
		return nil, err
	}
	resetTurnID, resetMessageID := store.SubagentResetIDs(in.ParentSessionID, in.ParentTurnID, in.CallID)
	var out *store.SubagentResetResult
	err := s.tx(ctx, func(ctx context.Context, tx *sql.Tx) error {
		parent, err := getSessionTx(ctx, tx, in.ParentSessionID)
		if err != nil {
			return err
		}
		owner, err := parentSessionIDTx(ctx, tx, parent.ID)
		if err != nil {
			return err
		}
		if owner != "" {
			return store.ErrInvalidSessionRelation
		}
		owner, err = parentSessionIDTx(ctx, tx, in.SessionID)
		if err != nil {
			return err
		}
		if owner != parent.ID {
			return store.ErrInvalidSessionRelation
		}
		child, err := getSessionTx(ctx, tx, in.SessionID)
		if err != nil {
			return err
		}
		// A replay returns its original receipt even if its queued task has since
		// started. It must not reset again, restore again, or consume another slot.
		existing := &store.Message{ID: resetMessageID}
		var metadata string
		err = tx.QueryRowContext(ctx, `SELECT session_id,turn_id,role,kind,metadata FROM messages WHERE id=?`, resetMessageID).
			Scan(&existing.SessionID, &existing.TurnID, &existing.Role, &existing.Kind, &metadata)
		if err == nil {
			existing.Metadata = []byte(metadata)
			out, err = store.SubagentResetReplay(existing, in)
			return err
		}
		if !errors.Is(err, sql.ErrNoRows) {
			return err
		}
		parentTurn, err := getTurnTx(ctx, tx, in.ParentTurnID)
		if err != nil {
			return err
		}
		if parentTurn.SessionID != parent.ID || parentTurn.Status != store.TurnRunning {
			return store.ErrInvalidSessionRelation
		}
		var stopped bool
		if err := tx.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM collaboration_stops WHERE parent_turn_id=?)`, parentTurn.ID).Scan(&stopped); err != nil {
			return err
		}
		if stopped {
			return store.ErrCollaborationStopped
		}
		var latestID, latestStatus string
		err = tx.QueryRowContext(ctx, `SELECT id,status FROM turns WHERE session_id=? ORDER BY created_at DESC,rowid DESC LIMIT 1`, child.ID).
			Scan(&latestID, &latestStatus)
		if errors.Is(err, sql.ErrNoRows) || (err == nil && latestID != in.ExpectedTurnID) {
			return store.ErrSubagentStateChanged
		}
		if err != nil {
			return err
		}
		if !store.SubagentSettledStatus(latestStatus) {
			return store.ErrSubagentResetBusy
		}
		var busy bool
		if err := tx.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM turns WHERE session_id=? AND status='running')
 OR EXISTS(SELECT 1 FROM queued_inputs WHERE session_id=? AND status IN ('queued','editing'))`, child.ID, child.ID).Scan(&busy); err != nil {
			return err
		}
		if busy {
			return store.ErrSubagentResetBusy
		}
		var maintenance, collected bool
		if err := tx.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM messages WHERE session_id=? AND turn_id=? AND role='summary' AND kind='summary' AND json_extract(metadata,'$.subagent_reset.version')=1)`, child.ID, latestID).Scan(&maintenance); err != nil {
			return err
		}
		if !maintenance {
			if err := tx.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM messages WHERE session_id=? AND id=?)`, parent.ID, "collaboration_result_"+latestID).Scan(&collected); err != nil {
				return err
			}
			if !collected {
				return store.ErrSubagentResultUncollected
			}
		}
		// Reset reuses an identity: the 24-child total cap does not apply here.
		if err := checkSubagentFreshContextBudgetTx(ctx, tx, parent.ID, parentTurn.ID); err != nil {
			return err
		}
		now := time.Now().UTC()
		if _, err := tx.ExecContext(ctx, `INSERT INTO turns(id,session_id,client_message_id,status,provider,model,mode,created_at,updated_at)
 VALUES(?,?,?,?,?,?,?,?,?)`, resetTurnID, child.ID, resetTurnID, string(store.TurnCompleted), child.Provider, child.Model, string(child.ActiveMode), unixMS(now), unixMS(now)); err != nil {
			return err
		}
		checkpoint := &store.Message{
			ID: resetMessageID, SessionID: child.ID, TurnID: resetTurnID,
			Role: store.RoleSummary, Kind: store.MessageKindSummary,
			Text: "Subagent task context reset. Earlier history is retained for explicit inspection but is not part of the new task context.",
			Metadata: store.NewSubagentResetMetadata(in).JSON(), CreatedAt: now,
		}
		checkpoint.Parts = store.TextPart(checkpoint.Text)
		if err := insertMessageTx(ctx, tx, checkpoint); err != nil {
			return err
		}
		out = &store.SubagentResetResult{SessionID: child.ID, ResetTurnID: resetTurnID, ContextReset: true}
		if in.Input != nil {
			queued, err := queueInputTx(ctx, tx, *in.Input)
			if err != nil {
				return err
			}
			// Reusing an unrelated existing queue identity would otherwise leave a
			// reset with no new task. Roll the entire transaction back in that case.
			if queued.Duplicate || queued.ExistingTurn != nil {
				return store.ErrSubagentResetConflict
			}
			out.Queued, out.ClientMessageID = true, in.Input.ClientMessageID
		}
		// Maintenance refreshes canonical history without announcing a finished
		// task. The paired desktop refreshes turns on collaboration changes.
		if err := insertEventTx(ctx, tx, &event.Event{SessionID: child.ID, Kind: event.CollaborationChanged}); err != nil {
			return err
		}
		return insertEventTx(ctx, tx, &event.Event{SessionID: parent.ID, Kind: event.CollaborationChanged})
	})
	if err != nil {
		return nil, err
	}
	return out, nil
}

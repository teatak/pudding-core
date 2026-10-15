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

// ResetSubagent is called under Engine's collaboration admission lock after
// checking runtime approvals, questions, background processes and compaction.
// Persistent ownership, stop barrier, version, busy state, result receipt,
// budget, checkpoint, archive restoration and queue admission share this tx.
func (s *Store) ResetSubagent(ctx context.Context, in store.SubagentResetInput) (*store.SubagentResetResult, error) {
	if err := store.ValidateSubagentReset(in); err != nil {
		return nil, err
	}
	resetTurnID, messageID := store.SubagentResetIDs(in.ParentSessionID, in.ParentTurnID, in.CallID)
	var out *store.SubagentResetResult
	err := s.tx(ctx, func(ctx context.Context, tx *sql.Tx) error {
		if _, err := getSessionTx(ctx, tx, in.ParentSessionID); err != nil {
			return err
		}
		parentOwner, err := parentSessionIDTx(ctx, tx, in.ParentSessionID)
		if err != nil {
			return err
		}
		owner, err := parentSessionIDTx(ctx, tx, in.SessionID)
		if err != nil {
			return err
		}
		if parentOwner != "" || owner == "" || owner != in.ParentSessionID {
			return store.ErrInvalidSessionRelation
		}
		// A successful call replays its receipt before quota/busy checks. It
		// cannot reset twice even after the new task starts or the parent stops.
		prior := &store.Message{ID: messageID, SessionID: in.SessionID}
		var rawMetadata string
		err = tx.QueryRowContext(ctx, `SELECT turn_id,role,kind,metadata FROM messages WHERE session_id=? AND id=?`, in.SessionID, messageID).Scan(&prior.TurnID, &prior.Role, &prior.Kind, &rawMetadata)
		if err == nil {
			prior.Metadata = []byte(rawMetadata)
			out, err = store.SubagentResetReplay(prior, in)
			return err
		}
		if !errors.Is(err, sql.ErrNoRows) {
			return err
		}
		parentTurn, err := getTurnTx(ctx, tx, in.ParentTurnID)
		if err != nil {
			return err
		}
		if parentTurn.SessionID != in.ParentSessionID || parentTurn.Status != store.TurnRunning {
			return store.ErrInvalidSessionRelation
		}
		var stopped bool
		if err := tx.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM collaboration_stops WHERE parent_turn_id=?)`, in.ParentTurnID).Scan(&stopped); err != nil {
			return err
		}
		if stopped {
			return store.ErrCollaborationStopped
		}
		var latestID, status string
		err = tx.QueryRowContext(ctx, `SELECT id,status FROM turns WHERE session_id=? ORDER BY created_at DESC,rowid DESC LIMIT 1`, in.SessionID).Scan(&latestID, &status)
		if errors.Is(err, sql.ErrNoRows) {
			return store.ErrSubagentStateChanged
		}
		if err != nil {
			return err
		}
		if latestID != in.ExpectedTurnID {
			return store.ErrSubagentStateChanged
		}
		if !store.SubagentSettledStatus(status) {
			return store.ErrSubagentResetBusy
		}
		var busy bool
		if err := tx.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM turns WHERE session_id=? AND status='running')
			OR EXISTS(SELECT 1 FROM queued_inputs WHERE session_id=? AND status IN ('queued','editing'))`, in.SessionID, in.SessionID).Scan(&busy); err != nil {
			return err
		}
		if busy {
			return store.ErrSubagentResetBusy
		}
		checkpoint, err := isSubagentResetTurnTx(ctx, tx, in.SessionID, latestID)
		if err != nil {
			return err
		}
		if !checkpoint {
			var collected bool
			if err := tx.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM messages WHERE session_id=? AND id=?)`, in.ParentSessionID, "collaboration_result_"+latestID).Scan(&collected); err != nil {
				return err
			}
			if !collected {
				return store.ErrSubagentResultUncollected
			}
		}
		if in.Input != nil {
			// A call previously accepted as a normal send cannot be replayed
			// with reset_context=true and silently discard its existing context.
			var used bool
			if err := tx.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM queued_inputs WHERE session_id=? AND client_message_id=?)
				OR EXISTS(SELECT 1 FROM turns WHERE session_id=? AND client_message_id=?)`, in.SessionID, in.Input.ClientMessageID, in.SessionID, in.Input.ClientMessageID).Scan(&used); err != nil {
				return err
			}
			if used {
				return store.ErrSubagentResetConflict
			}
		}
		if err := checkSubagentFreshBudgetTx(ctx, tx, in.ParentSessionID, in.ParentTurnID); err != nil {
			return err
		}
		child, err := getSessionTx(ctx, tx, in.SessionID)
		if err != nil {
			return err
		}
		now := time.Now()
		// This terminal maintenance turn performs no model work. Advancing
		// latest turn identity also makes retries of pre-reset tasks stale.
		if _, err := tx.ExecContext(ctx, `INSERT INTO turns(id,session_id,client_message_id,status,provider,model,mode,created_at,updated_at)
			VALUES(?,?,?,?,?,?,?,?,?)`, resetTurnID, in.SessionID, messageID, string(store.TurnCompleted), child.Provider, child.Model, string(child.ActiveMode), unixMS(now), unixMS(now)); err != nil {
			return err
		}
		meta := store.NewSubagentResetMetadata(in)
		text := "Subagent context reset. Earlier history is retained but excluded from new task context."
		marker := &store.Message{ID: messageID, SessionID: in.SessionID, TurnID: resetTurnID,
			Role: store.RoleSummary, Kind: store.MessageKindSummary, Text: text,
			Parts: store.UserInputParts(text, nil), Metadata: meta.JSON(), CreatedAt: now}
		if err := insertMessageTx(ctx, tx, marker); err != nil {
			return err
		}
		if in.Input != nil {
			if err := restoreSubtaskTx(ctx, tx, in.SessionID); err != nil {
				return err
			}
			if _, err := queueInputTx(ctx, tx, *in.Input); err != nil {
				return err
			}
		}
		// No fake turn.started/turn.completed notification: this is a
		// collaboration lifecycle change. Refresh the child and parent views.
		for _, sessionID := range []string{in.SessionID, in.ParentSessionID} {
			if err := insertEventTx(ctx, tx, &event.Event{SessionID: sessionID, Kind: event.CollaborationChanged}); err != nil {
				return err
			}
		}
		out = &store.SubagentResetResult{SessionID: in.SessionID, ResetTurnID: resetTurnID,
			ContextReset: true, Queued: in.Input != nil, ClientMessageID: meta.QueuedClientMessageID}
		return nil
	})
	return out, err
}

func isSubagentResetTurnTx(ctx context.Context, tx *sql.Tx, sessionID, turnID string) (bool, error) {
	var raw string
	err := tx.QueryRowContext(ctx, `SELECT metadata FROM messages WHERE session_id=? AND turn_id=? AND role='summary' AND kind='summary' ORDER BY rowid DESC LIMIT 1`, sessionID, turnID).Scan(&raw)
	if errors.Is(err, sql.ErrNoRows) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	_, ok := store.SubagentResetFromMessage(&store.Message{Role: store.RoleSummary, Kind: store.MessageKindSummary, Metadata: []byte(raw)})
	return ok, nil
}

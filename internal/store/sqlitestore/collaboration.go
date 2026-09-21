package sqlitestore

import (
	"context"
	"database/sql"
	"errors"
	"time"

	"github.com/teatak/pudding-core/internal/event"
	"github.com/teatak/pudding-core/internal/store"
)

func (s *Store) DispatchChild(ctx context.Context, in store.DispatchChildInput) (*store.DispatchChildResult, error) {
	if err := store.ValidateDispatchChild(in); err != nil {
		return nil, err
	}
	var out *store.DispatchChildResult
	err := s.tx(ctx, func(tx *sql.Tx) error {
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
		var existingID string
		err = tx.QueryRowContext(ctx, `SELECT child_session_id FROM session_dispatches WHERE parent_turn_id=? AND call_id=?`, in.ParentTurnID, in.CallID).Scan(&existingID)
		if err == nil {
			owner, err := parentSessionIDTx(ctx, tx, existingID)
			if err != nil {
				return err
			}
			if owner != parent.ID {
				return store.ErrInvalidSessionRelation
			}
			child, err := getSessionTx(ctx, tx, existingID)
			if err != nil {
				return err
			}
			out = &store.DispatchChildResult{Session: child, Duplicate: true}
			return nil
		}
		if !errors.Is(err, sql.ErrNoRows) {
			return err
		}
		turn, err := getTurnTx(ctx, tx, in.ParentTurnID)
		if err != nil {
			return err
		}
		if turn.SessionID != parent.ID || turn.Status != store.TurnRunning {
			return store.ErrInvalidSessionRelation
		}
		var stopped bool
		if err := tx.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM collaboration_stops WHERE parent_turn_id=?)`, turn.ID).Scan(&stopped); err != nil {
			return err
		}
		if stopped {
			return store.ErrCollaborationStopped
		}
		if err := store.PrepareChildSession(parent, in.Child); err != nil {
			return err
		}
		if err := createSessionTx(ctx, tx, in.Child); err != nil {
			return err
		}
		if _, err := tx.ExecContext(ctx, `INSERT INTO session_children(child_session_id,parent_session_id) VALUES(?,?)`, in.Child.ID, parent.ID); err != nil {
			return err
		}
		if _, err := tx.ExecContext(ctx, `INSERT INTO session_dispatches(child_session_id,parent_turn_id,call_id) VALUES(?,?,?)`, in.Child.ID, turn.ID, in.CallID); err != nil {
			return err
		}
		queued, err := queueInputTx(ctx, tx, in.Input)
		if err != nil {
			return err
		}
		ev := event.Event{SessionID: parent.ID, Kind: event.CollaborationChanged}
		if err := insertEventTx(ctx, tx, &ev); err != nil {
			return err
		}
		out = &store.DispatchChildResult{Session: in.Child, Events: []event.Event{*queued.QueuedEvent, ev}}
		return nil
	})
	return out, err
}

func (s *Store) StopCollaboration(ctx context.Context, parentID string) ([]event.Event, error) {
	var events []event.Event
	err := s.tx(ctx, func(tx *sql.Tx) error {
		if _, err := getSessionAnyTx(ctx, tx, parentID); err != nil {
			return err
		}
		owner, err := parentSessionIDTx(ctx, tx, parentID)
		if err != nil {
			return err
		}
		if owner != "" {
			return store.ErrInvalidSessionRelation
		}
		// A durable barrier covers both accepted dispatches and a dispatch racing the stop.
		if _, err := tx.ExecContext(ctx, `INSERT OR IGNORE INTO collaboration_stops(parent_turn_id)
    SELECT id FROM turns WHERE session_id=? AND (status='running' OR id IN
    (SELECT d.parent_turn_id FROM session_dispatches d JOIN session_children c ON c.child_session_id=d.child_session_id WHERE c.parent_session_id=?))`, parentID, parentID); err != nil {
			return err
		}
		rows, err := tx.QueryContext(ctx, `SELECT q.session_id,q.client_message_id FROM queued_inputs q JOIN session_children c ON c.child_session_id=q.session_id WHERE c.parent_session_id=? AND q.status IN ('queued','editing')`, parentID)
		if err != nil {
			return err
		}
		for rows.Next() {
			var ev event.Event
			ev.Kind = event.InputUpdated
			ev.Status = string(store.QueuedInputCancelled)
			if err := rows.Scan(&ev.SessionID, &ev.ClientMessageID); err != nil {
				rows.Close()
				return err
			}
			events = append(events, ev)
		}
		if err := rows.Err(); err != nil {
			rows.Close()
			return err
		}
		rows.Close()
		if _, err := tx.ExecContext(ctx, `UPDATE queued_inputs SET status='cancelled',updated_at=? WHERE session_id IN (SELECT child_session_id FROM session_children WHERE parent_session_id=?) AND status IN ('queued','editing')`, unixMS(time.Now()), parentID); err != nil {
			return err
		}
		events = append(events, event.Event{SessionID: parentID, Kind: event.CollaborationChanged})
		for i := range events {
			if err := insertEventTx(ctx, tx, &events[i]); err != nil {
				return err
			}
		}
		return nil
	})
	return events, err
}

// The latest child turn is the result version. A new input suppresses collection
// of the preceding turn; a delivery receipt is the canonical parent message.
const uncollectedChildTurnsSQL = `SELECT t.id FROM session_children c JOIN turns t ON t.session_id=c.child_session_id
 WHERE c.parent_session_id=? AND t.rowid=(SELECT t2.rowid FROM turns t2 WHERE t2.session_id=c.child_session_id ORDER BY t2.created_at DESC,t2.rowid DESC LIMIT 1)
 AND t.status<>'running'
 AND NOT EXISTS(SELECT 1 FROM queued_inputs q WHERE q.session_id=c.child_session_id AND q.status IN ('queued','editing'))
 AND NOT EXISTS(SELECT 1 FROM messages m WHERE m.id='collaboration_result_'||t.id)
 ORDER BY t.created_at,t.rowid`

func (s *Store) HasUncollectedChildResults(ctx context.Context, parentID string) (bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	var id string
	err := s.db.QueryRowContext(ctx, uncollectedChildTurnsSQL, parentID).Scan(&id)
	if errors.Is(err, sql.ErrNoRows) {
		return false, nil
	}
	return err == nil, err
}

func (s *Store) CollectChildResults(ctx context.Context, parentID string) ([]event.Event, error) {
	var events []event.Event
	err := s.tx(ctx, func(tx *sql.Tx) error {
		rows, err := tx.QueryContext(ctx, uncollectedChildTurnsSQL, parentID)
		if err != nil {
			return err
		}
		var ids []string
		for rows.Next() {
			var id string
			if err := rows.Scan(&id); err != nil {
				rows.Close()
				return err
			}
			ids = append(ids, id)
		}
		if err := rows.Err(); err != nil {
			rows.Close()
			return err
		}
		rows.Close()
		for _, id := range ids {
			turn, err := getTurnTx(ctx, tx, id)
			if err != nil {
				return err
			}
			child, err := getSessionTx(ctx, tx, turn.SessionID)
			if err != nil {
				return err
			}
			rows, err := tx.QueryContext(ctx, `SELECT text FROM messages WHERE session_id=? AND turn_id=? AND role='assistant' ORDER BY created_at,rowid`, child.ID, id)
			if err != nil {
				return err
			}
			var messages []*store.Message
			for rows.Next() {
				msg := &store.Message{Role: store.RoleAssistant, TurnID: id}
				if err := rows.Scan(&msg.Text); err != nil {
					rows.Close()
					return err
				}
				messages = append(messages, msg)
			}
			if err := rows.Err(); err != nil {
				rows.Close()
				return err
			}
			rows.Close()
			msg := store.ChildResultMessage(parentID, child, turn, messages)
			if err := insertMessageTx(ctx, tx, msg); err != nil {
				return err
			}
			ev := event.Event{SessionID: parentID, Kind: event.CollaborationChanged}
			if err := insertEventTx(ctx, tx, &ev); err != nil {
				return err
			}
			events = append(events, ev)
		}
		return nil
	})
	return events, err
}

func (s *Store) QueueChildInput(ctx context.Context, parentID, parentTurnID string, in store.QueueInputInput) (*store.QueueInputResult, error) {
	var out *store.QueueInputResult
	err := s.tx(ctx, func(tx *sql.Tx) error {
		owner, err := parentSessionIDTx(ctx, tx, in.SessionID)
		if err != nil {
			return err
		}
		if owner != parentID {
			return store.ErrInvalidSessionRelation
		}
		turn, err := getTurnTx(ctx, tx, parentTurnID)
		if err != nil {
			return err
		}
		if turn.SessionID != parentID || turn.Status != store.TurnRunning {
			return store.ErrInvalidSessionRelation
		}
		var stopped bool
		if err := tx.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM collaboration_stops WHERE parent_turn_id=?)`, parentTurnID).Scan(&stopped); err != nil {
			return err
		}
		if stopped {
			return store.ErrCollaborationStopped
		}
		out, err = queueInputTx(ctx, tx, in)
		return err
	})
	return out, err
}

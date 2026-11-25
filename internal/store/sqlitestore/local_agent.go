package sqlitestore

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"

	"github.com/teatak/pudding-core/internal/event"
	"github.com/teatak/pudding-core/internal/store"
)

func (s *Store) GetLocalAgentState(ctx context.Context, id string) (*store.LocalAgentState, error) {
	var data []byte
	err := s.readDB.QueryRowContext(ctx, `SELECT state FROM local_agent_sessions WHERE session_id=?`, id).Scan(&data)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, store.ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	var state store.LocalAgentState
	if err := json.Unmarshal(data, &state); err != nil {
		return nil, err
	}
	return &state, nil
}
func (s *Store) PutLocalAgentState(ctx context.Context, state *store.LocalAgentState) error {
	data, err := json.Marshal(state)
	if err != nil {
		return err
	}
	return s.tx(ctx, func(ctx context.Context, tx *sql.Tx) error {
		_, err := tx.ExecContext(ctx, `INSERT INTO local_agent_sessions(session_id,state) VALUES(?,?) ON CONFLICT(session_id) DO UPDATE SET state=excluded.state`, state.SessionID, string(data))
		return err
	})
}
func (s *Store) LookupSubmission(ctx context.Context, sessionID, clientID string) (*store.Turn, *store.QueuedInput, error) {
	turn, err := scanTurn(s.readDB.QueryRowContext(ctx, `SELECT id,session_id,client_message_id,status,provider,model,mode,model_config,error,created_at,updated_at,retry_of_turn_id FROM turns WHERE session_id=? AND client_message_id=?`, sessionID, clientID))
	if err == nil {
		return turn, nil, nil
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return nil, nil, err
	}
	queued, err := scanQueuedInput(s.readDB.QueryRowContext(ctx, `SELECT session_id,client_message_id,text,parts,status,provider,model,mode,model_config,turn_id,created_at,updated_at FROM queued_inputs WHERE session_id=? AND client_message_id=?`, sessionID, clientID))
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil, nil
	}
	return nil, queued, err
}

// Keep the notice with the canonical turn and publish it in the same commit.
func (s *Store) RecordLocalAgentContext(ctx context.Context, sessionID, turnID string, notice store.LocalAgentContext) error {
	return s.tx(ctx, func(ctx context.Context, tx *sql.Tx) error {
		turn, err := getTurnTx(ctx, tx, turnID)
		if err != nil {
			return err
		}
		if turn.SessionID != sessionID {
			return store.ErrNotFound
		}
		if turn.Status != store.TurnRunning {
			return errors.New("local agent context requires a running turn")
		}
		var config map[string]json.RawMessage
		if err := json.Unmarshal(turn.ModelConfig, &config); err != nil {
			return err
		}
		if config == nil {
			config = make(map[string]json.RawMessage)
		}
		payload, err := json.Marshal(notice)
		if err != nil {
			return err
		}
		config["localAgentContext"] = payload
		data, err := json.Marshal(config)
		if err != nil {
			return err
		}
		if _, err := tx.ExecContext(ctx, `UPDATE turns SET model_config=? WHERE id=?`, string(data), turnID); err != nil {
			return err
		}
		return insertEventTx(ctx, tx, &event.Event{Kind: event.TurnLocalAgentContext, SessionID: sessionID, TurnID: turnID, Payload: payload})
	})
}

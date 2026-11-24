package sqlitestore

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"

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

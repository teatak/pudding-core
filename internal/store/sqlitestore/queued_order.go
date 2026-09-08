package sqlitestore

import (
	"context"
	"database/sql"

	"github.com/teatak/pudding-core/internal/event"
	"github.com/teatak/pudding-core/internal/store"
)

func (s *Store) ReorderQueuedInputs(ctx context.Context, sessionID string, ids []string) (*store.ReorderQueuedInputsResult, error) {
	out := &store.ReorderQueuedInputsResult{}
	err := s.tx(ctx, func(tx *sql.Tx) error {
		if _, err := getSessionTx(ctx, tx, sessionID); err != nil {
			return err
		}
		rows, err := tx.QueryContext(ctx, `SELECT session_id,client_message_id,text,parts,status,provider,model,mode,model_config,turn_id,created_at,updated_at FROM queued_inputs WHERE session_id=? AND status IN ('queued','editing') ORDER BY sort_order,rowid`, sessionID)
		if err != nil {
			return err
		}
		active := make([]*store.QueuedInput, 0)
		for rows.Next() {
			input, err := scanQueuedInput(rows)
			if err != nil {
				rows.Close()
				return err
			}
			active = append(active, input)
		}
		err = rows.Err()
		rows.Close()
		if err != nil {
			return err
		}
		ordered, err := store.OrderQueuedInputs(active, ids)
		if err != nil {
			return err
		}
		for i, input := range ordered {
			if _, err := tx.ExecContext(ctx, `UPDATE queued_inputs SET sort_order=? WHERE session_id=? AND client_message_id=?`, i+1, sessionID, input.ClientMessageID); err != nil {
				return err
			}
			ev := event.Event{SessionID: sessionID, Kind: event.InputUpdated, ClientMessageID: input.ClientMessageID, Text: input.Text, Status: string(input.Status)}
			if err := insertEventTx(ctx, tx, &ev); err != nil {
				return err
			}
			out.Events = append(out.Events, ev)
		}
		out.Inputs = ordered
		return nil
	})
	return out, err
}

package memstore

import (
	"context"

	"github.com/teatak/pudding-core/internal/event"
	"github.com/teatak/pudding-core/internal/store"
)

func (m *Memstore) ReorderQueuedInputs(_ context.Context, sessionID string, ids []string) (*store.ReorderQueuedInputsResult, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if _, ok := m.sessions[sessionID]; !ok {
		return nil, store.ErrNotFound
	}
	active := make([]*store.QueuedInput, 0)
	for _, input := range m.queued[sessionID] {
		if input.Status == store.QueuedInputQueued || input.Status == store.QueuedInputEditing {
			active = append(active, input)
		}
	}
	ordered, err := store.OrderQueuedInputs(active, ids)
	if err != nil {
		return nil, err
	}
	out := &store.ReorderQueuedInputsResult{Inputs: make([]*store.QueuedInput, 0, len(ordered))}
	next := 0
	for i, input := range m.queued[sessionID] {
		if input.Status != store.QueuedInputQueued && input.Status != store.QueuedInputEditing {
			continue
		}
		input = ordered[next]
		m.queued[sessionID][i] = input
		next++
		out.Inputs = append(out.Inputs, cloneQueuedInput(input))
		ev := event.Event{Seq: m.nextSeq(sessionID), SessionID: sessionID, Kind: event.InputUpdated, ClientMessageID: input.ClientMessageID, Text: input.Text, Status: string(input.Status)}
		m.appendEventLocked(sessionID, ev)
		out.Events = append(out.Events, ev)
	}
	return out, nil
}

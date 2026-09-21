package memstore

import (
	"context"
	"sort"
	"time"

	"github.com/teatak/pudding-core/internal/event"
	"github.com/teatak/pudding-core/internal/store"
)

type childDispatch struct{ parentTurnID, callID string }

func (m *Memstore) DispatchChild(_ context.Context, in store.DispatchChildInput) (*store.DispatchChildResult, error) {
	if err := store.ValidateDispatchChild(in); err != nil {
		return nil, err
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	parent := m.sessions[in.ParentSessionID]
	if parent == nil || parent.ArchivedAt != nil {
		return nil, store.ErrNotFound
	}
	if m.parents[parent.ID] != "" {
		return nil, store.ErrInvalidSessionRelation
	}
	for id, d := range m.dispatches {
		if d.parentTurnID == in.ParentTurnID && d.callID == in.CallID {
			if m.parents[id] != parent.ID {
				return nil, store.ErrInvalidSessionRelation
			}
			return &store.DispatchChildResult{Session: cloneSession(m.sessions[id]), Duplicate: true}, nil
		}
	}
	turn := m.turns[in.ParentTurnID]
	if turn == nil {
		return nil, store.ErrNotFound
	}
	if turn.SessionID != parent.ID || turn.Status != store.TurnRunning {
		return nil, store.ErrInvalidSessionRelation
	}
	if m.collaborationStops[turn.ID] {
		return nil, store.ErrCollaborationStopped
	}
	if err := store.PrepareChildSession(parent, in.Child); err != nil {
		return nil, err
	}
	if err := m.createSessionLocked(in.Child); err != nil {
		return nil, err
	}
	m.parents[in.Child.ID] = parent.ID
	m.dispatches[in.Child.ID] = childDispatch{turn.ID, in.CallID}
	queued, err := m.queueInputLocked(in.Input)
	if err != nil {
		return nil, err
	}
	ev := event.Event{SessionID: parent.ID, Seq: m.nextSeq(parent.ID), Kind: event.CollaborationChanged}
	m.appendEventLocked(parent.ID, ev)
	return &store.DispatchChildResult{Session: cloneSession(in.Child), Events: []event.Event{*queued.QueuedEvent, ev}}, nil
}

func (m *Memstore) StopCollaboration(_ context.Context, parentID string) ([]event.Event, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	parent := m.sessions[parentID]
	if parent == nil {
		return nil, store.ErrNotFound
	}
	if m.parents[parentID] != "" {
		return nil, store.ErrInvalidSessionRelation
	}
	for _, turn := range m.turns {
		if turn.SessionID == parentID && turn.Status == store.TurnRunning {
			m.collaborationStops[turn.ID] = true
		}
	}
	var events []event.Event
	for childID, owner := range m.parents {
		if owner != parentID {
			continue
		}
		if d, ok := m.dispatches[childID]; ok {
			m.collaborationStops[d.parentTurnID] = true
		}
		for _, input := range m.queued[childID] {
			if input.Status != store.QueuedInputQueued && input.Status != store.QueuedInputEditing {
				continue
			}
			input.Status = store.QueuedInputCancelled
			input.UpdatedAt = time.Now()
			ev := event.Event{SessionID: childID, Seq: m.nextSeq(childID), Kind: event.InputUpdated, ClientMessageID: input.ClientMessageID, Status: string(input.Status)}
			m.appendEventLocked(childID, ev)
			events = append(events, ev)
		}
	}
	ev := event.Event{SessionID: parentID, Seq: m.nextSeq(parentID), Kind: event.CollaborationChanged}
	m.appendEventLocked(parentID, ev)
	return append(events, ev), nil
}

func (m *Memstore) uncollectedChildResultsLocked(parentID string) []*store.Message {
	var results []*store.Message
	for childID, owner := range m.parents {
		if owner != parentID {
			continue
		}
		queued := false
		for _, input := range m.queued[childID] {
			if input.Status == store.QueuedInputQueued || input.Status == store.QueuedInputEditing {
				queued = true
				break
			}
		}
		if queued {
			continue
		}
		var latest *store.Turn
		for _, turn := range m.turns {
			if turn.SessionID == childID && (latest == nil || turn.CreatedAt.After(latest.CreatedAt) || turn.CreatedAt.Equal(latest.CreatedAt) && turn.ID > latest.ID) {
				latest = turn
			}
		}
		if latest == nil || latest.Status == store.TurnRunning {
			continue
		}
		delivered := false
		for _, msg := range m.messages[parentID] {
			if msg.ID == "collaboration_result_"+latest.ID {
				delivered = true
				break
			}
		}
		if !delivered {
			results = append(results, store.ChildResultMessage(parentID, m.sessions[childID], latest, m.messages[childID]))
		}
	}
	sort.Slice(results, func(i, j int) bool { return results[i].ID < results[j].ID })
	return results
}

func (m *Memstore) HasUncollectedChildResults(_ context.Context, parentID string) (bool, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	return len(m.uncollectedChildResultsLocked(parentID)) > 0, nil
}

func (m *Memstore) CollectChildResults(_ context.Context, parentID string) ([]event.Event, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	var events []event.Event
	for _, msg := range m.uncollectedChildResultsLocked(parentID) {
		m.messages[parentID] = append(m.messages[parentID], msg)
		ev := event.Event{SessionID: parentID, Seq: m.nextSeq(parentID), Kind: event.CollaborationChanged}
		m.appendEventLocked(parentID, ev)
		events = append(events, ev)
	}
	return events, nil
}

func (m *Memstore) QueueChildInput(_ context.Context, parentID, parentTurnID string, in store.QueueInputInput) (*store.QueueInputResult, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.parents[in.SessionID] != parentID {
		return nil, store.ErrInvalidSessionRelation
	}
	turn := m.turns[parentTurnID]
	if turn == nil {
		return nil, store.ErrNotFound
	}
	if turn.SessionID != parentID || turn.Status != store.TurnRunning {
		return nil, store.ErrInvalidSessionRelation
	}
	if m.collaborationStops[parentTurnID] {
		return nil, store.ErrCollaborationStopped
	}
	return m.queueInputLocked(in)
}

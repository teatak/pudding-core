package memstore

import (
	"context"
	"sort"
	"time"

	"github.com/teatak/pudding-core/internal/store"
)

func cloneScheduledTask(t *store.ScheduledTask) *store.ScheduledTask {
	c := *t
	c.Schedule.Weekdays = append([]int(nil), t.Schedule.Weekdays...)
	if t.Schedule.At != nil {
		v := *t.Schedule.At
		c.Schedule.At = &v
	}
	if t.NextAt != nil {
		v := *t.NextAt
		c.NextAt = &v
	}
	return &c
}
func cloneScheduledRun(r *store.ScheduledTaskRun) *store.ScheduledTaskRun {
	c := *r
	c.Schedule = cloneScheduledTask(&store.ScheduledTask{Schedule: r.Schedule}).Schedule
	if r.SkippedThrough != nil {
		v := *r.SkippedThrough
		c.SkippedThrough = &v
	}
	return &c
}
func (m *Memstore) scheduledTargetLocked(id string) error {
	t := m.sessions[id]
	if t == nil || t.ArchivedAt != nil {
		return store.ErrNotFound
	}
	if m.parents[id] != "" {
		return store.ErrInvalidSessionRelation
	}
	return nil
}
func (m *Memstore) CreateScheduledTask(_ context.Context, t *store.ScheduledTask, session *store.Session) (*store.ScheduledTask, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, prev := range m.scheduledTasks {
		if prev.SessionID == t.SessionID && prev.RequestID == t.RequestID {
			if prev.RequestHash != t.RequestHash {
				return nil, store.ErrScheduleConflict
			}
			return cloneScheduledTask(prev), nil
		}
	}
	if t.NextAt == nil {
		return nil, store.ErrInvalidSchedule
	}
	if session != nil {
		if session.ID != t.SessionID {
			return nil, store.ErrInvalidSession
		}
		if err := store.NormalizeSessionProviderModel(session); err != nil {
			return nil, err
		}
		if err := m.createSessionLocked(session); err != nil {
			return nil, err
		}
	} else if err := m.scheduledTargetLocked(t.SessionID); err != nil {
		return nil, err
	}
	m.scheduledTasks[t.ID] = cloneScheduledTask(t)
	return cloneScheduledTask(t), nil
}
func (m *Memstore) GetScheduledTask(_ context.Context, id string) (*store.ScheduledTask, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	t := m.scheduledTasks[id]
	if t == nil {
		return nil, store.ErrNotFound
	}
	return cloneScheduledTask(t), nil
}
func (m *Memstore) ListScheduledTasks(_ context.Context, sessionID string, includeDeleted bool) ([]*store.ScheduledTask, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	out := make([]*store.ScheduledTask, 0)
	for _, t := range m.scheduledTasks {
		if (sessionID == "" || sessionID == t.SessionID) && (!t.Deleted || includeDeleted) {
			out = append(out, cloneScheduledTask(t))
		}
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].CreatedAt.Equal(out[j].CreatedAt) {
			return out[i].ID < out[j].ID
		}
		return out[i].CreatedAt.After(out[j].CreatedAt)
	})
	return out, nil
}
func (m *Memstore) UpdateScheduledTask(_ context.Context, id string, in store.ScheduledTaskUpdate, now time.Time) (*store.ScheduledTask, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	prev := m.scheduledTasks[id]
	if prev == nil {
		return nil, store.ErrNotFound
	}
	t := cloneScheduledTask(prev)
	if in.Enabled != nil && *in.Enabled {
		if err := m.scheduledTargetLocked(t.SessionID); err != nil {
			return nil, err
		}
	}
	if err := store.ApplyScheduledTaskUpdate(t, in, now); err != nil {
		return nil, err
	}
	m.scheduledTasks[id] = t
	return cloneScheduledTask(t), nil
}
func (m *Memstore) AcceptScheduledTask(_ context.Context, id string, in store.ScheduledTaskAccept) (*store.ScheduledTaskRun, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if in.RequestID != "" {
		for _, r := range m.scheduledRuns {
			if r.TaskID == id && r.Key == "manual:"+in.RequestID {
				return cloneScheduledRun(r), nil
			}
		}
	}
	prev := m.scheduledTasks[id]
	if prev == nil {
		return nil, store.ErrNotFound
	}
	if err := m.scheduledTargetLocked(prev.SessionID); err != nil {
		return nil, err
	}
	t := cloneScheduledTask(prev)
	r, err := store.PrepareScheduledTaskRun(t, in)
	if err != nil {
		return nil, err
	}
	for _, old := range m.scheduledRuns {
		if old.TaskID == id && old.Key == r.Key {
			return nil, store.ErrScheduleConflict
		}
	}
	m.scheduledTasks[id] = t
	m.scheduledRuns[r.ID] = r
	m.scheduledRunOrder = append(m.scheduledRunOrder, r.ID)
	return cloneScheduledRun(r), nil
}
func (m *Memstore) ListScheduledTaskRuns(_ context.Context, taskID string, limit, offset int) ([]*store.ScheduledTaskRun, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	all := make([]*store.ScheduledTaskRun, 0)
	for i := len(m.scheduledRunOrder) - 1; i >= 0; i-- {
		r := m.scheduledRuns[m.scheduledRunOrder[i]]
		if r != nil && (taskID == "" || r.TaskID == taskID) {
			all = append(all, cloneScheduledRun(r))
		}
	}
	if offset >= len(all) {
		return []*store.ScheduledTaskRun{}, nil
	}
	all = all[offset:]
	if limit >= 0 && len(all) > limit {
		all = all[:limit]
	}
	return all, nil
}
func (m *Memstore) PendingScheduledTaskRuns(_ context.Context) ([]*store.ScheduledTaskRun, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	out := make([]*store.ScheduledTaskRun, 0)
	for _, id := range m.scheduledRunOrder {
		if r := m.scheduledRuns[id]; r != nil && r.Handoff == "pending" {
			out = append(out, cloneScheduledRun(r))
		}
	}
	return out, nil
}
func (m *Memstore) SetScheduledTaskHandoff(_ context.Context, id, state, reason string) error {
	if state != "submitted" && state != "failed" {
		return store.ErrInvalidSchedule
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if r := m.scheduledRuns[id]; r != nil && r.Handoff == "pending" {
		r.Handoff = state
		r.Reason = reason
	}
	return nil
}
func (m *Memstore) FindInputTurn(_ context.Context, sessionID, clientID string) (*store.Turn, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, msg := range m.messages[sessionID] {
		if msg.Role == store.RoleUser && msg.ClientMessageID == clientID {
			if t := m.turns[msg.TurnID]; t != nil {
				c := *t
				return &c, nil
			}
		}
	}
	return nil, store.ErrNotFound
}
func (m *Memstore) FindRetryTurn(_ context.Context, sessionID, turnID string) (*store.Turn, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, t := range m.turns {
		if t.SessionID == sessionID && t.RetryOfTurnID == turnID {
			c := *t
			return &c, nil
		}
	}
	return nil, store.ErrNotFound
}

func (m *Memstore) FindQueuedInput(_ context.Context, sessionID, clientID string) (*store.QueuedInput, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, q := range m.queued[sessionID] {
		if q.ClientMessageID == clientID {
			return cloneQueuedInput(q), nil
		}
	}
	return nil, store.ErrNotFound
}

func (m *Memstore) GetScheduledTaskRun(_ context.Context, sessionID, id string) (*store.ScheduledTaskRun, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	r := m.scheduledRuns[id]
	if r == nil || r.SessionID != sessionID {
		return nil, store.ErrNotFound
	}
	return cloneScheduledRun(r), nil
}
func (m *Memstore) GetScheduledTaskTurn(_ context.Context, sessionID, id string) (*store.ConversationTurn, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	t := m.turns[id]
	if t == nil || t.SessionID != sessionID {
		return nil, store.ErrNotFound
	}
	var messages []*store.Message
	for _, msg := range m.messages[sessionID] {
		if msg.TurnID == id {
			messages = append(messages, cloneMessage(msg))
		}
	}
	return &store.ConversationTurn{ID: t.ID, SessionID: t.SessionID, Status: t.Status, Error: t.Error, CreatedAt: t.CreatedAt, UpdatedAt: t.UpdatedAt, Messages: messages}, nil
}

package memstore

import (
	"context"
	"encoding/json"
	"github.com/teatak/pudding-core/internal/store"
	"sort"
)

func cloneAction(a *store.WorkbenchAction) *store.WorkbenchAction {
	b, _ := json.Marshal(a)
	var out store.WorkbenchAction
	_ = json.Unmarshal(b, &out)
	return &out
}
func (m *Memstore) CreateWorkbenchAction(_ context.Context, a *store.WorkbenchAction) (*store.WorkbenchAction, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.workbenchActions == nil {
		m.workbenchActions = map[string]*store.WorkbenchAction{}
	}
	for _, old := range m.workbenchActions {
		if old.WorkbenchID == a.WorkbenchID && old.ClientRequestID == a.ClientRequestID {
			if old.RequestHash != a.RequestHash {
				return nil, store.ErrWorkbenchConflict
			}
			return cloneAction(old), nil
		}
	}
	m.workbenchActions[a.ID] = cloneAction(a)
	return cloneAction(a), nil
}
func (m *Memstore) GetWorkbenchAction(_ context.Context, wid, id string) (*store.WorkbenchAction, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	a := m.workbenchActions[id]
	if a == nil || a.WorkbenchID != wid {
		return nil, store.ErrNotFound
	}
	return cloneAction(a), nil
}
func (m *Memstore) ListWorkbenchActions(_ context.Context, wid string) ([]*store.WorkbenchAction, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	out := []*store.WorkbenchAction{}
	for _, a := range m.workbenchActions {
		if a.WorkbenchID == wid {
			out = append(out, cloneAction(a))
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].CreatedAt.After(out[j].CreatedAt) })
	if len(out) > 100 {
		out = out[:100]
	}
	return out, nil
}
func (m *Memstore) ClaimWorkbenchAction(_ context.Context, wid, id string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	a := m.workbenchActions[id]
	w := m.workbenches[wid]
	if a == nil || a.WorkbenchID != wid || w == nil || w.Deleted {
		return store.ErrNotFound
	}
	if a.State != "prepared" || w.Revision != a.Spec.ResourceRevision || w.BindingVersion != a.Spec.BindingVersion || w.ActiveRevision != a.Spec.RevisionHash {
		return store.ErrWorkbenchConflict
	}
	a.State = "executing"
	return nil
}
func (m *Memstore) FinishWorkbenchAction(_ context.Context, wid, id, state string, result json.RawMessage) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	a := m.workbenchActions[id]
	if a == nil || a.WorkbenchID != wid {
		return store.ErrNotFound
	}
	if a.State != "executing" || (state != "succeeded" && state != "failed" && state != "unknown") {
		return store.ErrWorkbenchConflict
	}
	a.State = state
	a.Result = append(json.RawMessage(nil), result...)
	return nil
}

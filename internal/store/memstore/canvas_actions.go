package memstore

import (
	"context"
	"encoding/json"
	"github.com/teatak/pudding-core/internal/store"
	"sort"
)

func cloneAction(a *store.CanvasAction) *store.CanvasAction {
	b, _ := json.Marshal(a)
	var out store.CanvasAction
	_ = json.Unmarshal(b, &out)
	return &out
}
func (m *Memstore) CreateCanvasAction(_ context.Context, a *store.CanvasAction) (*store.CanvasAction, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.canvasActions == nil {
		m.canvasActions = map[string]*store.CanvasAction{}
	}
	for _, old := range m.canvasActions {
		if old.CanvasID == a.CanvasID && old.ClientRequestID == a.ClientRequestID {
			if old.RequestHash != a.RequestHash {
				return nil, store.ErrCanvasConflict
			}
			return cloneAction(old), nil
		}
	}
	m.canvasActions[a.ID] = cloneAction(a)
	return cloneAction(a), nil
}
func (m *Memstore) GetCanvasAction(_ context.Context, wid, id string) (*store.CanvasAction, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	a := m.canvasActions[id]
	if a == nil || a.CanvasID != wid {
		return nil, store.ErrNotFound
	}
	return cloneAction(a), nil
}
func (m *Memstore) ListCanvasActions(_ context.Context, wid string) ([]*store.CanvasAction, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	out := []*store.CanvasAction{}
	for _, a := range m.canvasActions {
		if a.CanvasID == wid {
			out = append(out, cloneAction(a))
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].CreatedAt.After(out[j].CreatedAt) })
	if len(out) > 100 {
		out = out[:100]
	}
	return out, nil
}
func (m *Memstore) ClaimCanvasAction(_ context.Context, wid, id string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	a := m.canvasActions[id]
	w := m.canvases[wid]
	if a == nil || a.CanvasID != wid || w == nil || w.Deleted {
		return store.ErrNotFound
	}
	if a.State != "prepared" || w.Revision != a.Spec.ResourceRevision || w.BindingVersion != a.Spec.BindingVersion || w.ActiveRevision != a.Spec.RevisionHash {
		return store.ErrCanvasConflict
	}
	a.State = "executing"
	return nil
}
func (m *Memstore) FinishCanvasAction(_ context.Context, wid, id, state string, result json.RawMessage) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	a := m.canvasActions[id]
	if a == nil || a.CanvasID != wid {
		return store.ErrNotFound
	}
	if a.State != "executing" || (state != "succeeded" && state != "failed" && state != "unknown") {
		return store.ErrCanvasConflict
	}
	a.State = state
	a.Result = append(json.RawMessage(nil), result...)
	return nil
}

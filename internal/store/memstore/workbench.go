package memstore

import (
	"context"
	"encoding/json"
	"maps"
	"sort"
	"time"

	"github.com/teatak/pudding-core/internal/store"
)

func cloneWorkbench(w *store.Workbench) *store.Workbench {
	if w == nil {
		return nil
	}
	copy := *w
	copy.Bindings = maps.Clone(w.Bindings)
	return &copy
}
func cloneWorkbenchRevision(r *store.WorkbenchRevision) *store.WorkbenchRevision {
	copy := *r
	copy.BuildReceipt = append(json.RawMessage(nil), r.BuildReceipt...)
	return &copy
}
func (m *Memstore) ListWorkbenches(context.Context) ([]*store.Workbench, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	out := []*store.Workbench{}
	for _, w := range m.workbenches {
		if !w.Deleted {
			out = append(out, cloneWorkbench(w))
		}
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].UpdatedAt.Equal(out[j].UpdatedAt) {
			return out[i].ID < out[j].ID
		}
		return out[i].UpdatedAt.After(out[j].UpdatedAt)
	})
	return out, nil
}
func (m *Memstore) GetWorkbench(_ context.Context, id string) (*store.Workbench, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	w := m.workbenches[id]
	if w == nil || w.Deleted {
		return nil, store.ErrNotFound
	}
	return cloneWorkbench(w), nil
}
func (m *Memstore) CreateWorkbench(_ context.Context, w *store.Workbench) (*store.Workbench, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.workbenches[w.ID] != nil {
		return nil, store.ErrWorkbenchConflict
	}
	if w.SourceSessionID != "" && m.sessions[w.SourceSessionID] == nil {
		return nil, store.ErrNotFound
	}
	copy := cloneWorkbench(w)
	copy.Revision = 1
	copy.BindingVersion = 1
	copy.Bindings = map[string]string{}
	m.workbenches[w.ID] = copy
	return cloneWorkbench(copy), nil
}
func (m *Memstore) UpdateWorkbench(_ context.Context, w *store.Workbench, expected int64) (*store.Workbench, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	old := m.workbenches[w.ID]
	if old == nil || old.Deleted {
		return nil, store.ErrNotFound
	}
	if old.Revision != expected {
		return nil, store.ErrWorkbenchConflict
	}
	if w.ActiveRevision != "" {
		r := m.workbenchRevisions[w.ID+"/"+w.ActiveRevision]
		if r == nil || len(r.BuildReceipt) == 0 {
			return nil, store.ErrWorkbenchConflict
		}
	}
	copy := cloneWorkbench(w)
	copy.Revision = expected + 1
	copy.UpdatedAt = time.Now().UTC()
	m.workbenches[w.ID] = copy
	if copy.Deleted {
		for key, mount := range m.canvas {
			if mount.ResourceID == w.ID {
				delete(m.canvas, key)
			}
		}
	}
	return cloneWorkbench(copy), nil
}
func (m *Memstore) SaveWorkbenchRevision(_ context.Context, r *store.WorkbenchRevision, expected int64) (*store.Workbench, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.saveCanvasRevisionLocked(r, expected)
}
func (m *Memstore) saveCanvasRevisionLocked(r *store.WorkbenchRevision, expected int64) (*store.Workbench, error) {
	w := m.workbenches[r.WorkbenchID]
	if w == nil || w.Deleted {
		return nil, store.ErrNotFound
	}
	requestKey := r.WorkbenchID + "/" + r.ClientRequestID
	if hash, ok := m.workbenchSaves[requestKey]; ok {
		if hash != r.Hash {
			return nil, store.ErrWorkbenchConflict
		}
		return cloneWorkbench(w), nil
	}
	if w.Revision != expected {
		return nil, store.ErrWorkbenchConflict
	}
	key := r.WorkbenchID + "/" + r.Hash
	if m.workbenchRevisions[key] == nil {
		copy := cloneWorkbenchRevision(r)
		copy.ParentRevision = w.HeadRevision
		m.workbenchRevisions[key] = copy
	}
	m.workbenchSaves[requestKey] = r.Hash

	w.HeadRevision = r.Hash
	w.Revision++
	w.UpdatedAt = r.CreatedAt
	return cloneWorkbench(w), nil
}
func (m *Memstore) ListWorkbenchRevisions(_ context.Context, id string) ([]*store.WorkbenchRevision, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	out := []*store.WorkbenchRevision{}
	for _, r := range m.workbenchRevisions {
		if r.WorkbenchID == id {
			out = append(out, cloneWorkbenchRevision(r))
		}
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].CreatedAt.Equal(out[j].CreatedAt) {
			return out[i].Hash < out[j].Hash
		}
		return out[i].CreatedAt.After(out[j].CreatedAt)
	})
	return out, nil
}
func (m *Memstore) GetWorkbenchRevision(_ context.Context, id, hash string) (*store.WorkbenchRevision, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	r := m.workbenchRevisions[id+"/"+hash]
	if r == nil {
		return nil, store.ErrNotFound
	}
	return cloneWorkbenchRevision(r), nil
}
func (m *Memstore) PutWorkbenchBuildReceipt(_ context.Context, id, hash string, receipt json.RawMessage) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	r := m.workbenchRevisions[id+"/"+hash]
	if r == nil {
		return store.ErrNotFound
	}
	r.BuildReceipt = append(json.RawMessage(nil), receipt...)
	return nil
}

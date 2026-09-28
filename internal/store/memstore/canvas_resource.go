package memstore

import (
	"context"
	"encoding/json"
	"maps"
	"sort"
	"time"

	"github.com/teatak/pudding-core/internal/store"
)

func cloneCanvas(w *store.Canvas) *store.Canvas {
	if w == nil {
		return nil
	}
	copy := *w
	copy.Bindings = maps.Clone(w.Bindings)
	return &copy
}
func cloneCanvasRevision(r *store.CanvasRevision) *store.CanvasRevision {
	copy := *r
	copy.BuildReceipt = append(json.RawMessage(nil), r.BuildReceipt...)
	return &copy
}
func (m *Memstore) ListCanvases(context.Context) ([]*store.Canvas, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	out := []*store.Canvas{}
	for _, w := range m.canvases {
		if !w.Deleted {
			out = append(out, cloneCanvas(w))
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
func (m *Memstore) GetCanvas(_ context.Context, id string) (*store.Canvas, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	w := m.canvases[id]
	if w == nil || w.Deleted {
		return nil, store.ErrNotFound
	}
	return cloneCanvas(w), nil
}
func (m *Memstore) CreateCanvas(_ context.Context, w *store.Canvas) (*store.Canvas, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.canvases[w.ID] != nil {
		return nil, store.ErrCanvasConflict
	}
	if w.SourceSessionID != "" && m.sessions[w.SourceSessionID] == nil {
		return nil, store.ErrNotFound
	}
	copy := cloneCanvas(w)
	copy.Revision = 1
	copy.BindingVersion = 1
	copy.Bindings = map[string]string{}
	m.canvases[w.ID] = copy
	return cloneCanvas(copy), nil
}
func (m *Memstore) UpdateCanvas(_ context.Context, w *store.Canvas, expected int64) (*store.Canvas, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	old := m.canvases[w.ID]
	if old == nil || old.Deleted {
		return nil, store.ErrNotFound
	}
	if old.Revision != expected {
		return nil, store.ErrCanvasConflict
	}
	if w.ActiveRevision != "" {
		r := m.canvasRevisions[w.ID+"/"+w.ActiveRevision]
		if r == nil || len(r.BuildReceipt) == 0 {
			return nil, store.ErrCanvasConflict
		}
	}
	copy := cloneCanvas(w)
	copy.Revision = expected + 1
	copy.UpdatedAt = time.Now().UTC()
	m.canvases[w.ID] = copy
	if copy.Deleted {
		for key, mount := range m.canvas {
			if mount.ResourceID == w.ID {
				delete(m.canvas, key)
			}
		}
	}
	return cloneCanvas(copy), nil
}
func (m *Memstore) SaveCanvasRevision(_ context.Context, r *store.CanvasRevision, baseHash string) (*store.Canvas, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.saveCanvasRevisionLocked(r, baseHash)
}
func (m *Memstore) saveCanvasRevisionLocked(r *store.CanvasRevision, baseHash string) (*store.Canvas, error) {
	w := m.canvases[r.CanvasID]
	if w == nil || w.Deleted {
		return nil, store.ErrNotFound
	}
	requestKey := r.CanvasID + "/" + r.ClientRequestID
	if hash, ok := m.canvasSaves[requestKey]; ok {
		if hash != r.Hash {
			return nil, store.ErrCanvasConflict
		}
		return cloneCanvas(w), nil
	}
	if w.HeadRevision != baseHash {
		return nil, store.ErrCanvasConflict
	}
	key := r.CanvasID + "/" + r.Hash
	if m.canvasRevisions[key] == nil {
		copy := cloneCanvasRevision(r)
		copy.ParentRevision = w.HeadRevision
		m.canvasRevisions[key] = copy
	}
	m.canvasSaves[requestKey] = r.Hash

	w.HeadRevision = r.Hash
	w.Revision++
	w.UpdatedAt = r.CreatedAt
	return cloneCanvas(w), nil
}
func (m *Memstore) ListCanvasRevisions(_ context.Context, id string) ([]*store.CanvasRevision, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	out := []*store.CanvasRevision{}
	for _, r := range m.canvasRevisions {
		if r.CanvasID == id {
			out = append(out, cloneCanvasRevision(r))
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
func (m *Memstore) GetCanvasRevision(_ context.Context, id, hash string) (*store.CanvasRevision, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	r := m.canvasRevisions[id+"/"+hash]
	if r == nil {
		return nil, store.ErrNotFound
	}
	return cloneCanvasRevision(r), nil
}
func (m *Memstore) PutCanvasBuildReceipt(_ context.Context, id, hash string, receipt json.RawMessage) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	r := m.canvasRevisions[id+"/"+hash]
	if r == nil {
		return store.ErrNotFound
	}
	r.BuildReceipt = append(json.RawMessage(nil), receipt...)
	return nil
}

package memstore

import (
	"context"
	"sort"
	"time"

	"github.com/teatak/pudding-core/internal/store"
)

func (m *Memstore) canvasProjection(v *store.CanvasItem) (*store.CanvasItem, error) {
	w := m.canvases[v.ResourceID]
	if w == nil || w.Deleted {
		return nil, store.ErrNotFound
	}
	return store.ProjectCanvasItem(v, w), nil
}
func (m *Memstore) ListCanvasItems(_ context.Context, session string) ([]*store.CanvasItem, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.sessions[session] == nil {
		return nil, store.ErrNotFound
	}
	out := []*store.CanvasItem{}
	for _, v := range m.canvas {
		if v.SessionID == session {
			if w := m.canvases[v.ResourceID]; w != nil && !w.Deleted {
				item, err := m.canvasProjection(v)
				if err != nil {
					return nil, err
				}
				out = append(out, item)
			}
		}
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].CreatedAt.Equal(out[j].CreatedAt) {
			return out[i].ID < out[j].ID
		}
		return out[i].CreatedAt.Before(out[j].CreatedAt)
	})
	return out, nil
}
func (m *Memstore) DeleteCanvasItem(_ context.Context, session, id string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.sessions[session] == nil {
		return store.ErrNotFound
	}
	key := canvasMapKey(session, id)
	if m.canvas[key] == nil {
		return store.ErrNotFound
	}
	delete(m.canvas, key)
	return nil
}
func (m *Memstore) OpenCanvasResource(_ context.Context, session, id, itemID string) (*store.CanvasItem, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.sessions[session] == nil {
		return nil, store.ErrNotFound
	}
	w := m.canvases[id]
	if w == nil || w.Deleted {
		return nil, store.ErrNotFound
	}
	for _, v := range m.canvas {
		if v.SessionID == session && v.ResourceID == id {
			v.Visible = true
			return m.canvasProjection(v)
		}
	}
	key := canvasMapKey(session, itemID)
	if m.canvas[key] != nil {
		return nil, store.ErrCanvasConflict
	}
	v := &store.CanvasItem{ID: itemID, SessionID: session, ResourceID: id, Visible: true, CreatedAt: time.Now().UTC()}
	m.canvas[key] = v
	return m.canvasProjection(v)
}

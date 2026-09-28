package memstore

import (
	"context"
	"github.com/teatak/pudding-core/internal/store"
	"sort"
)

func (m *Memstore) ListCanvasLinks(_ context.Context, wid string) ([]*store.CanvasLink, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	out := []*store.CanvasLink{}
	for _, l := range m.canvasLinks {
		if l.CanvasID == wid {
			c := *l
			out = append(out, &c)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].CreatedAt.After(out[j].CreatedAt) })
	return out, nil
}
func (m *Memstore) PutCanvasLink(_ context.Context, l *store.CanvasLink, expected int64) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	w := m.canvases[l.CanvasID]
	if w == nil || w.Deleted {
		return store.ErrNotFound
	}
	if w.Revision != expected {
		return store.ErrCanvasConflict
	}
	if m.canvasLinks == nil {
		m.canvasLinks = map[string]*store.CanvasLink{}
	}
	for _, old := range m.canvasLinks {
		if old.CanvasID == l.CanvasID && old.Left == l.Left && old.Right == l.Right {
			return nil
		}
	}
	c := *l
	m.canvasLinks[l.ID] = &c
	return nil
}
func (m *Memstore) DeleteCanvasLink(_ context.Context, wid, id string, expected int64) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	w := m.canvases[wid]
	if w == nil || w.Deleted {
		return store.ErrNotFound
	}
	if w.Revision != expected {
		return store.ErrCanvasConflict
	}
	if l := m.canvasLinks[id]; l != nil && l.CanvasID == wid {
		delete(m.canvasLinks, id)
	}
	return nil
}

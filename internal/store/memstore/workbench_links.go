package memstore

import (
	"context"
	"github.com/teatak/pudding-core/internal/store"
	"sort"
)

func (m *Memstore) ListWorkbenchLinks(_ context.Context, wid string) ([]*store.WorkbenchLink, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	out := []*store.WorkbenchLink{}
	for _, l := range m.workbenchLinks {
		if l.WorkbenchID == wid {
			c := *l
			out = append(out, &c)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].CreatedAt.After(out[j].CreatedAt) })
	return out, nil
}
func (m *Memstore) PutWorkbenchLink(_ context.Context, l *store.WorkbenchLink, expected int64) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	w := m.workbenches[l.WorkbenchID]
	if w == nil || w.Deleted {
		return store.ErrNotFound
	}
	if w.Revision != expected {
		return store.ErrWorkbenchConflict
	}
	if m.workbenchLinks == nil {
		m.workbenchLinks = map[string]*store.WorkbenchLink{}
	}
	for _, old := range m.workbenchLinks {
		if old.WorkbenchID == l.WorkbenchID && old.Left == l.Left && old.Right == l.Right {
			return nil
		}
	}
	c := *l
	m.workbenchLinks[l.ID] = &c
	return nil
}
func (m *Memstore) DeleteWorkbenchLink(_ context.Context, wid, id string, expected int64) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	w := m.workbenches[wid]
	if w == nil || w.Deleted {
		return store.ErrNotFound
	}
	if w.Revision != expected {
		return store.ErrWorkbenchConflict
	}
	if l := m.workbenchLinks[id]; l != nil && l.WorkbenchID == wid {
		delete(m.workbenchLinks, id)
	}
	return nil
}

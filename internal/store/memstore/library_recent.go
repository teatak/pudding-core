package memstore

import (
	"context"
	"path/filepath"
	"sort"
	"time"

	"github.com/teatak/pudding-core/internal/store"
)

func (m *Memstore) ListLibraryRecentOpens(_ context.Context, actor string) ([]*store.LibraryRecentOpen, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.sessions[actor] == nil {
		return nil, store.ErrNotFound
	}
	out := make([]*store.LibraryRecentOpen, 0, len(m.recentOpens))
	for _, e := range m.recentOpens {
		copy := *e
		if e.Kind == "file" {
			copy.Title = filepath.Base(e.Path)
		} else if item := m.canvas[canvasMapKey(e.SourceSessionID, e.ItemID)]; item != nil {
			copy.Title = item.Title
			copy.CanvasKind = item.Kind
		}
		out = append(out, &copy)
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].OpenedAt.Equal(out[j].OpenedAt) {
			return out[i].ID > out[j].ID
		}
		return out[i].OpenedAt.After(out[j].OpenedAt)
	})
	return out, nil
}

func (m *Memstore) RecordLibraryRecentOpen(_ context.Context, actor string, e store.LibraryRecentOpen) error {
	if err := store.ValidateLibraryRecentOpen(e); err != nil {
		return err
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.sessions[actor] == nil {
		return store.ErrNotFound
	}
	if e.Kind == "canvas" && m.canvas[canvasMapKey(actor, e.ItemID)] == nil {
		return store.ErrNotFound
	}
	e.SourceSessionID = actor
	e.OpenedAt = time.Now().UTC()
	e.ID = store.NewID("recent")
	for _, existing := range m.recentOpens {
		if existing.Kind == e.Kind && (e.Kind == "file" && existing.RootPath == e.RootPath && existing.Path == e.Path || e.Kind == "canvas" && existing.SourceSessionID == actor && existing.ItemID == e.ItemID) {
			e.ID = existing.ID
			break
		}
	}
	e.Title = ""
	e.CanvasKind = ""
	m.recentOpens[e.ID] = &e
	if len(m.recentOpens) > store.LibraryRecentRetainLimit {
		var oldest *store.LibraryRecentOpen
		for _, entry := range m.recentOpens {
			if oldest == nil || entry.OpenedAt.Before(oldest.OpenedAt) || entry.OpenedAt.Equal(oldest.OpenedAt) && entry.ID < oldest.ID {
				oldest = entry
			}
		}
		delete(m.recentOpens, oldest.ID)
	}
	return nil
}

func (m *Memstore) DeleteLibraryRecentOpen(_ context.Context, actor, kind, id string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.sessions[actor] == nil {
		return store.ErrNotFound
	}
	if kind == "web" {
		delete(m.browserHistory, id)
	} else if entry := m.recentOpens[id]; entry != nil && entry.Kind == kind {
		delete(m.recentOpens, id)
	}
	return nil
}
func (m *Memstore) ClearLibraryRecentOpens(_ context.Context, actor, kind string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.sessions[actor] == nil {
		return store.ErrNotFound
	}
	if kind == "" || kind == "web" {
		clear(m.browserHistory)
	}
	for id, entry := range m.recentOpens {
		if kind == "" || entry.Kind == kind {
			delete(m.recentOpens, id)
		}
	}
	return nil
}

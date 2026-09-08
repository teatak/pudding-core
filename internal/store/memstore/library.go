package memstore

import (
	"context"
	"errors"
	"sort"
	"time"

	"github.com/teatak/pudding-core/internal/store"
)

func (m *Memstore) ListLibraryFavorites(_ context.Context, actor string) ([]*store.LibraryFavorite, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.sessions[actor] == nil {
		return nil, store.ErrNotFound
	}
	out := make([]*store.LibraryFavorite, 0, len(m.favorites))
	for _, f := range m.favorites {
		copy := *f
		out = append(out, &copy)
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].CreatedAt.Equal(out[j].CreatedAt) {
			return out[i].ID < out[j].ID
		}
		return out[i].CreatedAt.After(out[j].CreatedAt)
	})
	return out, nil
}
func (m *Memstore) PutLibraryFavorite(_ context.Context, actor string, f store.LibraryFavorite) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.sessions[actor] == nil {
		return store.ErrNotFound
	}
	if f.Kind != "canvas" && f.Kind != "web" {
		return errors.New("invalid favorite kind")
	}
	if f.Kind == "canvas" && m.savedCanvas[f.SavedItemID] == nil {
		return store.ErrNotFound
	}
	for _, v := range m.favorites {
		if v.Kind == f.Kind && (f.Kind == "canvas" && v.SavedItemID == f.SavedItemID || f.Kind == "web" && v.URL == f.URL) {
			return nil
		}
	}
	f.CreatedAt = time.Now()
	m.favorites[f.ID] = &f
	return nil
}
func (m *Memstore) DeleteLibraryFavorite(_ context.Context, actor, id string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.sessions[actor] == nil {
		return store.ErrNotFound
	}
	delete(m.favorites, id)
	return nil
}

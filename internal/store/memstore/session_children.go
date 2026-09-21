package memstore

import (
	"context"
	"sort"

	"github.com/teatak/pudding-core/internal/store"
)

func (m *Memstore) CreateChildSession(_ context.Context, parentSessionID string, child *store.Session) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	parent := m.sessions[parentSessionID]
	if parent == nil || parent.ArchivedAt != nil {
		return store.ErrNotFound
	}
	if m.parents[parentSessionID] != "" {
		return store.ErrInvalidSessionRelation
	}
	if err := store.PrepareChildSession(parent, child); err != nil {
		return err
	}
	if err := m.createSessionLocked(child); err != nil {
		return err
	}
	m.parents[child.ID] = parentSessionID
	return nil
}

func (m *Memstore) ParentSessionID(_ context.Context, sessionID string) (string, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.sessions[sessionID] == nil {
		return "", store.ErrNotFound
	}
	return m.parents[sessionID], nil
}

func (m *Memstore) ListChildSessions(_ context.Context, parentSessionID string) ([]*store.Session, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.sessions[parentSessionID] == nil {
		return nil, store.ErrNotFound
	}
	children := make([]*store.Session, 0)
	for childID, parentID := range m.parents {
		if parentID == parentSessionID {
			child := cloneSession(m.sessions[childID])
			child.Running = m.runningLocked(childID)
			children = append(children, child)
		}
	}
	sort.Slice(children, func(i, j int) bool {
		if !children[i].CreatedAt.Equal(children[j].CreatedAt) {
			return children[i].CreatedAt.Before(children[j].CreatedAt)
		}
		return children[i].ID < children[j].ID
	})
	return children, nil
}

func (m *Memstore) sessionGroupIDsLocked(parentSessionID string) []string {
	ids := []string{parentSessionID}
	for childID, parentID := range m.parents {
		if parentID == parentSessionID {
			ids = append(ids, childID)
		}
	}
	return ids
}

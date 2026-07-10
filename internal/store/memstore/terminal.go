package memstore

import (
	"context"
	"sort"
	"time"

	"github.com/teatak/pudding-core/internal/store"
)

func (m *Memstore) CreateTerminal(_ context.Context, item *store.Terminal) error {
	if err := store.NormalizeTerminal(item); err != nil {
		return err
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if _, ok := m.sessions[item.SessionID]; !ok {
		return store.ErrNotFound
	}
	if m.terminals[item.SessionID] == nil {
		m.terminals[item.SessionID] = make(map[string]*store.Terminal)
	}
	if _, exists := m.terminals[item.SessionID][item.ID]; exists {
		return store.ErrInvalidTerminal
	}
	now := time.Now()
	item.CreatedAt = now
	item.UpdatedAt = now
	m.terminals[item.SessionID][item.ID] = cloneTerminal(item)
	return nil
}

func (m *Memstore) GetTerminal(_ context.Context, sessionID, terminalID string) (*store.Terminal, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	item := m.terminals[sessionID][terminalID]
	if item == nil {
		return nil, store.ErrNotFound
	}
	return cloneTerminal(item), nil
}

func (m *Memstore) ListTerminals(_ context.Context, sessionID string) ([]*store.Terminal, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if _, ok := m.sessions[sessionID]; !ok {
		return nil, store.ErrNotFound
	}
	out := make([]*store.Terminal, 0, len(m.terminals[sessionID]))
	for _, item := range m.terminals[sessionID] {
		out = append(out, cloneTerminal(item))
	}
	sort.Slice(out, func(i, j int) bool {
		if !out[i].CreatedAt.Equal(out[j].CreatedAt) {
			return out[i].CreatedAt.Before(out[j].CreatedAt)
		}
		return out[i].ID < out[j].ID
	})
	return out, nil
}

func (m *Memstore) UpdateTerminalStatus(_ context.Context, sessionID, terminalID string, status store.TerminalStatus, exitCode *int) (*store.Terminal, error) {
	if status != store.TerminalRunning && status != store.TerminalExited {
		return nil, store.ErrInvalidTerminal
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	item := m.terminals[sessionID][terminalID]
	if item == nil {
		return nil, store.ErrNotFound
	}
	item.Status = status
	item.ExitCode = cloneInt(exitCode)
	item.UpdatedAt = time.Now()
	return cloneTerminal(item), nil
}

func (m *Memstore) DeleteTerminal(_ context.Context, sessionID, terminalID string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.terminals[sessionID][terminalID] == nil {
		return store.ErrNotFound
	}
	delete(m.terminals[sessionID], terminalID)
	return nil
}

func (m *Memstore) ResetRunningTerminals(_ context.Context) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	now := time.Now()
	for _, items := range m.terminals {
		for _, item := range items {
			if item.Status == store.TerminalRunning {
				item.Status = store.TerminalExited
				item.ExitCode = nil
				item.UpdatedAt = now
			}
		}
	}
	return nil
}

func cloneTerminal(item *store.Terminal) *store.Terminal {
	if item == nil {
		return nil
	}
	cp := *item
	cp.ExitCode = cloneInt(item.ExitCode)
	return &cp
}

func cloneInt(value *int) *int {
	if value == nil {
		return nil
	}
	cp := *value
	return &cp
}

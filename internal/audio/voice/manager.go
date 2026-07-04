// Package voice coordinates session-scoped voice ownership.
package voice

import (
	"context"
	"errors"
	"strings"
	"sync"
)

var ErrSessionRequired = errors.New("voice: session id required")

type Bindings struct {
	InputOwner  string `json:"inputOwner"`
	OutputOwner string `json:"outputOwner"`
}

type Manager struct {
	mu       sync.Mutex
	bindings Bindings
}

func NewManager() *Manager {
	return &Manager{}
}

func (m *Manager) Snapshot() Bindings {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.bindings
}

func (m *Manager) BindInput(sessionID string, enabled bool) (Bindings, error) {
	return m.bind(sessionID, enabled, func(bindings *Bindings, id string) {
		bindings.InputOwner = id
	}, func(bindings *Bindings, id string) {
		if bindings.InputOwner == id {
			bindings.InputOwner = ""
		}
	})
}

func (m *Manager) BindOutput(sessionID string, enabled bool) (Bindings, error) {
	return m.bind(sessionID, enabled, func(bindings *Bindings, id string) {
		bindings.OutputOwner = id
	}, func(bindings *Bindings, id string) {
		if bindings.OutputOwner == id {
			bindings.OutputOwner = ""
		}
	})
}

func (m *Manager) ReleaseSession(sessionID string) Bindings {
	id := strings.TrimSpace(sessionID)
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.bindings.InputOwner == id {
		m.bindings.InputOwner = ""
	}
	if m.bindings.OutputOwner == id {
		m.bindings.OutputOwner = ""
	}
	return m.bindings
}

func (m *Manager) CancelSession(context.Context, string) bool {
	return false
}

func (m *Manager) bind(sessionID string, enabled bool, set func(*Bindings, string), clear func(*Bindings, string)) (Bindings, error) {
	id := strings.TrimSpace(sessionID)
	if id == "" {
		return Bindings{}, ErrSessionRequired
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if enabled {
		set(&m.bindings, id)
	} else {
		clear(&m.bindings, id)
	}
	return m.bindings, nil
}

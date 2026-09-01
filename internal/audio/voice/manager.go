// Package voice coordinates session-scoped voice ownership.
package voice

import (
	"errors"
	"strings"
	"sync"
)

var (
	ErrSessionRequired  = errors.New("voice: session id required")
	ErrInvalidInputMode = errors.New("voice: invalid input mode")
)

type InputMode string

const (
	InputModeTranscribe InputMode = "transcribe"
	InputModeRaw        InputMode = "raw"
)

func NormalizeInputMode(mode InputMode) (InputMode, error) {
	switch InputMode(strings.ToLower(strings.TrimSpace(string(mode)))) {
	case "", InputModeTranscribe:
		return InputModeTranscribe, nil
	case InputModeRaw:
		return InputModeRaw, nil
	default:
		return "", ErrInvalidInputMode
	}
}

type Bindings struct {
	InputOwner string    `json:"inputOwner"`
	InputMode  InputMode `json:"inputMode"`
	InputLevel float64   `json:"inputLevel"`
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

func (m *Manager) BindInput(sessionID string, enabled bool, modes ...InputMode) (Bindings, error) {
	id := strings.TrimSpace(sessionID)
	if id == "" {
		return Bindings{}, ErrSessionRequired
	}
	mode := InputModeTranscribe
	if len(modes) > 0 {
		var err error
		mode, err = NormalizeInputMode(modes[0])
		if err != nil {
			return Bindings{}, err
		}
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if enabled {
		m.bindings.InputOwner = id
		m.bindings.InputMode = mode
	} else if m.bindings.InputOwner == id {
		m.bindings.InputOwner = ""
		m.bindings.InputMode = ""
	}
	return m.bindings, nil
}

func (m *Manager) ReleaseSession(sessionID string) Bindings {
	id := strings.TrimSpace(sessionID)
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.bindings.InputOwner == id {
		m.bindings.InputOwner = ""
		m.bindings.InputMode = ""
	}
	return m.bindings
}

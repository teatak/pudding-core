package lsp

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"time"
)

const (
	defaultInitializeTimeout = 15 * time.Second
	defaultShutdownTimeout   = 2 * time.Second
	defaultIdleTimeout       = 10 * time.Minute
	defaultReapInterval      = time.Minute
	defaultMaxProcesses      = 6
)

// Service is the protocol-level boundary injected into the tool layer.
type Service interface {
	Request(ctx context.Context, spec ServerSpec, method string, params, result any) error
}

type ManagerOption func(*Manager)

func WithInitializeTimeout(timeout time.Duration) ManagerOption {
	return func(m *Manager) { m.opts.initializeTimeout = timeout }
}

func WithShutdownTimeout(timeout time.Duration) ManagerOption {
	return func(m *Manager) { m.opts.shutdownTimeout = timeout }
}

func WithIdleTimeout(timeout time.Duration) ManagerOption {
	return func(m *Manager) { m.idleTimeout = timeout }
}

func WithReapInterval(interval time.Duration) ManagerOption {
	return func(m *Manager) { m.reapInterval = interval }
}

func WithMaxProcesses(maxProcesses int) ManagerOption {
	return func(m *Manager) { m.maxProcesses = maxProcesses }
}

func WithMaxMessageBytes(maxBytes int) ManagerOption {
	return func(m *Manager) { m.opts.maxMessageBytes = maxBytes }
}

type managerEntry struct {
	spec     ServerSpec
	process  *Process
	ready    chan struct{}
	err      error
	lastUsed time.Time
}

// Manager owns and shares LSP processes by canonical language root and server kind.
type Manager struct {
	mu           sync.Mutex
	entries      map[ProcessKey]*managerEntry
	closed       bool
	stop         chan struct{}
	stopOnce     sync.Once
	idleTimeout  time.Duration
	reapInterval time.Duration
	maxProcesses int
	opts         processOptions
}

func NewManager(options ...ManagerOption) *Manager {
	m := &Manager{
		entries:      map[ProcessKey]*managerEntry{},
		stop:         make(chan struct{}),
		idleTimeout:  defaultIdleTimeout,
		reapInterval: defaultReapInterval,
		maxProcesses: defaultMaxProcesses,
		opts: processOptions{
			initializeTimeout: defaultInitializeTimeout,
			shutdownTimeout:   defaultShutdownTimeout,
			maxMessageBytes:   DefaultMaxMessageBytes,
			maxHeaderBytes:    DefaultMaxHeaderBytes,
			stderrBytes:       defaultStderrBytes,
		},
	}
	for _, option := range options {
		option(m)
	}
	if m.maxProcesses <= 0 {
		m.maxProcesses = defaultMaxProcesses
	}
	if m.reapInterval > 0 && m.idleTimeout > 0 {
		go m.reapLoop()
	}
	return m
}

// Request acquires the shared process and forwards one JSON-RPC request.
func (m *Manager) Request(ctx context.Context, spec ServerSpec, method string, params, result any) error {
	process, err := m.Acquire(ctx, spec)
	if err != nil {
		return err
	}
	m.touch(process.spec.Key, process)
	err = process.Request(ctx, method, params, result)
	m.touch(process.spec.Key, process)
	return err
}

// Acquire returns the initialized process for spec, starting it once per key.
func (m *Manager) Acquire(ctx context.Context, spec ServerSpec) (*Process, error) {
	normalized, err := normalizeSpec(spec)
	if err != nil {
		return nil, err
	}
	key := normalized.Key
	for {
		m.mu.Lock()
		if m.closed {
			m.mu.Unlock()
			return nil, ErrClosed
		}
		if entry := m.entries[key]; entry != nil {
			if !sameSpec(entry.spec, normalized) {
				m.mu.Unlock()
				return nil, ErrSpecConflict
			}
			if entry.ready != nil {
				ready := entry.ready
				m.mu.Unlock()
				select {
				case <-ready:
					if entry.err != nil {
						return nil, entry.err
					}
					if entry.process != nil && entry.process.Alive() {
						m.touch(key, entry.process)
						return entry.process, nil
					}
					continue
				case <-ctx.Done():
					return nil, ctx.Err()
				}
			}
			if entry.process != nil && entry.process.Alive() {
				entry.lastUsed = time.Now()
				process := entry.process
				m.mu.Unlock()
				return process, nil
			}
			delete(m.entries, key)
		}

		victim, slotAvailable := m.removeLRUVictimLocked()
		if !slotAvailable {
			m.mu.Unlock()
			return nil, ErrCapacity
		}
		entry := &managerEntry{spec: normalized, ready: make(chan struct{}), lastUsed: time.Now()}
		ready := entry.ready
		m.entries[key] = entry
		m.mu.Unlock()

		if victim != nil {
			m.closeWithManagerTimeout(victim)
		}
		process, startErr := startProcess(ctx, normalized, m.opts)

		m.mu.Lock()
		if current := m.entries[key]; current == entry {
			if startErr != nil || m.closed {
				delete(m.entries, key)
			}
		}
		entry.process = process
		entry.err = startErr
		if m.closed && entry.err == nil {
			entry.err = ErrClosed
		}
		entry.ready = nil
		m.mu.Unlock()
		close(ready)

		if startErr != nil {
			return nil, startErr
		}
		if m.isClosed() {
			m.closeWithManagerTimeout(process)
			return nil, ErrClosed
		}
		go m.watchProcess(key, entry, process)
		return process, nil
	}
}

func (m *Manager) removeLRUVictimLocked() (*Process, bool) {
	if len(m.entries) < m.maxProcesses {
		return nil, true
	}
	var victimKey ProcessKey
	var victim *managerEntry
	for key, entry := range m.entries {
		if entry.ready != nil || entry.process == nil || entry.process.Pending() != 0 {
			continue
		}
		if victim == nil || entry.lastUsed.Before(victim.lastUsed) {
			victimKey = key
			victim = entry
		}
	}
	if victim == nil {
		return nil, false
	}
	delete(m.entries, victimKey)
	return victim.process, true
}

func (m *Manager) touch(key ProcessKey, process *Process) {
	m.mu.Lock()
	if entry := m.entries[key]; entry != nil && entry.process == process {
		entry.lastUsed = time.Now()
	}
	m.mu.Unlock()
}

func (m *Manager) watchProcess(key ProcessKey, entry *managerEntry, process *Process) {
	<-process.Done()
	m.mu.Lock()
	if current := m.entries[key]; current == entry && current.process == process {
		delete(m.entries, key)
	}
	m.mu.Unlock()
}

func (m *Manager) reapLoop() {
	ticker := time.NewTicker(m.reapInterval)
	defer ticker.Stop()
	for {
		select {
		case <-ticker.C:
			_ = m.ReapIdle(context.Background())
		case <-m.stop:
			return
		}
	}
}

// ReapIdle closes processes that have no pending requests and exceeded idle timeout.
func (m *Manager) ReapIdle(ctx context.Context) error {
	if m.idleTimeout <= 0 {
		return nil
	}
	cutoff := time.Now().Add(-m.idleTimeout)
	m.mu.Lock()
	var processes []*Process
	for key, entry := range m.entries {
		if entry.ready != nil || entry.process == nil || entry.process.Pending() != 0 || entry.lastUsed.After(cutoff) {
			continue
		}
		delete(m.entries, key)
		processes = append(processes, entry.process)
	}
	m.mu.Unlock()
	return closeProcesses(ctx, processes)
}

// Close stops new acquisitions and shuts down all running language servers.
func (m *Manager) Close(ctx context.Context) error {
	m.mu.Lock()
	if m.closed {
		m.mu.Unlock()
		return nil
	}
	m.closed = true
	var processes []*Process
	for _, entry := range m.entries {
		if entry.process != nil {
			processes = append(processes, entry.process)
		}
	}
	m.entries = map[ProcessKey]*managerEntry{}
	m.mu.Unlock()
	m.stopOnce.Do(func() { close(m.stop) })
	return closeProcesses(ctx, processes)
}

func (m *Manager) ProcessCount() int {
	m.mu.Lock()
	defer m.mu.Unlock()
	count := 0
	for _, entry := range m.entries {
		if entry.process != nil && entry.process.Alive() {
			count++
		}
	}
	return count
}

func (m *Manager) closeWithManagerTimeout(process *Process) {
	ctx, cancel := context.WithTimeout(context.Background(), m.opts.shutdownTimeout)
	defer cancel()
	_ = process.Close(ctx)
}

func (m *Manager) isClosed() bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.closed
}

func closeProcesses(ctx context.Context, processes []*Process) error {
	var firstErr error
	for _, process := range processes {
		if err := process.Close(ctx); err != nil && !errors.Is(err, context.Canceled) && firstErr == nil {
			firstErr = err
		}
	}
	return firstErr
}

func normalizeSpec(spec ServerSpec) (ServerSpec, error) {
	root := strings.TrimSpace(spec.Key.LanguageRoot)
	serverKind := strings.TrimSpace(spec.Key.ServerKind)
	command := strings.TrimSpace(spec.Command)
	if root == "" || serverKind == "" || command == "" {
		return ServerSpec{}, errors.New("language root, server kind, and command are required")
	}
	absRoot, err := filepath.Abs(root)
	if err != nil {
		return ServerSpec{}, fmt.Errorf("resolve language root: %w", err)
	}
	absRoot = filepath.Clean(absRoot)
	if evaluated, evalErr := filepath.EvalSymlinks(absRoot); evalErr == nil {
		absRoot = filepath.Clean(evaluated)
	}
	dir := strings.TrimSpace(spec.Dir)
	if dir == "" {
		dir = absRoot
	} else {
		absDir, absErr := filepath.Abs(dir)
		if absErr != nil {
			return ServerSpec{}, fmt.Errorf("resolve language server cwd: %w", absErr)
		}
		dir = filepath.Clean(absDir)
		if evaluated, evalErr := filepath.EvalSymlinks(dir); evalErr == nil {
			dir = filepath.Clean(evaluated)
		}
		if dir != absRoot {
			return ServerSpec{}, errors.New("language server cwd must equal language root")
		}
	}
	spec.Key = ProcessKey{LanguageRoot: absRoot, ServerKind: serverKind}
	spec.Command = command
	spec.Dir = dir
	spec.Args = append([]string(nil), spec.Args...)
	spec.Env = append([]string(nil), spec.Env...)
	return spec, nil
}

func sameSpec(left, right ServerSpec) bool {
	return left.Key == right.Key && left.Command == right.Command && left.Dir == right.Dir &&
		slices.Equal(left.Args, right.Args) && slices.Equal(left.Env, right.Env)
}

// Package terminal manages session-scoped interactive PTY processes.
package terminal

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/coder/websocket"
	"github.com/creack/pty"
	"github.com/teatak/pudding-core/internal/store"
)

const (
	defaultColumns      = 100
	defaultRows         = 30
	maxTerminalSize     = 1000
	maxScrollbackBytes  = 1 << 20
	maxClientInputBytes = 64 << 10
)

var (
	ErrUnavailable = errors.New("terminal: unavailable")
	ErrNotRunning  = errors.New("terminal: not running")
	ErrInvalidCWD  = errors.New("terminal: invalid cwd")
)

type CreateOptions struct {
	CWD     string `json:"cwd,omitempty"`
	Columns int    `json:"columns,omitempty"`
	Rows    int    `json:"rows,omitempty"`
}

type Service interface {
	Create(ctx context.Context, sessionID string, options CreateOptions) (*store.Terminal, error)
	Get(ctx context.Context, sessionID, terminalID string) (*store.Terminal, error)
	List(ctx context.Context, sessionID string) ([]*store.Terminal, error)
	Delete(ctx context.Context, sessionID, terminalID string) error
	ServeWebSocket(w http.ResponseWriter, request *http.Request, sessionID, terminalID string)
	CloseSession(sessionID string)
	Close() error
}

type Manager struct {
	store     store.Store
	mu        sync.Mutex
	processes map[string]*process
	terminals map[string]*store.Terminal
	closed    bool
	shellPath string
	shellArgs []string
}

type process struct {
	manager   *Manager
	sessionID string
	id        string
	cmd       *exec.Cmd
	ptmx      *os.File

	mu          sync.Mutex
	writeMu     sync.Mutex
	scrollback  []byte
	subscribers map[chan []byte]struct{}
	done        chan struct{}
	finishOnce  sync.Once
}

type clientMessage struct {
	Type    string `json:"type"`
	Data    string `json:"data,omitempty"`
	Columns int    `json:"columns,omitempty"`
	Rows    int    `json:"rows,omitempty"`
}

type statusMessage struct {
	Type     string               `json:"type"`
	Status   store.TerminalStatus `json:"status"`
	ExitCode *int                 `json:"exitCode,omitempty"`
}

func NewManager(metadata store.Store) (*Manager, error) {
	if metadata == nil {
		return nil, errors.New("terminal: metadata store is required")
	}
	return &Manager{
		store:     metadata,
		processes: make(map[string]*process),
		terminals: make(map[string]*store.Terminal),
	}, nil
}

func (m *Manager) Create(ctx context.Context, sessionID string, options CreateOptions) (*store.Terminal, error) {
	if runtime.GOOS == "windows" {
		return nil, ErrUnavailable
	}
	session, err := m.store.GetSession(ctx, strings.TrimSpace(sessionID))
	if err != nil {
		return nil, err
	}
	cwd, err := m.resolveCWD(ctx, session, options.CWD)
	if err != nil {
		return nil, err
	}
	shell, shellArgs, err := m.shellCommand()
	if err != nil {
		return nil, err
	}
	columns, rows := normalizeSize(options.Columns, options.Rows)
	cmd := exec.Command(shell, shellArgs...)
	cmd.Dir = cwd
	cmd.Env = terminalEnvironment(session.ID)
	ptmx, err := pty.StartWithSize(cmd, &pty.Winsize{Cols: uint16(columns), Rows: uint16(rows)})
	if err != nil {
		return nil, fmt.Errorf("terminal: start shell: %w", err)
	}

	item := &store.Terminal{
		ID:        store.NewID("term"),
		SessionID: session.ID,
		Title:     m.terminalTitle(ctx, session, shell),
		CWD:       cwd,
		Shell:     shell,
		Status:    store.TerminalRunning,
	}
	if err := store.NormalizeTerminal(item); err != nil {
		_ = ptmx.Close()
		_ = terminateTerminalProcess(cmd)
		_ = cmd.Wait()
		return nil, err
	}
	now := time.Now()
	item.CreatedAt = now
	item.UpdatedAt = now

	proc := &process{
		manager:     m,
		sessionID:   session.ID,
		id:          item.ID,
		cmd:         cmd,
		ptmx:        ptmx,
		subscribers: make(map[chan []byte]struct{}),
		done:        make(chan struct{}),
	}
	key := terminalKey(item.SessionID, item.ID)
	m.mu.Lock()
	if m.closed {
		m.mu.Unlock()
		_ = ptmx.Close()
		_ = terminateTerminalProcess(cmd)
		_ = cmd.Wait()
		return nil, ErrUnavailable
	}
	m.processes[key] = proc
	m.terminals[key] = cloneTerminal(item)
	m.mu.Unlock()

	go proc.readOutput()
	go proc.wait()
	return item, nil
}

func (m *Manager) Get(_ context.Context, sessionID, terminalID string) (*store.Terminal, error) {
	return m.getTerminal(strings.TrimSpace(sessionID), strings.TrimSpace(terminalID))
}

func (m *Manager) List(ctx context.Context, sessionID string) ([]*store.Terminal, error) {
	sessionID = strings.TrimSpace(sessionID)
	if _, err := m.store.GetSession(ctx, sessionID); err != nil {
		return nil, err
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	items := make([]*store.Terminal, 0)
	for _, item := range m.terminals {
		if item.SessionID == sessionID {
			items = append(items, cloneTerminal(item))
		}
	}
	sort.Slice(items, func(i, j int) bool {
		if !items[i].CreatedAt.Equal(items[j].CreatedAt) {
			return items[i].CreatedAt.Before(items[j].CreatedAt)
		}
		return items[i].ID < items[j].ID
	})
	return items, nil
}

func (m *Manager) Delete(_ context.Context, sessionID, terminalID string) error {
	sessionID = strings.TrimSpace(sessionID)
	terminalID = strings.TrimSpace(terminalID)
	key := terminalKey(sessionID, terminalID)
	m.mu.Lock()
	if m.terminals[key] == nil {
		m.mu.Unlock()
		return store.ErrNotFound
	}
	delete(m.terminals, key)
	proc := m.processes[key]
	m.mu.Unlock()
	if proc != nil {
		proc.terminate()
	}
	return nil
}

func (m *Manager) ServeWebSocket(w http.ResponseWriter, request *http.Request, sessionID, terminalID string) {
	item, err := m.getTerminal(sessionID, terminalID)
	if err != nil {
		http.Error(w, http.StatusText(http.StatusNotFound), http.StatusNotFound)
		return
	}
	proc := m.process(sessionID, terminalID)
	if proc == nil || item.Status != store.TerminalRunning {
		http.Error(w, http.StatusText(http.StatusConflict), http.StatusConflict)
		return
	}
	conn, err := websocket.Accept(w, request, &websocket.AcceptOptions{InsecureSkipVerify: true})
	if err != nil {
		slog.Warn("terminal: websocket accept failed", "sessionID", sessionID, "terminalID", terminalID, "err", err)
		return
	}
	defer conn.Close(websocket.StatusNormalClosure, "")
	conn.SetReadLimit(maxClientInputBytes)
	proc.serve(request.Context(), conn, item)
}

func (m *Manager) CloseSession(sessionID string) {
	m.mu.Lock()
	processes := make([]*process, 0)
	for key, proc := range m.processes {
		if proc.sessionID == sessionID {
			processes = append(processes, proc)
			delete(m.terminals, key)
		}
	}
	for key, item := range m.terminals {
		if item.SessionID == sessionID {
			delete(m.terminals, key)
		}
	}
	m.mu.Unlock()
	for _, proc := range processes {
		proc.terminate()
	}
	waitForProcesses(processes, time.Second)
}

func (m *Manager) Close() error {
	m.mu.Lock()
	if m.closed {
		m.mu.Unlock()
		return nil
	}
	m.closed = true
	processes := make([]*process, 0, len(m.processes))
	for _, proc := range m.processes {
		processes = append(processes, proc)
	}
	m.terminals = make(map[string]*store.Terminal)
	m.mu.Unlock()
	for _, proc := range processes {
		proc.terminate()
	}
	waitForProcesses(processes, 2*time.Second)
	return nil
}

func (m *Manager) process(sessionID, terminalID string) *process {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.processes[terminalKey(sessionID, terminalID)]
}

func (m *Manager) getTerminal(sessionID, terminalID string) (*store.Terminal, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	item := m.terminals[terminalKey(sessionID, terminalID)]
	if item == nil {
		return nil, store.ErrNotFound
	}
	return cloneTerminal(item), nil
}

func (m *Manager) updateTerminalStatus(sessionID, terminalID string, status store.TerminalStatus, exitCode *int) *store.Terminal {
	m.mu.Lock()
	defer m.mu.Unlock()
	item := m.terminals[terminalKey(sessionID, terminalID)]
	if item == nil {
		return nil
	}
	item.Status = status
	item.ExitCode = cloneInt(exitCode)
	item.UpdatedAt = time.Now()
	return cloneTerminal(item)
}

func (m *Manager) removeProcess(proc *process) {
	m.mu.Lock()
	key := terminalKey(proc.sessionID, proc.id)
	if m.processes[key] == proc {
		delete(m.processes, key)
	}
	m.mu.Unlock()
}

func (m *Manager) resolveCWD(ctx context.Context, session *store.Session, requested string) (string, error) {
	requested = strings.TrimSpace(requested)
	var projectRoots []string
	if session.ProjectID != "" {
		project, err := m.store.GetProject(ctx, session.ProjectID)
		if err != nil {
			return "", err
		}
		projectRoots = project.RootDirs
	}
	if requested == "" {
		if len(projectRoots) > 0 {
			requested = projectRoots[0]
		} else if home, err := os.UserHomeDir(); err == nil {
			requested = home
		}
	}
	if requested == "" {
		requested = string(filepath.Separator)
	}
	abs, err := filepath.Abs(requested)
	if err != nil {
		return "", ErrInvalidCWD
	}
	abs = filepath.Clean(abs)
	if len(projectRoots) > 0 && !withinAnyRoot(abs, projectRoots) {
		return "", ErrInvalidCWD
	}
	info, err := os.Stat(abs)
	if err != nil || !info.IsDir() {
		return "", ErrInvalidCWD
	}
	return abs, nil
}

func (m *Manager) terminalTitle(ctx context.Context, session *store.Session, shell string) string {
	if session.ProjectID != "" {
		project, err := m.store.GetProject(ctx, session.ProjectID)
		if err == nil && strings.TrimSpace(project.Name) != "" {
			return strings.TrimSpace(project.Name)
		}
	}
	return filepath.Base(shell)
}

func (m *Manager) shellCommand() (string, []string, error) {
	if m.shellPath != "" {
		return m.shellPath, append([]string(nil), m.shellArgs...), nil
	}
	shell, err := userShell()
	if err != nil {
		return "", nil, err
	}
	return shell, []string{"-l"}, nil
}

func (p *process) readOutput() {
	buffer := make([]byte, 32<<10)
	for {
		n, err := p.ptmx.Read(buffer)
		if n > 0 {
			p.broadcast(buffer[:n])
		}
		if err != nil {
			return
		}
	}
}

func (p *process) wait() {
	err := p.cmd.Wait()
	_ = p.ptmx.Close()
	exitCode := 0
	if p.cmd.ProcessState != nil {
		exitCode = p.cmd.ProcessState.ExitCode()
	} else if err != nil {
		exitCode = -1
	}
	p.finishOnce.Do(func() {
		p.manager.removeProcess(p)
		p.manager.updateTerminalStatus(p.sessionID, p.id, store.TerminalExited, &exitCode)
		close(p.done)
	})
}

func (p *process) terminate() {
	_ = terminateTerminalProcess(p.cmd)
	_ = p.ptmx.Close()
}

func (p *process) broadcast(data []byte) {
	chunk := append([]byte(nil), data...)
	p.mu.Lock()
	p.scrollback = append(p.scrollback, chunk...)
	if len(p.scrollback) > maxScrollbackBytes {
		p.scrollback = append(p.scrollback[:0], p.scrollback[len(p.scrollback)-maxScrollbackBytes:]...)
	}
	for subscriber := range p.subscribers {
		select {
		case subscriber <- chunk:
		default:
			delete(p.subscribers, subscriber)
			close(subscriber)
		}
	}
	p.mu.Unlock()
}

func (p *process) subscribe() (<-chan []byte, []byte, func()) {
	channel := make(chan []byte, 64)
	p.mu.Lock()
	p.subscribers[channel] = struct{}{}
	replay := append([]byte(nil), p.scrollback...)
	p.mu.Unlock()
	return channel, replay, func() {
		p.mu.Lock()
		if _, ok := p.subscribers[channel]; ok {
			delete(p.subscribers, channel)
			close(channel)
		}
		p.mu.Unlock()
	}
}

func (p *process) serve(parent context.Context, conn *websocket.Conn, item *store.Terminal) {
	output, replay, unsubscribe := p.subscribe()
	defer unsubscribe()
	ctx, cancel := context.WithCancel(parent)
	defer cancel()

	writerDone := make(chan struct{})
	go func() {
		defer close(writerDone)
		defer cancel()
		if !writeStatus(ctx, conn, item.Status, item.ExitCode) {
			return
		}
		if len(replay) > 0 {
			if err := conn.Write(ctx, websocket.MessageBinary, replay); err != nil {
				return
			}
		}
		for {
			select {
			case chunk, ok := <-output:
				if !ok || conn.Write(ctx, websocket.MessageBinary, chunk) != nil {
					return
				}
			case <-p.done:
				latest, _ := p.manager.getTerminal(p.sessionID, p.id)
				if latest != nil {
					writeStatus(ctx, conn, latest.Status, latest.ExitCode)
				}
				return
			case <-ctx.Done():
				return
			}
		}
	}()

	for {
		typ, payload, err := conn.Read(ctx)
		if err != nil {
			break
		}
		if typ != websocket.MessageText {
			continue
		}
		var message clientMessage
		if json.Unmarshal(payload, &message) != nil {
			continue
		}
		switch message.Type {
		case "input":
			p.writeMu.Lock()
			_, _ = p.ptmx.Write([]byte(message.Data))
			p.writeMu.Unlock()
		case "resize":
			columns, rows := normalizeSize(message.Columns, message.Rows)
			p.writeMu.Lock()
			_ = pty.Setsize(p.ptmx, &pty.Winsize{Cols: uint16(columns), Rows: uint16(rows)})
			p.writeMu.Unlock()
		}
	}
	cancel()
	<-writerDone
}

func writeStatus(ctx context.Context, conn *websocket.Conn, status store.TerminalStatus, exitCode *int) bool {
	payload, err := json.Marshal(statusMessage{Type: "status", Status: status, ExitCode: exitCode})
	return err == nil && conn.Write(ctx, websocket.MessageText, payload) == nil
}

func cloneTerminal(item *store.Terminal) *store.Terminal {
	if item == nil {
		return nil
	}
	cloned := *item
	cloned.ExitCode = cloneInt(item.ExitCode)
	return &cloned
}

func cloneInt(value *int) *int {
	if value == nil {
		return nil
	}
	cloned := *value
	return &cloned
}

func terminalEnvironment(sessionID string) []string {
	env := append([]string(nil), os.Environ()...)
	env = replaceEnv(env, "TERM", "xterm-256color")
	env = replaceEnv(env, "COLORTERM", "truecolor")
	env = replaceEnv(env, "PROMPT_EOL_MARK", "")
	env = replaceEnv(env, "PUDDING_SESSION_ID", sessionID)
	return env
}

func replaceEnv(env []string, key, value string) []string {
	prefix := key + "="
	out := make([]string, 0, len(env)+1)
	for _, entry := range env {
		if !strings.HasPrefix(entry, prefix) {
			out = append(out, entry)
		}
	}
	return append(out, prefix+value)
}

func userShell() (string, error) {
	candidates := []string{strings.TrimSpace(os.Getenv("SHELL")), "/bin/zsh", "/bin/bash", "/bin/sh"}
	for _, candidate := range candidates {
		if candidate == "" || !filepath.IsAbs(candidate) {
			continue
		}
		if info, err := os.Stat(candidate); err == nil && !info.IsDir() && info.Mode()&0o111 != 0 {
			return candidate, nil
		}
	}
	return "", ErrUnavailable
}

func normalizeSize(columns, rows int) (int, int) {
	if columns <= 0 || columns > maxTerminalSize {
		columns = defaultColumns
	}
	if rows <= 0 || rows > maxTerminalSize {
		rows = defaultRows
	}
	return columns, rows
}

func withinAnyRoot(path string, roots []string) bool {
	for _, root := range roots {
		rootAbs, err := filepath.Abs(root)
		if err != nil {
			continue
		}
		rel, err := filepath.Rel(filepath.Clean(rootAbs), path)
		if err == nil && rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
			return true
		}
	}
	return false
}

func terminalKey(sessionID, terminalID string) string {
	return sessionID + "\x00" + terminalID
}

func waitForProcesses(processes []*process, timeout time.Duration) {
	deadline := time.NewTimer(timeout)
	defer deadline.Stop()
	for _, proc := range processes {
		select {
		case <-proc.done:
		case <-deadline.C:
			return
		}
	}
}

var _ Service = (*Manager)(nil)

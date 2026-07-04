// Package browser owns the daemon-managed browser process and session-scoped tab
// bindings.
package browser

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"image/png"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/coder/websocket"
)

const (
	defaultObserveTextChars = 6000
	maxObserveTextChars     = 20000
	defaultObserveElements  = 30
	maxObserveElements      = 100
)

var (
	ErrUnavailable = errors.New("browser unavailable")
	ErrTabNotFound = errors.New("browser tab not found")
	ErrTabRequired = errors.New("browser tab id required")
)

type Service interface {
	CreateTab(ctx context.Context, sessionID string) (TabSnapshot, error)
	ListTabs(ctx context.Context, sessionID string) ([]TabSnapshot, error)
	GetTab(ctx context.Context, sessionID, tabID string) (TabSnapshot, error)
	ReleaseTab(ctx context.Context, sessionID, tabID string) error
	ReleaseSession(ctx context.Context, sessionID string) error
	Open(ctx context.Context, sessionID, tabID, rawURL string) (TabSnapshot, error)
	Observe(ctx context.Context, sessionID, tabID string, opts ObserveOptions) (ObserveResult, error)
	Screenshot(ctx context.Context, sessionID, tabID string, opts ScreenshotOptions) (ScreenshotResult, error)
	Click(ctx context.Context, sessionID, tabID string, in ClickInput) (ActionResult, error)
	Type(ctx context.Context, sessionID, tabID string, in TypeInput) (ActionResult, error)
	Scroll(ctx context.Context, sessionID, tabID string, in ScrollInput) (ActionResult, error)
	Close() error
}

type Config struct {
	HomeDir    string
	ChromePath string
	Headless   bool
}

type Manager struct {
	mu       sync.Mutex
	cfg      Config
	client   *http.Client
	process  *browserProcess
	tabs     map[string]*tabBinding
	sessions map[string]map[string]bool
}

type TabSnapshot struct {
	ID        string    `json:"id"`
	SessionID string    `json:"sessionID"`
	TargetID  string    `json:"targetID,omitempty"`
	URL       string    `json:"url"`
	Title     string    `json:"title"`
	CreatedAt time.Time `json:"createdAt"`
	UpdatedAt time.Time `json:"updatedAt"`
}

type ObserveOptions struct {
	MaxTextChars int
	MaxElements  int
}

type ObserveResult struct {
	Tab        TabSnapshot       `json:"tab"`
	Title      string            `json:"title"`
	URL        string            `json:"url"`
	ReadyState string            `json:"readyState"`
	Text       string            `json:"text"`
	TextChars  int               `json:"textChars"`
	Truncated  bool              `json:"truncated"`
	Elements   []ObservedElement `json:"elements"`
}

type ObservedElement struct {
	Index     int    `json:"index"`
	Tag       string `json:"tag"`
	Text      string `json:"text,omitempty"`
	Href      string `json:"href,omitempty"`
	Role      string `json:"role,omitempty"`
	AriaLabel string `json:"ariaLabel,omitempty"`
	Selector  string `json:"selector,omitempty"`
	InputType string `json:"inputType,omitempty"`
	Disabled  bool   `json:"disabled,omitempty"`
}

type ScreenshotOptions struct {
	FullPage bool
}

type ScreenshotResult struct {
	Tab               TabSnapshot `json:"tab"`
	MIME              string      `json:"mime"`
	DataBase64        string      `json:"dataBase64"`
	Size              int64       `json:"size"`
	Width             int         `json:"width,omitempty"`
	Height            int         `json:"height,omitempty"`
	ViewportWidth     int         `json:"viewportWidth,omitempty"`
	ViewportHeight    int         `json:"viewportHeight,omitempty"`
	DeviceScaleFactor float64     `json:"deviceScaleFactor,omitempty"`
	CapturedAt        time.Time   `json:"capturedAt"`
}

type ClickInput struct {
	TabID    string   `json:"tabID,omitempty"`
	Selector string   `json:"selector,omitempty"`
	X        *float64 `json:"x,omitempty"`
	Y        *float64 `json:"y,omitempty"`
}

type TypeInput struct {
	TabID    string `json:"tabID,omitempty"`
	Selector string `json:"selector,omitempty"`
	Text     string `json:"text"`
	Clear    bool   `json:"clear,omitempty"`
}

type ScrollInput struct {
	TabID    string  `json:"tabID,omitempty"`
	Selector string  `json:"selector,omitempty"`
	DeltaX   float64 `json:"deltaX,omitempty"`
	DeltaY   float64 `json:"deltaY,omitempty"`
}

type ActionResult struct {
	Tab    TabSnapshot    `json:"tab"`
	Action string         `json:"action"`
	Result map[string]any `json:"result"`
}

type tabBinding struct {
	id        string
	sessionID string
	targetID  string
	createdAt time.Time
	updatedAt time.Time
}

type browserProcess struct {
	cmd        *exec.Cmd
	endpoint   string
	chromePath string
	profileDir string
	port       int
}

type targetInfo struct {
	ID                   string `json:"id"`
	Type                 string `json:"type"`
	URL                  string `json:"url"`
	Title                string `json:"title"`
	WebSocketDebuggerURL string `json:"webSocketDebuggerUrl"`
}

func NewManager(cfg Config) *Manager {
	return &Manager{
		cfg:      cfg,
		client:   &http.Client{Timeout: 10 * time.Second},
		tabs:     map[string]*tabBinding{},
		sessions: map[string]map[string]bool{},
	}
}

func (m *Manager) CreateTab(ctx context.Context, sessionID string) (TabSnapshot, error) {
	sessionID = strings.TrimSpace(sessionID)
	if sessionID == "" {
		return TabSnapshot{}, errors.New("session id is required")
	}
	proc, err := m.ensureProcess(ctx)
	if err != nil {
		return TabSnapshot{}, err
	}
	target, err := proc.newTarget(ctx, m.client, "about:blank")
	if err != nil {
		return TabSnapshot{}, err
	}
	now := time.Now().UTC()
	binding := &tabBinding{
		id:        newID("tab"),
		sessionID: sessionID,
		targetID:  target.ID,
		createdAt: now,
		updatedAt: now,
	}
	m.mu.Lock()
	m.tabs[binding.id] = binding
	if m.sessions[sessionID] == nil {
		m.sessions[sessionID] = map[string]bool{}
	}
	m.sessions[sessionID][binding.id] = true
	m.mu.Unlock()
	return snapshotFromTarget(binding, target), nil
}

func (m *Manager) ListTabs(ctx context.Context, sessionID string) ([]TabSnapshot, error) {
	sessionID = strings.TrimSpace(sessionID)
	if sessionID == "" {
		return nil, errors.New("session id is required")
	}
	bindings := m.sessionBindings(sessionID)
	if len(bindings) == 0 {
		return []TabSnapshot{}, nil
	}
	proc, err := m.ensureProcess(ctx)
	if err != nil {
		return nil, err
	}
	targets, _ := proc.listTargets(ctx, m.client)
	byID := map[string]targetInfo{}
	for _, target := range targets {
		byID[target.ID] = target
	}
	out := make([]TabSnapshot, 0, len(bindings))
	for _, binding := range bindings {
		target, ok := byID[binding.targetID]
		if !ok {
			m.removeBinding(binding.id)
			continue
		}
		out = append(out, snapshotFromTarget(binding, target))
	}
	return out, nil
}

func (m *Manager) GetTab(ctx context.Context, sessionID, tabID string) (TabSnapshot, error) {
	binding, err := m.binding(sessionID, tabID)
	if err != nil {
		return TabSnapshot{}, err
	}
	proc, err := m.ensureProcess(ctx)
	if err != nil {
		return TabSnapshot{}, err
	}
	target, err := proc.target(ctx, m.client, binding.targetID)
	if err != nil {
		return TabSnapshot{}, err
	}
	return snapshotFromTarget(binding, target), nil
}

func (m *Manager) ReleaseTab(ctx context.Context, sessionID, tabID string) error {
	binding, err := m.binding(sessionID, tabID)
	if err != nil {
		return err
	}
	if proc := m.currentProcess(ctx); proc != nil {
		_ = proc.closeTarget(ctx, m.client, binding.targetID)
	}
	m.mu.Lock()
	delete(m.tabs, binding.id)
	if m.sessions[binding.sessionID] != nil {
		delete(m.sessions[binding.sessionID], binding.id)
		if len(m.sessions[binding.sessionID]) == 0 {
			delete(m.sessions, binding.sessionID)
		}
	}
	m.mu.Unlock()
	return nil
}

func (m *Manager) ReleaseSession(ctx context.Context, sessionID string) error {
	sessionID = strings.TrimSpace(sessionID)
	if sessionID == "" {
		return errors.New("session id is required")
	}
	bindings := m.releaseSessionBindings(sessionID)
	if len(bindings) == 0 {
		return nil
	}
	if proc := m.currentProcess(ctx); proc != nil {
		for _, binding := range bindings {
			_ = proc.closeTarget(ctx, m.client, binding.targetID)
		}
	}
	return nil
}

func (m *Manager) Open(ctx context.Context, sessionID, tabID, rawURL string) (TabSnapshot, error) {
	rawURL, err := normalizeURL(rawURL)
	if err != nil {
		return TabSnapshot{}, err
	}
	binding, err := m.resolveTab(ctx, sessionID, tabID, true)
	if err != nil {
		return TabSnapshot{}, err
	}
	proc, err := m.ensureProcess(ctx)
	if err != nil {
		return TabSnapshot{}, err
	}
	if _, err := proc.cdpCall(ctx, m.client, binding.targetID, "Page.navigate", map[string]any{"url": rawURL}); err != nil {
		return TabSnapshot{}, err
	}
	_ = proc.waitReady(ctx, m.client, binding.targetID)
	target, err := proc.target(ctx, m.client, binding.targetID)
	if err != nil {
		return TabSnapshot{}, err
	}
	m.touch(binding.id)
	return snapshotFromTarget(binding, target), nil
}

func (m *Manager) Observe(ctx context.Context, sessionID, tabID string, opts ObserveOptions) (ObserveResult, error) {
	binding, err := m.resolveTab(ctx, sessionID, tabID, false)
	if err != nil {
		return ObserveResult{}, err
	}
	proc, err := m.ensureProcess(ctx)
	if err != nil {
		return ObserveResult{}, err
	}
	maxText := clampInt(opts.MaxTextChars, defaultObserveTextChars, maxObserveTextChars)
	maxElements := clampInt(opts.MaxElements, defaultObserveElements, maxObserveElements)
	raw, err := proc.evaluateJSON(ctx, m.client, binding.targetID, observeScript(maxText, maxElements))
	if err != nil {
		return ObserveResult{}, err
	}
	var out ObserveResult
	if err := json.Unmarshal(raw, &out); err != nil {
		return ObserveResult{}, err
	}
	target, _ := proc.target(ctx, m.client, binding.targetID)
	out.Tab = snapshotFromTarget(binding, target)
	if out.Title == "" {
		out.Title = out.Tab.Title
	}
	if out.URL == "" {
		out.URL = out.Tab.URL
	}
	m.touch(binding.id)
	return out, nil
}

func (m *Manager) Screenshot(ctx context.Context, sessionID, tabID string, opts ScreenshotOptions) (ScreenshotResult, error) {
	binding, err := m.resolveTab(ctx, sessionID, tabID, false)
	if err != nil {
		return ScreenshotResult{}, err
	}
	proc, err := m.ensureProcess(ctx)
	if err != nil {
		return ScreenshotResult{}, err
	}
	raw, err := proc.cdpCall(ctx, m.client, binding.targetID, "Page.captureScreenshot", map[string]any{
		"format":                "png",
		"fromSurface":           true,
		"captureBeyondViewport": opts.FullPage,
	})
	if err != nil {
		return ScreenshotResult{}, err
	}
	var payload struct {
		Data string `json:"data"`
	}
	if err := json.Unmarshal(raw, &payload); err != nil {
		return ScreenshotResult{}, err
	}
	if payload.Data == "" {
		return ScreenshotResult{}, errors.New("browser screenshot returned no data")
	}
	decoded, err := base64.StdEncoding.DecodeString(payload.Data)
	if err != nil {
		return ScreenshotResult{}, err
	}
	imageConfig, _ := png.DecodeConfig(bytes.NewReader(decoded))
	var viewport struct {
		Width             int     `json:"width"`
		Height            int     `json:"height"`
		DeviceScaleFactor float64 `json:"deviceScaleFactor"`
	}
	if rawViewport, err := proc.evaluateJSON(ctx, m.client, binding.targetID, `(() => JSON.stringify({width: window.innerWidth, height: window.innerHeight, deviceScaleFactor: window.devicePixelRatio || 1}))()`); err == nil {
		_ = json.Unmarshal(rawViewport, &viewport)
	}
	target, _ := proc.target(ctx, m.client, binding.targetID)
	m.touch(binding.id)
	return ScreenshotResult{
		Tab:               snapshotFromTarget(binding, target),
		MIME:              "image/png",
		DataBase64:        payload.Data,
		Size:              int64(len(decoded)),
		Width:             imageConfig.Width,
		Height:            imageConfig.Height,
		ViewportWidth:     viewport.Width,
		ViewportHeight:    viewport.Height,
		DeviceScaleFactor: viewport.DeviceScaleFactor,
		CapturedAt:        time.Now().UTC(),
	}, nil
}

func (m *Manager) Click(ctx context.Context, sessionID, tabID string, in ClickInput) (ActionResult, error) {
	if tabID == "" {
		tabID = in.TabID
	}
	binding, err := m.resolveTab(ctx, sessionID, tabID, false)
	if err != nil {
		return ActionResult{}, err
	}
	proc, err := m.ensureProcess(ctx)
	if err != nil {
		return ActionResult{}, err
	}
	raw, err := proc.evaluateJSON(ctx, m.client, binding.targetID, clickScript(in))
	if err != nil {
		return ActionResult{}, err
	}
	return m.actionResult(ctx, proc, binding, "click", raw)
}

func (m *Manager) Type(ctx context.Context, sessionID, tabID string, in TypeInput) (ActionResult, error) {
	if tabID == "" {
		tabID = in.TabID
	}
	if in.Text == "" {
		return ActionResult{}, errors.New("text is required")
	}
	binding, err := m.resolveTab(ctx, sessionID, tabID, false)
	if err != nil {
		return ActionResult{}, err
	}
	proc, err := m.ensureProcess(ctx)
	if err != nil {
		return ActionResult{}, err
	}
	raw, err := proc.evaluateJSON(ctx, m.client, binding.targetID, typeScript(in))
	if err != nil {
		return ActionResult{}, err
	}
	return m.actionResult(ctx, proc, binding, "type", raw)
}

func (m *Manager) Scroll(ctx context.Context, sessionID, tabID string, in ScrollInput) (ActionResult, error) {
	if tabID == "" {
		tabID = in.TabID
	}
	if in.DeltaX == 0 && in.DeltaY == 0 {
		in.DeltaY = 600
	}
	binding, err := m.resolveTab(ctx, sessionID, tabID, false)
	if err != nil {
		return ActionResult{}, err
	}
	proc, err := m.ensureProcess(ctx)
	if err != nil {
		return ActionResult{}, err
	}
	raw, err := proc.evaluateJSON(ctx, m.client, binding.targetID, scrollScript(in))
	if err != nil {
		return ActionResult{}, err
	}
	return m.actionResult(ctx, proc, binding, "scroll", raw)
}

func (m *Manager) Close() error {
	m.mu.Lock()
	proc := m.process
	m.process = nil
	m.tabs = map[string]*tabBinding{}
	m.sessions = map[string]map[string]bool{}
	m.mu.Unlock()
	if proc == nil || proc.cmd == nil || proc.cmd.Process == nil {
		return nil
	}
	if err := proc.cmd.Process.Signal(os.Interrupt); err == nil {
		done := make(chan struct{})
		go func() {
			_, _ = proc.cmd.Process.Wait()
			close(done)
		}()
		select {
		case <-done:
			return nil
		case <-time.After(2 * time.Second):
		}
	}
	return proc.cmd.Process.Kill()
}

func (m *Manager) actionResult(ctx context.Context, proc *browserProcess, binding *tabBinding, action string, raw json.RawMessage) (ActionResult, error) {
	var result map[string]any
	if len(raw) > 0 {
		if err := json.Unmarshal(raw, &result); err != nil {
			return ActionResult{}, err
		}
	}
	target, _ := proc.target(ctx, m.client, binding.targetID)
	m.touch(binding.id)
	return ActionResult{Tab: snapshotFromTarget(binding, target), Action: action, Result: result}, nil
}

func (m *Manager) ensureProcess(ctx context.Context) (*browserProcess, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.process != nil {
		if err := m.process.ping(ctx, m.client); err == nil {
			return m.process, nil
		}
		m.process = nil
		m.clearBindingsLocked()
	}
	if proc, err := attachExisting(ctx, m.cfg, m.client); err == nil {
		m.process = proc
		return proc, nil
	}
	reapStaleProfileOwner(m.cfg)
	proc, err := launch(ctx, m.cfg, m.client)
	if err != nil {
		return nil, err
	}
	m.process = proc
	return proc, nil
}

func reapStaleProfileOwner(cfg Config) {
	profileDir, err := profileDir(cfg)
	if err != nil {
		return
	}
	pid, err := singletonLockPID(profileDir)
	if err != nil || pid <= 0 || pid == os.Getpid() {
		return
	}
	if proc, err := os.FindProcess(pid); err == nil {
		_ = proc.Signal(os.Interrupt)
		time.Sleep(500 * time.Millisecond)
		_ = proc.Kill()
		_ = proc.Release()
	}
	for _, name := range []string{"SingletonLock", "SingletonSocket", "SingletonCookie"} {
		_ = os.Remove(filepath.Join(profileDir, name))
	}
}

func singletonLockPID(profileDir string) (int, error) {
	target, err := os.Readlink(filepath.Join(profileDir, "SingletonLock"))
	if err != nil {
		return 0, err
	}
	idx := strings.LastIndex(target, "-")
	if idx < 0 || idx == len(target)-1 {
		return 0, errors.New("singleton lock pid missing")
	}
	return strconv.Atoi(target[idx+1:])
}

func attachExisting(ctx context.Context, cfg Config, client *http.Client) (*browserProcess, error) {
	profileDir, err := profileDir(cfg)
	if err != nil {
		return nil, err
	}
	data, err := os.ReadFile(filepath.Join(profileDir, "DevToolsActivePort"))
	if err != nil {
		return nil, err
	}
	lines := strings.Split(strings.TrimSpace(string(data)), "\n")
	if len(lines) == 0 {
		return nil, errors.New("devtools active port missing")
	}
	port, err := strconv.Atoi(strings.TrimSpace(lines[0]))
	if err != nil || port <= 0 {
		return nil, errors.New("devtools active port invalid")
	}
	proc := &browserProcess{
		endpoint:   "http://127.0.0.1:" + strconv.Itoa(port),
		profileDir: profileDir,
		port:       port,
	}
	if err := proc.ping(ctx, client); err != nil {
		return nil, err
	}
	return proc, nil
}

func profileDir(cfg Config) (string, error) {
	homeDir := strings.TrimSpace(cfg.HomeDir)
	if homeDir == "" {
		return "", fmt.Errorf("%w: home dir is required", ErrUnavailable)
	}
	return filepath.Join(homeDir, "browser-profiles", "default"), nil
}

func (m *Manager) currentProcess(ctx context.Context) *browserProcess {
	m.mu.Lock()
	proc := m.process
	m.mu.Unlock()
	if proc == nil {
		return nil
	}
	if err := proc.ping(ctx, m.client); err != nil {
		m.mu.Lock()
		if m.process == proc {
			m.process = nil
			m.clearBindingsLocked()
		}
		m.mu.Unlock()
		return nil
	}
	return proc
}

func (m *Manager) resolveTab(ctx context.Context, sessionID, tabID string, create bool) (*tabBinding, error) {
	sessionID = strings.TrimSpace(sessionID)
	tabID = strings.TrimSpace(tabID)
	if sessionID == "" {
		return nil, errors.New("session id is required")
	}
	if tabID != "" {
		return m.binding(sessionID, tabID)
	}
	bindings := m.sessionBindings(sessionID)
	switch len(bindings) {
	case 0:
		if !create {
			return nil, ErrTabRequired
		}
		tab, err := m.CreateTab(ctx, sessionID)
		if err != nil {
			return nil, err
		}
		return m.binding(sessionID, tab.ID)
	case 1:
		return bindings[0], nil
	default:
		return nil, ErrTabRequired
	}
}

func (m *Manager) binding(sessionID, tabID string) (*tabBinding, error) {
	sessionID = strings.TrimSpace(sessionID)
	tabID = strings.TrimSpace(tabID)
	if sessionID == "" {
		return nil, errors.New("session id is required")
	}
	if tabID == "" {
		return nil, ErrTabRequired
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	binding := m.tabs[tabID]
	if binding == nil || binding.sessionID != sessionID {
		return nil, ErrTabNotFound
	}
	cp := *binding
	return &cp, nil
}

func (m *Manager) sessionBindings(sessionID string) []*tabBinding {
	m.mu.Lock()
	defer m.mu.Unlock()
	ids := m.sessions[sessionID]
	out := make([]*tabBinding, 0, len(ids))
	for id := range ids {
		if binding := m.tabs[id]; binding != nil {
			cp := *binding
			out = append(out, &cp)
		}
	}
	return out
}

func (m *Manager) releaseSessionBindings(sessionID string) []*tabBinding {
	m.mu.Lock()
	defer m.mu.Unlock()
	ids := m.sessions[sessionID]
	out := make([]*tabBinding, 0, len(ids))
	for id := range ids {
		if binding := m.tabs[id]; binding != nil {
			cp := *binding
			out = append(out, &cp)
		}
		delete(m.tabs, id)
	}
	delete(m.sessions, sessionID)
	return out
}

func (m *Manager) removeBinding(tabID string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	binding := m.tabs[tabID]
	if binding == nil {
		return
	}
	delete(m.tabs, tabID)
	if ids := m.sessions[binding.sessionID]; ids != nil {
		delete(ids, tabID)
		if len(ids) == 0 {
			delete(m.sessions, binding.sessionID)
		}
	}
}

func (m *Manager) clearBindingsLocked() {
	m.tabs = map[string]*tabBinding{}
	m.sessions = map[string]map[string]bool{}
}

func (m *Manager) touch(tabID string) {
	m.mu.Lock()
	if binding := m.tabs[tabID]; binding != nil {
		binding.updatedAt = time.Now().UTC()
	}
	m.mu.Unlock()
}

func launch(ctx context.Context, cfg Config, client *http.Client) (*browserProcess, error) {
	chromePath := strings.TrimSpace(cfg.ChromePath)
	if chromePath == "" {
		var err error
		chromePath, err = findChromeExecutable()
		if err != nil {
			return nil, fmt.Errorf("%w: %v", ErrUnavailable, err)
		}
	}
	profileDir, err := profileDir(cfg)
	if err != nil {
		return nil, err
	}
	if err := os.MkdirAll(profileDir, 0o700); err != nil {
		return nil, err
	}
	port, err := freeLoopbackPort()
	if err != nil {
		return nil, err
	}
	args := []string{
		"--remote-debugging-address=127.0.0.1",
		"--remote-debugging-port=" + strconv.Itoa(port),
		"--user-data-dir=" + profileDir,
		"--no-first-run",
		"--no-default-browser-check",
		"--disable-background-mode",
	}
	if cfg.Headless {
		args = append(args, "--headless=new")
	}
	args = append(args, "about:blank")
	cmd := exec.Command(chromePath, args...)
	if err := cmd.Start(); err != nil {
		return nil, err
	}
	proc := &browserProcess{
		cmd:        cmd,
		endpoint:   "http://127.0.0.1:" + strconv.Itoa(port),
		chromePath: chromePath,
		profileDir: profileDir,
		port:       port,
	}
	if err := proc.wait(ctx, client); err != nil {
		_ = cmd.Process.Kill()
		return nil, err
	}
	return proc, nil
}

func (p *browserProcess) wait(ctx context.Context, client *http.Client) error {
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	ticker := time.NewTicker(150 * time.Millisecond)
	defer ticker.Stop()
	for {
		if err := p.ping(ctx, client); err == nil {
			return nil
		}
		select {
		case <-ctx.Done():
			return fmt.Errorf("browser did not become ready: %w", ctx.Err())
		case <-ticker.C:
		}
	}
}

func (p *browserProcess) ping(ctx context.Context, client *http.Client) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, p.endpoint+"/json/version", nil)
	if err != nil {
		return err
	}
	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("devtools status %d", resp.StatusCode)
	}
	return nil
}

func (p *browserProcess) newTarget(ctx context.Context, client *http.Client, rawURL string) (targetInfo, error) {
	targetURL := p.endpoint + "/json/new?" + url.QueryEscape(rawURL)
	req, err := http.NewRequestWithContext(ctx, http.MethodPut, targetURL, nil)
	if err != nil {
		return targetInfo{}, err
	}
	resp, err := client.Do(req)
	if err != nil {
		return targetInfo{}, err
	}
	defer resp.Body.Close()
	if resp.StatusCode == http.StatusMethodNotAllowed {
		return p.newTargetGET(ctx, client, rawURL)
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 1024))
		return targetInfo{}, fmt.Errorf("create tab: status %d: %s", resp.StatusCode, strings.TrimSpace(string(body)))
	}
	var target targetInfo
	if err := json.NewDecoder(resp.Body).Decode(&target); err != nil {
		return targetInfo{}, err
	}
	if target.ID == "" {
		return targetInfo{}, errors.New("create tab returned no target id")
	}
	return target, nil
}

func (p *browserProcess) newTargetGET(ctx context.Context, client *http.Client, rawURL string) (targetInfo, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, p.endpoint+"/json/new?"+url.QueryEscape(rawURL), nil)
	if err != nil {
		return targetInfo{}, err
	}
	resp, err := client.Do(req)
	if err != nil {
		return targetInfo{}, err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 1024))
		return targetInfo{}, fmt.Errorf("create tab: status %d: %s", resp.StatusCode, strings.TrimSpace(string(body)))
	}
	var target targetInfo
	if err := json.NewDecoder(resp.Body).Decode(&target); err != nil {
		return targetInfo{}, err
	}
	if target.ID == "" {
		return targetInfo{}, errors.New("create tab returned no target id")
	}
	return target, nil
}

func (p *browserProcess) listTargets(ctx context.Context, client *http.Client) ([]targetInfo, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, p.endpoint+"/json/list", nil)
	if err != nil {
		return nil, err
	}
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("list tabs: status %d", resp.StatusCode)
	}
	var targets []targetInfo
	if err := json.NewDecoder(resp.Body).Decode(&targets); err != nil {
		return nil, err
	}
	return targets, nil
}

func (p *browserProcess) target(ctx context.Context, client *http.Client, targetID string) (targetInfo, error) {
	targets, err := p.listTargets(ctx, client)
	if err != nil {
		return targetInfo{}, err
	}
	for _, target := range targets {
		if target.ID == targetID {
			return target, nil
		}
	}
	return targetInfo{}, ErrTabNotFound
}

func (p *browserProcess) closeTarget(ctx context.Context, client *http.Client, targetID string) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, p.endpoint+"/json/close/"+url.PathEscape(targetID), nil)
	if err != nil {
		return err
	}
	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("close tab: status %d", resp.StatusCode)
	}
	return nil
}

func (p *browserProcess) waitReady(ctx context.Context, client *http.Client, targetID string) error {
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	ticker := time.NewTicker(200 * time.Millisecond)
	defer ticker.Stop()
	for {
		raw, err := p.evaluateJSON(ctx, client, targetID, `(() => JSON.stringify({readyState: document.readyState}))()`)
		if err == nil {
			var payload struct {
				ReadyState string `json:"readyState"`
			}
			if json.Unmarshal(raw, &payload) == nil && (payload.ReadyState == "interactive" || payload.ReadyState == "complete") {
				return nil
			}
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-ticker.C:
		}
	}
}

func (p *browserProcess) evaluateJSON(ctx context.Context, client *http.Client, targetID, expression string) (json.RawMessage, error) {
	raw, err := p.cdpCall(ctx, client, targetID, "Runtime.evaluate", map[string]any{
		"expression":    expression,
		"returnByValue": true,
		"awaitPromise":  true,
	})
	if err != nil {
		return nil, err
	}
	var response struct {
		Result struct {
			Type        string          `json:"type"`
			Value       json.RawMessage `json:"value"`
			Description string          `json:"description"`
		} `json:"result"`
		ExceptionDetails *struct {
			Text string `json:"text"`
		} `json:"exceptionDetails"`
	}
	if err := json.Unmarshal(raw, &response); err != nil {
		return nil, err
	}
	if response.ExceptionDetails != nil {
		return nil, errors.New(response.ExceptionDetails.Text)
	}
	var text string
	if len(response.Result.Value) > 0 && json.Unmarshal(response.Result.Value, &text) == nil {
		return json.RawMessage(text), nil
	}
	if len(response.Result.Value) > 0 {
		return response.Result.Value, nil
	}
	return nil, errors.New("runtime evaluation returned no value")
}

func (p *browserProcess) cdpCall(ctx context.Context, client *http.Client, targetID, method string, params any) (json.RawMessage, error) {
	target, err := p.target(ctx, client, targetID)
	if err != nil {
		return nil, err
	}
	if target.WebSocketDebuggerURL == "" {
		return nil, errors.New("target has no websocket debugger url")
	}
	conn, _, err := websocket.Dial(ctx, target.WebSocketDebuggerURL, nil)
	if err != nil {
		return nil, err
	}
	defer conn.Close(websocket.StatusNormalClosure, "")
	conn.SetReadLimit(32 << 20)
	req := map[string]any{"id": 1, "method": method}
	if params != nil {
		req["params"] = params
	}
	data, err := json.Marshal(req)
	if err != nil {
		return nil, err
	}
	if err := conn.Write(ctx, websocket.MessageText, data); err != nil {
		return nil, err
	}
	for {
		_, msg, err := conn.Read(ctx)
		if err != nil {
			return nil, err
		}
		var resp struct {
			ID     int             `json:"id"`
			Result json.RawMessage `json:"result"`
			Error  *struct {
				Code    int    `json:"code"`
				Message string `json:"message"`
			} `json:"error"`
		}
		if err := json.Unmarshal(msg, &resp); err != nil {
			return nil, err
		}
		if resp.ID != 1 {
			continue
		}
		if resp.Error != nil {
			return nil, fmt.Errorf("cdp %s: %s", method, resp.Error.Message)
		}
		return resp.Result, nil
	}
}

func snapshotFromTarget(binding *tabBinding, target targetInfo) TabSnapshot {
	if binding == nil {
		return TabSnapshot{}
	}
	return TabSnapshot{
		ID:        binding.id,
		SessionID: binding.sessionID,
		TargetID:  binding.targetID,
		URL:       target.URL,
		Title:     target.Title,
		CreatedAt: binding.createdAt,
		UpdatedAt: binding.updatedAt,
	}
}

func normalizeURL(raw string) (string, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return "", errors.New("url is required")
	}
	if raw == "about:blank" {
		return raw, nil
	}
	if !strings.Contains(raw, "://") {
		scheme := "https"
		if strings.HasPrefix(raw, "localhost") || strings.HasPrefix(raw, "127.") || strings.HasPrefix(raw, "[::1]") {
			scheme = "http"
		}
		raw = scheme + "://" + raw
	}
	u, err := url.Parse(raw)
	if err != nil {
		return "", err
	}
	switch u.Scheme {
	case "http", "https":
	default:
		return "", errors.New("only http and https URLs are supported")
	}
	if u.Host == "" {
		return "", errors.New("url host is required")
	}
	return u.String(), nil
}

func clampInt(value, def, max int) int {
	if value <= 0 {
		value = def
	}
	if value > max {
		value = max
	}
	return value
}

func freeLoopbackPort() (int, error) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return 0, err
	}
	defer ln.Close()
	addr, ok := ln.Addr().(*net.TCPAddr)
	if !ok {
		return 0, errors.New("unexpected tcp addr")
	}
	return addr.Port, nil
}

func findChromeExecutable() (string, error) {
	if env := strings.TrimSpace(os.Getenv("PUDDING_CHROME_PATH")); env != "" {
		if fileExists(env) {
			return env, nil
		}
		return "", fmt.Errorf("PUDDING_CHROME_PATH does not exist: %s", env)
	}
	var candidates []string
	switch runtime.GOOS {
	case "darwin":
		candidates = []string{
			"/Applications/Google Chrome.app/Contents/MacOS/Google Chrome",
			"/Applications/Chromium.app/Contents/MacOS/Chromium",
		}
		if home, err := os.UserHomeDir(); err == nil {
			candidates = append(candidates,
				filepath.Join(home, "Applications/Google Chrome.app/Contents/MacOS/Google Chrome"),
				filepath.Join(home, "Applications/Chromium.app/Contents/MacOS/Chromium"),
			)
		}
	case "windows":
		for _, root := range []string{os.Getenv("PROGRAMFILES"), os.Getenv("PROGRAMFILES(X86)"), os.Getenv("LOCALAPPDATA")} {
			if root == "" {
				continue
			}
			candidates = append(candidates,
				filepath.Join(root, "Google/Chrome/Application/chrome.exe"),
				filepath.Join(root, "Chromium/Application/chrome.exe"),
			)
		}
	default:
		for _, name := range []string{"google-chrome", "google-chrome-stable", "chromium", "chromium-browser", "chrome"} {
			if path, err := exec.LookPath(name); err == nil {
				return path, nil
			}
		}
	}
	for _, path := range candidates {
		if fileExists(path) {
			return path, nil
		}
	}
	return "", errors.New("Chrome/Chromium executable not found")
}

func fileExists(path string) bool {
	info, err := os.Stat(path)
	return err == nil && !info.IsDir()
}

func newID(prefix string) string {
	var b [8]byte
	if _, err := rand.Read(b[:]); err != nil {
		return prefix + "_" + strconv.FormatInt(time.Now().UnixNano(), 36)
	}
	return prefix + "_" + hex.EncodeToString(b[:])
}

func observeScript(maxText, maxElements int) string {
	return fmt.Sprintf(`(() => {
  const maxText = %d;
  const maxElements = %d;
  const pickText = (el) => ((el.innerText || el.value || el.getAttribute("aria-label") || el.textContent || "").trim().replace(/\s+/g, " "));
  const visible = (el) => {
    const style = window.getComputedStyle(el);
    const rect = el.getBoundingClientRect();
    return style && style.visibility !== "hidden" && style.display !== "none" && rect.width > 0 && rect.height > 0;
  };
  const selectorFor = (el) => {
    if (!el || !el.tagName) return "";
    if (el.id) return "#" + CSS.escape(el.id);
    const name = el.getAttribute("name");
    if (name) return el.tagName.toLowerCase() + "[name=\"" + name.replace(/"/g, "\\\"") + "\"]";
    return el.tagName.toLowerCase();
  };
  const nodes = Array.from(document.querySelectorAll("a,button,input,textarea,select,[role=button],[contenteditable=true]"));
  const elements = [];
  for (const el of nodes) {
    if (elements.length >= maxElements) break;
    if (!visible(el)) continue;
    const text = pickText(el).slice(0, 160);
    const href = el.href || "";
    const ariaLabel = el.getAttribute("aria-label") || "";
    elements.push({
      index: elements.length,
      tag: el.tagName.toLowerCase(),
      text,
      href,
      role: el.getAttribute("role") || "",
      ariaLabel,
      selector: selectorFor(el),
      inputType: el.getAttribute("type") || "",
      disabled: Boolean(el.disabled || el.getAttribute("aria-disabled") === "true")
    });
  }
  const fullText = (document.body ? document.body.innerText : "").trim();
  const text = fullText.slice(0, maxText);
  return JSON.stringify({
    title: document.title,
    url: location.href,
    readyState: document.readyState,
    text,
    textChars: fullText.length,
    truncated: fullText.length > text.length,
    elements
  });
})()`, maxText, maxElements)
}

func clickScript(in ClickInput) string {
	selector := jsString(in.Selector)
	x := "null"
	y := "null"
	if in.X != nil {
		x = strconv.FormatFloat(*in.X, 'f', -1, 64)
	}
	if in.Y != nil {
		y = strconv.FormatFloat(*in.Y, 'f', -1, 64)
	}
	return fmt.Sprintf(`(() => {
  const selector = %s;
  const x = %s;
  const y = %s;
  let el = selector ? document.querySelector(selector) : null;
  if (!el && x !== null && y !== null) el = document.elementFromPoint(x, y);
  if (!el) throw new Error("target element not found");
  el.scrollIntoView({block: "center", inline: "center"});
  const rect = el.getBoundingClientRect();
  el.click();
  return JSON.stringify({ok: true, tag: el.tagName.toLowerCase(), text: (el.innerText || el.value || "").trim().slice(0, 160), x: rect.left + rect.width / 2, y: rect.top + rect.height / 2});
})()`, selector, x, y)
}

func typeScript(in TypeInput) string {
	return fmt.Sprintf(`(() => {
  const selector = %s;
  const text = %s;
  const clear = %t;
  let el = selector ? document.querySelector(selector) : document.activeElement;
  if (!el || el === document.body) throw new Error("target input not found");
  el.scrollIntoView({block: "center", inline: "center"});
  el.focus();
  if ("value" in el) {
    el.value = clear ? text : String(el.value || "") + text;
    el.dispatchEvent(new InputEvent("input", {bubbles: true, inputType: "insertText", data: text}));
    el.dispatchEvent(new Event("change", {bubbles: true}));
  } else if (el.isContentEditable) {
    if (clear) el.textContent = "";
    el.textContent = String(el.textContent || "") + text;
    el.dispatchEvent(new InputEvent("input", {bubbles: true, inputType: "insertText", data: text}));
  } else {
    throw new Error("target is not editable");
  }
  return JSON.stringify({ok: true, tag: el.tagName.toLowerCase(), textLength: text.length});
})()`, jsString(in.Selector), jsString(in.Text), in.Clear)
}

func scrollScript(in ScrollInput) string {
	return fmt.Sprintf(`(() => {
  const selector = %s;
  const dx = %s;
  const dy = %s;
  const target = selector ? document.querySelector(selector) : window;
  if (!target) throw new Error("scroll target not found");
  if (target === window) window.scrollBy(dx, dy);
  else target.scrollBy(dx, dy);
  return JSON.stringify({ok: true, x: window.scrollX, y: window.scrollY});
})()`, jsString(in.Selector), strconv.FormatFloat(in.DeltaX, 'f', -1, 64), strconv.FormatFloat(in.DeltaY, 'f', -1, 64))
}

func jsString(value string) string {
	b, err := json.Marshal(value)
	if err != nil {
		return `""`
	}
	return string(bytes.TrimSpace(b))
}

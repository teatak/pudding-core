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
	globalProcessKey        = "global"
)

var (
	ErrUnavailable = errors.New("browser unavailable")
	ErrTabNotFound = errors.New("browser tab not found")
	ErrTabRequired = errors.New("browser tab id required")
)

type Service interface {
	ProcessMode(ctx context.Context, sessionID string) string
	CreateTab(ctx context.Context, sessionID string) (TabSnapshot, error)
	ListTabs(ctx context.Context, sessionID string) ([]TabSnapshot, error)
	GetTab(ctx context.Context, sessionID, tabID string) (TabSnapshot, error)
	Recover(ctx context.Context, sessionID string, hint RecoverHint) (TabSnapshot, error)
	CloseSessionBrowser(ctx context.Context, sessionID string) error
	ReleaseTab(ctx context.Context, sessionID, tabID string) error
	ReleaseSession(ctx context.Context, sessionID string) error
	Open(ctx context.Context, sessionID, tabID, rawURL string) (TabSnapshot, error)
	Back(ctx context.Context, sessionID, tabID string) (TabSnapshot, error)
	Forward(ctx context.Context, sessionID, tabID string) (TabSnapshot, error)
	Reload(ctx context.Context, sessionID, tabID string) (TabSnapshot, error)
	Observe(ctx context.Context, sessionID, tabID string, opts ObserveOptions) (ObserveResult, error)
	Screenshot(ctx context.Context, sessionID, tabID string, opts ScreenshotOptions) (ScreenshotResult, error)
	Click(ctx context.Context, sessionID, tabID string, in ClickInput) (ActionResult, error)
	Type(ctx context.Context, sessionID, tabID string, in TypeInput) (ActionResult, error)
	Scroll(ctx context.Context, sessionID, tabID string, in ScrollInput) (ActionResult, error)
	Close() error
}

type MetadataRecoverySupport interface {
	SupportsMetadataRecovery() bool
}

type Config struct {
	HomeDir    string
	ChromePath string
	Headless   bool
}

type Manager struct {
	mu          sync.Mutex
	lifecycleMu sync.Mutex
	cfg         Config
	client      *http.Client
	processes   map[string]*browserProcess
	tabs        map[string]*tabBinding
	sessions    map[string]map[string]bool
}

type TabSnapshot struct {
	ID           string    `json:"id"`
	SessionID    string    `json:"sessionID"`
	TargetID     string    `json:"targetID,omitempty"`
	URL          string    `json:"url"`
	Title        string    `json:"title"`
	FaviconURL   string    `json:"faviconURL,omitempty"`
	Mode         string    `json:"mode,omitempty"`
	CanGoBack    bool      `json:"canGoBack,omitempty"`
	CanGoForward bool      `json:"canGoForward,omitempty"`
	CreatedAt    time.Time `json:"createdAt"`
	UpdatedAt    time.Time `json:"updatedAt"`
}

type RecoverHint struct {
	TabID      string
	URL        string
	Title      string
	FaviconURL string
	Mode       string
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
	Method   string   `json:"method,omitempty"`
}

type clickTarget struct {
	OK     bool    `json:"ok"`
	Tag    string  `json:"tag"`
	Text   string  `json:"text"`
	X      float64 `json:"x"`
	Y      float64 `json:"y"`
	Method string  `json:"method"`
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
	id         string
	sessionID  string
	targetID   string
	url        string
	title      string
	faviconURL string
	createdAt  time.Time
	updatedAt  time.Time
}

type browserProcess struct {
	cmd        *exec.Cmd
	endpoint   string
	chromePath string
	profileDir string
	port       int
	headless   bool
}

type targetInfo struct {
	ID                   string `json:"id"`
	Type                 string `json:"type"`
	URL                  string `json:"url"`
	Title                string `json:"title"`
	FaviconURL           string `json:"faviconUrl"`
	WebSocketDebuggerURL string `json:"webSocketDebuggerUrl"`
}

type versionInfo struct {
	Browser   string `json:"Browser"`
	UserAgent string `json:"User-Agent"`
}

func NewManager(cfg Config) *Manager {
	return &Manager{
		cfg:       cfg,
		client:    &http.Client{Timeout: 10 * time.Second},
		processes: map[string]*browserProcess{},
		tabs:      map[string]*tabBinding{},
		sessions:  map[string]map[string]bool{},
	}
}

func (m *Manager) CreateTab(ctx context.Context, sessionID string) (TabSnapshot, error) {
	_, binding, target, err := m.createTab(ctx, sessionID, "about:blank")
	if err != nil {
		return TabSnapshot{}, err
	}
	return m.snapshotFromLiveTarget(ctx, binding, target), nil
}

func (m *Manager) ProcessMode(ctx context.Context, sessionID string) string {
	sessionID = strings.TrimSpace(sessionID)
	if sessionID == "" {
		return "headless"
	}
	proc := m.currentProcess(ctx, sessionID)
	if proc != nil {
		if proc.headless {
			return "headless"
		}
		return "external"
	}
	m.lifecycleMu.Lock()
	defer m.lifecycleMu.Unlock()
	m.mu.Lock()
	cfg := m.cfg
	m.mu.Unlock()
	proc, err := attachExisting(ctx, cfg, sessionID, m.client)
	if err != nil {
		return "headless"
	}
	m.setProcess(sessionID, proc)
	if proc.headless {
		return "headless"
	}
	return "external"
}

func (m *Manager) createTab(ctx context.Context, sessionID, rawURL string) (*browserProcess, *tabBinding, targetInfo, error) {
	sessionID = strings.TrimSpace(sessionID)
	if sessionID == "" {
		return nil, nil, targetInfo{}, errors.New("session id is required")
	}
	rawURL = strings.TrimSpace(rawURL)
	if rawURL == "" {
		rawURL = "about:blank"
	}
	proc, err := m.ensureProcess(ctx, sessionID)
	if err != nil {
		return nil, nil, targetInfo{}, err
	}
	target, err := proc.newTarget(ctx, m.client, rawURL)
	if err != nil {
		return nil, nil, targetInfo{}, err
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
	return proc, binding, target, nil
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
	out := make([]TabSnapshot, 0, len(bindings))
	proc, err := m.ensureProcess(ctx, sessionID)
	if err != nil {
		return nil, err
	}
	for _, binding := range bindings {
		target, err := proc.target(ctx, m.client, binding.targetID)
		if err != nil {
			continue
		}
		out = append(out, m.snapshotFromLiveTarget(ctx, binding, target))
	}
	return out, nil
}

func (m *Manager) GetTab(ctx context.Context, sessionID, tabID string) (TabSnapshot, error) {
	_, binding, target, err := m.liveTarget(ctx, sessionID, tabID, false)
	if err != nil {
		return TabSnapshot{}, err
	}
	return m.snapshotFromLiveTarget(ctx, binding, target), nil
}

func (m *Manager) Recover(ctx context.Context, sessionID string, hint RecoverHint) (TabSnapshot, error) {
	sessionID = strings.TrimSpace(sessionID)
	if sessionID == "" {
		return TabSnapshot{}, errors.New("session id is required")
	}
	mode := strings.TrimSpace(hint.Mode)
	if mode == "" {
		mode = m.mode(sessionID)
	}
	if mode == "external" {
		return m.recoverExternal(ctx, sessionID, hint)
	}
	return m.recoverInternal(ctx, sessionID, hint)
}

func (m *Manager) recoverExternal(ctx context.Context, sessionID string, hint RecoverHint) (TabSnapshot, error) {
	proc, err := m.attachExternalProcess(ctx, sessionID)
	if err != nil {
		return TabSnapshot{}, err
	}
	targets, err := proc.listTargets(ctx, m.client)
	if err != nil {
		return TabSnapshot{}, err
	}
	target, ok := recoverTarget(targets, hint)
	if !ok {
		return TabSnapshot{}, ErrTabNotFound
	}
	now := time.Now().UTC()
	tabID := strings.TrimSpace(hint.TabID)
	if tabID == "" {
		tabID = newID("tab")
	}
	binding := &tabBinding{
		id:         tabID,
		sessionID:  sessionID,
		targetID:   target.ID,
		url:        target.URL,
		title:      target.Title,
		faviconURL: target.FaviconURL,
		createdAt:  now,
		updatedAt:  now,
	}
	m.mu.Lock()
	if existing := m.tabs[tabID]; existing != nil {
		binding.createdAt = existing.createdAt
	}
	m.tabs[tabID] = binding
	if m.sessions[sessionID] == nil {
		m.sessions[sessionID] = map[string]bool{}
	}
	m.sessions[sessionID][tabID] = true
	m.mu.Unlock()
	return m.snapshotFromLiveTarget(ctx, binding, target), nil
}

func (m *Manager) recoverInternal(ctx context.Context, sessionID string, hint RecoverHint) (TabSnapshot, error) {
	tabID := strings.TrimSpace(hint.TabID)
	if tabID == "" {
		return TabSnapshot{}, ErrTabRequired
	}
	binding, err := m.binding(sessionID, tabID)
	if err != nil {
		return TabSnapshot{}, err
	}
	proc, err := m.ensureProcess(ctx, sessionID)
	if err != nil {
		return TabSnapshot{}, err
	}
	if !proc.headless {
		return m.recoverExternal(ctx, sessionID, RecoverHint{
			TabID:      tabID,
			URL:        firstNonBlank(hint.URL, binding.url),
			Title:      firstNonBlank(hint.Title, binding.title),
			FaviconURL: firstNonBlank(hint.FaviconURL, binding.faviconURL),
			Mode:       "external",
		})
	}
	if binding.targetID != "" {
		_ = proc.closeTarget(ctx, m.client, binding.targetID)
	}
	rawURL := firstNonBlank(hint.URL, binding.url)
	if rawURL == "" {
		rawURL = "about:blank"
	}
	target, err := proc.newTarget(ctx, m.client, rawURL)
	if err != nil {
		return TabSnapshot{}, err
	}
	_ = proc.waitReady(ctx, m.client, target.ID)
	if current, err := proc.target(ctx, m.client, target.ID); err == nil {
		target = current
	}
	m.rememberBindingTarget(binding.id, target)
	if target.Title == "" || target.FaviconURL == "" {
		m.mu.Lock()
		if current := m.tabs[binding.id]; current != nil && current.sessionID == sessionID {
			if target.Title == "" && hint.Title != "" {
				current.title = hint.Title
			}
			if target.FaviconURL == "" && hint.FaviconURL != "" {
				current.faviconURL = hint.FaviconURL
			}
		}
		m.mu.Unlock()
	}
	return m.GetTab(ctx, sessionID, binding.id)
}

func (m *Manager) ReleaseTab(ctx context.Context, sessionID, tabID string) error {
	binding, err := m.binding(sessionID, tabID)
	if err != nil {
		return err
	}
	if proc := m.currentProcess(ctx, binding.sessionID); proc != nil {
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
	return m.CloseSessionBrowser(ctx, sessionID)
}

func (m *Manager) CloseSessionBrowser(ctx context.Context, sessionID string) error {
	sessionID = strings.TrimSpace(sessionID)
	if sessionID == "" {
		return errors.New("session id is required")
	}
	bindings := m.releaseSessionBindings(sessionID)
	if proc := m.currentProcess(ctx, sessionID); proc != nil {
		for _, binding := range bindings {
			_ = proc.closeTarget(ctx, m.client, binding.targetID)
		}
	}
	return nil
}

func (m *Manager) Open(ctx context.Context, sessionID, tabID, rawURL string) (TabSnapshot, error) {
	sessionID = strings.TrimSpace(sessionID)
	tabID = strings.TrimSpace(tabID)
	rawURL, err := normalizeURL(rawURL)
	if err != nil {
		return TabSnapshot{}, err
	}
	proc, binding, _, err := m.liveTarget(ctx, sessionID, tabID, false)
	if err != nil {
		if errors.Is(err, ErrTabRequired) && tabID == "" && len(m.sessionBindings(sessionID)) == 0 {
			proc, binding, target, err := m.createTab(ctx, sessionID, rawURL)
			if err != nil {
				return TabSnapshot{}, err
			}
			_ = proc.waitReady(ctx, m.client, binding.targetID)
			if current, err := proc.target(ctx, m.client, binding.targetID); err == nil {
				target = current
			}
			m.touch(binding.id)
			return m.snapshotFromLiveTarget(ctx, binding, target), nil
		}
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
	return m.snapshotFromLiveTarget(ctx, binding, target), nil
}

func (m *Manager) Reveal(ctx context.Context, sessionID, tabID string) (TabSnapshot, error) {
	proc, binding, target, err := m.liveTarget(ctx, sessionID, tabID, false)
	if err != nil {
		return TabSnapshot{}, err
	}
	if proc.headless {
		if err := m.switchMode(ctx, sessionID, false); err != nil {
			return TabSnapshot{}, err
		}
	}
	proc, binding, target, err = m.liveTarget(ctx, sessionID, binding.id, false)
	if err != nil {
		return TabSnapshot{}, err
	}
	if err := proc.activateTarget(ctx, m.client, binding.targetID); err != nil {
		return TabSnapshot{}, err
	}
	m.touch(binding.id)
	return m.snapshotFromLiveTarget(ctx, binding, target), nil
}

func (m *Manager) Internal(ctx context.Context, sessionID, tabID string) (TabSnapshot, error) {
	binding, err := m.binding(sessionID, tabID)
	if err != nil {
		return TabSnapshot{}, err
	}
	if proc := m.currentProcess(ctx, sessionID); proc != nil {
		if target, err := proc.target(ctx, m.client, binding.targetID); err == nil {
			m.rememberBindingTarget(binding.id, target)
		} else if !errors.Is(err, ErrTabNotFound) {
			return TabSnapshot{}, err
		}
	}
	if err := m.switchMode(ctx, sessionID, true); err != nil {
		return TabSnapshot{}, err
	}
	return m.GetTab(ctx, sessionID, binding.id)
}

func (m *Manager) Back(ctx context.Context, sessionID, tabID string) (TabSnapshot, error) {
	return m.navigateHistory(ctx, sessionID, tabID, -1)
}

func (m *Manager) Forward(ctx context.Context, sessionID, tabID string) (TabSnapshot, error) {
	return m.navigateHistory(ctx, sessionID, tabID, 1)
}

func (m *Manager) Reload(ctx context.Context, sessionID, tabID string) (TabSnapshot, error) {
	proc, binding, _, err := m.liveTarget(ctx, sessionID, tabID, false)
	if err != nil {
		return TabSnapshot{}, err
	}
	if _, err := proc.cdpCall(ctx, m.client, binding.targetID, "Page.reload", map[string]any{"ignoreCache": false}); err != nil {
		return TabSnapshot{}, err
	}
	_ = proc.waitReady(ctx, m.client, binding.targetID)
	target, err := proc.target(ctx, m.client, binding.targetID)
	if err != nil {
		return TabSnapshot{}, err
	}
	m.touch(binding.id)
	return m.snapshotFromLiveTarget(ctx, binding, target), nil
}

func (m *Manager) Observe(ctx context.Context, sessionID, tabID string, opts ObserveOptions) (ObserveResult, error) {
	proc, binding, _, err := m.liveTarget(ctx, sessionID, tabID, false)
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
	out.Tab = m.snapshotFromLiveTarget(ctx, binding, target)
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
	proc, binding, _, err := m.liveTarget(ctx, sessionID, tabID, false)
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
		Tab:               m.snapshotFromLiveTarget(ctx, binding, target),
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
	proc, binding, _, err := m.liveTarget(ctx, sessionID, tabID, false)
	if err != nil {
		return ActionResult{}, err
	}
	method := strings.ToLower(strings.TrimSpace(in.Method))
	if method == "" {
		method = "auto"
	}
	var raw json.RawMessage
	switch method {
	case "auto":
		raw, err = m.pointerClick(ctx, proc, binding, in, "pointer")
		if err != nil {
			raw, err = proc.evaluateJSON(ctx, m.client, binding.targetID, clickScript(in, "dom"))
		}
	case "pointer":
		raw, err = m.pointerClick(ctx, proc, binding, in, "pointer")
	case "dom":
		raw, err = proc.evaluateJSON(ctx, m.client, binding.targetID, clickScript(in, "dom"))
	default:
		return ActionResult{}, fmt.Errorf("unsupported click method %q", in.Method)
	}
	if err != nil {
		return ActionResult{}, err
	}
	result, err := m.actionResult(ctx, proc, binding, "click", raw)
	if err != nil {
		return ActionResult{}, err
	}
	return result, nil
}

func (m *Manager) pointerClick(ctx context.Context, proc *browserProcess, binding *tabBinding, in ClickInput, method string) (json.RawMessage, error) {
	raw, err := proc.evaluateJSON(ctx, m.client, binding.targetID, clickTargetScript(in, method))
	if err != nil {
		return nil, err
	}
	var target clickTarget
	if err := json.Unmarshal(raw, &target); err != nil {
		return nil, err
	}
	if !target.OK {
		return nil, errors.New("target element not found")
	}
	if err := proc.dispatchMouseClick(ctx, m.client, binding.targetID, target.X, target.Y); err != nil {
		return nil, err
	}
	return raw, nil
}

func (m *Manager) Type(ctx context.Context, sessionID, tabID string, in TypeInput) (ActionResult, error) {
	if tabID == "" {
		tabID = in.TabID
	}
	if in.Text == "" {
		return ActionResult{}, errors.New("text is required")
	}
	proc, binding, _, err := m.liveTarget(ctx, sessionID, tabID, false)
	if err != nil {
		return ActionResult{}, err
	}
	raw, err := proc.evaluateJSON(ctx, m.client, binding.targetID, typeScript(in))
	if err != nil {
		return ActionResult{}, err
	}
	result, err := m.actionResult(ctx, proc, binding, "type", raw)
	if err != nil {
		return ActionResult{}, err
	}
	return result, nil
}

func (m *Manager) Scroll(ctx context.Context, sessionID, tabID string, in ScrollInput) (ActionResult, error) {
	if tabID == "" {
		tabID = in.TabID
	}
	if in.DeltaX == 0 && in.DeltaY == 0 {
		in.DeltaY = 600
	}
	proc, binding, _, err := m.liveTarget(ctx, sessionID, tabID, false)
	if err != nil {
		return ActionResult{}, err
	}
	raw, err := proc.evaluateJSON(ctx, m.client, binding.targetID, scrollScript(in))
	if err != nil {
		return ActionResult{}, err
	}
	result, err := m.actionResult(ctx, proc, binding, "scroll", raw)
	if err != nil {
		return ActionResult{}, err
	}
	return result, nil
}

func (m *Manager) Close() error {
	m.mu.Lock()
	procs := make([]*browserProcess, 0, len(m.processes))
	for _, proc := range m.processes {
		procs = append(procs, proc)
	}
	m.processes = map[string]*browserProcess{}
	m.tabs = map[string]*tabBinding{}
	m.sessions = map[string]map[string]bool{}
	m.mu.Unlock()
	var firstErr error
	for _, proc := range procs {
		if err := stopBrowserProcess(proc); err != nil && firstErr == nil {
			firstErr = err
		}
	}
	return firstErr
}

func stopBrowserProcess(proc *browserProcess) error {
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
	return ActionResult{Tab: m.snapshotFromLiveTarget(ctx, binding, target), Action: action, Result: result}, nil
}

func numberValue(value any) (float64, bool) {
	switch v := value.(type) {
	case float64:
		return v, true
	case float32:
		return float64(v), true
	case int:
		return float64(v), true
	case int64:
		return float64(v), true
	case json.Number:
		n, err := v.Float64()
		return n, err == nil
	default:
		return 0, false
	}
}

func firstNonBlank(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return value
		}
	}
	return ""
}

func (m *Manager) liveTarget(ctx context.Context, sessionID, tabID string, create bool) (*browserProcess, *tabBinding, targetInfo, error) {
	binding, err := m.resolveTab(ctx, sessionID, tabID, create)
	if err != nil {
		return nil, nil, targetInfo{}, err
	}
	proc, err := m.ensureProcess(ctx, binding.sessionID)
	if err != nil {
		return nil, nil, targetInfo{}, err
	}
	target, err := proc.target(ctx, m.client, binding.targetID)
	if err == nil {
		return proc, binding, target, nil
	}
	if !errors.Is(err, ErrTabNotFound) {
		return nil, nil, targetInfo{}, err
	}
	if !create {
		return nil, nil, targetInfo{}, ErrTabNotFound
	}
	rawURL := binding.url
	if strings.TrimSpace(rawURL) == "" {
		rawURL = "about:blank"
	}
	target, err = proc.newTarget(ctx, m.client, rawURL)
	if err != nil {
		return nil, nil, targetInfo{}, err
	}
	binding.targetID = target.ID
	m.rememberBindingTarget(binding.id, target)
	return proc, binding, target, nil
}

func (m *Manager) rememberBindingTarget(tabID string, target targetInfo) {
	m.mu.Lock()
	if binding := m.tabs[tabID]; binding != nil {
		binding.targetID = target.ID
		binding.url = target.URL
		binding.title = target.Title
		binding.faviconURL = target.FaviconURL
		binding.updatedAt = time.Now().UTC()
	}
	m.mu.Unlock()
}

func (m *Manager) snapshotFromLiveTarget(ctx context.Context, binding *tabBinding, target targetInfo) TabSnapshot {
	snap := m.snapshotFromTarget(binding, target)
	if snap.TargetID == "" {
		return snap
	}
	proc := m.currentProcess(ctx, binding.sessionID)
	if proc == nil {
		return snap
	}
	if canGoBack, canGoForward, ok := proc.navigationAvailability(ctx, m.client, snap.TargetID); ok {
		snap.CanGoBack = canGoBack
		snap.CanGoForward = canGoForward
	}
	return snap
}

func (m *Manager) snapshotFromTarget(binding *tabBinding, target targetInfo) TabSnapshot {
	if binding != nil {
		m.mu.Lock()
		if current := m.tabs[binding.id]; current != nil {
			cp := *current
			binding = &cp
		}
		m.mu.Unlock()
	}
	snap := snapshotFromTarget(binding, target)
	if binding != nil {
		snap.Mode = m.mode(binding.sessionID)
	} else {
		snap.Mode = "headless"
	}
	m.rememberSnapshot(snap)
	return snap
}

func (m *Manager) mode(sessionID string) string {
	m.mu.Lock()
	defer m.mu.Unlock()
	if proc := m.processes[globalProcessKey]; proc != nil && !proc.headless {
		return "external"
	}
	return "headless"
}

func (m *Manager) rememberSnapshot(snap TabSnapshot) {
	if snap.ID == "" {
		return
	}
	m.mu.Lock()
	if binding := m.tabs[snap.ID]; binding != nil {
		binding.url = snap.URL
		binding.title = snap.Title
		binding.faviconURL = snap.FaviconURL
		binding.updatedAt = snap.UpdatedAt
	}
	m.mu.Unlock()
}

func (m *Manager) navigateHistory(ctx context.Context, sessionID, tabID string, delta int) (TabSnapshot, error) {
	proc, binding, _, err := m.liveTarget(ctx, sessionID, tabID, false)
	if err != nil {
		return TabSnapshot{}, err
	}
	raw, err := proc.cdpCall(ctx, m.client, binding.targetID, "Page.getNavigationHistory", nil)
	if err != nil {
		return TabSnapshot{}, err
	}
	var history struct {
		CurrentIndex int `json:"currentIndex"`
		Entries      []struct {
			ID int `json:"id"`
		} `json:"entries"`
	}
	if err := json.Unmarshal(raw, &history); err != nil {
		return TabSnapshot{}, err
	}
	nextIndex := history.CurrentIndex + delta
	if nextIndex >= 0 && nextIndex < len(history.Entries) {
		if _, err := proc.cdpCall(ctx, m.client, binding.targetID, "Page.navigateToHistoryEntry", map[string]any{
			"entryId": history.Entries[nextIndex].ID,
		}); err != nil {
			return TabSnapshot{}, err
		}
		_ = proc.waitReady(ctx, m.client, binding.targetID)
	}
	target, err := proc.target(ctx, m.client, binding.targetID)
	if err != nil {
		return TabSnapshot{}, err
	}
	m.touch(binding.id)
	return m.snapshotFromLiveTarget(ctx, binding, target), nil
}

func (m *Manager) ensureProcess(ctx context.Context, sessionID string) (*browserProcess, error) {
	sessionID = strings.TrimSpace(sessionID)
	if sessionID == "" {
		return nil, errors.New("session id is required")
	}
	m.lifecycleMu.Lock()
	defer m.lifecycleMu.Unlock()
	m.mu.Lock()
	proc := m.processes[globalProcessKey]
	cfg := m.cfg
	m.mu.Unlock()
	if proc != nil {
		if err := proc.ping(ctx, m.client); err == nil {
			return proc, nil
		}
		m.mu.Lock()
		if m.processes[globalProcessKey] == proc {
			delete(m.processes, globalProcessKey)
		}
		m.mu.Unlock()
		_ = stopBrowserProcess(proc)
	}
	if proc, err := attachExisting(ctx, cfg, sessionID, m.client); err == nil {
		m.setProcess(sessionID, proc)
		return proc, nil
	}
	reapStaleProfileOwner(cfg, sessionID)
	proc, err := launch(ctx, cfg, sessionID, m.client)
	if err != nil {
		return nil, err
	}
	m.setProcess(sessionID, proc)
	return proc, nil
}

func (m *Manager) setProcess(sessionID string, proc *browserProcess) {
	sessionID = strings.TrimSpace(sessionID)
	if sessionID == "" {
		return
	}
	m.mu.Lock()
	if m.processes == nil {
		m.processes = map[string]*browserProcess{}
	}
	m.processes[globalProcessKey] = proc
	m.mu.Unlock()
}

func (m *Manager) switchMode(ctx context.Context, sessionID string, headless bool) error {
	sessionID = strings.TrimSpace(sessionID)
	if sessionID == "" {
		return errors.New("session id is required")
	}
	m.lifecycleMu.Lock()
	defer m.lifecycleMu.Unlock()
	m.mu.Lock()
	proc := m.processes[globalProcessKey]
	cfg := m.cfg
	if proc != nil && proc.headless == headless {
		m.mu.Unlock()
		return nil
	}
	delete(m.processes, globalProcessKey)
	m.mu.Unlock()
	if err := stopBrowserProcess(proc); err != nil {
		return err
	}
	reapStaleProfileOwner(cfg, sessionID)
	next, err := launchMode(ctx, cfg, sessionID, m.client, headless)
	if err != nil {
		return err
	}
	m.setProcess(sessionID, next)
	return nil
}

func reapStaleProfileOwner(cfg Config, sessionID string) {
	profileDir, err := profileDir(cfg, sessionID)
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

func attachExisting(ctx context.Context, cfg Config, sessionID string, client *http.Client) (*browserProcess, error) {
	profileDir, err := profileDir(cfg, sessionID)
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
	version, err := proc.version(ctx, client)
	if err != nil {
		return nil, err
	}
	proc.headless = detectHeadless(version, cfg.Headless)
	return proc, nil
}

func profileDir(cfg Config, sessionID string) (string, error) {
	homeDir := strings.TrimSpace(cfg.HomeDir)
	if homeDir == "" {
		return "", fmt.Errorf("%w: home dir is required", ErrUnavailable)
	}
	if strings.TrimSpace(sessionID) == "" {
		return "", errors.New("session id is required")
	}
	return filepath.Join(homeDir, "browser-profiles", "default"), nil
}

func (m *Manager) currentProcess(ctx context.Context, sessionID string) *browserProcess {
	sessionID = strings.TrimSpace(sessionID)
	if sessionID == "" {
		return nil
	}
	m.lifecycleMu.Lock()
	defer m.lifecycleMu.Unlock()
	m.mu.Lock()
	proc := m.processes[globalProcessKey]
	m.mu.Unlock()
	if proc == nil {
		return nil
	}
	if err := proc.ping(ctx, m.client); err != nil {
		m.mu.Lock()
		if m.processes[globalProcessKey] == proc {
			delete(m.processes, globalProcessKey)
		}
		m.mu.Unlock()
		return nil
	}
	return proc
}

func (m *Manager) attachExternalProcess(ctx context.Context, sessionID string) (*browserProcess, error) {
	sessionID = strings.TrimSpace(sessionID)
	if sessionID == "" {
		return nil, errors.New("session id is required")
	}
	m.lifecycleMu.Lock()
	defer m.lifecycleMu.Unlock()
	m.mu.Lock()
	proc := m.processes[globalProcessKey]
	cfg := m.cfg
	m.mu.Unlock()
	if proc != nil {
		if err := proc.ping(ctx, m.client); err == nil {
			if proc.headless {
				return nil, ErrTabNotFound
			}
			return proc, nil
		}
		m.mu.Lock()
		if m.processes[globalProcessKey] == proc {
			delete(m.processes, globalProcessKey)
		}
		m.mu.Unlock()
		_ = stopBrowserProcess(proc)
	}
	proc, err := attachExisting(ctx, cfg, sessionID, m.client)
	if err != nil {
		return nil, ErrTabNotFound
	}
	if proc.headless {
		return nil, ErrTabNotFound
	}
	m.setProcess(sessionID, proc)
	return proc, nil
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

func (m *Manager) touch(tabID string) {
	m.mu.Lock()
	if binding := m.tabs[tabID]; binding != nil {
		binding.updatedAt = time.Now().UTC()
	}
	m.mu.Unlock()
}

func launch(ctx context.Context, cfg Config, sessionID string, client *http.Client) (*browserProcess, error) {
	return launchMode(ctx, cfg, sessionID, client, cfg.Headless)
}

func launchMode(ctx context.Context, cfg Config, sessionID string, client *http.Client, headless bool) (*browserProcess, error) {
	chromePath := strings.TrimSpace(cfg.ChromePath)
	if chromePath == "" {
		var err error
		chromePath, err = findChromeExecutable()
		if err != nil {
			return nil, fmt.Errorf("%w: %v", ErrUnavailable, err)
		}
	}
	profileDir, err := profileDir(cfg, sessionID)
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
	if headless {
		args = append(args, "--headless=new")
	}
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
		headless:   headless,
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
	_, err := p.version(ctx, client)
	return err
}

func (p *browserProcess) version(ctx context.Context, client *http.Client) (versionInfo, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, p.endpoint+"/json/version", nil)
	if err != nil {
		return versionInfo{}, err
	}
	resp, err := client.Do(req)
	if err != nil {
		return versionInfo{}, err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return versionInfo{}, fmt.Errorf("devtools status %d", resp.StatusCode)
	}
	var out versionInfo
	_ = json.NewDecoder(resp.Body).Decode(&out)
	return out, nil
}

func detectHeadless(version versionInfo, fallback bool) bool {
	marker := strings.ToLower(version.Browser + " " + version.UserAgent)
	if strings.Contains(marker, "headless") {
		return true
	}
	if strings.TrimSpace(marker) != "" {
		return false
	}
	return fallback
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

func recoverTarget(targets []targetInfo, hint RecoverHint) (targetInfo, bool) {
	rawURL := strings.TrimSpace(hint.URL)
	title := strings.TrimSpace(hint.Title)
	var candidates []targetInfo
	for _, target := range targets {
		if target.Type != "" && target.Type != "page" {
			continue
		}
		if strings.TrimSpace(target.URL) == "" || target.URL == "about:blank" {
			continue
		}
		candidates = append(candidates, target)
	}
	if rawURL != "" {
		for _, target := range candidates {
			if sameRecoverURL(target.URL, rawURL) {
				return target, true
			}
		}
	}
	if title != "" {
		for _, target := range candidates {
			if strings.TrimSpace(target.Title) == title {
				return target, true
			}
		}
	}
	if len(candidates) == 1 {
		return candidates[0], true
	}
	return targetInfo{}, false
}

func sameRecoverURL(a, b string) bool {
	a = strings.TrimSpace(a)
	b = strings.TrimSpace(b)
	if a == b {
		return true
	}
	return strings.TrimRight(a, "/") == strings.TrimRight(b, "/")
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

func (p *browserProcess) activateTarget(ctx context.Context, client *http.Client, targetID string) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, p.endpoint+"/json/activate/"+url.PathEscape(targetID), nil)
	if err != nil {
		return err
	}
	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 1024))
		return fmt.Errorf("activate tab: status %d: %s", resp.StatusCode, strings.TrimSpace(string(body)))
	}
	return nil
}

func (p *browserProcess) navigationAvailability(ctx context.Context, client *http.Client, targetID string) (bool, bool, bool) {
	raw, err := p.cdpCall(ctx, client, targetID, "Page.getNavigationHistory", nil)
	if err != nil {
		return false, false, false
	}
	var history struct {
		CurrentIndex int `json:"currentIndex"`
		Entries      []struct {
			ID int `json:"id"`
		} `json:"entries"`
	}
	if err := json.Unmarshal(raw, &history); err != nil {
		return false, false, false
	}
	return history.CurrentIndex > 0, history.CurrentIndex >= 0 && history.CurrentIndex < len(history.Entries)-1, true
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
			Text      string `json:"text"`
			Exception *struct {
				Description string `json:"description"`
			} `json:"exception"`
		} `json:"exceptionDetails"`
	}
	if err := json.Unmarshal(raw, &response); err != nil {
		return nil, err
	}
	if response.ExceptionDetails != nil {
		if response.ExceptionDetails.Exception != nil && response.ExceptionDetails.Exception.Description != "" {
			return nil, errors.New(response.ExceptionDetails.Exception.Description)
		}
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

func (p *browserProcess) dispatchMouseClick(ctx context.Context, client *http.Client, targetID string, x, y float64) error {
	events := []map[string]any{
		{
			"type":        "mouseMoved",
			"x":           x,
			"y":           y,
			"button":      "none",
			"buttons":     0,
			"clickCount":  0,
			"pointerType": "mouse",
		},
		{
			"type":        "mousePressed",
			"x":           x,
			"y":           y,
			"button":      "left",
			"buttons":     1,
			"clickCount":  1,
			"pointerType": "mouse",
		},
		{
			"type":        "mouseReleased",
			"x":           x,
			"y":           y,
			"button":      "left",
			"buttons":     0,
			"clickCount":  1,
			"pointerType": "mouse",
		},
	}
	for _, event := range events {
		if _, err := p.cdpCall(ctx, client, targetID, "Input.dispatchMouseEvent", event); err != nil {
			return err
		}
	}
	return nil
}

func snapshotFromTarget(binding *tabBinding, target targetInfo) TabSnapshot {
	if binding == nil {
		return TabSnapshot{}
	}
	rawURL := target.URL
	if rawURL == "" {
		rawURL = binding.url
	}
	title := target.Title
	if title == "about:blank" && rawURL != "" && rawURL != "about:blank" {
		title = ""
	}
	if title == "" {
		title = binding.title
		if title == "about:blank" && rawURL != "" && rawURL != "about:blank" {
			title = ""
		}
	}
	if title == "" {
		title = rawURL
	}
	faviconURL := target.FaviconURL
	if faviconURL == "" {
		faviconURL = binding.faviconURL
	}
	return TabSnapshot{
		ID:         binding.id,
		SessionID:  binding.sessionID,
		TargetID:   binding.targetID,
		URL:        rawURL,
		Title:      title,
		FaviconURL: faviconURL,
		CreatedAt:  binding.createdAt,
		UpdatedAt:  binding.updatedAt,
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

func clickTargetScript(in ClickInput, method string) string {
	selector := jsString(in.Selector)
	methodValue := jsString(method)
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
  const method = %s;
  const elementText = (node) => String(node.innerText || node.textContent || node.value || node.getAttribute?.("aria-label") || "").trim();
  const isVisible = (node) => Boolean(node && (node.offsetWidth || node.offsetHeight || node.getClientRects().length));
  const resolveSelector = (rawSelector) => {
    if (!rawSelector) return null;
    const match = rawSelector.match(/^(.*):contains\((["']?)(.*?)\2\)\s*$/);
    if (match) {
      const base = match[1].trim() || "*";
      const needle = match[3];
      let candidates;
      try {
        candidates = Array.from(document.querySelectorAll(base));
      } catch (err) {
        throw new Error("invalid selector: " + rawSelector);
      }
      return candidates.find((node) => isVisible(node) && elementText(node).includes(needle)) ||
        candidates.find((node) => elementText(node).includes(needle)) ||
        null;
    }
    try {
      return document.querySelector(rawSelector);
    } catch (err) {
      throw new Error("invalid selector: " + rawSelector);
    }
  };
  let el = selector ? resolveSelector(selector) : null;
  if (!el && x !== null && y !== null) el = document.elementFromPoint(x, y);
  if (!el) throw new Error("target element not found");
  if (selector) el.scrollIntoView({block: "center", inline: "center"});
  const rect = el.getBoundingClientRect();
  const cx = selector ? rect.left + rect.width / 2 : x;
  const cy = selector ? rect.top + rect.height / 2 : y;
  if (cx === null || cy === null) throw new Error("target coordinates not found");
  return JSON.stringify({ok: true, tag: el.tagName.toLowerCase(), text: (el.innerText || el.value || "").trim().slice(0, 160), x: cx, y: cy, cursorX: cx, cursorY: cy, method});
})()`, selector, x, y, methodValue)
}

func clickScript(in ClickInput, method string) string {
	selector := jsString(in.Selector)
	methodValue := jsString(method)
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
  const method = %s;
  const elementText = (node) => String(node.innerText || node.textContent || node.value || node.getAttribute?.("aria-label") || "").trim();
  const isVisible = (node) => Boolean(node && (node.offsetWidth || node.offsetHeight || node.getClientRects().length));
  const resolveSelector = (rawSelector) => {
    if (!rawSelector) return null;
    const match = rawSelector.match(/^(.*):contains\((["']?)(.*?)\2\)\s*$/);
    if (match) {
      const base = match[1].trim() || "*";
      const needle = match[3];
      let candidates;
      try {
        candidates = Array.from(document.querySelectorAll(base));
      } catch (err) {
        throw new Error("invalid selector: " + rawSelector);
      }
      return candidates.find((node) => isVisible(node) && elementText(node).includes(needle)) ||
        candidates.find((node) => elementText(node).includes(needle)) ||
        null;
    }
    try {
      return document.querySelector(rawSelector);
    } catch (err) {
      throw new Error("invalid selector: " + rawSelector);
    }
  };
  let el = selector ? resolveSelector(selector) : null;
  if (!el && x !== null && y !== null) el = document.elementFromPoint(x, y);
  if (!el) throw new Error("target element not found");
  el.scrollIntoView({block: "center", inline: "center"});
  const rect = el.getBoundingClientRect();
  el.click();
  const cx = rect.left + rect.width / 2;
  const cy = rect.top + rect.height / 2;
  return JSON.stringify({ok: true, tag: el.tagName.toLowerCase(), text: (el.innerText || el.value || "").trim().slice(0, 160), x: cx, y: cy, cursorX: cx, cursorY: cy, method});
})()`, selector, x, y, methodValue)
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
  const rect = el.getBoundingClientRect();
  const cx = rect.left + rect.width / 2;
  const cy = rect.top + Math.min(rect.height / 2, 18);
  return JSON.stringify({ok: true, tag: el.tagName.toLowerCase(), textLength: text.length, cursorX: cx, cursorY: cy});
})()`, jsString(in.Selector), jsString(in.Text), in.Clear)
}

func scrollScript(in ScrollInput) string {
	return fmt.Sprintf(`(() => {
  const selector = %s;
  const dx = %s;
  const dy = %s;
  const target = selector ? document.querySelector(selector) : window;
  if (!target) throw new Error("scroll target not found");
  let cursorX = window.innerWidth / 2;
  let cursorY = window.innerHeight / 2;
  if (target !== window) {
    const rect = target.getBoundingClientRect();
    cursorX = rect.left + rect.width / 2;
    cursorY = rect.top + rect.height / 2;
  }
  if (target === window) window.scrollBy(dx, dy);
  else target.scrollBy(dx, dy);
  return JSON.stringify({ok: true, x: window.scrollX, y: window.scrollY, cursorX, cursorY});
})()`, jsString(in.Selector), strconv.FormatFloat(in.DeltaX, 'f', -1, 64), strconv.FormatFloat(in.DeltaY, 'f', -1, 64))
}

func jsString(value string) string {
	b, err := json.Marshal(value)
	if err != nil {
		return `""`
	}
	return string(bytes.TrimSpace(b))
}

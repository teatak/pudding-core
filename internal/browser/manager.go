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
	maxScreenshotDimension  = 16384
	maxScreenshotPixels     = 32 * 1024 * 1024
	maxScreenshotBytes      = 64 * 1024 * 1024
	cdpCommandTimeout       = 8 * time.Second
	globalProcessKey        = "global"
	maxTabsPerSession       = 8
	maxTabsTotal            = 16
)

var (
	ErrUnavailable = errors.New("browser unavailable")
	ErrTabNotFound = errors.New("browser tab not found")
	ErrTabRequired = errors.New("browser tab id required")
	ErrTabLimit    = errors.New("browser tab limit reached")
)

type Service interface {
	ProcessMode(ctx context.Context, sessionID string) string
	CreateTab(ctx context.Context, sessionID string) (TabSnapshot, error)
	OpenNewTab(ctx context.Context, sessionID, rawURL string) (TabSnapshot, error)
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

// FileAccessRevoker removes cached project-directory grants from live browser
// tabs. Tabs that are still displaying file:// content are closed.
type FileAccessRevoker interface {
	RevokeFileAccess(ctx context.Context, sessionID string) ([]string, error)
}

type Config struct {
	HomeDir           string
	ChromePath        string
	Headless          bool
	FileURLAuthorizer FileURLAuthorizer
}

type Manager struct {
	mu          sync.Mutex
	lifecycleMu sync.Mutex
	cfg         Config
	client      *http.Client
	processes   map[string]*browserProcess
	tabs        map[string]*tabBinding
	sessions    map[string]map[string]bool
	fileURLs    FileURLAuthorizer
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
	CreatedAt  time.Time
	FileRoot   string
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
	OK bool    `json:"ok"`
	X  float64 `json:"x"`
	Y  float64 `json:"y"`
}

type scrollTarget struct {
	OK     bool    `json:"ok"`
	X      float64 `json:"x"`
	Y      float64 `json:"y"`
	StartX float64 `json:"startX"`
	StartY float64 `json:"startY"`
}

type typeExpectation struct {
	ExpectedValueLength int    `json:"expectedValueLength"`
	ExpectedValueHash   string `json:"expectedValueHash"`
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
	commandMu  *sync.Mutex
	id         string
	sessionID  string
	targetID   string
	url        string
	title      string
	faviconURL string
	createdAt  time.Time
	updatedAt  time.Time
	fileRoots  []string
}

type browserProcess struct {
	cmd        *exec.Cmd
	endpoint   string
	chromePath string
	profileDir string
	port       int
	headless   bool
	cdpMu      sync.Mutex
	cdp        map[string]*cdpSession
}

type cdpSession struct {
	mu       sync.Mutex
	conn     *websocket.Conn
	nextID   int
	targetID string
	wsURL    string
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
		fileURLs:  cfg.FileURLAuthorizer,
	}
}

func (m *Manager) CreateTab(ctx context.Context, sessionID string) (TabSnapshot, error) {
	_, binding, target, err := m.createTab(ctx, sessionID, AuthorizedURL{URL: "about:blank"})
	if err != nil {
		return TabSnapshot{}, err
	}
	return m.snapshotFromLiveTarget(ctx, binding, target), nil
}

func (m *Manager) OpenNewTab(ctx context.Context, sessionID, rawURL string) (TabSnapshot, error) {
	authorizedURL, err := m.normalizeURL(ctx, sessionID, rawURL)
	if err != nil {
		return TabSnapshot{}, err
	}
	proc, binding, target, err := m.createTab(ctx, sessionID, authorizedURL)
	if err != nil {
		return TabSnapshot{}, err
	}
	succeeded := false
	defer func() {
		if !succeeded {
			_ = m.ReleaseTab(context.Background(), sessionID, binding.id)
		}
	}()
	if current, targetErr := proc.target(ctx, m.client, binding.targetID); targetErr == nil {
		target = current
	} else {
		return TabSnapshot{}, targetErr
	}
	m.touch(binding.id)
	succeeded = true
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

func (m *Manager) createTab(ctx context.Context, sessionID string, authorizedURL AuthorizedURL) (*browserProcess, *tabBinding, targetInfo, error) {
	sessionID = strings.TrimSpace(sessionID)
	if sessionID == "" {
		return nil, nil, targetInfo{}, errors.New("session id is required")
	}
	rawURL := strings.TrimSpace(authorizedURL.URL)
	if rawURL == "" {
		rawURL = "about:blank"
	}
	if !m.canCreateTab(sessionID) {
		return nil, nil, targetInfo{}, ErrTabLimit
	}
	proc, err := m.ensureProcess(ctx, sessionID)
	if err != nil {
		return nil, nil, targetInfo{}, err
	}
	target, err := proc.newTarget(ctx, m.client, "about:blank")
	if err != nil {
		return nil, nil, targetInfo{}, err
	}
	if !sameRecoverURL(rawURL, "about:blank") {
		if err := proc.navigateAndWait(ctx, m.client, target.ID, "Page.navigate", map[string]any{"url": rawURL}); err != nil {
			_ = proc.closeTarget(context.Background(), m.client, target.ID)
			return nil, nil, targetInfo{}, err
		}
		if current, currentErr := proc.target(ctx, m.client, target.ID); currentErr == nil {
			target = current
		}
	}
	now := time.Now().UTC()
	binding := &tabBinding{
		commandMu: &sync.Mutex{},
		id:        newID("tab"),
		sessionID: sessionID,
		targetID:  target.ID,
		createdAt: now,
		updatedAt: now,
		fileRoots: appendFileRoot(nil, authorizedURL.FileRoot),
	}
	m.mu.Lock()
	if !m.canCreateTabLocked(sessionID) {
		m.mu.Unlock()
		_ = proc.closeTarget(context.Background(), m.client, target.ID)
		return nil, nil, targetInfo{}, ErrTabLimit
	}
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
		binding.commandMu.Lock()
		if !m.bindingIsCurrent(binding) {
			binding.commandMu.Unlock()
			continue
		}
		if err := m.validateBindingPage(ctx, proc, binding); err != nil {
			binding.commandMu.Unlock()
			continue
		}
		target, err := proc.target(ctx, m.client, binding.targetID)
		if err != nil {
			binding.commandMu.Unlock()
			continue
		}
		out = append(out, m.snapshotFromLiveTarget(ctx, binding, target))
		binding.commandMu.Unlock()
	}
	return out, nil
}

func (m *Manager) GetTab(ctx context.Context, sessionID, tabID string) (TabSnapshot, error) {
	proc, binding, target, err := m.liveTarget(ctx, sessionID, tabID, false)
	if err != nil {
		return TabSnapshot{}, err
	}
	binding.commandMu.Lock()
	defer binding.commandMu.Unlock()
	if !m.bindingIsCurrent(binding) {
		return TabSnapshot{}, ErrTabNotFound
	}
	if err := m.validateBindingPage(ctx, proc, binding); err != nil {
		return TabSnapshot{}, err
	}
	return m.snapshotFromLiveTarget(ctx, binding, target), nil
}

func (m *Manager) Recover(ctx context.Context, sessionID string, hint RecoverHint) (TabSnapshot, error) {
	sessionID = strings.TrimSpace(sessionID)
	if sessionID == "" {
		return TabSnapshot{}, errors.New("session id is required")
	}
	if strings.TrimSpace(hint.URL) != "" {
		authorizedURL, err := m.normalizeURL(ctx, sessionID, hint.URL)
		if err != nil {
			return TabSnapshot{}, err
		}
		hint.URL = authorizedURL.URL
		hint.FileRoot = authorizedURL.FileRoot
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
	for index := range targets {
		metadata, metadataErr := proc.pageMetadata(ctx, m.client, targets[index].ID)
		if metadataErr != nil {
			targets[index].URL = ""
			targets[index].Title = ""
			continue
		}
		targets[index].URL = metadata.URL
		targets[index].Title = metadata.Title
		targets[index].FaviconURL = metadata.FaviconURL
	}
	target, ok := recoverTarget(targets, hint)
	if !ok {
		return TabSnapshot{}, ErrTabNotFound
	}
	now := time.Now().UTC()
	createdAt := hint.CreatedAt
	if createdAt.IsZero() {
		createdAt = now
	}
	tabID := strings.TrimSpace(hint.TabID)
	if tabID == "" {
		tabID = newID("tab")
	}
	m.mu.Lock()
	existing := m.tabs[tabID]
	canCreate := existing != nil || m.canCreateTabLocked(sessionID)
	m.mu.Unlock()
	if !canCreate {
		return TabSnapshot{}, ErrTabLimit
	}
	if existing != nil {
		existing.commandMu.Lock()
		defer existing.commandMu.Unlock()
		if !m.bindingIsCurrent(existing) {
			return TabSnapshot{}, ErrTabNotFound
		}
	}
	binding := &tabBinding{
		commandMu:  &sync.Mutex{},
		id:         tabID,
		sessionID:  sessionID,
		targetID:   target.ID,
		url:        target.URL,
		title:      target.Title,
		faviconURL: target.FaviconURL,
		createdAt:  createdAt,
		updatedAt:  now,
		fileRoots:  appendFileRoot(nil, hint.FileRoot),
	}
	m.mu.Lock()
	if existing == nil && !m.canCreateTabLocked(sessionID) {
		m.mu.Unlock()
		return TabSnapshot{}, ErrTabLimit
	}
	if existing != nil {
		binding.createdAt = existing.createdAt
		binding.commandMu = existing.commandMu
		for _, root := range existing.fileRoots {
			binding.fileRoots = appendFileRoot(binding.fileRoots, root)
		}
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
	binding.commandMu.Lock()
	defer binding.commandMu.Unlock()
	if !m.bindingIsCurrent(binding) {
		return TabSnapshot{}, ErrTabNotFound
	}
	if binding.targetID != "" {
		_ = proc.closeTarget(ctx, m.client, binding.targetID)
	}
	rawURL := firstNonBlank(hint.URL, binding.url)
	fileRoot := hint.FileRoot
	if strings.TrimSpace(hint.URL) == "" {
		if len(binding.fileRoots) > 0 {
			fileRoot = binding.fileRoots[len(binding.fileRoots)-1]
		}
	}
	if rawURL == "" {
		rawURL = "about:blank"
	}
	target, err := proc.newTarget(ctx, m.client, "about:blank")
	if err != nil {
		return TabSnapshot{}, err
	}
	if !sameRecoverURL(rawURL, "about:blank") {
		if err := proc.navigateAndWait(ctx, m.client, target.ID, "Page.navigate", map[string]any{"url": rawURL}); err != nil {
			_ = proc.closeTarget(context.Background(), m.client, target.ID)
			return TabSnapshot{}, err
		}
	}
	if current, err := proc.target(ctx, m.client, target.ID); err == nil {
		target = current
	}
	m.rememberBindingTarget(binding.id, target)
	binding.fileRoots = appendFileRoot(binding.fileRoots, fileRoot)
	m.rememberBindingFileRoot(binding.id, fileRoot)
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
	m.touch(binding.id)
	return m.snapshotFromLiveTarget(ctx, binding, target), nil
}

func (m *Manager) ReleaseTab(ctx context.Context, sessionID, tabID string) error {
	binding, err := m.binding(sessionID, tabID)
	if err != nil {
		return err
	}
	binding.commandMu.Lock()
	defer binding.commandMu.Unlock()
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
			binding.commandMu.Lock()
			_ = proc.closeTarget(ctx, m.client, binding.targetID)
			binding.commandMu.Unlock()
		}
	}
	return nil
}

func (m *Manager) RevokeFileAccess(ctx context.Context, sessionID string) ([]string, error) {
	sessionID = strings.TrimSpace(sessionID)
	if sessionID == "" {
		return nil, errors.New("session id is required")
	}
	bindings := m.sessionBindings(sessionID)
	proc := m.currentProcess(ctx, sessionID)
	closed := make([]string, 0)
	var closeErr error
	for _, binding := range bindings {
		binding.commandMu.Lock()
		if !m.bindingIsCurrent(binding) {
			binding.commandMu.Unlock()
			continue
		}
		currentURL := binding.url
		metadataFailed := false
		if proc != nil && strings.TrimSpace(binding.targetID) != "" {
			metadata, err := proc.pageMetadata(ctx, m.client, binding.targetID)
			if err != nil {
				metadataFailed = true
			} else {
				currentURL = metadata.URL
			}
		}
		closeTab := strings.EqualFold(urlScheme(currentURL), "file") || (metadataFailed && len(binding.fileRoots) > 0)

		m.mu.Lock()
		current := m.tabs[binding.id]
		if current != nil && current.sessionID == sessionID && current.commandMu == binding.commandMu {
			current.fileRoots = nil
			if closeTab {
				delete(m.tabs, binding.id)
				delete(m.sessions[sessionID], binding.id)
				if len(m.sessions[sessionID]) == 0 {
					delete(m.sessions, sessionID)
				}
			}
		}
		m.mu.Unlock()
		if closeTab {
			closed = append(closed, binding.id)
			if proc != nil && strings.TrimSpace(binding.targetID) != "" {
				if err := proc.closeTarget(ctx, m.client, binding.targetID); err != nil && !errors.Is(err, ErrTabNotFound) && closeErr == nil {
					closeErr = err
				}
			}
		}
		binding.commandMu.Unlock()
	}
	return closed, closeErr
}

func (m *Manager) Open(ctx context.Context, sessionID, tabID, rawURL string) (TabSnapshot, error) {
	sessionID = strings.TrimSpace(sessionID)
	tabID = strings.TrimSpace(tabID)
	authorizedURL, err := m.normalizeURL(ctx, sessionID, rawURL)
	if err != nil {
		return TabSnapshot{}, err
	}
	proc, binding, _, err := m.liveTarget(ctx, sessionID, tabID, false)
	if err != nil {
		if errors.Is(err, ErrTabRequired) && tabID == "" && len(m.sessionBindings(sessionID)) == 0 {
			proc, binding, target, err := m.createTab(ctx, sessionID, authorizedURL)
			if err != nil {
				return TabSnapshot{}, err
			}
			if current, err := proc.target(ctx, m.client, binding.targetID); err == nil {
				target = current
			}
			m.touch(binding.id)
			return m.snapshotFromLiveTarget(ctx, binding, target), nil
		}
		return TabSnapshot{}, err
	}
	binding.commandMu.Lock()
	defer binding.commandMu.Unlock()
	if !m.bindingIsCurrent(binding) {
		return TabSnapshot{}, ErrTabNotFound
	}
	if err := proc.navigateAndWait(ctx, m.client, binding.targetID, "Page.navigate", map[string]any{"url": authorizedURL.URL}); err != nil {
		return TabSnapshot{}, err
	}
	binding.fileRoots = appendFileRoot(binding.fileRoots, authorizedURL.FileRoot)
	m.rememberBindingFileRoot(binding.id, authorizedURL.FileRoot)
	target, err := proc.target(ctx, m.client, binding.targetID)
	if err != nil {
		return TabSnapshot{}, err
	}
	m.touch(binding.id)
	return m.snapshotFromLiveTarget(ctx, binding, target), nil
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
	binding.commandMu.Lock()
	defer binding.commandMu.Unlock()
	if !m.bindingIsCurrent(binding) {
		return TabSnapshot{}, ErrTabNotFound
	}
	if err := m.validateBindingPage(ctx, proc, binding); err != nil {
		return TabSnapshot{}, err
	}
	if err := proc.navigateAndWait(ctx, m.client, binding.targetID, "Page.reload", map[string]any{"ignoreCache": false}); err != nil {
		return TabSnapshot{}, err
	}
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
	binding.commandMu.Lock()
	defer binding.commandMu.Unlock()
	if !m.bindingIsCurrent(binding) {
		return ObserveResult{}, ErrTabNotFound
	}
	if err := m.validateBindingPage(ctx, proc, binding); err != nil {
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
	binding.commandMu.Lock()
	defer binding.commandMu.Unlock()
	if !m.bindingIsCurrent(binding) {
		return ScreenshotResult{}, ErrTabNotFound
	}
	if err := m.validateBindingPage(ctx, proc, binding); err != nil {
		return ScreenshotResult{}, err
	}
	metricsRaw, err := proc.cdpCall(ctx, m.client, binding.targetID, "Page.getLayoutMetrics", nil)
	if err != nil {
		return ScreenshotResult{}, err
	}
	var metrics struct {
		CSSVisualViewport struct {
			ClientWidth  float64 `json:"clientWidth"`
			ClientHeight float64 `json:"clientHeight"`
		} `json:"cssVisualViewport"`
		CSSLayoutViewport struct {
			ClientWidth  float64 `json:"clientWidth"`
			ClientHeight float64 `json:"clientHeight"`
		} `json:"cssLayoutViewport"`
		CSSContentSize struct {
			X      float64 `json:"x"`
			Y      float64 `json:"y"`
			Width  float64 `json:"width"`
			Height float64 `json:"height"`
		} `json:"cssContentSize"`
	}
	if err := json.Unmarshal(metricsRaw, &metrics); err != nil {
		return ScreenshotResult{}, err
	}
	viewportWidth := metrics.CSSVisualViewport.ClientWidth
	viewportHeight := metrics.CSSVisualViewport.ClientHeight
	if viewportWidth <= 0 || viewportHeight <= 0 {
		viewportWidth = metrics.CSSLayoutViewport.ClientWidth
		viewportHeight = metrics.CSSLayoutViewport.ClientHeight
	}
	captureParams := map[string]any{
		"format":                "png",
		"fromSurface":           true,
		"captureBeyondViewport": opts.FullPage,
	}
	if opts.FullPage {
		if metrics.CSSContentSize.Width <= 0 || metrics.CSSContentSize.Height <= 0 {
			return ScreenshotResult{}, errors.New("browser screenshot content dimensions unavailable")
		}
		if metrics.CSSContentSize.Width > maxScreenshotDimension ||
			metrics.CSSContentSize.Height > maxScreenshotDimension ||
			metrics.CSSContentSize.Width*metrics.CSSContentSize.Height > maxScreenshotPixels {
			return ScreenshotResult{}, fmt.Errorf(
				"browser full-page screenshot exceeds limit: %.0fx%.0f (max dimension %d, max pixels %d)",
				metrics.CSSContentSize.Width,
				metrics.CSSContentSize.Height,
				maxScreenshotDimension,
				maxScreenshotPixels,
			)
		}
		captureParams["clip"] = map[string]any{
			"x":      metrics.CSSContentSize.X,
			"y":      metrics.CSSContentSize.Y,
			"width":  metrics.CSSContentSize.Width,
			"height": metrics.CSSContentSize.Height,
			"scale":  1,
		}
	}
	raw, err := proc.cdpCall(ctx, m.client, binding.targetID, "Page.captureScreenshot", captureParams)
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
	if base64.StdEncoding.DecodedLen(len(payload.Data)) > maxScreenshotBytes {
		return ScreenshotResult{}, errors.New("browser screenshot exceeds byte limit")
	}
	decoded, err := base64.StdEncoding.DecodeString(payload.Data)
	if err != nil {
		return ScreenshotResult{}, err
	}
	if len(decoded) > maxScreenshotBytes {
		return ScreenshotResult{}, errors.New("browser screenshot exceeds byte limit")
	}
	imageConfig, _ := png.DecodeConfig(bytes.NewReader(decoded))
	if imageConfig.Width <= 0 || imageConfig.Height <= 0 {
		return ScreenshotResult{}, errors.New("browser screenshot returned invalid png")
	}
	if imageConfig.Width > maxScreenshotDimension ||
		imageConfig.Height > maxScreenshotDimension ||
		int64(imageConfig.Width)*int64(imageConfig.Height) > int64(maxScreenshotPixels) {
		return ScreenshotResult{}, fmt.Errorf("browser screenshot dimensions exceed limit: %dx%d", imageConfig.Width, imageConfig.Height)
	}
	deviceScaleFactor := float64(0)
	if imageConfig.Width > 0 {
		cssWidth := viewportWidth
		if opts.FullPage && metrics.CSSContentSize.Width > 0 {
			cssWidth = metrics.CSSContentSize.Width
		}
		if cssWidth > 0 {
			deviceScaleFactor = float64(imageConfig.Width) / cssWidth
		}
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
		ViewportWidth:     int(viewportWidth),
		ViewportHeight:    int(viewportHeight),
		DeviceScaleFactor: deviceScaleFactor,
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
	binding.commandMu.Lock()
	defer binding.commandMu.Unlock()
	if !m.bindingIsCurrent(binding) {
		return ActionResult{}, ErrTabNotFound
	}
	if err := m.validateBindingPage(ctx, proc, binding); err != nil {
		return ActionResult{}, err
	}
	method := strings.ToLower(strings.TrimSpace(in.Method))
	if method == "" {
		method = "auto"
	}
	var raw json.RawMessage
	switch method {
	case "auto", "pointer":
		raw, err = m.pointerClick(ctx, proc, binding, in, "pointer")
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
	binding.commandMu.Lock()
	defer binding.commandMu.Unlock()
	if !m.bindingIsCurrent(binding) {
		return ActionResult{}, ErrTabNotFound
	}
	if err := m.validateBindingPage(ctx, proc, binding); err != nil {
		return ActionResult{}, err
	}
	preparedRaw, err := proc.evaluateJSON(ctx, m.client, binding.targetID, typePrepareScript(in))
	if err != nil {
		return ActionResult{}, err
	}
	var expectation typeExpectation
	if err := json.Unmarshal(preparedRaw, &expectation); err != nil {
		return ActionResult{}, err
	}
	if _, err := proc.evaluateJSON(ctx, m.client, binding.targetID, typeTargetInputScript(in)); err != nil {
		return ActionResult{}, fmt.Errorf("browser target input failed: %w", err)
	}
	raw, err := proc.evaluateJSON(ctx, m.client, binding.targetID, typeResultScript(in, "target", expectation))
	if err != nil {
		return ActionResult{}, err
	}
	var typed struct {
		MatchesExpected bool `json:"matchesExpected"`
	}
	if err := json.Unmarshal(raw, &typed); err != nil {
		return ActionResult{}, err
	}
	if !typed.MatchesExpected {
		return ActionResult{}, errors.New("browser input did not produce the expected value")
	}
	var typedResult map[string]any
	if err := json.Unmarshal(raw, &typedResult); err != nil {
		return ActionResult{}, err
	}
	delete(typedResult, "matchesExpected")
	raw, err = json.Marshal(typedResult)
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
	binding.commandMu.Lock()
	defer binding.commandMu.Unlock()
	if !m.bindingIsCurrent(binding) {
		return ActionResult{}, ErrTabNotFound
	}
	if err := m.validateBindingPage(ctx, proc, binding); err != nil {
		return ActionResult{}, err
	}
	targetRaw, err := proc.evaluateJSON(ctx, m.client, binding.targetID, scrollTargetScript(in))
	if err != nil {
		return ActionResult{}, err
	}
	var target scrollTarget
	if err := json.Unmarshal(targetRaw, &target); err != nil {
		return ActionResult{}, err
	}
	if !target.OK {
		return ActionResult{}, errors.New("scroll target not found")
	}
	raw, err := proc.waitForScrollResult(ctx, m.client, binding.targetID, in, target)
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
	if proc == nil {
		return nil
	}
	closeErr := proc.closeCDPSessions()
	if proc.cmd == nil || proc.cmd.Process == nil {
		return closeErr
	}
	if err := proc.cmd.Process.Signal(os.Interrupt); err == nil {
		done := make(chan struct{})
		go func() {
			_, _ = proc.cmd.Process.Wait()
			close(done)
		}()
		select {
		case <-done:
			return closeErr
		case <-time.After(2 * time.Second):
		}
	}
	if err := proc.cmd.Process.Kill(); err != nil {
		return err
	}
	return closeErr
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

func (m *Manager) rememberBindingFileRoot(tabID, fileRoot string) {
	fileRoot = strings.TrimSpace(fileRoot)
	if fileRoot == "" {
		return
	}
	m.mu.Lock()
	if binding := m.tabs[tabID]; binding != nil {
		binding.fileRoots = appendFileRoot(binding.fileRoots, fileRoot)
	}
	m.mu.Unlock()
}

func appendFileRoot(roots []string, root string) []string {
	root = strings.TrimSpace(root)
	if root == "" {
		return roots
	}
	for _, existing := range roots {
		if existing == root {
			return roots
		}
	}
	return append(append([]string(nil), roots...), root)
}

func (m *Manager) snapshotFromLiveTarget(ctx context.Context, binding *tabBinding, target targetInfo) TabSnapshot {
	proc := m.currentProcess(ctx, binding.sessionID)
	if proc != nil && target.ID != "" {
		if metadata, err := proc.pageMetadata(ctx, m.client, target.ID); err == nil {
			target.URL = metadata.URL
			target.Title = metadata.Title
			target.FaviconURL = metadata.FaviconURL
		} else {
			target.URL = ""
			target.Title = ""
			target.FaviconURL = ""
		}
	}
	snap := m.snapshotFromTarget(binding, target)
	if snap.TargetID == "" || proc == nil {
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
	binding.commandMu.Lock()
	defer binding.commandMu.Unlock()
	if !m.bindingIsCurrent(binding) {
		return TabSnapshot{}, ErrTabNotFound
	}
	if err := m.validateBindingPage(ctx, proc, binding); err != nil {
		return TabSnapshot{}, err
	}
	raw, err := proc.cdpCall(ctx, m.client, binding.targetID, "Page.getNavigationHistory", nil)
	if err != nil {
		return TabSnapshot{}, err
	}
	var history struct {
		CurrentIndex int `json:"currentIndex"`
		Entries      []struct {
			ID  int    `json:"id"`
			URL string `json:"url"`
		} `json:"entries"`
	}
	if err := json.Unmarshal(raw, &history); err != nil {
		return TabSnapshot{}, err
	}
	nextIndex := history.CurrentIndex + delta
	if nextIndex >= 0 && nextIndex < len(history.Entries) {
		if err := validatePageURL(history.Entries[nextIndex].URL, binding.fileRoots); err != nil {
			return TabSnapshot{}, err
		}
		before, err := proc.readNavigationState(ctx, m.client, binding.targetID)
		if err != nil {
			return TabSnapshot{}, err
		}
		navigationCtx, cancel := context.WithTimeout(ctx, 15*time.Second)
		defer cancel()
		entryID := history.Entries[nextIndex].ID
		if _, err := proc.cdpCall(navigationCtx, m.client, binding.targetID, "Page.navigateToHistoryEntry", map[string]any{
			"entryId": entryID,
		}); err != nil {
			return TabSnapshot{}, err
		}
		if err := proc.waitForHistoryEntry(navigationCtx, m.client, binding.targetID, entryID); err != nil {
			return TabSnapshot{}, err
		}
		if err := proc.waitForNavigationTransition(navigationCtx, m.client, binding.targetID, before); err != nil {
			return TabSnapshot{}, err
		}
		if err := m.validateBindingPage(navigationCtx, proc, binding); err != nil {
			return TabSnapshot{}, err
		}
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

func (m *Manager) bindingIsCurrent(binding *tabBinding) bool {
	if binding == nil {
		return false
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	current := m.tabs[binding.id]
	return current != nil &&
		current.sessionID == binding.sessionID &&
		current.targetID == binding.targetID &&
		current.commandMu == binding.commandMu
}

func (m *Manager) validateBindingPage(ctx context.Context, proc *browserProcess, binding *tabBinding) error {
	metadata, err := proc.pageMetadata(ctx, m.client, binding.targetID)
	if err != nil {
		return err
	}
	return validatePageURL(metadata.URL, binding.fileRoots)
}

func validatePageURL(rawURL string, roots []string) error {
	if fileURLAllowed(rawURL, roots) {
		return nil
	}
	if strings.EqualFold(urlScheme(rawURL), "file") {
		return ErrFileURLNotAllowed
	}
	return errors.New("browser page URL is not allowed")
}

func urlScheme(rawURL string) string {
	parsed, err := url.Parse(strings.TrimSpace(rawURL))
	if err != nil {
		return ""
	}
	return parsed.Scheme
}

func (m *Manager) canCreateTab(sessionID string) bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.canCreateTabLocked(sessionID)
}

func (m *Manager) canCreateTabLocked(sessionID string) bool {
	return len(m.tabs) < maxTabsTotal && len(m.sessions[sessionID]) < maxTabsPerSession
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
	p.closeCDPSession(targetID)
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

type navigationState struct {
	URL        string  `json:"url"`
	ReadyState string  `json:"readyState"`
	TimeOrigin float64 `json:"timeOrigin"`
}

type pageMetadata struct {
	URL        string `json:"url"`
	Title      string `json:"title"`
	FaviconURL string `json:"faviconURL"`
}

func (p *browserProcess) pageMetadata(ctx context.Context, client *http.Client, targetID string) (pageMetadata, error) {
	raw, err := p.evaluateJSON(ctx, client, targetID, `(() => JSON.stringify({
  url: String(location.href || ""),
  title: String(document.title || ""),
  faviconURL: String(document.querySelector('link[rel~="icon"]')?.href || "")
}))()`)
	if err != nil {
		return pageMetadata{}, err
	}
	var metadata pageMetadata
	if err := json.Unmarshal(raw, &metadata); err != nil {
		return pageMetadata{}, err
	}
	return metadata, nil
}

func (p *browserProcess) navigateAndWait(
	ctx context.Context,
	client *http.Client,
	targetID string,
	method string,
	params any,
) error {
	before, err := p.readNavigationState(ctx, client, targetID)
	if err != nil {
		return err
	}
	raw, err := p.cdpCall(ctx, client, targetID, method, params)
	if err != nil {
		return err
	}
	if method == "Page.navigate" {
		if err := pageNavigateError(raw); err != nil {
			return err
		}
	}
	return p.waitForNavigationTransition(ctx, client, targetID, before)
}

func (p *browserProcess) readNavigationState(ctx context.Context, client *http.Client, targetID string) (navigationState, error) {
	raw, err := p.evaluateJSON(ctx, client, targetID, `(() => JSON.stringify({
  url: String(location.href || ""),
  readyState: String(document.readyState || ""),
  timeOrigin: Number(performance.timeOrigin) || 0
}))()`)
	if err != nil {
		return navigationState{}, err
	}
	var state navigationState
	if err := json.Unmarshal(raw, &state); err != nil {
		return navigationState{}, err
	}
	return state, nil
}

func (p *browserProcess) waitForNavigationTransition(
	ctx context.Context,
	client *http.Client,
	targetID string,
	before navigationState,
) error {
	waitCtx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	ticker := time.NewTicker(50 * time.Millisecond)
	defer ticker.Stop()
	for {
		current, err := p.readNavigationState(waitCtx, client, targetID)
		if err == nil {
			changed := current.URL != before.URL || current.TimeOrigin != before.TimeOrigin
			ready := current.ReadyState == "interactive" || current.ReadyState == "complete"
			if changed && ready {
				return nil
			}
		}
		select {
		case <-waitCtx.Done():
			return fmt.Errorf("browser navigation timed out: %w", waitCtx.Err())
		case <-ticker.C:
		}
	}
}

func (p *browserProcess) waitForHistoryEntry(
	ctx context.Context,
	client *http.Client,
	targetID string,
	entryID int,
) error {
	waitCtx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	ticker := time.NewTicker(50 * time.Millisecond)
	defer ticker.Stop()
	for {
		raw, err := p.cdpCall(waitCtx, client, targetID, "Page.getNavigationHistory", nil)
		if err == nil {
			var history struct {
				CurrentIndex int `json:"currentIndex"`
				Entries      []struct {
					ID int `json:"id"`
				} `json:"entries"`
			}
			if json.Unmarshal(raw, &history) == nil &&
				history.CurrentIndex >= 0 && history.CurrentIndex < len(history.Entries) &&
				history.Entries[history.CurrentIndex].ID == entryID {
				return nil
			}
		}
		select {
		case <-waitCtx.Done():
			return fmt.Errorf("browser history navigation timed out: %w", waitCtx.Err())
		case <-ticker.C:
		}
	}
}

func pageNavigateError(raw json.RawMessage) error {
	var result struct {
		ErrorText  string `json:"errorText"`
		IsDownload bool   `json:"isDownload"`
	}
	if err := json.Unmarshal(raw, &result); err != nil {
		return err
	}
	if strings.TrimSpace(result.ErrorText) != "" {
		return fmt.Errorf("browser navigation failed: %s", result.ErrorText)
	}
	if result.IsDownload {
		return errors.New("browser navigation did not commit because it became a download")
	}
	return nil
}

func (p *browserProcess) evaluateJSON(ctx context.Context, client *http.Client, targetID, expression string) (json.RawMessage, error) {
	params := map[string]any{
		"expression":    expression,
		"returnByValue": true,
		"awaitPromise":  true,
	}
	raw, err := p.cdpCall(ctx, client, targetID, "Runtime.evaluate", params)
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

func (p *browserProcess) cdpCall(ctx context.Context, client *http.Client, targetID, method string, params any) (json.RawMessage, error) {
	session, err := p.cdpSession(ctx, client, targetID)
	if err != nil {
		return nil, err
	}
	callCtx, cancel := context.WithTimeout(ctx, cdpCommandTimeout)
	defer cancel()
	return session.call(callCtx, method, params)
}

func (p *browserProcess) cdpSession(ctx context.Context, client *http.Client, targetID string) (*cdpSession, error) {
	p.cdpMu.Lock()
	if session := p.cdp[targetID]; session != nil {
		p.cdpMu.Unlock()
		return session, nil
	}
	p.cdpMu.Unlock()
	target, err := p.target(ctx, client, targetID)
	if err != nil {
		return nil, err
	}
	if strings.TrimSpace(target.WebSocketDebuggerURL) == "" {
		return nil, errors.New("target has no websocket debugger url")
	}
	session := &cdpSession{targetID: targetID, wsURL: target.WebSocketDebuggerURL}
	p.cdpMu.Lock()
	if p.cdp == nil {
		p.cdp = make(map[string]*cdpSession)
	}
	if existing := p.cdp[targetID]; existing != nil {
		p.cdpMu.Unlock()
		return existing, nil
	}
	p.cdp[targetID] = session
	p.cdpMu.Unlock()
	return session, nil
}

func (s *cdpSession) call(ctx context.Context, method string, params any) (json.RawMessage, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.conn == nil {
		conn, _, err := websocket.Dial(ctx, s.wsURL, nil)
		if err != nil {
			return nil, err
		}
		conn.SetReadLimit(32 << 20)
		s.conn = conn
	}
	s.nextID++
	requestID := s.nextID
	req := map[string]any{"id": requestID, "method": method}
	if params != nil {
		req["params"] = params
	}
	data, err := json.Marshal(req)
	if err != nil {
		return nil, err
	}
	if err := s.conn.Write(ctx, websocket.MessageText, data); err != nil {
		s.resetLocked()
		return nil, err
	}
	for {
		_, msg, err := s.conn.Read(ctx)
		if err != nil {
			s.resetLocked()
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
			s.resetLocked()
			return nil, err
		}
		if resp.ID != requestID {
			continue
		}
		if resp.Error != nil {
			return nil, fmt.Errorf("cdp %s: %s", method, resp.Error.Message)
		}
		return resp.Result, nil
	}
}

func (s *cdpSession) resetLocked() {
	if s.conn != nil {
		_ = s.conn.CloseNow()
		s.conn = nil
	}
}

func (s *cdpSession) close() {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.conn != nil {
		_ = s.conn.Close(websocket.StatusNormalClosure, "")
		s.conn = nil
	}
}

func (p *browserProcess) closeCDPSession(targetID string) {
	p.cdpMu.Lock()
	session := p.cdp[targetID]
	delete(p.cdp, targetID)
	p.cdpMu.Unlock()
	if session != nil {
		session.close()
	}
}

func (p *browserProcess) closeCDPSessions() error {
	p.cdpMu.Lock()
	sessions := make([]*cdpSession, 0, len(p.cdp))
	for targetID, session := range p.cdp {
		sessions = append(sessions, session)
		delete(p.cdp, targetID)
	}
	p.cdpMu.Unlock()
	for _, session := range sessions {
		session.close()
	}
	return nil
}

func (p *browserProcess) waitForScrollResult(ctx context.Context, client *http.Client, targetID string, in ScrollInput, target scrollTarget) (json.RawMessage, error) {
	var latest json.RawMessage
	for attempt := 0; attempt < 10; attempt++ {
		raw, err := p.evaluateJSON(ctx, client, targetID, scrollResultScript(in, target))
		if err != nil {
			return nil, err
		}
		latest = raw
		var result struct {
			TargetX float64 `json:"targetX"`
			TargetY float64 `json:"targetY"`
		}
		if json.Unmarshal(raw, &result) == nil {
			movedX := in.DeltaX != 0 && result.TargetX != target.StartX
			movedY := in.DeltaY != 0 && result.TargetY != target.StartY
			if movedX || movedY || (in.DeltaX == 0 && in.DeltaY == 0) {
				return raw, nil
			}
		}
		if attempt == 9 {
			break
		}
		timer := time.NewTimer(20 * time.Millisecond)
		select {
		case <-ctx.Done():
			timer.Stop()
			return nil, ctx.Err()
		case <-timer.C:
		}
	}
	return latest, nil
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
	authorized, err := normalizeAuthorizedURL(context.Background(), "", raw, nil)
	return authorized.URL, err
}

func normalizeAuthorizedURL(ctx context.Context, sessionID, raw string, fileURLs FileURLAuthorizer) (AuthorizedURL, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return AuthorizedURL{}, errors.New("url is required")
	}
	if raw == "about:blank" {
		return AuthorizedURL{URL: raw}, nil
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
		return AuthorizedURL{}, err
	}
	switch u.Scheme {
	case "http", "https":
		if u.Host == "" {
			return AuthorizedURL{}, errors.New("url host is required")
		}
		return AuthorizedURL{URL: u.String()}, nil
	case "file":
		if fileURLs == nil || u.User != nil {
			return AuthorizedURL{}, ErrFileURLNotAllowed
		}
		return fileURLs(ctx, sessionID, u)
	default:
		return AuthorizedURL{}, errors.New("only http, https and authorized file URLs are supported")
	}
}

func (m *Manager) normalizeURL(ctx context.Context, sessionID, raw string) (AuthorizedURL, error) {
	return normalizeAuthorizedURL(ctx, sessionID, raw, m.fileURLs)
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
  const isPasswordInput = (el) => el instanceof HTMLInputElement && String(el.type || "").toLowerCase() === "password";
  const pickText = (el) => ((el.innerText || (isPasswordInput(el) ? "" : el.value) || el.getAttribute("aria-label") || el.textContent || "").trim().replace(/\s+/g, " "));
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
	  const isPasswordInput = (node) => node instanceof HTMLInputElement && String(node.type || "").toLowerCase() === "password";
	  const elementText = (node) => String(node.innerText || node.textContent || (isPasswordInput(node) ? "" : node.value) || node.getAttribute?.("aria-label") || "").trim();
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
	  if (el.disabled || el.getAttribute?.("aria-disabled") === "true") throw new Error("target element is not interactable");
	  if (selector) el.scrollIntoView({behavior: "instant", block: "center", inline: "center"});
	  const rect = el.getBoundingClientRect();
	  const visibleLeft = Math.max(0, rect.left);
	  const visibleTop = Math.max(0, rect.top);
	  const visibleRight = Math.min(window.innerWidth, rect.right);
	  const visibleBottom = Math.min(window.innerHeight, rect.bottom);
	  if (visibleRight <= visibleLeft || visibleBottom <= visibleTop) throw new Error("target element is not visible");
	  const cx = selector ? visibleLeft + (visibleRight - visibleLeft) / 2 : x;
	  const cy = selector ? visibleTop + (visibleBottom - visibleTop) / 2 : y;
	  if (!Number.isFinite(cx) || !Number.isFinite(cy)) throw new Error("target coordinates not found");
	  const hit = document.elementFromPoint(cx, cy);
	  if (!hit || (hit !== el && !el.contains(hit))) throw new Error("target element is not hittable");
	  const editableTarget = el.closest?.('input,textarea,[contenteditable]:not([contenteditable="false"])') || el;
	  globalThis[Symbol.for("pudding.browser.lastClickTarget")] = editableTarget;
	  return JSON.stringify({ok: true, tag: el.tagName.toLowerCase(), text: elementText(el).slice(0, 160), x: cx, y: cy, cursorX: cx, cursorY: cy, method});
	})()`, selector, x, y, methodValue)
}

func typePrepareScript(in TypeInput) string {
	return fmt.Sprintf(`(() => {
		  const selector = %s;
		  const text = %s;
		  const clear = %t;
		  const textInputTypes = new Set(["text", "search", "email", "tel", "url", "password", "number", "date", "datetime-local", "month", "time", "week"]);
		  const isTextInput = (node) => node instanceof HTMLTextAreaElement ||
		    (node instanceof HTMLInputElement && textInputTypes.has(String(node.type || "text").toLowerCase()));
		  const isEditable = (node) => isTextInput(node) || Boolean(node?.isContentEditable);
		  const editableText = (node) => String(node?.textContent || "").replace(/\uFEFF/g, "");
		  const fingerprint = (value) => {
		    let hash = 2166136261;
		    for (let index = 0; index < value.length; index += 1) {
		      hash ^= value.charCodeAt(index);
		      hash = Math.imul(hash, 16777619);
		    }
		    return (hash >>> 0).toString(16).padStart(8, "0");
		  };
		  const lastClickTargetKey = Symbol.for("pudding.browser.lastClickTarget");
		  const lastClickTarget = globalThis[lastClickTargetKey];
		  if (!selector) globalThis[lastClickTargetKey] = null;
		  let el = selector ? document.querySelector(selector) : (lastClickTarget?.isConnected ? lastClickTarget : document.activeElement);
		  if (!el || el === document.body) throw new Error("target input not found");
		  if (!isEditable(el)) throw new Error("target is not editable");
		  if (el.disabled || el.readOnly || el.getAttribute("aria-disabled") === "true" || el.getAttribute("aria-readonly") === "true") {
		    throw new Error("target is not editable");
		  }
		  el.scrollIntoView({behavior: "instant", block: "center", inline: "center"});
		  el.focus();
		  if (document.activeElement !== el) throw new Error("target input could not be focused");
		  const originalValue = isTextInput(el) ? String(el.value || "") : editableText(el);
		  const expectedValue = clear ? text : originalValue + text;
	  if (isTextInput(el)) {
	    try {
	      if (typeof el.setSelectionRange === "function") {
	        const end = String(el.value || "").length;
	        el.setSelectionRange(clear ? 0 : end, end);
	      }
	    } catch (_) {}
	  } else {
	    const range = document.createRange();
	    range.selectNodeContents(el);
	    if (!clear) range.collapse(false);
	    const selection = window.getSelection();
	    selection.removeAllRanges();
	    selection.addRange(range);
	  }
	  const rect = el.getBoundingClientRect();
	  if (rect.width <= 0 || rect.height <= 0 || rect.right <= 0 || rect.bottom <= 0 || rect.left >= window.innerWidth || rect.top >= window.innerHeight) {
	    throw new Error("target input is not visible");
	  }
	  const cx = rect.left + rect.width / 2;
	  const cy = rect.top + Math.min(rect.height / 2, 18);
		  return JSON.stringify({ok: true, tag: el.tagName.toLowerCase(), cursorX: cx, cursorY: cy, expectedValueLength: expectedValue.length, expectedValueHash: fingerprint(expectedValue)});
		})()`, jsString(in.Selector), jsString(in.Text), in.Clear)
}

func typeTargetInputScript(in TypeInput) string {
	return fmt.Sprintf(`(() => {
		  const selector = %s;
		  const text = %s;
		  const clear = %t;
		  const textInputTypes = new Set(["text", "search", "email", "tel", "url", "password", "number", "date", "datetime-local", "month", "time", "week"]);
		  const isTextInput = (node) => node instanceof HTMLTextAreaElement ||
		    (node instanceof HTMLInputElement && textInputTypes.has(String(node.type || "text").toLowerCase()));
		  const isEditable = (node) => isTextInput(node) || Boolean(node?.isContentEditable);
		  const el = selector ? document.querySelector(selector) : document.activeElement;
		  if (!el || el === document.body) throw new Error("target input not found");
		  if (!isEditable(el)) throw new Error("target is not editable");
		  if (document.activeElement !== el) el.focus();
		  if (document.activeElement !== el) throw new Error("target input could not be focused");
		  const inputEvent = (type, cancelable) => {
		    let event;
		    try {
		      event = new InputEvent(type, {bubbles: true, cancelable, composed: true, inputType: "insertText", data: text});
		    } catch (_) {
		      event = new Event(type, {bubbles: true, cancelable, composed: true});
		    }
		    return event;
		  };
		  const accepted = el.dispatchEvent(inputEvent("beforeinput", true));
		  if (!accepted) return JSON.stringify({ok: true, tag: el.tagName.toLowerCase(), canceled: true});
		  const dispatchInput = () => el.dispatchEvent(inputEvent("input", false));
		  if (isTextInput(el)) {
		    const currentValue = String(el.value || "");
		    const nextValue = clear ? text : currentValue + text;
		    let proto = Object.getPrototypeOf(el);
		    let setter = null;
		    while (proto && !setter) {
		      setter = Object.getOwnPropertyDescriptor(proto, "value")?.set || null;
		      proto = Object.getPrototypeOf(proto);
		    }
		    if (!setter) throw new Error("target is not editable: native value setter missing");
		    setter.call(el, nextValue);
		    try {
		      if (typeof el.setSelectionRange === "function") el.setSelectionRange(nextValue.length, nextValue.length);
		    } catch (_) {}
		    dispatchInput();
		  } else if (el.isContentEditable) {
		    let inserted = false;
		    let sawInput = false;
		    const observeInput = () => { sawInput = true; };
		    el.addEventListener("input", observeInput, true);
		    try {
		      inserted = document.execCommand("insertText", false, text);
		    } catch (_) {
		    } finally {
		      el.removeEventListener("input", observeInput, true);
		    }
		    if (inserted && !sawInput) dispatchInput();
		    if (!inserted) {
		      const selection = window.getSelection();
		      const range = selection?.rangeCount ? selection.getRangeAt(0) : document.createRange();
		      if (!selection?.rangeCount) {
		        range.selectNodeContents(el);
		        if (!clear) range.collapse(false);
		      }
		      if (clear) range.selectNodeContents(el);
		      range.deleteContents();
		      const node = document.createTextNode(text);
		      range.insertNode(node);
		      range.setStartAfter(node);
		      range.collapse(true);
		      selection?.removeAllRanges();
		      selection?.addRange(range);
		      dispatchInput();
		    }
		  } else {
		    throw new Error("target is not editable");
		  }
		  return JSON.stringify({ok: true, tag: el.tagName.toLowerCase()});
		})()`, jsString(in.Selector), jsString(in.Text), in.Clear)
}

func typeResultScript(in TypeInput, method string, expectation typeExpectation) string {
	return fmt.Sprintf(`(() => {
		  const selector = %s;
		  const expectedValueLength = %d;
		  const expectedValueHash = %s;
		  const textInputTypes = new Set(["text", "search", "email", "tel", "url", "password", "number", "date", "datetime-local", "month", "time", "week"]);
		  const isTextInput = (node) => node instanceof HTMLTextAreaElement ||
		    (node instanceof HTMLInputElement && textInputTypes.has(String(node.type || "text").toLowerCase()));
		  const isPasswordInput = (node) => node instanceof HTMLInputElement && String(node.type || "").toLowerCase() === "password";
		  const isEditable = (node) => isTextInput(node) || Boolean(node?.isContentEditable);
		  const editableText = (node) => String(node?.textContent || "").replace(/\uFEFF/g, "");
		  const fingerprint = (value) => {
		    let hash = 2166136261;
		    for (let index = 0; index < value.length; index += 1) {
		      hash ^= value.charCodeAt(index);
		      hash = Math.imul(hash, 16777619);
		    }
		    return (hash >>> 0).toString(16).padStart(8, "0");
		  };
		  const el = selector ? document.querySelector(selector) : document.activeElement;
		  if (!el || el === document.body) throw new Error("target input not found after typing");
		  if (!isEditable(el)) throw new Error("target is not editable after typing");
		  const rect = el.getBoundingClientRect();
		  const value = isTextInput(el) ? String(el.value || "") : editableText(el);
		  const matchesExpected = value.length === expectedValueLength && fingerprint(value) === expectedValueHash;
		  const result = {ok: true, tag: el.tagName.toLowerCase(), textLength: %d, matchesExpected, cursorX: rect.left + rect.width / 2, cursorY: rect.top + Math.min(rect.height / 2, 18), method: %s};
		  if (isPasswordInput(el)) result.sensitive = true;
		  else result.valueLength = value.length;
		  return JSON.stringify(result);
		})()`, jsString(in.Selector), expectation.ExpectedValueLength, jsString(expectation.ExpectedValueHash), len([]rune(in.Text)), jsString(method))
}

func scrollTargetScript(in ScrollInput) string {
	return fmt.Sprintf(`(() => {
  const selector = %s;
  const deltaX = %s;
  const deltaY = %s;
  const target = selector ? document.querySelector(selector) : window;
  if (!target) throw new Error("scroll target not found");
  let cursorX = window.innerWidth / 2;
  let cursorY = window.innerHeight / 2;
  if (target !== window) {
    const rect = target.getBoundingClientRect();
	    if (rect.width <= 0 || rect.height <= 0 || rect.right <= 0 || rect.bottom <= 0 || rect.left >= window.innerWidth || rect.top >= window.innerHeight) {
	      throw new Error("scroll target is outside the viewport");
	    }
	    cursorX = Math.max(0, Math.min(window.innerWidth - 1, rect.left + rect.width / 2));
	    cursorY = Math.max(0, Math.min(window.innerHeight - 1, rect.top + rect.height / 2));
	    const hit = document.elementFromPoint(cursorX, cursorY);
	    if (!hit || (hit !== target && !target.contains(hit))) throw new Error("scroll target is not hittable");
  }
	const startX = target === window ? window.scrollX : target.scrollLeft;
	const startY = target === window ? window.scrollY : target.scrollTop;
	target.scrollBy({left: deltaX, top: deltaY, behavior: "instant"});
	return JSON.stringify({ok: true, x: cursorX, y: cursorY, startX, startY});
})()`, jsString(in.Selector), strconv.FormatFloat(in.DeltaX, 'f', -1, 64), strconv.FormatFloat(in.DeltaY, 'f', -1, 64))
}

func scrollResultScript(in ScrollInput, target scrollTarget) string {
	return fmt.Sprintf(`(() => {
  const selector = %s;
  const target = selector ? document.querySelector(selector) : window;
  return JSON.stringify({
    ok: true,
    x: window.scrollX,
    y: window.scrollY,
    targetX: target && target !== window ? target.scrollLeft : window.scrollX,
    targetY: target && target !== window ? target.scrollTop : window.scrollY,
    cursorX: %s,
    cursorY: %s,
    method: "target"
  });
})()`, jsString(in.Selector), strconv.FormatFloat(target.X, 'f', -1, 64), strconv.FormatFloat(target.Y, 'f', -1, 64))
}

func jsString(value string) string {
	b, err := json.Marshal(value)
	if err != nil {
		return `""`
	}
	return string(bytes.TrimSpace(b))
}

package browser

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

type ElectronBridgeConfig struct {
	URL        string
	Token      string
	HTTPClient *http.Client
}

type ElectronBridgeService struct {
	endpoint string
	token    string
	client   *http.Client
}

type electronBridgeRequest struct {
	SessionID string `json:"sessionID"`
	TabID     string `json:"tabID,omitempty"`
	URL       string `json:"url,omitempty"`
}

type electronBridgeObserveRequest struct {
	SessionID    string `json:"sessionID"`
	TabID        string `json:"tabID,omitempty"`
	MaxTextChars int    `json:"maxTextChars,omitempty"`
	MaxElements  int    `json:"maxElements,omitempty"`
}

type electronBridgeScreenshotRequest struct {
	SessionID string `json:"sessionID"`
	TabID     string `json:"tabID,omitempty"`
	FullPage  bool   `json:"fullPage,omitempty"`
}

type electronBridgeClickRequest struct {
	SessionID string   `json:"sessionID"`
	TabID     string   `json:"tabID,omitempty"`
	Selector  string   `json:"selector,omitempty"`
	X         *float64 `json:"x,omitempty"`
	Y         *float64 `json:"y,omitempty"`
	Method    string   `json:"method,omitempty"`
}

type electronBridgeTypeRequest struct {
	SessionID string `json:"sessionID"`
	TabID     string `json:"tabID,omitempty"`
	Selector  string `json:"selector,omitempty"`
	Text      string `json:"text"`
	Clear     bool   `json:"clear,omitempty"`
}

type electronBridgeScrollRequest struct {
	SessionID string  `json:"sessionID"`
	TabID     string  `json:"tabID,omitempty"`
	Selector  string  `json:"selector,omitempty"`
	DeltaX    float64 `json:"deltaX,omitempty"`
	DeltaY    float64 `json:"deltaY,omitempty"`
}

type electronBridgeTabsResponse struct {
	Tabs        []electronBridgeSnapshot `json:"tabs"`
	ProcessMode string                   `json:"processMode,omitempty"`
}

type electronBridgeSnapshot struct {
	SessionID    string `json:"sessionID"`
	TabID        string `json:"tabID"`
	Status       string `json:"status"`
	URL          string `json:"url"`
	Title        string `json:"title"`
	CanGoBack    bool   `json:"canGoBack"`
	CanGoForward bool   `json:"canGoForward"`
	RuntimeID    string `json:"runtimeID"`
	Version      int64  `json:"version"`
}

type electronBridgeError struct {
	Error string `json:"error"`
}

type electronBridgeObserveResponse struct {
	Tab        electronBridgeSnapshot `json:"tab"`
	Title      string                 `json:"title"`
	URL        string                 `json:"url"`
	ReadyState string                 `json:"readyState"`
	Text       string                 `json:"text"`
	TextChars  int                    `json:"textChars"`
	Truncated  bool                   `json:"truncated"`
	Elements   []ObservedElement      `json:"elements"`
}

type electronBridgeScreenshotResponse struct {
	Tab               electronBridgeSnapshot `json:"tab"`
	MIME              string                 `json:"mime"`
	DataBase64        string                 `json:"dataBase64"`
	Size              int64                  `json:"size"`
	Width             int                    `json:"width,omitempty"`
	Height            int                    `json:"height,omitempty"`
	ViewportWidth     int                    `json:"viewportWidth,omitempty"`
	ViewportHeight    int                    `json:"viewportHeight,omitempty"`
	DeviceScaleFactor float64                `json:"deviceScaleFactor,omitempty"`
	CapturedAt        time.Time              `json:"capturedAt"`
}

type electronBridgeActionResponse struct {
	Tab    electronBridgeSnapshot `json:"tab"`
	Action string                 `json:"action"`
	Result map[string]any         `json:"result"`
}

func NewElectronBridgeService(cfg ElectronBridgeConfig) (*ElectronBridgeService, error) {
	endpoint := strings.TrimRight(strings.TrimSpace(cfg.URL), "/")
	token := strings.TrimSpace(cfg.Token)
	if endpoint == "" || token == "" {
		return nil, errors.New("electron browser bridge url and token are required")
	}
	parsed, err := url.Parse(endpoint)
	if err != nil || parsed.Scheme != "http" || parsed.Host == "" {
		return nil, errors.New("invalid electron browser bridge url")
	}
	client := cfg.HTTPClient
	if client == nil {
		client = &http.Client{Timeout: 15 * time.Second}
	}
	return &ElectronBridgeService{
		endpoint: endpoint,
		token:    token,
		client:   client,
	}, nil
}

func (s *ElectronBridgeService) ProcessMode(context.Context, string) string {
	return "headless"
}

func (s *ElectronBridgeService) SupportsMetadataRecovery() bool {
	return true
}

func (s *ElectronBridgeService) CreateTab(ctx context.Context, sessionID string) (TabSnapshot, error) {
	tabID := newID("tab")
	now := time.Now().UTC()
	return TabSnapshot{
		ID:        tabID,
		SessionID: sessionID,
		URL:       "about:blank",
		Mode:      "headless",
		CreatedAt: now,
		UpdatedAt: now,
	}, nil
}

func (s *ElectronBridgeService) ListTabs(ctx context.Context, sessionID string) ([]TabSnapshot, error) {
	var out electronBridgeTabsResponse
	if err := s.post(ctx, "/browser/tabs/list", electronBridgeRequest{SessionID: sessionID}, &out); err != nil {
		return nil, err
	}
	tabs := make([]TabSnapshot, 0, len(out.Tabs))
	for _, snapshot := range out.Tabs {
		if snapshot.Status == "lost" || strings.TrimSpace(snapshot.SessionID) != sessionID {
			continue
		}
		tabs = append(tabs, snapshot.tab())
	}
	return tabs, nil
}

func (s *ElectronBridgeService) GetTab(ctx context.Context, sessionID, tabID string) (TabSnapshot, error) {
	tabID, err := s.resolveTabID(ctx, sessionID, tabID, false)
	if err != nil {
		return TabSnapshot{}, err
	}
	tabs, err := s.ListTabs(ctx, sessionID)
	if err != nil {
		return TabSnapshot{}, err
	}
	for _, tab := range tabs {
		if tab.ID == tabID {
			return tab, nil
		}
	}
	return TabSnapshot{}, ErrTabNotFound
}

func (s *ElectronBridgeService) Recover(ctx context.Context, sessionID string, hint RecoverHint) (TabSnapshot, error) {
	tabID := strings.TrimSpace(hint.TabID)
	if tabID == "" {
		tabID = newID("tab")
	}
	rawURL := strings.TrimSpace(hint.URL)
	if rawURL == "" {
		return TabSnapshot{}, ErrTabNotFound
	}
	return s.Open(ctx, sessionID, tabID, rawURL)
}

func (s *ElectronBridgeService) CloseSessionBrowser(ctx context.Context, sessionID string) error {
	return s.post(ctx, "/browser/session/close", electronBridgeRequest{SessionID: sessionID}, nil)
}

func (s *ElectronBridgeService) ReleaseTab(ctx context.Context, sessionID, tabID string) error {
	tabID, err := s.resolveTabID(ctx, sessionID, tabID, false)
	if err != nil {
		return err
	}
	return s.post(ctx, "/browser/tabs/close", electronBridgeRequest{SessionID: sessionID, TabID: tabID}, nil)
}

func (s *ElectronBridgeService) ReleaseSession(ctx context.Context, sessionID string) error {
	return s.post(ctx, "/browser/session/release", electronBridgeRequest{SessionID: sessionID}, nil)
}

func (s *ElectronBridgeService) Open(ctx context.Context, sessionID, tabID, rawURL string) (TabSnapshot, error) {
	tabID, err := s.resolveTabID(ctx, sessionID, tabID, true)
	if err != nil {
		return TabSnapshot{}, err
	}
	normalizedURL, err := normalizeURL(rawURL)
	if err != nil {
		return TabSnapshot{}, err
	}
	var snapshot electronBridgeSnapshot
	if err := s.post(ctx, "/browser/tabs/open", electronBridgeRequest{
		SessionID: sessionID,
		TabID:     tabID,
		URL:       normalizedURL,
	}, &snapshot); err != nil {
		return TabSnapshot{}, err
	}
	return snapshot.tab(), nil
}

func (s *ElectronBridgeService) Back(ctx context.Context, sessionID, tabID string) (TabSnapshot, error) {
	return s.navigation(ctx, "/browser/tabs/back", sessionID, tabID)
}

func (s *ElectronBridgeService) Forward(ctx context.Context, sessionID, tabID string) (TabSnapshot, error) {
	return s.navigation(ctx, "/browser/tabs/forward", sessionID, tabID)
}

func (s *ElectronBridgeService) Reload(ctx context.Context, sessionID, tabID string) (TabSnapshot, error) {
	return s.navigation(ctx, "/browser/tabs/reload", sessionID, tabID)
}

func (s *ElectronBridgeService) Observe(ctx context.Context, sessionID, tabID string, opts ObserveOptions) (ObserveResult, error) {
	tabID, err := s.resolveTabID(ctx, sessionID, tabID, false)
	if err != nil {
		return ObserveResult{}, err
	}
	var out electronBridgeObserveResponse
	if err := s.post(ctx, "/browser/tabs/observe", electronBridgeObserveRequest{
		SessionID:    sessionID,
		TabID:        tabID,
		MaxTextChars: opts.MaxTextChars,
		MaxElements:  opts.MaxElements,
	}, &out); err != nil {
		return ObserveResult{}, err
	}
	return out.result(), nil
}

func (s *ElectronBridgeService) Screenshot(ctx context.Context, sessionID, tabID string, opts ScreenshotOptions) (ScreenshotResult, error) {
	tabID, err := s.resolveTabID(ctx, sessionID, tabID, false)
	if err != nil {
		return ScreenshotResult{}, err
	}
	var out electronBridgeScreenshotResponse
	if err := s.post(ctx, "/browser/tabs/screenshot", electronBridgeScreenshotRequest{
		SessionID: sessionID,
		TabID:     tabID,
		FullPage:  opts.FullPage,
	}, &out); err != nil {
		return ScreenshotResult{}, err
	}
	return out.result(), nil
}

func (s *ElectronBridgeService) Click(ctx context.Context, sessionID, tabID string, in ClickInput) (ActionResult, error) {
	if tabID == "" {
		tabID = in.TabID
	}
	tabID, err := s.resolveTabID(ctx, sessionID, tabID, false)
	if err != nil {
		return ActionResult{}, err
	}
	var out electronBridgeActionResponse
	if err := s.post(ctx, "/browser/tabs/click", electronBridgeClickRequest{
		SessionID: sessionID,
		TabID:     tabID,
		Selector:  in.Selector,
		X:         in.X,
		Y:         in.Y,
		Method:    in.Method,
	}, &out); err != nil {
		return ActionResult{}, err
	}
	return out.result("click"), nil
}

func (s *ElectronBridgeService) Type(ctx context.Context, sessionID, tabID string, in TypeInput) (ActionResult, error) {
	if tabID == "" {
		tabID = in.TabID
	}
	if in.Text == "" {
		return ActionResult{}, errors.New("text is required")
	}
	tabID, err := s.resolveTabID(ctx, sessionID, tabID, false)
	if err != nil {
		return ActionResult{}, err
	}
	var out electronBridgeActionResponse
	if err := s.post(ctx, "/browser/tabs/type", electronBridgeTypeRequest{
		SessionID: sessionID,
		TabID:     tabID,
		Selector:  in.Selector,
		Text:      in.Text,
		Clear:     in.Clear,
	}, &out); err != nil {
		return ActionResult{}, err
	}
	return out.result("type"), nil
}

func (s *ElectronBridgeService) Scroll(ctx context.Context, sessionID, tabID string, in ScrollInput) (ActionResult, error) {
	if tabID == "" {
		tabID = in.TabID
	}
	if in.DeltaX == 0 && in.DeltaY == 0 {
		in.DeltaY = 600
	}
	tabID, err := s.resolveTabID(ctx, sessionID, tabID, false)
	if err != nil {
		return ActionResult{}, err
	}
	var out electronBridgeActionResponse
	if err := s.post(ctx, "/browser/tabs/scroll", electronBridgeScrollRequest{
		SessionID: sessionID,
		TabID:     tabID,
		Selector:  in.Selector,
		DeltaX:    in.DeltaX,
		DeltaY:    in.DeltaY,
	}, &out); err != nil {
		return ActionResult{}, err
	}
	return out.result("scroll"), nil
}

func (s *ElectronBridgeService) Close() error {
	return nil
}

func (s *ElectronBridgeService) navigation(ctx context.Context, path, sessionID, tabID string) (TabSnapshot, error) {
	tabID, err := s.resolveTabID(ctx, sessionID, tabID, false)
	if err != nil {
		return TabSnapshot{}, err
	}
	var snapshot electronBridgeSnapshot
	if err := s.post(ctx, path, electronBridgeRequest{SessionID: sessionID, TabID: tabID}, &snapshot); err != nil {
		return TabSnapshot{}, err
	}
	return snapshot.tab(), nil
}

func (s *ElectronBridgeService) resolveTabID(ctx context.Context, sessionID, tabID string, create bool) (string, error) {
	sessionID = strings.TrimSpace(sessionID)
	tabID = strings.TrimSpace(tabID)
	if sessionID == "" {
		return "", errors.New("session id is required")
	}
	if tabID != "" {
		return tabID, nil
	}
	tabs, err := s.ListTabs(ctx, sessionID)
	if err != nil {
		return "", err
	}
	switch len(tabs) {
	case 0:
		if create {
			return newID("tab"), nil
		}
		return "", ErrTabRequired
	case 1:
		return tabs[0].ID, nil
	default:
		return "", ErrTabRequired
	}
}

func (s *ElectronBridgeService) post(ctx context.Context, path string, in any, out any) error {
	var body io.Reader
	if in != nil {
		raw, err := json.Marshal(in)
		if err != nil {
			return err
		}
		body = bytes.NewReader(raw)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, s.endpoint+path, body)
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+s.token)
	req.Header.Set("Content-Type", "application/json")
	resp, err := s.client.Do(req)
	if err != nil {
		return fmt.Errorf("%w: %v", ErrUnavailable, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return bridgeHTTPError(resp)
	}
	if out == nil {
		io.Copy(io.Discard, resp.Body)
		return nil
	}
	if err := json.NewDecoder(resp.Body).Decode(out); err != nil {
		return fmt.Errorf("decode electron browser bridge response: %w", err)
	}
	return nil
}

func bridgeHTTPError(resp *http.Response) error {
	raw, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
	var payload electronBridgeError
	if err := json.Unmarshal(raw, &payload); err != nil || strings.TrimSpace(payload.Error) == "" {
		payload.Error = strings.TrimSpace(string(raw))
	}
	if payload.Error == "" {
		payload.Error = resp.Status
	}
	switch resp.StatusCode {
	case http.StatusNotFound:
		return fmt.Errorf("%w: %s", ErrTabNotFound, payload.Error)
	case http.StatusBadRequest:
		if strings.Contains(payload.Error, "tab id") {
			return fmt.Errorf("%w: %s", ErrTabRequired, payload.Error)
		}
		return errors.New(payload.Error)
	case http.StatusServiceUnavailable:
		return fmt.Errorf("%w: %s", ErrUnavailable, payload.Error)
	default:
		return errors.New(payload.Error)
	}
}

func (snapshot electronBridgeSnapshot) tab() TabSnapshot {
	now := time.Now().UTC()
	rawURL := strings.TrimSpace(snapshot.URL)
	if rawURL == "" {
		rawURL = "about:blank"
	}
	title := strings.TrimSpace(snapshot.Title)
	if rawURL == "about:blank" {
		title = ""
	} else if title == "" {
		title = rawURL
	}
	return TabSnapshot{
		ID:           strings.TrimSpace(firstNonBlank(snapshot.TabID, "default")),
		SessionID:    strings.TrimSpace(snapshot.SessionID),
		TargetID:     strings.TrimSpace(snapshot.RuntimeID),
		URL:          rawURL,
		Title:        title,
		Mode:         "headless",
		CanGoBack:    snapshot.CanGoBack,
		CanGoForward: snapshot.CanGoForward,
		CreatedAt:    now,
		UpdatedAt:    now,
	}
}

func (out electronBridgeObserveResponse) result() ObserveResult {
	return ObserveResult{
		Tab:        out.Tab.tab(),
		Title:      out.Title,
		URL:        out.URL,
		ReadyState: out.ReadyState,
		Text:       out.Text,
		TextChars:  out.TextChars,
		Truncated:  out.Truncated,
		Elements:   out.Elements,
	}
}

func (out electronBridgeScreenshotResponse) result() ScreenshotResult {
	capturedAt := out.CapturedAt
	if capturedAt.IsZero() {
		capturedAt = time.Now().UTC()
	}
	mime := strings.TrimSpace(out.MIME)
	if mime == "" {
		mime = "image/png"
	}
	return ScreenshotResult{
		Tab:               out.Tab.tab(),
		MIME:              mime,
		DataBase64:        out.DataBase64,
		Size:              out.Size,
		Width:             out.Width,
		Height:            out.Height,
		ViewportWidth:     out.ViewportWidth,
		ViewportHeight:    out.ViewportHeight,
		DeviceScaleFactor: out.DeviceScaleFactor,
		CapturedAt:        capturedAt,
	}
}

func (out electronBridgeActionResponse) result(fallbackAction string) ActionResult {
	action := strings.TrimSpace(out.Action)
	if action == "" {
		action = fallbackAction
	}
	result := out.Result
	if result == nil {
		result = map[string]any{}
	}
	return ActionResult{Tab: out.Tab.tab(), Action: action, Result: result}
}

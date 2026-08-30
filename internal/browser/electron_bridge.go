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
	URL               string
	Token             string
	HTTPClient        *http.Client
	FileURLAuthorizer FileURLAuthorizer
}

type ElectronBridgeService struct {
	endpoint string
	token    string
	client   *http.Client
	fileURLs FileURLAuthorizer
}

type electronBridgeRequest struct {
	SessionID string `json:"sessionID"`
	TabID     string `json:"tabID,omitempty"`
	URL       string `json:"url,omitempty"`
	CreatedAt string `json:"createdAt,omitempty"`
	FileRoot  string `json:"fileRoot,omitempty"`
	Activate  *bool  `json:"activate,omitempty"`
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

type electronBridgeRevokeFileAccessResponse struct {
	ClosedTabIDs []string `json:"closedTabIDs"`
}

type electronBridgeSnapshot struct {
	SessionID    string `json:"sessionID"`
	TabID        string `json:"tabID"`
	Status       string `json:"status"`
	URL          string `json:"url"`
	Title        string `json:"title"`
	FaviconURL   string `json:"faviconURL"`
	CanGoBack    bool   `json:"canGoBack"`
	CanGoForward bool   `json:"canGoForward"`
	RuntimeID    string `json:"runtimeID"`
	Version      int64  `json:"version"`
	CreatedAt    string `json:"createdAt"`
	UpdatedAt    string `json:"updatedAt"`
}

type electronBridgeError struct {
	Error     string `json:"error"`
	Code      string `json:"code"`
	Retryable bool   `json:"retryable"`
}

type bridgeOperationError struct {
	code      string
	message   string
	retryable bool
	cause     error
}

func (e *bridgeOperationError) Error() string { return e.message }
func (e *bridgeOperationError) Unwrap() error { return e.cause }

// ErrorCode returns a stable browser operation error code when the active
// browser implementation provides one.
func ErrorCode(err error) string {
	if errors.Is(err, ErrFileURLNotAllowed) {
		return "file_url_not_allowed"
	}
	if errors.Is(err, ErrTabLimit) {
		return "browser_tab_limit_reached"
	}
	var operationErr *bridgeOperationError
	if errors.As(err, &operationErr) {
		return operationErr.code
	}
	return ""
}

// ErrorRetryable reports whether a failed operation is safe for the caller to
// retry explicitly. The browser service never retries write operations itself.
func ErrorRetryable(err error) bool {
	var operationErr *bridgeOperationError
	return errors.As(err, &operationErr) && operationErr.retryable
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
		// A first open can spend up to 10s waiting for the renderer webview and
		// another 15s waiting for the main-frame CDP navigation commit.
		client = &http.Client{Timeout: 35 * time.Second}
	}
	return &ElectronBridgeService{
		endpoint: endpoint,
		token:    token,
		client:   client,
		fileURLs: cfg.FileURLAuthorizer,
	}, nil
}

func (s *ElectronBridgeService) ProcessMode(context.Context, string) string {
	return "webview"
}

func (s *ElectronBridgeService) SupportsMetadataRecovery() bool {
	return true
}

func (s *ElectronBridgeService) CreateTab(ctx context.Context, sessionID string) (TabSnapshot, error) {
	tabID := newID("tab")
	var snapshot electronBridgeSnapshot
	if err := s.post(ctx, "/browser/tabs/ensure", electronBridgeRequest{
		SessionID: sessionID,
		TabID:     tabID,
		URL:       "about:blank",
	}, &snapshot); err != nil {
		return TabSnapshot{}, err
	}
	return snapshot.tab(), nil
}

func (s *ElectronBridgeService) OpenNewTab(ctx context.Context, sessionID, rawURL string) (TabSnapshot, error) {
	authorizedURL, err := normalizeAuthorizedURL(ctx, sessionID, rawURL, s.fileURLs)
	if err != nil {
		return TabSnapshot{}, err
	}
	tabID := newID("tab")
	var snapshot electronBridgeSnapshot
	if err := s.post(ctx, "/browser/tabs/open", electronBridgeRequest{
		SessionID: sessionID,
		TabID:     tabID,
		URL:       authorizedURL.URL,
		FileRoot:  authorizedURL.FileRoot,
	}, &snapshot); err != nil {
		cleanupCtx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		_ = s.post(cleanupCtx, "/browser/tabs/close", electronBridgeRequest{SessionID: sessionID, TabID: tabID}, nil)
		return TabSnapshot{}, err
	}
	return snapshot.tab(), nil
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
	authorizedURL, err := normalizeAuthorizedURL(ctx, sessionID, rawURL, s.fileURLs)
	if err != nil {
		return TabSnapshot{}, err
	}
	var snapshot electronBridgeSnapshot
	activate := false
	request := electronBridgeRequest{
		SessionID: sessionID,
		TabID:     tabID,
		URL:       authorizedURL.URL,
		FileRoot:  authorizedURL.FileRoot,
		Activate:  &activate,
	}
	if !hint.CreatedAt.IsZero() {
		request.CreatedAt = hint.CreatedAt.UTC().Format(time.RFC3339Nano)
	}
	if err := s.post(ctx, "/browser/tabs/ensure", request, &snapshot); err != nil {
		return TabSnapshot{}, err
	}
	return snapshot.tab(), nil
}

func (s *ElectronBridgeService) CloseSessionBrowser(ctx context.Context, sessionID string) error {
	return s.post(ctx, "/browser/session/close", electronBridgeRequest{SessionID: sessionID}, nil)
}

func (s *ElectronBridgeService) RevokeFileAccess(ctx context.Context, sessionID string) ([]string, error) {
	var out electronBridgeRevokeFileAccessResponse
	if err := s.post(ctx, "/browser/session/revoke-file-access", electronBridgeRequest{SessionID: sessionID}, &out); err != nil {
		return nil, err
	}
	return out.ClosedTabIDs, nil
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
	authorizedURL, err := normalizeAuthorizedURL(ctx, sessionID, rawURL, s.fileURLs)
	if err != nil {
		return TabSnapshot{}, err
	}
	var snapshot electronBridgeSnapshot
	if err := s.post(ctx, "/browser/tabs/open", electronBridgeRequest{
		SessionID: sessionID,
		TabID:     tabID,
		URL:       authorizedURL.URL,
		FileRoot:  authorizedURL.FileRoot,
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
	code := strings.TrimSpace(payload.Code)
	var cause error
	switch {
	case code == "file_url_not_allowed":
		cause = ErrFileURLNotAllowed
	case code == "browser_tab_limit_reached" || (code == "" && resp.StatusCode == http.StatusTooManyRequests):
		cause = ErrTabLimit
	case code == "browser_tab_not_found" || (code == "" && resp.StatusCode == http.StatusNotFound):
		cause = ErrTabNotFound
	case code == "browser_webview_not_ready" || code == "cdp_detached" || (code == "" && resp.StatusCode == http.StatusServiceUnavailable):
		cause = ErrUnavailable
	case code == "browser_tab_required" || (code == "" && resp.StatusCode == http.StatusBadRequest && strings.Contains(payload.Error, "tab id")):
		cause = ErrTabRequired
	}
	return &bridgeOperationError{
		code:      code,
		message:   payload.Error,
		retryable: payload.Retryable,
		cause:     cause,
	}
}

func (snapshot electronBridgeSnapshot) tab() TabSnapshot {
	now := time.Now().UTC()
	createdAt := parseElectronBridgeTime(snapshot.CreatedAt, now)
	updatedAt := parseElectronBridgeTime(snapshot.UpdatedAt, now)
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
		FaviconURL:   strings.TrimSpace(snapshot.FaviconURL),
		Mode:         "webview",
		CanGoBack:    snapshot.CanGoBack,
		CanGoForward: snapshot.CanGoForward,
		CreatedAt:    createdAt,
		UpdatedAt:    updatedAt,
	}
}

func parseElectronBridgeTime(value string, fallback time.Time) time.Time {
	parsed, err := time.Parse(time.RFC3339Nano, strings.TrimSpace(value))
	if err != nil {
		return fallback
	}
	return parsed.UTC()
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

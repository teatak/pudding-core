package api

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/coder/websocket"
	"github.com/teatak/pudding-core/internal/browser"
	"github.com/teatak/pudding-core/internal/engine"
	"github.com/teatak/pudding-core/internal/event"
	"github.com/teatak/pudding-core/internal/provider/mock"
	"github.com/teatak/pudding-core/internal/provider/registry"
	"github.com/teatak/pudding-core/internal/store"
	"github.com/teatak/pudding-core/internal/store/memstore"
)

func newBrowserTestServer(t *testing.T) (*httptest.Server, store.Store, *fakeBrowserService) {
	t.Helper()
	ms := memstore.New()
	homeDir := t.TempDir()
	hub := event.NewHub()
	eng := engine.New(ms, hub, registry.Static(mock.New(mock.WithScript([]string{"ok"}))), ms, engine.WithAttachmentHome(homeDir))
	browserSvc := &fakeBrowserService{tabs: map[string]browser.TabSnapshot{}}
	srv := httptest.NewServer(New(eng, ms, ms, hub).WithHome(homeDir).WithBrowser(browserSvc).Handler(testToken, nil))
	t.Cleanup(srv.Close)
	return srv, ms, browserSvc
}

func TestBrowserTabsAreSessionScoped(t *testing.T) {
	srv, st, browserSvc := newBrowserTestServer(t)
	ctx := context.Background()
	for _, id := range []string{"sess_a", "sess_b"} {
		if err := st.CreateSession(ctx, &store.Session{ID: id, Provider: "mock", Model: "mock"}); err != nil {
			t.Fatal(err)
		}
	}

	resp := req(t, http.MethodPost, srv.URL+"/sessions/sess_b/browser/open", map[string]string{"url": "example.org"})
	firstOpened := decodeJSON[browser.TabSnapshot](t, resp)
	if resp.StatusCode != http.StatusOK || firstOpened.SessionID != "sess_b" || firstOpened.URL != "example.org" || firstOpened.ID == "" {
		t.Fatalf("session open status=%d tab=%+v", resp.StatusCode, firstOpened)
	}
	if browserSvc.openSession != "sess_b" {
		t.Fatalf("session open session = %q", browserSvc.openSession)
	}

	resp = req(t, http.MethodPost, srv.URL+"/sessions/sess_a/browser/tabs", nil)
	tab := decodeJSON[browser.TabSnapshot](t, resp)
	if resp.StatusCode != http.StatusCreated || tab.SessionID != "sess_a" || tab.ID == "" {
		t.Fatalf("create tab status=%d tab=%+v", resp.StatusCode, tab)
	}

	resp = req(t, http.MethodGet, srv.URL+"/sessions/sess_b/browser/tabs/"+tab.ID, nil)
	resp.Body.Close()
	if resp.StatusCode != http.StatusNotFound {
		t.Fatalf("cross-session get status = %d", resp.StatusCode)
	}

	resp = req(t, http.MethodPost, srv.URL+"/sessions/sess_a/browser/tabs/"+tab.ID+"/open", map[string]string{"url": "example.com"})
	opened := decodeJSON[browser.TabSnapshot](t, resp)
	if resp.StatusCode != http.StatusOK || opened.URL != "example.com" {
		t.Fatalf("open status=%d tab=%+v", resp.StatusCode, opened)
	}
	if browserSvc.openSession != "sess_a" {
		t.Fatalf("open session = %q", browserSvc.openSession)
	}

	resp = req(t, http.MethodPost, srv.URL+"/sessions/sess_a/browser/tabs/"+tab.ID+"/reveal", nil)
	revealed := decodeJSON[browser.TabSnapshot](t, resp)
	if resp.StatusCode != http.StatusOK || revealed.ID != tab.ID || browserSvc.lastNavigation != "reveal" {
		t.Fatalf("reveal status=%d tab=%+v last=%q", resp.StatusCode, revealed, browserSvc.lastNavigation)
	}

	resp = req(t, http.MethodPost, srv.URL+"/sessions/sess_a/browser/tabs/"+tab.ID+"/internal", nil)
	internal := decodeJSON[browser.TabSnapshot](t, resp)
	if resp.StatusCode != http.StatusOK || internal.ID != tab.ID || browserSvc.lastNavigation != "internal" {
		t.Fatalf("internal status=%d tab=%+v last=%q", resp.StatusCode, internal, browserSvc.lastNavigation)
	}

	for _, action := range []struct {
		path string
		want string
	}{
		{path: "back", want: "back"},
		{path: "forward", want: "forward"},
		{path: "reload", want: "reload"},
	} {
		resp = req(t, http.MethodPost, srv.URL+"/sessions/sess_a/browser/tabs/"+tab.ID+"/"+action.path, nil)
		navigated := decodeJSON[browser.TabSnapshot](t, resp)
		if resp.StatusCode != http.StatusOK || navigated.ID != tab.ID || browserSvc.lastNavigation != action.want {
			t.Fatalf("%s status=%d tab=%+v last=%q", action.path, resp.StatusCode, navigated, browserSvc.lastNavigation)
		}
	}

	resp = req(t, http.MethodPost, srv.URL+"/sessions/sess_a/browser/tabs/"+tab.ID+"/click", map[string]string{"selector": "#save", "method": "pointer"})
	clicked := decodeJSON[browser.ActionResult](t, resp)
	if resp.StatusCode != http.StatusOK || clicked.Action != "click" || browserSvc.clickSession != "sess_a" || browserSvc.clickMethod != "pointer" {
		t.Fatalf("click status=%d result=%+v session=%q method=%q", resp.StatusCode, clicked, browserSvc.clickSession, browserSvc.clickMethod)
	}

	resp = req(t, http.MethodPost, srv.URL+"/sessions/sess_a/browser/tabs/"+tab.ID+"/type", map[string]any{"selector": "#name", "text": "Pudding", "clear": true})
	typed := decodeJSON[browser.ActionResult](t, resp)
	if resp.StatusCode != http.StatusOK || typed.Action != "type" || browserSvc.typeText != "Pudding" {
		t.Fatalf("type status=%d result=%+v text=%q", resp.StatusCode, typed, browserSvc.typeText)
	}

	resp = req(t, http.MethodPost, srv.URL+"/sessions/sess_a/browser/tabs/"+tab.ID+"/scroll", map[string]any{"deltaY": 400})
	scrolled := decodeJSON[browser.ActionResult](t, resp)
	if resp.StatusCode != http.StatusOK || scrolled.Action != "scroll" || browserSvc.scrollY != 400 {
		t.Fatalf("scroll status=%d result=%+v deltaY=%v", resp.StatusCode, scrolled, browserSvc.scrollY)
	}

	resp = req(t, http.MethodPost, srv.URL+"/sessions/sess_b/browser/tabs/"+tab.ID+"/click", map[string]string{"selector": "#save"})
	resp.Body.Close()
	if resp.StatusCode != http.StatusNotFound {
		t.Fatalf("cross-session click status = %d", resp.StatusCode)
	}

	resp = req(t, http.MethodDelete, srv.URL+"/sessions/sess_a", nil)
	resp.Body.Close()
	if resp.StatusCode != http.StatusNoContent {
		t.Fatalf("delete status=%d", resp.StatusCode)
	}
	if browserSvc.releasedSession != "sess_a" {
		t.Fatalf("released session = %q", browserSvc.releasedSession)
	}
	if _, ok := browserSvc.tabs[tab.ID]; ok {
		t.Fatalf("session tab was not released")
	}
}

func TestBrowserRoutesRequireExistingSession(t *testing.T) {
	srv, _, _ := newBrowserTestServer(t)
	resp := req(t, http.MethodPost, srv.URL+"/sessions/missing/browser/tabs", nil)
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusNotFound {
		t.Fatalf("status = %d", resp.StatusCode)
	}
}

func TestBrowserStateAPITracksRecoverableSessionState(t *testing.T) {
	srv, st, browserSvc := newBrowserTestServer(t)
	ctx := context.Background()
	if err := st.CreateSession(ctx, &store.Session{ID: "sess_state", Provider: "mock", Model: "mock"}); err != nil {
		t.Fatal(err)
	}
	type stateResponse struct {
		HasState    bool   `json:"hasState"`
		SessionID   string `json:"sessionID"`
		TabID       string `json:"tabID"`
		URL         string `json:"url"`
		Recoverable bool   `json:"recoverable"`
	}

	resp := req(t, http.MethodGet, srv.URL+"/sessions/sess_state/browser/state", nil)
	state := decodeJSON[stateResponse](t, resp)
	if resp.StatusCode != http.StatusOK || state.HasState || state.SessionID != "sess_state" {
		t.Fatalf("empty state status=%d state=%+v", resp.StatusCode, state)
	}

	resp = req(t, http.MethodPost, srv.URL+"/sessions/sess_state/browser/open", map[string]string{"url": "https://www.sohu.com/"})
	tab := decodeJSON[browser.TabSnapshot](t, resp)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("open status=%d tab=%+v", resp.StatusCode, tab)
	}

	resp = req(t, http.MethodGet, srv.URL+"/sessions/sess_state/browser/state", nil)
	state = decodeJSON[stateResponse](t, resp)
	if resp.StatusCode != http.StatusOK || !state.HasState || state.TabID != tab.ID || state.URL != "https://www.sohu.com/" || state.Recoverable {
		t.Fatalf("live state status=%d state=%+v", resp.StatusCode, state)
	}

	browserSvc.tabs = map[string]browser.TabSnapshot{}
	resp = req(t, http.MethodGet, srv.URL+"/sessions/sess_state/browser/state", nil)
	state = decodeJSON[stateResponse](t, resp)
	if resp.StatusCode != http.StatusOK || !state.HasState || state.URL != "https://www.sohu.com/" || !state.Recoverable {
		t.Fatalf("recoverable state status=%d state=%+v", resp.StatusCode, state)
	}

	resp = req(t, http.MethodDelete, srv.URL+"/sessions/sess_state/browser/state", nil)
	resp.Body.Close()
	if resp.StatusCode != http.StatusNoContent {
		t.Fatalf("delete state status=%d", resp.StatusCode)
	}
	if _, err := st.GetBrowserState(ctx, "sess_state"); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("browser state should be cleared: %v", err)
	}
}

func TestBrowserStateAPIRecoversExternalBinding(t *testing.T) {
	srv, st, browserSvc := newBrowserTestServer(t)
	ctx := context.Background()
	if err := st.CreateSession(ctx, &store.Session{ID: "sess_external", Provider: "mock", Model: "mock"}); err != nil {
		t.Fatal(err)
	}
	if _, err := st.PutBrowserState(ctx, store.BrowserStateInput{
		SessionID: "sess_external",
		TabID:     "tab_external",
		URL:       "https://www.sohu.com/",
		Title:     "搜狐",
		Mode:      "external",
	}); err != nil {
		t.Fatal(err)
	}
	type stateResponse struct {
		HasState    bool   `json:"hasState"`
		TabID       string `json:"tabID"`
		URL         string `json:"url"`
		Recoverable bool   `json:"recoverable"`
	}
	resp := req(t, http.MethodGet, srv.URL+"/sessions/sess_external/browser/state", nil)
	state := decodeJSON[stateResponse](t, resp)
	if resp.StatusCode != http.StatusOK || !state.HasState || state.TabID != "tab_external" || state.URL != "https://www.sohu.com/" || state.Recoverable {
		t.Fatalf("external recovery status=%d state=%+v", resp.StatusCode, state)
	}
	if _, ok := browserSvc.tabs["tab_external"]; !ok {
		t.Fatalf("external tab was not rebound")
	}
}

func TestBrowserStateDoesNotPersistProcessMode(t *testing.T) {
	srv, st, browserSvc := newBrowserTestServer(t)
	ctx := context.Background()
	if err := st.CreateSession(ctx, &store.Session{ID: "sess_mode", Provider: "mock", Model: "mock"}); err != nil {
		t.Fatal(err)
	}
	resp := req(t, http.MethodPost, srv.URL+"/sessions/sess_mode/browser/open", map[string]string{"url": "https://www.google.com/"})
	tab := decodeJSON[browser.TabSnapshot](t, resp)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("open status=%d tab=%+v", resp.StatusCode, tab)
	}
	resp = req(t, http.MethodPost, srv.URL+"/sessions/sess_mode/browser/tabs/"+tab.ID+"/reveal", nil)
	revealed := decodeJSON[browser.TabSnapshot](t, resp)
	if resp.StatusCode != http.StatusOK || revealed.Mode != "external" {
		t.Fatalf("reveal status=%d tab=%+v", resp.StatusCode, revealed)
	}
	stored, err := st.GetBrowserState(ctx, "sess_mode")
	if err != nil {
		t.Fatal(err)
	}
	if stored.Mode != "" {
		t.Fatalf("process mode must not persist in session state: %+v", stored)
	}

	browserSvc.processMode = "headless"
	type stateResponse struct {
		HasState    bool   `json:"hasState"`
		Mode        string `json:"mode"`
		ProcessMode string `json:"processMode"`
	}
	resp = req(t, http.MethodGet, srv.URL+"/sessions/sess_mode/browser/state", nil)
	state := decodeJSON[stateResponse](t, resp)
	if resp.StatusCode != http.StatusOK || !state.HasState || state.Mode != "headless" || state.ProcessMode != "headless" {
		t.Fatalf("state should report global process mode status=%d state=%+v", resp.StatusCode, state)
	}
	stored, err = st.GetBrowserState(ctx, "sess_mode")
	if err != nil {
		t.Fatal(err)
	}
	if stored.Mode != "" {
		t.Fatalf("session state was polluted by global process mode: %+v", stored)
	}
}

func TestReleaseBrowserTabClearsStateWhenTabAlreadyGone(t *testing.T) {
	srv, st, _ := newBrowserTestServer(t)
	ctx := context.Background()
	if err := st.CreateSession(ctx, &store.Session{ID: "sess_release", Provider: "mock", Model: "mock"}); err != nil {
		t.Fatal(err)
	}
	if _, err := st.PutBrowserState(ctx, store.BrowserStateInput{
		SessionID: "sess_release",
		TabID:     "tab_missing",
		URL:       "https://www.google.com/",
		Title:     "Google",
		Mode:      "headless",
	}); err != nil {
		t.Fatal(err)
	}
	resp := req(t, http.MethodPost, srv.URL+"/sessions/sess_release/browser/tabs/tab_missing/release", nil)
	resp.Body.Close()
	if resp.StatusCode != http.StatusNoContent {
		t.Fatalf("release missing tab status=%d", resp.StatusCode)
	}
	if _, err := st.GetBrowserState(ctx, "sess_release"); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("browser state should be cleared: %v", err)
	}
}

func TestBrowserScreencastIsSessionScoped(t *testing.T) {
	srv, st, browserSvc := newBrowserTestServer(t)
	ctx := context.Background()
	if err := st.CreateSession(ctx, &store.Session{ID: "sess_cast", Provider: "mock", Model: "mock"}); err != nil {
		t.Fatal(err)
	}
	browserSvc.screencastDone = make(chan struct{})

	resp := req(t, http.MethodPost, srv.URL+"/sessions/sess_cast/browser/tabs", nil)
	tab := decodeJSON[browser.TabSnapshot](t, resp)
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("create tab status=%d", resp.StatusCode)
	}
	wsURL := "ws" + strings.TrimPrefix(srv.URL, "http") + "/sessions/sess_cast/browser/tabs/" + tab.ID + "/screencast?token=" + testToken
	conn, _, err := websocket.Dial(ctx, wsURL, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close(websocket.StatusNormalClosure, "")
	select {
	case <-browserSvc.screencastDone:
	case <-time.After(time.Second):
		t.Fatalf("screencast was not called")
	}
	if browserSvc.screencastSession != "sess_cast" || browserSvc.screencastTab != tab.ID {
		t.Fatalf("screencast routed to session=%q tab=%q", browserSvc.screencastSession, browserSvc.screencastTab)
	}
}

func TestBrowserTestFormIsPublic(t *testing.T) {
	srv, _, _ := newBrowserTestServer(t)
	resp, err := http.Get(srv.URL + browserTestFormPath)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusOK || !strings.Contains(string(body), `id="test-form"`) {
		t.Fatalf("status=%d body=%q", resp.StatusCode, string(body))
	}
}

type fakeBrowserService struct {
	tabs              map[string]browser.TabSnapshot
	processMode       string
	openSession       string
	releasedSession   string
	clickSession      string
	clickMethod       string
	lastNavigation    string
	typeText          string
	scrollY           float64
	screencastSession string
	screencastTab     string
	screencastDone    chan struct{}
}

func (f *fakeBrowserService) ProcessMode(context.Context) string {
	if f.processMode != "" {
		return f.processMode
	}
	return "headless"
}

func (f *fakeBrowserService) CreateTab(_ context.Context, sessionID string) (browser.TabSnapshot, error) {
	return f.create(sessionID), nil
}

func (f *fakeBrowserService) create(sessionID string) browser.TabSnapshot {
	now := time.Now().UTC()
	tab := browser.TabSnapshot{
		ID:        "tab_" + sessionID,
		SessionID: sessionID,
		URL:       "about:blank",
		Title:     "Blank",
		Mode:      f.ProcessMode(context.Background()),
		CreatedAt: now,
		UpdatedAt: now,
	}
	f.tabs[tab.ID] = tab
	return tab
}

func (f *fakeBrowserService) ListTabs(_ context.Context, sessionID string) ([]browser.TabSnapshot, error) {
	var out []browser.TabSnapshot
	for _, tab := range f.tabs {
		if tab.SessionID == sessionID {
			tab.Mode = f.ProcessMode(context.Background())
			out = append(out, tab)
		}
	}
	return out, nil
}

func (f *fakeBrowserService) GetTab(_ context.Context, sessionID, tabID string) (browser.TabSnapshot, error) {
	tab, ok := f.tabs[tabID]
	if !ok || tab.SessionID != sessionID {
		return browser.TabSnapshot{}, browser.ErrTabNotFound
	}
	tab.Mode = f.ProcessMode(context.Background())
	return tab, nil
}

func (f *fakeBrowserService) Recover(_ context.Context, sessionID string, hint browser.RecoverHint) (browser.TabSnapshot, error) {
	if hint.Mode != "external" || hint.TabID == "" {
		return browser.TabSnapshot{}, browser.ErrTabNotFound
	}
	now := time.Now().UTC()
	tab := browser.TabSnapshot{
		ID:         hint.TabID,
		SessionID:  sessionID,
		URL:        hint.URL,
		Title:      hint.Title,
		FaviconURL: hint.FaviconURL,
		Mode:       "external",
		CreatedAt:  now,
		UpdatedAt:  now,
	}
	f.processMode = "external"
	tab.Mode = f.ProcessMode(context.Background())
	f.tabs[tab.ID] = tab
	return tab, nil
}

func (f *fakeBrowserService) ReleaseTab(_ context.Context, sessionID, tabID string) error {
	tab, ok := f.tabs[tabID]
	if !ok || tab.SessionID != sessionID {
		return browser.ErrTabNotFound
	}
	delete(f.tabs, tabID)
	return nil
}

func (f *fakeBrowserService) ReleaseSession(_ context.Context, sessionID string) error {
	f.releasedSession = sessionID
	for id, tab := range f.tabs {
		if tab.SessionID == sessionID {
			delete(f.tabs, id)
		}
	}
	return nil
}

func (f *fakeBrowserService) Open(_ context.Context, sessionID, tabID, rawURL string) (browser.TabSnapshot, error) {
	f.openSession = sessionID
	tab, ok := f.tabs[tabID]
	if !ok {
		tab = f.create(sessionID)
	}
	if tab.SessionID != sessionID {
		return browser.TabSnapshot{}, browser.ErrTabNotFound
	}
	tab.URL = rawURL
	tab.Mode = f.ProcessMode(context.Background())
	f.tabs[tab.ID] = tab
	return tab, nil
}

func (f *fakeBrowserService) Reveal(_ context.Context, sessionID, tabID string) (browser.TabSnapshot, error) {
	f.lastNavigation = "reveal"
	f.processMode = "external"
	tab, err := f.GetTab(context.Background(), sessionID, tabID)
	tab.Mode = f.ProcessMode(context.Background())
	if err == nil {
		f.tabs[tab.ID] = tab
	}
	return tab, err
}

func (f *fakeBrowserService) Internal(_ context.Context, sessionID, tabID string) (browser.TabSnapshot, error) {
	f.lastNavigation = "internal"
	f.processMode = "headless"
	tab, err := f.GetTab(context.Background(), sessionID, tabID)
	tab.Mode = f.ProcessMode(context.Background())
	if err == nil {
		f.tabs[tab.ID] = tab
	}
	return tab, err
}

func (f *fakeBrowserService) Back(_ context.Context, sessionID, tabID string) (browser.TabSnapshot, error) {
	f.lastNavigation = "back"
	return f.GetTab(context.Background(), sessionID, tabID)
}

func (f *fakeBrowserService) Forward(_ context.Context, sessionID, tabID string) (browser.TabSnapshot, error) {
	f.lastNavigation = "forward"
	return f.GetTab(context.Background(), sessionID, tabID)
}

func (f *fakeBrowserService) Reload(_ context.Context, sessionID, tabID string) (browser.TabSnapshot, error) {
	f.lastNavigation = "reload"
	return f.GetTab(context.Background(), sessionID, tabID)
}

func (f *fakeBrowserService) Observe(context.Context, string, string, browser.ObserveOptions) (browser.ObserveResult, error) {
	return browser.ObserveResult{}, nil
}

func (f *fakeBrowserService) Screenshot(context.Context, string, string, browser.ScreenshotOptions) (browser.ScreenshotResult, error) {
	return browser.ScreenshotResult{}, nil
}

func (f *fakeBrowserService) Click(_ context.Context, sessionID, tabID string, in browser.ClickInput) (browser.ActionResult, error) {
	tab, err := f.GetTab(context.Background(), sessionID, tabID)
	if err != nil {
		return browser.ActionResult{}, err
	}
	f.clickSession = sessionID
	f.clickMethod = in.Method
	return browser.ActionResult{Tab: tab, Action: "click", Result: map[string]any{"selector": in.Selector}}, nil
}

func (f *fakeBrowserService) Type(_ context.Context, sessionID, tabID string, in browser.TypeInput) (browser.ActionResult, error) {
	tab, err := f.GetTab(context.Background(), sessionID, tabID)
	if err != nil {
		return browser.ActionResult{}, err
	}
	f.typeText = in.Text
	return browser.ActionResult{Tab: tab, Action: "type", Result: map[string]any{"textLength": len(in.Text)}}, nil
}

func (f *fakeBrowserService) Scroll(_ context.Context, sessionID, tabID string, in browser.ScrollInput) (browser.ActionResult, error) {
	tab, err := f.GetTab(context.Background(), sessionID, tabID)
	if err != nil {
		return browser.ActionResult{}, err
	}
	f.scrollY = in.DeltaY
	return browser.ActionResult{Tab: tab, Action: "scroll", Result: map[string]any{"y": in.DeltaY}}, nil
}

func (f *fakeBrowserService) Screencast(_ context.Context, sessionID, tabID string, _ *websocket.Conn) error {
	f.screencastSession = sessionID
	f.screencastTab = tabID
	if f.screencastDone != nil {
		close(f.screencastDone)
	}
	return nil
}

func (f *fakeBrowserService) Close() error { return nil }

package api

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
	"time"

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

	resp = req(t, http.MethodPost, srv.URL+"/sessions/sess_a/browser/tabs/"+tab.ID+"/click", map[string]string{"selector": "#save", "method": "dom"})
	resp.Body.Close()
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("DOM click method status = %d, want %d", resp.StatusCode, http.StatusBadRequest)
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

func TestBrowserTabsPersistAndReleaseIndependently(t *testing.T) {
	srv, st, _ := newBrowserTestServer(t)
	ctx := context.Background()
	if err := st.CreateSession(ctx, &store.Session{ID: "sess_multi", Provider: "mock", Model: "mock"}); err != nil {
		t.Fatal(err)
	}

	created := make([]browser.TabSnapshot, 0, 2)
	for range 2 {
		resp := req(t, http.MethodPost, srv.URL+"/sessions/sess_multi/browser/tabs", nil)
		tab := decodeJSON[browser.TabSnapshot](t, resp)
		if resp.StatusCode != http.StatusCreated || tab.ID == "" {
			t.Fatalf("create tab status=%d tab=%+v", resp.StatusCode, tab)
		}
		created = append(created, tab)
	}
	for index, tab := range created {
		resp := req(t, http.MethodPost, srv.URL+"/sessions/sess_multi/browser/tabs/"+tab.ID+"/open", map[string]string{
			"url": "https://example.com/" + strconv.Itoa(index+1),
		})
		opened := decodeJSON[browser.TabSnapshot](t, resp)
		if resp.StatusCode != http.StatusOK || opened.ID != tab.ID {
			t.Fatalf("open tab status=%d tab=%+v", resp.StatusCode, opened)
		}
	}

	states, err := st.ListBrowserStates(ctx, "sess_multi")
	if err != nil || len(states) != 2 {
		t.Fatalf("expected two stored tabs: states=%+v err=%v", states, err)
	}
	resp := req(t, http.MethodPost, srv.URL+"/sessions/sess_multi/browser/tabs/"+created[0].ID+"/release", nil)
	resp.Body.Close()
	if resp.StatusCode != http.StatusNoContent {
		t.Fatalf("release tab status=%d", resp.StatusCode)
	}
	if _, err := st.GetBrowserTabState(ctx, "sess_multi", created[0].ID); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("released tab state should be deleted: %v", err)
	}
	if state, err := st.GetBrowserTabState(ctx, "sess_multi", created[1].ID); err != nil || state == nil {
		t.Fatalf("remaining tab state should survive: state=%+v err=%v", state, err)
	}
	resp = req(t, http.MethodGet, srv.URL+"/sessions/sess_multi/browser/tabs", nil)
	listed := decodeJSON[struct {
		Tabs []browser.TabSnapshot `json:"tabs"`
	}](t, resp)
	if resp.StatusCode != http.StatusOK || len(listed.Tabs) != 1 || listed.Tabs[0].ID != created[1].ID {
		t.Fatalf("unexpected remaining tabs status=%d tabs=%+v", resp.StatusCode, listed.Tabs)
	}
}

func TestListBrowserTabsRecoversAllStoredTabs(t *testing.T) {
	srv, st, browserSvc := newBrowserTestServer(t)
	browserSvc.supportsMetadataRecovery = true
	ctx := context.Background()
	if err := st.CreateSession(ctx, &store.Session{ID: "sess_restore_multi", Provider: "mock", Model: "mock"}); err != nil {
		t.Fatal(err)
	}
	for index, tabID := range []string{"tab_restore_a", "tab_restore_b"} {
		if _, err := st.PutBrowserState(ctx, store.BrowserStateInput{
			SessionID: "sess_restore_multi",
			TabID:     tabID,
			URL:       "https://restore.example/" + strconv.Itoa(index+1),
		}); err != nil {
			t.Fatal(err)
		}
	}

	resp := req(t, http.MethodGet, srv.URL+"/sessions/sess_restore_multi/browser/tabs", nil)
	listed := decodeJSON[struct {
		Tabs []browser.TabSnapshot `json:"tabs"`
	}](t, resp)
	if resp.StatusCode != http.StatusOK || len(listed.Tabs) != 2 {
		t.Fatalf("stored tabs were not recovered status=%d tabs=%+v", resp.StatusCode, listed.Tabs)
	}
	if browserSvc.recoverCount != 2 {
		t.Fatalf("expected two recovered tabs, count=%d", browserSvc.recoverCount)
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

func TestBrowserHistoryIsGlobalAndDeletable(t *testing.T) {
	srv, st, _ := newBrowserTestServer(t)
	ctx := context.Background()
	for _, id := range []string{"sess_history_a", "sess_history_b"} {
		if err := st.CreateSession(ctx, &store.Session{ID: id, Provider: "mock", Model: "mock"}); err != nil {
			t.Fatal(err)
		}
	}

	resp := req(t, http.MethodPost, srv.URL+"/sessions/sess_history_a/browser/open", map[string]string{"url": "https://pudding.example/docs"})
	opened := decodeJSON[browser.TabSnapshot](t, resp)
	if resp.StatusCode != http.StatusOK || opened.ID == "" {
		t.Fatalf("open status=%d tab=%+v", resp.StatusCode, opened)
	}
	resp = req(t, http.MethodPost, srv.URL+"/sessions/sess_history_a/browser/tabs/"+opened.ID+"/sync", map[string]string{
		"url": "https://pudding.example/docs", "title": "Pudding docs",
	})
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("sync status=%d", resp.StatusCode)
	}
	resp = req(t, http.MethodDelete, srv.URL+"/sessions/sess_history_a", nil)
	resp.Body.Close()
	if resp.StatusCode != http.StatusNoContent {
		t.Fatalf("delete source session status=%d", resp.StatusCode)
	}

	resp = req(t, http.MethodGet, srv.URL+"/sessions/sess_history_b/browser/history?q=pudding", nil)
	listed := decodeJSON[browserHistoryResp](t, resp)
	if resp.StatusCode != http.StatusOK || len(listed.History) != 1 || listed.History[0].Title != "Pudding docs" {
		t.Fatalf("global history status=%d entries=%+v", resp.StatusCode, listed.History)
	}

	resp = req(t, http.MethodDelete, srv.URL+"/sessions/sess_history_b/browser/history/"+listed.History[0].ID, nil)
	resp.Body.Close()
	if resp.StatusCode != http.StatusNoContent {
		t.Fatalf("delete history status=%d", resp.StatusCode)
	}
	resp = req(t, http.MethodGet, srv.URL+"/sessions/sess_history_b/browser/history", nil)
	listed = decodeJSON[browserHistoryResp](t, resp)
	if resp.StatusCode != http.StatusOK || len(listed.History) != 0 {
		t.Fatalf("deleted global history status=%d entries=%+v", resp.StatusCode, listed.History)
	}

	if _, err := st.PutBrowserHistory(ctx, store.BrowserHistoryInput{URL: "https://one.example/"}); err != nil {
		t.Fatal(err)
	}
	if _, err := st.PutBrowserHistory(ctx, store.BrowserHistoryInput{URL: "https://two.example/"}); err != nil {
		t.Fatal(err)
	}
	resp = req(t, http.MethodDelete, srv.URL+"/sessions/sess_history_b/browser/history", nil)
	resp.Body.Close()
	if resp.StatusCode != http.StatusNoContent {
		t.Fatalf("clear history status=%d", resp.StatusCode)
	}
	entries, err := st.ListBrowserHistory(ctx, "", 20)
	if err != nil || len(entries) != 0 {
		t.Fatalf("history should be cleared globally: entries=%+v err=%v", entries, err)
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

	resp = req(t, http.MethodPost, srv.URL+"/sessions/sess_state/browser/tabs", nil)
	blankTab := decodeJSON[browser.TabSnapshot](t, resp)
	if resp.StatusCode != http.StatusCreated || blankTab.URL != "about:blank" {
		t.Fatalf("blank tab status=%d tab=%+v", resp.StatusCode, blankTab)
	}
	resp = req(t, http.MethodGet, srv.URL+"/sessions/sess_state/browser/state", nil)
	state = decodeJSON[stateResponse](t, resp)
	if resp.StatusCode != http.StatusOK || !state.HasState || state.TabID != blankTab.ID || state.URL != "about:blank" {
		t.Fatalf("blank state status=%d state=%+v", resp.StatusCode, state)
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
	if resp.StatusCode != http.StatusOK || state.HasState || state.Recoverable {
		t.Fatalf("missing live target state status=%d state=%+v", resp.StatusCode, state)
	}
	if _, err := st.GetBrowserState(ctx, "sess_state"); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("stale browser state should be cleared: %v", err)
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

func TestBlankBrowserTabDoesNotLeakAcrossSessions(t *testing.T) {
	srv, st, _ := newBrowserTestServer(t)
	ctx := context.Background()
	for _, id := range []string{"sess_no_browser", "sess_blank_tab"} {
		if err := st.CreateSession(ctx, &store.Session{ID: id, Provider: "mock", Model: "mock"}); err != nil {
			t.Fatal(err)
		}
	}

	resp := req(t, http.MethodPost, srv.URL+"/sessions/sess_blank_tab/browser/tabs", nil)
	blank := decodeJSON[browser.TabSnapshot](t, resp)
	if resp.StatusCode != http.StatusCreated || blank.SessionID != "sess_blank_tab" || blank.URL != "about:blank" {
		t.Fatalf("blank tab status=%d tab=%+v", resp.StatusCode, blank)
	}

	resp = req(t, http.MethodGet, srv.URL+"/sessions/sess_no_browser/browser/state", nil)
	emptyState := decodeJSON[browserStateResp](t, resp)
	if resp.StatusCode != http.StatusOK || emptyState.HasState || emptyState.SessionID != "sess_no_browser" {
		t.Fatalf("empty session state leaked status=%d state=%+v", resp.StatusCode, emptyState)
	}
	resp = req(t, http.MethodGet, srv.URL+"/sessions/sess_no_browser/browser/tabs", nil)
	emptyTabs := decodeJSON[browserTabsResp](t, resp)
	if resp.StatusCode != http.StatusOK || len(emptyTabs.Tabs) != 0 {
		t.Fatalf("empty session tabs leaked status=%d tabs=%+v", resp.StatusCode, emptyTabs.Tabs)
	}

	resp = req(t, http.MethodGet, srv.URL+"/sessions/sess_blank_tab/browser/state", nil)
	blankState := decodeJSON[browserStateResp](t, resp)
	if resp.StatusCode != http.StatusOK || !blankState.HasState || blankState.SessionID != "sess_blank_tab" || blankState.TabID != blank.ID || blankState.URL != "about:blank" {
		t.Fatalf("blank session state status=%d state=%+v blank=%+v", resp.StatusCode, blankState, blank)
	}
}

func TestSyncBrowserTabIgnoresTransientBlankForRealState(t *testing.T) {
	srv, st, _ := newBrowserTestServer(t)
	ctx := context.Background()
	if err := st.CreateSession(ctx, &store.Session{ID: "sess_real_sync", Provider: "mock", Model: "mock"}); err != nil {
		t.Fatal(err)
	}

	resp := req(t, http.MethodPost, srv.URL+"/sessions/sess_real_sync/browser/open", map[string]string{"url": "https://example.com/"})
	opened := decodeJSON[browser.TabSnapshot](t, resp)
	if resp.StatusCode != http.StatusOK || opened.URL != "https://example.com/" {
		t.Fatalf("open status=%d tab=%+v", resp.StatusCode, opened)
	}

	resp = req(t, http.MethodPost, srv.URL+"/sessions/sess_real_sync/browser/tabs/"+opened.ID+"/sync", map[string]string{
		"url":   "about:blank",
		"title": "",
	})
	synced := decodeJSON[browser.TabSnapshot](t, resp)
	if resp.StatusCode != http.StatusOK || synced.URL != "https://example.com/" {
		t.Fatalf("transient blank should not replace real state status=%d tab=%+v", resp.StatusCode, synced)
	}
	state, err := st.GetBrowserState(ctx, "sess_real_sync")
	if err != nil || state.TabID != opened.ID || state.URL != "https://example.com/" {
		t.Fatalf("real browser state was overwritten: state=%+v err=%v", state, err)
	}
}

func TestReleaseBrowserTabBlocksStaleSync(t *testing.T) {
	srv, st, _ := newBrowserTestServer(t)
	ctx := context.Background()
	if err := st.CreateSession(ctx, &store.Session{ID: "sess_release_sync", Provider: "mock", Model: "mock"}); err != nil {
		t.Fatal(err)
	}
	resp := req(t, http.MethodPost, srv.URL+"/sessions/sess_release_sync/browser/open", map[string]string{"url": "https://example.com/"})
	opened := decodeJSON[browser.TabSnapshot](t, resp)
	if resp.StatusCode != http.StatusOK || opened.ID == "" {
		t.Fatalf("open status=%d tab=%+v", resp.StatusCode, opened)
	}

	resp = req(t, http.MethodPost, srv.URL+"/sessions/sess_release_sync/browser/tabs/"+opened.ID+"/release", nil)
	resp.Body.Close()
	if resp.StatusCode != http.StatusNoContent {
		t.Fatalf("release status=%d", resp.StatusCode)
	}
	if _, err := st.GetBrowserState(ctx, "sess_release_sync"); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("release should clear browser state: %v", err)
	}

	resp = req(t, http.MethodPost, srv.URL+"/sessions/sess_release_sync/browser/tabs/"+opened.ID+"/sync", map[string]string{
		"url":   "https://stale.example/",
		"title": "Stale",
	})
	resp.Body.Close()
	if resp.StatusCode != http.StatusNotFound {
		t.Fatalf("stale sync after release should fail status=%d", resp.StatusCode)
	}
	if _, err := st.GetBrowserState(ctx, "sess_release_sync"); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("stale sync revived browser state: %v", err)
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

func TestBrowserStateAPIRecoversElectronMetadata(t *testing.T) {
	srv, st, browserSvc := newBrowserTestServer(t)
	browserSvc.supportsMetadataRecovery = true
	ctx := context.Background()
	if err := st.CreateSession(ctx, &store.Session{ID: "sess_electron_restore", Provider: "mock", Model: "mock"}); err != nil {
		t.Fatal(err)
	}
	if _, err := st.PutBrowserState(ctx, store.BrowserStateInput{
		SessionID: "sess_electron_restore",
		TabID:     "tab_restored",
		URL:       "https://example.com/restored",
		Title:     "Restored",
	}); err != nil {
		t.Fatal(err)
	}
	type stateResponse struct {
		HasState bool   `json:"hasState"`
		TabID    string `json:"tabID"`
		URL      string `json:"url"`
		Mode     string `json:"mode"`
	}
	resp := req(t, http.MethodGet, srv.URL+"/sessions/sess_electron_restore/browser/state", nil)
	state := decodeJSON[stateResponse](t, resp)
	if resp.StatusCode != http.StatusOK || !state.HasState || state.TabID != "tab_restored" || state.URL != "https://example.com/restored" || state.Mode != "headless" {
		t.Fatalf("electron metadata recovery status=%d state=%+v", resp.StatusCode, state)
	}
	if browserSvc.recoverCount != 1 {
		t.Fatalf("metadata recovery should call browser service once, count=%d", browserSvc.recoverCount)
	}
	if tab, ok := browserSvc.tabs["tab_restored"]; !ok || tab.URL != "https://example.com/restored" || tab.Mode != "headless" {
		t.Fatalf("metadata should rebuild live tab: %+v ok=%v", tab, ok)
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
	browserSvc.setProcessMode("sess_mode", "external")
	type stateResponse struct {
		HasState    bool   `json:"hasState"`
		Mode        string `json:"mode"`
		ProcessMode string `json:"processMode"`
	}
	resp = req(t, http.MethodGet, srv.URL+"/sessions/sess_mode/browser/state", nil)
	state := decodeJSON[stateResponse](t, resp)
	if resp.StatusCode != http.StatusOK || !state.HasState || state.Mode != "external" || state.ProcessMode != "external" {
		t.Fatalf("state should report runtime process mode status=%d state=%+v", resp.StatusCode, state)
	}
	stored, err := st.GetBrowserState(ctx, "sess_mode")
	if err != nil {
		t.Fatal(err)
	}
	if stored.Mode != "" {
		t.Fatalf("process mode must not persist in session state: %+v", stored)
	}

	browserSvc.setProcessMode("sess_mode", "headless")
	resp = req(t, http.MethodGet, srv.URL+"/sessions/sess_mode/browser/state", nil)
	state = decodeJSON[stateResponse](t, resp)
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

func TestBrowserProcessModeIsGlobalAndTabsAreSessionScoped(t *testing.T) {
	srv, st, browserSvc := newBrowserTestServer(t)
	ctx := context.Background()
	for _, id := range []string{"sess_a", "sess_b"} {
		if err := st.CreateSession(ctx, &store.Session{ID: id, Provider: "mock", Model: "mock"}); err != nil {
			t.Fatal(err)
		}
	}
	resp := req(t, http.MethodPost, srv.URL+"/sessions/sess_a/browser/open", map[string]string{"url": "https://a.example/"})
	tabA := decodeJSON[browser.TabSnapshot](t, resp)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("open a status=%d tab=%+v", resp.StatusCode, tabA)
	}
	resp = req(t, http.MethodPost, srv.URL+"/sessions/sess_b/browser/open", map[string]string{"url": "https://b.example/"})
	tabB := decodeJSON[browser.TabSnapshot](t, resp)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("open b status=%d tab=%+v", resp.StatusCode, tabB)
	}

	browserSvc.setProcessMode("sess_a", "external")

	type tabsResponse struct {
		Tabs        []browser.TabSnapshot `json:"tabs"`
		ProcessMode string                `json:"processMode"`
	}
	resp = req(t, http.MethodGet, srv.URL+"/sessions/sess_a/browser/tabs", nil)
	tabsA := decodeJSON[tabsResponse](t, resp)
	if resp.StatusCode != http.StatusOK || tabsA.ProcessMode != "external" || len(tabsA.Tabs) != 1 || tabsA.Tabs[0].Mode != "external" {
		t.Fatalf("tabs a status=%d tabs=%+v", resp.StatusCode, tabsA)
	}
	resp = req(t, http.MethodGet, srv.URL+"/sessions/sess_b/browser/tabs", nil)
	tabsB := decodeJSON[tabsResponse](t, resp)
	if resp.StatusCode != http.StatusOK || tabsB.ProcessMode != "external" || len(tabsB.Tabs) != 1 || tabsB.Tabs[0].Mode != "external" || tabsB.Tabs[0].ID != tabB.ID {
		t.Fatalf("tabs b should stay scoped while process mode is global status=%d tabs=%+v", resp.StatusCode, tabsB)
	}

	type stateResponse struct {
		HasState    bool   `json:"hasState"`
		Mode        string `json:"mode"`
		ProcessMode string `json:"processMode"`
		TabID       string `json:"tabID"`
	}
	resp = req(t, http.MethodGet, srv.URL+"/sessions/sess_b/browser/state", nil)
	stateB := decodeJSON[stateResponse](t, resp)
	if resp.StatusCode != http.StatusOK || !stateB.HasState || stateB.TabID != tabB.ID || stateB.Mode != "external" || stateB.ProcessMode != "external" {
		t.Fatalf("state b should stay scoped while process mode is global status=%d state=%+v", resp.StatusCode, stateB)
	}
}

func TestCloseBrowserSessionIsAtomicAndSessionScoped(t *testing.T) {
	srv, st, browserSvc := newBrowserTestServer(t)
	ctx := context.Background()
	for _, id := range []string{"sess_a", "sess_b"} {
		if err := st.CreateSession(ctx, &store.Session{ID: id, Provider: "mock", Model: "mock"}); err != nil {
			t.Fatal(err)
		}
	}
	resp := req(t, http.MethodPost, srv.URL+"/sessions/sess_a/browser/open", map[string]string{"url": "https://a.example/"})
	tabA := decodeJSON[browser.TabSnapshot](t, resp)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("open a status=%d tab=%+v", resp.StatusCode, tabA)
	}
	resp = req(t, http.MethodPost, srv.URL+"/sessions/sess_b/browser/open", map[string]string{"url": "https://b.example/"})
	tabB := decodeJSON[browser.TabSnapshot](t, resp)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("open b status=%d tab=%+v", resp.StatusCode, tabB)
	}
	if _, err := st.PutCanvasItem(ctx, store.CanvasItemInput{
		ID:             "note_sess_a",
		ActorSessionID: "sess_a",
		Kind:           "note",
		Title:          "Note",
		Item:           []byte(`{"kind":"note"}`),
		Window:         []byte(`{}`),
	}); err != nil {
		t.Fatal(err)
	}

	resp = req(t, http.MethodPost, srv.URL+"/sessions/sess_a/browser/close", nil)
	resp.Body.Close()
	if resp.StatusCode != http.StatusNoContent {
		t.Fatalf("close a status=%d", resp.StatusCode)
	}
	if browserSvc.closedBrowserSession != "sess_a" {
		t.Fatalf("closed browser session = %q", browserSvc.closedBrowserSession)
	}
	if _, ok := browserSvc.tabs[tabA.ID]; ok {
		t.Fatalf("session a tab was not closed")
	}
	if _, ok := browserSvc.tabs[tabB.ID]; !ok {
		t.Fatalf("session b tab was closed")
	}
	if _, err := st.GetBrowserState(ctx, "sess_a"); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("session a browser state should be cleared: %v", err)
	}
	if state, err := st.GetBrowserState(ctx, "sess_b"); err != nil || state.TabID != tabB.ID {
		t.Fatalf("session b browser state should remain: state=%+v err=%v", state, err)
	}
	items, err := st.ListCanvasItems(ctx, "sess_a")
	if err != nil {
		t.Fatal(err)
	}
	seen := map[string]bool{}
	for _, item := range items {
		seen[item.ID] = true
	}
	if !seen["note_sess_a"] {
		t.Fatal("canvas item should remain")
	}

	resp = req(t, http.MethodPost, srv.URL+"/sessions/sess_a/browser/tabs", nil)
	newTab := decodeJSON[browser.TabSnapshot](t, resp)
	if resp.StatusCode != http.StatusCreated || newTab.URL != "about:blank" {
		t.Fatalf("create new tab after close status=%d tab=%+v", resp.StatusCode, newTab)
	}

	resp = req(t, http.MethodPost, srv.URL+"/sessions/sess_a/browser/close", nil)
	resp.Body.Close()
	if resp.StatusCode != http.StatusNoContent {
		t.Fatalf("repeat close a status=%d", resp.StatusCode)
	}
}

func TestCloseBrowserSessionSucceedsWhenStoredTabIsGone(t *testing.T) {
	srv, st, _ := newBrowserTestServer(t)
	ctx := context.Background()
	if err := st.CreateSession(ctx, &store.Session{ID: "sess_close_missing", Provider: "mock", Model: "mock"}); err != nil {
		t.Fatal(err)
	}
	if _, err := st.PutBrowserState(ctx, store.BrowserStateInput{
		SessionID: "sess_close_missing",
		TabID:     "tab_missing",
		URL:       "https://example.com/",
		Title:     "Example",
		Mode:      "external",
	}); err != nil {
		t.Fatal(err)
	}

	resp := req(t, http.MethodPost, srv.URL+"/sessions/sess_close_missing/browser/close", nil)
	resp.Body.Close()
	if resp.StatusCode != http.StatusNoContent {
		t.Fatalf("close missing status=%d", resp.StatusCode)
	}
	if _, err := st.GetBrowserState(ctx, "sess_close_missing"); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("browser state should be cleared: %v", err)
	}
}

func TestCloseBrowserSessionBlocksLiveTabRecovery(t *testing.T) {
	srv, st, browserSvc := newBrowserTestServer(t)
	browserSvc.keepTabsOnClose = true
	ctx := context.Background()
	if err := st.CreateSession(ctx, &store.Session{ID: "sess_live_close", Provider: "mock", Model: "mock"}); err != nil {
		t.Fatal(err)
	}
	resp := req(t, http.MethodPost, srv.URL+"/sessions/sess_live_close/browser/open", map[string]string{"url": "https://example.com/"})
	opened := decodeJSON[browser.TabSnapshot](t, resp)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("open status=%d tab=%+v", resp.StatusCode, opened)
	}
	resp = req(t, http.MethodPost, srv.URL+"/sessions/sess_live_close/browser/close", nil)
	resp.Body.Close()
	if resp.StatusCode != http.StatusNoContent {
		t.Fatalf("close status=%d", resp.StatusCode)
	}
	if _, ok := browserSvc.tabs[opened.ID]; !ok {
		t.Fatal("test setup expected live tab to remain after close")
	}
	resp = req(t, http.MethodGet, srv.URL+"/sessions/sess_live_close/browser/state", nil)
	state := decodeJSON[browserStateResp](t, resp)
	if resp.StatusCode != http.StatusOK || state.HasState {
		t.Fatalf("state should stay closed despite live tab status=%d state=%+v", resp.StatusCode, state)
	}
	var tabs struct {
		Tabs []browser.TabSnapshot `json:"tabs"`
	}
	resp = req(t, http.MethodGet, srv.URL+"/sessions/sess_live_close/browser/tabs", nil)
	tabs = decodeJSON[struct {
		Tabs []browser.TabSnapshot `json:"tabs"`
	}](t, resp)
	if resp.StatusCode != http.StatusOK || len(tabs.Tabs) != 0 {
		t.Fatalf("tabs should stay closed despite live tab status=%d tabs=%+v", resp.StatusCode, tabs.Tabs)
	}
	resp = req(t, http.MethodPost, srv.URL+"/sessions/sess_live_close/browser/tabs/"+opened.ID+"/sync", map[string]string{"url": "https://example.org/", "title": "Old"})
	resp.Body.Close()
	if resp.StatusCode != http.StatusNotFound {
		t.Fatalf("syncing a closed tab should fail status=%d", resp.StatusCode)
	}

	resp = req(t, http.MethodPost, srv.URL+"/sessions/sess_live_close/browser/open", map[string]string{"url": "https://fresh.example/"})
	fresh := decodeJSON[browser.TabSnapshot](t, resp)
	if resp.StatusCode != http.StatusOK || fresh.ID == "" || fresh.ID == opened.ID || fresh.URL != "https://fresh.example/" {
		t.Fatalf("fresh open status=%d tab=%+v old=%+v", resp.StatusCode, fresh, opened)
	}
	resp = req(t, http.MethodGet, srv.URL+"/sessions/sess_live_close/browser/tabs", nil)
	tabs = decodeJSON[struct {
		Tabs []browser.TabSnapshot `json:"tabs"`
	}](t, resp)
	if resp.StatusCode != http.StatusOK || len(tabs.Tabs) != 1 || tabs.Tabs[0].ID != fresh.ID {
		t.Fatalf("tabs should expose only fresh tab status=%d tabs=%+v fresh=%+v", resp.StatusCode, tabs.Tabs, fresh)
	}
	stateAfterFresh, err := st.GetBrowserState(ctx, "sess_live_close")
	if err != nil || stateAfterFresh.TabID != fresh.ID || stateAfterFresh.URL != "https://fresh.example/" {
		t.Fatalf("fresh state not persisted: state=%+v err=%v", stateAfterFresh, err)
	}
}

func TestAdoptBrowserTabAllowsNewLiveTabAfterSessionReopen(t *testing.T) {
	srv, st, browserSvc := newBrowserTestServer(t)
	browserSvc.keepTabsOnClose = true
	ctx := context.Background()
	if err := st.CreateSession(ctx, &store.Session{ID: "sess_adopt", Provider: "mock", Model: "mock"}); err != nil {
		t.Fatal(err)
	}
	resp := req(t, http.MethodPost, srv.URL+"/sessions/sess_adopt/browser/open", map[string]string{"url": "https://old.example/"})
	old := decodeJSON[browser.TabSnapshot](t, resp)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("open old status=%d tab=%+v", resp.StatusCode, old)
	}
	resp = req(t, http.MethodPost, srv.URL+"/sessions/sess_adopt/browser/close", nil)
	resp.Body.Close()
	if resp.StatusCode != http.StatusNoContent {
		t.Fatalf("close status=%d", resp.StatusCode)
	}
	resp = req(t, http.MethodPost, srv.URL+"/sessions/sess_adopt/browser/tabs", nil)
	fresh := decodeJSON[browser.TabSnapshot](t, resp)
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("create fresh status=%d tab=%+v", resp.StatusCode, fresh)
	}
	resp = req(t, http.MethodPost, srv.URL+"/sessions/sess_adopt/browser/tabs/"+old.ID+"/adopt", nil)
	resp.Body.Close()
	if resp.StatusCode != http.StatusNotFound {
		t.Fatalf("closed tab adoption status=%d", resp.StatusCode)
	}
	popup := browserSvc.create("sess_adopt", "tab_popup")
	popup.URL = "https://popup.example/"
	browserSvc.tabs[popup.ID] = popup
	resp = req(t, http.MethodPost, srv.URL+"/sessions/sess_adopt/browser/tabs/"+popup.ID+"/adopt", nil)
	adopted := decodeJSON[browser.TabSnapshot](t, resp)
	if resp.StatusCode != http.StatusOK || adopted.ID != popup.ID {
		t.Fatalf("adopt status=%d tab=%+v", resp.StatusCode, adopted)
	}
	resp = req(t, http.MethodGet, srv.URL+"/sessions/sess_adopt/browser/tabs", nil)
	listed := decodeJSON[struct {
		Tabs []browser.TabSnapshot `json:"tabs"`
	}](t, resp)
	if resp.StatusCode != http.StatusOK || len(listed.Tabs) != 2 {
		t.Fatalf("unexpected adopted tabs status=%d tabs=%+v", resp.StatusCode, listed.Tabs)
	}
	for _, tab := range listed.Tabs {
		if tab.ID == old.ID {
			t.Fatalf("closed tab was revived: %+v", listed.Tabs)
		}
	}
}

func TestClosedBrowserSessionAcceptsNewToolOpenedTab(t *testing.T) {
	srv, st, browserSvc := newBrowserTestServer(t)
	ctx := context.Background()
	if err := st.CreateSession(ctx, &store.Session{ID: "sess_tool_reopen", Provider: "mock", Model: "mock"}); err != nil {
		t.Fatal(err)
	}
	resp := req(t, http.MethodPost, srv.URL+"/sessions/sess_tool_reopen/browser/open", map[string]string{"url": "https://old.example/"})
	opened := decodeJSON[browser.TabSnapshot](t, resp)
	if resp.StatusCode != http.StatusOK || opened.ID == "" {
		t.Fatalf("open status=%d tab=%+v", resp.StatusCode, opened)
	}
	resp = req(t, http.MethodPost, srv.URL+"/sessions/sess_tool_reopen/browser/close", nil)
	resp.Body.Close()
	if resp.StatusCode != http.StatusNoContent {
		t.Fatalf("close status=%d", resp.StatusCode)
	}

	toolTab, err := browserSvc.Open(ctx, "sess_tool_reopen", "", "https://tool.example/")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := st.PutBrowserState(ctx, store.BrowserStateInput{
		SessionID: "sess_tool_reopen",
		TabID:     toolTab.ID,
		URL:       toolTab.URL,
		Title:     toolTab.Title,
	}); err != nil {
		t.Fatal(err)
	}

	resp = req(t, http.MethodGet, srv.URL+"/sessions/sess_tool_reopen/browser/state", nil)
	state := decodeJSON[browserStateResp](t, resp)
	if resp.StatusCode != http.StatusOK || !state.HasState || state.TabID != toolTab.ID || state.URL != "https://tool.example/" {
		t.Fatalf("state should expose tool tab status=%d state=%+v tool=%+v", resp.StatusCode, state, toolTab)
	}
	resp = req(t, http.MethodGet, srv.URL+"/sessions/sess_tool_reopen/browser/tabs", nil)
	tabs := decodeJSON[struct {
		Tabs []browser.TabSnapshot `json:"tabs"`
	}](t, resp)
	if resp.StatusCode != http.StatusOK || len(tabs.Tabs) != 1 || tabs.Tabs[0].ID != toolTab.ID {
		t.Fatalf("tabs should expose tool tab status=%d tabs=%+v tool=%+v", resp.StatusCode, tabs.Tabs, toolTab)
	}
}

func TestSyncBrowserTabPersistsState(t *testing.T) {
	srv, st, _ := newBrowserTestServer(t)
	ctx := context.Background()
	if err := st.CreateSession(ctx, &store.Session{ID: "sess_sync", Provider: "mock", Model: "mock"}); err != nil {
		t.Fatal(err)
	}
	resp := req(t, http.MethodPost, srv.URL+"/sessions/sess_sync/browser/open", map[string]string{"url": "https://start.example/"})
	tab := decodeJSON[browser.TabSnapshot](t, resp)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("open status=%d tab=%+v", resp.StatusCode, tab)
	}
	resp = req(t, http.MethodPost, srv.URL+"/sessions/sess_sync/browser/tabs/"+tab.ID+"/sync", map[string]any{
		"url":          "https://next.example/",
		"title":        "Next",
		"faviconURL":   "https://next.example/favicon.ico",
		"canGoBack":    true,
		"canGoForward": false,
	})
	synced := decodeJSON[browser.TabSnapshot](t, resp)
	if resp.StatusCode != http.StatusOK || synced.URL != "https://next.example/" || synced.Title != "Next" || !synced.CanGoBack {
		t.Fatalf("sync status=%d tab=%+v", resp.StatusCode, synced)
	}
	state, err := st.GetBrowserState(ctx, "sess_sync")
	if err != nil || state.TabID != tab.ID || state.URL != "https://next.example/" || state.Title != "Next" {
		t.Fatalf("state not synced: state=%+v err=%v", state, err)
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

func TestRecoverBrowserTabReturnsCurrentSessionTab(t *testing.T) {
	srv, st, browserSvc := newBrowserTestServer(t)
	ctx := context.Background()
	if err := st.CreateSession(ctx, &store.Session{ID: "sess_recover", Provider: "mock", Model: "mock"}); err != nil {
		t.Fatal(err)
	}
	resp := req(t, http.MethodPost, srv.URL+"/sessions/sess_recover/browser/open", map[string]string{"url": "https://example.com/"})
	opened := decodeJSON[browser.TabSnapshot](t, resp)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("open status=%d tab=%+v", resp.StatusCode, opened)
	}

	resp = req(t, http.MethodPost, srv.URL+"/sessions/sess_recover/browser/tabs/"+opened.ID+"/recover", nil)
	recovered := decodeJSON[browser.TabSnapshot](t, resp)
	if resp.StatusCode != http.StatusOK || recovered.ID != opened.ID || recovered.SessionID != "sess_recover" {
		t.Fatalf("recover status=%d tab=%+v", resp.StatusCode, recovered)
	}
	if browserSvc.recoverCount != 1 {
		t.Fatalf("recover should call browser service, count=%d", browserSvc.recoverCount)
	}
	if state, err := st.GetBrowserState(ctx, "sess_recover"); err != nil || state.TabID != opened.ID {
		t.Fatalf("browser state should stay synced: state=%+v err=%v", state, err)
	}
}

func TestRecoverBrowserTabRebuildsMissingInternalTarget(t *testing.T) {
	srv, st, browserSvc := newBrowserTestServer(t)
	ctx := context.Background()
	if err := st.CreateSession(ctx, &store.Session{ID: "sess_recover_missing", Provider: "mock", Model: "mock"}); err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC()
	browserSvc.recoverableTabs = map[string]browser.TabSnapshot{
		"tab_missing_target": {
			ID:        "tab_missing_target",
			SessionID: "sess_recover_missing",
			URL:       "https://example.com/recovered",
			Title:     "Recovered",
			Mode:      "headless",
			CreatedAt: now,
			UpdatedAt: now,
		},
	}

	resp := req(t, http.MethodPost, srv.URL+"/sessions/sess_recover_missing/browser/tabs/tab_missing_target/recover", nil)
	recovered := decodeJSON[browser.TabSnapshot](t, resp)
	if resp.StatusCode != http.StatusOK || recovered.ID != "tab_missing_target" || recovered.URL != "https://example.com/recovered" {
		t.Fatalf("recover missing target status=%d tab=%+v", resp.StatusCode, recovered)
	}
	if browserSvc.recoverCount != 1 {
		t.Fatalf("recover should call browser service once, count=%d", browserSvc.recoverCount)
	}
	if state, err := st.GetBrowserState(ctx, "sess_recover_missing"); err != nil || state.TabID != "tab_missing_target" {
		t.Fatalf("browser state should be synced after recover: state=%+v err=%v", state, err)
	}
}

func TestRecoverBrowserTabDoesNotCrossSessions(t *testing.T) {
	srv, st, _ := newBrowserTestServer(t)
	ctx := context.Background()
	for _, id := range []string{"sess_a", "sess_b"} {
		if err := st.CreateSession(ctx, &store.Session{ID: id, Provider: "mock", Model: "mock"}); err != nil {
			t.Fatal(err)
		}
	}
	resp := req(t, http.MethodPost, srv.URL+"/sessions/sess_a/browser/open", map[string]string{"url": "https://a.example/"})
	tab := decodeJSON[browser.TabSnapshot](t, resp)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("open status=%d tab=%+v", resp.StatusCode, tab)
	}

	resp = req(t, http.MethodPost, srv.URL+"/sessions/sess_b/browser/tabs/"+tab.ID+"/recover", nil)
	resp.Body.Close()
	if resp.StatusCode != http.StatusNotFound {
		t.Fatalf("cross-session recover status=%d", resp.StatusCode)
	}
}

func TestRecoverBrowserTabRebindsStoredExternalState(t *testing.T) {
	srv, st, browserSvc := newBrowserTestServer(t)
	ctx := context.Background()
	if err := st.CreateSession(ctx, &store.Session{ID: "sess_recover_external", Provider: "mock", Model: "mock"}); err != nil {
		t.Fatal(err)
	}
	if _, err := st.PutBrowserState(ctx, store.BrowserStateInput{
		SessionID: "sess_recover_external",
		TabID:     "tab_external",
		URL:       "https://example.com/auth",
		Title:     "Auth",
		Mode:      "external",
	}); err != nil {
		t.Fatal(err)
	}

	resp := req(t, http.MethodPost, srv.URL+"/sessions/sess_recover_external/browser/tabs/tab_external/recover", nil)
	recovered := decodeJSON[browser.TabSnapshot](t, resp)
	if resp.StatusCode != http.StatusOK || recovered.ID != "tab_external" || recovered.Mode != "external" {
		t.Fatalf("recover external status=%d tab=%+v", resp.StatusCode, recovered)
	}
	if _, ok := browserSvc.tabs["tab_external"]; !ok {
		t.Fatalf("stored external tab was not rebound")
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

func TestProjectChangesRevokeLiveBrowserFileAccess(t *testing.T) {
	srv, st, browserSvc := newBrowserTestServer(t)
	ctx := context.Background()
	for _, project := range []*store.Project{
		{ID: "project_old", Name: "old", RootDirs: []string{t.TempDir()}},
		{ID: "project_new", Name: "new", RootDirs: []string{t.TempDir()}},
	} {
		if err := st.CreateProject(ctx, project); err != nil {
			t.Fatal(err)
		}
	}
	if err := st.CreateSession(ctx, &store.Session{ID: "session_project_change", Provider: "mock", Model: "mock", ProjectID: "project_old"}); err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC()
	browserSvc.tabs["tab_file"] = browser.TabSnapshot{ID: "tab_file", SessionID: "session_project_change", URL: "file:///old/index.html", CreatedAt: now, UpdatedAt: now}
	browserSvc.tabs["tab_web"] = browser.TabSnapshot{ID: "tab_web", SessionID: "session_project_change", URL: "https://example.com/", CreatedAt: now, UpdatedAt: now.Add(time.Second)}
	for _, state := range []store.BrowserStateInput{
		{SessionID: "session_project_change", TabID: "tab_file", URL: "file:///old/index.html"},
		{SessionID: "session_project_change", TabID: "tab_orphan_file", URL: "file:///old/orphan.html"},
		{SessionID: "session_project_change", TabID: "tab_web", URL: "https://example.com/"},
	} {
		if _, err := st.PutBrowserState(ctx, state); err != nil {
			t.Fatal(err)
		}
	}

	resp := req(t, http.MethodPatch, srv.URL+"/sessions/session_project_change", map[string]string{"projectID": "project_new"})
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("patch session status = %d", resp.StatusCode)
	}
	if len(browserSvc.revokedSessions) != 1 || browserSvc.revokedSessions[0] != "session_project_change" {
		t.Fatalf("revoked sessions = %v", browserSvc.revokedSessions)
	}
	if _, err := st.GetBrowserTabState(ctx, "session_project_change", "tab_file"); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("file browser state should be removed: %v", err)
	}
	if _, err := st.GetBrowserTabState(ctx, "session_project_change", "tab_orphan_file"); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("orphan file browser state should be removed: %v", err)
	}
	if _, err := st.GetBrowserTabState(ctx, "session_project_change", "tab_web"); err != nil {
		t.Fatalf("ordinary web browser state should remain: %v", err)
	}

	newRoots := []string{t.TempDir()}
	resp = req(t, http.MethodPatch, srv.URL+"/projects/project_new", map[string]any{"rootDirs": newRoots})
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK || len(browserSvc.revokedSessions) != 2 {
		t.Fatalf("patch project status=%d revoked=%v", resp.StatusCode, browserSvc.revokedSessions)
	}
}

type fakeBrowserService struct {
	tabs                     map[string]browser.TabSnapshot
	recoverableTabs          map[string]browser.TabSnapshot
	processMode              string
	supportsMetadataRecovery bool
	openSession              string
	releasedSession          string
	closedBrowserSession     string
	keepTabsOnClose          bool
	clickSession             string
	clickMethod              string
	lastNavigation           string
	typeText                 string
	scrollY                  float64
	recoverCount             int
	nextTab                  int
	revokedSessions          []string
}

func (f *fakeBrowserService) SupportsMetadataRecovery() bool {
	return f.supportsMetadataRecovery
}

func (f *fakeBrowserService) ProcessMode(_ context.Context, _ string) string {
	if f.processMode != "" {
		return f.processMode
	}
	return "headless"
}

func (f *fakeBrowserService) setProcessMode(_ string, mode string) {
	f.processMode = mode
}

func (f *fakeBrowserService) CreateTab(_ context.Context, sessionID string) (browser.TabSnapshot, error) {
	return f.create(sessionID, ""), nil
}

func (f *fakeBrowserService) OpenNewTab(_ context.Context, sessionID, rawURL string) (browser.TabSnapshot, error) {
	tab := f.create(sessionID, "")
	tab.URL = rawURL
	tab.Title = rawURL
	tab.UpdatedAt = time.Now().UTC()
	f.tabs[tab.ID] = tab
	return tab, nil
}

func (f *fakeBrowserService) create(sessionID, tabID string) browser.TabSnapshot {
	now := time.Now().UTC()
	if strings.TrimSpace(tabID) == "" {
		f.nextTab++
		tabID = "tab_" + sessionID + "_" + strconv.Itoa(f.nextTab)
	}
	tab := browser.TabSnapshot{
		ID:        tabID,
		SessionID: sessionID,
		URL:       "about:blank",
		Title:     "Blank",
		Mode:      f.ProcessMode(context.Background(), sessionID),
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
			tab.Mode = f.ProcessMode(context.Background(), sessionID)
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
	tab.Mode = f.ProcessMode(context.Background(), sessionID)
	return tab, nil
}

func (f *fakeBrowserService) Recover(_ context.Context, sessionID string, hint browser.RecoverHint) (browser.TabSnapshot, error) {
	if hint.TabID == "" {
		return browser.TabSnapshot{}, browser.ErrTabNotFound
	}
	f.recoverCount++
	now := time.Now().UTC()
	if hint.Mode != "external" {
		tab, ok := f.tabs[hint.TabID]
		if !ok || tab.SessionID != sessionID {
			tab, ok = f.recoverableTabs[hint.TabID]
			if !ok || tab.SessionID != sessionID {
				if !f.supportsMetadataRecovery || hint.URL == "" {
					return browser.TabSnapshot{}, browser.ErrTabNotFound
				}
				tab = browser.TabSnapshot{
					ID:         hint.TabID,
					SessionID:  sessionID,
					URL:        hint.URL,
					Title:      hint.Title,
					FaviconURL: hint.FaviconURL,
					CreatedAt:  now,
				}
			}
		}
		if hint.URL != "" {
			tab.URL = hint.URL
		}
		if hint.Title != "" {
			tab.Title = hint.Title
		}
		if hint.FaviconURL != "" {
			tab.FaviconURL = hint.FaviconURL
		}
		tab.Mode = f.ProcessMode(context.Background(), sessionID)
		tab.UpdatedAt = now
		f.tabs[tab.ID] = tab
		return tab, nil
	}
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
	f.setProcessMode(sessionID, "external")
	tab.Mode = f.ProcessMode(context.Background(), sessionID)
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

func (f *fakeBrowserService) CloseSessionBrowser(_ context.Context, sessionID string) error {
	f.closedBrowserSession = sessionID
	if f.keepTabsOnClose {
		return nil
	}
	for id, tab := range f.tabs {
		if tab.SessionID == sessionID {
			delete(f.tabs, id)
		}
	}
	return nil
}

func (f *fakeBrowserService) RevokeFileAccess(_ context.Context, sessionID string) ([]string, error) {
	f.revokedSessions = append(f.revokedSessions, sessionID)
	closed := make([]string, 0)
	for id, tab := range f.tabs {
		if tab.SessionID == sessionID && strings.HasPrefix(strings.ToLower(strings.TrimSpace(tab.URL)), "file:") {
			closed = append(closed, id)
			delete(f.tabs, id)
		}
	}
	return closed, nil
}

func (f *fakeBrowserService) ReleaseSession(_ context.Context, sessionID string) error {
	f.releasedSession = sessionID
	return f.CloseSessionBrowser(context.Background(), sessionID)
}

func (f *fakeBrowserService) Open(_ context.Context, sessionID, tabID, rawURL string) (browser.TabSnapshot, error) {
	f.openSession = sessionID
	tab, ok := f.tabs[tabID]
	if !ok {
		tab = f.create(sessionID, tabID)
	}
	if tab.SessionID != sessionID {
		return browser.TabSnapshot{}, browser.ErrTabNotFound
	}
	tab.URL = rawURL
	tab.Mode = f.ProcessMode(context.Background(), sessionID)
	f.tabs[tab.ID] = tab
	return tab, nil
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

func (f *fakeBrowserService) Close() error { return nil }

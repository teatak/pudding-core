package api

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/teatak/cart/v3"
	"github.com/teatak/pudding-core/internal/browser"
	"github.com/teatak/pudding-core/internal/store"
)

type browserOpenReq struct {
	URL string `json:"url"`
}

type browserSyncReq struct {
	TargetID     string `json:"targetID"`
	URL          string `json:"url"`
	Title        string `json:"title"`
	FaviconURL   string `json:"faviconURL"`
	CanGoBack    bool   `json:"canGoBack"`
	CanGoForward bool   `json:"canGoForward"`
	HistoryVisit bool   `json:"historyVisit"`
}

type browserTabVersion struct {
	session uint64
	tab     uint64
}

type browserObserveReq struct {
	MaxTextChars int `json:"maxTextChars"`
	MaxElements  int `json:"maxElements"`
	Offset       int `json:"offset"`
}

type browserScreenshotReq struct {
	FullPage bool `json:"fullPage"` // Decode only to reject unsupported legacy requests.
}

type browserActionResp = browser.ActionResult

type browserStateResp struct {
	HasState    bool       `json:"hasState"`
	SessionID   string     `json:"sessionID"`
	TabID       string     `json:"tabID,omitempty"`
	URL         string     `json:"url,omitempty"`
	Title       string     `json:"title,omitempty"`
	FaviconURL  string     `json:"faviconURL,omitempty"`
	Mode        string     `json:"mode,omitempty"`
	ProcessMode string     `json:"processMode,omitempty"`
	Recoverable bool       `json:"recoverable,omitempty"`
	CreatedAt   *time.Time `json:"createdAt,omitempty"`
	UpdatedAt   *time.Time `json:"updatedAt,omitempty"`
}

type browserTabsResp struct {
	Tabs        []browser.TabSnapshot `json:"tabs"`
	ProcessMode string                `json:"processMode,omitempty"`
}

type browserHistoryResp struct {
	History []*store.BrowserHistoryEntry `json:"history"`
}

func (s *Server) listBrowserHistory(c *cart.Context) error {
	if _, ok := s.browserStateSession(c); !ok {
		return nil
	}
	limit := store.BrowserHistoryDefaultLimit
	if raw := strings.TrimSpace(c.Request.URL.Query().Get("limit")); raw != "" {
		parsed, err := strconv.Atoi(raw)
		if err != nil || parsed <= 0 {
			return badRequest(c, "invalid history limit")
		}
		limit = parsed
	}
	entries, err := s.store.ListBrowserHistory(c.Request.Context(), c.Request.URL.Query().Get("q"), limit)
	if err != nil {
		return browserStoreError(c, s, err)
	}
	c.JSON(http.StatusOK, browserHistoryResp{History: entries})
	return nil
}

func (s *Server) deleteBrowserHistory(c *cart.Context) error {
	if _, ok := s.browserStateSession(c); !ok {
		return nil
	}
	historyID, _ := c.Param("historyID")
	if err := s.store.DeleteBrowserHistory(c.Request.Context(), historyID); err != nil {
		return browserStoreError(c, s, err)
	}
	c.Response.WriteHeader(http.StatusNoContent)
	return nil
}

func (s *Server) clearBrowserHistory(c *cart.Context) error {
	if _, ok := s.browserStateSession(c); !ok {
		return nil
	}
	if err := s.store.ClearBrowserHistory(c.Request.Context()); err != nil {
		return browserStoreError(c, s, err)
	}
	c.Response.WriteHeader(http.StatusNoContent)
	return nil
}

func (s *Server) getBrowserState(c *cart.Context) error {
	sessionID, ok := s.browserStateSession(c)
	if !ok {
		return nil
	}
	processMode := s.browserProcessMode(c.Request.Context(), sessionID)
	var tabs []browser.TabSnapshot
	live := map[string]bool{}
	if s.browser != nil {
		var err error
		tabs, err = s.browser.ListTabs(c.Request.Context(), sessionID)
		if err != nil {
			return s.browserError(c, err)
		}
		if s.browserTabGateActive(sessionID) {
			if _, err := s.restoreStoredLiveBrowserTabGate(c.Request.Context(), sessionID, tabs); err != nil {
				return s.fail(c, err)
			}
		}
		tabs = s.filterBrowserTabs(sessionID, tabs)
		for _, tab := range tabs {
			live[tab.ID] = true
		}
	}
	// A summary query must not create pages or rewrite saved state. Recovery
	// belongs to the explicit recover route (or the target's next operation).
	if !s.browserTabGateClosed(sessionID) {
		var err error
		tabs, err = s.listStoredBrowserTabs(c.Request.Context(), sessionID, tabs)
		if err != nil {
			return s.fail(c, err)
		}
	}
	valid := make([]browser.TabSnapshot, 0, len(tabs))
	for _, tab := range s.filterBrowserTabs(sessionID, tabs) {
		if _, ok := browserStateInputFromTab(sessionID, tab); ok {
			valid = append(valid, tab)
		}
	}
	if tab, ok, err := latestBrowserTab(valid); err != nil {
		return s.browserError(c, err)
	} else if ok {
		response := browserStateResponseFromTab(tab)
		response.Recoverable = !live[tab.ID]
		c.JSON(http.StatusOK, response)
		return nil
	}
	c.JSON(http.StatusOK, browserStateResp{HasState: false, SessionID: sessionID, Mode: processMode, ProcessMode: processMode})
	return nil
}

func (s *Server) clearBrowserState(c *cart.Context) error {
	sessionID, ok := s.browserStateSession(c)
	if !ok {
		return nil
	}
	if err := s.clearSavedBrowserState(c.Request.Context(), sessionID); err != nil {
		return s.fail(c, err)
	}
	c.Response.WriteHeader(http.StatusNoContent)
	return nil
}

func (s *Server) clearSavedBrowserState(ctx context.Context, sessionID string) error {
	s.browserMu.Lock()
	defer s.browserMu.Unlock()
	// Invalidate in-flight snapshots without closing live tabs. Fresh snapshots
	// remain eligible to persist after the explicit clear has completed.
	if s.browserSessionVersions == nil {
		s.browserSessionVersions = map[string]uint64{}
	}
	s.browserSessionVersions[sessionID]++
	return s.store.ClearBrowserState(ctx, sessionID)
}

func (s *Server) closeBrowserSession(c *cart.Context) error {
	sessionID, ok := s.browserSession(c)
	if !ok {
		return nil
	}
	tabs, err := s.browser.ListTabs(c.Request.Context(), sessionID)
	if err != nil {
		return s.browserError(c, err)
	}
	s.rememberClosedBrowserTabs(sessionID, tabs)
	s.closeBrowserTabGate(sessionID)
	if err := s.browser.CloseSessionBrowser(c.Request.Context(), sessionID); err != nil {
		s.clearBrowserTabGate(sessionID)
		return s.browserError(c, err)
	}
	if err := s.clearClosedBrowserSessionState(c.Request.Context(), sessionID); err != nil {
		return s.fail(c, err)
	}
	c.Response.WriteHeader(http.StatusNoContent)
	return nil
}

func (s *Server) listBrowserTabs(c *cart.Context) error {
	sessionID, ok := s.browserSession(c)
	if !ok {
		return nil
	}
	tabs, err := s.browser.ListTabs(c.Request.Context(), sessionID)
	if err != nil {
		return s.browserError(c, err)
	}
	if s.browserTabGateActive(sessionID) {
		if _, err := s.restoreStoredLiveBrowserTabGate(c.Request.Context(), sessionID, tabs); err != nil {
			return s.fail(c, err)
		}
	}
	tabs = s.filterBrowserTabs(sessionID, tabs)
	if !s.browserTabGateClosed(sessionID) {
		tabs, err = s.listStoredBrowserTabs(c.Request.Context(), sessionID, tabs)
		if err != nil {
			return s.browserError(c, err)
		}
	}
	c.JSON(http.StatusOK, browserTabsResp{Tabs: tabs, ProcessMode: s.browserProcessMode(c.Request.Context(), sessionID)})
	return nil
}

func (s *Server) createBrowserTab(c *cart.Context) error {
	sessionID, ok := s.browserSession(c)
	if !ok {
		return nil
	}
	tab, err := s.browser.CreateTab(c.Request.Context(), sessionID)
	if err != nil {
		return s.browserError(c, err)
	}
	if err := s.allowAndSyncBrowserState(c.Request.Context(), sessionID, tab); err != nil {
		return browserStoreError(c, s, err)
	}
	c.JSON(http.StatusCreated, tab)
	return nil
}

func (s *Server) getBrowserTab(c *cart.Context) error {
	sessionID, ok := s.browserSession(c)
	if !ok {
		return nil
	}
	tabID, _ := c.Param("tabID")
	if !s.browserTabAllowed(sessionID, tabID) {
		return s.browserError(c, browser.ErrTabNotFound)
	}
	tab, err := s.browser.GetTab(c.Request.Context(), sessionID, tabID)
	if err != nil {
		return s.browserError(c, err)
	}
	c.JSON(http.StatusOK, tab)
	return nil
}

func (s *Server) recoverBrowserTab(c *cart.Context) error {
	sessionID, ok := s.browserSession(c)
	if !ok {
		return nil
	}
	tabID, _ := c.Param("tabID")
	if !s.browserTabAllowed(sessionID, tabID) {
		return s.browserError(c, browser.ErrTabNotFound)
	}
	tab, err := s.browser.GetTab(c.Request.Context(), sessionID, tabID)
	if err != nil {
		if !errors.Is(err, browser.ErrTabNotFound) {
			return s.browserError(c, err)
		}
		// Metadata-backed services need the saved URL. Services retaining their
		// own target bindings can still recover a missing live target by ID.
		if !browserSupportsMetadataRecovery(s.browser) {
			tab, err = s.browser.Recover(c.Request.Context(), sessionID, browser.RecoverHint{TabID: tabID})
			if err == nil {
				if err := s.syncBrowserState(c.Request.Context(), sessionID, tab); err != nil {
					return browserStoreError(c, s, err)
				}
				c.JSON(http.StatusOK, tab)
				return nil
			}
			if !errors.Is(err, browser.ErrTabNotFound) {
				return s.browserError(c, err)
			}
		}
		tab, ok, recoverErr := s.recoverStoredBrowserTab(c.Request.Context(), sessionID, tabID)
		if recoverErr != nil {
			return s.browserError(c, recoverErr)
		} else if !ok {
			return s.browserError(c, err)
		}
		c.JSON(http.StatusOK, tab)
		return nil
	}
	tab, err = s.browser.Recover(c.Request.Context(), sessionID, browser.RecoverHint{
		TabID:      tab.ID,
		URL:        tab.URL,
		Title:      tab.Title,
		FaviconURL: tab.FaviconURL,
		Mode:       tab.Mode,
		CreatedAt:  tab.CreatedAt,
	})
	if err != nil {
		return s.browserError(c, err)
	}
	if err := s.syncBrowserState(c.Request.Context(), sessionID, tab); err != nil {
		return browserStoreError(c, s, err)
	}
	c.JSON(http.StatusOK, tab)
	return nil
}

func (s *Server) openBrowserTab(c *cart.Context) error {
	sessionID, ok := s.browserSession(c)
	if !ok {
		return nil
	}
	tabID, _ := c.Param("tabID")
	var req browserOpenReq
	if err := decode(c, &req); err != nil {
		return badRequest(c, "invalid json body")
	}
	tab, err := s.browser.Open(c.Request.Context(), sessionID, tabID, req.URL)
	if err != nil {
		return s.browserError(c, err)
	}
	if err := s.allowAndSyncBrowserState(c.Request.Context(), sessionID, tab); err != nil {
		return browserStoreError(c, s, err)
	}
	c.JSON(http.StatusOK, tab)
	return nil
}

func (s *Server) syncBrowserTab(c *cart.Context) error {
	sessionID, ok := s.browserSession(c)
	if !ok {
		return nil
	}
	tabID, _ := c.Param("tabID")
	version, allowed := s.browserTabSyncVersion(sessionID, tabID)
	if !allowed {
		return s.browserError(c, browser.ErrTabNotFound)
	}
	var req browserSyncReq
	if err := decode(c, &req); err != nil {
		return badRequest(c, "invalid json body")
	}
	now := time.Now().UTC()
	tab := browser.TabSnapshot{
		ID:           tabID,
		SessionID:    sessionID,
		TargetID:     req.TargetID,
		URL:          strings.TrimSpace(req.URL),
		Title:        strings.TrimSpace(req.Title),
		FaviconURL:   strings.TrimSpace(req.FaviconURL),
		Mode:         s.browserProcessMode(c.Request.Context(), sessionID),
		CanGoBack:    req.CanGoBack,
		CanGoForward: req.CanGoForward,
		CreatedAt:    now,
		UpdatedAt:    now,
	}
	tab, err := s.syncBrowserSnapshot(c.Request.Context(), sessionID, tab, req.HistoryVisit, version)
	if err != nil {
		return browserStoreError(c, s, err)
	}
	c.JSON(http.StatusOK, tab)
	return nil
}

func (s *Server) adoptBrowserTab(c *cart.Context) error {
	sessionID, ok := s.browserSession(c)
	if !ok {
		return nil
	}
	tabID, _ := c.Param("tabID")
	if s.browserTabClosed(sessionID, tabID) {
		return s.browserError(c, browser.ErrTabNotFound)
	}
	tab, err := s.browser.GetTab(c.Request.Context(), sessionID, tabID)
	if err != nil {
		return s.browserError(c, err)
	}
	if err := s.allowAndSyncBrowserState(c.Request.Context(), sessionID, tab); err != nil {
		return browserStoreError(c, s, err)
	}
	c.JSON(http.StatusOK, tab)
	return nil
}

func (s *Server) openBrowserSession(c *cart.Context) error {
	sessionID, ok := s.browserSession(c)
	if !ok {
		return nil
	}
	var req browserOpenReq
	if err := decode(c, &req); err != nil {
		return badRequest(c, "invalid json body")
	}
	tabID := ""
	if tabID = s.singleBrowserTabGateID(sessionID); tabID == "" && s.browserTabGateActive(sessionID) {
		tab, err := s.browser.CreateTab(c.Request.Context(), sessionID)
		if err != nil {
			return s.browserError(c, err)
		}
		tabID = tab.ID
	}
	tab, err := s.browser.Open(c.Request.Context(), sessionID, tabID, req.URL)
	if err != nil {
		return s.browserError(c, err)
	}
	if err := s.allowAndSyncBrowserState(c.Request.Context(), sessionID, tab); err != nil {
		return browserStoreError(c, s, err)
	}
	c.JSON(http.StatusOK, tab)
	return nil
}

func (s *Server) browserTabGateClosed(sessionID string) bool {
	s.browserMu.Lock()
	defer s.browserMu.Unlock()
	allowed, ok := s.browserAllowedTabs[strings.TrimSpace(sessionID)]
	return ok && len(allowed) == 0
}

func (s *Server) closeBrowserTabGate(sessionID string) {
	sessionID = strings.TrimSpace(sessionID)
	if sessionID == "" {
		return
	}
	s.browserMu.Lock()
	defer s.browserMu.Unlock()
	s.closeBrowserTabGateLocked(sessionID)
}

func (s *Server) closeBrowserTabGateLocked(sessionID string) {
	if s.browserAllowedTabs == nil {
		s.browserAllowedTabs = map[string]map[string]struct{}{}
	}
	s.browserAllowedTabs[sessionID] = map[string]struct{}{}
	if s.browserSessionVersions == nil {
		s.browserSessionVersions = map[string]uint64{}
	}
	s.browserSessionVersions[sessionID]++
}

func (s *Server) clearClosedBrowserSessionState(ctx context.Context, sessionID string) error {
	s.browserMu.Lock()
	defer s.browserMu.Unlock()
	// Browser IPC finishes before this commit. A concurrent reopen either
	// persists before the close or starts a new lifetime after it.
	s.closeBrowserTabGateLocked(sessionID)
	return s.store.ClearBrowserState(ctx, sessionID)
}

func (s *Server) clearBrowserTabGate(sessionID string) {
	sessionID = strings.TrimSpace(sessionID)
	if sessionID == "" {
		return
	}
	s.browserMu.Lock()
	defer s.browserMu.Unlock()
	delete(s.browserAllowedTabs, sessionID)
	delete(s.browserClosedTabs, sessionID)
}

func (s *Server) allowBrowserTabInGate(sessionID, tabID string) {
	sessionID = strings.TrimSpace(sessionID)
	tabID = strings.TrimSpace(tabID)
	if sessionID == "" || tabID == "" {
		return
	}
	s.browserMu.Lock()
	defer s.browserMu.Unlock()
	s.allowBrowserTabInGateLocked(sessionID, tabID)
}

func (s *Server) allowBrowserTabInGateLocked(sessionID, tabID string) {
	if sessionID == "" || tabID == "" {
		return
	}
	delete(s.browserClosedTabs[sessionID], tabID)
	allowed, ok := s.browserAllowedTabs[sessionID]
	if !ok {
		return
	}
	if allowed == nil {
		allowed = map[string]struct{}{}
		s.browserAllowedTabs[sessionID] = allowed
	}
	allowed[tabID] = struct{}{}
}

func (s *Server) replaceBrowserTabGate(sessionID string, tabs []browser.TabSnapshot) {
	sessionID = strings.TrimSpace(sessionID)
	if sessionID == "" {
		return
	}
	allowed := map[string]struct{}{}
	for _, tab := range tabs {
		tabID := strings.TrimSpace(tab.ID)
		if strings.TrimSpace(tab.SessionID) == sessionID && tabID != "" {
			allowed[tabID] = struct{}{}
		}
	}
	s.browserMu.Lock()
	defer s.browserMu.Unlock()
	if s.browserAllowedTabs == nil {
		s.browserAllowedTabs = map[string]map[string]struct{}{}
	}
	s.browserAllowedTabs[sessionID] = allowed
}

func (s *Server) browserTabGateActive(sessionID string) bool {
	s.browserMu.Lock()
	defer s.browserMu.Unlock()
	_, ok := s.browserAllowedTabs[strings.TrimSpace(sessionID)]
	return ok
}

func (s *Server) singleBrowserTabGateID(sessionID string) string {
	s.browserMu.Lock()
	defer s.browserMu.Unlock()
	allowed, ok := s.browserAllowedTabs[strings.TrimSpace(sessionID)]
	if !ok || len(allowed) != 1 {
		return ""
	}
	for tabID := range allowed {
		return tabID
	}
	return ""
}

func (s *Server) browserTabAllowed(sessionID, tabID string) bool {
	s.browserMu.Lock()
	defer s.browserMu.Unlock()
	return s.browserTabAllowedLocked(strings.TrimSpace(sessionID), strings.TrimSpace(tabID))
}

func (s *Server) browserTabAllowedLocked(sessionID, tabID string) bool {
	if _, closed := s.browserClosedTabs[sessionID][tabID]; closed {
		return false
	}
	allowed, ok := s.browserAllowedTabs[sessionID]
	if !ok {
		return true
	}
	_, ok = allowed[tabID]
	return ok
}

func (s *Server) browserTabSyncVersion(sessionID, tabID string) (browserTabVersion, bool) {
	s.browserMu.Lock()
	defer s.browserMu.Unlock()
	sessionID, tabID = strings.TrimSpace(sessionID), strings.TrimSpace(tabID)
	return s.browserTabVersionLocked(sessionID, tabID), s.browserTabAllowedLocked(sessionID, tabID)
}

func (s *Server) browserTabVersionLocked(sessionID, tabID string) browserTabVersion {
	return browserTabVersion{session: s.browserSessionVersions[sessionID], tab: s.browserTabVersions[sessionID][tabID]}
}

func (s *Server) rememberClosedBrowserTabs(sessionID string, tabs []browser.TabSnapshot) {
	for _, tab := range tabs {
		if strings.TrimSpace(tab.SessionID) == strings.TrimSpace(sessionID) {
			s.rememberClosedBrowserTab(sessionID, tab.ID)
		}
	}
}

func (s *Server) rememberClosedBrowserTab(sessionID, tabID string) {
	sessionID = strings.TrimSpace(sessionID)
	tabID = strings.TrimSpace(tabID)
	if sessionID == "" || tabID == "" {
		return
	}
	s.browserMu.Lock()
	defer s.browserMu.Unlock()
	s.rememberClosedBrowserTabLocked(sessionID, tabID)
}

func (s *Server) rememberClosedBrowserTabLocked(sessionID, tabID string) {
	if sessionID == "" || tabID == "" {
		return
	}
	if s.browserClosedTabs == nil {
		s.browserClosedTabs = map[string]map[string]struct{}{}
	}
	if s.browserClosedTabs[sessionID] == nil {
		s.browserClosedTabs[sessionID] = map[string]struct{}{}
	}
	s.browserClosedTabs[sessionID][tabID] = struct{}{}
	if allowed, ok := s.browserAllowedTabs[sessionID]; ok {
		delete(allowed, tabID)
	}
	if s.browserTabVersions == nil {
		s.browserTabVersions = map[string]map[string]uint64{}
	}
	if s.browserTabVersions[sessionID] == nil {
		s.browserTabVersions[sessionID] = map[string]uint64{}
	}
	s.browserTabVersions[sessionID][tabID]++
}

func (s *Server) deleteClosedBrowserTabState(ctx context.Context, sessionID, tabID string) error {
	s.browserMu.Lock()
	defer s.browserMu.Unlock()
	sessionID, tabID = strings.TrimSpace(sessionID), strings.TrimSpace(tabID)
	s.rememberClosedBrowserTabLocked(sessionID, tabID)
	return s.store.DeleteBrowserState(ctx, sessionID, tabID)
}

func (s *Server) revokeBrowserFileAccess(ctx context.Context, sessionID string) error {
	if s.browser == nil {
		return s.clearStoredBrowserFileStates(ctx, sessionID)
	}
	revoker, ok := s.browser.(browser.FileAccessRevoker)
	if !ok {
		tabs, err := s.browser.ListTabs(ctx, sessionID)
		if err != nil {
			return err
		}
		s.rememberClosedBrowserTabs(sessionID, tabs)
		if err := s.browser.CloseSessionBrowser(ctx, sessionID); err != nil {
			return err
		}
		return s.clearClosedBrowserSessionState(ctx, sessionID)
	}
	closedTabIDs, revokeErr := revoker.RevokeFileAccess(ctx, sessionID)
	for _, tabID := range closedTabIDs {
		if err := s.deleteClosedBrowserTabState(ctx, sessionID, tabID); err != nil {
			return err
		}
	}
	if err := s.clearStoredBrowserFileStates(ctx, sessionID); err != nil {
		return err
	}
	return revokeErr
}

func (s *Server) clearStoredBrowserFileStates(ctx context.Context, sessionID string) error {
	states, err := s.store.ListBrowserStates(ctx, sessionID)
	if err != nil {
		return err
	}
	for _, state := range states {
		if state == nil || !strings.HasPrefix(strings.ToLower(strings.TrimSpace(state.URL)), "file:") {
			continue
		}
		if err := s.deleteClosedBrowserTabState(ctx, sessionID, state.TabID); err != nil {
			return err
		}
	}
	return nil
}

func (s *Server) browserTabClosed(sessionID, tabID string) bool {
	s.browserMu.Lock()
	defer s.browserMu.Unlock()
	_, closed := s.browserClosedTabs[strings.TrimSpace(sessionID)][strings.TrimSpace(tabID)]
	return closed
}

func (s *Server) ignoreTransientBlankSync(ctx context.Context, sessionID string, tab browser.TabSnapshot) (browser.TabSnapshot, bool, error) {
	if !browserURLIsBlank(tab.URL) {
		return browser.TabSnapshot{}, false, nil
	}
	state, err := s.store.GetBrowserTabState(ctx, sessionID, tab.ID)
	if errors.Is(err, store.ErrNotFound) {
		return browser.TabSnapshot{}, false, nil
	}
	if err != nil {
		return browser.TabSnapshot{}, false, err
	}
	if state.TabID != tab.ID || browserURLIsBlank(state.URL) {
		return browser.TabSnapshot{}, false, nil
	}
	return browser.TabSnapshot{
		ID:         state.TabID,
		SessionID:  state.SessionID,
		URL:        state.URL,
		Title:      state.Title,
		FaviconURL: state.FaviconURL,
		Mode:       tab.Mode,
		CreatedAt:  state.CreatedAt,
		UpdatedAt:  state.UpdatedAt,
	}, true, nil
}

func browserURLIsBlank(rawURL string) bool {
	return strings.EqualFold(strings.TrimSpace(rawURL), "about:blank")
}

func (s *Server) filterBrowserTabs(sessionID string, tabs []browser.TabSnapshot) []browser.TabSnapshot {
	sessionID = strings.TrimSpace(sessionID)
	s.browserMu.Lock()
	defer s.browserMu.Unlock()
	out := make([]browser.TabSnapshot, 0, len(tabs))
	for _, tab := range tabs {
		if strings.TrimSpace(tab.SessionID) != sessionID {
			continue
		}
		if !s.browserTabAllowedLocked(sessionID, strings.TrimSpace(tab.ID)) {
			continue
		}
		out = append(out, tab)
	}
	return out
}

// Listing restores metadata only. A visible tab uses the explicit recover route;
// automation recovers its own target before issuing a page operation.
func (s *Server) listStoredBrowserTabs(ctx context.Context, sessionID string, tabs []browser.TabSnapshot) ([]browser.TabSnapshot, error) {
	states, err := s.store.ListBrowserStates(ctx, sessionID)
	if err != nil {
		return nil, err
	}
	live := make(map[string]bool, len(tabs))
	for _, tab := range tabs {
		live[tab.ID] = true
	}
	out := append([]browser.TabSnapshot{}, tabs...)
	for _, state := range states {
		if live[state.TabID] || s.browserTabClosed(sessionID, state.TabID) || !s.browserTabAllowed(sessionID, state.TabID) {
			continue
		}
		out = append(out, browser.TabSnapshot{
			ID: state.TabID, SessionID: sessionID, URL: state.URL,
			Title: state.Title, FaviconURL: state.FaviconURL,
			Mode: s.browserProcessMode(ctx, sessionID), CreatedAt: state.CreatedAt, UpdatedAt: state.UpdatedAt,
		})
	}
	return out, nil
}

func (s *Server) restoreStoredLiveBrowserTabGate(ctx context.Context, sessionID string, tabs []browser.TabSnapshot) (bool, error) {
	states, err := s.store.ListBrowserStates(ctx, sessionID)
	if err != nil {
		return false, err
	}
	stored := make(map[string]struct{}, len(states))
	for _, state := range states {
		if s.browserTabClosed(sessionID, state.TabID) {
			continue
		}
		stored[strings.TrimSpace(state.TabID)] = struct{}{}
	}
	matched := make([]browser.TabSnapshot, 0, len(states))
	for _, tab := range tabs {
		if strings.TrimSpace(tab.SessionID) != strings.TrimSpace(sessionID) {
			continue
		}
		if _, ok := stored[strings.TrimSpace(tab.ID)]; ok {
			matched = append(matched, tab)
		}
	}
	if len(matched) == 0 {
		return false, nil
	}
	matched, err = s.listStoredBrowserTabs(ctx, sessionID, matched)
	if err != nil {
		return false, err
	}
	s.replaceBrowserTabGate(sessionID, matched)
	return true, nil
}

func (s *Server) backBrowserTab(c *cart.Context) error {
	sessionID, ok := s.browserSession(c)
	if !ok {
		return nil
	}
	tabID, _ := c.Param("tabID")
	if !s.browserTabAllowed(sessionID, tabID) {
		return s.browserError(c, browser.ErrTabNotFound)
	}
	tab, err := s.browser.Back(c.Request.Context(), sessionID, tabID)
	if err != nil {
		return s.browserError(c, err)
	}
	if err := s.syncBrowserState(c.Request.Context(), sessionID, tab); err != nil {
		return browserStoreError(c, s, err)
	}
	c.JSON(http.StatusOK, tab)
	return nil
}

func (s *Server) forwardBrowserTab(c *cart.Context) error {
	sessionID, ok := s.browserSession(c)
	if !ok {
		return nil
	}
	tabID, _ := c.Param("tabID")
	if !s.browserTabAllowed(sessionID, tabID) {
		return s.browserError(c, browser.ErrTabNotFound)
	}
	tab, err := s.browser.Forward(c.Request.Context(), sessionID, tabID)
	if err != nil {
		return s.browserError(c, err)
	}
	if err := s.syncBrowserState(c.Request.Context(), sessionID, tab); err != nil {
		return browserStoreError(c, s, err)
	}
	c.JSON(http.StatusOK, tab)
	return nil
}

func (s *Server) reloadBrowserTab(c *cart.Context) error {
	sessionID, ok := s.browserSession(c)
	if !ok {
		return nil
	}
	tabID, _ := c.Param("tabID")
	if !s.browserTabAllowed(sessionID, tabID) {
		return s.browserError(c, browser.ErrTabNotFound)
	}
	tab, err := s.browser.Reload(c.Request.Context(), sessionID, tabID)
	if err != nil {
		return s.browserError(c, err)
	}
	if err := s.syncBrowserState(c.Request.Context(), sessionID, tab); err != nil {
		return browserStoreError(c, s, err)
	}
	c.JSON(http.StatusOK, tab)
	return nil
}

func (s *Server) observeBrowserTab(c *cart.Context) error {
	sessionID, ok := s.browserSession(c)
	if !ok {
		return nil
	}
	tabID, _ := c.Param("tabID")
	if !s.browserTabAllowed(sessionID, tabID) {
		return s.browserError(c, browser.ErrTabNotFound)
	}
	var req browserObserveReq
	if err := decode(c, &req); err != nil && !errors.Is(err, io.EOF) {
		return badRequest(c, "invalid json body")
	}
	if req.Offset < 0 {
		return badRequest(c, "offset must be non-negative")
	}
	result, err := s.browser.Observe(c.Request.Context(), sessionID, tabID, browser.ObserveOptions{
		MaxTextChars: req.MaxTextChars,
		MaxElements:  req.MaxElements,
		Offset:       req.Offset,
	})
	if err != nil {
		return s.browserError(c, err)
	}
	c.JSON(http.StatusOK, result)
	return nil
}

func (s *Server) screenshotBrowserTab(c *cart.Context) error {
	sessionID, ok := s.browserSession(c)
	if !ok {
		return nil
	}
	tabID, _ := c.Param("tabID")
	if !s.browserTabAllowed(sessionID, tabID) {
		return s.browserError(c, browser.ErrTabNotFound)
	}
	var req browserScreenshotReq
	if err := decode(c, &req); err != nil && !errors.Is(err, io.EOF) {
		return badRequest(c, "invalid json body")
	}
	if req.FullPage {
		return badRequest(c, "Full-page screenshots are not supported. Capture the viewport and scroll for more content.")
	}

	result, err := s.browser.Screenshot(c.Request.Context(), sessionID, tabID, browser.ScreenshotOptions{})
	if err != nil {
		return s.browserError(c, err)
	}
	c.JSON(http.StatusOK, result)
	return nil
}

func (s *Server) clickBrowserTab(c *cart.Context) error {
	sessionID, ok := s.browserSession(c)
	if !ok {
		return nil
	}
	tabID, _ := c.Param("tabID")
	if !s.browserTabAllowed(sessionID, tabID) {
		return s.browserError(c, browser.ErrTabNotFound)
	}
	var req browser.ClickInput
	if err := decode(c, &req); err != nil && !errors.Is(err, io.EOF) {
		return badRequest(c, "invalid json body")
	}
	if strings.TrimSpace(req.Selector) == "" && (req.X == nil || req.Y == nil) {
		return badRequest(c, "selector or x/y is required")
	}
	req.Method = strings.ToLower(strings.TrimSpace(req.Method))
	switch req.Method {
	case "", "auto", "pointer":
	default:
		return badRequest(c, "invalid click method")
	}
	result, err := s.browser.Click(c.Request.Context(), sessionID, tabID, req)
	if err != nil {
		return s.browserError(c, err)
	}
	if err := s.syncBrowserState(c.Request.Context(), sessionID, result.Tab); err != nil {
		return browserStoreError(c, s, err)
	}
	c.JSON(http.StatusOK, browserActionResp(result))
	return nil
}

func (s *Server) typeBrowserTab(c *cart.Context) error {
	sessionID, ok := s.browserSession(c)
	if !ok {
		return nil
	}
	tabID, _ := c.Param("tabID")
	if !s.browserTabAllowed(sessionID, tabID) {
		return s.browserError(c, browser.ErrTabNotFound)
	}
	var req browser.TypeInput
	if err := decode(c, &req); err != nil {
		return badRequest(c, "invalid json body")
	}
	if req.Text == "" && !req.Clear {
		return badRequest(c, "text is required")
	}
	result, err := s.browser.Type(c.Request.Context(), sessionID, tabID, req)
	if err != nil {
		return s.browserError(c, err)
	}
	if err := s.syncBrowserState(c.Request.Context(), sessionID, result.Tab); err != nil {
		return browserStoreError(c, s, err)
	}
	c.JSON(http.StatusOK, browserActionResp(result))
	return nil
}

func (s *Server) selectBrowserTab(c *cart.Context) error {
	sessionID, ok := s.browserSession(c)
	if !ok {
		return nil
	}
	tabID, _ := c.Param("tabID")
	if !s.browserTabAllowed(sessionID, tabID) {
		return s.browserError(c, browser.ErrTabNotFound)
	}
	var req browser.SelectInput
	if err := decode(c, &req); err != nil {
		return badRequest(c, "invalid json body")
	}
	if err := req.Validate(); err != nil {
		return badRequest(c, err.Error())
	}
	result, err := s.browser.Select(c.Request.Context(), sessionID, tabID, req)
	if err != nil {
		return s.browserError(c, err)
	}
	if err := s.syncBrowserState(c.Request.Context(), sessionID, result.Tab); err != nil {
		return browserStoreError(c, s, err)
	}
	c.JSON(http.StatusOK, browserActionResp(result))
	return nil
}

func (s *Server) scrollBrowserTab(c *cart.Context) error {
	sessionID, ok := s.browserSession(c)
	if !ok {
		return nil
	}
	tabID, _ := c.Param("tabID")
	if !s.browserTabAllowed(sessionID, tabID) {
		return s.browserError(c, browser.ErrTabNotFound)
	}
	var req browser.ScrollInput
	if err := decode(c, &req); err != nil && !errors.Is(err, io.EOF) {
		return badRequest(c, "invalid json body")
	}
	result, err := s.browser.Scroll(c.Request.Context(), sessionID, tabID, req)
	if err != nil {
		return s.browserError(c, err)
	}
	if err := s.syncBrowserState(c.Request.Context(), sessionID, result.Tab); err != nil {
		return browserStoreError(c, s, err)
	}
	c.JSON(http.StatusOK, browserActionResp(result))
	return nil
}

func (s *Server) releaseBrowserTab(c *cart.Context) error {
	sessionID, ok := s.browserSession(c)
	if !ok {
		return nil
	}
	tabID, _ := c.Param("tabID")
	if err := s.browser.ReleaseTab(c.Request.Context(), sessionID, tabID); err != nil {
		if !errors.Is(err, browser.ErrTabNotFound) {
			return s.browserError(c, err)
		}
	}
	if err := s.deleteClosedBrowserTabState(c.Request.Context(), sessionID, tabID); err != nil {
		return s.fail(c, err)
	}
	tabs, err := s.browser.ListTabs(c.Request.Context(), sessionID)
	if err != nil {
		return s.browserError(c, err)
	}
	tabs, err = s.listStoredBrowserTabs(c.Request.Context(), sessionID, s.filterBrowserTabs(sessionID, tabs))
	if err != nil {
		return s.fail(c, err)
	}
	s.replaceBrowserTabGate(sessionID, tabs)
	c.String(http.StatusNoContent, "")
	return nil
}

func (s *Server) browserSession(c *cart.Context) (string, bool) {
	if s.browser == nil {
		c.JSON(http.StatusServiceUnavailable, map[string]string{"error": "browser_unavailable"})
		return "", false
	}
	sessionID, _ := c.Param("id")
	sessionID = strings.TrimSpace(sessionID)
	if sessionID == "" {
		_ = badRequest(c, "invalid session id")
		return "", false
	}
	if _, err := s.store.GetSession(c.Request.Context(), sessionID); err != nil {
		_ = s.fail(c, err)
		return "", false
	}
	if tabID, _ := c.Param("tabID"); tabID != "" {
		if err := s.awaitBrowserToolOpen(c.Request.Context(), sessionID, tabID); err != nil {
			_ = s.browserError(c, err)
			return "", false
		}
	}
	return sessionID, true
}

func (s *Server) browserStateSession(c *cart.Context) (string, bool) {
	sessionID, _ := c.Param("id")
	sessionID = strings.TrimSpace(sessionID)
	if sessionID == "" {
		_ = badRequest(c, "invalid session id")
		return "", false
	}
	if _, err := s.store.GetSession(c.Request.Context(), sessionID); err != nil {
		_ = s.fail(c, err)
		return "", false
	}
	return sessionID, true
}

func (s *Server) syncBrowserState(ctx context.Context, sessionID string, tab browser.TabSnapshot) error {
	s.browserMu.Lock()
	defer s.browserMu.Unlock()
	if !s.browserTabAllowedLocked(strings.TrimSpace(sessionID), strings.TrimSpace(tab.ID)) {
		return browser.ErrTabNotFound
	}
	return s.syncBrowserStateLocked(ctx, sessionID, tab)
}

func (s *Server) allowAndSyncBrowserState(ctx context.Context, sessionID string, tab browser.TabSnapshot) error {
	s.browserMu.Lock()
	defer s.browserMu.Unlock()
	// Publish the allowed tab and its saved state together, so a concurrent
	// gate refresh cannot hide a new tab before its first snapshot is stored.
	s.allowBrowserTabInGateLocked(strings.TrimSpace(sessionID), strings.TrimSpace(tab.ID))
	return s.syncBrowserStateLocked(ctx, sessionID, tab)
}

func (s *Server) syncBrowserStateLocked(ctx context.Context, sessionID string, tab browser.TabSnapshot) error {
	in, ok := browserStateInputFromTab(sessionID, tab)
	if !ok {
		if strings.TrimSpace(tab.ID) == "" {
			return nil
		}
		return s.store.DeleteBrowserState(ctx, sessionID, tab.ID)
	}
	_, err := s.store.PutBrowserState(ctx, in)
	return err
}

func (s *Server) syncBrowserSnapshot(ctx context.Context, sessionID string, tab browser.TabSnapshot, historyVisit bool, version browserTabVersion) (browser.TabSnapshot, error) {
	s.browserMu.Lock()
	defer s.browserMu.Unlock()
	sessionID, tabID := strings.TrimSpace(sessionID), strings.TrimSpace(tab.ID)
	// Validate and persist together. Neither a close nor a reopen of the same
	// tab ID can intervene after this check and before the store write.
	if !s.browserTabAllowedLocked(sessionID, tabID) || s.browserTabVersionLocked(sessionID, tabID) != version {
		return browser.TabSnapshot{}, browser.ErrTabNotFound
	}
	if current, ignore, err := s.ignoreTransientBlankSync(ctx, sessionID, tab); err != nil {
		return browser.TabSnapshot{}, err
	} else if ignore {
		return current, nil
	}
	if err := s.syncBrowserStateLocked(ctx, sessionID, tab); err != nil {
		return browser.TabSnapshot{}, err
	}
	in, ok := browserHistoryInputFromTab(tab)
	if !ok {
		return tab, nil
	}
	if historyVisit {
		_, err := s.store.PutBrowserHistory(ctx, in)
		return tab, err
	}
	return tab, s.store.UpdateBrowserHistoryMetadata(ctx, in)
}

func browserHistoryInputFromTab(tab browser.TabSnapshot) (store.BrowserHistoryInput, bool) {
	rawURL := strings.TrimSpace(tab.URL)
	if rawURL == "" || strings.EqualFold(rawURL, "about:blank") {
		return store.BrowserHistoryInput{}, false
	}
	parsed, err := url.Parse(rawURL)
	if err != nil {
		return store.BrowserHistoryInput{}, false
	}
	switch strings.ToLower(parsed.Scheme) {
	case "http", "https", "file":
	default:
		return store.BrowserHistoryInput{}, false
	}
	return store.BrowserHistoryInput{
		URL:        rawURL,
		Title:      tab.Title,
		FaviconURL: tab.FaviconURL,
	}, true
}

func (s *Server) browserProcessMode(ctx context.Context, sessionID string) string {
	if s.browser == nil {
		return ""
	}
	return s.browser.ProcessMode(ctx, sessionID)
}

func (s *Server) recoverStoredBrowserTab(ctx context.Context, sessionID, tabID string) (browser.TabSnapshot, bool, error) {
	if s.browser == nil {
		return browser.TabSnapshot{}, false, nil
	}
	version, allowed := s.browserTabSyncVersion(sessionID, tabID)
	if !allowed {
		return browser.TabSnapshot{}, false, nil
	}
	state, err := s.store.GetBrowserTabState(ctx, sessionID, tabID)
	if errors.Is(err, store.ErrNotFound) {
		return browser.TabSnapshot{}, false, nil
	}
	if err != nil {
		return browser.TabSnapshot{}, false, err
	}
	if tabID != "" && state.TabID != tabID {
		return browser.TabSnapshot{}, false, nil
	}
	tab, ok, err := s.recoverBrowserState(ctx, sessionID, state, version)
	if err != nil || !ok {
		return tab, ok, err
	}
	s.browserMu.Lock()
	defer s.browserMu.Unlock()
	if !s.browserTabAllowedLocked(sessionID, tabID) || s.browserTabVersionLocked(sessionID, tabID) != version {
		return browser.TabSnapshot{}, false, browser.ErrTabNotFound
	}
	if err := s.syncBrowserStateLocked(ctx, sessionID, tab); err != nil {
		return browser.TabSnapshot{}, false, err
	}
	return tab, true, nil
}

func (s *Server) recoverBrowserState(ctx context.Context, sessionID string, state *store.BrowserState, version browserTabVersion) (browser.TabSnapshot, bool, error) {
	if s.browser == nil || state == nil {
		return browser.TabSnapshot{}, false, nil
	}
	processMode := s.browser.ProcessMode(ctx, sessionID)
	recoverMode := ""
	if state.Mode == "external" || processMode == "external" {
		recoverMode = "external"
	}
	if recoverMode == "" && !browserSupportsMetadataRecovery(s.browser) {
		return browser.TabSnapshot{}, false, nil
	}
	currentVersion, allowed := s.browserTabSyncVersion(sessionID, state.TabID)
	if !allowed || currentVersion != version {
		return browser.TabSnapshot{}, false, nil
	}
	tab, err := s.browser.Recover(ctx, sessionID, browser.RecoverHint{
		TabID:      state.TabID,
		URL:        state.URL,
		Title:      state.Title,
		FaviconURL: state.FaviconURL,
		Mode:       recoverMode,
		CreatedAt:  state.CreatedAt,
	})
	if errors.Is(err, browser.ErrFileURLNotAllowed) {
		// A saved URL is not a file grant. Native preview approval dies with
		// its live tab; discard only that stale binding, preserving web tabs.
		s.browserMu.Lock()
		defer s.browserMu.Unlock()
		if s.browserTabVersionLocked(sessionID, state.TabID) != version {
			return browser.TabSnapshot{}, false, nil
		}
		s.rememberClosedBrowserTabLocked(sessionID, state.TabID)
		return browser.TabSnapshot{}, false, s.store.DeleteBrowserState(ctx, sessionID, state.TabID)
	}
	if errors.Is(err, browser.ErrTabNotFound) {
		return browser.TabSnapshot{}, false, nil
	}
	if err != nil {
		return browser.TabSnapshot{}, false, err
	}
	return tab, true, nil
}

func browserSupportsMetadataRecovery(service browser.Service) bool {
	support, ok := service.(browser.MetadataRecoverySupport)
	return ok && support.SupportsMetadataRecovery()
}

func browserStateInputFromTab(sessionID string, tab browser.TabSnapshot) (store.BrowserStateInput, bool) {
	if sessionID == "" {
		sessionID = tab.SessionID
	}
	in := store.BrowserStateInput{
		SessionID:  sessionID,
		TabID:      tab.ID,
		URL:        tab.URL,
		Title:      tab.Title,
		FaviconURL: tab.FaviconURL,
	}
	if err := store.NormalizeBrowserStateInput(&in); err != nil {
		return store.BrowserStateInput{}, false
	}
	return in, true
}

func browserStateResponseFromTab(tab browser.TabSnapshot) browserStateResp {
	created := tab.CreatedAt
	updated := tab.UpdatedAt
	return browserStateResp{
		HasState:    true,
		SessionID:   tab.SessionID,
		TabID:       tab.ID,
		URL:         tab.URL,
		Title:       tab.Title,
		FaviconURL:  tab.FaviconURL,
		Mode:        tab.Mode,
		ProcessMode: tab.Mode,
		Recoverable: false,
		CreatedAt:   &created,
		UpdatedAt:   &updated,
	}
}

func latestBrowserTab(tabs []browser.TabSnapshot) (browser.TabSnapshot, bool, error) {
	if len(tabs) == 0 {
		return browser.TabSnapshot{}, false, nil
	}
	latest := tabs[0]
	for _, tab := range tabs[1:] {
		if tab.UpdatedAt.After(latest.UpdatedAt) {
			latest = tab
		}
	}
	return latest, true, nil
}

func browserStoreError(c *cart.Context, s *Server, err error) error {
	if errors.Is(err, browser.ErrTabNotFound) {
		return s.browserError(c, err)
	}
	if errors.Is(err, store.ErrInvalidBrowserState) {
		return badRequest(c, "invalid browser state")
	}
	if errors.Is(err, store.ErrInvalidBrowserHistory) {
		return badRequest(c, "invalid browser history")
	}
	return s.fail(c, err)
}

func (s *Server) browserError(c *cart.Context, err error) error {
	switch {
	case browser.ErrorCode(err) == "browser_close_cancelled":
		c.JSON(http.StatusConflict, map[string]string{"error": "browser_close_cancelled"})
		return nil
	case errors.Is(err, browser.ErrFileURLNotAllowed):
		c.JSON(http.StatusForbidden, map[string]string{"error": "file_url_not_allowed"})
		return nil
	case errors.Is(err, browser.ErrUnavailable):
		c.JSON(http.StatusServiceUnavailable, map[string]string{"error": "browser_unavailable"})
		return nil
	case errors.Is(err, browser.ErrTabNotFound):
		c.JSON(http.StatusNotFound, map[string]string{"error": "browser_tab_not_found"})
		return nil
	case errors.Is(err, browser.ErrTabRequired):
		return badRequest(c, "browser tab id is required")
	case errors.Is(err, browser.ErrTabLimit):
		c.JSON(http.StatusTooManyRequests, map[string]string{"error": "browser_tab_limit_reached"})
		return nil
	default:
		return s.fail(c, err)
	}
}

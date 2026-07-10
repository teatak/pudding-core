package api

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
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
}

type browserObserveReq struct {
	MaxTextChars int `json:"maxTextChars"`
	MaxElements  int `json:"maxElements"`
}

type browserScreenshotReq struct {
	FullPage bool `json:"fullPage"`
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

func (s *Server) getBrowserState(c *cart.Context) error {
	sessionID, ok := s.browserStateSession(c)
	if !ok {
		return nil
	}
	processMode := s.browserProcessMode(c.Request.Context(), sessionID)
	closed := s.browserTabGateClosed(sessionID)
	if s.browser != nil {
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
		if !closed {
			tabs, err = s.recoverStoredBrowserTabs(c.Request.Context(), sessionID, tabs)
			if err != nil {
				return s.browserError(c, err)
			}
		}
		if tab, ok, err := latestBrowserTab(tabs); err != nil {
			return s.browserError(c, err)
		} else if ok {
			if _, persistable := browserStateInputFromTab(sessionID, tab); !persistable {
				if err := s.store.ClearBrowserState(c.Request.Context(), sessionID); err != nil {
					return s.fail(c, err)
				}
				c.JSON(http.StatusOK, browserStateResp{HasState: false, SessionID: sessionID, Mode: processMode, ProcessMode: processMode})
				return nil
			}
			if err := s.syncBrowserState(c.Request.Context(), sessionID, tab); err != nil {
				return browserStoreError(c, s, err)
			}
			c.JSON(http.StatusOK, browserStateResponseFromTab(tab))
			return nil
		}
	}
	if closed {
		c.JSON(http.StatusOK, browserStateResp{HasState: false, SessionID: sessionID, Mode: processMode, ProcessMode: processMode})
		return nil
	}
	state, err := s.store.GetBrowserState(c.Request.Context(), sessionID)
	if errors.Is(err, store.ErrNotFound) {
		c.JSON(http.StatusOK, browserStateResp{HasState: false, SessionID: sessionID, Mode: processMode, ProcessMode: processMode})
		return nil
	}
	if err != nil {
		return s.fail(c, err)
	}
	if !s.browserTabAllowed(sessionID, state.TabID) {
		if err := s.store.ClearBrowserState(c.Request.Context(), sessionID); err != nil {
			return s.fail(c, err)
		}
		c.JSON(http.StatusOK, browserStateResp{HasState: false, SessionID: sessionID, Mode: processMode, ProcessMode: processMode})
		return nil
	}
	if tab, ok, err := s.recoverBrowserState(c.Request.Context(), sessionID, state); err != nil {
		return s.browserError(c, err)
	} else if ok {
		if err := s.syncBrowserState(c.Request.Context(), sessionID, tab); err != nil {
			return browserStoreError(c, s, err)
		}
		c.JSON(http.StatusOK, browserStateResponseFromTab(tab))
		return nil
	}
	if err := s.store.ClearBrowserState(c.Request.Context(), sessionID); err != nil {
		return s.fail(c, err)
	}
	c.JSON(http.StatusOK, browserStateResp{HasState: false, SessionID: sessionID, Mode: processMode, ProcessMode: processMode})
	return nil
}

func (s *Server) clearBrowserState(c *cart.Context) error {
	sessionID, ok := s.browserStateSession(c)
	if !ok {
		return nil
	}
	if err := s.store.ClearBrowserState(c.Request.Context(), sessionID); err != nil {
		return s.fail(c, err)
	}
	c.Response.WriteHeader(http.StatusNoContent)
	return nil
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
	if err := s.store.ClearBrowserState(c.Request.Context(), sessionID); err != nil {
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
		tabs, err = s.recoverStoredBrowserTabs(c.Request.Context(), sessionID, tabs)
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
	s.allowBrowserTabInGate(sessionID, tab.ID)
	if err := s.syncBrowserState(c.Request.Context(), sessionID, tab); err != nil {
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
		if ok, recoverErr := s.recoverStoredBrowserTab(c.Request.Context(), sessionID, tabID); recoverErr != nil {
			return s.browserError(c, recoverErr)
		} else if !ok {
			return s.browserError(c, err)
		}
		tab, err = s.browser.GetTab(c.Request.Context(), sessionID, tabID)
		if err != nil {
			return s.browserError(c, err)
		}
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
	s.allowBrowserTabInGate(sessionID, tab.ID)
	if err := s.syncBrowserState(c.Request.Context(), sessionID, tab); err != nil {
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
	if !s.browserTabAllowed(sessionID, tabID) {
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
		Mode:         "headless",
		CanGoBack:    req.CanGoBack,
		CanGoForward: req.CanGoForward,
		CreatedAt:    now,
		UpdatedAt:    now,
	}
	if current, ignore, err := s.ignoreTransientBlankSync(c.Request.Context(), sessionID, tab); err != nil {
		return s.fail(c, err)
	} else if ignore {
		c.JSON(http.StatusOK, current)
		return nil
	}
	if err := s.syncBrowserState(c.Request.Context(), sessionID, tab); err != nil {
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
	s.allowBrowserTabInGate(sessionID, tab.ID)
	if err := s.syncBrowserState(c.Request.Context(), sessionID, tab); err != nil {
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
	s.allowBrowserTabInGate(sessionID, tab.ID)
	if err := s.syncBrowserState(c.Request.Context(), sessionID, tab); err != nil {
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
	if s.browserAllowedTabs == nil {
		s.browserAllowedTabs = map[string]map[string]struct{}{}
	}
	s.browserAllowedTabs[sessionID] = map[string]struct{}{}
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
	allowed, ok := s.browserAllowedTabs[strings.TrimSpace(sessionID)]
	if !ok {
		return true
	}
	_, ok = allowed[strings.TrimSpace(tabID)]
	return ok
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
	if s.browserClosedTabs == nil {
		s.browserClosedTabs = map[string]map[string]struct{}{}
	}
	if s.browserClosedTabs[sessionID] == nil {
		s.browserClosedTabs[sessionID] = map[string]struct{}{}
	}
	s.browserClosedTabs[sessionID][tabID] = struct{}{}
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
		Mode:       s.browserProcessMode(ctx, sessionID),
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
	allowed, gated := s.browserAllowedTabs[sessionID]
	s.browserMu.Unlock()
	out := make([]browser.TabSnapshot, 0, len(tabs))
	for _, tab := range tabs {
		if strings.TrimSpace(tab.SessionID) != sessionID {
			continue
		}
		if gated {
			if _, ok := allowed[strings.TrimSpace(tab.ID)]; !ok {
				continue
			}
		}
		out = append(out, tab)
	}
	return out
}

func (s *Server) recoverStoredBrowserTabs(ctx context.Context, sessionID string, tabs []browser.TabSnapshot) ([]browser.TabSnapshot, error) {
	states, err := s.store.ListBrowserStates(ctx, sessionID)
	if err != nil {
		return nil, err
	}
	live := make(map[string]struct{}, len(tabs))
	for _, tab := range tabs {
		live[strings.TrimSpace(tab.ID)] = struct{}{}
	}
	out := append([]browser.TabSnapshot(nil), tabs...)
	for _, state := range states {
		tabID := strings.TrimSpace(state.TabID)
		if tabID == "" {
			continue
		}
		if _, ok := live[tabID]; ok {
			continue
		}
		if !s.browserTabAllowed(sessionID, tabID) {
			continue
		}
		tab, ok, err := s.recoverBrowserState(ctx, sessionID, state)
		if err != nil {
			return nil, err
		}
		if !ok {
			continue
		}
		if err := s.syncBrowserState(ctx, sessionID, tab); err != nil {
			return nil, err
		}
		live[tab.ID] = struct{}{}
		out = append(out, tab)
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
	result, err := s.browser.Observe(c.Request.Context(), sessionID, tabID, browser.ObserveOptions{
		MaxTextChars: req.MaxTextChars,
		MaxElements:  req.MaxElements,
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
	result, err := s.browser.Screenshot(c.Request.Context(), sessionID, tabID, browser.ScreenshotOptions{FullPage: req.FullPage})
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
	case "", "auto", "pointer", "dom":
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
	if req.Text == "" {
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
	s.rememberClosedBrowserTab(sessionID, tabID)
	if err := s.browser.ReleaseTab(c.Request.Context(), sessionID, tabID); err != nil {
		if !errors.Is(err, browser.ErrTabNotFound) {
			return s.browserError(c, err)
		}
	}
	tabs, err := s.browser.ListTabs(c.Request.Context(), sessionID)
	if err != nil {
		return s.browserError(c, err)
	}
	s.replaceBrowserTabGate(sessionID, tabs)
	if err := s.store.DeleteBrowserState(c.Request.Context(), sessionID, tabID); err != nil {
		return s.fail(c, err)
	}
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

func (s *Server) browserProcessMode(ctx context.Context, sessionID string) string {
	if s.browser == nil {
		return "headless"
	}
	return s.browser.ProcessMode(ctx, sessionID)
}

func (s *Server) recoverStoredBrowserTab(ctx context.Context, sessionID, tabID string) (bool, error) {
	if s.browser == nil {
		return false, nil
	}
	state, err := s.store.GetBrowserTabState(ctx, sessionID, tabID)
	if errors.Is(err, store.ErrNotFound) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	if tabID != "" && state.TabID != tabID {
		return false, nil
	}
	tab, ok, err := s.recoverBrowserState(ctx, sessionID, state)
	if err != nil || !ok {
		return ok, err
	}
	if err := s.syncBrowserState(ctx, sessionID, tab); err != nil {
		return false, err
	}
	return true, nil
}

func (s *Server) recoverBrowserState(ctx context.Context, sessionID string, state *store.BrowserState) (browser.TabSnapshot, bool, error) {
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
	tab, err := s.browser.Recover(ctx, sessionID, browser.RecoverHint{
		TabID:      state.TabID,
		URL:        state.URL,
		Title:      state.Title,
		FaviconURL: state.FaviconURL,
		Mode:       recoverMode,
		CreatedAt:  state.CreatedAt,
	})
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

func browserStateResponse(state *store.BrowserState, recoverable bool, processMode string) browserStateResp {
	if state == nil {
		return browserStateResp{}
	}
	created := state.CreatedAt
	updated := state.UpdatedAt
	return browserStateResp{
		HasState:    true,
		SessionID:   state.SessionID,
		TabID:       state.TabID,
		URL:         state.URL,
		Title:       state.Title,
		FaviconURL:  state.FaviconURL,
		Mode:        processMode,
		ProcessMode: processMode,
		Recoverable: recoverable,
		CreatedAt:   &created,
		UpdatedAt:   &updated,
	}
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

func writeBrowserHTTPError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, browser.ErrUnavailable):
		writeJSONError(w, http.StatusServiceUnavailable, "browser_unavailable")
	case errors.Is(err, browser.ErrTabNotFound):
		writeJSONError(w, http.StatusNotFound, "browser_tab_not_found")
	case errors.Is(err, browser.ErrTabRequired):
		writeJSONError(w, http.StatusBadRequest, "browser tab id is required")
	default:
		writeJSONError(w, http.StatusInternalServerError, err.Error())
	}
}

func browserStoreError(c *cart.Context, s *Server, err error) error {
	if errors.Is(err, store.ErrInvalidBrowserState) {
		return badRequest(c, "invalid browser state")
	}
	return s.fail(c, err)
}

func writeStoreHTTPError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, store.ErrNotFound):
		writeJSONError(w, http.StatusNotFound, "not_found")
	case errors.Is(err, store.ErrInvalidSession):
		writeJSONError(w, http.StatusBadRequest, "no_model")
	default:
		writeJSONError(w, http.StatusInternalServerError, err.Error())
	}
}

func writeJSONError(w http.ResponseWriter, status int, code string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	payload, _ := json.Marshal(map[string]string{"error": code})
	_, _ = w.Write(payload)
}

func (s *Server) browserError(c *cart.Context, err error) error {
	switch {
	case errors.Is(err, browser.ErrUnavailable):
		c.JSON(http.StatusServiceUnavailable, map[string]string{"error": "browser_unavailable"})
		return nil
	case errors.Is(err, browser.ErrTabNotFound):
		c.JSON(http.StatusNotFound, map[string]string{"error": "browser_tab_not_found"})
		return nil
	case errors.Is(err, browser.ErrTabRequired):
		return badRequest(c, "browser tab id is required")
	default:
		return s.fail(c, err)
	}
}

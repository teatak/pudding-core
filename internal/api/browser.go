package api

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/coder/websocket"
	"github.com/teatak/cart/v3"
	"github.com/teatak/pudding-core/internal/browser"
	"github.com/teatak/pudding-core/internal/store"
)

type browserOpenReq struct {
	URL string `json:"url"`
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
	if s.browser != nil {
		tabs, err := s.browser.ListTabs(c.Request.Context(), sessionID)
		if err != nil {
			return s.browserError(c, err)
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
	state, err := s.store.GetBrowserState(c.Request.Context(), sessionID)
	if errors.Is(err, store.ErrNotFound) {
		c.JSON(http.StatusOK, browserStateResp{HasState: false, SessionID: sessionID, Mode: processMode, ProcessMode: processMode})
		return nil
	}
	if err != nil {
		return s.fail(c, err)
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
	if err := s.browser.CloseSessionBrowser(c.Request.Context(), sessionID); err != nil {
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
	c.JSON(http.StatusCreated, tab)
	return nil
}

func (s *Server) getBrowserTab(c *cart.Context) error {
	sessionID, ok := s.browserSession(c)
	if !ok {
		return nil
	}
	tabID, _ := c.Param("tabID")
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
	tab, err := s.browser.Open(c.Request.Context(), sessionID, "", req.URL)
	if err != nil {
		return s.browserError(c, err)
	}
	if err := s.syncBrowserState(c.Request.Context(), sessionID, tab); err != nil {
		return browserStoreError(c, s, err)
	}
	c.JSON(http.StatusOK, tab)
	return nil
}

func (s *Server) revealBrowserTab(c *cart.Context) error {
	sessionID, ok := s.browserSession(c)
	if !ok {
		return nil
	}
	tabID, _ := c.Param("tabID")
	tab, err := s.browser.Reveal(c.Request.Context(), sessionID, tabID)
	if err != nil {
		if !errors.Is(err, browser.ErrTabNotFound) {
			return s.browserError(c, err)
		}
		if ok, recoverErr := s.recoverStoredBrowserTab(c.Request.Context(), sessionID, tabID); recoverErr != nil {
			return s.browserError(c, recoverErr)
		} else if !ok {
			return s.browserError(c, err)
		}
		tab, err = s.browser.Reveal(c.Request.Context(), sessionID, tabID)
		if err != nil {
			return s.browserError(c, err)
		}
	}
	if err := s.syncBrowserState(c.Request.Context(), sessionID, tab); err != nil {
		return browserStoreError(c, s, err)
	}
	c.JSON(http.StatusOK, tab)
	return nil
}

func (s *Server) internalBrowserTab(c *cart.Context) error {
	sessionID, ok := s.browserSession(c)
	if !ok {
		return nil
	}
	tabID, _ := c.Param("tabID")
	tab, err := s.browser.Internal(c.Request.Context(), sessionID, tabID)
	if err != nil {
		if !errors.Is(err, browser.ErrTabNotFound) {
			return s.browserError(c, err)
		}
		if ok, recoverErr := s.recoverStoredBrowserTab(c.Request.Context(), sessionID, tabID); recoverErr != nil {
			return s.browserError(c, recoverErr)
		} else if !ok {
			return s.browserError(c, err)
		}
		tab, err = s.browser.Internal(c.Request.Context(), sessionID, tabID)
		if err != nil {
			return s.browserError(c, err)
		}
	}
	if err := s.syncBrowserState(c.Request.Context(), sessionID, tab); err != nil {
		return browserStoreError(c, s, err)
	}
	c.JSON(http.StatusOK, tab)
	return nil
}

func (s *Server) backBrowserTab(c *cart.Context) error {
	sessionID, ok := s.browserSession(c)
	if !ok {
		return nil
	}
	tabID, _ := c.Param("tabID")
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
	if err := s.store.ClearBrowserState(c.Request.Context(), sessionID); err != nil {
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

func (s *Server) serveBrowserScreencast(w http.ResponseWriter, r *http.Request) {
	sessionID, tabID, ok := parseBrowserScreencastPath(r.URL.Path)
	if !ok {
		http.NotFound(w, r)
		return
	}
	if s.browser == nil {
		writeJSONError(w, http.StatusServiceUnavailable, "browser_unavailable")
		return
	}
	if strings.TrimSpace(sessionID) == "" {
		writeJSONError(w, http.StatusBadRequest, "invalid session id")
		return
	}
	if _, err := s.store.GetSession(r.Context(), sessionID); err != nil {
		writeStoreHTTPError(w, err)
		return
	}
	if _, err := s.browser.GetTab(r.Context(), sessionID, tabID); err != nil {
		writeBrowserHTTPError(w, err)
		return
	}
	conn, err := websocket.Accept(w, r, &websocket.AcceptOptions{InsecureSkipVerify: true})
	if err != nil {
		return
	}
	defer conn.Close(websocket.StatusNormalClosure, "")
	if err := s.browser.Screencast(r.Context(), sessionID, tabID, conn); err != nil {
		_ = conn.Close(websocket.StatusInternalError, err.Error())
	}
}

func isBrowserScreencastPath(path string) bool {
	_, _, ok := parseBrowserScreencastPath(path)
	return ok
}

func parseBrowserScreencastPath(path string) (string, string, bool) {
	parts := strings.Split(strings.Trim(path, "/"), "/")
	if len(parts) != 6 ||
		parts[0] != "sessions" ||
		parts[2] != "browser" ||
		parts[3] != "tabs" ||
		parts[5] != "screencast" {
		return "", "", false
	}
	sessionID, err := url.PathUnescape(parts[1])
	if err != nil {
		return "", "", false
	}
	tabID, err := url.PathUnescape(parts[4])
	if err != nil {
		return "", "", false
	}
	return sessionID, tabID, true
}

func (s *Server) syncBrowserState(ctx context.Context, sessionID string, tab browser.TabSnapshot) error {
	in, ok := browserStateInputFromTab(sessionID, tab)
	if !ok {
		return s.store.ClearBrowserState(ctx, sessionID)
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
	state, err := s.store.GetBrowserState(ctx, sessionID)
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
	if state.Mode != "external" && processMode != "external" {
		return browser.TabSnapshot{}, false, nil
	}
	tab, err := s.browser.Recover(ctx, sessionID, browser.RecoverHint{
		TabID:      state.TabID,
		URL:        state.URL,
		Title:      state.Title,
		FaviconURL: state.FaviconURL,
		Mode:       "external",
	})
	if errors.Is(err, browser.ErrTabNotFound) {
		return browser.TabSnapshot{}, false, nil
	}
	if err != nil {
		return browser.TabSnapshot{}, false, err
	}
	return tab, true, nil
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

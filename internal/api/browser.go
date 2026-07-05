package api

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/url"
	"strings"

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

func (s *Server) listBrowserTabs(c *cart.Context) error {
	sessionID, ok := s.browserSession(c)
	if !ok {
		return nil
	}
	tabs, err := s.browser.ListTabs(c.Request.Context(), sessionID)
	if err != nil {
		return s.browserError(c, err)
	}
	c.JSON(http.StatusOK, map[string]any{"tabs": tabs})
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
		return s.browserError(c, err)
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
	result, err := s.browser.Click(c.Request.Context(), sessionID, tabID, req)
	if err != nil {
		return s.browserError(c, err)
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
		return s.browserError(c, err)
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

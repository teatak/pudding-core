package api

import (
	"errors"
	"io"
	"net/http"
	"net/url"
	"strings"

	"github.com/teatak/cart/v3"
	"github.com/teatak/pudding-core/internal/store"
	"github.com/teatak/pudding-core/internal/terminal"
)

func (s *Server) listTerminals(c *cart.Context) error {
	if s.terminals == nil {
		return terminalUnavailable(c)
	}
	sessionID, _ := c.Param("id")
	items, err := s.terminals.List(c.Request.Context(), sessionID)
	if err != nil {
		return s.terminalError(c, err)
	}
	c.JSON(http.StatusOK, map[string]any{"terminals": items})
	return nil
}

func (s *Server) createTerminal(c *cart.Context) error {
	if s.terminals == nil {
		return terminalUnavailable(c)
	}
	sessionID, _ := c.Param("id")
	var options terminal.CreateOptions
	if err := decode(c, &options); err != nil && !errors.Is(err, io.EOF) {
		return badRequest(c, "invalid json body")
	}
	item, err := s.terminals.Create(c.Request.Context(), sessionID, options)
	if err != nil {
		return s.terminalError(c, err)
	}
	c.JSON(http.StatusCreated, item)
	return nil
}

func (s *Server) getTerminal(c *cart.Context) error {
	if s.terminals == nil {
		return terminalUnavailable(c)
	}
	sessionID, _ := c.Param("id")
	terminalID, _ := c.Param("terminalID")
	item, err := s.terminals.Get(c.Request.Context(), sessionID, terminalID)
	if err != nil {
		return s.terminalError(c, err)
	}
	c.JSON(http.StatusOK, item)
	return nil
}

func (s *Server) deleteTerminal(c *cart.Context) error {
	if s.terminals == nil {
		return terminalUnavailable(c)
	}
	sessionID, _ := c.Param("id")
	terminalID, _ := c.Param("terminalID")
	if err := s.terminals.Delete(c.Request.Context(), sessionID, terminalID); err != nil {
		return s.terminalError(c, err)
	}
	c.Response.WriteHeader(http.StatusNoContent)
	return nil
}

func (s *Server) serveTerminalWebSocket(w http.ResponseWriter, request *http.Request) {
	sessionID, terminalID, ok := terminalWebSocketParams(request.URL.Path)
	if !ok || s.terminals == nil {
		http.NotFound(w, request)
		return
	}
	s.terminals.ServeWebSocket(w, request, sessionID, terminalID)
}

func isTerminalWebSocketPath(path string) bool {
	_, _, ok := terminalWebSocketParams(path)
	return ok
}

func terminalWebSocketParams(path string) (string, string, bool) {
	parts := strings.Split(strings.Trim(path, "/"), "/")
	if len(parts) != 5 || parts[0] != "sessions" || parts[2] != "terminals" || parts[4] != "ws" {
		return "", "", false
	}
	sessionID, sessionErr := url.PathUnescape(parts[1])
	terminalID, terminalErr := url.PathUnescape(parts[3])
	if sessionErr != nil || terminalErr != nil || strings.TrimSpace(sessionID) == "" || strings.TrimSpace(terminalID) == "" {
		return "", "", false
	}
	return sessionID, terminalID, true
}

func (s *Server) terminalError(c *cart.Context, err error) error {
	switch {
	case errors.Is(err, terminal.ErrUnavailable):
		c.JSON(http.StatusNotImplemented, map[string]string{"error": "terminal_unavailable"})
		return nil
	case errors.Is(err, terminal.ErrNotRunning):
		c.JSON(http.StatusConflict, map[string]string{"error": "terminal_not_running"})
		return nil
	case errors.Is(err, terminal.ErrInvalidCWD), errors.Is(err, store.ErrInvalidTerminal):
		c.JSON(http.StatusBadRequest, map[string]string{"error": "invalid_terminal"})
		return nil
	default:
		return s.fail(c, err)
	}
}

func terminalUnavailable(c *cart.Context) error {
	c.JSON(http.StatusNotImplemented, map[string]string{"error": "terminal_unavailable"})
	return nil
}

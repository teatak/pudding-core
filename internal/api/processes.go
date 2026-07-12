package api

import (
	"errors"
	"net/http"
	"strconv"
	"strings"

	"github.com/teatak/cart/v3"
	"github.com/teatak/pudding-core/internal/store"
	"github.com/teatak/pudding-core/internal/tool"
)

func (s *Server) enrichSessionProcesses(session *store.Session) {
	if session == nil {
		return
	}
	session.BackgroundProcessCount = s.engine.BackgroundProcessCount(session.ID)
}

func (s *Server) listBackgroundProcesses(c *cart.Context) error {
	sessionID, _ := c.Param("id")
	if _, err := s.store.GetSession(c.Request.Context(), sessionID); err != nil {
		return s.fail(c, err)
	}
	c.JSON(http.StatusOK, map[string]any{"processes": s.engine.BackgroundProcesses(sessionID)})
	return nil
}

func (s *Server) getBackgroundProcess(c *cart.Context) error {
	sessionID, _ := c.Param("id")
	processID, _ := c.Param("processID")
	if strings.TrimSpace(processID) == "" {
		return badRequest(c, "process id is required")
	}
	if _, err := s.store.GetSession(c.Request.Context(), sessionID); err != nil {
		return s.fail(c, err)
	}
	offset, err := backgroundProcessQueryInt(c, "offset", 0)
	if err != nil || offset < 0 {
		return badRequest(c, "offset must be a non-negative integer")
	}
	maxBytes, err := backgroundProcessQueryInt(c, "max_bytes", 0)
	if err != nil {
		return badRequest(c, "max_bytes must be an integer")
	}
	tailBytes, err := backgroundProcessQueryInt(c, "tail_bytes", 0)
	if err != nil {
		return badRequest(c, "tail_bytes must be an integer")
	}
	item, err := s.engine.ReadBackgroundProcess(sessionID, processID, int64(offset), maxBytes, tailBytes)
	if err != nil {
		if errors.Is(err, tool.ErrBackgroundProcessNotFound) {
			c.JSON(http.StatusNotFound, map[string]string{"error": "not_found"})
			return nil
		}
		return badRequest(c, err.Error())
	}
	c.JSON(http.StatusOK, item)
	return nil
}

func backgroundProcessQueryInt(c *cart.Context, name string, fallback int) (int, error) {
	raw := strings.TrimSpace(c.Request.URL.Query().Get(name))
	if raw == "" {
		return fallback, nil
	}
	return strconv.Atoi(raw)
}

func (s *Server) stopBackgroundProcess(c *cart.Context) error {
	sessionID, _ := c.Param("id")
	processID, _ := c.Param("processID")
	if strings.TrimSpace(processID) == "" {
		return badRequest(c, "process id is required")
	}
	if _, err := s.store.GetSession(c.Request.Context(), sessionID); err != nil {
		return s.fail(c, err)
	}
	if _, err := s.engine.StopBackgroundProcess(sessionID, processID); err != nil {
		if errors.Is(err, tool.ErrBackgroundProcessNotFound) {
			c.JSON(http.StatusNotFound, map[string]string{"error": "not_found"})
			return nil
		}
		return s.fail(c, err)
	}
	c.Response.WriteHeader(http.StatusNoContent)
	return nil
}

package api

import (
	"errors"
	"net/http"

	"github.com/teatak/cart/v3"
	"github.com/teatak/pudding-core/internal/engine"
)

func (s *Server) sessionDirectories(c *cart.Context) error {
	id, _ := c.Param("id")
	if _, err := s.store.GetSession(c.Request.Context(), id); err != nil {
		return s.fail(c, err)
	}
	dirs, err := s.store.GetSessionDirectories(c.Request.Context(), id)
	if err != nil {
		return s.fail(c, err)
	}
	c.JSON(http.StatusOK, map[string]any{"rootDirs": dirs})
	return nil
}

func (s *Server) setSessionDirectories(c *cart.Context) error {
	id, _ := c.Param("id")
	var in struct {
		RootDirs *[]string `json:"rootDirs"`
	}
	if err := decode(c, &in); err != nil || in.RootDirs == nil || len(*in.RootDirs) > 128 {
		return badRequest(c, "invalid_session_directories")
	}
	if _, err := s.store.GetSession(c.Request.Context(), id); err != nil {
		return s.fail(c, err)
	}
	dirs, err := s.engine.SetSessionDirectories(c.Request.Context(), id, *in.RootDirs)
	if errors.Is(err, engine.ErrInvalidSessionDirectories) {
		return badRequest(c, "invalid_session_directories")
	}
	if err != nil {
		return s.fail(c, err)
	}
	if err := s.revokeBrowserFileAccess(c.Request.Context(), id); err != nil {
		return s.browserError(c, err)
	}
	c.JSON(http.StatusOK, map[string]any{"rootDirs": dirs})
	return nil
}

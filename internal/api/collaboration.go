package api

import (
	"net/http"

	"github.com/teatak/cart/v3"
)

func (s *Server) listChildSessions(c *cart.Context) error {
	id, _ := c.Param("id")
	children, err := s.engine.ListChildSessions(c.Request.Context(), id)
	if err != nil {
		return s.fail(c, err)
	}
	c.JSON(http.StatusOK, map[string]any{"children": children})
	return nil
}

func (s *Server) stopCollaboration(c *cart.Context) error {
	id, _ := c.Param("id")
	if err := s.engine.StopCollaboration(c.Request.Context(), id); err != nil {
		return s.fail(c, err)
	}
	c.String(http.StatusNoContent, "")
	return nil
}

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

func (s *Server) archiveCompletedSubtasks(c *cart.Context) error {
	return s.setSubtaskArchived(c, "", true)
}

func (s *Server) archiveSubtask(c *cart.Context) error {
	id, _ := c.Param("childID")
	return s.setSubtaskArchived(c, id, true)
}

func (s *Server) restoreSubtask(c *cart.Context) error {
	id, _ := c.Param("childID")
	return s.setSubtaskArchived(c, id, false)
}

func (s *Server) setSubtaskArchived(c *cart.Context, childID string, archived bool) error {
	parentID, _ := c.Param("id")
	if err := s.engine.SetSubtaskArchived(c.Request.Context(), parentID, childID, archived); err != nil {
		return s.fail(c, err)
	}
	return s.listChildSessions(c)
}

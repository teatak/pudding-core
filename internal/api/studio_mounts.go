package api

import (
	"net/http"

	"github.com/teatak/cart/v3"
	"github.com/teatak/pudding-core/internal/store"
)

func (s *Server) listStudioMounts(c *cart.Context) error {
	sessionID, _ := c.Param("id")
	items, err := s.store.ListStudioMounts(c.Request.Context(), sessionID)
	if err != nil {
		return s.fail(c, err)
	}
	c.JSON(http.StatusOK, map[string]any{"items": items})
	return nil
}

func (s *Server) deleteStudioMount(c *cart.Context) error {
	sessionID, _ := c.Param("id")
	mountID, _ := c.Param("mountID")
	if err := s.store.DeleteStudioMount(c.Request.Context(), sessionID, mountID); err != nil {
		return s.fail(c, err)
	}
	c.Response.WriteHeader(http.StatusNoContent)
	return nil
}

func (s *Server) openStudioItem(c *cart.Context) error {
	sessionID, _ := c.Param("id")
	itemID, _ := c.Param("itemID")
	item, err := s.store.OpenStudioItem(c.Request.Context(), sessionID, itemID, store.NewID("mount"))
	if err != nil {
		return s.fail(c, err)
	}
	c.JSON(http.StatusOK, item)
	return nil
}

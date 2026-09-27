package api

import (
	"net/http"

	"github.com/teatak/cart/v3"
	"github.com/teatak/pudding-core/internal/store"
)

func (s *Server) listCanvasItems(c *cart.Context) error {
	sessionID, _ := c.Param("id")
	items, err := s.store.ListCanvasItems(c.Request.Context(), sessionID)
	if err != nil {
		return s.fail(c, err)
	}
	c.JSON(http.StatusOK, map[string]any{"items": items})
	return nil
}

func (s *Server) deleteCanvasItem(c *cart.Context) error {
	sessionID, _ := c.Param("id")
	itemID, _ := c.Param("itemID")
	if err := s.store.DeleteCanvasItem(c.Request.Context(), sessionID, itemID); err != nil {
		return s.fail(c, err)
	}
	c.Response.WriteHeader(http.StatusNoContent)
	return nil
}

func (s *Server) openCanvasResource(c *cart.Context) error {
	sessionID, _ := c.Param("id")
	resourceID, _ := c.Param("canvasID")
	item, err := s.store.OpenCanvasResource(c.Request.Context(), sessionID, resourceID, store.NewID("canvas"))
	if err != nil {
		return s.fail(c, err)
	}
	c.JSON(http.StatusOK, item)
	return nil
}

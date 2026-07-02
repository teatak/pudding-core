package api

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"

	"github.com/teatak/cart/v3"
	"github.com/teatak/pudding-core/internal/store"
)

type canvasItemRequest struct {
	ID              string          `json:"id"`
	SourceSessionID string          `json:"sourceSessionID"`
	Kind            string          `json:"kind"`
	Title           string          `json:"title"`
	Item            json.RawMessage `json:"item"`
	Window          json.RawMessage `json:"window"`
}

type canvasItemWindowRequest struct {
	Window json.RawMessage `json:"window"`
}

func (s *Server) listCanvasItems(c *cart.Context) error {
	sessionID, _ := c.Param("id")
	items, err := s.store.ListCanvasItems(c.Request.Context(), sessionID)
	if err != nil {
		return s.fail(c, err)
	}
	c.JSON(http.StatusOK, map[string]any{"items": items})
	return nil
}

func (s *Server) createCanvasItem(c *cart.Context) error {
	sessionID, _ := c.Param("id")
	var req canvasItemRequest
	if err := decode(c, &req); err != nil {
		return badRequest(c, "invalid json body")
	}
	if strings.TrimSpace(req.ID) == "" {
		req.ID = store.NewID("canvas")
	}
	item, err := s.store.PutCanvasItem(c.Request.Context(), store.CanvasItemInput{
		ID:              req.ID,
		ActorSessionID:  sessionID,
		SourceSessionID: req.SourceSessionID,
		Kind:            req.Kind,
		Title:           req.Title,
		Item:            req.Item,
		Window:          req.Window,
	})
	if err != nil {
		return canvasStoreError(c, s, err)
	}
	c.JSON(http.StatusCreated, item)
	return nil
}

func (s *Server) putCanvasItem(c *cart.Context) error {
	sessionID, _ := c.Param("id")
	itemID, _ := c.Param("itemID")
	var req canvasItemRequest
	if err := decode(c, &req); err != nil {
		return badRequest(c, "invalid json body")
	}
	if strings.TrimSpace(req.ID) == "" {
		req.ID = itemID
	}
	if strings.TrimSpace(req.ID) != itemID {
		return badRequest(c, "item id mismatch")
	}
	item, err := s.store.PutCanvasItem(c.Request.Context(), store.CanvasItemInput{
		ID:              req.ID,
		ActorSessionID:  sessionID,
		SourceSessionID: req.SourceSessionID,
		Kind:            req.Kind,
		Title:           req.Title,
		Item:            req.Item,
		Window:          req.Window,
	})
	if err != nil {
		return canvasStoreError(c, s, err)
	}
	c.JSON(http.StatusOK, item)
	return nil
}

func (s *Server) patchCanvasItem(c *cart.Context) error {
	sessionID, _ := c.Param("id")
	itemID, _ := c.Param("itemID")
	var req canvasItemWindowRequest
	if err := decode(c, &req); err != nil && !errors.Is(err, io.EOF) {
		return badRequest(c, "invalid json body")
	}
	item, err := s.store.UpdateCanvasItemWindow(c.Request.Context(), store.CanvasItemWindowPatch{
		ActorSessionID: sessionID,
		ItemID:         itemID,
		Window:         req.Window,
	})
	if err != nil {
		return canvasStoreError(c, s, err)
	}
	c.JSON(http.StatusOK, item)
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

func canvasStoreError(c *cart.Context, s *Server, err error) error {
	if errors.Is(err, store.ErrInvalidCanvas) {
		return badRequest(c, "invalid canvas item")
	}
	return s.fail(c, err)
}

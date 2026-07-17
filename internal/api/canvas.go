package api

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"

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

type closedCanvasItemRequest struct {
	ID           string          `json:"id"`
	SourceItemID string          `json:"sourceItemID"`
	Kind         string          `json:"kind"`
	Title        string          `json:"title"`
	Item         json.RawMessage `json:"item"`
	Window       json.RawMessage `json:"window"`
	ClosedAt     time.Time       `json:"closedAt"`
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

func (s *Server) listSavedCanvasItems(c *cart.Context) error {
	sessionID, _ := c.Param("id")
	items, err := s.store.ListSavedCanvasItems(c.Request.Context(), sessionID)
	if err != nil {
		return s.fail(c, err)
	}
	c.JSON(http.StatusOK, map[string]any{"items": items})
	return nil
}

func (s *Server) saveCanvasItem(c *cart.Context) error {
	sessionID, _ := c.Param("id")
	itemID, _ := c.Param("itemID")
	result, err := s.store.SaveCanvasItem(c.Request.Context(), sessionID, itemID, store.NewID("saved_canvas"))
	if err != nil {
		return canvasStoreError(c, s, err)
	}
	c.JSON(http.StatusOK, result)
	return nil
}

func (s *Server) openSavedCanvasItem(c *cart.Context) error {
	sessionID, _ := c.Param("id")
	savedID, _ := c.Param("savedID")
	item, err := s.store.OpenSavedCanvasItem(c.Request.Context(), sessionID, savedID, store.NewID("canvas"))
	if err != nil {
		return canvasStoreError(c, s, err)
	}
	c.JSON(http.StatusOK, item)
	return nil
}

func (s *Server) deleteSavedCanvasItem(c *cart.Context) error {
	sessionID, _ := c.Param("id")
	savedID, _ := c.Param("savedID")
	if err := s.store.DeleteSavedCanvasItem(c.Request.Context(), sessionID, savedID); err != nil {
		return canvasStoreError(c, s, err)
	}
	c.Response.WriteHeader(http.StatusNoContent)
	return nil
}

func (s *Server) listClosedCanvasItems(c *cart.Context) error {
	sessionID, _ := c.Param("id")
	limit, err := closedCanvasLimit(c.Request.URL.Query().Get("limit"))
	if err != nil {
		return badRequest(c, "invalid limit")
	}
	items, err := s.store.ListClosedCanvasItems(c.Request.Context(), sessionID, limit)
	if err != nil {
		return s.fail(c, err)
	}
	c.JSON(http.StatusOK, map[string]any{"items": items})
	return nil
}

func (s *Server) createClosedCanvasItem(c *cart.Context) error {
	sessionID, _ := c.Param("id")
	var req closedCanvasItemRequest
	if err := decode(c, &req); err != nil {
		return badRequest(c, "invalid json body")
	}
	if strings.TrimSpace(req.ID) == "" {
		req.ID = store.NewID("closed_canvas")
	}
	item, err := s.store.PutClosedCanvasItem(c.Request.Context(), store.ClosedCanvasItemInput{
		ID:             req.ID,
		SourceItemID:   req.SourceItemID,
		ActorSessionID: sessionID,
		Kind:           req.Kind,
		Title:          req.Title,
		Item:           req.Item,
		Window:         req.Window,
		ClosedAt:       req.ClosedAt,
	}, store.ClosedCanvasKeepLimit)
	if err != nil {
		return canvasStoreError(c, s, err)
	}
	c.JSON(http.StatusCreated, item)
	return nil
}

func (s *Server) deleteClosedCanvasItem(c *cart.Context) error {
	sessionID, _ := c.Param("id")
	closedID, _ := c.Param("closedID")
	if err := s.store.DeleteClosedCanvasItem(c.Request.Context(), sessionID, closedID); err != nil {
		return s.fail(c, err)
	}
	c.Response.WriteHeader(http.StatusNoContent)
	return nil
}

func (s *Server) clearClosedCanvasItems(c *cart.Context) error {
	sessionID, _ := c.Param("id")
	if err := s.store.ClearClosedCanvasItems(c.Request.Context(), sessionID); err != nil {
		return s.fail(c, err)
	}
	c.Response.WriteHeader(http.StatusNoContent)
	return nil
}

func canvasStoreError(c *cart.Context, s *Server, err error) error {
	if errors.Is(err, store.ErrCanvasConflict) {
		c.JSON(http.StatusConflict, map[string]string{"error": "saved_canvas_conflict"})
		return nil
	}
	if errors.Is(err, store.ErrInvalidCanvas) {
		return badRequest(c, "invalid canvas item")
	}
	return s.fail(c, err)
}

func closedCanvasLimit(raw string) (int, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return store.ClosedCanvasDefaultLimit, nil
	}
	limit, err := strconv.Atoi(raw)
	if err != nil {
		return 0, err
	}
	if limit <= 0 {
		return 0, errors.New("invalid limit")
	}
	if limit > store.ClosedCanvasMaxLimit {
		limit = store.ClosedCanvasMaxLimit
	}
	return limit, nil
}

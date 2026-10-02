package api

import (
	"fmt"
	"github.com/teatak/cart/v3"
	"github.com/teatak/pudding-core/internal/canvas"
	"github.com/teatak/pudding-core/internal/store"
	"net/http"
	"time"
)

type canvasEntityInput struct {
	Source     string `json:"source"`
	EntityType string `json:"entityType"`
	EntityID   string `json:"entityID"`
}

func (s *Server) listCanvasLinks(c *cart.Context) error {
	id, _ := c.Param("canvasID")
	if _, err := s.store.GetCanvas(c.Request.Context(), id); err != nil {
		return s.canvasError(c, err)
	}
	links, err := s.store.ListCanvasLinks(c.Request.Context(), id)
	if err != nil {
		return s.canvasError(c, err)
	}
	c.JSON(http.StatusOK, map[string]any{"links": links})
	return nil
}
func (s *Server) putCanvasLink(c *cart.Context) error {
	id, _ := c.Param("canvasID")
	var req struct {
		ExpectedRevision int64             `json:"expectedRevision"`
		RevisionHash     string            `json:"revisionHash"`
		Confirm          bool              `json:"confirm"`
		Left             canvasEntityInput `json:"left"`
		Right            canvasEntityInput `json:"right"`
	}
	if err := decodeCanvas(c, &req); err != nil {
		return badRequest(c, err.Error())
	}
	if !req.Confirm {
		return badRequest(c, "link confirmation required")
	}
	w, err := s.store.GetCanvas(c.Request.Context(), id)
	if err != nil {
		return s.canvasError(c, err)
	}
	if _, err = s.store.GetCanvasRevision(c.Request.Context(), id, req.RevisionHash); err != nil {
		return s.canvasError(c, err)
	}
	p, err := canvas.ReadPackage(s.home, id, req.RevisionHash)
	if err != nil {
		return s.canvasError(c, err)
	}
	m, _, err := p.Validate()
	if err != nil {
		return s.canvasError(c, err)
	}
	resolve := func(input canvasEntityInput) (store.CanvasEntity, error) {
		source, ok := m.Sources[input.Source]
		if !ok || input.EntityType == "" || len(input.EntityType) > 100 || input.EntityID == "" || len(input.EntityID) > 500 {
			return store.CanvasEntity{}, fmt.Errorf("invalid entity reference")
		}
		binding, _, err := s.resolveCanvasSource(c.Request.Context(), w, input.Source, source, "")
		if err != nil {
			return store.CanvasEntity{}, err
		}
		return store.CanvasEntity{AppID: source.AppID, ConnectionID: binding.ConnectionID, EntityType: input.EntityType, EntityID: input.EntityID}, nil
	}
	left, err := resolve(req.Left)
	if err != nil {
		return badRequest(c, err.Error())
	}
	right, err := resolve(req.Right)
	if err != nil {
		return badRequest(c, err.Error())
	}
	if left == right {
		return badRequest(c, "distinct entity references required")
	}
	link := &store.CanvasLink{ID: store.NewID("link"), CanvasID: id, Left: left, Right: right, CreatedAt: time.Now().UTC()}
	if err = s.store.PutCanvasLink(c.Request.Context(), link, req.ExpectedRevision); err != nil {
		return s.canvasError(c, err)
	}
	return s.listCanvasLinks(c)
}
func (s *Server) deleteCanvasLink(c *cart.Context) error {
	id, _ := c.Param("canvasID")
	linkID, _ := c.Param("linkID")
	var req struct {
		ExpectedRevision int64 `json:"expectedRevision"`
		Confirm          bool  `json:"confirm"`
	}
	if err := decodeCanvas(c, &req); err != nil {
		return badRequest(c, err.Error())
	}
	if !req.Confirm {
		return badRequest(c, "link removal confirmation required")
	}
	if err := s.store.DeleteCanvasLink(c.Request.Context(), id, linkID, req.ExpectedRevision); err != nil {
		return s.canvasError(c, err)
	}
	return s.listCanvasLinks(c)
}

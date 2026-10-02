package api

import (
	"fmt"
	"github.com/teatak/cart/v3"
	"github.com/teatak/pudding-core/internal/store"
	"github.com/teatak/pudding-core/internal/widget"
	"net/http"
	"time"
)

type widgetEntityInput struct {
	Source     string `json:"source"`
	EntityType string `json:"entityType"`
	EntityID   string `json:"entityID"`
}

func (s *Server) listWidgetLinks(c *cart.Context) error {
	id, _ := c.Param("itemID")
	if _, err := s.store.GetStudioItem(c.Request.Context(), id); err != nil {
		return s.studioItemError(c, err)
	}
	links, err := s.store.ListWidgetLinks(c.Request.Context(), id)
	if err != nil {
		return s.studioItemError(c, err)
	}
	c.JSON(http.StatusOK, map[string]any{"links": links})
	return nil
}
func (s *Server) putWidgetLink(c *cart.Context) error {
	id, _ := c.Param("itemID")
	var req struct {
		ExpectedRevision int64             `json:"expectedRevision"`
		RevisionHash     string            `json:"revisionHash"`
		Confirm          bool              `json:"confirm"`
		Left             widgetEntityInput `json:"left"`
		Right            widgetEntityInput `json:"right"`
	}
	if err := decodeStudioRequest(c, &req); err != nil {
		return badRequest(c, err.Error())
	}
	if !req.Confirm {
		return badRequest(c, "link confirmation required")
	}
	w, err := s.store.GetStudioItem(c.Request.Context(), id)
	if err != nil {
		return s.studioItemError(c, err)
	}
	if _, err = s.store.GetStudioItemRevision(c.Request.Context(), id, req.RevisionHash); err != nil {
		return s.studioItemError(c, err)
	}
	p, err := widget.ReadPackage(s.home, id, req.RevisionHash)
	if err != nil {
		return s.studioItemError(c, err)
	}
	m, _, err := p.Validate()
	if err != nil {
		return s.studioItemError(c, err)
	}
	resolve := func(input widgetEntityInput) (store.WidgetEntity, error) {
		source, ok := m.Sources[input.Source]
		if !ok || input.EntityType == "" || len(input.EntityType) > 100 || input.EntityID == "" || len(input.EntityID) > 500 {
			return store.WidgetEntity{}, fmt.Errorf("invalid entity reference")
		}
		binding, _, err := s.resolveWidgetSource(c.Request.Context(), w, input.Source, source, "")
		if err != nil {
			return store.WidgetEntity{}, err
		}
		return store.WidgetEntity{PluginID: source.PluginID, ConnectionID: binding.ConnectionID, EntityType: input.EntityType, EntityID: input.EntityID}, nil
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
	link := &store.WidgetLink{ID: store.NewID("link"), ItemID: id, Left: left, Right: right, CreatedAt: time.Now().UTC()}
	if err = s.store.PutWidgetLink(c.Request.Context(), link, req.ExpectedRevision); err != nil {
		return s.studioItemError(c, err)
	}
	return s.listWidgetLinks(c)
}
func (s *Server) deleteWidgetLink(c *cart.Context) error {
	id, _ := c.Param("itemID")
	linkID, _ := c.Param("linkID")
	var req struct {
		ExpectedRevision int64 `json:"expectedRevision"`
		Confirm          bool  `json:"confirm"`
	}
	if err := decodeStudioRequest(c, &req); err != nil {
		return badRequest(c, err.Error())
	}
	if !req.Confirm {
		return badRequest(c, "link removal confirmation required")
	}
	if err := s.store.DeleteWidgetLink(c.Request.Context(), id, linkID, req.ExpectedRevision); err != nil {
		return s.studioItemError(c, err)
	}
	return s.listWidgetLinks(c)
}

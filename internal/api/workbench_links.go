package api

import (
	"fmt"
	"github.com/teatak/cart/v3"
	"github.com/teatak/pudding-core/internal/store"
	"github.com/teatak/pudding-core/internal/workbench"
	"net/http"
	"time"
)

type workbenchEntityInput struct {
	Source     string `json:"source"`
	EntityType string `json:"entityType"`
	EntityID   string `json:"entityID"`
}

func (s *Server) listWorkbenchLinks(c *cart.Context) error {
	id, _ := c.Param("workbenchID")
	if _, err := s.store.GetWorkbench(c.Request.Context(), id); err != nil {
		return s.workbenchError(c, err)
	}
	links, err := s.store.ListWorkbenchLinks(c.Request.Context(), id)
	if err != nil {
		return s.workbenchError(c, err)
	}
	c.JSON(http.StatusOK, map[string]any{"links": links})
	return nil
}
func (s *Server) putWorkbenchLink(c *cart.Context) error {
	id, _ := c.Param("workbenchID")
	var req struct {
		ExpectedRevision int64                `json:"expectedRevision"`
		RevisionHash     string               `json:"revisionHash"`
		Confirm          bool                 `json:"confirm"`
		Left             workbenchEntityInput `json:"left"`
		Right            workbenchEntityInput `json:"right"`
	}
	if err := decodeWorkbench(c, &req); err != nil {
		return badRequest(c, err.Error())
	}
	if !req.Confirm {
		return badRequest(c, "link confirmation required")
	}
	w, err := s.store.GetWorkbench(c.Request.Context(), id)
	if err != nil {
		return s.workbenchError(c, err)
	}
	if _, err = s.store.GetWorkbenchRevision(c.Request.Context(), id, req.RevisionHash); err != nil {
		return s.workbenchError(c, err)
	}
	p, err := workbench.ReadPackage(s.home, id, req.RevisionHash)
	if err != nil {
		return s.workbenchError(c, err)
	}
	m, _, err := p.Validate()
	if err != nil {
		return s.workbenchError(c, err)
	}
	resolve := func(input workbenchEntityInput) (store.WorkbenchEntity, error) {
		source, ok := m.Sources[input.Source]
		if !ok || input.EntityType == "" || len(input.EntityType) > 100 || input.EntityID == "" || len(input.EntityID) > 500 {
			return store.WorkbenchEntity{}, fmt.Errorf("invalid entity reference")
		}
		binding, _, err := s.resolveWorkbenchSource(c.Request.Context(), w, input.Source, source, "")
		if err != nil {
			return store.WorkbenchEntity{}, err
		}
		return store.WorkbenchEntity{AppID: source.AppID, ConnectionID: binding.ConnectionID, EntityType: input.EntityType, EntityID: input.EntityID}, nil
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
	link := &store.WorkbenchLink{ID: store.NewID("link"), WorkbenchID: id, Left: left, Right: right, CreatedAt: time.Now().UTC()}
	if err = s.store.PutWorkbenchLink(c.Request.Context(), link, req.ExpectedRevision); err != nil {
		return s.workbenchError(c, err)
	}
	return s.listWorkbenchLinks(c)
}
func (s *Server) deleteWorkbenchLink(c *cart.Context) error {
	id, _ := c.Param("workbenchID")
	linkID, _ := c.Param("linkID")
	var req struct {
		ExpectedRevision int64 `json:"expectedRevision"`
		Confirm          bool  `json:"confirm"`
	}
	if err := decodeWorkbench(c, &req); err != nil {
		return badRequest(c, err.Error())
	}
	if !req.Confirm {
		return badRequest(c, "link removal confirmation required")
	}
	if err := s.store.DeleteWorkbenchLink(c.Request.Context(), id, linkID, req.ExpectedRevision); err != nil {
		return s.workbenchError(c, err)
	}
	return s.listWorkbenchLinks(c)
}

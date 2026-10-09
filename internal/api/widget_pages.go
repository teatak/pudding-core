package api

import (
	"encoding/json"
	"github.com/teatak/cart/v3"
	"net/http"
)

func (s *Server) openWidgetPage(c *cart.Context) error {
	itemID, _ := c.Param("itemID")
	var in struct {
		Scope        string `json:"scope"`
		RevisionHash string `json:"revisionHash"`
		TargetID     string `json:"targetID"`
	}
	if err := decodeStudioRequest(c, &in); err != nil {
		return badRequest(c, err.Error())
	}
	page, run, err := s.engine.OpenWidgetPage(c.Request.Context(), itemID, in.Scope, in.RevisionHash, in.TargetID)
	if err != nil {
		return s.studioItemError(c, err)
	}
	// Participant routing is host-only; the guest receives publicWidgetRun.
	c.JSON(http.StatusOK, map[string]any{"page": page, "run": run})
	return nil
}
func (s *Server) writeWidgetPage(c *cart.Context) error {
	id, _ := c.Param("pageID")
	var in struct {
		TargetID        string          `json:"targetID"`
		ExpectedVersion *int64          `json:"expectedVersion"`
		Data            json.RawMessage `json:"data"`
	}
	if err := decodeStudioRequest(c, &in); err != nil {
		return badRequest(c, err.Error())
	}
	if in.ExpectedVersion == nil {
		return badRequest(c, "expectedVersion is required")
	}
	saved, err := s.store.WriteWidgetPage(c.Request.Context(), id, in.TargetID, *in.ExpectedVersion, in.Data)
	if err != nil {
		return s.studioItemError(c, err)
	}
	c.JSON(http.StatusOK, saved)
	return nil
}
func (s *Server) closeWidgetPages(c *cart.Context) error {
	itemID, _ := c.Param("itemID")
	var in struct {
		Scope string `json:"scope"`
	}
	if err := decodeStudioRequest(c, &in); err != nil {
		return badRequest(c, err.Error())
	}
	if in.Scope == "" {
		return badRequest(c, "scope is required")
	}
	if err := s.engine.CloseWidgetPages(c.Request.Context(), itemID, in.Scope); err != nil {
		return s.studioItemError(c, err)
	}
	c.JSON(http.StatusOK, map[string]bool{"closed": true})
	return nil
}

func (s *Server) selectWidgetPage(c *cart.Context) error {
	id, _ := c.Param("itemID")
	var in struct {
		Scope        string `json:"scope"`
		RevisionHash string `json:"revisionHash"`
	}
	if err := decodeStudioRequest(c, &in); err != nil {
		return badRequest(c, err.Error())
	}
	hash, err := s.engine.SelectWidgetPage(c.Request.Context(), id, in.Scope, in.RevisionHash)
	if err != nil {
		return s.studioItemError(c, err)
	}
	c.JSON(http.StatusOK, map[string]string{"revisionHash": hash})
	return nil
}
func (s *Server) authorizeWidgetPage(c *cart.Context) error {
	id, _ := c.Param("itemID")
	var in struct {
		TargetID     string `json:"targetID"`
		RevisionHash string `json:"revisionHash"`
	}
	if err := decodeStudioRequest(c, &in); err != nil {
		return badRequest(c, err.Error())
	}
	if err := s.store.AuthorizeWidgetPage(c.Request.Context(), id, in.RevisionHash, in.TargetID); err != nil {
		return s.studioItemError(c, err)
	}
	c.JSON(http.StatusOK, map[string]bool{"authorized": true})
	return nil
}

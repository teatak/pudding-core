package api

import (
	"encoding/json"
	"net/http"

	"github.com/teatak/cart/v3"
)

func (s *Server) getWidgetData(c *cart.Context) error {
	id, _ := c.Param("itemID")
	data, err := s.store.GetWidgetData(c.Request.Context(), id)
	if err != nil {
		return s.studioItemError(c, err)
	}
	c.JSON(http.StatusOK, data)
	return nil
}

func (s *Server) writeWidgetData(c *cart.Context) error {
	id, _ := c.Param("itemID")
	var req struct {
		TargetID        string          `json:"targetID"`
		RevisionHash    string          `json:"revisionHash"`
		ExpectedVersion *int64          `json:"expectedVersion"`
		Data            json.RawMessage `json:"data"`
	}
	if err := decodeStudioRequest(c, &req); err != nil {
		return badRequest(c, err.Error())
	}
	if req.ExpectedVersion == nil {
		return badRequest(c, "expectedVersion is required")
	}
	data, err := s.store.WriteWidgetData(c.Request.Context(), id, req.RevisionHash, req.TargetID, *req.ExpectedVersion, req.Data)
	if err != nil {
		return s.studioItemError(c, err)
	}
	c.JSON(http.StatusOK, data)
	return nil
}

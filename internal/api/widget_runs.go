package api

import (
	"net/http"

	"github.com/teatak/cart/v3"
	"github.com/teatak/pudding-core/internal/engine"
)

func (s *Server) createWidgetRun(c *cart.Context) error {
	itemID, _ := c.Param("itemID")
	var in engine.WidgetRunCreate
	if err := decodeStudioRequest(c, &in); err != nil {
		return badRequest(c, err.Error())
	}
	run, err := s.engine.CreateWidgetRun(c.Request.Context(), itemID, in)
	if err != nil {
		return badRequest(c, err.Error())
	}
	c.JSON(http.StatusOK, run)
	return nil
}
func (s *Server) getWidgetRun(c *cart.Context) error {
	id, _ := c.Param("runID")
	run, err := s.engine.WidgetRun(c.Request.Context(), id, false)
	if err != nil {
		return badRequest(c, err.Error())
	}
	c.JSON(http.StatusOK, run)
	return nil
}
func (s *Server) updateWidgetRun(c *cart.Context) error {
	id, _ := c.Param("runID")
	var in struct {
		Action string `json:"action"`
	}
	if err := decodeStudioRequest(c, &in); err != nil {
		return badRequest(c, err.Error())
	}
	var run *engine.WidgetRun
	var err error
	if in.Action == "heartbeat" {
		run, err = s.engine.WidgetRun(c.Request.Context(), id, true)
	} else {
		run, err = s.engine.ChangeWidgetRun(c.Request.Context(), id, in.Action)
	}
	if err != nil {
		return badRequest(c, err.Error())
	}
	c.JSON(http.StatusOK, run)
	return nil
}
func (s *Server) notifyWidgetRun(c *cart.Context) error {
	id, _ := c.Param("runID")
	var in struct {
		Notification engine.WidgetNotification `json:"notification"`
		Actor        string                    `json:"actor"`
		StateVersion *int64                    `json:"stateVersion"`
	}
	if err := decodeStudioRequest(c, &in); err != nil {
		return badRequest(c, err.Error())
	}
	if in.StateVersion == nil {
		return badRequest(c, "stateVersion required")
	}
	run, err := s.engine.NotifyWidgetRun(c.Request.Context(), id, in.Notification, *in.StateVersion, in.Actor)
	if err != nil {
		return badRequest(c, err.Error())
	}
	c.JSON(http.StatusOK, run)
	return nil
}
func (s *Server) setWidgetRequests(c *cart.Context) error {
	id, _ := c.Param("runID")
	var in struct {
		Requests []engine.WidgetRequestKey `json:"requests"`
	}
	if err := decodeStudioRequest(c, &in); err != nil {
		return badRequest(c, err.Error())
	}
	if in.Requests == nil {
		return badRequest(c, "requests required; use an empty array to clear")
	}
	run, err := s.engine.SetWidgetRequests(c.Request.Context(), id, in.Requests)
	if err != nil {
		return badRequest(c, err.Error())
	}
	c.JSON(http.StatusOK, run)
	return nil
}
func (s *Server) authorizeWidgetRun(c *cart.Context) error {
	id, _ := c.Param("runID")
	sessionID, _ := c.Param("id")
	var in struct {
		NotificationID string `json:"notificationID"`
	}
	if err := decodeStudioRequest(c, &in); err != nil {
		return badRequest(c, err.Error())
	}
	p, err := s.engine.AuthorizeWidgetRun(c.Request.Context(), id, sessionID, in.NotificationID)
	if err != nil {
		return badRequest(c, err.Error())
	}
	c.JSON(http.StatusOK, p)
	return nil
}

func (s *Server) setWidgetParticipants(c *cart.Context) error {
	id, _ := c.Param("runID")
	var in struct {
		Participants []engine.WidgetParticipant `json:"participants"`
	}
	if err := decodeStudioRequest(c, &in); err != nil {
		return badRequest(c, err.Error())
	}
	run, err := s.engine.SetWidgetParticipants(c.Request.Context(), id, in.Participants)
	if err != nil {
		return badRequest(c, err.Error())
	}
	c.JSON(http.StatusOK, run)
	return nil
}

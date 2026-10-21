package api

import (
	"context"
	"errors"
	"github.com/teatak/cart/v3"
	"github.com/teatak/pudding-core/internal/plugin"
	"net/http"
)

type sessionPluginConnections interface {
	SessionConnection(context.Context, string, string) (*plugin.SessionConnectionView, error)
	SetSessionConnection(context.Context, string, string, string) (*plugin.SessionConnectionView, error)
}

func (s *Server) getSessionPluginConnection(c *cart.Context) error {
	return s.sessionPluginConnection(c, false)
}
func (s *Server) setSessionPluginConnection(c *cart.Context) error {
	return s.sessionPluginConnection(c, true)
}
func (s *Server) sessionPluginConnection(c *cart.Context, write bool) error {
	sessionID, _ := c.Param("id")
	pluginID, _ := c.Param("pluginID")
	if _, err := s.store.GetSession(c.Request.Context(), sessionID); err != nil {
		return s.fail(c, err)
	}
	service, ok := s.plugins.(sessionPluginConnections)
	if !ok {
		return badRequest(c, "session connection selection unavailable")
	}
	var view *plugin.SessionConnectionView
	var err error
	if write {
		var req struct {
			ConnectionID *string `json:"connectionID"`
		}
		if decode(c, &req) != nil || req.ConnectionID == nil {
			return badRequest(c, "connectionID is required")
		}
		view, err = service.SetSessionConnection(c.Request.Context(), sessionID, pluginID, *req.ConnectionID)
	} else {
		view, err = service.SessionConnection(c.Request.Context(), sessionID, pluginID)
	}
	if errors.Is(err, plugin.ErrInvalidID) {
		return badRequest(c, err.Error())
	}
	if errors.Is(err, plugin.ErrNotFound) {
		c.JSON(http.StatusNotFound, map[string]string{"error": "plugin_not_found"})
		return nil
	}
	if err != nil {
		return s.fail(c, err)
	}
	c.JSON(http.StatusOK, view)
	return nil
}

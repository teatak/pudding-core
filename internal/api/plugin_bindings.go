package api

import (
	"context"
	"errors"
	"net/http"

	"github.com/teatak/cart/v3"
	"github.com/teatak/pudding-core/internal/config"
	"github.com/teatak/pudding-core/internal/plugin"
)

type pluginBindingService interface {
	ConnectionBindings(context.Context, string) (*plugin.ConnectionBindingsView, error)
	PatchConnectionBindings(context.Context, string, map[string]string) (*plugin.ConnectionBindingsView, error)
}

func (s *Server) getPluginConnectionBindings(c *cart.Context) error {
	return s.pluginConnectionBindings(c, false)
}
func (s *Server) patchPluginConnectionBindings(c *cart.Context) error {
	return s.pluginConnectionBindings(c, true)
}
func (s *Server) pluginConnectionBindings(c *cart.Context, patch bool) error {
	service, ok := s.plugins.(pluginBindingService)
	if !ok {
		c.JSON(http.StatusNotImplemented, map[string]string{"error": "plugin_bindings_unavailable"})
		return nil
	}
	id, _ := c.Param("id")
	var view *plugin.ConnectionBindingsView
	var err error
	if patch {
		var req plugin.ConnectionBindingsView
		if decode(c, &req) != nil || req.Bindings == nil {
			return badRequest(c, "bindings is required")
		}
		view, err = service.PatchConnectionBindings(c.Request.Context(), id, req.Bindings)
	} else {
		view, err = service.ConnectionBindings(c.Request.Context(), id)
	}
	if errors.Is(err, plugin.ErrInvalidID) || errors.Is(err, config.ErrInvalidSetting) {
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

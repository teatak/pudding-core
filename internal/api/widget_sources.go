package api

import (
	"context"
	"github.com/teatak/cart/v3"
	"github.com/teatak/pudding-core/internal/config"
	"net/http"
)

type widgetSourcesConfig interface {
	WidgetSources(context.Context) ([]config.WidgetSource, error)
	AddWidgetSource(context.Context, string) (config.WidgetSource, error)
	RemoveWidgetSource(context.Context, string) error
}

func (s *Server) widgetSourcesConfig(c *cart.Context) (widgetSourcesConfig, bool) {
	cfg, ok := s.config.(widgetSourcesConfig)
	if !ok {
		c.JSON(http.StatusInternalServerError, map[string]string{"error": "widget_sources_config_unavailable"})
	}
	return cfg, ok
}
func (s *Server) listWidgetSources(c *cart.Context) error {
	cfg, ok := s.widgetSourcesConfig(c)
	if !ok {
		return nil
	}
	sources, err := cfg.WidgetSources(c.Request.Context())
	if err != nil {
		return s.fail(c, err)
	}
	c.JSON(http.StatusOK, map[string]any{"sources": sources})
	return nil
}
func (s *Server) addWidgetSource(c *cart.Context) error {
	cfg, ok := s.widgetSourcesConfig(c)
	if !ok {
		return nil
	}
	var req struct {
		URL string `json:"url"`
	}
	if err := decodeStudioRequest(c, &req); err != nil {
		return badRequest(c, err.Error())
	}
	source, err := cfg.AddWidgetSource(c.Request.Context(), req.URL)
	if err != nil {
		return badRequest(c, err.Error())
	}
	c.JSON(http.StatusOK, source)
	return nil
}
func (s *Server) removeWidgetSource(c *cart.Context) error {
	cfg, ok := s.widgetSourcesConfig(c)
	if !ok {
		return nil
	}
	id, _ := c.Param("sourceID")
	if err := cfg.RemoveWidgetSource(c.Request.Context(), id); err != nil {
		return badRequest(c, err.Error())
	}
	c.JSON(http.StatusOK, map[string]string{"removed": id})
	return nil
}

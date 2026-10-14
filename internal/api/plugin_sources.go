package api

import (
	"context"
	"github.com/teatak/cart/v3"
	"github.com/teatak/pudding-core/internal/config"
	"net/http"
)

type pluginSourcesConfig interface {
	PluginSources(context.Context) ([]config.PluginSource, error)
	AddPluginSource(context.Context, string) (config.PluginSource, error)
	RemovePluginSource(context.Context, string) error
}

func (s *Server) pluginSourcesConfig(c *cart.Context) (pluginSourcesConfig, bool) {
	cfg, ok := s.config.(pluginSourcesConfig)
	if !ok {
		c.JSON(http.StatusInternalServerError, map[string]string{"error": "plugin_sources_config_unavailable"})
	}
	return cfg, ok
}
func (s *Server) listPluginSources(c *cart.Context) error {
	cfg, ok := s.pluginSourcesConfig(c)
	if !ok {
		return nil
	}
	sources, err := cfg.PluginSources(c.Request.Context())
	if err != nil {
		return s.fail(c, err)
	}
	c.JSON(http.StatusOK, map[string]any{"sources": sources})
	return nil
}
func (s *Server) addPluginSource(c *cart.Context) error {
	cfg, ok := s.pluginSourcesConfig(c)
	if !ok {
		return nil
	}
	var req struct {
		URL string `json:"url"`
	}
	if err := decodeStudioRequest(c, &req); err != nil {
		return badRequest(c, err.Error())
	}
	source, err := cfg.AddPluginSource(c.Request.Context(), req.URL)
	if err != nil {
		return badRequest(c, err.Error())
	}
	c.JSON(http.StatusOK, source)
	return nil
}
func (s *Server) removePluginSource(c *cart.Context) error {
	cfg, ok := s.pluginSourcesConfig(c)
	if !ok {
		return nil
	}
	id, _ := c.Param("sourceID")
	if err := cfg.RemovePluginSource(c.Request.Context(), id); err != nil {
		return badRequest(c, err.Error())
	}
	c.JSON(http.StatusOK, map[string]string{"removed": id})
	return nil
}

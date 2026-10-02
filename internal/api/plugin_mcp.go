package api

import (
	"context"
	"errors"
	"net/http"
	"sort"
	"strings"

	"github.com/teatak/cart/v3"
	"github.com/teatak/pudding-core/internal/plugin"
	"github.com/teatak/pudding-core/internal/tool"
)

type pluginEndpointBindingService interface {
	ListEndpointBindings(ctx context.Context, kind string) ([]*plugin.EndpointBinding, error)
}

type pluginMCPStatusView struct {
	PluginID  string                        `json:"pluginID"`
	Endpoints []tool.PluginMCPProbeEndpoint `json:"endpoints"`
}

func (s *Server) getPluginMCPStatus(c *cart.Context) error {
	pluginID, _ := c.Param("id")
	pluginID = strings.TrimSpace(pluginID)
	if pluginID == "" {
		return badRequest(c, "plugin id is required")
	}
	def, err := s.getPluginDefinition(c.Request.Context(), pluginID)
	if err != nil {
		if errors.Is(err, plugin.ErrNotFound) {
			c.JSON(http.StatusNotFound, map[string]string{"error": "plugin_not_found"})
			return nil
		}
		return s.fail(c, err)
	}
	source, ok := s.plugins.(pluginEndpointBindingService)
	if !ok {
		c.JSON(http.StatusInternalServerError, map[string]string{"error": "plugin_endpoint_service_unavailable"})
		return nil
	}
	bindings, err := source.ListEndpointBindings(c.Request.Context(), plugin.EndpointKindMCP)
	if err != nil {
		return s.fail(c, err)
	}
	configuredByEndpoint, err := s.pluginMCPConfiguredEndpoints(c.Request.Context(), pluginID, def)
	if err != nil {
		return s.fail(c, err)
	}
	byEndpoint := map[string][]*plugin.EndpointBinding{}
	for _, binding := range bindings {
		if binding == nil || binding.PluginID != pluginID || binding.Endpoint.Kind != plugin.EndpointKindMCP {
			continue
		}
		byEndpoint[binding.EndpointName] = append(byEndpoint[binding.EndpointName], binding)
	}
	runner := tool.NewPluginMCPRunner(nil)
	endpoints := make([]tool.PluginMCPProbeEndpoint, 0)
	for _, name := range sortedMCPEndpointNames(def.Endpoints) {
		matches := byEndpoint[name]
		if len(matches) == 0 {
			endpoints = append(endpoints, tool.PluginMCPProbeEndpoint{
				PluginID:     pluginID,
				EndpointName: name,
				Transport:    strings.TrimSpace(def.Endpoints[name].Transport),
				Configured:   configuredByEndpoint[name],
				Status:       tool.PluginMCPProbeNeedsConnection,
			})
			continue
		}
		for _, binding := range matches {
			endpoint := runner.ProbeBinding(c.Request.Context(), binding)
			endpoint.Configured = configuredByEndpoint[name]
			endpoints = append(endpoints, endpoint)
		}
	}
	c.JSON(http.StatusOK, pluginMCPStatusView{PluginID: pluginID, Endpoints: endpoints})
	return nil
}

func (s *Server) pluginMCPConfiguredEndpoints(ctx context.Context, pluginID string, def *plugin.Definition) (map[string]bool, error) {
	out := map[string]bool{}
	cfg, ok := s.plugins.(pluginMCPOverrideConfig)
	if !ok || def == nil {
		return out, nil
	}
	for _, name := range sortedMCPEndpointNames(def.Endpoints) {
		_, configured, err := cfg.GetMCPOverride(ctx, pluginID, name)
		if err != nil {
			return nil, err
		}
		out[name] = configured
	}
	return out, nil
}

func sortedMCPEndpointNames(endpoints map[string]plugin.Endpoint) []string {
	names := make([]string, 0, len(endpoints))
	for name, endpoint := range endpoints {
		if endpoint.Kind == plugin.EndpointKindMCP {
			names = append(names, name)
		}
	}
	sort.Strings(names)
	return names
}

package api

import (
	"context"
	"errors"
	"net/http"
	"sort"
	"strings"

	"github.com/teatak/cart/v3"
	"github.com/teatak/pudding-core/internal/app"
	"github.com/teatak/pudding-core/internal/tool"
)

type appEndpointBindingService interface {
	ListEndpointBindings(ctx context.Context, kind string) ([]*app.EndpointBinding, error)
}

type appMCPStatusView struct {
	AppID     string                     `json:"appID"`
	Endpoints []tool.AppMCPProbeEndpoint `json:"endpoints"`
}

func (s *Server) getAppMCPStatus(c *cart.Context) error {
	appID, _ := c.Param("id")
	appID = strings.TrimSpace(appID)
	if appID == "" {
		return badRequest(c, "app id is required")
	}
	def, err := s.getAppDefinition(c.Request.Context(), appID)
	if err != nil {
		if errors.Is(err, app.ErrNotFound) {
			c.JSON(http.StatusNotFound, map[string]string{"error": "app_not_found"})
			return nil
		}
		return s.fail(c, err)
	}
	source, ok := s.apps.(appEndpointBindingService)
	if !ok {
		c.JSON(http.StatusInternalServerError, map[string]string{"error": "app_endpoint_service_unavailable"})
		return nil
	}
	bindings, err := source.ListEndpointBindings(c.Request.Context(), app.EndpointKindMCP)
	if err != nil {
		return s.fail(c, err)
	}
	byEndpoint := map[string][]*app.EndpointBinding{}
	for _, binding := range bindings {
		if binding == nil || binding.AppID != appID || binding.Endpoint.Kind != app.EndpointKindMCP {
			continue
		}
		byEndpoint[binding.EndpointName] = append(byEndpoint[binding.EndpointName], binding)
	}
	runner := tool.NewAppMCPRunner(nil)
	endpoints := make([]tool.AppMCPProbeEndpoint, 0)
	for _, name := range sortedMCPEndpointNames(def.Endpoints) {
		matches := byEndpoint[name]
		if len(matches) == 0 {
			endpoints = append(endpoints, tool.AppMCPProbeEndpoint{
				AppID:        appID,
				EndpointName: name,
				Transport:    strings.TrimSpace(def.Endpoints[name].Transport),
				Status:       tool.AppMCPProbeNeedsConnection,
			})
			continue
		}
		for _, binding := range matches {
			endpoints = append(endpoints, runner.ProbeBinding(c.Request.Context(), binding))
		}
	}
	c.JSON(http.StatusOK, appMCPStatusView{AppID: appID, Endpoints: endpoints})
	return nil
}

func sortedMCPEndpointNames(endpoints map[string]app.Endpoint) []string {
	names := make([]string, 0, len(endpoints))
	for name, endpoint := range endpoints {
		if endpoint.Kind == app.EndpointKindMCP {
			names = append(names, name)
		}
	}
	sort.Strings(names)
	return names
}

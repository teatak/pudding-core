package api

import (
	"context"
	"encoding/json"
	"net/http"

	"github.com/teatak/cart/v3"
	"github.com/teatak/pudding-core/internal/config"
	"github.com/teatak/pudding-core/internal/provider"
	"github.com/teatak/pudding-core/internal/tool"
)

type builtinToolView struct {
	ID          string          `json:"id"`
	Description string          `json:"description"`
	InputSchema json.RawMessage `json:"inputSchema,omitempty"`
}

type webToolsConfig interface {
	WebTools(context.Context) (*config.WebToolsView, error)
	PatchWebTools(context.Context, config.WebToolsUpdate) (*config.WebToolsView, error)
}

func (s *Server) listBuiltinTools(c *cart.Context) error {
	c.JSON(http.StatusOK, map[string]any{"tools": viewBuiltinTools(tool.BuiltinDefinitions())})
	return nil
}

func (s *Server) getWebTools(c *cart.Context) error {
	cfg, ok := s.webToolsConfig(c)
	if !ok {
		return nil
	}
	view, err := cfg.WebTools(c.Request.Context())
	if err != nil {
		return s.fail(c, err)
	}
	c.JSON(http.StatusOK, view)
	return nil
}

func (s *Server) patchWebTools(c *cart.Context) error {
	cfg, ok := s.webToolsConfig(c)
	if !ok {
		return nil
	}
	var req config.WebToolsUpdate
	if err := decode(c, &req); err != nil {
		return badRequest(c, "invalid json body")
	}
	view, err := cfg.PatchWebTools(c.Request.Context(), req)
	if err != nil {
		return badRequest(c, err.Error())
	}
	c.JSON(http.StatusOK, view)
	return nil
}

func (s *Server) webToolsConfig(c *cart.Context) (webToolsConfig, bool) {
	cfg, ok := s.config.(webToolsConfig)
	if !ok {
		c.JSON(http.StatusInternalServerError, map[string]string{"error": "web_tools_config_unavailable"})
		return nil, false
	}
	return cfg, true
}

func viewBuiltinTools(defs []provider.ToolDef) []builtinToolView {
	views := make([]builtinToolView, 0, len(defs))
	for _, def := range defs {
		views = append(views, builtinToolView{
			ID:          def.Name,
			Description: def.Description,
			InputSchema: def.InputSchema,
		})
	}
	return views
}

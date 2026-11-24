package api

import (
	"errors"
	"github.com/teatak/cart/v3"
	"github.com/teatak/pudding-core/internal/engine"
	"net/http"
)

func (s *Server) localAgentCatalog(c *cart.Context) error {
	catalog, err := s.engine.LocalAgentCatalog(c.Request.Context())
	if err != nil {
		return localAgentFailure(c, err)
	}
	c.JSON(http.StatusOK, catalog)
	return nil
}

func isLocalAgentFailure(err error) bool {
	var handoff *engine.LocalAgentHandoffRequired
	return errors.Is(err, engine.ErrLocalAgent) || errors.Is(err, engine.ErrLocalAgentDesktop) || errors.As(err, &handoff)
}
func localAgentFailure(c *cart.Context, err error) error {
	var handoff *engine.LocalAgentHandoffRequired
	if errors.As(err, &handoff) {
		c.JSON(http.StatusConflict, map[string]any{"error": "local_agent_handoff_required", "detail": err.Error(), "handoff": handoff.Text})
		return nil
	}
	code, status := "local_agent_unavailable", http.StatusBadRequest
	if errors.Is(err, engine.ErrLocalAgentDesktop) {
		code, status = "local_agent_desktop_required", http.StatusForbidden
	}
	c.JSON(status, map[string]string{"error": code, "detail": err.Error()})
	return nil
}

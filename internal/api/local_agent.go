package api

import (
	"errors"
	"net/http"

	"github.com/teatak/cart/v3"
	"github.com/teatak/pudding-core/internal/acp"
	"github.com/teatak/pudding-core/internal/engine"
)

func (s *Server) localAgentCatalog(c *cart.Context) error {
	catalog, err := s.engine.LocalAgentCatalog(c.Request.Context())
	if err != nil {
		return localAgentFailure(c, err)
	}
	c.JSON(http.StatusOK, catalog)
	return nil
}

func (s *Server) localAgentQuota(c *cart.Context) error {
	quota, err := s.engine.LocalAgentQuota(c.Request.Context(), c.Request.URL.Query().Get("refresh") == "1")
	if err == nil {
		c.JSON(http.StatusOK, quota)
		return nil
	}
	if errors.Is(err, engine.ErrLocalAgentDesktop) {
		return localAgentFailure(c, err)
	}
	code := "local_agent_quota_unavailable"
	switch {
	case errors.Is(err, acp.ErrQuotaCLIUnavailable):
		code = "local_agent_quota_cli_unavailable"
	case errors.Is(err, acp.ErrQuotaCLITooOld):
		code = "local_agent_quota_cli_too_old"
	}
	c.JSON(http.StatusBadGateway, map[string]string{"error": code})
	return nil
}

func isLocalAgentFailure(err error) bool {
	return errors.Is(err, engine.ErrLocalAgent) || errors.Is(err, engine.ErrLocalAgentDesktop) || errors.Is(err, acp.ErrSteerBlocked)
}
func localAgentFailure(c *cart.Context, err error) error {
	code, status := "local_agent_unavailable", http.StatusBadRequest
	if errors.Is(err, engine.ErrLocalAgentDesktop) {
		code, status = "local_agent_desktop_required", http.StatusForbidden
	}
	if errors.Is(err, acp.ErrSteerBlocked) {
		code, status = "local_agent_steer_blocked", http.StatusConflict
	}
	c.JSON(status, map[string]string{"error": code, "detail": err.Error()})
	return nil
}

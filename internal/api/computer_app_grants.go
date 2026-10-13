package api

import (
	"net/http"
	"strings"

	"github.com/teatak/cart/v3"
)

func (s *Server) listComputerAppGrants(c *cart.Context) error {
	sessionID, _ := c.Param("id")
	grants, err := s.store.ListComputerAppGrants(c.Request.Context(), sessionID)
	if err != nil {
		return s.fail(c, err)
	}
	c.JSON(http.StatusOK, map[string]any{"grants": grants})
	return nil
}

func (s *Server) revokeComputerAppGrant(c *cart.Context) error {
	sessionID, _ := c.Param("id")
	appID, _ := c.Param("appID")
	if strings.TrimSpace(appID) == "" {
		return badRequest(c, "appID is required")
	}
	if err := s.store.RevokeComputerAppGrant(c.Request.Context(), sessionID, appID); err != nil {
		return s.fail(c, err)
	}
	c.String(http.StatusNoContent, "")
	return nil
}

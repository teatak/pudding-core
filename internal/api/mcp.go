package api

import (
	"net/http"

	"github.com/teatak/cart/v3"
)

func (s *Server) listBrowserMCPSessions(c *cart.Context) error {
	if s.browserMCP == nil {
		c.JSON(http.StatusOK, map[string]any{"sessions": []any{}})
		return nil
	}
	c.JSON(http.StatusOK, map[string]any{"sessions": s.browserMCP.BrowserSessions()})
	return nil
}

package api

import (
	"errors"
	"net/http"

	"github.com/teatak/cart/v3"
	"github.com/teatak/pudding-core/internal/engine"
	"github.com/teatak/pudding-core/internal/store"
)

func (s *Server) retryTurn(c *cart.Context) error {
	sessionID, _ := c.Param("id")
	turnID, _ := c.Param("turnID")
	var req struct {
		ClientMessageID string `json:"clientMessageID"`
	}
	if err := decode(c, &req); err != nil {
		return badRequest(c, "invalid json body")
	}
	res, err := s.engine.Retry(c.Request.Context(), sessionID, turnID, req.ClientMessageID)
	switch {
	case errors.Is(err, engine.ErrEmptyInput):
		return badRequest(c, "clientMessageID is required")
	case errors.Is(err, store.ErrInvalidRetry):
		c.JSON(http.StatusConflict, map[string]string{"error": "turn_not_retryable"})
		return nil
	case errors.Is(err, engine.ErrTurnRunning):
		c.JSON(http.StatusConflict, map[string]string{"error": "turn_running"})
		return nil
	case errors.Is(err, engine.ErrNoModel), errors.Is(err, engine.ErrProviderConfig):
		return badRequest(c, "provider_config")
	case err != nil:
		return s.fail(c, err)
	}
	status := http.StatusAccepted
	if res.Duplicate {
		status = http.StatusOK
	}
	c.JSON(status, res)
	return nil
}

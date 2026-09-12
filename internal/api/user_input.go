package api

import (
	"errors"
	"net/http"

	"github.com/teatak/cart/v3"
	"github.com/teatak/pudding-core/internal/engine"
)

func (s *Server) getUserInputRequest(c *cart.Context) error {
	sessionID, _ := c.Param("id")
	requestID, _ := c.Param("requestID")
	result, err := s.engine.UserInputRequest(c.Request.Context(), sessionID, requestID)
	if err != nil {
		return s.fail(c, err)
	}
	c.JSON(http.StatusOK, result)
	return nil
}

func (s *Server) actOnUserInput(c *cart.Context) error {
	sessionID, _ := c.Param("id")
	requestID, _ := c.Param("requestID")
	var in engine.UserInputAction
	if err := decode(c, &in); err != nil {
		return badRequest(c, "invalid json body")
	}
	if in.Action != "dismiss" && in.Action != "answer" {
		return badRequest(c, "invalid input action")
	}
	result, err := s.engine.ActOnUserInput(c.Request.Context(), sessionID, requestID, in)
	switch {
	case errors.Is(err, engine.ErrEmptyInput):
		return badRequest(c, "answer text and form parts are required")
	case errors.Is(err, engine.ErrNoModel):
		c.JSON(http.StatusBadRequest, map[string]string{"error": "no_model"})
		return nil
	case errors.Is(err, engine.ErrProviderConfig):
		c.JSON(http.StatusBadRequest, map[string]string{"error": "provider_config"})
		return nil
	case err != nil:
		return s.fail(c, err)
	}
	c.JSON(http.StatusOK, result)
	return nil
}

package api

import (
	"net/http"

	"github.com/teatak/cart/v3"
	"github.com/teatak/pudding-core/internal/store"
)

func (s *Server) saveAssistantAppearanceFavorite(c *cart.Context) error {
	s.assistantMu.Lock()
	defer s.assistantMu.Unlock()
	var in struct {
		Appearance store.AssistantAppearance `json:"appearance"`
	}
	if err := decode(c, &in); err != nil {
		return badRequest(c, "invalid_json")
	}
	if err := in.Appearance.Validate(); err != nil {
		return badRequest(c, "invalid_assistant")
	}
	out, err := s.store.SaveAssistantAppearanceFavorite(c.Request.Context(), in.Appearance)
	if err != nil {
		return s.fail(c, err)
	}
	c.JSON(http.StatusOK, out)
	return nil
}

func (s *Server) deleteAssistantAppearanceFavorite(c *cart.Context) error {
	s.assistantMu.Lock()
	defer s.assistantMu.Unlock()
	id, _ := c.Param("favoriteID")
	out, err := s.store.DeleteAssistantAppearanceFavorite(c.Request.Context(), id)
	if err != nil {
		return s.fail(c, err)
	}
	c.JSON(http.StatusOK, out)
	return nil
}

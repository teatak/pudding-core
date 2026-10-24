package api

import (
	"context"
	"errors"
	"net/http"
	"strings"

	"github.com/teatak/cart/v3"
	"github.com/teatak/pudding-core/internal/store"
)

func (s *Server) openAssistant(c *cart.Context) error {
	s.assistantMu.Lock()
	defer s.assistantMu.Unlock()
	var in struct {
		Locale   string `json:"locale"`
		Provider string `json:"provider"`
		Model    string `json:"model"`
	}
	if err := decode(c, &in); err != nil {
		return badRequest(c, "invalid_json")
	}
	name := "小布"
	switch in.Locale {
	case "en":
		name = "Puddy"
	case "zh-CN", "zh-TW":
	default:
		return badRequest(c, "invalid_assistant")
	}
	in.Provider, in.Model = strings.TrimSpace(in.Provider), strings.TrimSpace(in.Model)
	if (in.Provider == "") != (in.Model == "") {
		return badRequest(c, "no_model")
	}
	out, err := s.store.OpenAssistant(c.Request.Context(), name, in.Provider, in.Model)
	if err != nil {
		return s.fail(c, err)
	}
	c.Response.Header().Set("Cache-Control", "no-store")
	c.JSON(http.StatusOK, out)
	return nil
}

func (s *Server) getAssistant(c *cart.Context) error {
	c.Response.Header().Set("Cache-Control", "no-store")
	out, err := s.store.GetAssistant(c.Request.Context())
	if err != nil {
		return s.fail(c, err)
	}
	c.JSON(http.StatusOK, out)
	return nil
}

func (s *Server) saveAssistant(c *cart.Context) error {
	s.assistantMu.Lock()
	defer s.assistantMu.Unlock()
	var in store.SaveAssistantInput
	if err := decode(c, &in); err != nil {
		return badRequest(c, "invalid_json")
	}
	if err := in.Normalize(); err != nil {
		return badRequest(c, "invalid_assistant")
	}
	out, err := s.store.SaveAssistant(c.Request.Context(), in)
	if errors.Is(err, store.ErrAssistantConflict) {
		c.JSON(http.StatusConflict, map[string]string{"error": "assistant_conflict"})
		return nil
	}
	if err != nil {
		return s.fail(c, err)
	}
	c.Response.Header().Set("Cache-Control", "no-store")
	c.JSON(http.StatusOK, out)
	return nil
}

// Only this endpoint may permanently remove the protected assistant conversation.
func (s *Server) deleteAssistant(c *cart.Context) error {
	s.assistantMu.Lock()
	defer s.assistantMu.Unlock()
	ctx := c.Request.Context()
	current, err := s.store.GetAssistant(ctx)
	if err != nil {
		return s.fail(c, err)
	}
	if current.Assistant != nil && current.Assistant.SessionID != "" {
		err = s.purgeSessionWith(ctx, current.Assistant.SessionID, s.store.PrepareAssistantDeletion,
			func(ctx context.Context, _ string) error { return s.store.DeleteAssistant(ctx) })
	} else {
		err = s.store.DeleteAssistant(ctx)
	}
	if err != nil {
		return s.fail(c, err)
	}
	c.String(http.StatusNoContent, "")
	return nil
}

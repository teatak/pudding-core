package api

import (
	"errors"
	"net/http"

	"github.com/teatak/cart/v3"
	"github.com/teatak/pudding-core/internal/audio/voice"
)

type audioBindingReq struct {
	Enabled *bool `json:"enabled"`
}

func (s *Server) getAudioBindings(c *cart.Context) error {
	if s.voice == nil {
		c.JSON(http.StatusServiceUnavailable, map[string]string{"error": "audio_unavailable"})
		return nil
	}
	id, _ := c.Param("id")
	if _, err := s.store.GetSession(c.Request.Context(), id); err != nil {
		return s.fail(c, err)
	}
	c.JSON(http.StatusOK, map[string]any{"bindings": s.voice.Snapshot()})
	return nil
}

func (s *Server) bindAudioInput(c *cart.Context) error {
	return s.bindAudio(c, func(sessionID string, enabled bool) (voice.Bindings, error) {
		return s.voice.BindInput(sessionID, enabled)
	})
}

func (s *Server) bindAudioOutput(c *cart.Context) error {
	return s.bindAudio(c, func(sessionID string, enabled bool) (voice.Bindings, error) {
		return s.voice.BindOutput(sessionID, enabled)
	})
}

func (s *Server) bindAudio(c *cart.Context, bind func(string, bool) (voice.Bindings, error)) error {
	if s.voice == nil {
		c.JSON(http.StatusServiceUnavailable, map[string]string{"error": "audio_unavailable"})
		return nil
	}
	id, _ := c.Param("id")
	if _, err := s.store.GetSession(c.Request.Context(), id); err != nil {
		return s.fail(c, err)
	}
	var req audioBindingReq
	if err := decode(c, &req); err != nil {
		return badRequest(c, "invalid json body")
	}
	if req.Enabled == nil {
		return badRequest(c, "enabled is required")
	}
	bindings, err := bind(id, *req.Enabled)
	if errors.Is(err, voice.ErrSessionRequired) {
		return badRequest(c, "session id is required")
	}
	if errors.Is(err, voice.ErrInputUnavailable) {
		c.JSON(http.StatusServiceUnavailable, map[string]string{
			"error": "audio_input_unavailable",
		})
		return nil
	}
	if err != nil {
		c.JSON(http.StatusInternalServerError, map[string]string{
			"error": "audio_binding_failed",
		})
		return nil
	}
	c.JSON(http.StatusOK, map[string]any{"ok": true, "bindings": bindings})
	return nil
}

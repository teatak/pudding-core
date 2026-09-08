package api

import (
	"errors"
	"net/http"

	"github.com/teatak/cart/v3"
	"github.com/teatak/pudding-core/internal/store"
)

func (s *Server) reorderQueuedInputs(c *cart.Context) error {
	id, _ := c.Param("id")
	var req struct {
		ClientMessageIDs []string `json:"clientMessageIDs"`
	}
	if err := decode(c, &req); err != nil || req.ClientMessageIDs == nil {
		return badRequest(c, "clientMessageIDs is required")
	}
	res, err := s.store.ReorderQueuedInputs(c.Request.Context(), id, req.ClientMessageIDs)
	if errors.Is(err, store.ErrQueueChanged) {
		c.JSON(http.StatusConflict, map[string]string{"error": "queued_inputs_changed"})
		return nil
	}
	if err != nil {
		return s.fail(c, err)
	}
	for _, ev := range res.Events {
		s.hub.Publish(ev)
	}
	// Moving an editing head can unblock the next queued input.
	s.engine.TryDrainQueued(id)
	c.JSON(http.StatusOK, map[string]any{"queuedInputs": res.Inputs})
	return nil
}

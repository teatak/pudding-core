package api

import (
	"context"
	"github.com/teatak/cart/v3"
	"github.com/teatak/pudding-core/internal/store"
	"net/http"
)

func (s *Server) listAssistantTasks(c *cart.Context) error {
	id, _ := c.Param("id")
	if _, err := s.store.GetSession(c.Request.Context(), id); err != nil {
		return s.fail(c, err)
	}
	tasks, err := s.engine.ListAssistantTasks(c.Request.Context(), id)
	if err != nil {
		return s.fail(c, err)
	}
	c.JSON(http.StatusOK, map[string]any{"tasks": tasks})
	return nil
}

func (s *Server) enrichAssistantTaskSources(ctx context.Context, sessions []*store.Session) error {
	profile, err := s.store.GetAssistant(ctx)
	if err != nil {
		return err
	}
	if profile.Assistant == nil || profile.Assistant.SessionID == "" {
		return nil
	}
	runs, err := s.store.ListAssistantTaskRuns(ctx, profile.Assistant.SessionID)
	if err != nil {
		return err
	}
	sources := map[string]store.AssistantTaskRun{}
	for _, r := range runs {
		sources[r.SessionID] = r
	}
	for _, session := range sessions {
		source := sources[session.ID]
		session.AssistantSessionID = source.AssistantSessionID
		session.AssistantTaskActive = source.Status == "queued" || source.Status == "running"
	}
	return nil
}

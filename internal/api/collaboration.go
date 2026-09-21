package api

import (
	"net/http"
	"strings"

	"github.com/teatak/cart/v3"
	"github.com/teatak/pudding-core/internal/store"
)

type childSessionView struct {
	Session          *store.Session `json:"session"`
	Status           string         `json:"status"`
	LatestTurnID     string         `json:"latestTurnID,omitempty"`
	PendingApprovals int            `json:"pendingApprovals"`
	ResultCollected  bool           `json:"resultCollected"`
	Summary          string         `json:"summary,omitempty"`
}

func (s *Server) listChildSessions(c *cart.Context) error {
	id, _ := c.Param("id")
	children, err := s.store.ListChildSessions(c.Request.Context(), id)
	if err != nil {
		return s.fail(c, err)
	}
	views := make([]childSessionView, 0, len(children))
	for _, child := range children {
		s.enrichSessionProcesses(child)
		view := childSessionView{Session: child, Status: "cancelled", PendingApprovals: len(s.engine.PendingApprovals(child.ID))}
		page, err := s.store.ListTurnsPage(c.Request.Context(), child.ID, "", 1)
		if err != nil {
			return s.fail(c, err)
		}
		if len(page.Turns) > 0 {
			for _, turn := range page.Turns {
				for _, message := range turn.Messages {
					if message.Role == store.RoleUser && strings.TrimSpace(message.Text) != "" {
						view.Summary = childTaskSummary(message.Text)
					}
				}
			}
			// A page of size one still includes the whole retry chain, oldest first.
			turn := page.Turns[len(page.Turns)-1]
			view.Status = string(turn.Status)
			view.LatestTurnID = turn.ID
			_, err := s.store.GetMessage(c.Request.Context(), id, "collaboration_result_"+turn.ID)
			view.ResultCollected = err == nil
		}
		inputs, err := s.store.ListQueuedInputs(c.Request.Context(), child.ID)
		if err != nil {
			return s.fail(c, err)
		}
		queued := len(inputs) > 0
		if queued && !child.Running {
			view.Status = "queued"
			view.Summary = childTaskSummary(inputs[0].Text)
		}
		if queued || child.Running {
			view.ResultCollected = false
		}
		views = append(views, view)
	}
	c.JSON(http.StatusOK, map[string]any{"children": views})
	return nil
}

// The card excerpt comes from the actual task input, not a second task record.
func childTaskSummary(text string) string {
	runes := []rune(strings.Join(strings.Fields(text), " "))
	if len(runes) > 120 {
		return string(runes[:120]) + "…"
	}
	return string(runes)
}

func (s *Server) stopCollaboration(c *cart.Context) error {
	id, _ := c.Param("id")
	if err := s.engine.StopCollaboration(c.Request.Context(), id); err != nil {
		return s.fail(c, err)
	}
	c.String(http.StatusNoContent, "")
	return nil
}

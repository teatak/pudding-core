package api

import (
	"net/http"

	"github.com/teatak/cart/v3"
	"github.com/teatak/pudding-core/internal/store"
)

type childSessionView struct {
	Session          *store.Session `json:"session"`
	TaskTitle        string         `json:"taskTitle"`
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
		view := childSessionView{Session: child, TaskTitle: child.Title, Status: "cancelled", PendingApprovals: len(s.engine.PendingApprovals(child.ID))}
		page, err := s.store.ListTurnsPage(c.Request.Context(), child.ID, "", 1)
		if err != nil {
			return s.fail(c, err)
		}
		if err := s.setChildTask(c.Request.Context(), &view, page); err != nil {
			return s.fail(c, err)
		}
		taskSummary := view.Summary
		if len(page.Turns) > 0 {
			// A page of size one still includes the whole retry chain, oldest first.
			turn := page.Turns[len(page.Turns)-1]
			view.Status = string(turn.Status)
			view.LatestTurnID = turn.ID
			if turn.Status == store.TurnCompleted {
				view.Summary = ""
				for _, message := range turn.Messages {
					if message.Role == store.RoleAssistant && message.Kind == store.MessageKindText && !message.Interrupted {
						view.Summary = childTextExcerpt(message.Text, 96)
					}
				}
			}
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
			view.Summary = taskSummary
			if childTaskInput(&store.Message{Role: store.RoleUser, Text: inputs[0].Text, Parts: inputs[0].Parts}) {
				view.setTask(inputs[0].Text, len(page.Turns) > 0)
			}
		}
		if queued || child.Running {
			view.ResultCollected = false
		}
		views = append(views, view)
	}
	c.JSON(http.StatusOK, map[string]any{"children": views})
	return nil
}

func (s *Server) stopCollaboration(c *cart.Context) error {
	id, _ := c.Param("id")
	if err := s.engine.StopCollaboration(c.Request.Context(), id); err != nil {
		return s.fail(c, err)
	}
	c.String(http.StatusNoContent, "")
	return nil
}

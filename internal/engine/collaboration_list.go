package engine

import (
	"context"
	"encoding/json"
	"strings"

	"github.com/teatak/pudding-core/internal/store"
	"github.com/teatak/pudding-core/internal/tool"
)

// The model only needs task metadata, not the session's settings or grants.
type collaborationListItem struct {
	SessionID              string `json:"session_id"`
	Title                  string `json:"title"`
	TaskTitle              string `json:"task_title"`
	Status                 string `json:"status"`
	LatestTurnID           string `json:"latest_turn_id,omitempty"`
	Summary                string `json:"summary,omitempty"`
	PendingApprovals       int    `json:"pending_approvals"`
	PendingUserInputs      int    `json:"pending_user_inputs"`
	BackgroundProcessCount int    `json:"background_process_count"`
	ResultCollected        bool   `json:"result_collected"`
}

// Discover unresolved question identities from canonical results and live tool
// calls, then use the existing resolver for answers (including queued answers).
// This is model-only metadata; the desktop already presents its input requests.
func (e *Engine) childPendingUserInputs(ctx context.Context, sessionID string) (int, error) {
	messages, err := e.store.ListMessages(ctx, sessionID, 0)
	if err != nil {
		return 0, err
	}
	needsAnswer := func(status string) bool {
		return status == "waiting" || status == "awaiting_user" || status == "timeout"
	}
	requests := make(map[string]bool)
	answered := make(map[string]bool)
	for _, message := range messages {
		if id, ok := strings.CutPrefix(message.ClientMessageID, "input-flow-"); ok {
			answered[id] = true
		}
		for _, part := range message.Parts {
			if part.Type != store.ContentPartToolResult || part.Name != tool.RequestUserInput {
				continue
			}
			var result struct {
				RequestID string `json:"requestID"`
				Status    string `json:"status"`
			}
			if json.Unmarshal([]byte(part.Content), &result) == nil && result.RequestID == message.TurnID+":"+part.CallID && needsAnswer(result.Status) {
				requests[result.RequestID] = true
			}
		}
	}
	e.mu.Lock()
	for _, request := range e.inputRequests {
		if request.SessionID == sessionID && needsAnswer(request.Status) {
			requests[request.ID] = true
		}
	}
	e.mu.Unlock()
	count := 0
	for id := range requests {
		if answered[id] {
			continue
		}
		request, err := e.UserInputRequest(ctx, sessionID, id)
		if err != nil {
			return 0, err
		}
		if needsAnswer(request.Status) {
			count++
		}
	}
	return count, nil
}

// ChildSessionView is shared by the desktop snapshot and the model's list tool.
// Task labels and results are projections of canonical session inputs and turns.
type ChildSessionView struct {
	Session          *store.Session `json:"session"`
	TaskTitle        string         `json:"taskTitle"`
	Status           string         `json:"status"`
	LatestTurnID     string         `json:"latestTurnID,omitempty"`
	PendingApprovals int            `json:"pendingApprovals"`
	ResultCollected  bool           `json:"resultCollected"`
	Summary          string         `json:"summary,omitempty"`
}

func (e *Engine) ListChildSessions(ctx context.Context, parentID string) ([]ChildSessionView, error) {
	children, err := e.store.ListChildSessions(ctx, parentID)
	if err != nil {
		return nil, err
	}
	views := make([]ChildSessionView, 0, len(children))
	for _, child := range children {
		child.BackgroundProcessCount = e.BackgroundProcessCount(child.ID)
		view := ChildSessionView{Session: child, TaskTitle: child.Title, Status: "cancelled", PendingApprovals: len(e.PendingApprovals(child.ID))}
		page, err := e.store.ListTurnsPage(ctx, child.ID, "", 1)
		if err != nil {
			return nil, err
		}
		if err := e.setChildTask(ctx, &view, page); err != nil {
			return nil, err
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
			_, err := e.store.GetMessage(ctx, parentID, "collaboration_result_"+turn.ID)
			view.ResultCollected = err == nil
		}
		inputs, err := e.store.ListQueuedInputs(ctx, child.ID)
		if err != nil {
			return nil, err
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
	return views, nil
}

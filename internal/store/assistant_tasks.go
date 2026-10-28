package store

import (
	"context"
	"errors"
	"time"
)

var ErrAssistantTaskBusy = errors.New("an assigned task is still active; follow up on that task or wait for its result before assigning another")

// These are coordination links between independent root sessions, not children.
// Status and results are projected from the dispatch's own queued input/turn.
type AssistantTaskRun struct {
	TaskID             string    `json:"taskID"`
	AssistantSessionID string    `json:"assistantSessionID"`
	SessionID          string    `json:"sessionID"`
	Title              string    `json:"title"`
	SourceTurnID       string    `json:"sourceTurnID"`
	ClientMessageID    string    `json:"clientMessageID"`
	TurnID             string    `json:"turnID,omitempty"`
	Status             string    `json:"status"`
	Summary            string    `json:"summary,omitempty"`
	ResultCollected    bool      `json:"resultCollected"`
	CreatedAt          time.Time `json:"createdAt"`
}

func (r AssistantTaskRun) ResultMessageID() string {
	if r.TurnID != "" {
		return "assistant_task_result_" + r.TurnID
	}
	return "assistant_task_result_" + r.ClientMessageID
}

type AssignAssistantTaskInput struct {
	AssistantSessionID string
	SourceTurnID       string
	CallID             string
	TaskID             string // Empty creates a new assignment; otherwise follows up.
	Title              string
	Session            *Session // New root session, or an existing active root.
	CreateSession      bool
	Input              QueueInputInput
}

type AssistantTaskStore interface {
	AssignAssistantTask(context.Context, AssignAssistantTaskInput) (*AssistantTaskRun, error)
	ListAssistantTaskRuns(context.Context, string) ([]AssistantTaskRun, error)
	CollectAssistantTaskResults(context.Context, string) (bool, error)
}

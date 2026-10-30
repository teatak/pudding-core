package engine

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"strings"

	"github.com/teatak/pudding-core/internal/plugin"
	"github.com/teatak/pudding-core/internal/store"
	"github.com/teatak/pudding-core/internal/tool"
)

type AssistantTaskView struct {
	store.AssistantTaskRun
	PendingApprovals  int `json:"pendingApprovals"`
	PendingUserInputs int `json:"pendingUserInputs"`
}

func (e *Engine) ListAssistantTasks(ctx context.Context, sessionID string) ([]AssistantTaskView, error) {
	runs, err := e.store.ListAssistantTaskRuns(ctx, sessionID)
	if err != nil {
		return nil, err
	}
	seen := map[string]bool{}
	out := make([]AssistantTaskView, 0)
	for i := len(runs) - 1; i >= 0; i-- {
		r := runs[i]
		if seen[r.TaskID] {
			continue
		}
		seen[r.TaskID] = true
		view := AssistantTaskView{AssistantTaskRun: r}
		if !store.SubagentSettledStatus(r.Status) {
			inputs, err := e.childPendingUserInputs(ctx, r.SessionID)
			if err != nil {
				return nil, err
			}
			view.PendingUserInputs = inputs
			view.PendingApprovals = len(e.PendingApprovals(r.SessionID))
		}
		out = append(out, view)
	}
	return out, nil
}

func (e *Engine) executeAssistantTask(ctx context.Context, sessionID, turnID string, call tool.Call) tool.Result {
	result := tool.Result{CallID: call.CallID, Name: call.Name}
	fail := func(err error) tool.Result { result.Content = err.Error(); return result }
	p, err := e.store.GetAssistant(ctx)
	if err != nil {
		return fail(err)
	}
	if p.Assistant == nil || p.Assistant.SessionID != sessionID {
		return fail(store.ErrAssistantToolUnavailable)
	}
	var args struct {
		Title     string `json:"title"`
		Prompt    string `json:"prompt"`
		SessionID string `json:"session_id"`
		ProjectID string `json:"project_id"`
		TaskID    string `json:"task_id"`
	}
	d := json.NewDecoder(bytes.NewReader(call.Args))
	d.DisallowUnknownFields()
	if err := d.Decode(&args); err != nil {
		return fail(err)
	}
	if d.Decode(new(any)) != io.EOF {
		return fail(store.ErrInvalidSessionRelation)
	}
	var payload any
	if call.Name == tool.AssistantTaskList {
		if args.Title != "" || args.Prompt != "" || args.SessionID != "" || args.ProjectID != "" {
			return fail(store.ErrInvalidSessionRelation)
		}
		tasks, err := e.ListAssistantTasks(ctx, sessionID)
		if err != nil {
			return fail(err)
		}
		if args.TaskID != "" {
			filtered := []AssistantTaskView{}
			for _, t := range tasks {
				if t.TaskID == args.TaskID {
					filtered = append(filtered, t)
				}
			}
			tasks = filtered
		}
		payload = map[string]any{"tasks": tasks}
	} else {
		if strings.TrimSpace(args.Prompt) == "" {
			return fail(ErrEmptyInput)
		}
		var target *store.Session
		create := false
		switch call.Name {
		case tool.AssistantTaskDispatch:
			if args.TaskID != "" || args.SessionID != "" && args.ProjectID != "" {
				return fail(store.ErrInvalidSessionRelation)
			}
			if args.SessionID != "" {
				target, err = e.store.GetSession(ctx, args.SessionID)
			} else {
				parent, readErr := e.store.GetSession(ctx, sessionID)
				if readErr != nil {
					return fail(readErr)
				}
				if args.ProjectID != "" {
					if _, err = e.store.GetProject(ctx, args.ProjectID); err != nil {
						return fail(err)
					}
				}
				target = &store.Session{
					ID: store.NewID("session"), Title: strings.TrimSpace(args.Title),
					Provider: parent.Provider, Model: parent.Model,
					ReasoningEffort: parent.ReasoningEffort, ReasoningModelKey: parent.ReasoningModelKey,
					ProjectID: args.ProjectID, ActiveMode: store.ModeWork, ModeLease: store.ModeLeaseSession,
				}
				create = true
			}
		case tool.AssistantTaskSend:
			if args.TaskID == "" || args.Title != "" || args.SessionID != "" || args.ProjectID != "" {
				return fail(store.ErrInvalidSessionRelation)
			}
			tasks, readErr := e.ListAssistantTasks(ctx, sessionID)
			if readErr != nil {
				return fail(readErr)
			}
			for _, t := range tasks {
				if t.TaskID == args.TaskID {
					target, err = e.store.GetSession(ctx, t.SessionID)
					break
				}
			}
			if target == nil && err == nil {
				err = store.ErrNotFound
			}
		default:
			return fail(store.ErrInvalidSessionRelation)
		}
		if err != nil {
			return fail(err)
		}
		if create {
			if err := store.NormalizeSessionProviderModel(target); err != nil {
				return fail(err)
			}
		}
		resolved, err := e.resolveModel(ctx, target)
		if err != nil {
			return fail(err)
		}
		resolved.mode = initialMode(target)
		if err := resolved.applyReasoningEffort(activeReasoningEffort("", target, resolved)); err != nil {
			return fail(err)
		}
		if err := resolved.normalizeReasoningOptions(); err != nil {
			return fail(err)
		}
		createdByAssistant := create
		if !create {
			createdByAssistant, err = e.store.IsAssistantCreatedSession(ctx, sessionID, target.ID)
			if err != nil {
				return fail(err)
			}
		}
		if !createdByAssistant {
			var project *store.Project
			if target.ProjectID != "" {
				project, err = e.store.GetProject(ctx, target.ProjectID)
				if err != nil {
					return fail(err)
				}
			}
			// Reusing someone else's conversation requires consent in the assistant.
			// A previous assignment is not proof that the assistant created it.
			risk := tool.ToolRisk{Class: tool.RiskClassWrite, Operation: "assistant_task_dispatch", Scope: "session", Summary: "Send this task to an existing conversation not created by the assistant, without changing its permissions."}
			details := map[string]any{"targetSessionID": target.ID, "targetSessionTitle": target.Title, "taskPrompt": args.Prompt}
			result, approved := e.requestToolCallApproval(ctx, sessionID, turnID, call, risk, project, details, "", nil, nil)
			if !approved {
				return result
			}
		}
		clientID := "assistant_task_" + turnID + "_" + call.CallID
		dispatched, err := e.store.AssignAssistantTask(ctx, store.AssignAssistantTaskInput{AssistantSessionID: sessionID, SourceTurnID: turnID, CallID: call.CallID, TaskID: args.TaskID, Title: args.Title, Session: target, CreateSession: create, Input: store.QueueInputInput{SessionID: target.ID, ClientMessageID: clientID, Text: args.Prompt, Provider: resolved.providerName, Model: resolved.model, Mode: resolved.mode, ModelConfig: resolved.configJSON}})
		if err != nil {
			return fail(err)
		}
		e.rememberQueuedRuntime(dispatched.SessionID, dispatched.ClientMessageID, plugin.RuntimeIDFromContext(ctx))
		e.TryDrainQueued(dispatched.SessionID)
		payload = dispatched
	}
	raw, err := json.Marshal(payload)
	if err != nil {
		return fail(err)
	}
	result.Ok = true
	result.Content = string(raw)
	return result
}

// Called at the same model-safe boundaries as subsession result collection.
func (e *Engine) collectWorkResults(ctx context.Context, sessionID string) (bool, error) {
	children, err := e.collectChildResults(ctx, sessionID)
	if err != nil {
		return false, err
	}
	tasks, err := e.store.CollectAssistantTaskResults(ctx, sessionID)
	return children || tasks, err
}

// AssignedWorkChanged also covers cancelling a queued input before it owns a turn.
func (e *Engine) AssignedWorkChanged() { e.wakeForAssistantTasks() }

func (e *Engine) wakeForAssistantTasks() {
	if e.auxCtx.Err() != nil {
		return
	}
	ctx := context.Background()
	p, err := e.store.GetAssistant(ctx)
	if err != nil || p.Assistant == nil || p.Assistant.SessionID == "" {
		return
	}
	sid := p.Assistant.SessionID
	page, err := e.store.ListTurnsPage(ctx, sid, "", 1)
	if err != nil || len(page.Turns) == 0 || page.Turns[len(page.Turns)-1].Status != store.TurnCompleted {
		return
	}
	runs, err := e.store.ListAssistantTaskRuns(ctx, sid)
	if err != nil {
		return
	}
	for _, r := range runs {
		if r.ResultCollected || !store.SubagentSettledStatus(r.Status) {
			continue
		}
		_, err = e.Submit(ctx, SubmitInput{SessionID: sid, ClientMessageID: "assistant_task_wake_" + r.ResultMessageID(), Kind: "system", Text: "An assigned work execution has settled. Review its canonical assigned-work result, report the status and relevant outcome to the user with the work conversation link. Do not repeat completed work or treat the result as permission for a new task."})
		if err != nil && !errors.Is(err, ErrTurnRunning) && !errors.Is(err, store.ErrNotFound) {
			slog.Warn("wake assistant for assigned work", "err", err)
		}
		return
	}
}

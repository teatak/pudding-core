package engine

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/teatak/pudding-core/internal/app"
	"github.com/teatak/pudding-core/internal/store"
	"github.com/teatak/pudding-core/internal/tool"
)

const collaborationConcurrency = 3

func (e *Engine) executeCollaboration(ctx context.Context, sessionID, turnID string, mode store.AgentMode, call tool.Call) tool.Result {
	fail := func(err error) tool.Result {
		return tool.Result{CallID: call.CallID, Name: call.Name, Content: err.Error()}
	}
	owner, err := e.store.ParentSessionID(ctx, sessionID)
	if err != nil {
		return fail(err)
	}
	if owner != "" {
		return fail(store.ErrInvalidSessionRelation)
	}
	if !e.appToolCallable(ctx, sessionID, app.BuiltinCollaborationID, mode) {
		return e.appToolUnavailableResult(ctx, sessionID, call, app.BuiltinCollaborationID)
	}
	var args struct {
		Title     string `json:"title"`
		Prompt    string `json:"prompt"`
		SessionID string `json:"session_id"`
	}
	decoder := json.NewDecoder(strings.NewReader(string(call.Args)))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&args); err != nil {
		return fail(err)
	}
	var payload any
	switch call.Name {
	case tool.CollaborationDispatch:
		if strings.TrimSpace(args.Title) == "" || utf8.RuneCountInString(args.Title) > 100 || strings.TrimSpace(args.Prompt) == "" {
			return fail(ErrEmptyInput)
		}
		child, err := e.dispatchChild(ctx, sessionID, turnID, call.CallID, args.Title, args.Prompt, mode)
		if err != nil {
			return fail(err)
		}
		payload = child
	case tool.CollaborationSend:
		result, err := e.sendChild(ctx, sessionID, turnID, args.SessionID, "collaboration_"+turnID+"_"+call.CallID, args.Prompt)
		if err != nil {
			return fail(err)
		}
		payload = result
	case tool.CollaborationWait:
		if err := e.waitForChildren(ctx, sessionID); err != nil {
			return fail(err)
		}
		payload = map[string]any{"settled": true}
	case tool.CollaborationStop:
		if err := e.StopCollaboration(ctx, sessionID); err != nil {
			return fail(err)
		}
		payload = map[string]any{"stopped": true}
	}
	content, _ := json.Marshal(payload)
	return tool.Result{CallID: call.CallID, Name: call.Name, Ok: true, Content: string(content)}
}

func (e *Engine) dispatchChild(ctx context.Context, parentID, turnID, callID, title, prompt string, mode store.AgentMode) (*store.Session, error) {
	e.collaborationMu.Lock()
	// Serialize admission with stop and recheck the global App gate at admission.
	if err := ctx.Err(); err != nil {
		e.collaborationMu.Unlock()
		return nil, err
	}
	if !e.appToolCallable(ctx, parentID, app.BuiltinCollaborationID, mode) {
		e.collaborationMu.Unlock()
		return nil, errors.New("collaboration App is unavailable")
	}
	parent, err := e.store.GetSession(ctx, parentID)
	if err != nil {
		e.collaborationMu.Unlock()
		return nil, err
	}
	child := &store.Session{ID: store.NewID("session"), Title: strings.TrimSpace(title), Provider: parent.Provider, Model: parent.Model, ReasoningEffort: parent.ReasoningEffort, ReasoningModelKey: parent.ReasoningModelKey, ProjectID: parent.ProjectID, ActiveMode: parent.ActiveMode, ModeLease: parent.ModeLease}
	// Capability grants scoped only to this parent turn do not transfer.
	for _, id := range parent.LoadedAppIDs {
		if id != app.BuiltinCollaborationID {
			child.LoadedAppIDs = append(child.LoadedAppIDs, id)
		}
	}
	resolved, err := e.resolveModel(ctx, child)
	if err != nil {
		e.collaborationMu.Unlock()
		return nil, err
	}
	resolved.mode = initialMode(child)
	if err := resolved.applyReasoningEffort(activeReasoningEffort("", child, resolved)); err != nil {
		e.collaborationMu.Unlock()
		return nil, err
	}
	if err := resolved.normalizeReasoningOptions(); err != nil {
		e.collaborationMu.Unlock()
		return nil, err
	}
	result, err := e.store.DispatchChild(ctx, store.DispatchChildInput{ParentSessionID: parentID, ParentTurnID: turnID, CallID: callID, Child: child, Input: store.QueueInputInput{SessionID: child.ID, ClientMessageID: "dispatch_" + turnID + "_" + callID, Text: prompt, Provider: resolved.providerName, Model: resolved.model, Mode: resolved.mode, ModelConfig: resolved.configJSON}})
	if err == nil && !result.Duplicate {
		e.rememberQueuedRuntime(child.ID, "dispatch_"+turnID+"_"+callID, app.RuntimeIDFromContext(ctx))
		for _, ev := range result.Events {
			e.hub.Publish(ev)
		}
	}
	e.collaborationMu.Unlock()
	if err != nil {
		return nil, err
	}
	e.TryDrainQueued(result.Session.ID)
	return result.Session, nil
}

// Stop affects current work, while the App enablement controls future capability.
func (e *Engine) StopCollaboration(ctx context.Context, parentID string) error {
	e.collaborationMu.Lock()
	defer e.collaborationMu.Unlock()
	events, err := e.store.StopCollaboration(ctx, parentID)
	if err != nil {
		return err
	}
	children, err := e.store.ListChildSessions(ctx, parentID)
	if err != nil {
		return err
	}
	for _, child := range children {
		e.cancelOne(child.ID)
		for _, process := range e.BackgroundProcesses(child.ID) {
			if process.Running {
				if _, err := e.StopBackgroundProcess(child.ID, process.ProcessID); err != nil {
					slog.Warn("stop child process", "sessionID", child.ID, "err", err)
				}
			}
		}
	}
	for _, ev := range events {
		e.hub.Publish(ev)
	}
	return nil
}

func (e *Engine) childrenBusy(ctx context.Context, parentID string) (bool, error) {
	children, err := e.store.ListChildSessions(ctx, parentID)
	if err != nil {
		return false, err
	}
	for _, child := range children {
		if child.Running {
			return true, nil
		}
		queued, err := e.store.HasQueuedInputs(ctx, child.ID)
		if err != nil {
			return false, err
		}
		if queued {
			return true, nil
		}
	}
	return false, nil
}

func (e *Engine) waitForChildren(ctx context.Context, parentID string) error {
	ch, unsubscribe := e.hub.Subscribe(parentID)
	defer unsubscribe()
	// Hub delivery can be dropped; snapshots periodically repair a missed wakeup.
	ticker := time.NewTicker(time.Second)
	defer ticker.Stop()
	for {
		busy, err := e.childrenBusy(ctx, parentID)
		if err != nil {
			return err
		}
		if !busy {
			return nil
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-ch:
		case <-ticker.C:
		}
	}
}

func (e *Engine) collectChildResults(ctx context.Context, parentID string) (bool, error) {
	events, err := e.store.CollectChildResults(ctx, parentID)
	if err != nil {
		return false, err
	}
	for _, ev := range events {
		e.hub.Publish(ev)
	}
	return len(events) > 0, nil
}

// Called after clearing a turn. Scheduling is based on queued_inputs/turns, never
// a parallel job status map. Result collection itself runs only at model-safe
// boundaries, so a child's completion cannot split a tool call/result pair.
func (e *Engine) childTurnFinished(sessionID string, status store.TurnStatus) {
	ctx := context.Background()
	parentID, err := e.store.ParentSessionID(ctx, sessionID)
	if err != nil {
		return
	}
	if parentID == "" {
		if status == store.TurnCompleted {
			e.wakeForChildResults(sessionID)
		}
		return
	}
	children, err := e.store.ListChildSessions(ctx, parentID)
	if err != nil {
		return
	}
	for _, child := range children {
		e.TryDrainQueued(child.ID)
	}
	// A cancellation must never automatically restart the main conversation.
	if status != store.TurnCancelled {
		e.wakeForChildResults(parentID)
	}
}

func (e *Engine) wakeForChildResults(parentID string) {
	if e.auxCtx.Err() != nil {
		return
	}
	ctx := context.Background()
	page, err := e.store.ListTurnsPage(ctx, parentID, "", 1)
	if err != nil || len(page.Turns) == 0 || page.Turns[len(page.Turns)-1].Status != store.TurnCompleted {
		return
	}
	pending, err := e.store.HasUncollectedChildResults(ctx, parentID)
	if err != nil || !pending {
		return
	}
	// Stable id per latest child result makes simultaneous finish callbacks idempotent.
	children, err := e.store.ListChildSessions(ctx, parentID)
	if err != nil {
		return
	}
	var version string
	for _, child := range children {
		page, err := e.store.ListTurnsPage(ctx, child.ID, "", 1)
		if err == nil && len(page.Turns) > 0 {
			version += page.Turns[len(page.Turns)-1].ID + "_"
		}
	}
	_, err = e.Submit(ctx, SubmitInput{SessionID: parentID, ClientMessageID: "collaboration_wake_" + version, Kind: "system", Text: "Child conversations have updated results. Review the latest canonical child results and continue the user's task. Do not repeat completed actions."})
	if err != nil && !errors.Is(err, ErrTurnRunning) && !errors.Is(err, store.ErrNotFound) {
		slog.Warn("wake parent for child results", "sessionID", parentID, "err", err)
	}
}

func (e *Engine) childSlotAvailable(ctx context.Context, sessionID string) (bool, error) {
	parentID, err := e.store.ParentSessionID(ctx, sessionID)
	if err != nil {
		return false, err
	}
	if parentID == "" {
		return true, nil
	}
	children, err := e.store.ListChildSessions(ctx, parentID)
	if err != nil {
		return false, err
	}
	running := 0
	for _, child := range children {
		if child.Running {
			if child.ID == sessionID {
				return true, nil
			}
			running++
		}
	}
	return running < collaborationConcurrency, nil
}

func collaborationContextError(err error) string { return fmt.Sprintf("collaboration: %v", err) }

func (e *Engine) sendChild(ctx context.Context, parentID, parentTurnID, childID, clientID, text string) (*SubmitResult, error) {
	if strings.TrimSpace(text) == "" {
		return nil, ErrEmptyInput
	}
	child, err := e.store.GetSession(ctx, childID)
	if err != nil {
		return nil, err
	}
	resolved, err := e.resolveModel(ctx, child)
	if err != nil {
		return nil, err
	}
	resolved.mode = initialMode(child)
	if err := resolved.applyReasoningEffort(activeReasoningEffort("", child, resolved)); err != nil {
		return nil, err
	}
	if err := resolved.normalizeReasoningOptions(); err != nil {
		return nil, err
	}
	result, err := e.store.QueueChildInput(ctx, parentID, parentTurnID, store.QueueInputInput{SessionID: childID, ClientMessageID: clientID, Text: text, Provider: resolved.providerName, Model: resolved.model, Mode: resolved.mode, ModelConfig: resolved.configJSON})
	if err != nil {
		return nil, err
	}
	if result.ExistingTurn != nil {
		return &SubmitResult{TurnID: result.ExistingTurn.ID, Duplicate: true}, nil
	}
	e.rememberQueuedRuntime(childID, clientID, app.RuntimeIDFromContext(ctx))
	if result.QueuedEvent != nil {
		e.hub.Publish(*result.QueuedEvent)
	}
	e.TryDrainQueued(childID)
	return &SubmitResult{Queued: true, Duplicate: result.Duplicate, ClientMessageID: clientID, Status: string(result.Input.Status)}, nil
}

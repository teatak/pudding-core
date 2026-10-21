package engine

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/teatak/pudding-core/internal/plugin"
	"github.com/teatak/pudding-core/internal/store"
	"github.com/teatak/pudding-core/internal/tool"
)

const collaborationConcurrency = 3

func (e *Engine) executeSubtask(ctx context.Context, sessionID, turnID string, mode store.AgentMode, call tool.Call) tool.Result {
	fail := func(err error) tool.Result {
		return tool.Result{CallID: call.CallID, Name: call.Name, Content: err.Error()}
	}
	if !tool.IsSubtaskTool(call.Name) {
		return fail(errors.New("unknown subagent tool"))
	}
	owner, err := e.store.ParentSessionID(ctx, sessionID)
	if err != nil {
		return fail(err)
	}
	if owner != "" {
		return fail(store.ErrInvalidSessionRelation)
	}
	if !e.pluginToolCallable(ctx, sessionID, plugin.BuiltinSubagentID, mode) {
		return e.pluginToolUnavailableResult(ctx, sessionID, call, plugin.BuiltinSubagentID)
	}
	var dispatchArgs struct {
		Title  string `json:"title"`
		Prompt string `json:"prompt"`
	}
	var sendArgs struct {
		SessionID      string `json:"session_id"`
		Prompt         string `json:"prompt"`
		ResetContext   bool   `json:"reset_context"`
		ExpectedTurnID string `json:"expected_turn_id"`
	}
	var resetArgs struct {
		SessionID      string `json:"session_id"`
		ExpectedTurnID string `json:"expected_turn_id"`
	}
	var listArgs struct {
		IncludeArchived bool `json:"include_archived"`
	}
	var archiveArgs struct {
		SessionID string `json:"session_id"`
	}
	var emptyArgs struct{}
	var input any = &emptyArgs
	switch call.Name {
	case tool.SubtaskList:
		input = &listArgs
	case tool.SubtaskDispatch:
		input = &dispatchArgs
	case tool.SubtaskSend:
		input = &sendArgs
	case tool.SubtaskReset:
		input = &resetArgs
	case tool.SubtaskArchive, tool.SubtaskRestore:
		input = &archiveArgs
	}
	decoder := json.NewDecoder(strings.NewReader(string(call.Args)))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(input); err != nil {
		return fail(err)
	}
	if err := decoder.Decode(new(any)); err != io.EOF {
		return fail(errors.New("subagent tool expects one JSON object"))
	}
	var payload any
	switch call.Name {
	case tool.SubtaskList:
		children, err := e.ListChildSessions(ctx, sessionID)
		if err != nil {
			return fail(err)
		}
		items := make([]collaborationListItem, 0, len(children))
		archived := 0
		for _, child := range children {
			if child.ArchivedAt != nil {
				archived++
				if !listArgs.IncludeArchived {
					continue
				}
			}
			items = append(items, collaborationListItem{
				SessionID: child.Session.ID, Title: child.Session.Title, TaskTitle: child.TaskTitle,
				Status: child.Status, LatestTurnID: child.LatestTurnID, Summary: child.Summary,
				PendingApprovals: child.PendingApprovals, BackgroundProcessCount: child.Session.BackgroundProcessCount,
				PendingUserInputs: child.PendingUserInputs, ArchivedAt: child.ArchivedAt, CanArchive: child.CanArchive,
				ResultCollected: child.ResultCollected, CanReset: e.subagentCanReset(child),
			})
		}
		payload = map[string]any{"children": items, "total_count": len(children), "archived_count": archived,
			"total_limit": store.SubagentTotalLimit, "concurrency_limit": collaborationConcurrency,
			"fresh_context_limit": store.SubtaskDispatchLimit}
		if lifecycle, ok := e.store.(store.SubagentLifecycleStore); ok {
			used, err := lifecycle.SubagentFreshContextCount(ctx, sessionID, turnID)
			if err != nil {
				return fail(err)
			}
			payload.(map[string]any)["fresh_context_used"] = used
			payload.(map[string]any)["fresh_context_remaining"] = max(0, store.SubtaskDispatchLimit-used)
		}
	case tool.SubtaskDispatch:
		if strings.TrimSpace(dispatchArgs.Title) == "" || utf8.RuneCountInString(dispatchArgs.Title) > 100 || strings.TrimSpace(dispatchArgs.Prompt) == "" {
			return fail(ErrEmptyInput)
		}
		child, err := e.dispatchChild(ctx, sessionID, turnID, call.CallID, dispatchArgs.Title, dispatchArgs.Prompt, mode)
		if err != nil {
			return fail(err)
		}
		payload = child
	case tool.SubtaskSend:
		if strings.TrimSpace(sendArgs.SessionID) == "" || strings.TrimSpace(sendArgs.Prompt) == "" {
			return fail(ErrEmptyInput)
		}
		if sendArgs.ResetContext {
			result, err := e.resetSubagent(ctx, sessionID, turnID, call.CallID, sendArgs.SessionID, sendArgs.ExpectedTurnID, &sendArgs.Prompt, mode)
			if err != nil {
				return fail(err)
			}
			payload = result
		} else {
			if sendArgs.ExpectedTurnID != "" {
				return fail(errors.New("expected_turn_id requires reset_context=true"))
			}
			result, err := e.sendChild(ctx, sessionID, turnID, sendArgs.SessionID, "collaboration_"+turnID+"_"+call.CallID, sendArgs.Prompt)
			if err != nil {
				return fail(err)
			}
			payload = result
		}
	case tool.SubtaskReset:
		result, err := e.resetSubagent(ctx, sessionID, turnID, call.CallID, resetArgs.SessionID, resetArgs.ExpectedTurnID, nil, mode)
		if err != nil {
			return fail(err)
		}
		payload = result
	case tool.SubtaskArchive, tool.SubtaskRestore:
		childID := strings.TrimSpace(archiveArgs.SessionID)
		if childID == "" {
			return fail(ErrEmptyInput)
		}
		if err := e.SetSubtaskArchived(ctx, sessionID, childID, call.Name == tool.SubtaskArchive); err != nil {
			return fail(err)
		}
		payload = map[string]any{"updated": true, "session_id": childID}
	case tool.SubtaskWait:
		if err := e.waitForChildren(ctx, sessionID); err != nil {
			return fail(err)
		}
		payload = map[string]any{"settled": true}
	case tool.SubtaskStop:
		if err := e.StopCollaboration(ctx, sessionID); err != nil {
			return fail(err)
		}
		payload = map[string]any{"stopped": true}
	default:
		return fail(errors.New("unknown subagent tool"))
	}
	content, _ := json.Marshal(payload)
	return tool.Result{CallID: call.CallID, Name: call.Name, Ok: true, Content: string(content)}
}

func (e *Engine) dispatchChild(ctx context.Context, parentID, turnID, callID, title, prompt string, mode store.AgentMode) (*store.Session, error) {
	e.collaborationMu.Lock()
	// Serialize admission with stop and recheck the global plugin gate at admission.
	if err := ctx.Err(); err != nil {
		e.collaborationMu.Unlock()
		return nil, err
	}
	if !e.pluginToolCallable(ctx, parentID, plugin.BuiltinSubagentID, mode) {
		e.collaborationMu.Unlock()
		return nil, errors.New("collaboration plugin is unavailable")
	}
	parent, err := e.store.GetSession(ctx, parentID)
	if err != nil {
		e.collaborationMu.Unlock()
		return nil, err
	}
	child := &store.Session{ID: store.NewID("session"), Title: strings.TrimSpace(title), Provider: parent.Provider, Model: parent.Model, ReasoningEffort: parent.ReasoningEffort, ReasoningModelKey: parent.ReasoningModelKey, ProjectID: parent.ProjectID, ActiveMode: parent.ActiveMode, ModeLease: parent.ModeLease}
	// Capability grants scoped only to this parent turn do not transfer.
	for _, id := range parent.LoadedPluginIDs {
		if id != plugin.BuiltinSubagentID {
			child.LoadedPluginIDs = append(child.LoadedPluginIDs, id)
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
		e.rememberQueuedRuntime(child.ID, "dispatch_"+turnID+"_"+callID, plugin.RuntimeIDFromContext(ctx))
	}
	e.collaborationMu.Unlock()
	if err != nil {
		return nil, err
	}
	e.TryDrainQueued(result.Session.ID)
	return result.Session, nil
}

// Stop affects current work, while the plugin enablement controls future capability.
func (e *Engine) StopCollaboration(ctx context.Context, parentID string) error {
	e.collaborationMu.Lock()
	defer e.collaborationMu.Unlock()
	_, err := e.store.StopCollaboration(ctx, parentID)
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
	defer func() { unsubscribe() }()
	// Snapshots periodically repair a missed wakeup.
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
		case _, ok := <-ch:
			if !ok {
				// The hub closes a full subscriber; subscribe again for later wakeups.
				ch, unsubscribe = e.hub.Subscribe(parentID)
			}
		case <-ticker.C:
		}
	}
}

func (e *Engine) collectChildResults(ctx context.Context, parentID string) (bool, error) {
	events, err := e.store.CollectChildResults(ctx, parentID)
	if err != nil {
		return false, err
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
	e.rememberQueuedRuntime(childID, clientID, plugin.RuntimeIDFromContext(ctx))
	e.TryDrainQueued(childID)
	return &SubmitResult{Queued: true, Duplicate: result.Duplicate, ClientMessageID: clientID, Status: string(result.Input.Status)}, nil
}

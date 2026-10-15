package engine

import (
	"context"
	"errors"
	"strings"

	"github.com/teatak/pudding-core/internal/plugin"
	"github.com/teatak/pudding-core/internal/store"
)

func subagentCanReset(child ChildSessionView) bool {
	return child.LatestTurnID != "" &&
		(store.SubagentSettledStatus(child.Status) || child.Status == "idle") &&
		!child.Session.Running && child.PendingApprovals == 0 &&
		child.PendingUserInputs == 0 && child.Session.BackgroundProcessCount == 0 && child.ResultCollected
}

func (e *Engine) listSubagents(ctx context.Context, parentID, turnID string, includeArchived bool) (any, error) {
	children, err := e.ListChildSessions(ctx, parentID)
	if err != nil {
		return nil, err
	}
	items := make([]collaborationListItem, 0, len(children))
	archived := 0
	for _, child := range children {
		if child.ArchivedAt != nil {
			archived++
			if !includeArchived {
				continue
			}
		}
		e.mu.Lock()
		contextBusy := e.compacting[child.Session.ID]
		e.mu.Unlock()
		items = append(items, collaborationListItem{
			SessionID: child.Session.ID, Title: child.Session.Title, TaskTitle: child.TaskTitle,
			Status: child.Status, LatestTurnID: child.LatestTurnID, Summary: child.Summary,
			PendingApprovals: child.PendingApprovals, BackgroundProcessCount: child.Session.BackgroundProcessCount,
			PendingUserInputs: child.PendingUserInputs, ArchivedAt: child.ArchivedAt, CanArchive: child.CanArchive,
			CanReset: subagentCanReset(child) && !contextBusy, ResultCollected: child.ResultCollected,
		})
	}
	payload := map[string]any{"children": items, "total_count": len(children), "archived_count": archived,
		"total_limit": store.SubagentTotalLimit, "concurrency_limit": collaborationConcurrency,
		"fresh_context_limit": store.SubtaskDispatchLimit}
	if lifecycle, ok := e.store.(store.SubagentLifecycleStore); ok {
		used, err := lifecycle.SubagentFreshContextCount(ctx, parentID, turnID)
		if err != nil {
			return nil, err
		}
		remaining := store.SubtaskDispatchLimit - used
		if remaining < 0 {
			remaining = 0
		}
		payload["fresh_context_used"], payload["fresh_context_remaining"] = used, remaining
	}
	return payload, nil
}

// resetChild is an admission operation, not two sequential tool calls. The
// collaboration lock excludes starting child turns and stopping the group; the
// store transaction checks persistent state again. Compaction and reset share
// the existing context-mutation guard, so a stale summary cannot cross a reset.
func (e *Engine) resetChild(ctx context.Context, in store.SubagentResetInput, prompt *string, mode store.AgentMode) (*store.SubagentResetResult, error) {
	if strings.TrimSpace(in.SessionID) == "" || strings.TrimSpace(in.ExpectedTurnID) == "" ||
		(prompt != nil && strings.TrimSpace(*prompt) == "") {
		return nil, ErrEmptyInput
	}
	if prompt != nil {
		in.Input = &store.QueueInputInput{SessionID: in.SessionID,
			ClientMessageID: "collaboration_" + in.ParentTurnID + "_" + in.CallID, Text: *prompt}
	}
	e.collaborationMu.Lock()
	result, err := func() (*store.SubagentResetResult, error) {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		if !e.pluginToolCallable(ctx, in.ParentSessionID, plugin.BuiltinCollaborationID, mode) {
			return nil, errors.New("collaboration plugin is unavailable")
		}
		owner, err := e.store.ParentSessionID(ctx, in.SessionID)
		if err != nil {
			return nil, err
		}
		parentOwner, err := e.store.ParentSessionID(ctx, in.ParentSessionID)
		if err != nil {
			return nil, err
		}
		if owner == "" || owner != in.ParentSessionID || parentOwner != "" {
			return nil, store.ErrInvalidSessionRelation
		}
		// Replay before busy/version/quota checks. Retrying the same call
		// after its task starts must not create another reset or queued input.
		_, markerID := store.SubagentResetIDs(in.ParentSessionID, in.ParentTurnID, in.CallID)
		if prior, err := e.store.GetMessage(ctx, in.SessionID, markerID); err == nil {
			return store.SubagentResetReplay(prior, in)
		} else if !errors.Is(err, store.ErrNotFound) {
			return nil, err
		}
		lifecycle, ok := e.store.(store.SubagentLifecycleStore)
		if !ok {
			return nil, errors.New("store does not support atomic subagent context reset")
		}
		children, err := e.ListChildSessions(ctx, in.ParentSessionID)
		if err != nil {
			return nil, err
		}
		var target *ChildSessionView
		for i := range children {
			if children[i].Session.ID == in.SessionID {
				target = &children[i]
				break
			}
		}
		if target == nil {
			return nil, store.ErrInvalidSessionRelation
		}
		if target.LatestTurnID != in.ExpectedTurnID {
			return nil, store.ErrSubagentStateChanged
		}
		if !target.ResultCollected && !target.Session.Running && store.SubagentSettledStatus(target.Status) {
			return nil, store.ErrSubagentResultUncollected
		}
		if !subagentCanReset(*target) {
			return nil, store.ErrSubagentResetBusy
		}
		e.mu.Lock()
		if e.compacting[in.SessionID] {
			e.mu.Unlock()
			return nil, store.ErrSubagentResetBusy
		}
		e.compacting[in.SessionID] = true
		e.mu.Unlock()
		defer func() {
			e.mu.Lock()
			delete(e.compacting, in.SessionID)
			e.mu.Unlock()
		}()
		if in.Input != nil {
			resolved, err := e.resolveModel(ctx, target.Session)
			if err != nil {
				return nil, err
			}
			resolved.mode = initialMode(target.Session)
			if err := resolved.applyReasoningEffort(activeReasoningEffort("", target.Session, resolved)); err != nil {
				return nil, err
			}
			if err := resolved.normalizeReasoningOptions(); err != nil {
				return nil, err
			}
			in.Input.Provider, in.Input.Model = resolved.providerName, resolved.model
			in.Input.Mode, in.Input.ModelConfig = resolved.mode, resolved.configJSON
		}
		return lifecycle.ResetSubagent(ctx, in)
	}()
	if err == nil && result.Queued && !result.Duplicate {
		e.rememberQueuedRuntime(in.SessionID, result.ClientMessageID, plugin.RuntimeIDFromContext(ctx))
	}
	e.collaborationMu.Unlock()
	if err != nil {
		return nil, err
	}
	if result.Queued {
		e.TryDrainQueued(in.SessionID)
	}
	return result, nil
}

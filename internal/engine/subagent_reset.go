package engine

import (
	"context"
	"errors"
	"strings"

	"github.com/teatak/pudding-core/internal/plugin"
	"github.com/teatak/pudding-core/internal/store"
)

// Reset shares the collaboration admission lock with turns, queue promotion and
// stop. Persistent snapshot/receipt/quota checks are repeated by the store in the
// same transaction as the context checkpoint and optional new task.
func (e *Engine) resetSubagent(ctx context.Context, parentID, parentTurnID, callID, childID, expectedTurnID string, prompt *string, mode store.AgentMode) (*store.SubagentResetResult, error) {
	childID, expectedTurnID = strings.TrimSpace(childID), strings.TrimSpace(expectedTurnID)
	in := store.SubagentResetInput{ParentSessionID: parentID, ParentTurnID: parentTurnID,
		CallID: callID, SessionID: childID, ExpectedTurnID: expectedTurnID}
	if prompt != nil {
		in.Input = &store.QueueInputInput{SessionID: childID,
			ClientMessageID: "collaboration_" + parentTurnID + "_" + callID, Text: *prompt}
	}
	if err := store.ValidateSubagentReset(in); err != nil {
		return nil, err
	}
	lifecycle, ok := e.store.(store.SubagentLifecycleStore)
	if !ok {
		return nil, errors.New("subagent context reset is unavailable in this store")
	}
	e.collaborationMu.Lock()
	result, err := func() (*store.SubagentResetResult, error) {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		if !e.pluginToolCallable(ctx, parentID, plugin.BuiltinCollaborationID, mode) {
			return nil, errors.New("collaboration plugin is unavailable")
		}
		owner, err := e.store.ParentSessionID(ctx, childID)
		if err != nil {
			return nil, err
		}
		if owner != parentID {
			return nil, store.ErrInvalidSessionRelation
		}
		owner, err = e.store.ParentSessionID(ctx, parentID)
		if err != nil {
			return nil, err
		}
		if owner != "" {
			return nil, store.ErrInvalidSessionRelation
		}
		// Resolve a previously committed operation before checking current busy
		// state. A lost response must not reset an already-running new task again.
		_, messageID := store.SubagentResetIDs(parentID, parentTurnID, callID)
		checkpoint, err := e.store.GetMessage(ctx, childID, messageID)
		if err == nil {
			return store.SubagentResetReplay(checkpoint, in)
		}
		if !errors.Is(err, store.ErrNotFound) {
			return nil, err
		}
		child, err := e.store.GetSession(ctx, childID)
		if err != nil {
			return nil, err
		}
		if child.Running || e.BackgroundProcessCount(childID) != 0 || len(e.PendingApprovals(childID)) != 0 {
			return nil, store.ErrSubagentResetBusy
		}
		queued, err := e.store.ListQueuedInputs(ctx, childID)
		if err != nil {
			return nil, err
		}
		if len(queued) != 0 {
			return nil, store.ErrSubagentResetBusy
		}
		pending, err := e.childPendingUserInputs(ctx, childID)
		if err != nil {
			return nil, err
		}
		if pending != 0 {
			return nil, store.ErrSubagentResetBusy
		}
		// Reserve the existing context-maintenance guard, so a compaction cannot
		// start between the idle check and reset commit. No LLM compaction is run.
		e.mu.Lock()
		if e.compacting[childID] || e.running[childID] != nil {
			e.mu.Unlock()
			return nil, store.ErrSubagentResetBusy
		}
		e.compacting[childID] = true
		e.mu.Unlock()
		defer func() {
			e.mu.Lock()
			delete(e.compacting, childID)
			e.mu.Unlock()
		}()
		if in.Input != nil {
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
			in.Input.Provider, in.Input.Model = resolved.providerName, resolved.model
			in.Input.Mode, in.Input.ModelConfig = resolved.mode, resolved.configJSON
		}
		return lifecycle.ResetSubagent(ctx, in)
	}()
	if err == nil && result.Queued && !result.Duplicate {
		e.rememberQueuedRuntime(childID, result.ClientMessageID, plugin.RuntimeIDFromContext(ctx))
	}
	e.collaborationMu.Unlock()
	if err != nil {
		return nil, err
	}
	if result.Queued && !result.Duplicate {
		e.TryDrainQueued(childID)
	}
	return result, nil
}

func (e *Engine) subagentCanReset(child ChildSessionView) bool {
	if child.LatestTurnID == "" || (!store.SubagentSettledStatus(child.Status) && child.Status != "idle") ||
		child.Session.Running || child.PendingApprovals != 0 || child.PendingUserInputs != 0 ||
		child.Session.BackgroundProcessCount != 0 || !child.ResultCollected {
		return false
	}
	e.mu.Lock()
	defer e.mu.Unlock()
	return !e.compacting[child.Session.ID] && e.running[child.Session.ID] == nil
}

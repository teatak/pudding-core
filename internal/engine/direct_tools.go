package engine

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"

	"github.com/teatak/pudding-core/internal/event"
	"github.com/teatak/pudding-core/internal/provider"
	"github.com/teatak/pudding-core/internal/store"
	"github.com/teatak/pudding-core/internal/tool"
)

const debugSessionID = "session_mcp_debug"

// DebugSession reuses one ordinary session for local tool debugging. Its model
// fields come from the existing configuration; creating it does not call a model.
func (e *Engine) DebugSession(ctx context.Context) (*store.Session, error) {
	e.mu.Lock()
	defer e.mu.Unlock()
	session, err := e.store.GetSession(ctx, debugSessionID)
	if err == nil || !errors.Is(err, store.ErrNotFound) {
		return session, err
	}
	resolved, err := e.fallbackModel(ctx, nil)
	if err != nil {
		return nil, err
	}
	session = &store.Session{
		ID: debugSessionID, Title: "MCP Debug",
		Provider: resolved.providerName, Model: resolved.model,
		ActiveMode: store.ModeCode, ModeLease: store.ModeLeaseSession,
	}
	if err := e.store.CreateSession(ctx, session); err != nil {
		return nil, err
	}
	return session, nil
}

// PrepareDebugPlugins exposes available plugins in the dedicated debug session.
// The caller owns and serializes prepared for the daemon lifetime so an explicit
// unload is not undone by the next list. Existing user sessions are not prepared.
func (e *Engine) PrepareDebugPlugins(ctx context.Context, sessionID string, mode store.AgentMode, prepared map[string]bool) error {
	if e.plugins == nil {
		return nil
	}
	definitions, err := e.plugins.ListDefinitions(ctx)
	if err != nil {
		return err
	}
	for _, definition := range definitions {
		if definition == nil || !definition.Enabled || prepared[definition.ID] || store.AgentModeRank(mode) < store.AgentModeRank(requiredPluginMode(definition)) {
			continue
		}
		call := tool.Call{
			SessionID: sessionID, CallID: store.NewID("call"), Name: tool.PluginLoad,
			Args: mustJSON(map[string]string{"plugin_id": definition.ID}),
		}
		result, _, _ := e.loadPlugin(ctx, sessionID, call, mode)
		if !result.Ok {
			// A broken plugin must not hide unrelated Core or Desktop tools.
			slog.WarnContext(ctx, "engine: debug plugin load failed", "sessionID", sessionID, "pluginID", definition.ID, "error", result.Content)
			continue
		}
		prepared[definition.ID] = true
	}
	return nil
}

// ToolDefinitions is the same session-, mode- and runtime-scoped tool catalog
// used by model requests.
func (e *Engine) ToolDefinitions(ctx context.Context, sessionID string, mode store.AgentMode) ([]provider.ToolDef, error) {
	return e.toolDefinitions(ctx, sessionID, mode)
}

// CallTool runs one externally requested tool in a canonical turn. It keeps the
// normal tool authorization, approvals, history and resource cleanup without
// starting a model loop, generating a title or automatically compacting history.
func (e *Engine) CallTool(ctx context.Context, sessionID string, mode store.AgentMode, name string, args json.RawMessage) (result tool.Result, callErr error) {
	session, err := e.store.GetSession(ctx, sessionID)
	if err != nil {
		return tool.Result{}, err
	}
	resolved, err := e.resolveModel(ctx, session)
	if err != nil {
		return tool.Result{}, err
	}
	allowed, err := e.toolDefinitions(ctx, sessionID, mode)
	if err != nil {
		return tool.Result{}, err
	}
	call := tool.Call{
		SessionID: sessionID, TurnID: store.NewID("turn"), CallID: store.NewID("call"),
		Name: name, Args: append(json.RawMessage(nil), args...),
	}
	turnCtx, cancel := context.WithCancel(ctx)
	defer cancel()
	e.mu.Lock()
	_, err = e.store.BeginTurn(turnCtx, store.BeginTurnInput{
		SessionID: sessionID, TurnID: call.TurnID, UserMessageID: store.NewID("msg"),
		ClientMessageID: "mcp:" + call.CallID, UserText: "MCP tool: " + name,
		Provider: resolved.providerName, Model: resolved.model, ModelConfig: resolved.configJSON, Mode: mode,
	})
	if err != nil {
		e.mu.Unlock()
		return tool.Result{}, err
	}
	active := newActiveTurn(call.TurnID, cancel)
	e.running[sessionID] = active
	e.wg.Add(1)
	e.mu.Unlock()
	defer e.wg.Done()

	var parts turnPartAccumulator
	defer func() {
		active.stopAcceptingSteers()
		if steers := active.consumeSteers(); len(steers) > 0 {
			if err := e.commitTurnParts(call.TurnID, &parts, true); err != nil {
				callErr = errors.Join(callErr, err)
			} else if err := e.applyTurnSteers(call.TurnID, steers); err != nil {
				callErr = errors.Join(callErr, err)
			}
		}
		status, message := store.TurnCompleted, ""
		if callErr != nil {
			status, message = store.TurnFailed, callErr.Error()
		}
		if turnCtx.Err() != nil {
			status = store.TurnCancelled
			callErr = errors.Join(callErr, turnCtx.Err())
		}
		if err := e.finishTurnState(sessionID, call.TurnID, mode, status, message, parts.UncommittedParts(), false); err != nil {
			callErr = errors.Join(callErr, err)
			return
		}
		// These are continuations of explicitly requested work, not a model step
		// for the MCP call itself.
		e.TryDrainQueued(sessionID)
		e.childTurnFinished(sessionID, status)
		e.wakeForAssistantTasks()
	}()

	result, mode, callErr = e.executeDirectToolCall(turnCtx, mode, allowed, &parts, call)
	if callErr != nil || !result.Ok || call.Name != tool.CommandRun {
		return result, callErr
	}
	var command struct {
		Yielded    bool   `json:"yielded"`
		Running    bool   `json:"running"`
		Background bool   `json:"background"`
		ProcessID  string `json:"processID"`
		NextOffset int64  `json:"nextOffset"`
		Reason     string `json:"reason"`
	}
	if err := json.Unmarshal([]byte(result.Content), &command); err != nil {
		return result, err
	}
	if !command.Yielded || command.Background {
		return result, nil
	}
	// Foreground work belongs to this turn. Keep polling its existing process
	// before finalization; background=true keeps the normal cross-call lifetime.
	for result.Ok && command.Running {
		if err := turnCtx.Err(); err != nil {
			return result, err
		}
		poll := tool.Call{
			SessionID: sessionID, TurnID: call.TurnID, CallID: store.NewID("call"), Name: tool.CommandSession,
			Args: mustJSON(map[string]any{"action": "poll", "process_id": command.ProcessID, "offset": command.NextOffset, "wait_ms": 10000}),
		}
		result, mode, callErr = e.executeDirectToolCall(turnCtx, mode, allowed, &parts, poll)
		result.CallID, result.Name = call.CallID, call.Name
		if callErr != nil || !result.Ok {
			return result, callErr
		}
		if err := json.Unmarshal([]byte(result.Content), &command); err != nil {
			return result, err
		}
	}
	return directForegroundCommandResult(result, command.Reason)
}

// A successful poll only means the process was inspected. Restore command_run's
// completion semantics when returning its final state under the original name.
func directForegroundCommandResult(result tool.Result, reason string) (tool.Result, error) {
	result.Ok = reason == "" || reason == "exited" || reason == "non_zero_exit"
	var payload map[string]json.RawMessage
	if err := json.Unmarshal([]byte(result.Content), &payload); err != nil {
		return result, err
	}
	payload["ok"] = mustJSON(result.Ok)
	content, err := json.Marshal(payload)
	if err != nil {
		return result, err
	}
	result.Content = string(content)
	return result, nil
}

func (e *Engine) executeDirectToolCall(ctx context.Context, mode store.AgentMode, allowed []provider.ToolDef, parts *turnPartAccumulator, call tool.Call) (tool.Result, store.AgentMode, error) {
	parts.AppendTool(provider.ToolCallChunk{Index: len(parts.parts), CallID: call.CallID, Name: call.Name, ArgsDelta: string(call.Args)})
	if err := e.commitTurnParts(call.TurnID, parts, true); err != nil {
		return tool.Result{}, mode, err
	}
	e.hub.Publish(event.Event{
		SessionID: call.SessionID, Kind: event.TurnTool, TurnID: call.TurnID,
		CallID: call.CallID, Name: call.Name, Phase: "running", ArgsText: string(call.Args),
	})
	result, nextMode, _ := e.dispatchToolCall(ctx, call.SessionID, call.TurnID, mode, allowed, call)
	if result.CallID == "" {
		result.CallID = call.CallID
	}
	call.Name = tool.NormalizeLegacyContentTool(call.Name)
	err := e.completeToolCall(call.SessionID, call.TurnID, allowed, parts, call, result)
	return result, nextMode, err
}

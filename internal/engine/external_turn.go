package engine

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"maps"
	"time"

	"github.com/teatak/pudding-core/internal/plugin"
	"github.com/teatak/pudding-core/internal/provider"
	"github.com/teatak/pudding-core/internal/store"
	"github.com/teatak/pudding-core/internal/tool"
)

const externalTurnIdle = 10 * time.Minute

type externalTurn struct {
	skillRefs map[string]tool.SkillReference
	bindingID string
	baseMode  store.AgentMode
	mode      store.AgentMode
	requests  chan externalRequest
}

type externalRequest struct {
	ctx     context.Context
	name    string
	args    json.RawMessage
	finish  store.TurnStatus
	summary string
	reply   chan externalResponse
}

type ExternalToolReply struct {
	SkillRefs           map[string]tool.SkillReference
	Mode                store.AgentMode
	RefreshProjectRules bool
	Result              tool.Result
	PluginID            string
	UserMessages        []string
}

type externalResponse struct {
	result ExternalToolReply
	err    error
}

func externalClientID(bindingID, turnID string) string {
	return "external-mcp:" + bindingID + ":" + turnID
}

func (e *Engine) BeginExternalTurn(ctx context.Context, credentialHash, task string) (string, error) {
	e.mu.Lock()
	defer e.mu.Unlock()
	b, err := e.store.AgentBinding(ctx, credentialHash)
	if err != nil {
		return "", err
	}
	sess, err := e.store.GetSession(ctx, b.SessionID)
	if err != nil {
		return "", err
	}
	turnID := store.NewID("turn")
	metadata, err := json.Marshal(struct {
		ExternalAgent store.ExternalAgentIdentity `json:"externalAgent"`
	}{ExternalAgent: b.ExternalAgentIdentity})
	if err != nil {
		return "", err
	}
	_, err = e.store.BeginTurn(ctx, store.BeginTurnInput{
		SessionID: sess.ID, TurnID: turnID, UserMessageID: store.NewID("msg"),
		ClientMessageID: externalClientID(b.ID, turnID), UserText: b.Name + " · " + task, UserMetadata: metadata,
		Provider: sess.Provider, Model: sess.Model, Mode: sess.ActiveMode,
	})
	if err != nil {
		return "", err
	}
	turnCtx, cancel := context.WithCancel(plugin.WithRuntimeID(e.auxCtx, b.RuntimeID))
	active := newActiveTurn(turnID, cancel)
	active.external = &externalTurn{bindingID: b.ID, requests: make(chan externalRequest)}
	e.running[sess.ID] = active
	e.wg.Add(1)
	go e.runExternalTurn(turnCtx, sess, active)
	return turnID, nil
}

func (e *Engine) runExternalTurn(ctx context.Context, sess *store.Session, a *activeTurn) {
	defer e.wg.Done()
	defer a.cancel()
	timer := time.NewTimer(externalTurnIdle)
	defer timer.Stop()
	var parts turnPartAccumulator
	mode := sess.ActiveMode
	status := store.TurnCancelled
	var reason string
	var last *externalRequest
loop:
	for {
		select {
		case <-ctx.Done():
			reason = "external turn interrupted"
			break loop
		case <-timer.C:
			reason = "external turn idle timeout"
			break loop
		case req := <-a.external.requests:
			timer.Stop()
			if req.finish != "" {
				status = req.finish
				last = &req
				if req.summary != "" {
					parts.AppendDelta(provider.PartText, req.summary)
				}
				break loop
			}
			callCtx, cancel := context.WithCancel(ctx)
			stop := context.AfterFunc(req.ctx, cancel)
			if req.name == tool.CommandRun {
				callCtx = tool.WithCommandTurnContext(callCtx, ctx)
			}
			reply, nextMode, err := e.executeExternalRequest(callCtx, sess.ID, a, &parts, req, mode)
			mode = nextMode
			stop()
			cancel()
			if err != nil {
				status = store.TurnFailed
				reason = err.Error()
				last = &req
				break loop
			}
			req.reply <- externalResponse{result: reply}
			timer.Reset(externalTurnIdle)
		}
	}
	a.stopAcceptingSteers()
	var err error
	if steers := a.consumeSteers(); len(steers) > 0 {
		err = e.commitTurnParts(a.turnID, &parts, true)
		if err == nil {
			err = e.applyTurnSteers(a.turnID, steers)
		}
	}
	if ctx.Err() != nil {
		status = store.TurnCancelled
	}
	if err != nil {
		status = store.TurnFailed
		reason = err.Error()
	}
	err = errors.Join(err, e.finishTurnState(sess.ID, a.turnID, mode, status, reason, parts.UncommittedParts(), false))
	if last != nil {
		if last.finish != "" && ctx.Err() != nil {
			err = errors.Join(err, ctx.Err())
		}
		if last.finish == "" && reason != "" {
			err = errors.Join(err, errors.New(reason))
		}
		last.reply <- externalResponse{err: err}
	}
	if err != nil {
		slog.Warn("external turn ended", "turnID", a.turnID, "error", err)
	}
	e.TryDrainQueued(sess.ID)
	e.childTurnFinished(sess.ID, status)
	e.wakeForAssistantTasks()
}

func (e *Engine) executeExternalRequest(ctx context.Context, sid string, active *activeTurn, parts *turnPartAccumulator, req externalRequest, mode store.AgentMode) (ExternalToolReply, store.AgentMode, error) {
	var reply ExternalToolReply
	current, err := e.store.GetSession(ctx, sid)
	if err != nil {
		return reply, mode, err
	}
	e.mu.Lock()
	mode = active.external.effectiveMode(current.ActiveMode)
	e.mu.Unlock()
	defs, err := e.toolDefinitions(ctx, sid, mode)
	if err != nil {
		return reply, mode, err
	}
	if active.external.skillRefs == nil {
		active.external.skillRefs, err = e.externalSkillRefs(ctx, sid)
		if err != nil {
			return reply, mode, err
		}
	}
	for _, def := range defs {
		if def.Name == req.name {
			reply.PluginID = def.PluginID
			if reply.PluginID == "" {
				reply.PluginID, _ = tool.BuiltinPluginIDForTool(req.name)
			}
			break
		}
	}
	call := tool.Call{SessionID: sid, TurnID: active.turnID, CallID: store.NewID("call"), Name: req.name, Args: req.args}
	previousMode := mode
	reply.Result, mode, err = e.executeDirectToolCall(ctx, mode, defs, parts, call)
	reply.Mode = mode
	reply.RefreshProjectRules = mode == store.ModeCode && (mode != previousMode || (req.name == tool.RequestCapability && reply.Result.Ok))
	if ref, ok := tool.ReferencedSkill(reply.Result.Name, reply.Result.Ok, reply.Result.Content); ok {
		active.external.skillRefs[ref.Key()] = ref
	}
	reply.SkillRefs = maps.Clone(active.external.skillRefs)
	// Loading a plugin can also obtain a capability approval for this turn.
	if mode != previousMode {
		e.mu.Lock()
		active.external.baseMode = current.ActiveMode
		active.external.mode = mode
		e.mu.Unlock()
	}
	if err != nil {
		return reply, mode, err
	}
	if steers := active.consumeSteers(); len(steers) > 0 {
		if err = e.commitTurnParts(active.turnID, parts, true); err != nil {
			return reply, mode, err
		}
		if err = e.applyTurnSteers(active.turnID, steers); err != nil {
			return reply, mode, err
		}
		for _, steer := range steers {
			reply.UserMessages = append(reply.UserMessages, steer.event.Text)
		}
	}
	return reply, mode, nil
}

func (e *Engine) externalRequest(ctx context.Context, b *store.ExternalAgentBinding, id string, r externalRequest) (ExternalToolReply, error) {
	e.mu.Lock()
	a := e.running[b.SessionID]
	e.mu.Unlock()
	if a == nil || a.turnID != id || a.external == nil || a.external.bindingID != b.ID {
		return ExternalToolReply{}, ErrTurnNotActive
	}
	r.ctx = ctx
	r.reply = make(chan externalResponse, 1)
	select {
	case <-ctx.Done():
		return ExternalToolReply{}, ctx.Err()
	case <-a.done:
		return ExternalToolReply{}, ErrTurnNotActive
	case a.external.requests <- r:
	}
	select {
	case <-ctx.Done():
		return ExternalToolReply{}, ctx.Err()
	case response := <-r.reply:
		return response.result, response.err
	}
}

func (e *Engine) CallExternalTool(ctx context.Context, b *store.ExternalAgentBinding, turnID, name string, args json.RawMessage) (ExternalToolReply, error) {
	return e.externalRequest(ctx, b, turnID, externalRequest{name: name, args: args})
}

func (e *Engine) EndExternalTurn(ctx context.Context, b *store.ExternalAgentBinding, turnID string, status store.TurnStatus, summary string) error {
	_, err := e.externalRequest(ctx, b, turnID, externalRequest{finish: status, summary: summary})
	return err
}

func (e *Engine) CancelExternalBinding(sessionID, bindingID string) {
	e.mu.Lock()
	defer e.mu.Unlock()
	if a := e.running[sessionID]; a != nil && a.external != nil && a.external.bindingID == bindingID {
		a.cancel()
	}
}

// A turn-only capability grant belongs to this binding until the turn ends or
// the session's persistent mode changes. Call with the engine mutex held.
func (t *externalTurn) effectiveMode(sessionMode store.AgentMode) store.AgentMode {
	if t.baseMode == sessionMode && t.mode != "" {
		return t.mode
	}
	t.mode = ""
	return sessionMode
}

func (e *Engine) ExternalTurnContext(b *store.ExternalAgentBinding, sessionMode store.AgentMode) (string, store.AgentMode) {
	e.mu.Lock()
	defer e.mu.Unlock()
	if a := e.running[b.SessionID]; a != nil && a.external != nil && a.external.bindingID == b.ID {
		return a.turnID, a.external.effectiveMode(sessionMode)
	}
	return "", sessionMode
}

// The database keeps skill references. Only the reply to the external model receives the live body.
func (e *Engine) ExternalSkillResult(ctx context.Context, sid string, mode store.AgentMode, r tool.Result) (tool.Result, error) {
	if _, ok := tool.ReferencedSkill(r.Name, r.Ok, r.Content); !ok {
		return r, nil
	}
	req := provider.Request{Messages: []provider.Message{{Parts: []provider.Part{{Type: provider.PartToolResult, Name: r.Name, Ok: r.Ok, Content: r.Content}}}}}
	req, err := e.builder.ResolveSkillReferences(ctx, sid, string(mode), req)
	if err != nil {
		return r, err
	}
	part := req.Messages[0].Parts[0]
	r.Content = part.Content
	r.Ok = part.Ok
	return r, nil
}

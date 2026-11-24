package engine

import (
	"context"
	"errors"
	"time"

	"github.com/teatak/pudding-core/internal/acp"
	"github.com/teatak/pudding-core/internal/event"
	"github.com/teatak/pudding-core/internal/store"
)

const ApprovalKindAgentPermission = "agent_permission"

func (e *Engine) requestLocalPermission(ctx context.Context, sid, tid, hostSecret string, permission acp.Permission) acp.Outcome {
	if ctx.Err() != nil {
		return acp.Outcome{Outcome: "cancelled"}
	}
	if outcome, ok := e.localHostPermission(sid, tid, hostSecret, permission); ok {
		return outcome
	}
	req := ApprovalRequest{ID: store.NewID("appr"), SessionID: sid, TurnID: tid, CallID: "acp:" + permission.ToolCall.ToolCallID, Kind: ApprovalKindAgentPermission, Title: "Antigravity", Reason: permission.ToolCall.Title, CreatedAt: time.Now(), Payload: mustJSON(map[string]any{"source": "Antigravity", "toolCall": permission.ToolCall, "options": permission.Options})}
	pending := &pendingApproval{req: req, ch: make(chan approvalDecision, 1), agentOptions: permission.Options}
	e.mu.Lock()
	e.approvals[req.ID] = pending
	e.mu.Unlock()
	defer e.abandonPendingApproval(req.ID, pending)
	e.hub.Publish(event.Event{SessionID: sid, TurnID: tid, CallID: req.CallID, Kind: event.ApprovalRequested, ApprovalID: req.ID, ApprovalKind: req.Kind, Title: req.Title, Reason: req.Reason, Payload: req.Payload})
	select {
	case <-ctx.Done():
		e.hub.Publish(event.Event{SessionID: sid, TurnID: tid, CallID: req.CallID, Kind: event.ApprovalResolved, ApprovalID: req.ID, ApprovalKind: req.Kind, Status: "cancelled"})
		return acp.Outcome{Outcome: "cancelled"}
	case decision := <-pending.ch:
		if ctx.Err() != nil {
			return acp.Outcome{Outcome: "cancelled"}
		}
		if decision.optionID != "" {
			return acp.Outcome{Outcome: "selected", OptionID: decision.optionID}
		}
		for _, option := range permission.Options {
			if option.Kind == "reject_once" {
				return acp.Outcome{Outcome: "selected", OptionID: option.ID}
			}
		}
		return acp.Outcome{Outcome: "cancelled"}
	}
}

// Antigravity names eagerly registered MCP tools mcp_<server>_<tool>. Skip its
// outer confirmation only for our two gateways on this live, owned connection.
// This does not authorize a business operation: the authenticated MCP request
// still goes through Core's session permissions and normal tool approvals.
func (e *Engine) localHostPermission(sid, tid, secret string, permission acp.Permission) (acp.Outcome, bool) {
	e.localMu.Lock()
	defer e.localMu.Unlock()
	host := e.localHosts[secret]
	if host == nil || host.sessionID != sid || host.turn == nil || host.turn.TurnID != tid || host.turn.Context.Err() != nil {
		return acp.Outcome{}, false
	}
	name := permission.ToolCall.Name
	if name != "mcp_"+host.name+"_pudding_tools" && name != "mcp_"+host.name+"_pudding_call" {
		return acp.Outcome{}, false
	}
	for _, option := range permission.Options {
		if option.Kind == "allow_once" && option.ID != "" {
			return acp.Outcome{Outcome: "selected", OptionID: option.ID}, true
		}
	}
	return acp.Outcome{}, false
}

// Manual approvals return exactly an offered option. Persistent ACP choices
// are interpreted by the Agent itself.
func (e *Engine) ResolveLocalPermission(ctx context.Context, sid, id, optionID string) (*store.Session, error) {
	pending, err := e.claimPendingApproval(sid, id)
	if err != nil {
		return nil, err
	}
	defer e.releasePendingApprovalClaim(sid, id, pending)
	if pending.req.Kind != ApprovalKindAgentPermission {
		return nil, ErrApprovalUnsupported
	}
	kind := ""
	for _, option := range pending.agentOptions {
		if option.ID == optionID && optionID != "" {
			kind = option.Kind
			break
		}
	}
	if kind == "" {
		return nil, errors.New("unknown ACP permission option")
	}
	sess, err := e.store.GetSession(ctx, sid)
	if err != nil {
		return nil, err
	}
	e.mu.Lock()
	active := e.running[sid]
	if active == nil || active.turnID != pending.req.TurnID || pending.abandoned || e.approvals[id] != pending {
		e.mu.Unlock()
		return nil, ErrApprovalNotFound
	}
	delete(e.approvals, id)
	e.mu.Unlock()
	status := "denied"
	if kind == "allow_once" || kind == "allow_always" {
		status = "approved"
	}
	e.hub.Publish(event.Event{SessionID: sid, TurnID: pending.req.TurnID, CallID: pending.req.CallID, Kind: event.ApprovalResolved, ApprovalID: id, ApprovalKind: pending.req.Kind, Status: status})
	pending.ch <- approvalDecision{optionID: optionID}
	return sess, nil
}

package engine

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/teatak/pudding-core/internal/event"
	"github.com/teatak/pudding-core/internal/store"
	"github.com/teatak/pudding-core/internal/tool"
)

var ErrApprovalNotFound = errors.New("engine: approval not found")
var ErrWorkspaceDirsRequired = errors.New("engine: workspace dirs required")
var ErrApprovalUnsupported = errors.New("engine: approval unsupported")

const (
	ApprovalKindCapability = "capability"
	ApprovalKindSkillDraft = "skill_draft"
)

type ApprovalScope string

const (
	ApprovalScopeTurn    ApprovalScope = "turn"
	ApprovalScopeSession ApprovalScope = "session"
)

type ApprovalRequest struct {
	ID            string          `json:"id"`
	SessionID     string          `json:"sessionID"`
	TurnID        string          `json:"turnID"`
	CallID        string          `json:"callID"`
	Kind          string          `json:"kind"`
	TargetMode    store.AgentMode `json:"targetMode,omitempty"`
	WorkspaceDirs []string        `json:"workspaceDirs,omitempty"`
	Title         string          `json:"title"`
	Reason        string          `json:"reason"`
	Risk          string          `json:"risk,omitempty"`
	Payload       json.RawMessage `json:"payload,omitempty"`
	CreatedAt     time.Time       `json:"createdAt"`
}

type approvalDecision struct {
	approved      bool
	scope         ApprovalScope
	reason        string
	workspaceDirs []string
}

type pendingApproval struct {
	req ApprovalRequest
	ch  chan approvalDecision
}

type skillDraftApplier interface {
	ApplySkillDraft(ctx context.Context, id string) error
}

func (e *Engine) PendingApprovals(sessionID string) []ApprovalRequest {
	e.mu.Lock()
	defer e.mu.Unlock()
	out := make([]ApprovalRequest, 0)
	for _, p := range e.approvals {
		if p.req.SessionID != sessionID {
			continue
		}
		req := p.req
		req.Payload = append(json.RawMessage(nil), req.Payload...)
		out = append(out, req)
	}
	return out
}

func normalizeApprovalScope(scope ApprovalScope) (ApprovalScope, bool) {
	switch ApprovalScope(strings.TrimSpace(string(scope))) {
	case "", ApprovalScopeTurn:
		return ApprovalScopeTurn, true
	case ApprovalScopeSession:
		return ApprovalScopeSession, true
	default:
		return "", false
	}
}

func (e *Engine) ApproveApproval(ctx context.Context, sessionID, approvalID string, scope ApprovalScope, workspaceDirs []string) error {
	scope, ok := normalizeApprovalScope(scope)
	if !ok {
		return fmt.Errorf("engine: invalid approval scope")
	}
	p, err := e.lookupPendingApproval(sessionID, approvalID)
	if err != nil {
		return err
	}
	switch p.req.Kind {
	case ApprovalKindCapability:
		workspaceDirs = store.NormalizeWorkspaceDirs(workspaceDirs)
		if len(workspaceDirs) == 0 {
			workspaceDirs = append([]string(nil), p.req.WorkspaceDirs...)
		}
		workspaceDirs = store.NormalizeWorkspaceDirs(workspaceDirs)
		if scope == ApprovalScopeSession {
			mode := p.req.TargetMode
			lease := store.ModeLeaseSession
			upd := store.SessionUpdate{ActiveMode: &mode, ModeLease: &lease}
			if p.req.TargetMode == store.ModeWorkspace {
				dirs := workspaceDirs
				if sess, err := e.store.GetSession(ctx, sessionID); err == nil {
					dirs = store.NormalizeWorkspaceDirs(append(sess.WorkspaceDirs, workspaceDirs...))
				}
				upd.WorkspaceDirs = &dirs
			}
			if _, err := e.store.UpdateSession(ctx, sessionID, upd); err != nil {
				return err
			}
		} else if p.req.TargetMode == store.ModeWorkspace {
			e.mu.Lock()
			e.turnWorkspaceDirs[p.req.TurnID] = store.NormalizeWorkspaceDirs(append(e.turnWorkspaceDirs[p.req.TurnID], workspaceDirs...))
			e.mu.Unlock()
		}
	case ApprovalKindSkillDraft:
		draftID := skillDraftApprovalID(p.req.Payload)
		if draftID == "" {
			return fmt.Errorf("engine: skill draft id missing")
		}
		applier, ok := e.tools.(skillDraftApplier)
		if !ok {
			return ErrApprovalUnsupported
		}
		if err := applier.ApplySkillDraft(ctx, draftID); err != nil {
			return err
		}
		scope = ApprovalScopeTurn
	default:
		return ErrApprovalUnsupported
	}
	if err := e.completePendingApproval(sessionID, approvalID, p); err != nil {
		return err
	}
	e.hub.Publish(event.Event{
		SessionID:    p.req.SessionID,
		Kind:         event.ApprovalResolved,
		TurnID:       p.req.TurnID,
		CallID:       p.req.CallID,
		ApprovalID:   p.req.ID,
		ApprovalKind: p.req.Kind,
		Status:       "approved",
		Payload:      approvalResolvedPayload(p.req.Kind, scope, workspaceDirs),
	})
	p.ch <- approvalDecision{approved: true, scope: scope, workspaceDirs: workspaceDirs}
	return nil
}

func (e *Engine) DenyApproval(_ context.Context, sessionID, approvalID, reason string) error {
	p, err := e.lookupPendingApproval(sessionID, approvalID)
	if err != nil {
		return err
	}
	if err := e.completePendingApproval(sessionID, approvalID, p); err != nil {
		return err
	}
	e.hub.Publish(event.Event{
		SessionID:    p.req.SessionID,
		Kind:         event.ApprovalResolved,
		TurnID:       p.req.TurnID,
		CallID:       p.req.CallID,
		ApprovalID:   p.req.ID,
		ApprovalKind: p.req.Kind,
		Status:       "denied",
		Reason:       reason,
	})
	p.ch <- approvalDecision{approved: false, reason: reason}
	return nil
}

func (e *Engine) lookupPendingApproval(sessionID, approvalID string) (*pendingApproval, error) {
	e.mu.Lock()
	defer e.mu.Unlock()
	p := e.approvals[approvalID]
	if p == nil || p.req.SessionID != sessionID {
		return nil, ErrApprovalNotFound
	}
	return p, nil
}

func (e *Engine) completePendingApproval(sessionID, approvalID string, p *pendingApproval) error {
	e.mu.Lock()
	defer e.mu.Unlock()
	if e.approvals[approvalID] != p || p.req.SessionID != sessionID {
		return ErrApprovalNotFound
	}
	delete(e.approvals, approvalID)
	return nil
}

func (e *Engine) requestCapabilityApproval(ctx context.Context, sessionID, turnID string, call tool.Call, currentMode store.AgentMode) (tool.Result, store.AgentMode, bool) {
	var req tool.CapabilityRequest
	if err := json.Unmarshal(call.Args, &req); err != nil {
		return capabilityToolResult(call, false, map[string]any{"ok": false, "reason": "invalid_arguments", "error": err.Error()}), currentMode, false
	}
	req.TargetMode = store.NormalizeAgentMode(req.TargetMode)
	req.WorkspaceDirs = store.NormalizeWorkspaceDirs(req.WorkspaceDirs)
	req.SuggestedDirName = strings.TrimSpace(req.SuggestedDirName)
	currentMode = store.NormalizeAgentMode(currentMode)
	if req.TargetMode != store.ModeWorkspace {
		return capabilityToolResult(call, false, map[string]any{"ok": false, "reason": "invalid_target_mode"}), currentMode, false
	}
	if currentMode == store.ModeWorkspace && len(req.WorkspaceDirs) == 0 && !req.NeedsWorkspaceDir {
		return capabilityToolResult(call, false, map[string]any{"ok": false, "reason": "workspace_dirs_required"}), currentMode, false
	}
	if currentMode != store.ModeWorkspace && store.AgentModeRank(req.TargetMode) <= store.AgentModeRank(currentMode) {
		return capabilityToolResult(call, false, map[string]any{"ok": false, "reason": "target_mode_not_higher", "currentMode": currentMode, "targetMode": req.TargetMode}), currentMode, false
	}
	payload := mustJSON(req)
	approval := ApprovalRequest{
		ID:            store.NewID("appr"),
		SessionID:     sessionID,
		TurnID:        turnID,
		CallID:        call.CallID,
		Kind:          ApprovalKindCapability,
		TargetMode:    req.TargetMode,
		WorkspaceDirs: req.WorkspaceDirs,
		Reason:        strings.TrimSpace(req.Reason),
		Risk:          strings.TrimSpace(req.Risk),
		Payload:       payload,
		CreatedAt:     time.Now(),
	}
	pending := &pendingApproval{req: approval, ch: make(chan approvalDecision, 1)}
	e.mu.Lock()
	e.approvals[approval.ID] = pending
	e.mu.Unlock()
	e.hub.Publish(event.Event{
		SessionID:    sessionID,
		Kind:         event.ApprovalRequested,
		TurnID:       turnID,
		CallID:       call.CallID,
		ApprovalID:   approval.ID,
		ApprovalKind: approval.Kind,
		Title:        approval.Title,
		Reason:       approval.Reason,
		Risk:         approval.Risk,
		Payload:      approval.Payload,
	})

	select {
	case <-ctx.Done():
		e.mu.Lock()
		if e.approvals[approval.ID] == pending {
			delete(e.approvals, approval.ID)
		}
		e.mu.Unlock()
		return capabilityToolResult(call, false, map[string]any{"ok": false, "reason": "cancelled"}), currentMode, false
	case decision := <-pending.ch:
		if !decision.approved {
			return capabilityToolResult(call, false, map[string]any{"ok": false, "status": "denied", "reason": decision.reason}), currentMode, false
		}
		return capabilityToolResult(call, true, map[string]any{"ok": true, "status": "approved", "scope": decision.scope, "mode": req.TargetMode, "workspaceDirs": decision.workspaceDirs}), req.TargetMode, true
	}
}

func (e *Engine) requestSkillDraftApproval(ctx context.Context, sessionID, turnID string, call tool.Call) tool.Result {
	id := skillDraftIDFromToolArgs(call.Args)
	if id == "" {
		return approvalToolResult(call, false, map[string]any{"ok": false, "reason": "draft_id_required"})
	}
	if e.tools == nil {
		return approvalToolResult(call, false, map[string]any{"ok": false, "reason": "tool_runner_unavailable"})
	}
	toolCtx, cancel := context.WithTimeout(ctx, toolCallTimeout)
	result := e.tools.Call(toolCtx, call)
	cancel()
	if toolCtx.Err() != nil && result.Content == "" {
		return approvalToolResult(call, false, map[string]any{"ok": false, "reason": "tool_timed_out"})
	}
	if !result.Ok {
		return result
	}
	payload := skillDraftApprovalPayload(id, result.Content)
	approval := ApprovalRequest{
		ID:        store.NewID("appr"),
		SessionID: sessionID,
		TurnID:    turnID,
		CallID:    call.CallID,
		Kind:      ApprovalKindSkillDraft,
		Title:     "Publish skill draft",
		Reason:    skillDraftApprovalReason(payload),
		Payload:   mustJSON(payload),
		CreatedAt: time.Now(),
	}
	pending := &pendingApproval{req: approval, ch: make(chan approvalDecision, 1)}
	e.mu.Lock()
	e.approvals[approval.ID] = pending
	e.mu.Unlock()
	e.hub.Publish(event.Event{
		SessionID:    sessionID,
		Kind:         event.ApprovalRequested,
		TurnID:       turnID,
		CallID:       call.CallID,
		ApprovalID:   approval.ID,
		ApprovalKind: approval.Kind,
		Title:        approval.Title,
		Reason:       approval.Reason,
		Payload:      approval.Payload,
	})
	payload["ok"] = true
	payload["status"] = "pending_user_review"
	payload["approval_id"] = approval.ID
	if _, ok := payload["draft_id"]; !ok {
		payload["draft_id"] = id
	}
	return approvalToolResult(call, true, payload)
}

func capabilityToolResult(call tool.Call, ok bool, payload map[string]any) tool.Result {
	return approvalToolResult(call, ok, payload)
}

func approvalToolResult(call tool.Call, ok bool, payload map[string]any) tool.Result {
	b, err := json.Marshal(payload)
	if err != nil {
		b = []byte(`{"ok":false,"reason":"encode_error"}`)
		ok = false
	}
	return tool.Result{CallID: call.CallID, Name: call.Name, Ok: ok, Content: string(b), SummaryKind: tool.SummaryReturnedFields, SummaryCount: len(payload)}
}

func skillDraftIDFromToolArgs(raw json.RawMessage) string {
	var args struct {
		DraftID string `json:"draft_id"`
	}
	if len(raw) == 0 || json.Unmarshal(raw, &args) != nil {
		return ""
	}
	return strings.TrimSpace(args.DraftID)
}

func skillDraftApprovalPayload(id, content string) map[string]any {
	payload := map[string]any{"draft_id": id}
	var parsed map[string]any
	if json.Unmarshal([]byte(content), &parsed) == nil {
		for key, value := range parsed {
			payload[key] = value
		}
	}
	payload["draft_id"] = id
	return payload
}

func skillDraftApprovalID(payload json.RawMessage) string {
	var data struct {
		DraftID string `json:"draft_id"`
	}
	if len(payload) == 0 || json.Unmarshal(payload, &data) != nil {
		return ""
	}
	return strings.TrimSpace(data.DraftID)
}

func skillDraftApprovalReason(payload map[string]any) string {
	draft, ok := payload["draft"].(map[string]any)
	if !ok {
		return ""
	}
	if description, ok := draft["description"].(string); ok {
		return strings.TrimSpace(description)
	}
	return ""
}

func approvalResolvedPayload(kind string, scope ApprovalScope, workspaceDirs []string) json.RawMessage {
	if kind == ApprovalKindSkillDraft {
		return mustJSON(map[string]any{"published": true})
	}
	return mustJSON(map[string]any{"scope": scope, "workspaceDirs": workspaceDirs})
}

func mustJSON(v any) json.RawMessage {
	b, err := json.Marshal(v)
	if err != nil {
		return nil
	}
	return b
}

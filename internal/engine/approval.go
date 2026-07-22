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
var ErrProjectDirsRequired = errors.New("engine: project dirs required")
var ErrApprovalUnsupported = errors.New("engine: approval unsupported")

const (
	ApprovalKindCapability = "capability"
	ApprovalKindToolCall   = "tool_call"
)

type ApprovalScope string

const (
	ApprovalScopeTurn    ApprovalScope = "turn"
	ApprovalScopeSession ApprovalScope = "session"
)

type ApprovalRequest struct {
	ID          string          `json:"id"`
	SessionID   string          `json:"sessionID"`
	TurnID      string          `json:"turnID"`
	CallID      string          `json:"callID"`
	Kind        string          `json:"kind"`
	TargetMode  store.AgentMode `json:"targetMode,omitempty"`
	ProjectDirs []string        `json:"projectDirs,omitempty"`
	Title       string          `json:"title"`
	Reason      string          `json:"reason"`
	Risk        string          `json:"risk,omitempty"`
	Payload     json.RawMessage `json:"payload,omitempty"`
	CreatedAt   time.Time       `json:"createdAt"`
}

type approvalDecision struct {
	approved    bool
	scope       ApprovalScope
	reason      string
	projectDirs []string
}

type ProjectAccessGrant struct {
	RootDirs []string
}

type pendingApproval struct {
	req ApprovalRequest
	ch  chan approvalDecision
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

func (e *Engine) ApproveApproval(ctx context.Context, sessionID, approvalID string, scope ApprovalScope, projectDirs []string) error {
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
		if p.req.TargetMode == store.ModeCode {
			projectDirs = store.NormalizeProjectDirs(projectDirs)
			if len(projectDirs) == 0 {
				projectDirs = append([]string(nil), p.req.ProjectDirs...)
			}
			projectDirs = store.NormalizeProjectDirs(projectDirs)
		} else {
			projectDirs = nil
		}
		if scope == ApprovalScopeSession {
			mode := p.req.TargetMode
			lease := store.ModeLeaseSession
			upd := store.SessionUpdate{ActiveMode: &mode, ModeLease: &lease}
			if p.req.TargetMode == store.ModeCode {
				if len(projectDirs) > 0 {
					project, err := e.bindSessionProject(ctx, sessionID, projectDirs)
					if err != nil {
						return err
					}
					upd.ProjectID = &project.ID
				}
			}
			if _, err := e.store.UpdateSession(ctx, sessionID, upd); err != nil {
				return err
			}
		} else if p.req.TargetMode == store.ModeCode && len(projectDirs) > 0 {
			e.mu.Lock()
			grant := e.turnProjectAccess[p.req.TurnID]
			grant.RootDirs = store.NormalizeProjectDirs(append(grant.RootDirs, projectDirs...))
			e.turnProjectAccess[p.req.TurnID] = grant
			e.mu.Unlock()
		}
	case ApprovalKindToolCall:
		scope = ApprovalScopeTurn
		projectDirs = nil
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
		Payload:      approvalResolvedPayload(p.req.Kind, scope, projectDirs),
	})
	p.ch <- approvalDecision{approved: true, scope: scope, projectDirs: projectDirs}
	return nil
}

func (e *Engine) bindSessionProject(ctx context.Context, sessionID string, rootDirs []string) (*store.Project, error) {
	rootDirs = store.NormalizeProjectDirs(rootDirs)
	if len(rootDirs) == 0 {
		return nil, ErrProjectDirsRequired
	}
	sess, err := e.store.GetSession(ctx, sessionID)
	if err != nil {
		return nil, err
	}
	if sess.ProjectID != "" {
		project, err := e.store.GetProject(ctx, sess.ProjectID)
		if err != nil {
			return nil, err
		}
		merged := store.NormalizeProjectDirs(append(project.RootDirs, rootDirs...))
		if !sameStringList(project.RootDirs, merged) {
			project, err = e.store.UpdateProject(ctx, project.ID, store.ProjectUpdate{RootDirs: &merged})
			if err != nil {
				return nil, err
			}
		}
		return project, nil
	}
	return e.projectForRootDirs(ctx, rootDirs)
}

func (e *Engine) projectForRootDirs(ctx context.Context, rootDirs []string) (*store.Project, error) {
	rootDirs = store.NormalizeProjectDirs(rootDirs)
	if len(rootDirs) == 0 {
		return nil, ErrProjectDirsRequired
	}
	projects, err := e.store.ListProjects(ctx)
	if err != nil {
		return nil, err
	}
	for _, project := range projects {
		if project == nil {
			continue
		}
		if sameStringList(project.RootDirs, rootDirs) {
			return project, nil
		}
	}
	project := &store.Project{
		ID:           store.NewID("proj"),
		RootDirs:     rootDirs,
		ApprovalMode: store.ApprovalAuto,
	}
	if err := e.store.CreateProject(ctx, project); err != nil {
		return nil, err
	}
	return project, nil
}

func sameStringList(left, right []string) bool {
	if len(left) != len(right) {
		return false
	}
	for i := range left {
		if left[i] != right[i] {
			return false
		}
	}
	return true
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
	targetMode, publicTargetMode := normalizeCapabilityTargetMode(req.TargetMode)
	req.TargetMode = targetMode
	req.ProjectDirs = store.NormalizeProjectDirs(req.ProjectDirs)
	req.SuggestedDirName = strings.TrimSpace(req.SuggestedDirName)
	currentMode = store.NormalizeAgentMode(currentMode)
	if currentMode == "" {
		currentMode = store.ModeChat
	}
	if req.TargetMode != store.ModeWork && req.TargetMode != store.ModeCode {
		return capabilityToolResult(call, false, map[string]any{"ok": false, "reason": "invalid_target_mode"}), currentMode, false
	}
	if req.TargetMode == store.ModeWork && (len(req.ProjectDirs) > 0 || req.NeedsProjectDir || req.SuggestedDirName != "") {
		return capabilityToolResult(call, false, map[string]any{"ok": false, "reason": "project_dirs_not_allowed"}), currentMode, false
	}
	if currentMode == store.ModeCode && req.TargetMode == store.ModeCode && len(req.ProjectDirs) == 0 && !req.NeedsProjectDir {
		return capabilityToolResult(call, true, map[string]any{
			"ok":     true,
			"status": "already_available",
			"mode":   string(store.ModeCode),
		}), currentMode, false
	}
	if !(currentMode == store.ModeCode && req.TargetMode == store.ModeCode) && store.AgentModeRank(req.TargetMode) <= store.AgentModeRank(currentMode) {
		return capabilityToolResult(call, false, map[string]any{"ok": false, "reason": "target_mode_not_higher", "currentMode": currentMode, "targetMode": publicTargetMode}), currentMode, false
	}
	payload := capabilityApprovalPayload(req, publicTargetMode)
	approval := ApprovalRequest{
		ID:          store.NewID("appr"),
		SessionID:   sessionID,
		TurnID:      turnID,
		CallID:      call.CallID,
		Kind:        ApprovalKindCapability,
		TargetMode:  req.TargetMode,
		ProjectDirs: append([]string(nil), req.ProjectDirs...),
		Reason:      strings.TrimSpace(req.Reason),
		Risk:        strings.TrimSpace(req.Risk),
		Payload:     payload,
		CreatedAt:   time.Now(),
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
		return capabilityToolResult(call, true, map[string]any{"ok": true, "status": "approved", "scope": decision.scope, "mode": publicTargetMode, "projectDirs": decision.projectDirs}), req.TargetMode, true
	}
}

func normalizeCapabilityTargetMode(mode store.AgentMode) (store.AgentMode, string) {
	switch store.AgentMode(strings.TrimSpace(strings.ToLower(string(mode)))) {
	case store.ModeWork:
		return store.ModeWork, string(store.ModeWork)
	case store.ModeCode:
		return store.ModeCode, string(store.ModeCode)
	default:
		return "", ""
	}
}

func capabilityApprovalPayload(req tool.CapabilityRequest, publicTargetMode string) json.RawMessage {
	return mustJSON(map[string]any{
		"targetMode":       publicTargetMode,
		"reason":           strings.TrimSpace(req.Reason),
		"projectDirs":      req.ProjectDirs,
		"needsProjectDir":  req.NeedsProjectDir,
		"suggestedDirName": req.SuggestedDirName,
		"risk":             strings.TrimSpace(req.Risk),
	})
}

func (e *Engine) requestToolCallApproval(ctx context.Context, sessionID, turnID string, call tool.Call, risk tool.ToolRisk, project *store.Project, details map[string]any) (tool.Result, bool) {
	payload := map[string]any{
		"toolName":  call.Name,
		"riskClass": string(risk.Class),
		"operation": risk.Operation,
		"scope":     risk.Scope,
		"paths":     risk.Paths,
		"lowRisk":   risk.LowRisk,
	}
	for key, value := range details {
		payload[key] = value
	}
	if project != nil {
		payload["projectID"] = project.ID
		payload["projectName"] = project.Name
	}
	approval := ApprovalRequest{
		ID:        store.NewID("appr"),
		SessionID: sessionID,
		TurnID:    turnID,
		CallID:    call.CallID,
		Kind:      ApprovalKindToolCall,
		Title:     "Approve tool call",
		Reason:    risk.Summary,
		Risk:      string(risk.Class),
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
		return approvalToolResult(call, false, map[string]any{"ok": false, "reason": "cancelled"}), false
	case decision := <-pending.ch:
		if !decision.approved {
			return approvalToolResult(call, false, map[string]any{"ok": false, "status": "denied", "reason": decision.reason}), false
		}
		return tool.Result{}, true
	}
}

func (e *Engine) toolCallApprovalRequired(ctx context.Context, sessionID string, risk tool.ToolRisk) (*store.Project, bool, error) {
	project, err := e.projectForToolCallPolicy(ctx, sessionID)
	if err != nil {
		return nil, false, err
	}
	mode := store.ApprovalAuto
	if project != nil {
		mode = store.NormalizeApprovalMode(project.ApprovalMode)
	}
	switch mode {
	case store.ApprovalFull:
		return project, false, nil
	case store.ApprovalAsk:
		return project, true, nil
	default:
		switch risk.Class {
		case tool.RiskClassRead:
			return project, false, nil
		case tool.RiskClassWrite:
			return project, !risk.LowRisk, nil
		case tool.RiskClassDestructive:
			return project, true, nil
		case tool.RiskClassCommand:
			return project, !risk.LowRisk, nil
		default:
			return project, false, nil
		}
	}
}

func commandSandboxModeForProject(project *store.Project) tool.CommandSandboxMode {
	if project != nil && store.NormalizeApprovalMode(project.ApprovalMode) == store.ApprovalFull {
		return tool.CommandSandboxBypass
	}
	return tool.CommandSandboxEnforce
}

func refineToolRisk(name string, risk tool.ToolRisk, details map[string]any) tool.ToolRisk {
	if name == tool.FilePatch {
		if destructive, _ := details["destructive"].(bool); destructive {
			risk.Class = tool.RiskClassDestructive
			risk.LowRisk = false
			risk.Summary = "Apply a multi-file patch that deletes project files."
		}
	}
	return risk
}

func (e *Engine) projectForToolCallPolicy(ctx context.Context, sessionID string) (*store.Project, error) {
	if sess, err := e.store.GetSession(ctx, sessionID); err != nil {
		return nil, err
	} else if sess.ProjectID != "" {
		project, err := e.store.GetProject(ctx, sess.ProjectID)
		if err != nil {
			return nil, err
		}
		return project, nil
	}
	return nil, nil
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

func approvalResolvedPayload(kind string, scope ApprovalScope, projectDirs []string) json.RawMessage {
	if kind == ApprovalKindToolCall {
		return mustJSON(map[string]any{"scope": ApprovalScopeTurn})
	}
	return mustJSON(map[string]any{"scope": scope, "projectDirs": projectDirs})
}

func mustJSON(v any) json.RawMessage {
	b, err := json.Marshal(v)
	if err != nil {
		return nil
	}
	return b
}

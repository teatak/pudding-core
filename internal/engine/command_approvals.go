package engine

import (
	"context"
	"encoding/json"

	"github.com/teatak/pudding-core/internal/store"
	"github.com/teatak/pudding-core/internal/tool"
)

// This in-memory state is the sole authority for command leases. Replacing the
// pointer invalidates both existing leases and approvals already on screen.
type commandApprovalState struct {
	context   string
	projectID string
	// Nil means no extra directories; map membership is the grant.
	identities map[string]tool.CommandGrantIdentity
	grants     map[string]*tool.CommandProjectAccess
	reasons    map[string]int
	reused     int
}

type CommandApprovalStatus struct {
	DiagnosticsEnabled bool           `json:"diagnosticsEnabled"`
	ProjectDirs        []string       `json:"projectDirs"`
	GrantCount         int            `json:"grantCount"`
	ApprovalReasons    map[string]int `json:"approvalReasons"`
	ReuseMissReasons   map[string]int `json:"reuseMissReasons"`
	ReusedCount        int            `json:"reusedCount"`
}

func commandApprovalContext(project *store.Project, roots []string) string {
	var projectID string
	mode := store.ApprovalAuto
	if project != nil {
		projectID, mode = project.ID, store.NormalizeApprovalMode(project.ApprovalMode)
	}
	data, _ := json.Marshal([]any{projectID, mode, store.NormalizeProjectDirs(roots)})
	return string(data)
}

func (e *Engine) commandApprovalState(sessionID string, project *store.Project, roots []string) *commandApprovalState {
	e.mu.Lock()
	defer e.mu.Unlock()
	key := commandApprovalContext(project, roots)
	state := e.commandGrants[sessionID]
	if state == nil || state.context != key {
		if state != nil && e.commandDiagnosticsEnabled {
			e.commandReuseDiagnosticLocked(sessionID).lastReset = "scope_changed"
		}
		state = &commandApprovalState{context: key, grants: make(map[string]*tool.CommandProjectAccess), identities: make(map[string]tool.CommandGrantIdentity), reasons: make(map[string]int)}
		if project != nil {
			state.projectID = project.ID
		}
		e.commandGrants[sessionID] = state
	}
	return state
}

func (e *Engine) commandApprovalStillCurrent(ctx context.Context, sessionID, turnID string, mode store.AgentMode, state *commandApprovalState) bool {
	project, err := e.projectForToolCallPolicy(ctx, sessionID)
	if err != nil {
		return false
	}
	roots, err := e.projectRootDirsForToolCall(ctx, sessionID, turnID, mode)
	if err != nil || commandApprovalContext(project, roots) != state.context {
		return false
	}
	e.mu.Lock()
	defer e.mu.Unlock()
	return e.commandGrants[sessionID] == state
}

func (e *Engine) CommandApprovals(sessionID string) CommandApprovalStatus {
	e.mu.Lock()
	defer e.mu.Unlock()
	result := CommandApprovalStatus{DiagnosticsEnabled: e.commandDiagnosticsEnabled, ApprovalReasons: make(map[string]int), ReuseMissReasons: make(map[string]int), ProjectDirs: []string{}}
	if diagnostics := e.commandReuseDiagnostics[sessionID]; diagnostics != nil {
		for reason, count := range diagnostics.misses {
			result.ReuseMissReasons[reason] = count
		}
	}
	if state := e.sessionProjectAccess[sessionID]; state != nil {
		result.ProjectDirs = append(result.ProjectDirs, state.dirs...)
	}
	if state := e.commandGrants[sessionID]; state != nil {
		result.GrantCount, result.ReusedCount = len(state.grants), state.reused
		for reason, count := range state.reasons {
			result.ApprovalReasons[reason] = count
		}
	}
	return result
}

// This endpoint revokes both temporary directory and command permissions.
// Revocation affects future dispatches only, not already-running processes.
func (e *Engine) RevokeCommandApprovals(sessionID string) {
	e.mu.Lock()
	defer e.mu.Unlock()
	if e.commandDiagnosticsEnabled {
		e.commandReuseDiagnosticLocked(sessionID).lastReset = "authorization_revoked"
	}
	delete(e.commandGrants, sessionID)
	delete(e.sessionProjectAccess, sessionID)
	for id, grant := range e.turnProjectAccess {
		if grant.SessionID == sessionID {
			delete(e.turnProjectAccess, id)
		}
	}
}

func (e *Engine) RevokeProjectCommandApprovals(projectID string) {
	e.mu.Lock()
	defer e.mu.Unlock()
	for id, state := range e.sessionProjectAccess {
		if state.projectID == projectID {
			delete(e.sessionProjectAccess, id)
		}
	}
	for id, grant := range e.turnProjectAccess {
		if grant.ProjectID == projectID {
			delete(e.turnProjectAccess, id)
		}
	}
	for id, state := range e.commandGrants {
		if state.projectID == projectID {
			if e.commandDiagnosticsEnabled {
				e.commandReuseDiagnosticLocked(id).lastReset = "scope_changed"
			}
			delete(e.commandGrants, id)
		}
	}
}

func commandApprovalReasons(risk tool.ToolRisk) []string {
	if len(risk.ApprovalReasons) > 0 {
		return risk.ApprovalReasons
	}
	return []string{"ask_mode"}
}

package engine

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/teatak/pudding-core/internal/provider"
	"github.com/teatak/pudding-core/internal/store"
)

// Directory leases are runtime permissions. They never create or mutate a
// project, and replacing this state also invalidates approvals still on screen.
type projectAccessState struct {
	projectID string
	dirs      []string
}

func (e *Engine) projectAccessState(sessionID, projectID string) *projectAccessState {
	e.mu.Lock()
	defer e.mu.Unlock()
	state := e.sessionProjectAccess[sessionID]
	if state == nil || state.projectID != projectID {
		state = &projectAccessState{projectID: projectID}
		e.sessionProjectAccess[sessionID] = state
	}
	return state
}

func approvedDirectories(dirs []string) ([]string, error) {
	var out []string
	for _, dir := range store.NormalizeProjectDirs(dirs) {
		if !filepath.IsAbs(dir) {
			return nil, fmt.Errorf("directory must be absolute: %s", dir)
		}
		resolved, err := filepath.EvalSymlinks(dir)
		if err != nil {
			return nil, err
		}
		info, err := os.Stat(resolved)
		if err != nil {
			return nil, err
		}
		if !info.IsDir() {
			return nil, fmt.Errorf("not a directory: %s", dir)
		}
		out = append(out, resolved)
	}
	return store.NormalizeProjectDirs(out), nil
}

func directoriesOutside(asked, available []string) []string {
	var missing []string
	for _, dir := range asked {
		covered := false
		for _, root := range available {
			resolved, err := filepath.EvalSymlinks(root)
			if err != nil {
				continue
			}
			rel, err := filepath.Rel(resolved, dir)
			if err == nil && rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
				covered = true
				break
			}
		}
		if !covered {
			missing = append(missing, dir)
		}
	}
	return missing
}

// A request-time projection of the same leases used by tool execution. It is
// never persisted or reconstructed from historical approval results.
type temporaryProjectAccess struct {
	Session []string `json:"session"`
	Turn    []string `json:"turn"`
}

func (e *Engine) temporaryProjectAccess(ctx context.Context, sessionID, turnID string) (temporaryProjectAccess, error) {
	access := temporaryProjectAccess{Session: []string{}, Turn: []string{}}
	sess, err := e.store.GetSession(ctx, sessionID)
	if err != nil {
		return access, err
	}
	e.mu.Lock()
	defer e.mu.Unlock()
	if state := e.sessionProjectAccess[sessionID]; state != nil && state.projectID == sess.ProjectID {
		access.Session = append(access.Session, state.dirs...)
	}
	if grant := e.turnProjectAccess[turnID]; grant.SessionID == sessionID && grant.ProjectID == sess.ProjectID {
		access.Turn = append(access.Turn, grant.RootDirs...)
	}
	return access, nil
}

func (e *Engine) temporaryProjectDirs(ctx context.Context, sessionID, turnID string) ([]string, error) {
	access, err := e.temporaryProjectAccess(ctx, sessionID, turnID)
	return store.NormalizeProjectDirs(append(access.Session, access.Turn...)), err
}

func (e *Engine) appendTemporaryProjectAccess(ctx context.Context, sessionID, turnID string, mode store.AgentMode, req *provider.Request) error {
	if store.NormalizeAgentMode(mode) != store.ModeCode {
		return nil
	}
	access, err := e.temporaryProjectAccess(ctx, sessionID, turnID)
	if err != nil {
		return err
	}
	data, err := json.Marshal(access)
	if err != nil {
		return err
	}
	req.System += "\n\n## Current Temporary Directory Access\n\n" +
		"These additional directories are currently authorized for Code tools, alongside the Project roots. " +
		"The turn list expires when this turn ends; the session list lasts only for this conversation in this daemon run. " +
		"Revocation, project changes or mode downgrades invalidate these permissions. " +
		"Use this current snapshot, not historical approval results, to determine available directories. " +
		"Fixed-command approvals are separate: they allow only the approved invocation and do not grant general directory access.\n" + string(data)
	return nil
}

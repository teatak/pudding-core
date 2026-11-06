package engine

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"github.com/teatak/pudding-core/internal/event"
	"github.com/teatak/pudding-core/internal/sessionworkspace"

	"github.com/teatak/pudding-core/internal/provider"
	"github.com/teatak/pudding-core/internal/store"
)

// This runtime identity invalidates pending approvals when permissions change.
// Persistent directory consent lives in the store, independently of a project.
type projectAccessState struct {
	projectID string
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

// DirectoryAccess is the current consent snapshot, never reconstructed from chat history.
type DirectoryAccess struct {
	Session []string `json:"session"`
	Turn    []string `json:"turn"`
}

func (e *Engine) DirectoryAccess(ctx context.Context, sessionID, turnID string) (DirectoryAccess, error) {
	sess, err := e.store.GetSession(ctx, sessionID)
	if err != nil {
		return DirectoryAccess{}, err
	}
	dirs, err := e.store.GetSessionDirectories(ctx, sessionID)
	if err != nil {
		return DirectoryAccess{}, err
	}
	access := DirectoryAccess{Session: dirs, Turn: []string{}}
	e.mu.Lock()
	defer e.mu.Unlock()
	for id, grant := range e.turnProjectAccess {
		if (turnID == "" || id == turnID) && grant.SessionID == sessionID && grant.ProjectID == sess.ProjectID {
			access.Turn = append(access.Turn, grant.RootDirs...)
		}
	}
	access.Turn = store.NormalizeProjectDirs(access.Turn)
	return access, nil
}

// SessionWorkspace is shared by tool dispatch and the file/search/Git APIs.
// An empty turnID includes the session's currently active turn grants.
func (e *Engine) SessionWorkspace(ctx context.Context, sessionID, turnID string, scratch sessionworkspace.ScratchPolicy) (sessionworkspace.Workspace, error) {
	workspace, err := sessionworkspace.Resolve(ctx, e.store, e.attachmentHome, sessionID, sessionworkspace.ScratchDisabled)
	if err != nil {
		return workspace, err
	}
	access, err := e.DirectoryAccess(ctx, sessionID, turnID)
	if err != nil {
		return workspace, err
	}
	workspace.AddRoots("turn", access.Turn)
	if len(access.Turn) > 0 {
		workspace.Temporary = false
	}
	err = workspace.IncludeScratch(e.attachmentHome, sessionID, scratch)
	return workspace, err
}

var ErrInvalidSessionDirectories = errors.New("invalid_session_directories")

func (e *Engine) SetSessionDirectories(ctx context.Context, sessionID string, dirs []string) ([]string, error) {
	// Retain offline paths already approved; validate every newly added path.
	current, err := e.store.GetSessionDirectories(ctx, sessionID)
	if err != nil {
		return nil, err
	}
	next := make([]string, 0, len(dirs))
	for _, dir := range dirs {
		if slices.Contains(current, dir) {
			next = append(next, dir)
			continue
		}
		if !filepath.IsAbs(dir) {
			return nil, fmt.Errorf("%w: expected absolute path", ErrInvalidSessionDirectories)
		}
		checked, err := approvedDirectories([]string{dir})
		if err != nil {
			return nil, fmt.Errorf("%w: %v", ErrInvalidSessionDirectories, err)
		}
		next = append(next, checked...)
	}
	next = store.NormalizeProjectDirs(next)
	e.mu.Lock()
	_, err = e.store.UpdateSession(ctx, sessionID, store.SessionUpdate{DirectoryGrants: &next})
	if err == nil {
		// A directory edit invalidates pending capability and command approvals.
		delete(e.sessionProjectAccess, sessionID)
		delete(e.commandGrants, sessionID)
	}
	e.mu.Unlock()
	if err != nil {
		return nil, err
	}
	e.hub.Publish(event.Event{Kind: event.SessionDirectoriesChanged, SessionID: sessionID})
	return next, nil
}

func (e *Engine) appendDirectoryAccess(ctx context.Context, sessionID, turnID string, mode store.AgentMode, req *provider.Request) error {
	if store.NormalizeAgentMode(mode) != store.ModeCode {
		return nil
	}
	access, err := e.DirectoryAccess(ctx, sessionID, turnID)
	if err != nil {
		return err
	}
	data, err := json.Marshal(access)
	if err != nil {
		return err
	}
	req.System += "\n\n## Current Directory Access\n\n" +
		"These directories are authorized alongside any Project roots. The session list is saved for this conversation across restarts until explicitly revoked or the conversation is deleted. The turn list expires when this turn ends. " +
		"Use this current snapshot, not historical approval results. Directory consent does not grant host command execution; fixed-command approvals are separate and retain their existing lifetime.\n" + string(data)
	return nil
}

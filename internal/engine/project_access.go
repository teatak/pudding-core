package engine

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"

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

func (e *Engine) temporaryProjectDirs(ctx context.Context, sessionID, turnID string) ([]string, error) {
	sess, err := e.store.GetSession(ctx, sessionID)
	if err != nil {
		return nil, err
	}
	e.mu.Lock()
	defer e.mu.Unlock()
	var dirs []string
	if state := e.sessionProjectAccess[sessionID]; state != nil && state.projectID == sess.ProjectID {
		dirs = append(dirs, state.dirs...)
	}
	if grant := e.turnProjectAccess[turnID]; grant.SessionID == sessionID && grant.ProjectID == sess.ProjectID {
		dirs = append(dirs, grant.RootDirs...)
	}
	return dirs, nil
}

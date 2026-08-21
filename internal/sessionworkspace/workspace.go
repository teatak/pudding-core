package sessionworkspace

import (
	"context"
	"strings"

	"github.com/teatak/pudding-core/internal/home"
	"github.com/teatak/pudding-core/internal/store"
)

type ScratchPolicy int

const (
	ScratchDisabled ScratchPolicy = iota
	ScratchExisting
	ScratchEnsure
)

type Workspace struct {
	Project     *store.Project
	RootDirs    []string
	ScratchRoot string
	Temporary   bool
}

// Resolve is the single source of truth for a session's persistent file
// workspace: Project roots plus any existing isolated Code scratch.
func Resolve(
	ctx context.Context,
	s store.Store,
	homeDir string,
	sessionID string,
	scratchPolicy ScratchPolicy,
) (Workspace, error) {
	session, err := s.GetSession(ctx, strings.TrimSpace(sessionID))
	if err != nil {
		return Workspace{}, err
	}
	if strings.TrimSpace(session.ProjectID) != "" {
		project, err := s.GetProject(ctx, session.ProjectID)
		if err != nil {
			return Workspace{}, err
		}
		workspace := Workspace{Project: project, RootDirs: store.NormalizeProjectDirs(project.RootDirs)}
		if scratchPolicy == ScratchExisting || scratchPolicy == ScratchEnsure {
			root, exists, err := home.ExistingCodeScratch(homeDir, session.ID)
			if err != nil {
				return Workspace{}, err
			}
			if !exists && scratchPolicy == ScratchEnsure && len(workspace.RootDirs) == 0 {
				root, err = home.PrepareCodeScratch(homeDir, session.ID)
				if err != nil {
					return Workspace{}, err
				}
				exists = true
			}
			if exists {
				workspace.ScratchRoot = root
				workspace.RootDirs = store.NormalizeProjectDirs(append(workspace.RootDirs, root))
			}
		}
		return workspace, nil
	}
	if scratchPolicy == ScratchDisabled {
		return Workspace{Temporary: true}, nil
	}
	if scratchPolicy == ScratchEnsure {
		root, err := home.PrepareCodeScratch(homeDir, session.ID)
		if err != nil {
			return Workspace{}, err
		}
		return Workspace{RootDirs: []string{root}, ScratchRoot: root, Temporary: true}, nil
	}
	root, exists, err := home.ExistingCodeScratch(homeDir, session.ID)
	if err != nil {
		return Workspace{}, err
	}
	if !exists {
		return Workspace{Temporary: true}, nil
	}
	return Workspace{RootDirs: []string{root}, ScratchRoot: root, Temporary: true}, nil
}

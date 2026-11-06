package sessionworkspace

import (
	"context"
	"slices"
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
	RootSources map[string][]string
	ScratchRoot string
	Temporary   bool
}

func (w *Workspace) AddRoots(source string, paths []string) {
	if w.RootSources == nil {
		w.RootSources = make(map[string][]string)
	}
	for _, path := range store.NormalizeProjectDirs(paths) {
		if !slices.Contains(w.RootDirs, path) {
			w.RootDirs = append(w.RootDirs, path)
		}
		if !slices.Contains(w.RootSources[path], source) {
			w.RootSources[path] = append(w.RootSources[path], source)
		}
	}
}

// Resolve combines persistent project and session consent. The engine adds
// active-turn consent to this same workspace for tools and the file/Git APIs.
func Resolve(ctx context.Context, s store.Store, homeDir, sessionID string, scratchPolicy ScratchPolicy) (Workspace, error) {
	session, err := s.GetSession(ctx, strings.TrimSpace(sessionID))
	if err != nil {
		return Workspace{}, err
	}
	workspace := Workspace{RootDirs: []string{}}
	if session.ProjectID != "" {
		workspace.Project, err = s.GetProject(ctx, session.ProjectID)
		if err != nil {
			return Workspace{}, err
		}
		workspace.AddRoots("project", workspace.Project.RootDirs)
	}
	dirs, err := s.GetSessionDirectories(ctx, session.ID)
	if err != nil {
		return Workspace{}, err
	}
	workspace.AddRoots("session", dirs)
	workspace.Temporary = workspace.Project == nil && len(dirs) == 0
	err = workspace.IncludeScratch(homeDir, session.ID, scratchPolicy)
	return workspace, err
}

// IncludeScratch retains existing scratch files, and creates a workspace only
// when requested and no authorized folders are available.
func (w *Workspace) IncludeScratch(homeDir, sessionID string, scratchPolicy ScratchPolicy) error {
	if scratchPolicy == ScratchDisabled {
		return nil
	}
	root, exists, err := home.ExistingCodeScratch(homeDir, sessionID)
	if err != nil {
		return err
	}
	if !exists && scratchPolicy == ScratchEnsure && len(w.RootDirs) == 0 {
		root, err = home.PrepareCodeScratch(homeDir, sessionID)
		if err != nil {
			return err
		}
		exists = true
	}
	if exists {
		w.ScratchRoot = root
		w.AddRoots("scratch", []string{root})
	}
	return nil
}

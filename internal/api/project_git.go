package api

import (
	"context"
	"errors"
	"net/http"
	"strconv"
	"strings"

	"github.com/teatak/cart/v3"
	"github.com/teatak/pudding-core/internal/projectgit"
)

const projectGitDiffMaxBytes = 2 << 20

type projectGitStatusView struct {
	RootID          string                  `json:"rootID"`
	Available       bool                    `json:"available"`
	Head            string                  `json:"head,omitempty"`
	Branch          string                  `json:"branch,omitempty"`
	Upstream        string                  `json:"upstream,omitempty"`
	Detached        bool                    `json:"detached"`
	Ahead           int                     `json:"ahead"`
	Behind          int                     `json:"behind"`
	Clean           bool                    `json:"clean"`
	Files           []projectgit.StatusFile `json:"files"`
	FileCount       int                     `json:"fileCount"`
	StagedCount     int                     `json:"stagedCount"`
	UnstagedCount   int                     `json:"unstagedCount"`
	UntrackedCount  int                     `json:"untrackedCount"`
	ConflictedCount int                     `json:"conflictedCount"`
}

type projectGitDiffView struct {
	RootID       string `json:"rootID"`
	Path         string `json:"path"`
	OriginalPath string `json:"originalPath,omitempty"`
	Staged       bool   `json:"staged"`
	OldContent   string `json:"oldContent"`
	NewContent   string `json:"newContent"`
	Binary       bool   `json:"binary"`
	TooLarge     bool   `json:"tooLarge"`
}

type projectGitRootReq struct {
	RootID string `json:"rootID"`
}

type projectGitPathsReq struct {
	RootID string   `json:"rootID"`
	Paths  []string `json:"paths"`
}

type projectGitCommitReq struct {
	RootID  string `json:"rootID"`
	Message string `json:"message"`
}

func (s *Server) getProjectGitStatus(c *cart.Context) error {
	_, roots, ok := s.sessionProject(c)
	if !ok {
		return nil
	}
	root, ok := projectRootByID(roots, strings.TrimSpace(c.Request.URL.Query().Get("rootID")))
	if !ok {
		return projectFileError(c, http.StatusBadRequest, "invalid_project_root")
	}
	repo, err := projectgit.Discover(c.Request.Context(), root.Path)
	if err != nil {
		if projectgit.ErrorCode(err) == projectgit.CodeNotRepository {
			c.JSON(http.StatusOK, projectGitStatusView{
				RootID: root.ID, Available: false, Clean: true, Files: []projectgit.StatusFile{},
			})
			return nil
		}
		return projectGitError(c, err)
	}
	status, err := projectgit.ReadStatus(c.Request.Context(), repo)
	if err != nil {
		return projectGitError(c, err)
	}
	c.JSON(http.StatusOK, projectGitStatusResponse(root.ID, status))
	return nil
}

func (s *Server) initializeProjectGit(c *cart.Context) error {
	var req projectGitRootReq
	if err := decode(c, &req); err != nil {
		return badRequest(c, "invalid json body")
	}
	root, ok := s.projectGitRoot(c, req.RootID)
	if !ok {
		return nil
	}
	repo, err := projectgit.Initialize(c.Request.Context(), root.Path)
	if err != nil {
		return projectGitError(c, err)
	}
	status, err := projectgit.ReadStatus(c.Request.Context(), repo)
	if err != nil {
		return projectGitError(c, err)
	}
	c.JSON(http.StatusOK, projectGitStatusResponse(root.ID, status))
	return nil
}

func (s *Server) stageProjectGit(c *cart.Context) error {
	return s.mutateProjectGitPaths(c, projectgit.Stage)
}

func (s *Server) unstageProjectGit(c *cart.Context) error {
	return s.mutateProjectGitPaths(c, projectgit.Unstage)
}

func (s *Server) discardProjectGit(c *cart.Context) error {
	return s.mutateProjectGitPaths(c, projectgit.Discard)
}

func (s *Server) mutateProjectGitPaths(
	c *cart.Context,
	operation func(context.Context, projectgit.Repository, []string) (projectgit.Status, error),
) error {
	var req projectGitPathsReq
	if err := decode(c, &req); err != nil {
		return badRequest(c, "invalid json body")
	}
	root, repo, ok := s.projectGitRepository(c, req.RootID)
	if !ok {
		return nil
	}
	status, err := operation(c.Request.Context(), repo, req.Paths)
	if err != nil {
		return projectGitError(c, err)
	}
	c.JSON(http.StatusOK, projectGitStatusResponse(root.ID, status))
	return nil
}

func (s *Server) commitProjectGit(c *cart.Context) error {
	var req projectGitCommitReq
	if err := decode(c, &req); err != nil {
		return badRequest(c, "invalid json body")
	}
	root, repo, ok := s.projectGitRepository(c, req.RootID)
	if !ok {
		return nil
	}
	status, err := projectgit.Commit(c.Request.Context(), repo, req.Message)
	if err != nil {
		return projectGitError(c, err)
	}
	c.JSON(http.StatusOK, projectGitStatusResponse(root.ID, status))
	return nil
}

func (s *Server) projectGitRoot(c *cart.Context, rootID string) (projectRootView, bool) {
	_, roots, ok := s.sessionProject(c)
	if !ok {
		return projectRootView{}, false
	}
	root, ok := projectRootByID(roots, strings.TrimSpace(rootID))
	if !ok {
		_ = projectFileError(c, http.StatusBadRequest, "invalid_project_root")
		return projectRootView{}, false
	}
	return root, true
}

func (s *Server) projectGitRepository(c *cart.Context, rootID string) (projectRootView, projectgit.Repository, bool) {
	root, ok := s.projectGitRoot(c, rootID)
	if !ok {
		return projectRootView{}, projectgit.Repository{}, false
	}
	repo, err := projectgit.Discover(c.Request.Context(), root.Path)
	if err != nil {
		_ = projectGitError(c, err)
		return projectRootView{}, projectgit.Repository{}, false
	}
	return root, repo, true
}

func projectGitStatusResponse(rootID string, status projectgit.Status) projectGitStatusView {
	return projectGitStatusView{
		RootID: rootID, Available: true,
		Head: status.Head, Branch: status.Branch, Upstream: status.Upstream,
		Detached: status.Detached, Ahead: status.Ahead, Behind: status.Behind,
		Clean: len(status.Files) == 0, Files: status.Files, FileCount: len(status.Files),
		StagedCount: status.Staged, UnstagedCount: status.Unstaged,
		UntrackedCount: status.Untracked, ConflictedCount: status.Conflicted,
	}
}

func (s *Server) getProjectGitDiff(c *cart.Context) error {
	_, roots, ok := s.sessionProject(c)
	if !ok {
		return nil
	}
	root, ok := projectRootByID(roots, strings.TrimSpace(c.Request.URL.Query().Get("rootID")))
	if !ok {
		return projectFileError(c, http.StatusBadRequest, "invalid_project_root")
	}
	repo, err := projectgit.Discover(c.Request.Context(), root.Path)
	if err != nil {
		return projectGitError(c, err)
	}
	staged, err := strconv.ParseBool(c.Request.URL.Query().Get("staged"))
	if err != nil {
		return projectFileError(c, http.StatusBadRequest, "invalid_git_diff_scope")
	}
	diff, err := projectgit.ReadFileDiff(
		c.Request.Context(), repo, c.Request.URL.Query().Get("path"), staged, projectGitDiffMaxBytes,
	)
	if err != nil {
		return projectGitError(c, err)
	}
	c.JSON(http.StatusOK, projectGitDiffView{
		RootID: root.ID, Path: diff.Path, OriginalPath: diff.OriginalPath, Staged: diff.Staged,
		OldContent: diff.OldContent, NewContent: diff.NewContent, Binary: diff.Binary, TooLarge: diff.TooLarge,
	})
	return nil
}

func projectGitError(c *cart.Context, err error) error {
	var gitErr *projectgit.Error
	if !errors.As(err, &gitErr) {
		return projectFileError(c, http.StatusInternalServerError, "git_operation_failed")
	}
	switch gitErr.Code {
	case projectgit.CodeCommitMessageRequired:
		return projectFileError(c, http.StatusBadRequest, gitErr.Code)
	case projectgit.CodeConflicts, projectgit.CodeNoStagedChanges:
		return projectFileError(c, http.StatusConflict, gitErr.Code)
	case projectgit.CodeInvalidPath:
		return projectFileError(c, http.StatusForbidden, gitErr.Code)
	case projectgit.CodeNotRepository:
		return projectFileError(c, http.StatusNotFound, gitErr.Code)
	case projectgit.CodeRepositoryOutsideRoot:
		return projectFileError(c, http.StatusForbidden, gitErr.Code)
	case projectgit.CodeGitUnavailable:
		return projectFileError(c, http.StatusServiceUnavailable, gitErr.Code)
	case projectgit.CodeTimedOut:
		return projectFileError(c, http.StatusGatewayTimeout, gitErr.Code)
	default:
		return projectFileError(c, http.StatusInternalServerError, gitErr.Code)
	}
}

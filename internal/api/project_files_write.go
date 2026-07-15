package api

import (
	"errors"
	"net/http"
	"path/filepath"
	"strings"
	"unicode/utf8"

	"github.com/teatak/cart/v3"
	"github.com/teatak/pudding-core/internal/projectfs"
)

type createProjectEntryReq struct {
	RootID     string `json:"rootID"`
	ParentPath string `json:"parentPath"`
	Name       string `json:"name"`
	Type       string `json:"type"`
}

type renameProjectEntryReq struct {
	RootID string `json:"rootID"`
	Path   string `json:"path"`
	Name   string `json:"name"`
}

type transferProjectEntryReq struct {
	SourceRootID     string `json:"sourceRootID"`
	SourcePath       string `json:"sourcePath"`
	TargetRootID     string `json:"targetRootID"`
	TargetParentPath string `json:"targetParentPath"`
	Name             string `json:"name,omitempty"`
	Unique           bool   `json:"unique,omitempty"`
}

type saveProjectFileReq struct {
	RootID           string `json:"rootID"`
	Path             string `json:"path"`
	Content          string `json:"content"`
	ExpectedRevision string `json:"expectedRevision"`
}

type projectEntryMutationView struct {
	RootID string `json:"rootID"`
	Name   string `json:"name"`
	Path   string `json:"path"`
	Type   string `json:"type"`
}

func (s *Server) createProjectEntry(c *cart.Context) error {
	var req createProjectEntryReq
	if err := decode(c, &req); err != nil {
		return badRequest(c, "invalid json body")
	}
	_, roots, ok := s.sessionProject(c)
	if !ok {
		return nil
	}
	root, ok := projectRootByID(roots, strings.TrimSpace(req.RootID))
	if !ok {
		return projectFileError(c, http.StatusBadRequest, "invalid_project_root")
	}
	parent, err := cleanProjectRelativePath(req.ParentPath, true)
	if err != nil {
		return projectFileError(c, http.StatusForbidden, "path_not_authorized")
	}
	entry, err := projectfs.Create(root.Path, parent, req.Name, req.Type)
	if err != nil {
		return s.projectMutationError(c, err)
	}
	c.JSON(http.StatusCreated, projectEntryMutationView{
		RootID: root.ID,
		Name:   entry.Name,
		Path:   entry.Path,
		Type:   entry.Type,
	})
	return nil
}

func (s *Server) renameProjectEntry(c *cart.Context) error {
	var req renameProjectEntryReq
	if err := decode(c, &req); err != nil {
		return badRequest(c, "invalid json body")
	}
	_, roots, ok := s.sessionProject(c)
	if !ok {
		return nil
	}
	root, ok := projectRootByID(roots, strings.TrimSpace(req.RootID))
	if !ok {
		return projectFileError(c, http.StatusBadRequest, "invalid_project_root")
	}
	rel, err := cleanProjectRelativePath(req.Path, false)
	if err != nil {
		return projectFileError(c, http.StatusForbidden, "path_not_authorized")
	}
	entry, err := projectfs.Rename(root.Path, rel, req.Name)
	if err != nil {
		return s.projectMutationError(c, err)
	}
	c.JSON(http.StatusOK, projectEntryMutationView{
		RootID: root.ID,
		Name:   entry.Name,
		Path:   entry.Path,
		Type:   entry.Type,
	})
	return nil
}

func (s *Server) copyProjectEntry(c *cart.Context) error {
	var req transferProjectEntryReq
	if err := decode(c, &req); err != nil {
		return badRequest(c, "invalid json body")
	}
	_, roots, ok := s.sessionProject(c)
	if !ok {
		return nil
	}
	sourceRoot, sourcePath, targetRoot, targetParentPath, ok := resolveProjectTransfer(c, roots, req)
	if !ok {
		return nil
	}
	entry, err := projectfs.Copy(sourceRoot.Path, sourcePath, targetRoot.Path, targetParentPath, req.Name, req.Unique)
	if err != nil {
		return s.projectMutationError(c, err)
	}
	c.JSON(http.StatusCreated, projectEntryMutationView{RootID: targetRoot.ID, Name: entry.Name, Path: entry.Path, Type: entry.Type})
	return nil
}

func (s *Server) moveProjectEntry(c *cart.Context) error {
	var req transferProjectEntryReq
	if err := decode(c, &req); err != nil {
		return badRequest(c, "invalid json body")
	}
	_, roots, ok := s.sessionProject(c)
	if !ok {
		return nil
	}
	sourceRoot, sourcePath, targetRoot, targetParentPath, ok := resolveProjectTransfer(c, roots, req)
	if !ok {
		return nil
	}
	entry, err := projectfs.Move(sourceRoot.Path, sourcePath, targetRoot.Path, targetParentPath, req.Name)
	if err != nil {
		return s.projectMutationError(c, err)
	}
	c.JSON(http.StatusOK, projectEntryMutationView{RootID: targetRoot.ID, Name: entry.Name, Path: entry.Path, Type: entry.Type})
	return nil
}

func resolveProjectTransfer(c *cart.Context, roots []projectRootView, req transferProjectEntryReq) (projectRootView, string, projectRootView, string, bool) {
	sourceRoot, ok := projectRootByID(roots, strings.TrimSpace(req.SourceRootID))
	if !ok {
		_ = projectFileError(c, http.StatusBadRequest, "invalid_project_root")
		return projectRootView{}, "", projectRootView{}, "", false
	}
	targetRoot, ok := projectRootByID(roots, strings.TrimSpace(req.TargetRootID))
	if !ok {
		_ = projectFileError(c, http.StatusBadRequest, "invalid_project_root")
		return projectRootView{}, "", projectRootView{}, "", false
	}
	sourcePath, err := cleanProjectRelativePath(req.SourcePath, false)
	if err != nil {
		_ = projectFileError(c, http.StatusForbidden, "path_not_authorized")
		return projectRootView{}, "", projectRootView{}, "", false
	}
	targetParentPath, err := cleanProjectRelativePath(req.TargetParentPath, true)
	if err != nil {
		_ = projectFileError(c, http.StatusForbidden, "path_not_authorized")
		return projectRootView{}, "", projectRootView{}, "", false
	}
	return sourceRoot, sourcePath, targetRoot, targetParentPath, true
}

func (s *Server) deleteProjectEntry(c *cart.Context) error {
	_, roots, ok := s.sessionProject(c)
	if !ok {
		return nil
	}
	root, ok := projectRootByID(roots, strings.TrimSpace(c.Request.URL.Query().Get("rootID")))
	if !ok {
		return projectFileError(c, http.StatusBadRequest, "invalid_project_root")
	}
	rel, err := cleanProjectRelativePath(c.Request.URL.Query().Get("path"), false)
	if err != nil {
		return projectFileError(c, http.StatusForbidden, "path_not_authorized")
	}
	if err := projectfs.Remove(root.Path, rel); err != nil {
		return s.projectMutationError(c, err)
	}
	c.Status(http.StatusNoContent)
	return nil
}

func (s *Server) putProjectFile(c *cart.Context) error {
	var req saveProjectFileReq
	if err := decode(c, &req); err != nil {
		return badRequest(c, "invalid json body")
	}
	_, roots, ok := s.sessionProject(c)
	if !ok {
		return nil
	}
	root, ok := projectRootByID(roots, strings.TrimSpace(req.RootID))
	if !ok {
		return projectFileError(c, http.StatusBadRequest, "invalid_project_root")
	}
	rel, err := cleanProjectRelativePath(req.Path, false)
	if err != nil {
		return projectFileError(c, http.StatusForbidden, "path_not_authorized")
	}
	content := []byte(req.Content)
	if !utf8.Valid(content) || containsBinaryNUL(content) {
		return projectFileError(c, http.StatusUnsupportedMediaType, "project_file_not_text")
	}
	saved, err := projectfs.Save(root.Path, rel, content, strings.TrimSpace(req.ExpectedRevision))
	if err != nil {
		return s.projectMutationError(c, err)
	}
	c.JSON(http.StatusOK, projectFileView{
		RootID:   root.ID,
		Path:     saved.Path,
		Name:     filepath.Base(filepath.FromSlash(saved.Path)),
		Content:  req.Content,
		MIME:     projectMIME(saved.Path, content),
		Size:     saved.Size,
		ModTime:  saved.ModTime,
		Revision: saved.Revision,
	})
	return nil
}

func (s *Server) projectMutationError(c *cart.Context, err error) error {
	switch {
	case errors.Is(err, projectfs.ErrConflict):
		return projectFileError(c, http.StatusConflict, "project_entry_exists")
	case errors.Is(err, projectfs.ErrInvalidName):
		return projectFileError(c, http.StatusBadRequest, "project_entry_invalid_name")
	case errors.Is(err, projectfs.ErrNotFound):
		return projectFileError(c, http.StatusNotFound, "project_path_not_found")
	case errors.Is(err, projectfs.ErrNotDirectory):
		return projectFileError(c, http.StatusBadRequest, "project_path_not_directory")
	case errors.Is(err, projectfs.ErrNotFile):
		return projectFileError(c, http.StatusUnsupportedMediaType, "project_file_not_regular")
	case errors.Is(err, projectfs.ErrPathNotAllowed), errors.Is(err, projectfs.ErrSymlink):
		return projectFileError(c, http.StatusForbidden, "path_not_authorized")
	case errors.Is(err, projectfs.ErrRevisionConflict):
		return projectFileError(c, http.StatusConflict, "project_file_revision_conflict")
	case errors.Is(err, projectfs.ErrRevisionRequired):
		return projectFileError(c, http.StatusBadRequest, "project_file_revision_required")
	case errors.Is(err, projectfs.ErrTooLarge):
		return projectFileError(c, http.StatusRequestEntityTooLarge, "project_file_too_large")
	default:
		return s.fail(c, err)
	}
}

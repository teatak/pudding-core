package api

import (
	"crypto/sha256"
	"errors"
	"fmt"
	"io"
	"mime"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"unicode/utf8"

	"github.com/teatak/cart/v3"
	"github.com/teatak/pudding-core/internal/attachment"
	"github.com/teatak/pudding-core/internal/projectfs"
	"github.com/teatak/pudding-core/internal/projectpath"
	"github.com/teatak/pudding-core/internal/sessionworkspace"
	"github.com/teatak/pudding-core/internal/store"
	"github.com/teatak/pudding-core/internal/tool"
)

const (
	projectTreeMaxEntries   = 1000
	projectTextMaxBytes     = 2 << 20
	projectResourceMaxBytes = 25 << 20
	projectSearchDefaultMax = 200
	projectSearchMax        = 500
)

type projectRootView struct {
	ID        string `json:"id"`
	Name      string `json:"name"`
	Path      string `json:"path"`
	Temporary bool   `json:"temporary,omitempty"`
}

type projectTreeEntry struct {
	Name    string `json:"name"`
	Path    string `json:"path"`
	Type    string `json:"type"`
	Size    int64  `json:"size,omitempty"`
	ModTime string `json:"mtime,omitempty"`
}

type projectFileView struct {
	RootID   string `json:"rootID"`
	Path     string `json:"path"`
	Name     string `json:"name"`
	Content  string `json:"content"`
	MIME     string `json:"mime"`
	Size     int64  `json:"size"`
	ModTime  string `json:"mtime"`
	Revision string `json:"revision"`
}

type projectSearchMatchView struct {
	RootID    string `json:"rootID"`
	Path      string `json:"path"`
	Line      int    `json:"line"`
	LineStart int    `json:"lineStart"`
	LineEnd   int    `json:"lineEnd"`
	Text      string `json:"text"`
	Excerpt   string `json:"excerpt"`
	Truncated bool   `json:"truncated"`
}

var projectTreeIgnoredDirs = map[string]struct{}{
	".cache":        {},
	".git":          {},
	".hg":           {},
	".next":         {},
	".pytest_cache": {},
	".svn":          {},
	".turbo":        {},
	"__pycache__":   {},
	"build":         {},
	"dist":          {},
	"node_modules":  {},
}

func (s *Server) searchProjectFiles(c *cart.Context) error {
	_, roots, ok := s.sessionWorkspace(c)
	if !ok {
		return nil
	}
	query := strings.TrimSpace(c.Request.URL.Query().Get("q"))
	if query == "" {
		return projectFileError(c, http.StatusBadRequest, "project_search_query_required")
	}
	limit := projectSearchDefaultMax
	if raw := strings.TrimSpace(c.Request.URL.Query().Get("limit")); raw != "" {
		parsed, err := strconv.Atoi(raw)
		if err != nil || parsed <= 0 {
			return projectFileError(c, http.StatusBadRequest, "project_search_limit_invalid")
		}
		limit = min(parsed, projectSearchMax)
	}
	caseSensitive := query != strings.ToLower(query)
	matches := make([]projectSearchMatchView, 0, min(limit, 64))
	filesScanned := 0
	resultsCapped := false
	for _, root := range roots {
		remaining := limit - len(matches)
		if remaining <= 0 {
			resultsCapped = true
			break
		}
		result, err := tool.SearchTextFiles(c.Request.Context(), root.Path, tool.TextFileSearchOptions{
			BaseRoot:      root.Path,
			CaseSensitive: caseSensitive,
			MaxResults:    remaining,
			Mode:          "literal",
			Query:         query,
		})
		if err != nil {
			if c.Request.Context().Err() != nil {
				return nil
			}
			return s.fail(c, err)
		}
		filesScanned += result.FilesScanned
		resultsCapped = resultsCapped || result.ResultsCapped
		for _, match := range result.Matches {
			rel, err := filepath.Rel(root.Path, match.Path)
			if err != nil {
				continue
			}
			matches = append(matches, projectSearchMatchView{
				RootID: root.ID, Path: filepath.ToSlash(rel), Line: match.Line,
				LineStart: match.LineStart, LineEnd: match.LineEnd, Text: match.Text,
				Excerpt: match.Excerpt, Truncated: match.Truncated,
			})
		}
	}
	c.JSON(http.StatusOK, map[string]any{
		"query":         query,
		"matches":       matches,
		"matchCount":    len(matches),
		"filesScanned":  filesScanned,
		"resultsCapped": resultsCapped,
		"caseSensitive": caseSensitive,
	})
	return nil
}

func (s *Server) listProjectTree(c *cart.Context) error {
	workspace, roots, ok := s.sessionWorkspace(c)
	if !ok {
		return nil
	}
	rootID := strings.TrimSpace(c.Request.URL.Query().Get("rootID"))
	if rootID == "" {
		projectID := ""
		if workspace.Project != nil {
			projectID = workspace.Project.ID
		}
		c.JSON(http.StatusOK, map[string]any{
			"projectID": projectID,
			"roots":     roots,
			"temporary": workspace.Temporary,
		})
		return nil
	}
	root, ok := projectRootByID(roots, rootID)
	if !ok {
		return projectFileError(c, http.StatusBadRequest, "invalid_project_root")
	}
	rel, err := cleanProjectRelativePath(c.Request.URL.Query().Get("path"), true)
	if err != nil {
		return projectFileError(c, http.StatusForbidden, "path_not_authorized")
	}
	_, target, resolvedRel, err := projectpath.Resolve([]string{root.Path}, rel, true, false)
	if err != nil {
		return projectResolveError(c, err)
	}
	info, err := os.Stat(target)
	if errors.Is(err, os.ErrNotExist) {
		return projectFileError(c, http.StatusNotFound, "project_path_not_found")
	}
	if err != nil {
		return s.fail(c, err)
	}
	if !info.IsDir() {
		return projectFileError(c, http.StatusBadRequest, "project_path_not_directory")
	}
	entries, err := os.ReadDir(target)
	if err != nil {
		return s.fail(c, err)
	}
	visibleEntries := entries[:0]
	for _, entry := range entries {
		if entry.IsDir() {
			if _, ignored := projectTreeIgnoredDirs[entry.Name()]; ignored {
				continue
			}
		}
		visibleEntries = append(visibleEntries, entry)
	}
	views := make([]projectTreeEntry, 0, min(len(visibleEntries), projectTreeMaxEntries))
	for _, entry := range visibleEntries {
		if len(views) >= projectTreeMaxEntries {
			break
		}
		entryType := "file"
		if entry.IsDir() {
			entryType = "dir"
		} else if entry.Type()&os.ModeSymlink != 0 {
			entryType = "symlink"
		} else if !entry.Type().IsRegular() && entry.Type() != 0 {
			entryType = "other"
		}
		childRel := filepath.ToSlash(filepath.Join(resolvedRel, entry.Name()))
		if resolvedRel == "." {
			childRel = filepath.ToSlash(entry.Name())
		}
		view := projectTreeEntry{Name: entry.Name(), Path: childRel, Type: entryType}
		if entryType != "symlink" {
			if entryInfo, infoErr := entry.Info(); infoErr == nil {
				view.ModTime = entryInfo.ModTime().UTC().Format(timeFormat)
				if entryType == "file" {
					view.Size = entryInfo.Size()
				}
			}
		}
		views = append(views, view)
	}
	sort.Slice(views, func(i, j int) bool {
		leftDir := views[i].Type == "dir"
		rightDir := views[j].Type == "dir"
		if leftDir != rightDir {
			return leftDir
		}
		return strings.ToLower(views[i].Name) < strings.ToLower(views[j].Name)
	})
	c.JSON(http.StatusOK, map[string]any{
		"rootID":     root.ID,
		"path":       resolvedRel,
		"entries":    views,
		"truncated":  len(visibleEntries) > len(views),
		"totalCount": len(visibleEntries),
	})
	return nil
}

func (s *Server) getProjectFile(c *cart.Context) error {
	_, roots, ok := s.sessionWorkspace(c)
	if !ok {
		return nil
	}
	root, target, rel, ok := s.resolveProjectRequestPath(c, roots, false)
	if !ok {
		return nil
	}
	file, info, ok := openProjectRegularFile(c, target, projectTextMaxBytes)
	if !ok {
		return nil
	}
	defer file.Close()
	content, err := io.ReadAll(file)
	if err != nil {
		return s.fail(c, err)
	}
	if !utf8.Valid(content) || containsBinaryNUL(content) {
		return projectFileError(c, http.StatusUnsupportedMediaType, "project_file_not_text")
	}
	c.JSON(http.StatusOK, projectFileView{
		RootID:   root.ID,
		Path:     rel,
		Name:     filepath.Base(target),
		Content:  string(content),
		MIME:     projectMIME(target, content),
		Size:     info.Size(),
		ModTime:  info.ModTime().UTC().Format(timeFormat),
		Revision: projectfs.Revision(content),
	})
	return nil
}

func (s *Server) getProjectResource(c *cart.Context) error {
	_, roots, ok := s.sessionWorkspace(c)
	if !ok {
		return nil
	}
	rootID, _ := c.Param("rootID")
	rel, _ := c.Param("path")
	rel = strings.TrimPrefix(rel, "/")
	root, ok := projectRootByID(roots, strings.TrimSpace(rootID))
	if !ok {
		return projectFileError(c, http.StatusBadRequest, "invalid_project_root")
	}
	rel, err := cleanProjectRelativePath(rel, false)
	if err != nil {
		return projectFileError(c, http.StatusForbidden, "path_not_authorized")
	}
	_, target, _, err := projectpath.Resolve([]string{root.Path}, rel, false, false)
	if err != nil {
		return projectResolveError(c, err)
	}
	file, info, ok := openProjectRegularFile(c, target, projectResourceMaxBytes)
	if !ok {
		return nil
	}
	defer file.Close()
	var header [512]byte
	n, readErr := file.Read(header[:])
	if readErr != nil && !errors.Is(readErr, io.EOF) {
		return s.fail(c, readErr)
	}
	if _, err := file.Seek(0, io.SeekStart); err != nil {
		return s.fail(c, err)
	}
	contentType := projectMIME(target, header[:n])
	if !projectPreviewResourceAllowed(contentType) {
		return projectFileError(c, http.StatusUnsupportedMediaType, "project_resource_not_previewable")
	}
	c.Header("Cache-Control", "private, max-age=60")
	c.Header("Content-Type", contentType)
	c.Header("Content-Disposition", "inline")
	c.Header("X-Content-Type-Options", "nosniff")
	if contentType == "image/svg+xml" {
		c.Header("Content-Security-Policy", "default-src 'none'; style-src 'unsafe-inline'; sandbox")
	}
	http.ServeContent(c.Response, c.Request, filepath.Base(target), info.ModTime(), file)
	return nil
}

func projectPreviewResourceAllowed(contentType string) bool {
	return strings.HasPrefix(contentType, "image/") || contentType == "application/pdf"
}

func (s *Server) sessionProject(c *cart.Context) (*store.Project, []projectRootView, bool) {
	sessionID, _ := c.Param("id")
	workspace, err := sessionworkspace.Resolve(
		c.Request.Context(), s.store, s.home, sessionID, sessionworkspace.ScratchDisabled,
	)
	if err != nil {
		_ = s.fail(c, err)
		return nil, nil, false
	}
	if workspace.Project == nil {
		_ = projectFileError(c, http.StatusNotFound, "session_has_no_project")
		return nil, nil, false
	}
	roots := projectRootViews(workspace.RootDirs)
	if len(roots) == 0 {
		_ = projectFileError(c, http.StatusNotFound, "project_roots_unavailable")
		return nil, nil, false
	}
	return workspace.Project, roots, true
}

func (s *Server) sessionWorkspace(c *cart.Context) (sessionworkspace.Workspace, []projectRootView, bool) {
	sessionID, _ := c.Param("id")
	workspace, err := sessionworkspace.Resolve(
		c.Request.Context(), s.store, s.home, sessionID, sessionworkspace.ScratchExisting,
	)
	if err != nil {
		_ = s.fail(c, err)
		return sessionworkspace.Workspace{}, nil, false
	}
	return workspace, workspaceRootViews(workspace), true
}

func projectRootViews(paths []string) []projectRootView {
	roots := projectpath.NormalizeRoots(paths)
	views := make([]projectRootView, 0, len(roots))
	for _, root := range roots {
		name := filepath.Base(root)
		if name == "." || name == string(filepath.Separator) || name == "" {
			name = root
		}
		sum := sha256.Sum256([]byte(root))
		views = append(views, projectRootView{
			ID:   fmt.Sprintf("root_%x", sum[:8]),
			Name: name,
			Path: root,
		})
	}
	return views
}

func workspaceRootViews(workspace sessionworkspace.Workspace) []projectRootView {
	views := projectRootViews(workspace.RootDirs)
	for index := range views {
		views[index].Temporary = workspace.ScratchRoot != "" && views[index].Path == workspace.ScratchRoot
	}
	return views
}

func projectRootByID(roots []projectRootView, id string) (projectRootView, bool) {
	for _, root := range roots {
		if root.ID == id {
			return root, true
		}
	}
	return projectRootView{}, false
}

func (s *Server) resolveProjectRequestPath(c *cart.Context, roots []projectRootView, allowRoot bool) (projectRootView, string, string, bool) {
	root, ok := projectRootByID(roots, strings.TrimSpace(c.Request.URL.Query().Get("rootID")))
	if !ok {
		_ = projectFileError(c, http.StatusBadRequest, "invalid_project_root")
		return projectRootView{}, "", "", false
	}
	rel, err := cleanProjectRelativePath(c.Request.URL.Query().Get("path"), allowRoot)
	if err != nil {
		_ = projectFileError(c, http.StatusForbidden, "path_not_authorized")
		return projectRootView{}, "", "", false
	}
	_, target, resolvedRel, err := projectpath.Resolve([]string{root.Path}, rel, allowRoot, false)
	if err != nil {
		_ = projectResolveError(c, err)
		return projectRootView{}, "", "", false
	}
	return root, target, resolvedRel, true
}

func cleanProjectRelativePath(raw string, allowRoot bool) (string, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		raw = "."
	}
	local := filepath.FromSlash(raw)
	if filepath.IsAbs(local) || filepath.VolumeName(local) != "" {
		return "", projectpath.ErrPathNotAllowed
	}
	cleaned := filepath.Clean(local)
	if cleaned == ".." || strings.HasPrefix(cleaned, ".."+string(filepath.Separator)) {
		return "", projectpath.ErrPathNotAllowed
	}
	if cleaned == "." && !allowRoot {
		return "", projectpath.ErrFileRequired
	}
	return filepath.ToSlash(cleaned), nil
}

func openProjectRegularFile(c *cart.Context, target string, maxBytes int64) (*os.File, os.FileInfo, bool) {
	file, err := os.Open(target)
	if errors.Is(err, os.ErrNotExist) {
		_ = projectFileError(c, http.StatusNotFound, "project_file_not_found")
		return nil, nil, false
	}
	if err != nil {
		_ = projectFileError(c, http.StatusInternalServerError, "project_file_open_failed")
		return nil, nil, false
	}
	info, err := file.Stat()
	if err != nil {
		file.Close()
		_ = projectFileError(c, http.StatusInternalServerError, "project_file_stat_failed")
		return nil, nil, false
	}
	if !info.Mode().IsRegular() {
		file.Close()
		_ = projectFileError(c, http.StatusUnsupportedMediaType, "project_file_not_regular")
		return nil, nil, false
	}
	if info.Size() > maxBytes {
		file.Close()
		_ = projectFileError(c, http.StatusRequestEntityTooLarge, "project_file_too_large")
		return nil, nil, false
	}
	return file, info, true
}

func containsBinaryNUL(content []byte) bool {
	for _, value := range content {
		if value == 0 {
			return true
		}
	}
	return false
}

func projectMIME(path string, header []byte) string {
	if strings.EqualFold(filepath.Ext(path), ".markdown") {
		return "text/markdown"
	}
	contentType := attachment.MIMEFromExt(path)
	if contentType == "" {
		contentType = mime.TypeByExtension(strings.ToLower(filepath.Ext(path)))
	}
	if contentType == "" {
		contentType = http.DetectContentType(header)
	}
	if parsed, _, err := mime.ParseMediaType(contentType); err == nil {
		return parsed
	}
	return contentType
}

func projectResolveError(c *cart.Context, err error) error {
	switch {
	case errors.Is(err, projectpath.ErrPathNotAllowed), errors.Is(err, projectpath.ErrFileRequired):
		return projectFileError(c, http.StatusForbidden, "path_not_authorized")
	case errors.Is(err, projectpath.ErrRootsRequired):
		return projectFileError(c, http.StatusNotFound, "project_roots_unavailable")
	default:
		return projectFileError(c, http.StatusNotFound, "project_path_not_found")
	}
}

func projectFileError(c *cart.Context, status int, code string) error {
	c.JSON(status, map[string]string{"error": code})
	return nil
}

const timeFormat = "2006-01-02T15:04:05.999999999Z07:00"

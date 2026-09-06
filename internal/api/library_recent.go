package api

import (
	"net/http"
	"sort"
	"strings"

	"github.com/teatak/cart/v3"
	"github.com/teatak/pudding-core/internal/store"
)

type libraryRecentEntry struct {
	store.LibraryRecentOpen
	librarySource
	URL        string `json:"url,omitempty"`
	FaviconURL string `json:"faviconURL,omitempty"`
	Available  bool   `json:"available"`
}

func validRecentKind(kind string) bool {
	return kind == "" || kind == "web" || kind == "file" || kind == "canvas"
}

func (s *Server) listLibraryRecentOpens(c *cart.Context) error {
	actor, _ := c.Param("id")
	ctx := c.Request.Context()
	kind := c.Request.URL.Query().Get("kind")
	if !validRecentKind(kind) {
		return badRequest(c, "invalid recent kind")
	}
	query := strings.ToLower(strings.TrimSpace(c.Request.URL.Query().Get("q")))
	recent, err := s.store.ListLibraryRecentOpens(ctx, actor)
	if err != nil {
		return s.fail(c, err)
	}
	ids := make([]string, 0, len(recent))
	for _, e := range recent {
		ids = append(ids, e.SourceSessionID)
	}
	sources, err := s.librarySources(ctx, ids)
	if err != nil {
		return s.fail(c, err)
	}
	entries := make([]libraryRecentEntry, 0)
	for _, e := range recent {
		if kind != "" && kind != e.Kind {
			continue
		}
		source := sources[e.SourceSessionID]
		haystack := strings.Join([]string{e.Title, e.Path, e.RootPath, e.Kind, e.CanvasKind, source.SourceSessionTitle, source.SourceProjectName}, " ")
		if !strings.Contains(strings.ToLower(haystack), query) {
			continue
		}
		entries = append(entries, libraryRecentEntry{LibraryRecentOpen: *e, librarySource: source, Available: true})
	}
	if kind == "" || kind == "web" {
		history, err := s.store.ListBrowserHistory(ctx, query, store.BrowserHistoryMaxLimit)
		if err != nil {
			return s.fail(c, err)
		}
		for _, e := range history {
			entries = append(entries, libraryRecentEntry{LibraryRecentOpen: store.LibraryRecentOpen{ID: e.ID, Kind: "web", Title: e.Title, OpenedAt: e.VisitedAt}, URL: e.URL, FaviconURL: e.FaviconURL, Available: true})
		}
	}
	sort.Slice(entries, func(i, j int) bool {
		if entries[i].OpenedAt.Equal(entries[j].OpenedAt) {
			return entries[i].Kind+entries[i].ID > entries[j].Kind+entries[j].ID
		}
		return entries[i].OpenedAt.After(entries[j].OpenedAt)
	})
	if len(entries) > store.BrowserHistoryMaxLimit {
		entries = entries[:store.BrowserHistoryMaxLimit]
	}
	for i := range entries {
		e := &entries[i]
		if e.Kind == "file" {
			_, err := s.libraryFileTarget(ctx, actor, e.SourceSessionID, e.RootPath, e.Path)
			e.Available = err == nil
		}
	}
	c.JSON(http.StatusOK, map[string]any{"entries": entries})
	return nil
}

type recordLibraryRecentReq struct {
	Kind   string `json:"kind"`
	RootID string `json:"rootID"`
	Path   string `json:"path"`
	ItemID string `json:"itemID"`
}

func (s *Server) recordLibraryRecentOpen(c *cart.Context) error {
	var req recordLibraryRecentReq
	if err := decode(c, &req); err != nil {
		return badRequest(c, "invalid json body")
	}
	actor, _ := c.Param("id")
	e := store.LibraryRecentOpen{Kind: req.Kind, ItemID: req.ItemID}
	switch req.Kind {
	case "file":
		root, path, ok := s.libraryFileReference(c, req.RootID, req.Path)
		if !ok {
			return nil
		}
		e.RootPath = root
		e.Path = path
	case "canvas":
	default:
		return badRequest(c, "invalid recent kind")
	}
	if err := store.ValidateLibraryRecentOpen(e); err != nil {
		return badRequest(c, "invalid recent reference")
	}
	if err := s.store.RecordLibraryRecentOpen(c.Request.Context(), actor, e); err != nil {
		return s.fail(c, err)
	}
	c.Status(http.StatusNoContent)
	return nil
}

func (s *Server) deleteLibraryRecentOpen(c *cart.Context) error {
	actor, _ := c.Param("id")
	kind, _ := c.Param("kind")
	id, _ := c.Param("recentID")
	if kind == "" || !validRecentKind(kind) {
		return badRequest(c, "invalid recent kind")
	}
	if err := s.store.DeleteLibraryRecentOpen(c.Request.Context(), actor, kind, id); err != nil {
		return s.fail(c, err)
	}
	c.Status(http.StatusNoContent)
	return nil
}
func (s *Server) clearLibraryRecentOpens(c *cart.Context) error {
	actor, _ := c.Param("id")
	kind := c.Request.URL.Query().Get("kind")
	if !validRecentKind(kind) {
		return badRequest(c, "invalid recent kind")
	}
	if err := s.store.ClearLibraryRecentOpens(c.Request.Context(), actor, kind); err != nil {
		return s.fail(c, err)
	}
	c.Status(http.StatusNoContent)
	return nil
}

type libraryRecentTarget struct {
	Kind         string `json:"kind"`
	SessionID    string `json:"sessionID"`
	RootPath     string `json:"rootPath,omitempty"`
	RelativePath string `json:"relativePath,omitempty"`
	ItemID       string `json:"itemID,omitempty"`
}

func (s *Server) openLibraryRecent(c *cart.Context) error {
	actor, _ := c.Param("id")
	id, _ := c.Param("recentID")
	kind, _ := c.Param("kind")
	ctx := c.Request.Context()
	entries, err := s.store.ListLibraryRecentOpens(ctx, actor)
	if err != nil {
		return s.fail(c, err)
	}
	for _, e := range entries {
		if e.ID != id || e.Kind != kind {
			continue
		}
		target := libraryRecentTarget{Kind: e.Kind, SessionID: e.SourceSessionID, ItemID: e.ItemID}
		if e.Kind == "file" {
			file, err := s.libraryFileTarget(ctx, actor, e.SourceSessionID, e.RootPath, e.Path)
			if err != nil {
				return projectFileError(c, http.StatusNotFound, "library_source_unavailable")
			}
			target.SessionID = file.SessionID
			target.RootPath = file.RootPath
			target.RelativePath = file.RelativePath
		}
		// Resolve only. The view records a visit after it is actually shown.
		c.JSON(http.StatusOK, target)
		return nil
	}
	return s.fail(c, store.ErrNotFound)
}

package api

import (
	"context"
	"errors"
	"net/http"
	"net/url"
	"os"
	"strings"
	"time"

	"github.com/teatak/cart/v3"
	"github.com/teatak/pudding-core/internal/projectpath"
	"github.com/teatak/pudding-core/internal/sessionworkspace"
	"github.com/teatak/pudding-core/internal/store"
)

type libraryEntry struct {
	store.LibraryFavorite
	FavoriteID string    `json:"favoriteID,omitempty"`
	CanvasKind string    `json:"canvasKind,omitempty"`
	Revision   int64     `json:"revision,omitempty"`
	UpdatedAt  time.Time `json:"updatedAt"`
	librarySource
	Available bool `json:"available"`
}

func (s *Server) listLibrary(c *cart.Context) error {
	actor, _ := c.Param("id")
	ctx := c.Request.Context()
	favorites, err := s.store.ListLibraryFavorites(ctx, actor)
	if err != nil {
		return s.fail(c, err)
	}
	saved, err := s.store.ListSavedCanvasItems(ctx, actor)
	if err != nil {
		return s.fail(c, err)
	}
	entries := make([]libraryEntry, 0, len(favorites)+len(saved))
	savedFavorites := map[string]string{}
	for _, f := range favorites {
		if f.Kind == "canvas" {
			savedFavorites[f.SavedItemID] = f.ID
			continue
		}
		entry := libraryEntry{LibraryFavorite: *f, FavoriteID: f.ID, UpdatedAt: f.CreatedAt, Available: true}

		entries = append(entries, entry)
	}
	for _, item := range saved {
		entries = append(entries, libraryEntry{
			LibraryFavorite: store.LibraryFavorite{ID: "canvas:" + item.ID, Kind: "canvas", SourceSessionID: item.SourceSessionID, SavedItemID: item.ID, Title: item.Title, CreatedAt: item.CreatedAt},
			FavoriteID:      savedFavorites[item.ID], CanvasKind: item.Kind, Revision: item.Revision, UpdatedAt: item.UpdatedAt, Available: true,
		})
	}
	ids := make([]string, 0, len(entries))
	for _, e := range entries {
		ids = append(ids, e.SourceSessionID)
	}
	sources, err := s.librarySources(ctx, ids)
	if err != nil {
		return s.fail(c, err)
	}
	for i := range entries {
		entries[i].librarySource = sources[entries[i].SourceSessionID]
	}

	c.JSON(http.StatusOK, map[string]any{"entries": entries})
	return nil
}

type putLibraryFavoriteReq struct {
	Kind        string `json:"kind"`
	SavedItemID string `json:"savedItemID"`
	URL         string `json:"url"`
	Title       string `json:"title"`
}

func (s *Server) putLibraryFavorite(c *cart.Context) error {
	var req putLibraryFavoriteReq
	if err := decode(c, &req); err != nil {
		return badRequest(c, "invalid json body")
	}
	actor, _ := c.Param("id")
	ctx := c.Request.Context()
	f := store.LibraryFavorite{ID: store.NewID("favorite"), Kind: req.Kind, SourceSessionID: actor, Title: strings.TrimSpace(req.Title)}
	switch req.Kind {
	case "canvas":
		f.ID = "canvas:" + req.SavedItemID
		f.SavedItemID = req.SavedItemID
		f.SourceSessionID = ""
		f.Title = ""
	case "web":
		parsed, err := url.Parse(strings.TrimSpace(req.URL))
		if err != nil || (parsed.Scheme != "http" && parsed.Scheme != "https") || parsed.Hostname() == "" || parsed.User != nil {
			return badRequest(c, "invalid bookmark URL")
		}
		f.URL = parsed.String()
	default:
		return badRequest(c, "invalid favorite kind")
	}
	if err := s.store.PutLibraryFavorite(ctx, actor, f); err != nil {
		return s.fail(c, err)
	}
	c.Status(http.StatusNoContent)
	return nil
}
func (s *Server) deleteLibraryFavorite(c *cart.Context) error {
	actor, _ := c.Param("id")
	id, _ := c.Param("favoriteID")
	if err := s.store.DeleteLibraryFavorite(c.Request.Context(), actor, id); err != nil {
		return s.fail(c, err)
	}
	c.Status(http.StatusNoContent)
	return nil
}

type libraryFileOpen struct {
	SessionID    string `json:"sessionID"`
	RootPath     string `json:"rootPath"`
	RelativePath string `json:"relativePath"`
}

// A file opens in the actor's workspace when its root is present there;
// otherwise it returns to its explicit source session. Never attach a root.
func (s *Server) libraryFileTarget(ctx context.Context, actor, sourceSessionID, rootPath, path string) (*libraryFileOpen, error) {
	for _, id := range []string{actor, sourceSessionID} {
		if id == "" {
			continue
		}
		workspace, err := sessionworkspace.Resolve(ctx, s.store, s.home, id, sessionworkspace.ScratchExisting)
		if errors.Is(err, store.ErrNotFound) {
			continue
		}
		if err != nil {
			return nil, err
		}
		for _, root := range workspace.RootDirs {
			if root != rootPath {
				continue
			}
			_, target, rel, err := projectpath.Resolve([]string{root}, path, false, false)
			if err != nil {
				return nil, err
			}
			info, err := os.Stat(target)
			if err != nil {
				return nil, err
			}
			if !info.Mode().IsRegular() {
				return nil, store.ErrNotFound
			}
			return &libraryFileOpen{SessionID: id, RootPath: root, RelativePath: rel}, nil
		}
	}
	return nil, store.ErrNotFound
}

type librarySource struct {
	SourceSessionAvailable bool   `json:"sourceSessionAvailable"`
	SourceSessionTitle     string `json:"sourceSessionTitle,omitempty"`
	SourceProjectName      string `json:"sourceProjectName,omitempty"`
}

func (s *Server) librarySources(ctx context.Context, ids []string) (map[string]librarySource, error) {
	out := map[string]librarySource{}
	projects := map[string]*store.Project{}
	for _, id := range ids {
		if _, known := out[id]; known {
			continue
		}
		info := librarySource{}
		if id == "" {
			continue
		}
		out[id] = info
		source, err := s.store.GetSession(ctx, id)
		if err != nil && !errors.Is(err, store.ErrNotFound) {
			return nil, err
		}
		if source == nil {
			continue
		}
		info.SourceSessionAvailable = true
		info.SourceSessionTitle = source.Title
		if source.ProjectID != "" {
			p, known := projects[source.ProjectID]
			if !known {
				p, err = s.store.GetProject(ctx, source.ProjectID)
				if err != nil && !errors.Is(err, store.ErrNotFound) {
					return nil, err
				}
				projects[source.ProjectID] = p
			}
			if p != nil {
				info.SourceProjectName = p.Name
			}
		}
		out[id] = info
	}
	return out, nil
}

func (s *Server) libraryFileReference(c *cart.Context, rootID, path string) (string, string, bool) {
	_, roots, ok := s.sessionWorkspace(c)
	if !ok {
		return "", "", false
	}
	root, ok := projectRootByID(roots, rootID)
	if !ok {
		badRequest(c, "invalid project root")
		return "", "", false
	}
	rel, err := cleanProjectRelativePath(path, false)
	if err != nil {
		projectResolveError(c, err)
		return "", "", false
	}
	_, target, resolved, err := projectpath.Resolve([]string{root.Path}, rel, false, false)
	if err != nil {
		projectResolveError(c, err)
		return "", "", false
	}
	info, err := os.Stat(target)
	if err != nil || !info.Mode().IsRegular() {
		projectFileError(c, http.StatusNotFound, "project_file_unavailable")
		return "", "", false
	}
	return root.Path, resolved, true
}

package api

import (
	"bytes"
	"encoding/json"
	"errors"
	"net/http"
	"os"
	"sort"
	"time"

	"github.com/teatak/cart/v3"
	"github.com/teatak/pudding-core/internal/canvas"
	"github.com/teatak/pudding-core/internal/store"
)

func draftResponse(d canvas.Draft) map[string]any {
	files := make([]string, 0, len(d.Files))
	for name := range d.Files {
		files = append(files, name)
	}
	sort.Strings(files)
	return map[string]any{
		"baseRevisionHash": d.BaseRevisionHash,
		"draftHash":        d.DraftHash,
		"files":            files,
	}
}

func (s *Server) readCanvasDraft(c *cart.Context, id string) (canvas.Draft, error) {
	if _, err := s.store.GetCanvas(c.Request.Context(), id); err != nil {
		return canvas.Draft{}, err
	}
	return canvas.ReadDraft(s.home, id)
}

func (s *Server) draftError(c *cart.Context, err error) error {
	if errors.Is(err, os.ErrNotExist) {
		c.JSON(http.StatusNotFound, map[string]string{"error": "draft_not_found"})
		return nil
	}
	return s.canvasError(c, err)
}

func (s *Server) startCanvasDraft(c *cart.Context) error {
	id, _ := c.Param("canvasID")
	canvas.DraftMu.Lock()
	defer canvas.DraftMu.Unlock()
	w, err := s.store.GetCanvas(c.Request.Context(), id)
	if err != nil {
		return s.canvasError(c, err)
	}
	d, err := canvas.StartDraft(s.home, id, w.HeadRevision)
	if err != nil {
		return s.draftError(c, err)
	}
	c.JSON(http.StatusOK, draftResponse(d))
	return nil
}

func (s *Server) getCanvasDraft(c *cart.Context) error {
	id, _ := c.Param("canvasID")
	canvas.DraftMu.Lock()
	defer canvas.DraftMu.Unlock()
	d, err := s.readCanvasDraft(c, id)
	if err != nil {
		return s.draftError(c, err)
	}
	c.JSON(http.StatusOK, draftResponse(d))
	return nil
}

func (s *Server) getCanvasDraftFile(c *cart.Context) error {
	id, _ := c.Param("canvasID")
	name := c.Request.URL.Query().Get("path")
	if !canvas.ValidFilePath(name) {
		return badRequest(c, "invalid draft source path")
	}
	canvas.DraftMu.Lock()
	defer canvas.DraftMu.Unlock()
	d, err := s.readCanvasDraft(c, id)
	if err != nil {
		return s.draftError(c, err)
	}
	content, ok := d.Files[name]
	if !ok {
		c.JSON(http.StatusNotFound, map[string]string{"error": "draft_file_not_found"})
		return nil
	}
	c.JSON(http.StatusOK, map[string]any{"path": name, "content": content, "draftHash": d.DraftHash, "baseRevisionHash": d.BaseRevisionHash})
	return nil
}

func (s *Server) putCanvasDraftFile(c *cart.Context) error {
	id, _ := c.Param("canvasID")
	var req struct {
		Path              string          `json:"path"`
		Content           json.RawMessage `json:"content"`
		ExpectedDraftHash string          `json:"expectedDraftHash"`
	}
	if err := decodeCanvas(c, &req); err != nil {
		return badRequest(c, err.Error())
	}
	if req.ExpectedDraftHash == "" || len(req.Content) == 0 {
		return badRequest(c, "expectedDraftHash and content are required")
	}
	var content *string
	if !bytes.Equal(bytes.TrimSpace(req.Content), []byte("null")) {
		var value string
		if err := json.Unmarshal(req.Content, &value); err != nil {
			return badRequest(c, "content must be a string or null")
		}
		content = &value
	}
	canvas.DraftMu.Lock()
	defer canvas.DraftMu.Unlock()
	if _, err := s.store.GetCanvas(c.Request.Context(), id); err != nil {
		return s.canvasError(c, err)
	}
	d, err := canvas.WriteDraftFile(s.home, id, req.Path, content, req.ExpectedDraftHash)
	if errors.Is(err, canvas.ErrDraftConflict) {
		c.JSON(http.StatusConflict, map[string]any{"error": "draft_conflict", "currentDraftHash": d.DraftHash, "baseRevisionHash": d.BaseRevisionHash})
		return nil
	}
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return s.draftError(c, err)
		}
		return badRequest(c, err.Error())
	}
	c.JSON(http.StatusOK, draftResponse(d))
	return nil
}

func (s *Server) commitCanvasDraft(c *cart.Context) error {
	id, _ := c.Param("canvasID")
	var req struct {
		ExpectedDraftHash string `json:"expectedDraftHash"`
		ClientRequestID   string `json:"clientRequestID"`
	}
	if err := decodeCanvas(c, &req); err != nil {
		return badRequest(c, err.Error())
	}
	if req.ExpectedDraftHash == "" || req.ClientRequestID == "" || len(req.ClientRequestID) > 100 {
		return badRequest(c, "expectedDraftHash and clientRequestID are required")
	}
	canvas.DraftMu.Lock()
	defer canvas.DraftMu.Unlock()
	d, err := s.readCanvasDraft(c, id)
	if err != nil {
		return s.draftError(c, err)
	}
	if d.DraftHash != req.ExpectedDraftHash {
		c.JSON(http.StatusConflict, map[string]any{"error": "draft_conflict", "currentDraftHash": d.DraftHash, "baseRevisionHash": d.BaseRevisionHash})
		return nil
	}
	pkg := canvas.Package{Files: d.Files}
	if _, _, err := pkg.Validate(); err != nil {
		return badRequest(c, err.Error())
	}
	hash, err := canvas.WritePackage(s.home, id, pkg)
	if err != nil {
		return badRequest(c, err.Error())
	}
	w, err := s.store.GetCanvas(c.Request.Context(), id)
	if err != nil {
		return s.canvasError(c, err)
	}
	if hash == d.BaseRevisionHash && w.HeadRevision == hash {
		c.JSON(http.StatusOK, w)
		return nil
	}
	w, err = s.store.SaveCanvasRevision(c.Request.Context(), &store.CanvasRevision{CanvasID: id, Hash: hash, ClientRequestID: req.ClientRequestID, CreatedAt: time.Now().UTC()}, d.BaseRevisionHash)
	if err != nil {
		return s.canvasError(c, err)
	}
	if _, err := canvas.SetDraftBase(s.home, id, hash); err != nil {
		return s.fail(c, err)
	}
	c.JSON(http.StatusOK, w)
	return nil
}

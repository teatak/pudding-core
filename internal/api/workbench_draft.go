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
	"github.com/teatak/pudding-core/internal/store"
	"github.com/teatak/pudding-core/internal/workbench"
)

func draftResponse(d workbench.Draft) map[string]any {
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

func (s *Server) readWorkbenchDraft(c *cart.Context, id string) (workbench.Draft, error) {
	if _, err := s.store.GetWorkbench(c.Request.Context(), id); err != nil {
		return workbench.Draft{}, err
	}
	return workbench.ReadDraft(s.home, id)
}

func (s *Server) draftError(c *cart.Context, err error) error {
	if errors.Is(err, os.ErrNotExist) {
		c.JSON(http.StatusNotFound, map[string]string{"error": "draft_not_found"})
		return nil
	}
	return s.workbenchError(c, err)
}

func (s *Server) startWorkbenchDraft(c *cart.Context) error {
	id, _ := c.Param("workbenchID")
	s.workbenchDraftMu.Lock()
	defer s.workbenchDraftMu.Unlock()
	w, err := s.store.GetWorkbench(c.Request.Context(), id)
	if err != nil {
		return s.workbenchError(c, err)
	}
	d, err := workbench.StartDraft(s.home, id, w.HeadRevision)
	if err != nil {
		return s.draftError(c, err)
	}
	c.JSON(http.StatusOK, draftResponse(d))
	return nil
}

func (s *Server) getWorkbenchDraft(c *cart.Context) error {
	id, _ := c.Param("workbenchID")
	s.workbenchDraftMu.Lock()
	defer s.workbenchDraftMu.Unlock()
	d, err := s.readWorkbenchDraft(c, id)
	if err != nil {
		return s.draftError(c, err)
	}
	c.JSON(http.StatusOK, draftResponse(d))
	return nil
}

func (s *Server) getWorkbenchDraftFile(c *cart.Context) error {
	id, _ := c.Param("workbenchID")
	name := c.Request.URL.Query().Get("path")
	if !workbench.ValidFilePath(name) {
		return badRequest(c, "invalid draft source path")
	}
	s.workbenchDraftMu.Lock()
	defer s.workbenchDraftMu.Unlock()
	d, err := s.readWorkbenchDraft(c, id)
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

func (s *Server) putWorkbenchDraftFile(c *cart.Context) error {
	id, _ := c.Param("workbenchID")
	var req struct {
		Path              string          `json:"path"`
		Content           json.RawMessage `json:"content"`
		ExpectedDraftHash string          `json:"expectedDraftHash"`
	}
	if err := decodeWorkbench(c, &req); err != nil {
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
	s.workbenchDraftMu.Lock()
	defer s.workbenchDraftMu.Unlock()
	if _, err := s.store.GetWorkbench(c.Request.Context(), id); err != nil {
		return s.workbenchError(c, err)
	}
	d, err := workbench.WriteDraftFile(s.home, id, req.Path, content, req.ExpectedDraftHash)
	if errors.Is(err, workbench.ErrDraftConflict) {
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

func (s *Server) commitWorkbenchDraft(c *cart.Context) error {
	id, _ := c.Param("workbenchID")
	var req struct {
		ExpectedDraftHash string `json:"expectedDraftHash"`
		ClientRequestID   string `json:"clientRequestID"`
	}
	if err := decodeWorkbench(c, &req); err != nil {
		return badRequest(c, err.Error())
	}
	if req.ExpectedDraftHash == "" || req.ClientRequestID == "" || len(req.ClientRequestID) > 100 {
		return badRequest(c, "expectedDraftHash and clientRequestID are required")
	}
	s.workbenchDraftMu.Lock()
	defer s.workbenchDraftMu.Unlock()
	d, err := s.readWorkbenchDraft(c, id)
	if err != nil {
		return s.draftError(c, err)
	}
	if d.DraftHash != req.ExpectedDraftHash {
		c.JSON(http.StatusConflict, map[string]any{"error": "draft_conflict", "currentDraftHash": d.DraftHash, "baseRevisionHash": d.BaseRevisionHash})
		return nil
	}
	pkg := workbench.Package{Files: d.Files}
	if _, _, err := pkg.Validate(); err != nil {
		return badRequest(c, err.Error())
	}
	hash, err := workbench.WritePackage(s.home, id, pkg)
	if err != nil {
		return badRequest(c, err.Error())
	}
	w, err := s.store.GetWorkbench(c.Request.Context(), id)
	if err != nil {
		return s.workbenchError(c, err)
	}
	if hash == d.BaseRevisionHash && w.HeadRevision == hash {
		c.JSON(http.StatusOK, w)
		return nil
	}
	w, err = s.store.SaveWorkbenchRevision(c.Request.Context(), &store.WorkbenchRevision{WorkbenchID: id, Hash: hash, ClientRequestID: req.ClientRequestID, CreatedAt: time.Now().UTC()}, d.BaseRevisionHash)
	if err != nil {
		return s.workbenchError(c, err)
	}
	if _, err := workbench.SetDraftBase(s.home, id, hash); err != nil {
		return s.fail(c, err)
	}
	c.JSON(http.StatusOK, w)
	return nil
}

package api

import (
	"bytes"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"os"
	"sort"
	"strings"
	"time"

	"github.com/teatak/cart/v3"
	"github.com/teatak/pudding-core/internal/store"
	"github.com/teatak/pudding-core/internal/widget"
)

func draftResponse(d widget.Draft) map[string]any {
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

func (s *Server) readWidgetDraft(c *cart.Context, id string) (widget.Draft, error) {
	if _, err := s.store.GetStudioItem(c.Request.Context(), id); err != nil {
		return widget.Draft{}, err
	}
	return widget.ReadDraft(s.home, id)
}

func (s *Server) draftError(c *cart.Context, err error) error {
	if errors.Is(err, os.ErrNotExist) {
		c.JSON(http.StatusNotFound, map[string]string{"error": "draft_not_found"})
		return nil
	}
	return s.studioItemError(c, err)
}

func (s *Server) startWidgetDraft(c *cart.Context) error {
	id, _ := c.Param("itemID")
	var req struct {
		ClientRequestID string `json:"clientRequestID"`
		CopyName        string `json:"copyName"`
	}
	if err := decodeStudioRequest(c, &req); err != nil {
		return badRequest(c, err.Error())
	}
	widget.DraftMu.Lock()
	defer widget.DraftMu.Unlock()
	w, err := s.store.GetStudioItem(c.Request.Context(), id)
	if err != nil {
		return s.studioItemError(c, err)
	}
	if w.Origin != nil && !w.Origin.Copy {
		if len(req.ClientRequestID) < 8 || len(req.ClientRequestID) > 100 || strings.TrimSpace(req.CopyName) == "" || len(req.CopyName) > 512 {
			return badRequest(c, "clientRequestID and copyName are required to edit a downloaded widget")
		}
		sum := sha256.Sum256([]byte(id + ":" + req.ClientRequestID))
		copyID := fmt.Sprintf("widget_%x", sum[:16])
		// Published packages precede the database reference. Repeated calls address
		// the same copy and never overwrite its independent data or working draft.
		for _, hash := range []string{w.HeadRevision, w.ActiveRevision} {
			if hash == "" {
				continue
			}
			pkg, err := widget.ReadPackage(s.home, id, hash)
			if err != nil {
				return s.studioItemError(c, err)
			}
			if _, err = widget.WritePackage(s.home, copyID, pkg); err != nil {
				return s.fail(c, err)
			}
		}
		w, err = s.store.ForkWidgetForEditing(c.Request.Context(), id, copyID, req.CopyName, w.Revision)
		if err != nil {
			return s.studioItemError(c, err)
		}
		id = w.ID
	}
	d, err := widget.StartDraft(s.home, id, w.HeadRevision)
	if err != nil {
		return s.draftError(c, err)
	}
	response := draftResponse(d)
	response["widget"] = w
	c.JSON(http.StatusOK, response)
	return nil
}

// Catalog originals can change source only through a validated catalog upgrade.
func (s *Server) writableWidgetDraft(c *cart.Context, id string) (bool, error) {
	item, err := s.store.GetStudioItem(c.Request.Context(), id)
	if err != nil {
		return false, s.studioItemError(c, err)
	}
	if item.Origin != nil && !item.Origin.Copy {
		c.JSON(http.StatusConflict, map[string]string{"error": "widget_copy_required", "message": "Open a draft to create an independent copy before editing this downloaded widget."})
		return false, nil
	}
	return true, nil
}

func (s *Server) getWidgetDraft(c *cart.Context) error {
	id, _ := c.Param("itemID")
	widget.DraftMu.Lock()
	defer widget.DraftMu.Unlock()
	d, err := s.readWidgetDraft(c, id)
	if err != nil {
		return s.draftError(c, err)
	}
	c.JSON(http.StatusOK, draftResponse(d))
	return nil
}

func (s *Server) getWidgetDraftFile(c *cart.Context) error {
	id, _ := c.Param("itemID")
	name := c.Request.URL.Query().Get("path")
	if !widget.ValidFilePath(name) {
		return badRequest(c, "invalid draft source path")
	}
	widget.DraftMu.Lock()
	defer widget.DraftMu.Unlock()
	d, err := s.readWidgetDraft(c, id)
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

func (s *Server) putWidgetDraftFile(c *cart.Context) error {
	id, _ := c.Param("itemID")
	var req struct {
		Path              string          `json:"path"`
		Content           json.RawMessage `json:"content"`
		ExpectedDraftHash string          `json:"expectedDraftHash"`
	}
	if err := decodeStudioRequest(c, &req); err != nil {
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
	widget.DraftMu.Lock()
	defer widget.DraftMu.Unlock()
	if ok, err := s.writableWidgetDraft(c, id); !ok {
		return err
	}
	d, err := widget.WriteDraftFile(s.home, id, req.Path, content, req.ExpectedDraftHash)
	if errors.Is(err, widget.ErrDraftConflict) {
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

func (s *Server) commitWidgetDraft(c *cart.Context) error {
	id, _ := c.Param("itemID")
	var req struct {
		ExpectedDraftHash string `json:"expectedDraftHash"`
		ClientRequestID   string `json:"clientRequestID"`
	}
	if err := decodeStudioRequest(c, &req); err != nil {
		return badRequest(c, err.Error())
	}
	if req.ExpectedDraftHash == "" || req.ClientRequestID == "" || len(req.ClientRequestID) > 100 {
		return badRequest(c, "expectedDraftHash and clientRequestID are required")
	}
	widget.DraftMu.Lock()
	defer widget.DraftMu.Unlock()
	if ok, err := s.writableWidgetDraft(c, id); !ok {
		return err
	}
	d, err := s.readWidgetDraft(c, id)
	if err != nil {
		return s.draftError(c, err)
	}
	if d.DraftHash != req.ExpectedDraftHash {
		c.JSON(http.StatusConflict, map[string]any{"error": "draft_conflict", "currentDraftHash": d.DraftHash, "baseRevisionHash": d.BaseRevisionHash})
		return nil
	}
	pkg := widget.Package{Files: d.Files}
	if _, _, err := pkg.Validate(); err != nil {
		return badRequest(c, err.Error())
	}
	hash, err := widget.WritePackage(s.home, id, pkg)
	if err != nil {
		return badRequest(c, err.Error())
	}
	w, err := s.store.GetStudioItem(c.Request.Context(), id)
	if err != nil {
		return s.studioItemError(c, err)
	}
	if hash == d.BaseRevisionHash && w.HeadRevision == hash {
		c.JSON(http.StatusOK, w)
		return nil
	}
	w, err = s.store.SaveStudioItemRevision(c.Request.Context(), &store.StudioItemRevision{ItemID: id, Hash: hash, ClientRequestID: req.ClientRequestID, CreatedAt: time.Now().UTC()}, d.BaseRevisionHash)
	if err != nil {
		return s.studioItemError(c, err)
	}
	if _, err := widget.SetDraftBase(s.home, id, hash); err != nil {
		return s.fail(c, err)
	}
	c.JSON(http.StatusOK, w)
	return nil
}

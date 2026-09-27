package api

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/teatak/cart/v3"
	"github.com/teatak/pudding-core/contracts"
	"github.com/teatak/pudding-core/internal/store"
	"github.com/teatak/pudding-core/internal/workbench"
)

func decodeWorkbench(c *cart.Context, target any) error {
	limit := contracts.Workbench().MaxRequestBytes
	if c.Request.Method == http.MethodPost && strings.HasSuffix(c.Request.URL.Path, "/revisions") {
		limit = contracts.Workbench().MaxPackageBytes*6 + 65536
	}
	data, err := io.ReadAll(io.LimitReader(c.Request.Body, int64(limit+1)))
	if err != nil {
		return err
	}
	if len(data) > limit {
		return errors.New("request too large")
	}
	return workbench.DecodeStrict(data, target)
}
func (s *Server) workbenchError(c *cart.Context, err error) error {
	if errors.Is(err, store.ErrWorkbenchConflict) {
		c.JSON(http.StatusConflict, map[string]string{"error": "revision_conflict"})
		return nil
	}
	return s.fail(c, err)
}
func (s *Server) listWorkbenches(c *cart.Context) error {
	items, err := s.store.ListWorkbenches(c.Request.Context())
	if err != nil {
		return s.fail(c, err)
	}
	c.JSON(http.StatusOK, map[string]any{"canvases": items})
	return nil
}
func (s *Server) createWorkbench(c *cart.Context) error {
	var req struct {
		Name            string `json:"name"`
		SourceSessionID string `json:"sourceSessionID,omitempty"`
	}
	if err := decodeWorkbench(c, &req); err != nil {
		return badRequest(c, err.Error())
	}
	req.Name = strings.TrimSpace(req.Name)
	if req.Name == "" || len(req.Name) > 200 {
		return badRequest(c, "name is required (max 200 bytes)")
	}
	now := time.Now().UTC()
	w, err := s.store.CreateWorkbench(c.Request.Context(), &store.Workbench{ID: store.NewID("canvas"), Name: req.Name, SourceSessionID: req.SourceSessionID, CreatedAt: now, UpdatedAt: now})
	if err != nil {
		return s.workbenchError(c, err)
	}
	c.JSON(http.StatusCreated, w)
	return nil
}
func (s *Server) getWorkbench(c *cart.Context) error {
	id, _ := c.Param("workbenchID")
	w, err := s.store.GetWorkbench(c.Request.Context(), id)
	if err != nil {
		return s.workbenchError(c, err)
	}
	c.JSON(http.StatusOK, w)
	return nil
}
func (s *Server) deleteWorkbench(c *cart.Context) error {
	id, _ := c.Param("workbenchID")
	var req struct {
		ExpectedRevision int64 `json:"expectedRevision"`
	}
	if err := decodeWorkbench(c, &req); err != nil {
		return badRequest(c, err.Error())
	}
	w, err := s.store.GetWorkbench(c.Request.Context(), id)
	if err != nil {
		return s.workbenchError(c, err)
	}
	w.Deleted = true
	_, err = s.store.UpdateWorkbench(c.Request.Context(), w, req.ExpectedRevision)
	if err != nil {
		return s.workbenchError(c, err)
	}
	c.JSON(http.StatusOK, map[string]any{"deleted": id})
	return nil
}
func (s *Server) listWorkbenchRevisions(c *cart.Context) error {
	id, _ := c.Param("workbenchID")
	if _, err := s.store.GetWorkbench(c.Request.Context(), id); err != nil {
		return s.workbenchError(c, err)
	}
	items, err := s.store.ListWorkbenchRevisions(c.Request.Context(), id)
	if err != nil {
		return s.workbenchError(c, err)
	}
	c.JSON(http.StatusOK, map[string]any{"revisions": items})
	return nil
}
func (s *Server) getWorkbenchRevision(c *cart.Context) error {
	id, _ := c.Param("workbenchID")
	hash, _ := c.Param("hash")
	if _, err := s.store.GetWorkbench(c.Request.Context(), id); err != nil {
		return s.workbenchError(c, err)
	}
	r, err := s.store.GetWorkbenchRevision(c.Request.Context(), id, hash)
	if err != nil {
		return s.workbenchError(c, err)
	}
	if len(r.Content) > 0 {
		c.JSON(http.StatusOK, map[string]any{"kind": "structured", "revision": r, "content": r.Content})
		return nil
	}
	p, err := workbench.ReadPackage(s.home, id, hash)
	if err != nil {
		return s.workbenchError(c, err)
	}
	m, _, err := p.Validate()
	if err != nil {
		return s.workbenchError(c, err)
	}
	c.JSON(http.StatusOK, map[string]any{"kind": "app", "revision": r, "package": p, "manifest": m})
	return nil
}
func (s *Server) saveWorkbenchRevision(c *cart.Context) error {
	id, _ := c.Param("workbenchID")
	var req struct {
		ExpectedRevision int64                `json:"expectedRevision"`
		ClientRequestID  string               `json:"clientRequestID"`
		Package          *workbench.Package   `json:"package"`
		Content          *store.CanvasContent `json:"content"`
	}
	if err := decodeWorkbench(c, &req); err != nil {
		return badRequest(c, err.Error())
	}
	if req.ClientRequestID == "" || len(req.ClientRequestID) > 100 {
		return badRequest(c, "clientRequestID required")
	}
	if _, err := s.store.GetWorkbench(c.Request.Context(), id); err != nil {
		return s.workbenchError(c, err)
	}
	var hash string
	var content json.RawMessage
	var err error
	if (req.Package == nil) == (req.Content == nil) {
		return badRequest(c, "provide exactly one of package or content")
	}
	if req.Content != nil {
		content, hash, err = req.Content.Encode()
	} else {
		if _, _, err := req.Package.Validate(); err != nil {
			return badRequest(c, err.Error())
		}
		hash, err = workbench.WritePackage(s.home, id, *req.Package)
	}
	if err != nil {
		return badRequest(c, err.Error())
	}
	w, err := s.store.SaveWorkbenchRevision(c.Request.Context(), &store.WorkbenchRevision{WorkbenchID: id, Hash: hash, Content: content, ClientRequestID: req.ClientRequestID, CreatedAt: time.Now().UTC()}, req.ExpectedRevision)
	if err != nil {
		return s.workbenchError(c, err)
	}
	c.JSON(http.StatusOK, w)
	return nil
}

type workbenchBuildReceipt struct {
	RevisionHash    string `json:"revisionHash"`
	SDKVersion      string `json:"sdkVersion"`
	CompilerVersion string `json:"compilerVersion"`
	DependencyHash  string `json:"dependencyHash"`
	OK              bool   `json:"ok"`
}

func (s *Server) workbenchBuildReceipt(c *cart.Context) error {
	id, _ := c.Param("workbenchID")
	var req workbenchBuildReceipt
	if err := decodeWorkbench(c, &req); err != nil {
		return badRequest(c, err.Error())
	}
	if !req.OK || req.SDKVersion != contracts.Workbench().SDKVersion || req.CompilerVersion == "" || len(req.DependencyHash) != 64 {
		return badRequest(c, "successful matching build receipt required")
	}
	if _, err := s.store.GetWorkbench(c.Request.Context(), id); err != nil {
		return s.workbenchError(c, err)
	}
	if _, err := workbench.ReadPackage(s.home, id, req.RevisionHash); err != nil {
		return s.workbenchError(c, err)
	}
	b, _ := json.Marshal(req)
	if err := s.store.PutWorkbenchBuildReceipt(c.Request.Context(), id, req.RevisionHash, b); err != nil {
		return s.workbenchError(c, err)
	}
	c.JSON(http.StatusOK, req)
	return nil
}
func (s *Server) activateWorkbench(c *cart.Context) error {
	id, _ := c.Param("workbenchID")
	var req struct {
		RevisionHash     string `json:"revisionHash"`
		ExpectedRevision int64  `json:"expectedRevision"`
	}
	if err := decodeWorkbench(c, &req); err != nil {
		return badRequest(c, err.Error())
	}
	w, err := s.store.GetWorkbench(c.Request.Context(), id)
	if err != nil {
		return s.workbenchError(c, err)
	}
	r, err := s.store.GetWorkbenchRevision(c.Request.Context(), id, req.RevisionHash)
	if err != nil {
		return s.workbenchError(c, err)
	}
	if len(r.Content) == 0 {
		if _, err := workbench.ReadPackage(s.home, id, req.RevisionHash); err != nil {
			return s.workbenchError(c, err)
		}
	} else {
		var content store.CanvasContent
		if err := json.Unmarshal(r.Content, &content); err != nil {
			return s.workbenchError(c, err)
		}
		w.Name = content.Title
	}
	w.ActiveRevision = req.RevisionHash
	w, err = s.store.UpdateWorkbench(c.Request.Context(), w, req.ExpectedRevision)
	if err != nil {
		return s.workbenchError(c, err)
	}
	c.JSON(http.StatusOK, w)
	return nil
}

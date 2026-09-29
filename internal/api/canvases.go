package api

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"regexp"
	"strings"
	"time"

	"github.com/teatak/cart/v3"
	"github.com/teatak/pudding-core/contracts"
	"github.com/teatak/pudding-core/internal/canvas"
	"github.com/teatak/pudding-core/internal/store"
)

var canvasIconID = regexp.MustCompile(`^[a-z][a-z0-9-]{0,47}$`)

func validCanvasAppearance(icon, color string) bool {
	if icon != "" && !canvasIconID.MatchString(icon) {
		return false
	}
	switch color {
	case "", "violet", "blue", "teal", "green", "amber", "rose", "slate":
		return true
	default:
		return false
	}
}

func decodeCanvas(c *cart.Context, target any) error {
	limit := contracts.Canvas().MaxRequestBytes
	data, err := io.ReadAll(io.LimitReader(c.Request.Body, int64(limit+1)))
	if err != nil {
		return err
	}
	if len(data) > limit {
		return errors.New("request too large")
	}
	return canvas.DecodeStrict(data, target)
}
func (s *Server) canvasError(c *cart.Context, err error) error {
	if errors.Is(err, errCanvasConnectionSelectionRequired) {
		c.JSON(http.StatusConflict, map[string]string{"error": "connection_selection_required"})
		return nil
	}
	if errors.Is(err, store.ErrCanvasConflict) {
		response := map[string]any{"error": "revision_conflict"}
		if id, ok := c.Param("canvasID"); ok {
			if current, getErr := s.store.GetCanvas(c.Request.Context(), id); getErr == nil {
				response["currentRevision"] = current.Revision
				response["currentHeadRevision"] = current.HeadRevision
			}
		}
		c.JSON(http.StatusConflict, response)
		return nil
	}
	return s.fail(c, err)
}
func (s *Server) listCanvases(c *cart.Context) error {
	items, err := s.store.ListCanvases(c.Request.Context())
	if err != nil {
		return s.fail(c, err)
	}
	c.JSON(http.StatusOK, map[string]any{"canvases": items})
	return nil
}
func (s *Server) createCanvas(c *cart.Context) error {
	var req struct {
		Name            string `json:"name"`
		SourceSessionID string `json:"sourceSessionID,omitempty"`
		Icon            string `json:"icon,omitempty"`
		IconColor       string `json:"iconColor,omitempty"`
	}
	if err := decodeCanvas(c, &req); err != nil {
		return badRequest(c, err.Error())
	}
	req.Name = strings.TrimSpace(req.Name)
	if req.Name == "" || len(req.Name) > 200 {
		return badRequest(c, "name is required (max 200 bytes)")
	}
	if !validCanvasAppearance(req.Icon, req.IconColor) {
		return badRequest(c, "invalid canvas appearance")
	}
	now := time.Now().UTC()
	w, err := s.store.CreateCanvas(c.Request.Context(), &store.Canvas{ID: store.NewID("canvas"), Name: req.Name, Icon: req.Icon, IconColor: req.IconColor, SourceSessionID: req.SourceSessionID, CreatedAt: now, UpdatedAt: now})
	if err != nil {
		return s.canvasError(c, err)
	}
	c.JSON(http.StatusCreated, w)
	return nil
}
func (s *Server) getCanvas(c *cart.Context) error {
	id, _ := c.Param("canvasID")
	w, err := s.store.GetCanvas(c.Request.Context(), id)
	if err != nil {
		return s.canvasError(c, err)
	}
	c.JSON(http.StatusOK, w)
	return nil
}

func (s *Server) patchCanvasAppearance(c *cart.Context) error {
	id, _ := c.Param("canvasID")
	var req struct {
		ExpectedRevision int64   `json:"expectedRevision"`
		Icon             *string `json:"icon"`
		IconColor        *string `json:"iconColor"`
	}
	if err := decodeCanvas(c, &req); err != nil {
		return badRequest(c, err.Error())
	}
	if req.Icon == nil || req.IconColor == nil || !validCanvasAppearance(*req.Icon, *req.IconColor) {
		return badRequest(c, "invalid canvas appearance")
	}
	w, err := s.store.GetCanvas(c.Request.Context(), id)
	if err != nil {
		return s.canvasError(c, err)
	}
	w.Icon, w.IconColor = *req.Icon, *req.IconColor
	updated, err := s.store.UpdateCanvas(c.Request.Context(), w, req.ExpectedRevision)
	if err != nil {
		return s.canvasError(c, err)
	}
	c.JSON(http.StatusOK, updated)
	return nil
}
func (s *Server) deleteCanvas(c *cart.Context) error {
	id, _ := c.Param("canvasID")
	var req struct {
		ExpectedRevision int64 `json:"expectedRevision"`
	}
	if err := decodeCanvas(c, &req); err != nil {
		return badRequest(c, err.Error())
	}
	w, err := s.store.GetCanvas(c.Request.Context(), id)
	if err != nil {
		return s.canvasError(c, err)
	}
	w.Deleted = true
	_, err = s.store.UpdateCanvas(c.Request.Context(), w, req.ExpectedRevision)
	if err != nil {
		return s.canvasError(c, err)
	}
	c.JSON(http.StatusOK, map[string]any{"deleted": id})
	return nil
}
func (s *Server) listCanvasRevisions(c *cart.Context) error {
	id, _ := c.Param("canvasID")
	if _, err := s.store.GetCanvas(c.Request.Context(), id); err != nil {
		return s.canvasError(c, err)
	}
	items, err := s.store.ListCanvasRevisions(c.Request.Context(), id)
	if err != nil {
		return s.canvasError(c, err)
	}
	c.JSON(http.StatusOK, map[string]any{"revisions": items})
	return nil
}
func (s *Server) getCanvasRevision(c *cart.Context) error {
	id, _ := c.Param("canvasID")
	hash, _ := c.Param("hash")
	if _, err := s.store.GetCanvas(c.Request.Context(), id); err != nil {
		return s.canvasError(c, err)
	}
	r, err := s.store.GetCanvasRevision(c.Request.Context(), id, hash)
	if err != nil {
		return s.canvasError(c, err)
	}
	p, err := canvas.ReadPackage(s.home, id, hash)
	if err != nil {
		return s.canvasError(c, err)
	}
	m, _, err := p.Validate()
	if err != nil {
		return s.canvasError(c, err)
	}
	c.JSON(http.StatusOK, map[string]any{"kind": "app", "revision": r, "package": p, "manifest": m})
	return nil
}

type canvasBuildReceipt struct {
	RevisionHash    string `json:"revisionHash"`
	SDKVersion      string `json:"sdkVersion"`
	CompilerVersion string `json:"compilerVersion"`
	DependencyHash  string `json:"dependencyHash"`
	OK              bool   `json:"ok"`
}

func (s *Server) canvasBuildReceipt(c *cart.Context) error {
	id, _ := c.Param("canvasID")
	var req canvasBuildReceipt
	if err := decodeCanvas(c, &req); err != nil {
		return badRequest(c, err.Error())
	}
	if !req.OK || req.SDKVersion != contracts.Canvas().SDKVersion || req.CompilerVersion == "" || len(req.DependencyHash) != 64 {
		return badRequest(c, "successful matching build receipt required")
	}
	if _, err := s.store.GetCanvas(c.Request.Context(), id); err != nil {
		return s.canvasError(c, err)
	}
	if _, err := canvas.ReadPackage(s.home, id, req.RevisionHash); err != nil {
		return s.canvasError(c, err)
	}
	b, _ := json.Marshal(req)
	if err := s.store.PutCanvasBuildReceipt(c.Request.Context(), id, req.RevisionHash, b); err != nil {
		return s.canvasError(c, err)
	}
	c.JSON(http.StatusOK, req)
	return nil
}
func (s *Server) activateCanvas(c *cart.Context) error {
	id, _ := c.Param("canvasID")
	var req struct {
		RevisionHash     string `json:"revisionHash"`
		ExpectedRevision int64  `json:"expectedRevision"`
	}
	if err := decodeCanvas(c, &req); err != nil {
		return badRequest(c, err.Error())
	}
	w, err := s.store.GetCanvas(c.Request.Context(), id)
	if err != nil {
		return s.canvasError(c, err)
	}
	if _, err := canvas.ReadPackage(s.home, id, req.RevisionHash); err != nil {
		return s.canvasError(c, err)
	}
	w.ActiveRevision = req.RevisionHash
	w, err = s.store.UpdateCanvas(c.Request.Context(), w, req.ExpectedRevision)
	if err != nil {
		return s.canvasError(c, err)
	}
	c.JSON(http.StatusOK, w)
	return nil
}

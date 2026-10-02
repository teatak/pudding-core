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
	"github.com/teatak/pudding-core/internal/store"
	"github.com/teatak/pudding-core/internal/widget"
)

var itemIconID = regexp.MustCompile(`^[a-z][a-z0-9-]{0,47}$`)

func validItemAppearance(icon, color string) bool {
	if icon != "" && !itemIconID.MatchString(icon) {
		return false
	}
	switch color {
	case "", "violet", "blue", "teal", "green", "amber", "rose", "slate":
		return true
	default:
		return false
	}
}

func decodeStudioRequest(c *cart.Context, target any) error {
	limit := contracts.Widget().MaxRequestBytes
	data, err := io.ReadAll(io.LimitReader(c.Request.Body, int64(limit+1)))
	if err != nil {
		return err
	}
	if len(data) > limit {
		return errors.New("request too large")
	}
	return widget.DecodeStrict(data, target)
}
func (s *Server) studioItemError(c *cart.Context, err error) error {
	if errors.Is(err, errWidgetConnectionSelectionRequired) {
		c.JSON(http.StatusConflict, map[string]string{"error": "connection_selection_required"})
		return nil
	}
	if errors.Is(err, store.ErrStudioItemConflict) {
		response := map[string]any{"error": "revision_conflict"}
		if id, ok := c.Param("itemID"); ok {
			if current, getErr := s.store.GetStudioItem(c.Request.Context(), id); getErr == nil {
				response["currentRevision"] = current.Revision
				response["currentHeadRevision"] = current.HeadRevision
			}
		}
		c.JSON(http.StatusConflict, response)
		return nil
	}
	return s.fail(c, err)
}
func (s *Server) listStudioItems(c *cart.Context) error {
	items, err := s.store.ListStudioItems(c.Request.Context())
	if err != nil {
		return s.fail(c, err)
	}
	c.JSON(http.StatusOK, map[string]any{"items": items})
	return nil
}
func (s *Server) createStudioItem(c *cart.Context) error {
	var req struct {
		Name            string `json:"name"`
		SourceSessionID string `json:"sourceSessionID,omitempty"`
		Icon            string `json:"icon,omitempty"`
		IconColor       string `json:"iconColor,omitempty"`
	}
	if err := decodeStudioRequest(c, &req); err != nil {
		return badRequest(c, err.Error())
	}
	req.Name = strings.TrimSpace(req.Name)
	if req.Name == "" || len(req.Name) > 200 {
		return badRequest(c, "name is required (max 200 bytes)")
	}
	if !validItemAppearance(req.Icon, req.IconColor) {
		return badRequest(c, "invalid item appearance")
	}
	now := time.Now().UTC()
	w, err := s.store.CreateStudioItem(c.Request.Context(), &store.StudioItem{ID: store.NewID("widget"), Kind: store.StudioItemKindWidget, Name: req.Name, Icon: req.Icon, IconColor: req.IconColor, SourceSessionID: req.SourceSessionID, CreatedAt: now, UpdatedAt: now})
	if err != nil {
		return s.studioItemError(c, err)
	}
	c.JSON(http.StatusCreated, w)
	return nil
}
func (s *Server) getStudioItem(c *cart.Context) error {
	id, _ := c.Param("itemID")
	w, err := s.store.GetStudioItem(c.Request.Context(), id)
	if err != nil {
		return s.studioItemError(c, err)
	}
	c.JSON(http.StatusOK, w)
	return nil
}

func (s *Server) patchStudioItemAppearance(c *cart.Context) error {
	id, _ := c.Param("itemID")
	var req struct {
		ExpectedRevision int64   `json:"expectedRevision"`
		Icon             *string `json:"icon"`
		IconColor        *string `json:"iconColor"`
	}
	if err := decodeStudioRequest(c, &req); err != nil {
		return badRequest(c, err.Error())
	}
	if req.Icon == nil || req.IconColor == nil || !validItemAppearance(*req.Icon, *req.IconColor) {
		return badRequest(c, "invalid item appearance")
	}
	w, err := s.store.GetStudioItem(c.Request.Context(), id)
	if err != nil {
		return s.studioItemError(c, err)
	}
	w.Icon, w.IconColor = *req.Icon, *req.IconColor
	updated, err := s.store.UpdateStudioItem(c.Request.Context(), w, req.ExpectedRevision)
	if err != nil {
		return s.studioItemError(c, err)
	}
	c.JSON(http.StatusOK, updated)
	return nil
}
func (s *Server) deleteStudioItem(c *cart.Context) error {
	id, _ := c.Param("itemID")
	var req struct {
		ExpectedRevision int64 `json:"expectedRevision"`
	}
	if err := decodeStudioRequest(c, &req); err != nil {
		return badRequest(c, err.Error())
	}
	w, err := s.store.GetStudioItem(c.Request.Context(), id)
	if err != nil {
		return s.studioItemError(c, err)
	}
	w.Deleted = true
	_, err = s.store.UpdateStudioItem(c.Request.Context(), w, req.ExpectedRevision)
	if err != nil {
		return s.studioItemError(c, err)
	}
	c.JSON(http.StatusOK, map[string]any{"deleted": id})
	return nil
}
func (s *Server) listStudioItemRevisions(c *cart.Context) error {
	id, _ := c.Param("itemID")
	if _, err := s.store.GetStudioItem(c.Request.Context(), id); err != nil {
		return s.studioItemError(c, err)
	}
	items, err := s.store.ListStudioItemRevisions(c.Request.Context(), id)
	if err != nil {
		return s.studioItemError(c, err)
	}
	c.JSON(http.StatusOK, map[string]any{"revisions": items})
	return nil
}
func (s *Server) getStudioItemRevision(c *cart.Context) error {
	id, _ := c.Param("itemID")
	hash, _ := c.Param("hash")
	w, err := s.store.GetStudioItem(c.Request.Context(), id)
	if err != nil {
		return s.studioItemError(c, err)
	}
	r, err := s.store.GetStudioItemRevision(c.Request.Context(), id, hash)
	if err != nil {
		return s.studioItemError(c, err)
	}
	p, err := widget.ReadPackage(s.home, id, hash)
	if err != nil {
		return s.studioItemError(c, err)
	}
	m, _, err := p.Validate()
	if err != nil {
		return s.studioItemError(c, err)
	}
	c.JSON(http.StatusOK, map[string]any{"kind": w.Kind, "revision": r, "package": p, "manifest": m})
	return nil
}

type widgetBuildReceipt struct {
	RevisionHash    string `json:"revisionHash"`
	SDKVersion      string `json:"sdkVersion"`
	CompilerVersion string `json:"compilerVersion"`
	DependencyHash  string `json:"dependencyHash"`
	OK              bool   `json:"ok"`
}

func (s *Server) widgetBuildReceipt(c *cart.Context) error {
	id, _ := c.Param("itemID")
	var req widgetBuildReceipt
	if err := decodeStudioRequest(c, &req); err != nil {
		return badRequest(c, err.Error())
	}
	if !req.OK || req.SDKVersion != contracts.Widget().SDKVersion || req.CompilerVersion == "" || len(req.DependencyHash) != 64 {
		return badRequest(c, "successful matching build receipt required")
	}
	if _, err := s.store.GetStudioItem(c.Request.Context(), id); err != nil {
		return s.studioItemError(c, err)
	}
	if _, err := widget.ReadPackage(s.home, id, req.RevisionHash); err != nil {
		return s.studioItemError(c, err)
	}
	b, _ := json.Marshal(req)
	if err := s.store.PutWidgetBuildReceipt(c.Request.Context(), id, req.RevisionHash, b); err != nil {
		return s.studioItemError(c, err)
	}
	c.JSON(http.StatusOK, req)
	return nil
}
func (s *Server) activateWidget(c *cart.Context) error {
	id, _ := c.Param("itemID")
	var req struct {
		RevisionHash     string `json:"revisionHash"`
		ExpectedRevision int64  `json:"expectedRevision"`
	}
	if err := decodeStudioRequest(c, &req); err != nil {
		return badRequest(c, err.Error())
	}
	w, err := s.store.GetStudioItem(c.Request.Context(), id)
	if err != nil {
		return s.studioItemError(c, err)
	}
	if _, err := widget.ReadPackage(s.home, id, req.RevisionHash); err != nil {
		return s.studioItemError(c, err)
	}
	w.ActiveRevision = req.RevisionHash
	w, err = s.store.UpdateStudioItem(c.Request.Context(), w, req.ExpectedRevision)
	if err != nil {
		return s.studioItemError(c, err)
	}
	c.JSON(http.StatusOK, w)
	return nil
}

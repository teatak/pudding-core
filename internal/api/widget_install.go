package api

import (
	"crypto/sha256"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"time"

	"github.com/teatak/cart/v3"
	"github.com/teatak/pudding-core/contracts"
	"github.com/teatak/pudding-core/internal/config"
	"github.com/teatak/pudding-core/internal/store"
	"github.com/teatak/pudding-core/internal/widget"
)

func (s *Server) installWidgetPackage(c *cart.Context) error {
	var req struct {
		PackageJSON      string `json:"packageJSON"`
		PackageHash      string `json:"packageHash"`
		RegistryURL      string `json:"registryURL"`
		PackageID        string `json:"packageID"`
		Version          string `json:"version"`
		Mode             string `json:"mode"`
		ClientRequestID  string `json:"clientRequestID"`
		ItemID           string `json:"itemID,omitempty"`
		ExpectedRevision int64  `json:"expectedRevision,omitempty"`
		Locale           string `json:"locale"`
	}
	limit := int64(contracts.Widget().Distribution.MaxBytes * 2)
	data, err := io.ReadAll(io.LimitReader(c.Request.Body, limit+1))
	if err != nil || int64(len(data)) > limit {
		return badRequest(c, "widget install request too large")
	}
	if err := widget.DecodeStrict(data, &req); err != nil {
		return badRequest(c, err.Error())
	}
	req.RegistryURL, err = config.NormalizeWidgetRegistryURL(req.RegistryURL)
	if err != nil {
		return badRequest(c, err.Error())
	}

	if len(req.ClientRequestID) < 8 || len(req.ClientRequestID) > 100 {
		return badRequest(c, "clientRequestID is required")
	}
	if req.Mode != "install" && req.Mode != "upgrade" {
		return badRequest(c, "invalid install mode")
	}
	if (req.Mode == "upgrade") != (req.ItemID != "" && req.ExpectedRevision > 0) || (req.Mode != "upgrade" && (req.ItemID != "" || req.ExpectedRevision != 0)) {
		return badRequest(c, "upgrade requires itemID and expectedRevision")
	}
	pkg, hash, err := widget.DecodeDistribution([]byte(req.PackageJSON), req.PackageHash)
	if err != nil {
		return badRequest(c, err.Error())
	}
	if pkg.ID != req.PackageID || pkg.Version != req.Version {
		return badRequest(c, "widget package identity mismatch")
	}
	title := pkg.Title[req.Locale]
	if title == "" {
		title = pkg.Title["en"]
	}
	if title == "" {
		return badRequest(c, "package is missing a title for this locale or English")
	}
	id := req.ItemID
	if id == "" {
		sum := sha256.Sum256([]byte(req.ClientRequestID))
		id = fmt.Sprintf("widget_%x", sum[:16])
	}
	widget.DraftMu.Lock()
	defer widget.DraftMu.Unlock()
	if req.Mode == "install" {
		for _, scope := range []store.StudioItemListScope{store.StudioItemsActive, store.StudioItemsArchived} {
			items, e := s.store.ListStudioItems(c.Request.Context(), scope)
			if e != nil {
				return s.fail(c, e)
			}
			for _, item := range items {
				if item.Origin != nil && !item.Origin.Copy && item.Origin.RegistryURL == req.RegistryURL && item.Origin.PackageID == pkg.ID {
					if item.ArchivedAt != nil {
						return s.studioItemError(c, store.ErrStudioItemConflict)
					}
					c.JSON(http.StatusOK, item)
					return nil
				}
			}
		}
	}
	if req.Mode == "upgrade" {
		current, e := s.store.GetStudioItem(c.Request.Context(), id)
		if e != nil {
			return s.studioItemError(c, e)
		}
		if current.Origin == nil || current.Origin.Copy || current.HeadRevision != current.Origin.SourceHash || current.Revision != req.ExpectedRevision {
			return s.studioItemError(c, store.ErrStudioItemConflict)
		}
		draft, e := widget.ReadDraft(s.home, id)
		if e != nil && !errors.Is(e, os.ErrNotExist) {
			return s.fail(c, e)
		}
		if e == nil {
			if draft.DraftHash != current.HeadRevision {
				c.JSON(http.StatusConflict, map[string]string{"error": "widget_local_changes"})
				return nil
			}
			// An unmodified working copy can be recreated from the new head. Never remove edited files.
			root, e := widget.DraftRoot(s.home, id)
			if e != nil {
				return s.fail(c, e)
			}
			if e = os.RemoveAll(root); e != nil {
				return s.fail(c, e)
			}
		}
	}
	if _, err = widget.WritePackage(s.home, id, pkg.Source); err != nil {
		return s.fail(c, err)
	}
	icon := pkg.Icon
	if icon == "" {
		icon = "component"
	}
	now := time.Now().UTC()
	item, err := s.store.InstallWidgetPackage(c.Request.Context(), &store.StudioItem{ID: id, Kind: store.StudioItemKindWidget, Name: title, Icon: icon, IconColor: "violet", CreatedAt: now, UpdatedAt: now, Origin: &store.WidgetOrigin{RegistryURL: req.RegistryURL, PackageID: pkg.ID, Version: pkg.Version, PackageHash: req.PackageHash, SourceHash: hash}}, req.ExpectedRevision)
	if err != nil {
		return s.studioItemError(c, err)
	}
	c.JSON(http.StatusOK, item)
	return nil
}

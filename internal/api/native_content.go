package api

import (
	"errors"
	"io"
	"net/http"
	"strings"

	"github.com/teatak/cart/v3"
	"github.com/teatak/pudding-core/contracts"
	"github.com/teatak/pudding-core/internal/store"
	"github.com/teatak/pudding-core/internal/widget"
)

func decodeNativeContentRequest(c *cart.Context, target any) error {
	// JSON escaping may expand each UTF-8 byte to six ASCII bytes.
	limit := max(contracts.Studio().MaxDocumentBytes, contracts.Studio().MaxSheetBytes)*6 + 65536
	data, err := io.ReadAll(io.LimitReader(c.Request.Body, int64(limit+1)))
	if err != nil {
		return err
	}
	if len(data) > limit {
		return errors.New("content request too large")
	}
	return widget.DecodeStrict(data, target)
}

func (s *Server) renameStudioItem(c *cart.Context) error {
	id, _ := c.Param("itemID")
	var in struct {
		Name             string `json:"name"`
		NameMode         string `json:"nameMode,omitempty"`
		ExpectedRevision int64  `json:"expectedRevision"`
	}
	if err := decodeStudioRequest(c, &in); err != nil {
		return badRequest(c, err.Error())
	}
	in.Name = strings.TrimSpace(in.Name)
	if (in.Name == "" && in.NameMode != store.PageNameAuto) || len(in.Name) > 200 {
		return badRequest(c, "name is required (max 200 bytes)")
	}
	item, err := s.store.GetStudioItem(c.Request.Context(), id)
	if err != nil {
		return s.studioItemError(c, err)
	}
	if in.NameMode != "" && (item.Kind != store.StudioItemKindPage || (in.NameMode != store.PageNameAuto && in.NameMode != store.PageNameManual)) {
		return badRequest(c, "invalid name mode")
	}
	if item.Kind == store.StudioItemKindPage {
		item.NameMode = store.PageNameManual
		if in.NameMode == store.PageNameAuto {
			item.NameMode = store.PageNameAuto
		}
	}
	if in.Name != "" {
		item.Name = in.Name
	}
	item, err = s.store.UpdateStudioItem(c.Request.Context(), item, in.ExpectedRevision)
	if err != nil {
		return s.studioItemError(c, err)
	}
	c.JSON(http.StatusOK, item)
	return nil
}

func (s *Server) widgetOnly(handler func(*cart.Context) error) func(*cart.Context) error {
	return func(c *cart.Context) error {
		id, _ := c.Param("itemID")
		item, err := s.store.GetStudioItem(c.Request.Context(), id)
		if err != nil {
			return s.studioItemError(c, err)
		}
		if item.Kind != store.StudioItemKindWidget {
			return badRequest(c, "widget kind required")
		}
		return handler(c)
	}
}

package api

import (
	"bytes"
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/teatak/cart/v3"
	"github.com/teatak/pudding-core/internal/attachment"
	"github.com/teatak/pudding-core/internal/desktopcamera"
)

func (s *Server) desktopPhoto(c *cart.Context) error {
	sessionID, _ := c.Param("id")
	sessionID = strings.TrimSpace(sessionID)
	if sessionID == "" {
		return badRequest(c, "invalid session id")
	}
	if sessionID != attachment.DraftSessionID {
		if _, err := s.store.GetSession(c.Request.Context(), sessionID); err != nil {
			return s.fail(c, err)
		}
	}
	if strings.TrimSpace(s.home) == "" {
		c.JSON(http.StatusInternalServerError, map[string]string{"error": "attachment_home_unavailable"})
		return nil
	}
	if s.camera == nil {
		c.JSON(http.StatusNotImplemented, map[string]string{"error": desktopcamera.CodeUnsupported})
		return nil
	}

	photo, err := s.camera.CapturePhoto(c.Request.Context())
	if err != nil {
		c.JSON(http.StatusConflict, map[string]string{"error": desktopcamera.Code(err), "detail": err.Error()})
		return nil
	}
	if photo == nil || len(photo.Data) == 0 {
		c.JSON(http.StatusConflict, map[string]string{"error": desktopcamera.CodeFailed})
		return nil
	}
	mime := photo.MIME
	if mime == "" {
		mime = "image/jpeg"
	}
	name := photo.Name
	if name == "" {
		name = desktopcamera.Filename(time.Now())
	}

	stored, err := attachment.NewService(s.home).StoreReader(sessionID, name, mime, bytes.NewReader(photo.Data))
	if errors.Is(err, attachment.ErrTooLarge) {
		c.JSON(http.StatusRequestEntityTooLarge, map[string]string{"error": "attachment_too_large"})
		return nil
	}
	if err != nil {
		if strings.Contains(err.Error(), "not allowed") {
			c.JSON(http.StatusUnsupportedMediaType, map[string]string{"error": "attachment_type_not_allowed"})
			return nil
		}
		return s.fail(c, err)
	}
	c.JSON(http.StatusOK, stored)
	return nil
}

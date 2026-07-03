package api

import (
	"errors"
	"io"
	"mime"
	"net/http"
	"os"
	"path/filepath"
	"strings"

	"github.com/teatak/cart/v3"
	"github.com/teatak/pudding-core/internal/attachment"
	"github.com/teatak/pudding-core/internal/store"
)

const attachmentMultipartMemory = 20 << 20

var (
	errAttachmentHomeUnavailable = errors.New("attachment home unavailable")
	errInvalidAttachment         = errors.New("invalid attachment")
)

func (s *Server) uploadAttachment(c *cart.Context) error {
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
	c.Request.Body = http.MaxBytesReader(c.Response, c.Request.Body, attachment.MaxUploadBytes+16*1024)
	if err := c.Request.ParseMultipartForm(attachmentMultipartMemory); err != nil {
		var maxErr *http.MaxBytesError
		if errors.As(err, &maxErr) {
			c.JSON(http.StatusRequestEntityTooLarge, map[string]string{"error": "attachment_too_large"})
			return nil
		}
		return badRequest(c, "invalid multipart form")
	}
	file, header, err := c.Request.FormFile("file")
	if err != nil {
		return badRequest(c, "missing file")
	}
	defer file.Close()
	if header.Size > attachment.MaxUploadBytes {
		c.JSON(http.StatusRequestEntityTooLarge, map[string]string{"error": "attachment_too_large"})
		return nil
	}
	origin := strings.TrimSpace(c.Request.FormValue("origin"))
	if origin != "" && origin != attachment.OriginTemp {
		return badRequest(c, "invalid attachment origin")
	}
	if origin == attachment.OriginTemp && sessionID != attachment.DraftSessionID {
		return badRequest(c, "temp attachments must use draft session")
	}
	mimeType := header.Header.Get("Content-Type")
	if cleaned, _, err := mime.ParseMediaType(mimeType); err == nil {
		mimeType = cleaned
	}
	stored, err := attachment.NewService(s.home).StoreReader(sessionID, header.Filename, mimeType, file)
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
	if origin == attachment.OriginTemp {
		stored.Origin = attachment.OriginTemp
	}
	c.JSON(http.StatusOK, stored)
	return nil
}

func (s *Server) getAttachment(c *cart.Context) error {
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
	rel, _ := c.Param("path")
	path, ok, err := attachment.NewService(s.home).Path(sessionID, rel)
	if err != nil {
		return s.fail(c, err)
	}
	if !ok {
		c.JSON(http.StatusNotFound, map[string]string{"error": "attachment_not_found"})
		return nil
	}
	file, err := os.Open(path)
	if errors.Is(err, os.ErrNotExist) {
		c.JSON(http.StatusNotFound, map[string]string{"error": "attachment_not_found"})
		return nil
	}
	if err != nil {
		return s.fail(c, err)
	}
	defer file.Close()
	info, err := file.Stat()
	if err != nil {
		return s.fail(c, err)
	}
	if info.IsDir() {
		c.JSON(http.StatusNotFound, map[string]string{"error": "attachment_not_found"})
		return nil
	}
	var header [512]byte
	n, readErr := file.Read(header[:])
	if readErr != nil && !errors.Is(readErr, io.EOF) {
		return s.fail(c, readErr)
	}
	if _, err := file.Seek(0, io.SeekStart); err != nil {
		return s.fail(c, err)
	}
	contentType := http.DetectContentType(header[:n])
	if extMIME := attachment.MIMEFromExt(path); extMIME != "" {
		contentType = extMIME
	}
	c.Header("Cache-Control", "private, max-age=300")
	c.Header("Content-Type", contentType)
	http.ServeContent(c.Response, c.Request, filepath.Base(path), info.ModTime(), file)
	return nil
}

func (s *Server) normalizeSubmitAttachments(sessionID string, values []store.Attachment) ([]store.Attachment, error) {
	if len(values) == 0 {
		return nil, nil
	}
	attachments := store.NormalizeAttachments(values)
	if len(attachments) != len(values) {
		return nil, errInvalidAttachment
	}
	if strings.TrimSpace(s.home) == "" {
		return nil, errAttachmentHomeUnavailable
	}
	svc := attachment.NewService(s.home)
	normalized := make([]store.Attachment, 0, len(attachments))
	for _, item := range attachments {
		next, err := s.normalizeSubmitAttachment(svc, sessionID, item)
		if err != nil {
			return nil, err
		}
		normalized = append(normalized, next)
	}
	return normalized, nil
}

func (s *Server) normalizeSubmitAttachment(svc *attachment.Service, sessionID string, item store.Attachment) (store.Attachment, error) {
	if item.Origin == attachment.OriginTemp {
		if !strings.Contains(item.AttachmentKey, "/"+attachment.DraftSessionID+"/") {
			return store.Attachment{}, errInvalidAttachment
		}
		if ok, err := attachmentFileExists(svc, attachment.DraftSessionID, item.AttachmentKey); !ok || err != nil {
			if err != nil {
				return store.Attachment{}, err
			}
			return store.Attachment{}, errInvalidAttachment
		}
		item.URL = attachment.URL(attachment.DraftSessionID, item.AttachmentKey)
		return item, nil
	}
	if ok, err := attachmentFileExists(svc, sessionID, item.AttachmentKey); ok || err != nil {
		if err != nil {
			return store.Attachment{}, err
		}
		item.URL = attachment.URL(sessionID, item.AttachmentKey)
		return item, nil
	}
	if !strings.Contains(item.AttachmentKey, "/"+attachment.DraftSessionID+"/") {
		return store.Attachment{}, errInvalidAttachment
	}
	if ok, err := attachmentFileExists(svc, attachment.DraftSessionID, item.AttachmentKey); !ok || err != nil {
		if err != nil {
			return store.Attachment{}, err
		}
		return store.Attachment{}, errInvalidAttachment
	}
	next, err := svc.CopyToSession(attachment.DraftSessionID, sessionID, item)
	if errors.Is(err, os.ErrNotExist) {
		return store.Attachment{}, errInvalidAttachment
	}
	if err != nil {
		return store.Attachment{}, err
	}
	return next, nil
}

func attachmentFileExists(svc *attachment.Service, sessionID, key string) (bool, error) {
	path, ok, err := svc.Path(sessionID, key)
	if err != nil {
		return false, err
	}
	if !ok {
		return false, nil
	}
	info, err := os.Stat(path)
	if errors.Is(err, os.ErrNotExist) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	if info.IsDir() {
		return false, nil
	}
	return true, nil
}

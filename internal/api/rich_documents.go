package api

import (
	"bytes"
	"net/http"
	"time"

	"github.com/teatak/cart/v3"
	"github.com/teatak/pudding-core/internal/store"
)

func (s *Server) getRichDocument(c *cart.Context) error {
	id, _ := c.Param("itemID")
	d, err := s.store.GetRichDocument(c.Request.Context(), id)
	if err != nil {
		return s.studioItemError(c, err)
	}
	c.JSON(http.StatusOK, d)
	return nil
}
func (s *Server) writeRichDocument(c *cart.Context) error {
	id, _ := c.Param("itemID")
	var in store.RichDocumentWrite
	if err := decodeNativeContentRequest(c, &in); err != nil {
		return badRequest(c, err.Error())
	}
	d, err := s.store.WriteRichDocument(c.Request.Context(), id, in)
	if err != nil {
		return s.studioItemError(c, err)
	}
	c.JSON(http.StatusOK, d)
	return nil
}
func (s *Server) getRichBlob(c *cart.Context) error {
	id, _ := c.Param("itemID")
	hash, _ := c.Param("hash")
	if !store.ValidRichHash(hash) {
		return badRequest(c, "invalid asset hash")
	}
	blob, err := s.store.GetRichBlob(c.Request.Context(), id, hash)
	if err != nil {
		return s.studioItemError(c, err)
	}
	c.Response.Header().Set("Content-Type", blob.Type)
	c.Response.Header().Set("X-Content-Type-Options", "nosniff")
	c.Response.Header().Set("Cache-Control", "private, max-age=31536000, immutable")
	if blob.Type == store.RichDocumentMIME {
		c.Response.Header().Set("Content-Disposition", `attachment; filename="original.docx"`)
	}
	http.ServeContent(c.Response, c.Request, hash, time.Time{}, bytes.NewReader(blob.Data))
	return nil
}

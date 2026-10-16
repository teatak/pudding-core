package api

import (
	"bytes"
	"net/http"
	"time"

	"github.com/teatak/cart/v3"
	"github.com/teatak/pudding-core/internal/store"
)

func (s *Server) getDocument(c *cart.Context) error {
	id, _ := c.Param("itemID")
	d, err := s.store.GetDocument(c.Request.Context(), id)
	if err != nil {
		return s.studioItemError(c, err)
	}
	c.JSON(http.StatusOK, d)
	return nil
}
func (s *Server) writeDocument(c *cart.Context) error {
	id, _ := c.Param("itemID")
	var in store.DocumentWrite
	if err := decodeNativeContentRequest(c, &in); err != nil {
		return badRequest(c, err.Error())
	}
	d, err := s.store.WriteDocument(c.Request.Context(), id, in)
	if err != nil {
		return s.studioItemError(c, err)
	}
	c.JSON(http.StatusOK, d)
	return nil
}
func (s *Server) getDocumentBlob(c *cart.Context) error {
	id, _ := c.Param("itemID")
	hash, _ := c.Param("hash")
	if !store.ValidContentHash(hash) {
		return badRequest(c, "invalid asset hash")
	}
	blob, err := s.store.GetDocumentBlob(c.Request.Context(), id, hash)
	if err != nil {
		return s.studioItemError(c, err)
	}
	c.Response.Header().Set("Content-Type", blob.Type)
	c.Response.Header().Set("X-Content-Type-Options", "nosniff")
	c.Response.Header().Set("Cache-Control", "private, max-age=31536000, immutable")
	if blob.Type == store.DOCXMIME {
		c.Response.Header().Set("Content-Disposition", `attachment; filename="original.docx"`)
	}
	http.ServeContent(c.Response, c.Request, hash, time.Time{}, bytes.NewReader(blob.Data))
	return nil
}

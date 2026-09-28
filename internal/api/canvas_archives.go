package api

import (
	"bytes"
	"errors"
	"io/fs"
	"net/http"

	"github.com/teatak/cart/v3"
	"github.com/teatak/pudding-core/internal/canvasarchive"
)

func (s *Server) listCanvasArchives(c *cart.Context) error {
	entries, err := canvasarchive.List(s.home)
	if err != nil {
		return s.fail(c, err)
	}
	c.JSON(http.StatusOK, map[string]any{"archives": entries})
	return nil
}
func (s *Server) previewCanvasArchive(c *cart.Context) error {
	id, _ := c.Param("archiveID")
	body, err := canvasarchive.Preview(s.home, id)
	if err != nil {
		return s.canvasArchiveError(c, err)
	}
	c.Response.Header().Set("Content-Type", "text/html; charset=utf-8")
	c.Response.Header().Set("Content-Security-Policy", "sandbox; default-src 'none'; img-src data:; style-src 'unsafe-inline'")
	_, err = c.Response.Write(body)
	return err
}
func (s *Server) exportCanvasArchive(c *cart.Context) error {
	id, _ := c.Param("archiveID")
	var b bytes.Buffer
	if err := canvasarchive.Export(s.home, id, &b); err != nil {
		return s.canvasArchiveError(c, err)
	}
	c.Response.Header().Set("Content-Type", "application/zip")
	c.Response.Header().Set("Content-Disposition", `attachment; filename="canvas-archive-`+id+`.zip"`)
	_, err := c.Response.Write(b.Bytes())
	return err
}
func (s *Server) removeCanvasArchives(c *cart.Context) error {
	var req struct {
		IDs     []string `json:"ids"`
		Confirm bool     `json:"confirm"`
	}
	if err := decodeCanvas(c, &req); err != nil {
		return badRequest(c, err.Error())
	}
	if !req.Confirm || len(req.IDs) == 0 || len(req.IDs) > 10000 {
		return badRequest(c, "explicit confirmation and archive IDs required")
	}
	if err := canvasarchive.Remove(s.home, req.IDs); err != nil {
		return s.canvasArchiveError(c, err)
	}
	return s.listCanvasArchives(c)
}
func (s *Server) canvasArchiveError(c *cart.Context, err error) error {
	if errors.Is(err, fs.ErrNotExist) {
		c.JSON(http.StatusNotFound, map[string]string{"error": "archive_not_found"})
		return nil
	}
	if errors.Is(err, fs.ErrInvalid) {
		return badRequest(c, "invalid archive ID")
	}
	return s.fail(c, err)
}

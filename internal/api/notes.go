package api

import (
	"encoding/hex"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"

	"github.com/teatak/cart/v3"
	"github.com/teatak/pudding-core/contracts"
	"github.com/teatak/pudding-core/internal/store"
	"github.com/teatak/pudding-core/internal/widget"
)

func (s *Server) getNote(c *cart.Context) error {
	id, _ := c.Param("itemID")
	d, err := s.store.GetNote(c.Request.Context(), id)
	if err != nil {
		return s.studioItemError(c, err)
	}
	c.JSON(http.StatusOK, d)
	return nil
}
func (s *Server) writeNote(c *cart.Context) error {
	id, _ := c.Param("itemID")
	var in store.NoteWrite
	if err := decodeNativeContentRequest(c, &in); err != nil {
		return badRequest(c, err.Error())
	}
	in.Author = store.ContentAuthor{Kind: "user"}
	d, err := s.store.WriteNote(c.Request.Context(), id, in)
	if err != nil {
		return s.studioItemError(c, err)
	}
	c.JSON(http.StatusOK, d)
	return nil
}
func (s *Server) getNoteEditState(c *cart.Context) error {
	id, _ := c.Param("itemID")
	state, err := s.store.GetNoteEditState(c.Request.Context(), id)
	if err != nil {
		return s.studioItemError(c, err)
	}
	c.JSON(http.StatusOK, state)
	return nil
}
func (s *Server) changeNoteEdit(c *cart.Context) error {
	id, _ := c.Param("itemID")
	var in store.NoteEditRequest
	if err := decodeNativeContentRequest(c, &in); err != nil {
		return badRequest(c, err.Error())
	}
	in.Author = store.ContentAuthor{Kind: "user"}
	op, err := s.store.ChangeNoteEdit(c.Request.Context(), id, in)
	if err != nil {
		return s.studioItemError(c, err)
	}
	c.JSON(http.StatusOK, op)
	return nil
}
func (s *Server) uploadNoteAsset(c *cart.Context) error {
	widget.DraftMu.Lock()
	defer widget.DraftMu.Unlock()
	id, _ := c.Param("itemID")
	if _, err := s.store.GetNote(c.Request.Context(), id); err != nil {
		return s.studioItemError(c, err)
	}
	limit := contracts.Studio().MaxAssetBytes
	data, err := io.ReadAll(io.LimitReader(c.Request.Body, int64(limit+1)))
	if err != nil {
		return badRequest(c, err.Error())
	}
	if len(data) > limit {
		return badRequest(c, "image exceeds maximum size")
	}
	mime := http.DetectContentType(data)
	extension := map[string]string{"image/png": "png", "image/jpeg": "jpg", "image/gif": "gif", "image/webp": "webp"}[mime]
	if extension == "" {
		return badRequest(c, "PNG, JPEG, GIF or WebP image required")
	}
	name := store.ContentHash(string(data)) + "." + extension
	dir := filepath.Join(s.home, "studio", id, "content", "assets")
	if err := os.MkdirAll(dir, 0700); err != nil {
		return s.fail(c, err)
	}
	f, err := os.CreateTemp(dir, ".upload-")
	if err != nil {
		return s.fail(c, err)
	}
	defer os.Remove(f.Name())
	if _, err = f.Write(data); err == nil {
		err = f.Sync()
	}
	closeErr := f.Close()
	if err == nil {
		err = closeErr
	}
	if err == nil {
		err = os.Rename(f.Name(), filepath.Join(dir, name))
	}
	if err != nil {
		return s.fail(c, err)
	}
	c.JSON(http.StatusCreated, map[string]string{"path": "assets/" + name})
	return nil
}

func (s *Server) getNoteAsset(c *cart.Context) error {
	id, _ := c.Param("itemID")
	name, _ := c.Param("filename")
	if _, err := s.store.GetNote(c.Request.Context(), id); err != nil {
		return s.studioItemError(c, err)
	}
	hash, ext, ok := strings.Cut(name, ".")
	decoded, err := hex.DecodeString(hash)
	if !ok || err != nil || len(decoded) != 32 || (ext != "png" && ext != "jpg" && ext != "gif" && ext != "webp") {
		return badRequest(c, "invalid asset path")
	}
	c.Response.Header().Set("X-Content-Type-Options", "nosniff")
	c.Response.Header().Set("Cache-Control", "private, max-age=31536000, immutable")
	http.ServeFile(c.Response, c.Request, filepath.Join(s.home, "studio", id, "content", "assets", name))
	return nil
}

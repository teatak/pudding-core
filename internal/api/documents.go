package api

import (
	"encoding/hex"
	"errors"
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

func decodeNativeContentRequest(c *cart.Context, target any) error {
	// JSON escaping may expand each UTF-8 byte to six ASCII bytes.
	limit := max(contracts.Studio().MaxDocumentBytes, contracts.Studio().MaxTableBytes)*6 + 65536
	data, err := io.ReadAll(io.LimitReader(c.Request.Body, int64(limit+1)))
	if err != nil {
		return err
	}
	if len(data) > limit {
		return errors.New("content request too large")
	}
	return widget.DecodeStrict(data, target)
}

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
	in.Author = store.ContentAuthor{Kind: "user"}
	d, err := s.store.WriteDocument(c.Request.Context(), id, in)
	if err != nil {
		return s.studioItemError(c, err)
	}
	c.JSON(http.StatusOK, d)
	return nil
}
func (s *Server) renameStudioItem(c *cart.Context) error {
	id, _ := c.Param("itemID")
	var in struct {
		Name             string `json:"name"`
		ExpectedRevision int64  `json:"expectedRevision"`
	}
	if err := decodeStudioRequest(c, &in); err != nil {
		return badRequest(c, err.Error())
	}
	in.Name = strings.TrimSpace(in.Name)
	if in.Name == "" || len(in.Name) > 200 {
		return badRequest(c, "name is required (max 200 bytes)")
	}
	item, err := s.store.GetStudioItem(c.Request.Context(), id)
	if err != nil {
		return s.studioItemError(c, err)
	}
	item.Name = in.Name
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

func (s *Server) uploadDocumentAsset(c *cart.Context) error {
	id, _ := c.Param("itemID")
	if _, err := s.store.GetDocument(c.Request.Context(), id); err != nil {
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
	name := store.DocumentHash(string(data)) + "." + extension
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

func (s *Server) getDocumentAsset(c *cart.Context) error {
	id, _ := c.Param("itemID")
	name, _ := c.Param("filename")
	if _, err := s.store.GetDocument(c.Request.Context(), id); err != nil {
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

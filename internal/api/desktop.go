package api

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"

	"github.com/teatak/cart/v3"
)

const desktopSaveBodyLimit = 80 * 1024 * 1024

type desktopSaveFileRequest struct {
	Filename string `json:"filename"`
	Mime     string `json:"mime"`
	Data     string `json:"data"`
}

type desktopRevealFileRequest struct {
	Path string `json:"path"`
}

func (s *Server) desktopSaveFile(c *cart.Context) error {
	var req desktopSaveFileRequest
	if err := json.NewDecoder(io.LimitReader(c.Request.Body, desktopSaveBodyLimit)).Decode(&req); err != nil {
		return badRequest(c, "invalid json body")
	}
	filename := safeDesktopFilename(req.Filename)
	if filename == "" {
		filename = "pudding-export.bin"
	}
	if filepath.Ext(filename) == "" && strings.Contains(req.Mime, "presentation") {
		filename += ".pptx"
	}
	data, err := base64.StdEncoding.DecodeString(req.Data)
	if err != nil {
		return badRequest(c, "bad base64")
	}
	dir, err := desktopDownloadsDir()
	if err != nil {
		return s.fail(c, err)
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return s.fail(c, err)
	}
	path := uniqueDesktopDownloadPath(dir, filename)
	if err := os.WriteFile(path, data, 0o644); err != nil {
		return s.fail(c, err)
	}
	c.JSON(http.StatusOK, map[string]any{
		"ok":       true,
		"path":     path,
		"filename": filepath.Base(path),
	})
	return nil
}

func (s *Server) desktopRevealFile(c *cart.Context) error {
	var req desktopRevealFileRequest
	if err := json.NewDecoder(io.LimitReader(c.Request.Body, 8*1024)).Decode(&req); err != nil {
		return badRequest(c, "invalid json body")
	}
	path := strings.TrimSpace(req.Path)
	if path == "" || !filepath.IsAbs(path) {
		return badRequest(c, "path must be absolute")
	}
	info, err := os.Stat(path)
	if err != nil {
		c.JSON(http.StatusNotFound, map[string]string{"error": "file_not_found"})
		return nil
	}
	var cmd *exec.Cmd
	if info.IsDir() {
		switch runtime.GOOS {
		case "windows":
			cmd = exec.Command("explorer", path)
		default:
			cmd = exec.Command("open", path)
			if runtime.GOOS != "darwin" {
				cmd = exec.Command("xdg-open", path)
			}
		}
	} else {
		switch runtime.GOOS {
		case "darwin":
			cmd = exec.Command("open", "-R", path)
		case "windows":
			cmd = exec.Command("explorer", "/select,"+path)
		default:
			cmd = exec.Command("xdg-open", filepath.Dir(path))
		}
	}
	if err := cmd.Start(); err != nil {
		return s.fail(c, err)
	}
	c.JSON(http.StatusOK, map[string]any{"ok": true})
	return nil
}

func desktopDownloadsDir() (string, error) {
	if override := strings.TrimSpace(os.Getenv("PUDDING_DESKTOP_DOWNLOADS_DIR")); override != "" {
		return override, nil
	}
	home, err := os.UserHomeDir()
	if err != nil || home == "" {
		return "", fmt.Errorf("home dir not found")
	}
	dir := filepath.Join(home, "Downloads")
	if st, err := os.Stat(dir); err == nil && st.IsDir() {
		return dir, nil
	}
	return home, nil
}

func uniqueDesktopDownloadPath(dir, filename string) string {
	base := strings.TrimSuffix(filename, filepath.Ext(filename))
	ext := filepath.Ext(filename)
	path := filepath.Join(dir, filename)
	for i := 1; ; i++ {
		if _, err := os.Stat(path); os.IsNotExist(err) {
			return path
		}
		path = filepath.Join(dir, fmt.Sprintf("%s (%d)%s", base, i, ext))
	}
}

func safeDesktopFilename(raw string) string {
	name := strings.TrimSpace(raw)
	name = strings.Map(func(r rune) rune {
		switch r {
		case '\\', '/', ':', '*', '?', '"', '<', '>', '|':
			return '-'
		default:
			return r
		}
	}, name)
	name = strings.Join(strings.Fields(name), " ")
	runes := []rune(name)
	if len(runes) > 120 {
		name = string(runes[:120])
	}
	return strings.TrimSpace(name)
}

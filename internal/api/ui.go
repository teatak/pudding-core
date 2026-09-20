package api

import (
	"fmt"
	"io/fs"
	"net/http"
	"os"
	"path/filepath"
	"strings"
)

// UIHandler serves an explicitly supplied client bundle. Without a directory,
// the daemon is API-only; no product UI is embedded or required to build it.
func UIHandler(dir string) (http.Handler, error) {
	if dir == "" {
		return nil, nil
	}
	if !filepath.IsAbs(dir) {
		return nil, fmt.Errorf("UI directory must be absolute")
	}
	files := uiFS(dir)
	index, err := fs.Stat(files, "index.html")
	if err != nil || !index.Mode().IsRegular() {
		return nil, fmt.Errorf("UI directory must contain index.html: %s", dir)
	}
	server := http.FileServer(http.FS(files))
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		name := strings.TrimPrefix(r.URL.Path, "/")
		if name == "" {
			name = "index.html"
		}
		if !fs.ValidPath(name) {
			http.NotFound(w, r)
			return
		}
		info, err := fs.Stat(files, name)
		if err != nil || !info.Mode().IsRegular() {
			http.NotFound(w, r)
			return
		}
		if name == "index.html" {
			w.Header().Set("Cache-Control", "no-cache")
		}
		server.ServeHTTP(w, r)
	}), nil
}

// OpenInRoot confines every open, including FileServer's later reads, so a
// symlink cannot expose files outside the explicitly supplied UI directory.
type uiFS string

func (dir uiFS) Open(name string) (fs.File, error) {
	if !fs.ValidPath(name) {
		return nil, &fs.PathError{Op: "open", Path: name, Err: fs.ErrInvalid}
	}
	return os.OpenInRoot(string(dir), name)
}

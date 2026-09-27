package api

import (
	"encoding/json"
	"github.com/teatak/pudding-core/internal/canvasarchive"
	"net/http"
	"testing"
)

func TestCanvasArchivesRequireExplicitCleanupAndAreIndependent(t *testing.T) {
	srv, _, home := newTestServerWithHome(t)
	entry, err := canvasarchive.Write(home, canvasarchive.Snapshot{ID: "old", Name: "Old", UpdatedAt: 1, Revisions: []canvasarchive.Revision{{Hash: "one", Content: json.RawMessage(`{"kind":"markdown","item":{"content":"keep"}}`)}}})
	if err != nil {
		t.Fatal(err)
	}
	for _, endpoint := range []string{"/canvas-archives", "/canvas-archives/" + entry.ID + "/preview", "/canvas-archives/" + entry.ID + "/export"} {
		r := req(t, "GET", srv.URL+endpoint, nil)
		r.Body.Close()
		if r.StatusCode != 200 {
			t.Fatal(endpoint, r.StatusCode)
		}
	}
	r := req(t, "DELETE", srv.URL+"/canvas-archives", map[string]any{"ids": []string{entry.ID}})
	r.Body.Close()
	if r.StatusCode != http.StatusBadRequest {
		t.Fatal("cleanup without confirmation")
	}
	r = req(t, "DELETE", srv.URL+"/canvas-archives", map[string]any{"ids": []string{entry.ID}, "confirm": true})
	r.Body.Close()
	if r.StatusCode != 200 {
		t.Fatal(r.StatusCode)
	}
	entries, err := canvasarchive.List(home)
	if err != nil || len(entries) != 0 {
		t.Fatal(entries, err)
	}
}

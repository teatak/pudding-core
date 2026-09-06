package api

import (
	"context"
	"net/http"
	"os"
	"path/filepath"
	"testing"

	"github.com/teatak/pudding-core/internal/store"
)

func TestLibraryFavoritesOnlyAcceptWebAndCanvas(t *testing.T) {
	srv, st := newTestServer(t)
	ctx := context.Background()
	root := t.TempDir()
	createProjectSession(t, st, "project", "source", root)
	if err := os.WriteFile(filepath.Join(root, "keep.md"), []byte("keep"), 0600); err != nil {
		t.Fatal(err)
	}
	rootID := projectRootViews([]string{root})[0].ID
	endpoint := srv.URL + "/sessions/source/library"
	for _, input := range []map[string]any{
		{"kind": "file", "rootID": rootID, "path": "keep.md"},
		{"kind": "web", "url": "javascript:alert(1)"},
		{"kind": "web", "url": "file:///etc/passwd"},
	} {
		r := req(t, "POST", endpoint+"/favorites", input)
		r.Body.Close()
		if r.StatusCode != http.StatusBadRequest {
			t.Fatalf("invalid favorite accepted: %+v (%d)", input, r.StatusCode)
		}
	}
	for _, title := range []string{"Example", "Duplicate"} {
		r := req(t, "POST", endpoint+"/favorites", map[string]any{"kind": "web", "url": "https://example.com/", "title": title})
		r.Body.Close()
		if r.StatusCode != 204 {
			t.Fatal(r.StatusCode)
		}
	}
	if err := st.ClearBrowserHistory(ctx); err != nil {
		t.Fatal(err)
	}
	view := decodeJSON[struct {
		Entries []libraryEntry `json:"entries"`
	}](t, req(t, "GET", endpoint, nil))
	if len(view.Entries) != 1 || view.Entries[0].Kind != "web" || view.Entries[0].SourceProjectName == "" {
		t.Fatalf("lost bookmark/source or duplicated URL: %+v", view)
	}
	r := req(t, "DELETE", endpoint+"/favorites/"+view.Entries[0].ID, nil)
	r.Body.Close()
	if r.StatusCode != 204 {
		t.Fatal(r.StatusCode)
	}
	if body, err := os.ReadFile(filepath.Join(root, "keep.md")); err != nil || string(body) != "keep" {
		t.Fatal("favorite removal affected file")
	}
}
func TestLibrarySavedVersionsRemainDiscoverableAfterUnfavorite(t *testing.T) {
	srv, st := newTestServer(t)
	ctx := context.Background()
	if err := st.CreateSession(ctx, &store.Session{ID: "actor", Title: "Source", Provider: "mock", Model: "mock"}); err != nil {
		t.Fatal(err)
	}
	if _, err := st.PutCanvasItem(ctx, store.CanvasItemInput{ActorSessionID: "actor", ID: "c", Kind: "table", Title: "Report", Item: []byte(`{"rows":[1]}`)}); err != nil {
		t.Fatal(err)
	}
	if _, err := st.SaveCanvasItem(ctx, "actor", "c", "s"); err != nil {
		t.Fatal(err)
	}
	r := req(t, "DELETE", srv.URL+"/sessions/actor/library/favorites/canvas:s", nil)
	r.Body.Close()
	if r.StatusCode != 204 {
		t.Fatal(r.StatusCode)
	}
	view := decodeJSON[struct {
		Entries []libraryEntry `json:"entries"`
	}](t, req(t, "GET", srv.URL+"/sessions/actor/library", nil))
	if len(view.Entries) != 1 {
		t.Fatal("unstarred saved version not discoverable")
	}
	e := view.Entries[0]
	if e.FavoriteID != "" || e.Revision != 1 || e.SavedItemID != "s" || e.SourceSessionTitle != "Source" || e.CanvasKind != "table" {
		t.Fatalf("lost metadata: %+v", e)
	}
	item := decodeJSON[store.CanvasItem](t, req(t, "POST", srv.URL+"/sessions/actor/canvas/saved/s/open", nil))
	if item.ID != "c" || string(item.Item) != `{"rows":[1]}` {
		t.Fatal("opening version changed content or identity")
	}
	r = req(t, "POST", srv.URL+"/sessions/actor/library/favorites", map[string]any{"kind": "canvas", "savedItemID": "s"})
	r.Body.Close()
	if r.StatusCode != 204 {
		t.Fatal(r.StatusCode)
	}
	view = decodeJSON[struct {
		Entries []libraryEntry `json:"entries"`
	}](t, req(t, "GET", srv.URL+"/sessions/actor/library", nil))
	if view.Entries[0].FavoriteID != "canvas:s" {
		t.Fatal("could not re-favorite saved content")
	}
}

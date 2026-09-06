package api

import (
	"context"
	"net/http"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/teatak/pudding-core/internal/store"
)

func TestLibraryRecentAPIValidatesRecordsAndRoutes(t *testing.T) {
	srv, st := newTestServer(t)
	ctx := context.Background()
	root := t.TempDir()
	otherRoot := t.TempDir()
	createProjectSession(t, st, "p", "source", root)
	createProjectSession(t, st, "q", "other", otherRoot)
	if err := st.CreateSession(ctx, &store.Session{ID: "shared", ProjectID: "p", Provider: "mock", Model: "mock"}); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "demo.md"), []byte("keep"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(otherRoot, "demo.md"), []byte("unrelated"), 0600); err != nil {
		t.Fatal(err)
	}
	rootID := projectRootViews([]string{root})[0].ID
	endpoint := func(actor string) string { return srv.URL + "/sessions/" + actor + "/library/recent" }
	mutate := func(method, url string, body any, want int) {
		t.Helper()
		r := req(t, method, url, body)
		defer r.Body.Close()
		if r.StatusCode != want {
			t.Fatalf("%s %s: got %d want %d", method, url, r.StatusCode, want)
		}
	}
	list := func(actor, params string) []libraryRecentEntry {
		t.Helper()
		return decodeJSON[struct {
			Entries []libraryRecentEntry `json:"entries"`
		}](t, req(t, "GET", endpoint(actor)+params, nil)).Entries
	}
	if len(list("other", "")) != 0 {
		t.Fatal("empty history fabricated records")
	}
	body := map[string]any{"kind": "file", "rootID": rootID, "path": "demo.md"}
	mutate("POST", endpoint("source"), body, 204)
	mutate("POST", endpoint("source"), body, 204)
	entries := list("other", "?q=DEMO&kind=file")
	if len(entries) != 1 || entries[0].SourceSessionID != "source" || entries[0].SourceProjectName == "" || !entries[0].Available {
		t.Fatalf("missing source, search, or dedupe: %+v", entries)
	}
	id := entries[0].ID
	opened := entries[0].OpenedAt
	for _, actor := range []string{"other", "shared"} {
		target := decodeJSON[libraryRecentTarget](t, req(t, "POST", endpoint(actor)+"/file/"+id+"/open", nil))
		want := actor
		if actor == "other" {
			want = "source"
		}
		if target.Kind != "file" || target.SessionID != want || target.RootPath != root || target.RelativePath != "demo.md" {
			t.Fatalf("wrong source route: %+v", target)
		}
	}
	if !list("other", "")[0].OpenedAt.Equal(opened) {
		t.Fatal("reading/resolving counted as viewing")
	}
	mutate("POST", endpoint("other")+"/canvas/"+id+"/open", nil, 404)
	mutate("PATCH", srv.URL+"/sessions/source/project/entries", map[string]any{"rootID": rootID, "path": "demo.md", "name": "renamed.md"}, 200)
	entries = list("other", "?q=renamed")
	if len(entries) != 1 || entries[0].ID != id {
		t.Fatal("rename lost history reference")
	}
	if err := os.Remove(filepath.Join(root, "renamed.md")); err != nil {
		t.Fatal(err)
	}
	if list("other", "")[0].Available {
		t.Fatal("missing file shown available")
	}
	mutate("POST", endpoint("other")+"/file/"+id+"/open", nil, 404)
	for _, invalid := range []map[string]any{
		{"kind": "file", "rootID": rootID, "path": "../escape"},
		{"kind": "file", "rootID": rootID, "path": "missing.md"},
		{"kind": "canvas", "itemID": "missing"},
		{"kind": "web", "url": "https://example.com/"},
	} {
		r := req(t, "POST", endpoint("source"), invalid)
		r.Body.Close()
		if r.StatusCode < 400 {
			t.Fatalf("invalid reference accepted: %+v", invalid)
		}
	}
	mutate("POST", endpoint("other"), body, 400)
	mutate("GET", endpoint("other")+"?kind=invalid", nil, 400)
	mutate("GET", endpoint("missing"), nil, 404)
	_, err := st.PutCanvasItem(ctx, store.CanvasItemInput{ActorSessionID: "source", ID: "c", Kind: "table", Title: "Report", Item: []byte(`{"rows":[1]}`)})
	if err != nil {
		t.Fatal(err)
	}
	mutate("POST", endpoint("source"), map[string]any{"kind": "canvas", "itemID": "c"}, 204)
	canvas := list("other", "?kind=canvas")
	if len(canvas) != 1 || canvas[0].CanvasKind != "table" || canvas[0].Title != "Report" {
		t.Fatal("canonical canvas metadata missing")
	}
	target := decodeJSON[libraryRecentTarget](t, req(t, "POST", endpoint("other")+"/canvas/"+canvas[0].ID+"/open", nil))
	if target.SessionID != "source" || target.ItemID != "c" {
		t.Fatal("canvas opened in unrelated actor or copied")
	}
	if items, err := st.ListCanvasItems(ctx, "other"); err != nil || len(items) != 0 {
		t.Fatal("opening history made a copy")
	}
	_, err = st.PutBrowserHistory(ctx, store.BrowserHistoryInput{URL: "https://example.com/", Title: "Newest", VisitedAt: time.Now().Add(time.Hour)})
	if err != nil {
		t.Fatal(err)
	}
	entries = list("other", "")
	if len(entries) != 3 || entries[0].Kind != "web" {
		t.Fatal("browser source not merged in time order")
	}
	mutate("POST", srv.URL+"/sessions/other/library/favorites", map[string]any{"kind": "web", "url": "https://example.com/"}, 204)
	mutate("DELETE", endpoint("other")+"/file/"+id, nil, 204)
	if len(list("source", "")) != 2 {
		t.Fatal("single removal affected other records")
	}
	mutate("DELETE", endpoint("other")+"?kind=web", nil, 204)
	if e := list("other", ""); len(e) != 1 || e[0].Kind != "canvas" {
		t.Fatal("filtered clear affected other types")
	}
	mutate("DELETE", endpoint("other"), nil, 204)
	if len(list("other", "")) != 0 {
		t.Fatal("clear all failed")
	}
	favorites, err := st.ListLibraryFavorites(ctx, "source")
	if err != nil || len(favorites) != 1 {
		t.Fatal("clear removed favorite")
	}
	items, err := st.ListCanvasItems(ctx, "source")
	if err != nil || len(items) != 1 || string(items[0].Item) != `{"rows":[1]}` {
		t.Fatal("history clear changed canvas")
	}
	// An arbitrary absolute root or another session's canvas cannot be recorded.
	mutate("POST", endpoint("other"), map[string]any{"kind": "canvas", "itemID": "c"}, http.StatusNotFound)
}

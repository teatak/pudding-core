package api

import (
	"context"
	"net/http"
	"os"
	"path/filepath"
	"testing"

	"github.com/teatak/pudding-core/internal/store"
)

func TestLibraryFavoritesOnlyAcceptWebAndStudio(t *testing.T) {
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
	initial, err := seedStudioMount(st, "actor", "c", "Report")
	if err != nil {
		t.Fatal(err)
	}
	itemID := initial.ItemID
	if err := st.PutLibraryFavorite(ctx, "actor", store.LibraryFavorite{ID: "studio:" + itemID, Kind: "studio", SavedItemID: itemID}); err != nil {
		t.Fatal(err)
	}

	r := req(t, "DELETE", srv.URL+"/sessions/actor/library/favorites/studio:"+itemID, nil)
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
	if e.FavoriteID != "" || e.Revision != 2 || e.SavedItemID != itemID || e.SourceSessionTitle != "Source" || e.ItemKind != store.StudioItemKindWidget {
		t.Fatalf("lost metadata: %+v", e)
	}
	item := decodeJSON[store.StudioMount](t, req(t, "POST", srv.URL+"/sessions/actor/studio/items/"+itemID+"/open", nil))
	if item.ID != "c" || item.ItemID != itemID {
		t.Fatal("opening version changed content or identity")
	}
	r = req(t, "POST", srv.URL+"/sessions/actor/library/favorites", map[string]any{"kind": "studio", "savedItemID": itemID})
	r.Body.Close()
	if r.StatusCode != 204 {
		t.Fatal(r.StatusCode)
	}
	view = decodeJSON[struct {
		Entries []libraryEntry `json:"entries"`
	}](t, req(t, "GET", srv.URL+"/sessions/actor/library", nil))
	if view.Entries[0].FavoriteID != "studio:"+itemID {
		t.Fatal("could not re-favorite saved content")
	}
}

func TestGlobalStudioFavoriteOpensInActorAndSurvivesSourceDeletion(t *testing.T) {
	srv, st := newTestServer(t)
	ctx := context.Background()
	for _, id := range []string{"source", "reader"} {
		if err := st.CreateSession(ctx, &store.Session{ID: id, Title: id, Provider: "mock", Model: "mock"}); err != nil {
			t.Fatal(err)
		}
	}
	initial, err := seedStudioMount(st, "source", "studio", "Reusable")
	if err != nil {
		t.Fatal(err)
	}
	itemID := initial.ItemID
	if err := st.PutLibraryFavorite(ctx, "source", store.LibraryFavorite{ID: "studio:" + itemID, Kind: "studio", SavedItemID: itemID}); err != nil {
		t.Fatal(err)
	}

	if err := st.DeleteSession(ctx, "source"); err != nil {
		t.Fatal(err)
	}
	endpoint := srv.URL + "/sessions/reader"
	view := decodeJSON[struct {
		Entries []libraryEntry `json:"entries"`
	}](t, req(t, "GET", endpoint+"/library", nil))
	if len(view.Entries) != 1 || view.Entries[0].FavoriteID == "" || !view.Entries[0].Available || view.Entries[0].SourceSessionAvailable {
		t.Fatalf("global favorite lost: %+v", view)
	}
	first := decodeJSON[store.StudioMount](t, req(t, "POST", endpoint+"/studio/items/"+itemID+"/open", nil))
	second := decodeJSON[store.StudioMount](t, req(t, "POST", endpoint+"/studio/items/"+itemID+"/open", nil))
	if first.SessionID != "reader" || second.ID != first.ID || first.ItemID != itemID {
		t.Fatal("opening favorite must reuse reader's working copy")
	}
	r := req(t, "DELETE", endpoint+"/library/favorites/studio:"+itemID, nil)
	r.Body.Close()
	view = decodeJSON[struct {
		Entries []libraryEntry `json:"entries"`
	}](t, req(t, "GET", endpoint+"/library", nil))
	if len(view.Entries) != 1 || view.Entries[0].FavoriteID != "" {
		t.Fatal("unfavorite must retain searchable saved version")
	}
	items, err := st.ListStudioMounts(ctx, "reader")
	if err != nil || len(items) != 1 {
		t.Fatal("unfavorite must preserve reader's widget")
	}
	r = req(t, "GET", endpoint+"/library/recent", nil)
	r.Body.Close()
	if r.StatusCode != http.StatusNotFound {
		t.Fatal("retired recent-file API still available")
	}
}

func seedStudioMount(st store.Store, session, id, name string) (*store.StudioMount, error) {
	ctx := context.Background()
	w, err := st.CreateStudioItem(ctx, &store.StudioItem{Kind: store.StudioItemKindWidget, ID: session + "-" + id + "-" + name, Name: name, SourceSessionID: session})
	if err != nil {
		return nil, err
	}
	if _, err = st.SaveStudioItemRevision(ctx, &store.StudioItemRevision{ItemID: w.ID, Hash: "first", ClientRequestID: "first"}, ""); err != nil {
		return nil, err
	}
	return st.OpenStudioItem(ctx, session, w.ID, id)
}

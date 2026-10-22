package api

import (
	"context"
	"net/http"
	"testing"

	"github.com/teatak/pudding-core/internal/store"
)

func TestProjectRequestAPI(t *testing.T) {
	srv, st := newTestServer(t)
	create := map[string]any{"name": "Original", "rootDirs": []string{}, "clientRequestID": "create"}
	response := req(t, http.MethodPost, srv.URL+"/projects", create)
	if response.StatusCode != 201 {
		t.Fatal(response.StatusCode)
	}
	first := decodeJSON[store.Project](t, response)
	again := decodeJSON[store.Project](t, req(t, http.MethodPost, srv.URL+"/projects", create))
	if first.ID != again.ID {
		t.Fatal("duplicate create")
	}
	create["name"] = "Changed"
	response = req(t, http.MethodPost, srv.URL+"/projects", create)
	if response.StatusCode != 409 {
		t.Fatal(response.StatusCode)
	}
	response.Body.Close()
	create["clientRequestID"] = ""
	response = req(t, http.MethodPost, srv.URL+"/projects", create)
	if response.StatusCode != 400 {
		t.Fatal(response.StatusCode)
	}
	response.Body.Close()
	source := &store.Project{ID: "source", Name: "Source", RootDirs: []string{"/shared"}}
	if err := st.CreateProject(context.Background(), source); err != nil {
		t.Fatal(err)
	}
	merge := map[string]any{"sourceProjectID": source.ID, "name": "Merged", "rootDirs": source.RootDirs, "clientRequestID": "merge", "expectedTargetUpdatedAt": first.UpdatedAt, "expectedSourceUpdatedAt": source.UpdatedAt}
	route := srv.URL + "/projects/" + first.ID + "/merge"
	for range 2 {
		response = req(t, http.MethodPost, route, merge)
		if response.StatusCode != 200 {
			t.Fatalf("merge %d", response.StatusCode)
		}
		decodeJSON[store.Project](t, response)
	}
}

package api

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"slices"
	"testing"

	"github.com/teatak/pudding-core/internal/engine"
	"github.com/teatak/pudding-core/internal/event"
	"github.com/teatak/pudding-core/internal/provider/mock"
	"github.com/teatak/pudding-core/internal/provider/registry"
	"github.com/teatak/pudding-core/internal/store"
	"github.com/teatak/pudding-core/internal/store/memstore"
)

func TestChildSessionsHiddenFromListsButIncludedInProjectMaintenance(t *testing.T) {
	ctx := context.Background()
	st := memstore.New()
	if err := st.CreateProject(ctx, &store.Project{ID: "project", Name: "Project", RootDirs: []string{t.TempDir()}}); err != nil {
		t.Fatal(err)
	}
	if err := st.CreateSession(ctx, &store.Session{ID: "parent", Provider: "mock", Model: "mock", ProjectID: "project"}); err != nil {
		t.Fatal(err)
	}
	if err := st.CreateChildSession(ctx, "parent", &store.Session{ID: "child", Provider: "mock", Model: "mock"}); err != nil {
		t.Fatal(err)
	}
	hub := event.NewHub()
	eng := engine.New(st, hub, registry.Static(mock.New()), st)
	server := New(eng, st, st, hub)
	ids, err := server.projectSessionIDs(ctx, "project")
	if err != nil || len(ids) != 2 || !slices.Contains(ids, "parent") || !slices.Contains(ids, "child") {
		t.Fatalf("permission cleanup lost child: %v %v", ids, err)
	}
	handler := server.Handler(testToken, nil)
	for _, scope := range []string{"active", "archived"} {
		if scope == "archived" {
			if _, err := st.ArchiveSession(ctx, "parent"); err != nil {
				t.Fatal(err)
			}
		}
		req := httptest.NewRequest(http.MethodGet, "/sessions?scope="+scope, nil)
		req.Header.Set("Authorization", "Bearer "+testToken)
		response := httptest.NewRecorder()
		handler.ServeHTTP(response, req)
		var payload struct {
			Sessions []*store.Session `json:"sessions"`
		}
		if err := json.Unmarshal(response.Body.Bytes(), &payload); err != nil {
			t.Fatal(err)
		}
		if response.Code != http.StatusOK || len(payload.Sessions) != 1 || payload.Sessions[0].ID != "parent" {
			t.Fatalf("%s list exposed child: %d %s", scope, response.Code, response.Body.String())
		}
	}
	if ids, err := server.projectSessionIDs(ctx, "project"); err != nil || len(ids) != 0 {
		t.Fatalf("archived group treated as active: %v %v", ids, err)
	}
}

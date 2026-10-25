package api

import (
	"net/http"
	"reflect"
	"testing"

	"github.com/teatak/pudding-core/internal/store"
)

func TestAssistantAppearanceFavoritesAPI(t *testing.T) {
	srv, _ := newTestServer(t)
	appearance := store.AssistantAppearance{"version": 1, "custom": map[string]any{"color": "peach"}}
	initial := decodeJSON[store.AssistantSnapshot](t, req(t, "POST", srv.URL+"/assistant/open", map[string]any{"locale": "en", "appearance": appearance}))
	var favoriteID string
	for attempt := 0; attempt < 2; attempt++ {
		saved := decodeJSON[store.AssistantSnapshot](t, req(t, "POST", srv.URL+"/assistant/appearance-favorites", map[string]any{"appearance": appearance}))
		if len(saved.Assistant.AppearanceFavorites) != 1 || !saved.Assistant.AppearanceFavorites[0].Appearance.Equal(appearance) {
			t.Fatalf("favorite/retry lost configuration: %+v", saved)
		}
		favoriteID = saved.Assistant.AppearanceFavorites[0].ID
		if saved.Assistant.Revision != initial.Assistant.Revision || !saved.Assistant.Appearance.Equal(initial.Assistant.Appearance) || len(saved.Assistant.AppearanceHistory) != 0 {
			t.Fatal("favorite changed profile or history")
		}
	}
	response := req(t, "POST", srv.URL+"/assistant/appearance-favorites", map[string]any{"appearance": map[string]any{"version": 0}})
	response.Body.Close()
	if response.StatusCode != http.StatusBadRequest {
		t.Fatalf("invalid configuration accepted: %d", response.StatusCode)
	}
	read := decodeJSON[store.AssistantSnapshot](t, req(t, "GET", srv.URL+"/assistant", nil))
	if len(read.Assistant.AppearanceFavorites) != 1 {
		t.Fatal("favorite not persisted")
	}
	for attempt := 0; attempt < 2; attempt++ {
		removed := decodeJSON[store.AssistantSnapshot](t, req(t, "DELETE", srv.URL+"/assistant/appearance-favorites/"+favoriteID, nil))
		if !reflect.DeepEqual(removed, initial) {
			t.Fatalf("unfavorite/retry changed profile: %+v", removed)
		}
	}
}

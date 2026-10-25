package api

import (
	"reflect"
	"testing"

	"github.com/teatak/pudding-core/internal/store"
)

func TestAssistantAppearanceHistoryOnDialogClose(t *testing.T) {
	srv, _ := newTestServer(t)
	opening := store.AssistantAppearance{"version": 1, "body": "square"}
	current := decodeJSON[store.AssistantSnapshot](t, req(t, "POST", srv.URL+"/assistant/open", map[string]any{"locale": "en", "appearance": opening}))
	for _, body := range []string{"cat", "square", "round"} {
		current = decodeJSON[store.AssistantSnapshot](t, req(t, "PUT", srv.URL+"/assistant", map[string]any{
			"expectedRevision": current.Assistant.Revision, "appearanceEditGroup": "dialog-1", "appearance": store.AssistantAppearance{"version": 1, "body": body},
		}))
		if len(current.Assistant.AppearanceHistory) != 0 {
			t.Fatal("part save changed history before close")
		}
	}
	closed := decodeJSON[store.AssistantSnapshot](t, req(t, "POST", srv.URL+"/assistant/appearance-history/finish", map[string]any{"appearanceEditGroup": "dialog-1"}))
	if len(closed.Assistant.AppearanceHistory) != 1 || !closed.Assistant.AppearanceHistory[0].Equal(opening) {
		t.Fatalf("close lost opening checkpoint: %+v", closed)
	}
	if closed.Assistant.Revision != current.Assistant.Revision || !closed.Assistant.Appearance.Equal(current.Assistant.Appearance) {
		t.Fatal("finishing history changed profile")
	}
	replayed := decodeJSON[store.AssistantSnapshot](t, req(t, "POST", srv.URL+"/assistant/appearance-history/finish", map[string]any{"appearanceEditGroup": "dialog-1"}))
	if !reflect.DeepEqual(replayed, closed) {
		t.Fatal("close retry changed history")
	}
}

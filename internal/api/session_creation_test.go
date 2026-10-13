package api

import (
	"context"
	"net/http"
	"strings"
	"testing"

	"github.com/teatak/pudding-core/internal/store"
)

func TestCreateSessionRequestReplay(t *testing.T) {
	srv, st := newTestServer(t)
	body := map[string]string{"clientRequestID": "creation-uuid", "provider": "mock", "model": "mock"}
	// Discard the response, as when a connection loses it after the commit.
	first := req(t, http.MethodPost, srv.URL+"/sessions", body)
	if first.StatusCode != http.StatusCreated {
		t.Fatal(first.Status)
	}
	first.Body.Close()
	replay := decodeJSON[store.Session](t, req(t, http.MethodPost, srv.URL+"/sessions", body))
	sessions, err := st.ListSessions(context.Background())
	if err != nil || len(sessions) != 1 || sessions[0].ID != replay.ID {
		t.Fatalf("creation duplicated: %v %v", sessions, err)
	}
	title := "new name"
	if _, err := st.UpdateSession(context.Background(), replay.ID, store.SessionUpdate{Title: &title}); err != nil {
		t.Fatal(err)
	}
	body["title"] = "old title"
	body["model"] = "other-model"
	again := decodeJSON[store.Session](t, req(t, http.MethodPost, srv.URL+"/sessions", body))
	if again.Title != title || again.Model != "mock" || again.ID != replay.ID {
		t.Fatalf("replay overwrote state: %+v", again)
	}
	for _, key := range []string{"", "bad key", "../path", "中文", strings.Repeat("x", 201)} {
		body["clientRequestID"] = key
		r := req(t, http.MethodPost, srv.URL+"/sessions", body)
		r.Body.Close()
		if r.StatusCode != http.StatusBadRequest {
			t.Fatalf("key %q: %s", key, r.Status)
		}
	}
}

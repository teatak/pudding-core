package api

import (
	"context"
	"errors"
	"net/http"
	"os"
	"path/filepath"
	"testing"

	"github.com/teatak/pudding-core/internal/home"
	"github.com/teatak/pudding-core/internal/store"
)

func TestDeleteAssistantAPI(t *testing.T) {
	srv, st, homeDir := newTestServerWithHome(t)
	ctx := context.Background()
	profile := decodeJSON[store.AssistantSnapshot](t, req(t, "POST", srv.URL+"/assistant/open", map[string]string{"locale": "en", "provider": "mock", "model": "mock"}))
	id := profile.Assistant.SessionID
	ordinary := decodeJSON[store.Session](t, req(t, "POST", srv.URL+"/sessions", map[string]string{"title": "Keep me", "provider": "mock", "model": "mock"}))
	item := decodeJSON[map[string]any](t, req(t, "POST", srv.URL+"/studio/items", map[string]string{"kind": "page", "name": "Kept artifact", "body": "Keep this page", "sourceSessionID": id}))
	itemID := item["id"].(string)
	response := req(t, "POST", srv.URL+"/sessions/"+id+"/studio/items/"+itemID+"/open", map[string]any{})
	response.Body.Close()
	if response.StatusCode != http.StatusOK {
		t.Fatalf("mount: %d", response.StatusCode)
	}
	scratch, err := home.PrepareCodeScratch(homeDir, id)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(scratch, "temporary.txt"), []byte("delete"), 0600); err != nil {
		t.Fatal(err)
	}
	response = req(t, "DELETE", srv.URL+"/assistant", nil)
	response.Body.Close()
	if response.StatusCode != http.StatusNoContent {
		t.Fatalf("delete: %d", response.StatusCode)
	}
	got := decodeJSON[store.AssistantSnapshot](t, req(t, "GET", srv.URL+"/assistant", nil))
	if got.Assistant != nil || len(got.Memories) != 0 {
		t.Fatalf("profile remains: %+v", got)
	}
	if _, err := st.GetSession(ctx, id); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("session remains: %v", err)
	}
	if _, err := os.Stat(scratch); !os.IsNotExist(err) {
		t.Fatalf("scratch remains: %v", err)
	}
	if _, err := st.GetSession(ctx, ordinary.ID); err != nil {
		t.Fatal(err)
	}
	response = req(t, "GET", srv.URL+"/studio/items/"+itemID, nil)
	response.Body.Close()
	if response.StatusCode != http.StatusOK {
		t.Fatalf("artifact removed: %d", response.StatusCode)
	}
	response = req(t, "POST", srv.URL+"/sessions/"+id+"/restore", nil)
	response.Body.Close()
	if response.StatusCode != http.StatusNotFound {
		t.Fatalf("permanent deletion is restorable: %d", response.StatusCode)
	}
	response = req(t, "DELETE", srv.URL+"/assistant", nil)
	response.Body.Close()
	if response.StatusCode != http.StatusNoContent {
		t.Fatalf("delete retry: %d", response.StatusCode)
	}
	fresh := decodeJSON[store.AssistantSnapshot](t, req(t, "POST", srv.URL+"/assistant/open", map[string]string{"locale": "en", "provider": "mock", "model": "mock"}))
	if fresh.Assistant.SessionID == id {
		t.Fatal("reused deleted conversation")
	}
}

func TestAssistantAPI(t *testing.T) {
	srv, _ := newTestServer(t)
	initial := decodeJSON[store.AssistantSnapshot](t, req(t, http.MethodGet, srv.URL+"/assistant", nil))
	if initial.Assistant != nil || len(initial.Memories) != 0 {
		t.Fatalf("initial: %+v", initial)
	}
	input := map[string]any{"expectedRevision": 0, "name": "  小布  ", "userName": "  杨  ", "appearance": store.AssistantAppearance{Palette: "lavender", Body: "classic", Eyes: "classic", Mouth: "classic", Headwear: "none", Arms: "none", Eyewear: "classic", FaceSticker: "none"}, "communicationPreference": "  回答简短  "}
	response := req(t, http.MethodPut, srv.URL+"/assistant", input)
	if response.StatusCode != http.StatusOK {
		t.Fatalf("save: %d", response.StatusCode)
	}
	got := decodeJSON[store.AssistantSnapshot](t, response)
	if got.Assistant.Name != "小布" || got.Assistant.UserName != "杨" || len(got.Memories) != 1 || got.Memories[0].Content != "回答简短" {
		t.Fatalf("saved: %+v", got)
	}
	response = req(t, http.MethodPut, srv.URL+"/assistant", input)
	replay := decodeJSON[store.AssistantSnapshot](t, response)
	if replay.Assistant.Revision != 1 {
		t.Fatalf("retry: %+v", replay)
	}
	input["name"] = "过期设置"
	response = req(t, http.MethodPut, srv.URL+"/assistant", input)
	response.Body.Close()
	if response.StatusCode != http.StatusConflict {
		t.Fatalf("stale setup: %d", response.StatusCode)
	}
	input["expectedRevision"] = 1
	input["name"] = "  "
	cleared := decodeJSON[store.AssistantSnapshot](t, req(t, http.MethodPut, srv.URL+"/assistant", input))
	if cleared.Assistant.Name != "" || cleared.Assistant.Revision != 2 {
		t.Fatalf("blank name did not restore localized default: %+v", cleared)
	}
	reopened := decodeJSON[store.AssistantSnapshot](t, req(t, http.MethodPost, srv.URL+"/assistant/open", map[string]any{"locale": "en"}))
	if reopened.Assistant.Name != "" || reopened.Assistant.Revision != 2 {
		t.Fatalf("opening changed the default-name selection: %+v", reopened)
	}
}

func TestAssistantAPIRejectsInvalidInput(t *testing.T) {
	srv, _ := newTestServer(t)
	appearance := map[string]string{"palette": "original", "body": "classic", "eyes": "classic", "mouth": "classic", "headwear": "none", "arms": "none", "eyewear": "none", "faceSticker": "none"}
	for _, invalid := range []map[string]any{
		{"expectedRevision": nil}, {"expectedRevision": -1},
		{"name": "invalid\nname"},
	} {
		input := map[string]any{"expectedRevision": 0, "name": "小布", "appearance": appearance}
		for k, v := range invalid {
			input[k] = v
		}
		response := req(t, http.MethodPut, srv.URL+"/assistant", input)
		response.Body.Close()
		if response.StatusCode != http.StatusBadRequest {
			t.Fatalf("input %+v: %d", input, response.StatusCode)
		}
	}
	for slot := range appearance {
		valid := appearance[slot]
		appearance[slot] = ""
		response := req(t, http.MethodPut, srv.URL+"/assistant", map[string]any{"expectedRevision": 0, "name": "小布", "appearance": appearance})
		response.Body.Close()
		if response.StatusCode != http.StatusBadRequest {
			t.Fatalf("invalid %s accepted: %d", slot, response.StatusCode)
		}
		appearance[slot] = valid
	}
	got := decodeJSON[store.AssistantSnapshot](t, req(t, http.MethodGet, srv.URL+"/assistant", nil))
	if got.Assistant != nil {
		t.Fatal("invalid setup created assistant")
	}
	response, err := http.Get(srv.URL + "/assistant")
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusUnauthorized {
		t.Fatalf("unauthenticated: %d", response.StatusCode)
	}
}

func TestAssistantNewPartsRoundTrip(t *testing.T) {
	srv, _ := newTestServer(t)
	opened := decodeJSON[store.AssistantSnapshot](t, req(t, http.MethodPost, srv.URL+"/assistant/open", map[string]any{"locale": "zh-CN", "provider": "mock", "model": "mock"}))
	for index, appearance := range []store.AssistantAppearance{
		{Palette: "lavender", Body: "classic", Eyes: "curved", Mouth: "smile", Headwear: "hat", Arms: "short", Eyewear: "classic", FaceSticker: "freckles"},
		{Palette: "mint", Body: "classic", Eyes: "dots", Mouth: "surprised", Headwear: "beacon", Arms: "orb", Eyewear: "goggles", FaceSticker: "stars"},
		{Palette: "blue", Body: "round", Eyes: "classic", Mouth: "classic", Headwear: "antenna", Arms: "classic", Eyewear: "classic", FaceSticker: "blush"},
		{Palette: "coral", Body: "round", Eyes: "classic", Mouth: "classic", Headwear: "antenna", Arms: "classic", Eyewear: "classic", FaceSticker: "blush"},
		{Palette: "yellow", Body: "square", Eyes: "dots", Mouth: "smile", Headwear: "hat", Arms: "short", Eyewear: "none", FaceSticker: "freckles"},
		{Palette: "violet", Body: "square", Eyes: "dots", Mouth: "smile", Headwear: "hat", Arms: "short", Eyewear: "none", FaceSticker: "freckles"},
		{Palette: "emerald", Body: "classic", Eyes: "curved", Mouth: "surprised", Headwear: "beacon", Arms: "orb", Eyewear: "goggles", FaceSticker: "stars"},
		{Palette: "violet", Body: "pudding", Eyes: "dots", Mouth: "smile", Headwear: "hat", Arms: "short", Eyewear: "classic", FaceSticker: "freckles"},
		{Palette: "violet", Body: "hexagon", Eyes: "dots", Mouth: "smile", Headwear: "hat", Arms: "short", Eyewear: "classic", FaceSticker: "freckles"},
		{Palette: "violet", Body: "cat", Eyes: "dots", Mouth: "smile", Headwear: "hat", Arms: "short", Eyewear: "classic", FaceSticker: "freckles"},
		{Palette: "coral", Body: "pudding", Eyes: "wink", Mouth: "grin", Headwear: "crown", Arms: "mitten", Eyewear: "glasses", FaceSticker: "hearts"},
		{Palette: "blue", Body: "hexagon", Eyes: "sleepy", Mouth: "mew", Headwear: "bow", Arms: "claw", Eyewear: "sunglasses", FaceSticker: "lightning"},
		{Palette: "crimson", Body: "devil", Eyes: "mischief", Mouth: "fangs", Headwear: "horns", Arms: "wings", Eyewear: "domino", FaceSticker: "flames"},
		{Palette: "apricot", Body: "plump", Eyes: "jolly", Mouth: "laugh", Headwear: "santa", Arms: "santa", Eyewear: "halfmoon", FaceSticker: "beard"},
		{Palette: "gold", Body: "classic", Eyes: "curved", Mouth: "grin", Headwear: "tang", Arms: "greeting", Eyewear: "none", FaceSticker: "papercut"},
	} {
		input := map[string]any{"expectedRevision": index + 1, "name": "小布", "appearance": appearance, "communicationPreference": "简短回答"}
		response := req(t, http.MethodPut, srv.URL+"/assistant", input)
		if response.StatusCode != http.StatusOK {
			t.Fatalf("save parts: %d", response.StatusCode)
		}
		got := decodeJSON[store.AssistantSnapshot](t, response)
		if got.Assistant.Appearance != appearance || got.Assistant.SessionID != opened.Assistant.SessionID {
			t.Fatalf("parts changed: %+v", got)
		}
		read := decodeJSON[store.AssistantSnapshot](t, req(t, http.MethodGet, srv.URL+"/assistant", nil))
		if read.Assistant.Appearance != appearance || read.Assistant.Revision != int64(index+2) {
			t.Fatalf("parts not persisted: %+v", read)
		}
	}
}

func TestOpenAssistantAPI(t *testing.T) {
	srv, _ := newTestServer(t)
	input := map[string]any{"locale": "en", "provider": "mock", "model": "mock"}
	got := decodeJSON[store.AssistantSnapshot](t, req(t, http.MethodPost, srv.URL+"/assistant/open", input))
	if got.Assistant == nil || got.Assistant.Name != "" || got.Assistant.SessionID == "" {
		t.Fatalf("open: %+v", got)
	}
	input["locale"] = "zh-CN"
	replay := decodeJSON[store.AssistantSnapshot](t, req(t, http.MethodPost, srv.URL+"/assistant/open", input))
	if replay.Assistant.Name != "" || replay.Assistant.SessionID != got.Assistant.SessionID || replay.Assistant.Revision != 1 {
		t.Fatalf("replay: %+v", replay)
	}
	if session := decodeJSON[store.Session](t, req(t, http.MethodGet, srv.URL+"/sessions/"+got.Assistant.SessionID, nil)); session.Model != "mock" {
		t.Fatalf("session: %+v", session)
	}
	for _, action := range []struct{ method, suffix string }{{http.MethodPost, "/archive"}, {http.MethodDelete, ""}} {
		response := req(t, action.method, srv.URL+"/sessions/"+got.Assistant.SessionID+action.suffix, nil)
		status := response.StatusCode
		body := decodeJSON[map[string]string](t, response)
		if status != http.StatusConflict || body["error"] != "assistant_conversation_protected" {
			t.Fatalf("assistant lifecycle %s: %d %+v", action.method, status, body)
		}
	}
	if session := decodeJSON[store.Session](t, req(t, http.MethodGet, srv.URL+"/sessions/"+got.Assistant.SessionID, nil)); session.ArchivedAt != nil {
		t.Fatal("assistant conversation was archived")
	}
	for _, invalid := range []map[string]any{{"locale": "fr"}, {"locale": "en", "provider": "mock"}} {
		response := req(t, http.MethodPost, srv.URL+"/assistant/open", invalid)
		response.Body.Close()
		if response.StatusCode != http.StatusBadRequest {
			t.Fatalf("invalid open: %d", response.StatusCode)
		}
	}
}

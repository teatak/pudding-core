package api

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/teatak/pudding-core/internal/engine"
	"github.com/teatak/pudding-core/internal/event"
	"github.com/teatak/pudding-core/internal/plugin"
	"github.com/teatak/pudding-core/internal/store"
	"github.com/teatak/pudding-core/internal/store/storetest"
)

func TestWidgetRunRoutesRequireAuthAndRuntimeAndPreserveNotices(t *testing.T) {
	st := storetest.New(t)
	ctx := context.Background()
	hub := event.NewHub()
	eng := engine.New(st, hub, nil, st)
	defer eng.Stop()
	if err := st.CreateSession(ctx, &store.Session{ID: "participant", Title: "Participant", Provider: "mock", Model: "mock-model"}); err != nil {
		t.Fatal(err)
	}
	item, err := st.CreateStudioItem(ctx, &store.StudioItem{ID: "widget", Kind: "widget", Name: "Review"})
	if err != nil {
		t.Fatal(err)
	}
	hash := strings.Repeat("a", 64)
	item, err = st.SaveStudioItemRevision(ctx, &store.StudioItemRevision{ItemID: item.ID, Hash: hash, ClientRequestID: "source", CreatedAt: time.Now()}, "")
	if err != nil {
		t.Fatal(err)
	}
	if err := st.PutWidgetBuildReceipt(ctx, item.ID, hash, json.RawMessage(`{"ok":true}`)); err != nil {
		t.Fatal(err)
	}
	item.ActiveRevision = hash
	item, err = st.UpdateStudioItem(ctx, item, item.Revision)
	if err != nil {
		t.Fatal(err)
	}
	handler := New(eng, st, st, hub).Handler("token", nil)
	call := func(method, path string, body any, auth, runtime string, status int) []byte {
		t.Helper()
		raw, _ := json.Marshal(body)
		req := httptest.NewRequest(method, path, bytes.NewReader(raw))
		if auth != "" {
			req.Header.Set("Authorization", "Bearer "+auth)
		}
		req.Header.Set(plugin.RuntimeIDHeader, runtime)
		out := httptest.NewRecorder()
		handler.ServeHTTP(out, req)
		if out.Code != status {
			t.Fatalf("%s %s: %d %s expected %d", method, path, out.Code, out.Body.String(), status)
		}
		if !json.Valid(out.Body.Bytes()) {
			t.Fatal("route escaped API handling", out.Body.String())
		}
		return out.Body.Bytes()
	}
	input := map[string]any{"targetID": "target", "revisionHash": hash, "bindingVersion": item.BindingVersion, "participants": []map[string]any{{"sessionID": "participant"}}}
	call("POST", "/studio/items/widget/runs", input, "", "desktop", 401)
	call("POST", "/studio/items/widget/runs", input, "token", "", 400)
	call("POST", "/studio/items/widget/pages/select", map[string]any{"scope": "library"}, "token", "desktop", 200)
	var opened struct {
		Page store.WidgetPage `json:"page"`
	}
	if err := json.Unmarshal(call("POST", "/studio/items/widget/pages", map[string]any{"scope": "library", "revisionHash": hash, "targetID": "target"}, "token", "desktop", 200), &opened); err != nil {
		t.Fatal(err)
	}
	pagePath := "/widget-pages/" + opened.Page.ID
	write := map[string]any{"targetID": "target", "expectedVersion": 0, "data": map[string]any{"board": []int{1, 2}}}
	call("PUT", pagePath, write, "", "desktop", 401)
	call("PUT", pagePath, write, "token", "desktop", 200)
	call("PUT", pagePath, write, "token", "desktop", 409)
	var run engine.WidgetRun
	if err := json.Unmarshal(call("POST", "/studio/items/widget/runs", input, "token", "desktop", 200), &run); err != nil {
		t.Fatal(err)
	}
	path := "/widget-runs/" + run.ID
	call("GET", path, nil, "", "desktop", 401)
	call("GET", path, nil, "token", "other", 400)
	call("GET", path, nil, "token", "desktop", 200)
	members := map[string]any{"participants": []map[string]any{{"sessionID": "participant"}, {}}}
	call("PUT", path+"/participants", members, "", "desktop", 401)
	call("PUT", path+"/participants", members, "token", "other", 400)
	call("PUT", path+"/participants", map[string]any{}, "token", "desktop", 400)
	call("PUT", path+"/participants", members, "token", "desktop", 200)
	call("PUT", path+"/requests", map[string]any{}, "token", "desktop", 400)
	call("PUT", path+"/requests", map[string]any{"requests": []any{}}, "token", "desktop", 200)
	n := map[string]any{"stateVersion": 0, "notification": map[string]any{"id": "notice", "audience": map[string]any{"kind": "all"}, "delivery": "inform", "topic": "update", "message": "Draft changed"}}
	call("POST", path+"/notifications", n, "token", "desktop", 200)
	call("POST", path+"/notifications", n, "token", "desktop", 200)
	page, err := st.ListTurnsPage(ctx, "participant", "", 10)
	if err != nil || len(page.Turns) != 1 {
		t.Fatal(page, err)
	}
	call("POST", "/sessions/participant/widget-runs/"+run.ID+"/authorize", map[string]any{}, "token", "desktop", 200)
	call("POST", "/sessions/outsider/widget-runs/"+run.ID+"/authorize", map[string]any{}, "token", "desktop", 400)
	// A default upgrade leaves the pinned page, its run and its shared writes valid.
	next := strings.Repeat("b", 64)
	item, err = st.SaveStudioItemRevision(ctx, &store.StudioItemRevision{ItemID: item.ID, Hash: next, ClientRequestID: "upgrade", CreatedAt: time.Now()}, item.HeadRevision)
	if err != nil {
		t.Fatal(err)
	}
	if err = st.PutWidgetBuildReceipt(ctx, item.ID, next, []byte(`{"ok":true}`)); err != nil {
		t.Fatal(err)
	}
	item.ActiveRevision = next
	if _, err = st.UpdateStudioItem(ctx, item, item.Revision); err != nil {
		t.Fatal(err)
	}
	authority := map[string]any{"revisionHash": hash, "targetID": "target"}
	call("POST", "/studio/items/widget/pages/authorize", authority, "", "desktop", 401)
	call("POST", "/studio/items/widget/pages/authorize", authority, "token", "desktop", 200)
	call("POST", "/sessions/participant/widget-runs/"+run.ID+"/authorize", map[string]any{}, "token", "desktop", 200)
	call("PUT", "/studio/items/widget/data", map[string]any{"revisionHash": hash, "targetID": "target", "expectedVersion": 0, "data": map[string]any{"keep": true}}, "token", "desktop", 200)
	// Opening an arbitrary historical preview in another scope does not grant access.
	call("POST", "/studio/items/widget/pages/select", map[string]any{"scope": "participant"}, "token", "desktop", 200)
	call("POST", "/studio/items/widget/pages", map[string]any{"scope": "participant", "revisionHash": hash, "targetID": "preview"}, "token", "desktop", 200)
	call("POST", "/studio/items/widget/pages/authorize", map[string]any{"revisionHash": hash, "targetID": "preview"}, "token", "desktop", 409)
	call("PUT", "/studio/items/widget/data", map[string]any{"revisionHash": hash, "targetID": "preview", "expectedVersion": 1, "data": map[string]any{}}, "token", "desktop", 409)

	call("PATCH", path, map[string]any{"action": "pause"}, "token", "desktop", 200)
	call("POST", "/sessions/participant/widget-runs/"+run.ID+"/authorize", map[string]any{}, "token", "desktop", 400)
	call("PATCH", path, map[string]any{"action": "stop"}, "token", "desktop", 200)
	call("GET", path, nil, "token", "desktop", 400)
	call("DELETE", "/studio/items/widget/pages", map[string]any{"scope": "library"}, "token", "desktop", 200)
	call("POST", "/studio/items/widget/pages/authorize", authority, "token", "desktop", 409)
	write["expectedVersion"] = 1
	call("PUT", pagePath, write, "token", "desktop", 404)
}

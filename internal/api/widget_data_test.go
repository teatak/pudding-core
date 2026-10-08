package api

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/teatak/pudding-core/contracts"
	"github.com/teatak/pudding-core/internal/store"
	"github.com/teatak/pudding-core/internal/store/storetest"
)

func TestWidgetDataAPI(t *testing.T) {
	st := storetest.New(t)
	ctx := context.Background()
	w, err := st.CreateStudioItem(ctx, &store.StudioItem{ID: "todo", Kind: "widget", Name: "Todo"})
	if err != nil {
		t.Fatal(err)
	}
	hash := strings.Repeat("a", 64)
	w, err = st.SaveStudioItemRevision(ctx, &store.StudioItemRevision{ItemID: w.ID, Hash: hash, ClientRequestID: "source", CreatedAt: time.Now()}, "")
	if err != nil {
		t.Fatal(err)
	}
	if err := st.PutWidgetBuildReceipt(ctx, w.ID, hash, json.RawMessage(`{"ok":true}`)); err != nil {
		t.Fatal(err)
	}
	w.ActiveRevision = hash
	if _, err = st.UpdateStudioItem(ctx, w, w.Revision); err != nil {
		t.Fatal(err)
	}
	handler := New(nil, st, st, nil).WithHome(t.TempDir()).Handler("fixture-token", nil)
	call := func(method, path string, input any, token string, status int) []byte {
		t.Helper()
		body, err := json.Marshal(input)
		if err != nil {
			t.Fatal(err)
		}
		req := httptest.NewRequest(method, path, bytes.NewReader(body))
		if token != "" {
			req.Header.Set("Authorization", "Bearer "+token)
		}
		out := httptest.NewRecorder()
		handler.ServeHTTP(out, req)
		if out.Code != status {
			t.Fatalf("%s %s: %d %s want %d", method, path, out.Code, out.Body.String(), status)
		}
		return out.Body.Bytes()
	}
	path := "/studio/items/" + w.ID + "/data"
	input := map[string]any{"revisionHash": hash, "expectedVersion": 0, "data": map[string]any{"tasks": []any{"Review form"}}}
	call(http.MethodGet, path, nil, "", 401)
	call(http.MethodPut, path, input, "", 401)
	var data store.WidgetData
	if err = json.Unmarshal(call("GET", path, nil, "fixture-token", 200), &data); err != nil || data.Version != 0 || string(data.Data) != "{}" {
		t.Fatal(data, err)
	}
	if err = json.Unmarshal(call("PUT", path, input, "fixture-token", 200), &data); err != nil || data.Version != 1 {
		t.Fatal(data, err)
	}
	var conflict map[string]any
	_ = json.Unmarshal(call("PUT", path, input, "fixture-token", 409), &conflict)
	if conflict["error"] != "widget_data_conflict" {
		t.Fatal(conflict)
	}
	for _, bad := range []map[string]any{
		{"revisionHash": hash, "data": map[string]any{}},
		{"revisionHash": hash, "expectedVersion": 1, "data": nil},
		{"revisionHash": hash, "expectedVersion": 1, "data": []any{}},
		{"revisionHash": hash, "expectedVersion": -1, "data": map[string]any{}},
		{"revisionHash": hash, "expectedVersion": 1.5, "data": map[string]any{}},
		{"revisionHash": hash, "expectedVersion": 1, "data": map[string]any{}, "itemID": "other"},
		{"revisionHash": hash, "expectedVersion": 1, "data": map[string]any{"text": strings.Repeat("x", contracts.Widget().MaxStorageBytes)}},
	} {
		call("PUT", path, bad, "fixture-token", 400)
	}
	call("PUT", path, map[string]any{"revisionHash": strings.Repeat("b", 64), "expectedVersion": 1, "data": map[string]any{}}, "fixture-token", 409)
	call("GET", "/studio/items/missing/data", nil, "fixture-token", 404)
	if _, err = st.CreateDocument(ctx, &store.StudioItem{ID: "doc", Kind: "doc", Name: "Doc"}, "body", store.ContentAuthor{Kind: "user"}); err != nil {
		t.Fatal(err)
	}
	call("GET", "/studio/items/doc/data", nil, "fixture-token", 400)
	if err = json.Unmarshal(call("GET", path, nil, "fixture-token", 200), &data); err != nil || data.Version != 1 || string(data.Data) != `{"tasks":["Review form"]}` {
		t.Fatal("rejected request changed data", data, err)
	}
}

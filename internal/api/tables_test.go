package api

import (
	"bytes"
	"encoding/json"
	"github.com/teatak/pudding-core/internal/store"
	"github.com/teatak/pudding-core/internal/store/storetest"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestNativeTableAPI(t *testing.T) {
	st := storetest.New(t)
	handler := New(nil, st, st, nil).WithHome(t.TempDir()).Handler("token", nil)
	call := func(method, path, body string, status int) []byte {
		t.Helper()
		req := httptest.NewRequest(method, path, bytes.NewBufferString(body))
		req.Header.Set("Authorization", "Bearer token")
		res := httptest.NewRecorder()
		handler.ServeHTTP(res, req)
		if res.Code != status {
			t.Fatalf("%s %s: %d %s; want %d", method, path, res.Code, res.Body.String(), status)
		}
		return res.Body.Bytes()
	}
	var item store.StudioItem
	json.Unmarshal(call("POST", "/studio/items", `{"kind":"table","name":"Data","body":{"columns":[{"id":"c","name":"Value","type":"number"}],"rows":[{"id":"r","cells":{"c":1}}]}}`, 201), &item)
	route := "/studio/items/" + item.ID
	var content store.TableContent
	json.Unmarshal(call("GET", route+"/content", "", 200), &content)
	if content.Body.Rows[0].Cells["c"] != float64(1) {
		t.Fatal(content)
	}
	raw := call("POST", route+"/operations", `{"clientRequestID":"edit","operations":[{"kind":"set_cell","rowID":"r","columnID":"c","value":2,"expected":1}]}`, 200)
	json.Unmarshal(raw, &content)
	if !strings.Contains(string(call("GET", route+"/revisions/"+content.RevisionID, "", 200)), `"kind":"table"`) {
		t.Fatal("table revision unavailable")
	}
	call("POST", route+"/operations", `{"clientRequestID":"stale","operations":[{"kind":"set_cell","rowID":"r","columnID":"c","value":3,"expected":1}]}`, 409)
	call("POST", route+"/operations", `{"clientRequestID":"invalid","operations":[{"kind":"set_cell","rowID":"r","columnID":"c","value":"wrong type"}]}`, 400)
	call("PUT", route+"/content", `{"clientRequestID":"doc","body":"oops","expectedHash":"wrong"}`, 404)
	call("POST", route+"/assets", "image", 404)
	call("POST", route+"/draft", `{}`, 400)
	call("POST", "/studio/items", `{"kind":"table","name":"Invalid","body":{"columns":null,"rows":[]}}`, 400)
}

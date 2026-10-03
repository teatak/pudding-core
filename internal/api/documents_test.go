package api

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/teatak/pudding-core/internal/store"
	"github.com/teatak/pudding-core/internal/store/storetest"
)

func TestDocumentAPIAndWidgetBoundary(t *testing.T) {
	st := storetest.New(t)
	handler := New(nil, st, st, nil).WithHome(t.TempDir()).Handler("token", nil)
	call := func(method, path string, body []byte, status int) []byte {
		t.Helper()
		r := httptest.NewRequest(method, path, bytes.NewReader(body))
		r.Header.Set("Authorization", "Bearer token")
		out := httptest.NewRecorder()
		handler.ServeHTTP(out, r)
		if out.Code != status {
			t.Fatalf("%s %s: %d %s; want %d", method, path, out.Code, out.Body.String(), status)
		}
		return out.Body.Bytes()
	}
	var item store.StudioItem
	if err := json.Unmarshal(call("POST", "/studio/items", []byte(`{"kind":"doc","name":"Notes","body":"# Hello\n\nInitial"}`), 201), &item); err != nil {
		t.Fatal(err)
	}
	path := "/studio/items/" + item.ID
	var doc store.DocumentContent
	json.Unmarshal(call("GET", path+"/content", nil, 200), &doc)
	body, _ := json.Marshal(map[string]any{"clientRequestID": "save", "body": "# Updated", "expectedHash": doc.ContentHash})
	json.Unmarshal(call("PUT", path+"/content", body, 200), &doc)
	call("PUT", path+"/content", []byte(`{"clientRequestID":"stale","body":"oops","expectedHash":"stale"}`), 409)
	response := call("GET", path+"/revisions/"+doc.RevisionID, nil, 200)
	if !strings.Contains(string(response), `"body":"# Updated"`) {
		t.Fatal("revision missing body", string(response))
	}
	call("POST", path+"/draft", []byte(`{}`), 400)
	call("POST", path+"/activate", []byte(`{}`), 400)
	call("POST", path+"/assets", []byte(`<svg xmlns="http://www.w3.org/2000/svg"><script>alert(1)</script></svg>`), 400)
	png, _ := base64.StdEncoding.DecodeString("iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAQAAAC1HAwCAAAAC0lEQVR42mNk+A8AAQUBAScY42YAAAAASUVORK5CYII=")
	var asset struct {
		Path string `json:"path"`
	}
	json.Unmarshal(call("POST", path+"/assets", png, 201), &asset)
	if got := call("GET", path+"/"+asset.Path, nil, 200); !bytes.Equal(got, png) {
		t.Fatal("asset changed")
	}
	call("GET", path+"/assets/not-a-hash.png", nil, 400)
	call("POST", "/studio/items", []byte(`{"kind":"table","name":"Not yet"}`), 400)
}

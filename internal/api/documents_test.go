package api

import (
	"bytes"
	"encoding/json"
	"net/http/httptest"
	"testing"

	"github.com/teatak/pudding-core/internal/store"
	"github.com/teatak/pudding-core/internal/store/storetest"
)

func TestDocumentAPIBoundaries(t *testing.T) {
	st := storetest.New(t)
	handler := New(nil, st, st, nil).WithHome(t.TempDir()).Handler("token", nil)
	call := func(method, path string, body any, status int) []byte {
		t.Helper()
		raw, _ := json.Marshal(body)
		req := httptest.NewRequest(method, path, bytes.NewReader(raw))
		req.Header.Set("Authorization", "Bearer token")
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)
		if rec.Code != status {
			t.Fatalf("%s %s: %d %s", method, path, rec.Code, rec.Body.String())
		}
		return rec.Body.Bytes()
	}
	var item store.StudioItem
	json.Unmarshal(call("POST", "/studio/items", map[string]any{"kind": "rich-document", "name": "DOCX"}, 201), &item)
	var doc store.DocumentContent
	path := "/studio/items/" + item.ID
	json.Unmarshal(call("GET", path+"/rich-content", nil, 200), &doc)
	if doc.Body.SchemaVersion != 1 || doc.Body.Body.Type != "doc" {
		t.Fatal("missing structured source")
	}
	call("GET", path+"/content", nil, 404)
	call("PUT", path+"/content", map[string]any{"clientRequestID": "md", "body": "overwrite", "expectedHash": doc.ContentHash}, 404)
	call("POST", path+"/draft", map[string]any{}, 400)
	write := store.DocumentWrite{ClientRequestID: "save", ExpectedHash: doc.ContentHash, Body: &doc.Body}
	saved := call("PUT", path+"/rich-content", write, 200)
	call("PUT", path+"/rich-content", write, 200)
	write.ExpectedHash = string(bytes.Repeat([]byte("a"), 64))
	write.ClientRequestID = "stale"
	call("PUT", path+"/rich-content", write, 409)
	json.Unmarshal(saved, &doc)
	call("GET", path+"/revisions/"+doc.RevisionID, nil, 200)
	call("GET", path+"/rich-assets/not-a-hash", nil, 400)
	call("POST", "/studio/items", map[string]any{"kind": "rich-document", "name": "Unsupported", "body": map[string]any{"schemaVersion": 2}}, 400)
	call("POST", "/studio/items", map[string]any{"kind": "doc", "name": "MD", "body": "# original"}, 201)
}

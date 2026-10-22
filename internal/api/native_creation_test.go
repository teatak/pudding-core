package api

import (
	"bytes"
	"encoding/json"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/teatak/pudding-core/internal/store"
	"github.com/teatak/pudding-core/internal/store/storetest"
)

func TestNativeCreationRequestAPI(t *testing.T) {
	st := storetest.New(t)
	handler := New(nil, st, st, nil).WithHome(t.TempDir()).Handler("token", nil)
	post := func(payload map[string]any, status int) store.StudioItem {
		t.Helper()
		body, err := json.Marshal(payload)
		if err != nil {
			t.Fatal(err)
		}
		req := httptest.NewRequest("POST", "/studio/items", bytes.NewReader(body))
		req.Header.Set("Authorization", "Bearer token")
		reply := httptest.NewRecorder()
		handler.ServeHTTP(reply, req)
		if reply.Code != status {
			t.Fatalf("status %d, want %d: %s", reply.Code, status, reply.Body)
		}
		var item store.StudioItem
		if status == 201 {
			if err := json.Unmarshal(reply.Body.Bytes(), &item); err != nil {
				t.Fatal(err)
			}
		}
		return item
	}
	for _, kind := range []string{"page", "sheet"} {
		payload := map[string]any{"kind": kind, "name": "Imported", "clientRequestID": kind + "_request"}
		if kind == "page" {
			payload["body"] = "# Imported\n"
		} else {
			payload["body"] = map[string]any{"columns": []any{}, "rows": []any{}}
		}
		first := post(payload, 201)
		retry := post(payload, 201)
		if first.ID != retry.ID || first.HeadRevision != retry.HeadRevision {
			t.Fatalf("duplicated %s: %+v %+v", kind, first, retry)
		}
		payload["name"] = "Changed"
		post(payload, 400)
	}
	for _, key := range []string{"", "has spaces", "中文", strings.Repeat("x", 201)} {
		post(map[string]any{"kind": "page", "name": "Invalid key", "clientRequestID": key}, 400)
	}
	// Compatibility aliases normalize before fingerprinting.
	legacy := map[string]any{"kind": "note", "name": "Alias", "clientRequestID": "alias"}
	first := post(legacy, 201)
	legacy["kind"] = "page"
	if post(legacy, 201).ID != first.ID {
		t.Fatal("alias created a duplicate")
	}
}

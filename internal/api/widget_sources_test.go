package api

import (
	"bytes"
	"encoding/json"
	"github.com/teatak/pudding-core/internal/config"
	"github.com/teatak/pudding-core/internal/store/storetest"
	"net/http/httptest"
	"testing"
)

func TestWidgetSourcesRoutes(t *testing.T) {
	home := t.TempDir()
	st := storetest.New(t)
	cfg := config.NewManager(home)
	handler := New(nil, st, cfg, nil).WithHome(home).Handler("test-token", nil)
	call := func(method, path, body, token string, status int) []byte {
		t.Helper()
		r := httptest.NewRequest(method, path, bytes.NewBufferString(body))
		r.Header.Set("Authorization", "Bearer "+token)
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, r)
		if w.Code != status {
			t.Fatalf("%s %s: %d %s", method, path, w.Code, w.Body.String())
		}
		return w.Body.Bytes()
	}
	call("GET", "/studio/widget-sources", "", "wrong", 401)
	call("GET", "/studio/widget-sources", "", "test-token", 200)
	var source config.WidgetSource
	json.Unmarshal(call("POST", "/studio/widget-sources", `{"url":"https://third.test/registry.json"}`, "test-token", 200), &source)
	if source.ID == "" || source.Official {
		t.Fatal(source)
	}
	call("POST", "/studio/widget-sources", `{"url":"file:///tmp/x"}`, "test-token", 400)
	call("DELETE", "/studio/widget-sources/official", "", "test-token", 400)
	call("DELETE", "/studio/widget-sources/"+source.ID, "", "test-token", 200)
	var result struct {
		Sources []config.WidgetSource `json:"sources"`
	}
	json.Unmarshal(call("GET", "/studio/widget-sources", "", "test-token", 200), &result)
	if len(result.Sources) != 1 {
		t.Fatal(result)
	}
}

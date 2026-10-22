package api

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/teatak/pudding-core/internal/store"
	"github.com/teatak/pudding-core/internal/store/storetest"
	"github.com/teatak/pudding-core/internal/widget"
)

func TestWidgetCreationRecoveryAPI(t *testing.T) {
	st, home := storetest.New(t), t.TempDir()
	handler := New(nil, st, st, nil).WithHome(home).Handler("token", nil)
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
			if err = json.Unmarshal(reply.Body.Bytes(), &item); err != nil {
				t.Fatal(err)
			}
		}
		return item
	}
	files := map[string]string{"widget.json": `{"schemaVersion":1,"sdkVersion":"1","entry":"src/App.tsx","sources":{},"operations":{}}`, "src/App.tsx": "export default ()=> <p>Starter</p>"}
	payload := map[string]any{"kind": "widget", "name": "Starter", "clientRequestID": "starter-request", "initialFiles": files}
	// The DB identity survives a filesystem initialization failure. Recovery
	// publishes every file together and does not create a replacement item.
	blocked := filepath.Join(home, "studio")
	if err := os.WriteFile(blocked, []byte("block directory"), 0600); err != nil {
		t.Fatal(err)
	}
	post(payload, 500)
	items, err := st.ListStudioItems(context.Background(), store.StudioItemsActive)
	if err != nil || len(items) != 1 {
		t.Fatalf("creation identity: %+v %v", items, err)
	}
	id := items[0].ID
	if err = os.Remove(blocked); err != nil {
		t.Fatal(err)
	}
	var workers sync.WaitGroup
	for range 8 {
		workers.Add(1)
		go func() {
			defer workers.Done()
			if got := post(payload, 201); got.ID != id {
				t.Errorf("duplicate: %s", got.ID)
			}
		}()
	}
	workers.Wait()
	draft, err := widget.ReadDraft(home, id)
	if err != nil || len(draft.Files) != 2 || draft.Files["src/App.tsx"] != files["src/App.tsx"] {
		t.Fatalf("partial starter: %+v %v", draft, err)
	}
	later := "export default ()=> <p>Later edit</p>"
	draft, err = widget.WriteDraftFile(home, id, "src/App.tsx", &later, draft.DraftHash)
	if err != nil {
		t.Fatal(err)
	}
	// Replay even after creation and all HTTP replies succeeded: the caller
	// could still have lost its tool result. Later edits must remain intact.
	if post(payload, 201).ID != id {
		t.Fatal("lost tool result duplicates widget")
	}
	retained, err := widget.ReadDraft(home, id)
	if err != nil || retained.DraftHash != draft.DraftHash || retained.Files["src/App.tsx"] != later {
		t.Fatalf("template overwrote edit: %+v %v", retained, err)
	}
	// If the working directory needs rebuilding after later source was saved,
	// creation replay must use the current head rather than the initial template.
	hash, err := widget.WritePackage(home, id, widget.Package{Files: retained.Files})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = st.SaveStudioItemRevision(context.Background(), &store.StudioItemRevision{ItemID: id, Hash: hash, ClientRequestID: "later-commit"}, ""); err != nil {
		t.Fatal(err)
	}
	root, err := widget.DraftRoot(home, id)
	if err != nil {
		t.Fatal(err)
	}
	if err = os.RemoveAll(root); err != nil {
		t.Fatal(err)
	}
	if current := post(payload, 201); current.ID != id || current.HeadRevision != hash {
		t.Fatalf("creation replay lost current source: %+v", current)
	}
	retained, err = widget.ReadDraft(home, id)
	if err != nil || retained.BaseRevisionHash != hash || retained.Files["src/App.tsx"] != later {
		t.Fatalf("template replaced committed source: %+v %v", retained, err)
	}
	files["src/App.tsx"] = "Changed template"
	post(payload, 400)
	payload["clientRequestID"] = "second-intent"
	if post(payload, 201).ID == id {
		t.Fatal("explicit second creation reused original")
	}
	for _, input := range []map[string]any{
		{"kind": "widget", "name": "Invalid", "initialFiles": files},
		{"kind": "page", "name": "Invalid", "clientRequestID": "key", "initialFiles": files},
		{"kind": "widget", "name": "Invalid", "clientRequestID": "key", "initialFiles": map[string]string{"../bad.ts": ""}},
		{"kind": "widget", "name": "Invalid", "clientRequestID": "key", "initialFiles": map[string]string{"fixtures/data.json": "{}"}},
		{"kind": "widget", "name": "Invalid", "clientRequestID": "key", "initialFiles": map[string]string{"src/a.ts": "", "src/a.ts/b.ts": ""}},
		{"kind": "widget", "name": "Invalid", "clientRequestID": "key", "initialFiles": map[string]string{"src/App.tsx": strings.Repeat("x", 524289)}},
	} {
		post(input, 400)
	}
	for _, key := range []string{"", "has spaces", "中文", strings.Repeat("x", 201)} {
		post(map[string]any{"kind": "widget", "name": "Invalid", "clientRequestID": key}, 400)
	}
}

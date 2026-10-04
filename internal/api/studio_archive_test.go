package api

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/teatak/pudding-core/internal/store"
	"github.com/teatak/pudding-core/internal/store/storetest"
)

func TestStudioArchiveLifecycleAndRetention(t *testing.T) {
	ctx := context.Background()
	st := storetest.New(t)
	home := t.TempDir()
	server := New(nil, st, st, nil).WithHome(home)
	handler := server.Handler("fixture-token", nil)
	call := func(method, path string, input any, status int) []byte {
		t.Helper()
		body, _ := json.Marshal(input)
		req := httptest.NewRequest(method, path, bytes.NewReader(body))
		req.Header.Set("Authorization", "Bearer fixture-token")
		out := httptest.NewRecorder()
		handler.ServeHTTP(out, req)
		if out.Code != status {
			t.Fatalf("%s %s: %d %s, want %d", method, path, out.Code, out.Body.String(), status)
		}
		return out.Body.Bytes()
	}
	for _, kind := range []string{"doc", "table", "widget"} {
		t.Run(kind, func(t *testing.T) {
			var item store.StudioItem
			if err := json.Unmarshal(call("POST", "/studio/items", map[string]any{"kind": kind, "name": "Archive fixture"}, 201), &item); err != nil {
				t.Fatal(err)
			}
			route := "/studio/items/" + item.ID
			var content, widgetDraft, widgetRevision []byte
			if kind == "widget" {
				files := map[string]string{
					"widget.json": `{"schemaVersion":1,"sdkVersion":"1","entry":"src/App.tsx","sources":{},"operations":{}}`,
					"src/App.tsx": `export default () => <div>Retained widget</div>`,
				}
				if err := json.Unmarshal(saveWidgetTestFiles(t, call, route, "initial-widget", files), &item); err != nil {
					t.Fatal(err)
				}
				widgetRevision = call("GET", route+"/revisions/"+item.HeadRevision, nil, 200)
				hash := startWidgetTestDraft(t, call, route)
				writeWidgetTestDraft(t, call, route, hash, "src/App.tsx", `export default () => <div>Unsaved widget source</div>`)
				widgetDraft = call("GET", route+"/draft", nil, 200)
			} else {
				content = call("GET", route+"/content", nil, 200)
			}
			asset := filepath.Join(home, "studio", item.ID, "content", "assets", "fixture.txt")
			if err := os.MkdirAll(filepath.Dir(asset), 0700); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(asset, []byte("retained content"), 0600); err != nil {
				t.Fatal(err)
			}
			call("POST", route+"/archive", map[string]any{"expectedRevision": item.Revision + 1}, 409)
			archived := call("POST", route+"/archive", map[string]any{"expectedRevision": item.Revision}, 200)
			retry := call("POST", route+"/archive", map[string]any{"expectedRevision": item.Revision}, 200)
			if !bytes.Equal(archived, retry) {
				t.Fatal("archive retry changed retention or revision")
			}
			if err := json.Unmarshal(archived, &item); err != nil {
				t.Fatal(err)
			}
			if item.ArchivedAt == nil {
				t.Fatal("missing archive timestamp")
			}
			call("GET", route, nil, 404)
			if kind != "widget" {
				call("GET", route+"/content", nil, 404)
			}
			call("PATCH", route, map[string]any{"name": "hidden", "expectedRevision": item.Revision}, 404)
			active, err := st.ListStudioItems(ctx, store.StudioItemsActive)
			if err != nil || len(active) != 0 {
				t.Fatalf("active: %+v %v", active, err)
			}
			var listing struct {
				Items []store.StudioItem `json:"items"`
			}
			if err := json.Unmarshal(call("GET", "/studio/items?scope=archived", nil, 200), &listing); err != nil || len(listing.Items) != 1 {
				t.Fatalf("archives: %+v %v", listing, err)
			}
			if err := server.purgeExpiredStudioArchives(ctx, item.ArchivedAt.Add(29*24*time.Hour)); err != nil {
				t.Fatal(err)
			}
			if _, err := os.Stat(asset); err != nil {
				t.Fatalf("removed early: %v", err)
			}
			restored := call("POST", route+"/restore", map[string]any{"expectedRevision": item.Revision}, 200)
			if err := json.Unmarshal(restored, &item); err != nil {
				t.Fatal(err)
			}
			// Unmarshal into a fresh value since the optional timestamp is now absent.
			var activeItem store.StudioItem
			if err := json.Unmarshal(restored, &activeItem); err != nil || activeItem.ArchivedAt != nil {
				t.Fatalf("restored: %+v %v", activeItem, err)
			}
			if err := server.purgeExpiredStudioArchives(ctx, time.Now().Add(31*24*time.Hour)); err != nil {
				t.Fatal(err)
			}
			call("GET", route, nil, 200)
			if kind == "widget" {
				if !bytes.Equal(widgetDraft, call("GET", route+"/draft", nil, 200)) || !bytes.Equal(widgetRevision, call("GET", route+"/revisions/"+item.HeadRevision, nil, 200)) {
					t.Fatal("restore changed widget source or working draft")
				}
			}
			if kind != "widget" && !bytes.Equal(content, call("GET", route+"/content", nil, 200)) {
				t.Fatal("restore changed content or history head")
			}
			if err := json.Unmarshal(call("POST", route+"/archive", map[string]any{"expectedRevision": item.Revision}, 200), &item); err != nil {
				t.Fatal(err)
			}
			if err := server.purgeExpiredStudioArchives(ctx, item.ArchivedAt.Add(archiveRetention)); err != nil {
				t.Fatal(err)
			}
			if _, err := os.Stat(filepath.Join(home, "studio", item.ID)); !errors.Is(err, os.ErrNotExist) {
				t.Fatalf("files remain: %v", err)
			}
			call("POST", route+"/restore", map[string]any{"expectedRevision": item.Revision}, 404)
			cleanup, err := st.ListStudioItemsForCleanup(ctx, time.Now().Add(31*24*time.Hour))
			if err != nil || len(cleanup) != 0 {
				t.Fatalf("cleanup records remain: %+v %v", cleanup, err)
			}
		})
	}
	call("GET", "/studio/items?scope=invalid", nil, 400)
}

func TestStudioPurgeRetriesFileCleanupFailure(t *testing.T) {
	ctx := context.Background()
	st := storetest.New(t)
	home := t.TempDir()
	server := New(nil, st, st, nil).WithHome(home)
	item, err := st.CreateStudioItem(ctx, &store.StudioItem{ID: "widget-cleanup", Kind: "widget", Name: "Cleanup", CreatedAt: time.Now(), UpdatedAt: time.Now()})
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(home, "studio", item.ID)
	if err := os.MkdirAll(path, 0o700); err != nil {
		t.Fatal(err)
	}
	release := blockDirectoryRemoval(t, path)
	if err := server.purgeStudioItem(ctx, item.ID, item.Revision); err == nil {
		t.Fatal("expected filesystem failure")
	}
	if _, err := st.GetStudioItem(ctx, item.ID); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("tombstone accessible: %v", err)
	}
	if _, err := st.SetStudioItemArchived(ctx, item.ID, item.Revision, false); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("tombstone restorable: %v", err)
	}
	pending, err := st.ListStudioItemsForCleanup(ctx, time.Now())
	if err != nil || len(pending) != 1 || !pending[0].Deleted {
		t.Fatalf("lost retry: %+v %v", pending, err)
	}
	release()
	if err := server.purgeExpiredStudioArchives(ctx, time.Now()); err != nil {
		t.Fatal(err)
	}
	pending, err = st.ListStudioItemsForCleanup(ctx, time.Now())
	if err != nil || len(pending) != 0 {
		t.Fatalf("retry failed: %+v %v", pending, err)
	}
}

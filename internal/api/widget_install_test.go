package api

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/teatak/pudding-core/internal/store"
	"github.com/teatak/pudding-core/internal/store/storetest"
	"github.com/teatak/pudding-core/internal/widget"
)

func distributionFixture(t *testing.T, version, source string) map[string]any {
	t.Helper()
	files := map[string]string{"widget.json": `{"schemaVersion":1,"sdkVersion":"1","entry":"src/App.tsx","sources":{},"operations":{}}`, "src/App.tsx": source}
	hashes := map[string]string{}
	for name, value := range files {
		hashes[name] = fmt.Sprintf("%x", sha256.Sum256([]byte(value)))
	}
	return map[string]any{"kind": "pudding.widget.source-package", "schemaVersion": 2, "id": "test/hub/widgets/todo", "version": version, "title": map[string]string{"en": "Todo"}, "description": map[string]string{"en": "Shared list"}, "requires": map[string]any{"protocolVersion": 18, "sdkVersion": "1"}, "source": widget.Package{Files: files}, "fileHashes": hashes}
}
func TestWidgetInstallUpgradeProtectsEditsAndData(t *testing.T) {
	st := storetest.New(t)
	home := t.TempDir()
	handler := New(nil, st, st, nil).WithHome(home).Handler("fixture-token", nil)
	call := func(method, path string, input any, status int) []byte {
		t.Helper()
		body, _ := json.Marshal(input)
		req := httptest.NewRequest(method, path, bytes.NewReader(body))
		req.Header.Set("Authorization", "Bearer fixture-token")
		out := httptest.NewRecorder()
		handler.ServeHTTP(out, req)
		if out.Code != status {
			t.Fatalf("%s %s: %d %s want %d", method, path, out.Code, out.Body.String(), status)
		}
		return out.Body.Bytes()
	}
	request := func(pkg map[string]any, mode, key string, item *store.StudioItem) map[string]any {
		t.Helper()
		raw, _ := json.Marshal(pkg)
		body := map[string]any{"packageJSON": string(raw), "packageHash": fmt.Sprintf("%x", sha256.Sum256(raw)), "packageID": pkg["id"], "version": pkg["version"], "registryURL": "https://hub.test/widgets/registry.json", "clientRequestID": key, "mode": mode, "locale": "en"}
		if item != nil {
			body["itemID"] = item.ID
			body["expectedRevision"] = item.Revision
		}
		return body
	}
	install := func(body map[string]any) store.StudioItem {
		t.Helper()
		var w store.StudioItem
		if err := json.Unmarshal(call("POST", "/studio/items/install", body, 200), &w); err != nil {
			t.Fatal(err)
		}
		return w
	}
	pkg := distributionFixture(t, "1.0.0", "export default ()=> <p>One</p>")
	icon := "data:image/svg+xml;base64," + base64.StdEncoding.EncodeToString([]byte(`<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 24 24"><circle cx="12" cy="12" r="8"/></svg>`))
	pkg["icon"] = icon
	w := install(request(pkg, "install", "install-1", nil))
	if w.Origin == nil || w.HeadRevision != w.Origin.SourceHash || w.ActiveRevision != "" {
		t.Fatalf("initial %+v", w)
	}
	if w.Icon != icon {
		t.Fatal("installation lost artwork")
	}
	again := install(request(pkg, "install", "install-2", nil))
	if again.ID != w.ID {
		t.Fatal("duplicate install", again.ID, w.ID)
	}
	dirs, err := os.ReadDir(filepath.Join(home, "studio"))
	if err != nil || len(dirs) != 1 {
		t.Fatal("repeat install wrote orphan", dirs, err)
	}
	// Copies are created only by opening an editing draft, not by reinstallation.
	call("POST", "/studio/items/install", request(pkg, "copy", "copy-0001", nil), 400)
	// Use normal build/activation APIs, then store shared data.
	base := "/studio/items/" + w.ID
	call("POST", base+"/build-receipts", map[string]any{"revisionHash": w.HeadRevision, "sdkVersion": "1", "compilerVersion": "test", "dependencyHash": fmt.Sprintf("%064d", 1), "ok": true}, 200)
	json.Unmarshal(call("POST", base+"/activate", map[string]any{"revisionHash": w.HeadRevision, "expectedRevision": w.Revision}, 200), &w)
	call("PUT", base+"/data", map[string]any{"revisionHash": w.ActiveRevision, "expectedVersion": 0, "data": map[string]any{"tasks": []string{"Keep me"}}}, 200)
	// An artwork-only update must persist without changing running source.
	artworkUpdate := distributionFixture(t, "1.0.1", "export default ()=> <p>One</p>")
	w = install(request(artworkUpdate, "upgrade", "upgrade-artwork", &w))
	if w.Icon != "component" || w.HeadRevision != w.ActiveRevision {
		t.Fatal("artwork-only update failed", w)
	}
	if stored, err := st.GetStudioItem(context.Background(), w.ID); err != nil || stored.Icon != "component" {
		t.Fatal("icon not persisted", err)
	}
	next := distributionFixture(t, "2.0.0", "export default ()=> <p>Two</p>")
	next["icon"] = icon
	previousActive := w.ActiveRevision
	w = install(request(next, "upgrade", "upgrade-1", &w))
	if w.ActiveRevision != previousActive || w.HeadRevision == previousActive || w.Origin.Version != "2.0.0" {
		t.Fatal("upgrade activated or failed to stage", w)
	}
	data, _ := st.GetWidgetData(context.Background(), w.ID)
	if !bytes.Contains(data.Data, []byte("Keep me")) {
		t.Fatal("upgrade lost data", data)
	}
	var opened struct {
		DraftHash string           `json:"draftHash"`
		Widget    store.StudioItem `json:"widget"`
	}
	openRequest := map[string]any{"clientRequestID": "first-edit-0001", "copyName": "Todo copy"}
	json.Unmarshal(call("POST", base+"/draft", openRequest, 200), &opened)
	custom := opened.Widget
	if custom.ID == w.ID || !custom.Origin.Copy || custom.ActiveRevision != w.ActiveRevision || custom.Icon != icon {
		t.Fatal("not an independent copy", custom)
	}
	var retried struct {
		Widget store.StudioItem `json:"widget"`
	}
	json.Unmarshal(call("POST", base+"/draft", openRequest, 200), &retried)
	if retried.Widget.ID != custom.ID {
		t.Fatal("retry created another copy")
	}
	customBase := "/studio/items/" + custom.ID
	call("PUT", base+"/draft/file", map[string]any{"path": "src/App.tsx", "content": "bad", "expectedDraftHash": opened.DraftHash}, 409)
	call("POST", base+"/draft/commit", map[string]any{"expectedDraftHash": opened.DraftHash, "clientRequestID": "bad-edit-original"}, 409)
	var customDraft struct {
		DraftHash string `json:"draftHash"`
	}
	json.Unmarshal(call("PUT", customBase+"/draft/file", map[string]any{"path": "src/App.tsx", "content": "export default ()=> <p>Custom</p>", "expectedDraftHash": opened.DraftHash}, 200), &customDraft)
	json.Unmarshal(call("POST", customBase+"/draft/commit", map[string]any{"expectedDraftHash": customDraft.DraftHash, "clientRequestID": "custom-edit"}, 200), &custom)
	var reopened struct {
		Widget store.StudioItem `json:"widget"`
	}
	json.Unmarshal(call("POST", customBase+"/draft", map[string]any{}, 200), &reopened)
	if reopened.Widget.ID != custom.ID {
		t.Fatal("copy was copied again")
	}
	customData, _ := st.GetWidgetData(context.Background(), custom.ID)
	if !bytes.Contains(customData.Data, []byte("Keep me")) {
		t.Fatal("copy did not snapshot data", customData)
	}
	call("PUT", customBase+"/data", map[string]any{"revisionHash": custom.ActiveRevision, "expectedVersion": customData.Version, "data": map[string]any{"tasks": []string{"Custom only"}}}, 200)
	data, _ = st.GetWidgetData(context.Background(), w.ID)
	if !bytes.Contains(data.Data, []byte("Keep me")) {
		t.Fatal("copy changed original data")
	}
	third := distributionFixture(t, "3.0.0", "export default ()=> <p>Three</p>")
	call("POST", "/studio/items/install", request(third, "upgrade", "copy-upgrade", &custom), 409)
	w = install(request(third, "upgrade", "upgrade-original", &w))
	originalDraft, err := widget.ReadDraft(home, w.ID)
	if !os.IsNotExist(err) {
		t.Fatal("original acquired an editing draft", originalDraft, err)
	}
	bad := request(pkg, "install", "invalid-1", nil)
	bad["packageHash"] = fmt.Sprintf("%064d", 0)
	call("POST", "/studio/items/install", bad, 400)
	bad = request(pkg, "install", "invalid-2", nil)
	bad["packageID"] = "test/hub/widgets/other"
	call("POST", "/studio/items/install", bad, 400)
	bad = request(pkg, "install", "invalid-3", nil)
	bad["registryURL"] = "file:///tmp/catalog.json"
	call("POST", "/studio/items/install", bad, 400)
	call("POST", base+"/archive", map[string]any{"expectedRevision": w.Revision}, 200)
	call("POST", "/studio/items/install", request(pkg, "install", "install-3", nil), 409)
}

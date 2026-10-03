package api

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/teatak/pudding-core/internal/plugin"
	"github.com/teatak/pudding-core/internal/pluginexec"
	"github.com/teatak/pudding-core/internal/store"
	"github.com/teatak/pudding-core/internal/store/storetest"
	"github.com/teatak/pudding-core/internal/widget"
)

type widgetFixturePlugins struct {
	pluginService
	identity string
	writes   bool
	multiple bool
}

func (a *widgetFixturePlugins) ResolveBoundEndpoint(_ context.Context, pluginID, endpoint, connection string) (*plugin.EndpointBinding, string, error) {
	if pluginID != "fixture" || endpoint != "rest" || (connection != "account-1" && !(a.multiple && connection == "account-2")) {
		return nil, "", fmt.Errorf("wrong connection")
	}
	return &plugin.EndpointBinding{PluginID: pluginID, EndpointName: endpoint, ConnectionID: connection, Endpoint: plugin.Endpoint{WidgetWrites: a.writes, Kind: "rest", URL: "https://fixture.test/api"}, Auth: plugin.Auth{Type: plugin.AuthTypeBearer, Token: "fixture-secret"}}, a.identity, nil
}

func (a *widgetFixturePlugins) ListEndpointBindings(_ context.Context, kind string) ([]*plugin.EndpointBinding, error) {
	if kind != "" && kind != "rest" {
		return nil, nil
	}
	bindings := []*plugin.EndpointBinding{{PluginID: "fixture", EndpointName: "rest", ConnectionID: "account-1"}}
	if a.multiple {
		bindings = append(bindings, &plugin.EndpointBinding{PluginID: "fixture", EndpointName: "rest", ConnectionID: "account-2"})
	}
	return bindings, nil
}

type widgetRoundTrip func(*http.Request) (*http.Response, error)

func (f widgetRoundTrip) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

type widgetTestCall func(string, string, any, int) []byte

func TestStudioItemAppearanceCanBeCreatedAndChanged(t *testing.T) {
	st := storetest.New(t)
	handler := New(nil, st, st, nil).WithHome(t.TempDir()).Handler("fixture-token", nil)
	call := func(method, path string, input any, status int) []byte {
		t.Helper()
		body, _ := json.Marshal(input)
		r := httptest.NewRequest(method, path, bytes.NewReader(body))
		r.Header.Set("Authorization", "Bearer fixture-token")
		out := httptest.NewRecorder()
		handler.ServeHTTP(out, r)
		if out.Code != status {
			t.Fatalf("%s %s: %d %s want %d", method, path, out.Code, out.Body.String(), status)
		}
		return out.Body.Bytes()
	}
	call("POST", "/studio/items", map[string]any{"kind": "widget", "name": "Invalid", "icon": "Bad/Icon"}, 400)
	var resource store.StudioItem
	if err := json.Unmarshal(call("POST", "/studio/items", map[string]any{"kind": "widget", "name": "Weather", "icon": "cloud-sun", "iconColor": "blue"}, 201), &resource); err != nil {
		t.Fatal(err)
	}
	if resource.Icon != "cloud-sun" || resource.IconColor != "blue" {
		t.Fatalf("create appearance: %+v", resource)
	}
	path := "/studio/items/" + resource.ID
	call("PATCH", path+"/appearance", map[string]any{"expectedRevision": resource.Revision, "icon": "cloud-sun", "iconColor": "invalid"}, 400)
	call("PATCH", path+"/appearance", map[string]any{"expectedRevision": resource.Revision - 1, "icon": "chart-column", "iconColor": "teal"}, 409)
	if err := json.Unmarshal(call("PATCH", path+"/appearance", map[string]any{"expectedRevision": resource.Revision, "icon": "chart-column", "iconColor": "teal"}, 200), &resource); err != nil {
		t.Fatal(err)
	}
	if resource.Icon != "chart-column" || resource.IconColor != "teal" {
		t.Fatalf("patched appearance: %+v", resource)
	}
	var listed struct {
		Items []store.StudioItem `json:"items"`
	}
	if err := json.Unmarshal(call("GET", "/studio/items", nil, 200), &listed); err != nil {
		t.Fatal(err)
	}
	if len(listed.Items) != 1 || listed.Items[0].Icon != resource.Icon || listed.Items[0].IconColor != resource.IconColor {
		t.Fatalf("listed appearance: %+v", listed.Items)
	}
	// Changing the appearance is not new work: an item created later stays first.
	time.Sleep(2 * time.Millisecond)
	var later store.StudioItem
	if err := json.Unmarshal(call("POST", "/studio/items", map[string]any{"kind": "widget", "name": "Later"}, 201), &later); err != nil {
		t.Fatal(err)
	}
	call("PATCH", path+"/appearance", map[string]any{"expectedRevision": resource.Revision, "icon": "cloud-sun", "iconColor": "blue"}, 200)
	if err := json.Unmarshal(call("GET", "/studio/items", nil, 200), &listed); err != nil {
		t.Fatal(err)
	}
	if len(listed.Items) != 2 || listed.Items[0].ID != later.ID || !listed.Items[1].UpdatedAt.Equal(resource.UpdatedAt) {
		t.Fatalf("appearance change reordered the list: %+v", listed.Items)
	}
}

func startWidgetTestDraft(t *testing.T, call widgetTestCall, base string) string {
	t.Helper()
	var draft struct {
		DraftHash string `json:"draftHash"`
	}
	if err := json.Unmarshal(call("POST", base+"/draft", map[string]any{}, 200), &draft); err != nil {
		t.Fatal(err)
	}
	return draft.DraftHash
}

func writeWidgetTestDraft(t *testing.T, call widgetTestCall, base, hash, path string, content any) string {
	t.Helper()
	var draft struct {
		DraftHash string `json:"draftHash"`
	}
	if err := json.Unmarshal(call("PUT", base+"/draft/file", map[string]any{"expectedDraftHash": hash, "path": path, "content": content}, 200), &draft); err != nil {
		t.Fatal(err)
	}
	return draft.DraftHash
}

func saveWidgetTestFiles(t *testing.T, call widgetTestCall, base, requestID string, files map[string]string) []byte {
	t.Helper()
	hash := startWidgetTestDraft(t, call, base)
	for path, content := range files {
		hash = writeWidgetTestDraft(t, call, base, hash, path, content)
	}
	return call("POST", base+"/draft/commit", map[string]any{"expectedDraftHash": hash, "clientRequestID": requestID}, 200)
}

func TestWidgetRevisionChangesKeepUnchangedFilesAndReportConflicts(t *testing.T) {
	st := storetest.New(t)
	home := t.TempDir()
	handler := New(nil, st, st, nil).WithHome(home).Handler("fixture-token", nil)
	call := func(method, path string, input any, status int) []byte {
		t.Helper()
		body, _ := json.Marshal(input)
		r := httptest.NewRequest(method, path, bytes.NewReader(body))
		r.Header.Set("Authorization", "Bearer fixture-token")
		out := httptest.NewRecorder()
		handler.ServeHTTP(out, r)
		if out.Code != status {
			t.Fatalf("%s %s: %d %s want %d", method, path, out.Code, out.Body.String(), status)
		}
		return out.Body.Bytes()
	}
	var w store.StudioItem
	_ = json.Unmarshal(call("POST", "/studio/items", map[string]any{"kind": "widget", "name": "Report"}, 201), &w)
	base := "/studio/items/" + w.ID
	files := map[string]string{
		"widget.json":   `{"schemaVersion":1,"sdkVersion":"1","entry":"src/App.tsx","sources":{},"operations":{}}`,
		"src/App.tsx":   "export default function App(){return <p>Before</p>}",
		"src/unused.ts": "export const unused = true",
	}
	draftHash := startWidgetTestDraft(t, call, base)
	draftHash = writeWidgetTestDraft(t, call, base, draftHash, "widget.json", files["widget.json"])
	call("POST", base+"/draft/commit", map[string]any{"expectedDraftHash": draftHash, "clientRequestID": "incomplete"}, 400)
	draftHash = writeWidgetTestDraft(t, call, base, draftHash, "src/App.tsx", files["src/App.tsx"])
	draftHash = writeWidgetTestDraft(t, call, base, draftHash, "src/unused.ts", files["src/unused.ts"])
	_ = json.Unmarshal(call("POST", base+"/draft/commit", map[string]any{"expectedDraftHash": draftHash, "clientRequestID": "initial"}, 200), &w)
	first := w.HeadRevision
	// Metadata changes the resource revision without changing source content.
	w.Name = "Renamed report"
	updatedItem, err := st.UpdateStudioItem(context.Background(), &w, w.Revision)
	if err != nil {
		t.Fatal(err)
	}
	w = *updatedItem
	updated := "export default function App(){return <p>After</p>}"
	draftHash = writeWidgetTestDraft(t, call, base, draftHash, "src/App.tsx", updated)
	var draftConflict struct {
		Error            string `json:"error"`
		CurrentDraftHash string `json:"currentDraftHash"`
	}
	_ = json.Unmarshal(call("PUT", base+"/draft/file", map[string]any{"expectedDraftHash": first, "path": "src/unused.ts", "content": nil}, 409), &draftConflict)
	if draftConflict.Error != "draft_conflict" || draftConflict.CurrentDraftHash != draftHash {
		t.Fatalf("missing current draft hash: %+v", draftConflict)
	}
	draftHash = writeWidgetTestDraft(t, call, base, draftHash, "src/unused.ts", nil)
	_ = json.Unmarshal(call("POST", base+"/draft/commit", map[string]any{"expectedDraftHash": draftHash, "clientRequestID": "edit"}, 200), &w)
	if w.HeadRevision == first {
		t.Fatal("edited source did not create a new revision")
	}
	var source struct {
		Package widget.Package `json:"package"`
	}
	_ = json.Unmarshal(call("GET", base+"/revisions/"+w.HeadRevision, nil, 200), &source)
	if source.Package.Files["src/App.tsx"] != updated || source.Package.Files["widget.json"] != files["widget.json"] {
		t.Fatal("unchanged files were not retained")
	}
	if _, exists := source.Package.Files["src/unused.ts"]; exists {
		t.Fatal("deleted file remained in source")
	}
	_ = json.Unmarshal(call("GET", base+"/revisions/"+first, nil, 200), &source)
	if source.Package.Files["src/unused.ts"] == "" {
		t.Fatal("immutable base revision was modified")
	}
	// A repeated commit is a no-op even though its base is now the new head.
	call("POST", base+"/draft/commit", map[string]any{"expectedDraftHash": draftHash, "clientRequestID": "edit"}, 200)
	// Simulate a second writer publishing a new source while this draft is open.
	other := widget.Package{Files: map[string]string{"widget.json": files["widget.json"], "src/App.tsx": "export default function App(){return <p>Other</p>}"}}
	otherHash, err := widget.WritePackage(home, w.ID, other)
	if err != nil {
		t.Fatal(err)
	}
	otherItem, err := st.SaveStudioItemRevision(context.Background(), &store.StudioItemRevision{ItemID: w.ID, Hash: otherHash, ClientRequestID: "other", CreatedAt: time.Now().UTC()}, w.HeadRevision)
	if err != nil {
		t.Fatal(err)
	}
	w = *otherItem
	draftHash = writeWidgetTestDraft(t, call, base, draftHash, "src/App.tsx", "export default function App(){return <p>Mine</p>}")
	var conflict struct {
		Error               string `json:"error"`
		CurrentRevision     int64  `json:"currentRevision"`
		CurrentHeadRevision string `json:"currentHeadRevision"`
	}
	_ = json.Unmarshal(call("POST", base+"/draft/commit", map[string]any{"expectedDraftHash": draftHash, "clientRequestID": "stale"}, 409), &conflict)
	if conflict.Error != "revision_conflict" || conflict.CurrentRevision != w.Revision || conflict.CurrentHeadRevision != w.HeadRevision {
		t.Fatalf("missing current revision in conflict: %+v", conflict)
	}
	draftHash = writeWidgetTestDraft(t, call, base, draftHash, "widget.json", nil)
	call("POST", base+"/draft/commit", map[string]any{"expectedDraftHash": draftHash, "clientRequestID": "invalid"}, 400)
}

func TestWidgetQueryUsesAuthorizedPluginConnection(t *testing.T) {
	st := storetest.New(t)
	plugins := &widgetFixturePlugins{identity: "authorization-1"}
	calls := 0
	client := &http.Client{Transport: widgetRoundTrip(func(r *http.Request) (*http.Response, error) {
		calls++
		if r.URL.String() != "https://fixture.test/api/items?status=open" || r.Header.Get("Authorization") != "Bearer fixture-secret" {
			t.Fatalf("wrong request: %s %+v", r.URL, r.Header)
		}
		return &http.Response{StatusCode: 200, Header: http.Header{"Content-Type": {"application/json"}}, Body: io.NopCloser(strings.NewReader(`{"items":[{"id":"1"}],"total":1}`))}, nil
	})}
	handler := New(nil, st, st, nil).WithHome(t.TempDir()).WithPlugins(plugins).WithPluginExecutor(pluginexec.New(client)).Handler("fixture-token", nil)
	call := func(method, path string, input any, status int) []byte {
		t.Helper()
		b, err := json.Marshal(input)
		if err != nil {
			t.Fatal(err)
		}
		r := httptest.NewRequest(method, path, bytes.NewReader(b))
		r.Header.Set("Authorization", "Bearer fixture-token")
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, r)
		if w.Code != status {
			t.Fatalf("%s %s: %d %s (want %d)", method, path, w.Code, w.Body.String(), status)
		}
		return w.Body.Bytes()
	}
	unauthorized := httptest.NewRecorder()
	handler.ServeHTTP(unauthorized, httptest.NewRequest("GET", "/studio/items", nil))
	if unauthorized.Code != 401 {
		t.Fatal("missing token accepted")
	}
	var w store.StudioItem
	if err := json.Unmarshal(call("POST", "/studio/items", map[string]any{"kind": "widget", "name": "Fixture"}, 201), &w); err != nil {
		t.Fatal(err)
	}
	base := "/studio/items/" + w.ID
	pkg := widget.Package{Files: map[string]string{
		"src/App.tsx": "export default ()=> <p>Fixture</p>",
		"widget.json": `{"schemaVersion":1,"sdkVersion":"1","entry":"src/App.tsx","sources":{"primary":{"pluginID":"fixture","endpoint":"rest"}},"operations":{"items":{"source":"primary","kind":"rest","effectHint":"read","inputSchema":{"type":"object","properties":{"status":{"type":"string","enum":["open","closed"]}},"required":["status"],"additionalProperties":false},"request":{"method":"GET","path":"/items","query":{"status":{"$input":"/status"}}},"result":{"rows":"/items","total":"/total"}}}}`,
	}}
	if err := json.Unmarshal(saveWidgetTestFiles(t, call, base, "save-1", pkg.Files), &w); err != nil {
		t.Fatal(err)
	}
	hash := w.HeadRevision
	call("POST", base+"/activate", map[string]any{"expectedRevision": w.Revision, "revisionHash": hash}, 409)
	query := map[string]any{"revisionHash": hash, "bindingVersion": w.BindingVersion, "params": map[string]any{"status": "open"}}
	body := call("POST", base+"/queries/items", query, 200)
	if calls != 1 || bytes.Contains(body, []byte("fixture-secret")) {
		t.Fatalf("wrong calls or exposed credential: %d %s", calls, body)
	}
	query["params"] = map[string]any{"status": "all"}
	call("POST", base+"/queries/items", query, 400)
	if calls != 1 {
		t.Fatal("invalid input reached App")
	}
	query["params"] = map[string]any{"status": "open"}
	plugins.identity = "authorization-2"
	call("POST", base+"/queries/items", query, 200)
	if calls != 2 {
		t.Fatal("authorized App connection was not reused")
	}
	plugins.multiple = true
	call("POST", base+"/queries/items", query, 409)
	if calls != 2 {
		t.Fatal("ambiguous App connection reached upstream")
	}
	if err := json.Unmarshal(call("PUT", base+"/bindings", map[string]any{"expectedRevision": w.Revision, "revisionHash": hash, "bindings": map[string]string{"primary": "account-1"}}, 200), &w); err != nil {
		t.Fatal(err)
	}
	query["bindingVersion"] = w.BindingVersion
	call("POST", base+"/queries/items", query, 200)
	if calls != 3 {
		t.Fatal("selected App account was not used")
	}
}

func TestWidgetActionsAreConfirmedOnceAndInvalidateOnChange(t *testing.T) {
	st := storetest.New(t)
	plugins := &widgetFixturePlugins{identity: "authorization-1", writes: true}
	calls := 0
	failTransport := false
	client := &http.Client{Transport: widgetRoundTrip(func(r *http.Request) (*http.Response, error) {
		calls++
		if failTransport {
			return nil, fmt.Errorf("connection interrupted after dispatch")
		}
		if r.Method != "POST" {
			t.Fatal(r.Method)
		}
		return &http.Response{StatusCode: 200, Header: http.Header{"Content-Type": {"application/json"}}, Body: io.NopCloser(strings.NewReader(`{"success":true}`))}, nil
	})}
	handler := New(nil, st, st, nil).WithHome(t.TempDir()).WithPlugins(plugins).WithPluginExecutor(pluginexec.New(client)).Handler("test", nil)
	call := func(method, path string, input any, status int) []byte {
		t.Helper()
		body, _ := json.Marshal(input)
		r := httptest.NewRequest(method, path, bytes.NewReader(body))
		r.Header.Set("Authorization", "Bearer test")
		out := httptest.NewRecorder()
		handler.ServeHTTP(out, r)
		if out.Code != status {
			t.Fatalf("%s %s: %d %s want %d", method, path, out.Code, out.Body.String(), status)
		}
		return out.Body.Bytes()
	}
	var w store.StudioItem
	_ = json.Unmarshal(call("POST", "/studio/items", map[string]any{"kind": "widget", "name": "Actions"}, 201), &w)
	base := "/studio/items/" + w.ID
	pkg := widget.Package{Files: map[string]string{"src/App.tsx": "export default ()=>null", "widget.json": `{"schemaVersion":1,"sdkVersion":"1","entry":"src/App.tsx","sources":{"primary":{"pluginID":"fixture","endpoint":"rest"}},"operations":{"save":{"source":"primary","kind":"rest","effectHint":"write","inputSchema":{"type":"object","properties":{"id":{"type":"string","maxLength":50}},"required":["id"],"additionalProperties":false},"request":{"method":"POST","path":"/items","body":{"id":{"$input":"/id"}}},"result":{"success":{"pointer":"/success","equals":true}}}}}`}}
	_ = json.Unmarshal(saveWidgetTestFiles(t, call, base, "save", pkg.Files), &w)
	call("POST", base+"/build-receipts", map[string]any{"revisionHash": w.HeadRevision, "sdkVersion": "1", "compilerVersion": "fixture", "dependencyHash": strings.Repeat("b", 64), "ok": true}, 200)
	_ = json.Unmarshal(call("PUT", base+"/bindings", map[string]any{"expectedRevision": w.Revision, "revisionHash": w.HeadRevision, "bindings": map[string]string{"primary": "account-1"}}, 200), &w)
	_ = json.Unmarshal(call("POST", base+"/activate", map[string]any{"expectedRevision": w.Revision, "revisionHash": w.HeadRevision}, 200), &w)
	call("POST", base+"/queries/save", map[string]any{"revisionHash": w.HeadRevision, "bindingVersion": w.BindingVersion, "params": map[string]any{"id": "one"}}, 403)
	if calls != 0 {
		t.Fatal("write operation ran through automatic query")
	}
	prepare := func(key string, status int) store.WidgetAction {
		t.Helper()
		var a store.WidgetAction
		_ = json.Unmarshal(call("POST", base+"/actions/save/prepare", map[string]any{"revisionHash": w.HeadRevision, "bindingVersion": w.BindingVersion, "clientRequestID": key, "params": map[string]any{"id": "one"}}, status), &a)
		return a
	}
	a := prepare("one", 200)
	if calls != 0 {
		t.Fatal("prepare wrote upstream")
	}
	if prepare("one", 200).ID != a.ID {
		t.Fatal("prepare not idempotent")
	}
	path := base + "/action-runs/" + a.ID + "/execute"
	call("POST", path, map[string]any{"confirm": false}, 400)
	if calls != 0 {
		t.Fatal("unconfirmed action dispatched")
	}
	var done store.WidgetAction
	_ = json.Unmarshal(call("POST", path, map[string]any{"confirm": true}, 200), &done)
	if done.State != "succeeded" || calls != 1 {
		t.Fatalf("result %+v calls %d", done, calls)
	}
	call("POST", path, map[string]any{"confirm": true}, 200)
	if calls != 1 {
		t.Fatal("replayed write dispatched")
	}
	a = prepare("two", 200)
	plugins.identity = "authorization-2"
	call("POST", base+"/action-runs/"+a.ID+"/execute", map[string]any{"confirm": true}, 409)
	if calls != 1 {
		t.Fatal("changed connection dispatched")
	}
	a = prepare("three", 200)
	failTransport = true
	path = base + "/action-runs/" + a.ID + "/execute"
	_ = json.Unmarshal(call("POST", path, map[string]any{"confirm": true}, 200), &done)
	if done.State != "unknown" {
		t.Fatal(done.State)
	}
	call("POST", path, map[string]any{"confirm": true}, 200)
	if calls != 2 {
		t.Fatal("unknown write retried")
	}
	plugins.writes = false
	prepare("four", 403)
	if calls != 2 {
		t.Fatal("App without write capability dispatched")
	}
}

func TestWidgetResponseFailuresAndBudget(t *testing.T) {
	rest := widget.Operation{Kind: "rest", Result: widget.Result{Rows: "/items", Total: "/total", Success: &widget.Condition{Pointer: "/success", Equals: true}}}
	for _, response := range []map[string]any{
		{"ok": false, "reason": "timeout"},
		{"ok": true, "status": 500, "body_json": map[string]any{}},
		{"ok": true, "status": 200, "body_truncated": true},
		{"ok": true, "status": 200, "body_json": map[string]any{"items": []any{}, "total": float64(42), "success": false}},
		{"ok": true, "status": 200, "body_json": map[string]any{"items": []any{}, "success": true}},
	} {
		if _, err := widgetResponseData(rest, response); err == nil {
			t.Fatalf("false success for %+v", response)
		}
	}
	graph := widget.Operation{Kind: "graphql"}
	if _, err := widgetResponseData(graph, map[string]any{"ok": true, "status": 200, "data": map[string]any{}, "errors": []any{map[string]any{"message": "partial failure"}}}); err == nil {
		t.Fatal("GraphQL errors accepted")
	}
	response := map[string]any{"ok": true, "status": 200, "body_json": map[string]any{"items": []any{map[string]any{"id": "one"}}, "total": float64(42), "success": true}}
	data, err := widgetResponseData(rest, response)
	if err != nil || data.(map[string]any)["total"] != float64(42) {
		t.Fatalf("source total changed %+v %v", data, err)
	}
	s := &Server{}
	var releases []func()
	for i := 0; i < 4; i++ {
		release, ok := s.acquireWidgetRequest("one")
		if !ok {
			t.Fatal("early concurrency limit")
		}
		releases = append(releases, release)
	}
	if _, ok := s.acquireWidgetRequest("one"); ok {
		t.Fatal("unbounded concurrent requests")
	}
	for _, release := range releases {
		release()
	}
	for i := 4; i < 120; i++ {
		release, ok := s.acquireWidgetRequest("one")
		if !ok {
			t.Fatal("early rate limit")
		}
		release()
	}
	if _, ok := s.acquireWidgetRequest("one"); ok {
		t.Fatal("unbounded request rate")
	}
}

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

	"github.com/teatak/pudding-core/internal/app"
	"github.com/teatak/pudding-core/internal/appexec"
	"github.com/teatak/pudding-core/internal/store"
	"github.com/teatak/pudding-core/internal/store/memstore"
	"github.com/teatak/pudding-core/internal/workbench"
)

type workbenchFixtureApps struct {
	appService
	identity string
	writes   bool
	multiple bool
}

func (a *workbenchFixtureApps) ResolveBoundEndpoint(_ context.Context, appID, endpoint, connection string) (*app.EndpointBinding, string, error) {
	if appID != "fixture" || endpoint != "rest" || (connection != "account-1" && !(a.multiple && connection == "account-2")) {
		return nil, "", fmt.Errorf("wrong connection")
	}
	return &app.EndpointBinding{AppID: appID, EndpointName: endpoint, ConnectionID: connection, Endpoint: app.Endpoint{WorkbenchWrites: a.writes, Kind: "rest", URL: "https://fixture.test/api"}, Auth: app.Auth{Type: app.AuthTypeBearer, Token: "fixture-secret"}}, a.identity, nil
}

func (a *workbenchFixtureApps) ListEndpointBindings(_ context.Context, kind string) ([]*app.EndpointBinding, error) {
	if kind != "" && kind != "rest" {
		return nil, nil
	}
	bindings := []*app.EndpointBinding{{AppID: "fixture", EndpointName: "rest", ConnectionID: "account-1"}}
	if a.multiple {
		bindings = append(bindings, &app.EndpointBinding{AppID: "fixture", EndpointName: "rest", ConnectionID: "account-2"})
	}
	return bindings, nil
}

type workbenchRoundTrip func(*http.Request) (*http.Response, error)

func (f workbenchRoundTrip) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

type canvasTestCall func(string, string, any, int) []byte

func startCanvasTestDraft(t *testing.T, call canvasTestCall, base string) string {
	t.Helper()
	var draft struct {
		DraftHash string `json:"draftHash"`
	}
	if err := json.Unmarshal(call("POST", base+"/draft", map[string]any{}, 200), &draft); err != nil {
		t.Fatal(err)
	}
	return draft.DraftHash
}

func writeCanvasTestDraft(t *testing.T, call canvasTestCall, base, hash, path string, content any) string {
	t.Helper()
	var draft struct {
		DraftHash string `json:"draftHash"`
	}
	if err := json.Unmarshal(call("PUT", base+"/draft/file", map[string]any{"expectedDraftHash": hash, "path": path, "content": content}, 200), &draft); err != nil {
		t.Fatal(err)
	}
	return draft.DraftHash
}

func saveCanvasTestFiles(t *testing.T, call canvasTestCall, base, requestID string, files map[string]string) []byte {
	t.Helper()
	hash := startCanvasTestDraft(t, call, base)
	for path, content := range files {
		hash = writeCanvasTestDraft(t, call, base, hash, path, content)
	}
	return call("POST", base+"/draft/commit", map[string]any{"expectedDraftHash": hash, "clientRequestID": requestID}, 200)
}

func TestWorkbenchRevisionChangesKeepUnchangedFilesAndReportConflicts(t *testing.T) {
	st := memstore.New()
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
	var w store.Workbench
	_ = json.Unmarshal(call("POST", "/canvases", map[string]any{"name": "Report"}, 201), &w)
	base := "/canvases/" + w.ID
	files := map[string]string{
		"workbench.json": `{"schemaVersion":1,"sdkVersion":"1","entry":"src/App.tsx","sources":{},"operations":{}}`,
		"src/App.tsx":    "export default function App(){return <p>Before</p>}",
		"src/unused.ts":  "export const unused = true",
	}
	draftHash := startCanvasTestDraft(t, call, base)
	draftHash = writeCanvasTestDraft(t, call, base, draftHash, "workbench.json", files["workbench.json"])
	call("POST", base+"/draft/commit", map[string]any{"expectedDraftHash": draftHash, "clientRequestID": "incomplete"}, 400)
	draftHash = writeCanvasTestDraft(t, call, base, draftHash, "src/App.tsx", files["src/App.tsx"])
	draftHash = writeCanvasTestDraft(t, call, base, draftHash, "src/unused.ts", files["src/unused.ts"])
	_ = json.Unmarshal(call("POST", base+"/draft/commit", map[string]any{"expectedDraftHash": draftHash, "clientRequestID": "initial"}, 200), &w)
	first := w.HeadRevision
	// Metadata changes the resource revision without changing source content.
	w.Name = "Renamed report"
	updatedWorkbench, err := st.UpdateWorkbench(context.Background(), &w, w.Revision)
	if err != nil {
		t.Fatal(err)
	}
	w = *updatedWorkbench
	updated := "export default function App(){return <p>After</p>}"
	draftHash = writeCanvasTestDraft(t, call, base, draftHash, "src/App.tsx", updated)
	var draftConflict struct {
		Error            string `json:"error"`
		CurrentDraftHash string `json:"currentDraftHash"`
	}
	_ = json.Unmarshal(call("PUT", base+"/draft/file", map[string]any{"expectedDraftHash": first, "path": "src/unused.ts", "content": nil}, 409), &draftConflict)
	if draftConflict.Error != "draft_conflict" || draftConflict.CurrentDraftHash != draftHash {
		t.Fatalf("missing current draft hash: %+v", draftConflict)
	}
	draftHash = writeCanvasTestDraft(t, call, base, draftHash, "src/unused.ts", nil)
	_ = json.Unmarshal(call("POST", base+"/draft/commit", map[string]any{"expectedDraftHash": draftHash, "clientRequestID": "edit"}, 200), &w)
	if w.HeadRevision == first {
		t.Fatal("edited source did not create a new revision")
	}
	var source struct {
		Package workbench.Package `json:"package"`
	}
	_ = json.Unmarshal(call("GET", base+"/revisions/"+w.HeadRevision, nil, 200), &source)
	if source.Package.Files["src/App.tsx"] != updated || source.Package.Files["workbench.json"] != files["workbench.json"] {
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
	other := workbench.Package{Files: map[string]string{"workbench.json": files["workbench.json"], "src/App.tsx": "export default function App(){return <p>Other</p>}"}}
	otherHash, err := workbench.WritePackage(home, w.ID, other)
	if err != nil {
		t.Fatal(err)
	}
	otherWorkbench, err := st.SaveWorkbenchRevision(context.Background(), &store.WorkbenchRevision{WorkbenchID: w.ID, Hash: otherHash, ClientRequestID: "other", CreatedAt: time.Now().UTC()}, w.HeadRevision)
	if err != nil {
		t.Fatal(err)
	}
	w = *otherWorkbench
	draftHash = writeCanvasTestDraft(t, call, base, draftHash, "src/App.tsx", "export default function App(){return <p>Mine</p>}")
	var conflict struct {
		Error               string `json:"error"`
		CurrentRevision     int64  `json:"currentRevision"`
		CurrentHeadRevision string `json:"currentHeadRevision"`
	}
	_ = json.Unmarshal(call("POST", base+"/draft/commit", map[string]any{"expectedDraftHash": draftHash, "clientRequestID": "stale"}, 409), &conflict)
	if conflict.Error != "revision_conflict" || conflict.CurrentRevision != w.Revision || conflict.CurrentHeadRevision != w.HeadRevision {
		t.Fatalf("missing current revision in conflict: %+v", conflict)
	}
	draftHash = writeCanvasTestDraft(t, call, base, draftHash, "workbench.json", nil)
	call("POST", base+"/draft/commit", map[string]any{"expectedDraftHash": draftHash, "clientRequestID": "invalid"}, 400)
}

func TestWorkbenchQueryUsesAuthorizedAppConnection(t *testing.T) {
	st := memstore.New()
	apps := &workbenchFixtureApps{identity: "authorization-1"}
	calls := 0
	client := &http.Client{Transport: workbenchRoundTrip(func(r *http.Request) (*http.Response, error) {
		calls++
		if r.URL.String() != "https://fixture.test/api/items?status=open" || r.Header.Get("Authorization") != "Bearer fixture-secret" {
			t.Fatalf("wrong request: %s %+v", r.URL, r.Header)
		}
		return &http.Response{StatusCode: 200, Header: http.Header{"Content-Type": {"application/json"}}, Body: io.NopCloser(strings.NewReader(`{"items":[{"id":"1"}],"total":1}`))}, nil
	})}
	handler := New(nil, st, st, nil).WithHome(t.TempDir()).WithApps(apps).WithAppExecutor(appexec.New(client)).Handler("fixture-token", nil)
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
	handler.ServeHTTP(unauthorized, httptest.NewRequest("GET", "/canvases", nil))
	if unauthorized.Code != 401 {
		t.Fatal("missing token accepted")
	}
	var w store.Workbench
	if err := json.Unmarshal(call("POST", "/canvases", map[string]any{"name": "Fixture"}, 201), &w); err != nil {
		t.Fatal(err)
	}
	base := "/canvases/" + w.ID
	pkg := workbench.Package{Files: map[string]string{
		"src/App.tsx":    "export default ()=> <p>Fixture</p>",
		"workbench.json": `{"schemaVersion":1,"sdkVersion":"1","entry":"src/App.tsx","sources":{"primary":{"appID":"fixture","endpoint":"rest"}},"operations":{"items":{"source":"primary","kind":"rest","effectHint":"read","inputSchema":{"type":"object","properties":{"status":{"type":"string","enum":["open","closed"]}},"required":["status"],"additionalProperties":false},"request":{"method":"GET","path":"/items","query":{"status":{"$input":"/status"}}},"result":{"rows":"/items","total":"/total"}}}}`,
	}}
	if err := json.Unmarshal(saveCanvasTestFiles(t, call, base, "save-1", pkg.Files), &w); err != nil {
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
	apps.identity = "authorization-2"
	call("POST", base+"/queries/items", query, 200)
	if calls != 2 {
		t.Fatal("authorized App connection was not reused")
	}
	apps.multiple = true
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

func TestWorkbenchActionsAreConfirmedOnceAndInvalidateOnChange(t *testing.T) {
	st := memstore.New()
	apps := &workbenchFixtureApps{identity: "authorization-1", writes: true}
	calls := 0
	failTransport := false
	client := &http.Client{Transport: workbenchRoundTrip(func(r *http.Request) (*http.Response, error) {
		calls++
		if failTransport {
			return nil, fmt.Errorf("connection interrupted after dispatch")
		}
		if r.Method != "POST" {
			t.Fatal(r.Method)
		}
		return &http.Response{StatusCode: 200, Header: http.Header{"Content-Type": {"application/json"}}, Body: io.NopCloser(strings.NewReader(`{"success":true}`))}, nil
	})}
	handler := New(nil, st, st, nil).WithHome(t.TempDir()).WithApps(apps).WithAppExecutor(appexec.New(client)).Handler("test", nil)
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
	var w store.Workbench
	_ = json.Unmarshal(call("POST", "/canvases", map[string]any{"name": "Actions"}, 201), &w)
	base := "/canvases/" + w.ID
	pkg := workbench.Package{Files: map[string]string{"src/App.tsx": "export default ()=>null", "workbench.json": `{"schemaVersion":1,"sdkVersion":"1","entry":"src/App.tsx","sources":{"primary":{"appID":"fixture","endpoint":"rest"}},"operations":{"save":{"source":"primary","kind":"rest","effectHint":"write","inputSchema":{"type":"object","properties":{"id":{"type":"string","maxLength":50}},"required":["id"],"additionalProperties":false},"request":{"method":"POST","path":"/items","body":{"id":{"$input":"/id"}}},"result":{"success":{"pointer":"/success","equals":true}}}}}`}}
	_ = json.Unmarshal(saveCanvasTestFiles(t, call, base, "save", pkg.Files), &w)
	call("POST", base+"/build-receipts", map[string]any{"revisionHash": w.HeadRevision, "sdkVersion": "1", "compilerVersion": "fixture", "dependencyHash": strings.Repeat("b", 64), "ok": true}, 200)
	_ = json.Unmarshal(call("PUT", base+"/bindings", map[string]any{"expectedRevision": w.Revision, "revisionHash": w.HeadRevision, "bindings": map[string]string{"primary": "account-1"}}, 200), &w)
	_ = json.Unmarshal(call("POST", base+"/activate", map[string]any{"expectedRevision": w.Revision, "revisionHash": w.HeadRevision}, 200), &w)
	call("POST", base+"/queries/save", map[string]any{"revisionHash": w.HeadRevision, "bindingVersion": w.BindingVersion, "params": map[string]any{"id": "one"}}, 403)
	if calls != 0 {
		t.Fatal("write operation ran through automatic query")
	}
	prepare := func(key string, status int) store.WorkbenchAction {
		t.Helper()
		var a store.WorkbenchAction
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
	var done store.WorkbenchAction
	_ = json.Unmarshal(call("POST", path, map[string]any{"confirm": true}, 200), &done)
	if done.State != "succeeded" || calls != 1 {
		t.Fatalf("result %+v calls %d", done, calls)
	}
	call("POST", path, map[string]any{"confirm": true}, 200)
	if calls != 1 {
		t.Fatal("replayed write dispatched")
	}
	a = prepare("two", 200)
	apps.identity = "authorization-2"
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
	apps.writes = false
	prepare("four", 403)
	if calls != 2 {
		t.Fatal("App without write capability dispatched")
	}
}

func TestWorkbenchResponseFailuresAndBudget(t *testing.T) {
	rest := workbench.Operation{Kind: "rest", Result: workbench.Result{Rows: "/items", Total: "/total", Success: &workbench.Condition{Pointer: "/success", Equals: true}}}
	for _, response := range []map[string]any{
		{"ok": false, "reason": "timeout"},
		{"ok": true, "status": 500, "body_json": map[string]any{}},
		{"ok": true, "status": 200, "body_truncated": true},
		{"ok": true, "status": 200, "body_json": map[string]any{"items": []any{}, "total": float64(42), "success": false}},
		{"ok": true, "status": 200, "body_json": map[string]any{"items": []any{}, "success": true}},
	} {
		if _, err := workbenchResponseData(rest, response); err == nil {
			t.Fatalf("false success for %+v", response)
		}
	}
	graph := workbench.Operation{Kind: "graphql"}
	if _, err := workbenchResponseData(graph, map[string]any{"ok": true, "status": 200, "data": map[string]any{}, "errors": []any{map[string]any{"message": "partial failure"}}}); err == nil {
		t.Fatal("GraphQL errors accepted")
	}
	response := map[string]any{"ok": true, "status": 200, "body_json": map[string]any{"items": []any{map[string]any{"id": "one"}}, "total": float64(42), "success": true}}
	data, err := workbenchResponseData(rest, response)
	if err != nil || data.(map[string]any)["total"] != float64(42) {
		t.Fatalf("source total changed %+v %v", data, err)
	}
	s := &Server{}
	var releases []func()
	for i := 0; i < 4; i++ {
		release, ok := s.acquireWorkbenchRequest("one")
		if !ok {
			t.Fatal("early concurrency limit")
		}
		releases = append(releases, release)
	}
	if _, ok := s.acquireWorkbenchRequest("one"); ok {
		t.Fatal("unbounded concurrent requests")
	}
	for _, release := range releases {
		release()
	}
	for i := 4; i < 120; i++ {
		release, ok := s.acquireWorkbenchRequest("one")
		if !ok {
			t.Fatal("early rate limit")
		}
		release()
	}
	if _, ok := s.acquireWorkbenchRequest("one"); ok {
		t.Fatal("unbounded request rate")
	}
}

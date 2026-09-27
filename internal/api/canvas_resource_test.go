package api

import (
	"context"
	"net/http"
	"strings"
	"testing"

	"github.com/teatak/pudding-core/internal/store"
	"github.com/teatak/pudding-core/internal/workbench"
)

func TestCanvasUpgradesToAppAndRestoresStructuredVersion(t *testing.T) {
	srv, st := newTestServer(t)
	ctx := context.Background()
	if err := st.CreateSession(ctx, &store.Session{ID: "author", Provider: "mock", Model: "m"}); err != nil {
		t.Fatal(err)
	}
	item, err := st.PutCanvasItem(ctx, store.CanvasItemInput{ActorSessionID: "author", ID: "table", Kind: "table", Title: "Report", Item: []byte(`{"columns":["name"],"rows":[{"name":"Keep"}]}`)})
	if err != nil {
		t.Fatal(err)
	}
	resource, err := st.GetWorkbench(ctx, item.ResourceID)
	if err != nil {
		t.Fatal(err)
	}
	first := resource.ActiveRevision
	base := srv.URL + "/canvases/" + resource.ID
	pkg := workbench.Package{Files: map[string]string{"workbench.json": `{"schemaVersion":1,"sdkVersion":"1","entry":"src/App.tsx","sources":{},"operations":{}}`, "src/App.tsx": "export default function App(){return <button>Keep</button>}"}}
	response := req(t, "POST", base+"/revisions", map[string]any{"expectedRevision": resource.Revision, "clientRequestID": "upgrade", "package": pkg})
	if response.StatusCode != 200 {
		t.Fatal(response.StatusCode)
	}
	resourcePtr := decodeJSON[store.Workbench](t, response)
	response.Body.Close()
	resource = &resourcePtr
	if resource.ActiveRevision != first {
		t.Fatal("unbuilt source replaced working canvas")
	}
	if _, err := st.GetWorkbenchRevision(ctx, resource.ID, first); err != nil {
		t.Fatal("upgrade lost structured history")
	}
	response = req(t, "POST", base+"/build-receipts", map[string]any{"revisionHash": resource.HeadRevision, "sdkVersion": "1", "compilerVersion": "test", "dependencyHash": strings.Repeat("a", 64), "ok": true})
	if response.StatusCode != 200 {
		t.Fatal(response.StatusCode)
	}
	response.Body.Close()
	response = req(t, "POST", base+"/activate", map[string]any{"expectedRevision": resource.Revision, "revisionHash": resource.HeadRevision})
	if response.StatusCode != 200 {
		t.Fatal(response.StatusCode)
	}
	resourcePtr = decodeJSON[store.Workbench](t, response)
	response.Body.Close()
	resource = &resourcePtr
	views, err := st.ListCanvasItems(ctx, "author")
	if err != nil || len(views) != 1 || views[0].Kind != "app" || views[0].ResourceID != item.ResourceID {
		t.Fatalf("upgrade detached mount: %+v %v", views, err)
	}
	response = req(t, "POST", base+"/activate", map[string]any{"expectedRevision": resource.Revision, "revisionHash": first})
	if response.StatusCode != 200 {
		t.Fatal(response.StatusCode)
	}
	response.Body.Close()
	views, err = st.ListCanvasItems(ctx, "author")
	if err != nil || views[0].Kind != "table" || string(views[0].Item) != string(item.Item) {
		t.Fatal("restore lost structured content")
	}
	if err := st.DeleteSession(ctx, "author"); err != nil {
		t.Fatal(err)
	}
	response = req(t, http.MethodGet, base+"/revisions/"+first, nil)
	if response.StatusCode != 200 {
		t.Fatal("source session deletion removed canvas")
	}
	response.Body.Close()
}

package api

import (
	"context"
	"net/http"
	"strings"
	"testing"

	"github.com/teatak/pudding-core/internal/store"
	"github.com/teatak/pudding-core/internal/workbench"
)

func TestCanvasAppVersionsRetainIdentityAndRequireBuild(t *testing.T) {
	srv, st := newTestServer(t)
	ctx := context.Background()
	if err := st.CreateSession(ctx, &store.Session{ID: "author", Provider: "mock", Model: "m"}); err != nil {
		t.Fatal(err)
	}
	resource, err := st.CreateWorkbench(ctx, &store.Workbench{ID: "canvas", Name: "Report", SourceSessionID: "author"})
	if err != nil {
		t.Fatal(err)
	}
	item, err := st.OpenCanvasResource(ctx, "author", resource.ID, "view")
	if err != nil {
		t.Fatal(err)
	}
	first := ""
	base := srv.URL + "/canvases/" + resource.ID
	pkg := workbench.Package{Files: map[string]string{"workbench.json": `{"schemaVersion":1,"sdkVersion":"1","entry":"src/App.tsx","sources":{},"operations":{}}`, "src/App.tsx": "export default function App(){return <button>Keep</button>}"}}
	response := req(t, "POST", base+"/draft", map[string]any{})
	if response.StatusCode != 200 {
		t.Fatal(response.StatusCode)
	}
	draft := decodeJSON[struct {
		DraftHash string `json:"draftHash"`
	}](t, response)
	response.Body.Close()
	for path, content := range pkg.Files {
		response = req(t, "PUT", base+"/draft/file", map[string]any{"path": path, "content": content, "expectedDraftHash": draft.DraftHash})
		if response.StatusCode != 200 {
			t.Fatal(response.StatusCode)
		}
		draft = decodeJSON[struct {
			DraftHash string `json:"draftHash"`
		}](t, response)
		response.Body.Close()
	}
	response = req(t, "POST", base+"/draft/commit", map[string]any{"expectedDraftHash": draft.DraftHash, "clientRequestID": "upgrade"})
	if response.StatusCode != 200 {
		t.Fatal(response.StatusCode)
	}
	resourcePtr := decodeJSON[store.Workbench](t, response)
	response.Body.Close()
	resource = &resourcePtr
	if resource.ActiveRevision != first {
		t.Fatal("unbuilt source replaced working canvas")
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
	first = resource.ActiveRevision
	if err := st.DeleteSession(ctx, "author"); err != nil {
		t.Fatal(err)
	}
	response = req(t, http.MethodGet, base+"/revisions/"+first, nil)
	if response.StatusCode != 200 {
		t.Fatal("source session deletion removed canvas")
	}
	response.Body.Close()
}

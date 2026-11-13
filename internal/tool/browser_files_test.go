package tool

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/teatak/pudding-core/internal/attachment"
	"github.com/teatak/pudding-core/internal/browser"
	"github.com/teatak/pudding-core/internal/store"
	"github.com/teatak/pudding-core/internal/store/storetest"
)

type fileToolBrowser struct {
	fakeToolBrowser
	uploaded      browser.UploadInput
	calls         int
	downloadInput browser.DownloadInput
	owner         string
}

func (f *fileToolBrowser) Upload(_ context.Context, session, tab string, in browser.UploadInput) (browser.ActionResult, error) {
	f.uploaded = in
	f.calls++
	f.owner = session
	return browser.ActionResult{Action: "upload", Tab: browser.TabSnapshot{ID: tab, SessionID: session}, Result: map[string]any{"delivered": true}}, nil
}
func (f *fileToolBrowser) Downloads(_ context.Context, session string, in browser.DownloadInput) (browser.DownloadResult, error) {
	f.owner = session
	f.downloadInput = in
	return browser.DownloadResult{Downloads: []browser.Download{{ID: "file", SessionID: session, State: "completed", Path: "/downloads/result.txt"}}}, nil
}
func TestBuiltinBrowserUploadSourcesAndSnapshots(t *testing.T) {
	home, project := t.TempDir(), t.TempDir()
	file := filepath.Join(project, "source.txt")
	if err := os.WriteFile(file, []byte("project-content"), 0600); err != nil {
		t.Fatal(err)
	}
	attached, err := attachment.NewService(home).StoreReader("session", "attached.txt", "text/plain", strings.NewReader("attachment-content"))
	if err != nil {
		t.Fatal(err)
	}
	fake := &fileToolBrowser{}
	history := storetest.New(t)
	persistExportDraftReference(t, history, "session", []store.ContentPart{store.AttachmentPart(attached)})
	runner := NewBuiltinRunner(WithHomeDir(home), WithBrowser(fake), WithHistorySearch(history))
	args := map[string]any{"tabID": "tab", "frameID": "frame", "windowID": "popup", "selector": "#files", "files": []map[string]string{{"source": "project", "path": file}, {"source": "attachment", "attachmentKey": attached.AttachmentKey}}}
	raw, _ := json.Marshal(args)
	call := Call{SessionID: "session", Name: BrowserUpload, ProjectDirs: []string{project}, Args: raw}
	result := runner.Call(context.Background(), call)
	if !result.Ok || fake.calls != 1 || fake.uploaded.FrameID != "frame" || fake.uploaded.WindowID != "popup" || len(fake.uploaded.Files) != 2 {
		t.Fatalf("upload failed %+v %+v", result, fake)
	}
	if err := os.WriteFile(file, []byte("changed"), 0600); err != nil {
		t.Fatal(err)
	}
	for i, want := range []string{"project-content", "attachment-content"} {
		got, err := os.ReadFile(fake.uploaded.Files[i])
		if err != nil || string(got) != want {
			t.Fatalf("snapshot changed: %s %v", got, err)
		}
	}
	if strings.Contains(result.Content, fake.uploaded.Files[0]) {
		t.Fatal("private snapshot path leaked")
	}
	if filepath.Base(fake.uploaded.Files[1]) != attached.Name {
		t.Fatal("attachment display name was replaced by its internal blob name")
	}
	// No authorized directory, foreign attachment, symlink escape, directories and
	// incompatible source fields all fail before the browser receives a request.
	escaped := filepath.Join(project, "escape")
	if err := os.Symlink(filepath.Join(t.TempDir(), "outside"), escaped); err != nil {
		t.Fatal(err)
	}
	for _, source := range []map[string]string{
		{"source": "project", "path": project}, {"source": "project", "path": escaped},
		{"source": "project", "path": filepath.Join(t.TempDir(), "secret")},
		{"source": "attachment", "attachmentKey": "sessions/other/blobs/file"},
		{"source": "attachment", "attachmentKey": attached.AttachmentKey, "path": file},
	} {
		args["files"] = []map[string]string{source}
		call.Args, _ = json.Marshal(args)
		if res := runner.Call(context.Background(), call); res.Ok {
			t.Fatalf("accepted source %+v", source)
		}
	}
	if fake.calls != 1 {
		t.Fatal("invalid source reached browser")
	}
	args["files"] = []map[string]string{{"source": "project", "path": file}}
	call.Args, _ = json.Marshal(args)
	call.ProjectDirs = nil
	if res := runner.Call(context.Background(), call); res.Ok {
		t.Fatal("unapproved project file accepted")
	}
	call.ProjectDirs = []string{project}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if res := runner.Call(ctx, call); res.Ok {
		t.Fatal("cancelled upload succeeded")
	}
	if fake.calls != 1 {
		t.Fatal("cancelled upload dispatched")
	}
	if err := os.Truncate(file, attachment.MaxUploadBytes+1); err != nil {
		t.Fatal(err)
	}
	if res := runner.Call(context.Background(), call); res.Ok || !strings.Contains(res.Content, "upload_too_large") {
		t.Fatalf("oversized upload was not rejected: %+v", res)
	}
	if fake.calls != 1 {
		t.Fatal("oversized upload dispatched")
	}
}
func TestBuiltinBrowserDownloadsOwnershipAndBaseline(t *testing.T) {
	fake := &fileToolBrowser{}
	runner := NewBuiltinRunner(WithBrowser(fake))
	result := runner.Call(context.Background(), Call{SessionID: "owner", Name: BrowserDownloads, Args: json.RawMessage(`{"action":"wait","tabID":"closed","windowID":"popup","knownIDs":[],"timeoutMs":500}`)})
	if !result.Ok || fake.owner != "owner" || fake.downloadInput.TabID != "closed" || fake.downloadInput.KnownIDs == nil || len(*fake.downloadInput.KnownIDs) != 0 || fake.downloadInput.WindowID == nil || *fake.downloadInput.WindowID != "popup" {
		t.Fatalf("download scope lost %+v %+v", result, fake)
	}
	if !strings.Contains(result.Content, "/downloads/result.txt") {
		t.Fatal("completed path omitted")
	}
}

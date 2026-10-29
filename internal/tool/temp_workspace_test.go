package tool

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/teatak/pudding-core/internal/attachment"
	"github.com/teatak/pudding-core/internal/home"
	"github.com/teatak/pudding-core/internal/store"
)

func TestTempWorkspaceLifecycle(t *testing.T) {
	homeDir, project := t.TempDir(), t.TempDir()
	r := NewBuiltinRunner(WithHomeDir(homeDir))
	t.Cleanup(func() { _ = r.Close() })
	invoke := func(session, action string) map[string]any {
		t.Helper()
		raw, _ := json.Marshal(map[string]string{"action": action})
		result := r.Call(context.Background(), Call{SessionID: session, Name: TempWorkspace, Args: raw, ProjectDirs: []string{project}})
		if !result.Ok {
			t.Fatalf("%s: %+v", action, result)
		}
		return decodeToolResult(t, result)
	}
	first, other := invoke("one", "open"), invoke("two", "open")
	path, relative := first["absolutePath"].(string), first["path"].(string)
	if path == other["absolutePath"] || commandPathInsideProject(path, project, []string{project}) {
		t.Fatal("scratch is not session-isolated outside the project")
	}
	raw, _ := json.Marshal(map[string]string{"scope": "temp", "path": filepath.Join(relative, "preview.log"), "content": "temporary"})
	call := Call{SessionID: "one", Name: FileWrite, Args: raw, ProjectDirs: []string{project}}
	if result := r.Call(context.Background(), call); !result.Ok {
		t.Fatal(result)
	}
	if _, tracked := MutationTrackingForCall(call); tracked {
		t.Fatal("scratch entered project diff tracking")
	}
	// Cleanup owns only scratch; exported analysis attachments are persistent.
	root, artifactPath, err := home.OpenSessionArtifacts(homeDir, "one")
	if err != nil {
		t.Fatal(err)
	}
	if err := root.WriteFile("export.txt", []byte("keep"), 0o600); err != nil {
		t.Fatal(err)
	}
	root.Close()
	if err := os.WriteFile(filepath.Join(project, "_preview.png"), []byte("user file"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(project, filepath.Join(path, "project-link")); err != nil {
		t.Fatal(err)
	}
	png, err := base64.StdEncoding.DecodeString("iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAQAAAC1HAwCAAAAC0lEQVR42mP8/x8AAwMCAO+a1ioAAAAASUVORK5CYII=")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(path, "preview.png"), png, 0o600); err != nil {
		t.Fatal(err)
	}
	mediaArgs, _ := json.Marshal(map[string]string{"source": "file", "scope": "temp", "path": filepath.Join(relative, "preview.png")})
	media := r.Call(context.Background(), Call{SessionID: "one", Mode: store.ModeCode, Name: MediaRead, Args: mediaArgs})
	if !media.Ok || len(media.Attachments) != 1 {
		t.Fatalf("preview was not preserved as an attachment: %+v", media)
	}
	invoke("one", "clear")
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatalf("scratch not removed: %v", err)
	}
	for _, keep := range []string{filepath.Join(project, "_preview.png"), filepath.Join(artifactPath, "export.txt"), other["absolutePath"].(string)} {
		if _, err := os.Stat(keep); err != nil {
			t.Fatalf("cleanup touched %s: %v", keep, err)
		}
	}
	storedPath, ok, err := attachment.NewService(homeDir).Path("one", media.Attachments[0].AttachmentKey)
	if err != nil || !ok {
		t.Fatalf("attachment lookup failed: %v", err)
	}
	if _, err := os.Stat(storedPath); err != nil {
		t.Fatal("cleanup deleted delivered preview", err)
	}
	invoke("one", "open")
	r.CloseSession("one")
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatal("session release retained scratch")
	}
	if err := r.Close(); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(other["absolutePath"].(string)); !os.IsNotExist(err) {
		t.Fatal("shutdown retained scratch")
	}
}

func TestTempWorkspaceRejectsCallerPathsAndSymlinkRoot(t *testing.T) {
	homeDir, outside := t.TempDir(), t.TempDir()
	r := NewBuiltinRunner(WithHomeDir(homeDir))
	t.Cleanup(func() { _ = r.Close() })
	for _, args := range []string{`{"action":"clear","path":"/"}`, `{"action":"clear","sessionID":"other"}`, `{"action":"delete"}`} {
		if result := r.Call(context.Background(), Call{SessionID: "one", Name: TempWorkspace, Args: json.RawMessage(args)}); result.Ok {
			t.Fatalf("accepted caller target: %s", args)
		}
	}
	root, path, err := home.OpenSessionArtifacts(homeDir, "one")
	if err != nil {
		t.Fatal(err)
	}
	root.Close()
	if err := os.Symlink(outside, filepath.Join(path, "scratch")); err != nil {
		t.Fatal(err)
	}
	if result := r.Call(context.Background(), Call{SessionID: "one", Name: TempWorkspace, Args: json.RawMessage(`{"action":"open"}`)}); result.Ok {
		t.Fatal("accepted symlink scratch root")
	}
}

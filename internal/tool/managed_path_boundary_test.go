package tool

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/teatak/pudding-core/internal/home"
	"github.com/teatak/pudding-core/internal/store"
)

func TestManagedPathsRejectReservedSymlinkTargets(t *testing.T) {
	for _, linkParent := range []bool{false, true} {
		for _, name := range []string{FileRead, FileWrite, FileCopy, MediaRead} {
			t.Run(name+map[bool]string{false: "/leaf", true: "/parent"}[linkParent], func(t *testing.T) {
				homeDir := t.TempDir()
				scratch, err := home.PrepareCodeScratch(homeDir, "other-session")
				if err != nil {
					t.Fatal(err)
				}
				artifacts, artifactDir, err := home.OpenSessionArtifacts(homeDir, "current-session")
				if err != nil {
					t.Fatal(err)
				}
				artifacts.Close()
				filename, data := "private.txt", []byte("private session content")
				if name == MediaRead {
					filename, data = "private.png", testPNGBytes()
				}
				privateFile := filepath.Join(scratch, filename)
				if err := os.WriteFile(privateFile, data, 0o600); err != nil {
					t.Fatal(err)
				}
				link := filepath.Join(artifactDir, "visible-link")
				linkTarget, requested := privateFile, link
				if linkParent {
					linkTarget, requested = scratch, filepath.Join(link, filename)
				}
				if err := os.Symlink(linkTarget, link); err != nil {
					t.Fatal(err)
				}
				path, err := filepath.Rel(home.TempPath(homeDir), requested)
				if err != nil {
					t.Fatal(err)
				}
				var args any
				switch name {
				case FileRead:
					args = map[string]any{"scope": "temp", "path": path}
				case FileWrite:
					args = map[string]any{"scope": "temp", "path": path, "content": "unauthorized change"}
				case FileCopy:
					args = map[string]any{"from": map[string]string{"scope": "temp", "path": path}, "to": map[string]string{"scope": "temp", "path": "copied.txt"}}
				case MediaRead:
					args = map[string]any{"source": "file", "scope": "temp", "path": path}
				}
				runner := NewBuiltinRunner(WithHomeDir(homeDir))
				t.Cleanup(func() { _ = runner.Close() })
				raw, _ := json.Marshal(args)
				result := runner.Call(context.Background(), Call{SessionID: "current-session", Name: name, Mode: store.ModeCode, Args: raw})
				if result.Ok || len(result.ContextAttachments) != 0 {
					t.Errorf("reserved target accepted through alias: %+v", result)
				}
				if actual, err := os.ReadFile(privateFile); err != nil || string(actual) != string(data) {
					t.Errorf("reserved target changed: %q %v", actual, err)
				}
				if _, err := os.Stat(filepath.Join(home.TempPath(homeDir), "copied.txt")); !os.IsNotExist(err) {
					t.Errorf("copy created a destination: %v", err)
				}
			})
		}
	}
}

func TestManagedPathsRejectNewFilesThroughReservedParent(t *testing.T) {
	for _, name := range []string{FileWrite, FileCopy} {
		t.Run(name, func(t *testing.T) {
			homeDir := t.TempDir()
			scratch, err := home.PrepareCodeScratch(homeDir, "other-session")
			if err != nil {
				t.Fatal(err)
			}
			if err := os.Symlink(scratch, filepath.Join(home.TempPath(homeDir), "link")); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(filepath.Join(home.TempPath(homeDir), "source.txt"), []byte("source"), 0o600); err != nil {
				t.Fatal(err)
			}
			var args any = map[string]any{"scope": "temp", "path": "link/new/created.txt", "content": "must not write"}
			if name == FileCopy {
				args = map[string]any{"from": map[string]string{"scope": "temp", "path": "source.txt"}, "to": map[string]string{"scope": "temp", "path": "link/new/created.txt"}}
			}
			runner := NewBuiltinRunner(WithHomeDir(homeDir))
			t.Cleanup(func() { _ = runner.Close() })
			raw, _ := json.Marshal(args)
			result := runner.Call(context.Background(), Call{SessionID: "current-session", Name: name, Mode: store.ModeCode, Args: raw})
			if result.Ok {
				t.Errorf("reserved new target accepted: %+v", result)
			}
			if entries, err := os.ReadDir(scratch); err != nil || len(entries) != 0 {
				t.Errorf("validation wrote into reserved directory: %v %v", entries, err)
			}
		})
	}
}

func TestManagedPathsCanonicalAliasRemainsReusable(t *testing.T) {
	actualHome := t.TempDir()
	homeAlias := filepath.Join(t.TempDir(), "home")
	if err := os.Symlink(actualHome, homeAlias); err != nil {
		t.Fatal(err)
	}
	temp := home.TempPath(actualHome)
	if err := os.MkdirAll(filepath.Join(temp, "visible"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(temp, "visible", "data.txt"), []byte("public content"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(filepath.Join(temp, "visible", "data.txt"), filepath.Join(temp, "link")); err != nil {
		t.Fatal(err)
	}
	runner := NewBuiltinRunner(WithHomeDir(homeAlias))
	t.Cleanup(func() { _ = runner.Close() })
	read := func(path string) Result {
		raw, _ := json.Marshal(map[string]string{"scope": "temp", "path": path})
		return runner.Call(context.Background(), Call{Name: FileRead, Args: raw})
	}
	first := read("link")
	if !first.Ok {
		t.Fatal(first.Content)
	}
	payload := decodeToolResult(t, first)
	if payload["path"] != "visible/data.txt" {
		t.Fatalf("canonical relative path=%v", payload["path"])
	}
	second := read(payload["path"].(string))
	if !second.Ok || decodeToolResult(t, second)["content"] != "public content" {
		t.Fatalf("returned scope/path cannot be reused: %+v", second)
	}
}

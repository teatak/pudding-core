package tool

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func TestBuiltinWorkspaceList(t *testing.T) {
	root := t.TempDir()
	if err := os.Mkdir(filepath.Join(root, "dir"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "file.txt"), []byte("hello"), 0o600); err != nil {
		t.Fatal(err)
	}

	res := NewBuiltinRunner().Call(context.Background(), Call{
		Name:          WorkspaceList,
		WorkspaceDirs: []string{root},
		Args:          json.RawMessage(`{"path":".","maxEntries":10}`),
	})
	if !res.Ok {
		t.Fatalf("workspace list should succeed: %+v", res)
	}
	payload := decodeToolResult(t, res)
	if payload["ok"] != true || payload["root"] != root || payload["totalCount"] != float64(2) {
		t.Fatalf("unexpected list result: %+v", payload)
	}
	entries := payload["entries"].([]any)
	first := entries[0].(map[string]any)
	if first["name"] != "dir" || first["type"] != "dir" {
		t.Fatalf("directories should be listed first: %+v", entries)
	}
}

func TestBuiltinWorkspaceListRequiresDirs(t *testing.T) {
	res := NewBuiltinRunner().Call(context.Background(), Call{Name: WorkspaceList})
	if res.Ok {
		t.Fatalf("workspace list without dirs should fail: %+v", res)
	}
	payload := decodeToolResult(t, res)
	if payload["reason"] != "workspace_dirs_required" {
		t.Fatalf("unexpected missing dirs result: %+v", payload)
	}
}

func TestBuiltinWorkspaceListRejectsOutsideRoot(t *testing.T) {
	root := t.TempDir()
	outside := t.TempDir()
	args, err := json.Marshal(map[string]any{"path": outside})
	if err != nil {
		t.Fatal(err)
	}

	res := NewBuiltinRunner().Call(context.Background(), Call{
		Name:          WorkspaceList,
		WorkspaceDirs: []string{root},
		Args:          args,
	})
	if res.Ok {
		t.Fatalf("outside path should fail: %+v", res)
	}
	payload := decodeToolResult(t, res)
	if payload["reason"] != "path_not_allowed" {
		t.Fatalf("unexpected outside path result: %+v", payload)
	}
}

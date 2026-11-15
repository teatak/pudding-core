//go:build !windows

package tool

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"testing"
)

func TestFilePatchRejectsApprovalPermissionDrift(t *testing.T) {
	for _, modes := range [][2]os.FileMode{{0o644, 0o755}, {0o755, 0o644}} {
		t.Run(fmt.Sprintf("%o-to-%o", modes[0], modes[1]), func(t *testing.T) {
			root := t.TempDir()
			first := filepath.Join(root, "first.txt")
			path := filepath.Join(root, "script.sh")
			writePatchTestFile(t, first, "first before\n")
			writePatchTestFile(t, path, "echo before\n")
			if err := os.Chmod(path, modes[0]); err != nil {
				t.Fatal(err)
			}
			runner := NewBuiltinRunner()
			defer runner.Close()
			args, err := json.Marshal(map[string]any{
				"scope": "project",
				"files": []map[string]any{
					{"path": "first.txt", "action": "replace", "content": "first after\n", "expected_hash": patchContentHash([]byte("first before\n"))},
					{"path": "script.sh", "action": "replace", "content": "echo after\n", "expected_hash": patchContentHash([]byte("echo before\n"))},
				},
			})
			if err != nil {
				t.Fatal(err)
			}
			call := Call{SessionID: "session", TurnID: "turn", CallID: "mode-drift", Name: FilePatch, Args: args, ProjectDirs: []string{root}}
			if _, err := runner.ApprovalDetails(context.Background(), call); err != nil {
				t.Fatal(err)
			}
			if err := os.Chmod(path, modes[1]); err != nil {
				t.Fatal(err)
			}
			result := runner.Call(context.Background(), call)
			if result.Ok || decodeToolResult(t, result)["reason"] != "patch_stale" {
				t.Fatalf("approved patch silently replaced user permissions: %s", result.Content)
			}
			info, err := os.Stat(path)
			if err != nil || info.Mode().Perm() != modes[1] {
				t.Fatalf("user permissions changed: %v %v", info, err)
			}
			if readPatchTestFile(t, path) != "echo before\n" || readPatchTestFile(t, first) != "first before\n" {
				t.Fatal("permission conflict partially applied the batch")
			}
			assertNoPatchTempFiles(t, root)
		})
	}
}

package tool

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestFilePatchRejectsFinalSymlinks(t *testing.T) {
	root, outside := projectContractDir(t), projectContractDir(t)
	writePatchTestFile(t, filepath.Join(root, "source.txt"), "keep\n")
	writePatchTestFile(t, filepath.Join(outside, "source.txt"), "keep\n")
	runner := NewBuiltinRunner()
	defer runner.Close()
	for _, target := range []string{"source.txt", "missing.txt", filepath.Join(outside, "source.txt")} {
		link := filepath.Join(root, "alias")
		if err := os.Symlink(target, link); err != nil {
			t.Skipf("symlink unavailable: %v", err)
		}
		for _, action := range []string{"create", "replace", "edit", "delete"} {
			file := map[string]any{"path": "alias", "action": action}
			switch action {
			case "create", "replace":
				file["content"] = "changed\n"
				if action == "replace" {
					file["expected_hash"] = patchContentHash([]byte("keep\n"))
				}
			case "edit":
				file["hunks"] = []map[string]any{{"old_lines": []string{"keep"}, "new_lines": []string{"changed"}}}
			}
			result := patchTestCall(runner, "session", root, FilePatch, map[string]any{"scope": "project", "files": []map[string]any{file}})
			if result.Ok || decodeToolResult(t, result)["reason"] != "symlink_unsupported" {
				t.Fatalf("%s followed final link %q: %s", action, target, result.Content)
			}
		}
		if err := os.Remove(link); err != nil {
			t.Fatal(err)
		}
	}
	for _, path := range []string{filepath.Join(root, "source.txt"), filepath.Join(outside, "source.txt")} {
		if got := readPatchTestFile(t, path); got != "keep\n" {
			t.Fatalf("patch changed a link referent: %q", got)
		}
	}
}

func TestFilePatchRejectsLinkCreatedAfterPreparation(t *testing.T) {
	root := projectContractDir(t)
	for _, name := range []string{"source.txt", "other.txt"} {
		writePatchTestFile(t, filepath.Join(root, name), "keep\n")
	}
	runner := NewBuiltinRunner()
	defer runner.Close()
	call := projectContractCall(FilePatch, []string{root}, map[string]any{"scope": "project", "files": []map[string]any{{"path": "source.txt", "action": "edit", "hunks": []map[string]any{{"old_lines": []string{"keep"}, "new_lines": []string{"changed"}}}}}})
	if _, err := runner.ApprovalDetails(context.Background(), call); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(filepath.Join(root, "source.txt")); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink("other.txt", filepath.Join(root, "source.txt")); err != nil {
		t.Skipf("symlink unavailable: %v", err)
	}
	result := runner.Call(context.Background(), call)
	if result.Ok || decodeToolResult(t, result)["reason"] != "patch_stale" {
		t.Fatalf("stale patch followed a new link: %s", result.Content)
	}
	if got := readPatchTestFile(t, filepath.Join(root, "other.txt")); got != "keep\n" {
		t.Fatalf("referent changed: %q", got)
	}
}

func TestFilePatchPreservesFilenameWhitespace(t *testing.T) {
	for _, name := range []string{" note.txt", "note.txt "} {
		for _, format := range []string{"files", "diff"} {
			t.Run(format+"/"+name, func(t *testing.T) {
				if runtime.GOOS == "windows" && strings.HasSuffix(name, " ") {
					t.Skip("Windows does not preserve trailing filename spaces")
				}
				root := projectContractDir(t)
				writePatchTestFile(t, filepath.Join(root, name), "old\n")
				writePatchTestFile(t, filepath.Join(root, strings.TrimSpace(name)), "old\n")
				args := map[string]any{"scope": "project"}
				if format == "diff" {
					args["diff"] = fmt.Sprintf("--- %q\n+++ %q\n@@ -1 +1 @@\n-old\n+new\n", "a/"+name, "b/"+name)
				} else {
					args["files"] = []map[string]any{{"path": name, "action": "replace", "content": "new\n", "expected_hash": patchContentHash([]byte("old\n"))}}
				}
				runner := NewBuiltinRunner()
				defer runner.Close()
				result := patchTestCall(runner, "session", root, FilePatch, args)
				if !result.Ok || readPatchTestFile(t, filepath.Join(root, name)) != "new\n" {
					t.Fatalf("patch changed file identity: %s", result.Content)
				}
				if got := readPatchTestFile(t, filepath.Join(root, strings.TrimSpace(name))); got != "old\n" {
					t.Fatalf("patch modified the neighbor: %q", got)
				}
				payload := decodePatchPayload(t, result)
				if len(payload.Files) != 1 || payload.Files[0].Path != name {
					t.Fatalf("patch receipt changed the path: %+v", payload.Files)
				}
			})
		}
	}
}

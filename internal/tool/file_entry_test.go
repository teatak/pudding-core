package tool

import (
	"context"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestFileEntryToolsOperateOnSymlinks(t *testing.T) {
	for _, kind := range []string{"file", "directory", "dangling", "outside"} {
		for _, recursive := range []bool{false, true} {
			t.Run(kind+"/recursive="+map[bool]string{false: "false", true: "true"}[recursive], func(t *testing.T) {
				root, outside := projectContractDir(t), projectContractDir(t)
				writePatchTestFile(t, filepath.Join(root, "source.txt"), "keep\n")
				writePatchTestFile(t, filepath.Join(root, "src", "keep.txt"), "directory keep\n")
				writePatchTestFile(t, filepath.Join(outside, "keep.txt"), "outside keep\n")
				target := map[string]string{"file": "source.txt", "directory": "src", "dangling": "missing.txt", "outside": outside}[kind]
				if err := os.Symlink(target, filepath.Join(root, "alias")); err != nil {
					t.Skipf("symlink unavailable: %v", err)
				}
				runner := NewBuiltinRunner()
				defer runner.Close()
				statResult := runner.Call(context.Background(), projectContractCall(FileStat, []string{root}, map[string]any{"scope": "project", "path": "alias"}))
				if payload := decodeToolResult(t, statResult); !statResult.Ok || payload["exists"] != true || payload["type"] != "symlink" || payload["relativePath"] != "alias" {
					t.Fatalf("stat followed the link: %s", statResult.Content)
				}
				move := runner.Call(context.Background(), projectContractCall(FileMove, []string{root}, map[string]any{"scope": "project", "from_path": "alias", "to_path": "moved"}))
				if !move.Ok {
					t.Fatal(move.Content)
				}
				if got, err := os.Readlink(filepath.Join(root, "moved")); err != nil || got != target {
					t.Fatalf("move changed the referent instead of the link: %q %v", got, err)
				}
				result := runner.Call(context.Background(), projectContractCall(FileDelete, []string{root}, map[string]any{"scope": "project", "path": "moved", "recursive": recursive}))
				if !result.Ok {
					t.Fatal(result.Content)
				}
				for _, name := range []string{"alias", "moved"} {
					if _, err := os.Lstat(filepath.Join(root, name)); !os.IsNotExist(err) {
						t.Fatalf("link %s survived: %v", name, err)
					}
				}
				for path, want := range map[string]string{filepath.Join(root, "source.txt"): "keep\n", filepath.Join(root, "src", "keep.txt"): "directory keep\n", filepath.Join(outside, "keep.txt"): "outside keep\n"} {
					if got := readPatchTestFile(t, path); got != want {
						t.Fatalf("referent changed: %q", got)
					}
				}
			})
		}
	}
}

func TestFileMoveReplacesOnlyDestinationSymlink(t *testing.T) {
	root := projectContractDir(t)
	writePatchTestFile(t, filepath.Join(root, "source.txt"), "replacement\n")
	writePatchTestFile(t, filepath.Join(root, "keep.txt"), "keep\n")
	if err := os.Symlink("keep.txt", filepath.Join(root, "alias")); err != nil {
		t.Skipf("symlink unavailable: %v", err)
	}
	runner := NewBuiltinRunner()
	defer runner.Close()
	args := map[string]any{"scope": "project", "from_path": "source.txt", "to_path": "alias"}
	result := runner.Call(context.Background(), projectContractCall(FileMove, []string{root}, args))
	if result.Ok || decodeToolResult(t, result)["reason"] != "destination_exists" {
		t.Fatalf("default move replaced a link: %s", result.Content)
	}
	args["overwrite"] = true
	result = runner.Call(context.Background(), projectContractCall(FileMove, []string{root}, args))
	if !result.Ok {
		t.Fatal(result.Content)
	}
	if info, err := os.Lstat(filepath.Join(root, "alias")); err != nil || !info.Mode().IsRegular() {
		t.Fatalf("destination link was not replaced: %v", err)
	}
	if got := readPatchTestFile(t, filepath.Join(root, "alias")); got != "replacement\n" {
		t.Fatalf("replacement content: %q", got)
	}
	if got := readPatchTestFile(t, filepath.Join(root, "keep.txt")); got != "keep\n" {
		t.Fatalf("destination referent changed: %q", got)
	}
}

func TestFileEntryToolsProtectNestedRoots(t *testing.T) {
	outer := projectContractDir(t)
	inner := filepath.Join(outer, "inner")
	writePatchTestFile(t, filepath.Join(inner, "keep.txt"), "keep\n")
	runner := NewBuiltinRunner()
	defer runner.Close()
	for _, roots := range [][]string{{inner, outer}, {outer, inner}} {
		for _, call := range []Call{
			projectContractCall(FileDelete, roots, map[string]any{"scope": "project", "path": inner, "recursive": true}),
			projectContractCall(FileMove, roots, map[string]any{"scope": "project", "from_path": inner, "to_path": filepath.Join(outer, "moved")}),
		} {
			if result := runner.Call(context.Background(), call); result.Ok {
				t.Fatalf("%s mutated a nested authorized root: %s", call.Name, result.Content)
			}
			if got := readPatchTestFile(t, filepath.Join(inner, "keep.txt")); got != "keep\n" {
				t.Fatalf("nested root content changed: %q", got)
			}
		}
	}
}

func TestFileToolsPreserveFilenameWhitespace(t *testing.T) {
	for _, name := range []string{" note.txt", "note.txt "} {
		t.Run(name, func(t *testing.T) {
			if runtime.GOOS == "windows" && strings.HasSuffix(name, " ") {
				t.Skip("Windows does not preserve trailing filename spaces")
			}
			root := projectContractDir(t)
			selected, neighbor := filepath.Join(root, name), filepath.Join(root, strings.TrimSpace(name))
			writePatchTestFile(t, selected, "selected\n")
			writePatchTestFile(t, neighbor, "neighbor\n")
			runner := NewBuiltinRunner()
			defer runner.Close()
			for _, path := range []string{name, selected} {
				read := runner.Call(context.Background(), projectContractCall(FileRead, []string{root}, map[string]any{"scope": "project", "path": path}))
				if !read.Ok || decodeToolResult(t, read)["content"] != "selected\n" {
					t.Fatalf("read changed file identity: %s", read.Content)
				}
			}
			write := runner.Call(context.Background(), projectContractCall(FileWrite, []string{root}, map[string]any{"scope": "project", "path": name, "content": "updated\n", "expected_hash": patchContentHash([]byte("selected\n"))}))
			if !write.Ok || readPatchTestFile(t, selected) != "updated\n" {
				t.Fatalf("write changed file identity: %s", write.Content)
			}
			copyName := strings.Replace(name, "note", "copy", 1)
			copyResult := runner.Call(context.Background(), projectContractCall(FileCopy, []string{root}, map[string]any{"from": map[string]string{"scope": "project", "path": name}, "to": map[string]string{"scope": "project", "path": copyName}}))
			if !copyResult.Ok || readPatchTestFile(t, filepath.Join(root, copyName)) != "updated\n" {
				t.Fatalf("copy changed file identity: %s", copyResult.Content)
			}
			moveName := strings.Replace(name, "note", "moved", 1)
			move := runner.Call(context.Background(), projectContractCall(FileMove, []string{root}, map[string]any{"scope": "project", "from_path": name, "to_path": moveName}))
			if !move.Ok || readPatchTestFile(t, filepath.Join(root, moveName)) != "updated\n" {
				t.Fatalf("move changed file identity: %s", move.Content)
			}
			deleted := runner.Call(context.Background(), projectContractCall(FileDelete, []string{root}, map[string]any{"scope": "project", "path": moveName}))
			if !deleted.Ok {
				t.Fatal(deleted.Content)
			}
			if _, err := os.Lstat(filepath.Join(root, moveName)); !os.IsNotExist(err) {
				t.Fatalf("selected file was not deleted: %v", err)
			}
			if got := readPatchTestFile(t, neighbor); got != "neighbor\n" {
				t.Fatalf("neighbor was modified: %q", got)
			}
		})
	}
}

package tool

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func TestFileCopyAcrossScopes(t *testing.T) {
	runner := NewBuiltinRunner(WithHomeDir(t.TempDir()))
	project, second := t.TempDir(), t.TempDir()
	seed := runner.Call(context.Background(), Call{Name: FileWrite, Args: json.RawMessage(`{"scope":"temp","path":"page.html","content":"<h1>hello</h1>"}`)})
	if !seed.Ok {
		t.Fatal(seed.Content)
	}
	for _, tc := range []struct{ name, fromScope, fromPath, toScope, toPath string }{
		{"temp to project same filename", "temp", "page.html", "project", filepath.Join(project, "page.html")},
		{"project to temp", "project", filepath.Join(project, "page.html"), "temp", "export.html"},
		{"authorized project roots", "project", filepath.Join(project, "page.html"), "project", filepath.Join(second, "page.html")},
		{"temp to skill", "temp", "page.html", "skill", "example/page.html"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			raw, _ := json.Marshal(map[string]any{"from": map[string]string{"scope": tc.fromScope, "path": tc.fromPath}, "to": map[string]string{"scope": tc.toScope, "path": tc.toPath}})
			result := runner.Call(context.Background(), Call{Name: FileCopy, Args: raw, ProjectDirs: []string{project, second}})
			if !result.Ok {
				t.Fatal(result.Content)
			}
			payload := decodeToolResult(t, result)
			if payload["fromScope"] != tc.fromScope || payload["toScope"] != tc.toScope {
				t.Fatalf("missing scopes: %+v", payload)
			}
			for _, endpoint := range []map[string]string{{"scope": tc.fromScope, "path": tc.fromPath}, {"scope": tc.toScope, "path": tc.toPath}} {
				readArgs, _ := json.Marshal(endpoint)
				read := runner.Call(context.Background(), Call{Name: FileRead, Args: readArgs, ProjectDirs: []string{project, second}})
				if !read.Ok || decodeToolResult(t, read)["content"] != "<h1>hello</h1>" {
					t.Fatal(read.Content)
				}
			}
		})
	}
}

func TestFileCopyScopeBoundaries(t *testing.T) {
	project, outside := t.TempDir(), t.TempDir()
	runner := NewBuiltinRunner(WithHomeDir(t.TempDir()))
	for _, root := range []string{project, outside} {
		if err := os.WriteFile(filepath.Join(root, "keep.txt"), []byte("keep"), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.Symlink(outside, filepath.Join(project, "escape")); err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct{ name, fromScope, fromPath, toScope, toPath, reason string }{
		{"outside source", "project", filepath.Join(outside, "keep.txt"), "temp", "copy.txt", "path_not_authorized"},
		{"outside destination", "project", "keep.txt", "project", filepath.Join(outside, "copy.txt"), "path_not_authorized"},
		{"absolute temp destination", "project", "keep.txt", "temp", filepath.Join(outside, "copy.txt"), "to_path_not_allowed"},
		{"parent temp escape", "project", "keep.txt", "temp", "../copy.txt", "to_path_not_allowed"},
		{"symlink destination escape", "project", "keep.txt", "project", "escape/copy.txt", "path_not_authorized"},
		{"same path", "project", "keep.txt", "project", "./keep.txt", "same_path"},
		{"existing destination", "project", "keep.txt", "project", "other.txt", "to_exists"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if err := os.WriteFile(filepath.Join(project, "other.txt"), []byte("original"), 0o600); err != nil {
				t.Fatal(err)
			}
			raw, _ := json.Marshal(map[string]any{"from": map[string]string{"scope": tc.fromScope, "path": tc.fromPath}, "to": map[string]string{"scope": tc.toScope, "path": tc.toPath}})
			result := runner.Call(context.Background(), Call{Name: FileCopy, Args: raw, ProjectDirs: []string{project}})
			if result.Ok || decodeToolResult(t, result)["reason"] != tc.reason {
				t.Fatalf("want %s: %s", tc.reason, result.Content)
			}
			data, err := os.ReadFile(filepath.Join(project, "other.txt"))
			if err != nil || string(data) != "original" {
				t.Fatal("destination was modified")
			}
			data, err = os.ReadFile(filepath.Join(outside, "keep.txt"))
			if err != nil || string(data) != "keep" {
				t.Fatal("outside file was modified")
			}
		})
	}
}

func TestFileCopyRejectsMalformedEndpoints(t *testing.T) {
	for _, raw := range []string{
		`{"scope":"temp","from_path":"a","to_path":"b"}`,
		`{"from":{"scope":"temp","path":"a"}}`,
		`{"from":null,"to":{"scope":"temp","path":"b"}}`,
		`{"from":{"scope":"temp","path":" "},"to":{"scope":"project","path":"b"}}`,
		`{"from":{"scope":"temp","path":"a","typo":1},"to":{"scope":"temp","path":"b"}}`,
		`{"from":{"scope":"temp","path":"a"},"to":{"scope":"temp","path":"b"}} {}`,
	} {
		result := NewBuiltinRunner(WithHomeDir(t.TempDir())).Call(context.Background(), Call{Name: FileCopy, Args: json.RawMessage(raw)})
		if result.Ok || decodeToolResult(t, result)["reason"] != "invalid_arguments" {
			t.Fatal(result.Content)
		}
	}
}

func TestFileCopyRejectsInvalidEndpointScope(t *testing.T) {
	for _, field := range []string{"from", "to"} {
		args := map[string]any{
			"from": map[string]string{"scope": "temp", "path": "a"},
			"to":   map[string]string{"scope": "temp", "path": "b"},
		}
		args[field] = map[string]string{"scope": "app", "path": "file.txt"}
		raw, _ := json.Marshal(args)
		result := NewBuiltinRunner(WithHomeDir(t.TempDir())).Call(context.Background(), Call{Name: FileCopy, Args: raw})
		payload := decodeToolResult(t, result)
		if result.Ok || payload["reason"] != "invalid_scope" || payload["field"] != field+".scope" {
			t.Fatal(result.Content)
		}
	}
}

func TestFileCopyDirectoryAndOverwriteGuards(t *testing.T) {
	runner := NewBuiltinRunner(WithHomeDir(t.TempDir()))
	root, other := t.TempDir(), t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "src", "nested"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "src", "nested", "a.txt"), []byte("new"), 0o600); err != nil {
		t.Fatal(err)
	}
	copy := func(from, to string, recursive, overwrite bool) Result {
		raw, _ := json.Marshal(map[string]any{"from": map[string]string{"scope": "project", "path": from}, "to": map[string]string{"scope": "project", "path": to}, "recursive": recursive, "overwrite": overwrite})
		return runner.Call(context.Background(), Call{Name: FileCopy, Args: raw, ProjectDirs: []string{root, other}})
	}
	dst := filepath.Join(other, "copy")
	if result := copy(filepath.Join(root, "src"), dst, true, false); !result.Ok {
		t.Fatal(result.Content)
	}
	if result := copy(filepath.Join(root, "src"), dst, true, false); decodeToolResult(t, result)["reason"] != "to_exists" {
		t.Fatal(result.Content)
	}
	if result := copy(filepath.Join(root, "src"), dst, true, true); !result.Ok {
		t.Fatal(result.Content)
	}
	for _, tc := range []struct{ from, to, reason string }{
		{"src", "src/nested/copy", "copy_into_self"},
		{"src/nested", "src", "copy_overlap"},
		{"src/nested/a.txt", "src", "copy_overlap"},
		{"src/nested/a.txt", "src/nested/a.txt", "same_path"},
	} {
		result := copy(filepath.Join(root, tc.from), filepath.Join(root, tc.to), true, true)
		if result.Ok || decodeToolResult(t, result)["reason"] != tc.reason {
			t.Fatal(result.Content)
		}
	}
	file := filepath.Join(root, "src", "nested", "a.txt")
	if err := os.Link(file, filepath.Join(root, "alias.txt")); err != nil {
		t.Fatal(err)
	}
	if result := copy(file, filepath.Join(root, "alias.txt"), false, true); decodeToolResult(t, result)["reason"] != "same_path" {
		t.Fatal(result.Content)
	}
	if err := os.Symlink(other, filepath.Join(root, "src", "escape")); err != nil {
		t.Fatal(err)
	}
	if result := copy(filepath.Join(root, "src"), dst, true, true); result.Ok {
		t.Fatal("recursive symlink accepted")
	}
	// Rejected recursive copies must not remove the existing destination.
	data, err := os.ReadFile(filepath.Join(dst, "nested", "a.txt"))
	if err != nil || string(data) != "new" {
		t.Fatal("existing destination changed")
	}
	if result := copy(file, filepath.Join(other, "file.txt"), false, true); !result.Ok {
		t.Fatal(result.Content)
	}
}

func TestFileCopyPolicyAndTrackingUseDestination(t *testing.T) {
	root := t.TempDir()
	resolved, err := filepath.EvalSymlinks(root)
	if err != nil {
		t.Fatal(err)
	}
	for _, sourceScope := range []string{"temp", "skill", "project"} {
		for _, destinationScope := range []string{"temp", "skill", "project"} {
			raw, _ := json.Marshal(map[string]any{"from": map[string]string{"scope": sourceScope, "path": "source.txt"}, "to": map[string]string{"scope": destinationScope, "path": "target.txt"}})
			risk, classified := ClassifyToolCall(FileCopy, raw)
			tracking, tracked := MutationTrackingForCall(Call{Name: FileCopy, Args: raw, ProjectDirs: []string{root}})
			want := destinationScope == "project"
			if classified != want || tracked != want {
				t.Fatalf("%s -> %s: risk=%+v tracking=%+v", sourceScope, destinationScope, risk, tracking)
			}
			if want && (risk.Class != RiskClassWrite || !risk.LowRisk || len(risk.Paths) != 1 || risk.Paths[0] != "target.txt" || len(tracking.Targets) != 1 || tracking.Targets[0] != filepath.Join(resolved, "target.txt")) {
				t.Fatalf("source counted as a mutation: %+v %+v", risk, tracking)
			}
		}
	}
}

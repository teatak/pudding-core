package tool

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestBuiltinFileDraftWriteReadPatch(t *testing.T) {
	home := t.TempDir()
	runner := NewBuiltinRunner(WithHomeDir(home))

	write := runner.Call(context.Background(), Call{
		Name: FileWrite,
		Args: json.RawMessage(`{"scope":"skill_draft","path":"demo/SKILL.md","content":"hello\nworld\n"}`),
	})
	if !write.Ok {
		t.Fatalf("write should succeed: %+v", write)
	}
	if _, err := os.Stat(filepath.Join(home, "skills-draft", "demo", "SKILL.md")); err != nil {
		t.Fatalf("file missing: %v", err)
	}

	patch := runner.Call(context.Background(), Call{
		Name: FilePatch,
		Args: json.RawMessage(`{"scope":"skill_draft","path":"demo/SKILL.md","old_string":"world","new_string":"pudding"}`),
	})
	if !patch.Ok {
		t.Fatalf("patch should succeed: %+v", patch)
	}
	read := runner.Call(context.Background(), Call{
		Name: FileRead,
		Args: json.RawMessage(`{"scope":"skill_draft","path":"demo/SKILL.md"}`),
	})
	if !read.Ok {
		t.Fatalf("read should succeed: %+v", read)
	}
	payload := decodeToolResult(t, read)
	if payload["content"] != "hello\npudding\n" {
		t.Fatalf("unexpected content: %+v", payload)
	}
}

func TestBuiltinFileListAllowsSkillDraftRoot(t *testing.T) {
	home := t.TempDir()
	if err := os.MkdirAll(filepath.Join(home, "skills-draft", "demo"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(home, "skills-draft", ".reserved"), []byte("hidden"), 0o600); err != nil {
		t.Fatal(err)
	}

	for _, args := range []string{
		`{"scope":"skill_draft","path":"."}`,
		`{"scope":"skill_draft"}`,
	} {
		res := NewBuiltinRunner(WithHomeDir(home)).Call(context.Background(), Call{
			Name: FileList,
			Args: json.RawMessage(args),
		})
		if !res.Ok {
			t.Fatalf("list root should succeed with args %s: %+v", args, res)
		}
		payload := decodeToolResult(t, res)
		entries, ok := payload["entries"].([]any)
		if !ok || len(entries) != 1 {
			t.Fatalf("unexpected entries with args %s: %+v", args, payload)
		}
		entry, ok := entries[0].(map[string]any)
		if !ok || entry["name"] != "demo" {
			t.Fatalf("unexpected entry with args %s: %+v", args, entries[0])
		}
	}
}

func TestBuiltinFileRejectsPublishedWrite(t *testing.T) {
	res := NewBuiltinRunner(WithHomeDir(t.TempDir())).Call(context.Background(), Call{
		Name: FileWrite,
		Args: json.RawMessage(`{"scope":"skill_published","path":"demo/SKILL.md","content":"x"}`),
	})
	if res.Ok {
		t.Fatalf("published write should fail: %+v", res)
	}
	payload := decodeToolResult(t, res)
	if payload["reason"] != "path_not_allowed" {
		t.Fatalf("unexpected reason: %+v", payload)
	}
}

func TestBuiltinFilePatchUsesPublishedFileIncrementally(t *testing.T) {
	home := t.TempDir()
	publishedDir := filepath.Join(home, "skills", "demo", "assets")
	if err := os.MkdirAll(publishedDir, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(home, "skills", "demo", "SKILL.md"), []byte("old"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(publishedDir, "icon.svg"), []byte("<svg/>"), 0o600); err != nil {
		t.Fatal(err)
	}

	res := NewBuiltinRunner(WithHomeDir(home)).Call(context.Background(), Call{
		Name: FilePatch,
		Args: json.RawMessage(`{"scope":"skill_draft","path":"demo/SKILL.md","old_string":"old","new_string":"new"}`),
	})
	if !res.Ok {
		t.Fatalf("patch should succeed: %+v", res)
	}
	if _, err := os.Stat(filepath.Join(home, "skills-draft", "demo", "SKILL.md")); err != nil {
		t.Fatalf("patched file should exist in draft: %v", err)
	}
	if _, err := os.Stat(filepath.Join(home, "skills-draft", "demo", "assets", "icon.svg")); !os.IsNotExist(err) {
		t.Fatalf("published icon should not be copied into draft, stat err=%v", err)
	}
}

func TestBuiltinFileDeleteRecordsDraftManifest(t *testing.T) {
	home := t.TempDir()
	publishedDir := filepath.Join(home, "skills", "demo", "assets")
	if err := os.MkdirAll(publishedDir, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(home, "skills", "demo", "SKILL.md"), []byte("old"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(publishedDir, "icon.svg"), []byte("<svg/>"), 0o600); err != nil {
		t.Fatal(err)
	}

	res := NewBuiltinRunner(WithHomeDir(home)).Call(context.Background(), Call{
		Name: FileDelete,
		Args: json.RawMessage(`{"scope":"skill_draft","path":"demo/assets/icon.svg"}`),
	})
	if !res.Ok {
		t.Fatalf("delete should succeed: %+v", res)
	}
	data, err := os.ReadFile(filepath.Join(home, "skills-draft", "demo", ".delete"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), "assets/icon.svg") {
		t.Fatalf("delete manifest missing icon: %q", data)
	}
}

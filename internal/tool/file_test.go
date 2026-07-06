package tool

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/teatak/pudding-core/internal/attachment"
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

func TestBuiltinFileWorkspaceScopeReadWrite(t *testing.T) {
	root := t.TempDir()
	if err := os.Mkdir(filepath.Join(root, "dir"), 0o755); err != nil {
		t.Fatal(err)
	}
	notePath := filepath.Join(root, "dir", "note.txt")
	if err := os.WriteFile(notePath, []byte("hello"), 0o600); err != nil {
		t.Fatal(err)
	}
	resolvedNotePath, err := filepath.EvalSymlinks(notePath)
	if err != nil {
		t.Fatal(err)
	}
	runner := NewBuiltinRunner(WithHomeDir(t.TempDir()))

	list := runner.Call(context.Background(), Call{
		Name:          FileList,
		Args:          json.RawMessage(`{"scope":"workspace","path":"dir"}`),
		WorkspaceDirs: []string{root},
	})
	if !list.Ok {
		t.Fatalf("workspace list should succeed: %+v", list)
	}
	listPayload := decodeToolResult(t, list)
	if listPayload["root"] != root || listPayload["relativePath"] != "dir" {
		t.Fatalf("workspace metadata missing: %+v", listPayload)
	}
	entries := listPayload["entries"].([]any)
	if len(entries) != 1 || entries[0].(map[string]any)["path"] != resolvedNotePath {
		t.Fatalf("workspace entries should use absolute paths: %+v", entries)
	}

	read := runner.Call(context.Background(), Call{
		Name:          FileRead,
		Args:          json.RawMessage(`{"scope":"workspace","path":"` + filepath.ToSlash(notePath) + `"}`),
		WorkspaceDirs: []string{root},
	})
	if !read.Ok {
		t.Fatalf("workspace read should succeed: %+v", read)
	}
	readPayload := decodeToolResult(t, read)
	if readPayload["content"] != "hello" || readPayload["path"] != resolvedNotePath {
		t.Fatalf("unexpected workspace read payload: %+v", readPayload)
	}

	writePath := filepath.Join(root, "nested", "created.txt")
	write := runner.Call(context.Background(), Call{
		Name:          FileWrite,
		Args:          json.RawMessage(`{"scope":"workspace","path":"` + filepath.ToSlash(writePath) + `","content":"created"}`),
		WorkspaceDirs: []string{root},
	})
	if !write.Ok {
		t.Fatalf("workspace write should succeed: %+v", write)
	}
	data, err := os.ReadFile(writePath)
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != "created" {
		t.Fatalf("workspace write content = %q", data)
	}
}

func TestBuiltinFileCopyFileAndDirectory(t *testing.T) {
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "src", "nested"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "src", "note.txt"), []byte("hello"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "src", "nested", "deep.txt"), []byte("deep"), 0o600); err != nil {
		t.Fatal(err)
	}
	runner := NewBuiltinRunner(WithHomeDir(t.TempDir()))

	fileCopy := runner.Call(context.Background(), Call{
		Name:          FileCopy,
		Args:          json.RawMessage(`{"scope":"workspace","from_path":"src/note.txt","to_path":"copy/note.txt"}`),
		WorkspaceDirs: []string{root},
	})
	if !fileCopy.Ok {
		t.Fatalf("file copy should succeed: %+v", fileCopy)
	}
	data, err := os.ReadFile(filepath.Join(root, "copy", "note.txt"))
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != "hello" {
		t.Fatalf("copied file content = %q", data)
	}

	dirWithoutRecursive := runner.Call(context.Background(), Call{
		Name:          FileCopy,
		Args:          json.RawMessage(`{"scope":"workspace","from_path":"src","to_path":"copy/src"}`),
		WorkspaceDirs: []string{root},
	})
	if dirWithoutRecursive.Ok {
		t.Fatalf("directory copy without recursive should fail")
	}
	payload := decodeToolResult(t, dirWithoutRecursive)
	if payload["reason"] != "recursive_required" {
		t.Fatalf("unexpected recursive reason: %+v", payload)
	}

	dirCopy := runner.Call(context.Background(), Call{
		Name:          FileCopy,
		Args:          json.RawMessage(`{"scope":"workspace","from_path":"src","to_path":"copy/src","recursive":true}`),
		WorkspaceDirs: []string{root},
	})
	if !dirCopy.Ok {
		t.Fatalf("directory copy should succeed: %+v", dirCopy)
	}
	deep, err := os.ReadFile(filepath.Join(root, "copy", "src", "nested", "deep.txt"))
	if err != nil {
		t.Fatal(err)
	}
	if string(deep) != "deep" {
		t.Fatalf("copied nested content = %q", deep)
	}
}

func TestBuiltinFileWorkspaceRejectsOutsideRoot(t *testing.T) {
	root := t.TempDir()
	outside := filepath.Join(t.TempDir(), "note.txt")
	if err := os.WriteFile(outside, []byte("secret"), 0o600); err != nil {
		t.Fatal(err)
	}
	res := NewBuiltinRunner(WithHomeDir(t.TempDir())).Call(context.Background(), Call{
		Name:          FileRead,
		Args:          json.RawMessage(`{"scope":"workspace","path":"` + filepath.ToSlash(outside) + `"}`),
		WorkspaceDirs: []string{root},
	})
	if res.Ok {
		t.Fatalf("outside workspace read should fail: %+v", res)
	}
	payload := decodeToolResult(t, res)
	if payload["reason"] != "path_not_authorized" {
		t.Fatalf("unexpected reason: %+v", payload)
	}
}

func TestBuiltinFileWorkspaceRequiresDirs(t *testing.T) {
	res := NewBuiltinRunner(WithHomeDir(t.TempDir())).Call(context.Background(), Call{
		Name: FileList,
		Args: json.RawMessage(`{"scope":"workspace","path":"."}`),
	})
	if res.Ok {
		t.Fatalf("workspace list without dirs should fail: %+v", res)
	}
	payload := decodeToolResult(t, res)
	if payload["reason"] != "workspace_dirs_required" {
		t.Fatalf("unexpected reason: %+v", payload)
	}
}

func TestBuiltinFileStatSearchAndSlice(t *testing.T) {
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "docs"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(root, "node_modules", "pkg"), 0o755); err != nil {
		t.Fatal(err)
	}
	notePath := filepath.Join(root, "docs", "note.txt")
	content := strings.Join([]string{
		"first line",
		"alpha marker",
		"middle line",
		"omega marker",
		"last line",
	}, "\n")
	if err := os.WriteFile(notePath, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "node_modules", "pkg", "junk.txt"), []byte("marker should be skipped"), 0o600); err != nil {
		t.Fatal(err)
	}
	runner := NewBuiltinRunner(WithHomeDir(t.TempDir()))

	stat := runner.Call(context.Background(), Call{
		Name:          FileStat,
		Args:          json.RawMessage(`{"scope":"workspace","path":"` + filepath.ToSlash(notePath) + `"}`),
		WorkspaceDirs: []string{root},
	})
	if !stat.Ok {
		t.Fatalf("stat should succeed: %+v", stat)
	}
	statPayload := decodeToolResult(t, stat)
	if statPayload["exists"] != true || statPayload["type"] != "file" {
		t.Fatalf("unexpected stat payload: %+v", statPayload)
	}

	search := runner.Call(context.Background(), Call{
		Name:          FileSearch,
		Args:          json.RawMessage(`{"scope":"workspace","path":".","query":"marker"}`),
		WorkspaceDirs: []string{root},
	})
	if !search.Ok {
		t.Fatalf("search should succeed: %+v", search)
	}
	searchPayload := decodeToolResult(t, search)
	matches := searchPayload["matches"].([]any)
	if len(matches) != 2 {
		t.Fatalf("expected 2 matches outside skipped dirs, got %+v", matches)
	}

	slice := runner.Call(context.Background(), Call{
		Name:          FileSlice,
		Args:          json.RawMessage(`{"scope":"workspace","path":"docs/note.txt","start":2,"end":3}`),
		WorkspaceDirs: []string{root},
	})
	if !slice.Ok {
		t.Fatalf("slice should succeed: %+v", slice)
	}
	slicePayload := decodeToolResult(t, slice)
	if slicePayload["content"] != "alpha marker\nmiddle line" || slicePayload["numberedContent"] != "2: alpha marker\n3: middle line" {
		t.Fatalf("unexpected slice payload: %+v", slicePayload)
	}

	reverseTail := runner.Call(context.Background(), Call{
		Name:          FileSlice,
		Args:          json.RawMessage(`{"scope":"workspace","path":"docs/note.txt","origin":"end","lines":2,"order":"reverse"}`),
		WorkspaceDirs: []string{root},
	})
	if !reverseTail.Ok {
		t.Fatalf("reverse tail slice should succeed: %+v", reverseTail)
	}
	tailPayload := decodeToolResult(t, reverseTail)
	if tailPayload["content"] != "last line\nomega marker" || tailPayload["numberedContent"] != "5: last line\n4: omega marker" {
		t.Fatalf("unexpected tail payload: %+v", tailPayload)
	}
}

func TestBuiltinFileReadRejectsLargeText(t *testing.T) {
	home := t.TempDir()
	tempDir := filepath.Join(home, "temp")
	if err := os.MkdirAll(tempDir, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(tempDir, "large.txt"), []byte(strings.Repeat("x", maxFileReadWholeBytes+1)), 0o600); err != nil {
		t.Fatal(err)
	}
	res := NewBuiltinRunner(WithHomeDir(home)).Call(context.Background(), Call{
		Name: FileRead,
		Args: json.RawMessage(`{"scope":"temp","path":"large.txt"}`),
	})
	if res.Ok {
		t.Fatalf("large read should fail with guidance: %+v", res)
	}
	payload := decodeToolResult(t, res)
	if payload["reason"] != "file_too_large" || payload["hint"] == "" {
		t.Fatalf("unexpected large read payload: %+v", payload)
	}
}

func TestBuiltinFileReadRejectsBinaryBeforeLargeHint(t *testing.T) {
	home := t.TempDir()
	tempDir := filepath.Join(home, "temp")
	if err := os.MkdirAll(tempDir, 0o700); err != nil {
		t.Fatal(err)
	}
	data := append([]byte{0, 1, 2, 3}, []byte(strings.Repeat("x", maxFileReadWholeBytes+1))...)
	if err := os.WriteFile(filepath.Join(tempDir, "large.wav"), data, 0o600); err != nil {
		t.Fatal(err)
	}
	res := NewBuiltinRunner(WithHomeDir(home)).Call(context.Background(), Call{
		Name: FileRead,
		Args: json.RawMessage(`{"scope":"temp","path":"large.wav"}`),
	})
	if res.Ok {
		t.Fatalf("large binary read should fail: %+v", res)
	}
	payload := decodeToolResult(t, res)
	if payload["reason"] != "binary_file" {
		t.Fatalf("large binary should report binary_file, got %+v", payload)
	}
}

func TestBuiltinFileReadRoutesImageAttachment(t *testing.T) {
	home := t.TempDir()
	tempDir := filepath.Join(home, "temp")
	if err := os.MkdirAll(tempDir, 0o700); err != nil {
		t.Fatal(err)
	}
	imagePath := filepath.Join(tempDir, "image.png")
	imageBytes := []byte{0x89, 'P', 'N', 'G', '\r', '\n', 0x1a, '\n', 0, 0, 0, 0, 'I', 'E', 'N', 'D'}
	if err := os.WriteFile(imagePath, imageBytes, 0o600); err != nil {
		t.Fatal(err)
	}

	res := NewBuiltinRunner(WithHomeDir(home)).Call(context.Background(), Call{
		SessionID: "sess_img",
		Name:      FileRead,
		Args:      json.RawMessage(`{"scope":"temp","path":"image.png"}`),
	})
	if !res.Ok {
		t.Fatalf("image read should route attachment: %+v", res)
	}
	if len(res.Attachments) != 1 || res.Attachments[0].MIME != "image/png" || res.Attachments[0].Origin != attachment.OriginTool {
		t.Fatalf("unexpected routed attachment: %+v", res.Attachments)
	}
	if len(res.ContextAttachments) != 1 || res.ContextAttachments[0].AttachmentKey != res.Attachments[0].AttachmentKey {
		t.Fatalf("image read should route a context attachment: %+v", res.ContextAttachments)
	}
	payload := decodeToolResult(t, res)
	if payload["kind"] != "attachment_routed" || payload["attachmentKey"] == "" {
		t.Fatalf("unexpected image payload: %+v", payload)
	}
	storedPath, ok, err := attachment.NewService(home).Path("sess_img", res.Attachments[0].AttachmentKey)
	if err != nil || !ok {
		t.Fatalf("stored attachment path missing: ok=%v err=%v", ok, err)
	}
	data, err := os.ReadFile(storedPath)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(data, imageBytes) {
		t.Fatalf("unexpected stored bytes: %x", data)
	}
}

package tool

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/teatak/pudding-core/internal/canvas"
	"github.com/teatak/pudding-core/internal/store"
)

type canvasFileStore struct{ id string }

func (s canvasFileStore) GetCanvas(_ context.Context, id string) (*store.Canvas, error) {
	if id != s.id {
		return nil, store.ErrNotFound
	}
	return &store.Canvas{ID: id}, nil
}

func TestCanvasFileScopeReadsAndPatchesOneDraftFile(t *testing.T) {
	const id = "canvas_scope_test"
	home := t.TempDir()
	draft, err := canvas.StartDraft(home, id, "")
	if err != nil {
		t.Fatal(err)
	}
	manifest := `{"schemaVersion":1}`
	draft, err = canvas.WriteDraftFile(home, id, "canvas.json", &manifest, draft.DraftHash)
	if err != nil {
		t.Fatal(err)
	}
	source := "export default 1;\n"
	draft, err = canvas.WriteDraftFile(home, id, "src/App.tsx", &source, draft.DraftHash)
	if err != nil {
		t.Fatal(err)
	}
	root, _ := canvas.DraftRoot(home, id)
	if err := os.MkdirAll(filepath.Join(root, "fixtures"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "fixtures", "old.json"), []byte(`{"secret":"fixture"}`), 0o600); err != nil {
		t.Fatal(err)
	}
	draft, err = canvas.ReadDraft(home, id)
	if err != nil {
		t.Fatal(err)
	}
	runner := NewBuiltinRunner(WithHomeDir(home), WithCanvasResources(canvasFileStore{id}))
	call := func(name, callID string, args any) Call {
		raw, err := json.Marshal(args)
		if err != nil {
			t.Fatal(err)
		}
		return Call{Name: name, CallID: callID, SessionID: "session_canvas", Mode: store.ModeCode, Args: raw}
	}
	list := runner.Call(context.Background(), call(FileList, "list", map[string]any{"scope": "canvas", "canvas_id": id, "path": "."}))
	if !list.Ok {
		t.Fatalf("list: %s", list.Content)
	}
	entries := decodeToolResult(t, list)["entries"].([]any)
	if len(entries) != 2 {
		t.Fatalf("list included private draft files: %s", list.Content)
	}
	read := runner.Call(context.Background(), call(FileRead, "read", map[string]any{"scope": "canvas", "canvas_id": id, "path": "src/App.tsx"}))
	if !read.Ok {
		t.Fatalf("read: %s", read.Content)
	}
	if payload := decodeToolResult(t, read); payload["content"] != source || payload["draftHash"] != draft.DraftHash {
		t.Fatalf("read payload: %v", payload)
	}
	for _, path := range []string{".base", "fixtures/old.json", "../draft/.base", "/etc/passwd"} {
		res := runner.Call(context.Background(), call(FileRead, "blocked", map[string]any{"scope": "canvas", "canvas_id": id, "path": path}))
		if res.Ok {
			t.Fatalf("read %q should fail", path)
		}
	}
	search := runner.Call(context.Background(), call(FileSearch, "search", map[string]any{"scope": "canvas", "canvas_id": id, "path": ".", "query": "fixture"}))
	if !search.Ok || decodeToolResult(t, search)["matchCount"] != float64(0) {
		t.Fatalf("search exposed fixture: %s", search.Content)
	}
	write := runner.Call(context.Background(), call(FileWrite, "write", map[string]any{"scope": "canvas", "canvas_id": id, "path": "src/App.tsx", "content": "bypass"}))
	if write.Ok {
		t.Fatalf("generic file write bypassed draft hash: %s", write.Content)
	}
	patchArgs := map[string]any{
		"scope": "canvas", "canvas_id": id, "expectedDraftHash": draft.DraftHash,
		"files": []any{map[string]any{"path": "src/App.tsx", "action": "edit", "hunks": []any{map[string]any{"start_line": 1, "old_lines": []string{"export default 1;"}, "new_lines": []string{"export default 2;"}}}}},
	}
	patchCall := call(FilePatch, "patch", patchArgs)
	approval, err := runner.ApprovalDetails(context.Background(), patchCall)
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := approval["projectRoot"]; ok {
		t.Fatalf("canvas approval exposed project root: %v", approval)
	}
	result := runner.Call(context.Background(), patchCall)
	if !result.Ok {
		t.Fatalf("patch: %s", result.Content)
	}
	updated, err := canvas.ReadDraft(home, id)
	if err != nil {
		t.Fatal(err)
	}
	if updated.Files["src/App.tsx"] != "export default 2;\n" || updated.Files["canvas.json"] != manifest {
		t.Fatalf("unexpected draft: %+v", updated.Files)
	}
	if payload := decodeToolResult(t, result); payload["draftHash"] != updated.DraftHash {
		t.Fatalf("missing new draft hash: %v", payload)
	}
	stale := call(FilePatch, "stale", patchArgs)
	if _, err := runner.ApprovalDetails(context.Background(), stale); err == nil {
		t.Fatal("stale hash should fail")
	} else if payload := decodeToolResult(t, ApprovalDetailsFailure(stale, err)); payload["currentDraftHash"] != updated.DraftHash {
		t.Fatalf("conflict should return current draft hash: %v", payload)
	}
	patchArgs["expectedDraftHash"] = updated.DraftHash
	patchArgs["files"] = []any{map[string]any{"path": ".base", "action": "delete"}}
	if _, err := runner.ApprovalDetails(context.Background(), call(FilePatch, "hidden", patchArgs)); err == nil {
		t.Fatal("metadata patch should fail")
	}
	patchArgs["files"] = []any{map[string]any{"path": "fixtures/old.json", "action": "delete"}}
	if _, err := runner.ApprovalDetails(context.Background(), call(FilePatch, "fixture", patchArgs)); err == nil {
		t.Fatal("fixture patch should fail")
	}
	chat := call(FileRead, "chat", map[string]any{"scope": "canvas", "canvas_id": id, "path": "canvas.json"})
	chat.Mode = store.ModeChat
	if runner.Call(context.Background(), chat).Ok {
		t.Fatal("Chat mode should not access canvas scope")
	}
}

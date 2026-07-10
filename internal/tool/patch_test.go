package tool

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

type patchProposalResultPayload struct {
	OK          bool                    `json:"ok"`
	Status      string                  `json:"status"`
	ProposalID  string                  `json:"proposalID"`
	ProjectRoot string                  `json:"projectRoot"`
	Files       []patchProposalFileView `json:"files"`
	FileCount   int                     `json:"fileCount"`
	Additions   int                     `json:"additions"`
	Deletions   int                     `json:"deletions"`
	Diff        string                  `json:"diff"`
}

func TestPatchProposalDoesNotWriteBeforeApply(t *testing.T) {
	root := t.TempDir()
	writePatchTestFile(t, filepath.Join(root, "update.txt"), "old line\n")
	writePatchTestFile(t, filepath.Join(root, "delete.txt"), "remove me\n")
	runner := NewBuiltinRunner()
	propose := patchTestCall(runner, "session_a", root, PatchPropose, map[string]any{
		"scope": "project",
		"files": []map[string]any{
			{"path": "update.txt", "new_text": "new line\n"},
			{"path": "nested/create.txt", "new_text": "created\n"},
			{"path": "delete.txt", "delete": true},
		},
	})
	payload := decodePatchPayload(t, propose)
	if !propose.Ok || !payload.OK || payload.Status != "proposed" || payload.ProposalID == "" || payload.FileCount != 3 {
		t.Fatalf("unexpected proposal result: result=%+v payload=%+v", propose, payload)
	}
	if !strings.Contains(payload.Diff, "diff --git a/update.txt b/update.txt") || !strings.Contains(payload.Diff, "-old line") || !strings.Contains(payload.Diff, "+new line") {
		t.Fatalf("update unified diff missing: %s", payload.Diff)
	}
	if !strings.Contains(payload.Diff, "--- /dev/null") || !strings.Contains(payload.Diff, "+++ b/nested/create.txt") {
		t.Fatalf("create unified diff missing: %s", payload.Diff)
	}
	if !strings.Contains(payload.Diff, "--- a/delete.txt") || !strings.Contains(payload.Diff, "+++ /dev/null") {
		t.Fatalf("delete unified diff missing: %s", payload.Diff)
	}
	if got := readPatchTestFile(t, filepath.Join(root, "update.txt")); got != "old line\n" {
		t.Fatalf("proposal changed update target: %q", got)
	}
	if _, err := os.Stat(filepath.Join(root, "nested", "create.txt")); !os.IsNotExist(err) {
		t.Fatalf("proposal created a file before approval: %v", err)
	}
	if got := readPatchTestFile(t, filepath.Join(root, "delete.txt")); got != "remove me\n" {
		t.Fatalf("proposal deleted content before approval: %q", got)
	}

	applyArgs, _ := json.Marshal(map[string]any{"proposal_id": payload.ProposalID})
	details, err := runner.ApprovalDetails(context.Background(), Call{
		SessionID:   "session_a",
		CallID:      "approval_details",
		Name:        PatchApply,
		Args:        applyArgs,
		ProjectDirs: []string{root},
	})
	if err != nil {
		t.Fatal(err)
	}
	if details["diff"] != payload.Diff || details["fileCount"] != 3 {
		t.Fatalf("approval details must carry the review diff: %+v", details)
	}
	paths, _ := details["paths"].([]string)
	if len(paths) != 3 {
		t.Fatalf("approval paths missing: %+v", details["paths"])
	}

	applied := patchTestCall(runner, "session_a", root, PatchApply, map[string]any{"proposal_id": payload.ProposalID})
	appliedPayload := decodePatchPayload(t, applied)
	if !applied.Ok || appliedPayload.Status != "applied" || appliedPayload.FileCount != 3 {
		t.Fatalf("unexpected apply result: result=%+v payload=%+v", applied, appliedPayload)
	}
	if got := readPatchTestFile(t, filepath.Join(root, "update.txt")); got != "new line\n" {
		t.Fatalf("update not applied: %q", got)
	}
	if got := readPatchTestFile(t, filepath.Join(root, "nested", "create.txt")); got != "created\n" {
		t.Fatalf("create not applied: %q", got)
	}
	if _, err := os.Stat(filepath.Join(root, "delete.txt")); !os.IsNotExist(err) {
		t.Fatalf("delete not applied: %v", err)
	}
	assertNoPatchTempFiles(t, root)

	again := patchTestCall(runner, "session_a", root, PatchApply, map[string]any{"proposal_id": payload.ProposalID})
	if again.Ok || !strings.Contains(again.Content, `"reason":"proposal_not_found"`) {
		t.Fatalf("applied proposal must be one-shot: %+v", again)
	}
}

func TestPatchProposalSupportsOrderedEdits(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "main.go")
	original := "package main\n\nfunc message() string {\n\treturn \"old\"\n}\n"
	writePatchTestFile(t, path, original)
	runner := NewBuiltinRunner()
	proposal := patchTestCall(runner, "session_a", root, PatchPropose, map[string]any{
		"scope": "project",
		"files": []map[string]any{{
			"path": "main.go",
			"edits": []map[string]any{
				{"old_text": "func message() string", "new_text": "func greeting() string"},
				{"old_text": "return \"old\"", "new_text": "return \"new\""},
			},
		}},
	})
	payload := decodePatchPayload(t, proposal)
	if !proposal.Ok || payload.FileCount != 1 || !strings.Contains(payload.Diff, "func greeting() string") {
		t.Fatalf("unexpected edit proposal: result=%+v payload=%+v", proposal, payload)
	}
	if got := readPatchTestFile(t, path); got != original {
		t.Fatalf("proposal changed file before apply: %q", got)
	}
	applied := patchTestCall(runner, "session_a", root, PatchApply, map[string]any{"proposal_id": payload.ProposalID})
	if !applied.Ok {
		t.Fatalf("edit proposal apply failed: %+v", applied)
	}
	if got := readPatchTestFile(t, path); !strings.Contains(got, "func greeting() string") || !strings.Contains(got, "return \"new\"") {
		t.Fatalf("ordered edits not applied: %q", got)
	}
}

func TestPatchProposalRejectsAmbiguousOrConflictingEdits(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "notes.txt")
	writePatchTestFile(t, path, "same\nsame\n")
	runner := NewBuiltinRunner()

	ambiguous := patchTestCall(runner, "session_a", root, PatchPropose, map[string]any{
		"scope": "project",
		"files": []map[string]any{{
			"path":  "notes.txt",
			"edits": []map[string]any{{"old_text": "same", "new_text": "changed"}},
		}},
	})
	if ambiguous.Ok || !strings.Contains(ambiguous.Content, `"reason":"edit_text_ambiguous"`) {
		t.Fatalf("ambiguous edit should fail: %+v", ambiguous)
	}

	conflicting := patchTestCall(runner, "session_a", root, PatchPropose, map[string]any{
		"scope": "project",
		"files": []map[string]any{{
			"path": "notes.txt",
			"edits": []map[string]any{
				{"old_text": "same", "new_text": "changed", "replace_all": true},
				{"old_text": "missing", "new_text": "never"},
			},
		}},
	})
	if conflicting.Ok || !strings.Contains(conflicting.Content, `"reason":"edit_text_not_found"`) {
		t.Fatalf("conflicting edit should fail: %+v", conflicting)
	}
	if got := readPatchTestFile(t, path); got != "same\nsame\n" {
		t.Fatalf("failed edits changed worktree: %q", got)
	}
}

func TestPatchProposalRequiresOneFileOperation(t *testing.T) {
	root := t.TempDir()
	writePatchTestFile(t, filepath.Join(root, "notes.txt"), "old\n")
	runner := NewBuiltinRunner()
	result := patchTestCall(runner, "session_a", root, PatchPropose, map[string]any{
		"scope": "project",
		"files": []map[string]any{{
			"path":     "notes.txt",
			"new_text": "new\n",
			"edits":    []map[string]any{{"old_text": "old", "new_text": "new"}},
		}},
	})
	if result.Ok || !strings.Contains(result.Content, `"reason":"invalid_arguments"`) {
		t.Fatalf("multiple file operations should fail: %+v", result)
	}
}

func TestPatchApplyRejectsDriftWithoutPartialWrites(t *testing.T) {
	root := t.TempDir()
	first := filepath.Join(root, "first.txt")
	second := filepath.Join(root, "second.txt")
	writePatchTestFile(t, first, "first old\n")
	writePatchTestFile(t, second, "second old\n")
	runner := NewBuiltinRunner()
	proposal := decodePatchPayload(t, patchTestCall(runner, "session_a", root, PatchPropose, map[string]any{
		"scope": "project",
		"files": []map[string]any{
			{"path": "first.txt", "new_text": "first new\n"},
			{"path": "second.txt", "new_text": "second new\n"},
		},
	}))
	writePatchTestFile(t, second, "external edit\n")

	apply := patchTestCall(runner, "session_a", root, PatchApply, map[string]any{"proposal_id": proposal.ProposalID})
	if apply.Ok || !strings.Contains(apply.Content, `"reason":"proposal_stale"`) {
		t.Fatalf("stale proposal should fail: %+v", apply)
	}
	if got := readPatchTestFile(t, first); got != "first old\n" {
		t.Fatalf("first file was partially applied: %q", got)
	}
	if got := readPatchTestFile(t, second); got != "external edit\n" {
		t.Fatalf("external edit was overwritten: %q", got)
	}
	assertNoPatchTempFiles(t, root)
}

func TestRollbackPatchItemsRestoresWorktree(t *testing.T) {
	root := t.TempDir()
	existing := filepath.Join(root, "existing.txt")
	backup := filepath.Join(root, ".pudding-patch-backup-test")
	createdDir := filepath.Join(root, "nested")
	created := filepath.Join(createdDir, "created.txt")
	staged := filepath.Join(root, ".pudding-patch-stage-test")
	writePatchTestFile(t, existing, "new content\n")
	writePatchTestFile(t, backup, "old content\n")
	writePatchTestFile(t, created, "created content\n")
	writePatchTestFile(t, staged, "staged content\n")

	items := []patchApplyItem{
		{file: patchProposalFile{Target: existing}, backupPath: backup, installed: true},
		{file: patchProposalFile{Target: created}, installed: true},
		{tempPath: staged},
	}
	if err := rollbackPatchItems(items, map[string]bool{createdDir: true}); err != nil {
		t.Fatal(err)
	}
	if got := readPatchTestFile(t, existing); got != "old content\n" {
		t.Fatalf("existing file was not restored: %q", got)
	}
	if _, err := os.Stat(created); !os.IsNotExist(err) {
		t.Fatalf("created file survived rollback: %v", err)
	}
	if _, err := os.Stat(staged); !os.IsNotExist(err) {
		t.Fatalf("staged file survived rollback: %v", err)
	}
	if _, err := os.Stat(createdDir); !os.IsNotExist(err) {
		t.Fatalf("created directory survived rollback: %v", err)
	}
	assertNoPatchTempFiles(t, root)
}

func TestPatchProposalIsSessionScoped(t *testing.T) {
	root := t.TempDir()
	writePatchTestFile(t, filepath.Join(root, "notes.txt"), "old\n")
	runner := NewBuiltinRunner()
	proposal := decodePatchPayload(t, patchTestCall(runner, "session_a", root, PatchPropose, map[string]any{
		"scope": "project",
		"files": []map[string]any{{"path": "notes.txt", "new_text": "new\n"}},
	}))
	apply := patchTestCall(runner, "session_b", root, PatchApply, map[string]any{"proposal_id": proposal.ProposalID})
	if apply.Ok || !strings.Contains(apply.Content, `"reason":"proposal_not_found"`) {
		t.Fatalf("proposal crossed session boundary: %+v", apply)
	}
	if got := readPatchTestFile(t, filepath.Join(root, "notes.txt")); got != "old\n" {
		t.Fatalf("cross-session apply changed file: %q", got)
	}
}

func TestPatchProposalRejectsCrossRootAndNoop(t *testing.T) {
	firstRoot := t.TempDir()
	secondRoot := t.TempDir()
	first := filepath.Join(firstRoot, "first.txt")
	second := filepath.Join(secondRoot, "second.txt")
	writePatchTestFile(t, first, "same\n")
	writePatchTestFile(t, second, "old\n")
	runner := NewBuiltinRunner()

	noop := patchTestCallWithRoots(runner, "session_a", []string{firstRoot}, PatchPropose, map[string]any{
		"scope": "project",
		"files": []map[string]any{{"path": "first.txt", "new_text": "same\n"}},
	})
	if noop.Ok || !strings.Contains(noop.Content, `"reason":"no_changes"`) {
		t.Fatalf("no-op proposal should fail: %+v", noop)
	}

	crossRoot := patchTestCallWithRoots(runner, "session_a", []string{firstRoot, secondRoot}, PatchPropose, map[string]any{
		"scope": "project",
		"files": []map[string]any{
			{"path": first, "new_text": "first new\n"},
			{"path": second, "new_text": "second new\n"},
		},
	})
	if crossRoot.Ok || !strings.Contains(crossRoot.Content, `"reason":"cross_root_patch"`) {
		t.Fatalf("cross-root proposal should fail: %+v", crossRoot)
	}
}

func patchTestCall(runner *BuiltinRunner, sessionID, root, name string, args map[string]any) Result {
	return patchTestCallWithRoots(runner, sessionID, []string{root}, name, args)
}

func patchTestCallWithRoots(runner *BuiltinRunner, sessionID string, roots []string, name string, args map[string]any) Result {
	raw, _ := json.Marshal(args)
	return runner.Call(context.Background(), Call{
		SessionID:   sessionID,
		TurnID:      "turn_patch",
		CallID:      "call_patch",
		Name:        name,
		Args:        raw,
		ProjectDirs: roots,
	})
}

func decodePatchPayload(t *testing.T, result Result) patchProposalResultPayload {
	t.Helper()
	var payload patchProposalResultPayload
	if err := json.Unmarshal([]byte(result.Content), &payload); err != nil {
		t.Fatalf("decode patch result: %v content=%q", err, result.Content)
	}
	return payload
}

func writePatchTestFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
}

func readPatchTestFile(t *testing.T, path string) string {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}

func assertNoPatchTempFiles(t *testing.T, root string) {
	t.Helper()
	err := filepath.WalkDir(root, func(path string, entry os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if strings.HasPrefix(entry.Name(), ".pudding-patch-") {
			t.Errorf("patch temp file was not cleaned: %s", path)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}

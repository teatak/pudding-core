package tool

import (
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

type gitStatusPayload struct {
	OK              bool            `json:"ok"`
	Head            string          `json:"head"`
	Branch          string          `json:"branch"`
	Clean           bool            `json:"clean"`
	Files           []gitStatusFile `json:"files"`
	FileCount       int             `json:"fileCount"`
	StagedCount     int             `json:"stagedCount"`
	UnstagedCount   int             `json:"unstagedCount"`
	UntrackedCount  int             `json:"untrackedCount"`
	ConflictedCount int             `json:"conflictedCount"`
}

type gitDiffPayload struct {
	OK        bool          `json:"ok"`
	Staged    bool          `json:"staged"`
	Diff      string        `json:"diff"`
	Truncated bool          `json:"truncated"`
	Files     []gitDiffFile `json:"files"`
	FileCount int           `json:"fileCount"`
	Additions int           `json:"additions"`
	Deletions int           `json:"deletions"`
}

type gitLogPayload struct {
	OK      bool        `json:"ok"`
	Commits []gitCommit `json:"commits"`
	Count   int         `json:"count"`
}

type gitWritePayload struct {
	OK              bool      `json:"ok"`
	Status          string    `json:"status"`
	Commit          gitCommit `json:"commit"`
	Paths           []string  `json:"paths"`
	StagedCount     int       `json:"stagedCount"`
	UnstagedCount   int       `json:"unstagedCount"`
	UntrackedCount  int       `json:"untrackedCount"`
	ConflictedCount int       `json:"conflictedCount"`
}

func TestGitReadToolsReturnStructuredRepositoryData(t *testing.T) {
	root := newGitTestRepository(t, true)
	if err := os.WriteFile(filepath.Join(root, "tracked.txt"), []byte("one\ntwo\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "untracked.txt"), []byte("new\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	status := decodeGitPayload[gitStatusPayload](t, gitTestCall(root, GitStatus, map[string]any{"scope": "project"}))
	if !status.OK || status.Clean || status.Head == "" || status.Branch == "" || status.FileCount != 2 || status.UnstagedCount != 1 || status.UntrackedCount != 1 {
		t.Fatalf("unexpected git status: %+v", status)
	}
	if file := findGitStatusFile(status.Files, "tracked.txt"); file == nil || file.Kind != "modified" || file.WorktreeStatus != "M" {
		t.Fatalf("tracked modification missing from status: %+v", status.Files)
	}
	if file := findGitStatusFile(status.Files, "untracked.txt"); file == nil || file.Kind != "untracked" {
		t.Fatalf("untracked file missing from status: %+v", status.Files)
	}

	diff := decodeGitPayload[gitDiffPayload](t, gitTestCall(root, GitDiff, map[string]any{"scope": "project"}))
	if !diff.OK || diff.Staged || diff.Truncated || diff.FileCount != 1 || diff.Additions != 1 || diff.Deletions != 0 || !strings.Contains(diff.Diff, "+two") {
		t.Fatalf("unexpected git diff: %+v", diff)
	}
	if len(diff.Files) != 1 || diff.Files[0].Path != "tracked.txt" || diff.Files[0].Additions != 1 {
		t.Fatalf("unexpected git diff files: %+v", diff.Files)
	}

	log := decodeGitPayload[gitLogPayload](t, gitTestCall(root, GitLog, map[string]any{"scope": "project", "limit": 1}))
	if !log.OK || log.Count != 1 || len(log.Commits) != 1 || log.Commits[0].Hash == "" || log.Commits[0].Subject != "initial commit" {
		t.Fatalf("unexpected git log: %+v", log)
	}

	runGitTest(t, root, "add", "tracked.txt")
	staged := decodeGitPayload[gitDiffPayload](t, gitTestCall(root, GitDiff, map[string]any{"scope": "project", "staged": true}))
	if !staged.OK || !staged.Staged || staged.FileCount != 1 || !strings.Contains(staged.Diff, "+two") {
		t.Fatalf("unexpected staged diff: %+v", staged)
	}
}

func TestGitStatusAndDiffParseRename(t *testing.T) {
	root := newGitTestRepository(t, true)
	runGitTest(t, root, "mv", "tracked.txt", "renamed.txt")

	status := decodeGitPayload[gitStatusPayload](t, gitTestCall(root, GitStatus, map[string]any{"scope": "project"}))
	if status.FileCount != 1 || status.Files[0].Path != "renamed.txt" || status.Files[0].OriginalPath != "tracked.txt" || status.Files[0].Kind != "renamed" {
		t.Fatalf("unexpected rename status: %+v", status.Files)
	}
	diff := decodeGitPayload[gitDiffPayload](t, gitTestCall(root, GitDiff, map[string]any{"scope": "project", "staged": true}))
	if diff.FileCount != 1 || diff.Files[0].Path != "renamed.txt" || diff.Files[0].OriginalPath != "tracked.txt" {
		t.Fatalf("unexpected rename diff: %+v", diff.Files)
	}
}

func TestGitDiffTruncatesPatchButKeepsStatistics(t *testing.T) {
	root := newGitTestRepository(t, true)
	large := strings.Repeat("large changed line\n", 12000)
	if err := os.WriteFile(filepath.Join(root, "tracked.txt"), []byte(large), 0o600); err != nil {
		t.Fatal(err)
	}
	diff := decodeGitPayload[gitDiffPayload](t, gitTestCall(root, GitDiff, map[string]any{"scope": "project"}))
	if !diff.OK || !diff.Truncated || diff.FileCount != 1 || diff.Additions != 12000 || diff.Deletions != 1 || !strings.Contains(diff.Diff, "output truncated") {
		t.Fatalf("large diff should be truncated with complete statistics: %+v", diff)
	}
}

func TestGitReadRejectsRepositoryAboveAuthorizedProject(t *testing.T) {
	root := newGitTestRepository(t, true)
	subdir := filepath.Join(root, "subdir")
	if err := os.Mkdir(subdir, 0o700); err != nil {
		t.Fatal(err)
	}
	res := gitTestCall(subdir, GitStatus, map[string]any{"scope": "project"})
	if res.Ok || !strings.Contains(res.Content, `"reason":"repository_outside_project"`) {
		t.Fatalf("repository above authorized root must be rejected: %+v", res)
	}
}

func TestGitLogAllowsRepositoryWithoutCommits(t *testing.T) {
	root := newGitTestRepository(t, false)
	log := decodeGitPayload[gitLogPayload](t, gitTestCall(root, GitLog, map[string]any{"scope": "project"}))
	if !log.OK || log.Count != 0 || len(log.Commits) != 0 {
		t.Fatalf("empty repository should return an empty log: %+v", log)
	}
}

func TestGitStageAndUnstageExplicitFiles(t *testing.T) {
	root := newGitTestRepository(t, true)
	tracked := filepath.Join(root, "tracked.txt")
	untracked := filepath.Join(root, "untracked.txt")
	if err := os.WriteFile(tracked, []byte("changed\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(untracked, []byte("new\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	runner := NewBuiltinRunner()
	stageCall := newGitToolCall("session_a", "stage_call", root, GitStage, map[string]any{
		"scope": "project",
		"paths": []string{"untracked.txt", "tracked.txt", "tracked.txt"},
	})
	details, err := runner.ApprovalDetails(context.Background(), stageCall)
	if err != nil {
		t.Fatal(err)
	}
	paths, _ := details["paths"].([]string)
	if len(paths) != 2 || paths[0] != "tracked.txt" || paths[1] != "untracked.txt" {
		t.Fatalf("unexpected normalized approval paths: %+v", details)
	}
	staged := decodeGitPayload[gitWritePayload](t, runner.Call(context.Background(), stageCall))
	if !staged.OK || staged.Status != "staged" || staged.StagedCount != 2 || staged.UnstagedCount != 0 || staged.UntrackedCount != 0 {
		t.Fatalf("unexpected stage result: %+v", staged)
	}

	unstageCall := newGitToolCall("session_a", "unstage_call", root, GitUnstage, map[string]any{
		"scope": "project",
		"paths": []string{"tracked.txt", "untracked.txt"},
	})
	unstaged := decodeGitPayload[gitWritePayload](t, runner.Call(context.Background(), unstageCall))
	if !unstaged.OK || unstaged.Status != "unstaged" || unstaged.StagedCount != 0 || unstaged.UnstagedCount != 1 || unstaged.UntrackedCount != 1 {
		t.Fatalf("unexpected unstage result: %+v", unstaged)
	}
	if got := readPatchTestFile(t, tracked); got != "changed\n" {
		t.Fatalf("unstage changed tracked worktree content: %q", got)
	}
	if got := readPatchTestFile(t, untracked); got != "new\n" {
		t.Fatalf("unstage removed untracked worktree content: %q", got)
	}
}

func TestGitStageDeletionAndUnstageInUnbornRepository(t *testing.T) {
	root := newGitTestRepository(t, true)
	tracked := filepath.Join(root, "tracked.txt")
	if err := os.Remove(tracked); err != nil {
		t.Fatal(err)
	}
	runner := NewBuiltinRunner()
	deleted := decodeGitPayload[gitWritePayload](t, runner.Call(context.Background(), newGitToolCall("session_a", "stage_delete", root, GitStage, map[string]any{
		"scope": "project",
		"paths": []string{"tracked.txt"},
	})))
	if !deleted.OK || deleted.StagedCount != 1 || deleted.UnstagedCount != 0 {
		t.Fatalf("tracked deletion was not staged: %+v", deleted)
	}

	unborn := newGitTestRepository(t, false)
	newFile := filepath.Join(unborn, "new.txt")
	if err := os.WriteFile(newFile, []byte("new\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	unbornRunner := NewBuiltinRunner()
	if result := unbornRunner.Call(context.Background(), newGitToolCall("session_a", "stage_unborn", unborn, GitStage, map[string]any{
		"scope": "project",
		"paths": []string{"new.txt"},
	})); !result.Ok {
		t.Fatalf("stage in unborn repository failed: %+v", result)
	}
	unstaged := decodeGitPayload[gitWritePayload](t, unbornRunner.Call(context.Background(), newGitToolCall("session_a", "unstage_unborn", unborn, GitUnstage, map[string]any{
		"scope": "project",
		"paths": []string{"new.txt"},
	})))
	if !unstaged.OK || unstaged.StagedCount != 0 || unstaged.UntrackedCount != 1 {
		t.Fatalf("unstage in unborn repository failed: %+v", unstaged)
	}
	if got := readPatchTestFile(t, newFile); got != "new\n" {
		t.Fatalf("unborn unstage changed worktree content: %q", got)
	}
}

func TestGitCommitRequiresReviewedStableIndex(t *testing.T) {
	root := newGitTestRepository(t, true)
	if err := os.WriteFile(filepath.Join(root, "tracked.txt"), []byte("reviewed\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	runner := NewBuiltinRunner()
	stage := newGitToolCall("session_a", "stage_call", root, GitStage, map[string]any{
		"scope": "project",
		"paths": []string{"tracked.txt"},
	})
	if result := runner.Call(context.Background(), stage); !result.Ok {
		t.Fatalf("stage failed: %+v", result)
	}
	commit := newGitToolCall("session_a", "commit_call", root, GitCommit, map[string]any{
		"scope":   "project",
		"message": "reviewed commit",
	})
	details, err := runner.ApprovalDetails(context.Background(), commit)
	if err != nil {
		t.Fatal(err)
	}
	if details["commitMessage"] != "reviewed commit" || details["fileCount"] != 1 || !strings.Contains(details["diff"].(string), "+reviewed") {
		t.Fatalf("commit approval must contain staged summary and diff: %+v", details)
	}
	committed := decodeGitPayload[gitWritePayload](t, runner.Call(context.Background(), commit))
	if !committed.OK || committed.Status != "committed" || committed.Commit.Subject != "reviewed commit" || committed.StagedCount != 0 {
		t.Fatalf("unexpected commit result: %+v", committed)
	}

	again := runner.Call(context.Background(), commit)
	if again.Ok || !strings.Contains(again.Content, `"reason":"commit_approval_snapshot_required"`) {
		t.Fatalf("commit approval snapshot must be one-shot: %+v", again)
	}
}

func TestGitCommitRejectsIndexDriftAfterApproval(t *testing.T) {
	root := newGitTestRepository(t, true)
	tracked := filepath.Join(root, "tracked.txt")
	if err := os.WriteFile(tracked, []byte("first staged\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	runGitTest(t, root, "add", "tracked.txt")
	runner := NewBuiltinRunner()
	commit := newGitToolCall("session_a", "commit_drift", root, GitCommit, map[string]any{
		"scope":   "project",
		"message": "must not commit drift",
	})
	if _, err := runner.ApprovalDetails(context.Background(), commit); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(tracked, []byte("second staged\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	runGitTest(t, root, "add", "tracked.txt")
	result := runner.Call(context.Background(), commit)
	if result.Ok || !strings.Contains(result.Content, `"reason":"commit_snapshot_stale"`) {
		t.Fatalf("index drift must reject commit: %+v", result)
	}
	log := decodeGitPayload[gitLogPayload](t, gitTestCall(root, GitLog, map[string]any{"scope": "project", "limit": 1}))
	if log.Commits[0].Subject != "initial commit" {
		t.Fatalf("drifted index was committed: %+v", log.Commits[0])
	}
}

func TestGitWriteDisablesRepositoryFiltersAndCommitHooks(t *testing.T) {
	root := newGitTestRepository(t, true)
	filterMarker := filepath.Join(root, "filter-ran")
	hookMarker := filepath.Join(root, "hook-ran")
	runGitTest(t, root, "config", "filter.pudding.clean", "touch "+filterMarker+"; cat")
	runGitTest(t, root, "config", "filter.pudding.required", "true")
	if err := os.WriteFile(filepath.Join(root, ".gitattributes"), []byte("tracked.txt filter=pudding\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "tracked.txt"), []byte("safe content\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	hook := filepath.Join(root, ".git", "hooks", "pre-commit")
	if err := os.WriteFile(hook, []byte("#!/bin/sh\ntouch \""+hookMarker+"\"\nexit 1\n"), 0o700); err != nil {
		t.Fatal(err)
	}
	runner := NewBuiltinRunner()
	stage := newGitToolCall("session_a", "safe_stage", root, GitStage, map[string]any{
		"scope": "project",
		"paths": []string{"tracked.txt"},
	})
	if result := runner.Call(context.Background(), stage); !result.Ok {
		t.Fatalf("safe stage failed: %+v", result)
	}
	if _, err := os.Stat(filterMarker); !os.IsNotExist(err) {
		t.Fatalf("repository clean filter executed: %v", err)
	}
	commit := newGitToolCall("session_a", "safe_commit", root, GitCommit, map[string]any{
		"scope":   "project",
		"message": "hooks disabled",
	})
	if _, err := runner.ApprovalDetails(context.Background(), commit); err != nil {
		t.Fatal(err)
	}
	if result := runner.Call(context.Background(), commit); !result.Ok {
		t.Fatalf("commit with disabled hooks failed: %+v", result)
	}
	if _, err := os.Stat(hookMarker); !os.IsNotExist(err) {
		t.Fatalf("repository commit hook executed: %v", err)
	}
}

func TestGitWriteRejectsMetadataOutsideProject(t *testing.T) {
	mainRoot := newGitTestRepository(t, true)
	projectParent := t.TempDir()
	linked := filepath.Join(projectParent, "linked")
	runGitTest(t, mainRoot, "worktree", "add", "-b", "linked-test", linked)
	result := gitTestCall(linked, GitStage, map[string]any{
		"scope": "project",
		"paths": []string{"tracked.txt"},
	})
	if result.Ok || !strings.Contains(result.Content, `"reason":"git_metadata_outside_project"`) {
		t.Fatalf("Git metadata outside the Project must be rejected: %+v", result)
	}
}

func newGitTestRepository(t *testing.T, commit bool) string {
	t.Helper()
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git is unavailable")
	}
	root := t.TempDir()
	runGitTest(t, root, "init")
	runGitTest(t, root, "config", "user.name", "Pudding Test")
	runGitTest(t, root, "config", "user.email", "pudding@example.test")
	runGitTest(t, root, "config", "commit.gpgsign", "false")
	if !commit {
		return root
	}
	if err := os.WriteFile(filepath.Join(root, "tracked.txt"), []byte("one\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	runGitTest(t, root, "add", "tracked.txt")
	runGitTest(t, root, "commit", "-m", "initial commit")
	return root
}

func runGitTest(t *testing.T, dir string, args ...string) {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	if output, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git %s: %v\n%s", strings.Join(args, " "), err, output)
	}
}

func gitTestCall(root, name string, args map[string]any) Result {
	return NewBuiltinRunner().Call(context.Background(), newGitToolCall("", "call_git", root, name, args))
}

func newGitToolCall(sessionID, callID, root, name string, args map[string]any) Call {
	raw, _ := json.Marshal(args)
	return Call{
		SessionID:   sessionID,
		CallID:      callID,
		Name:        name,
		Args:        raw,
		ProjectDirs: []string{root},
	}
}

func decodeGitPayload[T any](t *testing.T, res Result) T {
	t.Helper()
	var payload T
	if err := json.Unmarshal([]byte(res.Content), &payload); err != nil {
		t.Fatalf("decode git payload: %v content=%q", err, res.Content)
	}
	return payload
}

func findGitStatusFile(files []gitStatusFile, path string) *gitStatusFile {
	for i := range files {
		if files[i].Path == path {
			return &files[i]
		}
	}
	return nil
}

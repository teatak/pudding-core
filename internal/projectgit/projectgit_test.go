package projectgit

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

func TestStatusAndFileDiff(t *testing.T) {
	requireGit(t)
	root := newRepository(t)
	writeFile(t, root, "tracked.txt", "base\n")
	git(t, root, "add", "tracked.txt")
	git(t, root, "commit", "-m", "initial")

	writeFile(t, root, "tracked.txt", "working\n")
	writeFile(t, root, "untracked.txt", "new\n")
	repo, err := Discover(context.Background(), root)
	if err != nil {
		t.Fatal(err)
	}
	status, err := ReadStatus(context.Background(), repo)
	if err != nil {
		t.Fatal(err)
	}
	if status.Branch == "" || status.Unstaged != 1 || status.Untracked != 1 || len(status.Files) != 2 {
		t.Fatalf("unexpected status: %+v", status)
	}
	diff, err := ReadFileDiff(context.Background(), repo, "tracked.txt", false, 1<<20)
	if err != nil {
		t.Fatal(err)
	}
	if diff.OldContent != "base\n" || diff.NewContent != "working\n" || diff.Staged {
		t.Fatalf("unexpected working tree diff: %+v", diff)
	}
	untracked, err := ReadFileDiff(context.Background(), repo, "untracked.txt", false, 1<<20)
	if err != nil {
		t.Fatal(err)
	}
	if untracked.OldContent != "" || untracked.NewContent != "new\n" {
		t.Fatalf("unexpected untracked diff: %+v", untracked)
	}

	git(t, root, "add", "tracked.txt")
	staged, err := ReadFileDiff(context.Background(), repo, "tracked.txt", true, 1<<20)
	if err != nil {
		t.Fatal(err)
	}
	if staged.OldContent != "base\n" || staged.NewContent != "working\n" || !staged.Staged {
		t.Fatalf("unexpected staged diff: %+v", staged)
	}
}

func TestDiscoverRejectsRepositoryOutsideProjectRoot(t *testing.T) {
	requireGit(t)
	root := newRepository(t)
	subdir := filepath.Join(root, "nested")
	if err := os.Mkdir(subdir, 0o755); err != nil {
		t.Fatal(err)
	}
	_, err := Discover(context.Background(), subdir)
	if ErrorCode(err) != CodeRepositoryOutsideRoot {
		t.Fatalf("error = %v, code = %q", err, ErrorCode(err))
	}
}

func TestStatusParsesRenameAndStagedDeletion(t *testing.T) {
	requireGit(t)
	root := newRepository(t)
	writeFile(t, root, "before.txt", "content\n")
	writeFile(t, root, "delete.txt", "delete\n")
	git(t, root, "add", ".")
	git(t, root, "commit", "-m", "initial")
	git(t, root, "mv", "before.txt", "after.txt")
	if err := os.Remove(filepath.Join(root, "delete.txt")); err != nil {
		t.Fatal(err)
	}
	git(t, root, "add", "delete.txt")

	repo, err := Discover(context.Background(), root)
	if err != nil {
		t.Fatal(err)
	}
	status, err := ReadStatus(context.Background(), repo)
	if err != nil {
		t.Fatal(err)
	}
	if status.Staged != 2 {
		t.Fatalf("staged count = %d, files = %+v", status.Staged, status.Files)
	}
	var renamed bool
	for _, file := range status.Files {
		if file.Path == "after.txt" && file.OriginalPath == "before.txt" && file.Kind == "renamed" {
			renamed = true
		}
	}
	if !renamed {
		t.Fatalf("rename not found: %+v", status.Files)
	}
	deleted, err := ReadFileDiff(context.Background(), repo, "delete.txt", true, 1<<20)
	if err != nil {
		t.Fatal(err)
	}
	if deleted.OldContent != "delete\n" || deleted.NewContent != "" {
		t.Fatalf("unexpected deleted diff: %+v", deleted)
	}
}

func TestInitializeStageUnstageDiscardAndCommit(t *testing.T) {
	requireGit(t)
	ctx := context.Background()
	root := t.TempDir()
	repo, err := Initialize(ctx, root)
	if err != nil {
		t.Fatal(err)
	}
	git(t, root, "config", "user.name", "Pudding Test")
	git(t, root, "config", "user.email", "pudding@example.test")
	writeFile(t, root, "tracked.txt", "base\n")
	status, err := Stage(ctx, repo, []string{"tracked.txt"})
	if err != nil || status.Staged != 1 {
		t.Fatalf("stage initial file: status=%+v err=%v", status, err)
	}
	if _, err := Commit(ctx, repo, "initial", false); err != nil {
		t.Fatal(err)
	}

	writeFile(t, root, "tracked.txt", "staged\n")
	writeFile(t, root, "untracked.txt", "new\n")
	status, err = Stage(ctx, repo, []string{"tracked.txt", "untracked.txt"})
	if err != nil || status.Staged != 2 {
		t.Fatalf("stage changes: status=%+v err=%v", status, err)
	}
	status, err = Unstage(ctx, repo, []string{"untracked.txt"})
	if err != nil || status.Staged != 1 || status.Untracked != 1 {
		t.Fatalf("unstage file: status=%+v err=%v", status, err)
	}
	writeFile(t, root, "tracked.txt", "working\n")
	status, err = Discard(ctx, repo, []string{"tracked.txt", "untracked.txt"})
	if err != nil {
		t.Fatal(err)
	}
	content, err := os.ReadFile(filepath.Join(root, "tracked.txt"))
	if err != nil || string(content) != "staged\n" {
		t.Fatalf("discard should restore index content: %q err=%v", content, err)
	}
	if _, err := os.Stat(filepath.Join(root, "untracked.txt")); !os.IsNotExist(err) {
		t.Fatalf("untracked file should be removed: %v", err)
	}
	if status.Staged != 1 || status.Unstaged != 0 || status.Untracked != 0 {
		t.Fatalf("unexpected status after discard: %+v", status)
	}
	status, err = Commit(ctx, repo, "update tracked", false)
	if err != nil || !statusIsClean(status) {
		t.Fatalf("commit changes: status=%+v err=%v", status, err)
	}
}

func TestSmartCommitPublishAndSync(t *testing.T) {
	requireGit(t)
	ctx := context.Background()
	root := newRepository(t)
	remote := filepath.Join(t.TempDir(), "remote.git")
	git(t, filepath.Dir(remote), "init", "--bare", "--quiet", remote)
	git(t, root, "remote", "add", "origin", remote)
	writeFile(t, root, "first.txt", "first\n")

	repo, err := Discover(ctx, root)
	if err != nil {
		t.Fatal(err)
	}
	status, err := Commit(ctx, repo, "initial", true)
	if err != nil || !statusIsClean(status) {
		t.Fatalf("smart commit: status=%+v err=%v", status, err)
	}
	status, err = Publish(ctx, repo)
	if err != nil || status.Upstream == "" || status.Ahead != 0 {
		t.Fatalf("publish: status=%+v err=%v", status, err)
	}

	writeFile(t, root, "second.txt", "second\n")
	status, err = Commit(ctx, repo, "second", true)
	if err != nil || status.Ahead != 1 {
		t.Fatalf("second smart commit: status=%+v err=%v", status, err)
	}
	status, err = Sync(ctx, repo)
	if err != nil || status.Ahead != 0 || status.Behind != 0 {
		t.Fatalf("sync: status=%+v err=%v", status, err)
	}
	primaryBranch := status.Branch
	git(t, root, "switch", "-c", "remote-only")
	git(t, root, "push", "origin", "remote-only")
	git(t, root, "switch", primaryBranch)
	git(t, root, "branch", "-d", "remote-only")
	status, err = SwitchBranch(ctx, repo, "origin/remote-only")
	if err != nil || status.Branch != "remote-only" || status.Upstream != "origin/remote-only" {
		t.Fatalf("switch remote branch: status=%+v err=%v", status, err)
	}
}

func TestBranchManagement(t *testing.T) {
	requireGit(t)
	ctx := context.Background()
	root := newRepository(t)
	writeFile(t, root, "tracked.txt", "base\n")
	git(t, root, "add", "tracked.txt")
	git(t, root, "commit", "-m", "initial")
	repo, err := Discover(ctx, root)
	if err != nil {
		t.Fatal(err)
	}
	original, err := ReadStatus(ctx, repo)
	if err != nil {
		t.Fatal(err)
	}
	status, err := CreateBranch(ctx, repo, "feature/test")
	if err != nil || status.Branch != "feature/test" {
		t.Fatalf("create branch: status=%+v err=%v", status, err)
	}
	status, err = RenameCurrentBranch(ctx, repo, "feature/renamed")
	if err != nil || status.Branch != "feature/renamed" {
		t.Fatalf("rename branch: status=%+v err=%v", status, err)
	}
	status, err = SwitchBranch(ctx, repo, original.Branch)
	if err != nil || status.Branch != original.Branch {
		t.Fatalf("switch branch: status=%+v err=%v", status, err)
	}
	branches, err := ListBranches(ctx, repo)
	if err != nil || len(branches) != 2 {
		t.Fatalf("list branches: branches=%+v err=%v", branches, err)
	}
	if branches[0].Name != "feature/renamed" || branches[1].Name != original.Branch {
		t.Fatalf("branches should keep name order: %+v", branches)
	}
	status, err = DeleteBranch(ctx, repo, "feature/renamed")
	if err != nil || status.Branch != original.Branch {
		t.Fatalf("delete branch: status=%+v err=%v", status, err)
	}
	branches, err = ListBranches(ctx, repo)
	if err != nil || len(branches) != 1 {
		t.Fatalf("branches after delete: branches=%+v err=%v", branches, err)
	}
}

func statusIsClean(status Status) bool {
	return len(status.Files) == 0 && status.Staged == 0 && status.Unstaged == 0 && status.Untracked == 0 && status.Conflicted == 0
}

func requireGit(t *testing.T) {
	t.Helper()
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git unavailable")
	}
}

func newRepository(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	git(t, root, "init", "--quiet")
	git(t, root, "config", "user.name", "Pudding Test")
	git(t, root, "config", "user.email", "pudding@example.test")
	return root
}

func git(t *testing.T, root string, args ...string) {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = root
	if output, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git %v: %v: %s", args, err, output)
	}
}

func writeFile(t *testing.T, root, path, content string) {
	t.Helper()
	target := filepath.Join(root, filepath.FromSlash(path))
	if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(target, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

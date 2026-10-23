package projectgit

import (
	"context"
	"os"
	"path/filepath"
	"sync"
	"testing"
)

func reviewedRepository(t *testing.T) (Repository, Status) {
	t.Helper()
	root := newRepository(t)
	writeFile(t, root, "file.txt", "base\n")
	git(t, root, "add", ".")
	git(t, root, "commit", "-m", "base")
	writeFile(t, root, "file.txt", "reviewed\n")
	repo, err := Discover(context.Background(), root)
	if err != nil {
		t.Fatal(err)
	}
	status, err := ReadStatus(context.Background(), repo)
	if err != nil {
		t.Fatal(err)
	}
	return repo, status
}

func TestCommitRequestReplayPreservesLaterWorkAndConcurrentRetries(t *testing.T) {
	repo, before := reviewedRepository(t)
	request := MutationRequest{ID: "commit-one", Scope: "session", ExpectedRevision: before.Revision}
	var wg sync.WaitGroup
	results := make(chan Status, 4)
	for i := 0; i < 4; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			status, err := CommitOnce(context.Background(), repo, "Reviewed", true, request)
			if err != nil {
				t.Error(err)
				return
			}
			results <- status
		}()
	}
	wg.Wait()
	close(results)
	var first Status
	for status := range results {
		if first.Head == "" {
			first = status
		}
		if status.Head != first.Head {
			t.Fatal("duplicate commit")
		}
	}
	writeFile(t, repo.Root, "later.txt", "later work\n")
	// Re-discovering reads the persisted receipt, not a daemon-local result cache.
	fresh, err := Discover(context.Background(), repo.Root)
	if err != nil {
		t.Fatal(err)
	}
	replay, err := CommitOnce(context.Background(), fresh, "Reviewed", true, request)
	if err != nil || replay.Head != first.Head || replay.Untracked != 1 {
		t.Fatalf("replay=%+v err=%v", replay, err)
	}
	if _, err := CommitOnce(context.Background(), fresh, "Different", true, request); ErrorCode(err) != CodeRequestConflict {
		t.Fatalf("identity conflict: %v", err)
	}
	next := MutationRequest{ID: "commit-two", Scope: "session", ExpectedRevision: replay.Revision}
	second, err := CommitOnce(context.Background(), repo, "Reviewed", true, next)
	if err != nil || second.Head == first.Head || len(second.Files) != 0 {
		t.Fatalf("new intent=%+v err=%v", second, err)
	}
}

func TestReviewedCommitRejectsChangesBeforeFirstExecution(t *testing.T) {
	for _, change := range []string{"content", "new-file", "index", "branch", "head"} {
		t.Run(change, func(t *testing.T) {
			repo, before := reviewedRepository(t)
			switch change {
			case "content":
				writeFile(t, repo.Root, "file.txt", "changed again\n")
			case "new-file":
				writeFile(t, repo.Root, "later.txt", "new\n")
			case "index":
				git(t, repo.Root, "add", "file.txt")
			case "branch":
				git(t, repo.Root, "switch", "-c", "other")
			case "head":
				git(t, repo.Root, "commit", "--allow-empty", "-m", "external")
			}
			_, err := CommitOnce(context.Background(), repo, "Reviewed", true, MutationRequest{ID: "offline", ExpectedRevision: before.Revision})
			if ErrorCode(err) != CodeStateConflict {
				t.Fatalf("got %v", err)
			}
		})
	}
}

func TestBranchRequestReplayDoesNotSwitchBackOrReviveDeletedBranch(t *testing.T) {
	repo, before := reviewedRepository(t)
	req := MutationRequest{ID: "new-branch", ExpectedRevision: before.Revision}
	first, err := CreateBranchOnce(context.Background(), repo, "feature/recover", req)
	if err != nil || first.Branch != "feature/recover" {
		t.Fatalf("%+v %v", first, err)
	}
	git(t, repo.Root, "switch", before.Branch)
	git(t, repo.Root, "branch", "-d", "feature/recover")
	replay, err := CreateBranchOnce(context.Background(), repo, "feature/recover", req)
	if err != nil || replay.Branch != before.Branch {
		t.Fatalf("%+v %v", replay, err)
	}
	branches, err := ListBranches(context.Background(), repo)
	if err != nil {
		t.Fatal(err)
	}
	for _, b := range branches {
		if b.Name == "feature/recover" {
			t.Fatal("revived branch")
		}
	}
	_, err = CreateBranchOnce(context.Background(), repo, before.Branch, MutationRequest{ID: "preexisting", ExpectedRevision: replay.Revision})
	if ErrorCode(err) != CodeBranchCreateFailed {
		t.Fatalf("preexisting branch accepted: %v", err)
	}
}

func TestGitRequestValidationAndUnrecordedSuccessNeverRepeats(t *testing.T) {
	repo, before := reviewedRepository(t)
	_, err := CommitOnce(context.Background(), repo, "Reviewed", true, MutationRequest{ID: "../bad", ExpectedRevision: before.Revision})
	if ErrorCode(err) != CodeInvalidRequest {
		t.Fatal(err)
	}
	// Simulate the failure window between Git success and recording its receipt.
	if _, err := Commit(context.Background(), repo, "Reviewed", true); err != nil {
		t.Fatal(err)
	}
	writeFile(t, repo.Root, "later.txt", "keep\n")
	_, err = CommitOnce(context.Background(), repo, "Reviewed", true, MutationRequest{ID: "missing-receipt", ExpectedRevision: before.Revision})
	if ErrorCode(err) != CodeStateConflict {
		t.Fatal(err)
	}
	if data, err := os.ReadFile(filepath.Join(repo.Root, "later.txt")); err != nil || string(data) != "keep\n" {
		t.Fatalf("later work changed: %v", err)
	}
}

func TestGitReceiptDirectoryCannotRedirectWrites(t *testing.T) {
	repo, before := reviewedRepository(t)
	outside := t.TempDir()
	if err := os.Symlink(outside, filepath.Join(repo.Root, ".git", "pudding")); err != nil {
		t.Skip(err)
	}
	_, err := CommitOnce(context.Background(), repo, "Unsafe", true, MutationRequest{ID: "request", ExpectedRevision: before.Revision})
	if ErrorCode(err) != CodeReceiptFailed {
		t.Fatalf("unexpected error: %v", err)
	}
	status, err := ReadStatus(context.Background(), repo)
	if err != nil || status.Head != before.Head {
		t.Fatalf("repository changed: %+v %v", status, err)
	}
	entries, err := os.ReadDir(outside)
	if err != nil || len(entries) != 0 {
		t.Fatalf("wrote outside metadata: %v %v", entries, err)
	}
}

func TestGitRevisionHandlesUnbornEmbeddedRepository(t *testing.T) {
	repo, _ := reviewedRepository(t)
	nested := filepath.Join(repo.Root, "nested")
	if err := os.Mkdir(nested, 0755); err != nil {
		t.Fatal(err)
	}
	git(t, nested, "init", "--quiet")
	if _, err := ReadStatus(context.Background(), repo); err != nil {
		t.Fatal(err)
	}
}

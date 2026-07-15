package api

import (
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

func TestProjectGitStatusAndDiff(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git unavailable")
	}
	srv, st := newTestServer(t)
	root := t.TempDir()
	runProjectGit(t, root, "init", "--quiet")
	runProjectGit(t, root, "config", "user.name", "Pudding Test")
	runProjectGit(t, root, "config", "user.email", "pudding@example.test")
	if err := os.WriteFile(filepath.Join(root, "main.go"), []byte("package main\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	runProjectGit(t, root, "add", "main.go")
	runProjectGit(t, root, "commit", "-m", "initial")
	if err := os.WriteFile(filepath.Join(root, "main.go"), []byte("package changed\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "notes.md"), []byte("# Notes\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	createProjectSession(t, st, "proj_git", "sess_git", root)
	roots := decodeJSON[struct {
		Roots []projectRootView `json:"roots"`
	}](t, req(t, http.MethodGet, srv.URL+"/sessions/sess_git/project/tree", nil))
	rootID := roots.Roots[0].ID

	query := url.Values{"rootID": {rootID}}
	status := decodeJSON[projectGitStatusView](t, req(t, http.MethodGet, srv.URL+"/sessions/sess_git/project/git/status?"+query.Encode(), nil))
	if !status.Available || status.Branch == "" || status.FileCount != 2 || status.UnstagedCount != 1 || status.UntrackedCount != 1 {
		t.Fatalf("unexpected Git status: %+v", status)
	}

	query.Set("path", "main.go")
	query.Set("staged", "false")
	diff := decodeJSON[projectGitDiffView](t, req(t, http.MethodGet, srv.URL+"/sessions/sess_git/project/git/diff?"+query.Encode(), nil))
	if diff.OldContent != "package main\n" || diff.NewContent != "package changed\n" || diff.Binary || diff.TooLarge {
		t.Fatalf("unexpected Git diff: %+v", diff)
	}
}

func TestProjectGitStatusGracefullyHandlesNonRepository(t *testing.T) {
	srv, st := newTestServer(t)
	root := t.TempDir()
	createProjectSession(t, st, "proj_plain", "sess_plain", root)
	roots := decodeJSON[struct {
		Roots []projectRootView `json:"roots"`
	}](t, req(t, http.MethodGet, srv.URL+"/sessions/sess_plain/project/tree", nil))
	query := url.Values{"rootID": {roots.Roots[0].ID}}
	status := decodeJSON[projectGitStatusView](t, req(t, http.MethodGet, srv.URL+"/sessions/sess_plain/project/git/status?"+query.Encode(), nil))
	if status.Available || !status.Clean || status.Files == nil {
		t.Fatalf("unexpected non-repository status: %+v", status)
	}
}

func TestProjectGitWriteOperations(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git unavailable")
	}
	srv, st := newTestServer(t)
	root := t.TempDir()
	createProjectSession(t, st, "proj_git_write", "sess_git_write", root)
	roots := decodeJSON[struct {
		Roots []projectRootView `json:"roots"`
	}](t, req(t, http.MethodGet, srv.URL+"/sessions/sess_git_write/project/tree", nil))
	rootID := roots.Roots[0].ID
	endpoint := srv.URL + "/sessions/sess_git_write/project/git/"

	status := decodeJSON[projectGitStatusView](t, req(t, http.MethodPost, endpoint+"init", map[string]any{"rootID": rootID}))
	if !status.Available || !status.Clean {
		t.Fatalf("unexpected initialized status: %+v", status)
	}
	runProjectGit(t, root, "config", "user.name", "Pudding Test")
	runProjectGit(t, root, "config", "user.email", "pudding@example.test")
	if err := os.WriteFile(filepath.Join(root, "tracked.txt"), []byte("base\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	status = decodeJSON[projectGitStatusView](t, req(t, http.MethodPost, endpoint+"stage", map[string]any{
		"rootID": rootID, "paths": []string{"tracked.txt"},
	}))
	if status.StagedCount != 1 {
		t.Fatalf("unexpected staged status: %+v", status)
	}
	status = decodeJSON[projectGitStatusView](t, req(t, http.MethodPost, endpoint+"commit", map[string]any{
		"rootID": rootID, "message": "initial", "stageAll": false,
	}))
	if !status.Clean || status.Files == nil {
		t.Fatalf("unexpected committed status: %+v", status)
	}

	if err := os.WriteFile(filepath.Join(root, "tracked.txt"), []byte("changed\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "untracked.txt"), []byte("new\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	paths := []string{"tracked.txt", "untracked.txt"}
	status = decodeJSON[projectGitStatusView](t, req(t, http.MethodPost, endpoint+"stage", map[string]any{"rootID": rootID, "paths": paths}))
	if status.StagedCount != 2 {
		t.Fatalf("unexpected staged changes: %+v", status)
	}
	status = decodeJSON[projectGitStatusView](t, req(t, http.MethodPost, endpoint+"unstage", map[string]any{"rootID": rootID, "paths": paths}))
	if status.UnstagedCount != 1 || status.UntrackedCount != 1 {
		t.Fatalf("unexpected unstaged changes: %+v", status)
	}
	status = decodeJSON[projectGitStatusView](t, req(t, http.MethodPost, endpoint+"discard", map[string]any{"rootID": rootID, "paths": paths}))
	if !status.Clean {
		t.Fatalf("unexpected discarded status: %+v", status)
	}
	content, err := os.ReadFile(filepath.Join(root, "tracked.txt"))
	if err != nil || string(content) != "base\n" {
		t.Fatalf("tracked content = %q, err = %v", content, err)
	}
	if _, err := os.Stat(filepath.Join(root, "untracked.txt")); !os.IsNotExist(err) {
		t.Fatalf("untracked file should be removed: %v", err)
	}
}

func TestProjectGitSmartCommitPublishAndSync(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git unavailable")
	}
	srv, st := newTestServer(t)
	root := t.TempDir()
	remote := filepath.Join(t.TempDir(), "remote.git")
	runProjectGit(t, filepath.Dir(remote), "init", "--bare", "--quiet", remote)
	runProjectGit(t, root, "init", "--quiet")
	runProjectGit(t, root, "config", "user.name", "Pudding Test")
	runProjectGit(t, root, "config", "user.email", "pudding@example.test")
	runProjectGit(t, root, "remote", "add", "origin", remote)
	if err := os.WriteFile(filepath.Join(root, "smart.txt"), []byte("smart\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	createProjectSession(t, st, "proj_git_smart", "sess_git_smart", root)
	roots := decodeJSON[struct {
		Roots []projectRootView `json:"roots"`
	}](t, req(t, http.MethodGet, srv.URL+"/sessions/sess_git_smart/project/tree", nil))
	rootID := roots.Roots[0].ID
	endpoint := srv.URL + "/sessions/sess_git_smart/project/git/"

	status := decodeJSON[projectGitStatusView](t, req(t, http.MethodPost, endpoint+"commit", map[string]any{
		"rootID": rootID, "message": "smart", "stageAll": true,
	}))
	if !status.Clean || status.Head == "" {
		t.Fatalf("unexpected smart commit status: %+v", status)
	}
	status = decodeJSON[projectGitStatusView](t, req(t, http.MethodPost, endpoint+"publish", map[string]any{"rootID": rootID}))
	if status.Upstream == "" || status.Ahead != 0 {
		t.Fatalf("unexpected published status: %+v", status)
	}
	if err := os.WriteFile(filepath.Join(root, "second.txt"), []byte("second\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	status = decodeJSON[projectGitStatusView](t, req(t, http.MethodPost, endpoint+"commit", map[string]any{
		"rootID": rootID, "message": "second", "stageAll": true,
	}))
	if status.Ahead != 1 {
		t.Fatalf("unexpected ahead status: %+v", status)
	}
	status = decodeJSON[projectGitStatusView](t, req(t, http.MethodPost, endpoint+"sync", map[string]any{"rootID": rootID}))
	if status.Ahead != 0 || status.Behind != 0 {
		t.Fatalf("unexpected synced status: %+v", status)
	}
}

func TestProjectGitBranchOperations(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git unavailable")
	}
	srv, st := newTestServer(t)
	root := t.TempDir()
	runProjectGit(t, root, "init", "--quiet")
	runProjectGit(t, root, "config", "user.name", "Pudding Test")
	runProjectGit(t, root, "config", "user.email", "pudding@example.test")
	if err := os.WriteFile(filepath.Join(root, "branch.txt"), []byte("branch\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	runProjectGit(t, root, "add", "branch.txt")
	runProjectGit(t, root, "commit", "-m", "initial")
	createProjectSession(t, st, "proj_git_branch", "sess_git_branch", root)
	roots := decodeJSON[struct {
		Roots []projectRootView `json:"roots"`
	}](t, req(t, http.MethodGet, srv.URL+"/sessions/sess_git_branch/project/tree", nil))
	rootID := roots.Roots[0].ID
	endpoint := srv.URL + "/sessions/sess_git_branch/project/git/branches"
	query := url.Values{"rootID": {rootID}}
	initial := decodeJSON[projectGitBranchesView](t, req(t, http.MethodGet, endpoint+"?"+query.Encode(), nil))
	if len(initial.Branches) != 1 {
		t.Fatalf("unexpected initial branches: %+v", initial)
	}
	status := decodeJSON[projectGitStatusView](t, req(t, http.MethodPost, endpoint, map[string]any{"rootID": rootID, "name": "feature/test"}))
	if status.Branch != "feature/test" {
		t.Fatalf("unexpected created branch: %+v", status)
	}
	status = decodeJSON[projectGitStatusView](t, req(t, http.MethodPost, endpoint+"/rename", map[string]any{"rootID": rootID, "name": "feature/renamed"}))
	if status.Branch != "feature/renamed" {
		t.Fatalf("unexpected renamed branch: %+v", status)
	}
	status = decodeJSON[projectGitStatusView](t, req(t, http.MethodPost, endpoint+"/switch", map[string]any{"rootID": rootID, "name": initial.Branches[0].Name}))
	if status.Branch != initial.Branches[0].Name {
		t.Fatalf("unexpected switched branch: %+v", status)
	}
	_ = decodeJSON[projectGitStatusView](t, req(t, http.MethodPost, endpoint+"/delete", map[string]any{"rootID": rootID, "name": "feature/renamed"}))
	remaining := decodeJSON[projectGitBranchesView](t, req(t, http.MethodGet, endpoint+"?"+query.Encode(), nil))
	if len(remaining.Branches) != 1 {
		t.Fatalf("unexpected remaining branches: %+v", remaining)
	}
}

func runProjectGit(t *testing.T, root string, args ...string) {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = root
	if output, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git %v: %v: %s", args, err, output)
	}
}

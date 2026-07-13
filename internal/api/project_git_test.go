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

func runProjectGit(t *testing.T, root string, args ...string) {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = root
	if output, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git %v: %v: %s", args, err, output)
	}
}

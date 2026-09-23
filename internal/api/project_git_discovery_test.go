package api

import (
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

func TestProjectGitStatusReportsDiscoveryFailure(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git unavailable")
	}
	t.Setenv("GIT_CONFIG_GLOBAL", os.DevNull)
	t.Setenv("GIT_CONFIG_NOSYSTEM", "1")
	root := t.TempDir()
	runProjectGit(t, root, "init", "--quiet")
	if err := os.WriteFile(filepath.Join(root, ".git", "config"), []byte("[invalid config\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	srv, st := newTestServer(t)
	createProjectSession(t, st, "proj_git_failure", "sess_git_failure", root)
	roots := decodeJSON[struct {
		Roots []projectRootView `json:"roots"`
	}](t, req(t, http.MethodGet, srv.URL+"/sessions/sess_git_failure/project/tree", nil))
	query := url.Values{"rootID": {roots.Roots[0].ID}}
	resp := req(t, http.MethodGet, srv.URL+"/sessions/sess_git_failure/project/git/status?"+query.Encode(), nil)
	payload := decodeJSON[map[string]any](t, resp)
	if resp.StatusCode != http.StatusInternalServerError || payload["error"] != "git_discovery_failed" {
		t.Fatalf("discovery failure should return an error, got HTTP %d: %+v", resp.StatusCode, payload)
	}
	if _, ok := payload["available"]; ok {
		t.Fatalf("discovery failure must not report repository availability: %+v", payload)
	}
}

func TestProjectGitStatusHonorsConfigIsolation(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git unavailable")
	}
	userHome := t.TempDir()
	if err := os.WriteFile(filepath.Join(userHome, ".gitconfig"), []byte("[invalid config\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("HOME", userHome)
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	t.Setenv("GIT_CONFIG_GLOBAL", os.DevNull)
	t.Setenv("GIT_CONFIG_NOSYSTEM", "1")
	root := t.TempDir()
	runProjectGit(t, root, "init", "--quiet")
	if err := os.WriteFile(filepath.Join(root, "untracked.txt"), []byte("working file\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	srv, st := newTestServer(t)
	createProjectSession(t, st, "proj_git_isolation", "sess_git_isolation", root)
	roots := decodeJSON[struct {
		Roots []projectRootView `json:"roots"`
	}](t, req(t, http.MethodGet, srv.URL+"/sessions/sess_git_isolation/project/tree", nil))
	query := url.Values{"rootID": {roots.Roots[0].ID}}
	resp := req(t, http.MethodGet, srv.URL+"/sessions/sess_git_isolation/project/git/status?"+query.Encode(), nil)
	status := decodeJSON[projectGitStatusView](t, resp)
	if resp.StatusCode != http.StatusOK || !status.Available || status.UntrackedCount != 1 {
		t.Fatalf("isolated status should read the repository, got HTTP %d: %+v", resp.StatusCode, status)
	}
}

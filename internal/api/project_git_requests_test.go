package api

import (
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"testing"
)

func TestProjectGitRequestReplayAndStateConflict(t *testing.T) {
	srv, st := newTestServer(t)
	root := t.TempDir()
	runProjectGit(t, root, "init", "--quiet")
	runProjectGit(t, root, "config", "user.name", "Request Test")
	runProjectGit(t, root, "config", "user.email", "request@example.invalid")
	if err := os.WriteFile(filepath.Join(root, "one.txt"), []byte("first\n"), 0600); err != nil {
		t.Fatal(err)
	}
	createProjectSession(t, st, "git_requests", "request_session", root)
	tree := decodeJSON[struct {
		Roots []projectRootView `json:"roots"`
	}](t, req(t, http.MethodGet, srv.URL+"/sessions/request_session/project/tree", nil))
	rootID := tree.Roots[0].ID
	endpoint := srv.URL + "/sessions/request_session/project/git/"
	statusURL := endpoint + "status?" + url.Values{"rootID": {rootID}}.Encode()
	before := decodeJSON[projectGitStatusView](t, req(t, http.MethodGet, statusURL, nil))
	body := map[string]any{"rootID": rootID, "message": "Once", "stageAll": true, "clientRequestID": "commit-once", "expectedRevision": before.Revision}
	first := decodeJSON[projectGitStatusView](t, req(t, http.MethodPost, endpoint+"commit", body))
	if first.Head == "" {
		t.Fatal("missing commit")
	}
	if err := os.WriteFile(filepath.Join(root, "later.txt"), []byte("later\n"), 0600); err != nil {
		t.Fatal(err)
	}
	replay := decodeJSON[projectGitStatusView](t, req(t, http.MethodPost, endpoint+"commit", body))
	if replay.Head != first.Head || replay.UntrackedCount != 1 {
		t.Fatalf("%+v", replay)
	}
	body["message"] = "changed"
	response := req(t, http.MethodPost, endpoint+"commit", body)
	if response.StatusCode != http.StatusConflict {
		t.Fatalf("identity conflict: %d", response.StatusCode)
	}
	response.Body.Close()
	body["clientRequestID"] = "unexecuted"
	response = req(t, http.MethodPost, endpoint+"commit", body)
	if response.StatusCode != http.StatusConflict {
		t.Fatalf("state conflict: %d", response.StatusCode)
	}
	response.Body.Close()
	branch := map[string]any{"rootID": rootID, "name": "feature/once", "clientRequestID": "branch-once", "expectedRevision": replay.Revision}
	created := decodeJSON[projectGitStatusView](t, req(t, http.MethodPost, endpoint+"branches", branch))
	repeated := decodeJSON[projectGitStatusView](t, req(t, http.MethodPost, endpoint+"branches", branch))
	if created.Branch != "feature/once" || repeated.Branch != created.Branch {
		t.Fatalf("%+v %+v", created, repeated)
	}
	branch["clientRequestID"] = "bad/id"
	response = req(t, http.MethodPost, endpoint+"branches", branch)
	if response.StatusCode != http.StatusBadRequest {
		t.Fatalf("invalid identity: %d", response.StatusCode)
	}
	response.Body.Close()
}

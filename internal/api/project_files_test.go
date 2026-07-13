package api

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/teatak/pudding-core/internal/store"
)

func TestProjectBrowserListsRootsAndReadsText(t *testing.T) {
	srv, st := newTestServer(t)
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "docs"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(root, "node_modules", "hidden"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(root, ".github", "workflows"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, ".env"), []byte("SECRET=hidden\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "README.md"), []byte("# Demo\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	createProjectSession(t, st, "proj_browser", "sess_browser", root)

	roots := decodeJSON[struct {
		ProjectID string            `json:"projectID"`
		Roots     []projectRootView `json:"roots"`
	}](t, req(t, http.MethodGet, srv.URL+"/sessions/sess_browser/project/tree", nil))
	if roots.ProjectID != "proj_browser" || len(roots.Roots) != 1 || roots.Roots[0].Path != root {
		t.Fatalf("unexpected roots: %+v", roots)
	}

	query := url.Values{"rootID": {roots.Roots[0].ID}, "path": {"."}}
	tree := decodeJSON[struct {
		Entries []projectTreeEntry `json:"entries"`
	}](t, req(t, http.MethodGet, srv.URL+"/sessions/sess_browser/project/tree?"+query.Encode(), nil))
	if len(tree.Entries) != 2 || tree.Entries[0].Name != "docs" || tree.Entries[0].Type != "dir" || tree.Entries[1].Name != "README.md" {
		t.Fatalf("unexpected tree: %+v", tree.Entries)
	}

	query.Set("path", "README.md")
	file := decodeJSON[projectFileView](t, req(t, http.MethodGet, srv.URL+"/sessions/sess_browser/project/file?"+query.Encode(), nil))
	if file.RootID != roots.Roots[0].ID || file.Path != "README.md" || file.Content != "# Demo\n" || file.MIME != "text/markdown" {
		t.Fatalf("unexpected file: %+v", file)
	}
}

func TestProjectBrowserRejectsTraversalAndOtherProjectRoot(t *testing.T) {
	srv, st := newTestServer(t)
	rootA := t.TempDir()
	rootB := t.TempDir()
	if err := os.WriteFile(filepath.Join(rootB, "secret.txt"), []byte("secret"), 0o600); err != nil {
		t.Fatal(err)
	}
	createProjectSession(t, st, "proj_a", "sess_a", rootA)
	createProjectSession(t, st, "proj_b", "sess_b", rootB)

	rootsA := decodeJSON[struct {
		Roots []projectRootView `json:"roots"`
	}](t, req(t, http.MethodGet, srv.URL+"/sessions/sess_a/project/tree", nil))
	rootsB := decodeJSON[struct {
		Roots []projectRootView `json:"roots"`
	}](t, req(t, http.MethodGet, srv.URL+"/sessions/sess_b/project/tree", nil))

	query := url.Values{"rootID": {rootsA.Roots[0].ID}, "path": {"../secret.txt"}}
	resp := req(t, http.MethodGet, srv.URL+"/sessions/sess_a/project/file?"+query.Encode(), nil)
	resp.Body.Close()
	if resp.StatusCode != http.StatusForbidden {
		t.Fatalf("traversal status = %d", resp.StatusCode)
	}

	query = url.Values{"rootID": {rootsB.Roots[0].ID}, "path": {"secret.txt"}}
	resp = req(t, http.MethodGet, srv.URL+"/sessions/sess_a/project/file?"+query.Encode(), nil)
	resp.Body.Close()
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("foreign root status = %d", resp.StatusCode)
	}
}

func TestProjectBrowserRejectsEscapingSymlinkAndBinaryText(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("symlink permissions vary on Windows")
	}
	srv, st := newTestServer(t)
	root := t.TempDir()
	outside := t.TempDir()
	if err := os.WriteFile(filepath.Join(outside, "secret.txt"), []byte("secret"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(filepath.Join(outside, "secret.txt"), filepath.Join(root, "escape.txt")); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "binary.dat"), []byte{0, 1, 2, 3}, 0o600); err != nil {
		t.Fatal(err)
	}
	createProjectSession(t, st, "proj_secure", "sess_secure", root)
	roots := decodeJSON[struct {
		Roots []projectRootView `json:"roots"`
	}](t, req(t, http.MethodGet, srv.URL+"/sessions/sess_secure/project/tree", nil))

	query := url.Values{"rootID": {roots.Roots[0].ID}, "path": {"escape.txt"}}
	resp := req(t, http.MethodGet, srv.URL+"/sessions/sess_secure/project/file?"+query.Encode(), nil)
	resp.Body.Close()
	if resp.StatusCode != http.StatusForbidden {
		t.Fatalf("symlink escape status = %d", resp.StatusCode)
	}

	query.Set("path", "binary.dat")
	resp = req(t, http.MethodGet, srv.URL+"/sessions/sess_secure/project/file?"+query.Encode(), nil)
	resp.Body.Close()
	if resp.StatusCode != http.StatusUnsupportedMediaType {
		t.Fatalf("binary status = %d", resp.StatusCode)
	}
}

func TestProjectBrowserServesOnlyImageResources(t *testing.T) {
	srv, st := newTestServer(t)
	root := t.TempDir()
	imageBytes := []byte{0x89, 'P', 'N', 'G', '\r', '\n', 0x1a, '\n'}
	if err := os.WriteFile(filepath.Join(root, "pixel.png"), imageBytes, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "note.txt"), []byte("note"), 0o600); err != nil {
		t.Fatal(err)
	}
	createProjectSession(t, st, "proj_resource", "sess_resource", root)
	roots := decodeJSON[struct {
		Roots []projectRootView `json:"roots"`
	}](t, req(t, http.MethodGet, srv.URL+"/sessions/sess_resource/project/tree", nil))

	resourceBase := srv.URL + "/sessions/sess_resource/project/resources/" + roots.Roots[0].ID + "/"
	resp := req(t, http.MethodGet, resourceBase+"pixel.png", nil)
	data, err := io.ReadAll(resp.Body)
	resp.Body.Close()
	if err != nil {
		t.Fatal(err)
	}
	if resp.StatusCode != http.StatusOK || resp.Header.Get("Content-Type") != "image/png" || string(data) != string(imageBytes) {
		t.Fatalf("unexpected image response: status=%d contentType=%q data=%x", resp.StatusCode, resp.Header.Get("Content-Type"), data)
	}

	resp = req(t, http.MethodGet, resourceBase+"note.txt", nil)
	resp.Body.Close()
	if resp.StatusCode != http.StatusUnsupportedMediaType {
		t.Fatalf("text resource status = %d", resp.StatusCode)
	}
}

func TestProjectBrowserFileMutations(t *testing.T) {
	srv, st := newTestServer(t)
	root := t.TempDir()
	createProjectSession(t, st, "proj_mutations", "sess_mutations", root)
	roots := decodeJSON[struct {
		Roots []projectRootView `json:"roots"`
	}](t, req(t, http.MethodGet, srv.URL+"/sessions/sess_mutations/project/tree", nil))
	rootID := roots.Roots[0].ID

	dir := decodeJSON[projectEntryMutationView](t, req(t, http.MethodPost, srv.URL+"/sessions/sess_mutations/project/entries", map[string]any{
		"rootID": rootID, "parentPath": ".", "name": "docs", "type": "dir",
	}))
	if dir.Path != "docs" || dir.Type != "dir" {
		t.Fatalf("created dir = %+v", dir)
	}
	created := decodeJSON[projectEntryMutationView](t, req(t, http.MethodPost, srv.URL+"/sessions/sess_mutations/project/entries", map[string]any{
		"rootID": rootID, "parentPath": "docs", "name": "guide.md", "type": "file",
	}))
	if created.Path != "docs/guide.md" || created.Type != "file" {
		t.Fatalf("created file = %+v", created)
	}

	query := url.Values{"rootID": {rootID}, "path": {created.Path}}
	file := decodeJSON[projectFileView](t, req(t, http.MethodGet, srv.URL+"/sessions/sess_mutations/project/file?"+query.Encode(), nil))
	if file.Revision == "" {
		t.Fatal("file revision is empty")
	}
	saved := decodeJSON[projectFileView](t, req(t, http.MethodPut, srv.URL+"/sessions/sess_mutations/project/file", map[string]any{
		"rootID": rootID, "path": created.Path, "content": "# Guide\n", "expectedRevision": file.Revision,
	}))
	if saved.Content != "# Guide\n" || saved.Revision == file.Revision {
		t.Fatalf("saved file = %+v", saved)
	}
	conflict := req(t, http.MethodPut, srv.URL+"/sessions/sess_mutations/project/file", map[string]any{
		"rootID": rootID, "path": created.Path, "content": "stale", "expectedRevision": file.Revision,
	})
	conflict.Body.Close()
	if conflict.StatusCode != http.StatusConflict {
		t.Fatalf("stale save status = %d", conflict.StatusCode)
	}

	renamed := decodeJSON[projectEntryMutationView](t, req(t, http.MethodPatch, srv.URL+"/sessions/sess_mutations/project/entries", map[string]any{
		"rootID": rootID, "path": created.Path, "name": "intro.md",
	}))
	if renamed.Path != "docs/intro.md" {
		t.Fatalf("renamed entry = %+v", renamed)
	}
	query.Set("path", "docs")
	deleted := req(t, http.MethodDelete, srv.URL+"/sessions/sess_mutations/project/entries?"+query.Encode(), nil)
	deleted.Body.Close()
	if deleted.StatusCode != http.StatusNoContent {
		t.Fatalf("delete status = %d", deleted.StatusCode)
	}
	if _, err := os.Stat(filepath.Join(root, "docs")); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("deleted directory stat error = %v", err)
	}
}

func createProjectSession(t *testing.T, st store.Store, projectID, sessionID, root string) {
	t.Helper()
	ctx := context.Background()
	if err := st.CreateProject(ctx, &store.Project{ID: projectID, Name: projectID, RootDirs: []string{root}}); err != nil {
		t.Fatal(err)
	}
	if err := st.CreateSession(ctx, &store.Session{ID: sessionID, Provider: "mock", Model: "mock", ProjectID: projectID}); err != nil {
		t.Fatal(err)
	}
}

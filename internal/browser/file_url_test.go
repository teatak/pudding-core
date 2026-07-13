package browser

import (
	"context"
	"errors"
	"net/url"
	"os"
	"path/filepath"
	"testing"

	"github.com/teatak/pudding-core/internal/store"
)

type fileURLTestScope struct {
	session *store.Session
	project *store.Project
}

func (s fileURLTestScope) GetSession(context.Context, string) (*store.Session, error) {
	if s.session == nil {
		return nil, store.ErrNotFound
	}
	return s.session, nil
}

func (s fileURLTestScope) GetProject(context.Context, string) (*store.Project, error) {
	if s.project == nil {
		return nil, store.ErrNotFound
	}
	return s.project, nil
}

func TestProjectFileURLAuthorizerAllowsOnlyRegularFilesInsideProject(t *testing.T) {
	root := t.TempDir()
	file := filepath.Join(root, "page.html")
	if err := os.WriteFile(file, []byte("<h1>Pudding</h1>"), 0o600); err != nil {
		t.Fatal(err)
	}
	authorize := ProjectFileURLAuthorizer(fileURLTestScope{
		session: &store.Session{ID: "session-1", ProjectID: "project-1"},
		project: &store.Project{ID: "project-1", RootDirs: []string{root}},
	})
	parsed := &url.URL{Scheme: "file", Path: filepath.ToSlash(file), Fragment: "intro"}
	authorized, err := authorize(context.Background(), "session-1", parsed)
	if err != nil {
		t.Fatal(err)
	}
	resolvedRoot, err := filepath.EvalSymlinks(root)
	if err != nil {
		t.Fatal(err)
	}
	if authorized.FileRoot != resolvedRoot {
		t.Fatalf("file root = %q, want %q", authorized.FileRoot, resolvedRoot)
	}
	if !fileURLAllowed(authorized.URL, []string{authorized.FileRoot}) {
		t.Fatalf("authorized URL was rejected: %s", authorized.URL)
	}

	outside := filepath.Join(t.TempDir(), "outside.html")
	if err := os.WriteFile(outside, []byte("outside"), 0o600); err != nil {
		t.Fatal(err)
	}
	_, err = authorize(context.Background(), "session-1", &url.URL{Scheme: "file", Path: filepath.ToSlash(outside)})
	if !errors.Is(err, ErrFileURLNotAllowed) {
		t.Fatalf("outside file error = %v", err)
	}
	_, err = authorize(context.Background(), "session-1", &url.URL{Scheme: "file", Path: filepath.ToSlash(root)})
	if !errors.Is(err, ErrFileURLNotAllowed) {
		t.Fatalf("directory error = %v", err)
	}
}

func TestProjectFileURLAuthorizerRejectsSymlinkEscape(t *testing.T) {
	root := t.TempDir()
	outside := filepath.Join(t.TempDir(), "outside.html")
	if err := os.WriteFile(outside, []byte("outside"), 0o600); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(root, "linked.html")
	if err := os.Symlink(outside, link); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}
	authorize := ProjectFileURLAuthorizer(fileURLTestScope{
		session: &store.Session{ID: "session-1", ProjectID: "project-1"},
		project: &store.Project{ID: "project-1", RootDirs: []string{root}},
	})
	_, err := authorize(context.Background(), "session-1", &url.URL{Scheme: "file", Path: filepath.ToSlash(link)})
	if !errors.Is(err, ErrFileURLNotAllowed) {
		t.Fatalf("symlink escape error = %v", err)
	}
}

func TestFileURLNotAllowedHasStableToolErrorCode(t *testing.T) {
	if got := ErrorCode(ErrFileURLNotAllowed); got != "file_url_not_allowed" {
		t.Fatalf("error code = %q", got)
	}
}

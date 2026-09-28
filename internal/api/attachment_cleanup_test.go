package api

import (
	"bytes"
	"context"
	"io"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/teatak/pudding-core/internal/attachment"
	"github.com/teatak/pudding-core/internal/engine"
	"github.com/teatak/pudding-core/internal/event"
	"github.com/teatak/pudding-core/internal/store"
	"github.com/teatak/pudding-core/internal/store/memstore"
)

func TestPurgeSessionRemovesOnlyGroupAttachments(t *testing.T) {
	ctx := context.Background()
	ms := memstore.New()
	for _, id := range []string{"parent", "other"} {
		if err := ms.CreateSession(ctx, &store.Session{ID: id, Provider: "mock", Model: "m"}); err != nil {
			t.Fatal(err)
		}
	}
	if err := ms.CreateChildSession(ctx, "parent", &store.Session{ID: "child", Provider: "mock", Model: "m"}); err != nil {
		t.Fatal(err)
	}
	hub := event.NewHub()
	eng := engine.New(ms, hub, nil, ms)
	srv := New(eng, ms, ms, hub).WithHome(t.TempDir())
	svc := attachment.NewService(srv.home)
	for _, id := range []string{"parent", "child", "other"} {
		item, err := svc.StoreReader(id, "test.txt", "text/plain", strings.NewReader(id))
		if err != nil {
			t.Fatal(err)
		}
		path, _, _ := svc.Path(id, item.AttachmentKey)
		cache := filepath.Join(filepath.Dir(path), ".model")
		if err := os.MkdirAll(cache, 0700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(cache, "derived.jpg"), []byte("cached"), 0600); err != nil {
			t.Fatal(err)
		}
	}
	if err := srv.purgeSession(ctx, "parent"); err != nil {
		t.Fatal(err)
	}
	for _, id := range []string{"parent", "child"} {
		if _, err := os.Stat(filepath.Join(srv.home, "attachments", "sessions", id)); !os.IsNotExist(err) {
			t.Fatalf("%s attachments survived deletion: %v", id, err)
		}
	}
	if _, err := os.Stat(filepath.Join(srv.home, "attachments", "sessions", "other", "blobs")); err != nil {
		t.Fatal(err)
	}
}

func TestOrphanAttachmentCleanupRetriesAfterRestartAndPreservesArchives(t *testing.T) {
	ctx := context.Background()
	ms := memstore.New()
	dir := t.TempDir()
	svc := attachment.NewService(dir)
	for _, id := range []string{"live", "archived", "orphan"} {
		if id != "orphan" {
			if err := ms.CreateSession(ctx, &store.Session{ID: id, Provider: "mock", Model: "mock"}); err != nil {
				t.Fatal(err)
			}
		}
		if _, err := svc.StoreReader(id, "file.txt", "text/plain", strings.NewReader(id)); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := ms.ArchiveSession(ctx, "archived"); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.StoreReader(attachment.DraftSessionID, "draft.txt", "text/plain", strings.NewReader("draft")); err != nil {
		t.Fatal(err)
	}
	base := filepath.Join(dir, "attachments", "sessions")
	legacyDraft := filepath.Join(base, attachment.DraftSessionID, "blobs")
	if err := os.MkdirAll(legacyDraft, 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(base, 0500); err != nil {
		t.Fatal(err)
	}
	defer os.Chmod(base, 0700)
	srv := &Server{store: ms, home: dir}
	if err := srv.reclaimOrphanAttachments(ctx); err == nil {
		t.Fatal("cleanup failure was hidden")
	}
	if err := os.Chmod(base, 0700); err != nil {
		t.Fatal(err)
	}
	// A fresh Server has no memory of the failed deletion, only the store and disk.
	srv = &Server{store: ms, home: dir}
	if err := srv.reclaimOrphanAttachments(ctx); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(base, "orphan")); !os.IsNotExist(err) {
		t.Fatalf("orphan survived retry: %v", err)
	}
	for _, path := range []string{filepath.Join(base, "live"), filepath.Join(base, "archived"), legacyDraft, filepath.Join(dir, "temp", "attachments")} {
		if _, err := os.Stat(path); err != nil {
			t.Fatalf("retained attachments removed: %v", err)
		}
	}
}

// Pause the multipart body after the handler's initial session lookup. Deletion
// while that body is still arriving must not recreate the deleted directory.
type delayedAttachmentBody struct {
	io.Reader
	started chan struct{}
	release chan struct{}
	once    sync.Once
}

func (r *delayedAttachmentBody) Read(p []byte) (int, error) {
	r.once.Do(func() { close(r.started); <-r.release })
	return r.Reader.Read(p)
}
func TestUploadArrivingAfterDeletionDoesNotRecreateAttachments(t *testing.T) {
	ctx := context.Background()
	ms := memstore.New()
	hub := event.NewHub()
	if err := ms.CreateSession(ctx, &store.Session{ID: "s", Provider: "mock", Model: "mock"}); err != nil {
		t.Fatal(err)
	}
	srv := New(engine.New(ms, hub, nil, ms), ms, ms, hub).WithHome(t.TempDir())
	var body bytes.Buffer
	form := multipart.NewWriter(&body)
	file, err := form.CreateFormFile("file", "test.txt")
	if err != nil {
		t.Fatal(err)
	}
	_, _ = file.Write([]byte("late"))
	_ = form.Close()
	reader := &delayedAttachmentBody{Reader: &body, started: make(chan struct{}), release: make(chan struct{})}
	request := httptest.NewRequest(http.MethodPost, "/sessions/s/attachments", reader)
	request.Header.Set("Authorization", "Bearer "+testToken)
	request.Header.Set("Content-Type", form.FormDataContentType())
	response := httptest.NewRecorder()
	done := make(chan struct{})
	go func() { defer close(done); srv.Handler(testToken, nil).ServeHTTP(response, request) }()
	<-reader.started
	if err := srv.purgeSession(ctx, "s"); err != nil {
		close(reader.release)
		t.Fatal(err)
	}
	close(reader.release)
	<-done
	if response.Code != http.StatusNotFound {
		t.Fatalf("late upload status=%d body=%s", response.Code, response.Body.String())
	}
	if _, err := os.Stat(filepath.Join(srv.home, "attachments", "sessions", "s")); !os.IsNotExist(err) {
		t.Fatalf("deleted files recreated: %v", err)
	}
}

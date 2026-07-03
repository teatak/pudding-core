package attachment

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestDraftAttachmentsStoredUnderTempRoot(t *testing.T) {
	home := t.TempDir()
	svc := NewService(home)

	stored, err := svc.StoreReader(DraftSessionID, "note.txt", "text/plain", strings.NewReader("hello"))
	if err != nil {
		t.Fatalf("store draft attachment: %v", err)
	}
	if !strings.HasPrefix(stored.AttachmentKey, "sessions/draft/blobs/") {
		t.Fatalf("draft key = %q, want sessions/draft/blobs/...", stored.AttachmentKey)
	}

	draftPath, ok, err := svc.Path(DraftSessionID, stored.AttachmentKey)
	if err != nil {
		t.Fatalf("resolve draft path: %v", err)
	}
	if !ok {
		t.Fatal("draft path did not resolve")
	}
	wantDraftRoot := filepath.Join(home, "temp", "attachments")
	if filepath.Dir(draftPath) != wantDraftRoot {
		t.Fatalf("draft path = %q, want directly under %q", draftPath, wantDraftRoot)
	}
	data, err := os.ReadFile(draftPath)
	if err != nil {
		t.Fatalf("read draft attachment: %v", err)
	}
	if string(data) != "hello" {
		t.Fatalf("draft body = %q, want hello", data)
	}
	legacyPath := filepath.Join(home, "attachments", filepath.FromSlash(stored.AttachmentKey))
	if _, err := os.Stat(legacyPath); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("legacy draft path stat err = %v, want not exist", err)
	}

	copied, err := svc.CopyToSession(DraftSessionID, "sess_real", stored)
	if err != nil {
		t.Fatalf("copy draft attachment: %v", err)
	}
	targetPath, ok, err := svc.Path("sess_real", copied.AttachmentKey)
	if err != nil {
		t.Fatalf("resolve copied path: %v", err)
	}
	if !ok {
		t.Fatal("copied path did not resolve")
	}
	wantTargetRoot := filepath.Join(home, "attachments", "sessions", "sess_real", "blobs")
	if !strings.HasPrefix(filepath.Clean(targetPath), wantTargetRoot+string(os.PathSeparator)) {
		t.Fatalf("copied path = %q, want under %q", targetPath, wantTargetRoot)
	}
	copiedData, err := os.ReadFile(targetPath)
	if err != nil {
		t.Fatalf("read copied attachment: %v", err)
	}
	if string(copiedData) != "hello" {
		t.Fatalf("copied body = %q, want hello", copiedData)
	}
}

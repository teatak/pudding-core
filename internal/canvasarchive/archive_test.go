package canvasarchive

import (
	"archive/zip"
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func sample(t *testing.T, home string) Snapshot {
	t.Helper()
	image := filepath.Join(home, "picture.png")
	if err := os.WriteFile(image, []byte("image bytes"), 0600); err != nil {
		t.Fatal(err)
	}
	content, _ := json.Marshal(map[string]any{"kind": "markdown", "title": "<script>bad</script>", "item": map[string]any{"content": "![local](file://" + image + ")"}})
	return Snapshot{ID: "old", Name: "Archive", UpdatedAt: 1, Revisions: []Revision{{Hash: "first", Content: content}, {Hash: "chart", Content: json.RawMessage(`{"kind":"chart","item":{"chart":{"data":[{"day":"Monday","value":42}]}}}`)}}}
}
func TestArchivePortableVerifiedAndCleaned(t *testing.T) {
	home := t.TempDir()
	s := sample(t, home)
	entry, err := Write(home, s)
	if err != nil {
		t.Fatal(err)
	}
	same, err := Write(home, s)
	if err != nil || same.ID != entry.ID {
		t.Fatal("retry changed archive", err)
	}
	body, err := Preview(home, entry.ID)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Contains(body, []byte("data:image/png;base64,")) || bytes.Contains(body, []byte("<script>bad")) || !bytes.Contains(body, []byte("Monday")) {
		t.Fatal("unsafe or incomplete preview")
	}
	var out bytes.Buffer
	if err = Export(home, entry.ID, &out); err != nil {
		t.Fatal(err)
	}
	zr, err := zip.NewReader(bytes.NewReader(out.Bytes()), int64(out.Len()))
	if err != nil {
		t.Fatal(err)
	}
	if len(zr.File) < 6 {
		t.Fatal("incomplete export")
	}
	if err = Remove(home, []string{entry.ID}); err != nil {
		t.Fatal(err)
	}
	entries, err := List(home)
	if err != nil || len(entries) != 0 {
		t.Fatal(entries, err)
	}
	if _, err = os.Stat(entry.Path); !os.IsNotExist(err) {
		t.Fatal("cleanup left files")
	}
}
func TestArchiveFailureStaysVisibleAndRetryable(t *testing.T) {
	home := t.TempDir()
	entry, err := Write(home, sample(t, home))
	if err != nil {
		t.Fatal(err)
	}
	if err = os.Chmod(entry.Path, 0500); err != nil {
		t.Fatal(err)
	}
	defer os.Chmod(entry.Path, 0700)
	if err = Remove(home, []string{entry.ID}); err == nil {
		t.Fatal("expected cleanup failure")
	}
	entries, err := List(home)
	if err != nil || len(entries) != 1 {
		t.Fatal("failed cleanup disappeared")
	}
	os.Chmod(entry.Path, 0700)
	if err = Remove(home, []string{entry.ID}); err != nil {
		t.Fatal(err)
	}
}
func TestArchiveRejectsTraversalSymlinksAndCorruption(t *testing.T) {
	home := t.TempDir()
	entry, err := Write(home, sample(t, home))
	if err != nil {
		t.Fatal(err)
	}
	if err = Remove(home, []string{"../escape"}); err == nil {
		t.Fatal("accepted traversal")
	}
	if err = os.WriteFile(filepath.Join(entry.Path, "index.html"), []byte("corrupt"), 0600); err != nil {
		t.Fatal(err)
	}
	var out bytes.Buffer
	if err = Export(home, entry.ID, &out); err == nil {
		t.Fatal("exported corruption")
	}
	if _, err = Write(home, sample(t, home)); err == nil {
		t.Fatal("accepted corrupt archive retry")
	}
	outside := t.TempDir()
	id := strings.Repeat("1", 8) + "T111111Z-" + strings.Repeat("a", 32)
	if err = os.Symlink(outside, filepath.Join(Root(home), id)); err != nil {
		t.Fatal(err)
	}
	if err = Remove(home, []string{id}); err == nil {
		t.Fatal("followed symlink")
	}
}
func TestMissingLocalImageWarningAndAttachmentTraversal(t *testing.T) {
	home := t.TempDir()
	s := Snapshot{ID: "old", UpdatedAt: 1, Revisions: []Revision{{Hash: "missing", Content: json.RawMessage(`{"kind":"gallery","item":{"items":[{"src":"file:///missing/pudding-archive.png"}]}}`)}}}
	e, err := Write(home, s)
	if err != nil || len(e.Warnings) != 1 {
		t.Fatal(e, err)
	}
	s.ID = "invalid"
	s.Revisions[0].Content = json.RawMessage(`{"kind":"gallery","item":{"items":[{"src":"/sessions/s/attachments/../../secret"}]}}`)
	if _, err = Write(home, s); err == nil {
		t.Fatal("accepted attachment traversal")
	}
}

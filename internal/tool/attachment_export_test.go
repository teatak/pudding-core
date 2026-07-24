package tool

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/teatak/pudding-core/internal/attachment"
)

func TestBuiltinAttachmentExportWritesAuthorizedProjectFile(t *testing.T) {
	home := t.TempDir()
	project := t.TempDir()
	imageBytes := []byte{0x89, 'P', 'N', 'G', '\r', '\n', 0x1a, '\n', 1, 2, 3}
	stored, err := attachment.NewService(home).StoreReader("sess_export", "capture.png", "image/png", bytes.NewReader(imageBytes))
	if err != nil {
		t.Fatal(err)
	}
	runner := NewBuiltinRunner(WithHomeDir(home))
	res := runner.Call(context.Background(), Call{
		SessionID:   "sess_export",
		Name:        AttachmentExport,
		ProjectDirs: []string{project},
		Args:        json.RawMessage(`{"scope":"project","attachmentKey":"` + stored.AttachmentKey + `","path":"assets/capture.png"}`),
	})
	if !res.Ok {
		t.Fatalf("attachment export should succeed: %+v", res)
	}
	canonicalProject, err := filepath.EvalSymlinks(project)
	if err != nil {
		t.Fatal(err)
	}
	wantPath := filepath.Join(canonicalProject, "assets", "capture.png")
	payload := decodeToolResult(t, res)
	if payload["path"] != wantPath || payload["relativePath"] != filepath.Join("assets", "capture.png") || payload["attachmentKey"] != stored.AttachmentKey {
		t.Fatalf("unexpected export payload: %+v", payload)
	}
	data, err := os.ReadFile(wantPath)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(data, imageBytes) {
		t.Fatal("exported attachment bytes changed")
	}
}

func TestBuiltinAttachmentExportRejectsOtherSessionAndExistingDestination(t *testing.T) {
	home := t.TempDir()
	project := t.TempDir()
	stored, err := attachment.NewService(home).StoreReader("sess_source", "capture.png", "image/png", bytes.NewReader([]byte("image")))
	if err != nil {
		t.Fatal(err)
	}
	runner := NewBuiltinRunner(WithHomeDir(home))
	otherSession := runner.Call(context.Background(), Call{
		SessionID:   "sess_other",
		Name:        AttachmentExport,
		ProjectDirs: []string{project},
		Args:        json.RawMessage(`{"scope":"project","attachmentKey":"` + stored.AttachmentKey + `","path":"capture.png"}`),
	})
	if otherSession.Ok || decodeToolResult(t, otherSession)["reason"] != "attachment_not_found" {
		t.Fatalf("other session attachment must be rejected: %+v", otherSession)
	}

	destination := filepath.Join(project, "capture.png")
	if err := os.WriteFile(destination, []byte("existing"), 0o600); err != nil {
		t.Fatal(err)
	}
	existing := runner.Call(context.Background(), Call{
		SessionID:   "sess_source",
		Name:        AttachmentExport,
		ProjectDirs: []string{project},
		Args:        json.RawMessage(`{"scope":"project","attachmentKey":"` + stored.AttachmentKey + `","path":"capture.png"}`),
	})
	if existing.Ok || decodeToolResult(t, existing)["reason"] != "to_exists" {
		t.Fatalf("existing destination must require overwrite: %+v", existing)
	}
}

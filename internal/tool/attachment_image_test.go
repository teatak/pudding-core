package tool

import (
	"bytes"
	"context"
	"encoding/json"
	"testing"

	"github.com/teatak/pudding-core/internal/attachment"
)

func TestBuiltinAttachmentReadImageRoutesContextAttachment(t *testing.T) {
	home := t.TempDir()
	stored, err := attachment.NewService(home).StoreReader("sess_img", "photo.png", "image/png", bytes.NewReader([]byte{0x89, 'P', 'N', 'G', '\r', '\n', 0x1a, '\n'}))
	if err != nil {
		t.Fatal(err)
	}

	args, err := json.Marshal(map[string]string{"url": stored.URL})
	if err != nil {
		t.Fatal(err)
	}
	res := NewBuiltinRunner(WithHomeDir(home)).Call(context.Background(), Call{
		SessionID: "sess_img",
		Name:      AttachmentReadImage,
		Args:      args,
	})
	if !res.Ok {
		t.Fatalf("attachment image read should succeed: %+v", res)
	}
	if len(res.Attachments) != 1 || len(res.ContextAttachments) != 1 {
		t.Fatalf("image read should return display and context attachments: display=%+v context=%+v", res.Attachments, res.ContextAttachments)
	}
	if got := res.ContextAttachments[0]; got.AttachmentKey != stored.AttachmentKey || got.URL != stored.URL || got.Origin != attachment.OriginTool {
		t.Fatalf("unexpected context attachment: %+v", got)
	}
	payload := decodeToolResult(t, res)
	if payload["kind"] != "image_attachment_routed" || payload["attachmentKey"] != stored.AttachmentKey {
		t.Fatalf("unexpected payload: %+v", payload)
	}
}

func TestBuiltinAttachmentReadImageRejectsOtherSessionURL(t *testing.T) {
	home := t.TempDir()
	stored, err := attachment.NewService(home).StoreReader("sess_other", "photo.png", "image/png", bytes.NewReader([]byte{0x89, 'P', 'N', 'G', '\r', '\n', 0x1a, '\n'}))
	if err != nil {
		t.Fatal(err)
	}

	args, err := json.Marshal(map[string]string{"url": stored.URL})
	if err != nil {
		t.Fatal(err)
	}
	res := NewBuiltinRunner(WithHomeDir(home)).Call(context.Background(), Call{
		SessionID: "sess_img",
		Name:      AttachmentReadImage,
		Args:      args,
	})
	if res.Ok {
		t.Fatalf("cross-session image read should fail: %+v", res)
	}
	payload := decodeToolResult(t, res)
	if payload["reason"] != "session_mismatch" {
		t.Fatalf("unexpected payload: %+v", payload)
	}
}

func TestBuiltinAttachmentReadImageRejectsFilesystemPath(t *testing.T) {
	args, err := json.Marshal(map[string]string{"attachmentKey": "/Users/me/.pudding-dev/attachments/sessions/sess_img/blobs/photo.png"})
	if err != nil {
		t.Fatal(err)
	}
	res := NewBuiltinRunner(WithHomeDir(t.TempDir())).Call(context.Background(), Call{
		SessionID: "sess_img",
		Name:      AttachmentReadImage,
		Args:      args,
	})
	if res.Ok {
		t.Fatalf("filesystem path should not be accepted as attachmentKey: %+v", res)
	}
	payload := decodeToolResult(t, res)
	if payload["reason"] != "invalid_attachment_key" {
		t.Fatalf("unexpected payload: %+v", payload)
	}
}

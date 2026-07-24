package tool

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"testing"

	"github.com/teatak/pudding-core/internal/attachment"
	"github.com/teatak/pudding-core/internal/desktopcamera"
)

func TestBuiltinCameraCaptureRoutesAttachment(t *testing.T) {
	home := t.TempDir()
	photoBytes := []byte{0xff, 0xd8, 0xff, 0xd9}
	runner := NewBuiltinRunner(WithHomeDir(home), WithCamera(fakeToolCamera{
		photo: &desktopcamera.Photo{Data: photoBytes, MIME: "image/jpeg", Name: "camera.jpg"},
	}))

	res := runner.Call(context.Background(), Call{
		SessionID: "sess_camera",
		Name:      CameraCapture,
		Args:      json.RawMessage(`{}`),
	})
	if !res.Ok {
		t.Fatalf("camera capture should succeed: %+v", res)
	}
	if len(res.Attachments) != 1 || res.Attachments[0].Origin != attachment.OriginTool || res.Attachments[0].MIME != "image/jpeg" {
		t.Fatalf("unexpected camera attachment: %+v", res.Attachments)
	}
	if len(res.ContextAttachments) != 0 {
		t.Fatalf("camera capture must be display-only, got context attachments: %+v", res.ContextAttachments)
	}
	payload := decodeToolResult(t, res)
	if payload["attachmentKey"] == "" || payload["url"] == "" || payload["exportTool"] != AttachmentExport || payload["exportHint"] == "" {
		t.Fatalf("missing attachment metadata: %+v", payload)
	}
	path, ok, err := attachment.NewService(home).Path("sess_camera", res.Attachments[0].AttachmentKey)
	if err != nil || !ok {
		t.Fatalf("stored camera photo missing: ok=%v err=%v", ok, err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(data, photoBytes) {
		t.Fatalf("stored camera photo bytes changed")
	}
}

type fakeToolCamera struct {
	photo *desktopcamera.Photo
	err   error
}

func (f fakeToolCamera) CapturePhoto(context.Context) (*desktopcamera.Photo, error) {
	return f.photo, f.err
}

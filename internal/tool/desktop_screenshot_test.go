package tool

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"testing"
	"time"

	"github.com/teatak/pudding-core/internal/attachment"
	"github.com/teatak/pudding-core/internal/desktopscreen"
)

func TestBuiltinDesktopScreenshotRoutesAttachments(t *testing.T) {
	home := t.TempDir()
	first := []byte{0x89, 'P', 'N', 'G', '\r', '\n', 0x1a, '\n', 1}
	second := []byte{0x89, 'P', 'N', 'G', '\r', '\n', 0x1a, '\n', 2}
	runner := NewBuiltinRunner(WithHomeDir(home), WithDesktopScreen(fakeToolScreen{
		shots: []desktopscreen.Screenshot{
			{Data: first, MIME: "image/png", Name: "screen-1.png", Display: 0, DisplayCount: 2, Width: 100, Height: 80, CapturedAt: time.Now().UTC()},
			{Data: second, MIME: "image/png", Name: "screen-2.png", Display: 1, DisplayCount: 2, Width: 120, Height: 90, CapturedAt: time.Now().UTC()},
		},
	}))

	res := runner.Call(context.Background(), Call{
		SessionID: "sess_screen",
		Name:      DesktopScreenshot,
		Args:      json.RawMessage(`{}`),
	})
	if !res.Ok {
		t.Fatalf("desktop screenshot should succeed: %+v", res)
	}
	if len(res.Attachments) != 2 {
		t.Fatalf("unexpected screenshot attachment count: %+v", res.Attachments)
	}
	if len(res.ContextAttachments) != len(res.Attachments) {
		t.Fatalf("desktop screenshot should route context attachments: %+v", res.ContextAttachments)
	}
	for i, item := range res.Attachments {
		if item.Origin != attachment.OriginTool || item.MIME != "image/png" {
			t.Fatalf("unexpected screenshot attachment: %+v", item)
		}
		want := first
		if i == 1 {
			want = second
		}
		path, ok, err := attachment.NewService(home).Path("sess_screen", item.AttachmentKey)
		if err != nil || !ok {
			t.Fatalf("stored screenshot missing: ok=%v err=%v", ok, err)
		}
		data, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		if !bytes.Equal(data, want) {
			t.Fatalf("stored screenshot bytes changed")
		}
	}
	payload := decodeToolResult(t, res)
	if payload["count"] != float64(2) || payload["exportTool"] != AttachmentExport || payload["exportHint"] == "" {
		t.Fatalf("unexpected payload: %+v", payload)
	}
}

type fakeToolScreen struct {
	shots []desktopscreen.Screenshot
	err   error
}

func (f fakeToolScreen) CaptureScreenshots(context.Context, *int) ([]desktopscreen.Screenshot, error) {
	return f.shots, f.err
}

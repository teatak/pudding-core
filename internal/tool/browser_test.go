package tool

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"os"
	"testing"
	"time"

	"github.com/coder/websocket"
	"github.com/teatak/pudding-core/internal/attachment"
	"github.com/teatak/pudding-core/internal/browser"
)

func TestBuiltinBrowserScreenshotRoutesAttachment(t *testing.T) {
	home := t.TempDir()
	imageBytes := []byte{0x89, 'P', 'N', 'G', '\r', '\n', 0x1a, '\n', 0, 0, 0, 0, 'I', 'E', 'N', 'D'}
	runner := NewBuiltinRunner(WithHomeDir(home), WithBrowser(&fakeToolBrowser{
		screenshot: browser.ScreenshotResult{
			Tab:        browser.TabSnapshot{ID: "tab_1", SessionID: "sess_browser", URL: "https://example.com", Title: "Example"},
			MIME:       "image/png",
			DataBase64: base64.StdEncoding.EncodeToString(imageBytes),
			Size:       int64(len(imageBytes)),
			CapturedAt: time.Now().UTC(),
		},
	}))

	res := runner.Call(context.Background(), Call{
		SessionID: "sess_browser",
		Name:      BrowserScreenshot,
		Args:      json.RawMessage(`{"tabID":"tab_1"}`),
	})
	if !res.Ok {
		t.Fatalf("screenshot should succeed: %+v", res)
	}
	if len(res.Attachments) != 1 || res.Attachments[0].Origin != attachment.OriginTool || res.Attachments[0].MIME != "image/png" {
		t.Fatalf("unexpected screenshot attachment: %+v", res.Attachments)
	}
	payload := decodeToolResult(t, res)
	if payload["attachmentKey"] == "" || payload["url"] == "" {
		t.Fatalf("missing attachment metadata: %+v", payload)
	}
	path, ok, err := attachment.NewService(home).Path("sess_browser", res.Attachments[0].AttachmentKey)
	if err != nil || !ok {
		t.Fatalf("stored screenshot missing: ok=%v err=%v", ok, err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != string(imageBytes) {
		t.Fatalf("stored screenshot bytes changed")
	}
}

func TestBuiltinBrowserMissingService(t *testing.T) {
	res := NewBuiltinRunner().Call(context.Background(), Call{
		SessionID: "sess_browser",
		Name:      BrowserOpen,
		Args:      json.RawMessage(`{"url":"https://example.com"}`),
	})
	if res.Ok {
		t.Fatalf("missing browser service should fail: %+v", res)
	}
	payload := decodeToolResult(t, res)
	if payload["reason"] != "browser_unavailable" {
		t.Fatalf("unexpected payload: %+v", payload)
	}
}

type fakeToolBrowser struct {
	screenshot browser.ScreenshotResult
}

func (f *fakeToolBrowser) CreateTab(context.Context, string) (browser.TabSnapshot, error) {
	return browser.TabSnapshot{}, nil
}

func (f *fakeToolBrowser) ListTabs(context.Context, string) ([]browser.TabSnapshot, error) {
	return nil, nil
}

func (f *fakeToolBrowser) GetTab(context.Context, string, string) (browser.TabSnapshot, error) {
	return browser.TabSnapshot{}, nil
}

func (f *fakeToolBrowser) ReleaseTab(context.Context, string, string) error {
	return nil
}

func (f *fakeToolBrowser) ReleaseSession(context.Context, string) error {
	return nil
}

func (f *fakeToolBrowser) Open(_ context.Context, sessionID, tabID, rawURL string) (browser.TabSnapshot, error) {
	return browser.TabSnapshot{ID: tabID, SessionID: sessionID, URL: rawURL}, nil
}

func (f *fakeToolBrowser) Reveal(_ context.Context, sessionID, tabID string) (browser.TabSnapshot, error) {
	return browser.TabSnapshot{ID: tabID, SessionID: sessionID}, nil
}

func (f *fakeToolBrowser) Back(_ context.Context, sessionID, tabID string) (browser.TabSnapshot, error) {
	return browser.TabSnapshot{ID: tabID, SessionID: sessionID}, nil
}

func (f *fakeToolBrowser) Forward(_ context.Context, sessionID, tabID string) (browser.TabSnapshot, error) {
	return browser.TabSnapshot{ID: tabID, SessionID: sessionID}, nil
}

func (f *fakeToolBrowser) Reload(_ context.Context, sessionID, tabID string) (browser.TabSnapshot, error) {
	return browser.TabSnapshot{ID: tabID, SessionID: sessionID}, nil
}

func (f *fakeToolBrowser) Observe(_ context.Context, sessionID, tabID string, _ browser.ObserveOptions) (browser.ObserveResult, error) {
	tab := browser.TabSnapshot{ID: tabID, SessionID: sessionID}
	return browser.ObserveResult{Tab: tab, Title: "Example", URL: "https://example.com", Text: "Example"}, nil
}

func (f *fakeToolBrowser) Screenshot(context.Context, string, string, browser.ScreenshotOptions) (browser.ScreenshotResult, error) {
	return f.screenshot, nil
}

func (f *fakeToolBrowser) Click(context.Context, string, string, browser.ClickInput) (browser.ActionResult, error) {
	return browser.ActionResult{}, nil
}

func (f *fakeToolBrowser) Type(context.Context, string, string, browser.TypeInput) (browser.ActionResult, error) {
	return browser.ActionResult{}, nil
}

func (f *fakeToolBrowser) Scroll(context.Context, string, string, browser.ScrollInput) (browser.ActionResult, error) {
	return browser.ActionResult{}, nil
}

func (f *fakeToolBrowser) Screencast(context.Context, string, string, *websocket.Conn) error {
	return nil
}

func (f *fakeToolBrowser) Close() error { return nil }

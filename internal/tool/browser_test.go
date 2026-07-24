package tool

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"os"
	"strconv"
	"testing"
	"time"

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
	if len(res.ContextAttachments) != 1 || res.ContextAttachments[0].AttachmentKey != res.Attachments[0].AttachmentKey {
		t.Fatalf("browser screenshot should route a context attachment: %+v", res.ContextAttachments)
	}
	payload := decodeToolResult(t, res)
	if payload["attachmentKey"] == "" || payload["url"] == "" || payload["exportTool"] != AttachmentExport || payload["exportHint"] == "" {
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

func TestBuiltinBrowserStatusListsSessionTabs(t *testing.T) {
	now := time.Now().UTC()
	runner := NewBuiltinRunner(WithBrowser(&fakeToolBrowser{
		tabs: []browser.TabSnapshot{
			{ID: "tab_old", SessionID: "sess_browser", URL: "https://old.example", Title: "Old", UpdatedAt: now.Add(-time.Minute)},
			{ID: "tab_new", SessionID: "sess_browser", URL: "https://new.example", Title: "New", UpdatedAt: now},
		},
	}))

	res := runner.Call(context.Background(), Call{
		SessionID: "sess_browser",
		Name:      BrowserStatus,
		Args:      json.RawMessage(`{}`),
	})
	if !res.Ok {
		t.Fatalf("status should succeed: %+v", res)
	}
	payload := decodeToolResult(t, res)
	if payload["has_tab"] != true || payload["tab_count"] != float64(2) || len(payload["tabs"].([]any)) != 2 {
		t.Fatalf("unexpected status payload: %+v", payload)
	}
}

func TestBuiltinBrowserNavigationRequiresTabIDWhenMultipleTabsExist(t *testing.T) {
	now := time.Now().UTC()
	fake := &fakeToolBrowser{
		tabs: []browser.TabSnapshot{
			{ID: "tab_old", SessionID: "sess_browser", UpdatedAt: now.Add(-time.Minute)},
			{ID: "tab_new", SessionID: "sess_browser", UpdatedAt: now},
		},
	}
	runner := NewBuiltinRunner(WithBrowser(fake))

	res := runner.Call(context.Background(), Call{
		SessionID: "sess_browser",
		Name:      BrowserReload,
		Args:      json.RawMessage(`{}`),
	})
	if res.Ok {
		t.Fatalf("reload without tabID should fail: %+v", res)
	}
	payload := decodeToolResult(t, res)
	if payload["reason"] != "browser_tab_required" {
		t.Fatalf("unexpected reload error: %+v", payload)
	}

	res = runner.Call(context.Background(), Call{
		SessionID: "sess_browser",
		Name:      BrowserReload,
		Args:      json.RawMessage(`{"tabID":"tab_new"}`),
	})
	if !res.Ok {
		t.Fatalf("reload with tabID should succeed: %+v", res)
	}
	if fake.reloadTabID != "tab_new" {
		t.Fatalf("reload used wrong tab: %q", fake.reloadTabID)
	}
}

func TestBuiltinBrowserCloseCanCloseOneTab(t *testing.T) {
	fake := &fakeToolBrowser{
		tabs: []browser.TabSnapshot{
			{ID: "tab_a", SessionID: "sess_browser", URL: "https://a.example"},
			{ID: "tab_b", SessionID: "sess_browser", URL: "https://b.example"},
		},
	}
	runner := NewBuiltinRunner(WithBrowser(fake))

	res := runner.Call(context.Background(), Call{
		SessionID: "sess_browser",
		Name:      BrowserClose,
		Args:      json.RawMessage(`{"tabID":"tab_a"}`),
	})
	if !res.Ok {
		t.Fatalf("close tab should succeed: %+v", res)
	}
	payload := decodeToolResult(t, res)
	if payload["closed"] != float64(1) || payload["has_tab"] != true {
		t.Fatalf("unexpected close tab payload: %+v", payload)
	}
	if len(fake.tabs) != 1 || fake.tabs[0].ID != "tab_b" {
		t.Fatalf("wrong remaining tabs: %+v", fake.tabs)
	}
}

func TestBuiltinBrowserOpenCanCreateNewTab(t *testing.T) {
	fake := &fakeToolBrowser{
		tabs: []browser.TabSnapshot{{ID: "tab_existing", SessionID: "sess_browser", URL: "https://existing.example"}},
	}
	runner := NewBuiltinRunner(WithBrowser(fake))
	res := runner.Call(context.Background(), Call{
		SessionID: "sess_browser",
		Name:      BrowserOpen,
		Args:      json.RawMessage(`{"url":"https://new.example","newTab":true}`),
	})
	if !res.Ok {
		t.Fatalf("open new tab should succeed: %+v", res)
	}
	payload := decodeToolResult(t, res)
	tab := payload["tab"].(map[string]any)
	if tab["id"] != "tab_created_1" || tab["url"] != "https://new.example" {
		t.Fatalf("unexpected opened tab: %+v", payload)
	}
	if fake.blankTabs != 0 || fake.openNewTabURL != "https://new.example" {
		t.Fatalf("new tab should open its URL atomically: blankTabs=%d openNewTabURL=%q", fake.blankTabs, fake.openNewTabURL)
	}
}

func TestBuiltinBrowserCloseClosesSessionBrowser(t *testing.T) {
	fake := &fakeToolBrowser{
		tabs: []browser.TabSnapshot{
			{ID: "tab_a", SessionID: "sess_browser"},
			{ID: "tab_b", SessionID: "sess_browser"},
		},
	}
	runner := NewBuiltinRunner(WithBrowser(fake))

	res := runner.Call(context.Background(), Call{
		SessionID: "sess_browser",
		Name:      BrowserClose,
		Args:      json.RawMessage(`{}`),
	})
	if !res.Ok {
		t.Fatalf("close should succeed: %+v", res)
	}
	payload := decodeToolResult(t, res)
	if payload["closed"] != float64(2) || payload["has_tab"] != false {
		t.Fatalf("unexpected close payload: %+v", payload)
	}
	if fake.closedSession != "sess_browser" {
		t.Fatalf("closed session = %q", fake.closedSession)
	}
}

type fakeToolBrowser struct {
	screenshot     browser.ScreenshotResult
	tabs           []browser.TabSnapshot
	releasedTabIDs []string
	closedSession  string
	reloadTabID    string
	createdTabs    int
	blankTabs      int
	openNewTabURL  string
}

func (f *fakeToolBrowser) ProcessMode(context.Context, string) string {
	return "headless"
}

func (f *fakeToolBrowser) CreateTab(_ context.Context, sessionID string) (browser.TabSnapshot, error) {
	f.createdTabs++
	f.blankTabs++
	tab := browser.TabSnapshot{ID: "tab_created_" + strconv.Itoa(f.createdTabs), SessionID: sessionID, URL: "about:blank"}
	f.tabs = append(f.tabs, tab)
	return tab, nil
}

func (f *fakeToolBrowser) OpenNewTab(_ context.Context, sessionID, rawURL string) (browser.TabSnapshot, error) {
	f.createdTabs++
	f.openNewTabURL = rawURL
	tab := browser.TabSnapshot{ID: "tab_created_" + strconv.Itoa(f.createdTabs), SessionID: sessionID, URL: rawURL}
	f.tabs = append(f.tabs, tab)
	return tab, nil
}

func (f *fakeToolBrowser) ListTabs(context.Context, string) ([]browser.TabSnapshot, error) {
	return append([]browser.TabSnapshot(nil), f.tabs...), nil
}

func (f *fakeToolBrowser) GetTab(context.Context, string, string) (browser.TabSnapshot, error) {
	return browser.TabSnapshot{}, nil
}

func (f *fakeToolBrowser) ReleaseTab(_ context.Context, _ string, tabID string) error {
	f.releasedTabIDs = append(f.releasedTabIDs, tabID)
	for index, tab := range f.tabs {
		if tab.ID == tabID {
			f.tabs = append(f.tabs[:index], f.tabs[index+1:]...)
			break
		}
	}
	return nil
}

func (f *fakeToolBrowser) CloseSessionBrowser(_ context.Context, sessionID string) error {
	f.closedSession = sessionID
	return nil
}

func (f *fakeToolBrowser) ReleaseSession(context.Context, string) error {
	return nil
}

func (f *fakeToolBrowser) Open(_ context.Context, sessionID, tabID, rawURL string) (browser.TabSnapshot, error) {
	return browser.TabSnapshot{ID: tabID, SessionID: sessionID, URL: rawURL}, nil
}

func (f *fakeToolBrowser) Recover(_ context.Context, sessionID string, hint browser.RecoverHint) (browser.TabSnapshot, error) {
	return browser.TabSnapshot{ID: hint.TabID, SessionID: sessionID, URL: hint.URL, Title: hint.Title, Mode: hint.Mode}, nil
}

func (f *fakeToolBrowser) Back(_ context.Context, sessionID, tabID string) (browser.TabSnapshot, error) {
	return browser.TabSnapshot{ID: tabID, SessionID: sessionID}, nil
}

func (f *fakeToolBrowser) Forward(_ context.Context, sessionID, tabID string) (browser.TabSnapshot, error) {
	return browser.TabSnapshot{ID: tabID, SessionID: sessionID}, nil
}

func (f *fakeToolBrowser) Reload(_ context.Context, sessionID, tabID string) (browser.TabSnapshot, error) {
	f.reloadTabID = tabID
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

func (f *fakeToolBrowser) Close() error { return nil }

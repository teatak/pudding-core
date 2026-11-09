package tool

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"image"
	"image/png"
	"os"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/teatak/pudding-core/internal/attachment"
	"github.com/teatak/pudding-core/internal/browser"
)

func TestBuiltinBrowserObservePagination(t *testing.T) {
	next := 120
	fake := &fakeToolBrowser{observation: browser.ObserveResult{
		Elements:      []browser.ObservedElement{{Index: 100, Tag: "button", Selector: "#create"}},
		TotalElements: 125, NextOffset: &next,
	}}
	runner := NewBuiltinRunner(WithBrowser(fake))
	res := runner.Call(context.Background(), Call{SessionID: "sess_browser", Name: BrowserObserve, Args: json.RawMessage(`{"tabID":"tab_1","maxElements":20,"offset":100}`)})
	if !res.Ok || fake.observeOptions.Offset != 100 || fake.observeOptions.MaxElements != 20 {
		t.Fatalf("observation options lost: %+v result=%+v", fake.observeOptions, res)
	}
	observation := decodeToolResult(t, res)["observation"].(map[string]any)
	if observation["totalElements"] != float64(125) || observation["nextOffset"] != float64(120) {
		t.Fatalf("pagination lost: %+v", observation)
	}
	fake.observation.NextOffset = nil
	res = runner.Call(context.Background(), Call{SessionID: "sess_browser", Name: BrowserObserve, Args: json.RawMessage(`{"tabID":"tab_1","offset":120}`)})
	observation = decodeToolResult(t, res)["observation"].(map[string]any)
	if next, exists := observation["nextOffset"]; !exists || next != nil {
		t.Fatalf("last page must have explicit null nextOffset: %+v", observation)
	}
	res = runner.Call(context.Background(), Call{SessionID: "sess_browser", Name: BrowserObserve, Args: json.RawMessage(`{"tabID":"tab_1","offset":-1}`)})
	if res.Ok || fake.observeOptions.Offset != 120 {
		t.Fatalf("invalid offset reached browser: %+v result=%+v", fake.observeOptions, res)
	}
}

func TestBuiltinBrowserScreenshotRejectsFullPage(t *testing.T) {
	runner := NewBuiltinRunner(WithHomeDir(t.TempDir()), WithBrowser(&fakeToolBrowser{}))
	res := runner.Call(context.Background(), Call{SessionID: "sess_browser", Name: BrowserScreenshot, Args: json.RawMessage(`{"tabID":"tab_1","fullPage":true}`)})
	if res.Ok || !strings.Contains(res.Content, "Full-page screenshots are not supported") || len(res.Attachments) != 0 {
		t.Fatalf("expected explicit full-page rejection: %+v", res)
	}
}

func TestBuiltinBrowserScreenshotRoutesAttachment(t *testing.T) {
	home := t.TempDir()
	var imageData bytes.Buffer
	if err := png.Encode(&imageData, image.NewNRGBA(image.Rect(0, 0, 2504, 1988))); err != nil {
		t.Fatal(err)
	}
	imageBytes := imageData.Bytes()
	runner := NewBuiltinRunner(WithHomeDir(home), WithBrowser(&fakeToolBrowser{
		screenshot: browser.ScreenshotResult{
			Tab:        browser.TabSnapshot{ID: "tab_1", SessionID: "sess_browser", URL: "https://example.com", Title: "Example"},
			MIME:       "image/png",
			DataBase64: base64.StdEncoding.EncodeToString(imageBytes),
			Size:       int64(len(imageBytes)),
			Width:      2504, Height: 1988, ViewportWidth: 1252, ViewportHeight: 994, DeviceScaleFactor: 2,
			CaptureBounds: browser.Bounds{Width: 1252, Height: 994},
			CapturedAt:    time.Now().UTC(),
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
	modelWidth, modelHeight := attachment.ScreenshotDimensions(2504, 1988)
	if payload["attachmentKey"] == "" || payload["url"] == "" || payload["exportTool"] != AttachmentExport || payload["exportHint"] == "" {
		t.Fatalf("missing attachment metadata: %+v", payload)
	}
	if payload["width"] != float64(2504) || payload["height"] != float64(1988) || payload["modelWidth"] != float64(modelWidth) || payload["modelHeight"] != float64(modelHeight) || payload["viewportWidth"] != float64(1252) || payload["viewportHeight"] != float64(994) || payload["deviceScaleFactor"] != float64(2) || payload["fullPage"] != false || payload["coordinateSpace"] != "viewport_css_pixels" || payload["coordinateHint"] == "" {
		t.Fatalf("screenshot lost coordinate metadata: %+v", payload)
	}
	// A model-image point at its center maps to the current CSS viewport,
	// independently of Retina pixels and the provider image downscale.
	bounds := payload["captureBounds"].(map[string]any)
	y := bounds["y"].(float64) + payload["modelHeight"].(float64)/2*bounds["height"].(float64)/payload["modelHeight"].(float64)
	if y != 497 {
		t.Fatalf("incorrect image-to-viewport mapping: y=%v", y)
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

func TestHistoricalScreenshotCoordinateProjection(t *testing.T) {
	original := `{"ok":true,"width":3032,"height":1852,"modelWidth":2048,"modelHeight":1251,"captureBounds":{"x":20,"y":10,"width":1516,"height":926},"attachmentKey":"saved"}`
	view := ModelResultContent(BrowserScreenshot, true, original, "t1", "c1", true)
	var payload map[string]any
	if err := json.Unmarshal([]byte(view), &payload); err != nil {
		t.Fatal(err)
	}
	w, h := attachment.ScreenshotDimensions(3032, 1852)
	if payload["modelWidth"] != float64(w) || payload["modelHeight"] != float64(h) || payload["width"] != float64(3032) || payload["attachmentKey"] != "saved" {
		t.Fatalf("wrong projection: %s", view)
	}
	if ModelResultContent(BrowserScreenshot, true, view, "t1", "c1", true) != view {
		t.Fatal("projection is not stable")
	}
	if !strings.Contains(original, `"modelWidth":2048`) {
		t.Fatal("canonical result changed")
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

// Without a tabID the tool picks the only tab itself; a failed open there must
// still fail the call instead of reporting an empty tab.
func TestBuiltinBrowserOpenWithoutTabIDReportsOpenFailure(t *testing.T) {
	for _, tabs := range [][]browser.TabSnapshot{nil, {{ID: "tab_only", SessionID: "sess_browser"}}} {
		fake := &fakeToolBrowser{tabs: tabs, openErr: browser.ErrUnavailable}
		runner := NewBuiltinRunner(WithBrowser(fake))
		res := runner.Call(context.Background(), Call{
			SessionID: "sess_browser",
			Name:      BrowserOpen,
			Args:      json.RawMessage(`{"url":"https://blocked.example"}`),
		})
		if res.Ok {
			t.Fatalf("open with %d tabs must report the open failure: %+v", len(tabs), res)
		}
	}
}

type fakeToolBrowser struct {
	hoverInput  browser.HoverInput
	dialogInput browser.DialogInput
	keyInput    browser.KeyInput
	waitInput   browser.WaitInput
	actionError error

	observation     browser.ObserveResult
	observeOptions  browser.ObserveOptions
	screenshot      browser.ScreenshotResult
	tabs            []browser.TabSnapshot
	releasedTabIDs  []string
	closedSession   string
	closedWindow    string
	closedWindowTab string
	reloadTabID     string
	createdTabs     int
	blankTabs       int
	openNewTabURL   string
	openErr         error
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
	if f.openErr != nil {
		return browser.TabSnapshot{}, f.openErr
	}
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

func (f *fakeToolBrowser) Observe(_ context.Context, sessionID, tabID string, options browser.ObserveOptions) (browser.ObserveResult, error) {
	f.observeOptions = options
	out := f.observation
	out.Tab = browser.TabSnapshot{ID: tabID, SessionID: sessionID}
	out.Title, out.URL, out.Text = "Example", "https://example.com", "Example"
	return out, nil
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

func (f *fakeToolBrowser) Select(context.Context, string, string, browser.SelectInput) (browser.ActionResult, error) {
	return browser.ActionResult{Action: "select"}, nil
}

func (f *fakeToolBrowser) PressKey(_ context.Context, _, tabID string, in browser.KeyInput) (browser.ActionResult, error) {
	in.TabID = tabID
	f.keyInput = in
	return browser.ActionResult{Action: "press-key", Result: map[string]any{"delivered": true, "effect": "unverified"}}, f.actionError
}

func (f *fakeToolBrowser) Wait(_ context.Context, _, tabID string, in browser.WaitInput) (browser.ActionResult, error) {
	in.TabID = tabID
	f.waitInput = in
	return browser.ActionResult{Action: "wait", Result: map[string]any{"matched": true}}, f.actionError
}

func TestBuiltinBrowserKeyAndWait(t *testing.T) {
	fake := &fakeToolBrowser{}
	runner := NewBuiltinRunner(WithBrowser(fake))
	key := runner.Call(context.Background(), Call{SessionID: "session", Name: BrowserPressKey, Args: json.RawMessage(`{"tabID":"tab","selector":"#input","key":"Enter","modifiers":["Shift"]}`)})
	if !key.Ok || fake.keyInput.Key != "Enter" || fake.keyInput.TabID != "tab" || len(fake.keyInput.Modifiers) != 1 {
		t.Fatalf("key input lost: %+v %+v", key, fake.keyInput)
	}
	wait := runner.Call(context.Background(), Call{SessionID: "session", Name: BrowserWait, Args: json.RawMessage(`{"tabID":"tab","condition":"text","text":"Saved","timeoutMs":500}`)})
	if !wait.Ok || fake.waitInput.Condition != "text" || fake.waitInput.Text != "Saved" || fake.waitInput.TimeoutMS == nil || *fake.waitInput.TimeoutMS != 500 {
		t.Fatalf("wait input lost: %+v %+v", wait, fake.waitInput)
	}
	fake.actionError = context.Canceled
	cancelled := runner.Call(context.Background(), Call{SessionID: "session", Name: BrowserPressKey, Args: json.RawMessage(`{"tabID":"tab","key":"Enter"}`)})
	if cancelled.Ok || !strings.Contains(cancelled.Content, "browser_operation_cancelled") || !strings.Contains(cancelled.Content, "observe before retrying") {
		t.Fatalf("cancellation was misreported: %+v", cancelled)
	}
}

func (f *fakeToolBrowser) CloseWindow(_ context.Context, _ string, tabID, windowID string) error {
	f.closedWindow = windowID
	f.closedWindowTab = tabID
	return f.actionError
}

func TestBuiltinBrowserFrameAndPopupTargets(t *testing.T) {
	fake := &fakeToolBrowser{}
	runner := NewBuiltinRunner(WithBrowser(fake))
	observed := runner.Call(context.Background(), Call{SessionID: "session", Name: BrowserObserve, Args: json.RawMessage(`{"tabID":"tab","windowID":"popup","frameID":"frame"}`)})
	if !observed.Ok || fake.observeOptions.FrameID != "frame" || fake.observeOptions.WindowID != "popup" {
		t.Fatalf("target lost: %+v %+v", observed, fake.observeOptions)
	}
	key := runner.Call(context.Background(), Call{SessionID: "session", Name: BrowserPressKey, Args: json.RawMessage(`{"tabID":"tab","windowID":"popup","frameID":"frame","key":"Enter"}`)})
	if !key.Ok || fake.keyInput.FrameID != "frame" || fake.keyInput.WindowID != "popup" {
		t.Fatalf("target lost: %+v %+v", key, fake.keyInput)
	}
	closed := runner.Call(context.Background(), Call{SessionID: "session", Name: BrowserClose, Args: json.RawMessage(`{"tabID":"tab","windowID":"popup"}`)})
	if !closed.Ok || fake.closedWindow != "popup" || fake.closedWindowTab != "tab" || fake.closedSession != "" || len(fake.releasedTabIDs) != 0 {
		t.Fatalf("popup close affected parent: %+v %+v", closed, fake)
	}
	fake.actionError = context.Canceled
	failed := runner.Call(context.Background(), Call{SessionID: "session", Name: BrowserClose, Args: json.RawMessage(`{"tabID":"tab","windowID":"popup"}`)})
	if failed.Ok {
		t.Fatalf("failed popup close reported success: %+v", failed)
	}
}

func (f *fakeToolBrowser) Hover(_ context.Context, sessionID, tabID string, in browser.HoverInput) (browser.ActionResult, error) {
	f.hoverInput = in
	return browser.ActionResult{Tab: browser.TabSnapshot{ID: tabID, SessionID: sessionID}, Action: "hover"}, f.actionError
}

func TestBuiltinBrowserHoverAndNewTargets(t *testing.T) {
	fake := &fakeToolBrowser{}
	runner := NewBuiltinRunner(WithBrowser(fake))
	res := runner.Call(context.Background(), Call{SessionID: "session", Name: BrowserHover, Args: json.RawMessage(`{"tabID":"tab","frameID":"frame","windowID":"popup","selector":"#menu"}`)})
	if !res.Ok || fake.hoverInput.FrameID != "frame" || fake.hoverInput.WindowID != "popup" || fake.hoverInput.Selector != "#menu" {
		t.Fatalf("hover target lost: %+v %+v", res, fake.hoverInput)
	}
	for _, condition := range []string{"newTab", "newWindow"} {
		args, _ := json.Marshal(map[string]any{"tabID": "tab", "condition": condition, "knownIDs": []string{}})
		waited := runner.Call(context.Background(), Call{SessionID: "session", Name: BrowserWait, Args: args})
		if !waited.Ok || fake.waitInput.KnownIDs == nil || len(*fake.waitInput.KnownIDs) != 0 || fake.waitInput.Condition != condition {
			t.Fatalf("empty baseline lost: %+v %+v", waited, fake.waitInput)
		}
	}
	fake.actionError = context.Canceled
	cancelled := runner.Call(context.Background(), Call{SessionID: "session", Name: BrowserHover, Args: json.RawMessage(`{"tabID":"tab","selector":"#menu"}`)})
	if cancelled.Ok || !strings.Contains(cancelled.Content, "browser_operation_cancelled") {
		t.Fatalf("hover cancellation lost: %+v", cancelled)
	}
}

func (f *fakeToolBrowser) Dialog(_ context.Context, sessionID, tabID string, in browser.DialogInput) (browser.ActionResult, error) {
	f.dialogInput = in
	return browser.ActionResult{Tab: browser.TabSnapshot{ID: tabID, SessionID: sessionID}, Action: "dialog"}, f.actionError
}

func TestBuiltinBrowserDialogDispatch(t *testing.T) {
	fake := &fakeToolBrowser{}
	runner := NewBuiltinRunner(WithBrowser(fake))
	res := runner.Call(context.Background(), Call{SessionID: "session", Name: BrowserDialog, Args: json.RawMessage(`{"tabID":"tab","windowID":"popup","action":"respond","dialogID":"dialog","accept":true,"promptText":""}`)})
	if !res.Ok || fake.dialogInput.WindowID != "popup" || fake.dialogInput.DialogID != "dialog" || fake.dialogInput.Accept == nil || !*fake.dialogInput.Accept || fake.dialogInput.PromptText == nil || *fake.dialogInput.PromptText != "" {
		t.Fatalf("dialog response lost: %+v %+v", res, fake.dialogInput)
	}
	fake.actionError = context.Canceled
	res = runner.Call(context.Background(), Call{SessionID: "session", Name: BrowserDialog, Args: json.RawMessage(`{"tabID":"tab","action":"inspect"}`)})
	if res.Ok {
		t.Fatalf("cancelled dialog reported success: %+v", res)
	}
}

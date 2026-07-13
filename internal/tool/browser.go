package tool

import (
	"bytes"
	"context"
	"encoding/base64"
	"errors"
	"strings"

	"github.com/teatak/pudding-core/internal/attachment"
	"github.com/teatak/pudding-core/internal/browser"
	"github.com/teatak/pudding-core/internal/store"
)

func (r *BuiltinRunner) browserOpen(ctx context.Context, call Call) Result {
	out := browserBaseResult(call)
	if res, ok := r.browserReady(call, out); !ok {
		return res
	}
	var args struct {
		URL    string `json:"url"`
		TabID  string `json:"tabID"`
		NewTab bool   `json:"newTab"`
	}
	if err := decodeStructToolArgs(call.Args, &args); err != nil {
		return toolJSONError(out, "invalid_arguments", err.Error())
	}
	if args.NewTab && strings.TrimSpace(args.TabID) != "" {
		return toolJSONError(out, "invalid_arguments", "tabID and newTab cannot be used together")
	}
	var tab browser.TabSnapshot
	var err error
	if args.NewTab {
		tab, err = r.browser.OpenNewTab(ctx, call.SessionID, args.URL)
	} else if strings.TrimSpace(args.TabID) == "" {
		tabs, err := r.browserTabs(ctx, call.SessionID)
		if err != nil {
			return browserToolError(out, err)
		}
		switch len(tabs) {
		case 0:
		case 1:
			args.TabID = tabs[0].ID
		default:
			return browserToolError(out, browser.ErrTabRequired)
		}
		tab, err = r.browser.Open(ctx, call.SessionID, args.TabID, args.URL)
	} else {
		tab, err = r.browser.Open(ctx, call.SessionID, args.TabID, args.URL)
	}
	if err != nil {
		return browserToolError(out, err)
	}
	if err := r.syncBrowserState(ctx, call.SessionID, tab); err != nil {
		return browserToolError(out, err)
	}
	out.Ok = true
	out.Content = jsonString(map[string]any{"ok": true, "tab": tab})
	out.SummaryKind = SummaryReturnedFields
	out.SummaryCount = 2
	return out
}

func (r *BuiltinRunner) browserObserve(ctx context.Context, call Call) Result {
	out := browserBaseResult(call)
	if res, ok := r.browserReady(call, out); !ok {
		return res
	}
	var args struct {
		TabID        string `json:"tabID"`
		MaxTextChars int    `json:"maxTextChars"`
		MaxElements  int    `json:"maxElements"`
	}
	if err := decodeStructToolArgs(call.Args, &args); err != nil {
		return toolJSONError(out, "invalid_arguments", err.Error())
	}
	tabID, err := r.browserResolveTabID(ctx, call.SessionID, args.TabID)
	if err != nil {
		return browserToolError(out, err)
	}
	result, err := r.browser.Observe(ctx, call.SessionID, tabID, browser.ObserveOptions{
		MaxTextChars: args.MaxTextChars,
		MaxElements:  args.MaxElements,
	})
	if err != nil {
		return browserToolError(out, err)
	}
	if err := r.syncBrowserState(ctx, call.SessionID, result.Tab); err != nil {
		return browserToolError(out, err)
	}
	out.Ok = true
	out.Content = jsonString(map[string]any{"ok": true, "observation": result})
	out.SummaryKind = SummaryReturnedItems
	out.SummaryCount = len(result.Elements)
	return out
}

func (r *BuiltinRunner) browserScreenshot(ctx context.Context, call Call) Result {
	out := browserBaseResult(call)
	if res, ok := r.browserReady(call, out); !ok {
		return res
	}
	var args struct {
		TabID    string `json:"tabID"`
		FullPage bool   `json:"fullPage"`
	}
	if err := decodeStructToolArgs(call.Args, &args); err != nil {
		return toolJSONError(out, "invalid_arguments", err.Error())
	}
	tabID, err := r.browserResolveTabID(ctx, call.SessionID, args.TabID)
	if err != nil {
		return browserToolError(out, err)
	}
	result, err := r.browser.Screenshot(ctx, call.SessionID, tabID, browser.ScreenshotOptions{FullPage: args.FullPage})
	if err != nil {
		return browserToolError(out, err)
	}
	if err := r.syncBrowserState(ctx, call.SessionID, result.Tab); err != nil {
		return browserToolError(out, err)
	}
	data, err := base64.StdEncoding.DecodeString(result.DataBase64)
	if err != nil {
		return toolJSONError(out, "invalid_screenshot", err.Error())
	}
	stored, err := attachment.NewService(r.homeDir).StoreReader(call.SessionID, "browser-screenshot.png", result.MIME, bytes.NewReader(data))
	if err != nil {
		return toolJSONError(out, "attachment_store_failed", err.Error())
	}
	stored.Origin = attachment.OriginTool
	out.Ok = true
	out.Attachments = []store.Attachment{stored}
	out.ContextAttachments = []store.Attachment{stored}
	out.Content = jsonString(map[string]any{
		"ok":            true,
		"tab":           result.Tab,
		"mime":          result.MIME,
		"size":          result.Size,
		"capturedAt":    result.CapturedAt,
		"attachmentKey": stored.AttachmentKey,
		"url":           stored.URL,
	})
	out.SummaryKind = SummaryReturnedFields
	out.SummaryCount = 7
	return out
}

func (r *BuiltinRunner) browserClick(ctx context.Context, call Call) Result {
	out := browserBaseResult(call)
	if res, ok := r.browserReady(call, out); !ok {
		return res
	}
	var args browser.ClickInput
	if err := decodeStructToolArgs(call.Args, &args); err != nil {
		return toolJSONError(out, "invalid_arguments", err.Error())
	}
	tabID, err := r.browserResolveTabID(ctx, call.SessionID, args.TabID)
	if err != nil {
		return browserToolError(out, err)
	}
	args.TabID = tabID
	result, err := r.browser.Click(ctx, call.SessionID, args.TabID, args)
	if err != nil {
		return browserToolError(out, err)
	}
	if err := r.syncBrowserState(ctx, call.SessionID, result.Tab); err != nil {
		return browserToolError(out, err)
	}
	out.Ok = true
	out.Content = jsonString(map[string]any{"ok": true, "action": result})
	out.SummaryKind = SummaryReturnedFields
	out.SummaryCount = 2
	return out
}

func (r *BuiltinRunner) browserType(ctx context.Context, call Call) Result {
	out := browserBaseResult(call)
	if res, ok := r.browserReady(call, out); !ok {
		return res
	}
	var args browser.TypeInput
	if err := decodeStructToolArgs(call.Args, &args); err != nil {
		return toolJSONError(out, "invalid_arguments", err.Error())
	}
	tabID, err := r.browserResolveTabID(ctx, call.SessionID, args.TabID)
	if err != nil {
		return browserToolError(out, err)
	}
	args.TabID = tabID
	result, err := r.browser.Type(ctx, call.SessionID, args.TabID, args)
	if err != nil {
		return browserToolError(out, err)
	}
	if err := r.syncBrowserState(ctx, call.SessionID, result.Tab); err != nil {
		return browserToolError(out, err)
	}
	out.Ok = true
	out.Content = jsonString(map[string]any{"ok": true, "action": result})
	out.SummaryKind = SummaryReturnedFields
	out.SummaryCount = 2
	return out
}

func (r *BuiltinRunner) browserScroll(ctx context.Context, call Call) Result {
	out := browserBaseResult(call)
	if res, ok := r.browserReady(call, out); !ok {
		return res
	}
	var args browser.ScrollInput
	if err := decodeStructToolArgs(call.Args, &args); err != nil {
		return toolJSONError(out, "invalid_arguments", err.Error())
	}
	tabID, err := r.browserResolveTabID(ctx, call.SessionID, args.TabID)
	if err != nil {
		return browserToolError(out, err)
	}
	args.TabID = tabID
	result, err := r.browser.Scroll(ctx, call.SessionID, args.TabID, args)
	if err != nil {
		return browserToolError(out, err)
	}
	if err := r.syncBrowserState(ctx, call.SessionID, result.Tab); err != nil {
		return browserToolError(out, err)
	}
	out.Ok = true
	out.Content = jsonString(map[string]any{"ok": true, "action": result})
	out.SummaryKind = SummaryReturnedFields
	out.SummaryCount = 2
	return out
}

func (r *BuiltinRunner) browserStatus(ctx context.Context, call Call) Result {
	out := browserBaseResult(call)
	if res, ok := r.browserReady(call, out); !ok {
		return res
	}
	tabs, err := r.browserTabs(ctx, call.SessionID)
	if err != nil {
		return browserToolError(out, err)
	}
	status := map[string]any{
		"ok":           true,
		"has_tab":      len(tabs) > 0,
		"tab_count":    len(tabs),
		"tabs":         tabs,
		"process_mode": r.browser.ProcessMode(ctx, call.SessionID),
	}
	out.Ok = true
	out.Content = jsonString(status)
	out.SummaryKind = SummaryReturnedFields
	out.SummaryCount = len(status)
	return out
}

func (r *BuiltinRunner) browserNavigate(ctx context.Context, call Call, action string) Result {
	out := browserBaseResult(call)
	if res, ok := r.browserReady(call, out); !ok {
		return res
	}
	var args struct {
		TabID string `json:"tabID"`
	}
	if err := decodeStructToolArgs(call.Args, &args); err != nil {
		return toolJSONError(out, "invalid_arguments", err.Error())
	}
	tabID, err := r.browserResolveTabID(ctx, call.SessionID, args.TabID)
	if err != nil {
		return browserToolError(out, err)
	}
	var tab browser.TabSnapshot
	switch action {
	case "back":
		tab, err = r.browser.Back(ctx, call.SessionID, tabID)
	case "forward":
		tab, err = r.browser.Forward(ctx, call.SessionID, tabID)
	case "reload":
		tab, err = r.browser.Reload(ctx, call.SessionID, tabID)
	default:
		return toolJSONError(out, "invalid_browser_action", action)
	}
	if err != nil {
		return browserToolError(out, err)
	}
	if err := r.syncBrowserState(ctx, call.SessionID, tab); err != nil {
		return browserToolError(out, err)
	}
	out.Ok = true
	out.Content = jsonString(map[string]any{"ok": true, "action": action, "tab": tab})
	out.SummaryKind = SummaryReturnedFields
	out.SummaryCount = 3
	return out
}

func (r *BuiltinRunner) browserClose(ctx context.Context, call Call) Result {
	out := browserBaseResult(call)
	if res, ok := r.browserReady(call, out); !ok {
		return res
	}
	var args struct {
		TabID string `json:"tabID"`
	}
	if err := decodeStructToolArgs(call.Args, &args); err != nil {
		return toolJSONError(out, "invalid_arguments", err.Error())
	}
	tabs, err := r.browserTabs(ctx, call.SessionID)
	if err != nil {
		return browserToolError(out, err)
	}
	requestedTabID := strings.TrimSpace(args.TabID)
	if requestedTabID != "" {
		closed := 0
		for _, tab := range tabs {
			if tab.ID == requestedTabID {
				closed = 1
				break
			}
		}
		if err := r.browser.ReleaseTab(ctx, call.SessionID, requestedTabID); err != nil && !errors.Is(err, browser.ErrTabNotFound) {
			return browserToolError(out, err)
		}
		if r.browserState != nil {
			if err := r.browserState.DeleteBrowserState(ctx, call.SessionID, requestedTabID); err != nil {
				return browserToolError(out, err)
			}
		}
		remaining, err := r.browser.ListTabs(ctx, call.SessionID)
		if err != nil {
			return browserToolError(out, err)
		}
		out.Ok = true
		out.Content = jsonString(map[string]any{"ok": true, "closed": closed, "has_tab": len(remaining) > 0, "tabs": remaining})
		out.SummaryKind = SummaryReturnedFields
		out.SummaryCount = 4
		return out
	}
	closed := 0
	for _, tab := range tabs {
		if strings.TrimSpace(tab.ID) == "" {
			continue
		}
		closed++
	}
	if err := r.browser.CloseSessionBrowser(ctx, call.SessionID); err != nil {
		return browserToolError(out, err)
	}
	if r.browserState != nil {
		if err := r.browserState.ClearBrowserState(ctx, call.SessionID); err != nil {
			return browserToolError(out, err)
		}
	}
	out.Ok = true
	out.Content = jsonString(map[string]any{"ok": true, "closed": closed, "has_tab": false})
	out.SummaryKind = SummaryReturnedFields
	out.SummaryCount = 3
	return out
}

func browserBaseResult(call Call) Result {
	return Result{CallID: call.CallID, Name: call.Name}
}

func (r *BuiltinRunner) browserReady(call Call, out Result) (Result, bool) {
	if strings.TrimSpace(call.SessionID) == "" {
		return toolJSONError(out, "session_required", "session id is required"), false
	}
	if r.browser == nil {
		return toolJSONError(out, "browser_unavailable", "managed browser service is unavailable"), false
	}
	return out, true
}

func (r *BuiltinRunner) browserResolveTabID(ctx context.Context, sessionID, tabID string) (string, error) {
	tabID = strings.TrimSpace(tabID)
	if tabID != "" {
		return tabID, nil
	}
	tabs, err := r.browserTabs(ctx, sessionID)
	if err != nil {
		return "", err
	}
	if len(tabs) != 1 {
		return "", browser.ErrTabRequired
	}
	return tabs[0].ID, nil
}

func (r *BuiltinRunner) browserTabs(ctx context.Context, sessionID string) ([]browser.TabSnapshot, error) {
	tabs, err := r.browser.ListTabs(ctx, sessionID)
	if err != nil || r.browserState == nil {
		return tabs, err
	}
	support, ok := r.browser.(browser.MetadataRecoverySupport)
	if !ok || !support.SupportsMetadataRecovery() {
		return tabs, nil
	}
	states, err := r.browserState.ListBrowserStates(ctx, sessionID)
	if err != nil {
		return nil, err
	}
	live := make(map[string]struct{}, len(tabs))
	for _, tab := range tabs {
		live[tab.ID] = struct{}{}
	}
	for _, state := range states {
		if _, ok := live[state.TabID]; ok {
			continue
		}
		tab, err := r.browser.Recover(ctx, sessionID, browser.RecoverHint{
			TabID:      state.TabID,
			URL:        state.URL,
			Title:      state.Title,
			FaviconURL: state.FaviconURL,
			Mode:       state.Mode,
			CreatedAt:  state.CreatedAt,
		})
		if errors.Is(err, browser.ErrTabNotFound) {
			continue
		}
		if err != nil {
			return nil, err
		}
		live[tab.ID] = struct{}{}
		tabs = append(tabs, tab)
	}
	return tabs, nil
}

func (r *BuiltinRunner) syncBrowserState(ctx context.Context, sessionID string, tab browser.TabSnapshot) error {
	if r.browserState == nil {
		return nil
	}
	in := store.BrowserStateInput{
		SessionID:  sessionID,
		TabID:      tab.ID,
		URL:        tab.URL,
		Title:      tab.Title,
		FaviconURL: tab.FaviconURL,
	}
	if err := store.NormalizeBrowserStateInput(&in); err != nil {
		if strings.TrimSpace(tab.ID) == "" {
			return nil
		}
		return r.browserState.DeleteBrowserState(ctx, sessionID, tab.ID)
	}
	_, err := r.browserState.PutBrowserState(ctx, in)
	return err
}

func browserToolError(out Result, err error) Result {
	reason := "browser_error"
	if code := browser.ErrorCode(err); code != "" {
		reason = code
	} else if errors.Is(err, browser.ErrUnavailable) {
		reason = "browser_unavailable"
	} else if errors.Is(err, browser.ErrTabRequired) {
		reason = "browser_tab_required"
	} else if errors.Is(err, browser.ErrTabNotFound) {
		reason = "browser_tab_not_found"
	}
	return toolJSONError(out, reason, err.Error())
}

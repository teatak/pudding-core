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
		URL   string `json:"url"`
		TabID string `json:"tabID"`
	}
	if err := decodeStructToolArgs(call.Args, &args); err != nil {
		return toolJSONError(out, "invalid_arguments", err.Error())
	}
	tab, err := r.browser.Open(ctx, call.SessionID, args.TabID, args.URL)
	if err != nil {
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
	result, err := r.browser.Observe(ctx, call.SessionID, args.TabID, browser.ObserveOptions{
		MaxTextChars: args.MaxTextChars,
		MaxElements:  args.MaxElements,
	})
	if err != nil {
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
	result, err := r.browser.Screenshot(ctx, call.SessionID, args.TabID, browser.ScreenshotOptions{FullPage: args.FullPage})
	if err != nil {
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
	result, err := r.browser.Click(ctx, call.SessionID, args.TabID, args)
	if err != nil {
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
	result, err := r.browser.Type(ctx, call.SessionID, args.TabID, args)
	if err != nil {
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
	result, err := r.browser.Scroll(ctx, call.SessionID, args.TabID, args)
	if err != nil {
		return browserToolError(out, err)
	}
	out.Ok = true
	out.Content = jsonString(map[string]any{"ok": true, "action": result})
	out.SummaryKind = SummaryReturnedFields
	out.SummaryCount = 2
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

func browserToolError(out Result, err error) Result {
	reason := "browser_error"
	if errors.Is(err, browser.ErrUnavailable) {
		reason = "browser_unavailable"
	} else if errors.Is(err, browser.ErrTabRequired) {
		reason = "browser_tab_required"
	} else if errors.Is(err, browser.ErrTabNotFound) {
		reason = "browser_tab_not_found"
	}
	return toolJSONError(out, reason, err.Error())
}

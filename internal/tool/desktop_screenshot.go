package tool

import (
	"bytes"
	"context"

	"github.com/teatak/pudding-core/internal/attachment"
	"github.com/teatak/pudding-core/internal/desktopscreen"
	"github.com/teatak/pudding-core/internal/store"
)

type DesktopScreenCapturer interface {
	CaptureScreenshots(ctx context.Context, display *int) ([]desktopscreen.Screenshot, error)
}

func (r *BuiltinRunner) desktopScreenshot(ctx context.Context, call Call) Result {
	out := Result{CallID: call.CallID, Name: call.Name}
	if r.screen == nil {
		return toolJSONError(out, desktopscreen.CodeUnsupported, "desktop screenshot is unavailable")
	}
	if call.SessionID == "" {
		return toolJSONError(out, "session_required", "session id is required to route desktop screenshot attachments")
	}
	var args struct {
		Display *int `json:"display"`
	}
	if err := decodeStructToolArgs(call.Args, &args); err != nil {
		return toolJSONError(out, "invalid_arguments", err.Error())
	}
	shots, err := r.screen.CaptureScreenshots(ctx, args.Display)
	if err != nil {
		return toolJSONError(out, desktopscreen.Code(err), err.Error())
	}
	if len(shots) == 0 {
		return toolJSONError(out, desktopscreen.CodeNoActiveDisplay, "desktop screenshot returned no images")
	}

	service := attachment.NewService(r.homeDir)
	stored := make([]store.Attachment, 0, len(shots))
	items := make([]map[string]any, 0, len(shots))
	for _, shot := range shots {
		if len(shot.Data) == 0 {
			continue
		}
		mime := shot.MIME
		if mime == "" {
			mime = "image/png"
		}
		name := shot.Name
		if name == "" {
			name = "desktop-screenshot.png"
		}
		item, err := service.StoreReader(call.SessionID, name, mime, bytes.NewReader(shot.Data))
		if err != nil {
			return toolJSONError(out, "attachment_store_failed", err.Error())
		}
		item.Origin = attachment.OriginTool
		stored = append(stored, item)
		items = append(items, map[string]any{
			"display":       shot.Display,
			"displayCount":  shot.DisplayCount,
			"width":         shot.Width,
			"height":        shot.Height,
			"mime":          mime,
			"size":          len(shot.Data),
			"capturedAt":    shot.CapturedAt,
			"attachmentKey": item.AttachmentKey,
			"url":           item.URL,
		})
	}
	if len(stored) == 0 {
		return toolJSONError(out, desktopscreen.CodeNoActiveDisplay, "desktop screenshot returned no images")
	}
	out.Ok = true
	out.Attachments = stored
	out.Content = jsonString(map[string]any{
		"ok":          true,
		"attachments": items,
		"count":       len(stored),
	})
	out.SummaryKind = SummaryReturnedItems
	out.SummaryCount = len(stored)
	return out
}

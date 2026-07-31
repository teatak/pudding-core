package tool

import (
	"bytes"
	"context"
	"time"

	"github.com/teatak/pudding-core/internal/attachment"
	"github.com/teatak/pudding-core/internal/desktopcamera"
	"github.com/teatak/pudding-core/internal/store"
)

type CameraCapturer interface {
	CapturePhoto(ctx context.Context) (*desktopcamera.Photo, error)
}

func (r *BuiltinRunner) cameraCapture(ctx context.Context, call Call) Result {
	out := Result{CallID: call.CallID, Name: call.Name}
	if r.camera == nil {
		return toolJSONError(out, desktopcamera.CodeUnsupported, "camera capture is unavailable")
	}
	if call.SessionID == "" {
		return toolJSONError(out, "session_required", "session id is required to route camera photo attachments")
	}
	photo, err := r.camera.CapturePhoto(ctx)
	if err != nil {
		return toolJSONError(out, desktopcamera.Code(err), err.Error())
	}
	if photo == nil || len(photo.Data) == 0 {
		return toolJSONError(out, desktopcamera.CodeFailed, "camera returned empty photo")
	}
	mime := photo.MIME
	if mime == "" {
		mime = "image/jpeg"
	}
	name := photo.Name
	if name == "" {
		name = desktopcamera.Filename(time.Now())
	}
	stored, err := attachment.NewService(r.homeDir).StoreReader(call.SessionID, name, mime, bytes.NewReader(photo.Data))
	if err != nil {
		return toolJSONError(out, "attachment_store_failed", err.Error())
	}
	stored.Origin = attachment.OriginTool
	displayMarkdown := "![Camera photo](" + stored.URL + ")"
	out.Ok = true
	out.Attachments = []store.Attachment{stored}
	out.Content = jsonString(map[string]any{
		"ok":                      true,
		"mime":                    mime,
		"size":                    len(photo.Data),
		"attachmentKey":           stored.AttachmentKey,
		"url":                     stored.URL,
		"displayedInConversation": true,
		"displayMarkdown":         displayMarkdown,
		"displayHint":             "The photo is already displayed with this tool result. Reuse displayMarkdown exactly only when the user asks to show it again; do not append tokens or guess a filesystem path.",
		"exportTool":              AttachmentExport,
		"exportHint":              attachmentExportToolHint,
	})
	out.SummaryKind = SummaryReturnedFields
	out.SummaryCount = 10
	return out
}

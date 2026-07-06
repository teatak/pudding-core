package tool

import (
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/teatak/pudding-core/internal/attachment"
	"github.com/teatak/pudding-core/internal/store"
)

func (r *BuiltinRunner) attachmentReadImage(call Call) Result {
	out := Result{CallID: call.CallID, Name: call.Name}
	sessionID := strings.TrimSpace(call.SessionID)
	if sessionID == "" {
		return toolJSONError(out, "session_required", "session id is required to read image attachments")
	}
	var args struct {
		AttachmentKey string `json:"attachmentKey"`
		URL           string `json:"url"`
	}
	if err := decodeStructToolArgs(call.Args, &args); err != nil {
		return toolJSONError(out, "invalid_arguments", err.Error())
	}
	key := strings.TrimSpace(args.AttachmentKey)
	if key == "" {
		var wrongSession bool
		key, wrongSession = attachmentReadImageKeyFromURL(sessionID, args.URL)
		if wrongSession {
			return toolJSONError(out, "session_mismatch", "attachment URL belongs to another session")
		}
	}
	key = strings.TrimSpace(key)
	if key == "" {
		return toolJSONError(out, "attachment_required", "attachmentKey or url is required")
	}

	service := attachment.NewService(r.homeDir)
	path, ok, err := service.Path(sessionID, key)
	if err != nil {
		return toolJSONError(out, "attachment_lookup_failed", err.Error())
	}
	if !ok {
		return toolJSONError(out, "attachment_not_found", "attachment was not found in this session")
	}
	info, err := os.Stat(path)
	if err != nil {
		return toolJSONError(out, "attachment_stat_failed", err.Error())
	}
	if info.IsDir() {
		return toolJSONError(out, "not_file", "attachment path is a directory")
	}
	mime := attachment.MIMEFromExt(path)
	if mime == "" {
		mime = sniffFileMIME(path)
	}
	if !isReadableImageMIME(mime) {
		return toolJSONError(out, "not_image", "attachment is not a supported image")
	}

	key = canonicalAttachmentReadImageKey(sessionID, key)
	item := store.Attachment{
		ID:            attachmentReadImageID(key),
		Name:          filepath.Base(path),
		AttachmentKey: key,
		URL:           attachment.URL(sessionID, key),
		MIME:          mime,
		Size:          info.Size(),
		Origin:        attachment.OriginTool,
	}
	if !info.ModTime().IsZero() {
		item.CreatedAt = info.ModTime().UTC().Format(time.RFC3339)
	}

	out.Ok = true
	out.Attachments = []store.Attachment{item}
	out.ContextAttachments = []store.Attachment{item}
	out.Content = jsonString(map[string]any{
		"ok":            true,
		"kind":          "image_attachment_routed",
		"mime":          item.MIME,
		"size":          item.Size,
		"attachmentKey": item.AttachmentKey,
		"url":           item.URL,
		"hint":          "image bytes were routed to the model for inspection.",
	})
	out.SummaryKind = SummaryReturnedFields
	out.SummaryCount = 7
	return out
}

func attachmentReadImageKeyFromURL(sessionID, raw string) (string, bool) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return "", false
	}
	path := raw
	if parsed, err := url.Parse(raw); err == nil && parsed.Path != "" {
		path = parsed.Path
	}
	path = strings.Trim(strings.ReplaceAll(path, "\\", "/"), "/")
	if path == "" {
		return "", false
	}
	parts := strings.Split(path, "/")
	for i := 0; i+3 < len(parts); i++ {
		if parts[i] != "sessions" || parts[i+2] != "attachments" {
			continue
		}
		if parts[i+1] != sessionID {
			return "", true
		}
		return strings.Join(parts[i+3:], "/"), false
	}
	return path, false
}

func canonicalAttachmentReadImageKey(sessionID, key string) string {
	key = strings.Trim(strings.ReplaceAll(key, "\\", "/"), "/")
	key = filepath.ToSlash(filepath.Clean(key))
	if key == "." || key == ".." || strings.HasPrefix(key, "../") {
		return ""
	}
	if strings.HasPrefix(key, "blobs/") {
		return "sessions/" + sessionID + "/" + key
	}
	return key
}

func attachmentReadImageID(key string) string {
	name := filepath.Base(filepath.FromSlash(key))
	id := strings.TrimSuffix(name, filepath.Ext(name))
	if strings.TrimSpace(id) == "" {
		return "attachment"
	}
	return id
}

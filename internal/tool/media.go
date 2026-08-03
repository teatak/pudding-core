package tool

import (
	"errors"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/teatak/pudding-core/internal/attachment"
	"github.com/teatak/pudding-core/internal/store"
)

const (
	mediaSourceAttachment = "attachment"
	mediaSourceFile       = "file"
	mediaTypeImage        = "image"
	mediaTypeAudio        = "audio"
)

type mediaReadArgs struct {
	Source        string `json:"source"`
	AttachmentKey string `json:"attachmentKey"`
	URL           string `json:"url"`
	Scope         string `json:"scope"`
	Path          string `json:"path"`
}

func (r *BuiltinRunner) mediaRead(call Call) Result {
	out := Result{CallID: call.CallID, Name: call.Name}
	sessionID := strings.TrimSpace(call.SessionID)
	if sessionID == "" {
		return toolJSONError(out, "session_required", "session id is required to route media")
	}
	var args mediaReadArgs
	if err := decodeStructToolArgs(call.Args, &args); err != nil {
		return toolJSONError(out, "invalid_arguments", err.Error())
	}
	switch strings.TrimSpace(args.Source) {
	case mediaSourceAttachment:
		if strings.TrimSpace(args.Scope) != "" || strings.TrimSpace(args.Path) != "" {
			return toolJSONError(out, "invalid_arguments", "source=attachment does not accept scope or path")
		}
		return r.mediaReadAttachment(out, sessionID, args)
	case mediaSourceFile:
		if strings.TrimSpace(args.AttachmentKey) != "" || strings.TrimSpace(args.URL) != "" {
			return toolJSONError(out, "invalid_arguments", "source=file does not accept attachmentKey or url")
		}
		return r.mediaReadFile(out, call, args)
	default:
		return toolJSONError(out, "invalid_source", "source must be attachment or file")
	}
}

func (r *BuiltinRunner) mediaReadAttachment(out Result, sessionID string, args mediaReadArgs) Result {
	key := strings.TrimSpace(args.AttachmentKey)
	if key == "" {
		var wrongSession bool
		key, wrongSession = mediaAttachmentKeyFromURL(sessionID, args.URL)
		if wrongSession {
			return toolJSONError(out, "session_mismatch", "attachment URL belongs to another session")
		}
	}
	key = strings.TrimSpace(key)
	if key == "" {
		return toolJSONError(out, "attachment_required", "attachmentKey or url is required")
	}
	if filepath.IsAbs(key) {
		return toolJSONError(out, "invalid_attachment_key", "attachmentKey must be the attachment key, not a local filesystem path")
	}
	if owner := mediaAttachmentKeySession(key); owner != "" && owner != sessionID {
		return toolJSONError(out, "session_mismatch", "attachmentKey belongs to another session")
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
	if !info.Mode().IsRegular() {
		return toolJSONError(out, "not_file", "attachment path is not a regular file")
	}
	if info.Size() == 0 {
		return toolJSONError(out, "empty_media", "media file is empty")
	}
	if info.Size() > attachment.MaxUploadBytes {
		return mediaTooLargeResult(out, info.Size())
	}
	mimeType := attachment.MIMEFromExt(path)
	if mimeType == "" {
		mimeType = sniffFileMIME(path)
	}
	mediaType, ok := readableMediaKind(mimeType)
	if !ok {
		return unsupportedMediaResult(out, mimeType, mediaSourceAttachment)
	}

	key = canonicalMediaAttachmentKey(sessionID, key)
	if key == "" {
		return toolJSONError(out, "invalid_attachment_key", "attachmentKey is invalid")
	}
	item := store.Attachment{
		ID:            mediaAttachmentID(key),
		Name:          filepath.Base(path),
		AttachmentKey: key,
		URL:           attachment.URL(sessionID, key),
		MIME:          mimeType,
		Size:          info.Size(),
		Origin:        attachment.OriginTool,
	}
	if !info.ModTime().IsZero() {
		item.CreatedAt = info.ModTime().UTC().Format(time.RFC3339)
	}
	return routedMediaResult(out, item, mediaType, map[string]any{"source": mediaSourceAttachment})
}

func (r *BuiltinRunner) mediaReadFile(out Result, call Call, args mediaReadArgs) Result {
	if store.NormalizeAgentMode(call.Mode) != store.ModeCode {
		return toolJSONError(out, "capability_required", "source=file requires Code capability")
	}
	if strings.TrimSpace(args.Scope) == "" || strings.TrimSpace(args.Path) == "" {
		return toolJSONError(out, "file_required", "scope and path are required for source=file")
	}
	resolved, err := r.resolveFilePath(call, args.Scope, args.Path, false, false, false)
	if err != nil {
		return filePathError(out, args.Scope, err)
	}
	info, err := os.Stat(resolved.target)
	if err != nil {
		return toolJSONError(out, "media_stat_failed", err.Error())
	}
	if !info.Mode().IsRegular() {
		return toolJSONError(out, "not_file", "media path is not a regular file")
	}
	if info.Size() == 0 {
		return toolJSONError(out, "empty_media", "media file is empty")
	}
	if info.Size() > attachment.MaxUploadBytes {
		return mediaTooLargeResult(out, info.Size())
	}
	mimeType := attachment.MIMEFromExt(resolved.target)
	if mimeType == "" {
		mimeType = sniffFileMIME(resolved.target)
	}
	mediaType, ok := readableMediaKind(mimeType)
	if !ok {
		return unsupportedMediaResult(out, mimeType, mediaSourceFile)
	}
	stored, err := attachment.NewService(r.homeDir).StorePath(call.SessionID, resolved.target)
	if err != nil {
		if errors.Is(err, attachment.ErrTooLarge) {
			return mediaTooLargeResult(out, info.Size())
		}
		return toolJSONError(out, "attachment_store_failed", err.Error())
	}
	stored = attachment.WithSourcePath(stored, resolved.target)
	stored.Origin = attachment.OriginTool
	payload := resolved.payload(map[string]any{
		"source": mediaSourceFile,
		"scope":  args.Scope,
	})
	return routedMediaResult(out, stored, mediaType, payload)
}

func routedMediaResult(out Result, item store.Attachment, mediaType string, payload map[string]any) Result {
	payload["ok"] = true
	payload["kind"] = "media_routed"
	payload["mediaType"] = mediaType
	payload["mime"] = item.MIME
	payload["size"] = item.Size
	payload["attachmentKey"] = item.AttachmentKey
	payload["url"] = item.URL
	payload["hint"] = mediaType + " bytes are available only to models with matching input support; otherwise use metadata only and do not infer the media contents."
	out.Ok = true
	out.Attachments = []store.Attachment{item}
	out.ContextAttachments = []store.Attachment{item}
	out.Content = jsonString(payload)
	out.SummaryKind = SummaryReturnedFields
	out.SummaryCount = len(payload)
	return out
}

func readableMediaKind(rawMIME string) (string, bool) {
	mimeType := normalizedMediaMIME(rawMIME)
	switch mimeType {
	case "image/gif", "image/jpeg", "image/png", "image/webp":
		return mediaTypeImage, true
	}
	switch mimeType {
	case "audio/aac", "audio/flac", "audio/m4a", "audio/mp3", "audio/mp4", "audio/mpeg", "audio/ogg", "audio/opus", "audio/wav", "audio/wave", "audio/webm", "audio/x-m4a", "audio/x-wav":
		return mediaTypeAudio, true
	default:
		return "", false
	}
}

func isMediaMIME(rawMIME string) bool {
	mimeType := normalizedMediaMIME(rawMIME)
	return mimeType != "image/svg+xml" && (strings.HasPrefix(mimeType, "image/") || strings.HasPrefix(mimeType, "audio/"))
}

func normalizedMediaMIME(raw string) string {
	raw = strings.ToLower(strings.TrimSpace(raw))
	if main, _, ok := strings.Cut(raw, ";"); ok {
		return strings.TrimSpace(main)
	}
	return raw
}

func mediaRequiredForTextTool(out Result, mimeType string) Result {
	out.Ok = false
	out.Content = jsonString(map[string]any{
		"ok":              false,
		"reason":          "unsupported_media",
		"detail":          "file is media, not UTF-8 text; mime=" + normalizedMediaMIME(mimeType),
		"mime":            normalizedMediaMIME(mimeType),
		"recommendedTool": MediaRead,
	})
	out.SummaryKind = SummaryReturnedFields
	out.SummaryCount = 5
	return out
}

func unsupportedMediaResult(out Result, mimeType, source string) Result {
	mimeType = normalizedMediaMIME(mimeType)
	detail := "media type is not supported; mime=" + mimeType
	payload := map[string]any{
		"ok":                  false,
		"reason":              "unsupported_media",
		"detail":              detail,
		"mime":                mimeType,
		"supportedImageTypes": []string{"image/gif", "image/jpeg", "image/png", "image/webp"},
		"supportedAudioTypes": []string{"audio/aac", "audio/flac", "audio/mp4", "audio/mpeg", "audio/ogg", "audio/opus", "audio/wav", "audio/webm"},
	}
	if mimeType == "image/svg+xml" {
		if source == mediaSourceFile {
			payload["detail"] = "SVG visual input is not supported; read its UTF-8 source instead"
			payload["recommendedTool"] = FileRead
		} else {
			payload["detail"] = "SVG attachment visual input is not supported; no text-file path is available for builtin_file_read"
		}
	}
	out.Ok = false
	out.Content = jsonString(payload)
	out.SummaryKind = SummaryReturnedFields
	out.SummaryCount = len(payload)
	return out
}

func mediaTooLargeResult(out Result, size int64) Result {
	out.Ok = false
	out.Content = jsonString(map[string]any{
		"ok":     false,
		"reason": "media_too_large",
		"detail": "media exceeds the 20 MiB attachment limit",
		"size":   size,
		"limit":  attachment.MaxUploadBytes,
	})
	out.SummaryKind = SummaryReturnedFields
	out.SummaryCount = 5
	return out
}

func mediaAttachmentKeyFromURL(sessionID, raw string) (string, bool) {
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

func canonicalMediaAttachmentKey(sessionID, key string) string {
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

func mediaAttachmentKeySession(key string) string {
	key = strings.Trim(strings.ReplaceAll(key, "\\", "/"), "/")
	parts := strings.Split(key, "/")
	if len(parts) >= 4 && parts[0] == "sessions" && parts[2] == "blobs" {
		return parts[1]
	}
	return ""
}

func mediaAttachmentID(key string) string {
	name := filepath.Base(filepath.FromSlash(key))
	id := strings.TrimSuffix(name, filepath.Ext(name))
	if strings.TrimSpace(id) == "" {
		return "attachment"
	}
	return id
}

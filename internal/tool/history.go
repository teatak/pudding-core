package tool

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/teatak/pudding-core/internal/store"
)

const (
	historySearchDefaultLimit       = 10
	historySearchMaxLimit           = 30
	historySearchMaxBytes           = 20 * 1024
	historySearchPerMessageMaxBytes = 1024
	historyGetMessageMaxBytes       = 128 * 1024
	historyGetMessagePartMaxBytes   = 32 * 1024
)

func (r *BuiltinRunner) historySearch(ctx context.Context, call Call) Result {
	out := Result{CallID: call.CallID, Name: call.Name}
	var args struct {
		Query     string `json:"query"`
		SessionID string `json:"session_id"`
		Limit     int    `json:"limit"`
	}
	if len(call.Args) > 0 {
		if err := json.Unmarshal(call.Args, &args); err != nil {
			out.Ok = false
			out.Content = "invalid arguments: " + err.Error()
			return out
		}
	}
	query := strings.TrimSpace(args.Query)
	if query == "" {
		return jsonToolResult(out, false, map[string]any{
			"ok":        false,
			"reason":    "missing_query",
			"user_hint": "query cannot be empty.",
		})
	}
	if r.history == nil {
		return jsonToolResult(out, false, map[string]any{
			"ok":        false,
			"reason":    "history_unavailable",
			"user_hint": "history search is not enabled in this environment.",
		})
	}
	sessionID := strings.TrimSpace(args.SessionID)
	if sessionID == "" {
		sessionID = strings.TrimSpace(call.SessionID)
	}
	if sessionID == "" {
		return jsonToolResult(out, false, map[string]any{
			"ok":        false,
			"reason":    "missing_session_id",
			"user_hint": "history_search needs a session_id when there is no current session.",
		})
	}
	limit := args.Limit
	if limit <= 0 {
		limit = historySearchDefaultLimit
	}
	if limit > historySearchMaxLimit {
		limit = historySearchMaxLimit
	}
	hits, err := r.history.SearchMessages(ctx, store.MessageSearchInput{
		SessionID: sessionID,
		Query:     query,
		Limit:     limit,
	})
	if err != nil {
		reason := "search_failed"
		hint := ""
		if errors.Is(err, store.ErrHistorySearchUnavailable) {
			reason = "history_unavailable"
			hint = "history search requires a build with SQLite FTS5 enabled."
		}
		payload := map[string]any{
			"ok":     false,
			"reason": reason,
			"error":  err.Error(),
		}
		if hint != "" {
			payload["user_hint"] = hint
		}
		return jsonToolResult(out, false, payload)
	}
	messages, truncated := historySearchMessagesPayload(hits, historySearchMaxBytes)
	payload := map[string]any{
		"ok":         true,
		"query":      query,
		"session_id": sessionID,
		"hit_count":  len(hits),
		"messages":   messages,
	}
	if truncated {
		payload["truncated"] = true
		payload["truncate_hint"] = "The response was truncated by the byte limit; retry with a lower limit or more specific query."
	}
	return jsonToolResult(out, true, payload)
}

func (r *BuiltinRunner) historyGetMessage(ctx context.Context, call Call) Result {
	out := Result{CallID: call.CallID, Name: call.Name}
	var args struct {
		MessageID string `json:"message_id"`
		SessionID string `json:"session_id"`
	}
	if len(call.Args) > 0 {
		if err := json.Unmarshal(call.Args, &args); err != nil {
			out.Ok = false
			out.Content = "invalid arguments: " + err.Error()
			return out
		}
	}
	messageID := strings.TrimSpace(args.MessageID)
	if messageID == "" {
		return jsonToolResult(out, false, map[string]any{
			"ok":        false,
			"reason":    "missing_message_id",
			"user_hint": "message_id cannot be empty.",
		})
	}
	if r.historyMessages == nil {
		return jsonToolResult(out, false, map[string]any{
			"ok":        false,
			"reason":    "history_unavailable",
			"user_hint": "history message lookup is not enabled in this environment.",
		})
	}
	sessionID := strings.TrimSpace(args.SessionID)
	if sessionID == "" {
		sessionID = strings.TrimSpace(call.SessionID)
	}
	if sessionID == "" {
		return jsonToolResult(out, false, map[string]any{
			"ok":        false,
			"reason":    "missing_session_id",
			"user_hint": "history_get_message needs a session_id when there is no current session.",
		})
	}
	msg, err := r.historyMessages.GetMessage(ctx, sessionID, messageID)
	if err != nil {
		reason := "fetch_message_failed"
		if errors.Is(err, store.ErrNotFound) {
			reason = "message_not_found"
		}
		return jsonToolResult(out, false, map[string]any{
			"ok":         false,
			"reason":     reason,
			"session_id": sessionID,
			"message_id": messageID,
			"error":      err.Error(),
		})
	}
	if msg == nil {
		return jsonToolResult(out, false, map[string]any{
			"ok":         false,
			"reason":     "message_not_found",
			"session_id": sessionID,
			"message_id": messageID,
		})
	}
	payload, truncated := historyGetMessagePayload(msg, historyGetMessageMaxBytes)
	if truncated {
		payload["truncated"] = true
		payload["truncate_hint"] = "The response was truncated by the byte limit; use builtin_history_search for a narrower target."
	}
	return jsonToolResult(out, true, payload)
}

func historyGetMessagePayload(msg *store.Message, maxBytes int) (map[string]any, bool) {
	if msg == nil {
		return map[string]any{"ok": false, "reason": "message_not_found"}, false
	}
	text, textTruncated := truncateUTF8Bytes(msg.Text, maxBytes)
	parts, partsTruncated := historyPartsPayload(msg.Parts)
	payload := map[string]any{
		"ok":         true,
		"message_id": msg.ID,
		"ref":        "@message(" + msg.ID + ")",
		"session_id": msg.SessionID,
		"turn_id":    msg.TurnID,
		"role":       msg.Role,
		"kind":       msg.Kind,
		"text":       text,
		"created_at": msg.CreatedAt.UTC().Format(time.RFC3339),
	}
	if len(parts) > 0 {
		payload["parts"] = parts
	}
	if msg.ClientMessageID != "" {
		payload["client_message_id"] = msg.ClientMessageID
	}
	if msg.Interrupted {
		payload["interrupted"] = true
	}
	if len(msg.Metadata) > 0 {
		payload["metadata"] = msg.Metadata
	}
	if attachments := store.AttachmentsFromParts(msg.Parts); len(attachments) > 0 {
		payload["attachments"] = historyAttachmentPayload(attachments)
	}
	if folders := store.LocalFoldersFromParts(msg.Parts); len(folders) > 0 {
		payload["local_folders"] = historyLocalFolderPayload(folders)
	}
	return payload, textTruncated || partsTruncated
}

func historyPartsPayload(parts []store.ContentPart) ([]store.ContentPart, bool) {
	out := store.CloneContentParts(parts)
	truncated := false
	for i := range out {
		var textTruncated bool
		out[i].Text, textTruncated = truncateUTF8Bytes(out[i].Text, historyGetMessagePartMaxBytes)
		truncated = truncated || textTruncated
		out[i].Content, textTruncated = truncateUTF8Bytes(out[i].Content, historyGetMessagePartMaxBytes)
		truncated = truncated || textTruncated
		out[i].AudioTranscript, textTruncated = truncateUTF8Bytes(out[i].AudioTranscript, historyGetMessagePartMaxBytes)
		truncated = truncated || textTruncated
	}
	return out, truncated
}

func historySearchMessagesPayload(hits []*store.Message, maxBytes int) ([]map[string]any, bool) {
	used := 0
	out := make([]map[string]any, 0, len(hits))
	for _, msg := range hits {
		if msg == nil {
			continue
		}
		text, textTruncated := truncateUTF8Bytes(msg.Text, historySearchPerMessageMaxBytes)
		entry := map[string]any{
			"id":         msg.ID,
			"ref":        "@message(" + msg.ID + ")",
			"session_id": msg.SessionID,
			"role":       msg.Role,
			"text":       text,
			"ts":         msg.CreatedAt.UTC().Format(time.RFC3339),
		}
		if msg.Kind != "" {
			entry["kind"] = msg.Kind
		}
		if textTruncated {
			entry["truncated"] = true
		}
		if attachments := store.AttachmentsFromParts(msg.Parts); len(attachments) > 0 {
			entry["attachments"] = historyAttachmentPayload(attachments)
		}
		entryBytes := len(msg.ID) + len(msg.SessionID) + len(msg.Role) + len(text) + 128
		if used+entryBytes > maxBytes {
			return out, true
		}
		used += entryBytes
		out = append(out, entry)
	}
	return out, false
}

func historyAttachmentPayload(attachments []store.Attachment) []map[string]any {
	out := make([]map[string]any, 0, len(attachments))
	for _, attachment := range attachments {
		item := map[string]any{
			"id":   attachment.ID,
			"name": attachment.Name,
			"mime": attachment.MIME,
			"size": attachment.Size,
		}
		if attachment.AttachmentKey != "" {
			item["attachment_key"] = attachment.AttachmentKey
		}
		if attachment.URL != "" {
			item["url"] = attachment.URL
		}
		if attachment.SourcePath != "" {
			item["source_path"] = attachment.SourcePath
		}
		if attachment.CreatedAt != "" {
			item["created_at"] = attachment.CreatedAt
		}
		if attachment.AudioTranscript != "" {
			item["audio_transcript"] = attachment.AudioTranscript
		}
		out = append(out, item)
	}
	return out
}

func historyLocalFolderPayload(folders []store.LocalFolder) []map[string]any {
	out := make([]map[string]any, 0, len(folders))
	for _, folder := range folders {
		item := map[string]any{
			"id":   folder.ID,
			"name": folder.Name,
			"path": folder.Path,
		}
		if folder.Origin != "" {
			item["origin"] = folder.Origin
		}
		out = append(out, item)
	}
	return out
}

func truncateUTF8Bytes(s string, max int) (string, bool) {
	if max <= 0 || len(s) <= max {
		return s, false
	}
	cut := 0
	for idx := range s {
		if idx > max {
			break
		}
		cut = idx
	}
	if cut <= 0 {
		return "...(truncated)", true
	}
	return s[:cut] + "...(truncated)", true
}

func jsonToolResult(out Result, ok bool, payload map[string]any) Result {
	b, err := json.Marshal(payload)
	if err != nil {
		out.Ok = false
		out.Content = fmt.Sprintf("marshal result: %v", err)
		return out
	}
	out.Ok = ok
	out.Content = string(b)
	if messages, ok := payload["messages"].([]map[string]any); ok {
		out.SummaryKind = SummaryReturnedItems
		out.SummaryCount = len(messages)
	} else if ok {
		out.SummaryKind = SummaryReturnedFields
		out.SummaryCount = len(payload)
	}
	return out
}

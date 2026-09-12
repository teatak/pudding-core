package tool

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"unicode/utf8"

	"github.com/teatak/pudding-core/internal/store"
)

type resultReadOptions struct {
	Field  string  `json:"field"`
	Unit   string  `json:"unit"`
	Query  *string `json:"query"`
	Offset *int    `json:"offset"`
	Limit  *int    `json:"limit"`
}

// Read the saved tool result, not a re-execution of its command or a fresh file
// read. This also works within a running turn: results are committed before the
// next model request. No hidden provider state is exposed by this path.
func (r *BuiltinRunner) historyReadResult(ctx context.Context, out Result, sessionID string, ref ResultReference, options resultReadOptions) Result {
	if sessionID == "" || strings.TrimSpace(ref.TurnID) == "" || strings.TrimSpace(ref.CallID) == "" {
		return toolJSONError(out, "invalid_arguments", "result_ref requires turn_id, call_id and a session scope")
	}
	unit := options.Unit
	if unit == "" {
		unit = "chars"
	}
	offset, limit, maxLimit := 0, resultPageDefaultChars, resultPageMaxChars
	switch unit {
	case "chars":
	case "lines":
		limit, maxLimit = 40, 200
	case "items":
		limit, maxLimit = 20, 200
	default:
		return toolJSONError(out, "invalid_arguments", "unit must be chars, lines or items")
	}
	if options.Offset != nil {
		offset = *options.Offset
	}
	if options.Limit != nil {
		limit = *options.Limit
	}
	if offset < 0 || limit < 1 || limit > maxLimit {
		return toolJSONError(out, "invalid_arguments", "offset must be non-negative; limit is 1-8000 for chars or 1-200 for lines/items")
	}
	if options.Query != nil && (unit != "lines" || *options.Query == "" || utf8.RuneCountInString(*options.Query) > 500 || strings.ContainsAny(*options.Query, "\r\n")) {
		return toolJSONError(out, "invalid_arguments", "query requires unit=lines and a non-empty single-line literal of at most 500 characters; matching is case-sensitive")
	}
	if r.historyTurns == nil {
		return toolJSONError(out, "history_unavailable", "canonical tool result lookup is unavailable")
	}
	turn, err := r.historyTurns.GetConversationTurn(ctx, sessionID, ref.TurnID)
	if err != nil {
		if errors.Is(err, store.ErrNotFound) {
			return toolJSONError(out, "result_not_found", "tool result not found in this session")
		}
		return toolJSONError(out, "read_failed", err.Error())
	}
	if turn == nil || turn.SessionID != sessionID || turn.ID != ref.TurnID {
		return toolJSONError(out, "result_not_found", "tool result not found in this session")
	}
	var found *store.ContentPart
	for _, message := range turn.Messages {
		for i := range message.Parts {
			part := &message.Parts[i]
			if part.Type != store.ContentPartToolResult || part.CallID != ref.CallID {
				continue
			}
			if found != nil {
				return toolJSONError(out, "ambiguous_result", "multiple results have this call_id; cannot select a snapshot safely")
			}
			found = part
		}
	}
	if found == nil {
		return toolJSONError(out, "result_not_found", "tool result not found in this turn")
	}
	content := found.Content
	if options.Field != "" {
		var fields map[string]json.RawMessage
		if json.Unmarshal([]byte(content), &fields) != nil {
			return toolJSONError(out, "invalid_field", "original tool result is not a JSON object; omit field to read raw text")
		}
		value, exists := fields[options.Field]
		if !exists {
			return toolJSONError(out, "invalid_field", "field does not exist in the original tool result")
		}
		content = string(value)
		isString := len(value) > 0 && value[0] == '"'
		if unit == "lines" && !isString {
			return toolJSONError(out, "invalid_field", "unit=lines requires a string field; use unit=items for an array")
		}
		if isString && unit != "items" {
			_ = json.Unmarshal(value, &content)
		}
	}
	payload := map[string]any{
		"ok": true, "session_id": sessionID, "result_ref": ref,
		"tool": found.Name, "result_ok": found.Ok, "snapshot": true,
		"unit": unit, "offset": offset,
	}
	if options.Field != "" {
		payload["field"] = options.Field
	}
	if unit != "chars" {
		return resultRecordPage(ctx, out, payload, content, unit, options.Query, offset, limit)
	}
	runes := []rune(content)
	if offset > len(runes) {
		return toolJSONError(out, "invalid_offset", "offset exceeds the selected result's total_chars")
	}
	end := offset + min(limit, len(runes)-offset)
	payload["total_chars"], payload["content"] = len(runes), string(runes[offset:end])
	payload["has_more"] = end < len(runes)
	if end < len(runes) {
		payload["next_offset"] = end
	}
	return jsonToolResult(out, true, payload)
}

// Pages contain complete records, bounded by both count and encoded size.
// A filtered page keeps offsets in the original text, so the same positions can
// be used without query to inspect neighboring lines. No new result cache exists.
func resultRecordPage(ctx context.Context, out Result, payload map[string]any, content, unit string, query *string, offset, limit int) Result {
	var lines []string
	var items []json.RawMessage
	total := 0
	if unit == "items" {
		if !strings.HasPrefix(strings.TrimSpace(content), "[") || json.Unmarshal([]byte(content), &items) != nil {
			return toolJSONError(out, "invalid_field", "unit=items requires a JSON array, not a string containing JSON")
		}
		total = len(items)
	} else {
		lines = strings.SplitAfter(content, "\n")
		if lines[len(lines)-1] == "" {
			lines = lines[:len(lines)-1]
		}
		total = len(lines)
	}
	if offset > total {
		return toolJSONError(out, "invalid_offset", "offset exceeds the selected result's total_"+unit)
	}
	payload["total_"+unit] = total
	if query != nil {
		payload["query"] = *query
	}
	records := make([]json.RawMessage, 0, min(limit, total-offset))
	size, next := 2, total // JSON array brackets; offsets always refer to the original snapshot.
	for i := offset; i < total; i++ {
		if err := ctx.Err(); err != nil {
			return toolJSONError(out, "cancelled", err.Error())
		}
		if query != nil && !strings.Contains(lines[i], *query) {
			continue
		}
		if len(records) >= limit {
			next = i
			break
		}
		var record json.RawMessage
		if unit == "items" {
			// Compact whitespace outside strings without decoding numeric values.
			record = resultJSON(items[i])
		} else {
			record = resultJSON(map[string]any{"line": i + 1, "text": lines[i]})
		}
		recordSize := utf8.RuneCount(record)
		separator := 0
		if len(records) > 0 {
			separator = 1
		}
		if size+separator+recordSize > resultPageMaxChars {
			if len(records) == 0 {
				payload["ok"], payload["reason"] = false, "record_too_large"
				payload["offset"], payload["record_chars"] = i, recordSize
				payload["detail"] = "One complete record exceeds the 8000-character page budget. Use unit=chars for bounded fragments; its offset counts characters, not records."
				return jsonToolResult(out, false, payload)
			}
			next = i
			break
		}
		records = append(records, record)
		size += separator + recordSize
	}
	payload[unit], payload["has_more"] = records, next < total
	if next < total {
		payload["next_offset"] = next
	}
	return jsonToolResult(out, true, payload)
}

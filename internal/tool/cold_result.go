package tool

import (
	"encoding/json"
	"unicode/utf8"
)

// ColdResultContent removes excerpts only from large, successful tool results.
// Eligibility uses the L1 view. Exempt results return the original content so
// the builder applies L1 exactly once, leaving canonical data untouched.
func ColdResultContent(name string, ok bool, content, turnID, callID string) string {
	view := ModelResultContent(name, ok, content, turnID, callID, true)
	if !ok || turnID == "" || callID == "" || name == SkillRead || IsPluginLoad(name) || name == HistoryGetMessage || name == RequestUserInput || utf8.RuneCountInString(view) <= 1200 {
		return content
	}
	var fields map[string]json.RawMessage
	_ = json.Unmarshal([]byte(view), &fields)
	stub := map[string]any{
		"ok": ok, "cold_only": true, "tool": name, "call_id": callID,
		"result_ref": ResultReference{TurnID: turnID, CallID: callID},
		"view_chars": utf8.RuneCountInString(view),
	}
	copyResultMetadata(stub, fields)
	result := jsonString(stub)
	if utf8.RuneCountInString(result) >= utf8.RuneCountInString(view) {
		return content
	}
	return result
}

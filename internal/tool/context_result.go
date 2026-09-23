package tool

import (
	"encoding/json"
	"strconv"
	"strings"
	"unicode/utf8"
)

const (
	modelResultMaxChars    = 12000
	modelResultHeadChars   = 6000
	modelResultTailChars   = 2000
	modelSearchMaxMatches  = 20
	resultPageDefaultChars = 4000
	resultPageMaxChars     = 8000
)

// ResultReference points to an immutable canonical tool result, never a live
// filesystem path or a provider-side cache. The lookup is scoped by sessionID.
type ResultReference struct {
	TurnID string `json:"turn_id"`
	CallID string `json:"call_id"`
}

// ModelResultContent builds a deterministic model-only view. Canonical history
// and UI results remain unchanged. Only shorten results when a reader is in the
// request's tool set; skill instructions and explicit readback pages stay intact.
func ModelResultContent(name string, ok bool, content, turnID, callID string, canRead bool) string {
	if name == AppLoad || name == SkillRead {
		return SkillReferenceOnly(name, ok, content)
	}
	if name == HistoryGetMessage {
		return content
	}
	ref := ResultReference{TurnID: turnID, CallID: callID}
	canRead = canRead && ref.TurnID != "" && ref.CallID != ""
	var fields map[string]json.RawMessage
	if name == FileSlice || name == FileSearch {
		if json.Unmarshal([]byte(content), &fields) == nil && fields != nil {
			switch name {
			case FileSlice:
				var numbered string
				if value := fields["numberedContent"]; len(value) > 0 && value[0] == '"' && json.Unmarshal(value, &numbered) == nil {
					delete(fields, "content")
				}
			case FileSearch:
				var matches []map[string]json.RawMessage
				if json.Unmarshal(fields["matches"], &matches) == nil {
					for _, match := range matches {
						if numberSearchExcerpt(match) {
							continue
						}
						var text, excerpt string
						if json.Unmarshal(match["text"], &text) == nil && json.Unmarshal(match["excerpt"], &excerpt) == nil && text == excerpt {
							delete(match, "excerpt")
						}
					}
					if canRead && len(matches) > modelSearchMaxMatches {
						matches = matches[:modelSearchMaxMatches]
						fields["result_ref"] = resultJSON(ref)
						fields["preview_only"] = resultJSON(true)
						fields["shown_matches"] = resultJSON(len(matches))
					}
					fields["matches"] = resultJSON(matches)
				}
			}
			content = jsonString(fields)
		}
	}
	if !canRead || utf8.RuneCountInString(content) <= modelResultMaxChars {
		return content
	}
	// Keep outcome metadata separate from excerpts so an error near the middle
	// cannot turn into apparent success. Excerpts are explicitly not full JSON or
	// complete source lines; read the original result before relying on omissions.
	preview := map[string]any{
		"ok": ok, "result_ref": ref, "preview_only": true,
		"view_chars": utf8.RuneCountInString(content),
	}
	if fields == nil {
		_ = json.Unmarshal([]byte(content), &fields)
	}
	for _, key := range []string{
		"scope", "path", "root", "projectRoot", "relativePath", "cwd", "start", "end", "lines", "origin", "order",
		"reason", "error", "detail", "hint", "errorKind", "field", "expected", "offset", "receivedBytes", "metric", "actual", "limit", "unit", "count", "allowedScopes", "projectRoots",
		"exitCode", "timedOut", "cancelled", "execution", "sandboxDenied", "truncated", "stdoutTruncated", "stderrTruncated", "verificationKind", "verificationStatus", "diagnosticCount", "matchCount", "resultsCapped", "shown_matches",
	} {
		if value, exists := fields[key]; exists && len(value) <= 1024 {
			preview[key] = value
		}
	}
	var recovery map[string]json.RawMessage
	if json.Unmarshal(fields["recovery"], &recovery) == nil && recovery != nil {
		// Keep the exact candidate coordinates even when diagnostic source lines
		// require a preview. The canonical result remains the source for readback.
		coordinates := make(map[string]json.RawMessage)
		for _, key := range []string{"path", "hunk", "startLine", "fileLineCount", "exactMatchCount", "candidateStartLines", "candidatesTruncated"} {
			if value, exists := recovery[key]; exists && len(value) <= 1024 {
				coordinates[key] = value
			}
		}
		if len(coordinates) > 0 {
			preview["recovery"] = coordinates
		}
	}
	runes := []rune(content)
	preview["head"] = string(runes[:modelResultHeadChars])
	preview["tail"] = string(runes[len(runes)-modelResultTailChars:])
	return jsonString(preview)
}

func numberSearchExcerpt(match map[string]json.RawMessage) bool {
	var start, end, line int
	var excerpt string
	if json.Unmarshal(match["lineStart"], &start) != nil || json.Unmarshal(match["lineEnd"], &end) != nil || json.Unmarshal(match["line"], &line) != nil || start < 1 || end < start || line < start || line > end {
		return false
	}
	value := match["excerpt"]
	if len(value) == 0 || value[0] != '"' || json.Unmarshal(value, &excerpt) != nil {
		return false
	}
	lines := strings.Split(excerpt, "\n")
	if len(lines) != end-start+1 {
		return false
	}
	var numbered strings.Builder
	for index, text := range lines {
		if index > 0 {
			numbered.WriteByte('\n')
		}
		numbered.WriteString(strconv.Itoa(start + index))
		numbered.WriteString(": ")
		numbered.WriteString(text)
	}
	match["numberedExcerpt"] = resultJSON(numbered.String())
	delete(match, "excerpt")
	// Search text is a shorter preview of the matching excerpt line. Remove it
	// only when the numbered body includes that evidence, including blank lines.
	var text string
	if json.Unmarshal(match["text"], &text) == nil && strings.HasPrefix(lines[line-start], text) {
		delete(match, "text")
	}
	return true
}

func resultJSON(value any) json.RawMessage {
	data, _ := json.Marshal(value)
	return data
}

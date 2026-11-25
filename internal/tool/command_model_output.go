package tool

import (
	"encoding/json"
	"strings"
	"unicode/utf8"

	"github.com/teatak/pudding-core/internal/modeloutput"
)

// Bound command logs as whole streams. Per-write blocks can exhaust the model
// budget with headers alone, even when their combined text is small.
func modelCommandResultPreview(name, content string, ok bool, ref ResultReference, expand bool) (string, bool) {
	var fields map[string]json.RawMessage
	if json.Unmarshal([]byte(content), &fields) != nil {
		return "", false
	}
	streams := make(map[string]string)
	var chunks []backgroundProcessOutputChunk
	hasChunks := json.Unmarshal(fields["output"], &chunks) == nil && chunks != nil
	// A later log page (including an empty end page) must not be replaced by
	// the full foreground captures. Keep those captures in the canonical result
	// for direct command callers; only command_session's model view selects a page.
	paged := name == CommandSession && hasChunks && (len(chunks) == 0 || chunks[0].Offset > 0)
	var stdout, stderr string
	if !paged && json.Unmarshal(fields["stdout"], &stdout) == nil && json.Unmarshal(fields["stderr"], &stderr) == nil {
		// Completed foreground commands already carry both captured streams.
		streams["stdout"], streams["stderr"] = stdout, stderr
	} else {
		if !hasChunks {
			return "", false
		}
		var output strings.Builder
		stream := ""
		for _, chunk := range chunks {
			if chunk.Stream != stream {
				output.WriteString("\n[" + chunk.Stream + "]\n")
				stream = chunk.Stream
			}
			output.WriteString(chunk.Content)
		}
		streams["output"] = output.String()
	}
	preview := map[string]any{"ok": ok, "result_ref": ref, "preview_only": true, "view_chars": utf8.RuneCountInString(content)}
	copyResultMetadata(preview, fields)
	if utf8.RuneCountInString(jsonString(preview)) > modelResultMaxChars-1200 {
		return presentationStub(content, ok, ref.TurnID, ref.CallID, "Text omitted in full; read the original result."), true
	}
	// Reserve space for excerpt metadata and text-block framing.
	budget := min(modelResultHeadChars+modelResultTailChars, max(0, modelResultMaxChars-utf8.RuneCountInString(jsonString(preview))-1200)) / len(streams)
	spec := &modeloutput.Spec{Version: 1}
	for _, key := range []string{"stdout", "stderr", "output"} {
		value, exists := streams[key]
		if !exists {
			continue
		}
		runes := []rune(value)
		head := modelCommandExcerptChars(runes, budget*3/4, false)
		tail := modelCommandExcerptChars(runes[head:], budget/4, true)
		preview[key] = map[string]any{"head": string(runes[:head]), "tail": string(runes[len(runes)-tail:]), "chars": len(runes), "omitted_chars": len(runes) - head - tail}
		for _, part := range []string{"head", "tail"} {
			spec.TextFields = append(spec.TextFields, modeloutput.TextField{Path: "/" + key + "/" + part, Title: key + " " + part})
		}
	}
	raw := jsonString(preview)
	if !expand {
		return raw, true
	}
	view, err := modeloutput.Bind(raw, spec)
	if err != nil {
		return raw, true
	}
	return view.Render(), true
}

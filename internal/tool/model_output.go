package tool

import (
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"unicode/utf8"

	"github.com/teatak/pudding-core/internal/modeloutput"
)

type OutputPresentation struct {
	MCPResult json.RawMessage
	Enabled   bool
	Spec      *modeloutput.Spec
}

func builtinModelOutput(name string) *modeloutput.Spec {
	field := func(path, title string) modeloutput.TextField {
		return modeloutput.TextField{Path: path, Title: title, Optional: true}
	}
	array := func(each, path, title string) modeloutput.TextField {
		f := field(path, title)
		f.Each = &each
		return f
	}
	var fields []modeloutput.TextField
	switch name {
	case CommandRun, CommandSession:
		fields = []modeloutput.TextField{field("/stdout", "Standard output"), field("/stderr", "Standard error")}
	case FileSlice:
		fields = []modeloutput.TextField{field("/numberedContent", "Source lines (line prefixes are not source text)")}
	case FileSearch:
		fields = []modeloutput.TextField{array("/matches", "/excerpt", "Search excerpt (line prefixes are not source text)"), array("/matches", "/text", "Matched line")}
	case HistoryGetMessage:
		fields = []modeloutput.TextField{field("/content", "Result page"), array("/lines", "/text", "Result line"), field("/text", "Message text")}
	default:
		return nil
	}
	return &modeloutput.Spec{Version: 1, TextFields: fields}
}

func ModelResultContent(name string, ok bool, content, turnID, callID string, canRead bool, presentation ...OutputPresentation) string {
	if len(presentation) > 0 && presentation[0].Enabled && presentation[0].Spec == nil && len(presentation[0].MCPResult) > 0 {
		content = mcpModelText(presentation[0].MCPResult, content)
	}
	if len(presentation) == 0 || !presentation[0].Enabled || presentation[0].Spec == nil {
		return modelResultJSON(name, ok, content, turnID, callID, canRead)
	}
	v, err := modeloutput.Bind(content, presentation[0].Spec)
	if err != nil {
		// Formatting cannot change the action's success/failure or cause a retry.
		return presentationFailure(name, ok, content, turnID, callID, canRead, err)
	}
	if len(v.Blocks) == 0 {
		return modelResultJSON(name, ok, content, turnID, callID, canRead)
	}
	normalized := modelResultJSON(name, ok, content, turnID, callID, false)
	aliases := map[string]string{}
	if name == FileSearch {
		root, _ := modeloutput.Decode(normalized)
		for _, b := range v.Blocks {
			if strings.HasSuffix(b.Path, "/excerpt") {
				path := strings.TrimSuffix(b.Path, "/excerpt") + "/numberedExcerpt"
				if _, exists := modeloutput.Get(root, path); exists {
					aliases[b.Path] = path
				}
			}
		}
	}
	if err = v.Rebase(normalized, aliases); err != nil {
		return presentationFailure(name, ok, content, turnID, callID, canRead, err)
	}
	// Explicit readback pages are already bounded; never truncate them again.
	canRead = canRead && turnID != "" && callID != "" && name != HistoryGetMessage
	if !canRead {
		return v.Render()
	}
	if root, ok := v.Root.(map[string]any); ok {
		if matches, ok := root["matches"].([]any); name == FileSearch && ok && len(matches) > modelSearchMaxMatches {
			root["matches"] = matches[:modelSearchMaxMatches]
			root["preview_only"] = true
			root["shown_matches"] = modelSearchMaxMatches
			root["result_ref"] = ResultReference{TurnID: turnID, CallID: callID}
			raw, _ := json.Marshal(root)
			_ = v.Rebase(string(raw), aliases)
		}
	}
	if utf8.RuneCountInString(v.Render()) <= modelResultMaxChars {
		return v.Render()
	}
	if _, ok := v.Root.(map[string]any); !ok {
		v.Root = map[string]any{"value": v.Root}
		for i := range v.Blocks {
			v.Blocks[i].ViewPath = "/value" + v.Blocks[i].ViewPath
		}
	}
	if root, ok := v.Root.(map[string]any); ok {
		root["result_ref"] = ResultReference{TurnID: turnID, CallID: callID}
		root["preview_only"] = true
	}
	if v.Limit(modelResultMaxChars) {
		return v.Render()
	}
	// Large undeclared metadata must not force a partial "whole" block. Retain
	// the same status/coordinate metadata and readback reference as old previews.
	return presentationStub(content, ok, turnID, callID, "Text omitted in full; read the original result.")
}

func presentationFailure(name string, ok bool, content, turnID, callID string, canRead bool, err error) string {
	detail := []rune(err.Error())
	if len(detail) > 200 {
		detail = append(detail[:200], '…')
	}
	diagnostic := "Presentation unavailable: " + fmt.Sprintf("%q", string(detail))
	out := modelResultJSON(name, ok, content, turnID, callID, canRead) + "\n[" + diagnostic + "]"
	if canRead && turnID != "" && callID != "" && name != HistoryGetMessage && utf8.RuneCountInString(out) > modelResultMaxChars {
		return presentationStub(content, ok, turnID, callID, diagnostic)
	}
	return out
}

func presentationStub(content string, ok bool, turnID, callID, reason string) string {
	stub := map[string]any{"ok": ok, "preview_only": true, "result_ref": ResultReference{TurnID: turnID, CallID: callID}, "presentation": reason}
	var fields map[string]json.RawMessage
	_ = json.Unmarshal([]byte(content), &fields)
	metadata := map[string]any{}
	copyResultMetadata(metadata, fields)
	for _, key := range []string{"stateVersion", "status"} {
		if value, exists := fields[key]; exists && len(value) <= 1024 {
			metadata[key] = value
		}
	}
	// Keep failure and concurrency facts before optional descriptive metadata.
	priority := []string{"exitCode", "signal", "timedOut", "cancelled", "stateVersion", "status", "errorKind", "error", "reason"}
	keys := make([]string, 0, len(metadata))
	for key := range metadata {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	for _, key := range append(priority, keys...) {
		value, exists := metadata[key]
		if !exists {
			continue
		}
		if _, added := stub[key]; added {
			continue
		}
		stub[key] = value
		if utf8.RuneCountInString(jsonString(stub)) > modelResultMaxChars {
			delete(stub, key)
		}
	}
	return jsonString(stub)
}

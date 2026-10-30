package tool

import (
	"encoding/json"
	"reflect"
	"strings"

	"github.com/teatak/pudding-core/internal/modeloutput"
)

// The namespaced envelope is produced by Pudding's widget bridge. Other MCP
// content is not guessed to be a presentation, even when structured data exists.
func applyMCPPresentation(out *Result, raw json.RawMessage) {
	var envelope struct {
		Content []pluginMCPContent `json:"content"`
		Meta    struct {
			Pudding struct {
				Spec      *modeloutput.Spec `json:"modelOutput"`
				Canonical json.RawMessage   `json:"canonical"`
			} `json:"pudding"`
		} `json:"_meta"`
	}
	if json.Unmarshal(raw, &envelope) != nil {
		return
	}
	meta := envelope.Meta.Pudding
	if meta.Spec == nil || !json.Valid(meta.Canonical) || len(envelope.Content) != 1 || envelope.Content[0].Type != "text" {
		return
	}
	out.Content = string(meta.Canonical)
	out.ModelOutput = meta.Spec
}

func mcpModelText(raw json.RawMessage, fallback string) string {
	var envelope struct {
		Content    []pluginMCPContent `json:"content"`
		Structured json.RawMessage    `json:"structuredContent"`
	}
	if json.Unmarshal(raw, &envelope) != nil || len(envelope.Structured) == 0 || string(envelope.Structured) == "null" {
		return fallback
	}
	structured, err := modeloutput.Decode(string(envelope.Structured))
	if err != nil {
		return fallback
	}
	parts := []string{}
	for _, block := range envelope.Content {
		if block.Type != "text" || block.Text == "" {
			continue
		}
		value, err := modeloutput.Decode(block.Text)
		if err == nil && reflect.DeepEqual(value, structured) {
			continue
		}
		parts = append(parts, block.Text)
	}
	return strings.Join(append(parts, string(envelope.Structured)), "\n")
}

package tool

import (
	"encoding/json"
	"strings"
	"unicode/utf8"

	"github.com/teatak/pudding-core/internal/provider"
	"github.com/teatak/pudding-core/internal/store"
)

func NextPromptDefinition() provider.ToolDef {
	return provider.ToolDef{
		Name:        SetNextPrompt,
		Description: "Optionally suggest one useful next user message for the input placeholder. Only after completing the current task: output the full final answer as normal text, then call this tool in the SAME response, with no other tools. Write a concise, ready-to-send user instruction in the user's language, not advice addressed to the user. This ends the turn without another model request. Omit when no useful next step exists; never call it instead of answering or while work remains. The suggestion is not authorization to perform that next task.",
		InputSchema: json.RawMessage(`{"type":"object","properties":{"text":{"type":"string","minLength":1,"maxLength":200,"description":"One short next user instruction, as plain text."}},"required":["text"],"additionalProperties":false}`),
		Capability:  store.ModeChat,
	}
}

func nextPrompt(call Call) Result {
	out := Result{CallID: call.CallID, Name: call.Name}
	var args struct {
		Text string `json:"text"`
	}
	if err := decodeStructToolArgs(call.Args, &args); err != nil {
		return toolJSONError(out, "invalid_arguments", err.Error())
	}
	text := strings.Join(strings.Fields(args.Text), " ")
	if text == "" || utf8.RuneCountInString(text) > 200 {
		return toolJSONError(out, "invalid_text", "text must contain 1 to 200 characters")
	}
	out.Ok = true
	out.Content = jsonString(map[string]any{"text": text})
	return out
}

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
		Description: "Optionally suggest one useful next user message for the input placeholder. Only after completing the current task: output the full final answer as normal text, then call this tool in the SAME response, optionally together with builtin_set_turn_topic and no task tools. Write a concise, ready-to-send user instruction in the user's language, not advice addressed to the user. The complete answer text and this UI update are delivered together; the answer is not truncated and no additional model response is needed. The call and result are saved locally for the UI but omitted from later user turns' model context and history summaries; do not wait for confirmation or repeat the call to recover missing history. Tab only fills an unsent draft. It never sends a message or authorizes execution; act on the suggestion only after the user actually sends it. If the user explicitly asks to set the next input suggestion, call this tool; otherwise omit when no useful next step exists; never call it instead of answering or while work remains.",
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

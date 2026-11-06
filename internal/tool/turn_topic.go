package tool

import (
	"encoding/json"
	"strings"
	"unicode/utf8"

	"github.com/teatak/pudding-core/internal/provider"
	"github.com/teatak/pudding-core/internal/store"
)

func TurnTopicDefinition() provider.ToolDef {
	return provider.ToolDef{
		Name:        SetTurnTopic,
		Description: "Mark the CURRENT user turn as the beginning of a new conversation topic. Set a short, specific title in the user's language when no topic exists or the user changes the task/topic. Later turns automatically inherit this topic until another marker; omit this tool for continuations, corrections, confirmations, or short follow-ups such as 'continue'. Returning to an older topic starts a new chronological segment. Do not proactively mark every turn. If the user explicitly asks to set the current turn's topic, call this tool even for a continuation. This overrides only the default marking frequency; do not backdate a marker or rewrite earlier topics. Only after completing the current task: output the full final answer as normal text, then call this tool in the SAME response, optionally together with builtin_set_next_prompt and no task tools. The complete answer text and these UI updates are delivered together; the answer is not truncated and no additional model response is needed. Their records are omitted from model history; the current topic is supplied separately as metadata. Do not repeat a marker to recover missing history.",
		InputSchema: json.RawMessage(`{"type":"object","properties":{"title":{"type":"string","minLength":1,"maxLength":80,"description":"A short topic title in the user's language, ideally 2-12 Chinese characters or 2-6 words."}},"required":["title"],"additionalProperties":false}`),
		Capability:  store.ModeChat,
	}
}

func turnTopic(call Call) Result {
	out := Result{CallID: call.CallID, Name: call.Name}
	var args struct {
		Title string `json:"title"`
	}
	if err := decodeStructToolArgs(call.Args, &args); err != nil {
		return toolJSONError(out, "invalid_arguments", err.Error())
	}
	title := strings.Join(strings.Fields(args.Title), " ")
	if title == "" || utf8.RuneCountInString(title) > 80 {
		return toolJSONError(out, "invalid_title", "title must contain 1 to 80 characters")
	}
	out.Ok = true
	out.Content = jsonString(map[string]any{"title": title})
	return out
}

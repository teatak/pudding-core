package contextbuilder

import (
	"encoding/json"
	"fmt"

	"github.com/teatak/pudding-core/internal/provider"
	"github.com/teatak/pudding-core/internal/tool"
)

const (
	ToolImageLimit = 8
	ToolImageKeep  = 3
)

// LimitToolImages projects a bounded visual working set without changing saved
// history. Browser captures and media reads marked observation share the budget;
// references and user uploads do not. Omitted parts keep their
// identity, making projection idempotent across builder + current-turn replay.
// Remove six at a time: 8 -> 3 on the next image, then grow back to 8. Stable
// batch boundaries avoid changing the cached prefix on every screenshot.
func LimitToolImages(messages []provider.Message) []provider.Message {
	type position struct{ message, part int }
	var observations []position
	readIsObservation := make(map[string]bool)
	for i, message := range messages {
		for j, part := range message.Parts {
			if part.Type == provider.PartToolUse && part.Name == tool.MediaRead {
				// Purpose is a canonical tool argument: text cooling cannot erase
				// it. Legacy calls without purpose are references, not disposable
				// screenshots. Resolve in order even if a later turn reuses an ID.
				var args struct {
					Purpose string `json:"purpose"`
				}
				_ = json.Unmarshal(part.Args, &args)
				readIsObservation[part.CallID] = args.Purpose == tool.MediaPurposeObservation
			}
			if part.ImageKey != "" && (part.Name == tool.BrowserScreenshot || (part.Name == tool.MediaRead && readIsObservation[part.CallID])) {
				observations = append(observations, position{i, j})
			}
		}
	}
	if len(observations) <= ToolImageLimit {
		return messages
	}
	batch := ToolImageLimit - ToolImageKeep + 1
	omit := (len(observations) - ToolImageKeep) / batch * batch
	out := append([]provider.Message(nil), messages...)
	next := 0
	prefixChanged := false
	for i, message := range out {
		parts := append([]provider.Part(nil), message.Parts...)
		for j, part := range parts {
			if next >= omit || observations[next] != (position{i, j}) {
				continue
			}
			next++
			prefixChanged = true
			args, _ := json.Marshal(map[string]string{"source": "attachment", "attachmentKey": part.ImageKey, "purpose": tool.MediaPurposeObservation})
			parts[j] = provider.Part{
				Type: provider.PartText, CallID: part.CallID, Name: part.Name, ImageKey: part.ImageKey,
				Text: fmt.Sprintf("[Earlier screenshot omitted from model context; original preserved. To inspect it again, call %s with %s. Use purpose=reference instead if this image is needed for ongoing comparison. This is a historical image, not the current screen.]", tool.MediaRead, args),
			}
		}
		out[i].Parts = parts
		if len(parts) > 0 {
			out[i].Text = textFromProviderParts(parts)
		}
		if prefixChanged {
			// Anthropic thinking may be bound to the unchanged input prefix.
			// Drop only those blocks after an omission, preserving tool calls,
			// text and the other providers' required continuation state.
			out[i].Continuations = withoutBoundThinking(message.Continuations)
		}
	}
	return out
}

func withoutBoundThinking(states []provider.Continuation) []provider.Continuation {
	out := append([]provider.Continuation(nil), states...)
	for i, state := range out {
		if state.Kind != provider.ContinuationAnthropic {
			continue
		}
		var blocks []json.RawMessage
		if json.Unmarshal(state.Data, &blocks) != nil {
			continue // The adapter handles invalid native state via canonical parts.
		}
		kept := make([]json.RawMessage, 0, len(blocks))
		for _, block := range blocks {
			var header struct {
				Type string `json:"type"`
			}
			if json.Unmarshal(block, &header) != nil || (header.Type != "thinking" && header.Type != "redacted_thinking") {
				kept = append(kept, block)
			}
		}
		if len(kept) != len(blocks) {
			out[i].Data, _ = json.Marshal(kept)
		}
	}
	return out
}

package contextbuilder

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/teatak/pudding-core/internal/provider"
	"github.com/teatak/pudding-core/internal/store"
	"github.com/teatak/pudding-core/internal/tool"
)

// These are engineering budgets, not provider billing formulas or API limits.
// A cleanup leaves half the budget free instead of sliding on every capture.
type ToolImageBudget struct{ MaxTokens, MaxBytes int }

func DefaultToolImageBudget(window int) ToolImageBudget {
	tokens := 32768
	if window > 0 {
		tokens = min(tokens, max(1, window/10))
	}
	return ToolImageBudget{MaxTokens: tokens, MaxBytes: 16 << 20}
}

type imagePosition struct{ message, part int }

func observationPositions(messages []provider.Message) []imagePosition {
	var positions []imagePosition
	readIsObservation := make(map[string]bool)
	for i, message := range messages {
		for j, part := range message.Parts {
			if part.Type == provider.PartToolUse && part.Name == tool.MediaRead {
				var args struct {
					Purpose string `json:"purpose"`
				}
				_ = json.Unmarshal(part.Args, &args)
				readIsObservation[part.CallID] = args.Purpose == tool.MediaPurposeObservation
			}
			if part.ImageKey != "" && (part.Name == tool.BrowserScreenshot || (part.Name == tool.MediaRead && readIsObservation[part.CallID])) {
				positions = append(positions, imagePosition{i, j})
			}
		}
	}
	return positions
}

func ToolImageID(p provider.Part) string {
	id, _ := json.Marshal([3]string{p.ImageTurnID, p.CallID, p.ImageKey})
	return string(id)
}

// ToolImageCandidate only plans a new boundary. Callers commit it after the
// complete projected request passes the normal context checks.
// coalesce cleans to three recent observations at an existing prefix rewrite.
func ToolImageCandidate(messages []provider.Message, budget ToolImageBudget, coalesce bool) (string, error) {
	var active []provider.Part
	tokens, size := 0, 0
	for _, pos := range observationPositions(messages) {
		p := messages[pos.message].Parts[pos.part]
		if p.Type != provider.PartImage {
			continue
		}
		active = append(active, p)
		tokens += provider.EstimateImageTokens(p.Width, p.Height)
		size += (len(p.Data) + 2) / 3 * 4
	}
	over := tokens > budget.MaxTokens || size > budget.MaxBytes
	if !over && (!coalesce || len(active) <= 3) {
		return "", nil
	}
	omit := 0
	for omit < len(active)-1 {
		targetReached := !over || (tokens <= budget.MaxTokens/2 && size <= budget.MaxBytes/2)
		if targetReached && (!coalesce || len(active)-omit <= 3) {
			break
		}
		p := active[omit]
		tokens -= provider.EstimateImageTokens(p.Width, p.Height)
		size -= (len(p.Data) + 2) / 3 * 4
		omit++
	}
	if tokens > budget.MaxTokens || size > budget.MaxBytes {
		return "", fmt.Errorf("latest screenshot exceeds image budget: %d estimated tokens, %d encoded bytes", tokens, size)
	}
	if omit == 0 {
		return "", nil
	}
	return ToolImageID(active[omit-1]), nil
}

func imageTargets(messages []provider.Message, through string) map[string]bool {
	targets := map[string]bool{}
	if through == "" {
		return targets
	}
	for _, pos := range observationPositions(messages) {
		id := ToolImageID(messages[pos.message].Parts[pos.part])
		targets[id] = true
		if id == through {
			return targets
		}
	}
	return nil // Boundary belongs to a different effective history.
}

func ApplyToolImageWatermark(messages []provider.Message, through string) []provider.Message {
	return omitToolImages(messages, imageTargets(messages, through))
}

func ImageEpoch(messages []*store.Message) string {
	epoch := ""
	for _, m := range EffectiveMessages(messages) {
		if m.Role == store.RoleSummary {
			epoch = m.ID
		}
	}
	return epoch
}

// Resolve the boundary before mode/tool filtering, so a temporarily unavailable
// tool cannot revive older images when its boundary image is absent on the wire.
func applySavedToolImages(messages []provider.Message, canonical []*store.Message, state store.ContextCooling) []provider.Message {
	if state.ImageThroughID == "" || ImageEpoch(canonical) != state.SummaryMessageID {
		return messages
	}
	var history []provider.Message
	var callID, name, turnID string
	for _, m := range canonical {
		if m.TurnID != turnID {
			callID, name, turnID = "", "", m.TurnID
		}
		var parts []provider.Part
		for _, p := range m.Parts {
			switch p.Type {
			case store.ContentPartToolUse:
				parts = append(parts, provider.Part{Type: provider.PartToolUse, Name: tool.CurrentToolName(p.Name), CallID: p.CallID, Args: p.Args})
			case store.ContentPartToolResult:
				callID, name = p.CallID, tool.CurrentToolName(p.Name)
			case store.ContentPartAttachment:
				if p.Origin == "tool" && strings.HasPrefix(p.MIME, "image/") {
					parts = append(parts, provider.Part{Type: provider.PartImage, Name: name, CallID: callID, ImageKey: p.AttachmentKey, ImageTurnID: m.TurnID})
				}
			}
		}
		history = append(history, provider.Message{Parts: parts})
	}
	return omitToolImages(messages, imageTargets(history, state.ImageThroughID))
}

func omitToolImages(messages []provider.Message, targets map[string]bool) []provider.Message {
	if len(targets) == 0 {
		return messages
	}
	out := append([]provider.Message(nil), messages...)
	prefixChanged := false
	for i, message := range out {
		parts := append([]provider.Part(nil), message.Parts...)
		for j, part := range parts {
			if part.ImageKey == "" || !targets[ToolImageID(part)] {
				continue
			}
			prefixChanged = true
			args, _ := json.Marshal(map[string]string{"source": "attachment", "attachmentKey": part.ImageKey, "purpose": tool.MediaPurposeObservation})
			parts[j] = provider.Part{Type: provider.PartText, CallID: part.CallID, Name: part.Name, ImageKey: part.ImageKey, ImageTurnID: part.ImageTurnID,
				Text: fmt.Sprintf("[Earlier screenshot omitted from model context; original preserved. To inspect it again, call %s with %s. Use purpose=reference instead if this image is needed for ongoing comparison. This is a historical image, not the current screen.]", tool.MediaRead, args)}
		}
		out[i].Parts = parts
		if len(parts) > 0 {
			out[i].Text = textFromProviderParts(parts)
		}
		if prefixChanged {
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

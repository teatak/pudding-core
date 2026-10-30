package contextbuilder

import (
	"context"

	"github.com/teatak/pudding-core/internal/config"
	"github.com/teatak/pudding-core/internal/store"
	"github.com/teatak/pudding-core/internal/tool"
)

type CoolingSettings interface {
	Settings(context.Context) (map[string]string, error)
}

func WithCoolingSettings(settings CoolingSettings) Option {
	return func(b *Builder) { b.coolingSettings = settings }
}

func (b *Builder) contextCooling(ctx context.Context, sessionID string) (store.ContextCooling, error) {
	if b.coolingSettings != nil {
		settings, err := b.coolingSettings.Settings(ctx)
		if err != nil {
			return store.ContextCooling{}, err
		}
		if settings[config.SettingToolResultCoolingEnabled] == "false" {
			return store.ContextCooling{}, nil
		}
	}
	return b.store.ContextCooling(ctx, sessionID)
}

// CoolingCandidate protects the current turn and keepEnded preceding input
// turns. Notification receipts, steers and Compact's synthetic turns do not count.
func CoolingCandidate(messages []*store.Message, currentTurnID string, keepEnded int) store.ContextCooling {
	messages = historyInTurnOrder(EffectiveMessages(messages))
	var turns []int
	state := store.ContextCooling{}
	for i, m := range messages {
		if m.Role == store.RoleSummary {
			state.SummaryMessageID = m.ID
		}
		if IsInputTurnBoundary(m) && m.TurnID != currentTurnID {
			turns = append(turns, i)
		}
	}
	if len(turns) <= keepEnded {
		return state
	}
	// Include receipts following the last cold input, up to the first hot input.
	split := turns[len(turns)-keepEnded-1] + 1
	for split < len(messages) && !IsInputTurnBoundary(messages[split]) {
		split++
	}
	// A hot action can refer to a receipt recorded before that action started.
	// Keep that source available too; this boundary is persisted, never slid.
	sources := map[string]bool{}
	for _, m := range messages[split:] {
		if source := widgetActionSource(m); source != "" {
			sources[source] = true
		}
	}
	for i, m := range messages[:split] {
		if sources[m.ID] {
			split = i
			break
		}
	}
	if split > 0 && messages[split-1].Role != store.RoleSummary {
		state.ThroughTurnID = messages[split-1].TurnID
	}
	return state
}

func cooledMessages(messages []*store.Message, state store.ContextCooling) []*store.Message {
	if state.ThroughTurnID == "" {
		return messages
	}
	summaryID := ""
	coldTurns := map[string]bool{}
	found := false
	for _, m := range messages {
		if m.Role == store.RoleSummary {
			summaryID = m.ID
		}
		if !found && m.TurnID != "" && m.Role != store.RoleSummary {
			coldTurns[m.TurnID] = true
			found = m.TurnID == state.ThroughTurnID
		}
	}
	// A proposed Compact or a hard task reset must not inherit an old watermark.
	if !found || summaryID != state.SummaryMessageID {
		return messages
	}
	out := append([]*store.Message(nil), messages...)
	for i, m := range messages {
		if !coldTurns[m.TurnID] {
			continue
		}
		if notice := widgetNotification(m); notice != nil {
			out[i] = coldWidgetNotification(m, notice)
			continue
		}
		if m.Role != store.RoleTool && m.Role != store.RoleAssistant {
			continue
		}
		copy := *m
		copy.Parts = append([]store.ContentPart(nil), m.Parts...)
		for j, part := range copy.Parts {
			if part.Type == store.ContentPartToolResult {
				copy.Parts[j].Content = tool.ColdResultContent(part.Name, part.Ok, part.Content, m.TurnID, part.CallID)
				if copy.Parts[j].Content != part.Content {
					copy.Parts[j].ModelOutput = nil
					copy.Parts[j].MCPResult = nil
				}
			}
		}
		out[i] = &copy
	}
	return out
}

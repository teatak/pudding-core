package engine

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"strings"

	"github.com/teatak/pudding-core/internal/config"
	"github.com/teatak/pudding-core/internal/contextbuilder"
	"github.com/teatak/pudding-core/internal/provider"
	"github.com/teatak/pudding-core/internal/store"
)

const (
	defaultCompactTailInputTurns = 2
	compactMessageTextLimit      = 4000
)

type CompactInput struct {
	SessionID string
	Hint      string
}

type CompactResult struct {
	TurnID           string `json:"turnID"`
	SummaryMessageID string `json:"summaryMessageID"`
	Status           string `json:"status"`
	SourceMessages   int    `json:"sourceMessages"`
	TailMessages     int    `json:"tailMessages"`
	SummaryChars     int    `json:"summaryChars"`
}

func (e *Engine) Compact(ctx context.Context, in CompactInput) (*CompactResult, error) {
	sessionID := strings.TrimSpace(in.SessionID)
	if sessionID == "" {
		return nil, store.ErrNotFound
	}
	if !e.compactMu.TryLock() {
		return nil, ErrCompactRunning
	}
	defer e.compactMu.Unlock()

	if _, err := e.store.RunningTurn(ctx, sessionID); err == nil {
		return nil, ErrTurnRunning
	} else if !errors.Is(err, store.ErrNotFound) {
		return nil, err
	}
	sess, err := e.store.GetSession(ctx, sessionID)
	if err != nil {
		return nil, err
	}
	resolved, err := e.resolveModel(ctx, sess)
	if err != nil {
		return nil, err
	}
	resolved.mode = initialMode(sess)
	client, err := e.resolver.Resolve(ctx, resolved.providerName)
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrProviderConfig, err)
	}
	msgs, err := e.store.ListMessages(ctx, sessionID, 0)
	if err != nil {
		return nil, err
	}
	effective := contextbuilder.EffectiveMessages(msgs)
	cold, tail := contextbuilder.SplitRecentInputTail(effective, e.compactTailInputTurns(ctx))
	if len(cold) == 0 {
		return nil, ErrCompactEmpty
	}
	sourceIDs := messageIDs(cold)
	tailIDs := messageIDs(tail)
	dump := compactHistoryDump(cold)
	if strings.TrimSpace(dump) == "" {
		return nil, ErrCompactEmpty
	}
	summary, err := e.generateCompactSummary(ctx, sessionID, resolved, client, dump, strings.TrimSpace(in.Hint))
	if err != nil {
		return nil, err
	}
	if strings.TrimSpace(summary) == "" {
		return nil, ErrCompactEmpty
	}

	turnID := store.NewID("turn")
	msgID := store.NewID("msg")
	res, err := e.store.AppendCompactSummary(ctx, store.AppendCompactSummaryInput{
		SessionID:       sessionID,
		TurnID:          turnID,
		MessageID:       msgID,
		ClientMessageID: "compact:" + turnID,
		Provider:        resolved.providerName,
		Model:           resolved.model,
		Mode:            resolved.mode,
		ModelConfig:     resolved.configJSON,
		Text:            summary,
		Metadata:        store.CompactMessageMetadataWithCounts(sourceIDs, tailIDs, messageInputTurnCount(cold), messageInputTurnCount(tail)),
	})
	if err != nil {
		return nil, err
	}
	if res.FinalEvent != nil {
		e.hub.Publish(*res.FinalEvent)
	}
	return &CompactResult{
		TurnID:           turnID,
		SummaryMessageID: msgID,
		Status:           string(store.TurnCompleted),
		SourceMessages:   len(sourceIDs),
		TailMessages:     len(tailIDs),
		SummaryChars:     len([]rune(summary)),
	}, nil
}

func (e *Engine) compactTailInputTurns(ctx context.Context) int {
	return e.intSetting(ctx, config.SettingCompactTailInputTurns, defaultCompactTailInputTurns, 1, 50)
}

func (e *Engine) autoCompactThresholdPercent(ctx context.Context) int {
	return e.intSetting(ctx, config.SettingCompactAutoThresholdPercent, config.DefaultCompactAutoThresholdPercent, 0, 100)
}

func (e *Engine) intSetting(ctx context.Context, key string, fallback, min, max int) int {
	settings, err := e.config.Settings(ctx)
	if err != nil {
		return fallback
	}
	n, err := strconv.Atoi(strings.TrimSpace(settings[key]))
	if err != nil || n < min || n > max {
		return fallback
	}
	return n
}

func messageInputTurnCount(msgs []*store.Message) int {
	seen := make(map[string]struct{}, len(msgs))
	for _, msg := range msgs {
		if !contextbuilder.IsInputTurnBoundary(msg) {
			continue
		}
		seen[msg.TurnID] = struct{}{}
	}
	return len(seen)
}

func (e *Engine) generateCompactSummary(ctx context.Context, sessionID string, resolved *resolvedModel, client provider.Client, history, hint string) (string, error) {
	user := compactUserPrompt(history, hint)
	req := provider.Request{
		Model:  resolved.model,
		System: compactSystemPrompt,
		Config: resolved.config,
		Messages: []provider.Message{{
			Role:  provider.RoleUser,
			Text:  user,
			Parts: []provider.Part{{Type: provider.PartText, Text: user}},
		}},
	}
	ch, err := client.Stream(ctx, req)
	if err != nil {
		return "", fmt.Errorf("provider: %w", err)
	}
	var out strings.Builder
	var usage provider.UsageInfo
	usageSeen := false
	for {
		select {
		case <-ctx.Done():
			return "", ctx.Err()
		case chunk, ok := <-ch:
			if !ok {
				if usageSeen {
					e.recordUsage(ctx, sessionID, resolved.model, usage, 1)
				}
				return "", errors.New("provider stream ended without terminal chunk")
			}
			switch {
			case chunk.Err != nil:
				if usageSeen {
					e.recordUsage(ctx, sessionID, resolved.model, usage, 1)
				}
				return "", chunk.Err
			case chunk.Usage != nil:
				mergeUsageInfo(&usage, *chunk.Usage)
				usageSeen = true
			case chunk.Done:
				if usageSeen {
					e.recordUsage(ctx, sessionID, resolved.model, usage, 1)
				} else {
					e.recordUsage(ctx, sessionID, resolved.model, provider.UsageInfo{}, 1)
				}
				return strings.TrimSpace(out.String()), nil
			case chunk.Delta != "":
				part := chunk.Part
				if part == "" {
					part = provider.PartText
				}
				if part == provider.PartText {
					out.WriteString(chunk.Delta)
				}
			}
		}
	}
}

const compactSystemPrompt = `You compact conversation history for future LLM context.

Accuracy is more important than brevity. Preserve user preferences, key facts, decisions, file paths, identifiers, commands, code names, current task state, TODOs, blockers, and open questions.

Use the dominant language of the conversation. Cite important source messages with @message(message_id). You may omit greetings, repeated text, and bulky tool output.

Return markdown with these sections:
## User Context
## Key Decisions
## Recent Actions
## TODO / Open Questions`

func compactUserPrompt(history, hint string) string {
	var b strings.Builder
	b.WriteString("Summarize the following conversation history into a compact context summary.\n")
	if hint != "" {
		b.WriteString("\nUser hint:\n")
		b.WriteString(hint)
		b.WriteString("\n")
	}
	b.WriteString("\n<conversation>\n")
	b.WriteString(history)
	b.WriteString("\n</conversation>\n")
	return b.String()
}

func compactHistoryDump(msgs []*store.Message) string {
	var b strings.Builder
	for _, msg := range msgs {
		text := strings.TrimSpace(msg.Text)
		if text == "" {
			text = strings.TrimSpace(store.MessageTextFromParts(msg.Parts))
		}
		if text == "" {
			continue
		}
		b.WriteString(`<message id="`)
		b.WriteString(msg.ID)
		b.WriteString(`" ref="@message(`)
		b.WriteString(msg.ID)
		b.WriteString(`)" role="`)
		b.WriteString(string(msg.Role))
		b.WriteString(`">`)
		b.WriteString("\n")
		b.WriteString(truncateCompactText(text, compactMessageTextLimit))
		b.WriteString("\n</message>\n")
	}
	return b.String()
}

func truncateCompactText(text string, limit int) string {
	if limit <= 0 {
		return text
	}
	runes := []rune(text)
	if len(runes) <= limit {
		return text
	}
	return string(runes[:limit]) + "\n[truncated]"
}

func messageIDs(msgs []*store.Message) []string {
	out := make([]string, 0, len(msgs))
	for _, msg := range msgs {
		if msg != nil && msg.ID != "" {
			out = append(out, msg.ID)
		}
	}
	return out
}

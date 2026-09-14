package engine

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"math"
	"strconv"
	"strings"
	"time"

	"github.com/teatak/pudding-core/internal/config"
	"github.com/teatak/pudding-core/internal/contextbuilder"
	"github.com/teatak/pudding-core/internal/provider"
	"github.com/teatak/pudding-core/internal/store"
	"github.com/teatak/pudding-core/internal/tool"
)

const (
	defaultCompactTailInputTurns = 2
	compactSummaryMaxTokens      = 4096
	compactMaxInputTokens        = 64000
	autoCompactTimeout           = 2 * time.Minute
)

type CompactInput struct {
	SessionID       string
	Hint            string
	ClientMessageID string
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
	return e.compactIdle(ctx, in, false)
}

func (e *Engine) compactIdle(ctx context.Context, in CompactInput, automatic bool) (*CompactResult, error) {
	sessionID := strings.TrimSpace(in.SessionID)
	if sessionID == "" {
		return nil, store.ErrNotFound
	}
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
	return e.compact(ctx, in, resolved, resolved.mode, "", automatic)
}

// compact is also called by the engine between complete provider exchanges.
// The store checks the owning running turn and snapshot end in the same
// transaction as the summary, turn and event write.
func (e *Engine) compact(ctx context.Context, in CompactInput, resolved *resolvedModel, mode store.AgentMode, runningTurnID string, automatic bool) (*CompactResult, error) {
	sessionID := strings.TrimSpace(in.SessionID)
	e.mu.Lock()
	if e.compacting[sessionID] {
		e.mu.Unlock()
		return nil, ErrCompactRunning
	}
	e.compacting[sessionID] = true
	e.mu.Unlock()
	defer func() {
		e.mu.Lock()
		delete(e.compacting, sessionID)
		e.mu.Unlock()
	}()
	client, err := e.resolver.Resolve(ctx, resolved.providerName)
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrProviderConfig, err)
	}
	all, err := e.store.ListMessages(ctx, sessionID, 0)
	if err != nil {
		return nil, err
	}
	if len(all) == 0 {
		return nil, ErrCompactEmpty
	}
	var defs []provider.ToolDef
	if modelSupportsTools(resolved.config) {
		defs, err = e.toolDefinitions(ctx, sessionID, mode)
		if err != nil {
			return nil, err
		}
	}
	project := func(messages []*store.Message) (int, error) {
		req, err := e.builder.BuildForProviderWithHistory(ctx, sessionID, resolved.providerName, resolved.model, string(mode), defs, messages, resolved.config)
		if err != nil {
			return 0, err
		}
		req.Config, req.Tools = resolved.config, defs
		req, err = e.builder.ResolveSkillReferences(ctx, sessionID, string(mode), req)
		if err != nil {
			return 0, err
		}
		return contextbuilder.EstimateRequest(req).Total(), nil
	}
	before, err := project(all)
	if err != nil {
		return nil, err
	}
	stat, err := e.store.SessionUsage(ctx, sessionID)
	if err != nil {
		return nil, err
	}
	factor, _ := sessionInputCalibrationFactor(stat, resolved.providerName, resolved.model)
	inputLimit := contextInputLimit(resolved.config, resolved.protocol)
	target := inputLimit
	if trigger := compactTrigger(resolved.config, resolved.protocol, e.autoCompactThresholdPercent(ctx)); automatic && trigger > 0 {
		target = trigger
	}
	if target == 0 {
		target = calibratedTokenEstimate(before, factor)
	}
	effective := contextbuilder.EffectiveMessages(all)
	summaryID, turnID := store.NewID("msg"), store.NewID("turn")
	if runningTurnID != "" {
		turnID = runningTurnID
	}
	proposed := &store.Message{ID: summaryID, SessionID: sessionID, TurnID: turnID, Role: store.RoleSummary}
	candidate := append(append([]*store.Message(nil), all...), proposed)
	var cold, tail []*store.Message
	summaryTokens := 0
	for keep := e.compactTailInputTurns(ctx); keep >= 0; keep-- {
		cold, tail = compactPartition(effective, keep, runningTurnID)
		proposed.Metadata = store.CompactMessageMetadataWithCounts(messageIDs(cold), messageIDs(tail), messageInputTurnCount(cold), messageInputTurnCount(tail))
		overhead, err := project(candidate)
		if err != nil {
			return nil, err
		}
		room := int(math.Floor(float64(target-calibratedTokenEstimate(overhead, factor))/factor)) - 4
		if len(cold) == 0 {
			if room > 0 || keep == 0 {
				return nil, ErrCompactEmpty
			}
			continue
		}
		// Aim below both the post-compact budget and half the replaced content.
		summaryTokens = min(compactSummaryMaxTokens, room, (before-overhead-4)/2)
		if !automatic && keep == 0 {
			// Manual compaction may still reduce history when fixed prompts/tools
			// alone exceed capacity. Only the summary request must fit to run it.
			summaryTokens = max(1, min(compactSummaryMaxTokens, (before-overhead-4)/2))
		}
		if summaryTokens > 0 {
			break
		}
	}
	if summaryTokens <= 0 {
		return nil, ErrContextBudget
	}
	summary, err := e.summarizeCompactHistory(ctx, sessionID, resolved, client, cold, strings.TrimSpace(in.Hint), summaryTokens)
	if err != nil {
		return nil, err
	}
	proposed.Text, proposed.Parts = summary, store.TextPart(summary)
	after, err := project(candidate)
	if err != nil {
		return nil, err
	}
	if after >= before {
		return nil, ErrCompactNotReduced
	}
	if automatic && calibratedTokenEstimate(after, factor) > target {
		return nil, ErrContextBudget
	}
	metadata, _ := store.CompactMetadataFromMessage(proposed)
	metadata.BeforeInputEstimate = calibratedTokenEstimate(before, factor)
	metadata.AfterInputEstimate = calibratedTokenEstimate(after, factor)
	metadata.InputBudget = inputLimit
	proposed.Metadata, err = json.Marshal(store.MessageMetadata{Compact: metadata})
	if err != nil {
		return nil, err
	}
	clientMessageID := strings.TrimSpace(in.ClientMessageID)
	if clientMessageID == "" {
		clientMessageID = "compact:" + turnID
	}
	res, err := e.store.AppendCompactSummary(ctx, store.AppendCompactSummaryInput{
		ExpectedLastMessageID: all[len(all)-1].ID,
		RunningTurnID:         runningTurnID,
		SessionID:             sessionID, TurnID: turnID, MessageID: summaryID,
		ClientMessageID: clientMessageID, Provider: resolved.providerName,
		Model: resolved.model, Mode: mode, ModelConfig: resolved.configJSON,
		Text: summary, Metadata: proposed.Metadata,
	})
	if err != nil {
		return nil, err
	}
	if res.Event != nil {
		e.hub.Publish(*res.Event)
	}
	slog.Info("engine: context compacted", "sessionID", sessionID, "beforeInputEstimate", before, "afterInputEstimate", after, "sourceMessages", len(cold), "tailMessages", len(tail))
	return &CompactResult{TurnID: turnID, SummaryMessageID: summaryID, Status: string(res.Turn.Status),
		SourceMessages: len(cold), TailMessages: len(tail), SummaryChars: len([]rune(summary))}, nil
}

// Start with the configured recent input turns. If they cannot fit, progressively
// move older complete turns into the summary. At a running turn's last boundary,
// keep all its inputs and its latest complete tool exchange (including native
// continuation state and attachments).
func compactPartition(messages []*store.Message, keep int, runningTurnID string) ([]*store.Message, []*store.Message) {
	if keep > 0 || runningTurnID == "" {
		return contextbuilder.SplitRecentInputTail(messages, keep)
	}
	start := len(messages)
	for i := len(messages) - 1; i >= 0; i-- {
		if messages[i].TurnID != runningTurnID {
			continue
		}
		for _, part := range messages[i].Parts {
			if part.Type == store.ContentPartToolUse {
				start = i
				break
			}
		}
		if start != len(messages) {
			break
		}
	}
	if start < len(messages) {
		for start > 0 && messages[start-1].TurnID == runningTurnID && messages[start-1].Role == store.RoleAssistant {
			start--
		}
		// Attachments from the previous exchange stay with its tool result.
		for start < len(messages) && len(messages[start].Parts) == 1 && messages[start].Parts[0].Type == store.ContentPartAttachment {
			start++
		}
	}
	var cold, tail []*store.Message
	for i, message := range messages {
		protected := message.TurnID == runningTurnID && (message.Role == store.RoleUser || message.Role == store.RoleSystem || i >= start)
		if protected {
			tail = append(tail, message)
		} else {
			cold = append(cold, message)
		}
	}
	return cold, tail
}

func (e *Engine) scheduleAutoCompact(sessionID string) {
	sessionID = strings.TrimSpace(sessionID)
	if sessionID == "" {
		return
	}
	e.wg.Add(1)
	go func() {
		defer e.wg.Done()
		ctx, cancel := context.WithTimeout(e.auxCtx, autoCompactTimeout)
		defer cancel()
		if err := e.maybeAutoCompact(ctx, sessionID); err != nil && !errors.Is(err, context.Canceled) && !errors.Is(err, context.DeadlineExceeded) {
			slog.Warn("engine: auto compact failed", "sessionID", sessionID, "err", err)
		}
	}()
}

func (e *Engine) maybeAutoCompact(ctx context.Context, sessionID string) error {
	due, err := e.autoCompactDue(ctx, sessionID)
	if err != nil || !due {
		return err
	}
	if queued, err := e.store.HasQueuedInputs(ctx, sessionID); err != nil {
		return err
	} else if queued {
		return nil
	}
	_, err = e.compactIdle(ctx, CompactInput{SessionID: sessionID}, true)
	if errors.Is(err, ErrCompactEmpty) || errors.Is(err, ErrCompactRunning) || errors.Is(err, ErrTurnRunning) || errors.Is(err, store.ErrHistoryChanged) || errors.Is(err, ErrCompactNotReduced) {
		return nil
	}
	return err
}

func (e *Engine) autoCompactDue(ctx context.Context, sessionID string) (bool, error) {
	if e.autoCompactThresholdPercent(ctx) <= 0 {
		return false, nil
	}
	if _, err := e.store.RunningTurn(ctx, sessionID); err == nil {
		return false, nil
	} else if !errors.Is(err, store.ErrNotFound) {
		return false, err
	}
	if queued, err := e.store.HasQueuedInputs(ctx, sessionID); err != nil {
		return false, err
	} else if queued {
		return false, nil
	}
	usage, err := e.SessionUsage(ctx, sessionID)
	if err != nil {
		return false, err
	}
	effectiveTokens := usage.ContextEstimatedTokens
	if usage.LastPromptTokens > effectiveTokens {
		effectiveTokens = usage.LastPromptTokens
	}
	return usage.AutoCompactThresholdTokens > 0 && effectiveTokens >= usage.AutoCompactThresholdTokens, nil
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

func compactSummaryRequest(resolved *resolvedModel, history, hint, previous string, target int) provider.Request {
	cfg := resolved.config
	limits := provider.ModelLimits{}
	if cfg.Limits != nil {
		limits = *cfg.Limits
	}
	limits.MaxOutputTokens = compactSummaryMaxTokens
	if configured, ok := resolved.config.MaxOutputTokens(); ok {
		limits.MaxOutputTokens = min(limits.MaxOutputTokens, configured)
	}
	if cfg.ContextWindow > 0 {
		limits.MaxOutputTokens = min(limits.MaxOutputTokens, max(1, cfg.ContextWindow/4))
	}
	cfg.Limits = &limits
	user := fmt.Sprintf("Keep the updated summary within approximately %d tokens. Preserve unresolved tasks and source references.\n", target)
	if previous != "" {
		user += "\nPrevious summary of earlier fragments; update it with the history below:\n" + previous + "\n"
	}
	user += compactUserPrompt(history, hint)
	return provider.Request{Model: resolved.model, System: compactSystemPrompt, Config: cfg,
		Messages: []provider.Message{{Role: provider.RoleUser, Text: user, Parts: []provider.Part{{Type: provider.PartText, Text: user}}}}}
}

func (e *Engine) generateCompactSummary(ctx context.Context, sessionID string, resolved *resolvedModel, client provider.Client, req provider.Request) (string, error) {
	estimatedInputTokens := contextbuilder.EstimateRequest(req).Total()
	if limit := contextInputLimit(req.Config, resolved.protocol); limit > 0 && estimatedInputTokens > limit {
		return "", ErrContextBudget
	}
	callCtx, cancel := context.WithCancel(ctx)
	defer cancel()
	ch, err := client.Stream(callCtx, req)
	if err != nil {
		return "", fmt.Errorf("provider: %w", err)
	}
	// Providers send their terminal chunk even after cancellation. Drain it on
	// every exit, including rejected/truncated summaries.
	defer func() {
		cancel()
		for range ch {
		}
	}()
	var out strings.Builder
	var usage provider.UsageInfo
	defer func() {
		e.recordUsage(context.Background(), sessionID, resolved.providerName, resolved.model, estimatedInputTokens, usage, 1)
	}()
	for {
		select {
		case <-ctx.Done():
			return "", ctx.Err()
		case chunk, ok := <-ch:
			if !ok {
				return "", errors.New("provider stream ended without terminal chunk")
			}
			if chunk.Err != nil {
				return "", chunk.Err
			}
			if chunk.Usage != nil {
				mergeUsageInfo(&usage, *chunk.Usage)
			}
			if chunk.Part == "" || chunk.Part == provider.PartText {
				out.WriteString(chunk.Delta)
			}
			if chunk.Done {
				if chunk.Finish != "" && chunk.Finish != provider.FinishStop {
					return "", fmt.Errorf("%w: %s", ErrCompactIncomplete, chunk.Finish)
				}
				summary := strings.TrimSpace(out.String())
				if summary == "" {
					return "", ErrCompactSummaryEmpty
				}
				return summary, nil
			}
		}
	}
}

const compactSystemPrompt = `You compact conversation history for future LLM context.

Accuracy is more important than brevity. Preserve user preferences, key facts, decisions, file paths, identifiers, commands, code names, current task state, TODOs, blockers, and open questions.

Use the dominant language of the conversation. Cite important source messages with @message(message_id). You may omit greetings, repeated text, and bulky tool output.

Do not copy or summarize App/Skill instruction bodies into the summary. Their registered references are retained separately and resolved from current sources on future requests. Summarize task facts and user decisions, not historical tool operating rules.

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

func compactMessageRecord(msg *store.Message, text string) string {
	if strings.TrimSpace(text) == "" {
		return ""
	}
	return fmt.Sprintf("<message id=%q ref=%q role=%q>\n%s\n</message>\n", msg.ID, "@message("+msg.ID+")", msg.Role, text)
}

func compactMessageText(msg *store.Message) string {
	if len(msg.Parts) == 0 {
		return msg.Text
	}
	var text strings.Builder
	for _, part := range msg.Parts {
		switch part.Type {
		case store.ContentPartThought:
			continue
		case store.ContentPartText:
			text.WriteString(part.Text)
		case store.ContentPartToolUse:
			fmt.Fprintf(&text, "tool_call %s id=%s args=%s", part.Name, part.CallID, part.Args)
		case store.ContentPartToolResult:
			content := tool.ModelResultContent(part.Name, part.Ok, part.Content, msg.TurnID, part.CallID, true)
			fmt.Fprintf(&text, "tool_result %s id=%s ok=%t\n%s", part.Name, part.CallID, part.Ok, content)
		default:
			// Keep form answers, attachment references and workspace context.
			// No attachment bytes or hidden provider state are serialized here.
			data, _ := json.Marshal(part)
			text.Write(data)
		}
		text.WriteByte('\n')
	}
	return strings.TrimSpace(text.String())
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

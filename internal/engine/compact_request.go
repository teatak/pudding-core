package engine

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/teatak/pudding-core/internal/contextbuilder"
	"github.com/teatak/pudding-core/internal/provider"
	"github.com/teatak/pudding-core/internal/store"
)

// Send the complete effective history once when it fits. If a multi-message
// compaction exceeds the model window, fall back to rolling batches at
// message/turn boundaries without truncating individual messages or splitting
// single oversized items.
func (e *Engine) summarizeCompactHistory(ctx context.Context, sessionID string, resolved *resolvedModel, client provider.Client, effective, source []*store.Message, hint string, target int) (string, error) {
	history, err := compactHistoryText(effective, source)
	if err != nil {
		return "", err
	}
	req, err := compactSummaryRequest(resolved, history, hint, target)
	if err == nil {
		return e.generateCompactSummary(ctx, sessionID, resolved, client, req)
	}
	if !errors.Is(err, ErrContextBudget) {
		return "", err
	}
	return e.summarizeCompactBatches(ctx, sessionID, resolved, client, effective, source, hint, target, err)
}

func compactHistoryText(effective, source []*store.Message) (string, error) {
	externalTurns := contextbuilder.ExternalMCPTurns(effective)
	sourceIDs := make(map[string]bool, len(source))
	for _, message := range source {
		sourceIDs[message.ID] = true
	}
	var history strings.Builder
	hasSource := false
	for _, message := range effective {
		text := compactMessageTextFor(message, externalTurns[message.TurnID])
		if strings.TrimSpace(text) == "" {
			continue
		}
		action := "retain"
		if sourceIDs[message.ID] {
			action, hasSource = "summarize", true
		}
		history.WriteString(compactMessageRecord(message, text, action))
	}
	if !hasSource {
		return "", ErrCompactEmpty
	}
	return history.String(), nil
}

func (e *Engine) summarizeCompactBatches(
	ctx context.Context,
	sessionID string,
	resolved *resolvedModel,
	client provider.Client,
	effective, source []*store.Message,
	hint string,
	target int,
	singleErr error,
) (string, error) {
	externalTurns := contextbuilder.ExternalMCPTurns(effective)
	nonEmptySource := make([]*store.Message, 0, len(source))
	for _, message := range source {
		if text := compactMessageTextFor(message, externalTurns[message.TurnID]); strings.TrimSpace(text) != "" {
			nonEmptySource = append(nonEmptySource, message)
		}
	}
	if len(nonEmptySource) == 0 {
		return "", singleErr
	}
	// Fail before calling the provider if any single message cannot fit alone.
	for _, message := range nonEmptySource {
		text := compactMessageTextFor(message, externalTurns[message.TurnID])
		if _, err := compactSummaryRequest(resolved, compactMessageRecord(message, text, "summarize"), hint, target); err != nil {
			return "", err
		}
	}
	sourceIDs := make(map[string]bool, len(nonEmptySource))
	for _, message := range nonEmptySource {
		sourceIDs[message.ID] = true
	}
	hasRetain := false
	for _, message := range effective {
		if sourceIDs[message.ID] {
			continue
		}
		if strings.TrimSpace(compactMessageTextFor(message, externalTurns[message.TurnID])) != "" {
			hasRetain = true
			break
		}
	}
	maxOut := compactMaxOutputTokens
	if configured := configuredOutputLimit(resolved.config, resolved.protocol); configured > 0 {
		maxOut = min(maxOut, configured)
	}
	minIntermediateOut := min(512, max(2, maxOut/2), max(2, target))
	intermediateTarget := min(target, max(256, minIntermediateOut))

	fitRequest := func(history string, batchTarget, minOut int) (provider.Request, bool) {
		req, err := compactSummaryRequest(resolved, history, hint, batchTarget)
		if err != nil {
			return provider.Request{}, false
		}
		if resolved.config.ContextWindow > 0 {
			output, _ := req.Config.MaxOutputTokens()
			if output < minOut {
				return provider.Request{}, false
			}
		}
		return req, true
	}
	largestFitting := func(rolling string, msgs []*store.Message, minOut int) int {
		lo, hi, best := 1, len(msgs), 0
		for lo <= hi {
			mid := (lo + hi) / 2
			text := rollingBatchHistory(rolling, msgs[:mid], effective, sourceIDs, false, externalTurns)
			if _, ok := fitRequest(text, intermediateTarget, minOut); ok {
				best = mid
				lo = mid + 1
			} else {
				hi = mid - 1
			}
		}
		return best
	}

	remaining := nonEmptySource
	rolling := ""
	probeTokens := min(compactSummaryMaxTokens, max(1, intermediateTarget))
	for step := 0; len(remaining) > 0 && step < len(nonEmptySource)+2; step++ {
		if req, ok := fitRequest(rollingBatchHistory(rolling, remaining, effective, sourceIDs, hasRetain, externalTurns), target, 2); ok {
			return e.generateCompactSummary(ctx, sessionID, resolved, client, req)
		}
		end := largestFitting(rolling, remaining, minIntermediateOut)
		if end == 0 {
			end = largestFitting(rolling, remaining, 2)
		}
		if end == 0 {
			return "", singleErr
		}
		if end < len(remaining) {
			if aligned := alignChunkToTurnBoundary(remaining, end); aligned > 0 {
				end = aligned
			}
		} else if hasRetain && len(remaining) > 1 {
			probe := rollingBatchHistory(strings.Repeat("x ", probeTokens*2), nil, effective, sourceIDs, true, externalTurns)
			if _, ok := fitRequest(probe, target, 2); ok {
				half := max(1, len(remaining)/2)
				if aligned := alignChunkToTurnBoundary(remaining, half); aligned > 0 {
					end = aligned
				} else {
					end = half
				}
			} else {
				req, err := compactSummaryRequest(resolved, rollingBatchHistory(rolling, remaining, effective, sourceIDs, false, externalTurns), hint, target)
				if err != nil {
					return "", err
				}
				return e.generateCompactSummary(ctx, sessionID, resolved, client, req)
			}
		} else {
			req, err := compactSummaryRequest(resolved, rollingBatchHistory(rolling, remaining, effective, sourceIDs, false, externalTurns), hint, target)
			if err != nil {
				return "", err
			}
			return e.generateCompactSummary(ctx, sessionID, resolved, client, req)
		}
		req, err := compactSummaryRequest(resolved, rollingBatchHistory(rolling, remaining[:end], effective, sourceIDs, false, externalTurns), hint, intermediateTarget)
		if err != nil {
			return "", err
		}
		rolling, err = e.generateCompactSummary(ctx, sessionID, resolved, client, req)
		if err != nil {
			return "", err
		}
		remaining = remaining[end:]
	}
	if len(remaining) > 0 {
		return "", singleErr
	}
	return rolling, nil
}

func alignChunkToTurnBoundary(msgs []*store.Message, end int) int {
	if end >= len(msgs) {
		return len(msgs)
	}
	for i := end; i >= 1; i-- {
		if msgs[i-1].TurnID != "" && msgs[i-1].TurnID != msgs[i].TurnID {
			return i
		}
	}
	return 0
}

func rollingBatchHistory(rolling string, chunk, effective []*store.Message, sourceIDs map[string]bool, includeRetain bool, externalTurns map[string]bool) string {
	var b strings.Builder
	hasRolling := strings.TrimSpace(rolling) != ""
	wroteRolling := false
	writeRolling := func() {
		if hasRolling && !wroteRolling {
			b.WriteString(compactMessageRecord(&store.Message{ID: "rolling_summary", Role: store.RoleSummary}, rolling, "summarize"))
			wroteRolling = true
		}
	}
	if !includeRetain {
		writeRolling()
		for _, message := range chunk {
			text := compactMessageTextFor(message, externalTurns[message.TurnID])
			if strings.TrimSpace(text) == "" {
				continue
			}
			b.WriteString(compactMessageRecord(message, text, "summarize"))
		}
		return b.String()
	}
	chunkIDs := make(map[string]bool, len(chunk))
	for _, message := range chunk {
		chunkIDs[message.ID] = true
	}
	for _, message := range effective {
		if sourceIDs[message.ID] {
			writeRolling()
			if !chunkIDs[message.ID] {
				continue
			}
			text := compactMessageTextFor(message, externalTurns[message.TurnID])
			if strings.TrimSpace(text) == "" {
				continue
			}
			b.WriteString(compactMessageRecord(message, text, "summarize"))
			continue
		}
		text := compactMessageTextFor(message, externalTurns[message.TurnID])
		if strings.TrimSpace(text) == "" {
			continue
		}
		b.WriteString(compactMessageRecord(message, text, "retain"))
	}
	writeRolling()
	return b.String()
}

func compactSummaryRequest(resolved *resolvedModel, history, hint string, target int) (provider.Request, error) {
	outputLimit := compactMaxOutputTokens
	if configured := configuredOutputLimit(resolved.config, resolved.protocol); configured > 0 {
		outputLimit = min(outputLimit, configured)
	}
	target = max(1, min(target, compactSummaryMaxTokens, outputLimit/2))
	for {
		user := fmt.Sprintf("Keep the summary within approximately %d visible-text tokens. Be concise and preserve unresolved tasks and source references.\n", target) + compactUserPrompt(history, hint)
		req := provider.Request{Model: resolved.model, System: compactSystemPrompt, Config: resolved.config,
			Messages: []provider.Message{{Role: provider.RoleUser, Text: user, Parts: []provider.Part{{Type: provider.PartText, Text: user}}}}}
		estimate, err := contextbuilder.EstimateRequest(req, resolved.protocol)
		if err != nil {
			return provider.Request{}, err
		}
		output := outputLimit
		if window := req.Config.ContextWindow; window > 0 {
			input := estimate.Total()
			margin := compactInputMargin(input)
			output = min(output, window-input-margin)
			if output < 2 {
				return provider.Request{}, fmt.Errorf("%w: estimated summary input %d + margin %d leaves no usable output space in window %d; use a larger-context model", ErrContextBudget, input, margin, window)
			}
		}
		if output < 2 {
			return provider.Request{}, fmt.Errorf("%w: summary output limit %d is too small", ErrContextBudget, output)
		}
		// Changing the target changes the prompt. Re-estimate locally until it
		// fits; this loop never sends a model request or removes any history.
		if smaller := min(target, output/2); smaller < target {
			target = smaller
			continue
		}
		req.Config = compactSummaryConfig(resolved.config, output)
		return req, nil
	}
}

// This is an estimation allowance, not an exact tokenizer guarantee. Keep it
// proportional to the actual summary input rather than the model's window.
func compactInputMargin(input int) int { return max(256, (input+49)/50) }

func compactSummaryConfig(base provider.ModelConfig, output int) provider.ModelConfig {
	cfg := base
	limits := provider.ModelLimits{}
	if base.Limits != nil {
		limits = *base.Limits
	}
	limits.MaxOutputTokens = output
	cfg.Limits = &limits
	if base.ProviderOptions == nil {
		return cfg
	}
	opts := &provider.ModelProviderOptions{
		OpenAI: cloneOptions(base.OpenAIOptions()), Google: cloneOptions(base.GoogleOptions()), Anthropic: cloneOptions(base.AnthropicOptions()),
	}
	cfg.ProviderOptions = opts
	// Only lower configured reasoning controls. Do not introduce unsupported
	// parameters to models that have no reasoning configuration.
	if effort, ok := provider.StringOption(opts.OpenAI, "reasoning_effort"); ok {
		opts.OpenAI["reasoning_effort"] = compactReasoningEffort(effort)
	}
	if original, ok := opts.Anthropic["output_config"].(map[string]any); ok {
		if effort, ok := provider.StringOption(original, "effort"); ok {
			config := cloneOptions(original)
			config["effort"] = compactReasoningEffort(effort)
			opts.Anthropic["output_config"] = config
		}
	}
	compactGoogleReasoning(opts.Google, output)
	return cfg
}

func compactReasoningEffort(effort string) string {
	switch strings.ToLower(strings.TrimSpace(effort)) {
	case "none", "minimal", "low":
		return effort
	default:
		return "low"
	}
}

func compactGoogleReasoning(opts map[string]any, output int) {
	var thinking map[string]any
	for _, key := range []string{"thinking", "thinkingConfig", "thinking_config"} {
		if value, ok := opts[key].(map[string]any); ok {
			thinking = value
			break
		}
	}
	level, _ := provider.StringOption(thinking, "level", "thinkingLevel", "thinking_level")
	if level == "" {
		level, _ = provider.StringOption(opts, "thinkingLevel", "thinking_level")
	}
	budget, budgetSet := provider.IntOption(thinking, "budget", "thinkingBudget", "thinking_budget")
	if !budgetSet {
		budget, budgetSet = provider.IntOption(opts, "thinkingBudget", "thinking_budget")
	}
	if level == "" && !budgetSet {
		return
	}
	lowBudget := min(1024, output/2)
	if level == "minimal" {
		lowBudget = min(lowBudget, 128)
	}
	if budgetSet && budget >= 0 {
		lowBudget = min(lowBudget, budget)
	}
	for _, key := range []string{"thinking", "thinkingConfig", "thinking_config", "thinkingLevel", "thinking_level", "thinkingBudget", "thinking_budget"} {
		delete(opts, key)
	}
	opts["thinking"] = map[string]any{"level": compactReasoningEffort(level), "budget": lowBudget, "include_thoughts": false}
}

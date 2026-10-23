package engine

import (
	"context"
	"fmt"
	"strings"

	"github.com/teatak/pudding-core/internal/contextbuilder"
	"github.com/teatak/pudding-core/internal/provider"
	"github.com/teatak/pudding-core/internal/store"
)

// Send the complete effective history once. The retained messages provide
// context for the summary, including corrections after the replaced messages.
// Oversized inputs fail before calling the provider; never split or truncate.
func (e *Engine) summarizeCompactHistory(ctx context.Context, sessionID string, resolved *resolvedModel, client provider.Client, effective, source []*store.Message, hint string, target int) (string, error) {
	history, err := compactHistoryText(effective, source)
	if err != nil {
		return "", err
	}
	req, err := compactSummaryRequest(resolved, history, hint, target)
	if err != nil {
		return "", err
	}
	return e.generateCompactSummary(ctx, sessionID, resolved, client, req)
}

func compactHistoryText(effective, source []*store.Message) (string, error) {
	sourceIDs := make(map[string]bool, len(source))
	for _, message := range source {
		sourceIDs[message.ID] = true
	}
	var history strings.Builder
	hasSource := false
	for _, message := range effective {
		text := compactMessageText(message)
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

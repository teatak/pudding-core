package engine

import (
	"context"
	"errors"
	"fmt"
	"log/slog"

	"github.com/teatak/pudding-core/internal/contextbuilder"
	"github.com/teatak/pudding-core/internal/provider"
	"github.com/teatak/pudding-core/internal/provider/anthropic"
	"github.com/teatak/pudding-core/internal/store"
)

// Reserve output plus estimation headroom. An unknown window has no inferred
// hard limit; provider-reported usage still calibrates estimates when available.
func contextInputLimit(cfg provider.ModelConfig, protocol string) int {
	if cfg.ContextWindow <= 0 {
		return 0
	}
	output, ok := cfg.MaxOutputTokens()
	if !ok {
		switch protocol {
		case "anthropic":
			output = anthropic.DefaultMaxTokens
			if n, found := provider.IntOption(cfg.AnthropicOptions(), "max_tokens", "max_output_tokens"); found {
				output = n
			}
		case "google":
			output, _ = provider.IntOption(cfg.GoogleOptions(), "maxOutputTokens", "max_output_tokens", "max_tokens")
		case "openai-responses":
			output, _ = provider.IntOption(cfg.OpenAIOptions(), "max_output_tokens", "max_completion_tokens", "max_tokens")
		default:
			output, _ = provider.IntOption(cfg.OpenAIOptions(), "max_completion_tokens", "max_output_tokens", "max_tokens")
		}
	}
	if output <= 0 {
		output = min(8192, max(1, cfg.ContextWindow/5))
	}
	return max(1, cfg.ContextWindow-output-max(1, cfg.ContextWindow/20))
}

func compactTrigger(cfg provider.ModelConfig, protocol string, percent int) int {
	if cfg.ContextWindow <= 0 || percent <= 0 {
		return 0
	}
	return min(contextInputLimit(cfg, protocol), max(1, cfg.ContextWindow*percent/100))
}

// compactBeforeRequest covers queued turns as well as tool-loop growth. The
// engine calls it only after committing the preceding complete tool exchange.
func (e *Engine) compactBeforeRequest(ctx context.Context, sessionID, turnID string, resolved *resolvedModel, mode store.AgentMode, req provider.Request) (bool, error) {
	hard := contextInputLimit(req.Config, resolved.protocol)
	if hard == 0 {
		return false, nil
	}
	stat, err := e.store.SessionUsage(ctx, sessionID)
	if err != nil {
		return false, err
	}
	factor, _ := sessionInputCalibrationFactor(stat, resolved.providerName, resolved.model)
	tokens := calibratedTokenEstimate(contextbuilder.EstimateRequest(req).Total(), factor)
	soft := compactTrigger(req.Config, resolved.protocol, e.autoCompactThresholdPercent(ctx))
	if (soft == 0 || tokens < soft) && tokens <= hard {
		return false, nil
	}
	if soft > 0 {
		compactCtx, cancel := context.WithTimeout(ctx, autoCompactTimeout)
		_, err = e.compact(compactCtx, CompactInput{SessionID: sessionID}, resolved, mode, turnID, true)
		cancel()
		if err == nil {
			return true, nil
		}
		if errors.Is(err, store.ErrHistoryChanged) || ctx.Err() != nil {
			return false, err
		}
		if tokens <= hard {
			slog.Warn("engine: pre-request compact skipped", "sessionID", sessionID, "err", err)
			return false, nil
		}
	}
	if err != nil {
		return false, fmt.Errorf("%w: estimated input %d, available %d; compact: %v", ErrContextBudget, tokens, hard, err)
	}
	return false, fmt.Errorf("%w: estimated input %d, available %d", ErrContextBudget, tokens, hard)
}

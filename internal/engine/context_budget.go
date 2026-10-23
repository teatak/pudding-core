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

// Input may use the window up to the last token, which must remain for output.
// The model's maximum output capability does not reserve input capacity.
func contextInputLimit(cfg provider.ModelConfig) int {
	return max(0, cfg.ContextWindow-1)
}

func compactTrigger(cfg provider.ModelConfig, percent int) int {
	if cfg.ContextWindow <= 0 || percent <= 0 {
		return 0
	}
	return max(1, cfg.ContextWindow*percent/100)
}

// Apply a per-request limit without changing the model profile or a shared
// config snapshot. Unknown windows retain the provider's configured behavior.
func limitRequestOutput(req *provider.Request, protocol string, inputTokens int) error {
	cfg := req.Config
	if cfg.ContextWindow <= 0 {
		return nil
	}
	remaining := cfg.ContextWindow - inputTokens
	if remaining <= 0 {
		return fmt.Errorf("%w: estimated input %d leaves no output space in window %d", ErrContextBudget, inputTokens, cfg.ContextWindow)
	}
	output := configuredOutputLimit(cfg, protocol)
	// Optional provider limits remain omitted when the user has not set one.
	// Anthropic's required max_tokens is supplied by configuredOutputLimit.
	if output <= 0 {
		return nil
	}
	output = min(output, remaining)
	limits := provider.ModelLimits{}
	if cfg.Limits != nil {
		limits = *cfg.Limits
	}
	limits.MaxOutputTokens = output
	req.Config.Limits = &limits
	return nil
}

func configuredOutputLimit(cfg provider.ModelConfig, protocol string) int {
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
	return output
}

// prepareContextRequest covers queued turns as well as tool-loop growth. The
// engine calls it only after committing the preceding complete tool exchange.
func (e *Engine) prepareContextRequest(ctx context.Context, sessionID, turnID string, resolved *resolvedModel, mode store.AgentMode, req *provider.Request, allowCooling bool) (bool, error) {
	if req.Config.ContextWindow <= 0 {
		return false, nil
	}
	hard := contextInputLimit(req.Config)
	stat, err := e.store.SessionUsage(ctx, sessionID)
	if err != nil {
		return false, err
	}
	factor, _ := sessionInputCalibrationFactor(stat, resolved.providerName, resolved.model)
	estimate, err := contextbuilder.EstimateRequest(*req, resolved.protocol)
	if err != nil {
		return false, err
	}
	tokens := calibratedTokenEstimate(estimate.Total(), factor)
	soft := compactTrigger(req.Config, e.autoCompactThresholdPercent(ctx))
	summaryPressure := false
	if soft > 0 {
		summaryPressure, err = e.cooledSummaryPressure(ctx, sessionID, resolved, factor)
		if err != nil {
			return false, err
		}
	}
	if soft > 0 && tokens >= soft && !summaryPressure && allowCooling {
		cooled, coolingErr := e.tryCoolContext(ctx, sessionID, turnID, resolved, mode, req, tokens, soft, factor)
		if coolingErr != nil || cooled {
			return cooled, coolingErr
		}
	}
	if soft > 0 && (tokens >= soft || summaryPressure) {
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
		}
	}
	if tokens > hard {
		if err != nil {
			return false, fmt.Errorf("%w: estimated input %d, available %d; compact: %v", ErrContextBudget, tokens, hard, err)
		}
		return false, fmt.Errorf("%w: estimated input %d, available %d", ErrContextBudget, tokens, hard)
	}
	return false, limitRequestOutput(req, resolved.protocol, tokens)
}

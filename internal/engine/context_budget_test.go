package engine

import (
	"testing"

	"github.com/teatak/pudding-core/internal/provider"
)

func TestContextBudgetReservesProviderOutputLimit(t *testing.T) {
	cfg := provider.ModelConfig{ContextWindow: 128000}
	if got := contextInputLimit(cfg, "anthropic"); got != 57600 {
		t.Fatalf("must reserve Anthropic's actual default 64000 output tokens: %d", got)
	}
	cfg.ProviderOptions = &provider.ModelProviderOptions{
		OpenAI:    map[string]any{"max_completion_tokens": 32000, "max_output_tokens": 3000},
		Anthropic: map[string]any{"max_tokens": 16000},
	}
	if got := contextInputLimit(cfg, "openai-compatible"); got != 89600 {
		t.Fatalf("OpenAI output option: %d", got)
	}
	if got := contextInputLimit(cfg, "anthropic"); got != 105600 {
		t.Fatalf("Anthropic output option: %d", got)
	}
	if got := contextInputLimit(cfg, "openai-responses"); got != 118600 {
		t.Fatalf("Responses must match its adapter's option precedence: %d", got)
	}
	cfg.Limits = &provider.ModelLimits{MaxOutputTokens: 8000}
	if got := compactTrigger(cfg, "anthropic", 95); got != 113600 {
		t.Fatalf("neutral output limit must take precedence and cap the trigger: %d", got)
	}
	if compactTrigger(cfg, "anthropic", 0) != 0 || contextInputLimit(provider.ModelConfig{}, "anthropic") != 0 {
		t.Fatal("disabled trigger or unknown window must stay disabled")
	}
}

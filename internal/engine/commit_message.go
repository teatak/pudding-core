package engine

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/teatak/pudding-core/internal/contextbuilder"
	"github.com/teatak/pudding-core/internal/provider"
)

const commitMessageSystemPrompt = `You generate concise, high-quality Git commit messages based on provided git diffs and status.

Requirements:
- Follow the Conventional Commits specification (e.g. feat:, fix:, refactor:, chore:, docs:, style:, test:).
- When applicable, include a scope in parentheses, e.g. feat(widgets): or fix(git):.
- The description must be concise, accurate, and summarize the intent and key impact of the changes.
- Write in the dominant language of the changes or comments. If Chinese is used, write the description in Simplified Chinese; otherwise write in English.
- Do not wrap the message in code blocks, quotes, or markdown backticks.
- Output ONLY the commit message header (and optional brief bullet points if there are multiple substantial changes), nothing else.`

const commitMessageUserTemplate = `<git_diff>
%s
</git_diff>

Return only the commit message.`

const (
	commitMessageTimeout         = 45 * time.Second
	commitMessageMaxOutputTokens = 512
	commitMessageMaxDiffBytes    = 64 * 1024
)

func (e *Engine) GenerateCommitMessage(ctx context.Context, sessionID, diffText string) (string, error) {
	sess, err := e.store.GetSession(ctx, sessionID)
	if err != nil {
		return "", err
	}
	resolved, err := e.resolveModel(ctx, sess)
	if err != nil {
		return "", err
	}
	client, err := e.resolver.Resolve(ctx, resolved.providerName)
	if err != nil {
		return "", fmt.Errorf("%w: %v", ErrProviderConfig, err)
	}

	trimmedDiff := strings.TrimSpace(diffText)
	if len(trimmedDiff) > commitMessageMaxDiffBytes {
		trimmedDiff = trimmedDiff[:commitMessageMaxDiffBytes] + "\n\n[diff truncated]"
	}

	genCtx, cancel := context.WithTimeout(ctx, commitMessageTimeout)
	defer cancel()

	cfg := commitMessageConfig(resolved.config)
	req := provider.Request{
		Model:  resolved.model,
		System: commitMessageSystemPrompt,
		Messages: []provider.Message{
			{Role: provider.RoleUser, Text: fmt.Sprintf(commitMessageUserTemplate, trimmedDiff)},
		},
		Config: cfg,
	}

	estimate, err := contextbuilder.EstimateRequest(req, resolved.protocol)
	if err == nil {
		_ = limitRequestOutput(&req, resolved.protocol, estimate.Total())
	}

	ch, err := client.Stream(genCtx, req)
	if err != nil {
		return "", err
	}

	var buf strings.Builder
	for chunk := range ch {
		if chunk.Err != nil {
			return "", chunk.Err
		}
		if chunk.Part == "" || chunk.Part == provider.PartText {
			buf.WriteString(chunk.Delta)
		}
	}

	return cleanCommitMessage(buf.String()), nil
}

func commitMessageConfig(base provider.ModelConfig) provider.ModelConfig {
	cfg := provider.ModelConfig{
		ContextWindow: base.ContextWindow,
	}
	if base.Capabilities != nil {
		cfg.Capabilities = &provider.ModelCapabilities{
			Image: false,
			Audio: false,
			Tools: false,
		}
	}
	cfg.Limits = &provider.ModelLimits{MaxOutputTokens: commitMessageMaxOutputTokens}
	cfg.ProviderOptions = &provider.ModelProviderOptions{
		OpenAI: map[string]any{"reasoning_effort": "low"},
	}
	return cfg
}

func cleanCommitMessage(raw string) string {
	s := strings.TrimSpace(raw)
	// Remove outer markdown code block if present
	if strings.HasPrefix(s, "```") {
		lines := strings.Split(s, "\n")
		if len(lines) >= 2 && strings.HasPrefix(lines[0], "```") {
			lines = lines[1:]
			if len(lines) > 0 && strings.HasPrefix(lines[len(lines)-1], "```") {
				lines = lines[:len(lines)-1]
			}
			s = strings.TrimSpace(strings.Join(lines, "\n"))
		}
	}
	// Trim outer quotation marks
	s = strings.Trim(s, "\"'`“”‘’")
	return strings.TrimSpace(s)
}

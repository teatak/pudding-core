package provider

import (
	"regexp"
	"strings"
)

// Match versioned Mimo reasoning models, including vendor-qualified IDs, but
// not unrelated aliases or the ASR/TTS families.
var mimoReasoningModel = regexp.MustCompile(`(?i)^(?:xiaomi/|xiaomimimo/)?mimo-v[0-9]+(?:\.[0-9]+)*(?:-(?:flash|pro)(?:-ultraspeed)?)?$`)

// WireReasoningEffort translates the five product choices only when building a
// provider request. Keep session preferences and model configuration unchanged.
// Missing levels map to the next supported level above them, capped at the
// model's maximum. This is product policy, not an upstream alias guarantee.
// Unlisted models/families and non-product values pass through; provider brands
// do not determine capabilities. Mimo family verified 2026-09-22.
func WireReasoningEffort(protocol, model, effort string) string {
	if protocol == "google" {
		// https://ai.google.dev/gemini-api/docs/generate-content/thinking
		switch strings.ToLower(strings.TrimSpace(effort)) {
		case "xhigh", "max":
			return "high"
		}
		return effort
	}
	// https://mimo.mi.com/docs/en-US/api/chat/responses
	// Live Chat/Responses probes rejected xhigh and accepted high.
	if mimoReasoningModel.MatchString(model) && (effort == "xhigh" || effort == "max") {
		return "high"
	}
	switch model {
	// Gemini also accepts reasoning_effort through OpenAI-compatible endpoints.
	// https://ai.google.dev/gemini-api/docs/openai#thinking
	case "gemini-3.8-flash", "gemini-3.5-flash-lite", "gemini-3.1-pro-preview",
		"gemini-3-flash-preview", "gemini-3.1-flash-lite-preview",
		"gemini-2.5-pro", "gemini-2.5-flash", "gemini-2.5-flash-lite":
		if effort == "xhigh" || effort == "max" {
			return "high"
		}
	// https://platform.kimi.com/docs/guide/kimi-k3-quickstart
	// https://github.com/zai-org/GLM-5/blob/main/README_zh.md#注意事项
	case "kimi-k3", "glm-5.3", "glm-5.3-flash":
		if effort == "medium" {
			return "high"
		}
		if effort == "xhigh" {
			return "max"
		}
	case "glm-5.2":
		if effort == "low" || effort == "medium" {
			return "high"
		}
		if effort == "xhigh" {
			return "max"
		}
	// https://developers.openai.com/api/docs/models/gpt-5.5
	// https://developers.openai.com/api/docs/models/gpt-5.4
	// https://developers.openai.com/api/docs/models/gpt-5.4-mini
	// https://developers.openai.com/api/docs/models/gpt-5.4-nano
	case "gpt-5.5", "gpt-5.5-2026-04-23",
		"gpt-5.4", "gpt-5.4-2026-03-05",
		"gpt-5.4-mini", "gpt-5.4-mini-2026-03-17",
		"gpt-5.4-nano", "gpt-5.4-nano-2026-03-17":
		if effort == "max" {
			return "xhigh"
		}
	// https://platform.claude.com/docs/en/build-with-claude/effort
	case "claude-opus-4-5", "claude-opus-4-5-20251101":
		if effort == "xhigh" || effort == "max" {
			return "high"
		}
	case "claude-opus-4-6", "claude-sonnet-4-6", "claude-mythos-preview":
		if effort == "xhigh" {
			return "max"
		}
	}
	// DeepSeek already maps aliases upstream; GPT-6 and newer Claude models
	// accept all five levels. Preserve their requested effort.
	return effort
}

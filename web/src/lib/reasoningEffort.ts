import type { ResolvedModelSelection } from "@/lib/modelSelection";

export const STANDARD_REASONING_EFFORT_OPTIONS = ["low", "medium", "high", "xhigh", "max"] as const;
export const GOOGLE_REASONING_EFFORT_OPTIONS = ["low", "medium", "high"] as const;

const ANTHROPIC_BASE_EFFORT_OPTIONS = ["low", "medium", "high"] as const;
const ANTHROPIC_MAX_EFFORT_OPTIONS = [...ANTHROPIC_BASE_EFFORT_OPTIONS, "max"] as const;

// Verified 2026-09-17 against Anthropic's effort compatibility and model ID docs:
// https://platform.claude.com/docs/en/build-with-claude/effort
// https://platform.claude.com/docs/en/about-claude/models/model-ids-and-versions
// Exact IDs only: the Anthropic wire protocol does not imply effort support.
const ANTHROPIC_EFFORT_OPTIONS = new Map<string, readonly string[]>([
  ["claude-opus-4-5", ANTHROPIC_BASE_EFFORT_OPTIONS],
  ["claude-opus-4-5-20251101", ANTHROPIC_BASE_EFFORT_OPTIONS],
  ["claude-opus-4-6", ANTHROPIC_MAX_EFFORT_OPTIONS],
  ["claude-sonnet-4-6", ANTHROPIC_MAX_EFFORT_OPTIONS],
  ["claude-opus-4-7", STANDARD_REASONING_EFFORT_OPTIONS],
  ["claude-opus-4-8", STANDARD_REASONING_EFFORT_OPTIONS],
  ["claude-opus-5", STANDARD_REASONING_EFFORT_OPTIONS],
  ["claude-sonnet-5", STANDARD_REASONING_EFFORT_OPTIONS],
  ["claude-fable-5", STANDARD_REASONING_EFFORT_OPTIONS],
  ["claude-fable-5-1", STANDARD_REASONING_EFFORT_OPTIONS],
  ["claude-mythos-5", STANDARD_REASONING_EFFORT_OPTIONS],
  ["claude-mythos-5-1", STANDARD_REASONING_EFFORT_OPTIONS],
  ["claude-mythos-preview", ANTHROPIC_MAX_EFFORT_OPTIONS],
]);

export function reasoningEffortOptionsForSelection(selection: ResolvedModelSelection | null): string[] {
  if (!selection) {
    return [];
  }
  if (selection.providerProtocol === "anthropic") {
    return [...(ANTHROPIC_EFFORT_OPTIONS.get(selection.model) || [])];
  }
  if (supportsStandardReasoning(selection)) {
    return [...STANDARD_REASONING_EFFORT_OPTIONS];
  }
  if (supportsGoogleThinking(selection)) {
    return [...GOOGLE_REASONING_EFFORT_OPTIONS];
  }
  return [];
}

export function recommendedReasoningEffortForSelection(selection: ResolvedModelSelection | null): string {
  const options = reasoningEffortOptionsForSelection(selection);
  if (!selection || options.length === 0) {
    return "";
  }
  const configured = defaultReasoningEffortForSelection(selection);
  if (configured && options.includes(configured)) {
    return configured;
  }
  const isDeepSeek =
    selection.providerBrand === "deepseek" ||
    selection.model.toLowerCase().includes("deepseek");
  if (isDeepSeek && options.includes("high")) {
    return "high";
  }
  return options.includes("medium") ? "medium" : options[0];
}

export function resolveReasoningEffortForSelection(
  selection: ResolvedModelSelection | null,
  preferredValue = "",
): string {
  const options = reasoningEffortOptionsForSelection(selection);
  return options.includes(preferredValue)
    ? preferredValue
    : recommendedReasoningEffortForSelection(selection);
}

export function defaultReasoningEffortForSelection(selection: ResolvedModelSelection | null) {
  if (!selection) {
    return undefined;
  }
  if (selection.providerProtocol === "anthropic") {
    return anthropicEffort(selection.modelConfig?.providerOptions?.anthropic);
  }
  if (supportsStandardReasoning(selection)) {
    const value = selection.modelConfig?.providerOptions?.openai?.reasoning_effort;
    return typeof value === "string" ? value : undefined;
  }
  if (supportsGoogleThinking(selection)) {
    const thinking = selection.modelConfig?.providerOptions?.google?.thinking;
    if (!thinking || typeof thinking !== "object" || Array.isArray(thinking)) {
      return undefined;
    }
    const value = (thinking as Record<string, unknown>).level;
    return typeof value === "string" ? value : undefined;
  }
  return undefined;
}

function supportsStandardReasoning(selection: ResolvedModelSelection) {
  return selection.providerProtocol === "openai-compatible"
    || selection.providerProtocol === "openai-responses";
}

function anthropicEffort(options: Record<string, unknown> | undefined) {
  const outputConfig = options?.output_config;
  if (!outputConfig || typeof outputConfig !== "object" || Array.isArray(outputConfig)) {
    return undefined;
  }
  const effort = (outputConfig as Record<string, unknown>).effort;
  return typeof effort === "string" ? effort : undefined;
}

function supportsGoogleThinking(selection: ResolvedModelSelection) {
  return selection.providerProtocol === "google" || selection.modelConfig?.providerOptions?.google?.thinking !== undefined;
}

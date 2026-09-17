import type { ResolvedModelSelection } from "./modelSelection";

const STANDARD_EFFORTS = ["low", "medium", "high", "xhigh", "max"];
const OPENAI_EFFORTS = ["none", "minimal", ...STANDARD_EFFORTS];
const GOOGLE_EFFORTS = ["low", "medium", "high"];

// These are the overrides accepted by the daemon's protocol adapters.
// "auto" is a UI choice; the API represents inheritance with an empty string.
export function normalizeReasoningEffort(value: string) {
  const effort = value.trim();
  return effort === "auto" ? "" : effort;
}

export function reasoningEffortOptionsForSelection(selection: ResolvedModelSelection | null): string[] {
  switch (selection?.providerProtocol) {
    case "openai-compatible":
    case "openai-responses":
      return [...OPENAI_EFFORTS];
    case "anthropic":
      return [...STANDARD_EFFORTS];
    case "google":
      return [...GOOGLE_EFFORTS];
    default:
      return [];
  }
}

export function defaultReasoningEffortForSelection(selection: ResolvedModelSelection | null): string | undefined {
  const options = selection?.modelConfig?.providerOptions;
  let value: unknown;
  switch (selection?.providerProtocol) {
    case "openai-compatible":
    case "openai-responses":
      value = options?.openai?.reasoning_effort;
      break;
    case "anthropic": {
      const config = options?.anthropic?.output_config;
      if (config && typeof config === "object" && !Array.isArray(config)) {
        value = (config as Record<string, unknown>).effort;
      }
      break;
    }
    case "google": {
      const thinking = options?.google?.thinking;
      if (thinking && typeof thinking === "object" && !Array.isArray(thinking)) {
        // An explicit token budget cannot be represented as a known slider level.
        for (const config of [thinking, options?.google]) {
          if (["budget", "thinkingBudget", "thinking_budget"].some((key) =>
            (config as Record<string, unknown> | undefined)?.[key] !== undefined)) return undefined;
        }
        value = (thinking as Record<string, unknown>).level;
      }
      break;
    }
  }
  const effort = typeof value === "string" ? value.trim() : "";
  if (!effort) return undefined;
  if (selection?.providerProtocol === "google") {
    const level = effort.toLowerCase();
    return ["minimal", ...GOOGLE_EFFORTS].includes(level) ? level : undefined;
  }
  // OpenAI/Anthropic config is normalized by the daemon before each request.
  if (!reasoningEffortOptionsForSelection(selection).includes(effort)) return undefined;
  return effort;
}

export function resolveReasoningEffort(selection: ResolvedModelSelection | null, value: string) {
  const efforts = reasoningEffortOptionsForSelection(selection);
  const configured = defaultReasoningEffortForSelection(selection);
  const requested = normalizeReasoningEffort(value);
  // The daemon ignores unsupported overrides and keeps the configured value.
  const override = efforts.includes(requested) ? requested : "";
  const recommended = configured && efforts.includes(configured)
    ? configured
    : selection?.providerBrand === "deepseek" || selection?.model.toLowerCase().includes("deepseek")
      ? "high"
      : "medium";

  return {
    options: efforts.length ? ["auto", ...efforts] : [],
    value: override || "auto",
    override,
    effective: override || configured,
    inherited: !override,
    recommended,
  };
}

import assert from "node:assert/strict";
import { test } from "node:test";
import type { ResolvedModelSelection } from "../src/lib/modelSelection.ts";
import {
  defaultReasoningEffortForSelection,
  reasoningEffortOptionsForSelection,
  recommendedReasoningEffortForSelection,
  resolveReasoningEffortForSelection,
} from "../src/lib/reasoningEffort.ts";

function selection(
  model: string,
  protocol: ResolvedModelSelection["providerProtocol"] = "anthropic",
  providerOptions?: NonNullable<ResolvedModelSelection["modelConfig"]>["providerOptions"],
): ResolvedModelSelection {
  return { provider: "fixture", model, providerProtocol: protocol, modelConfig: { id: model, providerOptions } };
}

test("Haiku and older Claude models do not get a synthesized effort parameter", () => {
  for (const model of ["claude-haiku-4-5", "claude-haiku-4-5-20251001", "claude-sonnet-4-5", "claude-opus-4"]) {
    const value = selection(model);
    assert.deepEqual(reasoningEffortOptionsForSelection(value), [], model);
    assert.equal(recommendedReasoningEffortForSelection(value), "", model);
  }
  assert.deepEqual(reasoningEffortOptionsForSelection(null), []);
  assert.equal(recommendedReasoningEffortForSelection(null), "");
});

test("Anthropic effort levels follow the selected model, including the pinned Opus 4.5 snapshot", () => {
  for (const model of ["claude-opus-4-5", "claude-opus-4-5-20251101"]) {
    assert.deepEqual(reasoningEffortOptionsForSelection(selection(model)), ["low", "medium", "high"]);
  }
  for (const model of ["claude-opus-4-6", "claude-sonnet-4-6", "claude-mythos-preview"]) {
    assert.deepEqual(reasoningEffortOptionsForSelection(selection(model)), ["low", "medium", "high", "max"]);
  }
  for (const model of ["claude-opus-4-7", "claude-opus-4-8", "claude-opus-5", "claude-sonnet-5", "claude-fable-5", "claude-fable-5-1", "claude-mythos-5", "claude-mythos-5-1"]) {
    assert.deepEqual(reasoningEffortOptionsForSelection(selection(model)), ["low", "medium", "high", "xhigh", "max"]);
  }
});

test("model family names, compatible transports, and configured effort do not assert support", () => {
  for (const model of ["custom-model", "deepseek-flash", "claude-opus-6", "claude-sonnet-5-custom", "claude-opus-5-20990101"]) {
    const value = { ...selection(model, "anthropic", { anthropic: { output_config: { effort: "high" } } }), providerBrand: "anthropic" };
    const before = structuredClone(value);
    assert.deepEqual(reasoningEffortOptionsForSelection(value), [], model);
    assert.equal(recommendedReasoningEffortForSelection(value), "", model);
    assert.deepEqual(value, before, "leave raw provider options to the provider adapter");
  }
});

test("configured recommendations must belong to the model's actual effort levels", () => {
  const configured = selection("claude-opus-4-6", "anthropic", { anthropic: { output_config: { effort: "max" } } });
  assert.equal(defaultReasoningEffortForSelection(configured), "max");
  assert.equal(recommendedReasoningEffortForSelection(configured), "max");
  const unavailable = selection("claude-opus-4-5", "anthropic", { anthropic: { output_config: { effort: "max" } } });
  assert.equal(recommendedReasoningEffortForSelection(unavailable), "medium");
  const haiku = selection("claude-haiku-4-5", "anthropic", { anthropic: { output_config: { effort: "medium" } } });
  assert.equal(recommendedReasoningEffortForSelection(haiku), "");
});

test("OpenAI protocols retain their concrete options and configured recommendations", () => {
  for (const protocol of ["openai-compatible", "openai-responses"] as const) {
    const value = selection("custom-model", protocol, { openai: { reasoning_effort: "xhigh" } });
    assert.deepEqual(reasoningEffortOptionsForSelection(value), ["low", "medium", "high", "xhigh", "max"]);
    assert.equal(recommendedReasoningEffortForSelection(value), "xhigh");
    assert.equal(recommendedReasoningEffortForSelection(selection("deepseek-flash", protocol)), "high");
    assert.equal(recommendedReasoningEffortForSelection({ ...selection("custom-model", protocol), providerBrand: "deepseek" }), "high");
    for (const removedValue of ["none", "auto", "minimal"]) {
      assert.equal(recommendedReasoningEffortForSelection(selection("custom-model", protocol, { openai: { reasoning_effort: removedValue } })), "medium");
    }
  }
});

test("Google retains three levels and reads the configured thinking level", () => {
  const value = selection("gemini-custom", "google", { google: { thinking: { level: "low", include_thoughts: true } } });
  assert.deepEqual(reasoningEffortOptionsForSelection(value), ["low", "medium", "high"]);
  assert.equal(defaultReasoningEffortForSelection(value), "low");
  assert.equal(recommendedReasoningEffortForSelection(value), "low");
  assert.equal(recommendedReasoningEffortForSelection(selection("gemini-custom", "google")), "medium");
});

test("UI and submissions resolve saved effort against the target model's capabilities", () => {
  const configured = selection("claude-opus-4-6", "anthropic", { anthropic: { output_config: { effort: "max" } } });
  assert.equal(resolveReasoningEffortForSelection(configured, "low"), "low");
  assert.equal(resolveReasoningEffortForSelection(configured, "xhigh"), "max");
  assert.equal(resolveReasoningEffortForSelection(configured), "max");
  assert.equal(resolveReasoningEffortForSelection(selection("gemini-custom", "google"), "max"), "medium");
  for (const value of [selection("claude-haiku-4-5"), selection("custom-model"), null]) {
    assert.equal(resolveReasoningEffortForSelection(value, "medium"), "", "an old override cannot establish effort support");
  }
  for (const removedValue of ["none", "auto", ""]) {
    assert.equal(resolveReasoningEffortForSelection(selection("claude-opus-5"), removedValue), "medium");
  }
});

test("no concrete effort list exposes the removed none or automatic choices", () => {
  for (const value of [selection("claude-opus-5"), selection("claude-sonnet-4-6"), selection("claude-opus-4-5"), selection("custom-model", "openai-compatible"), selection("custom-model", "google")]) {
    const options = reasoningEffortOptionsForSelection(value);
    assert.equal(options.some((option) => ["", "none", "auto"].includes(option)), false);
    options.length = 0;
    assert.ok(reasoningEffortOptionsForSelection(value).length > 0, "callers cannot mutate the shared capability list");
  }
});

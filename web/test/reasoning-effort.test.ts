import assert from "node:assert/strict";
import { test } from "node:test";
import type { ProviderProfile } from "../src/api/client.ts";
import { modelSelectionKey, resolveModelSelection, type ResolvedModelSelection } from "../src/lib/modelSelection.ts";
import { normalizeReasoningEffort, resolveReasoningEffort } from "../src/lib/reasoningEffort.ts";

function model(protocol: ResolvedModelSelection["providerProtocol"], options = {}): ResolvedModelSelection {
  return {
    provider: "test-provider", model: "test-model", providerProtocol: protocol,
    modelConfig: { id: "test-model", providerOptions: options },
  };
}

test("inheritance displays the configured OpenAI value without creating an override", () => {
  for (const effort of ["none", "minimal", "low", "medium", "high", "xhigh", "max"]) {
    const selection = model("openai-responses", { openai: { reasoning_effort: effort } });
    const state = resolveReasoningEffort(selection, "");
    assert.equal(state.value, "auto");
    assert.equal(state.override, "");
    assert.equal(state.effective, effort);
    assert.equal(state.inherited, true);
    assert.equal(state.recommended, effort);
    assert.notEqual(state.value, state.recommended, "applying a recommendation remains an explicit action");
  }
});

test("unknown provider defaults remain unknown instead of displaying the recommendation as effective", () => {
  const state = resolveReasoningEffort(model("openai-compatible"), "");
  assert.equal(state.effective, undefined);
  assert.equal(state.override, "");
  assert.equal(state.value, "auto");
  assert.equal(state.recommended, "medium");
});

test("an explicit value wins; clearing it immediately restores the latest provider configuration", () => {
  const selection = model("openai-responses", { openai: { reasoning_effort: "none" } });
  assert.equal(resolveReasoningEffort(selection, "high").effective, "high");
  assert.equal(resolveReasoningEffort(selection, "high").inherited, false);
  assert.equal(resolveReasoningEffort(selection, "auto").effective, "none");
  const changed = model("openai-responses", { openai: { reasoning_effort: "minimal" } });
  assert.equal(resolveReasoningEffort(changed, "").effective, "minimal");
  assert.equal(resolveReasoningEffort(changed, "high").effective, "high");
});

test("auto is a UI sentinel and never an API override", () => {
  assert.equal(normalizeReasoningEffort("auto"), "");
  assert.equal(normalizeReasoningEffort(" auto "), "");
  assert.equal(normalizeReasoningEffort(""), "");
  assert.equal(normalizeReasoningEffort(" high "), "high");
});

test("configured values removed by the daemon are not displayed as effective", () => {
  for (const effort of ["auto", "turbo", " "]) {
    for (const selection of [
      model("openai-responses", { openai: { reasoning_effort: effort } }),
      model("anthropic", { anthropic: { output_config: { effort } } }),
    ]) {
      assert.equal(resolveReasoningEffort(selection, "").effective, undefined);
      assert.equal(resolveReasoningEffort(selection, "").value, "auto");
    }
  }
});

test("protocol configuration is read independently of selectable overrides", () => {
  const google = model("google", { google: { thinking: { level: "minimal" } } });
  const inherited = resolveReasoningEffort(google, "");
  assert.equal(inherited.effective, "minimal");
  assert.equal(inherited.value, "auto");
  assert.equal(inherited.options.includes("minimal"), false, "the Google adapter does not accept a minimal override");
  assert.equal(resolveReasoningEffort(google, "xhigh").override, "");
  assert.equal(resolveReasoningEffort(model("google", { google: { thinking: { level: "HIGH" } } }), "").effective, "high");
  assert.equal(resolveReasoningEffort(model("google", { google: { thinking: { level: "turbo" } } }), "").effective, undefined);
  assert.equal(resolveReasoningEffort(model("google", { google: { thinking: { level: "high", budget: 128 } } }), "").effective, undefined);
  const anthropic = model("anthropic", {
    anthropic: { output_config: { effort: "max" } }, openai: { reasoning_effort: "low" },
  });
  assert.equal(resolveReasoningEffort(anthropic, "").effective, "max");
  assert.equal(resolveReasoningEffort(anthropic, "none").override, "");
  assert.equal(resolveReasoningEffort(anthropic, "high").effective, "high");
});

test("DeepSeek recommendation is separate from inheritance", () => {
  const selection = { ...model("openai-responses"), providerBrand: "deepseek" };
  const state = resolveReasoningEffort(selection, "");
  assert.equal(state.recommended, "high");
  assert.equal(state.effective, undefined);
  assert.equal(state.override, "");
  assert.equal(resolveReasoningEffort(selection, state.recommended).effective, "high");
});

test("missing metadata does not invent selectable or effective values", () => {
  assert.deepEqual(resolveReasoningEffort(null, "high").options, []);
  assert.equal(resolveReasoningEffort(null, "high").effective, undefined);
  assert.equal(resolveReasoningEffort(model(undefined), "high").override, "");
});

test("model metadata resolves by provider and model together, including same-named models", () => {
  const profiles: ProviderProfile[] = [
    { id: "a", displayName: "A", protocol: "openai-responses", baseURL: "https://example.invalid", apiKeySet: false,
      models: [{ id: "shared", providerOptions: { openai: { reasoning_effort: "none" } } }] },
    { id: "b", displayName: "B", protocol: "anthropic", baseURL: "https://example.invalid", apiKeySet: false,
      models: [{ id: "shared", providerOptions: { anthropic: { output_config: { effort: "high" } } } }] },
  ];
  const before = JSON.stringify(profiles);
  const a = resolveModelSelection(profiles, { provider: "a", model: "shared" });
  const b = resolveModelSelection(profiles, { provider: "b", model: "shared" });
  assert.equal(a?.providerProtocol, "openai-responses");
  assert.equal(b?.providerProtocol, "anthropic");
  assert.equal(resolveReasoningEffort(a, "").effective, "none");
  assert.equal(resolveReasoningEffort(b, "").effective, "high");
  assert.notEqual(modelSelectionKey(a!), modelSelectionKey(b!));
  assert.equal(resolveModelSelection(profiles, { provider: "missing", model: "shared" }), null);
  assert.equal(resolveModelSelection(profiles, { provider: "a", model: "missing" }), null);
  assert.equal(resolveModelSelection(profiles, { provider: "a" }), null);
  assert.equal(modelSelectionKey({ provider: "a" }), "");
  assert.equal(JSON.stringify(profiles), before, "resolving display state must not modify saved metadata");
});

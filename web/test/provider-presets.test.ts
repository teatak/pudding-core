import assert from "node:assert/strict";
import { after, test } from "node:test";
import { fileURLToPath } from "node:url";
import { createServer } from "vite";
import type { ProviderPreset, ProviderPresetProtocol } from "../src/provider/presets.ts";

const server = await createServer({
  root: fileURLToPath(new URL("..", import.meta.url)),
  optimizeDeps: { noDiscovery: true, include: [] },
  server: { middlewareMode: true, watch: null, hmr: false, ws: false },
});
after(() => server.close());
const { PROVIDER_PRESETS, mergeProviderModelCandidate, providerPresetVariantForSelection } = await server.ssrLoadModule("/src/provider/presets.ts");
const presets = PROVIDER_PRESETS as ProviderPreset[];

test("DeepSeek Responses selection and model import retain vision and normalized limits", () => {
  const preset = presets.find((item) => item.id === "deepseek")!;
  const variant = providerPresetVariantForSelection(preset, undefined, "openai-responses");
  assert.equal(variant?.protocol, "openai-responses");
  assert.equal(variant?.baseURL, "https://api.deepseek.com");
  const flash = mergeProviderModelCandidate({ id: " deepseek-flash " }, variant, "openai-responses");
  assert.equal(flash.capabilities.image, true);
  assert.equal(flash.limits.maxOutputTokens, 384_000);
  assert.equal(flash.contextWindow, 1_000_000);
  assert.equal(flash.providerOptions, undefined);
  for (const other of preset.variants) {
    assert.deepEqual(other.models, variant.models, "protocols share the same model metadata");
  }
  assert.equal(variant.models.find((item: { id: string }) => item.id === "deepseek-v4-pro")?.capabilities?.image, undefined);
});

test("Astra's tool-capable preset is restricted to Responses", () => {
  const preset = presets.find((item) => item.id === "openai")!;
  assert.equal(preset.variants.find((item) => item.protocol === "openai-responses")?.models.find((item) => item.id === "gpt-6-astra")?.capabilities?.tools, true);
  assert.equal(preset.variants.find((item) => item.protocol === "openai-compatible")?.models.some((item) => item.id === "gpt-6-astra"), false);
  assert.equal(mergeProviderModelCandidate({ id: "gpt-6-astra" }, undefined, "openai-compatible").capabilities.tools, false, "discovery must also respect Astra's protocol restriction");
});

test("presets and discovered candidates leave temperature to the provider", () => {
  for (const preset of presets) {
    for (const variant of preset.variants) {
      for (const model of variant.models) {
        for (const options of Object.values(model.providerOptions || {})) {
          assert.equal(Object.hasOwn(options!, "temperature"), false, `${preset.id}/${model.id}`);
          assert.equal(Object.hasOwn(options!, "max_tokens"), false, "output limits have one normalized source");
        }
      }
    }
  }
  for (const protocol of ["openai-compatible", "openai-responses", "anthropic", "google"] as ProviderPresetProtocol[]) {
    for (const id of ["custom-model", "claude-opus-5", "gemini-3.8-flash"]) {
      const imported = mergeProviderModelCandidate({ id }, undefined, protocol);
      assert.equal(JSON.stringify(imported).includes('"temperature"'), false, `${protocol}/${id}`);
      if (protocol === "google") {
        assert.deepEqual(imported.providerOptions, { google: { thinking: { include_thoughts: true } } });
      } else {
        assert.equal(imported.providerOptions, undefined, "do not import options from another protocol");
      }
    }
  }
});

test("multimodal and context metadata do not overstate text-only or smaller models", () => {
  const models = presets.flatMap((preset) => preset.variants.flatMap((variant) => variant.models));
  const get = (id: string) => models.find((model) => model.id === id)!;
  assert.equal(get("glm-5.3-flash").capabilities?.image, true);
  assert.equal(get("glm-5.2").capabilities?.image, false);
  assert.equal(get("glm-5.1").contextWindow, 200_000);
  assert.equal(get("kimi-k2.6").contextWindow, 262_144);
  assert.equal(get("kimi-k2.7-code").capabilities?.image, true);
  assert.equal(get("gpt-5.5").capabilities?.audio, undefined);
  assert.equal(get("gpt-5.4").capabilities?.audio, undefined);
  assert.equal(get("gemini-3.8-flash").contextWindow, 1_048_576);
  assert.equal(get("gemini-3.8-flash").limits?.maxOutputTokens, 65_536);
  assert.equal(models.some((model) => model.id === "gemini-3.1-pro" || model.id === "openrouter/owl-alpha"), false);
});


test("endpoint metadata overrides presets per field, preserving explicit false and protocol options", () => {
  const variant = presets.find((preset) => preset.id === "deepseek")!.variants[0];
  const original = structuredClone(variant.models);
  const imported = mergeProviderModelCandidate({
    id: " deepseek-flash ", displayName: "Gateway Flash", contextWindow: 32768,
    capabilities: { image: false, audio: true, tools: false }, limits: { maxOutputTokens: 4096 },
  }, variant, variant.protocol);
  assert.equal(imported.id, "deepseek-flash");
  assert.equal(imported.displayName, "Gateway Flash");
  assert.equal(imported.contextWindow, 32768);
  assert.deepEqual(imported.capabilities, { image: false, audio: true, tools: false });
  assert.equal(imported.limits.maxOutputTokens, 4096);
  assert.deepEqual(variant.models, original, "import must not mutate the shared preset");
  const partial = mergeProviderModelCandidate({ id: "deepseek-flash", capabilities: { image: false } }, undefined, "google");
  assert.equal(partial.contextWindow, 1_000_000);
  assert.equal(partial.limits.maxOutputTokens, 384_000);
  assert.equal(partial.capabilities.image, false);
  assert.equal(partial.capabilities.tools, true);
  assert.deepEqual(partial.providerOptions, { google: { thinking: { include_thoughts: true } } });
});

test("unknown model IDs retain endpoint metadata without family guesses", () => {
  const imported = mergeProviderModelCandidate({ id: "qwen3.6", contextWindow: 65536, limits: { maxOutputTokens: 8192 }, capabilities: { tools: false } }, undefined, "openai-compatible");
  assert.equal(imported.contextWindow, 65536);
  assert.equal(imported.limits.maxOutputTokens, 8192);
  assert.equal(imported.capabilities.tools, false);
  const unknown = mergeProviderModelCandidate({ id: "qwen3.6" }, undefined, "openai-compatible");
  assert.equal(unknown.contextWindow, undefined);
  assert.equal(unknown.limits, undefined);
  assert.equal(unknown.capabilities.image, undefined);
});

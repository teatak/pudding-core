import assert from "node:assert/strict";
import { after, test } from "node:test";
import { fileURLToPath } from "node:url";
import { createServer } from "vite";

const server = await createServer({
  root: fileURLToPath(new URL("..", import.meta.url)),
  optimizeDeps: { noDiscovery: true, include: [] },
  server: { middlewareMode: true, watch: null, hmr: false, ws: false },
});
after(() => server.close());
const { providerBrandForModel } = await server.ssrLoadModule("/src/provider/presets.ts");

test("中转预设中的模型优先显示模型厂商图标", () => {
  assert.equal(providerBrandForModel("deepseek-v4-pro"), "deepseek");
  assert.equal(providerBrandForModel("claude-sonnet-5"), "anthropic");
  assert.equal(providerBrandForModel("openrouter/free"), "openrouter");
  assert.equal(providerBrandForModel("deepseek/deepseek-v4.1-flash"), "deepseek");
  assert.equal(providerBrandForModel("openai/gpt-5.6-sol"), "openai");
  assert.equal(providerBrandForModel("anthropic/claude-sonnet-5"), "anthropic");
  assert.equal(providerBrandForModel("google/gemini-3.8-flash"), "gemini");
  assert.equal(providerBrandForModel("qwen/qwen3.8-flash"), "qwen");
  assert.equal(providerBrandForModel("moonshotai/kimi-k3"), "moonshot");
  assert.equal(providerBrandForModel(" OpenRouter/DeepSeek/DeepSeek-V4.1-Flash:free "), "deepseek");
});

test("别名、自建 id 与中转前缀按厂商关键字命中", () => {
  assert.equal(providerBrandForModel("deepseek-flash"), "deepseek");
  assert.equal(providerBrandForModel("deepseek-chat"), "deepseek");
  assert.equal(providerBrandForModel("deepseek-reasoner"), "deepseek");
  assert.equal(providerBrandForModel("qwen3-max"), "qwen");
  assert.equal(providerBrandForModel("glm-4.5-air"), "zhipu");
  assert.equal(providerBrandForModel("z-ai/glm-4.5-air:free"), "zhipu");
  assert.equal(providerBrandForModel("z-ai/glm-5"), "zhipu");
  assert.equal(providerBrandForModel("anthropic/claude-sonnet-4-6"), "anthropic");
  assert.equal(providerBrandForModel("openai/gpt-5.4"), "openai");
  assert.equal(providerBrandForModel("o3-mini"), "openai");
  assert.equal(providerBrandForModel("gemma-3-27b"), "gemini");
  assert.equal(providerBrandForModel("moonshot-v1-128k"), "moonshot");
  assert.equal(providerBrandForModel("mimo-v2.5-terra"), "mimo");
  assert.equal(providerBrandForModel("grok-4"), "grok");
});

test("判定不出厂商时交回调用方回退 profile 品牌", () => {
  assert.equal(providerBrandForModel("some-proxy/mystery-model"), undefined);
  assert.equal(providerBrandForModel("nvidia/nemotron-4-340b"), undefined);
  assert.equal(providerBrandForModel(""), undefined);
  assert.equal(providerBrandForModel(undefined), undefined);
});

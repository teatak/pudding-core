import assert from "node:assert/strict";
import { test } from "node:test";
import { createTestViteServer } from "./vite-test-server.ts";

const server = await createTestViteServer();
const { cleanModelDisplayName, formatModelLabel } = await server.ssrLoadModule("/src/lib/model.ts");

test("cleanModelDisplayName 剥离斜杠渠道前缀", () => {
  assert.equal(cleanModelDisplayName("OpenRouter / Free"), "Free");
  assert.equal(cleanModelDisplayName("Z Ai / GLM 5.3 Flash"), "GLM 5.3 Flash");
  assert.equal(cleanModelDisplayName("DeepSeek / DeepSeek V4.1 Flash"), "DeepSeek V4.1 Flash");
});

test("cleanModelDisplayName 剥离冒号厂商前缀与冗余后缀", () => {
  assert.equal(cleanModelDisplayName("Qwen: Qwen3.8 27B (free)"), "Qwen3.8 27B");
  assert.equal(cleanModelDisplayName("GLM 5.2 (Free)"), "GLM 5.2");
  assert.equal(cleanModelDisplayName("Z.ai: GLM 5.3 Flash"), "GLM 5.3 Flash");
  assert.equal(cleanModelDisplayName("Z.ai: GLM 5.3 Flash (batch)"), "GLM 5.3 Flash");
  assert.equal(cleanModelDisplayName("Meta: Llama 3.3 70B Instruct (free)"), "Llama 3.3 70B Instruct");
  assert.equal(cleanModelDisplayName("inclusionAI: Ling 3.0 Flash VL (free)"), "Ling 3.0 Flash VL");
});

test("cleanModelDisplayName 保留合法模型名与自定义中文命名", () => {
  assert.equal(cleanModelDisplayName("Free"), "Free");
  assert.equal(cleanModelDisplayName("DeepSeek Flash"), "DeepSeek Flash");
  assert.equal(cleanModelDisplayName("我的助手: 测试模型"), "我的助手: 测试模型");
});

test("formatModelLabel 未提供 displayName 时使用 tail id 格式化", () => {
  assert.equal(formatModelLabel("qwen/qwen3.8-27b:free", "Qwen: Qwen3.8 27B (free)"), "Qwen3.8 27B");
  assert.equal(formatModelLabel("deepseek-v4-pro"), "DeepSeek V4 Pro");
});

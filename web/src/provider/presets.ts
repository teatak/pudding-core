import type { ProviderModel } from "@/api/client";

export type ProviderPresetId =
  | "deepseek"
  | "qwen"
  | "mimo"
  | "openai"
  | "anthropic"
  | "moonshot"
  | "zhipu"
  | "openrouter"
  | "ollama";

export type ProviderPreset = {
  id: ProviderPresetId;
  name: string;
  type: "openai-compatible" | "openai-responses" | "google" | "anthropic";
  baseURL: string;
  models: ProviderModel[];
  apiKeyURL?: string;
  apiKeyOptional?: boolean;
};

export const PROVIDER_PRESETS: ProviderPreset[] = [
  {
    id: "deepseek",
    name: "DeepSeek",
    type: "openai-compatible",
    baseURL: "https://api.deepseek.com",
    models: [
      model("deepseek-v4-flash", { contextWindow: 1_050_000, capabilities: { tools: true }, openai: { temperature: 0.7, max_completion_tokens: 384_000, max_tool_loops: 64 } }),
      model("deepseek-v4-pro", { contextWindow: 1_050_000, capabilities: { tools: true }, openai: { temperature: 0.7, max_completion_tokens: 384_000, max_tool_loops: 64 } }),
    ],
    apiKeyURL: "https://platform.deepseek.com/api_keys",
  },
  {
    id: "qwen",
    name: "Qwen",
    type: "openai-compatible",
    baseURL: "https://dashscope.aliyuncs.com/compatible-mode/v1",
    models: ["qwen3.6-flash", "qwen3.7-max", "qwen3.6-plus", "qwen3-max"].map((id) => model(id, { contextWindow: 1_000_000, capabilities: { tools: true }, openai: { temperature: 0.7 } })),
    apiKeyURL: "https://bailian.console.aliyun.com/?apiKey=1",
  },
  {
    id: "mimo",
    name: "MiMo",
    type: "openai-compatible",
    baseURL: "https://api.xiaomimimo.com/v1",
    models: ["mimo-v2.5", "mimo-v2.5-pro"].map((id) => model(id, { contextWindow: 1_000_000, capabilities: { tools: true }, openai: { temperature: 0.7 } })),
    apiKeyURL: "https://platform.xiaomimimo.com/",
  },
  {
    id: "openai",
    name: "OpenAI",
    type: "openai-responses",
    baseURL: "https://api.openai.com/v1",
    models: [
      model("gpt-5.5", { contextWindow: 1_050_000, capabilities: { image: true, audio: true, tools: true }, openai: { temperature: 0.7, reasoning_effort: "medium" } }),
      model("gpt-5.4", { contextWindow: 1_050_000, capabilities: { image: true, audio: true, tools: true }, openai: { temperature: 0.7, reasoning_effort: "medium" } }),
      model("gpt-5.4-mini", { contextWindow: 400_000, capabilities: { image: true, tools: true }, openai: { temperature: 0.7, reasoning_effort: "low" } }),
    ],
    apiKeyURL: "https://platform.openai.com/api-keys",
  },
  {
    id: "anthropic",
    name: "Anthropic",
    type: "anthropic",
    baseURL: "https://api.anthropic.com",
    models: [
      model("claude-opus-4-8", { contextWindow: 1_000_000, capabilities: { image: true, tools: true }, anthropic: { max_tokens: 128_000, temperature: 0.7 } }),
      model("claude-sonnet-4-6", { contextWindow: 1_000_000, capabilities: { image: true, tools: true }, anthropic: { max_tokens: 128_000, temperature: 0.7 } }),
      model("claude-haiku-4-5", { contextWindow: 200_000, capabilities: { image: true, tools: true }, anthropic: { max_tokens: 64_000, temperature: 0.7 } }),
    ],
    apiKeyURL: "https://platform.claude.com/settings/keys",
  },
  {
    id: "moonshot",
    name: "Moonshot",
    type: "openai-compatible",
    baseURL: "https://api.moonshot.cn/v1",
    models: ["kimi-k2.6", "kimi-k2.5"].map((id) => model(id, { contextWindow: 1_000_000, capabilities: { tools: true }, openai: { temperature: 0.7 } })),
    apiKeyURL: "https://platform.moonshot.cn/console/api-keys",
  },
  {
    id: "zhipu",
    name: "Zhipu GLM",
    type: "openai-compatible",
    baseURL: "https://open.bigmodel.cn/api/paas/v4",
    models: ["glm-5.1", "glm-5"].map((id) => model(id, { contextWindow: 1_000_000, capabilities: { image: true, tools: true }, openai: { temperature: 0.7 } })),
    apiKeyURL: "https://open.bigmodel.cn/usercenter/apikeys",
  },
  {
    id: "openrouter",
    name: "OpenRouter",
    type: "openai-compatible",
    baseURL: "https://openrouter.ai/api/v1",
    models: [
      "openrouter/free",
      "openrouter/owl-alpha",
      "nvidia/nemotron-3-super-120b-a12b:free",
      "poolside/laguna-m.1:free",
      "openai/gpt-oss-120b:free",
      "z-ai/glm-4.5-air:free",
    ].map((id) => model(id, { capabilities: { tools: true }, openai: { temperature: 0.7 } })),
    apiKeyURL: "https://openrouter.ai/keys",
  },
  {
    id: "ollama",
    name: "Ollama",
    type: "openai-compatible",
    baseURL: "http://localhost:11434/v1",
    models: ["llama3.3", "qwen3", "gemma3", "gpt-oss:120b-cloud", "qwen3-coder:480b-cloud"].map((id) => model(id, { capabilities: { tools: true }, openai: { temperature: 0.7 } })),
    apiKeyOptional: true,
    apiKeyURL: "https://ollama.com/download",
  },
];

const ZH_ORDER: ProviderPresetId[] = [
  "deepseek",
  "qwen",
  "mimo",
  "openai",
  "anthropic",
  "moonshot",
  "zhipu",
  "openrouter",
  "ollama",
];

const DEFAULT_ORDER: ProviderPresetId[] = [
  "openai",
  "anthropic",
  "deepseek",
  "qwen",
  "mimo",
  "moonshot",
  "zhipu",
  "openrouter",
  "ollama",
];

export function getOrderedProviderPresets(locale: string) {
  const order = locale.startsWith("zh") ? ZH_ORDER : DEFAULT_ORDER;
  return order
    .map((id) => PROVIDER_PRESETS.find((preset) => preset.id === id))
    .filter((preset): preset is ProviderPreset => preset !== undefined);
}

export function providerPresetName(preset: ProviderPreset) {
  return preset.id;
}

function model(id: string, patch: Omit<ProviderModel, "id"> = {}): ProviderModel {
  return { id, ...patch };
}

export type ProviderPresetId =
  | "deepseek"
  | "qwen"
  | "mimo"
  | "openai"
  | "moonshot"
  | "zhipu"
  | "openrouter"
  | "ollama";

export type ProviderPreset = {
  id: ProviderPresetId;
  name: string;
  baseURL: string;
  defaultModel: string;
  models: string[];
  apiKeyURL?: string;
  apiKeyOptional?: boolean;
};

export const PROVIDER_PRESETS: ProviderPreset[] = [
  {
    id: "deepseek",
    name: "DeepSeek",
    baseURL: "https://api.deepseek.com",
    defaultModel: "deepseek-v4-flash",
    models: ["deepseek-v4-flash", "deepseek-v4-pro"],
    apiKeyURL: "https://platform.deepseek.com/api_keys",
  },
  {
    id: "qwen",
    name: "Qwen",
    baseURL: "https://dashscope.aliyuncs.com/compatible-mode/v1",
    defaultModel: "qwen3.6-flash",
    models: ["qwen3.6-flash", "qwen3.7-max", "qwen3.6-plus", "qwen3-max"],
    apiKeyURL: "https://bailian.console.aliyun.com/?apiKey=1",
  },
  {
    id: "mimo",
    name: "MiMo",
    baseURL: "https://api.xiaomimimo.com/v1",
    defaultModel: "mimo-v2.5",
    models: ["mimo-v2.5", "mimo-v2.5-pro"],
    apiKeyURL: "https://platform.xiaomimimo.com/",
  },
  {
    id: "openai",
    name: "OpenAI",
    baseURL: "https://api.openai.com/v1",
    defaultModel: "gpt-5.5",
    models: ["gpt-5.5", "gpt-5.4", "gpt-5.4-mini"],
    apiKeyURL: "https://platform.openai.com/api-keys",
  },
  {
    id: "moonshot",
    name: "Moonshot",
    baseURL: "https://api.moonshot.cn/v1",
    defaultModel: "kimi-k2.6",
    models: ["kimi-k2.6", "kimi-k2.5"],
    apiKeyURL: "https://platform.moonshot.cn/console/api-keys",
  },
  {
    id: "zhipu",
    name: "Zhipu GLM",
    baseURL: "https://open.bigmodel.cn/api/paas/v4",
    defaultModel: "glm-5.1",
    models: ["glm-5.1", "glm-5"],
    apiKeyURL: "https://open.bigmodel.cn/usercenter/apikeys",
  },
  {
    id: "openrouter",
    name: "OpenRouter",
    baseURL: "https://openrouter.ai/api/v1",
    defaultModel: "openrouter/free",
    models: [
      "openrouter/free",
      "openrouter/owl-alpha",
      "nvidia/nemotron-3-super-120b-a12b:free",
      "poolside/laguna-m.1:free",
      "openai/gpt-oss-120b:free",
      "z-ai/glm-4.5-air:free",
    ],
    apiKeyURL: "https://openrouter.ai/keys",
  },
  {
    id: "ollama",
    name: "Ollama",
    baseURL: "http://localhost:11434/v1",
    defaultModel: "llama3.3",
    models: ["llama3.3", "qwen3", "gemma3", "gpt-oss:120b-cloud", "qwen3-coder:480b-cloud"],
    apiKeyOptional: true,
    apiKeyURL: "https://ollama.com/download",
  },
];

const ZH_ORDER: ProviderPresetId[] = [
  "deepseek",
  "qwen",
  "mimo",
  "openai",
  "moonshot",
  "zhipu",
  "openrouter",
  "ollama",
];

const DEFAULT_ORDER: ProviderPresetId[] = [
  "openai",
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

export function applyProviderPreset(settings: Record<string, string>, preset: ProviderPreset) {
  return {
    ...settings,
    "provider.openai.base_url": preset.baseURL,
    "provider.openai.api_key": settings["provider.openai.api_key"] ?? "",
    "model.default": settings["model.default"] && preset.models.includes(settings["model.default"])
      ? settings["model.default"]
      : preset.defaultModel,
  };
}

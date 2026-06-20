import type { ProviderModel, ProviderProfile } from "@/api/client";

export type ProviderPresetId =
  | "deepseek"
  | "qwen"
  | "mimo"
  | "gemini"
  | "openai"
  | "anthropic"
  | "moonshot"
  | "zhipu"
  | "openrouter"
  | "ollama";

export type ProviderPresetProtocol = "openai-compatible" | "openai-responses" | "google" | "anthropic";

export type ProviderPresetVariant = {
  id: string;
  label: string;
  description: string;
  protocol: ProviderPresetProtocol;
  baseURL: string;
  models: ProviderModel[];
  apiKeyOptional?: boolean;
  profileName?: string;
};

export type ProviderPreset = {
  id: ProviderPresetId;
  name: string;
  description: string;
  apiKeyURL?: string;
  defaultVariantId: string;
  variants: ProviderPresetVariant[];
};

type ProviderModelPatch = Omit<ProviderModel, "id" | "limits" | "providerOptions"> & {
  openai?: Record<string, unknown>;
  google?: Record<string, unknown>;
  anthropic?: Record<string, unknown>;
};

const DEEPSEEK_OPENAI_MODELS = [
  model("deepseek-v4-flash", { contextWindow: 1_050_000, capabilities: { tools: true }, openai: { temperature: 0.7, max_completion_tokens: 384_000, max_tool_loops: 64 } }),
  model("deepseek-v4-pro", { contextWindow: 1_050_000, capabilities: { tools: true }, openai: { temperature: 0.7, max_completion_tokens: 384_000, max_tool_loops: 64 } }),
];

const DEEPSEEK_ANTHROPIC_MODELS = [
  model("deepseek-v4-flash", { contextWindow: 1_050_000, capabilities: { tools: true }, anthropic: { temperature: 0.7, max_tokens: 384_000 } }),
  model("deepseek-v4-pro", { contextWindow: 1_050_000, capabilities: { tools: true }, anthropic: { temperature: 0.7, max_tokens: 384_000 } }),
];

const MIMO_OPENAI_MODELS = ["mimo-v2.5", "mimo-v2.5-pro"].map((id) =>
  model(id, { contextWindow: 1_000_000, capabilities: { tools: true }, openai: { temperature: 0.7, max_completion_tokens: 131_072 } }),
);

const MIMO_ANTHROPIC_MODELS = ["mimo-v2.5", "mimo-v2.5-pro"].map((id) =>
  model(id, { contextWindow: 1_000_000, capabilities: { tools: true }, anthropic: { temperature: 0.7, max_tokens: 131_072 } }),
);

const QWEN_MODEL_IDS = ["qwen3.6-flash", "qwen3.7-max", "qwen3.6-plus", "qwen3-max"];
const QWEN_OPENAI_MODELS = QWEN_MODEL_IDS.map((id) =>
  model(id, { contextWindow: 1_000_000, capabilities: { tools: true }, openai: { temperature: 0.7 } }),
);
const QWEN_ANTHROPIC_MODELS = QWEN_MODEL_IDS.map((id) =>
  model(id, { contextWindow: 1_000_000, capabilities: { tools: true }, anthropic: { temperature: 0.7, max_tokens: 131_072 } }),
);

const MOONSHOT_MODEL_IDS = ["kimi-k2.6", "kimi-k2.5"];
const MOONSHOT_OPENAI_MODELS = MOONSHOT_MODEL_IDS.map((id) =>
  model(id, { contextWindow: 1_000_000, capabilities: { tools: true }, openai: { temperature: 0.7 } }),
);
const MOONSHOT_ANTHROPIC_MODELS = MOONSHOT_MODEL_IDS.map((id) =>
  model(id, { contextWindow: 1_000_000, capabilities: { tools: true }, anthropic: { temperature: 0.7, max_tokens: 131_072 } }),
);

const ZHIPU_MODEL_IDS = ["glm-5.1", "glm-5"];
const ZHIPU_OPENAI_MODELS = ZHIPU_MODEL_IDS.map((id) =>
  model(id, { contextWindow: 1_000_000, capabilities: { image: true, tools: true }, openai: { temperature: 0.7 } }),
);
const ZHIPU_ANTHROPIC_MODELS = ZHIPU_MODEL_IDS.map((id) =>
  model(id, { contextWindow: 1_000_000, capabilities: { image: true, tools: true }, anthropic: { temperature: 0.7, max_tokens: 131_072 } }),
);

const OPENAI_MODELS = [
  model("gpt-5.5", { contextWindow: 1_050_000, capabilities: { image: true, audio: true, tools: true }, openai: { temperature: 0.7, reasoning_effort: "medium" } }),
  model("gpt-5.4", { contextWindow: 1_050_000, capabilities: { image: true, audio: true, tools: true }, openai: { temperature: 0.7, reasoning_effort: "medium" } }),
  model("gpt-5.4-mini", { contextWindow: 400_000, capabilities: { image: true, tools: true }, openai: { temperature: 0.7, reasoning_effort: "low" } }),
];

export const PROVIDER_PRESETS: ProviderPreset[] = [
  {
    id: "deepseek",
    name: "DeepSeek",
    description: "DeepSeek V4 models with OpenAI-compatible and Anthropic-compatible protocols.",
    defaultVariantId: "openai",
    apiKeyURL: "https://platform.deepseek.com/api_keys",
    variants: [
      {
        id: "openai",
        label: "OpenAI Compatible",
        description: "https://api.deepseek.com",
        protocol: "openai-compatible",
        baseURL: "https://api.deepseek.com",
        models: DEEPSEEK_OPENAI_MODELS,
      },
      {
        id: "anthropic",
        label: "Anthropic Compatible",
        description: "https://api.deepseek.com/anthropic",
        protocol: "anthropic",
        baseURL: "https://api.deepseek.com/anthropic",
        models: DEEPSEEK_ANTHROPIC_MODELS,
        profileName: "DeepSeek Anthropic",
      },
    ],
  },
  {
    id: "qwen",
    name: "Qwen",
    description: "Alibaba Qwen via DashScope OpenAI-compatible or Anthropic-compatible endpoints.",
    defaultVariantId: "default",
    apiKeyURL: "https://bailian.console.aliyun.com/?apiKey=1",
    variants: [
      {
        id: "default",
        label: "OpenAI Compatible",
        description: "https://dashscope.aliyuncs.com/compatible-mode/v1",
        protocol: "openai-compatible",
        baseURL: "https://dashscope.aliyuncs.com/compatible-mode/v1",
        models: QWEN_OPENAI_MODELS,
      },
      {
        id: "anthropic",
        label: "Anthropic Compatible",
        description: "https://dashscope.aliyuncs.com/apps/anthropic",
        protocol: "anthropic",
        baseURL: "https://dashscope.aliyuncs.com/apps/anthropic",
        models: QWEN_ANTHROPIC_MODELS,
        profileName: "Qwen Anthropic",
      },
    ],
  },
  {
    id: "mimo",
    name: "MiMo",
    description: "Xiaomi MiMo standard and plan endpoints.",
    defaultVariantId: "standard-openai",
    apiKeyURL: "https://platform.xiaomimimo.com/",
    variants: [
      {
        id: "standard-openai",
        label: "标准 API / OpenAI",
        description: "api.xiaomimimo.com/v1 · 按 token 计费",
        protocol: "openai-compatible",
        baseURL: "https://api.xiaomimimo.com/v1",
        models: MIMO_OPENAI_MODELS,
      },
      {
        id: "standard-anthropic",
        label: "标准 API / Anthropic",
        description: "api.xiaomimimo.com/anthropic · 按 token 计费",
        protocol: "anthropic",
        baseURL: "https://api.xiaomimimo.com/anthropic",
        models: MIMO_ANTHROPIC_MODELS,
        profileName: "MiMo Anthropic",
      },
      {
        id: "plan-openai",
        label: "Plan / OpenAI",
        description: "token-plan-cn.xiaomimimo.com/v1 · 订阅会员",
        protocol: "openai-compatible",
        baseURL: "https://token-plan-cn.xiaomimimo.com/v1",
        models: MIMO_OPENAI_MODELS,
        profileName: "MiMo Plan",
      },
      {
        id: "plan-anthropic",
        label: "Plan / Anthropic",
        description: "token-plan-cn.xiaomimimo.com/anthropic · 订阅会员",
        protocol: "anthropic",
        baseURL: "https://token-plan-cn.xiaomimimo.com/anthropic",
        models: MIMO_ANTHROPIC_MODELS,
        profileName: "MiMo Plan Anthropic",
      },
    ],
  },
  {
    id: "gemini",
    name: "Google Gemini",
    description: "Google Gemini API with multimodal chat models.",
    defaultVariantId: "default",
    apiKeyURL: "https://aistudio.google.com/app/apikey",
    variants: [
      {
        id: "default",
        label: "Google API",
        description: "Google AI Studio API",
        protocol: "google",
        baseURL: "",
        models: [
          model("gemini-3.5-flash", { contextWindow: 1_000_000, capabilities: { image: true, audio: true, tools: true }, google: { temperature: 0.7, maxOutputTokens: 64_000 } }),
          model("gemini-3.5-pro", { contextWindow: 1_000_000, capabilities: { image: true, audio: true, tools: true }, google: { temperature: 0.7, maxOutputTokens: 64_000 } }),
          model("gemini-2.5-flash", { contextWindow: 1_000_000, capabilities: { image: true, audio: true, tools: true }, google: { temperature: 0.7, maxOutputTokens: 64_000 } }),
        ],
      },
    ],
  },
  {
    id: "openai",
    name: "OpenAI",
    description: "OpenAI official API with Responses API or OpenAI-compatible chat.",
    defaultVariantId: "responses",
    apiKeyURL: "https://platform.openai.com/api-keys",
    variants: [
      {
        id: "responses",
        label: "Responses API",
        description: "https://api.openai.com/v1/responses",
        protocol: "openai-responses",
        baseURL: "https://api.openai.com/v1",
        models: OPENAI_MODELS,
      },
      {
        id: "compatible",
        label: "OpenAI Compatible",
        description: "https://api.openai.com/v1/chat/completions",
        protocol: "openai-compatible",
        baseURL: "https://api.openai.com/v1",
        models: OPENAI_MODELS,
        profileName: "OpenAI Compatible",
      },
    ],
  },
  {
    id: "anthropic",
    name: "Anthropic",
    description: "Anthropic Claude native Messages API.",
    defaultVariantId: "default",
    apiKeyURL: "https://console.anthropic.com/settings/keys",
    variants: [
      {
        id: "default",
        label: "Messages API",
        description: "https://api.anthropic.com",
        protocol: "anthropic",
        baseURL: "https://api.anthropic.com",
        models: [
          model("claude-opus-4-8", { contextWindow: 1_000_000, capabilities: { image: true, tools: true }, anthropic: { max_tokens: 128_000, temperature: 0.7 } }),
          model("claude-sonnet-4-6", { contextWindow: 1_000_000, capabilities: { image: true, tools: true }, anthropic: { max_tokens: 128_000, temperature: 0.7 } }),
          model("claude-haiku-4-5", { contextWindow: 200_000, capabilities: { image: true, tools: true }, anthropic: { max_tokens: 64_000, temperature: 0.7 } }),
        ],
      },
    ],
  },
  {
    id: "moonshot",
    name: "Moonshot",
    description: "Moonshot Kimi OpenAI-compatible or Anthropic-compatible endpoints.",
    defaultVariantId: "default",
    apiKeyURL: "https://platform.moonshot.cn/console/api-keys",
    variants: [
      {
        id: "default",
        label: "OpenAI Compatible",
        description: "https://api.moonshot.cn/v1",
        protocol: "openai-compatible",
        baseURL: "https://api.moonshot.cn/v1",
        models: MOONSHOT_OPENAI_MODELS,
      },
      {
        id: "anthropic",
        label: "Anthropic Compatible",
        description: "https://api.moonshot.ai/anthropic",
        protocol: "anthropic",
        baseURL: "https://api.moonshot.ai/anthropic",
        models: MOONSHOT_ANTHROPIC_MODELS,
        profileName: "Moonshot Anthropic",
      },
    ],
  },
  {
    id: "zhipu",
    name: "Zhipu GLM",
    description: "Zhipu GLM OpenAI-compatible or Anthropic-compatible endpoints.",
    defaultVariantId: "default",
    apiKeyURL: "https://open.bigmodel.cn/usercenter/apikeys",
    variants: [
      {
        id: "default",
        label: "OpenAI Compatible",
        description: "https://open.bigmodel.cn/api/paas/v4",
        protocol: "openai-compatible",
        baseURL: "https://open.bigmodel.cn/api/paas/v4",
        models: ZHIPU_OPENAI_MODELS,
      },
      {
        id: "anthropic",
        label: "Anthropic Compatible",
        description: "https://open.bigmodel.cn/api/anthropic",
        protocol: "anthropic",
        baseURL: "https://open.bigmodel.cn/api/anthropic",
        models: ZHIPU_ANTHROPIC_MODELS,
        profileName: "Zhipu GLM Anthropic",
      },
    ],
  },
  {
    id: "openrouter",
    name: "OpenRouter",
    description: "OpenRouter meta-router through its OpenAI-compatible endpoint.",
    defaultVariantId: "default",
    apiKeyURL: "https://openrouter.ai/keys",
    variants: [
      {
        id: "default",
        label: "OpenAI Compatible",
        description: "https://openrouter.ai/api/v1",
        protocol: "openai-compatible",
        baseURL: "https://openrouter.ai/api/v1",
        models: [
          "openrouter/free",
          "openrouter/owl-alpha",
          "nvidia/nemotron-3-super-120b-a12b:free",
          "poolside/laguna-m.1:free",
          "openai/gpt-oss-120b:free",
          "z-ai/glm-4.5-air:free",
        ].map((id) => model(id, { capabilities: { tools: true }, openai: { temperature: 0.7 } })),
      },
    ],
  },
  {
    id: "ollama",
    name: "Ollama",
    description: "Local Ollama OpenAI-compatible endpoint.",
    defaultVariantId: "default",
    apiKeyURL: "https://ollama.com/download",
    variants: [
      {
        id: "default",
        label: "Local OpenAI Compatible",
        description: "http://localhost:11434/v1",
        protocol: "openai-compatible",
        baseURL: "http://localhost:11434/v1",
        models: ["llama3.3", "qwen3", "gemma3", "gpt-oss:120b-cloud", "qwen3-coder:480b-cloud"].map((id) => model(id, { capabilities: { tools: true }, openai: { temperature: 0.7 } })),
        apiKeyOptional: true,
      },
    ],
  },
];

const ZH_ORDER: ProviderPresetId[] = [
  "deepseek",
  "qwen",
  "mimo",
  "moonshot",
  "zhipu",
  "openrouter",
  "ollama",
  "openai",
  "anthropic",
  "gemini",
];

const DEFAULT_ORDER: ProviderPresetId[] = [
  "openai",
  "anthropic",
  "gemini",
  "deepseek",
  "qwen",
  "mimo",
  "moonshot",
  "zhipu",
  "openrouter",
  "ollama",
];

const PROFILE_ID_PREFIX = "prof_";
const PROFILE_ID_RANDOM_CHARS = 12;
const PROFILE_ID_DEFAULT_BRAND = "custom";
const PROFILE_ID_BRANDS = new Set([
  "gemini",
  "openai",
  "anthropic",
  "mimo",
  "deepseek",
  "qwen",
  "ollama",
  "kimi",
  "grok",
  "glm",
  "openrouter",
  PROFILE_ID_DEFAULT_BRAND,
]);

export function getOrderedProviderPresets(locale: string) {
  const order = locale.startsWith("zh") ? ZH_ORDER : DEFAULT_ORDER;
  return order
    .map((id) => PROVIDER_PRESETS.find((preset) => preset.id === id))
    .filter((preset): preset is ProviderPreset => preset !== undefined);
}

export function defaultProviderPresetVariant(preset: ProviderPreset) {
  return providerPresetVariant(preset, preset.defaultVariantId);
}

export function providerPresetVariant(preset: ProviderPreset, variantID: string) {
  return preset.variants.find((variant) => variant.id === variantID) || preset.variants[0];
}

export function providerPresetProfileName(preset: ProviderPreset, variant: ProviderPresetVariant) {
  return variant.profileName || preset.name;
}

export function generateProviderProfileID(existingProfiles: ProviderProfile[] | Iterable<string>, presetID: string) {
  const existingIDs = Array.isArray(existingProfiles)
    ? existingProfiles.map((profile) => typeof profile === "string" ? profile : profile.id)
    : Array.from(existingProfiles);
  const existing = new Set(existingIDs);
  const brandSlug = profileBrandSlugFromPreset(presetID);
  for (let index = 0; index < 20; index += 1) {
    const id = `${PROFILE_ID_PREFIX}${brandSlug}_${randomToken()}`;
    if (!existing.has(id)) {
      return id;
    }
  }
  return `${PROFILE_ID_PREFIX}${brandSlug}_${Date.now().toString(36)}`;
}

function profileBrandSlugFromPreset(presetID: string | undefined): string {
  switch ((presetID || "").trim().toLowerCase()) {
    case "moonshot":
      return "kimi";
    case "zhipu":
      return "glm";
    default:
      return normalizeBrandSlug(presetID);
  }
}

function normalizeBrandSlug(brand: string | undefined): string {
  const slug = (brand || "")
    .trim()
    .toLowerCase()
    .replace(/[^a-z0-9]+/g, "_")
    .replace(/^_+|_+$/g, "");
  return PROFILE_ID_BRANDS.has(slug) ? slug : PROFILE_ID_DEFAULT_BRAND;
}

function randomToken(): string {
  const cryptoAPI = globalThis.crypto;
  if (cryptoAPI?.randomUUID) {
    return cryptoAPI.randomUUID().replaceAll("-", "").slice(0, PROFILE_ID_RANDOM_CHARS);
  }
  if (cryptoAPI?.getRandomValues) {
    const bytes = new Uint8Array(9);
    cryptoAPI.getRandomValues(bytes);
    return Array.from(bytes, (byte) => byte.toString(36).padStart(2, "0")).join("").slice(0, PROFILE_ID_RANDOM_CHARS);
  }
  return `${Date.now().toString(36)}${Math.random().toString(36).slice(2)}`.slice(0, PROFILE_ID_RANDOM_CHARS);
}

function model(id: string, patch: ProviderModelPatch = {}): ProviderModel {
  const { openai, google, anthropic, ...rest } = patch;
  const maxOutputTokens = numericOption(openai?.max_output_tokens ?? openai?.max_completion_tokens ?? google?.maxOutputTokens ?? anthropic?.max_tokens);
  const maxToolLoops = numericOption(openai?.max_tool_loops);
  const cleanOpenAI = omitOptions(openai, ["max_output_tokens", "max_completion_tokens", "max_tool_loops"]);
  const cleanGoogle = omitOptions(google, ["maxOutputTokens", "max_output_tokens", "max_tokens"]);
  const cleanAnthropic = omitOptions(anthropic, ["max_tokens", "max_output_tokens"]);
  return {
    id,
    ...rest,
    limits: maxOutputTokens || maxToolLoops ? { maxOutputTokens, maxToolLoops } : undefined,
    providerOptions: cleanProviderOptions({
      openai: cleanOpenAI,
      google: cleanGoogle,
      anthropic: cleanAnthropic,
    }),
  };
}

function numericOption(value: unknown) {
  return typeof value === "number" && Number.isFinite(value) && value > 0 ? value : undefined;
}

function omitOptions(options: Record<string, unknown> | undefined, keys: string[]) {
  if (!options) {
    return undefined;
  }
  const out: Record<string, unknown> = {};
  for (const [key, value] of Object.entries(options)) {
    if (!keys.includes(key) && value !== undefined) {
      out[key] = value;
    }
  }
  return Object.keys(out).length > 0 ? out : undefined;
}

function cleanProviderOptions(options: NonNullable<ProviderModel["providerOptions"]>) {
  return options.openai || options.google || options.anthropic ? options : undefined;
}

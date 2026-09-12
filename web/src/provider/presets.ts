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
  | "buzzhive"
  | "ollama";

export type ProviderPresetProtocol = "openai-compatible" | "openai-responses" | "google" | "anthropic";

export function providerProtocolDisplayName(protocol: ProviderPresetProtocol) {
  switch (protocol) {
    case "openai-compatible":
      return "OpenAI";
    case "openai-responses":
      return "OpenAI Responses";
    case "google":
      return "Google";
    case "anthropic":
      return "Anthropic";
  }
}

export type ProviderPresetVariant = {
  id: string;
  label: string;
  description: string;
  group?: string;
  protocol: ProviderPresetProtocol;
  baseURL: string;
  models: ProviderModel[];
  dynamicModels?: boolean;
  supportsModelDiscovery?: boolean;
  modelDiscovery?: {
    protocol: ProviderPresetProtocol;
    baseURL: string;
  };
  baseURLEditable?: boolean;
  baseURLPlaceholder?: string;
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

// Creation/import templates only; saved profile.models remain authoritative.
// Verified against vendor model catalogs on 2026-09-12 (docs/provider-presets.md).
const DEEPSEEK_MODELS = [
  model("deepseek-flash", { contextWindow: 1_000_000, capabilities: { image: true, tools: true }, limits: { maxOutputTokens: 384_000, maxToolLoops: 64 } }),
  model("deepseek-v4-pro", { contextWindow: 1_000_000, capabilities: { tools: true }, limits: { maxOutputTokens: 384_000, maxToolLoops: 64 } }),
];

const MIMO_MODELS = ["mimo-v2.5", "mimo-v2.5-pro"].map((id) =>
  model(id, {
    contextWindow: 1_000_000,
    capabilities: id === "mimo-v2.5" ? { image: true, audio: true, tools: true } : { tools: true },
    limits: { maxOutputTokens: 131_072 },
  }),
);

const QWEN_MODELS = ["qwen3.8-flash", "qwen3.8-max-0902", "qwen3.7-plus"].map((id) =>
  model(id, { contextWindow: 1_000_000, capabilities: { image: true, tools: true }, limits: { maxOutputTokens: 131_072 } }),
);

const MOONSHOT_MODELS = ["kimi-k3", "kimi-k2.7-code", "kimi-k2.6"].map((id) =>
  model(id, { contextWindow: id === "kimi-k3" ? 1_000_000 : 262_144, capabilities: { image: true, tools: true }, limits: { maxOutputTokens: 131_072 } }),
);

const ZHIPU_MODELS = ["glm-5.3-flash", "glm-5.2", "glm-5.1"].map((id) =>
  model(id, {
    contextWindow: id === "glm-5.1" ? 200_000 : 1_000_000,
    capabilities: { image: id === "glm-5.3-flash", tools: true },
    limits: { maxOutputTokens: 128_000 },
  }),
);

const OPENAI_MODELS = ["gpt-5.6-sol", "gpt-5.6-terra", "gpt-5.6-luna", "gpt-5.5", "gpt-5.4", "gpt-5.4-mini", "gpt-5.4-nano"].map((id) =>
  model(id, { contextWindow: id === "gpt-5.4-mini" || id === "gpt-5.4-nano" ? 400_000 : 1_050_000, capabilities: { image: true, tools: true }, limits: { maxOutputTokens: 128_000 } }),
);
// Astra's tool use requires Responses; Chat Completions only supports text.
const OPENAI_RESPONSES_MODELS = [
  model("gpt-6-astra", { contextWindow: 1_050_000, capabilities: { image: true, tools: true }, limits: { maxOutputTokens: 128_000 } }),
  ...OPENAI_MODELS,
];

export const PROVIDER_PRESETS: ProviderPreset[] = [
  {
    id: "deepseek",
    name: "DeepSeek",
    description: "DeepSeek models with Responses, OpenAI-compatible, and Anthropic-compatible protocols.",
    defaultVariantId: "openai",
    apiKeyURL: "https://platform.deepseek.com/api_keys",
    variants: [
      {
        id: "responses",
        label: "Responses API",
        description: "https://api.deepseek.com/responses",
        protocol: "openai-responses",
        baseURL: "https://api.deepseek.com",
        models: DEEPSEEK_MODELS,
        profileName: "DeepSeek Responses",
      },
      {
        id: "openai",
        label: "OpenAI Compatible",
        description: "https://api.deepseek.com",
        protocol: "openai-compatible",
        baseURL: "https://api.deepseek.com",
        models: DEEPSEEK_MODELS,
      },
      {
        id: "anthropic",
        label: "Anthropic Compatible",
        description: "https://api.deepseek.com/anthropic",
        protocol: "anthropic",
        baseURL: "https://api.deepseek.com/anthropic",
        models: DEEPSEEK_MODELS,
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
        models: QWEN_MODELS,
      },
      {
        id: "anthropic",
        label: "Anthropic Compatible",
        description: "https://dashscope.aliyuncs.com/apps/anthropic",
        protocol: "anthropic",
        baseURL: "https://dashscope.aliyuncs.com/apps/anthropic",
        models: QWEN_MODELS,
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
        label: "Standard API / OpenAI",
        description: "api.xiaomimimo.com/v1 · token-based billing",
        group: "standard",
        protocol: "openai-compatible",
        baseURL: "https://api.xiaomimimo.com/v1",
        models: MIMO_MODELS,
      },
      {
        id: "standard-anthropic",
        label: "Standard API / Anthropic",
        description: "api.xiaomimimo.com/anthropic · token-based billing",
        group: "standard",
        protocol: "anthropic",
        baseURL: "https://api.xiaomimimo.com/anthropic",
        modelDiscovery: {
          protocol: "openai-compatible",
          baseURL: "https://api.xiaomimimo.com/v1",
        },
        models: MIMO_MODELS,
        profileName: "MiMo Anthropic",
      },
      {
        id: "plan-openai",
        label: "Plan / OpenAI",
        description: "token-plan-cn.xiaomimimo.com/v1 · subscription plan",
        group: "plan",
        protocol: "openai-compatible",
        baseURL: "https://token-plan-cn.xiaomimimo.com/v1",
        models: MIMO_MODELS,
        profileName: "MiMo Plan",
      },
      {
        id: "plan-anthropic",
        label: "Plan / Anthropic",
        description: "token-plan-cn.xiaomimimo.com/anthropic · subscription plan",
        group: "plan",
        protocol: "anthropic",
        baseURL: "https://token-plan-cn.xiaomimimo.com/anthropic",
        modelDiscovery: {
          protocol: "openai-compatible",
          baseURL: "https://token-plan-cn.xiaomimimo.com/v1",
        },
        models: MIMO_MODELS,
        profileName: "MiMo Plan Anthropic",
      },
    ],
  },
  {
    id: "gemini",
    name: "Gemini",
    description: "Google Gemini API with multimodal chat models.",
    defaultVariantId: "default",
    apiKeyURL: "https://aistudio.google.com/app/apikey",
    variants: [
      {
        id: "default",
        label: "Google API",
        description: "https://generativelanguage.googleapis.com",
        protocol: "google",
        baseURL: "https://generativelanguage.googleapis.com",
        models: [
          "gemini-3.8-flash", "gemini-3.5-flash-lite", "gemini-3.1-pro-preview",
        ].map((id) => model(id, {
          contextWindow: 1_048_576,
          capabilities: { image: true, audio: true, tools: true },
          limits: { maxOutputTokens: 65_536 },
          providerOptions: { google: { thinking: { include_thoughts: true } } },
        })),
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
        models: OPENAI_RESPONSES_MODELS,
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
          model("claude-fable-5-1", { contextWindow: 1_000_000, capabilities: { image: true, tools: true }, limits: { maxOutputTokens: 128_000 } }),
          model("claude-opus-5", { contextWindow: 1_000_000, capabilities: { image: true, tools: true }, limits: { maxOutputTokens: 128_000 } }),
          model("claude-sonnet-5", { contextWindow: 1_000_000, capabilities: { image: true, tools: true }, limits: { maxOutputTokens: 128_000 } }),
          model("claude-haiku-4-5", { contextWindow: 200_000, capabilities: { image: true, tools: true }, limits: { maxOutputTokens: 64_000 } }),
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
        models: MOONSHOT_MODELS,
      },
      {
        id: "anthropic",
        label: "Anthropic Compatible",
        description: "https://api.moonshot.ai/anthropic",
        protocol: "anthropic",
        baseURL: "https://api.moonshot.ai/anthropic",
        models: MOONSHOT_MODELS,
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
        models: ZHIPU_MODELS,
      },
      {
        id: "anthropic",
        label: "Anthropic Compatible",
        description: "https://open.bigmodel.cn/api/anthropic",
        protocol: "anthropic",
        baseURL: "https://open.bigmodel.cn/api/anthropic",
        models: ZHIPU_MODELS,
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
          "nvidia/nemotron-3.5-lightning:free",
          "nvidia/nemotron-3-super-120b-a12b:free",
          "z-ai/glm-4.5-air:free",
        ].map((id) => model(id, { capabilities: { tools: true } })),
      },
    ],
  },
  {
    id: "buzzhive",
    name: "BuzzHive",
    description: "BuzzHive proxy with OpenAI Chat, OpenAI Responses, Anthropic, and Gemini-compatible endpoints; models are loaded from the endpoint.",
    apiKeyURL: "https://github.com/teatak/buzzhive",
    defaultVariantId: "openai-compatible",
    variants: [
      {
        id: "openai-compatible",
        label: "OpenAI Chat",
        description: "http://127.0.0.1:9622/v1",
        protocol: "openai-compatible",
        baseURL: "http://127.0.0.1:9622/v1",
        baseURLPlaceholder: "http://127.0.0.1:9622/v1",
        baseURLEditable: true,
        dynamicModels: true,
        models: [],
        profileName: "BuzzHive",
      },
      {
        id: "openai-responses",
        label: "OpenAI Responses",
        description: "http://127.0.0.1:9622/v1",
        protocol: "openai-responses",
        baseURL: "http://127.0.0.1:9622/v1",
        baseURLPlaceholder: "http://127.0.0.1:9622/v1",
        baseURLEditable: true,
        dynamicModels: true,
        models: [],
        profileName: "BuzzHive Responses",
      },
      {
        id: "anthropic",
        label: "Anthropic",
        description: "http://127.0.0.1:9622",
        protocol: "anthropic",
        baseURL: "http://127.0.0.1:9622",
        baseURLPlaceholder: "http://127.0.0.1:9622",
        baseURLEditable: true,
        dynamicModels: true,
        models: [],
        profileName: "BuzzHive Anthropic",
      },
      {
        id: "google",
        label: "Gemini",
        description: "http://127.0.0.1:9622",
        protocol: "google",
        baseURL: "http://127.0.0.1:9622",
        baseURLPlaceholder: "http://127.0.0.1:9622",
        baseURLEditable: true,
        dynamicModels: true,
        models: [],
        profileName: "BuzzHive Gemini",
      },
    ],
  },
  {
    id: "ollama",
    name: "Ollama",
    description: "Local Ollama OpenAI-compatible endpoint; models are loaded from the endpoint.",
    defaultVariantId: "default",
    apiKeyURL: "https://ollama.com/download",
    variants: [
      {
        id: "default",
        label: "Local OpenAI Compatible",
        description: "http://localhost:11434/v1",
        protocol: "openai-compatible",
        baseURL: "http://localhost:11434/v1",
        baseURLPlaceholder: "http://localhost:11434/v1",
        baseURLEditable: true,
        dynamicModels: true,
        models: [],
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
  "buzzhive",
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
  "buzzhive",
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
  "buzzhive",
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

export function providerPresetForBrand(brand: string | undefined) {
  const normalizedBrand = (brand || "").trim().toLowerCase();
  if (!normalizedBrand) {
    return undefined;
  }
  return PROVIDER_PRESETS.find((preset) => preset.id === normalizedBrand);
}

// 模型 id 分段里用于判定厂商的家族关键字。内置清单只收录在售的完整 id,
// 别名(deepseek-chat)、自建 id(deepseek-flash)和中转前缀(z-ai/glm-4.5-air:free)
// 靠这里命中,否则会错误回退成 profile 品牌图标。
const MODEL_BRAND_KEYWORDS: ReadonlyArray<readonly [string, readonly string[]]> = [
  ["deepseek", ["deepseek"]],
  ["anthropic", ["anthropic", "claude"]],
  ["openai", ["gpt", "chatgpt", "o1", "o3", "o4", "o5"]],
  ["gemini", ["gemini", "gemma"]],
  ["qwen", ["qwen"]],
  ["mimo", ["mimo"]],
  ["moonshot", ["moonshot", "kimi"]],
  ["zhipu", ["zhipu", "glm", "zai"]],
  ["grok", ["grok"]],
];

// 模型 → 品牌图标 key。先按内置 preset 的模型清单精确命中,再按家族关键字判定
// (id 按非字母数字分段,允许 qwen3 / kimi-k3 这类带版本号的写法);判定不出返回
// undefined,由调用方回退 profile 品牌。
export function providerBrandForModel(modelID: string | undefined) {
  const normalizedModelID = (modelID || "").trim().toLowerCase();
  if (!normalizedModelID) {
    return undefined;
  }
  const exactPreset = PROVIDER_PRESETS.find((preset) =>
    preset.variants.some((variant) =>
      variant.models.some((model) => model.id.trim().toLowerCase() === normalizedModelID),
    ),
  );
  if (exactPreset) {
    return exactPreset.id;
  }
  const tokens = normalizedModelID.split(/[^a-z0-9]+/).filter(Boolean);
  for (const [brand, keywords] of MODEL_BRAND_KEYWORDS) {
    if (tokens.some((token) => keywords.some((keyword) => token.startsWith(keyword)))) {
      return brand;
    }
  }
  return undefined;
}

export function providerPresetVariantGroup(variant: ProviderPresetVariant | undefined) {
  return variant?.group || "default";
}

export function providerPresetGroups(preset: ProviderPreset) {
  const seen = new Set<string>();
  const out: string[] = [];
  for (const variant of preset.variants) {
    const group = providerPresetVariantGroup(variant);
    if (!seen.has(group)) {
      seen.add(group);
      out.push(group);
    }
  }
  return out;
}

export function providerPresetProtocolsForGroup(preset: ProviderPreset, group: string) {
  const activeGroup = group || "default";
  const seen = new Set<ProviderPresetProtocol>();
  const out: ProviderPresetProtocol[] = [];
  for (const variant of preset.variants) {
    if (providerPresetVariantGroup(variant) !== activeGroup || seen.has(variant.protocol)) {
      continue;
    }
    seen.add(variant.protocol);
    out.push(variant.protocol);
  }
  return out;
}

export function providerPresetVariantForSelection(
  preset: ProviderPreset | undefined,
  group: string | undefined,
  protocol: ProviderPresetProtocol | undefined,
) {
  if (!preset || !protocol) {
    return undefined;
  }
  const activeGroup = group || "default";
  return preset.variants.find(
    (variant) => providerPresetVariantGroup(variant) === activeGroup && variant.protocol === protocol,
  );
}

export function providerSupportsModelDiscovery(variant?: ProviderPresetVariant) {
  return variant?.supportsModelDiscovery !== false;
}

export function providerModelDiscoveryForVariant(
  preset: ProviderPreset,
  variant: ProviderPresetVariant,
  activeBaseURL = variant.baseURL,
) {
  if (variant.modelDiscovery) {
    return variant.modelDiscovery;
  }
  if (variant.protocol === "anthropic") {
    const group = providerPresetVariantGroup(variant);
    const openAIVariant = preset.variants.find(
      (candidate) =>
        providerPresetVariantGroup(candidate) === group &&
        (candidate.protocol === "openai-compatible" || candidate.protocol === "openai-responses"),
    );
    if (openAIVariant) {
      return {
        protocol: openAIVariant.protocol,
        baseURL: openAIVariant.baseURL,
      };
    }
  }
  return {
    protocol: variant.protocol,
    baseURL: activeBaseURL,
  };
}

export function mergeProviderModelCandidate(
  id: string,
  variant: ProviderPresetVariant | undefined,
  protocol: ProviderPresetProtocol,
): ProviderModel {
  const trimmedID = id.trim();
  const presetModel = variant?.models.find((model) => model.id === trimmedID);
  if (presetModel) {
    return { ...presetModel };
  }
  const fallback = providerModelFromCandidate(trimmedID, protocol);
  const globalModel = globalPresetModel(trimmedID, protocol);
  if (!globalModel) {
    return fallback;
  }
  const capabilities = { ...(globalModel.capabilities || fallback.capabilities) };
  // Astra may be discovered through Chat Completions, but its tools require Responses.
  if (trimmedID === "gpt-6-astra" && protocol === "openai-compatible") {
    capabilities.tools = false;
  }
  // 端点通常只给模型 ID。复用元数据时需遵循协议能力限制；请求参数使用
  // 当前协议的默认值，避免把 OpenAI options 带到 Anthropic / Google。
  return {
    ...fallback,
    displayName: globalModel.displayName,
    contextWindow: globalModel.contextWindow,
    capabilities,
    limits: globalModel.limits ? { ...globalModel.limits } : undefined,
  };
}

export function providerModelDisplayName(id: string) {
  const value = id.trim();
  if (!value) {
    return "";
  }
  const wordNames: Record<string, string> = {
    api: "API",
    claude: "Claude",
    deepseek: "DeepSeek",
    flash: "Flash",
    gemini: "Gemini",
    glm: "GLM",
    gpt: "GPT",
    kimi: "Kimi",
    max: "Max",
    mimo: "MiMo",
    mini: "Mini",
    nano: "Nano",
    openai: "OpenAI",
    openrouter: "OpenRouter",
    pro: "Pro",
    qwen: "Qwen",
  };
  return value
    .replaceAll("_", "-")
    .split("/")
    .map((segment) => segment
      .split("-")
      .map((word) => {
        const [base, suffix] = word.split(":", 2);
        const normalized = wordNames[base.toLowerCase()]
          || (/^v\d/i.test(base) ? `V${base.slice(1)}` : `${base.charAt(0).toUpperCase()}${base.slice(1)}`);
        return suffix ? `${normalized} (${suffix.charAt(0).toUpperCase()}${suffix.slice(1)})` : normalized;
      })
      .join(" "))
    .join(" / ");
}

function globalPresetModel(id: string, protocol: ProviderPresetProtocol): ProviderModel | undefined {
  let crossProtocolFallback: ProviderModel | undefined;
  for (const preset of PROVIDER_PRESETS) {
    for (const variant of preset.variants) {
      const matched = variant.models.find((model) => model.id === id);
      if (!matched) {
        continue;
      }
      if (variant.protocol === protocol) {
        return matched;
      }
      crossProtocolFallback ||= matched;
    }
  }
  return crossProtocolFallback;
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

function model(id: string, patch: Omit<ProviderModel, "id"> = {}): ProviderModel {
  return {
    ...patch,
    id,
    displayName: patch.displayName?.trim() || providerModelDisplayName(id),
  };
}

function providerModelFromCandidate(id: string, protocol: ProviderPresetProtocol): ProviderModel {
  // Leave sampling to the provider: several reasoning models reject temperature.
  return model(id, {
    capabilities: { tools: true },
    ...(protocol === "google" ? { providerOptions: { google: { thinking: { include_thoughts: true } } } } : {}),
  });
}

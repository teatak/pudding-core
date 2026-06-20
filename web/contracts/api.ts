// REST payload 的 web 侧镜像。Go 侧来源:internal/store/store.go(实体)
// 与 internal/api/server.go(请求/响应);字段名一一对应。
import { z } from "zod";

export const session = z.object({
  id: z.string(),
  title: z.string(),
  provider: z.string(), // provider profile 名;session 创建时必须显式写入
  model: z.string(),
  pinned: z.boolean(),
  pinnedOrder: z.number(),
  createdAt: z.string(), // RFC3339
  updatedAt: z.string(),
  lastActivityAt: z.string(),
  running: z.boolean(), // 读取时从 turns 派生,rail 运行态指示
});
export type Session = z.infer<typeof session>;

// provider profile 的设置视图:apiKey 来自本地配置,编辑时可回显;apiKeySet 用于列表状态。
export const providerProtocol = z.enum(["openai-compatible", "openai-responses", "google", "anthropic"]);

export const providerModelLimits = z.object({
  maxOutputTokens: z.number().optional(),
  maxToolLoops: z.number().optional(),
});

export const providerModelOptions = z.object({
  openai: z.record(z.string(), z.unknown()).optional(),
  google: z.record(z.string(), z.unknown()).optional(),
  anthropic: z.record(z.string(), z.unknown()).optional(),
});

export const providerModel = z.object({
  id: z.string(),
  displayName: z.string().optional(),
  contextWindow: z.number().optional(),
  capabilities: z
    .object({
      image: z.boolean().optional(),
      audio: z.boolean().optional(),
      tools: z.boolean().optional(),
    })
    .optional(),
  limits: providerModelLimits.optional(),
  providerOptions: providerModelOptions.optional(),
});
export type ProviderModel = z.infer<typeof providerModel>;

export const providerProfile = z.object({
  id: z.string(),
  displayName: z.string(),
  brand: z.string().optional(),
  protocol: providerProtocol,
  baseURL: z.string(),
  apiKey: z.string().optional(),
  apiKeySet: z.boolean(),
  // 配置的可选模型清单;选择器只显示这里的内容,没有默认模型语义。
  models: z.array(providerModel),
});
export type ProviderProfile = z.infer<typeof providerProfile>;

export const createProviderRequest = z.object({
  id: z.string().min(1),
  displayName: z.string().min(1),
  brand: z.string().optional(),
  protocol: providerProtocol,
  baseURL: z.string().optional(),
  apiKey: z.string().optional(),
  models: z.array(providerModel).optional(),
});

// apiKey 传非空才覆盖;清除走 DELETE 后重建
export const patchProviderRequest = z.object({
  displayName: z.string().optional(),
  brand: z.string().optional(),
  protocol: providerProtocol.optional(),
  baseURL: z.string().optional(),
  apiKey: z.string().optional(),
  models: z.array(providerModel).optional(),
});

export const listProvidersResponse = z.object({ providers: z.array(providerProfile) });

export const listModelsResponse = z.object({ models: z.array(z.string()) });

export const message = z.object({
  id: z.string(),
  sessionID: z.string(),
  turnID: z.string(),
  role: z.enum(["user", "assistant"]),
  text: z.string(),
  clientMessageID: z.string().optional(), // 仅 user message,overlay 对账键
  interrupted: z.boolean().optional(),
  createdAt: z.string(),
});
export type Message = z.infer<typeof message>;

export const submitRequest = z.object({
  clientMessageID: z.string().min(1),
  text: z.string().min(1),
});

// 202 新 turn;200 + duplicate=true 幂等重放
export const submitResponse = z.object({
  duplicate: z.boolean().optional(),
  turnID: z.string(),
  userMessageID: z.string().optional(),
});

export const listSessionsResponse = z.object({ sessions: z.array(session) });
export const listMessagesResponse = z.object({ messages: z.array(message) });
export const settingsResponse = z.object({ settings: z.record(z.string(), z.string()) });

// 409 响应体:submit → turn_running;cancel → no_running_turn;
// POST /providers 重名 → profile_exists
export const conflictResponse = z.object({
  error: z.enum(["turn_running", "no_running_turn", "profile_exists"]),
});

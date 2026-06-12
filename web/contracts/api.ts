// REST payload 的 web 侧镜像。Go 侧来源:internal/store/store.go(实体)
// 与 internal/api/server.go(请求/响应);字段名一一对应。
import { z } from "zod";

export const session = z.object({
  id: z.string(),
  title: z.string(),
  provider: z.string(), // provider profile 名,空 = 默认
  model: z.string(),
  createdAt: z.string(), // RFC3339
  updatedAt: z.string(),
  running: z.boolean(), // 读取时从 turns 派生,rail 运行态指示
});
export type Session = z.infer<typeof session>;

// provider profile 的脱敏视图:api_key 只进不出,读端点只回 apiKeySet
export const providerProfile = z.object({
  name: z.string(),
  type: z.enum(["openai-compatible", "google"]),
  baseURL: z.string(),
  apiKeySet: z.boolean(),
  // 默认模型是 profile 属性:模型名只在所属 profile 下有意义,无全局默认模型
  defaultModel: z.string(),
  // 配置的可选模型清单;选择器只显示这里的内容
  models: z.array(z.string()),
  extra: z.string().optional(),
  createdAt: z.string(),
  updatedAt: z.string(),
});
export type ProviderProfile = z.infer<typeof providerProfile>;

export const createProviderRequest = z.object({
  name: z.string().min(1),
  type: z.enum(["openai-compatible", "google"]),
  baseURL: z.string().optional(),
  apiKey: z.string().optional(),
  defaultModel: z.string().optional(),
  models: z.array(z.string()).optional(),
  extra: z.string().optional(),
});

// apiKey 传非空才覆盖;清除走 DELETE 后重建
export const patchProviderRequest = z.object({
  type: z.enum(["openai-compatible", "google"]).optional(),
  baseURL: z.string().optional(),
  apiKey: z.string().optional(),
  defaultModel: z.string().optional(),
  models: z.array(z.string()).optional(),
  extra: z.string().optional(),
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

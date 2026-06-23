// REST payload 的 web 侧镜像。Go 侧来源:internal/store/store.go(实体)
// 与 internal/api/server.go(请求/响应);字段名一一对应。
import { z } from "zod";

export const session = z.object({
  id: z.string(),
  title: z.string(),
  provider: z.string(), // provider profile 名;session 创建时必须显式写入
  model: z.string(),
  activeMode: z.enum(["chat", "research", "workspace"]),
  modeLease: z.enum(["none", "session"]),
  workspaceDirs: z.array(z.string()).optional(),
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

export const probeProviderModelsRequest = z.object({
  protocol: providerProtocol,
  baseURL: z.string().optional(),
  apiKey: z.string().optional(),
});

export const contentPart = z.discriminatedUnion("type", [
  z.object({ type: z.literal("text"), text: z.string() }),
  z.object({ type: z.literal("thought"), text: z.string() }),
  z.object({
    type: z.literal("tool_use"),
    id: z.string().optional(),
    name: z.string().optional(),
    args: z.unknown().optional(),
  }),
  z.object({
    type: z.literal("tool_result"),
    id: z.string().optional(),
    name: z.string().optional(),
    ok: z.boolean().optional(),
    content: z.string().optional(),
    summaryKind: z.string().optional(),
    summaryCount: z.number().optional(),
  }),
]);
export type ContentPart = z.infer<typeof contentPart>;

export const message = z
  .object({
    id: z.string(),
    sessionID: z.string(),
    turnID: z.string(),
    role: z.enum(["user", "assistant", "tool", "summary"]),
    kind: z.enum(["text", "thought", "tool_use", "tool_result", "summary"]),
    text: z.string(),
    parts: z.array(contentPart),
    turnIndex: z.number().int(),
    clientMessageID: z.string().optional(), // 仅 user message,overlay 对账键
    interrupted: z.boolean().optional(),
    createdAt: z.string(),
  })
  .superRefine((value, ctx) => {
    if (value.text.trim() && value.parts.length === 0) {
      ctx.addIssue({
        code: z.ZodIssueCode.custom,
        message: "message.parts is required when message.text is present",
        path: ["parts"],
      });
    }
  });
export type Message = z.infer<typeof message>;

export const conversationTurn = z.object({
  id: z.string(),
  sessionID: z.string(),
  clientMessageID: z.string(),
  status: z.enum(["running", "completed", "failed", "cancelled"]),
  provider: z.string().optional(),
  model: z.string().optional(),
  mode: z.enum(["chat", "research", "workspace"]).optional(),
  error: z.string().optional(),
  createdAt: z.string(),
  updatedAt: z.string(),
  messages: z.array(message),
});
export type ConversationTurn = z.infer<typeof conversationTurn>;

export const queuedInputStatus = z.enum(["queued", "editing", "cancelled", "promoted"]);

export const queuedInput = z.object({
  sessionID: z.string(),
  clientMessageID: z.string(),
  text: z.string(),
  status: queuedInputStatus,
  provider: z.string().optional(),
  model: z.string().optional(),
  mode: z.enum(["chat", "research", "workspace"]).optional(),
  modelConfig: z.unknown().optional(),
  turnID: z.string().optional(),
  createdAt: z.string(),
  updatedAt: z.string(),
});
export type QueuedInput = z.infer<typeof queuedInput>;

export const submitRequest = z.object({
  clientMessageID: z.string().min(1),
  text: z.string().min(1),
});

// 202 新 turn 或 queued input;200 + duplicate=true 幂等重放
export const submitResponse = z.object({
  duplicate: z.boolean().optional(),
  queued: z.boolean().optional(),
  turnID: z.string().optional(),
  userMessageID: z.string().optional(),
  status: queuedInputStatus.optional(),
  clientMessageID: z.string().optional(),
});

export const pendingApproval = z.object({
  id: z.string(),
  sessionID: z.string(),
  turnID: z.string(),
  callID: z.string().optional(),
  approvalKind: z.string(),
  targetMode: z.enum(["chat", "research", "workspace"]).optional(),
  title: z.string().optional(),
  reason: z.string().optional(),
  risk: z.string().optional(),
  payload: z.unknown().optional(),
  createdAt: z.string(),
});
export type PendingApproval = z.infer<typeof pendingApproval>;

export const listPendingApprovalsResponse = z.object({ approvals: z.array(pendingApproval) });

export const sessionUsage = z.object({
  sessionID: z.string(),
  contextWindow: z.number(),
  contextEstimatedTokens: z.number(),
  messageEstimatedTokens: z.number(),
  promptOverheadEstimatedTokens: z.number(),
  systemPromptEstimatedTokens: z.number(),
  toolsSchemaEstimatedTokens: z.number(),
  autoCompactThresholdTokens: z.number(),
  requestCount: z.number(),
  lastPromptTokens: z.number(),
  lastInputUncachedTokens: z.number(),
  lastInputCachedTokens: z.number(),
  lastCacheCreationTokens: z.number(),
  lastOutputContentTokens: z.number(),
  lastOutputReasoningTokens: z.number(),
  lastOutputTokens: z.number(),
  cumulativeInputUncachedTokens: z.number(),
  cumulativeInputCachedTokens: z.number(),
  cumulativeCacheCreationTokens: z.number(),
  cumulativeOutputContentTokens: z.number(),
  cumulativeOutputReasoningTokens: z.number(),
  cumulativeInputTokens: z.number(),
  cumulativeOutputTokens: z.number(),
  cumulativeTotalTokens: z.number(),
  updatedAt: z.string().optional(),
});
export type SessionUsage = z.infer<typeof sessionUsage>;

export const dailyUsageStat = z.object({
  date: z.string(),
  requestCount: z.number(),
  inputUncachedTokens: z.number(),
  inputCachedTokens: z.number(),
  cacheCreationTokens: z.number(),
  outputContentTokens: z.number(),
  outputReasoningTokens: z.number(),
  totalTokens: z.number(),
});
export type DailyUsageStat = z.infer<typeof dailyUsageStat>;
export const dailyUsageResponse = z.object({ days: z.array(dailyUsageStat) });

export const listSessionsResponse = z.object({ sessions: z.array(session) });
export const listMessagesResponse = z.object({ messages: z.array(message), hasMore: z.boolean() });
export const listTurnsResponse = z.object({ turns: z.array(conversationTurn), hasMore: z.boolean() });
export const listQueuedInputsResponse = z.object({ queuedInputs: z.array(queuedInput) });
export const patchQueuedInputRequest = z.object({
  text: z.string().min(1).optional(),
  status: z.enum(["queued", "editing", "cancelled"]).optional(),
});
export const settingsResponse = z.object({ settings: z.record(z.string(), z.string()) });

export const builtinTool = z.object({
  id: z.string(),
  description: z.string(),
  capability: z.enum(["chat", "research", "workspace"]),
  inputSchema: z.unknown().optional(),
});
export type BuiltinTool = z.infer<typeof builtinTool>;

export const listBuiltinToolsResponse = z.object({ tools: z.array(builtinTool) });

export const webToolProvider = z.object({
  name: z.string(),
  apiKey: z.string().optional(),
  apiKeySet: z.boolean(),
});
export type WebToolProvider = z.infer<typeof webToolProvider>;

export const webToolsConfig = z.object({
  searchProvider: z.string().optional(),
  fetchProvider: z.string().optional(),
  providers: z.array(webToolProvider),
});
export type WebToolsConfig = z.infer<typeof webToolsConfig>;

export const patchWebToolsRequest = z.object({
  searchProvider: z.string().optional(),
  fetchProvider: z.string().optional(),
  providers: z
    .record(
      z.string(),
      z.object({
        apiKey: z.string().optional(),
      }),
    )
    .optional(),
});

// 409 响应体:submit → turn_running;cancel → no_running_turn;
// POST /providers 重名 → profile_exists
export const conflictResponse = z.object({
  error: z.enum(["turn_running", "no_running_turn", "profile_exists"]),
});

// REST payload 的 web 侧镜像。Go 侧来源:internal/store/store.go(实体)
// 与 internal/api/server.go(请求/响应);字段名一一对应。
import { z } from "zod";

export const session = z.object({
  id: z.string(),
  title: z.string(),
  model: z.string(),
  createdAt: z.string(), // RFC3339
  updatedAt: z.string(),
});
export type Session = z.infer<typeof session>;

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

// 409 响应体:submit → turn_running;cancel → no_running_turn
export const conflictResponse = z.object({
  error: z.enum(["turn_running", "no_running_turn"]),
});

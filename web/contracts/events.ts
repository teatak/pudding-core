// 事件协议的 web 侧镜像。Go 侧唯一来源:internal/event/types.go;
// 字段名一一对应,改动必须两边同步并更新 docs/contracts-checklist.md。
// 本目录在轨道 E 脚手架落地后由前端直接 import。
import { z } from "zod";

export const turnStartedEvent = z.object({
  kind: z.literal("turn.started"),
  seq: z.number().int().positive(),
  sessionID: z.string(),
  turnID: z.string(),
  clientMessageID: z.string(),
  userMessageID: z.string(),
});

// turn.delta 不落库、无 seq;丢失由 turn.completed 后 refetch 兜底
export const turnDeltaEvent = z.object({
  kind: z.literal("turn.delta"),
  sessionID: z.string(),
  turnID: z.string(),
  part: z.enum(["text", "thought"]),
  delta: z.string(),
});

export const turnToolEvent = z.object({
  kind: z.literal("turn.tool"),
  sessionID: z.string(),
  turnID: z.string(),
  callID: z.string(),
  name: z.string().optional(),
  phase: z.enum(["streaming_args", "running", "ok", "error"]),
  argsDelta: z.string().optional(),
  summary: z.string().optional(),
  summaryKind: z.string().optional(),
  summaryCount: z.number().optional(),
});

export const turnCompletedEvent = z.object({
  kind: z.literal("turn.completed"),
  seq: z.number().int().positive(),
  sessionID: z.string(),
  turnID: z.string(),
  assistantMessageID: z.string(),
});

export const turnFailedEvent = z.object({
  kind: z.literal("turn.failed"),
  seq: z.number().int().positive(),
  sessionID: z.string(),
  turnID: z.string(),
  error: z.string(),
  // 失败前已有部分输出时,半截 message 保留并标记 interrupted
  assistantMessageID: z.string().optional(),
  interrupted: z.boolean().optional(),
});

export const turnCancelledEvent = z.object({
  kind: z.literal("turn.cancelled"),
  seq: z.number().int().positive(),
  sessionID: z.string(),
  turnID: z.string(),
  assistantMessageID: z.string().optional(),
  interrupted: z.boolean().optional(),
});

export const inputQueuedEvent = z.object({
  kind: z.literal("input.queued"),
  seq: z.number().int().positive(),
  sessionID: z.string(),
  clientMessageID: z.string(),
  text: z.string(),
  status: z.enum(["queued", "editing", "cancelled", "promoted"]),
});

export const inputUpdatedEvent = z.object({
  kind: z.literal("input.updated"),
  seq: z.number().int().positive(),
  sessionID: z.string(),
  clientMessageID: z.string(),
  text: z.string(),
  status: z.enum(["queued", "editing", "cancelled", "promoted"]),
});

export const approvalRequestedEvent = z.object({
  kind: z.literal("approval.requested"),
  sessionID: z.string(),
  turnID: z.string(),
  callID: z.string().optional(),
  approvalID: z.string(),
  approvalKind: z.string(),
  title: z.string().optional(),
  reason: z.string().optional(),
  risk: z.string().optional(),
  payload: z.unknown().optional(),
});

export const approvalResolvedEvent = z.object({
  kind: z.literal("approval.resolved"),
  sessionID: z.string(),
  turnID: z.string(),
  callID: z.string().optional(),
  approvalID: z.string(),
  approvalKind: z.string(),
  status: z.enum(["approved", "denied", "cancelled", "expired"]),
  reason: z.string().optional(),
  payload: z.unknown().optional(),
});

// session.titled 不落库、无 seq:自动标题写回(provisional / LLM 各一次),
// 丢失由 sessions 轮询兜底
export const sessionTitledEvent = z.object({
  kind: z.literal("session.titled"),
  sessionID: z.string(),
  title: z.string(),
});

export const pingEvent = z.object({
  kind: z.literal("ping"),
  sessionID: z.string(),
});

export const sessionEvent = z.discriminatedUnion("kind", [
  turnStartedEvent,
  turnDeltaEvent,
  turnToolEvent,
  turnCompletedEvent,
  turnFailedEvent,
  turnCancelledEvent,
  inputQueuedEvent,
  inputUpdatedEvent,
  approvalRequestedEvent,
  approvalResolvedEvent,
  sessionTitledEvent,
  pingEvent,
]);

export type SessionEvent = z.infer<typeof sessionEvent>;

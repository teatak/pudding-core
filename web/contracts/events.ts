// 事件协议的 web 侧镜像。Go 侧唯一来源:internal/event/types.go;
// 字段名一一对应,改动必须两边同步并更新 docs/contracts-checklist.md。
// 本目录在轨道 E 脚手架落地后由前端直接 import。
import { z } from "zod";

import { attachment, backgroundProcess } from "./api";

export const turnStartedEvent = z.object({
  kind: z.literal("turn.started"),
  seq: z.number().int().positive(),
  sessionID: z.string(),
  turnID: z.string(),
  clientMessageID: z.string(),
  userMessageID: z.string(),
  text: z.string().optional(),
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
  phase: z.enum(["streaming_args", "running", "output", "ok", "error"]),
  argsDelta: z.string().optional(),
  stream: z.enum(["stdout", "stderr"]).optional(),
  ok: z.boolean().optional(),
  content: z.string().optional(),
  summaryKind: z.string().optional(),
  summaryCount: z.number().optional(),
  attachments: z.array(attachment).optional(),
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

export const inputSteeredEvent = z.object({
  kind: z.literal("input.steered"),
  seq: z.number().int().positive(),
  sessionID: z.string(),
  turnID: z.string(),
  clientMessageID: z.string(),
  userMessageID: z.string(),
  text: z.string(),
});

export const audioBindingsEvent = z.object({
  kind: z.literal("audio.bindings"),
  sessionID: z.string(),
  inputOwner: z.string().default(""),
  inputMode: z.union([z.literal(""), z.literal("transcribe"), z.literal("raw")]).default(""),
  inputLevel: z.number().default(0),
});

export const audioInputLevelEvent = z.object({
  kind: z.literal("audio.input_level"),
  sessionID: z.string(),
  inputLevel: z.number().default(0),
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

const backgroundProcessEventFields = {
  sessionID: z.string(),
  turnID: z.string().optional(),
  callID: z.string().optional(),
  payload: backgroundProcess,
};

export const processStartedEvent = z.object({
  kind: z.literal("process.started"),
  ...backgroundProcessEventFields,
});

export const processFinishedEvent = z.object({
  kind: z.literal("process.finished"),
  ...backgroundProcessEventFields,
});

export const processStoppedEvent = z.object({
  kind: z.literal("process.stopped"),
  ...backgroundProcessEventFields,
});

export const processRemovedEvent = z.object({
  kind: z.literal("process.removed"),
  ...backgroundProcessEventFields,
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
  inputSteeredEvent,
  audioBindingsEvent,
  audioInputLevelEvent,
  approvalRequestedEvent,
  approvalResolvedEvent,
  processStartedEvent,
  processFinishedEvent,
  processStoppedEvent,
  processRemovedEvent,
  sessionTitledEvent,
  pingEvent,
]);

export type SessionEvent = z.infer<typeof sessionEvent>;
